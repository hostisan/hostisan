package session

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

// CookieName is the session cookie (Hestia's PHP panel used HESTIASID).
const CookieName = "HOSTISANSID"

const msgLoggedOut = "You are logged out, please log in again."

// Manager loads sessions and enforces the checks web/inc/main.php ran on every request.
type Manager struct {
	Store Store
	H     *hestia.Client
	R     runner.Runner
	Dev   bool             // allows a non-Secure cookie for plain-HTTP development
	Now   func() time.Time // default time.Now

	// SystemHostname is the server's own name for the CSRF origin check;
	// default os.Hostname().
	SystemHostname string

	// WriteError writes API errors; set to httpapi.WriteError (a func field avoids an
	// import cycle). Defaults to the same JSON shape.
	WriteError func(w http.ResponseWriter, status int, code, message string)

	keyOnce sync.Once
	anonKey []byte // signs anonymous sessions' CSRF tokens

	pwMu   sync.Mutex
	pwSeen map[string]passwordSeen // by user; see passwordMark
	// ended holds when PasswordChanged ended a session, by key(id), so that
	// its browser is told why (SignedOutReason); guarded by pwMu.
	ended map[string]time.Time
	// usersChecked is when Sweep last asked whether the stored users exist.
	usersChecked time.Time
}

// signedOutKeep is how long a session ended by PasswordChanged keeps its reason.
// In memory only: after a panel restart such a browser gets no reason, as before.
const signedOutKeep = time.Hour

// userCheckEvery is how often Sweep asks whether the users of stored sessions
// still exist.
const userCheckEvery = 10 * time.Minute

// passwordCheckEvery is how long a user's password mark is reused before it is
// read again: a password changed outside the panel ends its sessions within it.
const passwordCheckEvery = 15 * time.Second

type passwordSeen struct {
	mark string
	at   time.Time
}

type ctxKey struct{}

// FromContext returns the request's session, set by Load.
func FromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(ctxKey{}).(*Session)
	return s
}

// WithSession is for handlers that replace the session (login, logout).
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// Load attaches the session from the cookie, or an anonymous one. Anonymous sessions
// aren't stored, so visits cost no memory: their CSRF token is derived from the cookie
// (see anonymous). A session is stored once a password has been checked.
func (m *Manager) Load(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var s *Session
		if c, err := r.Cookie(CookieName); err == nil && validID(c.Value) {
			if s, _ = m.Store.Get(c.Value); s == nil {
				s = m.anonymous(c.Value)
			}
		}
		if s == nil {
			s = m.NewAnonymous(w)
		}
		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), s)))
	})
}

// NewAnonymous starts a signed-out session with a new cookie and CSRF token.
func (m *Manager) NewAnonymous(w http.ResponseWriter) *Session {
	s := m.anonymous(NewToken())
	m.SetCookie(w, s.ID)
	return s
}

// anonymous is the signed-out session for a cookie id. Its CSRF token is an HMAC of
// the id under a key made at startup, so it can be checked without storing anything.
func (m *Manager) anonymous(id string) *Session {
	m.keyOnce.Do(func() {
		m.anonKey = make([]byte, 32)
		if _, err := rand.Read(m.anonKey); err != nil {
			panic("session: no randomness: " + err.Error())
		}
	})
	mac := hmac.New(sha256.New, m.anonKey)
	mac.Write([]byte(id))
	return &Session{ID: id, Token: hex.EncodeToString(mac.Sum(nil))[:32]}
}

// validID reports whether a cookie value looks like an id NewToken made.
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// Sweep removes sessions nobody can use any more: signed-in sessions idle longer than
// INACTIVE_SESSION_TIMEOUT, 2FA steps (with the password they hold) past their
// TTL and, every userCheckEvery, those of users who no longer exist. serve runs
// it every minute.
func (m *Manager) Sweep(ctx context.Context) {
	now := m.now()
	m.pwMu.Lock()
	for k, at := range m.ended {
		if now.Sub(at) >= signedOutKeep || now.Before(at) {
			delete(m.ended, k)
		}
	}
	checkUsers := m.usersChecked.IsZero() || now.Sub(m.usersChecked) >= userCheckEvery || now.Before(m.usersChecked)
	if checkUsers {
		m.usersChecked = now
	}
	m.pwMu.Unlock()
	if checkUsers {
		m.dropGoneUsers(ctx)
	}
	cfg, err := m.H.Config(ctx)
	if err != nil {
		slog.Warn("session sweep skipped", "err", err)
		return
	}
	minutes, _ := strconv.Atoi(cfg["INACTIVE_SESSION_TIMEOUT"])
	m.Store.Sweep(now, time.Duration(minutes)*time.Minute)
}

