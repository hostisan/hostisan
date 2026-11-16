package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/secret"
	"github.com/hostisan/hostisan/internal/session"
)

// Recovery (Plan 3 Slice C, Parity 16 and 17): password reset by emailed code, and 2FA
// unlock by password plus emailed code. Codes are 128 random bits; only
// sha256(prefix + code) is stored in RKEY, with a different prefix per flow so one
// flow's code never works in the other.

const (
	acctResetPrefix  = "reset:"
	acctUnlockPrefix = "2fa:"

	acctRecoveryCodeTTL   = 15 * time.Minute
	acctRecoveryWindow    = time.Hour
	acctResetRequestLimit = 10 // requests per IP per hour
	acctResetConfirmLimit = 20 // confirm attempts per IP per hour
	acctFailedLoginDelay  = 2 * time.Second
	acctFailedCodeDelay   = 5 * time.Second
	acctRecoveryMailWait  = time.Minute // the most a background recovery email may take

	acctMsgInvalidCode     = "Invalid username or code"
	acctMsgCodeExpired     = "Code has been expired"
	acctMsgPasswordMatch   = "Passwords do not match"
	acctMsgPasswordRule    = "Password does not match the minimum requirements."
	acctMsgInternal        = "An internal error occurred"
	acctMsgResetFailed     = "Failed to enter correct password reset code"
	acctMsgResetExpired    = "Reset code has been expired"
	acctMsgResetThrottled  = "Too many password reset attempts"
	acctMsgUnlockThrottled = "Too many 2FA unlock attempts"
	acctMsgUnlockFailed    = "Failed to enter correct 2FA unlock code"
	acctMsgUnlockExpired   = "2FA unlock code has been expired"
)

// The email bodies are Hestia's msgids, so existing translations and custom templates
// keep working; the unlock body is new (Hestia has no such email).
const (
	acctResetEmailBody = "Hello {{name}},\n" +
		"\n" +
		"To reset your {{appname}} password, please follow this link:\n" +
		"https://{{hostname}}/reset/?action=confirm&user={{user}}&code={{resetcode}}\n" +
		"\n" +
		"Alternatively, you may go to https://{{hostname}}/reset/?action=code&user={{user}} and enter the following reset code:\n" +
		"{{resetcode}}\n" +
		"\n" +
		"If you did not request password reset, please ignore this message and accept our apologies.\n" +
		"\n" +
		"Best regards,\n" +
		"\n" +
		"--\n" +
		"{{appname}}"
	acctUnlockEmailBody = "Hello {{name}},\n" +
		"\n" +
		"To turn off two-factor authentication on your {{appname}} account, please follow this link:\n" +
		"https://{{hostname}}/reset-2fa/?action=confirm&user={{user}}&code={{resetcode}}\n" +
		"\n" +
		"Alternatively, you may go to https://{{hostname}}/reset-2fa/?action=code&user={{user}} and enter the following unlock code:\n" +
		"{{resetcode}}\n" +
		"\n" +
		"The code is valid for 15 minutes. If you did not request this, please ignore this message and change your password.\n" +
		"\n" +
		"Best regards,\n" +
		"\n" +
		"--\n" +
		"{{appname}}"
)

// recoveryRoutes registers password reset and 2FA reset (Plan 3 Slice C). Called from registerAccountRoutes.
func (s *server) recoveryRoutes(mux *http.ServeMux) {
	m := s.Sessions
	anon := func(h http.HandlerFunc) http.Handler { return m.Load(m.RequireCSRF(h)) }
	mux.Handle("POST /api/v1/auth/reset/request", anon(s.resetRequest))
	mux.Handle("POST /api/v1/auth/reset/confirm", anon(s.resetConfirm))
	mux.Handle("POST /api/v1/auth/reset-2fa/request", anon(s.unlockRequest))
	mux.Handle("POST /api/v1/auth/reset-2fa/confirm", anon(s.unlockConfirm))
}

// ---------------------------------------------------------------- helpers

func acctRecoveryDecode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(into); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", badRequestMsg)
		return false
	}
	return true
}

// acctRecoveryHash is the value stored in RKEY for code in the given flow.
func acctRecoveryHash(prefix, code string) string {
	sum := sha256.Sum256([]byte(prefix + code))
	return hex.EncodeToString(sum[:])
}

