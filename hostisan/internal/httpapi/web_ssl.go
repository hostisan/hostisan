package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/secret"
)

// webSSLRoutes registers Slice C's routes: PUT /api/v1/web/{domain}/ssl and POST
// /api/v1/web/generate-ssl.
func (s *server) webSSLRoutes(mux *http.ServeMux) {
	mux.Handle("PUT /api/v1/web/{domain}/ssl", s.write(s.webSSLSave))
	mux.Handle("POST /api/v1/web/generate-ssl", s.write(s.webGenerateSSL))
}

// webSSLFiles is the certificate files for a temp directory, named <domain>.crt, .key
// and .ca. Line endings become "\n"; blank texts are left out. extra adds PHP's extra
// trailing "\n" (the replace path); otherwise the text only gets a final newline when
// it has none.
func webSSLFiles(domain string, req WebSSLRequest, extra bool) map[string]string {
	files := map[string]string{}
	for ext, v := range map[string]string{"crt": req.Crt, "key": req.Key, "ca": req.CA} {
		if strings.TrimSpace(v) == "" {
			continue
		}
		t := strings.ReplaceAll(v, "\r\n", "\n")
		if extra || !strings.HasSuffix(t, "\n") {
			t += "\n"
		}
		files[domain+"."+ext] = t
	}
	return files
}

// webSSLDir writes the certificate files to a private temp directory, runs cmd on it
// (cmd's own arguments come after the directory) and removes the directory again. The
// key never reaches a log or an error.
func (s *server) webSSLDir(w http.ResponseWriter, r *http.Request, files map[string]string, cmd string, args func(dir string) []string) bool {
	dir, cleanup, err := secret.TempDir("/tmp", files)
	if err != nil {
		serverError(w, err)
		return false
	}
	defer cleanup()
	if err := s.run(r.Context(), cmd, args(dir)...); err != nil {
		writeCommandError(w, err)
		return false
	}
	return true
}

