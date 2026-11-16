package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

type harness struct {
	t       *testing.T
	srv     *httptest.Server
	client  *http.Client
	fixture *runner.Fixture
	store   *session.MemoryStore
	deps    Deps
	token   string
	api     *server // the server behind srv, for the shared hooks (tools sessions)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithLocale(t, "")
}

func newHarnessWithLocale(t *testing.T, localeDir string) *harness {
	t.Helper()
	return newHarnessFrom(t, filepath.Join("..", "..", "testdata", "fixtures", "dev"), localeDir)
}

// newHarnessWith runs the dev fixtures with o applied (see harness_test.go).
func newHarnessWith(t *testing.T, o overrides, opts ...func(*Deps)) *harness {
	t.Helper()
	if o.BackupDir != "" {
		opts = append([]func(*Deps){func(d *Deps) { d.BackupDir = o.BackupDir }}, opts...)
	}
	return newHarnessFrom(t, fixtureDir(t, o), "", opts...)
}

func newHarnessFrom(t *testing.T, fixtures, localeDir string, opts ...func(*Deps)) *harness {
	t.Helper()
	f, err := runner.LoadFixtures(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	h := &hestia.Client{R: f}
	store := session.NewMemoryStore()
	sessions := &session.Manager{Store: store, H: h, R: stablePasswords{f}, Dev: true, SystemHostname: "server1.acme-hosting.net"}
	deps := Deps{
		Auth:      &auth.Service{R: f, H: h, Sleep: func(time.Duration) {}},
		Sessions:  sessions,
		H:         h,
		R:         f,
		Version:   "1.0.0-test",
		Product:   Product{URL: "https://example.test", SecurityEmail: "security@example.test"},
		LocaleDir: localeDir,
		// The test machine may not have them; TestSysFwToolsMissing sets its own.
		FirewallToolsMissing: func() []string { return nil },
	}
	for _, o := range opts {
		o(&deps)
	}
	api, handler := newServer(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, srv: srv, client: &http.Client{Jar: jar}, fixture: f, store: store, deps: deps, api: api}
}

// call sends a JSON request with the current CSRF token and decodes the reply.
func (h *harness) call(method, path string, body any, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("X-CSRF-Token", h.token)
	}
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func (h *harness) bootstrap() Bootstrap {
	h.t.Helper()
	var b Bootstrap
	if code := h.call("GET", "/api/v1/bootstrap", nil, &b); code != 200 {
		h.t.Fatalf("bootstrap: %d", code)
	}
	h.token = b.CSRFToken
	return b
}

func (h *harness) sessionID() string {
	h.t.Helper()
	for _, c := range h.client.Jar.Cookies(mustURL(h.srv.URL)) {
		if c.Name == session.CookieName {
			return c.Value
		}
	}
	return ""
}

type statusReply struct {
	Status  string `json:"status"`
	Landing string `json:"landing"`
	Error   struct{ Code, Message string }
}

func TestBootstrapAnonymous(t *testing.T) {
	h := newHarness(t)
	b := h.bootstrap()
	if b.Authenticated || len(b.CSRFToken) != 32 || b.User != nil {
		t.Fatalf("bootstrap = %+v", b)
	}
	if b.AppName != "Hostisan" || b.Version != "1.0.0-test" || b.Language != "en" || len(b.Languages) < 40 {
		t.Fatalf("bootstrap = %+v", b)
	}
	// the product site and security address (product.env, via -ldflags -X)
	if b.ProductURL != "https://example.test" || b.SecurityEmail != "security@example.test" {
		t.Fatalf("bootstrap product = %q %q", b.ProductURL, b.SecurityEmail)
	}
}

func TestLoginAdmin(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	before := h.sessionID()
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, &r); code != 200 || r.Status != "ok" || r.Landing != "/users" {
		t.Fatalf("login: %d %+v", code, r)
	}
	if after := h.sessionID(); after == "" || after == before {
		t.Fatal("session id must change on login")
	}
	b := h.bootstrap()
	if !b.Authenticated || b.User == nil || b.User.Name != "admin" || b.User.Role != "admin" || b.User.Landing != "/users" {
		t.Fatalf("bootstrap after login = %+v / %+v", b, b.User)
	}
}

func TestCSRFTokenSurvivesLogin(t *testing.T) {
	h := newHarness(t)
	before := h.bootstrap().CSRFToken
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	if after := h.bootstrap().CSRFToken; after != before {
		t.Fatalf("CSRF token changed on login: %s → %s", before, after)
	}
}

func TestLoginWithTwoFA(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "bob", Password: "pw"}, &r); code != 200 || r.Status != "twofa_required" {
		t.Fatalf("step 1: %d %+v", code, r)
	}
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{TwoFA: "000000"}, &r); code != 401 || r.Error.Code != "login_failed" || r.Error.Message != "Invalid or missing 2FA token" {
		t.Fatalf("bad code: %d %+v", code, r)
	}
	r = statusReply{}
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{TwoFA: "123456"}, &r); code != 200 || r.Status != "ok" || r.Landing != "/web" {
		t.Fatalf("step 2: %d %+v", code, r)
	}
}

