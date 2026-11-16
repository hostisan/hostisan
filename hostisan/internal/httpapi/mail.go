package httpapi

import (
	"net/http"

	"github.com/hostisan/hostisan/internal/hestia"
)

// registerMailRoutes mounts the mail section under /api/v1/mail. Each slice fills its
// own hook: domains (mail_domains.go), accounts (mail_accounts.go), and SSL plus the
// DNS records view (mail_extras.go).
func (s *server) registerMailRoutes(mux *http.ServeMux) {
	s.mailDomainRoutes(mux)
	s.mailAccountRoutes(mux)
	s.mailExtraRoutes(mux)
}

// mailAccountParam is the {account} path value; it answers 400 itself when unusable.
// Use domainParam (dns.go) for {domain}.
func mailAccountParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	a := r.PathValue("account")
	if !hestia.ValidMailAccountArg(a) {
		invalidFormat(w, "account", a)
		return "", false
	}
	return a, true
}

// mailDomain lists one mail domain of user (v-list-mail-domain), answering the request
// itself on failure (ok is false then; a missing domain is 404).
func (s *server) mailDomain(w http.ResponseWriter, r *http.Request, user, domain string) (hestia.MailDomainDetail, bool) {
	out, ok := s.output(w, r, "v-list-mail-domain", user, domain, "json")
	if !ok {
		return hestia.MailDomainDetail{}, false
	}
	ds, err := hestia.DecodeObjects[hestia.MailDomainDetail](out)
	if err != nil || len(ds) != 1 {
		serverError(w, firstErr(err, "v-list-mail-domain: unexpected output"))
		return hestia.MailDomainDetail{}, false
	}
	return ds[0], true
}

// mailDomainSSL lists a mail domain's certificate (v-list-mail-domain-ssl), answering
// the request itself on failure. The result holds the private key: never log it.
func (s *server) mailDomainSSL(w http.ResponseWriter, r *http.Request, user, domain string) (hestia.MailSSL, bool) {
	out, ok := s.output(w, r, "v-list-mail-domain-ssl", user, domain, "json")
	if !ok {
		return hestia.MailSSL{}, false
	}
	certs, err := hestia.DecodeObjects[hestia.MailSSL](out)
	if err != nil || len(certs) != 1 {
		serverError(w, firstErr(err, "v-list-mail-domain-ssl: unexpected output"))
		return hestia.MailSSL{}, false
	}
	return certs[0], true
}
