package webapp

import (
	"context"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hostisan/hostisan/internal/runner"
)

// fakeFS is a runner that keeps public_html's top-level entries (`ls -p`: a directory
// ends in "/"), so the tests see what an install's writes and its cleanup leave.
// on[cmd] runs before cmd (a user upload meanwhile); a command whose line holds fail
// exits 3 after its effect.
type fakeFS struct {
	mu      sync.Mutex
	calls   []string
	root    map[string]bool
	outside []string // writes that landed outside public_html
	on      map[string]func(f *fakeFS)
	fail    string
}

func newFakeFS() *fakeFS {
	return &fakeFS{root: map[string]bool{"keep.txt": true, "mine/": true}, on: map[string]func(*fakeFS){}}
}

// put adds the top-level entry of public_html that a write to p lands in.
func (f *fakeFS) put(p string, dir bool) {
	rel, ok := strings.CutPrefix(path.Clean(p), pub+"/")
	if !ok {
		f.outside = append(f.outside, p)
		return
	}
	name, rest, nested := strings.Cut(rel, "/")
	if nested && rest != "" || dir {
		name += "/"
	}
	f.root[name] = true
}

func (f *fakeFS) Run(_ context.Context, cmd string, args ...string) (runner.Result, error) {
	line := cmd + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	hook := f.on[cmd]
	f.mu.Unlock()
	if hook != nil {
		hook(f)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	last := args[len(args)-1]
	var out string
	switch {
	case cmd == "v-list-sys-php":
		out = `["8.3"]`
	case cmd == "v-run-cli-cmd" && args[1] == "ls":
		if path.Clean(last) == pub {
			var names []string
			for n := range f.root {
				names = append(names, n)
			}
			sort.Strings(names)
			out = strings.Join(names, "\n") + "\n"
		}
	case cmd == "v-run-cli-cmd" && slices.Contains(args, "core"):
		for _, n := range []string{"wp-admin/", "wp-includes/", "wp-load.php"} {
			f.root[n] = true
		}
	case cmd == "v-add-fs-directory":
		f.put(last, true)
	case cmd == "v-copy-fs-file" || cmd == "v-move-fs-file":
		f.put(last, false)
	case cmd == "v-delete-fs-directory" || cmd == "v-delete-fs-file":
		if rel, ok := strings.CutPrefix(last, pub+"/"); ok {
			delete(f.root, rel)
			delete(f.root, rel+"/")
		}
	}
	if f.fail != "" && strings.Contains(line, f.fail) {
		return runner.Result{Stdout: []byte("Error: x\n"), Exit: 3}, nil
	}
	return runner.Result{Stdout: []byte(out)}, nil
}

func (f *fakeFS) entries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for n := range f.root {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (f *fakeFS) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "v-delete-fs-") && !strings.HasSuffix(c, "/robots.txt") &&
			!strings.HasSuffix(c, "/index.html") {
			out = append(out, c)
		}
	}
	return out
}

// testApp is an installer that fetches WordPress (wp core download) and then runs
// setup, which fails the install unless it returns nil.
type testApp struct {
	setup func(ctx context.Context, t *Target, h *Host) error
}

func (a testApp) Info() Info     { return Info{ID: "test", Version: "1"} }
func (a testApp) Form() []Field  { return nil }
func (a testApp) Config() Config { return Config{Resource: Resource{Kind: "wp"}} }
func (a testApp) Setup(ctx context.Context, t *Target, _ map[string]string, h *Host) error {
	return a.setup(ctx, t, h)
}

// runTestApp runs Install of setup on f and returns the undo records it saved.
func runTestApp(t *testing.T, f *fakeFS, tg *Target, setup func(context.Context, *Target, *Host) error) []Undo {
	t.Helper()
	var saved []Undo
	h := &Host{R: f, User: "u", TmpDir: t.TempDir(), Undo: func(u Undo) error {
		saved = append(saved, u)
		return nil
	}}
	if err := Install(context.Background(), testApp{setup}, tg, wpOpts, h, Env{}, nil); err == nil {
		t.Fatal("err = nil")
	}
	return saved
}

var errSetup = Fail("setup failed")

// The undo record's Dir is public_html cleaned, so a home path with a "." or ".."
// step still records the named writes (and Recover accepts the record).
func TestInstallCleansTheDocRoot(t *testing.T) {
	f := newFakeFS()
	tg := wpTarget()
	tg.Home = "/home/u/."
	saved := runTestApp(t, f, tg, func(ctx context.Context, t *Target, h *Host) error {
		if err := h.CreateFile(ctx, t.DocRoot("wp-config.php"), "x"); err != nil {
			return err
		}
		return errSetup
	})
	if len(saved) == 0 || saved[0].Dir != pub {
		t.Fatalf("undo = %+v", saved)
	}
	if last := saved[len(saved)-1]; !slices.Contains(last.Wrote, "wp-config.php") {
		t.Errorf("wp-config.php not recorded: %+v", last)
	}
	if got, want := f.entries(), []string{"keep.txt", "mine/"}; !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}
}

