package httpapi

// The web terminal's panel side (Slice C, parity decisions 8-13): a single-use token
// for the signed-in session, then a websocket the panel checks itself and relays to
// the root terminal process over its unix socket, in internal/terminal frames.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/i18n"
	"github.com/hostisan/hostisan/internal/session"
	"github.com/hostisan/hostisan/internal/terminal"
)

const (
	termTokenTTL     = 30 * time.Second // decision 13
	termAuthWait     = 10 * time.Second // the client's auth message must come within this
	termPingEvery    = 25 * time.Second // keepalive (WT34); under nginx's read timeout
	termRefreshEvery = time.Minute      // typing extends the session at most this often (decision 12)
	termRecheckEvery = time.Minute      // an idle terminal's session and gates are re-checked this often
	termDialTimeout  = 5 * time.Second
	termWriteWait    = 10 * time.Second // a browser that stops reading is dropped after this
	// termMaxTokens bounds the tokens one session may hold at once (several tabs).
	termMaxTokens = 8
	// termReadLimit is the largest websocket message accepted from the browser (a
	// big paste); it is split into frames of at most terminal.MaxPayload.
	termReadLimit = 1 << 20
	termMaxSize   = 500 // cols and rows are clamped to this
	// termMaxPerUser bounds the terminals open at once for one effective user, so a
	// single account can't fill the terminal process's terminal.DefaultMaxSessions.
	termMaxPerUser = 8
)

// Close reasons (decision 11): Hestia's English texts.
const (
	termNotAuthenticated = "You are not authenticated."
	termNotAllowed       = "You are not allowed to access this server."
	termConnectionError  = "Connection error."
	// termUnavailable is the UI's own text for a terminal turned off server-wide
	// (WEB_TERMINAL off, e.g. v-delete-sys-web-terminal): that isn't an expired
	// session, and a reconnect would only show the same "not available" page.
	termUnavailable = "The web terminal is not available"
)

// termNoShell is a close reason too. It is a var so i18n.N can mark it.
var termNoShell = i18n.N("You have no shell access.")

// termReasons are the reasons a terminal's FrameClose may pass on to the browser;
// anything else closes with an empty reason.
var termReasons = map[string]bool{
	termNotAuthenticated: true, termNoShell: true, termNotAllowed: true, toolsSessionExpired: true,
	terminal.ReasonTooMany: true,
}

// termState is the server's outstanding terminal tokens (server.term).
type termState struct {
	mu     sync.Mutex
	tokens map[string][]termToken // session id -> unused tokens
	open   map[string]int         // effective user -> open terminals

	authWait   atomic.Int64 // test hook: the auth deadline in ns; 0 means termAuthWait
	writeWait  atomic.Int64 // test hook: the per-write deadline in ns; 0 means termWriteWait
	maxPerUser atomic.Int64 // test hook: the per-user cap; 0 means termMaxPerUser
	// recheckTicks is a test hook: when set, every open terminal's idle re-check runs
	// on its ticks instead of on a termRecheckEvery ticker.
	recheckTicks atomic.Pointer[chan time.Time]
	watchers     atomic.Int64 // idle re-check goroutines running (a leak check for tests)
}

type termToken struct {
	token   string
	user    string // the effective user it was issued for
	ip      string
	expires time.Time
}

func newTermState() *termState {
	return &termState{tokens: map[string][]termToken{}, open: map[string]int{}}
}

// acquire takes one of user's terminal slots; ok is false when all are in use. The
// release func gives the slot back and must be called once.
func (t *termState) acquire(user string) (release func(), ok bool) {
	limit := int(t.maxPerUser.Load())
	if limit <= 0 {
		limit = termMaxPerUser
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open[user] >= limit {
		return nil, false
	}
	t.open[user]++
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.open[user]--; t.open[user] <= 0 {
			delete(t.open, user)
		}
	}, true
}

// openFor is the number of terminals open for user.
func (t *termState) openFor(user string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.open[user]
}

// termState is the server's terminal state, created on first use for a server built
// without newServer (route-only tests).
func (s *server) termState() *termState {
	s.toolsInit.Do(s.initToolsState)
	return s.term
}

