package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

func TestServeRefusesWrongUser(t *testing.T) {
	if u, err := user.Current(); err == nil && u.Username == "hestiaweb" {
		t.Skip("running as hestiaweb")
	}
	var out, errOut bytes.Buffer
	code := run([]string{"serve", "--listen", "127.0.0.1:0"}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "hostisan must run as hestiaweb") {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestServeRejectsBadFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"serve", "--nope"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestDevLogRedactsSecrets(t *testing.T) {
	cases := map[string][]string{
		"v-check-user-2fa":  {"bob", "123456"},
		"v-log-user-login":  {"bob", "203.0.113.9", "success", "csrf-token-value", "Mozilla"},
		"v-log-user-logout": {"bob", "csrf-token-value"},
	}
	for cmd, args := range cases {
		got := strings.Join(logArgs(cmd, args), " ")
		if strings.Contains(got, "123456") || strings.Contains(got, "csrf-token-value") {
			t.Errorf("%s logged a secret: %q", cmd, got)
		}
	}
	if got := logArgs("v-list-user", []string{"bob", "json"}); strings.Join(got, " ") != "bob json" {
		t.Errorf("ordinary args changed: %v", got)
	}
}

// --dev wraps the runner in logged; the legacy /api/ needs RunAny through it, and the
// pass-through's arguments (passwords, zone files) never reach the log.
func TestDevLogForwardsRunAny(t *testing.T) {
	f, err := runner.LoadFixtures(filepath.Join("..", "..", "testdata", "fixtures", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	var r runner.Runner = logged{f}
	a, ok := r.(runner.AnyRunner)
	if !ok {
		t.Fatal("logged hides RunAny")
	}
	if !a.Exists("v-list-users") || a.Exists("v-nope") {
		t.Error("Exists not forwarded")
	}
	_, _ = a.RunAny(context.Background(), "v-list-users", "s3cret-arg") // no fixture for it: an error, still logged
	if _, err := a.RunAny(context.Background(), "v-nope"); !errors.Is(err, runner.ErrNoCommand) {
		t.Errorf("RunAny(v-nope) = %v", err)
	}
	if strings.Contains(buf.String(), "s3cret-arg") || !strings.Contains(buf.String(), "v-list-users") {
		t.Errorf("log = %q", buf.String())
	}
	for cmd, args := range map[string][]string{
		"v-check-access-key": {"Zx9yWv8uTs7rQp6oNm5l", "the-secret", "v-list-users", "127.0.0.1", "json"},
		"v-check-api-key":    {"/usr/local/hestia/data/keys/the-secret", "127.0.0.1"},
	} {
		if got := strings.Join(logArgs(cmd, args), " "); strings.Contains(got, "the-secret") {
			t.Errorf("%s logged a secret: %q", cmd, got)
		}
	}
}

// serve clears the FileGator sessions a previous run handed off before it listens
// (parity decision 16), and leaves hestia-php's own sessions alone.
func TestServeSweepsFileManagerSessionsAtStart(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, "sess_"+strings.Repeat("ab", 32))
	theirs := filepath.Join(dir, "sess_0123456789abcdefghijklmnop")
	for _, p := range []string{ours, theirs} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	old := serveContext
	serveContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { serveContext = old }()

	out := &syncBuffer{}
	done := make(chan int)
	go func() {
		done <- run([]string{"serve", "--fixtures", filepath.Join("..", "..", "testdata", "fixtures", "dev"),
			"--listen", "127.0.0.1:0", "--php-session-dir", dir, "--locale-dir", t.TempDir()}, out, out)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "listening on") {
		if time.Now().After(deadline) {
			t.Fatalf("serve never listened: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(ours); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("panel-written session survived the start: %v", err)
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Errorf("hestia-php's own session was removed: %v", err)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d: %q", code, out.String())
	}
}

// syncBuffer is a bytes.Buffer safe for serve's goroutines and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// slowRunner is a v-* command that takes d to finish and can't be interrupted, like
// Sudo once the script has started.
type slowRunner struct {
	d        time.Duration
	started  chan struct{}
	finished atomic.Int32
}

func (s *slowRunner) Run(context.Context, string, ...string) (runner.Result, error) {
	s.started <- struct{}{}
	time.Sleep(s.d)
	s.finished.Add(1)
	return runner.Result{}, nil
}

// Stopping the panel (systemctl stop/restart) must let a running v-* command finish:
// KillMode=mixed SIGKILLs sudo and the script once serve exits, which can leave a
// half-written user.conf.
func TestDrainWaitsForRunningCommands(t *testing.T) {
	slow := &slowRunner{d: 400 * time.Millisecond, started: make(chan struct{}, 2)}
	cmds := &runner.Inflight{Runner: slow}
	go cmds.Run(context.Background(), "v-add-user")
	go cmds.Run(context.Background(), "v-add-web-domain")
	<-slow.started
	<-slow.started

	start := time.Now()
	if err := drain(&http.Server{}, cmds, jobs.NewRegistry(0, nil), 5*time.Second, 0); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n := slow.finished.Load(); n != 2 {
		t.Fatalf("drain returned with %d of 2 commands finished", n)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("drain took %v after the commands finished", elapsed)
	}
}

func TestDrainGivesUpAtLimit(t *testing.T) {
	slow := &slowRunner{d: 3 * time.Second, started: make(chan struct{}, 1)}
	cmds := &runner.Inflight{Runner: slow}
	go cmds.Run(context.Background(), "v-add-user")
	<-slow.started

	start := time.Now()
	if err := drain(&http.Server{}, cmds, jobs.NewRegistry(0, nil), 100*time.Millisecond, 0); err == nil {
		t.Fatal("drain = nil with a command still running")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("drain took %v, want about the 100ms limit", elapsed)
	}
}

func TestDrainIdleReturnsAtOnce(t *testing.T) {
	cmds := &runner.Inflight{Runner: &slowRunner{started: make(chan struct{}, 1)}}
	start := time.Now()
	if err := drain(&http.Server{}, cmds, jobs.NewRegistry(0, nil), 5*time.Second, 0); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("idle drain took %v", elapsed)
	}
}

// The wait covers the longest command timeout (legacy API and app installs: 300 s)
// up to the runner's backstop; hostisan.service's TimeoutStopSec (320) must stay above it.
func TestShutdownLimitCoversLongestCommand(t *testing.T) {
	worst := runner.MaxRunTime(300 * time.Second)
	if worst < 306*time.Second {
		t.Fatalf("MaxRunTime(300s) = %v, want at least 300 + 5 (kill-after) + 1 (output wait)", worst)
	}
	if shutdownLimit < worst || shutdownLimit >= 320*time.Second {
		t.Fatalf("shutdownLimit = %v, want %v <= limit < 320s (TimeoutStopSec)", shutdownLimit, worst)
	}
}

// A stopping panel waits for a background job that ends within the grace period.
func TestDrainWaitsForJobs(t *testing.T) {
	reg := jobs.NewRegistry(0, nil)
	id, _ := reg.Start(context.Background(), jobs.Spec{Key: "k"}, func(ctx context.Context, j *jobs.Job) (any, error) {
		time.Sleep(200 * time.Millisecond)
		return nil, jobs.Interrupted(ctx)
	})
	cmds := &runner.Inflight{Runner: &slowRunner{started: make(chan struct{}, 1)}}
	if err := drain(&http.Server{}, cmds, reg, 5*time.Second, 2*time.Second); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if st, _ := reg.Get(id); st.State != jobs.Done {
		t.Fatalf("job = %+v", st)
	}
}

// Past the grace period the job is asked to stop and ends "interrupted" before drain
// returns (Plan 6 C6 D2: a restart used to drop the job's remaining steps unseen).
func TestDrainStopsJobsAtGrace(t *testing.T) {
	reg := jobs.NewRegistry(0, nil)
	id, _ := reg.Start(context.Background(), jobs.Spec{Key: "k"}, func(ctx context.Context, j *jobs.Job) (any, error) {
		for jobs.Interrupted(ctx) == nil {
			time.Sleep(5 * time.Millisecond)
		}
		return nil, jobs.Interrupted(ctx)
	})
	cmds := &runner.Inflight{Runner: &slowRunner{started: make(chan struct{}, 1)}}
	if err := drain(&http.Server{}, cmds, reg, 5*time.Second, 50*time.Millisecond); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if st, _ := reg.Get(id); st.State != jobs.Failed || !errors.Is(st.Err, jobs.ErrInterrupted) {
		t.Fatalf("job = %+v", st)
	}
}

// The grace period leaves room, before the shutdown limit, for the command a job is
// running when asked to stop and for its clean-up.
func TestJobGraceLeavesRoomToCleanUp(t *testing.T) {
	if jobGrace <= 0 || shutdownLimit-jobGrace < 30*time.Second {
		t.Fatalf("jobGrace = %v with shutdownLimit %v", jobGrace, shutdownLimit)
	}
}

// At start the panel loads the jobs file from --state-dir: a job the last run left
// running is saved back failed and "interrupted".
func TestServeMarksInterruptedJobsAtStart(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "jobs.json")
	id := strings.Repeat("ab", 16)
	if err := os.WriteFile(file, []byte(`{"jobs":[{"id":"`+id+`","key":"web:admin:a.example","owner":"admin",`+
		`"title":"WordPress","state":"running","step":"Fetching the files","steps":["Fetching the files"],`+
		`"started":"2026-10-30T12:00:00Z","finished":"0001-01-01T00:00:00Z"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	old := serveContext
	serveContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { serveContext = old }()

	out := &syncBuffer{}
	done := make(chan int)
	go func() {
		done <- run([]string{"serve", "--fixtures", filepath.Join("..", "..", "testdata", "fixtures", "dev"),
			"--listen", "127.0.0.1:0", "--php-session-dir", t.TempDir(), "--locale-dir", t.TempDir(),
			"--state-dir", dir}, out, out)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "listening on") {
		if time.Now().After(deadline) {
			t.Fatalf("serve never listened: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d: %q", code, out.String())
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, `"state":"failed"`) || !strings.Contains(s, `"code":"interrupted"`) {
		t.Fatalf("jobs file after start: %s", s)
	}
}

// At start the panel loads the signed-in sessions from --state-dir, so a restart (an
// update restarts hostisan.service) signs nobody out; one idle past
// INACTIVE_SESSION_TIMEOUT (60 min in the fixtures) is dropped, and the file stays
// private to the panel user.
func TestServeKeepsSessionsInStateDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sessions.json")
	live, stale := strings.Repeat("1", 64), strings.Repeat("2", 64)
	rec := func(k string, last time.Time) string {
		return `{"key":"` + k + `","token":"` + strings.Repeat("ab", 16) + `","user":"admin","role":"admin",` +
			`"last_activity":"` + last.UTC().Format(time.RFC3339) + `","ip":"203.0.113.7"}`
	}
	body := `{"version":1,"sessions":[` + rec(live, time.Now().Add(-time.Minute)) + `,` +
		rec(stale, time.Now().Add(-2*time.Hour)) + `]}`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	old := serveContext
	serveContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { serveContext = old }()

	out := &syncBuffer{}
	done := make(chan int)
	go func() {
		done <- run([]string{"serve", "--fixtures", filepath.Join("..", "..", "testdata", "fixtures", "dev"),
			"--listen", "127.0.0.1:0", "--php-session-dir", t.TempDir(), "--locale-dir", t.TempDir(),
			"--state-dir", dir}, out, out)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "listening on") {
		if time.Now().After(deadline) {
			t.Fatalf("serve never listened: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d: %q", code, out.String())
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, live) || strings.Contains(s, stale) {
		t.Fatalf("sessions file after start: %s", s)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o600 {
		t.Fatalf("sessions file mode %v, want 0600", fi.Mode().Perm())
	}
}

// The state dir holds what lets a cookie back in (sessions.json) and what the panel
// undoes at start (jobs.json): one others can write to is not trusted.
func TestCheckStateDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkStateDir(dir); err != nil {
		t.Fatalf("0700 dir refused: %v", err)
	}
	for _, mode := range []os.FileMode{0o770, 0o707, 0o777, 0o720} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := checkStateDir(dir); err == nil {
			t.Errorf("mode %v accepted", mode)
		}
	}
	os.Chmod(dir, 0o700)
	if err := checkStateDir(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing dir accepted")
	}
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o600)
	if err := checkStateDir(file); err == nil {
		t.Error("a file accepted as the state dir")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := checkStateDir(link); err == nil {
		t.Error("a symlink accepted as the state dir")
	}
}

// serve does not load or write sessions and jobs in a state dir others can write
// to, and says so in the log.
func TestServeRefusesAnUnsafeStateDir(t *testing.T) {
	var logs syncBuffer
	oldLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(oldLog)

	dir := t.TempDir()
	file := filepath.Join(dir, "sessions.json")
	body := `{"version":1,"sessions":[{"key":"` + strings.Repeat("1", 64) + `","token":"` + strings.Repeat("ab", 16) +
		`","user":"admin","role":"admin","last_activity":"2020-01-01T00:00:00Z","ip":"203.0.113.7"}]}`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	old := serveContext
	serveContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { serveContext = old }()

	out := &syncBuffer{}
	done := make(chan int)
	go func() {
		done <- run([]string{"serve", "--fixtures", filepath.Join("..", "..", "testdata", "fixtures", "dev"),
			"--listen", "127.0.0.1:0", "--php-session-dir", t.TempDir(), "--locale-dir", t.TempDir(),
			"--state-dir", dir}, out, out)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "listening on") {
		if time.Now().After(deadline) {
			t.Fatalf("serve never listened: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d: %q", code, out.String())
	}
	// untouched: the stale entry is still there and no jobs file was made
	if b, _ := os.ReadFile(file); string(b) != body {
		t.Fatalf("sessions file rewritten: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs.json")); err == nil {
		t.Fatal("jobs.json written to an unsafe dir")
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "state dir") {
		t.Fatalf("log = %q", logs.String())
	}
}
