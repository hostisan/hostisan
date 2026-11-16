package webapp

import (
	"context"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

type recorder struct {
	mu    sync.Mutex
	calls []string
	out   map[string]string // command → stdout
	fail  string            // command that exits 3
	reads map[string]string // v-open-fs-file path → file contents
	files map[string]string // v-copy-fs-file destination → contents of the source file
	srcs  []string          // v-copy-fs-file source paths
	args  [][]string        // each call's arguments, as passed
}

func (r *recorder) Run(_ context.Context, cmd string, args ...string) (runner.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.args = append(r.args, append([]string(nil), args...))
	line := cmd + " " + strings.Join(args, " ")
	if cmd == "v-copy-fs-file" && len(args) == 3 {
		b, _ := os.ReadFile(args[1])
		if r.files == nil {
			r.files = map[string]string{}
		}
		r.files[args[2]] = string(b)
		r.srcs = append(r.srcs, args[1])
		line = cmd + " " + args[0] + " <T> " + args[2]
	}
	r.calls = append(r.calls, line)
	if cmd == r.fail {
		return runner.Result{Stdout: []byte("Error: x\n"), Exit: 3}, nil
	}
	out := r.out[cmd]
	if cmd == "v-run-cli-cmd" && len(args) > 2 && args[1] == "ls" {
		// -p marks folders; the notes file must not be taken for the app's folder.
		out = "ClassicPress-notes.txt\nClassicPress-release-2.0/\nconcrete-cms-notes\nconcrete-cms-9.3/\n"
	}
	if cmd == "v-open-fs-file" && len(args) == 2 {
		out = r.reads[args[1]]
	}
	return runner.Result{Stdout: []byte(out)}, nil
}

func TestCatalogueOrder(t *testing.T) {
	var ids []string
	for _, a := range All() {
		ids = append(ids, a.Info().ID)
	}
	want := []string{"castopod", "classicpress", "concretecms", "dokuwiki", "dolibarr", "drupal", "flarum", "grav",
		"joomla", "laravel", "mediawiki", "namelessmc", "nextcloud", "opencart", "prestashop", "qloapps",
		"shopware", "symfony", "thirtybees", "vvveb", "wordpress", "osticket"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids = %v", ids)
	}
	for _, id := range want {
		if inst, ok := Get(id); !ok || inst.Info().ID != id {
			t.Errorf("Get(%q)", id)
		}
	}
	if _, ok := Get("WordPress"); ok {
		t.Error("ids are lowercase")
	}
	for _, id := range []string{"dokuwiki", "grav", "namelessmc", "symfony"} {
		if inst, _ := Get(id); inst.Info().Database {
			t.Errorf("%s has no database", id)
		}
	}
}

func TestLabelsAndSelects(t *testing.T) {
	for key, want := range map[string]string{
		"disable_XML-RPC?":   "Disable XML-RPC?",
		"site_name":          "Site Name",
		"initial_ACL_policy": "Initial ACL Policy",
		"email":              "Email",
		"a.b_c":              "A B C",
	} {
		if got := (Field{Key: key}).Label(); got != want {
			t.Errorf("Label(%q) = %q, want %q", key, got, want)
		}
	}
	cp, _ := Get("classicpress")
	for _, f := range cp.Form() {
		switch f.Key {
		case "language": // assoc: value and label differ
			if f.Value != "en_US" || f.Options[0] != (Option{"ar_AR", "Arabic"}) {
				t.Errorf("language = %+v", f)
			}
		case "search_engine_indexing": // list: value is label, first option preselected
			if f.Value != "Allow" || f.Options[1] != (Option{"Discourage", "Discourage"}) {
				t.Errorf("indexing = %+v", f)
			}
		}
	}
}

func TestSupportedPHP(t *testing.T) {
	got := SupportedPHP([]string{"8.3", "7.4", "8.10", "8.2", "9.0"}, []string{"7.4", "8.2", "8.3", "8.10"})
	if !slices.Equal(got, []string{"7.4", "8.2", "8.3", "8.10"}) {
		t.Errorf("got %v", got)
	}
	if got := SupportedPHP([]string{"8.2"}, []string{"8.4"}); got == nil || len(got) != 0 {
		t.Errorf("none = %#v", got)
	}
}

func TestPaths(t *testing.T) {
	if got := phpDirname("/h/web/d/public_html/"); got != "/h/web/d" {
		t.Error(got)
	}
	if got := phpBasename("/h/web/d/public_html/"); got != "public_html" {
		t.Error(got)
	}
	tg := &Target{Domain: "d.example", Home: "/home/u"}
	if got := tg.DocRoot("/castopod/."); got != "/home/u/web/d.example/public_html/castopod/." {
		t.Error(got)
	}
	if got := tg.DocRoot("/"); got != "/home/u/web/d.example/public_html/" {
		t.Error(got)
	}
}

// v-run-cli-cmd passes its arguments on as an array (B2c), so they reach it as they
// are: no PHP-style "\\ " escaping.
func TestRunCLINoEscape(t *testing.T) {
	r := &recorder{}
	h := &Host{R: r, User: "u"}
	if err := h.RunPHP(context.Background(), "8.3", "/home/u/x.php", "a b", "*", `c\ d`); err != nil {
		t.Fatal(err)
	}
	if err := h.DeleteFile(context.Background(), "/a b/c d"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"u", "/usr/bin/php8.3", "/home/u/x.php", "a b", "*", `c\ d`}, {"u", "/a b/c d"}}
	if !reflect.DeepEqual(r.args, want) {
		t.Errorf("args = %q, want %q", r.args, want)
	}
}