// acctRecoveryCode is 128 random bits, base32 (26 characters).
func acctRecoveryCode() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]), nil
}

// allowIn records an attempt for key in m and reports whether it is within limit per
// hour.
func (a *accountsState) allowIn(m *map[string][]time.Time, key string, limit int, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(now)
	if *m == nil {
		*m = map[string][]time.Time{}
	}
	recent := (*m)[key][:0:0]
	for _, t := range (*m)[key] {
		if now.Sub(t) < acctRecoveryWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		(*m)[key] = recent
		return false
	}
	(*m)[key] = append(recent, now)
	return true
}

// sweepLocked drops, at most once a minute, the IPs whose attempts are all older than
// the window and the email slots older than a code's lifetime, so the maps only hold
// recent callers. Hold a.mu.
func (a *accountsState) sweepLocked(now time.Time) {
	if now.Sub(a.swept) < time.Minute {
		return
	}
	a.swept = now
	for _, m := range []map[string][]time.Time{a.resetRequests, a.resetConfirms} {
		for k, ts := range m {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= acctRecoveryWindow {
				delete(m, k)
			}
		}
	}
	for k, t := range a.resetEmails {
		if now.Sub(t) >= acctRecoveryCodeTTL {
			delete(a.resetEmails, k)
		}
	}
}

// acctThrottleKey is the per-IP throttle key: the address itself, or its /64 for IPv6,
// where one client usually holds the whole prefix.
func acctThrottleKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Unmap().Is6() {
		return ip
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return p.String()
}

// allowRequest and allowConfirm count one attempt from ip.
func (a *accountsState) allowRequest(ip string, now time.Time) bool {
	return a.allowIn(&a.resetRequests, acctThrottleKey(ip), acctResetRequestLimit, now)
}

func (a *accountsState) allowConfirm(ip string, now time.Time) bool {
	return a.allowIn(&a.resetConfirms, acctThrottleKey(ip), acctResetConfirmLimit, now)
}

// takeEmailSlot reserves the one email per user and flow per 15 minutes.
func (a *accountsState) takeEmailSlot(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(now)
	if a.resetEmails == nil {
		a.resetEmails = map[string]time.Time{}
	}
	if last, ok := a.resetEmails[key]; ok && now.Sub(last) < acctRecoveryCodeTTL {
		return false
	}
	a.resetEmails[key] = now
	return true
}

// acctRecoverySent is the one reply a request step ever gives.
func acctRecoverySent(w http.ResponseWriter) { writeJSON(w, http.StatusOK, OK{Status: "sent"}) }

// recoveryLogFailed writes the failed-login line fail2ban watches, with the real IP, the
// session token and the user agent.
func (s *server) recoveryLogFailed(r *http.Request, user, reason string) {
	if !hestia.ValidUsername(user) {
		return
	}
	sess := session.FromContext(r.Context())
	token := ""
	if sess != nil {
		token = sess.Token
	}
	res, err := s.R.Run(r.Context(), "v-log-user-login", user, session.RealIP(r), "failed", token, auth.LogSafe(r.UserAgent()), "yes", reason)
	if err == nil {
		err = res.Err("v-log-user-login")
	}
	if err != nil {
		slog.Warn("recovery failure log failed", "err", err)
	}
}

// recoveryStore saves hash as the user's RKEY through a private file; the code itself is
// never in argv.
func (s *server) recoveryStore(ctx context.Context, user, hash string) error {
	path, cleanup, err := secret.TempFile(s.tmpDir(), hash)
	if err != nil {
		return err
	}
	defer cleanup()
	res, err := s.R.Run(ctx, "v-change-user-rkey", user, path)
	if err != nil {
		return err
	}
	return res.Err("v-change-user-rkey")
}

