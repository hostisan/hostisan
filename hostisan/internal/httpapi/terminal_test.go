package httpapi

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
	"github.com/hostisan/hostisan/internal/terminal"
)

// testClock is a settable Deps.Now.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// cfgSwitch patches v-list-sys-config's output while a test runs (a gate turned off
// after a token was issued).
type cfgSwitch struct {
	runner.Runner
	mu    sync.Mutex
	patch map[string]string
	users map[string]map[string]string
}

func (c *cfgSwitch) set(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.patch == nil {
		c.patch = map[string]string{}
	}
	c.patch[k] = v
}

// setUser patches one field of user's v-list-user record.
func (c *cfgSwitch) setUser(user, k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.users == nil {
		c.users = map[string]map[string]string{}
	}
	if c.users[user] == nil {
		c.users[user] = map[string]string{}
	}
	c.users[user][k] = v
}

func (c *cfgSwitch) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	res, err := c.Runner.Run(ctx, cmd, args...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && cmd == "v-list-user" && len(args) > 0 && c.users[args[0]] != nil {
		var doc map[string]map[string]string
		if err := json.Unmarshal(res.Stdout, &doc); err != nil {
			return res, err
		}
		for k, v := range c.users[args[0]] {
			doc[args[0]][k] = v
		}
		res.Stdout, _ = json.Marshal(doc)
		return res, nil
	}
	if err != nil || cmd != "v-list-sys-config" || len(c.patch) == 0 {
		return res, err
	}
	var doc struct {
		Config map[string]string `json:"config"`
	}
	if err := json.Unmarshal(res.Stdout, &doc); err != nil {
		return res, err
	}
	for k, v := range c.patch {
		doc.Config[k] = v
	}
	res.Stdout, _ = json.Marshal(doc)
	return res, nil
}

// fakeTerm is the root terminal process: a unix listener speaking terminal frames.
type fakeTerm struct {
	t     *testing.T
	path  string
	conns chan *fakeTermConn
}

type fakeTermConn struct {
	net.Conn
	hello  terminal.Hello
	frames chan termFrame // every frame after the hello; closed at EOF
}

type termFrame struct {
	typ     byte
	payload []byte
}

func newFakeTerm(t *testing.T) *fakeTerm {
	t.Helper()
	// Unix socket paths are limited to 108 bytes; t.TempDir can be longer.
	dir, err := os.MkdirTemp("", "hterm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "t.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeTerm{t: t, path: path, conns: make(chan *fakeTermConn, 8)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
			fc := &fakeTermConn{Conn: c, frames: make(chan termFrame, 64)}
			typ, p, err := terminal.ReadFrame(c)
			if err != nil || typ != terminal.FrameHello {
				fc.hello.User = "<no hello>"
			} else {
				_ = json.Unmarshal(p, &fc.hello)
			}
			go func() {
				defer close(fc.frames)
				for {
					typ, p, err := terminal.ReadFrame(c)
					if err != nil {
						return
					}
					fc.frames <- termFrame{typ, p}
				}
			}()
			f.conns <- fc
		}
	}()
	return f
}

func newJar() (http.CookieJar, error) { return cookiejar.New(nil) }

// accept waits for the panel's connection.
func (f *fakeTerm) accept() *fakeTermConn {
	f.t.Helper()
	select {
	case c := <-f.conns:
		return c
	case <-time.After(5 * time.Second):
		f.t.Fatal("the terminal got no connection")
		return nil
	}
}

// none checks that the panel never dialed the terminal.
func (f *fakeTerm) none() {
	f.t.Helper()
	select {
	case <-f.conns:
		f.t.Fatal("the terminal got a connection")
	case <-time.After(100 * time.Millisecond):
	}
}

func (c *fakeTermConn) next(t *testing.T) termFrame {
	t.Helper()
	select {
	case fr, ok := <-c.frames:
		if !ok {
			t.Fatal("terminal connection closed")
		}
		return fr
	case <-time.After(5 * time.Second):
		t.Fatal("no frame reached the terminal")
		return termFrame{}
	}
}

// closed waits until the panel closed its side.
func (c *fakeTermConn) closed(t *testing.T) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-c.frames:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the panel kept the terminal connection open")
		}
	}
}

