package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

// Fixtures used here:
//   - testdata/fixtures/tools_search/v-list-user-files-restic.json: _source
//     bin/v-list-user-files-restic:54 (`restic --json ls $snapshot $folder`: the
//     snapshot line, the folder itself, then its direct children, JSON Lines).
//   - dev v-restore-file-restic: _source bin/v-restore-file-restic:50-58 (restic dump
//     failure -> check_result E_NOTEXIST "Unable to download snapshot data", exit 3;
//     restic restore's own output on success; the script exits 0 even when restic
//     restore fails, R5).

const (
	rfSnap   = "7d1c0b9a"
	rfPublic = "/home/bob/web/x/public_html"
	rfP1     = rfPublic + "/index.php"
	rfP2     = rfPublic + "/about.html"
	rfP3     = rfPublic + "/contact.html"
	rfURL    = "/api/v1/backup/incremental/" + rfSnap + "/restore-files"
)

// rfCalls is every restic command so far, as "cmd arg arg".
func (h *harness) rfCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.Contains(c.Cmd, "restic") {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// rfBob is the dev v-list-user fixtures with bob's fields changed.
func rfBob(t *testing.T, fields map[string]string) []fixtureEntry {
	t.Helper()
	var entries []fixtureEntry
	readEntries(t, filepath.Join("..", "..", "testdata", "fixtures", "dev", "v-list-user.json"), &entries)
	for i, e := range entries {
		if len(e.Args) == 0 || e.Args[0] != "bob" {
			continue
		}
		var doc map[string]map[string]string
		if err := json.Unmarshal([]byte(e.Stdout), &doc); err != nil {
			t.Fatal(err)
		}
		for k, v := range fields {
			doc["bob"][k] = v
		}
		out, _ := json.MarshalIndent(doc, "", "    ")
		entries[i].Stdout = string(out) + "\n"
	}
	return entries
}

// rfEnable turns per-file restore on and points the live-tree checks at an empty
// temp root (no /home/bob), returned for tests that build a live tree. Swapping
// restoreRoot (a package variable) is why these tests must not use t.Parallel().
func rfEnable(t *testing.T) string {
	t.Helper()
	t.Setenv(restoreEnableEnv, "on")
	root := t.TempDir()
	old := restoreRoot
	restoreRoot = root
	t.Cleanup(func() { restoreRoot = old })
	return root
}

// newRestoreHarness is bob with incremental backups, per-file restore on, and the
// tools_search fixtures.
func newRestoreHarness(t *testing.T, fixtures map[string][]fixtureEntry, opts ...func(*Deps)) *harness {
	t.Helper()
	all := map[string][]fixtureEntry{"v-list-user": rfBob(t, map[string]string{"BACKUPS_INCREMENTAL": "yes"})}
	for k, v := range fixtures {
		all[k] = v
	}
	rfEnable(t)
	return newHarnessWith(t, overrides{Dir: srToolsDir, Fixtures: all}, opts...)
}

func (h *harness) rfStart(paths []string) (int, JobRef, APIError) {
	h.t.Helper()
	var r struct {
		JobRef
		Error ErrorBody `json:"error"`
	}
	code := h.call("POST", rfURL, RestoreFilesRequest{Paths: paths}, &r)
	return code, r.JobRef, APIError{Error: r.Error}
}

// rfWait waits for the job and reads it through the API.
func (h *harness) rfWait(id string) JobStatus {
	h.t.Helper()
	select {
	case <-h.api.jobs.Done(id):
	case <-time.After(5 * time.Second):
		h.t.Fatalf("job %s didn't end", id)
	}
	var st JobStatus
	if code := h.call("GET", "/api/v1/backup/restore-jobs/"+id, nil, &st); code != 200 {
		h.t.Fatalf("job read: %d", code)
	}
	return st
}

func TestRestoreFilesHappy(t *testing.T) {
	tr := &timeoutRunner{seen: map[string]time.Duration{}}
	h := newRestoreHarness(t, nil, func(d *Deps) { tr.Runner = d.R; d.R = tr })
	h.loginBob()
	code, ref, e := h.rfStart([]string{rfP1, rfP2})
	if code != 202 || !jobs.ValidID(ref.JobID) {
		t.Fatalf("start: %d %+v %+v", code, ref, e)
	}
	st := h.rfWait(ref.JobID)
	if st.State != "done" || st.Title != "Restore files" || !slices.Equal(st.Steps, []string{rfP1, rfP2}) || st.Error != nil {
		t.Fatalf("job = %+v", st)
	}
	res, _ := json.Marshal(st.Result)
	if string(res) != `{"restored":["`+rfP1+`","`+rfP2+`"]}` {
		t.Errorf("result = %s", res)
	}
	acctWantCalls(t, h.rfCalls(),
		"v-list-user-files-restic bob "+rfSnap+" "+rfPublic,
		"v-restore-file-restic bob "+rfSnap+" "+rfP1,
		"v-restore-file-restic bob "+rfSnap+" "+rfP2)
	if tr.seen["v-restore-file-restic"] != 30*time.Minute {
		t.Errorf("restore timeout = %v", tr.seen["v-restore-file-restic"])
	}
}

func TestRestoreFilesFoldersAndDuplicates(t *testing.T) {
	h := newRestoreHarness(t, nil)
	h.loginBob()
	// A folder restores everything under it; each parent is listed once, in order of
	// first use; a repeated path runs once.
	code, ref, e := h.rfStart([]string{rfPublic + "/img", "/home/bob/web", rfP3, "/home/bob/web"})
	if code != 202 {
		t.Fatalf("start: %d %+v", code, e)
	}
	if st := h.rfWait(ref.JobID); st.State != "done" {
		t.Fatalf("job = %+v", st)
	}
	acctWantCalls(t, h.rfCalls(),
		"v-list-user-files-restic bob "+rfSnap+" "+rfPublic,
		"v-list-user-files-restic bob "+rfSnap+" /home/bob",
		"v-restore-file-restic bob "+rfSnap+" "+rfPublic+"/img",
		"v-restore-file-restic bob "+rfSnap+" /home/bob/web",
		"v-restore-file-restic bob "+rfSnap+" "+rfP3)
}

func TestRestoreFilesRefused(t *testing.T) {
	h := newRestoreHarness(t, nil)
	h.loginBob()
	hostile := []string{
		"/home/bob/web/../../../etc/shadow", "../etc/passwd", "web/x", "/home/bob2/x", "/home/bobx",
		"/home/bob/-x", "/home/bob/web/-rf", "/home/bob/a b", "/home/bob/a\tb", "/home/bob/*.php",
		"/home/bob/?", "/home/bob/[a]", `/home/bob/a\b`, "/home/bob/conf/x", "/home/bob/conf",
		"/home/bob", "/home/bob/", "/home/bob/./x", "//home/bob/x", "/etc/passwd", "/home/admin/x",
		"/home/bob/x\n", "/home/bob/x\x00y", "/home/bob/x\x7f", "/home/bob/a b", "/home/bob/\xff",
		"", "/home/bob/" + strings.Repeat("a", 4096),
	}
	for _, p := range hostile {
		if code, _, e := h.rfStart([]string{rfP1, p}); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%q: %d %+v", p, code, e)
		}
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = rfP1
	}
	for name, paths := range map[string][]string{"51 paths": many, "empty": {}, "null": nil} {
		if code, _, e := h.rfStart(paths); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", name, code, e)
		}
	}
	for _, snap := range []string{"NOT-HEX", "-v", "deadbee", "3c9b8a7e%20x"} {
		var e APIError
		if code := h.call("POST", "/api/v1/backup/incremental/"+snap+"/restore-files", RestoreFilesRequest{Paths: []string{rfP1}}, &e); code != 400 {
			t.Errorf("snapshot %q: %d %+v", snap, code, e)
		}
	}
	var e APIError
	if code := h.call("POST", rfURL, map[string]any{"paths": "x"}, &e); code != 400 {
		t.Errorf("paths not a list: %d", code)
	}
	acctWantCalls(t, h.rfCalls())

	// A path the snapshot doesn't hold: refused after only the listing.
	code, _, e := h.rfStart([]string{rfP1, rfPublic + "/nope.php"})
	if code != 400 || e.Error.Code != "invalid" || e.Error.Message != "%s is not in this snapshot." ||
		!slices.Equal(e.Error.Args, []string{rfPublic + "/nope.php"}) {
		t.Errorf("missing: %d %+v", code, e)
	}
	acctWantCalls(t, h.rfCalls(), "v-list-user-files-restic bob "+rfSnap+" "+rfPublic)
}

