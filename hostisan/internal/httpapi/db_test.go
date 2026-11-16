package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/runner"
)

func (h *harness) dbCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.Contains(c.Cmd, "database") {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// optionsAs fetches /db/options with the Host header set to host and returns the raw body.
func (h *harness) optionsAs(host string) (DBOptions, string) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/db/options", nil)
	req.Host = host
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		h.t.Fatalf("options: %d %s", res.StatusCode, body)
	}
	var o DBOptions
	if err := json.Unmarshal(body, &o); err != nil {
		h.t.Fatal(err)
	}
	return o, string(body)
}

func TestDBList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r Databases
	if code := h.call("GET", "/api/v1/db", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(r.Databases) != 2 || r.Databases[0].DATABASE != "admin_wordpress" || r.Databases[1].DATABASE != "admin_reports" {
		t.Fatalf("databases = %+v", r.Databases)
	}
	d := r.Databases[1]
	if d.DBUSER != "admin_reports" || d.HOST != "localhost" || d.TYPE != "pgsql" || d.CHARSET != "UTF8" ||
		d.U_DISK != "0" || d.SUSPENDED != "yes" || d.TIME != "08:05:12" || d.DATE != "2026-10-09" {
		t.Fatalf("fields = %+v", d)
	}
	if r.Usage.Databases == "" || r.Usage.DatabasesLimit != "unlimited" || r.Usage.Suspended == "" {
		t.Fatalf("usage = %+v", r.Usage)
	}
}

func TestDBListWhileLooking(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	var r Databases
	if code := h.call("GET", "/api/v1/db", nil, &r); code != 200 || len(r.Databases) != 1 || r.Databases[0].DATABASE != "bob_shop" {
		t.Fatalf("%d %+v", code, r)
	}
	if r.Usage.DatabasesLimit != "1" {
		t.Fatalf("usage = %+v", r.Usage)
	}
	if calls := h.dbCalls(); len(calls) != 1 || calls[0] != "v-list-databases bob json" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestDBOne(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var d struct{ DATABASE, DBUSER, TYPE string }
	if code := h.call("GET", "/api/v1/db/admin_wordpress", nil, &d); code != 200 || d.DATABASE != "admin_wordpress" || d.DBUSER != "admin_wpuser" || d.TYPE != "mysql" {
		t.Fatalf("%d %+v", code, d)
	}
}

func TestDBNotFound(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	code := h.call("GET", "/api/v1/db/missing_db", nil, &e)
	if code != 404 || e.Error.Code != "not_found" || e.Error.Message == "" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestDBBadParam(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/db/-h", nil, &e); code != 400 || e.Error.Code != "invalid" {
		t.Fatalf("%d %+v", code, e)
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("commands ran: %v", calls)
	}
}

func TestDBRequiresAuth(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	for _, p := range []string{"/api/v1/db", "/api/v1/db/admin_wordpress", "/api/v1/db/options"} {
		if code := h.call("GET", p, nil, nil); code != 401 {
			t.Errorf("%s: %d", p, code)
		}
	}
}

func TestDBOptions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	o, raw := h.optionsAs("panel.example.com")
	if o.Prefix != "admin_" || o.MaxUserLength != 32 || !o.AdminGate {
		t.Errorf("options = %+v", o)
	}
	if strings.Join(o.Types, ",") != "mysql,pgsql" {
		t.Errorf("types = %v", o.Types)
	}
	wantCharsets := "big5,dec8,cp850,hp8,koi8r,latin1,latin2,swe7,ascii,ujis,sjis,hebrew,tis620,euckr,koi8u,gb2312,greek,cp1250,gbk,latin5,armscii8,utf8,utf8mb4,ucs2,cp866,keybcs2,macce,macroman,cp852,latin7,cp1251,cp1256,cp1257,binary,geostd8,cp932,eucjpms"
	if got := strings.Join(o.MySQLCharsets, ","); got != wantCharsets {
		t.Errorf("mysql charsets = %s", got)
	}
	if o.DefaultCharset["mysql"] != "utf8mb4" || o.DefaultCharset["pgsql"] != "UTF8" {
		t.Errorf("default charset = %v", o.DefaultCharset)
	}
	if len(o.Hosts) != 2 || o.Hosts[0].Host != "localhost" || o.Hosts[0].Type != "mysql" ||
		strings.Join(o.Hosts[0].Charsets, ",") != "BIG5,UTF8,UTF8MB4,LATIN1" ||
		o.Hosts[1].Type != "pgsql" || strings.Join(o.Hosts[1].Charsets, ",") != "UTF8" {
		t.Errorf("hosts = %+v", o.Hosts)
	}
	if o.PHPMyAdminURL != "//panel.example.com/phpmyadmin/" || o.PHPPgAdminURL != "//panel.example.com/phppgadmin/" {
		t.Errorf("urls = %q %q", o.PHPMyAdminURL, o.PHPPgAdminURL)
	}
	if !o.SSOEnabled {
		t.Error("sso should be enabled on a hostname")
	}
	if strings.Contains(raw, "dev-pma-key-not-secret") {
		t.Errorf("PHPMYADMIN_KEY leaked: %s", raw)
	}
}