func TestSendPostRedirects(t *testing.T) {
	hops := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
	}))
	defer srv.Close()
	h := &Host{}
	err := h.SendPost(context.Background(), srv.URL, F("a", "1"), nil, "")
	if err == nil || hops != 4 {
		t.Errorf("err = %v after %d requests", err, hops)
	}
}

// Every installer runs end to end against a recording runner and a stub site, and its
// own steps come after the shared ones.
func TestEveryInstaller(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer site.Close()
	for _, inst := range All() {
		t.Run(inst.Info().ID, func(t *testing.T) {
			php := inst.Config().PHP[len(inst.Config().PHP)-1]
			r := &recorder{out: map[string]string{"v-list-sys-php": `["` + php + `"]`}}
			h := &Host{R: r, User: "u", TmpDir: t.TempDir(),
				Sleep: func(context.Context, time.Duration) error { return nil },
				DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, n, site.Listener.Addr().String())
				}}
			opts := map[string]string{"php_version": php}
			for _, f := range inst.Form() {
				opts[f.Key] = f.Value
				if f.Type != "select" && f.Key != "username" {
					opts[f.Key] = "x"
				}
			}
			opts["username"] = "adm"
			tg := &Target{Domain: "d.example", Home: "/home/u", IP: "203.0.113.5"}
			if inst.Info().Database {
				tg.DB = Database{Host: "localhost", Name: "u_n", User: "u_n", Password: "dbpw", Create: true}
			}
			var steps []string
			err := Install(context.Background(), inst, tg, opts, h, Env{WebSystem: "nginx", WebBackend: "php-fpm"}, func(s string) { steps = append(steps, s) })
			if err != nil {
				t.Fatalf("install: %v\n%q", err, r.calls)
			}
			if want := 4 + map[bool]int{true: 1}[inst.Info().Database]; len(steps) != want {
				t.Errorf("steps = %v", steps)
			}
			hasDB := slices.ContainsFunc(r.calls, func(c string) bool { return strings.HasPrefix(c, "v-add-database u n n ") })
			if hasDB != inst.Info().Database {
				t.Errorf("database call = %v", hasDB)
			}
			tplIdx := slices.IndexFunc(r.calls, func(c string) bool { return strings.HasPrefix(c, "v-change-web-domain-tpl u d.example ") })
			be := slices.IndexFunc(r.calls, func(c string) bool {
				return c == "v-change-web-domain-backend-tpl u d.example PHP-"+strings.ReplaceAll(php, ".", "_")
			})
			if tplIdx < 0 || be != tplIdx+2 { // v-list-sys-php sits between
				t.Errorf("template calls at %d, %d", tplIdx, be)
			}
			wantTpl := inst.Config().Template
			if !strings.HasSuffix(r.calls[tplIdx], " "+wantTpl) {
				t.Errorf("template call = %q", r.calls[tplIdx])
			}
			for _, c := range r.calls {
				if strings.Contains(c, "\n") {
					t.Errorf("newline in %q", c)
				}
			}
			left, _ := os.ReadDir(h.TmpDir)
			if len(left) != 0 {
				t.Errorf("temp files left: %v", left)
			}
		})
	}
}

