package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"net/http/cookiejar"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/webapp"
)

const webAppsPass = "pa ss"

// webAppsSpy wraps the runner: it can hold a command until released, and it notes the
// state of the files a command is given.
type webAppsSpy struct {
	inner runner.Runner
	hold  func(cmd string, args []string) <-chan struct{}

	mu       sync.Mutex
	pwMode   os.FileMode
	pwText   string
	copies   map[string]string // v-copy-fs-file destination → contents
	copyMode map[string]os.FileMode
}

func (s *webAppsSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if s.hold != nil {
		if ch := s.hold(cmd, args); ch != nil {
			<-ch
		}
	}
	s.mu.Lock()
	switch cmd {
	case "v-add-database":
		if len(args) > 3 {
			if st, err := os.Stat(args[3]); err == nil {
				s.pwMode = st.Mode().Perm()
				b, _ := os.ReadFile(args[3])
				s.pwText = string(b)
			}
		}
	case "v-copy-fs-file":
		if len(args) == 3 {
			if s.copies == nil {
				s.copies, s.copyMode = map[string]string{}, map[string]os.FileMode{}
			}
			b, _ := os.ReadFile(args[1])
			s.copies[args[2]] = string(b)
			if st, err := os.Stat(args[1]); err == nil {
				s.copyMode[args[2]] = st.Mode().Perm()
			}
		}
	}
	s.mu.Unlock()
	return s.inner.Run(ctx, cmd, args...)
}

type webAppsEnv struct {
	h    *harness
	s    *server
	base string
	tmp  string
	spy  *webAppsSpy
	// posts are the requests the installers sent: "<dialed addr> <path>?<query> <form>".
	mu    sync.Mutex
	posts []string
}

// newWebAppsEnv serves the quick-install routes on their own server (so the test holds
// the job registry) behind the harness's sessions.
func newWebAppsEnv(t *testing.T, cfg map[string]string, fx map[string][]fixtureEntry) *webAppsEnv {
	t.Helper()
	e := &webAppsEnv{tmp: t.TempDir()}
	c := map[string]string{"WEB_SYSTEM": "nginx", "WEB_BACKEND": "php-fpm", "POLICY_SYSTEM_PROTECTED_ADMIN": "no"}
	for k, v := range cfg {
		c[k] = v
	}
	e.h = newHarnessWith(t, overrides{Dir: "../../testdata/fixtures/web_apps", Config: c, Fixtures: fx}, func(d *Deps) {
		e.spy = &webAppsSpy{inner: d.R}
		d.R = e.spy
		d.TmpDir = e.tmp
	})
	e.s = &server{Deps: e.h.deps, jobs: jobs.NewRegistry(0, nil)}
	mux := http.NewServeMux()
	e.s.webAppRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e.base = srv.URL

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		e.mu.Lock()
		e.posts = append(e.posts, r.URL.RequestURI()+" "+string(b))
		e.mu.Unlock()
	}))
	t.Cleanup(site.Close)
	webAppHostHook = func(h *webapp.Host) {
		h.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			e.mu.Lock()
			e.posts = append(e.posts, "dial "+addr)
			e.mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, site.Listener.Addr().String())
		}
		h.Sleep = func(_ context.Context, _ time.Duration) error { return nil }
	}
	t.Cleanup(func() { webAppHostHook = nil })
	return e
}

