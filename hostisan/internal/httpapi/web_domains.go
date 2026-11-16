package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"path"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

// webDomainRoutes registers Slice A's routes: the list, options, add, delete,
// suspend, unsuspend, cache purge, bulk, logs and downloads.
func (s *server) webDomainRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/web", s.read(s.webDomainsList))
	// Go's mux prefers the literals /options and /bulk over the {domain} wildcard.
	mux.Handle("GET /api/v1/web/options", s.read(s.webDomainsOptions))
	mux.Handle("POST /api/v1/web", s.write(s.webDomainsAdd))
	mux.Handle("DELETE /api/v1/web/{domain}", s.write(s.webDomainsAction("v-delete-web-domain", "yes")))
	mux.Handle("POST /api/v1/web/{domain}/suspend", s.adminWrite(s.webDomainsAction("v-suspend-web-domain")))
	mux.Handle("POST /api/v1/web/{domain}/unsuspend", s.adminWrite(s.webDomainsAction("v-unsuspend-web-domain")))
	mux.Handle("POST /api/v1/web/{domain}/purge-cache", s.write(s.webDomainsAction("v-purge-nginx-cache")))
	mux.Handle("POST /api/v1/web/bulk", s.write(s.webDomainsBulk))
	mux.Handle("GET /api/v1/web/{domain}/logs", s.read(s.webDomainsLog))
	mux.Handle("GET /api/v1/web/{domain}/logs/download", s.read(s.webDomainsLogDownload))
	mux.Handle("GET /api/v1/web/{domain}/download", s.read(s.webDomainsSiteDownload))
}

func (s *server) webDomainsList(w http.ResponseWriter, r *http.Request) {
	user := s.actingUser(r)
	out, ok := s.output(w, r, "v-list-web-domains", user, "json")
	if !ok {
		return
	}
	domains, err := hestia.DecodeObjects[hestia.WebDomain](out)
	if err != nil {
		serverError(w, err)
		return
	}
	out, ok = s.output(w, r, "v-list-sys-ips", "json")
	if !ok {
		return
	}
	ips, err := hestia.DecodeObjects[hestia.SysIP](out)
	if err != nil {
		serverError(w, err)
		return
	}
	// Only the addresses of the listed domains go out (Parity 14).
	used := map[string]bool{}
	for _, d := range domains {
		used[d.IP] = true
	}
	display := map[string]string{}
	for _, ip := range ips {
		if used[ip.IP] {
			display[ip.IP] = ip.Display()
		}
	}
	for ip := range used {
		if _, known := display[ip]; !known {
			display[ip] = ip
		}
	}
	u, ok, err := s.H.User(r.Context(), user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "web list: user "+user+" not found"))
		return
	}
	writeJSON(w, http.StatusOK, WebDomains{Domains: domains, DisplayIPs: display, Usage: webDomainsUsage(u)})
}

// webDomainsUsage builds the usage summary; with either factor of the alias limit
// unlimited (or not a number) the account limit is unlimited too.
func webDomainsUsage(u hestia.User) WebUsage {
	use := WebUsage{
		Domains: u["U_WEB_DOMAINS"], DomainsLimit: u["WEB_DOMAINS"], Suspended: u["SUSPENDED_WEB"],
		Aliases: u["U_WEB_ALIASES"], AliasesLimit: "unlimited",
	}
	perDomain, err1 := strconv.Atoi(u["WEB_ALIASES"])
	domains, err2 := strconv.Atoi(u["WEB_DOMAINS"])
	if err1 == nil && err2 == nil {
		use.AliasesLimit = strconv.Itoa(perDomain * domains)
	}
	return use
}

// webDefaultProxyExt is the extension list v-add-web-domain gives a new domain.
const webDefaultProxyExt = "css,htm,html,js,mjs,json,xml," +
	"apng,avif,bmp,cur,gif,ico,jfif,jpg,jpeg,pjp,pjpeg,png,svg,tif,tiff,webp," +
	"aac,caf,flac,m4a,midi,mp3,ogg,opus,wav," +
	"3gp,av1,avi,m4v,mkv,mov,mpg,mpeg,mp4,mp4v,webm," +
	"otf,ttf,woff,woff2," +
	"doc,docx,odf,odp,ods,odt,pdf,ppt,pptx,rtf,txt,xls,xlsx," +
	"7z,bz2,gz,rar,tar,tgz,zip," +
	"apk,appx,bin,dmg,exe,img,iso,jar,msi," +
	"webmanifest"

