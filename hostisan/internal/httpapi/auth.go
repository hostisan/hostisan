package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

func (s *server) bootstrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	var signedOut string
	if sess.User != "" {
		// A reload is a request like any other: an expired session ends here, and the
		// visitor gets a fresh anonymous one and the login page.
		code, msg, err := s.Sessions.Validate(r, sess)
		if err != nil {
			serverError(w, err)
			return
		}
		if code != "" {
			sess, signedOut = s.Sessions.NewAnonymous(w), msg
		}
	} else if msg := s.Sessions.SignedOutReason(sess.ID); msg != "" {
		// a password change in another browser ended this session
		sess, signedOut = s.Sessions.NewAnonymous(w), msg
	}
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	langs, err := s.H.Languages(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	b := Bootstrap{
		CSRFToken: sess.Token,
		AppName:   brand.Rewrite(cfg["APP_NAME"]),
		Version:   s.Version,
		Language:  pickLanguage(langs, sess.Language, cfg["LANGUAGE"]),
		Languages: langs,
		Theme:     firstNonEmpty(sess.Theme, cfg["THEME"]),

		SignedOutReason: signedOut,
		PasswordReset:   cfg["POLICY_SYSTEM_PASSWORD_RESET"] != "no",
		HideDocs:        cfg["HIDE_DOCS"] == "yes",
		ProductURL:      s.Product.URL,
		SecurityEmail:   s.Product.SecurityEmail,
	}
	if sess.User != "" {
		user, err := s.sessionUser(r, sess, cfg)
		if err != nil {
			serverError(w, err)
			return
		}
		if user != nil {
			b.Authenticated, b.User = true, user
		}
	}
	writeJSON(w, http.StatusOK, b)
}

// sessionUser describes the signed-in user; nil if the account no longer exists.
func (s *server) sessionUser(r *http.Request, sess *session.Session, cfg hestia.SysConfig) (*SessionUser, error) {
	effective := s.Sessions.Effective(sess)
	u, ok, err := s.H.User(r.Context(), effective)
	if err != nil || !ok {
		return nil, err
	}
	self := u
	if effective != sess.User {
		if self, ok, err = s.H.User(r.Context(), sess.User); err != nil || !ok {
			return nil, err
		}
	}
	admin := sess.UserContext == "admin"
	apiLevel, _ := strconv.Atoi(cfg["API_SYSTEM"])
	// The real role, as in PHP: impersonating a user doesn't change it.
	server := admin && (cfg["POLICY_SYSTEM_HIDE_SERVICES"] != "yes" || sess.User == rootUser(cfg))
	firewall := server && cfg["FIREWALL_SYSTEM"] == "iptables"
	readOnly := session.ReadOnly(sess, u, cfg)
	// Parity decisions 9 and 14 (and D6: read-only sessions get neither).
	tools := !readOnly && !protectedAdminLook(sess, cfg)
	return &SessionUser{
		Name:        sess.User,
		DisplayName: self["NAME"],
		Contact:     self["CONTACT"],
		Role:        sess.UserContext,
		Look:        sess.Look,
		ReadOnly:    readOnly,
		Landing:     landing(sess, u),
		Usage: Usage{
			DiskUsed:       u["U_DISK"],
			DiskQuota:      u["DISK_QUOTA"],
			BandwidthUsed:  u["U_BANDWIDTH"],
			BandwidthQuota: u["BANDWIDTH"],
		},
		SortOrder: u.SortOrder(),
		Features: Features{
			Web:          cfg["WEB_SYSTEM"] != "" && u["WEB_DOMAINS"] != "0",
			DNS:          cfg["DNS_SYSTEM"] != "" && u["DNS_DOMAINS"] != "0",
			DB:           cfg["DB_SYSTEM"] != "" && u["DATABASES"] != "0",
			Mail:         cfg["MAIL_SYSTEM"] != "" && u["MAIL_DOMAINS"] != "0",
			Cron:         cfg["CRON_SYSTEM"] != "" && u["CRON_JOBS"] != "0",
			Backup:       cfg["BACKUP_SYSTEM"] != "" && (u["BACKUPS"] != "0" || u["U_BACKUPS"] != "0" || u["BACKUPS_INCREMENTAL"] == "yes"),
			Incremental:  u["BACKUPS_INCREMENTAL"] == "yes",
			Logs:         admin || cfg["POLICY_USER_VIEW_LOGS"] != "no",
			LoginHistory: (admin || cfg["POLICY_USER_VIEW_LOGS"] != "no") && cfg["DEMO_MODE"] != "yes",
			AccessKeys: (effective == rootUser(cfg) && apiLevel >= 1) ||
				(effective != rootUser(cfg) && apiLevel >= 2),
			Server:       server,
			Firewall:     firewall,
			FirewallBans: firewall && cfg["FIREWALL_EXTENSION"] != "",
			Terminal:     tools && cfg["WEB_TERMINAL"] == "true" && !strings.HasSuffix(u.Shell(), "nologin"),
			FileManager:  tools && cfg["FILE_MANAGER"] == "true",
		},
		CanEditDetails: admin || cfg["POLICY_USER_EDIT_DETAILS"] == "yes",
		CanChangeTheme: cfg["POLICY_USER_CHANGE_THEME"] != "no",
		CanDeleteLogs:  admin || cfg["POLICY_USER_DELETE_LOGS"] != "no",
		Notifications:  u["NOTIFICATIONS"] == "yes",
		Package:        u["PACKAGE"],
	}, nil
}

