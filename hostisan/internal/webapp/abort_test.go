package webapp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

// docroot is a runner whose public_html listing grows once the app's files are
// fetched (wp core download), and that can hold one command until released.
type docroot struct {
	mu      sync.Mutex
	calls   []string
	fetched bool
	fail    string // command that exits 3
	upload  string // command during which the user uploads upload.zip
	uploads bool
	hold    string        // command that waits for release
	held    chan struct{} // closed when hold starts
	release chan struct{}
}

const pub = "/home/u/web/d.example/public_html"

func (d *docroot) Run(_ context.Context, cmd string, args ...string) (runner.Result, error) {
	line := cmd + " " + strings.Join(args, " ")
	d.mu.Lock()
	d.calls = append(d.calls, line)
	d.mu.Unlock()
	if cmd == d.upload {
		d.mu.Lock()
		d.uploads = true
		d.mu.Unlock()
	}
	if cmd == d.hold {
		close(d.held)
		<-d.release
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case cmd == d.fail:
		return runner.Result{Stdout: []byte("Error: x\n"), Exit: 3}, nil
	case cmd == "v-list-sys-php":
		return runner.Result{Stdout: []byte(`["8.3"]`)}, nil
	case cmd == "v-run-cli-cmd" && slices.Contains(args, "ls"):
		out := "keep.txt\nmine/\n"
		if d.fetched {
			out += "wp-admin/\nwp-config.php\nwp-includes/\n"
		}
		if d.uploads {
			out += "upload.zip\n"
		}
		return runner.Result{Stdout: []byte(out)}, nil
	case cmd == "v-run-cli-cmd" && slices.Contains(args, "core"):
		d.fetched = true
	}
	return runner.Result{}, nil
}

func (d *docroot) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.calls)
}

var wpOpts = map[string]string{"php_version": "8.3", "language": "en_US", "site_name": "B", "username": "a",
	"email": "e@x.org", "password": "pw"}

func wpTarget() *Target {
	return &Target{Domain: "d.example", Home: "/home/u", IP: "203.0.113.5",
		DB: Database{Host: "localhost", Name: "u_n", User: "u_n", Password: "dbpw", Create: true}}
}

// wantRemoved checks the install removed exactly the entries it wrote to public_html
// (Plan 6 C6 D2: a half-installed WordPress left /wp-admin/setup-config.php public).
func wantRemoved(t *testing.T, calls []string) {
	t.Helper()
	var removed []string
	for _, c := range calls {
		if strings.HasPrefix(c, "v-delete-fs-directory ") || strings.HasPrefix(c, "v-delete-fs-file u "+pub+"/") &&
			!strings.HasSuffix(c, "/robots.txt") && !strings.HasSuffix(c, "/index.html") {
			removed = append(removed, c)
		}
	}
	want := []string{
		"v-delete-fs-directory u " + pub + "/wp-admin",
		"v-delete-fs-file u " + pub + "/wp-config.php",
		"v-delete-fs-directory u " + pub + "/wp-includes",
	}
	if !slices.Equal(removed, want) {
		t.Errorf("removed:\n%q\nwant:\n%q\ncalls:\n%q", removed, want, calls)
	}
}