// dropGoneUsers ends the stored sessions of users Hestia says no longer exist: a
// user deleted over SSH (v-delete-user) kept theirs on file until they timed out.
// Validate refuses such a cookie anyway; this keeps the file clean, also for
// browsers that never come back. One v-list-user (Validate's check) per stored
// user; a failed check keeps the sessions.
func (m *Manager) dropGoneUsers(ctx context.Context) {
	for _, user := range m.Store.Users() {
		_, ok, err := m.H.User(ctx, user)
		if err != nil {
			slog.Warn("session user check failed", "user", user, "err", err)
			continue
		}
		if !ok {
			slog.Info("sessions of a deleted user ended", "user", user)
			m.Store.DeleteUser(user, "")
		}
	}
}

// SetCookie sends the session cookie, e.g. after login regenerates the ID.
func (m *Manager) SetCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   !m.Dev,
		SameSite: http.SameSiteLaxMode,
	})
}

// RequireAuth runs Hestia's per-request session checks; any failure ends the session.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := FromContext(r.Context())
		if s == nil || s.User == "" {
			if s != nil {
				if msg := m.SignedOutReason(s.ID); msg != "" {
					m.writeError(w, http.StatusUnauthorized, "logged_out", msg)
					return
				}
			}
			m.writeError(w, http.StatusUnauthorized, "session_expired", "")
			return
		}
		code, msg, err := m.Validate(r, s)
		if err != nil {
			m.serverError(w, err)
			return
		}
		if code != "" {
			m.writeError(w, http.StatusUnauthorized, code, msg)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Validate runs the checks web/inc/main.php ran on every request for a signed-in
// session s. If they pass it records the activity and returns code "". Otherwise it
// ends the session and returns the API error code and message to report.
func (m *Manager) Validate(r *http.Request, s *Session) (code, msg string, err error) {
	ctx := r.Context()
	cfg, err := m.H.Config(ctx)
	if err != nil {
		return "", "", err
	}
	if s.CombinedIP != RealIP(r) && cfg["DISABLE_IP_CHECK"] != "yes" {
		return m.endSession(r, s, true, "session_expired", "")
	}
	u, ok, err := m.H.User(ctx, s.User)
	if err != nil {
		return "", "", err
	}
	if !ok || (s.UserContext == "admin" && u.Role() != "admin") {
		return m.endSession(r, s, false, "session_expired", "")
	}
	if u.Suspended() && cfg["POLICY_USER_VIEW_SUSPENDED"] != "yes" {
		return m.endSession(r, s, false, "logged_out", msgLoggedOut)
	}
	// A password changed outside the panel (v-change-user-password over SSH or
	// the legacy API) ends the session; the panel's own forms call
	// PasswordChanged. The first check of a session records the mark.
	mark, err := m.passwordMark(ctx, s.User, s.PasswordMark == "")
	if err != nil {
		return "", "", err
	}
	if s.PasswordMark != "" && s.PasswordMark != mark {
		// PasswordChanged may have reset it since this request loaded s
		if cur, ok := m.Store.Get(s.ID); !ok || cur.PasswordMark != "" {
			return m.endSession(r, s, false, "logged_out", msgLoggedOut)
		}
		s.PasswordMark = ""
	}
	minutes, err := strconv.Atoi(cfg["INACTIVE_SESSION_TIMEOUT"])
	if err != nil || minutes <= 0 || s.LastActivity.IsZero() {
		return m.endSession(r, s, false, "session_expired", "")
	}
	if m.now().After(s.LastActivity.Add(time.Duration(minutes) * time.Minute)) {
		return m.endSession(r, s, true, "session_expired", "")
	}
	now := m.now()
	if !m.Store.Update(s.ID, func(cur *Session) {
		cur.LastActivity = now
		if cur.PasswordMark == "" {
			cur.PasswordMark = mark
		}
	}) {
		// Logged out by a parallel request since this one started.
		return "session_expired", "", nil
	}
	s.LastActivity = now
	if s.PasswordMark == "" {
		s.PasswordMark = mark
	}
	return "", "", nil
}

// passwordMark is a SHA-256 of user's stored password hash: the MD5 field of
// Hestia's user.conf, which v-change-user-password sets from /etc/shadow (and
// which a rebuild writes back to /etc/shadow). It is read once per
// passwordCheckEvery and user; fresh reads it now (a session's first check).
// A password set with passwd(1) alone, behind Hestia's back, does not change the
// field and is not seen: v-delete-sys-hostisan-sessions ends those sessions.
func (m *Manager) passwordMark(ctx context.Context, user string, fresh bool) (string, error) {
	now := m.now()
	m.pwMu.Lock()
	seen, ok := m.pwSeen[user]
	m.pwMu.Unlock()
	if ok && !fresh && now.Sub(seen.at) < passwordCheckEvery && !now.Before(seen.at) {
		return seen.mark, nil
	}
	res, err := m.R.Run(ctx, "v-get-user-value", user, "MD5")
	if err == nil {
		err = res.Err("v-get-user-value")
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hostisan password mark\x00"), bytes.TrimSpace(res.Stdout)...))
	mark := hex.EncodeToString(sum[:])
	m.pwMu.Lock()
	if m.pwSeen == nil {
		m.pwSeen = map[string]passwordSeen{}
	}
	m.pwSeen[user] = passwordSeen{mark: mark, at: now}
	m.pwMu.Unlock()
	return mark, nil
}

// PasswordChanged ends user's sessions after the panel changed the password,
// except keepID ("" for none): the session that changed it stays signed in and
// takes the new mark at its next request.
// The ended sessions' browsers are told why (SignedOutReason), as after a change
// outside the panel.
func (m *Manager) PasswordChanged(user, keepID string) {
	m.pwMu.Lock()
	delete(m.pwSeen, user)
	m.pwMu.Unlock()
	keys := m.Store.DeleteUser(user, keepID)
	if keepID != "" {
		m.Store.Update(keepID, func(cur *Session) { cur.PasswordMark = "" })
	}
	now := m.now()
	m.pwMu.Lock()
	if m.ended == nil && len(keys) > 0 {
		m.ended = map[string]time.Time{}
	}
	for _, k := range keys {
		m.ended[k] = now
	}
	m.pwMu.Unlock()
}

// SignedOutReason is the message for the browser whose session cookie is id when
// PasswordChanged ended that session less than signedOutKeep ago, and "" otherwise.
// A signed-out request then reports logged_out with it, not a bare
// session_expired, and the login page shows it.
func (m *Manager) SignedOutReason(id string) string {
	if !validID(id) {
		return ""
	}
	m.pwMu.Lock()
	at, ok := m.ended[key(id)]
	m.pwMu.Unlock()
	if !ok || m.now().Sub(at) >= signedOutKeep {
		return ""
	}
	return msgLoggedOut
}

// Effective is the account the request acts on: the looked-at user for an admin
// using "log in as", otherwise the signed-in user.
func (m *Manager) Effective(s *Session) string {
	if s.UserContext == "admin" && s.Look != "" {
		return s.Look
	}
	return s.User
}

func (m *Manager) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := FromContext(r.Context()); s == nil || s.UserContext != "admin" {
			m.writeError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireWritable blocks changes while the session is read-only (see ReadOnly).
func (m *Manager) RequireWritable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		s := FromContext(r.Context())
		cfg, err := m.H.Config(r.Context())
		if err != nil {
			m.serverError(w, err)
			return
		}
		u, _, err := m.H.User(r.Context(), m.Effective(s))
		if err != nil {
			m.serverError(w, err)
			return
		}
		if ReadOnly(s, u, cfg) {
			m.writeError(w, http.StatusForbidden, "read_only", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireCSRF checks the Origin (per POLICY_CSRF_STRICTNESS) and the X-CSRF-Token
// header on every state-changing request.
func (m *Manager) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		cfg, err := m.H.Config(r.Context())
		if err != nil {
			m.serverError(w, err)
			return
		}
		strictness, _ := strconv.Atoi(cfg["POLICY_CSRF_STRICTNESS"])
		if !CheckOrigin(r, m.hostname(), strictness) || !CheckToken(FromContext(r.Context()), r) {
			m.writeError(w, http.StatusBadRequest, "csrf", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// endSession destroys the session; logLogout also writes Hestia's logout log line.
func (m *Manager) endSession(r *http.Request, s *Session, logLogout bool, code, msg string) (string, string, error) {
	if logLogout {
		m.LogLogout(r.Context(), s)
	}
	m.Store.Delete(s.ID)
	return code, msg, nil
}

// LogLogout writes `v-log-user-logout <user> <token>`.
func (m *Manager) LogLogout(ctx context.Context, s *Session) {
	res, err := m.R.Run(ctx, "v-log-user-logout", s.User, s.Token)
	if err == nil {
		err = res.Err("v-log-user-logout")
	}
	if err != nil {
		slog.Warn("logout log failed", "user", s.User, "err", err)
	}
}

func (m *Manager) serverError(w http.ResponseWriter, err error) {
	slog.Error("session check failed", "err", err)
	m.writeError(w, http.StatusInternalServerError, "server_error", "")
}

func (m *Manager) writeError(w http.ResponseWriter, status int, code, message string) {
	if m.WriteError != nil {
		m.WriteError(w, status, code, message)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) hostname() string {
	if m.SystemHostname != "" {
		return m.SystemHostname
	}
	h, _ := os.Hostname()
	return h
}
