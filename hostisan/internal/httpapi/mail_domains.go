package httpapi

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

// mailDomainRoutes mounts the mail domain API (Plan 2c, Slice A).
func (s *server) mailDomainRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/mail", s.read(s.mailList))
	// Go's mux prefers the literals /options and /bulk over the {domain} wildcard.
	mux.Handle("GET /api/v1/mail/options", s.read(s.mailOptions))
	mux.Handle("GET /api/v1/mail/{domain}", s.read(s.mailOne))
	mux.Handle("POST /api/v1/mail", s.write(s.mailAdd))
	mux.Handle("PUT /api/v1/mail/{domain}", s.write(s.mailEdit))
	mux.Handle("DELETE /api/v1/mail/{domain}", s.write(s.mailDomainAction("v-delete-mail-domain")))
	mux.Handle("POST /api/v1/mail/{domain}/suspend", s.adminWrite(s.mailDomainAction("v-suspend-mail-domain")))
	mux.Handle("POST /api/v1/mail/{domain}/unsuspend", s.adminWrite(s.mailDomainAction("v-unsuspend-mail-domain")))
	mux.Handle("POST /api/v1/mail/bulk", s.write(s.mailBulkDomains))
}

func (s *server) mailList(w http.ResponseWriter, r *http.Request) {
	user := s.actingUser(r)
	out, ok := s.output(w, r, "v-list-mail-domains", user, "json")
	if !ok {
		return
	}
	domains, err := hestia.DecodeObjects[hestia.MailDomain](out)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(r.Context(), user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "mail list: user "+user+" not found"))
		return
	}
	writeJSON(w, http.StatusOK, MailDomains{Domains: domains, Usage: mailUsage(u)})
}

// mailUsage builds the usage summary; with either account factor unlimited (or not a
// number) the account limit is unlimited too.
func mailUsage(u hestia.User) MailUsage {
	use := MailUsage{
		Domains: u["U_MAIL_DOMAINS"], DomainsLimit: u["MAIL_DOMAINS"], Suspended: u["SUSPENDED_MAIL"],
		Accounts: u["U_MAIL_ACCOUNTS"], AccountsLimit: "unlimited",
	}
	perDomain, err1 := strconv.Atoi(u["MAIL_ACCOUNTS"])
	domains, err2 := strconv.Atoi(u["MAIL_DOMAINS"])
	if err1 == nil && err2 == nil {
		use.AccountsLimit = strconv.Itoa(perDomain * domains)
	}
	return use
}

func (s *server) mailOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "mail options: user "+user+" not found"))
		return
	}
	clients := []string{}
	if cfg["WEBMAIL_SYSTEM"] != "" {
		out, ok := s.output(w, r, "v-list-sys-webmail", "json")
		if !ok {
			return
		}
		if clients, err = hestia.WebmailClients(out); err != nil {
			serverError(w, err)
			return
		}
	}
	alias := cfg["WEBMAIL_ALIAS"]
	if alias == "" {
		alias = "webmail"
	}
	writeJSON(w, http.StatusOK, MailOptions{
		WebmailClients: clients,
		WebmailAlias:   alias,
		Antispam:       cfg["ANTISPAM_SYSTEM"] != "",
		Antivirus:      cfg["ANTIVIRUS_SYSTEM"] != "",
		Webmail:        cfg["WEBMAIL_SYSTEM"] != "",
		Imap:           cfg["IMAP_SYSTEM"] != "",
		CanSetRate:     session.FromContext(ctx).UserContext == "admin",
		AdminGate:      u.Role() == "admin",
		KeyAlgos:       []string{"ecdsa-256", "ecdsa-384", "rsa"},
	})
}

func (s *server) mailOne(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	d, ok := s.mailDomain(w, r, user, domain)
	if !ok {
		return
	}
	view := MailDomainView{Domain: d}
	if d.SSL != "no" {
		ssl, ok := s.mailDomainSSL(w, r, user, domain)
		if !ok {
			return
		}
		view.SSL = &ssl
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *server) mailDomainAction(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, ok := domainParam(w, r)
		if !ok {
			return
		}
		if s.steps(w, r, [][]string{{cmd, s.actingUser(r), domain}}) {
			writeJSON(w, http.StatusOK, OK{Status: "ok"})
		}
	}
}