func TestDBOptionsHostForms(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for host, want := range map[string]struct {
		url string
		sso bool
	}{
		"panel.example.com:8083": {"//panel.example.com/phpmyadmin/", true},
		"203.0.113.5:8083":       {"//203.0.113.5/phpmyadmin/", false},
		"203.0.113.5":            {"//203.0.113.5/phpmyadmin/", false},
		"[2001:db8::1]:8083":     {"//2001:db8::1/phpmyadmin/", false},
		"[2001:db8::1]":          {"//2001:db8::1/phpmyadmin/", false},
	} {
		o, raw := h.optionsAs(host)
		if o.PHPMyAdminURL != want.url || o.SSOEnabled != want.sso {
			t.Errorf("%s: url %q sso %v", host, o.PHPMyAdminURL, o.SSOEnabled)
		}
		if strings.Contains(raw, "dev-pma-key-not-secret") {
			t.Errorf("%s: key leaked", host)
		}
	}
}

func TestDBOptionsConfigVariants(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{
		"DB_SYSTEM": "pgsql", "DB_PMA_ALIAS": "", "DB_PGA_ALIAS": "pga", "PHPMYADMIN_KEY": "",
	}})
	h.loginAdmin()
	o, _ := h.optionsAs("panel.example.com")
	if len(o.Types) != 1 || o.Types[0] != "pgsql" || o.PHPMyAdminURL != "" || o.PHPPgAdminURL != "//panel.example.com/pga/" || o.SSOEnabled {
		t.Errorf("%+v", o)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"DB_SYSTEM": "pgsql, mysql", "DB_PMA_ALIAS": ""}})
	h.loginAdmin()
	o, _ = h.optionsAs("panel.example.com")
	if o.PHPMyAdminURL != "//panel.example.com/phpmyadmin/" || o.PHPPgAdminURL == "" || strings.Join(o.Types, ",") != "pgsql,mysql" {
		t.Errorf("%+v", o)
	}
}

func TestDBOptionsForUser(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var o DBOptions
	if code := h.call("GET", "/api/v1/db/options", nil, &o); code != 200 || o.Prefix != "bob_" || o.AdminGate {
		t.Fatalf("%d %+v", code, o)
	}
	h = newHarness(t)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	h.call("GET", "/api/v1/db/options", nil, &o)
	if o.Prefix != "bob_" || o.AdminGate {
		t.Fatalf("login-as: %+v", o)
	}
}

func TestDBOptionsRouteBeatsWildcard(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.call("GET", "/api/v1/db/options", nil, nil)
	for _, c := range h.dbCalls() {
		if strings.HasPrefix(c, "v-list-database ") {
			t.Fatalf("options handled as a database: %v", h.dbCalls())
		}
	}
}

func TestBootstrapFeaturesDB(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if b := h.bootstrap(); !b.User.Features.DB {
		t.Fatalf("admin: %+v", b.User.Features)
	}
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user": append(userFixture(t, "admin", map[string]string{"ROLE": "admin", "DATABASES": "0"}),
			userFixture(t, "bob", map[string]string{"DATABASES": "3"})...),
	}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User.Features.DB {
		t.Fatalf("package without databases: %+v", b.User.Features)
	}
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if b := h.bootstrap(); !b.User.Features.DB {
		t.Fatalf("looking at bob: %+v", b.User.Features)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"DB_SYSTEM": ""}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User.Features.DB {
		t.Fatalf("no db system: %+v", b.User.Features)
	}
}

// ---- writes (Task 4) ----