// protectedAdminLook: an admin viewing ROOT_USER while POLICY_SYSTEM_PROTECTED_ADMIN
// is on, which hides the web terminal and the file manager (WT26, FM1).
func protectedAdminLook(sess *session.Session, cfg hestia.SysConfig) bool {
	return sess.UserContext == "admin" && sess.Look != "" && sess.Look == rootUser(cfg) &&
		cfg["POLICY_SYSTEM_PROTECTED_ADMIN"] == "yes"
}

// rootUser is the server's root admin account (ROOT_USER), or Hestia's default
// "admin" when the value is missing or blank; otherwise HIDE_SERVICES=yes would hide
// the server pages from every admin.
func rootUser(cfg hestia.SysConfig) string {
	if u := strings.TrimSpace(cfg["ROOT_USER"]); u != "" {
		return u
	}
	return "admin"
}

// landing: admins start on the users list; everyone else (including an admin viewing
// as a user) on the first section their package allows.
func landing(sess *session.Session, effective hestia.User) string {
	if sess.UserContext == "admin" && sess.Look == "" {
		return "/users"
	}
	return effective.LandingPath()
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", badRequestMsg)
		return
	}
	username, password := req.Username, req.Password
	// The 2FA step sends only the code; the password checked a moment ago is kept in
	// memory on the session (PHP kept it in $_SESSION["login"]).
	if req.TwoFA != "" && username == "" && sess.Pending != nil && time.Now().Before(sess.Pending.Expires) {
		username, password = sess.Pending.Username, sess.Pending.Password
	}

	res, err := s.Auth.Login(ctx, auth.LoginInput{
		Username:    username,
		Password:    password,
		TwoFA:       req.TwoFA,
		IP:          session.RealIP(r),
		UserAgent:   r.UserAgent(),
		Fingerprint: sess.Token,
	})
	if err != nil {
		serverError(w, err)
		return
	}

	switch res.Outcome {
	case auth.LoginNeedTwoFA:
		pending := &session.PendingLogin{Username: username, Password: password, Expires: time.Now().Add(session.PendingLoginTTL)}
		// The first time a session is stored: anonymous ones live only in the cookie.
		if !s.Sessions.Store.Update(sess.ID, func(cur *session.Session) { cur.Pending = pending }) {
			sess.Pending = pending
			s.Sessions.Store.Save(sess)
		}
		writeJSON(w, http.StatusOK, StatusReply{Status: "twofa_required"})
	case auth.LoginFailed:
		WriteError(w, http.StatusUnauthorized, "login_failed", res.Message)
	case auth.LoginOK:
		if err := s.startSession(w, r, sess, res); err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, StatusReply{Status: "ok", Landing: landing(sess, res.User)})
	}
}