// termHarness is a harness whose server trusts X-Real-IP (as behind hestia-nginx),
// runs on a settable clock, dials a fake terminal and can switch config keys.
type termHarness struct {
	*harness
	clock *testClock
	term  *fakeTerm
	cfg   *cfgSwitch
}

func newTermHarness(t *testing.T, o overrides, serverTimeout time.Duration) *termHarness {
	t.Helper()
	h := newHarnessWith(t, o)
	th := &termHarness{harness: h, clock: &testClock{t: time.Now()}, term: newFakeTerm(t)}
	th.cfg = &cfgSwitch{Runner: h.deps.H.R}
	h.deps.H.R = th.cfg
	h.deps.Now = th.clock.Now
	h.deps.TerminalSocket = th.term.path
	api, handler := newServer(h.deps)
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context { return session.FromProxy(ctx) }
	if serverTimeout > 0 {
		srv.Config.ReadTimeout, srv.Config.WriteTimeout = serverTimeout, serverTimeout
	}
	srv.Start()
	t.Cleanup(srv.Close)
	h.srv, h.api = srv, api
	return th
}

func (h *termHarness) issueToken() TerminalToken {
	h.t.Helper()
	var tok TerminalToken
	if code := h.call("POST", "/api/v1/terminal/token", nil, &tok); code != 200 {
		h.t.Fatalf("token: %d", code)
	}
	return tok
}

// dial opens the websocket with the session cookie and a same-origin Origin.
func (h *termHarness) dial(headers map[string]string) (*websocket.Conn, *http.Response, error) {
	hdr := http.Header{"Origin": {h.srv.URL}}
	for k, v := range headers {
		hdr.Set(k, v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.srv.URL, "http")+"/api/v1/terminal/socket",
		&websocket.DialOptions{HTTPClient: h.client, HTTPHeader: hdr})
}

func (h *termHarness) mustDial(headers map[string]string) *websocket.Conn {
	h.t.Helper()
	c, _, err := h.dial(headers)
	if err != nil {
		h.t.Fatalf("dial: %v", err)
	}
	h.t.Cleanup(func() { c.CloseNow() })
	return c
}

func sendJSON(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func sendAuth(t *testing.T, c *websocket.Conn, token string) {
	t.Helper()
	sendJSON(t, c, map[string]any{"type": "auth", "token": token, "cols": 100, "rows": 30})
}

// closeOf reads until the server closes and returns the close frame.
func closeOf(t *testing.T, c *websocket.Conn) websocket.CloseError {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		if !errors.As(err, &ce) {
			t.Fatalf("read ended without a close frame: %v", err)
		}
		return ce
	}
}

func wantClose(t *testing.T, c *websocket.Conn, reason string) {
	t.Helper()
	if ce := closeOf(t, c); ce.Code != websocket.StatusNormalClosure || ce.Reason != reason {
		t.Fatalf("close = %d %q, want 1000 %q", ce.Code, ce.Reason, reason)
	}
}

func readBinary(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, p, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary {
		t.Fatalf("read: %v %v", typ, err)
	}
	return string(p)
}

// openRelay signs in as admin and opens a working terminal.
func (h *termHarness) openRelay() (*websocket.Conn, *fakeTermConn) {
	h.t.Helper()
	h.loginAdmin()
	tok := h.issueToken()
	c := h.mustDial(nil)
	sendAuth(h.t, c, tok.Token)
	return c, h.term.accept()
}

func suspReadOnlyOverrides(t *testing.T) overrides {
	return overrides{
		Config: map[string]string{"POLICY_USER_VIEW_SUSPENDED": "yes"},
		Fixtures: map[string][]fixtureEntry{
			"v-check-user-hash": {{Args: []string{"*", "*", "*"}}},
			"v-get-user-salt": {{Args: []string{"susp", "*", "json"},
				Stdout: `{"susp": {"METHOD": "yescrypt", "SALT": "j9T$abcdefghijklmnop", "TIME": "", "DATE": ""}}`}},
			"v-list-user": hookUserFixtures(t, map[string]map[string]string{"susp": {"SHELL": "bash"}}),
		},
	}
}

