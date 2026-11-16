package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
	"github.com/hostisan/hostisan/internal/totp"
)

// profileRoutes registers the account page, user editing and 2FA enrolment (Plan 3 Slice B). Called from registerAccountRoutes.
func (s *server) profileRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/account", s.read(s.accountGet))
	mux.Handle("GET /api/v1/account/options", s.read(s.accountOptions))
	mux.Handle("PUT /api/v1/account", s.write(s.accountSave))
	mux.Handle("POST /api/v1/account/2fa/setup", s.write(s.twofaSetup))
	mux.Handle("POST /api/v1/account/2fa/confirm", s.write(s.twofaConfirm))
	mux.Handle("DELETE /api/v1/account/2fa", s.write(s.twofaDisable))
	mux.Handle("POST /api/v1/users", s.acctPageWrite(s.userAdd))
	mux.Handle("GET /api/v1/users/{user}", s.acctPageRead(s.userGet))
	mux.Handle("PUT /api/v1/users/{user}", s.acctPageWrite(s.userSave))
	mux.Handle("DELETE /api/v1/users/{user}/2fa", s.acctPageWrite(s.userTwofaDisable))
}

const (
	pending2FATTL     = 10 * time.Minute
	invalidNameMsg    = "Please enter a valid contact name."
	wrongPasswordMsg  = "The current password is not correct."
	wrongCodeMsg      = "The code is not correct."
	setup2FAAgainMsg  = "Start setting up two-factor authentication again."
	twofaAlreadyOnMsg = "Error: 2FA already enabled"
	tooManyNSMsg      = "Up to 8 name servers are allowed."
	// invalidNameQuotesMsg is is_name_format_valid's refusal (sic, as the bin prints it).
	invalidNameQuotesMsg = `Error: Invalid name contains qoutes (" or ') :: %s`
	welcomeMailBody      = "Hello {{name}},\n\nYour account has been created and ready to use.\n\nhttps://{{hostname}}/login/\nUsername: {{user}}\nPassword: {{password}}\n\nBest regards,\n\n--\n{{appname}}"
	currentPasswordLbl   = "Current Password"
)

var (
	roleOK  = []string{"user", "admin", "dns-cluster"}
	wordRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	phpVerR = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

// userDetailOf builds the form's view of a user from v-list-user's map.
func userDetailOf(name string, u hestia.User) hestia.UserDetail {
	return hestia.UserDetail{
		USER: name, NAME: u["NAME"], PACKAGE: u["PACKAGE"], NS: u["NS"], SHELL: u["SHELL"],
		CONTACT: u["CONTACT"], ROLE: u["ROLE"], SUSPENDED: u["SUSPENDED"], LANGUAGE: u["LANGUAGE"],
		THEME: u["THEME"], PREF_UI_SORT: u["PREF_UI_SORT"], LOGIN_DISABLED: u["LOGIN_DISABLED"],
		LOGIN_USE_IPLIST: u["LOGIN_USE_IPLIST"], LOGIN_ALLOW_IPS: u["LOGIN_ALLOW_IPS"],
		PHPCLI: u["PHPCLI"], TIME: u["TIME"], DATE: u["DATE"],
		DISK_QUOTA: u["DISK_QUOTA"], BANDWIDTH: u["BANDWIDTH"], WEB_DOMAINS: u["WEB_DOMAINS"],
		DNS_DOMAINS: u["DNS_DOMAINS"], MAIL_DOMAINS: u["MAIL_DOMAINS"], DATABASES: u["DATABASES"],
		CRON_JOBS: u["CRON_JOBS"], BACKUPS: u["BACKUPS"],
	}
}

// profileTarget loads the edited account. It answers the request itself (false) when the
// user is missing or is the ROOT_USER and the signed-in admin is not (Parity 5).
func (s *server) profileTarget(w http.ResponseWriter, r *http.Request, cfg hestia.SysConfig, name string) (hestia.User, bool) {
	sess := session.FromContext(r.Context())
	if root := rootUser(cfg); root != "" && sess.UserContext == "admin" && name == root && sess.User != root {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return nil, false
	}
	u, ok, err := s.H.User(r.Context(), name)
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return nil, false
	}
	return u, true
}

