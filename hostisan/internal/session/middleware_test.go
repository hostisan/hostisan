package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

type fx struct {
	Args   []string `json:"args"`
	Stdout string   `json:"stdout"`
	Exit   int      `json:"exit"`
}

var baseConfig = map[string]string{
	"INACTIVE_SESSION_TIMEOUT": "60", "DISABLE_IP_CHECK": "no", "POLICY_USER_VIEW_SUSPENDED": "no",
	"POLICY_SYSTEM_PROTECTED_ADMIN": "no", "POLICY_CSRF_STRICTNESS": "1", "ROOT_USER": "admin",
}

var baseUsers = map[string]map[string]string{
	"admin": {"ROLE": "admin", "SUSPENDED": "no"},
	"bob":   {"ROLE": "user", "SUSPENDED": "no"},
}

// newManager builds a Manager over fixtures, with config and user overrides.
func newManager(t *testing.T, cfg map[string]string, users map[string]map[string]string) (*Manager, *runner.Fixture, *time.Time) {
	t.Helper()
	c := map[string]string{}
	for k, v := range baseConfig {
		c[k] = v
	}
	for k, v := range cfg {
		c[k] = v
	}
	if users == nil {
		users = baseUsers
	}
	cfgJSON, _ := json.Marshal(map[string]any{"config": c})
	files := map[string][]fx{
		"v-list-sys-config": {{Args: []string{"json"}, Stdout: string(cfgJSON)}},
		"v-log-user-logout": {{Args: []string{"*", "*"}}},
		"v-get-user-value":  {{Args: []string{"*", "MD5"}, Stdout: "$6$salt$hash\n"}},
	}
	for name, u := range users {
		b, _ := json.Marshal(map[string]map[string]string{name: u})
		files["v-list-user"] = append(files["v-list-user"], fx{Args: []string{name, "json"}, Stdout: string(b)})
	}
	files["v-list-user"] = append(files["v-list-user"], fx{Args: []string{"*", "json"}, Stdout: "Error: no such user\n", Exit: 3})
	dir := t.TempDir()
	for name, es := range files {
		b, _ := json.Marshal(es)
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := runner.LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	m := &Manager{Store: NewMemoryStore(), H: &hestia.Client{R: f}, R: f, Now: func() time.Time { return now }, SystemHostname: "server1.acme-hosting.net"}
	return m, f, &now
}

// signIn stores a signed-in session and returns its id.
func signIn(m *Manager, user, role string, lastActivity time.Time) *Session {
	s := &Session{ID: NewToken(), Token: NewToken(), User: user, UserContext: role, LastActivity: lastActivity, CombinedIP: "203.0.113.9"}
	m.Store.Save(s)
	return s
}

func request(method, sid string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/x", nil)
	r.RemoteAddr = "203.0.113.9:5000"
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: sid})
	}
	return r
}

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Error.Code
}

func logouts(f *runner.Fixture) [][]string {
	var out [][]string
	for _, c := range f.Calls() {
		if c.Cmd == "v-log-user-logout" {
			out = append(out, c.Args)
		}
	}
	return out
}

func TestLoadCreatesAnonymousSession(t *testing.T) {
	m, _, _ := newManager(t, nil, nil)
	var seen *Session
	w := serve(m.Load(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = FromContext(r.Context()) })), request("GET", ""))
	if seen == nil || seen.User != "" || len(seen.Token) != 32 {
		t.Fatalf("session = %+v", seen)
	}
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].Name != CookieName || !c[0].HttpOnly || !c[0].Secure || c[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", c)
	}
	if _, stored := m.Store.Get(seen.ID); stored {
		t.Fatal("anonymous session was stored; visits without a cookie must not use memory")
	}
}

func TestLoadAnonymousTokenFollowsCookie(t *testing.T) {
	m, _, _ := newManager(t, nil, nil)
	var seen *Session
	h := m.Load(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = FromContext(r.Context()) }))
	w := serve(h, request("GET", ""))
	id := w.Result().Cookies()[0].Value
	first := seen.Token
	if serve(h, request("GET", id)); seen.ID != id || seen.Token != first {
		t.Fatalf("same cookie gave session %q token %q, want %q %q", seen.ID, seen.Token, id, first)
	}
	if serve(h, request("GET", NewToken())); seen.Token == first {
		t.Fatal("another cookie got the same CSRF token")
	}
	if w := serve(h, request("GET", "not-a-session-id")); len(w.Result().Cookies()) != 1 || seen.ID == "not-a-session-id" {
		t.Fatal("a malformed cookie was used as a session id")
	}
}

