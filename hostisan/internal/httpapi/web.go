package httpapi

import (
	"net/http"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
)

// registerWebRoutes mounts the web section under /api/v1/web. Each slice fills its own
// hook: domains, logs and downloads (web_domains.go), the settings form
// (web_edit.go), SSL and the certificate generator (web_ssl.go), and the quick
// installers (web_apps.go).
func (s *server) registerWebRoutes(mux *http.ServeMux) {
	s.webDomainRoutes(mux)
	s.webEditRoutes(mux)
	s.webSSLRoutes(mux)
	s.webAppRoutes(mux)
}

// webDomain lists one web domain of user (v-list-web-domain), answering the request
// itself on failure (ok is false then; a domain the user doesn't have is 404).
func (s *server) webDomain(w http.ResponseWriter, r *http.Request, user, domain string) (hestia.WebDomainDetail, bool) {
	out, ok := s.output(w, r, "v-list-web-domain", user, domain, "json")
	if !ok {
		return hestia.WebDomainDetail{}, false
	}
	ds, err := hestia.DecodeObjects[hestia.WebDomainDetail](out)
	if err != nil || len(ds) != 1 {
		serverError(w, firstErr(err, "v-list-web-domain: unexpected output"))
		return hestia.WebDomainDetail{}, false
	}
	return ds[0], true
}

// webDomainSSL lists a web domain's certificate (v-list-web-domain-ssl), answering the
// request itself on failure. The result holds the private key: never log it.
func (s *server) webDomainSSL(w http.ResponseWriter, r *http.Request, user, domain string) (hestia.WebSSL, bool) {
	out, ok := s.output(w, r, "v-list-web-domain-ssl", user, domain, "json")
	if !ok {
		return hestia.WebSSL{}, false
	}
	certs, err := hestia.DecodeObjects[hestia.WebSSL](out)
	if err != nil || len(certs) != 1 {
		// Not the output itself: it holds the key.
		serverError(w, firstErr(err, "v-list-web-domain-ssl: unexpected output"))
		return hestia.WebSSL{}, false
	}
	return certs[0], true
}

// splitWebList splits a textarea's text as PHP did for aliases and proxy extensions:
// newlines and commas become spaces, then it is split on runs of whitespace. The
// result is never nil.
func splitWebList(text string) []string {
	out := strings.Fields(strings.ReplaceAll(text, ",", " "))
	if out == nil {
		return []string{}
	}
	return out
}

// webRestartFlags says which services a save's steps need restarted.
type webRestartFlags struct {
	Web, Proxy, DNS bool
}

// webRestart runs the restarts a save flagged, in PHP's order (checklist section 3.2
// step 27): v-restart-web, v-restart-proxy (only with a PROXY_SYSTEM), v-restart-dns.
// It answers the first failure itself; false means it did.
func (s *server) webRestart(w http.ResponseWriter, r *http.Request, flags webRestartFlags) bool {
	var steps [][]string
	if flags.Web {
		steps = append(steps, []string{"v-restart-web"})
	}
	if flags.Proxy {
		cfg, err := s.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return false
		}
		if cfg["PROXY_SYSTEM"] != "" {
			steps = append(steps, []string{"v-restart-proxy"})
		}
	}
	if flags.DNS {
		steps = append(steps, []string{"v-restart-dns"})
	}
	return s.steps(w, r, steps)
}

// webKeyAlgos is the Let's Encrypt key types, in form order.
var webKeyAlgos = []string{"ecdsa-256", "ecdsa-384", "rsa"}

// webKeyAlgo is the key type the edit form preselects (Parity 21): the stored
// SSL_KEY_ALGO lowercased when it is a known type ("ecdsa" alone reads as ecdsa-256),
// else "rsa" for a domain that has SSL (a legacy certificate), else "ecdsa-256".
func webKeyAlgo(stored, ssl string) string {
	switch k := strings.ToLower(strings.TrimSpace(stored)); k {
	case "ecdsa-256", "ecdsa-384", "rsa":
		return k
	case "ecdsa":
		return "ecdsa-256"
	}
	if ssl == "yes" {
		return "rsa"
	}
	return "ecdsa-256"
}

// webJobKey is the shared job registry key of a domain's install jobs: one running job
// per user and domain. It is built only here; the status route compares against it.
func webJobKey(user, domain string) string {
	return "web:" + user + ":" + domain
}