func (t *termState) writeTimeout() time.Duration {
	if d := time.Duration(t.writeWait.Load()); d > 0 {
		return d
	}
	return termWriteWait
}

func (t *termState) wait() time.Duration {
	if d := time.Duration(t.authWait.Load()); d > 0 {
		return d
	}
	return termAuthWait
}

// issue stores a new token for the session and prunes every expired one, so the map
// never holds more than live sessions' recent tokens.
func (t *termState) issue(sessID, user, ip string, now time.Time) termToken {
	tok := termToken{token: session.NewToken(), user: user, ip: ip, expires: now.Add(termTokenTTL)}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, list := range t.tokens {
		kept := list[:0]
		for _, k := range list {
			if !now.After(k.expires) {
				kept = append(kept, k)
			}
		}
		if len(kept) == 0 {
			delete(t.tokens, id)
		} else {
			t.tokens[id] = kept
		}
	}
	list := append(t.tokens[sessID], tok)
	if len(list) > termMaxTokens {
		list = list[len(list)-termMaxTokens:]
	}
	t.tokens[sessID] = list
	return tok
}

// redeem consumes the session's token equal to got (compared in constant time) and
// reports whether it is still valid for this user and address. A matching token is
// used up whether or not it is valid.
func (t *termState) redeem(sessID, got, user, ip string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.tokens[sessID]
	found := -1
	for i, k := range list {
		if subtle.ConstantTimeCompare([]byte(got), []byte(k.token)) == 1 {
			found = i
		}
	}
	if found < 0 {
		return false
	}
	k := list[found]
	list = append(list[:found:found], list[found+1:]...)
	if len(list) == 0 {
		delete(t.tokens, sessID)
	} else {
		t.tokens[sessID] = list
	}
	return !now.After(k.expires) && k.user == user && k.ip == ip
}

// termRoutes mounts the terminal token and websocket routes.
func (s *server) termRoutes(mux *http.ServeMux) {
	m := s.Sessions
	// Writable is checked by termGate, after the protected-admin rule, so that case
	// answers forbidden rather than read_only.
	mux.Handle("POST /api/v1/terminal/token", m.Load(m.RequireAuth(m.RequireCSRF(http.HandlerFunc(s.termToken)))))
	mux.Handle("GET /api/v1/terminal/socket", termSameOrigin(m.Load(m.RequireAuth(http.HandlerFunc(s.termSocket)))))
}

// termRefusal is why the terminal is refused: an HTTP status, an error code and a
// message for the token route.
type termRefusal struct {
	status        int
	code, message string
}

// termGate checks decision 9's rules for the session: WEB_TERMINAL on, not the
// protected admin, not read-only (D6), and a login shell. It is run when the token is
// issued and again when the socket connects. ok is false with the refusal.
func (s *server) termGate(ctx context.Context, sess *session.Session) (target string, refusal termRefusal, ok bool, err error) {
	cfg, err := s.H.Config(ctx)
	if err != nil {
		return "", termRefusal{}, false, err
	}
	if cfg["WEB_TERMINAL"] != "true" {
		return "", termRefusal{http.StatusNotFound, "not_found", ""}, false, nil
	}
	if protectedAdminLook(sess, cfg) {
		return "", termRefusal{http.StatusForbidden, "forbidden", ""}, false, nil
	}
	target = s.Sessions.Effective(sess)
	if !hestia.ValidUsername(target) {
		return "", termRefusal{http.StatusForbidden, "forbidden", ""}, false, nil
	}
	u, found, err := s.H.User(ctx, target)
	if err != nil {
		return "", termRefusal{}, false, err
	}
	if !found {
		return "", termRefusal{http.StatusNotFound, "not_found", ""}, false, nil
	}
	if session.ReadOnly(sess, u, cfg) {
		return "", termRefusal{http.StatusForbidden, "read_only", ""}, false, nil
	}
	if strings.HasSuffix(u.Shell(), "nologin") {
		return "", termRefusal{http.StatusForbidden, "no_shell", termNoShell}, false, nil
	}
	return target, termRefusal{}, true, nil
}