func (h *harness) loginSusp() {
	h.t.Helper()
	h.bootstrap()
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "susp", Password: "pw"}, nil); code != 200 {
		h.t.Fatalf("susp login: %d", code)
	}
}

func (h *harness) loginOpsAsProtectedAdmin() {
	h.t.Helper()
	h.loginOps()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil); code != 200 {
		h.t.Fatalf("login-as admin: %d", code)
	}
}

func TestTermTokenGates(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.loginAdmin()
	var tok TerminalToken
	if code := h.call("POST", "/api/v1/terminal/token", nil, &tok); code != 200 {
		t.Fatalf("admin: %d", code)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(tok.Token) || tok.Token == h.harness.token {
		t.Fatalf("token = %q", tok.Token)
	}
	exp, err := time.Parse(time.RFC3339, tok.Expires)
	if err != nil || exp.Sub(h.clock.Now()) > 30*time.Second || exp.Sub(h.clock.Now()) < 29*time.Second {
		t.Fatalf("expires = %q (%v), now %v", tok.Expires, err, h.clock.Now())
	}

	cases := []struct {
		name          string
		o             overrides
		login         func(h *harness)
		status        int
		code, message string
	}{
		{"bob (nologin)", overrides{}, (*harness).loginBob, 403, "no_shell", "You have no shell access."},
		{"WEB_TERMINAL=false", overrides{Config: map[string]string{"WEB_TERMINAL": "false"}}, (*harness).loginAdmin, 404, "not_found", ""},
		{"protected admin", overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}},
			(*harness).loginOpsAsProtectedAdmin, 403, "forbidden", ""},
		{"read-only susp", suspReadOnlyOverrides(t), (*harness).loginSusp, 403, "read_only", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTermHarness(t, c.o, 0)
			c.login(h.harness)
			var e APIError
			if code := h.call("POST", "/api/v1/terminal/token", nil, &e); code != c.status || e.Error.Code != c.code || e.Error.Message != c.message {
				t.Fatalf("got %d %+v, want %d %s %q", code, e, c.status, c.code, c.message)
			}
		})
	}

	t.Run("missing CSRF", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.loginAdmin()
		h.harness.token = ""
		var e APIError
		if code := h.call("POST", "/api/v1/terminal/token", nil, &e); code != 400 || e.Error.Code != "csrf" {
			t.Fatalf("got %d %+v", code, e)
		}
	})
}

func TestTermSocketRelay(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	c, tc := h.openRelay()
	if tc.hello != (terminal.Hello{User: "admin", Cols: 100, Rows: 30}) {
		t.Fatalf("hello = %+v", tc.hello)
	}
	ctx := context.Background()
	if err := c.Write(ctx, websocket.MessageBinary, []byte("ls -la\r")); err != nil {
		t.Fatal(err)
	}
	if fr := tc.next(t); fr.typ != terminal.FrameData || string(fr.payload) != "ls -la\r" {
		t.Fatalf("frame = %c %q", fr.typ, fr.payload)
	}
	if err := terminal.WriteFrame(tc, terminal.FrameData, []byte("total 0\r\n")); err != nil {
		t.Fatal(err)
	}
	if got := readBinary(t, c); got != "total 0\r\n" {
		t.Fatalf("client got %q", got)
	}
	sendJSON(t, c, map[string]any{"type": "resize", "cols": 120, "rows": 40})
	if fr := tc.next(t); fr.typ != terminal.FrameResize || string(fr.payload) != string(terminal.ResizePayload(120, 40)) {
		t.Fatalf("frame = %c %v", fr.typ, fr.payload)
	}
	if err := terminal.WriteFrame(tc, terminal.FrameClose, []byte("You have no shell access.")); err != nil {
		t.Fatal(err)
	}
	wantClose(t, c, "You have no shell access.")
}

// The terminal process's own refusals reach the browser verbatim; an unknown reason
// is dropped.
func TestTermRelayedCloseReasons(t *testing.T) {
	for _, c := range []struct{ sent, want string }{
		{terminal.ReasonTooMany, "Too many terminals are open. Close one and try again."},
		{"You have no shell access.", "You have no shell access."},
		{"some internal error", ""},
	} {
		h := newTermHarness(t, overrides{}, 0)
		ws, tc := h.openRelay()
		if err := terminal.WriteFrame(tc, terminal.FrameClose, []byte(c.sent)); err != nil {
			t.Fatal(err)
		}
		wantClose(t, ws, c.want)
	}
}

