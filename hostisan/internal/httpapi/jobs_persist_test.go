package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/webapp"
)

// notifyRunner records v-add-user-notification calls.
type notifyRunner struct {
	mu    sync.Mutex
	calls [][]string
	fail  bool
}

func (n *notifyRunner) Run(_ context.Context, cmd string, args ...string) (runner.Result, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, append([]string{cmd}, args...))
	if n.fail {
		return runner.Result{Stdout: []byte("Error: x\n"), Exit: 3}, nil
	}
	return runner.Result{}, nil
}

// A job a restart cut short leaves its owner a panel notification (the bell), with the
// task's title escaped for the notice's HTML (Plan 6 C6 D2).
func TestNotifyInterrupted(t *testing.T) {
	r := &notifyRunner{}
	NotifyInterrupted(context.Background(), r, []jobs.Status{
		{Owner: "bob", Title: "WordPress"},
		{Owner: "admin", Title: "Save <nginx> configuration"},
		{Owner: "", Title: "nobody"},
	})
	want := [][]string{
		{"v-add-user-notification", "bob", interruptedTopic, `"WordPress" ` + interruptedNotice},
		{"v-add-user-notification", "admin", interruptedTopic, `"Save &lt;nginx&gt; configuration" ` + interruptedNotice},
	}
	if !slices.EqualFunc(r.calls, want, slices.Equal) {
		t.Fatalf("calls:\n%q\nwant:\n%q", r.calls, want)
	}
	if !runner.Allowed("v-add-user-notification") {
		t.Error("v-add-user-notification is not on the allowlist")
	}
	// A failing notification doesn't stop the others.
	r = &notifyRunner{fail: true}
	NotifyInterrupted(context.Background(), r, []jobs.Status{{Owner: "a", Title: "x"}, {Owner: "b", Title: "y"}})
	if len(r.calls) != 2 {
		t.Errorf("calls = %q", r.calls)
	}
}

// JobFailure saves an error as the API would show it; a raw error's text isn't kept.
func TestJobFailure(t *testing.T) {
	f := JobFailure(&runner.CommandError{Cmd: "v-add-database", Exit: 3, Output: "Error: db exists"})
	if f.Code == "" || !strings.Contains(f.Message, "db exists") {
		t.Errorf("command error = %+v", f)
	}
	if f := JobFailure(errors.New("secret /home/x")); f.Code != "internal" || strings.Contains(f.Message, "secret") {
		t.Errorf("raw error = %+v", f)
	}
}

// After a restart, the app install's job id answers "failed / interrupted" instead of
// 404, to its owner only.
func TestWebAppJobInterruptedAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	before := jobs.NewRegistry(0, nil)
	t.Cleanup(before.Close) // before the temp dir is removed
	if _, err := before.Persist(path, JobFailure); err != nil {
		t.Fatal(err)
	}
	hold, stepped := make(chan struct{}), make(chan struct{})
	var id string
	defer func() { close(hold); <-before.Done(id) }() // before the temp dir goes
	id, _ = before.Start(context.Background(), jobs.Spec{Key: webJobKey("admin", "plain.example.org"), Owner: "admin", Title: "WordPress"},
		func(ctx context.Context, j *jobs.Job) (any, error) {
			j.Step("Fetching the files") // saved before Step returns
			close(stepped)
			<-hold
			return nil, nil
		})
	<-stepped

	after := jobs.NewRegistry(0, nil)
	t.Cleanup(after.Close)
	interrupted, err := after.Persist(path, JobFailure)
	if err != nil || len(interrupted) != 1 {
		t.Fatalf("Persist = %+v, %v", interrupted, err)
	}
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	e.s.jobs = after
	var st JobStatus
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps/jobs/"+id, nil, &st); code != 200 ||
		st.State != "failed" || st.Error == nil || st.Error.Code != jobs.InterruptedCode ||
		st.Error.Message != jobs.InterruptedMsg || st.Step != "Fetching the files" {
		t.Fatalf("job after restart: %d %+v", code, st)
	}
}

// siteRunner is a box for one WordPress site: public_html holds the user's keep.txt,
// then WordPress once `wp core download` ran; v-delete-fs-* remove entries. hold
// blocks one command forever, as a panel killed during it never sees it end.
type siteRunner struct {
	mu    sync.Mutex
	files []string
	calls []string
	hold  string
	held  chan struct{}
}