func (s *server) accountView(w http.ResponseWriter, r *http.Request, name string, self bool) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok := s.profileTarget(w, r, cfg, name)
	if !ok {
		return
	}
	admin := session.FromContext(r.Context()).UserContext == "admin"
	writeJSON(w, http.StatusOK, AccountView{
		User: userDetailOf(name, u), TwoFA: u.TwoFAEnabled(), Self: self,
		Editable: AccountEditable{
			Details: admin || cfg["POLICY_USER_EDIT_DETAILS"] == "yes",
			Theme:   cfg["POLICY_USER_CHANGE_THEME"] != "no",
			Admin:   admin,
		},
	})
}

func (s *server) accountGet(w http.ResponseWriter, r *http.Request) {
	s.accountView(w, r, s.actingUser(r), acctIsSelf(r))
}

func (s *server) userGet(w http.ResponseWriter, r *http.Request) {
	name, ok := acctUserParam(w, r)
	if !ok {
		return
	}
	s.accountView(w, r, name, name == session.FromContext(r.Context()).User)
}

// defaultPHP is the panel's PHP version without the "php-" prefix (users §2.3).
func defaultPHP(cfg hestia.SysConfig) string {
	v := cfg["DEFAULT_PHP_VERSION"]
	if len(v) > 4 {
		return v[4:]
	}
	return ""
}

func (s *server) accountOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	sess := session.FromContext(ctx)
	admin := sess.UserContext == "admin"
	out := AccountOptions{
		Languages: []string{}, Themes: []string{}, Packages: []string{}, Shells: []string{}, PHPVersions: []string{},
		DNSSystem:      cfg["DNS_SYSTEM"] != "",
		LogsVisible:    admin || cfg["POLICY_USER_VIEW_LOGS"] != "no",
		SystemLanguage: firstNonEmpty(cfg["LANGUAGE"], "en"),
	}
	api, _ := strconv.Atoi(cfg["API_SYSTEM"])
	eff := s.actingUser(r)
	out.APIAccess = (eff == rootUser(cfg) && api >= 1) || (eff != rootUser(cfg) && api >= 2)
	if out.Languages, err = s.H.Languages(ctx); err != nil {
		serverError(w, err)
		return
	}
	if cfg["POLICY_USER_CHANGE_THEME"] != "no" {
		if !s.listJSON(w, ctx, &out.Themes, "v-list-sys-themes", "json") {
			return
		}
		out.Themes = nonNil(out.Themes)
	}
	if admin {
		out.DefaultPHP = defaultPHP(cfg)
		if !s.listJSON(w, ctx, &out.Shells, "v-list-sys-shells", "json") || !s.listJSON(w, ctx, &out.PHPVersions, "v-list-sys-php", "json") {
			return
		}
		out.Shells, out.PHPVersions = nonNil(out.Shells), nonNil(out.PHPVersions)
		stdout, ok := s.output(w, r, "v-list-user-packages", "json")
		if !ok {
			return
		}
		pk, err := hestia.DecodeObjects[hestia.PackageEntry](stdout)
		if err != nil {
			serverError(w, err)
			return
		}
		for _, p := range pk {
			out.Packages = append(out.Packages, p.NAME)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- save

func (s *server) accountSave(w http.ResponseWriter, r *http.Request) {
	var req UserEditRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.profileSave(w, r, s.actingUser(r), acctIsSelf(r), req)
}

func (s *server) userSave(w http.ResponseWriter, r *http.Request) {
	name, ok := acctUserParam(w, r)
	if !ok {
		return
	}
	var req UserEditRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.profileSave(w, r, name, name == session.FromContext(r.Context()).User, req)
}

// compactNS trims spaces and dots and drops blanks after the second entry; the first
// two are required and checked by the caller.
func compactNS(in []string) []string {
	out := []string{}
	for i, n := range in {
		n = strings.Trim(strings.TrimSpace(n), ".")
		if n != "" || i < 2 {
			out = append(out, n)
		}
	}
	return out
}

func forbidden(w http.ResponseWriter) { WriteError(w, http.StatusForbidden, "forbidden", "") }

func invalid(w http.ResponseWriter, msg string, args ...string) {
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: msg, Args: args}})
}

