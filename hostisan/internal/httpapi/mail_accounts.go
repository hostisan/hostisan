package httpapi

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/session"
)

func (s *server) mailAccountRoutes(mux *http.ServeMux) {
	const base = "/api/v1/mail/{domain}/accounts"
	mux.Handle("GET "+base, s.read(s.mailAccountList))
	mux.Handle("GET "+base+"/{account}", s.read(s.mailAccountOne))
	mux.Handle("POST "+base, s.write(s.mailAccountAdd))
	mux.Handle("PUT "+base+"/{account}", s.write(s.mailAccountEdit))
	mux.Handle("DELETE "+base+"/{account}", s.write(s.mailAccountAction("v-delete-mail-account")))
	mux.Handle("POST "+base+"/{account}/suspend", s.adminWrite(s.mailAccountAction("v-suspend-mail-account")))
	mux.Handle("POST "+base+"/{account}/unsuspend", s.adminWrite(s.mailAccountAction("v-unsuspend-mail-account")))
	mux.Handle("POST "+base+"/bulk", s.write(s.mailAccountBulk))
}

// maxAutoreply is Hestia's limit: the message must be shorter than this many characters.
const maxAutoreply = 10240

const credentialsMailBody = "Mail account has been created.\n\nCommon Account Settings:\nUsername: {{account}}@{{domain}}\nPassword: {{password}}\nWebmail: {{webmail}}\nHostname: {{hostname}}\n\nIMAP Settings\nAuthentication: Normal Password\nSSL/TLS: Port 993\nSTARTTLS: Port 143\nNo encryption: Port 143\n\nPOP3 Settings\nAuthentication: Normal Password\nSSL/TLS: Port 995\nSTARTTLS: Port 110\nNo encryption: Port 110\n\nSMTP Settings\nAuthentication: Normal Password\nSSL/TLS: Port 465\nSTARTTLS: Port 587\nNo encryption: Port 25\n\nBest regards,\n\n--\n{{appname}}"

// splitWords splits textarea text on newlines, commas and whitespace, as PHP did.
func splitWords(v string) []string {
	return strings.Fields(strings.NewReplacer("\n", " ", "\r", " ", ",", " ").Replace(v))
}

// notIn is the items of a that are not in b, in a's order.
func notIn(a, b []string) []string {
	have := make(map[string]bool, len(b))
	for _, x := range b {
		have[x] = true
	}
	var out []string
	for _, x := range a {
		if !have[x] {
			out = append(out, x)
		}
	}
	return out
}

// quotaArg is the quota as Hestia takes it: "" is unlimited, sent as 0.
func quotaArg(q string) string {
	if q = strings.TrimSpace(q); q == "" {
		return "0"
	}
	return q
}

// sameQuota compares quotas the way Hestia stores them: "", "0" and "unlimited" are one.
func sameQuota(a, b string) bool {
	canon := func(q string) string {
		if q = strings.TrimSpace(q); q == "" || q == "0" {
			return "unlimited"
		}
		return q
	}
	return canon(a) == canon(b)
}

func isAdminContext(r *http.Request) bool {
	return session.FromContext(r.Context()).UserContext == "admin"
}

// mailAccount lists one account (v-list-mail-account), answering the request on failure.
func (s *server) mailAccount(w http.ResponseWriter, r *http.Request, user, domain, account string) (hestia.MailAccountDetail, bool) {
	out, ok := s.output(w, r, "v-list-mail-account", user, domain, account, "json")
	if !ok {
		return hestia.MailAccountDetail{}, false
	}
	as, err := hestia.DecodeObjects[hestia.MailAccountDetail](out)
	if err != nil || len(as) != 1 {
		serverError(w, firstErr(err, "v-list-mail-account: unexpected output"))
		return hestia.MailAccountDetail{}, false
	}
	return as[0], true
}

// mailAutoreplyMessage is the stored autoreply text with Hestia's literal "\n" turned
// into newlines.
func (s *server) mailAutoreplyMessage(w http.ResponseWriter, r *http.Request, user, domain, account string) (string, bool) {
	out, ok := s.output(w, r, "v-list-mail-account-autoreply", user, domain, account, "json")
	if !ok {
		return "", false
	}
	rs, err := hestia.DecodeObjects[hestia.MailAutoreply](out)
	if err != nil || len(rs) != 1 {
		serverError(w, firstErr(err, "v-list-mail-account-autoreply: unexpected output"))
		return "", false
	}
	return strings.ReplaceAll(rs[0].MSG, `\n`, "\n"), true
}

func (s *server) mailAccountList(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	d, ok := s.mailDomain(w, r, user, domain)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-list-mail-accounts", user, domain, "json")
	if !ok {
		return
	}
	accounts, err := hestia.DecodeObjects[hestia.MailAccount](out)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, MailAccounts{Domain: d, Accounts: accounts})
}