func (e *webAppsEnv) do(method, path string, body, out any) int {
	e.h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", e.h.token)
	res, err := e.h.client.Do(req)
	if err != nil {
		e.h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

// asBob replaces the session with bob's.
func (e *webAppsEnv) asBob() {
	jar, _ := cookiejar.New(nil)
	e.h.client = &http.Client{Jar: jar}
	e.h.loginBob()
}

// install posts an install and returns the job id once it has been accepted.
func (e *webAppsEnv) install(domain, app string, req WebAppInstallRequest) string {
	e.h.t.Helper()
	var ref JobRef
	if code := e.do("POST", "/api/v1/web/"+domain+"/apps/"+app, req, &ref); code != 202 || !jobs.ValidID(ref.JobID) {
		e.h.t.Fatalf("install %s/%s: %d %+v", domain, app, code, ref)
	}
	return ref.JobID
}

func (e *webAppsEnv) wait(id string) JobStatus {
	e.h.t.Helper()
	<-e.s.jobs.Done(id)
	return e.status("plain.example.org", id)
}

func (e *webAppsEnv) status(domain, id string) JobStatus {
	e.h.t.Helper()
	var st JobStatus
	if code := e.do("GET", "/api/v1/web/"+domain+"/apps/jobs/"+id, nil, &st); code != 200 {
		e.h.t.Fatalf("status: %d", code)
	}
	return st
}

var (
	webAppsTmpRe  = regexp.MustCompile(`\S*/hostisan-[A-Za-z0-9]+(/f-[0-9]+)?`)
	webAppsRandRe = regexp.MustCompile(`wp_[A-Za-z0-9]{5}_`)
)

// writes are the commands the job ran, with the random parts masked, less the
// listings with which the installer records what it writes (`ls -A -p -U`; the
// webapp tests cover them).
func (e *webAppsEnv) writes() []string {
	var out []string
	for _, c := range e.h.webWrites() {
		if strings.Contains(c, " ls -A -p -U ") {
			continue
		}
		c = webAppsTmpRe.ReplaceAllString(c, "<tmp>")
		out = append(out, webAppsRandRe.ReplaceAllString(c, "wp_RAND_"))
	}
	return out
}

func webAppsWP() WebAppInstallRequest {
	return WebAppInstallRequest{
		Values: map[string]string{"site_name": "My Blog", "username": "wpadmin", "email": "a@b.example",
			"password": "Admin pw1", "language": "en_US"},
		PHPVersion: "8.3",
		Database:   &WebAppDatabase{Create: true, Host: "localhost", Name: "wp", User: "wpu", Password: webAppsPass},
	}
}

func TestWebAppsList(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	var apps WebApps
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps", nil, &apps); code != 200 || len(apps.Apps) != 22 {
		t.Fatalf("list: %d %d", code, len(apps.Apps))
	}
	if apps.Apps[0].ID != "castopod" || apps.Apps[21].ID != "osticket" || apps.Apps[21].Name != "osTicket" {
		t.Fatalf("order: %+v ... %+v", apps.Apps[0], apps.Apps[21])
	}
	for _, a := range apps.Apps {
		// The installed PHP is 8.2 and 8.3; Symfony needs 8.4 or later.
		if want := a.ID != "symfony"; a.Installable != want {
			t.Errorf("%s installable = %v, want %v", a.ID, a.Installable, want)
		}
	}
	if code := e.do("GET", "/api/v1/web/missing.example/apps", nil, nil); code != 404 {
		t.Errorf("unknown domain: %d", code)
	}
}

func TestWebAppsForm(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	var f WebAppForm
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps/classicpress", nil, &f); code != 200 {
		t.Fatalf("form: %d", code)
	}
	if !slices.Equal(f.PHPVersions, []string{"8.2", "8.3"}) || !f.DocrootClean || !f.App.Installable {
		t.Errorf("form = %+v", f)
	}
	if f.Database == nil || !slices.Equal(f.Database.Hosts, []string{"localhost"}) ||
		!strings.HasPrefix(f.Database.Name, "classicpress_") || len(f.Database.Name) != len("classicpress_")+10 {
		t.Errorf("database = %+v", f.Database)
	}
	byKey := map[string]WebAppField{}
	for _, fd := range f.Fields {
		byKey[fd.Key] = fd
	}
	if fd := byKey["disable_XML-RPC?"]; fd.Label != "Disable XML-RPC?" || fd.Type != "select" || fd.Value != "Yes" ||
		len(fd.Options) != 2 || fd.Options[1] != (WebAppOption{Value: "No", Label: "No"}) {
		t.Errorf("xml-rpc field = %+v", fd)
	}
	if fd := byKey["language"]; fd.Value != "en_US" || fd.Options[0] != (WebAppOption{Value: "ar_AR", Label: "Arabic"}) {
		t.Errorf("language field = %+v", fd)
	}
	if fd := byKey["password"]; fd.Type != "password" || fd.Label != "Password" {
		t.Errorf("password field = %+v", fd)
	}

	var dk WebAppForm
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps/dokuwiki", nil, &dk); code != 200 || dk.Database != nil {
		t.Fatalf("dokuwiki: %d %+v", code, dk.Database)
	}
	if dk.Fields[5].Label != "Initial ACL Policy" || dk.Fields[5].Options[0].Value != "0: Open Wiki (read, write, upload for everyone)" {
		t.Errorf("dokuwiki acl = %+v", dk.Fields[5])
	}

	var dirty WebAppForm
	if code := e.do("GET", "/api/v1/web/dirty.example.org/apps/wordpress", nil, &dirty); code != 200 || dirty.DocrootClean {
		t.Fatalf("dirty docroot: %d clean=%v", code, dirty.DocrootClean)
	}

	for _, p := range []string{"/apps/WordPress", "/apps/nope", "/apps/jobs", "/apps/..%2f..%2fx"} {
		if code := e.do("GET", "/api/v1/web/plain.example.org"+p, nil, nil); code != 404 {
			t.Errorf("GET %s: %d", p, code)
		}
	}
	if code := e.do("GET", "/api/v1/web/-rf/apps/wordpress", nil, nil); code != 404 {
		t.Errorf("hostile domain: %d", code)
	}
}

