package httpapi

// Mail wire types (Plan 2c). Generated into ui/src/api/types.gen.ts with the rest.
//
// Convention: a pointer field with omitempty (TS "field?") means "not offered: leave
// as is". The UI leaves it out when the server or the signed-in role doesn't offer
// that control (antispam with no ANTISPAM_SYSTEM, rate limit for non-admins, the
// webmail client with no IMAP/WEBMAIL system), and the handler then runs no command
// for it. Paths are under /api/v1/mail; see ui/src/api/mail.ts for the routes.

import "github.com/hostisan/hostisan/internal/hestia"

// MailDomains answers GET /api/v1/mail, in Hestia's order.
type MailDomains struct {
	Domains []hestia.MailDomain `json:"domains"`
	Usage   MailUsage           `json:"usage"`
}

// MailUsage is the mail tab's usage summary from v-list-user, as Hestia strings
// ("unlimited" or a number): U_MAIL_DOMAINS / MAIL_DOMAINS, SUSPENDED_MAIL,
// U_MAIL_ACCOUNTS and the account limit, which is MAIL_ACCOUNTS times MAIL_DOMAINS
// ("unlimited" when either is).
type MailUsage struct {
	Domains       string `json:"domains"`
	DomainsLimit  string `json:"domainsLimit"`
	Suspended     string `json:"suspended"`
	Accounts      string `json:"accounts"`
	AccountsLimit string `json:"accountsLimit"`
}

// MailOptions feeds the mail forms and lists.
type MailOptions struct {
	// WebmailClients is v-list-sys-webmail without blanks ([] when none, never [""]).
	WebmailClients []string `json:"webmailClients"`
	// WebmailAlias is the system WEBMAIL_ALIAS, or "webmail" when unset. Webmail links
	// are https://<webmailAlias>.<domain>/.
	WebmailAlias string `json:"webmailAlias"`
	Antispam     bool   `json:"antispam"`  // ANTISPAM_SYSTEM is set
	Antivirus    bool   `json:"antivirus"` // ANTIVIRUS_SYSTEM is set
	Webmail      bool   `json:"webmail"`   // WEBMAIL_SYSTEM is set
	Imap         bool   `json:"imap"`      // IMAP_SYSTEM is set
	// CanSetRate is true when the signed-in user (not the one being viewed) is an
	// admin; rate limits from anyone else are ignored.
	CanSetRate bool `json:"canSetRate"`
	// AdminGate is true when the account being viewed is an admin: adding a mail
	// domain then first shows the "create a standard user" warning.
	AdminGate bool `json:"adminGate"`
	// KeyAlgos is the Let's Encrypt key types, in form order: ecdsa-256, ecdsa-384, rsa.
	KeyAlgos []string `json:"keyAlgos"`
}

// MailSMTPRelay is a domain's outgoing SMTP relay. Host is required. Port "" means
// 587. Hestia's v-add-mail-domain-smtp-relay rewrites the whole relay (host, port,
// user and pass) every time it runs, and the stored password is never listed back, so
// an empty Password can never mean "keep the stored one".
//
// Password rule, on add and on edit: when the relay is being written, Password must
// not be empty if User is set (blank-field error, `Field "%s" can not be blank.`,
// args ["Password"]). An empty User with an empty Password is an unauthenticated
// relay and is allowed (the script takes USERNAME and PASSWORD as optional).
type MailSMTPRelay struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// AddMailDomainRequest is POST /api/v1/mail. Domain is trimmed, a leading "www." is
// stripped and it is lowercased. Webmail "" means no webmail client (no
// v-add-mail-domain-webmail call); otherwise it is a client name. SMTPRelay absent
// means no relay; see MailSMTPRelay for its password rule. DKIMSelector "" means
// "mail". Antispam, Reject and Antivirus may be absent: absent = off (the UI leaves out
// what the server doesn't offer); omitempty only makes them optional in TypeScript.
type AddMailDomainRequest struct {
	Domain       string         `json:"domain"`
	Webmail      string         `json:"webmail"`
	Antispam     bool           `json:"antispam,omitempty"`
	Reject       bool           `json:"reject,omitempty"`
	Antivirus    bool           `json:"antivirus,omitempty"`
	DKIM         bool           `json:"dkim"`
	DKIMSelector string         `json:"dkimSelector"`
	SMTPRelay    *MailSMTPRelay `json:"smtpRelay,omitempty"`
}