// termToken answers POST /api/v1/terminal/token.
func (s *server) termToken(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	target, refusal, ok, err := s.termGate(r.Context(), sess)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		WriteError(w, refusal.status, refusal.code, refusal.message)
		return
	}
	tok := s.termState().issue(sess.ID, target, session.RealIP(r), s.now())
	writeJSON(w, http.StatusOK, TerminalToken{Token: tok.token, Expires: tok.expires.UTC().Format(time.RFC3339)})
}

// termSameOrigin refuses a websocket upgrade whose Origin isn't the host the request
// was sent to (WT28): a page on another site can't open a terminal with the
// visitor's cookie. Browsers always send Origin on a websocket; a missing one is
// refused too.
func termSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !termOriginOK(r) {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func termOriginOK(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return false
	}
	defPort := map[string]string{"http": "80", "https": "443"}[u.Scheme]
	oHost, oPort := splitHostDefault(u.Host, defPort)
	rHost, rPort := splitHostDefault(r.Host, defPort)
	return oHost != "" && strings.EqualFold(oHost, rHost) && oPort == rPort
}

func splitHostDefault(hostport, defPort string) (string, string) {
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return h, p
	}
	return strings.Trim(hostport, "[]"), defPort
}

// termClientMsg is a text message from the browser: {"type":"auth","token","cols",
// "rows"} first, then {"type":"resize","cols","rows"}.
type termClientMsg struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

// termSize clamps a window size from the browser; 0 or less takes def.
func termSize(n, def int) uint16 {
	switch {
	case n <= 0:
		return uint16(def)
	case n > termMaxSize:
		return termMaxSize
	}
	return uint16(n)
}

// termSocket serves GET /api/v1/terminal/socket.
func (s *server) termSocket(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	ip := session.RealIP(r)
	// The server's read and write timeouts would cut the terminal off; the connection
	// is hijacked with the deadlines it has, so clear them first.
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(time.Time{}); err != nil {
		slog.Warn("terminal: clear read deadline", "err", err)
	}
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Warn("terminal: clear write deadline", "err", err)
	}
	// Accept also checks Origin against Host.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(termReadLimit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	refuse := func() { _ = ws.Close(websocket.StatusNormalClosure, termNotAuthenticated) }

	// A cancelled Read drops the connection without a close frame, so the deadline
	// closes the socket properly instead, which ends the Read.
	timer := time.AfterFunc(s.termState().wait(), refuse)
	typ, data, err := ws.Read(ctx)
	if !timer.Stop() {
		return
	}
	var auth termClientMsg
	if err != nil || typ != websocket.MessageText || json.Unmarshal(data, &auth) != nil || auth.Type != "auth" {
		refuse()
		return
	}
	target := s.Sessions.Effective(sess)
	if !s.termState().redeem(sess.ID, auth.Token, target, ip, s.now()) {
		refuse()
		return
	}
	// Every gate again (decision 9): a setting or the account may have changed since
	// the token was issued.
	if cur, ok := s.Sessions.Store.Get(sess.ID); !ok || cur.Look != sess.Look || cur.User != sess.User {
		refuse()
		return
	}
	if _, _, ok, err := s.termGate(ctx, sess); err != nil || !ok {
		if err != nil {
			slog.Error("terminal gate", "err", err)
		}
		refuse()
		return
	}

	release, ok := s.termState().acquire(target)
	if !ok {
		_ = ws.Close(websocket.StatusNormalClosure, terminal.ReasonTooMany)
		return
	}
	defer release()

	opened := s.now() // before the terminal hears of us: the first refresh is due a minute later
	dialer := net.Dialer{Timeout: termDialTimeout}
	tc, err := dialer.DialContext(ctx, "unix", s.terminalSocket())
	if err != nil {
		slog.Error("terminal: the terminal service is unreachable", "err", err)
		_ = ws.Close(websocket.StatusInternalError, termConnectionError)
		return
	}
	defer tc.Close()
	hello, _ := json.Marshal(terminal.Hello{User: target, Cols: termSize(auth.Cols, 80), Rows: termSize(auth.Rows, 24)})
	if err := terminal.WriteFrame(tc, terminal.FrameHello, hello); err != nil {
		slog.Error("terminal: hello", "err", err)
		_ = ws.Close(websocket.StatusInternalError, termConnectionError)
		return
	}

	relay := &termRelay{ws: ws, tc: tc}
	unregister := s.tools.register(sess.ID, toolKindTerminal, func(reason string) {
		// Called from logout or the session store: don't wait for the close handshake.
		go relay.end(websocket.StatusNormalClosure, reason)
	})
	defer unregister()
	// The session may have ended between the gate and register; its closers have run.
	if cur, ok := s.Sessions.Store.Get(sess.ID); !ok || cur.Look != sess.Look || cur.User != sess.User {
		relay.end(websocket.StatusNormalClosure, toolsSessionExpired)
		return
	}

	slog.Info("terminal opened", "user", sess.User, "target", target, "ip", ip)
	defer slog.Info("terminal closed", "user", sess.User, "target", target, "ip", ip)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.termFromShell(ctx, relay) }()
	go func() { defer wg.Done(); s.termToShell(ctx, relay, sess.ID, opened) }()
	go s.termKeepalive(ctx, relay)
	s.termState().watchers.Add(1)
	go func() { defer s.termState().watchers.Add(-1); s.termWatch(ctx, relay, sess.ID) }()
	wg.Wait()
}