func TestRestoreFilesGates(t *testing.T) {
	rfEnable(t)
	// Without incremental backups in the package.
	h := newHarnessWith(t, overrides{Dir: srToolsDir})
	h.loginBob()
	if code, _, e := h.rfStart([]string{rfP1}); code != 403 || e.Error.Code != "forbidden" {
		t.Errorf("no incremental: %d %+v", code, e)
	}
	acctWantCalls(t, h.rfCalls())

	// A suspended account, through an admin's look (not read-only).
	h = newHarnessWith(t, overrides{Dir: srToolsDir, Fixtures: map[string][]fixtureEntry{
		"v-list-user": rfBob(t, map[string]string{"BACKUPS_INCREMENTAL": "yes", "SUSPENDED": "yes"}),
	}})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	if code, _, e := h.rfStart([]string{rfP1}); code != 403 || e.Error.Code != "forbidden" {
		t.Errorf("suspended: %d %+v", code, e)
	}
	acctWantCalls(t, h.rfCalls())

	// Read-only: an admin looking at the protected root admin.
	h = newHarnessWith(t, overrides{Dir: srToolsDir, Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginOps()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	if code, _, e := h.rfStart([]string{"/home/admin/Documents/todo.md"}); code != 403 || e.Error.Code != "read_only" {
		t.Errorf("read-only: %d %+v", code, e)
	}
	acctWantCalls(t, h.rfCalls())

	// CSRF and sign-in.
	h = newRestoreHarness(t, nil)
	h.loginBob()
	h.token = "wrong"
	if code, _, e := h.rfStart([]string{rfP1}); code != 400 || e.Error.Code != "csrf" {
		t.Errorf("csrf: %d %+v", code, e)
	}
	h = newRestoreHarness(t, nil)
	h.bootstrap()
	if code, _, _ := h.rfStart([]string{rfP1}); code != 401 {
		t.Errorf("signed out: %d", code)
	}
	acctWantCalls(t, h.rfCalls())
}

// rfTree builds /home/bob's live tree under root: the public_html folder with its
// files, plus tmp.
func rfTree(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{rfPublic + "/img", "/home/bob/tmp"} {
		if err := os.MkdirAll(root+d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{rfP1, rfP2, rfP3, rfPublic + "/img/logo.png"} {
		writeFile(t, root+f, []byte("x"))
	}
}

func rfSymlink(t *testing.T, root, at string) {
	t.Helper()
	if err := os.RemoveAll(root + at); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", root+at); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreFilesRefusesLiveSymlinks(t *testing.T) {
	const linkMsg = "%s goes through a link on the server and can't be restored."
	for _, c := range []struct {
		link, path, arg string
	}{
		{"/home/bob", rfP1, rfP1},
		{"/home/bob/web", rfP1, rfP1},
		{rfPublic, rfP1, rfP1},
		{rfP1, rfP1, rfP1},
		{rfPublic + "/img/logo.png", rfPublic + "/img", rfPublic + "/img"}, // below a restored folder
		{rfPublic + "/img", rfPublic, rfPublic},
		{"/home/bob/tmp", rfP1, "/home/bob/tmp"}, // I2: the core mktemps and rm -frs there as root
	} {
		h := newRestoreHarness(t, nil)
		rfTree(t, restoreRoot)
		rfSymlink(t, restoreRoot, c.link)
		h.loginBob()
		code, _, e := h.rfStart([]string{c.path})
		if code != 400 || e.Error.Code != "invalid" || e.Error.Message != linkMsg || !slices.Equal(e.Error.Args, []string{c.arg}) {
			t.Errorf("link at %s, restoring %s: %d %+v", c.link, c.path, code, e)
		}
		acctWantCalls(t, h.rfCalls())
	}
	// A clean live tree (and folders that don't exist live yet) restores.
	h := newRestoreHarness(t, nil)
	rfTree(t, restoreRoot)
	h.loginBob()
	if code, _, e := h.rfStart([]string{rfP1, rfPublic + "/img", "/home/bob/web"}); code != 202 {
		t.Errorf("no links: %d %+v", code, e)
	}
}

func TestRestoreFilesFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	const checkMsg = "%s can't be checked on the server, so it can't be restored."
	// A folder on the way that hestiaweb can't read must not hide a link.
	h := newRestoreHarness(t, nil)
	rfTree(t, restoreRoot)
	locked := restoreRoot + "/home/bob/web/x"
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	h.loginBob()
	code, _, e := h.rfStart([]string{rfP1})
	if code != 400 || e.Error.Code != "invalid" || e.Error.Message != checkMsg || !slices.Equal(e.Error.Args, []string{rfP1}) {
		t.Errorf("unreadable parent: %d %+v", code, e)
	}
	// An unreadable folder inside a restored folder.
	_ = os.Chmod(locked, 0o755)
	if err := os.Chmod(restoreRoot+rfPublic+"/img", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(restoreRoot+rfPublic+"/img", 0o755) })
	if code, _, e := h.rfStart([]string{rfPublic}); code != 400 || e.Error.Message != checkMsg {
		t.Errorf("unreadable subfolder: %d %+v", code, e)
	}
	// An unreadable tmp folder.
	_ = os.Chmod(restoreRoot+rfPublic+"/img", 0o755)
	if err := os.Chmod(restoreRoot+"/home/bob", 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(restoreRoot+"/home/bob", 0o755) })
	if code, _, e := h.rfStart([]string{rfP1}); code != 400 || e.Error.Message != checkMsg {
		t.Errorf("unreadable home: %d %+v", code, e)
	}
	acctWantCalls(t, h.rfCalls())
}

func TestRestoreFilesSubtreeLimit(t *testing.T) {
	h := newRestoreHarness(t, nil)
	rfTree(t, restoreRoot)
	for i := 0; i <= restoreMaxWalk; i++ {
		writeFile(t, fmt.Sprintf("%s%s/img/f%05d", restoreRoot, rfPublic, i), nil)
	}
	h.loginBob()
	code, _, e := h.rfStart([]string{rfPublic + "/img"})
	if code != 400 || e.Error.Message != "%s holds too many files to check; restore a smaller folder." ||
		!slices.Equal(e.Error.Args, []string{rfPublic + "/img"}) {
		t.Errorf("%d %+v", code, e)
	}
	acctWantCalls(t, h.rfCalls())
}

// A link planted after the 202 is caught by the check the job repeats before each path.
func TestRestoreFilesRecheckInJob(t *testing.T) {
	br := &blockRunner{release: make(chan struct{}), started: make(chan struct{})}
	h := newRestoreHarness(t, nil, func(d *Deps) { br.Runner = d.R; d.R = br })
	rfTree(t, restoreRoot)
	h.loginBob()
	code, ref, e := h.rfStart([]string{rfP1, rfP2, rfP3})
	if code != 202 {
		t.Fatalf("start: %d %+v", code, e)
	}
	<-br.started
	rfSymlink(t, restoreRoot, rfP2)
	close(br.release)
	st := h.rfWait(ref.JobID)
	if st.State != "failed" || st.Error == nil || st.Error.Code != "invalid" ||
		st.Error.Message != "%s goes through a link on the server and can't be restored." || !slices.Equal(st.Error.Args, []string{rfP2}) {
		t.Fatalf("job = %+v err %+v", st, st.Error)
	}
	acctWantCalls(t, h.rfCalls(),
		"v-list-user-files-restic bob "+rfSnap+" "+rfPublic,
		"v-restore-file-restic bob "+rfSnap+" "+rfP1)
}

// Per-file restore is off unless the server enables it (controller ruling).
func TestRestoreFilesOffByDefault(t *testing.T) {
	h := newRestoreHarness(t, nil)
	for _, v := range []string{"", "off", "yes please"} {
		t.Setenv(restoreEnableEnv, v)
		h.loginBob()
		code, _, e := h.rfStart([]string{rfP1})
		if code != 409 || e.Error.Code != "disabled" || e.Error.Message != "Restoring single files is turned off on this server." {
			t.Errorf("%s=%q: %d %+v", restoreEnableEnv, v, code, e)
		}
	}
	acctWantCalls(t, h.rfCalls())
	t.Setenv(restoreEnableEnv, "on")
	if code, _, e := h.rfStart([]string{rfP1}); code != 202 {
		t.Errorf("on: %d %+v", code, e)
	}
}

func TestRestoreFilesFailureStops(t *testing.T) {
	h := newRestoreHarness(t, map[string][]fixtureEntry{"v-restore-file-restic": {
		{Args: []string{"bob", rfSnap, rfP2}, Stdout: "Error: Unable to download snapshot data\n", Exit: 3},
		{Args: []string{"*", "*", "*"}, Stdout: "Summary: Restored 1 files/dirs (4.112 KiB) in 0:00\n"},
	}})
	h.loginBob()
	code, ref, e := h.rfStart([]string{rfP1, rfP2, rfP3})
	if code != 202 {
		t.Fatalf("start: %d %+v", code, e)
	}
	st := h.rfWait(ref.JobID)
	if st.State != "failed" || st.Error == nil || st.Error.Code != "not_found" ||
		st.Error.Message != "Error: Unable to download snapshot data" || !slices.Equal(st.Error.Args, []string{rfP2}) {
		t.Fatalf("job = %+v err %+v", st, st.Error)
	}
	if !slices.Equal(st.Steps, []string{rfP1, rfP2}) || st.Result != nil {
		t.Errorf("steps/result = %v %v", st.Steps, st.Result)
	}
	acctWantCalls(t, h.rfCalls(),
		"v-list-user-files-restic bob "+rfSnap+" "+rfPublic,
		"v-restore-file-restic bob "+rfSnap+" "+rfP1,
		"v-restore-file-restic bob "+rfSnap+" "+rfP2)
}

// R5: the script exits 0 even when restic restore fails; restic's own error lines
// fail the step.
func TestRestoreFilesResticErrorOutput(t *testing.T) {
	for _, c := range []struct{ out, errOut, want string }{
		{
			"restoring <Snapshot 7d1c0b9a of [/home/bob]> to /\n",
			"Fatal: unable to open repository at /backup/restic/bob: permission denied\n",
			"Fatal: unable to open repository at /backup/restic/bob: permission denied",
		},
		{
			"Summary: Restored 0 files/dirs (0 B) in 0:00\n",
			"ignoring error for /home/bob/web/x/public_html/about.html: lchown: operation not permitted\n",
			"ignoring error for /home/bob/web/x/public_html/about.html: lchown: operation not permitted",
		},
		{
			"error: open /home/bob/a: permission denied\n",
			"Fatal: There were 1 errors\n",
			"error: open /home/bob/a: permission denied\nFatal: There were 1 errors",
		},
		{
			"Fatal: unable to open repository at /backup/restic/bob: permission denied\n", "",
			"Fatal: unable to open repository at /backup/restic/bob: permission denied",
		},
		{
			"restoring <Snapshot 7d1c0b9a of [/home/bob]> to /\n" +
				"error: lchown /home/bob/web/x/public_html/about.html: operation not permitted\n" +
				"Summary: Restored 0 files/dirs (0 B) in 0:00\n", "",
			"error: lchown /home/bob/web/x/public_html/about.html: operation not permitted",
		},
	} {
		h := newRestoreHarness(t, map[string][]fixtureEntry{"v-restore-file-restic": {
			{Args: []string{"bob", rfSnap, rfP2}, Stdout: c.out, Stderr: c.errOut},
			{Args: []string{"*", "*", "*"}, Stdout: "Summary: Restored 1 files/dirs (4.112 KiB) in 0:00\n"},
		}})
		h.loginBob()
		_, ref, _ := h.rfStart([]string{rfP1, rfP2, rfP3})
		st := h.rfWait(ref.JobID)
		if st.State != "failed" || st.Error == nil || st.Error.Code != "command_failed" || st.Error.Message != c.want ||
			!slices.Equal(st.Error.Args, []string{rfP2}) {
			t.Errorf("%q: job = %+v err %+v", c.out, st, st.Error)
		}
		if n := len(h.rfCalls()); n != 3 {
			t.Errorf("%q: calls = %q", c.out, h.rfCalls())
		}
	}
}

// blockRunner holds every v-restore-file-restic call until release is closed.
type blockRunner struct {
	runner.Runner
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func (b *blockRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == "v-restore-file-restic" {
		b.once.Do(func() { close(b.started) })
		<-b.release
	}
	return b.Runner.Run(ctx, cmd, args...)
}

func TestRestoreFilesBusy(t *testing.T) {
	br := &blockRunner{release: make(chan struct{}), started: make(chan struct{})}
	h := newRestoreHarness(t, nil, func(d *Deps) { br.Runner = d.R; d.R = br })
	h.loginBob()
	code, ref, _ := h.rfStart([]string{rfP1})
	if code != 202 {
		t.Fatalf("first: %d", code)
	}
	<-br.started
	before := len(h.rfCalls())
	code, _, e := h.rfStart([]string{rfP2})
	if code != 409 || e.Error.Code != "busy" || e.Error.Message != "A restore is already running." {
		t.Errorf("second: %d %+v", code, e)
	}
	if after := len(h.rfCalls()); after != before {
		t.Errorf("second start ran %q", h.rfCalls()[before:])
	}
	var st JobStatus
	if code := h.call("GET", "/api/v1/backup/restore-jobs/"+ref.JobID, nil, &st); code != 200 || st.State != "running" || st.Step != rfP1 {
		t.Errorf("running job: %d %+v", code, st)
	}
	close(br.release)
	if st := h.rfWait(ref.JobID); st.State != "done" {
		t.Errorf("job = %+v", st)
	}
	// Free again once it ended.
	if code, _, e := h.rfStart([]string{rfP2}); code != 202 {
		t.Errorf("after: %d %+v", code, e)
	}
}

func TestRestoreJobRead(t *testing.T) {
	h := newRestoreHarness(t, nil)
	h.loginBob()
	_, ref, _ := h.rfStart([]string{rfP1})
	h.rfWait(ref.JobID)

	webID, err := h.api.jobs.Start(context.Background(), jobs.Spec{Key: webJobKey("bob", "bakery.example"), Owner: "bob", Title: "WordPress"},
		func(context.Context, *jobs.Job) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	<-h.api.jobs.Done(webID)
	for _, id := range []string{webID, "0123456789abcdef0123456789abcdef", "NOT-AN-ID", strings.ToUpper(ref.JobID), ref.JobID + "0"} {
		var e APIError
		if code := h.call("GET", "/api/v1/backup/restore-jobs/"+id, nil, &e); code != 404 || e.Error.Code != "not_found" {
			t.Errorf("%s: %d %+v", id, code, e)
		}
	}

	// Another account's job.
	h.call("POST", "/api/v1/auth/logout", nil, nil)
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/backup/restore-jobs/"+ref.JobID, nil, &e); code != 404 {
		t.Errorf("admin reading bob's job: %d %+v", code, e)
	}
	// While looking at bob, the job is bob's to read.
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if code := h.call("GET", "/api/v1/backup/restore-jobs/"+ref.JobID, nil, nil); code != 200 {
		t.Errorf("admin as bob: %d", code)
	}
}

func TestRestoreRoutesGuards(t *testing.T) {
	h := newRestoreHarness(t, nil)
	h.bootstrap()
	if code := h.call("GET", "/api/v1/backup/restore-jobs/0123456789abcdef0123456789abcdef", nil, nil); code != 401 {
		t.Errorf("job read signed out: %d", code)
	}
}

// C3 D3: when the script fails, the job's message is the real cause (restic's error
// lines, cp's complaint on stderr) and the script's Error line, never restic's
// progress chatter, the root temp folder or a repository password. These are the
// outputs recorded on the VPS (C3 V6), with bob's paths.
func TestRestoreFilesFailureCause(t *testing.T) {
	const tmp = "/tmp/tmp.nDbGY5NlXd"
	chatter := "restoring <Snapshot 80821338 of [/home/bob]> at 2026-10-30 18:02:11.512 +0000 UTC by root@vps to " + tmp + "/root\n" +
		"Summary: Restored 4 / 1 files/dirs (12 B / 12 B) in 0:00\n"
	var many strings.Builder
	for i := range 12 {
		fmt.Fprintf(&many, "ignoring error for %s/f%d: mkdir %s/root%s/f%d: permission denied\n", rfPublic, i, tmp, rfPublic, i)
	}
	many.WriteString("Fatal: There were 12 errors\n")
	for _, c := range []struct {
		name        string
		out, errOut string
		exit        int
		code, want  string
	}{
		{
			"cp into a root-owned folder", chatter + "Error: Unable to restore " + rfP2 + "\n",
			"cp: cannot create regular file '" + rfP2 + "': Permission denied\n", 21, "command_failed",
			"cp: cannot create regular file '" + rfP2 + "': Permission denied\nError: Unable to restore " + rfP2,
		},
		{
			"unwritable restic target", chatter + "Error: Unable to restore " + rfP2 + "\n",
			"ignoring error for " + rfP2 + ": mkdir " + tmp + "/root" + rfPublic + "/ro/x: permission denied\nFatal: There were 4 errors\n",
			21, "command_failed",
			"ignoring error for " + rfP2 + ": mkdir " + rfPublic + "/ro/x: permission denied\nFatal: There were 4 errors\nError: Unable to restore " + rfP2,
		},
		{
			"root-owned existing tree", chatter + "Error: Unable to restore " + rfP2 + "\n",
			"ignoring error for " + rfP2 + ": open " + tmp + "/root" + rfP2 + ": permission denied\nFatal: There were 2 errors\n",
			21, "command_failed",
			"ignoring error for " + rfP2 + ": open " + rfP2 + ": permission denied\nFatal: There were 2 errors\nError: Unable to restore " + rfP2,
		},
		{
			"repository not readable", "Error: Unable to download snapshot data\n",
			"Fatal: unable to open repository at /srv/restic/bob: ReadDir: open /srv/restic/bob/keys: permission denied\n", 3, "not_found",
			"Fatal: unable to open repository at /srv/restic/bob: ReadDir: open /srv/restic/bob/keys: permission denied\nError: Unable to download snapshot data",
		},
		{
			"truncated packs", "Error: Unable to download snapshot data\n",
			"Load(<data/5e1f0b0c3a>, 0, 0) returned error, retrying after 552.330144ms: ReadFull: EOF\n" +
				"ignoring error for /home/bob/backup/backup.conf: StreamPack: ReadFull: EOF\nFatal: There were 2 errors\n", 3, "not_found",
			"ignoring error for /home/bob/backup/backup.conf: StreamPack: ReadFull: EOF\nFatal: There were 2 errors\nError: Unable to download snapshot data",
		},
		{
			"missing config", "Error: Unable to download snapshot data\n",
			"Fatal: unable to open config file: stat /srv/restic/bob/config: no such file or directory\nIs there a repository at the following location?\n/srv/restic/bob\n",
			3, "not_found",
			"Fatal: unable to open config file: stat /srv/restic/bob/config: no such file or directory\nError: Unable to download snapshot data",
		},
		{
			"missing index", chatter + "Error: Unable to restore " + rfP2 + "\n",
			"ignoring error for /: id 5f3e9a7c not found in repository\nFatal: There were 2 errors\n", 21, "command_failed",
			"ignoring error for /: id 5f3e9a7c not found in repository\nFatal: There were 2 errors\nError: Unable to restore " + rfP2,
		},
		{
			"wrong password", "Error: Unable to download snapshot data\n", "Fatal: wrong password or no key found\n", 3, "not_found",
			"Fatal: wrong password or no key found\nError: Unable to download snapshot data",
		},
		{
			"password in a repository URL", "Error: Unable to download snapshot data\n",
			"Fatal: unable to open repository at rest:https://bob:s3cr3t@backup.example.com/bob/: 401 Unauthorized\n", 3, "not_found",
			"Fatal: unable to open repository at rest:https://bob:***@backup.example.com/bob/: 401 Unauthorized\nError: Unable to download snapshot data",
		},
		{
			"many errors", chatter + "Error: Unable to restore " + rfP2 + "\n", many.String(), 21, "command_failed",
			"ignoring error for " + rfPublic + "/f0: mkdir " + rfPublic + "/f0: permission denied\n" +
				"ignoring error for " + rfPublic + "/f1: mkdir " + rfPublic + "/f1: permission denied\n" +
				"ignoring error for " + rfPublic + "/f2: mkdir " + rfPublic + "/f2: permission denied\n" +
				"ignoring error for " + rfPublic + "/f3: mkdir " + rfPublic + "/f3: permission denied\n" +
				"ignoring error for " + rfPublic + "/f4: mkdir " + rfPublic + "/f4: permission denied\n" +
				"…\nFatal: There were 12 errors\nError: Unable to restore " + rfP2,
		},
		{
			"no cause given", chatter + "Error: Unable to restore " + rfP2 + "\n", "", 21, "command_failed",
			"Error: Unable to restore " + rfP2,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRestoreHarness(t, map[string][]fixtureEntry{"v-restore-file-restic": {
				{Args: []string{"bob", rfSnap, rfP2}, Stdout: c.out, Stderr: c.errOut, Exit: c.exit},
				{Args: []string{"*", "*", "*"}, Stdout: "Summary: Restored 1 files/dirs (4.112 KiB) in 0:00\n"},
			}})
			h.loginBob()
			_, ref, _ := h.rfStart([]string{rfP1, rfP2, rfP3})
			st := h.rfWait(ref.JobID)
			if st.State != "failed" || st.Error == nil || st.Error.Code != c.code || !slices.Equal(st.Error.Args, []string{rfP2}) {
				t.Fatalf("job = %+v err %+v", st, st.Error)
			}
			if st.Error.Message != c.want {
				t.Errorf("message\n%s\nwant\n%s", st.Error.Message, c.want)
			}
			if strings.Contains(st.Error.Message, "/tmp/") || strings.Contains(st.Error.Message, "s3cr3t") {
				t.Errorf("message leaks a temp path or a secret: %q", st.Error.Message)
			}
		})
	}
}