func TestSafeRequestHost(t *testing.T) {
	cases := map[string]string{
		"panel.example.com":       "panel.example.com",
		"panel.example.com:8083":  "panel.example.com",
		"localhost":               "localhost",
		"203.0.113.9":             "203.0.113.9",
		"203.0.113.9:8083":        "203.0.113.9",
		"[2001:db8::1]:8083":      "2001:db8::1",
		"[2001:db8::1]":           "2001:db8::1",
		"user@evil.com":           "",
		"user@evil.com:8083":      "",
		"evil.com/x":              "",
		"evil.com\\x":             "",
		"evil .com":               "",
		"-bad.example.com":        "",
		"a..b":                    "",
		"":                        "",
		"evil.com:80/../../x":     "",
		"[2001:db8::1%25eth0]:80": "",
	}
	for host, want := range cases {
		r, _ := http.NewRequest("GET", "http://x/", nil)
		r.Host = host
		if got := safeRequestHost(r); got != want {
			t.Errorf("safeRequestHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestDBOptionsUnsafeHost(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	o, body := h.optionsAs("user@evil.com")
	if o.PHPMyAdminURL != "" || o.PHPPgAdminURL != "" || o.SSOEnabled || strings.Contains(body, "evil.com") {
		t.Fatalf("options with unsafe host: %s", body)
	}
}

// spyRunner calls before for every command, then runs it for real.
type spyRunner struct {
	runner.Runner
	before func(cmd string, args []string)
}

func (s spyRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if s.before != nil {
		s.before(cmd, args)
	}
	return s.Runner.Run(ctx, cmd, args...)
}

type fakeMailer struct {
	sent []mailer.Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, _ hestia.SysConfig, m mailer.Message) error {
	f.sent = append(f.sent, m)
	return f.err
}

type fakeTemplates struct {
	subject, body string
	ok            bool
	asked         [][2]string
}

func (f *fakeTemplates) Load(lang, name string) (string, string, bool) {
	f.asked = append(f.asked, [2]string{lang, name})
	return f.subject, f.body, f.ok
}

func withMail(m *fakeMailer, tp *fakeTemplates, tmp string) func(*Deps) {
	return func(d *Deps) {
		d.Mailer, d.EmailTemplates, d.TmpDir = m, tp, tmp
	}
}

func rawCall(h *harness, method, path, host string, body any) (int, string) {
	h.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, h.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", h.token)
	if host != "" {
		req.Host = host
	}
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

func goodAdd() AddDBRequest {
	return AddDBRequest{Database: "Shop", DBUser: "WpUser", Password: "Passw0rd", Type: "mysql", Host: "localhost", Charset: "utf8mb4"}
}

func failDB(cmd string, exit int, out string, argc int) overrides {
	args := make([]string, argc)
	for i := range args {
		args[i] = "*"
	}
	return overrides{Fixtures: map[string][]fixtureEntry{cmd: {{Args: args, Stdout: out, Exit: exit}}}}
}

func TestAddDBArgsAndTempFile(t *testing.T) {
	tmp := t.TempDir()
	var path, content string
	var mode os.FileMode
	h := newHarnessWith(t, overrides{}, withMail(&fakeMailer{}, &fakeTemplates{}, tmp), func(d *Deps) {
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-add-database" {
				return
			}
			path = args[3]
			if fi, err := os.Stat(path); err == nil {
				mode = fi.Mode().Perm()
			}
			b, _ := os.ReadFile(path)
			content = string(b)
		}}
	})
	h.loginAdmin()
	code, body := rawCall(h, "POST", "/api/v1/db", "", goodAdd())
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if !strings.HasPrefix(path, tmp+"/") || mode != 0o600 || content != "Passw0rd\n" {
		t.Fatalf("temp file %q mode %v content %q", path, mode, content)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temp file still there: %v", err)
	}
	want := "v-add-database admin Shop WpUser " + path + " mysql localhost utf8mb4"
	if calls := h.dbCalls(); len(calls) != 1 || calls[0] != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if strings.Contains(body, "Passw0rd") {
		t.Fatalf("reply has the password: %s", body)
	}
	for _, c := range h.fixture.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), "Passw0rd") {
			t.Fatalf("password in call %v", c)
		}
	}
	var r AddDBReply
	_ = json.Unmarshal([]byte(body), &r)
	if r.Status != "ok" || r.Database != "admin_Shop" || r.DBAdmin != "phpMyAdmin" || r.DBAdminURL != "https://127.0.0.1/phpmyadmin" {
		t.Fatalf("reply = %+v", r)
	}
}