func TestLoginFailure(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "ghost", Password: "pw"}, &r); code != 401 || r.Error.Code != "login_failed" || r.Error.Message != "Invalid username or password" {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestLoginNeedsCSRF(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.token = ""
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, &r); code != 400 || r.Error.Code != "csrf" {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestLoginAsByUserForbidden(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "bob", Password: "pw"}, nil)
	h.call("POST", "/api/v1/auth/login", LoginRequest{TwoFA: "123456"}, nil)
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil); code != 403 {
		t.Fatalf("got %d, want 403", code)
	}
}

func TestLoginAsAndReturn(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, &r); code != 200 || r.Status != "ok" || r.Landing != "/web" {
		t.Fatalf("login-as: %d %+v", code, r)
	}
	if b := h.bootstrap(); b.User == nil || b.User.Look != "bob" || b.User.Name != "admin" {
		t.Fatalf("bootstrap user = %+v", b.User)
	}
	var actions [][]string
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-log-action" {
			actions = append(actions, c.Args)
		}
	}
	want := [][]string{
		{"admin", "Info", "Security", "Logged in as another user (User: bob)"},
		{"system", "Warning", "Security", "User impersonation session started (User: bob, Administrator: admin)"},
	}
	if !slices.EqualFunc(actions, want, slices.Equal) {
		t.Fatalf("action log = %v", actions)
	}
	if code := h.call("POST", "/api/v1/auth/logout", nil, &r); code != 200 || r.Status != "returned" {
		t.Fatalf("return: %d %+v", code, r)
	}
	if b := h.bootstrap(); b.User == nil || b.User.Look != "" || !b.Authenticated {
		t.Fatalf("after return = %+v", b.User)
	}
}

func TestLoginAsUnknownUser(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "ghost"}, &r); code != 404 || r.Error.Code != "not_found" {
		t.Fatalf("got %d %+v", code, r)
	}
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "-rf"}, &r); code != 404 {
		t.Fatalf("invalid name: got %d", code)
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	var r statusReply
	if code := h.call("POST", "/api/v1/auth/logout", nil, &r); code != 200 || r.Status != "logged_out" {
		t.Fatalf("logout: %d %+v", code, r)
	}
	if code := h.call("POST", "/api/v1/auth/logout", nil, &r); code != 401 && code != 400 {
		t.Fatalf("after logout, an authenticated call gave %d", code)
	}
	if b := h.bootstrap(); b.Authenticated {
		t.Fatal("still authenticated after logout")
	}
	var logouts int
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-log-user-logout" && c.Args[0] == "admin" {
			logouts++
		}
	}
	if logouts != 1 {
		t.Fatalf("v-log-user-logout ran %d times", logouts)
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	h := newHarness(t)
	var r statusReply
	if code := h.call("GET", "/api/v1/nope", nil, &r); code != 404 || r.Error.Code != "not_found" {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestBootstrapCarriesProfileAndUsage(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	u := h.bootstrap().User
	if u == nil || u.DisplayName != "System Administrator" || u.Contact != "admin@acme-hosting.net" {
		t.Fatalf("profile = %+v", u)
	}
	if u.Usage.DiskUsed != "7342" || u.Usage.DiskQuota != "unlimited" || u.Usage.BandwidthUsed != "38211" || u.Usage.BandwidthQuota != "unlimited" {
		t.Fatalf("usage = %+v", u.Usage)
	}
	// Viewing as bob, usage is bob's but the profile stays the admin's.
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	u = h.bootstrap().User
	if u.DisplayName != "System Administrator" || u.Usage.DiskQuota == "unlimited" {
		t.Fatalf("while looking: profile %q, usage %+v", u.DisplayName, u.Usage)
	}
}

// A reload after the session timed out must show the login page, not the shell.
func TestBootstrapEndsTimedOutSession(t *testing.T) {
	h := newHarness(t)
	old := h.bootstrap().CSRFToken
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "bob", Password: "pw"}, nil)
	h.call("POST", "/api/v1/auth/login", LoginRequest{TwoFA: "123456"}, nil)
	id := h.sessionID()
	if !h.store.Update(id, func(s *session.Session) { s.LastActivity = time.Now().Add(-24 * time.Hour) }) {
		t.Fatal("not signed in")
	}
	b := h.bootstrap()
	if b.Authenticated || b.User != nil {
		t.Fatalf("timed-out session bootstrapped as signed in: %+v", b.User)
	}
	if len(b.CSRFToken) != 32 || b.CSRFToken == old {
		t.Fatalf("token = %q; want a fresh anonymous session's", b.CSRFToken)
	}
	if _, ok := h.store.Get(id); ok {
		t.Fatal("the timed-out session still exists")
	}
	if h.sessionID() == id {
		t.Fatal("the browser kept the ended session's cookie")
	}
}