// A failed install removes what it wrote to public_html and keeps the rest.
func TestFailedInstallRemovesItsFiles(t *testing.T) {
	d := &docroot{fail: "v-change-web-domain-tpl"}
	inst, _ := Get("wordpress")
	err := Install(context.Background(), inst, wpTarget(), wpOpts, &Host{R: d, User: "u"}, Env{WebBackend: "php-fpm"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Failed to change to template") {
		t.Fatalf("err = %v", err)
	}
	wantRemoved(t, d.list())
}

// A file the user uploads to public_html while the install runs is not the
// install's: a failed install keeps it (final review M4).
func TestFailedInstallKeepsAUserUpload(t *testing.T) {
	d := &docroot{fail: "v-change-web-domain-tpl", upload: "v-add-database"}
	inst, _ := Get("wordpress")
	err := Install(context.Background(), inst, wpTarget(), wpOpts, &Host{R: d, User: "u"}, Env{WebBackend: "php-fpm"}, nil)
	if err == nil {
		t.Fatal("err = nil")
	}
	wantRemoved(t, d.list())
}

// A failure before any file arrived removes nothing.
func TestEarlyFailureRemovesNothing(t *testing.T) {
	d := &docroot{fail: "v-add-user-wp-cli"}
	inst, _ := Get("wordpress")
	if err := Install(context.Background(), inst, wpTarget(), wpOpts, &Host{R: d, User: "u"}, Env{}, nil); err == nil {
		t.Fatal("err = nil")
	}
	for _, c := range d.list() {
		if strings.HasPrefix(c, "v-delete-fs-directory") {
			t.Errorf("removed %q", c)
		}
	}
}

// A panel stop mid-install lets the running command finish, starts no further step,
// removes the app's files and ends the job "interrupted".
func TestInterruptedInstallStopsAndCleansUp(t *testing.T) {
	d := &docroot{hold: "v-add-database", held: make(chan struct{}), release: make(chan struct{})}
	inst, _ := Get("wordpress")
	reg := jobs.NewRegistry(0, nil)
	id, err := reg.Start(context.Background(), jobs.Spec{Key: "web:u:d.example", Owner: "u"},
		func(ctx context.Context, j *jobs.Job) (any, error) {
			return nil, Install(ctx, inst, wpTarget(), wpOpts, &Host{R: d, User: "u"}, Env{WebBackend: "php-fpm"}, nil)
		})
	if err != nil {
		t.Fatal(err)
	}
	<-d.held
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopped <- reg.Shutdown(ctx, 10*time.Millisecond)
	}()
	time.Sleep(50 * time.Millisecond) // past the grace: the stop is requested
	close(d.release)
	if err := <-stopped; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	st, _ := reg.Get(id)
	if st.State != jobs.Failed || !errors.Is(st.Err, jobs.ErrInterrupted) {
		t.Fatalf("job = %+v", st)
	}
	calls := d.list()
	at := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, "v-add-database ") })
	for _, c := range calls[at+1:] {
		if !strings.HasPrefix(c, "v-run-cli-cmd u ls") && !strings.HasPrefix(c, "v-delete-fs-") {
			t.Errorf("ran %q after the stop", c)
		}
	}
	wantRemoved(t, calls)
}

// Install hands its public_html snapshot to Host.Undo before any app file arrives, so
// a killed job can be undone by the next start, and saves it again as the install
// writes: before a write whose names aren't known (Running, with the listing it
// started from) and after it (what it wrote).
func TestInstallSavesItsUndo(t *testing.T) {
	d := &docroot{fail: "v-change-web-domain-tpl"} // stop before the site is posted to
	var got []Undo
	var callsAt []int
	h := &Host{R: d, User: "u", Undo: func(u Undo) error {
		got = append(got, u)
		callsAt = append(callsAt, len(d.list()))
		return nil
	}}
	inst, _ := Get("wordpress")
	_ = Install(context.Background(), inst, wpTarget(), wpOpts, h, Env{}, nil)
	before := []string{"keep.txt", "mine/"}
	if len(got) != 3 || got[0].Dir != pub || !slices.Equal(got[0].Before, before) || !got[0].Tracked ||
		len(got[0].Wrote) != 0 || got[0].Running {
		t.Fatalf("undo = %+v", got)
	}
	calls := d.list()
	if !strings.HasPrefix(calls[callsAt[0]-1], "v-run-cli-cmd u ls -A -p ") ||
		strings.HasPrefix(calls[callsAt[0]], "v-delete-") {
		t.Errorf("undo saved at the wrong time: %q", calls[:callsAt[0]+1])
	}
	// the download is recorded as running before it starts
	if !got[1].Running || !slices.Equal(got[1].Open, before) || !strings.Contains(calls[callsAt[1]], "core download") {
		t.Errorf("running undo = %+v, next call %q", got[1], calls[callsAt[1]])
	}
	if got[2].Running || !slices.Equal(got[2].Wrote, []string{"wp-admin", "wp-config.php", "wp-includes"}) {
		t.Errorf("after the download: %+v", got[2])
	}
}

