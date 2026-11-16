package httpapi

import (
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/session"
)

// persistedHarness is a harness whose sessions are kept in path, as serve keeps them
// in <state-dir>/sessions.json. Each call is a new panel start on the same file.
func persistedHarness(t *testing.T, path string) *harness {
	t.Helper()
	h := newHarness(t)
	if err := h.store.Persist(path, time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.store.Close)
	return h
}

// withCookie is a client of h that sends cookie id, as a browser that kept it over
// the restart.
func (h *harness) withCookie(id string) *harness {
	h.t.Helper()
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(mustURL(h.srv.URL), []*http.Cookie{{Name: session.CookieName, Value: id, Path: "/"}})
	c := *h
	c.client = &http.Client{Jar: jar}
	c.token = ""
	return &c
}

// A panel restart keeps a signed-in browser signed in: its cookie passes
// RequireAuth on the new server, bootstrap hands back the same CSRF token and that
// token is accepted. A browser that logged out before the restart stays out, and so
// does one that logs out after it, across the next restart.
func TestSessionsSurviveAPanelRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	first := persistedHarness(t, path)
	first.loginAdmin()
	token := first.bootstrap().CSRFToken
	cookie := first.sessionID()
	if cookie == "" || token == "" {
		t.Fatalf("no session after login: cookie %q token %q", cookie, token)
	}
	gone := first.withCookie("")
	gone.loginAdmin()
	goneCookie := gone.sessionID()
	var r statusReply
	if code := gone.call("POST", "/api/v1/auth/logout", nil, &r); code != 200 || r.Status != "logged_out" {
		t.Fatalf("logout: %d %+v", code, r)
	}
	first.store.Close() // the panel stops

	second := persistedHarness(t, path)
	kept := second.withCookie(cookie)
	b := kept.bootstrap()
	if !b.Authenticated || b.User == nil || b.CSRFToken != token {
		t.Fatalf("bootstrap after restart = authenticated %v, token %q (want %q)", b.Authenticated, b.CSRFToken, token)
	}
	if code := kept.call("GET", "/api/v1/dns", nil, nil); code != 200 {
		t.Fatalf("RequireAuth after restart: %d", code)
	}
	out := second.withCookie(goneCookie)
	if b := out.bootstrap(); b.Authenticated {
		t.Fatal("a logged-out cookie is signed in after the restart")
	}
	if code := out.call("GET", "/api/v1/dns", nil, nil); code != 401 {
		t.Fatalf("logged-out cookie after restart: %d, want 401", code)
	}
	// the restored token passes the CSRF check: log out with it
	if code := kept.call("POST", "/api/v1/auth/logout", nil, &r); code != 200 || r.Status != "logged_out" {
		t.Fatalf("logout after restart: %d %+v", code, r)
	}
	// no Close: a logout is on disk at once

	third := persistedHarness(t, path)
	for _, id := range []string{cookie, goneCookie} {
		c := third.withCookie(id)
		if code := c.call("GET", "/api/v1/dns", nil, nil); code != 401 {
			t.Fatalf("logged-out cookie after a second restart: %d, want 401", code)
		}
	}
}