func TestLoadManyVisitorsUseNoMemory(t *testing.T) {
	m, _, _ := newManager(t, nil, nil)
	h := m.Load(ok)
	for range 1000 {
		serve(h, request("GET", ""))
	}
	if n := len(m.Store.(*MemoryStore).sessions); n != 0 {
		t.Fatalf("%d sessions stored for anonymous visits", n)
	}
}

func TestSweepEndsIdleSessionsAndForgetsPasswords(t *testing.T) {
	m, _, now := newManager(t, nil, nil) // INACTIVE_SESSION_TIMEOUT 60
	idle := signIn(m, "bob", "user", now.Add(-61*time.Minute))
	active := signIn(m, "admin", "admin", now.Add(-5*time.Minute))
	m.Store.Save(&Session{ID: "pending-old", Pending: &PendingLogin{Username: "bob", Password: "pw", Expires: now.Add(-time.Second)}})
	m.Store.Save(&Session{ID: "pending-new", Pending: &PendingLogin{Username: "bob", Password: "pw", Expires: now.Add(time.Minute)}})
	m.Sweep(context.Background())
	for id, want := range map[string]bool{idle.ID: false, active.ID: true, "pending-old": false, "pending-new": true} {
		if _, got := m.Store.Get(id); got != want {
			t.Errorf("session %s kept = %v, want %v", id, got, want)
		}
	}
}

func TestRequireAuthAnonymous(t *testing.T) {
	m, _, _ := newManager(t, nil, nil)
	w := serve(m.Load(m.RequireAuth(ok)), request("GET", ""))
	if w.Code != 401 || errCode(t, w) != "session_expired" {
		t.Fatalf("got %d %q", w.Code, errCode(t, w))
	}
}

func TestRequireAuthPasses(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "user", now.Add(-10*time.Minute))
	w := serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID))
	if w.Code != 204 {
		t.Fatalf("status %d %s", w.Code, w.Body)
	}
	got, _ := m.Store.Get(s.ID)
	if !got.LastActivity.Equal(*now) {
		t.Fatalf("LastActivity = %v, want %v", got.LastActivity, *now)
	}
}

func expectDestroyed(t *testing.T, m *Manager, w *httptest.ResponseRecorder, id, code string) {
	t.Helper()
	if w.Code != 401 || errCode(t, w) != code {
		t.Fatalf("got %d %q, want 401 %q", w.Code, errCode(t, w), code)
	}
	if _, ok := m.Store.Get(id); ok {
		t.Fatal("session was not destroyed")
	}
}

func TestRequireAuthIPChanged(t *testing.T) {
	m, f, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "user", *now)
	r := request("GET", s.ID)
	r.RemoteAddr = "198.51.100.1:5000"
	expectDestroyed(t, m, serve(m.Load(m.RequireAuth(ok)), r), s.ID, "session_expired")
	if got := logouts(f); !reflect.DeepEqual(got, [][]string{{"bob", s.Token}}) {
		t.Fatalf("logout log = %v", got)
	}
}

func TestRequireAuthIPCheckDisabled(t *testing.T) {
	m, _, now := newManager(t, map[string]string{"DISABLE_IP_CHECK": "yes"}, nil)
	s := signIn(m, "bob", "user", *now)
	r := request("GET", s.ID)
	r.RemoteAddr = "198.51.100.1:5000"
	if w := serve(m.Load(m.RequireAuth(ok)), r); w.Code != 204 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestRequireAuthUserDeleted(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "ghost", "user", *now)
	expectDestroyed(t, m, serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID)), s.ID, "session_expired")
}

func TestRequireAuthAdminDemoted(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "admin", *now) // signed in as admin, now a plain user
	expectDestroyed(t, m, serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID)), s.ID, "session_expired")
}