func TestWebAppsPluginOff(t *testing.T) {
	e := newWebAppsEnv(t, map[string]string{"PLUGIN_APP_INSTALLER": "false"}, nil)
	e.h.loginAdmin()
	id := strings.Repeat("a", 32)
	for _, r := range []struct{ method, path string }{
		{"GET", "/apps"}, {"GET", "/apps/wordpress"}, {"POST", "/apps/wordpress"}, {"GET", "/apps/jobs/" + id},
	} {
		var er APIError
		if code := e.do(r.method, "/api/v1/web/plain.example.org"+r.path, webAppsWP(), &er); code != 403 || er.Error.Code != "forbidden" {
			t.Errorf("%s %s: %d %+v", r.method, r.path, code, er)
		}
	}
	if w := e.h.webWrites(); len(w) != 0 {
		t.Errorf("commands ran: %q", w)
	}
}

func TestWebAppsNotInstallable(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	var er APIError
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/symfony", WebAppInstallRequest{PHPVersion: "8.3"}, &er); code != 400 ||
		er.Error.Message != "Unable to install %s, required php version is not available." || !slices.Equal(er.Error.Args, []string{"Symfony"}) {
		t.Fatalf("symfony: %d %+v", code, er)
	}
	req := webAppsWP()
	req.PHPVersion = "7.4"
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/wordpress", req, &er); code != 400 || er.Error.Message != "Required PHP version is not supported" {
		t.Fatalf("php 7.4: %d %+v", code, er)
	}
	req = webAppsWP()
	req.Values["language"] = "xx"
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/wordpress", req, nil); code != 400 {
		t.Fatalf("bad select: %d", code)
	}
	req = webAppsWP()
	req.Values["password"] = "a\nb"
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/wordpress", req, nil); code != 400 {
		t.Fatalf("newline: %d", code)
	}
	req = webAppsWP()
	req.Database = nil
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/wordpress", req, nil); code != 400 {
		t.Fatalf("no database block: %d", code)
	}
	if code := e.do("POST", "/api/v1/web/missing.example/apps/wordpress", webAppsWP(), nil); code != 404 {
		t.Fatalf("unknown domain: %d", code)
	}
	if w := e.h.webWrites(); len(w) != 0 {
		t.Errorf("commands ran: %q", w)
	}
}