func TestTermDefaultSocket(t *testing.T) {
	// The root terminal process listens here (cmd/hostisan defaultTerminalSocket).
	if DefaultTerminalSocket != "/run/hestia-web-terminal/terminal.sock" {
		t.Fatal(DefaultTerminalSocket)
	}
	if got := (&server{}).terminalSocket(); got != DefaultTerminalSocket {
		t.Fatal(got)
	}
}

// Hestia's terminal looked users up by the look user (WT14): an admin viewing as a
// user gets that user's shell.
func TestTermLookUser(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "carol"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	c := h.mustDial(nil)
	sendAuth(t, c, h.issueToken().Token)
	if tc := h.term.accept(); tc.hello.User != "carol" {
		t.Fatalf("hello = %+v", tc.hello)
	}
}

func TestTermTokenSingleUse(t *testing.T) {
	refused := func(t *testing.T, h *termHarness, c *websocket.Conn) {
		t.Helper()
		wantClose(t, c, "You are not authenticated.")
		h.term.none()
	}

	t.Run("second use", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.loginAdmin()
		tok := h.issueToken()
		c := h.mustDial(nil)
		sendAuth(t, c, tok.Token)
		h.term.accept()
		c2 := h.mustDial(nil)
		sendAuth(t, c2, tok.Token)
		refused(t, h, c2)
	})
	t.Run("expired", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.loginAdmin()
		tok := h.issueToken()
		h.clock.Add(31 * time.Second)
		c := h.mustDial(nil)
		sendAuth(t, c, tok.Token)
		refused(t, h, c)
	})
	t.Run("wrong token", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.loginAdmin()
		h.issueToken()
		c := h.mustDial(nil)
		sendAuth(t, c, strings.Repeat("0", 32))
		refused(t, h, c)
	})
	t.Run("another session's token", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.loginAdmin()
		tok := h.issueToken()
		// A second browser signs in as admin and tries the first one's token.
		other := *h.harness
		jar, _ := newJar()
		other.client = &http.Client{Jar: jar}
		other.token = ""
		other.loginAdmin()
		h2 := &termHarness{harness: &other, clock: h.clock, term: h.term, cfg: h.cfg}
		c := h2.mustDial(nil)
		sendAuth(t, c, tok.Token)
		refused(t, h, c)
	})
	t.Run("another IP", func(t *testing.T) {
		// The IP check is off, so the session survives the new address; the token
		// still refuses it.
		h := newTermHarness(t, overrides{Config: map[string]string{"DISABLE_IP_CHECK": "yes"}}, 0)
		h.loginAdmin()
		tok := h.issueToken()
		c := h.mustDial(map[string]string{"X-Real-IP": "203.0.113.9"})
		sendAuth(t, c, tok.Token)
		refused(t, h, c)
	})
	t.Run("no auth message in time", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.api.termState().authWait.Store(int64(50 * time.Millisecond))
		h.loginAdmin()
		h.issueToken()
		c := h.mustDial(nil)
		refused(t, h, c)
	})
	t.Run("not an auth message", func(t *testing.T) {
		h := newTermHarness(t, overrides{}, 0)
		h.loginAdmin()
		tok := h.issueToken()
		c := h.mustDial(nil)
		if err := c.Write(context.Background(), websocket.MessageBinary, []byte(tok.Token)); err != nil {
			t.Fatal(err)
		}
		refused(t, h, c)
	})
}

func TestTermGatesAtConnect(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.loginAdmin()
	tok := h.issueToken()
	h.cfg.set("WEB_TERMINAL", "false")
	h.deps.H.InvalidateConfig()
	c := h.mustDial(nil)
	sendAuth(t, c, tok.Token)
	wantClose(t, c, "You are not authenticated.")
	h.term.none()
}

func TestTermSignedOutSocket(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.bootstrap()
	_, res, err := h.dial(nil)
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed out: %v %v", res, err)
	}
	h.term.none()
}

func TestTermClosedOnLogout(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	c, tc := h.openRelay()
	if code := h.call("POST", "/api/v1/auth/logout", nil, nil); code != 200 {
		t.Fatalf("logout: %d", code)
	}
	wantClose(t, c, "Your session has expired.")
	tc.closed(t)
}