func (s *server) mailAccountOne(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	account, ok := mailAccountParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	a, ok := s.mailAccount(w, r, user, domain, account)
	if !ok {
		return
	}
	view := MailAccountView{Account: a}
	if a.AUTOREPLY == "yes" {
		if view.AutoreplyMessage, ok = s.mailAutoreplyMessage(w, r, user, domain, account); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *server) mailAccountAdd(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req AddMailAccountRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	pairs := []string{"Account", req.Account}
	if !req.ForwardOnly {
		pairs = append(pairs, "Password", req.Password)
	}
	if !blankFields(w, pairs...) {
		return
	}
	if !hestia.ValidMailAccountArg(req.Account) {
		// Hestia's own wording for a bad account name.
		WriteError(w, http.StatusBadRequest, "invalid", "Error: invalid account format :: "+req.Account)
		return
	}
	sendTo := strings.TrimSpace(req.SendTo)
	if sendTo != "" && !validEmail(sendTo) {
		WriteError(w, http.StatusBadRequest, "invalid", invalidEmailMsg)
		return
	}
	if !req.ForwardOnly && !hestia.ValidPassword(req.Password) {
		WriteError(w, http.StatusBadRequest, "invalid", passwordRuleMsg)
		return
	}
	user, account := s.actingUser(r), req.Account
	// The password file is always sent, empty for a forward-only account.
	err := s.withSecret(r, req.Password, "v-add-mail-account",
		[]string{user, domain, account, quotaArg(req.Quota)}, 3)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	var steps [][]string
	for _, a := range splitWords(req.Aliases) {
		steps = append(steps, []string{"v-add-mail-account-alias", user, domain, account, a})
	}
	if req.Discard {
		steps = append(steps, []string{"v-add-mail-account-forward", user, domain, account, ":blackhole:"})
	} else {
		for _, f := range splitWords(req.Forwards) {
			steps = append(steps, []string{"v-add-mail-account-forward", user, domain, account, f})
		}
	}
	if req.ForwardOnly {
		steps = append(steps, []string{"v-add-mail-account-fwd-only", user, domain, account})
	}
	if req.Rate != nil && *req.Rate != "" && isAdminContext(r) {
		steps = append(steps, []string{"v-change-mail-account-rate-limit", user, domain, account, *req.Rate})
	}
	if !s.steps(w, r, steps) {
		return
	}
	if sendTo != "" {
		s.sendMailCredentials(r.Context(), sendTo, domain, account, req.Password)
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) mailAccountEdit(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	account, ok := mailAccountParam(w, r)
	if !ok {
		return
	}
	var req EditMailAccountRequest
	if !decodeBody(w, r, &req) {
		return
	}
	sendTo := strings.TrimSpace(req.SendTo)
	if sendTo != "" && !validEmail(sendTo) {
		WriteError(w, http.StatusBadRequest, "invalid", invalidEmailMsg)
		return
	}
	if req.Password != "" && !hestia.ValidPassword(req.Password) {
		WriteError(w, http.StatusBadRequest, "invalid", passwordRuleMsg)
		return
	}
	msg := strings.ReplaceAll(req.AutoreplyMessage, "\r\n", "\n")
	if req.Autoreply {
		if !blankFields(w, "Message", msg) {
			return
		}
		if utf8.RuneCountInString(msg) >= maxAutoreply {
			WriteError(w, http.StatusBadRequest, "invalid", "Autoreply message is too long.")
			return
		}
	}
	user := s.actingUser(r)
	cur, ok := s.mailAccount(w, r, user, domain, account)
	if !ok {
		return
	}
	oldMsg := ""
	if req.Autoreply && cur.AUTOREPLY == "yes" {
		if oldMsg, ok = s.mailAutoreplyMessage(w, r, user, domain, account); !ok {
			return
		}
	}

	if req.Password != "" {
		if err := s.withSecret(r, req.Password, "v-change-mail-account-password", []string{user, domain, account}, 3); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	var steps [][]string
	add := func(cmd string, args ...string) {
		steps = append(steps, append([]string{cmd, user, domain, account}, args...))
	}
	if !sameQuota(cur.QUOTA, req.Quota) {
		add("v-change-mail-account-quota", quotaArg(req.Quota))
	}
	if req.Rate != nil && isAdminContext(r) {
		want, have := firstNonEmpty(*req.Rate, "system"), firstNonEmpty(cur.RATE_LIMIT, "system")
		if want != have {
			add("v-change-mail-account-rate-limit", want)
		}
	}
	oldAliases, newAliases := splitList(cur.ALIAS), splitWords(req.Aliases)
	for _, a := range notIn(oldAliases, newAliases) {
		add("v-delete-mail-account-alias", a)
	}
	for _, a := range notIn(newAliases, oldAliases) {
		add("v-add-mail-account-alias", a)
	}
	oldFwd := splitList(cur.FWD)
	if req.Discard {
		if len(oldFwd) != 1 || oldFwd[0] != ":blackhole:" {
			for _, f := range oldFwd {
				add("v-delete-mail-account-forward", f)
			}
			add("v-add-mail-account-forward", ":blackhole:")
		}
	} else {
		newFwd := splitWords(req.Forwards)
		for _, f := range notIn(oldFwd, newFwd) {
			add("v-delete-mail-account-forward", f)
		}
		for _, f := range notIn(newFwd, oldFwd) {
			add("v-add-mail-account-forward", f)
		}
	}
	switch {
	case cur.FWD_ONLY == "yes" && !req.ForwardOnly:
		add("v-delete-mail-account-fwd-only")
	case cur.FWD_ONLY != "yes" && req.ForwardOnly:
		add("v-add-mail-account-fwd-only")
	}
	if cur.AUTOREPLY == "yes" && !req.Autoreply {
		add("v-delete-mail-account-autoreply")
	}
	if req.Autoreply && msg != oldMsg {
		add("v-add-mail-account-autoreply", msg)
	}
	if !s.steps(w, r, steps) {
		return
	}
	if sendTo != "" {
		s.sendMailCredentials(r.Context(), sendTo, domain, account, req.Password)
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// mailAccountAction is delete, suspend or unsuspend: one command on the account.
func (s *server) mailAccountAction(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, ok := domainParam(w, r)
		if !ok {
			return
		}
		account, ok := mailAccountParam(w, r)
		if !ok {
			return
		}
		if s.steps(w, r, [][]string{{cmd, s.actingUser(r), domain, account}}) {
			writeJSON(w, http.StatusOK, OK{Status: "ok"})
		}
	}
}

// Bulk actions on accounts: suspend and unsuspend are admin-only (the bulk helper
// enforces it), delete is open to users; there is no rebuild for accounts.
var mailAccountBulkCmds = map[string]string{
	"suspend": "v-suspend-mail-account", "unsuspend": "v-unsuspend-mail-account", "delete": "v-delete-mail-account",
}

func (s *server) mailAccountBulk(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	s.bulk(w, r, mailAccountBulkCmds, hestia.ValidMailAccountArg,
		func(user, name string) []string { return []string{user, domain, name} }, nil)
}

// sendMailCredentials emails an account's login details (Parity decision 7). A failure
// is logged, never reported to the client, and never includes the password.
func (s *server) sendMailCredentials(ctx context.Context, to, domain, account, password string) {
	address := strings.ToLower(account) + "@" + domain
	if s.Mailer == nil {
		slog.Warn("mail credentials email not sent: no mailer configured", "account", address)
		return
	}
	cfg, err := s.H.Config(ctx)
	if err != nil {
		slog.Warn("mail credentials email not sent: config", "account", address, "err", err)
		return
	}
	lang := firstNonEmpty(session.FromContext(ctx).Language, cfg["LANGUAGE"], "en")
	hostname := s.Sessions.SystemHostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	app := brand.Rewrite(cfg["APP_NAME"])
	var subject, body string
	var found bool
	if s.EmailTemplates != nil {
		subject, body, found = s.EmailTemplates.Load(lang, "email_credentials")
	}
	if !found {
		body = credentialsMailBody
		if msgs, err := s.catalogs.Messages(lang); err == nil && msgs[body] != "" {
			body = msgs[body]
		}
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		wrap := cfg["SUBJECT_EMAIL"]
		if wrap == "" {
			wrap = "{{subject}}"
		}
		subject = strings.NewReplacer("{{subject}}", "Email Credentials: "+address).Replace(wrap)
	}
	mailHost := "mail." + domain
	webmail := "https://" + firstNonEmpty(cfg["WEBMAIL_ALIAS"], "webmail") + "." + domain + "/"
	lower := strings.ToLower(account)
	subject = strings.NewReplacer("{{hostname}}", hostname, "{{appname}}", app,
		"{{account}}", lower, "{{domain}}", domain).Replace(subject)
	body = strings.NewReplacer("{{domain}}", html.EscapeString(domain), "{{account}}", html.EscapeString(lower),
		"{{password}}", html.EscapeString(password), "{{webmail}}", html.EscapeString(webmail),
		"{{hostname}}", html.EscapeString(mailHost), "{{appname}}", app).Replace(body)
	if err := s.Mailer.Send(ctx, cfg, mailer.Message{To: to, Subject: subject, Body: body}); err != nil {
		slog.Warn("mail credentials email failed", "account", address, "to", to, "err", err)
	}
}
