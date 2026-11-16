package hestia

// Web types follow the key order of Hestia's own JSON (web checklist section 6).
// Every value is a string as Hestia prints it; the object key becomes NAME.

// WebDomain is one domain from `v-list-web-domains`. ALIAS and PROXY_EXT are
// comma-separated. FTP_USER and FTP_PATH are colon-separated and aligned by index:
// full account names (<user>_<name>) and paths relative to the domain root (a leading
// "/", or "" for the root). STATS is "" or an engine; SSL and SUSPENDED are yes/no.
type WebDomain struct {
	NAME          string `json:"NAME"`
	IP            string `json:"IP"`
	IP6           string `json:"IP6"`
	DOCUMENT_ROOT string `json:"DOCUMENT_ROOT"`
	U_DISK        string `json:"U_DISK"`
	U_BANDWIDTH   string `json:"U_BANDWIDTH"`
	TPL           string `json:"TPL"`
	ALIAS         string `json:"ALIAS"`
	STATS         string `json:"STATS"`
	STATS_USER    string `json:"STATS_USER"`
	SSL           string `json:"SSL"`
	SSL_HOME      string `json:"SSL_HOME"`
	LETSENCRYPT   string `json:"LETSENCRYPT"`
	FTP_USER      string `json:"FTP_USER"`
	FTP_PATH      string `json:"FTP_PATH"`
	AUTH_USER     string `json:"AUTH_USER"`
	BACKEND       string `json:"BACKEND"`
	PROXY         string `json:"PROXY"`
	PROXY_EXT     string `json:"PROXY_EXT"`
	SUSPENDED     string `json:"SUSPENDED"`
	TIME          string `json:"TIME"`
	DATE          string `json:"DATE"`
}

func (d *WebDomain) SetName(name string) { d.NAME = name }

// WebDomainDetail is the one domain from `v-list-web-domain`: the list's keys plus
// SSL_FORCE, SSL_HSTS, SSL_KEY_ALGO, LETSENCRYPT_FAIL_COUNT, FASTCGI_CACHE,
// FASTCGI_DURATION, REDIRECT, REDIRECT_CODE and CUSTOM_DOCROOT. DOCUMENT_ROOT is
// CUSTOM_DOCROOT when set, else /home/<user>/web/<domain>/public_html/. REDIRECT_CODE is
// "" when never set (the form shows 301).
type WebDomainDetail struct {
	NAME                   string `json:"NAME"`
	IP                     string `json:"IP"`
	IP6                    string `json:"IP6"`
	DOCUMENT_ROOT          string `json:"DOCUMENT_ROOT"`
	U_DISK                 string `json:"U_DISK"`
	U_BANDWIDTH            string `json:"U_BANDWIDTH"`
	TPL                    string `json:"TPL"`
	ALIAS                  string `json:"ALIAS"`
	STATS                  string `json:"STATS"`
	STATS_USER             string `json:"STATS_USER"`
	SSL                    string `json:"SSL"`
	SSL_FORCE              string `json:"SSL_FORCE"`
	SSL_HSTS               string `json:"SSL_HSTS"`
	SSL_HOME               string `json:"SSL_HOME"`
	SSL_KEY_ALGO           string `json:"SSL_KEY_ALGO"`
	LETSENCRYPT_FAIL_COUNT string `json:"LETSENCRYPT_FAIL_COUNT"`
	LETSENCRYPT            string `json:"LETSENCRYPT"`
	FTP_USER               string `json:"FTP_USER"`
	FTP_PATH               string `json:"FTP_PATH"`
	AUTH_USER              string `json:"AUTH_USER"`
	BACKEND                string `json:"BACKEND"`
	PROXY                  string `json:"PROXY"`
	PROXY_EXT              string `json:"PROXY_EXT"`
	FASTCGI_CACHE          string `json:"FASTCGI_CACHE"`
	FASTCGI_DURATION       string `json:"FASTCGI_DURATION"`
	REDIRECT               string `json:"REDIRECT"`
	REDIRECT_CODE          string `json:"REDIRECT_CODE"`
	CUSTOM_DOCROOT         string `json:"CUSTOM_DOCROOT"`
	SUSPENDED              string `json:"SUSPENDED"`
	TIME                   string `json:"TIME"`
	DATE                   string `json:"DATE"`
}

func (d *WebDomainDetail) SetName(name string) { d.NAME = name }

// WebSSL is `v-list-web-domain-ssl`'s one entry. CRT, KEY and CA are PEM text ("" when
// missing); ALIASES is the comma-separated SAN names; PUB_KEY is e.g. "256 bit". KEY is
// a secret: never log it.
type WebSSL struct {
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
func (s *WebSSL) SetName(string) {}

// ValidFTPUserArg reports whether s is safe to pass to a v-* command as an FTP account
// name (short or full): 1 to 30 ASCII characters from [A-Za-z0-9_.-], not starting
// with "-" (it would read as an option). Hestia's own format check is stricter.
func ValidFTPUserArg(s string) bool {
	if s == "" || len(s) > 30 || s[0] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}