// termRelay is one open terminal; end closes both sides once, with the first reason.
type termRelay struct {
	ws    *websocket.Conn
	tc    net.Conn
	once  sync.Once
	ended atomic.Bool // end has been called
}

func (t *termRelay) end(code websocket.StatusCode, reason string) {
	t.once.Do(func() {
		t.ended.Store(true)
		t.tc.Close()
		_ = t.ws.Close(code, reason)
	})
}

// termFromShell copies the terminal's output to the browser until it closes.
func (s *server) termFromShell(ctx context.Context, t *termRelay) {
	for {
		typ, p, err := terminal.ReadFrame(t.tc)
		if err != nil {
			t.end(websocket.StatusNormalClosure, s.termEndReason(ctx, t))
			return
		}
		switch typ {
		case terminal.FrameData:
			wctx, cancel := context.WithTimeout(ctx, s.termState().writeTimeout())
			err := t.ws.Write(wctx, websocket.MessageBinary, p)
			cancel()
			if err != nil {
				t.end(websocket.StatusNormalClosure, "")
				return
			}
		case terminal.FrameClose:
			reason := string(p)
			if !termReasons[reason] {
				reason = ""
			}
			t.end(websocket.StatusNormalClosure, reason)
			return
		}
	}
}

// termEndReason is the close reason when the terminal process ended the connection
// without a FrameClose: the shell exited (no reason), or the service was removed
// (v-delete-sys-web-terminal turns WEB_TERMINAL off before stopping it).
func (s *server) termEndReason(ctx context.Context, t *termRelay) string {
	if t.ended.Load() {
		return "" // the panel closed it; that close already carries the reason
	}
	// The cached config may predate the change.
	s.H.InvalidateConfig()
	cctx, cancel := context.WithTimeout(ctx, termDialTimeout)
	defer cancel()
	if cfg, err := s.H.Config(cctx); err == nil && cfg["WEB_TERMINAL"] != "true" {
		return termUnavailable
	}
	return ""
}

// termToShell copies the browser's keystrokes and resizes to the terminal; typing
// keeps the panel session alive (decision 12).
func (s *server) termToShell(ctx context.Context, t *termRelay, sessID string, lastRefresh time.Time) {
	for {
		typ, p, err := t.ws.Read(ctx)
		if err != nil {
			t.end(websocket.StatusNormalClosure, "")
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if now := s.now(); now.Sub(lastRefresh) >= termRefreshEvery {
				lastRefresh = now
				if reason, ok := s.termTouch(ctx, sessID, now); !ok {
					t.end(websocket.StatusNormalClosure, reason)
					return
				}
			}
			for len(p) > 0 {
				n := min(len(p), terminal.MaxPayload)
				if err := terminal.WriteFrame(t.tc, terminal.FrameData, p[:n]); err != nil {
					t.end(websocket.StatusNormalClosure, "")
					return
				}
				p = p[n:]
			}
		case websocket.MessageText:
			var msg termClientMsg
			if json.Unmarshal(p, &msg) != nil || msg.Type != "resize" || msg.Cols <= 0 || msg.Rows <= 0 {
				continue
			}
			if err := terminal.WriteFrame(t.tc, terminal.FrameResize,
				terminal.ResizePayload(termSize(msg.Cols, 80), termSize(msg.Rows, 24))); err != nil {
				t.end(websocket.StatusNormalClosure, "")
				return
			}
		}
	}
}