// profileSave is Parity 8 to 10: authorise, validate, then run only what changed in the
// ruled order, stopping at the first error.
func (s *server) profileSave(w http.ResponseWriter, r *http.Request, target string, self bool, req UserEditRequest) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	cur, ok := s.profileTarget(w, r, cfg, target)
	if !ok {
		return
	}
	admin := sess.UserContext == "admin"

	// An empty value is "not offered" for these admin fields (the form omits them when
	// the section was never filled), the same as a missing one.
	if req.Shell != nil && *req.Shell == "" {
		req.Shell = nil
	}
	if req.PHPCli != nil && *req.PHPCli == "" {
		req.PHPCli = nil
	}
	if len(req.Nameservers) == 0 {
		req.Nameservers = nil
	}

	// Authorisation: 403 with no command.
	if !admin && (req.LoginDisabled != nil || req.Package != nil || req.Role != nil || req.Shell != nil ||
		req.PHPCli != nil || req.Nameservers != nil) {
		forbidden(w)
		return
	}
	if req.Role != nil && *req.Role != cur["ROLE"] && (target == rootUser(cfg) || target == sess.User) {
		forbidden(w)
		return
	}
	nameChanged, emailChanged := req.Name != cur["NAME"], req.Email != cur["CONTACT"]
	if !admin && cfg["POLICY_USER_EDIT_DETAILS"] != "yes" && (nameChanged || emailChanged) {
		forbidden(w)
		return
	}
	themeChanged := req.Theme != "" && req.Theme != cur.Theme()
	if themeChanged && cfg["POLICY_USER_CHANGE_THEME"] == "no" {
		forbidden(w)
		return
	}

	// Validation: 400 with no command.
	newPW := req.Password != ""
	if newPW && !hestia.ValidPassword(req.Password) {
		invalid(w, passwordRuleMsg)
		return
	}
	if emailChanged && !validEmail(req.Email) {
		invalid(w, invalidEmailMsg)
		return
	}
	if nameChanged && strings.TrimSpace(req.Name) == "" {
		invalid(w, invalidNameMsg)
		return
	}
	var ns []string
	nsSend := false
	if admin && req.Nameservers != nil && cfg["DNS_SYSTEM"] != "" {
		raw := req.Nameservers
		for i, label := range []string{"Nameserver 1", "Nameserver 2"} {
			if i >= len(raw) || strings.Trim(strings.TrimSpace(raw[i]), ".") == "" {
				writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: blankFieldMsg, Args: []string{label}}})
				return
			}
		}
		ns = compactNS(raw)
		if len(ns) > 8 {
			invalid(w, tooManyNSMsg)
			return
		}
		for _, n := range ns {
			if !validHostname(n) {
				invalid(w, "Error: invalid NS format :: "+n)
				return
			}
		}
		nsSend = strings.Join(ns, ",") != cur["NS"]
	}
	if req.Role != nil && !slices.Contains(roleOK, *req.Role) {
		invalidFormat(w, "role", *req.Role)
		return
	}
	if req.Package != nil && !hestia.ValidPackageArg(*req.Package) {
		invalidFormat(w, "package", *req.Package)
		return
	}
	switch {
	case req.Shell != nil && !wordRE.MatchString(*req.Shell):
		invalidFormat(w, "shell", *req.Shell)
		return
	case req.PHPCli != nil && !phpVerR.MatchString(*req.PHPCli):
		invalidFormat(w, "php version", *req.PHPCli)
		return
	case req.Theme != "" && !wordRE.MatchString(req.Theme):
		invalidFormat(w, "theme", req.Theme)
		return
	case req.SortOrder != "" && req.SortOrder != "name" && req.SortOrder != "date":
		invalidFormat(w, "sort_order", req.SortOrder)
		return
	}
	langChanged := req.Language != "" && req.Language != cur.Language()
	if langChanged {
		langs, err := s.H.Languages(ctx)
		if err != nil {
			serverError(w, err)
			return
		}
		if !slices.Contains(langs, req.Language) {
			invalidFormat(w, "language", req.Language)
			return
		}
	}
	if req.UseIPList && len(req.AllowedIPs) == 0 {
		blankFields(w, "Allowed IP addresses", "")
		return
	}
	for _, ip := range req.AllowedIPs {
		if !validAllowedIP(ip) {
			invalid(w, "Error: invalid LOGIN_ALLOW_IPS format :: "+ip)
			return
		}
	}
	needCurrent := newPW && self
	if needCurrent {
		if req.CurrentPassword == "" {
			blankFields(w, currentPasswordLbl, "")
			return
		}
		if !s.reauth(w, r, sess, req.CurrentPassword) {
			return
		}
	}

	// Commands, in Hestia's order.
	var steps [][]string
	add := func(cmd string, args ...string) { steps = append(steps, append([]string{cmd, target}, args...)) }
	if req.SortOrder != "" && req.SortOrder != cur.SortOrder() {
		add("v-change-user-sort-order", req.SortOrder)
	}
	if req.LoginDisabled != nil && *req.LoginDisabled != cur.LoginDisabled() {
		add("v-change-user-config-value", "LOGIN_DISABLED", yesNo(*req.LoginDisabled))
	}
	allowed := strings.Join(req.AllowedIPs, ",")
	switch {
	case req.UseIPList != cur.UsesIPList():
		// Turning the list on writes the addresses first, so a failed step never leaves
		// the list on with an empty or stale set.
		if !req.UseIPList {
			add("v-change-user-config-value", "LOGIN_USE_IPLIST", "no")
			if cur["LOGIN_ALLOW_IPS"] != "" {
				add("v-change-user-config-value", "LOGIN_ALLOW_IPS", "")
			}
		} else {
			add("v-change-user-config-value", "LOGIN_ALLOW_IPS", allowed)
			add("v-change-user-config-value", "LOGIN_USE_IPLIST", "yes")
		}
	case req.UseIPList && allowed != cur["LOGIN_ALLOW_IPS"]:
		add("v-change-user-config-value", "LOGIN_ALLOW_IPS", allowed)
	}

	// The password goes first; the package command is long.
	run := func() bool {
		if newPW {
			if err := s.withSecret(r, req.Password, "v-change-user-password", []string{target}, 1); err != nil {
				writeCommandError(w, err)
				return false
			}
			// The password changed: end the user's other sessions now, even if a later step fails.
			except := ""
			if target == sess.User {
				except = sess.ID
			}
			s.Sessions.PasswordChanged(target, except)
		}
		if !s.steps(w, r, steps) {
			return false
		}
		long := r.WithContext(runner.WithTimeout(ctx, acctUserCommandTimeout))
		var later [][]string
		addLater := func(cmd string, args ...string) { later = append(later, append([]string{cmd, target}, args...)) }
		if req.Package != nil && *req.Package != cur["PACKAGE"] {
			if !s.steps(w, long, [][]string{{"v-change-user-package", target, *req.Package}}) {
				return false
			}
		}
		if req.PHPCli != nil && *req.PHPCli != firstNonEmpty(cur["PHPCLI"], defaultPHP(cfg)) {
			addLater("v-change-user-php-cli", *req.PHPCli)
		}
		if req.Role != nil && *req.Role != cur["ROLE"] {
			addLater("v-change-user-role", *req.Role)
		}
		if req.Shell != nil && *req.Shell != cur["SHELL"] {
			addLater("v-change-user-shell", *req.Shell)
		}
		if langChanged {
			addLater("v-change-user-language", req.Language)
		}
		if emailChanged {
			addLater("v-change-user-contact", req.Email)
		}
		if nameChanged {
			addLater("v-change-user-name", req.Name)
		}
		if themeChanged {
			addLater("v-change-user-theme", req.Theme)
		}
		if nsSend {
			addLater("v-change-user-ns", ns...)
		}
		return s.steps(w, r, later)
	}
	if !run() {
		return
	}

	if target == sess.User {
		s.Sessions.Store.Update(sess.ID, func(c *session.Session) {
			if langChanged {
				c.Language = req.Language
			}
			if req.SortOrder != "" {
				c.SortOrder = req.SortOrder
			}
			if themeChanged {
				c.Theme = req.Theme
			}
		})
	}
	writeJSON(w, http.StatusOK, AccountSaved{Status: "ok", LanguageChanged: langChanged})
}