func TestRequireAuthSuspended(t *testing.T) {
	m, _, now := newManager(t, nil, map[string]map[string]string{"bob": {"ROLE": "user", "SUSPENDED": "yes"}})
	s := signIn(m, "bob", "user", *now)
	w := serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID))
	expectDestroyed(t, m, w, s.ID, "logged_out")
	var body struct{ Error struct{ Message string } }
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error.Message != "You are logged out, please log in again." {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

func TestRequireAuthSuspendedMayView(t *testing.T) {
	m, _, now := newManager(t, map[string]string{"POLICY_USER_VIEW_SUSPENDED": "yes"}, map[string]map[string]string{"bob": {"ROLE": "user", "SUSPENDED": "yes"}})
	s := signIn(m, "bob", "user", *now)
	if w := serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID)); w.Code != 204 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestRequireAuthNoTimeoutConfigured(t *testing.T) {
	m, _, now := newManager(t, map[string]string{"INACTIVE_SESSION_TIMEOUT": ""}, nil)
	s := signIn(m, "bob", "user", *now)
	expectDestroyed(t, m, serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID)), s.ID, "session_expired")
}

func TestRequireAuthTimedOut(t *testing.T) {
	m, f, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "user", now.Add(-61*time.Minute))
	expectDestroyed(t, m, serve(m.Load(m.RequireAuth(ok)), request("GET", s.ID)), s.ID, "session_expired")
	if got := logouts(f); !reflect.DeepEqual(got, [][]string{{"bob", s.Token}}) {
		t.Fatalf("logout log = %v", got)
	}
}

func TestEffective(t *testing.T) {
	m := &Manager{}
	if got := m.Effective(&Session{User: "admin", UserContext: "admin", Look: "bob"}); got != "bob" {
		t.Fatalf("admin looking at bob: %q", got)
	}
	if got := m.Effective(&Session{User: "bob", UserContext: "user", Look: "admin"}); got != "bob" {
		t.Fatalf("a user can never look: %q", got)
	}
}

func TestReadOnly(t *testing.T) {
	cfg := hestia.SysConfig{"POLICY_USER_VIEW_SUSPENDED": "yes", "POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}
	if !ReadOnly(&Session{UserContext: "user"}, hestia.User{"SUSPENDED": "yes"}, cfg) {
		t.Error("suspended user viewing their account should be read-only")
	}
	if !ReadOnly(&Session{UserContext: "admin", Look: "admin"}, hestia.User{}, cfg) {
		t.Error("admin looking at the protected root user should be read-only")
	}
	if ReadOnly(&Session{UserContext: "admin", Look: "bob"}, hestia.User{}, cfg) {
		t.Error("admin looking at bob should not be read-only")
	}
	if ReadOnly(&Session{UserContext: "user"}, hestia.User{"SUSPENDED": "no"}, cfg) {
		t.Error("active user should not be read-only")
	}
}

func TestRequireAdmin(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	user := signIn(m, "bob", "user", *now)
	if w := serve(m.Load(m.RequireAuth(m.RequireAdmin(ok))), request("GET", user.ID)); w.Code != 403 || errCode(t, w) != "forbidden" {
		t.Fatalf("user: %d %q", w.Code, errCode(t, w))
	}
	admin := signIn(m, "admin", "admin", *now)
	if w := serve(m.Load(m.RequireAuth(m.RequireAdmin(ok))), request("GET", admin.ID)); w.Code != 204 {
		t.Fatalf("admin: %d", w.Code)
	}
}

func TestRequireWritable(t *testing.T) {
	m, _, now := newManager(t, map[string]string{"POLICY_USER_VIEW_SUSPENDED": "yes"}, map[string]map[string]string{"bob": {"ROLE": "user", "SUSPENDED": "yes"}})
	s := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireAuth(m.RequireWritable(ok)))
	if w := serve(h, request("GET", s.ID)); w.Code != 204 {
		t.Fatalf("GET: %d", w.Code)
	}
	if w := serve(h, request("POST", s.ID)); w.Code != 403 || errCode(t, w) != "read_only" {
		t.Fatalf("POST: %d %q", w.Code, errCode(t, w))
	}
}

func TestRequireCSRF(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireCSRF(ok))
	if w := serve(h, request("GET", s.ID)); w.Code != 204 {
		t.Fatalf("GET without token: %d", w.Code)
	}
	if w := serve(h, request("POST", s.ID)); w.Code != 400 || errCode(t, w) != "csrf" {
		t.Fatalf("POST without token: %d %q", w.Code, errCode(t, w))
	}
	r := request("POST", s.ID)
	r.Header.Set("X-CSRF-Token", s.Token)
	if w := serve(h, r); w.Code != 204 {
		t.Fatalf("POST with token: %d", w.Code)
	}
	r = request("POST", s.ID)
	r.Header.Set("X-CSRF-Token", s.Token)
	r.Header.Set("Origin", "https://evil.example")
	if w := serve(h, r); w.Code != 400 {
		t.Fatalf("POST from another origin at strictness 1: %d", w.Code)
	}
}