func TestTermClosedOnLookChange(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	c, tc := h.openRelay()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	wantClose(t, c, "Your session has expired.")
	tc.closed(t)
}

func TestTermClosedOnSweep(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	c, tc := h.openRelay()
	h.store.Sweep(time.Now().Add(2*time.Hour), time.Hour)
	wantClose(t, c, "Your session has expired.")
	tc.closed(t)
}

func TestTermBadOrigin(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.loginAdmin()
	for _, origin := range []string{"https://evil.example", "", "null", strings.Replace(h.srv.URL, "127.0.0.1", "127.0.0.2", 1)} {
		hdr := map[string]string{"Origin": origin}
		_, res, err := h.dial(hdr)
		if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			t.Errorf("Origin %q: %v %v", origin, res, err)
		}
	}
	h.term.none()
}

func TestTermOutlivesServerTimeouts(t *testing.T) {
	h := newTermHarness(t, overrides{}, time.Second)
	c, tc := h.openRelay()
	time.Sleep(3 * time.Second)
	if err := c.Write(context.Background(), websocket.MessageBinary, []byte("echo hi\r")); err != nil {
		t.Fatal(err)
	}
	fr := tc.next(t)
	if err := terminal.WriteFrame(tc, terminal.FrameData, fr.payload); err != nil {
		t.Fatal(err)
	}
	if got := readBinary(t, c); got != "echo hi\r" {
		t.Fatalf("echo = %q", got)
	}
}

func TestTermActivityRefresh(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	c, tc := h.openRelay()
	sid := h.sessionID()
	last := func() time.Time {
		s, ok := h.store.Get(sid)
		if !ok {
			t.Fatal("session gone")
		}
		return s.LastActivity
	}
	typeKey := func() {
		t.Helper()
		if err := c.Write(context.Background(), websocket.MessageBinary, []byte("x")); err != nil {
			t.Fatal(err)
		}
		tc.next(t) // the relay handled it
	}
	before := last()
	typeKey() // within the first minute: no update
	if !last().Equal(before) {
		t.Fatal("typing at once updated LastActivity")
	}
	h.clock.Add(61 * time.Second)
	t1 := h.clock.Now()
	typeKey()
	if !last().Equal(t1) {
		t.Fatalf("after 61 s: LastActivity %v, want %v", last(), t1)
	}
	h.clock.Add(30 * time.Second)
	typeKey()
	typeKey()
	if !last().Equal(t1) {
		t.Fatalf("twice within a minute: LastActivity %v, want %v", last(), t1)
	}
	h.clock.Add(31 * time.Second)
	t2 := h.clock.Now()
	typeKey()
	if !last().Equal(t2) {
		t.Fatalf("a minute later: LastActivity %v, want %v", last(), t2)
	}
}

// Once a minute, typing also re-checks the account and the gates: a terminal opened
// before a suspension or a policy change doesn't outlive it.
func TestTermRecheckOnActivity(t *testing.T) {
	cases := []struct {
		name   string
		change func(h *termHarness)
		reason string
	}{
		{"suspended", func(h *termHarness) { h.cfg.setUser("admin", "SUSPENDED", "yes") }, "Your session has expired."},
		{"WEB_TERMINAL off", func(h *termHarness) { h.cfg.set("WEB_TERMINAL", "false") }, "The web terminal is not available"},
		{"shell taken away", func(h *termHarness) { h.cfg.setUser("admin", "SHELL", "nologin") }, "You have no shell access."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTermHarness(t, overrides{}, 0)
			ws, tc := h.openRelay()
			c.change(h)
			h.deps.H.InvalidateConfig()
			h.clock.Add(61 * time.Second)
			if err := ws.Write(context.Background(), websocket.MessageBinary, []byte("x")); err != nil {
				t.Fatal(err)
			}
			wantClose(t, ws, c.reason)
			tc.closed(t)
		})
	}

	// Nothing changed: the terminal stays open.
	h := newTermHarness(t, overrides{}, 0)
	ws, tc := h.openRelay()
	h.clock.Add(61 * time.Second)
	if err := ws.Write(context.Background(), websocket.MessageBinary, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if fr := tc.next(t); string(fr.payload) != "x" {
		t.Fatalf("frame %q", fr.payload)
	}
}