// startSession fills the session from the user record and gives it a new ID, keeping
// the CSRF token, as web/login/index.php does with session_regenerate_id.
func (s *server) startSession(w http.ResponseWriter, r *http.Request, sess *session.Session, res auth.LoginResult) error {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		return err
	}
	langs, err := s.H.Languages(r.Context())
	if err != nil {
		return err
	}
	sess.User = res.Username
	sess.UserContext = res.User.Role()
	sess.Look = ""
	sess.LastActivity = time.Now()
	sess.CombinedIP = session.RealIP(r)
	// An empty LANGUAGE (what v-add-user writes) means the system's.
	sess.Language = pickLanguage(langs, res.User.Language(), cfg["LANGUAGE"])
	sess.Theme = ""
	if cfg["POLICY_USER_CHANGE_THEME"] == "yes" {
		sess.Theme = res.User.Theme()
	}
	sess.SortOrder = res.User.SortOrder()
	sess.Pending = nil
	sess.PasswordMark = "" // recorded by its first request
	s.Sessions.SetCookie(w, s.Sessions.Store.Regenerate(sess))
	return nil
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	if sess.Look != "" {
		s.logAction(r, "system", "Warning", "Security",
			fmt.Sprintf("User impersonation session ended (User: %s, Administrator: %s)", sess.Look, sess.User))
		s.Sessions.Store.Update(sess.ID, func(cur *session.Session) { cur.Look = "" })
		s.tools.lookChanged(sess.ID)
		writeJSON(w, http.StatusOK, StatusReply{Status: "returned"})
		return
	}
	s.Sessions.LogLogout(ctx, sess)
	s.Sessions.Store.Delete(sess.ID)
	http.SetCookie(w, &http.Cookie{Name: session.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: !s.Sessions.Dev, SameSite: http.SameSiteLaxMode})
	// FileGator's PHP session cookie goes too (its file is deleted through the
	// session-end hook, decision 16).
	http.SetCookie(w, &http.Cookie{Name: phpSessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: !s.Sessions.Dev, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, StatusReply{Status: "logged_out"})
}

func (s *server) loginAs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	var req LoginAsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", badRequestMsg)
		return
	}
	if !hestia.ValidUsername(req.User) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	u, ok, err := s.H.User(ctx, req.User)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	s.logAction(r, sess.User, "Info", "Security", fmt.Sprintf("Logged in as another user (User: %s)", req.User))
	s.logAction(r, "system", "Warning", "Security",
		fmt.Sprintf("User impersonation session started (User: %s, Administrator: %s)", req.User, sess.User))
	sess.Look = req.User
	s.Sessions.Store.Update(sess.ID, func(cur *session.Session) { cur.Look = req.User })
	s.tools.lookChanged(sess.ID)
	writeJSON(w, http.StatusOK, StatusReply{Status: "ok", Landing: landing(sess, u)})
}

// logAction writes Hestia's action log (v-log-action user level category message).
func (s *server) logAction(r *http.Request, user, level, category, message string) {
	res, err := s.R.Run(r.Context(), "v-log-action", user, level, category, message)
	if err == nil {
		err = res.Err("v-log-action")
	}
	if err != nil {
		slog.Warn("action log failed", "err", err)
	}
}

// pickLanguage is the first candidate that is a known language, else "en". The
// candidates go from the most specific: the session's or user's own LANGUAGE (Hestia's
// web/login/index.php), then hestia.conf's (web/inc/main.php, "Detect language").
func pickLanguage(langs []string, candidates ...string) string {
	for _, c := range candidates {
		if c != "" && slices.Contains(langs, c) {
			return c
		}
	}
	return "en"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