func TestParallelRequestsOnOneSession(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireAuth(ok))
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := serve(h, request("GET", s.ID)); w.Code != 204 {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if _, ok := m.Store.Get(s.ID); !ok {
		t.Fatal("session lost under parallel requests")
	}
}

// between runs f on the stored session after Load has read its copy, the way a
// parallel request would.
func between(m *Manager, f func(id string), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f(FromContext(r.Context()).ID)
		next.ServeHTTP(w, r)
	})
}

func TestRequireAuthDoesNotUndoLogout(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "bob", "user", *now)
	h := m.Load(between(m, m.Store.Delete, m.RequireAuth(ok)))
	w := serve(h, request("GET", s.ID))
	if w.Code != 401 {
		t.Fatalf("status %d after the session was logged out mid-request", w.Code)
	}
	if _, ok := m.Store.Get(s.ID); ok {
		t.Fatal("a request that started before logout brought the session back")
	}
}

func TestRequireAuthKeepsParallelChanges(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	s := signIn(m, "admin", "admin", *now)
	lookAtBob := func(id string) {
		cur, _ := m.Store.Get(id)
		cur.Look = "bob"
		m.Store.Save(cur)
	}
	serve(m.Load(between(m, lookAtBob, m.RequireAuth(ok))), request("GET", s.ID))
	if got, _ := m.Store.Get(s.ID); got.Look != "bob" {
		t.Fatal("a parallel request's log-in-as was overwritten by a stale session copy")
	}
}

// shadowRunner answers `v-get-user-value USER MD5` (Hestia's copy of the user's
// /etc/shadow hash) from md5, and passes every other command on.
type shadowRunner struct {
	runner.Runner
	mu    sync.Mutex
	md5   map[string]string
	reads int
}

func (r *shadowRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == "v-get-user-value" && len(args) == 2 && args[1] == "MD5" {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.reads++
		return runner.Result{Stdout: []byte(r.md5[args[0]] + "\n")}, nil
	}
	return r.Runner.Run(ctx, cmd, args...)
}

func (r *shadowRunner) set(user, md5 string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.md5[user] = md5
}

func withShadow(m *Manager) *shadowRunner {
	sr := &shadowRunner{Runner: m.R, md5: map[string]string{"bob": "$6$a$one", "admin": "$6$b$adm"}}
	m.R = sr
	m.H = &hestia.Client{R: sr}
	return sr
}

// A password changed outside the panel (v-change-user-password over SSH or the
// legacy API) left every session of that user signed in: only DeleteUser, run by
// the panel's own password forms, ended them. Validate now compares the session's
// mark of the password with the current one.
func TestRequireAuthPasswordChangedOutside(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	sr := withShadow(m)
	s := signIn(m, "bob", "user", *now)
	other := signIn(m, "admin", "admin", *now)
	h := m.Load(m.RequireAuth(ok))
	if w := serve(h, request("GET", s.ID)); w.Code != 204 {
		t.Fatalf("status %d", w.Code)
	}
	if cur, _ := m.Store.Get(s.ID); cur.PasswordMark == "" || strings.Contains(cur.PasswordMark, "one") {
		t.Fatalf("mark = %q: want a hash of the stored password hash", cur.PasswordMark)
	}
	sr.set("bob", "$6$c$two")
	*now = now.Add(passwordCheckEvery + time.Second)
	w := serve(h, request("GET", s.ID))
	expectDestroyed(t, m, w, s.ID, "logged_out")
	// another user's session is untouched
	if w := serve(h, request("GET", other.ID)); w.Code != 204 {
		t.Fatalf("admin: status %d", w.Code)
	}
	// a session that signs in with the new password stays signed in, even while
	// the old mark is still cached
	fresh := signIn(m, "bob", "user", *now)
	if w := serve(h, request("GET", fresh.ID)); w.Code != 204 {
		t.Fatalf("new login: status %d", w.Code)
	}
	*now = now.Add(passwordCheckEvery + time.Second)
	if w := serve(h, request("GET", fresh.ID)); w.Code != 204 {
		t.Fatalf("new login later: status %d", w.Code)
	}
}