// A write to a named path in a subdirectory records the subdirectory's top-level
// entry, which a failed install removes.
func TestNamedWritesIntoASubdirectory(t *testing.T) {
	f := newFakeFS()
	saved := runTestApp(t, f, wpTarget(), func(ctx context.Context, t *Target, h *Host) error {
		if err := h.AddDirectory(ctx, t.DocRoot("app/cache")); err != nil {
			return err
		}
		if err := h.CreateFile(ctx, t.DocRoot("conf/site.php"), "x"); err != nil {
			return err
		}
		if err := h.MoveFile(ctx, t.DocRoot("wp-load.php"), t.DocRoot("lib/load.php")); err != nil {
			return err
		}
		return errSetup
	})
	last := saved[len(saved)-1]
	for _, n := range []string{"app", "conf", "lib"} {
		if !slices.Contains(last.Wrote, n) {
			t.Errorf("%s not recorded: %+v", n, last)
		}
	}
	if got, want := f.entries(), []string{"keep.txt", "mine/"}; !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}
}

// A write outside public_html, or whose path climbs out of it with "..", is not
// recorded, and the cleanup deletes nothing outside public_html.
func TestWritesOutsideTheDocRootStayUntracked(t *testing.T) {
	f := newFakeFS()
	saved := runTestApp(t, f, wpTarget(), func(ctx context.Context, t *Target, h *Host) error {
		for _, p := range []string{
			"/home/u/tmp/x.txt",
			pub + "/../outside.txt",
			pub + "/a/../../escape.txt",
			pub + "/../public_html_x/y.txt",
		} {
			if err := h.CreateFile(ctx, p, "x"); err != nil {
				return err
			}
		}
		if err := h.AddDirectory(ctx, pub+"/../../other.example/public_html/z"); err != nil {
			return err
		}
		return errSetup
	})
	for _, u := range saved {
		for _, n := range u.Wrote {
			if !slices.Contains([]string{"wp-admin", "wp-includes", "wp-load.php"}, n) {
				t.Errorf("recorded %q: %+v", n, u)
			}
		}
	}
	if len(f.outside) != 5 {
		t.Errorf("outside = %q", f.outside)
	}
	for _, c := range f.deleted() {
		if !strings.HasPrefix(c, "v-delete-fs-directory u "+pub+"/wp-") &&
			c != "v-delete-fs-file u "+pub+"/wp-load.php" {
			t.Errorf("deleted %q", c)
		}
	}
	if got, want := f.entries(), []string{"keep.txt", "mine/"}; !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}
}

// A directory the user makes in public_html mid-install isn't the install's, even
// when the install then writes into it: a failed install keeps it.
func TestNamedWriteIntoAUserDirectoryKeepsIt(t *testing.T) {
	f := newFakeFS()
	f.on["v-add-database"] = func(f *fakeFS) {
		f.mu.Lock()
		f.root["documents/"] = true
		f.mu.Unlock()
	}
	saved := runTestApp(t, f, wpTarget(), func(ctx context.Context, t *Target, h *Host) error {
		if err := h.CreateFile(ctx, t.DocRoot("documents/readme.txt"), "x"); err != nil {
			return err
		}
		return errSetup
	})
	for _, u := range saved {
		if slices.Contains(u.Wrote, "documents") {
			t.Errorf("documents recorded: %+v", u)
		}
	}
	if got, want := f.entries(), []string{"documents/", "keep.txt", "mine/"}; !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}
}

// A live install that fails during a write whose names aren't known in advance (wp
// core download) removes what appeared since that write's listing (Open), and keeps
// an upload made before it.
func TestFailureDuringARunningWriteUsesOpen(t *testing.T) {
	f := newFakeFS()
	f.fail = "core download"
	f.on["v-add-user-wp-cli"] = func(f *fakeFS) {
		f.mu.Lock()
		f.root["upload.zip"] = true
		f.mu.Unlock()
	}
	saved := runTestApp(t, f, wpTarget(), func(context.Context, *Target, *Host) error {
		t.Fatal("setup ran")
		return nil
	})
	last := saved[len(saved)-1]
	if !last.Running || !slices.Contains(last.Open, "upload.zip") || len(last.Wrote) != 0 {
		t.Errorf("undo = %+v", last)
	}
	want := []string{
		"v-delete-fs-directory u " + pub + "/wp-admin",
		"v-delete-fs-directory u " + pub + "/wp-includes",
		"v-delete-fs-file u " + pub + "/wp-load.php",
	}
	if got := f.deleted(); !slices.Equal(got, want) {
		t.Errorf("deleted %q, want %q", got, want)
	}
	if got, want := f.entries(), []string{"keep.txt", "mine/", "upload.zip"}; !slices.Equal(got, want) {
		t.Errorf("left %q, want %q", got, want)
	}
}