// recoverySend emails the code in the background, so the SMTP server's time can't
// show through the reply's 2 s pad; a failure is logged without the code and never
// changes the reply.
func (s *server) recoverySend(ctx context.Context, cfg hestia.SysConfig, u hestia.User, user, tpl, body, subject, code string) {
	if s.Mailer == nil {
		slog.Warn("recovery email not sent: no mailer configured", "user", user)
		return
	}
	name := u["NAME"]
	if strings.TrimSpace(name) == "" {
		name = user
	}
	msg := s.renderEmail(ctx, cfg, u.Language(), tpl, body, subject,
		map[string]string{"date": s.now().Format("2006-01-02 15:04:05"), "user": user},
		map[string]string{"name": name, "user": user, "resetcode": code, "hostname": acctPanelHost(s.acctHostname(), cfg),
			"date": s.now().Format("2006-01-02 15:04:05")})
	msg.To = u["CONTACT"]
	s.background(func() {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acctRecoveryMailWait)
		defer cancel()
		if err := s.Mailer.Send(mctx, cfg, msg); err != nil {
			slog.Warn("recovery email not sent", "user", user, "err", err)
		}
	})
}

// recoveryIssue creates a code for user in the flow, stores its hash and emails it.
func (s *server) recoveryIssue(ctx context.Context, cfg hestia.SysConfig, u hestia.User, user, prefix, tpl, body, subject string) {
	code, err := acctRecoveryCode()
	if err != nil {
		slog.Warn("recovery code not generated", "err", err)
		return
	}
	if err := s.recoveryStore(ctx, user, acctRecoveryHash(prefix, code)); err != nil {
		slog.Warn("recovery key not stored", "user", user, "err", err)
		return
	}
	s.recoverySend(ctx, cfg, u, user, tpl, body, subject, code)
}

type acctCodeResult int

const (
	acctCodeOK acctCodeResult = iota
	acctCodeInvalid
	acctCodeExpired
)

// recoveryCheck compares code with the user's RKEY for the flow in constant time, then
// checks its age (RKEYEXP is when it was issued). Unknown, suspended and invalid users
// are "invalid", like a wrong code.
func (s *server) recoveryCheck(ctx context.Context, user, prefix, code string) (acctCodeResult, hestia.User, error) {
	want := acctRecoveryHash(prefix, strings.ToUpper(strings.TrimSpace(code)))
	var u hestia.User
	stored, found := "", false
	if hestia.ValidUsername(user) {
		var err error
		if u, found, err = s.H.User(ctx, user); err != nil {
			return acctCodeInvalid, nil, err
		}
	}
	if found && !u.Suspended() {
		stored = u["RKEY"]
	}
	if stored == "" {
		stored = strings.Repeat("0", len(want)) // compare anyway, then refuse
		found = false
	}
	match := subtle.ConstantTimeCompare([]byte(want), []byte(stored)) == 1
	if !match || !found || u.Suspended() {
		return acctCodeInvalid, nil, nil
	}
	res, err := s.R.Run(ctx, "v-get-user-value", user, "RKEYEXP")
	if err == nil {
		err = res.Err("v-get-user-value")
	}
	if err != nil {
		return acctCodeInvalid, nil, err
	}
	issued, perr := strconv.ParseInt(strings.TrimSpace(string(res.Stdout)), 10, 64)
	if perr != nil || s.now().After(time.Unix(issued, 0).Add(acctRecoveryCodeTTL)) {
		return acctCodeExpired, u, nil
	}
	return acctCodeOK, u, nil
}

// recoveryFail logs a failed confirm for fail2ban, waits and gives the one generic reply.
func (s *server) recoveryFail(w http.ResponseWriter, r *http.Request, user, reason string) {
	s.recoveryLogFailed(r, user, reason)
	s.Auth.Delay(acctFailedCodeDelay)
	WriteError(w, http.StatusBadRequest, "invalid", acctMsgInvalidCode)
}

func (s *server) recoveryExpired(w http.ResponseWriter, r *http.Request, user, reason string) {
	s.recoveryLogFailed(r, user, reason)
	s.Auth.Delay(acctFailedCodeDelay)
	WriteError(w, http.StatusBadRequest, "expired", acctMsgCodeExpired)
}

// ---------------------------------------------------------------- password reset

func (s *server) resetRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := s.now()
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	if cfg["POLICY_SYSTEM_PASSWORD_RESET"] == "no" {
		WriteError(w, http.StatusNotFound, "disabled", "")
		return
	}
	var req ResetRequest
	if !acctRecoveryDecode(w, r, &req) {
		return
	}
	s.recoveryResetMail(ctx, cfg, r, req)
	s.recoveryPad(start) // every path takes the same time
	acctRecoverySent(w)
}