// termTouch re-checks the session and gates (termRecheck) and records activity on
// the session, unless it has already outlived INACTIVE_SESSION_TIMEOUT (the sweep
// will end it; typing must not revive it). ok false means close with reason.
func (s *server) termTouch(ctx context.Context, sessID string, now time.Time) (reason string, ok bool) {
	reason, ok, cfg := s.termRecheck(ctx, sessID)
	if !ok || cfg == nil {
		return reason, ok
	}
	idle, _ := time.ParseDuration(strings.TrimSpace(cfg["INACTIVE_SESSION_TIMEOUT"]) + "m")
	s.Sessions.Store.Update(sessID, func(cur *session.Session) {
		if idle > 0 && now.After(cur.LastActivity) && !now.After(cur.LastActivity.Add(idle)) {
			cur.LastActivity = now
		}
	})
	return "", true
}

// termRecheck re-checks the session and decision 9's gates: a suspension or a policy
// change since the terminal opened ends it. ok false means close with reason. A
// config or user read error skips the check (cfg is then nil).
func (s *server) termRecheck(ctx context.Context, sessID string) (reason string, ok bool, cfg hestia.SysConfig) {
	cfg, err := s.H.Config(ctx)
	if err != nil {
		slog.Warn("terminal recheck skipped", "err", err)
		return "", true, nil
	}
	cur, found := s.Sessions.Store.Get(sessID)
	if !found {
		return toolsSessionExpired, false, nil
	}
	// Validate's rule: a suspended signed-in user is logged out unless policy lets
	// them look around.
	self, found, err := s.H.User(ctx, cur.User)
	if err != nil {
		slog.Warn("terminal recheck skipped", "err", err)
		return "", true, nil
	}
	if !found || self.Suspended() && cfg["POLICY_USER_VIEW_SUSPENDED"] != "yes" {
		return toolsSessionExpired, false, nil
	}
	if _, refusal, allowed, err := s.termGate(ctx, cur); err != nil {
		slog.Warn("terminal recheck skipped", "err", err)
		return "", true, nil
	} else if !allowed {
		if refusal.code == "no_shell" {
			return termNoShell, false, nil
		}
		if cfg["WEB_TERMINAL"] != "true" {
			return termUnavailable, false, nil
		}
		return toolsSessionExpired, false, nil
	}
	return "", true, cfg
}

// termWatch re-checks an open terminal every termRecheckEvery, typing or not, so an
// idle shell doesn't outlive a suspension or a gate change (typing alone used to
// trigger the check). It never counts as activity. It ends with ctx.
func (s *server) termWatch(ctx context.Context, t *termRelay, sessID string) {
	var ticks <-chan time.Time
	if c := s.termState().recheckTicks.Load(); c != nil {
		ticks = *c
	} else {
		tick := time.NewTicker(termRecheckEvery)
		defer tick.Stop()
		ticks = tick.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if reason, ok, _ := s.termRecheck(ctx, sessID); !ok {
				t.end(websocket.StatusNormalClosure, reason)
				return
			}
		}
	}
}

// termKeepalive pings the browser so proxies keep an idle terminal open.
func (s *server) termKeepalive(ctx context.Context, t *termRelay) {
	tick := time.NewTicker(termPingEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			pctx, cancel := context.WithTimeout(ctx, termPingEvery)
			err := t.ws.Ping(pctx)
			cancel()
			if err != nil {
				t.end(websocket.StatusNormalClosure, "")
				return
			}
		}
	}
}