func (s *siteRunner) Run(_ context.Context, cmd string, args ...string) (runner.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, cmd+" "+strings.Join(args, " "))
	if cmd == s.hold {
		s.mu.Unlock()
		close(s.held)
		select {}
	}
	defer s.mu.Unlock()
	const pub = "/home/bob/web/b.example/public_html/"
	switch {
	case cmd == "v-run-cli-cmd" && slices.Contains(args, "ls"):
		return runner.Result{Stdout: []byte(strings.Join(s.files, "\n") + "\n")}, nil
	case cmd == "v-run-cli-cmd" && slices.Contains(args, "core"):
		s.files = append(s.files, "wp-admin/", "wp-includes/", "index.php")
	case cmd == "v-delete-fs-directory" || cmd == "v-delete-fs-file":
		name := strings.TrimPrefix(args[1], pub)
		s.files = slices.DeleteFunc(s.files, func(f string) bool { return strings.TrimSuffix(f, "/") == name })
	}
	return runner.Result{}, nil
}

func (s *siteRunner) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.files)
}

// The panel is killed in the middle of a WordPress install (after the files arrived,
// before wp-config): the next start removes the files the install added (with the
// public wp-admin/setup-config.php), keeps the user's, marks the job interrupted and
// rings the owner's bell (Plan 6 C6 D2 follow-up).
func TestKilledInstallCleanedAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	box := &siteRunner{files: []string{"keep.txt"}, hold: "v-add-database", held: make(chan struct{})}
	before := jobs.NewRegistry(0, nil)
	t.Cleanup(before.Close) // before the temp dir is removed
	if _, err := before.Persist(path, JobFailure); err != nil {
		t.Fatal(err)
	}
	inst, _ := webapp.Get("wordpress")
	tg := &webapp.Target{Domain: "b.example", Home: "/home/bob", IP: "203.0.113.5",
		DB: webapp.Database{Host: "localhost", Name: "bob_wp", User: "bob_wp", Password: "pw", Create: true}}
	opts := map[string]string{"php_version": "8.3", "language": "en_US", "site_name": "B", "username": "a",
		"email": "e@x.org", "password": "pw"}
	id, _ := before.Start(context.Background(), jobs.Spec{Key: webJobKey("bob", "b.example"), Owner: "bob", Title: "WordPress"},
		func(ctx context.Context, j *jobs.Job) (any, error) {
			return nil, webapp.Install(ctx, inst, tg, opts, webAppHost(j, box, "bob", ""), webapp.Env{}, j.Step)
		})
	<-box.held // killed here: the job never ends, nothing of `before` runs again
	if got := box.list(); !slices.Contains(got, "wp-admin/") {
		t.Fatalf("files before the kill: %q", got)
	}

	after := jobs.NewRegistry(0, nil)
	t.Cleanup(after.Close)
	restarted := &siteRunner{files: box.list()}
	interrupted, err := after.PersistRecover(path, JobFailure, RecoverInterrupted(context.Background(), restarted))
	if err != nil || len(interrupted) != 1 || interrupted[0].ID != id {
		t.Fatalf("PersistRecover = %+v, %v", interrupted, err)
	}
	if got := restarted.list(); !slices.Equal(got, []string{"keep.txt"}) {
		t.Errorf("files after the start: %q (calls %q)", got, restarted.calls)
	}
	if st, _ := after.Get(id); st.State != jobs.Failed || st.Err == nil || JobFailure(st.Err).Code != jobs.InterruptedCode {
		t.Errorf("job = %+v", st)
	}
	bell := &notifyRunner{}
	NotifyInterrupted(context.Background(), bell, interrupted)
	if len(bell.calls) != 1 || bell.calls[0][0] != "v-add-user-notification" || bell.calls[0][1] != "bob" {
		t.Errorf("bell = %q", bell.calls)
	}
}

// Recovery refuses a record that isn't a web app job of its owner's own domain, and a
// job without a record; nothing runs for them.
func TestRecoverInterruptedRefuses(t *testing.T) {
	data := func(dir string) json.RawMessage {
		b, _ := json.Marshal(webapp.Undo{Dir: dir, Before: []string{"keep.txt"}})
		return b
	}
	for _, st := range []jobs.Status{
		{Key: webJobKey("bob", "b.example"), Owner: "eve", Data: data("/home/bob/web/b.example/public_html")},
		{Key: webJobKey("bob", "b.example"), Owner: "bob", Data: data("/home/bob/web/c.example/public_html")},
		{Key: webJobKey("bob", "b.example"), Owner: "bob", Data: data("/home/eve/web/b.example/public_html")},
		{Key: webJobKey("bob", "../b"), Owner: "bob", Data: data("/home/bob/web/../b/public_html")},
		{Key: "server", Owner: "bob", Data: data("/home/bob/web/b.example/public_html")},
		{Key: webJobKey("bob", "b.example"), Owner: "bob"},
		{Key: webJobKey("bob", "b.example"), Owner: "bob", Data: json.RawMessage(`"x"`)},
	} {
		r := &siteRunner{files: []string{"keep.txt", "wp-admin/"}}
		RecoverInterrupted(context.Background(), r)(st)
		if len(r.calls) != 0 {
			t.Errorf("%+v: ran %q", st, r.calls)
		}
	}
}