// recoveryResetMail issues a code when user, email and throttles allow it; it reports
// nothing, because the reply never depends on it.
func (s *server) recoveryResetMail(ctx context.Context, cfg hestia.SysConfig, r *http.Request, req ResetRequest) {
	if !s.acct.allowRequest(session.RealIP(r), s.now()) || !hestia.ValidUsername(req.User) {
		return
	}
	u, ok, err := s.H.User(ctx, req.User)
	if err != nil {
		slog.Warn("password reset lookup failed", "err", err)
		return
	}
	contact := strings.TrimSpace(u["CONTACT"])
	if !ok || u.Suspended() || contact == "" || !strings.EqualFold(strings.TrimSpace(req.Email), contact) {
		return
	}
	if !s.acct.takeEmailSlot("reset:"+req.User, s.now()) {
		return
	}
	// Hestia's subject (owner ruling 10): translated, then filled with the date, so
	// renderEmail's own translation finds nothing and keeps it.
	lang := firstNonEmpty(u.Language(), cfg["LANGUAGE"], "en")
	subject := fmt.Sprintf(s.acctTranslate(lang, "Password Reset at %s"), s.now().Format("2006-01-02 15:04:05"))
	s.recoveryIssue(ctx, cfg, u, req.User, acctResetPrefix, "reset_password", acctResetEmailBody, subject)
	return
}

func (s *server) resetConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	if cfg["POLICY_SYSTEM_PASSWORD_RESET"] == "no" {
		WriteError(w, http.StatusNotFound, "disabled", "")
		return
	}
	var req ResetConfirmRequest
	if !acctRecoveryDecode(w, r, &req) {
		return
	}
	if req.Password != req.PasswordConfirm {
		WriteError(w, http.StatusBadRequest, "invalid", acctMsgPasswordMatch)
		return
	}
	if !hestia.ValidPassword(req.Password) {
		WriteError(w, http.StatusBadRequest, "invalid", acctMsgPasswordRule)
		return
	}
	if !s.acct.allowConfirm(session.RealIP(r), s.now()) {
		s.recoveryFail(w, r, req.User, acctMsgResetThrottled)
		return
	}
	unlock := acctLockUser(req.User)
	defer unlock()
	res, _, err := s.recoveryCheck(ctx, req.User, acctResetPrefix, req.Code)
	if err != nil {
		serverError(w, err)
		return
	}
	switch res {
	case acctCodeInvalid:
		unlock()
		s.recoveryFail(w, r, req.User, acctMsgResetFailed)
		return
	case acctCodeExpired:
		unlock()
		s.recoveryExpired(w, r, req.User, acctMsgResetExpired)
		return
	}
	if !s.recoveryChangePassword(ctx, req.User, req.Password) {
		WriteError(w, http.StatusInternalServerError, "internal", acctMsgInternal)
		return
	}
	s.Sessions.PasswordChanged(req.User, "")
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) recoveryChangePassword(ctx context.Context, user, password string) bool {
	path, cleanup, err := secret.TempFile(s.tmpDir(), password)
	if err != nil {
		slog.Error("password file failed", "err", err)
		return false
	}
	defer cleanup()
	res, err := s.R.Run(ctx, "v-change-user-password", user, path)
	if err == nil {
		err = res.Err("v-change-user-password")
	}
	if err != nil {
		slog.Error("password reset failed", "user", user, "err", err)
		return false
	}
	return true
}

// ---------------------------------------------------------------- 2FA unlock

func (s *server) unlockRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := s.now()
	var req UnlockRequest
	if !acctRecoveryDecode(w, r, &req) {
		return
	}
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	s.recoveryUnlockMail(ctx, cfg, r, req)
	s.recoveryPad(start) // every path takes the same time
	acctRecoverySent(w)
}