// validAllowedIP accepts a plain IPv4 address: the core's format check refuses ":" and
// "/", and login compares exact addresses, so a range or IPv6 entry would lock the user out.
func validAllowedIP(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

// reauth checks the signed-in user's own password; a wrong one is logged as a failed
// login and answered 400. False means the request has been answered.
func (s *server) reauth(w http.ResponseWriter, r *http.Request, sess *session.Session, password string) bool {
	ok, err := s.Auth.VerifyPassword(r.Context(), auth.LoginInput{
		Username: sess.User, Password: password, IP: session.RealIP(r), UserAgent: r.UserAgent(), Fingerprint: sess.Token,
	})
	if err != nil {
		serverError(w, err)
		return false
	}
	if !ok {
		invalid(w, wrongPasswordMsg)
	}
	return ok
}

// ---------------------------------------------------------------- add user

func (s *server) userAdd(w http.ResponseWriter, r *http.Request) {
	var req AddUserRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !blankFields(w, "Username", req.Username, "Password", req.Password, "Package", req.Package,
		"Email", req.Email, "Contact Name", req.Name) {
		return
	}
	if !validEmail(req.Email) {
		invalid(w, invalidEmailMsg)
		return
	}
	if !hestia.ValidPassword(req.Password) {
		invalid(w, passwordRuleMsg)
		return
	}
	switch {
	case !hestia.ValidUsername(req.Username):
		invalidFormat(w, "user", req.Username)
		return
	case !hestia.ValidPackageArg(req.Package):
		invalidFormat(w, "package", req.Package)
		return
	case !slices.Contains(roleOK, req.Role):
		invalidFormat(w, "role", req.Role)
		return
	case strings.ContainsAny(req.Name, "'\"<>"):
		invalid(w, invalidNameQuotesMsg, req.Name)
		return
	case strings.ContainsAny(req.Name, "\r\n"):
		invalid(w, invalidNameMsg)
		return
	}
	if req.Notify != "" && !validEmail(req.Notify) {
		invalid(w, invalidEmailMsg)
		return
	}
	ctx := r.Context()
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
	lang := firstNonEmpty(req.Language, cfg["LANGUAGE"], "en")
	if !slices.Contains(langs, lang) {
		invalidFormat(w, "language", lang)
		return
	}
	long := r.WithContext(runner.WithTimeout(ctx, acctUserCommandTimeout))
	if err := s.withSecret(long, req.Password, "v-add-user", []string{req.Username, req.Email, req.Package, req.Name}, 1); err != nil {
		writeCommandError(w, err)
		return
	}
	steps := [][]string{
		{"v-change-user-language", req.Username, lang},
		{"v-change-user-role", req.Username, req.Role},
	}
	if req.LoginDisabled {
		steps = append(steps, []string{"v-change-user-config-value", req.Username, "LOGIN_DISABLED", "yes"})
	}
	if !s.steps(w, r, steps) {
		return
	}
	if req.Notify != "" {
		s.sendWelcome(ctx, cfg, lang, req)
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// sendWelcome emails the new account's login details. A failure is logged, never
// reported, and never includes the password.
func (s *server) sendWelcome(ctx context.Context, cfg hestia.SysConfig, lang string, req AddUserRequest) {
	if s.Mailer == nil {
		slog.Warn("welcome email not sent: no mailer configured", "user", req.Username)
		return
	}
	app := brand.Rewrite(cfg["APP_NAME"])
	msg := s.renderEmail(ctx, cfg, lang, "account_ready", welcomeMailBody, "Welcome to %s",
		map[string]string{"user": req.Username, "name": req.Name},
		map[string]string{"name": req.Name, "user": req.Username, "password": req.Password,
			"hostname": acctPanelHost(s.acctHostname(), cfg)})
	// The default subject has a %s for the application name.
	if strings.Contains(msg.Subject, "%s") {
		msg.Subject = strings.Replace(msg.Subject, "%s", app, 1)
	}
	msg.To = req.Notify
	if err := s.Mailer.Send(ctx, cfg, msg); err != nil {
		slog.Warn("welcome email failed", "user", req.Username, "err", err)
	}
}

// ---------------------------------------------------------------- 2FA

func (s *server) twofaSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	if !acctIsSelf(r) {
		forbidden(w)
		return
	}
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, sess.User)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if u.TwoFAEnabled() {
		WriteError(w, http.StatusConflict, "exists", twofaAlreadyOnMsg)
		return
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		serverError(w, err)
		return
	}
	url := totp.URL(brand.Rewrite(cfg["APP_NAME"]), sess.User, secret)
	qr, err := totp.PNGDataURI(url)
	if err != nil {
		serverError(w, err)
		return
	}
	s.acct.mu.Lock()
	if s.acct.pending2FA == nil {
		s.acct.pending2FA = map[string]acctPending2FA{}
	}
	for id, p := range s.acct.pending2FA {
		if !s.now().Before(p.Expires) {
			delete(s.acct.pending2FA, id)
		}
	}
	s.acct.pending2FA[sess.ID] = acctPending2FA{User: sess.User, Secret: secret, Expires: s.now().Add(pending2FATTL)}
	s.acct.mu.Unlock()
	writeJSON(w, http.StatusOK, TwoFASetup{Secret: secret, OtpauthURL: url, QR: qr})
}

func (s *server) twofaConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	if !acctIsSelf(r) {
		forbidden(w)
		return
	}
	var req TwoFAConfirmRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.acct.mu.Lock()
	p, ok := s.acct.pending2FA[sess.ID]
	if ok && (p.User != sess.User || !s.now().Before(p.Expires)) {
		delete(s.acct.pending2FA, sess.ID)
		ok = false
	}
	s.acct.mu.Unlock()
	if !ok {
		invalid(w, setup2FAAgainMsg)
		return
	}
	if req.CurrentPassword == "" {
		blankFields(w, currentPasswordLbl, "")
		return
	}
	if !s.reauth(w, r, sess, req.CurrentPassword) {
		return
	}
	if !totp.Verify(p.Secret, strings.TrimSpace(req.Code), s.now()) {
		invalid(w, wrongCodeMsg)
		return
	}
	if u, found, err := s.H.User(ctx, sess.User); err != nil {
		serverError(w, err)
		return
	} else if found && u.TwoFAEnabled() {
		s.acct.mu.Lock()
		delete(s.acct.pending2FA, sess.ID)
		s.acct.mu.Unlock()
		WriteError(w, http.StatusConflict, "exists", twofaAlreadyOnMsg)
		return
	}
	s.acct.mu.Lock()
	delete(s.acct.pending2FA, sess.ID)
	s.acct.mu.Unlock()
	// The secret is in this command's argv: only root can read sudo's log (Parity 11).
	if !s.steps(w, r, [][]string{{"v-change-user-config-value", sess.User, "TWOFA", p.Secret}}) {
		return
	}
	s.logAction(r, sess.User, "Info", "System", "Two-factor authentication enabled.")
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) twofaDisable(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if acctIsSelf(r) {
		var req PasswordConfirmRequest
		if !decodeBody(w, r, &req) {
			return
		}
		if req.CurrentPassword == "" {
			blankFields(w, currentPasswordLbl, "")
			return
		}
		if !s.reauth(w, r, sess, req.CurrentPassword) {
			return
		}
	}
	if s.steps(w, r, [][]string{{"v-delete-user-2fa", s.actingUser(r)}}) {
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

func (s *server) userTwofaDisable(w http.ResponseWriter, r *http.Request) {
	name, ok := acctUserParam(w, r)
	if !ok {
		return
	}
	// Your own 2FA goes off through DELETE /account/2fa, with the password.
	if name == session.FromContext(r.Context()).User {
		forbidden(w)
		return
	}
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if _, ok := s.profileTarget(w, r, cfg, name); !ok {
		return
	}
	if s.steps(w, r, [][]string{{"v-delete-user-2fa", name}}) {
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}