func TestWebAppsInstallWordPress(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	id := e.install("plain.example.org", "wordpress", webAppsWP())
	st := e.wait(id)
	if st.State != "done" || st.Error != nil || st.Title != "WordPress" {
		t.Fatalf("status = %+v", st)
	}
	root := "/home/admin/web/plain.example.org/public_html"
	wp := "v-run-cli-cmd admin /usr/bin/php8.3 /home/admin/.wp-cli/wp"
	want := []string{
		"v-delete-fs-file admin " + root + "/robots.txt",
		"v-delete-fs-file admin " + root + "/index.html",
		"v-run-cli-cmd admin ls -A -p " + root, // what a failed install must not remove
		"v-add-user-wp-cli admin yes",
		wp + " core download --locale=en_US --version=latest --path=" + root,
		"v-add-database admin wp wpu <tmp> mysql localhost utf8mb4",
		"v-change-web-domain-tpl admin plain.example.org wordpress",
		"v-change-web-domain-backend-tpl admin plain.example.org PHP-8_3",
		"v-add-user-wp-cli admin yes",
		wp + ` config create --dbname=admin_wp --dbuser=admin_wpu --dbpass=pa ss --dbhost=localhost --dbprefix=wp_RAND_ --dbcharset=utf8mb4 --locale=en_US --path=` + root,
	}
	if got := e.writes(); !slices.Equal(got, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", got, want)
	}
	// The install request: resolved to the site's IP and port, form in PHP's order,
	// after a GET of the front page that waits for the PHP pool (Plan 6 C6 D1).
	e.mu.Lock()
	posts := slices.Clone(e.posts)
	e.mu.Unlock()
	wantPosts := []string{
		"dial 203.0.113.50:80",
		"/ ",
		"dial 203.0.113.50:80",
		"/wp-admin/install.php?step=2 weblog_title=My+Blog&user_name=wpadmin&admin_password=Admin+pw1&admin_password2=Admin+pw1&admin_email=a%40b.example",
	}
	if !slices.Equal(posts, wantPosts) {
		t.Fatalf("requests:\n%q\nwant:\n%q", posts, wantPosts)
	}
	// The database password went through a private file that is gone again.
	if e.spy.pwMode != 0o600 || e.spy.pwText != webAppsPass+"\n" {
		t.Errorf("password file: mode %v text %q", e.spy.pwMode, e.spy.pwText)
	}
	left, _ := os.ReadDir(e.tmp)
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	for _, secretText := range []string{webAppsPass, "Admin pw1", "Admin\\ pw1"} {
		if strings.Contains(logs.String(), secretText) {
			t.Errorf("a secret reached the log: %q", secretText)
		}
	}
}

func TestWebAppsInstallLaravel(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	id := e.install("plain.example.org", "laravel", WebAppInstallRequest{
		PHPVersion: "8.3",
		Database:   &WebAppDatabase{Create: false, Host: "localhost", Name: "app", User: "app", Password: "x"},
	})
	if st := e.wait(id); st.State != "done" {
		t.Fatalf("status = %+v", st)
	}
	root := "/home/admin/web/plain.example.org/public_html"
	php := "v-run-cli-cmd admin /usr/bin/php8.3 "
	want := []string{
		"v-delete-fs-file admin " + root + "/robots.txt",
		"v-delete-fs-file admin " + root + "/index.html",
		"v-run-cli-cmd admin ls -A -p " + root, // what a failed install must not remove
		"v-add-user-composer admin 2 yes",
		php + "/home/admin/.composer/composer create-project --no-progress --no-interaction --prefer-dist laravel/laravel -d /home/admin/web/plain.example.org public_html",
		"v-change-web-domain-tpl admin plain.example.org laravel",
		"v-change-web-domain-backend-tpl admin plain.example.org PHP-8_3",
		"v-copy-fs-file admin <tmp> " + root + "/.htaccess",
		"v-open-fs-file admin " + root + "/.env",
		"v-copy-fs-file admin <tmp> " + root + "/.env",
		php + root + "/artisan key:generate --force",
		php + root + "/artisan migrate --force",
		"v-delete-fs-file admin " + root + "/database/database.sqlite",
	}
	if got := e.writes(); !slices.Equal(got, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", got, want)
	}
	if mode := e.spy.copyMode[root+"/.htaccess"]; mode != 0o644 {
		t.Errorf("source file mode = %v", mode)
	}
}