// A browser that stops reading can't hold the relay (and the shell) forever: a
// write to it times out and the terminal connection is closed.
func TestTermStalledBrowser(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.api.termState().writeWait.Store(int64(200 * time.Millisecond))
	_, tc := h.openRelay() // the client never reads again
	go func() {
		chunk := make([]byte, terminal.MaxPayload)
		for terminal.WriteFrame(tc, terminal.FrameData, chunk) == nil {
		}
	}()
	tc.closed(t)
}

// rawWS is a minimal websocket client that sees every frame the server writes, as a
// browser does (coder/websocket's client hides a second close frame; Chrome fails
// the socket on one with "Close received after close").
type rawWS struct {
	conn net.Conn
	r    *bufio.Reader
}

func (h *termHarness) rawDial() *rawWS {
	h.t.Helper()
	u, _ := url.Parse(h.srv.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { conn.Close() })
	var cookies []string
	for _, c := range h.client.Jar.Cookies(u) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	fmt.Fprintf(conn, "GET /api/v1/terminal/socket HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nCookie: %s\r\n"+
		"Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", u.Host, h.srv.URL, strings.Join(cookies, "; "))
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		h.t.Fatalf("upgrade: %v %v", resp, err)
	}
	return &rawWS{conn: conn, r: r}
}

// write sends one masked frame (the mask is zero, so the payload goes as is).
func (w *rawWS) write(t *testing.T, op byte, p []byte) {
	t.Helper()
	if len(p) > 125 {
		t.Fatal("rawWS: payload too long")
	}
	frame := append([]byte{0x80 | op, 0x80 | byte(len(p)), 0, 0, 0, 0}, p...)
	if _, err := w.conn.Write(frame); err != nil {
		t.Fatal(err)
	}
}