func TestAddDBTempFileRemovedOnFailure(t *testing.T) {
	tmp := t.TempDir()
	h := newHarnessWith(t, failDB("v-add-database", 4, "Error: database admin_Shop exists\n", 7), withMail(&fakeMailer{}, &fakeTemplates{}, tmp))
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/db", goodAdd(), &e); code != 409 || e.Error.Code != "exists" || e.Error.Message != "Error: database admin_Shop exists" {
		t.Fatalf("%d %+v", code, e)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestAddDBValidationOrder(t *testing.T) {
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/db", AddDBRequest{}, &e); code != 400 || e.Error.Message != `Field "%s" can not be blank.` ||
		strings.Join(e.Error.Args, ",") != "Database,Username,Password,Type,Host,Charset" {
		t.Fatalf("blank: %d %+v", code, e)
	}
	req := goodAdd()
	req.Email, req.Password = "not-an-email", "weak"
	e = APIError{}
	if code := h.call("POST", "/api/v1/db", req, &e); code != 400 || e.Error.Message != "Please enter a valid email address." {
		t.Fatalf("email: %d %+v", code, e)
	}
	req.Email = "me@example.com"
	e = APIError{}
	if code := h.call("POST", "/api/v1/db", req, &e); code != 400 || e.Error.Message != "Password does not match the minimum requirements." {
		t.Fatalf("password: %d %+v", code, e)
	}
	if calls := h.dbCalls(); len(calls) != 0 || len(mm.sent) != 0 {
		t.Fatalf("calls %v mails %v", calls, mm.sent)
	}
}

func TestAddDBEmailSent(t *testing.T) {
	mm := &fakeMailer{}
	tp := &fakeTemplates{ok: true,
		subject: "New {{dabase}} for {{dbuser}} on {{hostname}} by {{appname}}",
		body:    "DB {{database}} U {{username}} P {{password}} A {{dbadmin}} {{appname}}"}
	h := newHarnessWith(t, overrides{}, withMail(mm, tp, t.TempDir()))
	h.loginAdmin()
	req := goodAdd()
	req.Email = "me@example.com"
	req.Password = "Pa<ss>w0rd"
	if code, body := rawCall(h, "POST", "/api/v1/db", "", req); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if len(mm.sent) != 1 {
		t.Fatalf("sent %v", mm.sent)
	}
	m := mm.sent[0]
	if m.To != "me@example.com" ||
		m.Subject != "New admin_Shop for admin_WpUser on server1.acme-hosting.net by Hostisan" ||
		m.Body != "DB admin_Shop U admin_WpUser P Pa&lt;ss&gt;w0rd A https://127.0.0.1/phpmyadmin Hostisan" {
		t.Fatalf("message = %+v", m)
	}
	if len(tp.asked) != 1 || tp.asked[0] != [2]string{"en", "database_credentials"} {
		t.Fatalf("template asked %v", tp.asked)
	}
}

func TestAddDBEmailFallbacks(t *testing.T) {
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{Config: map[string]string{"SUBJECT_EMAIL": "[{{hostname}}] {{subject}}"}},
		withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	req := goodAdd()
	req.Email = "me@example.com"
	h.call("POST", "/api/v1/db", req, nil)
	if len(mm.sent) != 1 {
		t.Fatalf("sent %v", mm.sent)
	}
	want := "Database has been created.\n\nDatabase: admin_Shop\nUsername: admin_WpUser\nPassword: Passw0rd\nSQL Manager: https://127.0.0.1/phpmyadmin\n\nBest regards,\n\n--\nHostisan"
	if m := mm.sent[0]; m.Subject != "[server1.acme-hosting.net] Database Credentials: admin_Shop" || m.Body != want {
		t.Fatalf("message = %+v", m)
	}
}

func TestAddDBNoEmailNoMail(t *testing.T) {
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	h.call("POST", "/api/v1/db", goodAdd(), nil)
	if len(mm.sent) != 0 {
		t.Fatalf("sent %v", mm.sent)
	}
}

func TestAddDBEmailFailureStillOK(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	mm := &fakeMailer{err: errors.New("smtp down")}
	h := newHarnessWith(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	req := goodAdd()
	req.Email = "me@example.com"
	var r AddDBReply
	if code := h.call("POST", "/api/v1/db", req, &r); code != 200 || r.Status != "ok" {
		t.Fatalf("%d %+v", code, r)
	}
	if !strings.Contains(logs.String(), "smtp down") || !strings.Contains(logs.String(), "credentials") {
		t.Fatalf("failure not logged: %q", logs.String())
	}
	if strings.Contains(logs.String(), "Passw0rd") {
		t.Fatalf("password logged: %q", logs.String())
	}
}

func TestAddDBAdminUrl(t *testing.T) {
	cases := []struct {
		name, host, reqHost, typ, alias, want string
		cfgKey                                string
	}{
		{"localhost uses request host", "localhost", "panel.example.com:8083", "mysql", "phpmyadmin", "https://panel.example.com/phpmyadmin", "DB_PMA_ALIAS"},
		{"remote host wins", "db1.example.com", "panel.example.com:8083", "mysql", "pma", "https://db1.example.com/pma", "DB_PMA_ALIAS"},
		{"no alias keeps slash", "localhost", "panel.example.com", "mysql", "", "https://panel.example.com/phpmyadmin/", "DB_PMA_ALIAS"},
		{"pgsql", "localhost", "panel.example.com", "pgsql", "", "https://panel.example.com/phppgadmin/", "DB_PGA_ALIAS"},
		{"unsafe host and localhost", "localhost", "user@evil.com", "mysql", "", "", "DB_PMA_ALIAS"},
		{"unsafe host but remote db", "db1.example.com", "user@evil.com", "mysql", "", "https://db1.example.com/phpmyadmin/", "DB_PMA_ALIAS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarnessWith(t, overrides{Config: map[string]string{c.cfgKey: c.alias}}, withMail(&fakeMailer{}, &fakeTemplates{}, t.TempDir()))
			h.loginAdmin()
			req := goodAdd()
			req.Type, req.Host = c.typ, c.host
			var r AddDBReply
			b, _ := json.Marshal(req)
			hr, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/db", bytes.NewReader(b))
			hr.Header.Set("X-CSRF-Token", h.token)
			hr.Host = c.reqHost
			res, err := h.client.Do(hr)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			_ = json.NewDecoder(res.Body).Decode(&r)
			if res.StatusCode != 200 || r.DBAdminURL != c.want {
				t.Fatalf("%d %+v, want url %q", res.StatusCode, r, c.want)
			}
		})
	}
}

func TestEditDBRenameThenPassword(t *testing.T) {
	tmp := t.TempDir()
	var path string
	h := newHarnessWith(t, overrides{}, withMail(&fakeMailer{}, &fakeTemplates{}, tmp), func(d *Deps) {
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd == "v-change-database-password" {
				path = args[2]
			}
		}}
	})
	h.loginAdmin()
	code, body := rawCall(h, "PUT", "/api/v1/db/admin_wordpress", "", EditDBRequest{DBUser: "newuser", Password: "Passw0rd"})
	if code != 200 || strings.Contains(body, "Passw0rd") {
		t.Fatalf("%d %s", code, body)
	}
	want := []string{
		"v-list-database admin admin_wordpress json",
		"v-change-database-user admin admin_wordpress newuser",
		"v-change-database-password admin admin_wordpress " + path,
	}
	if got := h.dbCalls(); strings.Join(got, "\n") != strings.Join(want, "\n") || !strings.HasPrefix(path, tmp+"/") {
		t.Fatalf("calls %q want %q", got, want)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestEditDBPasswordFailureKeepsRename(t *testing.T) {
	o := failDB("v-change-database-password", 5, "Error: database admin_wordpress is suspended\n", 3)
	h := newHarnessWith(t, o, withMail(&fakeMailer{}, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	var e APIError
	if code := h.call("PUT", "/api/v1/db/admin_wordpress", EditDBRequest{DBUser: "newuser", Password: "Passw0rd"}, &e); code != 409 || e.Error.Code != "suspended" {
		t.Fatalf("%d %+v", code, e)
	}
	calls := h.dbCalls()
	if len(calls) != 3 || !strings.HasPrefix(calls[1], "v-change-database-user admin admin_wordpress newuser") {
		t.Fatalf("calls = %q", calls)
	}
}

func TestEditDBRenameFailureStops(t *testing.T) {
	o := failDB("v-change-database-user", 4, "Error: user exists\n", 3)
	h := newHarnessWith(t, o, withMail(&fakeMailer{}, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	var e APIError
	if code := h.call("PUT", "/api/v1/db/admin_wordpress", EditDBRequest{DBUser: "newuser", Password: "Passw0rd"}, &e); code != 409 {
		t.Fatalf("%d %+v", code, e)
	}
	if calls := h.dbCalls(); len(calls) != 2 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestEditDBUnchangedUserSkipsRename(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	// The stored user is admin_wpuser; the form shows it without the prefix.
	var r OK
	if code := h.call("PUT", "/api/v1/db/admin_wordpress", EditDBRequest{DBUser: "wpuser"}, &r); code != 200 || r.Status != "ok" {
		t.Fatalf("%d %+v", code, r)
	}
	if calls := h.dbCalls(); len(calls) != 1 || calls[0] != "v-list-database admin admin_wordpress json" {
		t.Fatalf("calls = %q", calls)
	}
}

func TestEditDBBlankUser(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	if code := h.call("PUT", "/api/v1/db/admin_wordpress", EditDBRequest{DBUser: "  ", Password: "Passw0rd"}, &e); code != 400 ||
		e.Error.Message != `Field "%s" can not be blank.` || strings.Join(e.Error.Args, ",") != "Username" {
		t.Fatalf("%d %+v", code, e)
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestEditDBWeakPassword(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	if code := h.call("PUT", "/api/v1/db/admin_wordpress", EditDBRequest{DBUser: "newuser", Password: "weak"}, &e); code != 400 ||
		e.Error.Message != "Password does not match the minimum requirements." {
		t.Fatalf("%d %+v", code, e)
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("no command may run: %q", calls)
	}
}

func TestEditDBBadParam(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("PUT", "/api/v1/db/-h", EditDBRequest{DBUser: "x"}, nil); code != 400 {
		t.Fatalf("code %d", code)
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestDBDelete(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var r OK
	if code := h.call("DELETE", "/api/v1/db/bob_shop", nil, &r); code != 200 || r.Status != "ok" {
		t.Fatalf("%d %+v", code, r)
	}
	if calls := h.dbCalls(); len(calls) != 1 || calls[0] != "v-delete-database bob bob_shop" {
		t.Fatalf("calls = %q", calls)
	}
}

func TestDBSuspendAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, a := range []string{"suspend", "unsuspend"} {
		if code := h.call("POST", "/api/v1/db/bob_shop/"+a, nil, nil); code != 403 {
			t.Fatalf("bob %s: %d", a, code)
		}
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
	h = newHarness(t)
	h.loginAdmin()
	for _, a := range []string{"suspend", "unsuspend"} {
		if code := h.call("POST", "/api/v1/db/admin_wordpress/"+a, nil, nil); code != 200 {
			t.Fatalf("admin %s: %d", a, code)
		}
	}
	want := "v-suspend-database admin admin_wordpress\nv-unsuspend-database admin admin_wordpress"
	if got := strings.Join(h.dbCalls(), "\n"); got != want {
		t.Fatalf("calls = %q", got)
	}
}

func TestDBBulkUserDeleteOnly(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var e APIError
	for _, a := range []string{"suspend", "unsuspend", "rebuild"} {
		if code := h.call("POST", "/api/v1/db/bulk", BulkRequest{Action: a, Names: []string{"bob_shop"}}, &e); code != 403 {
			t.Fatalf("%s: %d", a, code)
		}
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
	var r BulkReply
	if code := h.call("POST", "/api/v1/db/bulk", BulkRequest{Action: "delete", Names: []string{"bob_shop"}}, &r); code != 200 || r.Status != "ok" || r.Failed == nil || len(r.Failed) != 0 {
		t.Fatalf("%d %+v", code, r)
	}
	if calls := h.dbCalls(); len(calls) != 1 || calls[0] != "v-delete-database bob bob_shop" {
		t.Fatalf("calls = %q", calls)
	}
}

func TestDBBulkAdminRebuild(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r BulkReply
	names := []string{"admin_wordpress", "admin_reports"}
	if code := h.call("POST", "/api/v1/db/bulk", BulkRequest{Action: "rebuild", Names: names}, &r); code != 200 || len(r.Failed) != 0 {
		t.Fatalf("%d %+v", code, r)
	}
	want := "v-rebuild-database admin admin_wordpress\nv-rebuild-database admin admin_reports"
	if got := strings.Join(h.dbCalls(), "\n"); got != want {
		t.Fatalf("calls = %q", got)
	}
	// One bad name stops the whole run before any command.
	h2 := newHarness(t)
	h2.loginAdmin()
	if code := h2.call("POST", "/api/v1/db/bulk", BulkRequest{Action: "delete", Names: []string{"ok_db", "-h"}}, nil); code != 400 {
		t.Fatalf("bad name: %d", code)
	}
	if calls := h2.dbCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestDBBulkReportsFailures(t *testing.T) {
	o := overrides{Fixtures: map[string][]fixtureEntry{"v-delete-database": {
		{Args: []string{"*", "gone"}, Stdout: "Error: database gone doesn't exist\n", Exit: 3},
		{Args: []string{"*", "*"}},
	}}}
	h := newHarnessWith(t, o)
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/db/bulk", BulkRequest{Action: "delete", Names: []string{"gone", "here"}}, &r); code != 200 ||
		len(r.Failed) != 1 || r.Failed[0].Name != "gone" || r.Failed[0].Message != "Error: database gone doesn't exist" {
		t.Fatalf("%d %+v", code, r)
	}
	if len(h.dbCalls()) != 2 {
		t.Fatalf("calls = %q", h.dbCalls())
	}
}

func TestDBWritesReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}},
		withMail(&fakeMailer{}, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	routes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/db", goodAdd()},
		{"PUT", "/api/v1/db/admin_wordpress", EditDBRequest{DBUser: "x", Password: "Passw0rd"}},
		{"DELETE", "/api/v1/db/admin_wordpress", nil},
		{"POST", "/api/v1/db/admin_wordpress/suspend", nil},
		{"POST", "/api/v1/db/admin_wordpress/unsuspend", nil},
		{"POST", "/api/v1/db/bulk", BulkRequest{Action: "delete", Names: []string{"admin_wordpress"}}},
	}
	for _, rt := range routes {
		var e APIError
		if code := h.call(rt.method, rt.path, rt.body, &e); code != 403 || e.Error.Code != "read_only" {
			t.Errorf("%s %s: %d %+v", rt.method, rt.path, code, e)
		}
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestAddDBEmailMultilineSubject(t *testing.T) {
	mm := &fakeMailer{}
	tp := &fakeTemplates{ok: true, subject: "\n  Hi {{dbuser}}\n", body: "x"}
	h := newHarnessWith(t, overrides{}, withMail(mm, tp, t.TempDir()))
	h.loginAdmin()
	req := goodAdd()
	req.Email = "me@example.com"
	if code, body := rawCall(h, "POST", "/api/v1/db", "", req); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if len(mm.sent) != 1 || mm.sent[0].Subject != "Hi admin_WpUser" {
		t.Fatalf("sent %+v", mm.sent)
	}
}

// getNoFollow sends a GET with the given Host and does not follow redirects.
func getNoFollow(h *harness, path, host string) *http.Response {
	h.t.Helper()
	c := *h.client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
	if host != "" {
		req.Host = host
	}
	res, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { res.Body.Close() })
	return res
}

const dumpName = "admin_admin_wordpress_mysql_2026-10-17_10-00-00.sql.gz"

func TestDBDownloadHeaders(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/download", "")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || len(body) != 0 {
		t.Fatalf("%d %q", res.StatusCode, body)
	}
	if res.Header.Get("Content-Type") != "application/sql" ||
		res.Header.Get("X-Accel-Redirect") != accelPrefix+"/backup/"+dumpName ||
		res.Header.Get("Content-Disposition") != `attachment; filename="`+dumpName+`"` {
		t.Fatalf("headers = %v", res.Header)
	}
	var dump string
	for _, c := range h.dbCalls() {
		if strings.HasPrefix(c, "v-dump-database") {
			dump = c
		}
	}
	if dump != "v-dump-database admin admin_wordpress file gzip" {
		t.Fatalf("dump call = %q", dump)
	}
}

func TestDBDownloadNotFound(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/db/missing_db/download", nil, &e); code != 404 || e.Error.Code != "not_found" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestDBDownloadRejectsRelativePath(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-dump-database": {{Args: []string{"*", "*", "*", "*"}, Stdout: "dump.sql.gz\n"}}}})
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/db/admin_wordpress/download", nil, &e); code != 500 || e.Error.Code != "server_error" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestDBDownloadBadParam(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("GET", "/api/v1/db/-x/download", nil, nil); code != 400 {
		t.Fatalf("code %d", code)
	}
}

func TestDBDownloadAllowedReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/download", "")
	if res.StatusCode != 200 || res.Header.Get("X-Accel-Redirect") == "" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
}

func withClock(at time.Time) func(*Deps) {
	return func(d *Deps) { d.Now = func() time.Time { return at } }
}

func TestPhpMyAdminSSORedirect(t *testing.T) {
	now := time.Unix(1790000000, 0)
	h := newHarnessWith(t, overrides{}, withClock(now))
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", "panel.example.com:8083")
	if res.StatusCode != http.StatusFound || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	loc := res.Header.Get("Location")
	prefix := "//panel.example.com/phpmyadmin/hestia-sso.php?host=localhost&database=admin_wordpress&user=admin&exp=1790000000&hestia_token="
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("location = %q", loc)
	}
	token, err := url.QueryUnescape(strings.TrimPrefix(loc, prefix))
	if err != nil {
		t.Fatal(err)
	}
	sess, ok := h.store.Get(h.sessionID())
	if !ok || sess.CombinedIP == "" {
		t.Fatalf("session %+v", sess)
	}
	if token != phpMyAdminToken("dev-pma-key-not-secret", "admin_wordpress", "admin", sess.CombinedIP, "1790000000") {
		t.Fatalf("token %q is not the HMAC over the session IP", token)
	}
	if token == phpMyAdminToken("dev-pma-key-not-secret", "admin_wordpress", "admin", "198.51.100.7", "1790000000") {
		t.Fatal("token matches another IP")
	}
}

// The token is hex(HMAC-SHA256(PHPMYADMIN_KEY, database|user|ip|exp)), which
// hestia-sso.php checks with hash_equals (SSO4). The value was computed with:
// printf '%s' 'admin_wordpress|admin|203.0.113.9|1790000000' | openssl dgst -sha256 -hmac dev-pma-key-not-secret
func TestPhpMyAdminTokenHMAC(t *testing.T) {
	got := phpMyAdminToken("dev-pma-key-not-secret", "admin_wordpress", "admin", "203.0.113.9", "1790000000")
	if want := "2f10a9a4152a8cbb486853f1d7f0ea3998f39a11d70ce2389801d6855cd358c9"; got != want {
		t.Fatalf("token = %q, want %q", got, want)
	}
	// A long key counts in full (bcrypt read only 72 bytes of its input).
	long := strings.Repeat("k", 100)
	if phpMyAdminToken(long, "admin_wordpress", "admin", "203.0.113.9", "1790000000") ==
		phpMyAdminToken(long+"x", "admin_wordpress", "admin", "203.0.113.9", "1790000000") {
		t.Fatal("key bytes past 72 are ignored")
	}
	// The separators keep the fields apart.
	if phpMyAdminToken("k", "ab", "c", "ip", "1") == phpMyAdminToken("k", "a", "bc", "ip", "1") {
		t.Fatal("fields run together")
	}
}

func TestPhpMyAdminSSOCustomAlias(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"DB_PMA_ALIAS": ""}})
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", "panel.example.com")
	if loc := res.Header.Get("Location"); res.StatusCode != 302 || !strings.HasPrefix(loc, "//panel.example.com/phpmyadmin/hestia-sso.php?") {
		t.Fatalf("%d %q", res.StatusCode, loc)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"DB_PMA_ALIAS": "pma"}})
	h.loginAdmin()
	res = getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", "panel.example.com")
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "//panel.example.com/pma/hestia-sso.php?") {
		t.Fatalf("%q", loc)
	}
}