// rc8 N1: a browser whose session the panel's own password change ended elsewhere
// reloads to the login page with the reason, as after a change outside the panel.
func TestBootstrapSaysWhyAPasswordChangeEndedTheSession(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "bob", Password: "pw"}, nil)
	h.call("POST", "/api/v1/auth/login", LoginRequest{TwoFA: "123456"}, nil)
	id := h.sessionID()
	h.api.Sessions.PasswordChanged("bob", "")
	b := h.bootstrap()
	if b.Authenticated || b.SignedOutReason != "You are logged out, please log in again." {
		t.Fatalf("bootstrap after the password change: authenticated %v, reason %q", b.Authenticated, b.SignedOutReason)
	}
	if h.sessionID() == id {
		t.Fatal("the browser kept the ended session's cookie")
	}
	// the reason is given once; the next reload is a plain anonymous visit
	if b := h.bootstrap(); b.SignedOutReason != "" {
		t.Fatalf("second bootstrap reason %q", b.SignedOutReason)
	}
}

// HIDE_SERVICES compares the session user with ROOT_USER; a missing or empty value
// falls back to Hestia's default "admin" so no admin is locked out.
func TestRootUserFallsBackToAdmin(t *testing.T) {
	for _, tc := range []struct {
		cfg  hestia.SysConfig
		want string
	}{
		{hestia.SysConfig{}, "admin"},
		{hestia.SysConfig{"ROOT_USER": ""}, "admin"},
		{hestia.SysConfig{"ROOT_USER": "  "}, "admin"},
		{hestia.SysConfig{"ROOT_USER": "root2"}, "root2"},
	} {
		if got := rootUser(tc.cfg); got != tc.want {
			t.Errorf("rootUser(%v) = %q, want %q", tc.cfg, got, tc.want)
		}
	}
}

// devUsersWithLanguage is the dev v-list-user fixture with each named user's LANGUAGE replaced.
func devUsersWithLanguage(t *testing.T, langs map[string]string) []fixtureEntry {
	t.Helper()
	var entries []fixtureEntry
	readEntries(t, filepath.Join("..", "..", "testdata", "fixtures", "dev", "v-list-user.json"), &entries)
	for i, e := range entries {
		lang, ok := langs[e.Args[0]]
		if !ok {
			continue
		}
		var doc map[string]map[string]any
		if err := json.Unmarshal([]byte(e.Stdout), &doc); err != nil {
			t.Fatal(err)
		}
		doc[e.Args[0]]["LANGUAGE"] = lang
		out, _ := json.Marshal(doc)
		entries[i].Stdout = string(out)
	}
	return entries
}

// Hestia's main.php gives a session with no language the system LANGUAGE (when it is a
// known language), then "en". A user whose own LANGUAGE is empty or unknown (what
// v-add-user writes) gets that too, not "en".
func TestLoginLanguageFallsBackToSystem(t *testing.T) {
	for _, tc := range []struct{ name, user, system, want string }{
		{"own language wins", "de", "fr", "de"},
		{"empty follows the system", "", "fr", "fr"},
		{"unknown follows the system", "xx", "fr", "fr"},
		{"empty with no system language", "", "", "en"},
		{"empty with an unknown system language", "", "zz", "en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessWith(t, overrides{
				Fixtures: map[string][]fixtureEntry{"v-list-user": devUsersWithLanguage(t, map[string]string{"admin": tc.user})},
				Config:   map[string]string{"LANGUAGE": tc.system},
			})
			h.bootstrap()
			if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil); code != 200 {
				t.Fatalf("login: %d", code)
			}
			b := h.bootstrap()
			if !b.Authenticated || b.Language != tc.want {
				t.Fatalf("language = %q, want %q (authenticated %v)", b.Language, tc.want, b.Authenticated)
			}
			// The session holds it too: FileGator gets it from there.
			sess, ok := h.store.Get(h.sessionID())
			if !ok {
				t.Fatal("no session")
			}
			if sess.Language != tc.want {
				t.Fatalf("session language = %q, want %q", sess.Language, tc.want)
			}
		})
	}
}

// Login-as keeps the admin's own language, as Hestia's ?loginas did (it never touched
// $_SESSION["language"]), even when the user has none of their own.
func TestLoginAsKeepsTheAdminLanguage(t *testing.T) {
	h := newHarnessWith(t, overrides{
		Fixtures: map[string][]fixtureEntry{"v-list-user": devUsersWithLanguage(t, map[string]string{"admin": "de", "bob": ""})},
		Config:   map[string]string{"LANGUAGE": "fr"},
	})
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	if b := h.bootstrap(); b.Language != "de" {
		t.Fatalf("admin language = %q", b.Language)
	}
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	if b := h.bootstrap(); b.User == nil || b.User.Look != "bob" || b.Language != "de" {
		t.Fatalf("after login-as: language %q user %+v", b.Language, b.User)
	}
}