// EditMailDomainRequest is PUT /api/v1/mail/{domain}. The pointer fields with
// omitempty are "not offered, leave as is" (see the file comment): Webmail, Rate
// ("" means "system"), Antispam, Reject and Antivirus.
//
// Webmail: nil = not offered, no change. "" = remove webmail
// (`v-delete-mail-domain-webmail <u> <d> yes`), the same meaning as on add.
// "disabled" = Hestia's own disabled value (`v-add-mail-domain-webmail <u> <d>
// disabled yes`). Any other value is a client name (`v-add-mail-domain-webmail <u>
// <d> <client> yes`).
//
// DKIMSelector "" means "mail", as on add.
//
// SMTPRelay is always sent, as the form's state, never left out:
//   - null: relay off. The relay is deleted only if it is currently on
//     (U_SMTP_RELAY == "true"); otherwise nothing runs.
//   - an object: relay on with these values. The relay is (re)written when it is
//     enabled from off, or Host, User or Port (after "" -> 587) differ from the stored
//     values, or Password is non-empty. In that case an empty Password with a User
//     set is a blank-field error (`Field "%s" can not be blank.`, args
//     ["Password"]), because Hestia cannot keep the stored password: the script
//     rewrites it each time. When the relay is already on, nothing changed and Password
//     is empty, no relay command runs.
//
// "Unchanged" is therefore null for a domain without a relay, or the stored host, port
// and user with an empty password for one with a relay: neither runs a command. Go
// cannot tell an absent field from null, so absent also means "off".
type EditMailDomainRequest struct {
	Webmail      *string        `json:"webmail,omitempty"`
	Catchall     string         `json:"catchall"` // "" removes the catch-all
	Rate         *string        `json:"rate,omitempty"`
	Antispam     *bool          `json:"antispam,omitempty"`
	Reject       *bool          `json:"reject,omitempty"`
	Antivirus    *bool          `json:"antivirus,omitempty"`
	DKIM         bool           `json:"dkim"`
	DKIMSelector string         `json:"dkimSelector"`
	SMTPRelay    *MailSMTPRelay `json:"smtpRelay" tstype:"MailSMTPRelay | null,required"`
}

// MailDomainView answers GET /api/v1/mail/{domain}. SSL is set when the domain's SSL
// is not "no"; its KEY is the stored private key, for the edit form.
type MailDomainView struct {
	Domain hestia.MailDomainDetail `json:"domain"`
	SSL    *hestia.MailSSL         `json:"ssl,omitempty"`
}

// MailSSLRequest is PUT /api/v1/mail/{domain}/ssl: the SSL section's state. KeyAlgo is
// one of MailOptions.KeyAlgos ("ecdsa-256" when not). Crt, Key and CA are the pasted
// PEM text, used when Let's Encrypt is off.
type MailSSLRequest struct {
	SSL         bool   `json:"ssl"`
	LetsEncrypt bool   `json:"letsencrypt"`
	KeyAlgo     string `json:"keyAlgo"`
	Crt         string `json:"crt"`
	Key         string `json:"key"`
	CA          string `json:"ca"`
}

// MailDNSView answers GET /api/v1/mail/{domain}/dns: the records to publish, in the
// checklist's order.
type MailDNSView struct {
	Records []MailDNSRow `json:"records"`
}

// MailDNSRow is one suggested DNS record; Priority is "" except for MX.
type MailDNSRow struct {
	Record   string `json:"record"`
	Type     string `json:"type"`
	Priority string `json:"priority"`
	TTL      string `json:"ttl"`
	Value    string `json:"value"`
}

// MailAccounts answers GET /api/v1/mail/{domain}/accounts, in Hestia's order.
type MailAccounts struct {
	Domain   hestia.MailDomainDetail `json:"domain"`
	Accounts []hestia.MailAccount    `json:"accounts"`
}

// MailAccountView answers GET /api/v1/mail/{domain}/accounts/{account}.
// AutoreplyMessage is "" unless AUTOREPLY is "yes".
type MailAccountView struct {
	Account          hestia.MailAccountDetail `json:"account"`
	AutoreplyMessage string                   `json:"autoreplyMessage"`
}

// AddMailAccountRequest is POST /api/v1/mail/{domain}/accounts. Quota is MB,
// "unlimited" (accepted as is), or "" (unlimited, sent to Hestia as 0). Aliases and Forwards are the raw textarea text; the
// server splits them on newlines, commas and whitespace as PHP did. Discard ignores
// Forwards. Rate is admin-only ("not offered" otherwise). SendTo "" sends no email.
type AddMailAccountRequest struct {
	Account     string  `json:"account"`
	Password    string  `json:"password"`
	Quota       string  `json:"quota"`
	Aliases     string  `json:"aliases"`
	Forwards    string  `json:"forwards"`
	Discard     bool    `json:"discard"`
	ForwardOnly bool    `json:"forwardOnly"`
	Rate        *string `json:"rate,omitempty"`
	SendTo      string  `json:"sendTo"`
}

// EditMailAccountRequest is PUT /api/v1/mail/{domain}/accounts/{account}. Password ""
// keeps the current one. AutoreplyMessage is used when Autoreply is on and must not be
// empty then. Rate "" means "system"; absent means "not offered". Quota is MB,
// "unlimited" (accepted as is), or "" (unlimited, sent to Hestia as 0).
type EditMailAccountRequest struct {
	Password         string  `json:"password"`
	Quota            string  `json:"quota"`
	Aliases          string  `json:"aliases"`
	Forwards         string  `json:"forwards"`
	Discard          bool    `json:"discard"`
	ForwardOnly      bool    `json:"forwardOnly"`
	Autoreply        bool    `json:"autoreply"`
	AutoreplyMessage string  `json:"autoreplyMessage"`
	Rate             *string `json:"rate,omitempty"`
	SendTo           string  `json:"sendTo"`
}