// webDomainsStrings runs a read command that prints a JSON array of strings; the
// result is never nil. ok is false when it answered the request itself.
func (s *server) webDomainsStrings(w http.ResponseWriter, r *http.Request, cmd string, args ...string) ([]string, bool) {
	out, ok := s.output(w, r, cmd, args...)
	if !ok {
		return nil, false
	}
	list := []string{}
	if err := json.Unmarshal(out, &list); err != nil {
		serverError(w, err)
		return nil, false
	}
	if list == nil {
		list = []string{}
	}
	return list, true
}

func (s *server) webDomainsOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "web options: user "+user+" not found"))
		return
	}
	out, ok := s.output(w, r, "v-list-user-ips", user, "json")
	if !ok {
		return
	}
	userIPs, err := hestia.DecodeObjects[hestia.SysIP](out)
	if err != nil {
		serverError(w, err)
		return
	}
	ips := make([]WebIP, 0, len(userIPs))
	for _, ip := range userIPs {
		ips = append(ips, WebIP{IP: ip.IP, Display: ip.Display()})
	}
	stats, ok := s.webDomainsStrings(w, r, "v-list-web-stats", "json")
	if !ok {
		return
	}
	admin := session.FromContext(ctx).UserContext == "admin"
	canEdit := admin || cfg["POLICY_USER_EDIT_WEB_TEMPLATES"] == "yes"
	templates, backend, proxy := []string{}, []string{}, []string{}
	if canEdit {
		if templates, ok = s.webDomainsStrings(w, r, "v-list-web-templates", "json"); !ok {
			return
		}
		if cfg["WEB_BACKEND"] != "" {
			if backend, ok = s.webDomainsStrings(w, r, "v-list-web-templates-backend", "json"); !ok {
				return
			}
		}
		if cfg["PROXY_SYSTEM"] != "" {
			if proxy, ok = s.webDomainsStrings(w, r, "v-list-web-templates-proxy", "json"); !ok {
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, WebOptions{
		IPs:              ips,
		WebSystem:        cfg["WEB_SYSTEM"],
		WebBackend:       cfg["WEB_BACKEND"],
		ProxySystem:      cfg["PROXY_SYSTEM"],
		Nginx:            cfg["WEB_SYSTEM"] == "nginx",
		Purge:            webDomainsPurgeOffered(cfg),
		FTP:              cfg["FTP_SYSTEM"] == "vsftpd" || cfg["FTP_SYSTEM"] == "proftpd",
		StatsTypes:       stats,
		DNS:              webDomainsDNSOffered(cfg, u),
		Mail:             webDomainsMailOffered(cfg, u),
		AppInstaller:     cfg["PLUGIN_APP_INSTALLER"] != "false",
		CanEditTemplates: canEdit,
		Templates:        templates,
		BackendTemplates: backend,
		ProxyTemplates:   proxy,
		DefaultProxyExt:  strings.Split(webDefaultProxyExt, ","),
		CanRebuild:       admin,
		AdminGate:        u.Role() == "admin",
		FTPPrefix:        user + "_",
		WebRoot:          "/home/" + user + "/web/",
		KeyAlgos:         webKeyAlgos,
	})
}

func webDomainsPurgeOffered(cfg hestia.SysConfig) bool {
	return cfg["WEB_SYSTEM"] == "nginx" || cfg["PROXY_SYSTEM"] == "nginx"
}

func webDomainsDNSOffered(cfg hestia.SysConfig, u hestia.User) bool {
	return cfg["DNS_SYSTEM"] != "" && u["DNS_DOMAINS"] != "0"
}

func webDomainsMailOffered(cfg hestia.SysConfig, u hestia.User) bool {
	return cfg["IMAP_SYSTEM"] != "" && u["MAIL_DOMAINS"] != "0"
}

func (s *server) webDomainsAdd(w http.ResponseWriter, r *http.Request) {
	var req WebAddRequest
	if !decodeBody(w, r, &req) {
		return
	}
	raw := strings.TrimSpace(req.Domain)
	ip := strings.TrimSpace(req.IP)
	if !blankFields(w, "Domain", raw, "IP Address", ip) {
		return
	}
	// 2c Parity 8: strip a leading "www." only, then lower-case.
	domain := strings.TrimPrefix(strings.ToLower(raw), "www.")
	if !hestia.ValidDomainArg(domain) {
		shown := domain
		if shown == "" {
			shown = raw
		}
		WriteError(w, http.StatusBadRequest, "invalid", "Error: invalid domain format :: "+shown)
		return
	}
	if addr, err := netip.ParseAddr(ip); err != nil || addr.Zone() != "" {
		WriteError(w, http.StatusBadRequest, "invalid", "Error: invalid ip format :: "+ip)
		return
	}
	ctx := r.Context()
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "web add: user "+user+" not found"))
		return
	}
	out, ok := s.output(w, r, "v-list-user-ips", user, "json")
	if !ok {
		return
	}
	userIPs, err := hestia.DecodeObjects[hestia.SysIP](out)
	if err != nil {
		serverError(w, err)
		return
	}
	// DNS gets the NAT address when the IP has one; web gets the internal address.
	publicIP := ip
	for _, a := range userIPs {
		if a.IP == ip && strings.TrimSpace(a.NAT) != "" {
			publicIP = strings.TrimSpace(a.NAT)
		}
	}
	steps := [][]string{{"v-add-web-domain", user, domain, ip, "yes"}}
	if req.DNS && webDomainsDNSOffered(cfg, u) {
		steps = append(steps, []string{"v-add-dns-domain", user, domain, publicIP, "", "", "", "", "", "", "", "", "no"})
	}
	if req.Mail && webDomainsMailOffered(cfg, u) {
		steps = append(steps, []string{"v-add-mail-domain", user, domain})
	}
	if s.steps(w, r, steps) {
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

// webDomainsAction runs cmd <user> <domain> [extra...] for the path's domain.
func (s *server) webDomainsAction(cmd string, extra ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, ok := domainParam(w, r)
		if !ok {
			return
		}
		if s.steps(w, r, [][]string{append([]string{cmd, s.actingUser(r), domain}, extra...)}) {
			writeJSON(w, http.StatusOK, OK{Status: "ok"})
		}
	}
}