// read returns the next frame's opcode and payload; ok is false at EOF or timeout.
func (w *rawWS) read(timeout time.Duration) (op byte, p []byte, ok bool) {
	w.conn.SetReadDeadline(time.Now().Add(timeout))
	var hdr [2]byte
	if _, err := io.ReadFull(w.r, hdr[:]); err != nil {
		return 0, nil, false
	}
	n := int(hdr[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(w.r, ext[:]); err != nil {
			return 0, nil, false
		}
		n = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(w.r, ext[:]); err != nil {
			return 0, nil, false
		}
		n = int(binary.BigEndian.Uint64(ext[:]))
	}
	p = make([]byte, n)
	if _, err := io.ReadFull(w.r, p); err != nil {
		return 0, nil, false
	}
	return hdr[0] & 0x0f, p, true
}

// TestTermSingleCloseFrame: when the panel ends a terminal (here a logout), the
// browser answers the close frame at once while the relay is still reading; the
// server must not send a second close frame in reply, or Chrome reports a
// connection error instead of the reason.
func TestTermSingleCloseFrame(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.loginAdmin()
	tok := h.issueToken()
	ws := h.rawDial()
	auth, _ := json.Marshal(map[string]any{"type": "auth", "token": tok.Token, "cols": 80, "rows": 24})
	ws.write(t, 0x1, auth)
	tc := h.term.accept()
	ws.write(t, 0x2, []byte("x"))
	if fr := tc.next(t); string(fr.payload) != "x" { // the relay is running and reading from the browser
		t.Fatalf("frame = %c %q", fr.typ, fr.payload)
	}
	if code := h.call("POST", "/api/v1/auth/logout", nil, nil); code != 200 {
		t.Fatalf("logout: %d", code)
	}
	closes := 0
	for {
		op, p, ok := ws.read(3 * time.Second)
		if !ok {
			break
		}
		if op != 0x8 {
			continue
		}
		closes++
		if closes == 1 {
			if len(p) < 2 || string(p[2:]) != "Your session has expired." {
				t.Fatalf("close payload %q", p)
			}
			ws.write(t, 0x8, p) // echo it, as a browser does
		}
	}
	if closes != 1 {
		t.Fatalf("server sent %d close frames, want 1", closes)
	}
}

// One account can't hold every slot of the root terminal process: past the per-user
// cap the panel refuses before dialing, and a closed terminal frees its slot.
func TestTermPerUserCap(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	h.api.termState().maxPerUser.Store(2)
	first, firstTC := h.openRelay()
	h.mustDialAuth()
	h.term.accept()

	extra := h.mustDialAuth()
	wantClose(t, extra, terminal.ReasonTooMany)
	h.term.none()

	first.Close(websocket.StatusNormalClosure, "")
	firstTC.closed(t)
	deadline := time.Now().Add(5 * time.Second)
	for h.api.termState().openFor("admin") >= 2 {
		if time.Now().After(deadline) {
			t.Fatal("the closed terminal kept its slot")
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mustDialAuth()
	h.term.accept()
}

// mustDialAuth opens another terminal for the signed-in session.
func (h *termHarness) mustDialAuth() *websocket.Conn {
	h.t.Helper()
	tok := h.issueToken()
	c := h.mustDial(nil)
	sendAuth(h.t, c, tok.Token)
	return c
}

// An idle terminal is re-checked on a timer too (C3 D1): a suspension or a gate
// change ends it without anyone typing, and the check alone doesn't count as
// activity.
func TestTermRecheckWhileIdle(t *testing.T) {
	cases := []struct {
		name   string
		change func(h *termHarness)
		reason string
	}{
		{"suspended", func(h *termHarness) { h.cfg.setUser("admin", "SUSPENDED", "yes") }, "Your session has expired."},
		{"shell taken away", func(h *termHarness) { h.cfg.setUser("admin", "SHELL", "nologin") }, "You have no shell access."},
		{"WEB_TERMINAL off", func(h *termHarness) { h.cfg.set("WEB_TERMINAL", "false") }, "The web terminal is not available"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTermHarness(t, overrides{}, 0)
			ticks := make(chan time.Time)
			h.api.termState().recheckTicks.Store(&ticks)
			ws, tc := h.openRelay()
			c.change(h)
			h.deps.H.InvalidateConfig()
			select {
			case ticks <- time.Now():
			case <-time.After(5 * time.Second):
				t.Fatal("no idle re-check is waiting")
			}
			wantClose(t, ws, c.reason)
			tc.closed(t)
			waitNoWatchers(t, h)
		})
	}

	// Nothing changed: the terminal stays open and the session's activity is untouched.
	h := newTermHarness(t, overrides{}, 0)
	ticks := make(chan time.Time)
	h.api.termState().recheckTicks.Store(&ticks)
	ws, tc := h.openRelay()
	sid := h.sessionID()
	before, _ := h.store.Get(sid)
	h.clock.Add(61 * time.Second)
	for range 2 {
		select {
		case ticks <- time.Now():
		case <-time.After(5 * time.Second):
			t.Fatal("no idle re-check is waiting")
		}
	}
	if after, _ := h.store.Get(sid); !after.LastActivity.Equal(before.LastActivity) {
		t.Fatalf("an idle re-check moved LastActivity to %v", after.LastActivity)
	}
	if err := ws.Write(context.Background(), websocket.MessageBinary, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if fr := tc.next(t); string(fr.payload) != "x" {
		t.Fatalf("frame %q", fr.payload)
	}
	ws.Close(websocket.StatusNormalClosure, "")
	tc.closed(t)
	waitNoWatchers(t, h)
}

// waitNoWatchers fails unless every idle re-check goroutine has ended.
func waitNoWatchers(t *testing.T, h *termHarness) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.api.termState().watchers.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d idle re-check goroutines left", h.api.termState().watchers.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// v-delete-sys-web-terminal turns WEB_TERMINAL off and stops the service, so the
// terminal connection just ends (C3 D2). The browser is told the terminal is gone,
// not that its session expired; a shell that simply exits still closes plainly.
func TestTermServiceRemoved(t *testing.T) {
	h := newTermHarness(t, overrides{}, 0)
	ws, tc := h.openRelay()
	h.cfg.set("WEB_TERMINAL", "false") // the config cache still says true
	tc.Close()
	wantClose(t, ws, "The web terminal is not available")

	h = newTermHarness(t, overrides{}, 0)
	ws, tc = h.openRelay()
	tc.Close()
	wantClose(t, ws, "")
}
