package hestia

import (
	"encoding/json"
	"strings"
)

// Mail types follow the key order of Hestia's own JSON (mail checklist section 6.1).
// Every value is a string as Hestia prints it; the object key becomes NAME.

// MailDomain is one domain from `v-list-mail-domains`. WEBMAIL_ALIAS here is the bare
// system alias (e.g. "webmail"), not alias.domain.
type MailDomain struct {
	NAME          string `json:"NAME"`
	ANTIVIRUS     string `json:"ANTIVIRUS"`
	ANTISPAM      string `json:"ANTISPAM"`
	REJECT        string `json:"REJECT"`
	RATE_LIMIT    string `json:"RATE_LIMIT"`
	DKIM          string `json:"DKIM"`
	CATCHALL      string `json:"CATCHALL"`
	ACCOUNTS      string `json:"ACCOUNTS"`
	U_DISK        string `json:"U_DISK"`
	SSL           string `json:"SSL"`
	SUSPENDED     string `json:"SUSPENDED"`
	TIME          string `json:"TIME"`
	DATE          string `json:"DATE"`
	WEBMAIL_ALIAS string `json:"WEBMAIL_ALIAS"`
	WEBMAIL       string `json:"WEBMAIL"`
}

func (d *MailDomain) SetName(name string) { d.NAME = name }

// MailDomainDetail is the one domain from `v-list-mail-domain`. WEBMAIL_ALIAS is
// alias.domain (e.g. "webmail.example.com"). U_SMTP_RELAY is "true", "false" or "";
// the relay password is never listed.
type MailDomainDetail struct {
	NAME                  string `json:"NAME"`
	ANTIVIRUS             string `json:"ANTIVIRUS"`
	ANTISPAM              string `json:"ANTISPAM"`
	DKIM                  string `json:"DKIM"`
	DKIM_SELECTOR         string `json:"DKIM_SELECTOR"`
	CATCHALL              string `json:"CATCHALL"`
	ACCOUNTS              string `json:"ACCOUNTS"`
	RATE_LIMIT            string `json:"RATE_LIMIT"`
	REJECT                string `json:"REJECT"`
	U_DISK                string `json:"U_DISK"`
	SSL                   string `json:"SSL"`
	SSL_KEY_ALGO          string `json:"SSL_KEY_ALGO"`
	LETSENCRYPT           string `json:"LETSENCRYPT"`
	SUSPENDED             string `json:"SUSPENDED"`
	TIME                  string `json:"TIME"`
	DATE                  string `json:"DATE"`
	WEBMAIL_ALIAS         string `json:"WEBMAIL_ALIAS"`
	WEBMAIL               string `json:"WEBMAIL"`
	U_SMTP_RELAY          string `json:"U_SMTP_RELAY"`
	U_SMTP_RELAY_HOST     string `json:"U_SMTP_RELAY_HOST"`
	U_SMTP_RELAY_PORT     string `json:"U_SMTP_RELAY_PORT"`
	U_SMTP_RELAY_USERNAME string `json:"U_SMTP_RELAY_USERNAME"`
}

func (d *MailDomainDetail) SetName(name string) { d.NAME = name }

// MailAccount is one account from `v-list-mail-accounts`. ALIAS and FWD are
// comma-separated (FWD ":blackhole:" discards all mail); QUOTA is MB or "unlimited".
type MailAccount struct {
	NAME      string `json:"NAME"`
	ALIAS     string `json:"ALIAS"`
	FWD       string `json:"FWD"`
	FWD_ONLY  string `json:"FWD_ONLY"`
	AUTOREPLY string `json:"AUTOREPLY"`
	QUOTA     string `json:"QUOTA"`
	U_DISK    string `json:"U_DISK"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (a *MailAccount) SetName(name string) { a.NAME = name }

// MailAccountDetail is the one account from `v-list-mail-account`: MailAccount plus
// RATE_LIMIT and WEBMAIL_ALIAS (alias.domain).
type MailAccountDetail struct {
	NAME          string `json:"NAME"`
	ALIAS         string `json:"ALIAS"`
	FWD           string `json:"FWD"`
	FWD_ONLY      string `json:"FWD_ONLY"`
	AUTOREPLY     string `json:"AUTOREPLY"`
	QUOTA         string `json:"QUOTA"`
	RATE_LIMIT    string `json:"RATE_LIMIT"`
	U_DISK        string `json:"U_DISK"`
	SUSPENDED     string `json:"SUSPENDED"`
	TIME          string `json:"TIME"`
	DATE          string `json:"DATE"`
	WEBMAIL_ALIAS string `json:"WEBMAIL_ALIAS"`
}

func (a *MailAccountDetail) SetName(name string) { a.NAME = name }

// MailAutoreply is `v-list-mail-account-autoreply`'s one entry; MSG is "" when the
// account has no message file.
type MailAutoreply struct {
	NAME string `json:"NAME"`
	MSG  string `json:"MSG"`
}

func (a *MailAutoreply) SetName(name string) { a.NAME = name }

// MailSSL is `v-list-mail-domain-ssl`'s one entry. CRT, KEY and CA are PEM text ("" when
// missing); ALIASES is comma-separated SANs. KEY is a secret: never log it.
type MailSSL struct {
	CRT        string `json:"CRT"`
	KEY        string `json:"KEY"`
	CA         string `json:"CA"`
	SUBJECT    string `json:"SUBJECT"`
	ALIASES    string `json:"ALIASES"`
	NOT_BEFORE string `json:"NOT_BEFORE"`
	NOT_AFTER  string `json:"NOT_AFTER"`
	SIGNATURE  string `json:"SIGNATURE"`
	PUB_KEY    string `json:"PUB_KEY"`
	ISSUER     string `json:"ISSUER"`
}

// SetName ignores the key: it is the domain, which the caller already knows.
func (s *MailSSL) SetName(string) {}

// DKIMRecord is one entry from `v-list-mail-domain-dkim-dns`; the key (e.g.
// "mail._domainkey") becomes NAME. TXT is wrapped in single quotes as Hestia prints it.
type DKIMRecord struct {
	NAME string `json:"NAME"`
	TTL  string `json:"TTL"`
	TXT  string `json:"TXT"`
}

func (r *DKIMRecord) SetName(name string) { r.NAME = name }

// WebmailClients decodes `v-list-sys-webmail json`, dropping blank entries: with no
// webmail system Hestia prints [""]. The result is never nil.
func WebmailClients(stdout []byte) ([]string, error) {
	var raw []string
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, err
	}
	out := []string{}
	for _, c := range raw {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out, nil
}