func TestWebAppsInstallGrav(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	id := e.install("plain.example.org", "grav", WebAppInstallRequest{
		PHPVersion: "8.3",
		Values:     map[string]string{"full_name": "Ann Admin", "username": "ann", "password": "secret1", "email": "ann@example.org"},
	})
	if st := e.wait(id); st.State != "done" {
		t.Fatalf("status = %+v", st)
	}
	root := "/home/admin/web/plain.example.org/public_html"
	zip := "https://github.com/getgrav/grav/releases/download/2.0.24/grav-admin-v2.0.24.zip"
	file := root + "/grav-admin-v2.0.24.zip"
	want := []string{
		"v-delete-fs-file admin " + root + "/robots.txt",
		"v-delete-fs-file admin " + root + "/index.html",
		"v-run-cli-cmd admin ls -A -p " + root, // what a failed install must not remove
		"v-run-cli-cmd admin /usr/bin/wget --tries 3 --timeout=30 --no-dns-cache -nv " + zip + " -P " + root,
		"v-extract-fs-archive admin " + file + " " + root + "  1",
		"v-delete-fs-file admin " + file,
		"v-change-web-domain-tpl admin plain.example.org grav",
		"v-change-web-domain-backend-tpl admin plain.example.org PHP-8_3",
		"v-copy-fs-directory admin " + root + "/grav-admin/. " + root,
		"v-copy-fs-file admin <tmp> " + root + "/user/accounts/ann.yaml",
		"v-delete-fs-directory admin " + root + "/grav-admin",
	}
	if got := e.writes(); !slices.Equal(got, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", got, want)
	}
	yaml := e.spy.copies[root+"/user/accounts/ann.yaml"]
	if !strings.HasPrefix(yaml, "state: enabled\nemail: ann@example.org\nfullname: \"Ann Admin\"\n") ||
		!strings.Contains(yaml, "hashed_password: '$2a$") {
		t.Errorf("account file:\n%s", yaml)
	}
}

func TestWebAppsInstallFailure(t *testing.T) {
	e := newWebAppsEnv(t, nil, map[string][]fixtureEntry{
		"v-extract-fs-archive": {{Args: []string{"*", "*", "*", "*", "*"}, Stdout: "Error: boom\n", Exit: 3}},
	})
	e.h.loginAdmin()
	id := e.install("plain.example.org", "grav", WebAppInstallRequest{PHPVersion: "8.3"})
	st := e.wait(id)
	zip := "/home/admin/web/plain.example.org/public_html/grav-admin-v2.0.24.zip"
	if st.State != "failed" || st.Error == nil || st.Error.Message != `Failed to extract "%s"` ||
		!slices.Equal(st.Error.Args, []string{zip}) || st.Finished == "" {
		t.Fatalf("status = %+v / %+v", st, st.Error)
	}
	for _, c := range e.writes() {
		if strings.HasPrefix(c, "v-change-web-domain-tpl") {
			t.Errorf("ran past the failure: %s", c)
		}
	}
}

