package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
)

// mailExtraRoutes mounts the mail SSL section and the DNS records view.
func (s *server) mailExtraRoutes(mux *http.ServeMux) {
	mux.Handle("PUT /api/v1/mail/{domain}/ssl", s.write(s.mailSSLSave))
	mux.Handle("GET /api/v1/mail/{domain}/dns", s.read(s.mailDNS))
}

// letsEncryptTimeout is how long a Let's Encrypt command may run. Mail issues two names
// with sleeps, retries and service restarts, which can pass the runner's 60s default.
// It must stay under the HTTP write timeout (90s, cmd/hostisan/serve.go); time a real
// issue on the VPS before changing it.
const letsEncryptTimeout = 85 * time.Second

// letsEncryptSteps runs Let's Encrypt commands with letsEncryptTimeout.
func (s *server) letsEncryptSteps(w http.ResponseWriter, r *http.Request, steps [][]string) bool {
	return s.steps(w, r.WithContext(runner.WithTimeout(r.Context(), letsEncryptTimeout)), steps)
}

var mailKeyAlgos = map[string]bool{"ecdsa-256": true, "ecdsa-384": true, "rsa": true}

// mailKeyAlgo is the posted key type, or ecdsa-256 when it is not a valid one.
func mailKeyAlgo(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if mailKeyAlgos[v] {
		return v
	}
	return "ecdsa-256"
}

// storedKeyAlgo is the key type stored for a Let's Encrypt certificate; the legacy
// "ecdsa" is P-256 and an unset one predates the setting (RSA).
func storedKeyAlgo(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case v == "ecdsa":
		return "ecdsa-256"
	case mailKeyAlgos[v]:
		return v
	}
	return "rsa"
}

// pemText is PEM text with line endings and surrounding blank space normalised, for
// comparing what was pasted with what is stored.
func pemText(v string) string {
	return strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
}

// mailSSLDir writes the certificate files to a private temp directory, runs cmd on it
// and removes the directory again. The key never reaches a log or an error.
func (s *server) mailSSLDir(w http.ResponseWriter, r *http.Request, cmd, user, domain string, req MailSSLRequest) bool {
	files := map[string]string{}
	for ext, v := range map[string]string{"crt": req.Crt, "key": req.Key, "ca": req.CA} {
		if t := pemText(v); t != "" {
			files[domain+"."+ext] = t + "\n"
		}
	}
	dir, cleanup, err := secret.TempDir("/tmp", files)
	if err != nil {
		serverError(w, err)
		return false
	}
	defer cleanup()
	if err := s.run(r.Context(), cmd, user, domain, dir, "no"); err != nil {
		writeCommandError(w, err)
		return false
	}
	return true
}

func (s *server) mailSSLSave(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req MailSSLRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user := s.actingUser(r)
	cur, ok := s.mailDomain(w, r, user, domain)
	if !ok {
		return
	}
	curSSL, curLE := cur.SSL == "yes", cur.LETSENCRYPT == "yes"
	algo := mailKeyAlgo(req.KeyAlgo)
	pasted := req.SSL && !req.LetsEncrypt
	haveCert := func() bool {
		return blankFields(w, "ssl certificate", pemText(req.Crt), "ssl key", pemText(req.Key))
	}
	// A pasted certificate is needed to add one, also after Let's Encrypt is removed;
	// checking first keeps a missing one from costing the user their certificate.
	if pasted && (!curSSL || curLE) && !haveCert() {
		return
	}

	// Replace a pasted certificate when its text changed.
	if pasted && curSSL && !curLE {
		stored, ok := s.mailDomainSSL(w, r, user, domain)
		if !ok {
			return
		}
		if pemText(req.Crt) != pemText(stored.CRT) || pemText(req.Key) != pemText(stored.KEY) || pemText(req.CA) != pemText(stored.CA) {
			if !haveCert() || !s.mailSSLDir(w, r, "v-change-mail-domain-sslcert", user, domain, req) {
				return
			}
		}
	}
	// Change the Let's Encrypt key type.
	if req.SSL && req.LetsEncrypt && curLE && algo != storedKeyAlgo(cur.SSL_KEY_ALGO) {
		if !s.letsEncryptSteps(w, r, [][]string{{"v-add-letsencrypt-domain", user, domain, " ", "yes", algo}}) {
			return
		}
	}
	// Turn Let's Encrypt off. This also removes the mail certificate.
	if curLE && (!req.LetsEncrypt || !req.SSL) {
		if !s.letsEncryptSteps(w, r, [][]string{{"v-delete-letsencrypt-domain", user, domain, "", "yes"}}) {
			return
		}
		curSSL = false
	}
	// Turn SSL off.
	if curSSL && !req.SSL {
		if !s.steps(w, r, [][]string{{"v-delete-mail-domain-ssl", user, domain}}) {
			return
		}
	}
	// Turn Let's Encrypt on.
	if req.SSL && req.LetsEncrypt && !curLE {
		if !s.letsEncryptSteps(w, r, [][]string{{"v-add-letsencrypt-domain", user, domain, " ", "yes", algo}}) {
			return
		}
	}
	// Add a pasted certificate.
	if pasted && !curSSL {
		if !s.mailSSLDir(w, r, "v-add-mail-domain-ssl", user, domain, req) {
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) mailDNS(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	if _, ok := s.mailDomain(w, r, user, domain); !ok {
		return
	}
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out, ok := s.output(w, r, "v-list-user-ips", user, "json")
	if !ok {
		return
	}
	ips, err := hestia.DecodeObjects[hestia.UserIP](out)
	if err != nil {
		serverError(w, err)
		return
	}
	// PHP reversed the list and took its first entry: the last address Hestia lists.
	ip := ""
	if len(ips) > 0 {
		ip = ips[len(ips)-1].Display()
	}
	out, ok = s.output(w, r, "v-list-mail-domain-dkim-dns", user, domain, "json")
	if !ok {
		return
	}
	dkim, err := hestia.DecodeObjects[hestia.DKIMRecord](out)
	if err != nil {
		serverError(w, err)
		return
	}

	const ttl = "14400"
	rows := []MailDNSRow{{Record: "mail." + domain, Type: "A", TTL: ttl, Value: ip}}
	if cfg["WEBMAIL_SYSTEM"] != "" {
		alias := cfg["WEBMAIL_ALIAS"]
		if alias == "" {
			alias = "webmail"
		}
		rows = append(rows, MailDNSRow{Record: alias + "." + domain, Type: "A", TTL: ttl, Value: ip})
	}
	rows = append(rows,
		MailDNSRow{Record: domain, Type: "MX", Priority: "10", TTL: ttl, Value: "mail." + domain + "."},
		MailDNSRow{Record: domain, Type: "TXT", TTL: ttl, Value: "v=spf1 a mx ip4:" + ip + " -all"},
		MailDNSRow{Record: "_dmarc", Type: "TXT", TTL: ttl, Value: "v=DMARC1; p=quarantine; pct=100"},
	)
	for i := len(dkim) - 1; i >= 0; i-- { // PHP reversed this list too
		d := dkim[i]
		rows = append(rows, MailDNSRow{Record: d.NAME, Type: "TXT", TTL: "3600",
			Value: strings.NewReplacer(`"`, "", "'", "").Replace(d.TXT)})
	}
	writeJSON(w, http.StatusOK, MailDNSView{Records: rows})
}