func (s *server) webDomainsBulk(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	cmds := map[string]string{
		"delete":    "v-delete-web-domain",
		"rebuild":   "v-rebuild-web-domain",
		"suspend":   "v-suspend-web-domain",
		"unsuspend": "v-unsuspend-web-domain",
	}
	// Purge exists only where nginx does (Parity 3); otherwise it is an unknown action.
	if webDomainsPurgeOffered(cfg) {
		cmds["purge"] = "v-purge-nginx-cache"
	}
	s.bulkFor(w, r, cmds, []string{"delete", "purge"}, hestia.ValidDomainArg, func(user, n string) []string {
		return []string{user, n, "no"}
	}, [][]string{{"v-restart-web"}, {"v-restart-proxy"}, {"v-restart-dns"}, {"v-restart-web-backend"}})
}

// webDomainsLogType is "error" for ?type=error, else "access".
func webDomainsLogType(r *http.Request) string {
	if r.URL.Query().Get("type") == "error" {
		return "error"
	}
	return "access"
}

func (s *server) webDomainsLog(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	typ := webDomainsLogType(r)
	lines := []string{}
	// A failing command gives an empty log, as PHP's empty <pre> (Parity 11).
	cmd := "v-list-web-domain-" + typ + "log"
	if res, err := s.R.Run(r.Context(), cmd, s.actingUser(r), domain); err == nil && res.Err(cmd) == nil {
		if text := strings.TrimRight(strings.ReplaceAll(string(res.Stdout), "\r", ""), "\n"); text != "" {
			lines = strings.Split(text, "\n")
		}
	}
	writeJSON(w, http.StatusOK, WebLog{Type: typ, Lines: lines})
}

func (s *server) webDomainsLogDownload(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	typ := webDomainsLogType(r)
	out, ok := s.output(w, r, "v-list-web-domain-"+typ+"log", s.actingUser(r), domain, "5000")
	if !ok {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream") // as Hestia (owner ruling 9)
	h.Set("Content-Disposition", "attachment; filename="+strconv.Quote(domain+"."+typ+"-log.txt"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// webDomainsSiteDownload dumps the site and hands the archive to hestia-nginx, which
// serves it from its internal /__accel/backup/ location (Parity 12). The body stays empty.
func (s *server) webDomainsSiteDownload(w http.ResponseWriter, r *http.Request) {
	if !sameOriginGET(w, r) {
		return
	}
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-dump-site", s.actingUser(r), domain, "full")
	if !ok {
		return
	}
	// Line 1 is the archive's name, line 2 its path.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 || !path.IsAbs(strings.TrimSpace(lines[1])) {
		serverError(w, errors.New("v-dump-site: unexpected output"))
		return
	}
	file := strings.TrimSpace(lines[1])
	name := path.Base(strings.TrimSpace(lines[0]))
	if name == "." || name == "/" {
		name = path.Base(file)
	}
	if !s.accelRedirect(w, r, file) {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", "attachment; filename="+strconv.Quote(name))
	w.WriteHeader(http.StatusOK)
}