// webSSLSave saves the SSL section (checklist section 3.2 steps 6c and 7-15, with
// Parity 4, 6 and 7). At most one Let's Encrypt command runs; the first error stops it.
func (s *server) webSSLSave(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req WebSSLRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user := s.actingUser(r)
	cur, ok := s.webDomain(w, r, user, domain)
	if !ok {
		return
	}
	curSSL, curLE := cur.SSL == "yes", cur.LETSENCRYPT == "yes"
	wantLE := req.SSL && req.LetsEncrypt
	pasted := req.SSL && !req.LetsEncrypt
	algo := mailKeyAlgo(req.KeyAlgo)
	haveCert := func() bool {
		return blankFields(w, "ssl certificate", pemText(req.Crt), "ssl key", pemText(req.Key))
	}
	// Adding a certificate, also after Let's Encrypt is removed, needs the pasted one;
	// checking before any command keeps a typo from costing the user their certificate.
	if pasted && (!curSSL || curLE) && !haveCert() {
		return
	}
	force := curSSL && cur.SSL_FORCE == "yes"
	hsts := curSSL && cur.SSL_HSTS == "yes"
	ran := false

	// 1. Re-issue the Let's Encrypt certificate.
	if wantLE && curLE && (req.Reissue || algo != webKeyAlgo(cur.SSL_KEY_ALGO, cur.SSL)) {
		if !s.letsEncryptSteps(w, r, [][]string{{"v-add-letsencrypt-domain", user, domain, cur.ALIAS, "", algo}}) {
			return
		}
		ran = true
	}
	// 2. Replace a custom certificate whose text changed. The comparison uses pemText
	// (\r\n normalisation and trim), so a whitespace-only edit does not replace the
	// certificate (intentional). The v-list-web-domain-ssl read comes before haveCert(),
	// still before any mutating command.
	if pasted && curSSL && !curLE {
		stored, ok := s.webDomainSSL(w, r, user, domain)
		if !ok {
			return
		}
		if pemText(req.Crt) != pemText(stored.CRT) || pemText(req.Key) != pemText(stored.KEY) || pemText(req.CA) != pemText(stored.CA) {
			if !haveCert() || !s.webSSLDir(w, r, webSSLFiles(domain, req, true), "v-change-web-domain-sslcert",
				func(dir string) []string { return []string{user, domain, dir, "no"} }) {
				return
			}
			ran = true
		}
	}
	// 3. Turn Let's Encrypt off. Deleting the certificate also turns SSL off.
	if curLE && !wantLE {
		if !s.letsEncryptSteps(w, r, [][]string{{"v-delete-letsencrypt-domain", user, domain, ""}}) {
			return
		}
		ran, curSSL, force, hsts = true, false, false, false
	}
	// 4. Turn SSL off on a domain without Let's Encrypt.
	if curSSL && !req.SSL {
		if !s.steps(w, r, [][]string{{"v-delete-web-domain-ssl", user, domain, "no"}}) {
			return
		}
		ran, curSSL, force, hsts = true, false, false, false
	}
	// 5. Turn Let's Encrypt on.
	if wantLE && !curLE {
		if !s.letsEncryptSteps(w, r, [][]string{{"v-add-letsencrypt-domain", user, domain, cur.ALIAS, "", algo}}) {
			return
		}
		ran, curSSL = true, true
	}
	// 6. Add a custom certificate.
	if pasted && !curSSL {
		home := cur.SSL_HOME
		if home == "" {
			home = "same"
		}
		if !s.webSSLDir(w, r, webSSLFiles(domain, req, false), "v-add-web-domain-ssl",
			func(dir string) []string { return []string{user, domain, dir, home, "no"} }) {
			return
		}
		ran = true
	}
	// 7. Force-HTTPS and HSTS, only when their state changes.
	var steps [][]string
	toggle := func(have, want bool, name string) {
		switch {
		case want && !have:
			steps = append(steps, []string{"v-add-web-domain-ssl-" + name, user, domain})
		case !want && have:
			steps = append(steps, []string{"v-delete-web-domain-ssl-" + name, user, domain})
		}
	}
	toggle(force, req.SSL && req.ForceSSL, "force")
	toggle(hsts, req.SSL && req.HSTS, "hsts")
	if !s.steps(w, r, steps) {
		return
	}
	// 8. Restarts.
	if ran || len(steps) > 0 {
		if !s.webRestart(w, r, webRestartFlags{Web: true, Proxy: true}) {
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// webSSLTextArg reports whether v is safe as a positional argument: no leading "-" and
// no control characters (a newline included).
func webSSLTextArg(v string) bool {
	if strings.HasPrefix(v, "-") {
		return false
	}
	return !strings.ContainsFunc(v, func(c rune) bool { return c < 0x20 || c == 0x7f })
}

// webGenerateSSL makes a self-signed certificate (checklist section 3.8, Parity 17).
// It needs no web domain and no user argument.
func (s *server) webGenerateSSL(w http.ResponseWriter, r *http.Request) {
	var req WebGenerateSSLRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !slices.Contains(webKeyAlgos, req.KeyAlgo) {
		WriteError(w, http.StatusBadRequest, "invalid", "Invalid key algorithm selected.")
		return
	}
	// The fields are trimmed; PHP passed them raw (a harmless divergence).
	domain, email := strings.TrimSpace(req.Domain), strings.TrimSpace(req.Email)
	country, state := strings.TrimSpace(req.Country), strings.TrimSpace(req.State)
	locality, org := strings.TrimSpace(req.Locality), strings.TrimSpace(req.Org)
	if !blankFields(w, "Domain", domain, "Country", country, "State", state, "City", locality, "Organization", org) {
		return
	}
	aliases := splitWebList(req.Aliases)
	// An unsafe value is 400 invalid, naming the field in Args like the blank check.
	badAlias := ""
	if i := slices.IndexFunc(aliases, func(a string) bool { return !webSSLTextArg(a) }); i >= 0 {
		badAlias = aliases[i]
	}
	checks := []struct {
		label, value string
		ok           bool
	}{
		{"Domain", domain, hestia.ValidDomainArg(domain)},
		{"Email", email, webSSLTextArg(email)},
		{"Country", country, webSSLTextArg(country)},
		{"State", state, webSSLTextArg(state)},
		{"City", locality, webSSLTextArg(locality)},
		{"Organization", org, webSSLTextArg(org)},
		{"Aliases", badAlias, badAlias == ""},
	}
	for _, c := range checks {
		if !c.ok {
			invalidFormat(w, c.label, c.value)
			return
		}
	}
	out, ok := s.output(w, r, "v-generate-ssl-cert", domain, email, country, state, locality, org, "IT",
		strings.Join(aliases, "\n"), "json", req.KeyAlgo)
	if !ok {
		return
	}
	var certs map[string]WebGeneratedSSL
	// The output holds the key: never put it in an error.
	if err := json.Unmarshal(out, &certs); err != nil || len(certs) != 1 {
		serverError(w, firstErr(nil, "v-generate-ssl-cert: unexpected output"))
		return
	}
	for _, c := range certs {
		writeJSON(w, http.StatusOK, WebGeneratedSSL{Crt: c.Crt, Key: c.Key, CSR: c.CSR})
	}
}
