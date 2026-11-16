package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

// webEditRoutes registers Slice B's routes: GET and PUT /api/v1/web/{domain} (the edit
// form's view and settings save). Go's mux prefers the literal /options over {domain}.
func (s *server) webEditRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/web/{domain}", s.read(s.webEditView))
	mux.Handle("PUT /api/v1/web/{domain}", s.write(s.webEditSave))
}

const webFTPCredentialsBody = "FTP account has been created and ready to use.\n\nHostname: {{domain}}\nUsername: {{username}}\nPassword: {{password}}\n\nBest regards,\n\n--\n{{appname}}"

// webEditUserIPs lists the NAT address of each IP the user may use, keyed by IP.
func (s *server) webEditUserIPs(w http.ResponseWriter, r *http.Request, user string) (map[string]string, bool) {
	out, ok := s.output(w, r, "v-list-user-ips", user, "json")
	if !ok {
		return nil, false
	}
	ips, err := hestia.DecodeObjects[hestia.UserIP](out)
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	m := make(map[string]string, len(ips))
	for _, ip := range ips {
		m[ip.IP] = ip.NAT
	}
	return m, true
}

func (s *server) webEditView(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	d, ok := s.webDomain(w, r, user, domain)
	if !ok {
		return
	}
	view := WebDomainView{Domain: d}
	if d.SSL != "no" {
		ssl, ok := s.webDomainSSL(w, r, user, domain)
		if !ok {
			return
		}
		view.SSL = &ssl
	}
	nat, ok := s.webEditUserIPs(w, r, user)
	if !ok {
		return
	}
	view.DisplayIP = d.IP
	if n := nat[d.IP]; n != "" {
		view.DisplayIP = n
	}
	view.Aliases = webEditSplitComma(d.ALIAS)
	view.KeyAlgo = webKeyAlgo(d.SSL_KEY_ALGO, d.SSL)
	view.FTP = webEditFTPAccounts(user, d)
	view.Docroot = webEditDocroot(user, d.CUSTOM_DOCROOT)
	view.Redirect = webEditRedirect(domain, d)
	writeJSON(w, http.StatusOK, view)
}