// A failure to save the undo stops the install before it fetches anything.
func TestInstallStopsWhenUndoNotSaved(t *testing.T) {
	d := &docroot{}
	h := &Host{R: d, User: "u", Undo: func(Undo) error { return errors.New("disk full") }}
	inst, _ := Get("wordpress")
	if err := Install(context.Background(), inst, wpTarget(), wpOpts, h, Env{}, nil); err == nil {
		t.Fatal("err = nil")
	}
	for _, c := range d.list() {
		if strings.Contains(c, "core download") {
			t.Fatalf("fetched after the undo failed: %q", d.list())
		}
	}
}

// Recover (the next start, after a kill) from a record of an older build (no Wrote)
// removes every entry added since Before, again and again without harm, and refuses
// an undo that isn't for that domain's public_html.
func TestRecoverRemovesAddedFilesOnly(t *testing.T) {
	d := &docroot{fetched: true}
	h := &Host{R: d, User: "u"}
	u := Undo{Dir: pub, Before: []string{"keep.txt", "mine/"}}
	if err := Recover(context.Background(), h, "d.example", u); err != nil {
		t.Fatal(err)
	}
	wantRemoved(t, d.list())

	for _, bad := range []Undo{
		{Dir: "/home/u/web/other.example/public_html", Before: u.Before},
		{Dir: "/home/u/web/d.example/public_html/..", Before: u.Before},
		{Dir: "/etc", Before: u.Before},
		{Dir: "/home/u/web/d.example", Before: u.Before},
		{Dir: pub, Before: []string{"../x"}},
	} {
		d := &docroot{fetched: true}
		if err := Recover(context.Background(), &Host{R: d, User: "u"}, "d.example", bad); err == nil {
			t.Errorf("%+v: err = nil", bad)
		}
		if calls := d.list(); len(calls) != 0 {
			t.Errorf("%+v: ran %q", bad, calls)
		}
	}
	if err := Recover(context.Background(), &Host{R: &docroot{}, User: "u"}, "../x", u); err == nil {
		t.Error("bad domain: err = nil")
	}
}

// Recover from a record that tracks what the install wrote removes only that, and
// while a write was running, what appeared since its listing; a user upload in
// between stays. A record with a bad name in Wrote or Open is refused.
func TestRecoverKeepsAUserUpload(t *testing.T) {
	before := []string{"keep.txt", "mine/"}
	for _, u := range []Undo{
		{Dir: pub, Before: before, Tracked: true, Wrote: []string{"wp-admin", "wp-config.php", "wp-includes"}},
		{Dir: pub, Before: before, Tracked: true, Running: true, Open: []string{"keep.txt", "mine/", "upload.zip"}},
	} {
		d := &docroot{fetched: true, uploads: true}
		if err := Recover(context.Background(), &Host{R: d, User: "u"}, "d.example", u); err != nil {
			t.Fatal(err)
		}
		wantRemoved(t, d.list())
	}
	for _, bad := range []Undo{
		{Dir: pub, Before: before, Tracked: true, Wrote: []string{"../x"}},
		{Dir: pub, Before: before, Tracked: true, Running: true, Open: []string{"a/b"}},
	} {
		d := &docroot{fetched: true}
		if err := Recover(context.Background(), &Host{R: d, User: "u"}, "d.example", bad); err == nil {
			t.Errorf("%+v: err = nil", bad)
		}
		if calls := d.list(); len(calls) != 0 {
			t.Errorf("%+v: ran %q", bad, calls)
		}
	}
}