// recoveryUnlockMail issues an unlock code when the password, the account and the
// throttles allow it (VerifyPassword delays and logs a wrong password itself). It
// reports nothing, because the reply never depends on it.
func (s *server) recoveryUnlockMail(ctx context.Context, cfg hestia.SysConfig, r *http.Request, req UnlockRequest) {
	ip := session.RealIP(r)
	if !s.acct.allowRequest(ip, s.now()) {
		return
	}
	sess := session.FromContext(ctx)
	token := ""
	if sess != nil {
		token = sess.Token
	}
	good, err := s.Auth.VerifyPassword(ctx, acctAuthInput(req.User, req.Password, ip, r.UserAgent(), token))
	if err != nil {
		slog.Warn("2FA unlock password check failed", "err", err)
		return
	}
	if !good {
		return
	}
	u, ok, err := s.H.User(ctx, req.User)
	if err != nil {
		slog.Warn("2FA unlock lookup failed", "err", err)
		return
	}
	contact := strings.TrimSpace(u["CONTACT"])
	if !ok || !u.TwoFAEnabled() || u.Suspended() || contact == "" {
		return
	}
	if !s.acct.takeEmailSlot("2fa:"+req.User, s.now()) {
		return
	}
	s.recoveryIssue(ctx, cfg, u, req.User, acctUnlockPrefix, "two_factor_unlock", acctUnlockEmailBody, "Two-factor authentication unlock code")
}

func (s *server) unlockConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req UnlockConfirmRequest
	if !acctRecoveryDecode(w, r, &req) {
		return
	}
	if !s.acct.allowConfirm(session.RealIP(r), s.now()) {
		s.recoveryFail(w, r, req.User, acctMsgUnlockThrottled)
		return
	}
	unlock := acctLockUser(req.User)
	defer unlock()
	res, _, err := s.recoveryCheck(ctx, req.User, acctUnlockPrefix, req.Code)
	if err != nil {
		serverError(w, err)
		return
	}
	switch res {
	case acctCodeInvalid:
		unlock()
		s.recoveryFail(w, r, req.User, acctMsgUnlockFailed)
		return
	case acctCodeExpired:
		unlock()
		s.recoveryExpired(w, r, req.User, acctMsgUnlockExpired)
		return
	}
	out, err := s.R.Run(ctx, "v-delete-user-2fa", req.User)
	if err == nil {
		err = out.Err("v-delete-user-2fa")
	}
	if err != nil {
		slog.Error("2FA unlock failed", "user", req.User, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", acctMsgInternal)
		return
	}
	// 2FA is off; if the code can't be rotated, say so in the log but still succeed.
	out, err = s.R.Run(ctx, "v-change-user-rkey", req.User)
	if err == nil {
		err = out.Err("v-change-user-rkey")
	}
	if err != nil {
		slog.Error("2FA unlock code not rotated", "user", req.User, "err", err)
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func acctAuthInput(user, password, ip, ua, token string) auth.LoginInput {
	return auth.LoginInput{Username: user, Password: password, IP: ip, UserAgent: ua, Fingerprint: token}
}

// acctPanelHost is the host the email links point at: the system hostname plus the
// panel port (BACKEND_PORT) unless that is empty or 443. It comes from the system
// config only, never from the request's Host header.
func acctPanelHost(hostname string, cfg hestia.SysConfig) string {
	if port := strings.TrimSpace(cfg["BACKEND_PORT"]); port != "" && port != "443" {
		return hostname + ":" + port
	}
	return hostname
}

// recoveryPad waits until acctFailedLoginDelay has passed since start, so a request
// that sent mail takes as long as one that did not.
func (s *server) recoveryPad(start time.Time) {
	if d := acctFailedLoginDelay - s.now().Sub(start); d > 0 {
		s.Auth.Delay(d)
	}
}

// acctRecoveryLocks serialises the check-and-act of the confirm steps per user, so one
// code can't be used twice by parallel requests. The locks are striped by a hash of
// the username: a fixed size, whatever names unauthenticated callers send.
var acctRecoveryLocks [64]sync.Mutex

func acctLockIndex(user string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(user))
	return int(h.Sum32() % uint32(len(acctRecoveryLocks)))
}

// acctLockUser locks user's stripe and returns an idempotent unlock, so a handler can
// release it early (before any delay) and still defer it.
func acctLockUser(user string) func() {
	mu := &acctRecoveryLocks[acctLockIndex(user)]
	mu.Lock()
	var once sync.Once
	return func() { once.Do(mu.Unlock) }
}