// mailBulk maps a bulk action to its command. Everything but delete is admin-only,
// which s.bulk enforces.
var mailBulk = map[string]string{
	"rebuild":   "v-rebuild-mail-domain",
	"suspend":   "v-suspend-mail-domain",
	"unsuspend": "v-unsuspend-mail-domain",
	"delete":    "v-delete-mail-domain",
}

func (s *server) mailBulkDomains(w http.ResponseWriter, r *http.Request) {
	s.bulk(w, r, mailBulk, hestia.ValidDomainArg, func(user, n string) []string {
		return []string{user, n}
	}, nil)
}

var dkimSelectorRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// dkimSelector is the selector to send: "mail" when empty. ok is false (and the
// request answered) when it is not one Hestia accepts, in Hestia's own wording.
func dkimSelector(w http.ResponseWriter, v string) (string, bool) {
	if v == "" {
		return "mail", true
	}
	if len(v) > 63 || !dkimSelectorRe.MatchString(v) {
		WriteError(w, http.StatusBadRequest, "invalid", "Error: invalid dkim selector format :: "+v)
		return "", false
	}
	return v, true
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// relayPort is the port to send: 587 when empty.
func relayPort(p string) string {
	if p = strings.TrimSpace(p); p == "" {
		return "587"
	}
	return p
}

// checkRelay answers 400 itself when a relay has no host, or a password with a line
// break, which the one-line password file cannot hold.
func checkRelay(w http.ResponseWriter, rl *MailSMTPRelay) bool {
	if !blankFields(w, "Host", strings.TrimSpace(rl.Host)) {
		return false
	}
	// Hestia refuses quotes and "|" and prints the password back in its error, so
	// they are stopped here, with a message that never holds the value.
	if strings.ContainsAny(rl.Password, "'\"|\r\n") {
		WriteError(w, http.StatusBadRequest, "invalid", `Invalid Password contains quotes (" or ' or | )`)
		return false
	}
	return true
}

// writeRelay runs v-add-mail-domain-smtp-relay for rl. The password goes through a
// private file that is always removed again, so this is not a plain step.
func (s *server) writeRelay(r *http.Request, user, domain string, rl MailSMTPRelay) error {
	args := []string{user, domain, strings.TrimSpace(rl.Host), rl.User}
	port := relayPort(rl.Port)
	if rl.Password == "" {
		return s.run(r.Context(), "v-add-mail-domain-smtp-relay", append(args, "", port)...)
	}
	return s.withSecret(r, rl.Password, "v-add-mail-domain-smtp-relay", append(args, port), 4)
}

func (s *server) mailAdd(w http.ResponseWriter, r *http.Request) {
	var req AddMailDomainRequest
	if !decodeBody(w, r, &req) {
		return
	}
	raw := strings.TrimSpace(req.Domain)
	if !blankFields(w, "Domain", raw) {
		return
	}
	// Parity decision 8: strip a leading "www." only, then lower-case.
	domain := strings.TrimPrefix(strings.ToLower(raw), "www.")
	if !hestia.ValidDomainArg(domain) {
		shown := domain
		if shown == "" {
			shown = raw
		}
		WriteError(w, http.StatusBadRequest, "invalid", "Error: invalid domain format :: "+shown)
		return
	}
	selector, ok := dkimSelector(w, req.DKIMSelector)
	if !ok {
		return
	}
	// The relay is always written on add, so a user needs a password (see MailSMTPRelay).
	if rl := req.SMTPRelay; rl != nil && (!checkRelay(w, rl) || (rl.User != "" && !blankFields(w, "Password", rl.Password))) {
		return
	}
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	// A control the server doesn't offer is off, as Hestia itself forces it.
	antispam := req.Antispam && cfg["ANTISPAM_SYSTEM"] != ""
	antivirus := req.Antivirus && cfg["ANTIVIRUS_SYSTEM"] != ""
	user := s.actingUser(r)

	steps := [][]string{{"v-add-mail-domain", user, domain, yesNo(antispam), yesNo(antivirus), yesNo(req.DKIM), "2048", "", "", selector}}
	if req.Reject && antispam {
		steps = append(steps, []string{"v-add-mail-domain-reject", user, domain, "yes"})
	}
	if cfg["IMAP_SYSTEM"] != "" && cfg["WEBMAIL_SYSTEM"] != "" {
		if req.Webmail != "" {
			steps = append(steps, []string{"v-add-mail-domain-webmail", user, domain, req.Webmail, "yes"})
		} else {
			steps = append(steps, []string{"v-delete-mail-domain-webmail", user, domain, "yes"})
		}
	}
	if !s.steps(w, r, steps) {
		return
	}
	if req.SMTPRelay != nil {
		if err := s.writeRelay(r, user, domain, *req.SMTPRelay); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) mailEdit(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req EditMailDomainRequest
	if !decodeBody(w, r, &req) {
		return
	}
	selector, ok := dkimSelector(w, req.DKIMSelector)
	if !ok {
		return
	}
	user := s.actingUser(r)
	cur, ok := s.mailDomain(w, r, user, domain)
	if !ok {
		return
	}

	// The relay is checked before anything runs, so a bad one cannot leave the
	// earlier changes half saved.
	relayOn := cur.U_SMTP_RELAY == "true"
	writeRelay := false
	if rl := req.SMTPRelay; rl != nil {
		if !checkRelay(w, rl) {
			return
		}
		writeRelay = !relayOn || strings.TrimSpace(rl.Host) != cur.U_SMTP_RELAY_HOST ||
			rl.User != cur.U_SMTP_RELAY_USERNAME || relayPort(rl.Port) != relayPort(cur.U_SMTP_RELAY_PORT) ||
			rl.Password != ""
		if writeRelay && rl.User != "" && !blankFields(w, "Password", rl.Password) {
			return
		}
	}

	var steps [][]string
	step := func(cmd string, args ...string) {
		steps = append(steps, append([]string{cmd, user, domain}, args...))
	}

	// Parity decision 2: change only what changed. A value stored as "" is not "yes".
	antispamNow := cur.ANTISPAM == "yes"
	if req.Antispam != nil {
		if *req.Antispam && !antispamNow {
			step("v-add-mail-domain-antispam")
			antispamNow = true
		} else if !*req.Antispam && antispamNow {
			step("v-delete-mail-domain-antispam")
			antispamNow = false
		}
	}
	if req.Antivirus != nil {
		if *req.Antivirus && cur.ANTIVIRUS != "yes" {
			step("v-add-mail-domain-antivirus")
		} else if !*req.Antivirus && cur.ANTIVIRUS == "yes" {
			step("v-delete-mail-domain-antivirus")
		}
	}
	curSelector := cur.DKIM_SELECTOR
	if curSelector == "" {
		curSelector = "mail"
	}
	dkimOn := cur.DKIM == "yes"
	if dkimOn && (!req.DKIM || selector != curSelector) {
		step("v-delete-mail-domain-dkim")
		dkimOn = false
	}
	if req.DKIM && !dkimOn {
		step("v-add-mail-domain-dkim", "", selector)
	}

	catchall := strings.TrimSpace(req.Catchall)
	if cur.CATCHALL != "" && catchall == "" {
		step("v-delete-mail-domain-catchall")
	}
	if req.Rate != nil && session.FromContext(r.Context()).UserContext == "admin" {
		want, have := strings.TrimSpace(*req.Rate), cur.RATE_LIMIT
		if want == "" {
			want = "system"
		}
		if have == "" {
			have = "system"
		}
		if want != have {
			step("v-change-mail-domain-rate-limit", want)
		}
	}
	if req.Reject != nil {
		if *req.Reject && antispamNow && cur.REJECT != "yes" {
			step("v-add-mail-domain-reject", "yes")
		} else if !*req.Reject && cur.REJECT == "yes" {
			step("v-delete-mail-domain-reject", "yes")
		}
	}
	if cur.CATCHALL != "" && catchall != "" && catchall != cur.CATCHALL {
		step("v-change-mail-domain-catchall", catchall)
	}
	if cur.CATCHALL == "" && catchall != "" {
		step("v-add-mail-domain-catchall", catchall)
	}
	if req.Webmail != nil && *req.Webmail != cur.WEBMAIL {
		cfg, err := s.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		// Webmail needs both an IMAP and a webmail system, as on add; without them the
		// client is left as it is.
		if cfg["IMAP_SYSTEM"] != "" && cfg["WEBMAIL_SYSTEM"] != "" {
			if *req.Webmail == "" {
				step("v-delete-mail-domain-webmail", "yes")
			} else {
				step("v-add-mail-domain-webmail", *req.Webmail, "yes")
			}
		}
	}
	if req.SMTPRelay == nil && relayOn {
		step("v-delete-mail-domain-smtp-relay")
	}

	if !s.steps(w, r, steps) {
		return
	}
	if writeRelay {
		if err := s.writeRelay(r, user, domain, *req.SMTPRelay); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}