func TestInstallStopsAtFirstFailure(t *testing.T) {
	r := &recorder{fail: "v-change-web-domain-tpl"}
	inst, _ := Get("grav")
	err := Install(context.Background(), inst, &Target{Domain: "d.example", Home: "/home/u"},
		map[string]string{"php_version": "8.4"}, &Host{R: r, User: "u"}, Env{}, nil)
	f, ok := err.(interface{ Error() string })
	if !ok || err == nil || !strings.Contains(f.Error(), "Failed to change to template") {
		t.Fatalf("err = %v", err)
	}
	// Then only the clean-up's listing (the recorder's lists match: nothing to remove).
	if n := len(r.calls); !strings.HasPrefix(r.calls[n-2], "v-change-web-domain-tpl") ||
		!strings.HasPrefix(r.calls[n-1], "v-run-cli-cmd u ls -A -p ") {
		t.Errorf("last calls %q", r.calls[n-2:])
	}
}

func TestClassicPressFindsItsFolder(t *testing.T) {
	r := &recorder{}
	inst, _ := Get("classicpress")
	h := &Host{R: r, User: "u", Sleep: func(context.Context, time.Duration) error { return nil }}
	site := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer site.Close()
	h.DialContext = func(ctx context.Context, n, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, site.Listener.Addr().String())
	}
	opts := map[string]string{"php_version": "8.4", "language": "de_DE", "search_engine_indexing": "Discourage", "disable_XML-RPC?": "Yes"}
	tg := &Target{Domain: "d.example", Home: "/home/u", IP: "1.2.3.4"}
	if err := inst.Setup(context.Background(), tg, opts, h); err != nil {
		t.Fatal(err)
	}
	root := "/home/u/web/d.example/public_html"
	if !slices.Contains(r.calls, "v-copy-fs-directory u "+root+"/ClassicPress-release-2.0/. "+root) ||
		!slices.Contains(r.calls, "v-delete-fs-directory u "+root+"/ClassicPress-release-2.0/") ||
		!slices.Contains(r.calls, "v-run-cli-cmd u ls -A -p "+root) {
		t.Errorf("calls = %q", r.calls)
	}
}

func TestCreateFileNamesAreRandom(t *testing.T) {
	r := &recorder{}
	h := &Host{R: r, User: "u", TmpDir: t.TempDir()}
	for i := 0; i < 2; i++ {
		if err := h.CreateFile(context.Background(), "/home/u/x", "data"); err != nil {
			t.Fatal(err)
		}
	}
	a, b := path.Base(r.srcs[0]), path.Base(r.srcs[1])
	if a == b || a == "f" || !strings.HasPrefix(a, "f-") || !strings.HasPrefix(b, "f-") {
		t.Errorf("source names %q, %q", a, b)
	}
	if left, _ := os.ReadDir(h.TmpDir); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

func TestGravPasswordCutAt72Bytes(t *testing.T) {
	r := &recorder{}
	inst, _ := Get("grav")
	long := strings.Repeat("a", 80)
	h := &Host{R: r, User: "u", TmpDir: t.TempDir()}
	opts := map[string]string{"php_version": "8.4", "username": "ann", "password": long, "email": "e", "full_name": "A"}
	if err := inst.Setup(context.Background(), &Target{Domain: "d.example", Home: "/home/u"}, opts, h); err != nil {
		t.Fatal(err)
	}
	yaml := r.files["/home/u/web/d.example/public_html/user/accounts/ann.yaml"]
	hash := yaml[strings.Index(yaml, "'$")+1 : strings.LastIndex(yaml, "'")]
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(long[:72])) != nil {
		t.Error("hash does not match the first 72 bytes")
	}
}