func TestPhpMyAdminSSONotForPgsql(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/admin_reports/phpmyadmin", "panel.example.com")
	if res.StatusCode != 404 {
		t.Fatalf("code %d", res.StatusCode)
	}
}

func TestPhpMyAdminSSOIPHost(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, host := range []string{"", "203.0.113.5:8083", "user@evil.com"} {
		res := getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", host)
		if res.StatusCode != 404 {
			t.Fatalf("host %q: code %d", host, res.StatusCode)
		}
	}
}

func TestPhpMyAdminSSONoKey(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"PHPMYADMIN_KEY": ""}})
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", "panel.example.com")
	if res.StatusCode != 404 {
		t.Fatalf("code %d", res.StatusCode)
	}
}

func TestPhpMyAdminSSOMissingDatabase(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/missing_db/phpmyadmin", "panel.example.com")
	if res.StatusCode != 404 {
		t.Fatalf("code %d", res.StatusCode)
	}
}

func TestSSOKeyNotLogged(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	h := newHarness(t)
	h.loginAdmin()
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", "panel.example.com")
	if res.StatusCode != 302 {
		t.Fatalf("code %d", res.StatusCode)
	}
	token := strings.SplitN(res.Header.Get("Location"), "hestia_token=", 2)[1]
	if strings.Contains(logs.String(), "dev-pma-key-not-secret") || strings.Contains(logs.String(), token) {
		t.Fatalf("secret logged: %q", logs.String())
	}
	for _, c := range h.fixture.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), "dev-pma-key") {
			t.Fatalf("command carries the key: %v", c)
		}
	}
}

func TestPhpMyAdminSSOBlockedReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	res := getNoFollow(h, "/api/v1/db/admin_wordpress/phpmyadmin", "panel.example.com")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 403 || !strings.Contains(string(body), `"read_only"`) || res.Header.Get("Location") != "" {
		t.Fatalf("%d %s %v", res.StatusCode, body, res.Header)
	}
	if calls := h.dbCalls(); len(calls) != 0 {
		t.Fatalf("commands ran: %v", calls)
	}
}

func TestAddDBPgsqlLowercasesNames(t *testing.T) {
	mm := &fakeMailer{}
	tp := &fakeTemplates{ok: true, subject: "New {{dabase}} for {{dbuser}}", body: "DB {{database}} U {{username}}"}
	h := newHarnessWith(t, overrides{}, withMail(mm, tp, t.TempDir()))
	h.loginAdmin()
	req := goodAdd()
	req.Type = "pgsql"
	req.Email = "me@example.com"
	code, body := rawCall(h, "POST", "/api/v1/db", "", req)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var r AddDBReply
	_ = json.Unmarshal([]byte(body), &r)
	if r.Database != "admin_shop" {
		t.Fatalf("reply database = %q", r.Database)
	}
	if len(mm.sent) != 1 || mm.sent[0].Subject != "New admin_shop for admin_wpuser" || mm.sent[0].Body != "DB admin_shop U admin_wpuser" {
		t.Fatalf("sent = %+v", mm.sent)
	}
	calls := h.dbCalls()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "v-add-database admin Shop WpUser ") || !strings.Contains(calls[0], " pgsql ") {
		t.Fatalf("calls = %q", calls)
	}
}