func TestWebAppsJobs(t *testing.T) {
	release := make(chan struct{})
	e := newWebAppsEnv(t, nil, nil)
	e.spy.hold = func(cmd string, args []string) <-chan struct{} {
		if cmd == "v-delete-fs-file" && strings.Contains(args[1], "plain.example.org") {
			return release
		}
		return nil
	}
	e.h.loginAdmin()
	req := WebAppInstallRequest{PHPVersion: "8.3", Database: &WebAppDatabase{Host: "localhost"}}

	id := e.install("plain.example.org", "laravel", req)
	// The request is over; the job keeps going and shows as running.
	if st := e.status("plain.example.org", id); st.State != "running" || st.Title != "Laravel" || st.Steps == nil {
		t.Fatalf("running = %+v", st)
	}
	var er APIError
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/grav", WebAppInstallRequest{PHPVersion: "8.3"}, &er); code != 409 || er.Error.Code != "busy" ||
		er.Error.Message != "An installation is already running for this domain." {
		t.Fatalf("second install: %d %+v", code, er)
	}
	other := e.install("other.example.org", "grav", WebAppInstallRequest{PHPVersion: "8.3"})
	<-e.s.jobs.Done(other)

	// Another domain's path, another user, and Plan 4's server job all read as 404.
	if code := e.do("GET", "/api/v1/web/other.example.org/apps/jobs/"+id, nil, nil); code != 404 {
		t.Errorf("other domain: %d", code)
	}
	srvID, err := e.s.jobs.Start(context.Background(), jobs.Spec{Key: "server", Owner: "admin", Title: "Server"},
		func(context.Context, *jobs.Job) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	<-e.s.jobs.Done(srvID)
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps/jobs/"+srvID, nil, nil); code != 404 {
		t.Errorf("server job: %d", code)
	}
	for _, bad := range []string{"nope", strings.ToUpper(id), id[:31], id + "0", strings.Repeat("0", 32)} {
		if code := e.do("GET", "/api/v1/web/plain.example.org/apps/jobs/"+bad, nil, nil); code != 404 {
			t.Errorf("job %q: %d", bad, code)
		}
	}
	e.asBob()
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps/jobs/"+id, nil, nil); code != 404 {
		t.Errorf("bob reading admin's job: %d", code)
	}

	close(release)
	<-e.s.jobs.Done(id)
	jar, _ := cookiejar.New(nil)
	e.h.client = &http.Client{Jar: jar}
	e.h.loginAdmin()
	if st := e.status("plain.example.org", id); st.State != "done" || st.Finished == "" {
		t.Fatalf("done = %+v", st)
	}
	// Free again: a new install on the domain starts.
	<-e.s.jobs.Done(e.install("plain.example.org", "grav", WebAppInstallRequest{PHPVersion: "8.3"}))
}

func TestWebAppsDatabaseLimit(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.asBob()
	var er APIError
	req := WebAppInstallRequest{PHPVersion: "8.3", Database: &WebAppDatabase{Create: true, Host: "localhost"},
		Values: map[string]string{}}
	if code := e.do("POST", "/api/v1/web/bakery.example/apps/laravel", req, &er); code != 409 || er.Error.Message != "Unable to add database! Limit reached!" {
		t.Fatalf("limit: %d %+v", code, er)
	}
	if w := e.h.webWrites(); len(w) != 0 {
		t.Errorf("commands ran: %q", w)
	}
}

func TestWebAppsReadOnly(t *testing.T) {
	e := newWebAppsEnv(t, map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}, nil)
	e.h.loginAdmin()
	e.h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var er APIError
	if code := e.do("POST", "/api/v1/web/plain.example.org/apps/wordpress", webAppsWP(), &er); code != 403 || er.Error.Code != "read_only" {
		t.Fatalf("read-only: %d %+v", code, er)
	}
	if calls := e.h.webWrites(); len(calls) != 0 {
		t.Errorf("commands ran: %q", calls)
	}
}

func TestWebAppsFormMissingFolder(t *testing.T) {
	e := newWebAppsEnv(t, nil, map[string][]fixtureEntry{
		"v-run-cli-cmd": {{Args: []string{"*", "ls", "-A", "*"}, Stdout: "ls: cannot access\n", Exit: 2}},
	})
	e.h.loginAdmin()
	var er APIError
	if code := e.do("GET", "/api/v1/web/plain.example.org/apps/wordpress", nil, &er); code != 404 ||
		er.Error.Message != `Web domain path "%s" not found for domain "%s"` ||
		!slices.Equal(er.Error.Args, []string{"/home/admin/web/plain.example.org", "plain.example.org"}) {
		t.Fatalf("missing folder: %d %+v", code, er)
	}
}

func TestWebAppsBlankDatabaseNameAndUserMatch(t *testing.T) {
	e := newWebAppsEnv(t, nil, nil)
	e.h.loginAdmin()
	req := webAppsWP()
	req.Database = &WebAppDatabase{Create: true, Host: "localhost"}
	<-e.s.jobs.Done(e.install("plain.example.org", "wordpress", req))
	for _, c := range e.h.webWrites() {
		if f := strings.Fields(c); len(f) > 3 && f[0] == "v-add-database" {
			if f[2] != f[3] {
				t.Errorf("database %q and user %q differ", f[2], f[3])
			}
			return
		}
	}
	t.Fatal("no v-add-database call")
}