// An installer page answering 4xx/5xx fails the install with the status (C6 D1): PHP's
// curl ignored it, so a 503 from a backend that wasn't up yet reported "done".
func TestSendPostFailsOnErrorStatus(t *testing.T) {
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		err := (&Host{}).SendPost(context.Background(), srv.URL+"/wp-admin/install.php?step=2", F("a", "1"), nil, "")
		srv.Close()
		var f *jobs.Failure
		if !errors.As(err, &f) || f.Message != postStatusMsg ||
			!slices.Equal(f.Args, []string{"/wp-admin/install.php", strconv.Itoa(code)}) {
			t.Errorf("%d: err = %#v", code, err)
		}
	}
	for _, code := range []int{http.StatusOK, http.StatusFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if code == http.StatusFound && r.URL.Path == "/" {
				http.Redirect(w, r, "/done", code)
			}
		}))
		err := (&Host{}).SendPost(context.Background(), srv.URL+"/", F("a", "1"), nil, "")
		srv.Close()
		if err != nil {
			t.Errorf("%d: err = %v", code, err)
		}
	}
}

// pool is a site whose PHP backend answers 503 until `down` GETs have been made, as
// Apache does while a new PHP-FPM pool's socket isn't up yet (C6 D1).
type pool struct {
	mu         sync.Mutex
	down, gets int
	posts      []string
	srv        *httptest.Server
}

func newPool(down int) *pool {
	p := &pool{down: down}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.Method == http.MethodGet {
			p.gets++
		}
		if p.gets <= p.down {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodPost {
			p.posts = append(p.posts, r.URL.RequestURI())
		}
	}))
	return p
}

func (p *pool) host(sleeps *int) *Host {
	return &Host{
		Sleep: func(context.Context, time.Duration) error { *sleeps++; return nil },
		DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, n, p.srv.Listener.Addr().String())
		}}
}

// The first POST of an install waits until the site stops answering 5xx: after a
// backend switch the new pool takes a moment to come up.
func TestPostWaitsForThePool(t *testing.T) {
	p := newPool(3)
	defer p.srv.Close()
	sleeps := 0
	x := &Seq{h: p.host(&sleeps), ctx: context.Background()}
	x.Post("http://d.example/wp-admin/install.php?step=2", F("a", "1"), nil, "")
	x.Post("http://d.example/next", F("a", "1"), nil, "")
	if err := x.Err(); err != nil {
		t.Fatalf("err = %v", err)
	}
	if p.gets != 4 || sleeps != 3 || !slices.Equal(p.posts, []string{"/wp-admin/install.php?step=2", "/next"}) {
		t.Errorf("gets %d, sleeps %d, posts %q", p.gets, sleeps, p.posts)
	}
}

// A pool that never comes up is waited for a bounded time, then the POST's 503 fails
// the install.
func TestPostGivesUpOnADeadPool(t *testing.T) {
	p := newPool(1 << 30)
	defer p.srv.Close()
	sleeps := 0
	x := &Seq{h: p.host(&sleeps), ctx: context.Background()}
	x.Post("http://d.example/wp-admin/install.php?step=2", F("a", "1"), nil, "")
	var f *jobs.Failure
	if !errors.As(x.Err(), &f) || f.Message != postStatusMsg || f.Args[1] != "503" {
		t.Fatalf("err = %#v", x.Err())
	}
	if p.gets != siteWaitTries || len(p.posts) != 0 {
		t.Errorf("gets %d, posts %q", p.gets, p.posts)
	}
}