// webEditSplitComma splits a comma-separated Hestia list; the result is never nil.
func webEditSplitComma(v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// webEditFTPAccounts pairs FTP_USER and FTP_PATH (colon separated) by index.
func webEditFTPAccounts(user string, d hestia.WebDomainDetail) []WebFTPAccount {
	out := []WebFTPAccount{}
	if d.FTP_USER == "" {
		return out
	}
	paths := strings.Split(d.FTP_PATH, ":")
	for i, full := range strings.Split(d.FTP_USER, ":") {
		if full == "" {
			continue
		}
		acc := WebFTPAccount{Name: webEditStripPrefix(user, full)}
		if i < len(paths) {
			acc.Path = paths[i]
		}
		out = append(out, acc)
	}
	return out
}

// webEditStripPrefix removes a leading "<user>_" (any case).
func webEditStripPrefix(user, name string) string {
	p := user + "_"
	if len(name) >= len(p) && strings.EqualFold(name[:len(p)], p) {
		return name[len(p):]
	}
	return name
}

// webEditDocroot parses CUSTOM_DOCROOT lexically. Domain and Folder stay empty when
// the path is not under /home/<user>/web/<domain>/public_html.
func webEditDocroot(user, custom string) WebDocroot {
	if custom == "" {
		return WebDocroot{}
	}
	dr := WebDocroot{Enabled: true}
	prefix := "/home/" + user + "/web/"
	rest, ok := strings.CutPrefix(path.Clean(custom), prefix)
	if !ok {
		return dr
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[1] != "public_html" || !hestia.ValidDomainArg(parts[0]) {
		return dr
	}
	dr.Domain = parts[0]
	if len(parts) == 3 {
		dr.Folder = parts[2]
	}
	return dr
}

func webEditRedirect(domain string, d hestia.WebDomainDetail) WebRedirectView {
	rv := WebRedirectView{Enabled: d.REDIRECT != "", Mode: "custom", Target: d.REDIRECT, Code: d.REDIRECT_CODE}
	if rv.Code == "" {
		rv.Code = "301"
	}
	switch d.REDIRECT {
	case "www." + domain:
		rv.Mode = "www"
	case domain:
		rv.Mode = "bare"
	}
	return rv
}

// webEditArgvSafe reports whether v may become a command argument: it does not start
// with "-" and holds no control characters or newlines.
func webEditArgvSafe(v string) bool {
	if strings.HasPrefix(v, "-") {
		return false
	}
	return !strings.ContainsFunc(v, unicode.IsControl)
}

// webEditValidate runs every panel-side check before any command. It answers the
// request itself on failure.
func webEditValidate(w http.ResponseWriter, req *WebEditRequest) bool {
	bad := func(name, v string) bool {
		invalidFormat(w, name, v)
		return false
	}
	fields := [][2]string{{"ip", req.IP}, {"stats", req.Stats}, {"stats user", strings.TrimSpace(req.StatsUser)},
		{"redirect", strings.TrimSpace(req.RedirectTarget)}, {"code", req.RedirectCode},
		{"docroot domain", req.DocrootDomain}, {"docroot folder", req.DocrootFolder}}
	for _, a := range splitWebList(req.Aliases) {
		fields = append(fields, [2]string{"alias", a})
	}
	if t := req.Templates; t != nil {
		fields = append(fields, [2]string{"template", t.Template})
		if t.Backend != nil {
			fields = append(fields, [2]string{"backend template", *t.Backend})
		}
		if t.ProxyTemplate != nil {
			fields = append(fields, [2]string{"proxy template", *t.ProxyTemplate})
		}
		if t.ProxyExt != nil {
			for _, e := range splitWebList(*t.ProxyExt) {
				fields = append(fields, [2]string{"proxy extensions", e})
			}
		}
		if t.FastCGI != nil {
			fields = append(fields, [2]string{"cache duration", t.FastCGI.Duration})
		}
	}
	for _, f := range fields {
		if !webEditArgvSafe(f[1]) {
			return bad(f[0], f[1])
		}
	}
	if req.Redirect {
		// v-add-web-domain-redirect's own check reads "invalid code format :: X"
		if c := strings.TrimSpace(req.RedirectCode); c != "" && c != "301" && c != "302" {
			return bad("code", c)
		}
	}
	if req.StatsAuth && req.Stats != "none" && !blankFields(w, "Username", strings.TrimSpace(req.StatsUser)) {
		return false
	}
	for _, row := range req.FTP {
		if row.IsNew && (row.Delete || strings.TrimSpace(row.Name) == "") {
			continue
		}
		name := strings.TrimSpace(row.Name)
		if !row.IsNew && !blankFields(w, "Username", name) {
			return false
		}
		if !hestia.ValidFTPUserArg(name) {
			return bad("ftp user", name)
		}
		if p := strings.TrimSpace(row.Path); !webEditArgvSafe(p) {
			return bad("ftp path", p)
		}
		if row.IsNew && !row.Delete && !blankFields(w, "Password", row.Password) {
			return false
		}
		if !row.Delete && row.Email != "" && !validEmail(row.Email) {
			WriteError(w, http.StatusBadRequest, "invalid", invalidEmailMsg)
			return false
		}
	}
	return true
}

// webEditRun holds one save's state.
type webEditRun struct {
	s      *server
	w      http.ResponseWriter
	r      *http.Request
	user   string
	domain string
	cur    hestia.WebDomainDetail
	flags  webRestartFlags
	nat    map[string]string
}

// do runs a write command, answering the failure itself.
func (e *webEditRun) do(cmd string, args ...string) bool {
	if err := e.s.run(e.r.Context(), cmd, append([]string{e.user, e.domain}, args...)...); err != nil {
		writeCommandError(e.w, err)
		return false
	}
	return true
}

// probe says whether a read command succeeded; a non-zero exit means "not found".
func (e *webEditRun) probe(cmd string, args ...string) (found, ok bool) {
	err := e.s.run(e.r.Context(), cmd, args...)
	var ce *runner.CommandError
	switch {
	case err == nil:
		return true, true
	case errors.As(err, &ce):
		return false, true
	}
	writeCommandError(e.w, err)
	return false, false
}

// public is the NAT address of ip, or ip itself.
func (e *webEditRun) public(ip string) (string, bool) {
	if e.nat == nil {
		nat, ok := e.s.webEditUserIPs(e.w, e.r, e.user)
		if !ok {
			return "", false
		}
		e.nat = nat
	}
	if n := e.nat[ip]; n != "" {
		return n, true
	}
	return ip, true
}

func (s *server) webEditSave(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req WebEditRequest
	if !decodeBody(w, r, &req) || !webEditValidate(w, &req) {
		return
	}
	user := s.actingUser(r)
	cur, ok := s.webDomain(w, r, user, domain)
	if !ok {
		return
	}
	// A brand-new statistics login needs its password; checked before any write.
	if req.StatsAuth && req.Stats != "none" && cur.STATS_USER == "" && !blankFields(w, "Password", req.StatsPassword) {
		return
	}
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	e := &webEditRun{s: s, w: w, r: r, user: user, domain: domain, cur: cur}
	canTpl := session.FromContext(r.Context()).UserContext == "admin" || cfg["POLICY_USER_EDIT_WEB_TEMPLATES"] == "yes"
	oldAliases := webEditSplitComma(cur.ALIAS)
	newAliases := webEditDedupe(splitWebList(req.Aliases))
	removed, added := webEditDiff(oldAliases, newAliases), webEditDiff(newAliases, oldAliases)
	aliasesChanged := len(removed)+len(added) > 0

	steps := []func() bool{func() bool { return e.ip(req) }}
	if canTpl && req.Templates != nil {
		steps = append(steps, func() bool { return e.templates(cfg, *req.Templates) })
	}
	steps = append(steps,
		func() bool { return e.aliases(removed, added, req.IP) },
		func() bool { return e.stats(req, aliasesChanged) },
		func() bool { return e.ftp(req.FTP, cfg) },
		func() bool { return e.docroot(req) },
		func() bool { return e.redirect(req) },
	)
	for _, step := range steps {
		if !step() {
			return
		}
	}
	if !s.webRestart(w, r, e.flags) {
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func webEditDedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// webEditDiff is the items of a that b lacks, in a's order.
func webEditDiff(a, b []string) []string {
	var out []string
	for _, v := range a {
		found := false
		for _, o := range b {
			if v == o {
				found = true
				break
			}
		}
		if !found {
			out = append(out, v)
		}
	}
	return out
}

// ip is step 1: the IP change and its DNS and mail cascade.
func (e *webEditRun) ip(req WebEditRequest) bool {
	if req.IP == "" || req.IP == e.cur.IP {
		return true
	}
	public, ok := e.public(req.IP)
	if !ok || !e.do("v-change-web-domain-ip", req.IP, "no") {
		return false
	}
	e.flags.Web, e.flags.Proxy = true, true
	found, ok := e.probe("v-list-dns-domain", e.user, e.domain, "json")
	if !ok {
		return false
	}
	if found {
		if !e.do("v-change-dns-domain-ip", public, "no") {
			return false
		}
		e.flags.DNS = true
	}
	for _, a := range webEditSplitComma(e.cur.ALIAS) {
		found, ok := e.probe("v-list-dns-domain", e.user, a, "json")
		if !ok {
			return false
		}
		if found {
			if err := e.s.run(e.r.Context(), "v-change-dns-domain-ip", e.user, a, public); err != nil {
				writeCommandError(e.w, err)
				return false
			}
			e.flags.DNS = true
		}
	}
	found, ok = e.probe("v-list-mail-domain", e.user, e.domain, "json")
	if !ok {
		return false
	}
	return !found || e.do("v-rebuild-mail-domain")
}

// templates is step 2 (the caller checked the policy).
func (e *webEditRun) templates(cfg hestia.SysConfig, t WebTemplatesChange) bool {
	cur := e.cur
	if t.Template != "" && t.Template != cur.TPL {
		if !e.do("v-change-web-domain-tpl", t.Template, "no") {
			return false
		}
		e.flags.Web = true
	}
	if cfg["WEB_BACKEND"] != "" && t.Backend != nil && *t.Backend != "" && *t.Backend != cur.BACKEND {
		if !e.do("v-change-web-domain-backend-tpl", *t.Backend) {
			return false
		}
	}
	if cfg["WEB_SYSTEM"] == "nginx" && t.FastCGI != nil {
		on := cur.FASTCGI_CACHE == "yes"
		oldDur := cur.FASTCGI_DURATION
		if oldDur == "" {
			oldDur = "2m"
		}
		dur := strings.TrimSpace(t.FastCGI.Duration)
		if dur == "" {
			dur = "2m"
		}
		switch {
		case t.FastCGI.Enabled && (!on || dur != oldDur):
			if !e.do("v-add-fastcgi-cache", dur) {
				return false
			}
			e.flags.Web = true
		case !t.FastCGI.Enabled && on:
			if !e.do("v-delete-fastcgi-cache") {
				return false
			}
			e.flags.Web = true
		}
	}
	if cfg["PROXY_SYSTEM"] != "" && cur.PROXY != "" && (t.ProxyTemplate != nil || t.ProxyExt != nil) {
		tpl := cur.PROXY
		if t.ProxyTemplate != nil && *t.ProxyTemplate != "" {
			tpl = *t.ProxyTemplate
		}
		oldExt := strings.Join(webEditSplitComma(cur.PROXY_EXT), ",")
		ext := oldExt
		if t.ProxyExt != nil {
			ext = strings.Join(splitWebList(*t.ProxyExt), ",")
		}
		if tpl != cur.PROXY || ext != oldExt {
			if !e.do("v-change-web-domain-proxy-tpl", tpl, ext, "no") {
				return false
			}
			e.flags.Proxy = true
		}
	}
	return true
}

// aliases is step 3: removals, then additions, each with its DNS probe. newIP is the
// requested IP, whose public address DNS gets for an added alias.
func (e *webEditRun) aliases(removed, added []string, newIP string) bool {
	zone := func() (bool, bool) { return e.probe("v-list-dns-domain", e.user, e.domain) }
	for _, a := range removed {
		if !e.do("v-delete-web-domain-alias", a, "no") {
			return false
		}
		e.flags.Web, e.flags.Proxy = true, true
		found, ok := zone()
		if !ok {
			return false
		}
		if found {
			if !e.do("v-delete-dns-on-web-alias", a, "no") {
				return false
			}
			e.flags.DNS = true
		}
	}
	for _, a := range added {
		if !e.do("v-add-web-domain-alias", a, "no") {
			return false
		}
		e.flags.Web, e.flags.Proxy = true, true
		found, ok := zone()
		if !ok {
			return false
		}
		if found {
			ip := e.cur.IP
			if newIP != "" {
				ip = newIP
			}
			public, ok := e.public(ip)
			if !ok {
				return false
			}
			if err := e.s.run(e.r.Context(), "v-add-dns-on-web-alias", e.user, a, public, "no"); err != nil {
				writeCommandError(e.w, err)
				return false
			}
			e.flags.DNS = true
		}
	}
	return true
}

// stats is step 4: the engine, then the login.
func (e *webEditRun) stats(req WebEditRequest, aliasesChanged bool) bool {
	cur := e.cur.STATS
	want := req.Stats
	if want == "" {
		want = cur
		if want == "" {
			want = "none"
		}
	}
	switch {
	case cur != "" && want == cur && aliasesChanged, cur != "" && want != "none" && want != cur:
		if !e.do("v-change-web-domain-stats", want) {
			return false
		}
	case cur != "" && want == "none":
		if !e.do("v-delete-web-domain-stats") {
			return false
		}
	case cur == "" && want != "none":
		if !e.do("v-add-web-domain-stats", want) {
			return false
		}
	}
	oldUser := e.cur.STATS_USER
	if !(req.StatsAuth && want != "none") {
		return oldUser == "" || e.do("v-delete-web-domain-stats-user")
	}
	user := strings.TrimSpace(req.StatsUser)
	if oldUser == "" || user != oldUser || req.StatsPassword != "" {
		return e.withSecret(req.StatsPassword, "v-add-web-domain-stats-user", []string{e.user, e.domain, user}, 3)
	}
	return true
}

// withSecret runs a command with a password file, answering a failure itself.
func (e *webEditRun) withSecret(password, cmd string, args []string, at int) bool {
	if err := e.s.withSecret(e.r, password, cmd, args, at); err != nil {
		writeCommandError(e.w, err)
		return false
	}
	return true
}

// ftp is step 5, row by row.
func (e *webEditRun) ftp(rows []WebFTPChange, cfg hestia.SysConfig) bool {
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		p := strings.TrimSpace(row.Path)
		switch {
		case row.IsNew && (row.Delete || name == ""):
			continue
		case row.IsNew:
			name = webEditStripPrefix(e.user, name)
			if !e.withSecret(row.Password, "v-add-web-domain-ftp", []string{e.user, e.domain, name, p}, 3) {
				return false
			}
		case row.Delete:
			if !e.do("v-delete-web-domain-ftp", e.user+"_"+name) {
				return false
			}
			continue
		default:
			full := e.user + "_" + name
			if p != strings.TrimSpace(row.PrevPath) && !e.do("v-change-web-domain-ftp-path", full, p) {
				return false
			}
			if row.Password != "" && !e.withSecret(row.Password, "v-change-web-domain-ftp-password", []string{e.user, e.domain, full}, 3) {
				return false
			}
		}
		// An existing account is emailed only with a new password (owner ruling 7).
		if row.Email != "" && (row.IsNew || row.Password != "") {
			e.s.sendFTPCredentials(e.r.Context(), cfg, row.Email, e.domain, e.user+"_"+name, row.Password)
		}
	}
	return true
}

// sendFTPCredentials emails an FTP account's login (Parity decision 9): raw values,
// and a failure is logged without the password, never reported.
func (s *server) sendFTPCredentials(ctx context.Context, cfg hestia.SysConfig, to, domain, account, password string) {
	if s.Mailer == nil {
		slog.Warn("ftp credentials email not sent: no mailer configured", "account", account)
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
		subject, body, found = s.EmailTemplates.Load(lang, "ftpaccount_created")
	}
	if !found {
		body = webFTPCredentialsBody
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
		subject = strings.NewReplacer("{{subject}}", "FTP Account Credentials: "+account).Replace(wrap)
	}
	subject = strings.NewReplacer("{{hostname}}", hostname, "{{appname}}", app, "{{domain}}", domain, "{{username}}", account).Replace(subject)
	body = strings.NewReplacer("{{domain}}", domain, "{{username}}", account, "{{password}}", password, "{{appname}}", app).Replace(body)
	if err := s.Mailer.Send(ctx, cfg, mailer.Message{To: to, Subject: subject, Body: body}); err != nil {
		slog.Warn("ftp credentials email failed", "account", account, "to", to, "err", err)
	}
}

// docroot is step 6.
func (e *webEditRun) docroot(req WebEditRequest) bool {
	cur := webEditDocroot(e.user, e.cur.CUSTOM_DOCROOT)
	if !req.Docroot {
		if !cur.Enabled {
			return true
		}
		if !e.do("v-change-web-domain-docroot", "default") {
			return false
		}
		e.flags.Web, e.flags.Proxy = true, true
		return true
	}
	target, folder := req.DocrootDomain, strings.Trim(req.DocrootFolder, "/")
	if target == "" {
		return true
	}
	curDomain, curFolder := cur.Domain, cur.Folder
	if !cur.Enabled {
		curDomain, curFolder = e.domain, ""
	}
	if target == curDomain && folder == curFolder {
		return true
	}
	var ok bool
	if target == e.domain && folder == "" {
		ok = e.do("v-change-web-domain-docroot", "default")
	} else {
		ok = e.do("v-change-web-domain-docroot", target, folder, "yes")
	}
	if ok {
		e.flags.Web, e.flags.Proxy = true, true
	}
	return ok
}

// redirect is step 7.
func (e *webEditRun) redirect(req WebEditRequest) bool {
	if !req.Redirect {
		if e.cur.REDIRECT == "" {
			return true
		}
		if !e.do("v-delete-web-domain-redirect") {
			return false
		}
		e.flags.Web, e.flags.Proxy = true, true
		return true
	}
	target := strings.TrimSpace(req.RedirectTarget)
	if target == "" {
		return true
	}
	code := strings.TrimSpace(req.RedirectCode)
	if code == "" {
		code = "301"
	}
	if e.cur.REDIRECT != "" && target == e.cur.REDIRECT && code == webEditRedirect(e.domain, e.cur).Code {
		return true
	}
	if !e.do("v-add-web-domain-redirect", target, code) {
		return false
	}
	e.flags.Web, e.flags.Proxy = true, true
	return true
}
