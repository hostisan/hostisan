package httpapi

// Shared mail test helpers (Task 1). The slices' own test files use these and must
// not redefine them; any helper a slice adds is prefixed with its area (for example
// mailDomainsBody, mailAccountsLogin) so the merged package still compiles.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// mailCalls is every mail-related command run so far, as "cmd arg arg…": everything
// with "mail" or "letsencrypt" in its name.
func (h *harness) mailCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.Contains(c.Cmd, "mail") || strings.Contains(c.Cmd, "letsencrypt") {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// mailWrites is mailCalls without the v-list-* reads: the commands that change state.
func (h *harness) mailWrites() []string {
	var out []string
	for _, c := range h.mailCalls() {
		if !strings.HasPrefix(c, "v-list-") {
			out = append(out, c)
		}
	}
	return out
}

// mailServer is the server behind h, for calling handler helpers directly.
func (h *harness) mailServer() *server {
	return &server{Deps: h.deps}
}

func TestBootstrapFeaturesMail(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if b := h.bootstrap(); !b.User.Features.Mail {
		t.Fatalf("admin: %+v", b.User.Features)
	}
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user": append(userFixture(t, "admin", map[string]string{"ROLE": "admin", "MAIL_DOMAINS": "0"}),
			userFixture(t, "bob", map[string]string{"MAIL_DOMAINS": "1"})...),
	}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User.Features.Mail {
		t.Fatalf("package without mail domains: %+v", b.User.Features)
	}
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if b := h.bootstrap(); !b.User.Features.Mail {
		t.Fatalf("looking at bob: %+v", b.User.Features)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"MAIL_SYSTEM": ""}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User.Features.Mail {
		t.Fatalf("no mail system: %+v", b.User.Features)
	}
}

func TestMailDevConfig(t *testing.T) {
	h := newHarness(t)
	cfg, err := h.deps.H.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"MAIL_SYSTEM": "exim4", "IMAP_SYSTEM": "dovecot", "ANTISPAM_SYSTEM": "spamassassin",
		"ANTIVIRUS_SYSTEM": "clamav-daemon", "WEBMAIL_SYSTEM": "roundcube,snappymail", "WEBMAIL_ALIAS": "webmail",
	} {
		if cfg[k] != want {
			t.Errorf("%s = %q, want %q", k, cfg[k], want)
		}
	}
	u, ok, err := h.deps.H.User(context.Background(), "admin")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, k := range []string{"MAIL_DOMAINS", "MAIL_ACCOUNTS", "U_MAIL_DOMAINS", "U_MAIL_ACCOUNTS", "SUSPENDED_MAIL"} {
		if u[k] == "" {
			t.Errorf("admin %s is empty", k)
		}
	}
}

func TestMailDomainHelper(t *testing.T) {
	h := newHarness(t)
	s := h.mailServer()
	w := httptest.NewRecorder()
	d, ok := s.mailDomain(w, httptest.NewRequest("GET", "/", nil), "admin", "example.com")
	if !ok || d.NAME != "example.com" || d.DKIM_SELECTOR != "mail" || d.WEBMAIL_ALIAS != "webmail.example.com" {
		t.Fatalf("detail = %+v, %v (%d %s)", d, ok, w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	if _, ok := s.mailDomain(w, httptest.NewRequest("GET", "/", nil), "admin", "missing.example"); ok || w.Code != http.StatusNotFound {
		t.Fatalf("missing.example: ok=%v code=%d", ok, w.Code)
	}
	w = httptest.NewRecorder()
	ssl, ok := s.mailDomainSSL(w, httptest.NewRequest("GET", "/", nil), "admin", "example.com")
	if !ok || ssl.SUBJECT != "mail.example.com" || !strings.Contains(ssl.KEY, "PRIVATE KEY") {
		t.Fatalf("ssl = %+v, %v (%d)", ssl, ok, w.Code)
	}
}

func TestMailAccountParam(t *testing.T) {
	for v, want := range map[string]bool{
		"info": true, "a.b-c_d": true,
		"-h": false, "": false, "a b": false, "x;y": false, strings.Repeat("a", 65): false, ".": false,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetPathValue("account", v)
		w := httptest.NewRecorder()
		got, ok := mailAccountParam(w, r)
		if ok != want || (ok && got != v) || (!ok && w.Code != http.StatusBadRequest) {
			t.Errorf("mailAccountParam(%q) = %q, %v (code %d)", v, got, ok, w.Code)
		}
	}
}

// registerMailRoutes calls all three slice hooks; together they must register
// disjoint patterns (a duplicate would panic here).
func TestMailRouteHooks(t *testing.T) {
	newHarness(t).mailServer().registerMailRoutes(http.NewServeMux())
}

// overrides.Dir puts a slice's own fixture files ahead of the dev fixtures.
func TestOverridesDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "v-list-mail-domain.json"),
		[]byte(`[{"args":["admin","slice.example","json"],"stdout":"{\"slice.example\":{}}","exit":0}]`))
	h := newHarnessWith(t, overrides{Dir: dir})
	res, err := h.fixture.Run(context.Background(), "v-list-mail-domain", "admin", "slice.example", "json")
	if err != nil || res.Exit != 0 || !strings.Contains(string(res.Stdout), "slice.example") {
		t.Fatalf("slice entry not used: %+v, %v", res, err)
	}
	res, err = h.fixture.Run(context.Background(), "v-list-mail-domain", "admin", "example.com", "json")
	if err != nil || !strings.Contains(string(res.Stdout), "DKIM_SELECTOR") {
		t.Fatalf("dev entries lost: %+v, %v", res, err)
	}
}