// Decision 29 (D13): an admin is told why phpMyAdmin SSO can't work with the API
// settings. Server IPs come from v-list-sys-ips (10.0.0.5 NAT 198.51.100.7,
// 203.0.113.10, 203.0.113.11 in the dev fixtures).
func TestDBOptionsSSOHint(t *testing.T) {
	const (
		apiMsg = "phpMyAdmin single sign-on needs the API. Turn it on in the server configuration."
		ipMsg  = "phpMyAdmin single sign-on needs this server's IP address in the API allowed IP list."
	)
	ipsListed := func(h *harness) int {
		n := 0
		for _, c := range h.fixture.Calls() {
			if c.Cmd == "v-list-sys-ips" {
				n++
			}
		}
		return n
	}
	for _, c := range []struct {
		name string
		cfg  map[string]string
		want string
		ips  int // v-list-sys-ips runs
	}{
		{"api off", map[string]string{"API_SYSTEM": "0"}, apiMsg, 0},
		{"api empty", map[string]string{"API_SYSTEM": ""}, apiMsg, 0},
		{"other ip", map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "10.9.9.9"}, ipMsg, 1},
		{"empty list", map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": ""}, ipMsg, 1},
		{"allow-all", map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "allow-all"}, "", 0},
		{"loopback", map[string]string{"API_SYSTEM": "2", "API_ALLOWED_IP": "10.9.9.9,127.0.0.1"}, "", 0},
		{"any", map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "0.0.0.0"}, "", 0},
		{"server ip", map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "10.9.9.9, 203.0.113.11"}, "", 1},
		{"server nat", map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "198.51.100.7"}, "", 1},
		{"no key", map[string]string{"API_SYSTEM": "0", "PHPMYADMIN_KEY": ""}, "", 0},
	} {
		h := newHarnessWith(t, overrides{Config: c.cfg})
		h.loginAdmin()
		o, raw := h.optionsAs("panel.example.com")
		if o.SSOHint != c.want {
			t.Errorf("%s: hint = %q, want %q", c.name, o.SSOHint, c.want)
		}
		if n := ipsListed(h); n != c.ips {
			t.Errorf("%s: v-list-sys-ips ran %d times, want %d", c.name, n, c.ips)
		}
		if strings.Contains(raw, "dev-pma-key-not-secret") {
			t.Errorf("%s: key leaked", c.name)
		}
	}
	// No hint where the SSO link isn't offered (panel reached by IP address).
	h := newHarnessWith(t, overrides{Config: map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "10.9.9.9"}})
	h.loginAdmin()
	if o, _ := h.optionsAs("203.0.113.5:8083"); o.SSOEnabled || o.SSOHint != "" || ipsListed(h) != 0 {
		t.Errorf("by IP: sso %v hint %q ips %d", o.SSOEnabled, o.SSOHint, ipsListed(h))
	}
	// Users never see it, and nothing extra runs for them.
	h = newHarnessWith(t, overrides{Config: map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "10.9.9.9"}})
	h.loginBob()
	if o, _ := h.optionsAs("panel.example.com"); o.SSOHint != "" || ipsListed(h) != 0 {
		t.Errorf("bob: hint %q, ips %d", o.SSOHint, ipsListed(h))
	}
	// An admin looking at a user is still the admin who can fix it.
	h = newHarnessWith(t, overrides{Config: map[string]string{"API_SYSTEM": "0"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if o, _ := h.optionsAs("panel.example.com"); o.SSOHint != apiMsg {
		t.Errorf("admin as bob: hint %q", o.SSOHint)
	}
}