// The mark is read at most once per passwordCheckEvery and user, not per request.
func TestRequireAuthPasswordMarkIsCached(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	sr := withShadow(m)
	s := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireAuth(ok))
	for range 5 {
		if w := serve(h, request("GET", s.ID)); w.Code != 204 {
			t.Fatalf("status %d", w.Code)
		}
	}
	if sr.reads != 1 {
		t.Fatalf("%d password reads for 5 requests, want 1", sr.reads)
	}
	*now = now.Add(passwordCheckEvery + time.Second)
	serve(h, request("GET", s.ID))
	if sr.reads != 2 {
		t.Fatalf("%d password reads after the cache expired, want 2", sr.reads)
	}
}

// The panel's own password change keeps the session that made it (PasswordChanged)
// and ends the others at once.
func TestPasswordChangedKeepsTheChanger(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	sr := withShadow(m)
	me := signIn(m, "bob", "user", *now)
	elsewhere := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireAuth(ok))
	serve(h, request("GET", me.ID))
	serve(h, request("GET", elsewhere.ID))
	sr.set("bob", "$6$d$three")
	m.PasswordChanged("bob", me.ID)
	if _, ok := m.Store.Get(elsewhere.ID); ok {
		t.Fatal("the other session survived the password change")
	}
	for range 2 {
		if w := serve(h, request("GET", me.ID)); w.Code != 204 {
			t.Fatalf("the changer was signed out: status %d", w.Code)
		}
		*now = now.Add(passwordCheckEvery + time.Second)
	}
}

// A request of the changer that loaded its session before PasswordChanged reset the
// mark is not signed out by the new one.
func TestPasswordChangedRaceKeepsTheChanger(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	sr := withShadow(m)
	me := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireAuth(ok))
	serve(h, request("GET", me.ID))
	loaded, _ := m.Store.Get(me.ID) // the in-flight request's copy
	oldMark := loaded.PasswordMark
	sr.set("bob", "$6$e$four")
	m.PasswordChanged("bob", me.ID)
	code, _, err := m.Validate(request("GET", me.ID), loaded)
	if err != nil || code != "" {
		t.Fatalf("Validate = %q, %v", code, err)
	}
	if cur, ok := m.Store.Get(me.ID); !ok || cur.PasswordMark == "" || cur.PasswordMark == oldMark {
		t.Fatalf("session after the race = %+v, %v", cur, ok)
	}
}

// rc8 N1: a browser signed out by the panel's own password change elsewhere came
// back anonymous and landed on the login page with no reason. Its requests now
// say logged_out with the same message as an outside change, until it signs in
// again or signedOutKeep passes.
func TestPasswordChangedTellsTheOthersWhy(t *testing.T) {
	m, _, now := newManager(t, nil, nil)
	sr := withShadow(m)
	me := signIn(m, "bob", "user", *now)
	elsewhere := signIn(m, "bob", "user", *now)
	h := m.Load(m.RequireAuth(ok))
	serve(h, request("GET", elsewhere.ID))
	sr.set("bob", "$6$f$five")
	m.PasswordChanged("bob", me.ID)
	// every request of that browser, also parallel ones, gets the reason
	for range 2 {
		w := serve(h, request("GET", elsewhere.ID))
		if w.Code != 401 || errCode(t, w) != "logged_out" || !strings.Contains(w.Body.String(), msgLoggedOut) {
			t.Fatalf("ended session: %d %s", w.Code, w.Body)
		}
	}
	if got := m.SignedOutReason(elsewhere.ID); got != msgLoggedOut {
		t.Fatalf("SignedOutReason = %q", got)
	}
	// an unknown cookie still just says session_expired
	w := serve(h, request("GET", NewToken()))
	if w.Code != 401 || errCode(t, w) != "session_expired" {
		t.Fatalf("unknown cookie: %d %s", w.Code, w.Body)
	}
	if got := m.SignedOutReason(me.ID); got != "" {
		t.Fatalf("the changer has a sign-out reason %q", got)
	}
	// the sweep forgets it after signedOutKeep
	*now = now.Add(signedOutKeep + time.Minute)
	m.Sweep(context.Background())
	if got := m.SignedOutReason(elsewhere.ID); got != "" {
		t.Fatalf("SignedOutReason after signedOutKeep = %q", got)
	}
}
