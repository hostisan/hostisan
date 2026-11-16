package httpapi

// Web wire types (Plan 2d). Generated into ui/src/api/types.gen.ts with the rest.
//
// Convention, as in 2c: a pointer field with omitempty (TS "field?") means "not
// offered: leave as is". The UI leaves it out when the server or the signed-in role
// doesn't offer that control, and the handler then runs no command for it. Paths are
// under /api/v1/web; see ui/src/api/web.ts for the routes. Values called "Hestia
// strings" are passed through as Hestia printed them ("unlimited" or a number).
//
// Installs run as background jobs: POST /api/v1/web/{domain}/apps/{app} answers 202
// JobRef and GET /api/v1/web/{domain}/apps/jobs/{job} answers JobStatus (types.go). A
// failed install's JobStatus.error carries the checklist section 4 msgid and args; a
// successful one has no result (null).

import "github.com/hostisan/hostisan/internal/hestia"

// WebDomains answers GET /api/v1/web, in Hestia's order (the UI sorts by the user's
// sortOrder).
type WebDomains struct {
	Domains []hestia.WebDomain `json:"domains"`
	// DisplayIPs maps each IP of the listed domains (only those) to the address to
	// show: its NAT address from v-list-sys-ips, or the IP itself.
	DisplayIPs map[string]string `json:"displayIps"`
	Usage      WebUsage          `json:"usage"`
}

// WebUsage is the web tab's usage summary from v-list-user, as Hestia strings:
// U_WEB_DOMAINS / WEB_DOMAINS, SUSPENDED_WEB, U_WEB_ALIASES and the alias limit, which
// is "unlimited" when WEB_ALIASES or WEB_DOMAINS is, else WEB_ALIASES times WEB_DOMAINS
// (checklist section 1).
type WebUsage struct {
	Domains      string `json:"domains"`
	DomainsLimit string `json:"domainsLimit"`
	Suspended    string `json:"suspended"`
	Aliases      string `json:"aliases"`
	AliasesLimit string `json:"aliasesLimit"`
}

// WebIP is one address the viewed user may use (v-list-user-ips): IP is the internal
// address sent back in requests; Display is its NAT address, or IP when it has none.
type WebIP struct {
	IP      string `json:"ip"`
	Display string `json:"display"`
}

// WebOptions answers GET /api/v1/web/options: what the server offers to the web forms
// and lists, for the viewed account and the signed-in role.
type WebOptions struct {
	// IPs is v-list-user-ips for the viewed user, in Hestia's order; the add form
	// selects the first.
	IPs []WebIP `json:"ips"`
	// WebSystem, WebBackend and ProxySystem are the raw config values (WEB_SYSTEM,
	// WEB_BACKEND, PROXY_SYSTEM; "" when unset). The UI uppercases them for badges.
	WebSystem   string `json:"webSystem"`
	WebBackend  string `json:"webBackend"`
	ProxySystem string `json:"proxySystem"`
	// Nginx: WEB_SYSTEM is nginx, so the FastCGI cache is offered.
	Nginx bool `json:"nginx"`
	// Purge: WEB_SYSTEM or PROXY_SYSTEM is nginx, so cache purge (single and bulk) is
	// offered.
	Purge bool `json:"purge"`
	// FTP: FTP_SYSTEM is vsftpd or proftpd, so FTP accounts are offered.
	FTP bool `json:"ftp"`
	// StatsTypes is v-list-web-stats: "none" first, then the engines.
	StatsTypes []string `json:"statsTypes"`
	// DNS and Mail: the system is set (DNS_SYSTEM, IMAP_SYSTEM) and the viewed
	// account's package limit (DNS_DOMAINS, MAIL_DOMAINS) isn't "0". They decide
	// whether the add form shows its DNS and Mail boxes.
	DNS  bool `json:"dns"`
	Mail bool `json:"mail"`
	// AppInstaller: PLUGIN_APP_INSTALLER is not exactly "false". When false the Quick
	// Install entries are hidden and the apps routes answer 403.
	AppInstaller bool `json:"appInstaller"`
	// CanEditTemplates: the signed-in user is an admin, or
	// POLICY_USER_EDIT_WEB_TEMPLATES is "yes". Without it the template fields are
	// hidden and ignored by the server.
	CanEditTemplates bool `json:"canEditTemplates"`
	// Templates, BackendTemplates and ProxyTemplates are the select options, [] unless
	// CanEditTemplates. BackendTemplates is [] without WEB_BACKEND, ProxyTemplates []
	// without PROXY_SYSTEM.
	Templates        []string `json:"templates"`
	BackendTemplates []string `json:"backendTemplates"`
	ProxyTemplates   []string `json:"proxyTemplates"`
	// DefaultProxyExt is Hestia's default proxy extension list, in order.
	DefaultProxyExt []string `json:"defaultProxyExt"`
	// CanRebuild: the signed-in user (not the viewed one) is an admin; bulk then
	// offers Rebuild.
	CanRebuild bool `json:"canRebuild"`
	// AdminGate: the viewed account is an admin, so the add form first shows the
	// "create a standard user" warning.
	AdminGate bool `json:"adminGate"`
	// FTPPrefix is "<user>_", shown before FTP account names.
	FTPPrefix string `json:"ftpPrefix"`
	// WebRoot is "/home/<user>/web/", shown before document root and FTP paths.
	WebRoot string `json:"webRoot"`
	// KeyAlgos is the Let's Encrypt key types, in form order: ecdsa-256, ecdsa-384, rsa.
	KeyAlgos []string `json:"keyAlgos"`
}

// WebAddRequest is POST /api/v1/web. Domain is trimmed, a leading "www." is stripped
// and it is lowercased. IP is a WebIP.IP of the viewed user. DNS and Mail are the
// boxes; each is ignored when WebOptions doesn't offer it.
type WebAddRequest struct {
	Domain string `json:"domain"`
	IP     string `json:"ip"`
	DNS    bool   `json:"dns"`
	Mail   bool   `json:"mail"`
}

// WebDomainView answers GET /api/v1/web/{domain}: the edit form's state.
type WebDomainView struct {
	Domain hestia.WebDomainDetail `json:"domain"`
	// SSL is set when the domain's SSL is not "no"; its KEY is the stored private key,
	// for the edit form.
	SSL *hestia.WebSSL `json:"ssl,omitempty"`
	// Aliases is ALIAS split on commas, in Hestia's order ([] when none).
	Aliases []string `json:"aliases"`
	// DisplayIP is the domain IP's NAT address, or the IP itself.
	DisplayIP string `json:"displayIp"`
	// KeyAlgo is the key type to preselect: SSL_KEY_ALGO lowercased when it is one of
	// KeyAlgos ("ecdsa" reads as ecdsa-256), else "rsa" for a domain with SSL, else
	// "ecdsa-256" (Parity 21).
	KeyAlgo string `json:"keyAlgo"`
	// FTP is FTP_USER/FTP_PATH paired by index ([] when none).
	FTP []WebFTPAccount `json:"ftp"`
	// Docroot is CUSTOM_DOCROOT parsed.
	Docroot WebDocroot `json:"docroot"`
	// Redirect is REDIRECT and REDIRECT_CODE parsed.
	Redirect WebRedirectView `json:"redirect"`
}

// WebFTPAccount is one stored FTP account. Name is the short name, without "<user>_".
// Path is relative to the domain root with a leading "/", or "" for the root itself.
type WebFTPAccount struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// WebDocroot is the custom document root. Enabled is CUSTOM_DOCROOT != "". Domain and
// Folder are parsed lexically from /home/<user>/web/<domain>/public_html/<folder>/:
// Folder has no leading or trailing "/" ("" for public_html itself). Both are "" when
// the path is not under the user's own web directory.
type WebDocroot struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain"`
	Folder  string `json:"folder"`
}

// WebRedirectView is the stored redirect. Enabled is REDIRECT != "". Mode is "www"
// when Target is www.<domain>, "bare" when it is <domain>, else "custom" (also when
// disabled). Target is REDIRECT as stored. Code is REDIRECT_CODE, or "301" when empty.
type WebRedirectView struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode" tstype:"'www' | 'bare' | 'custom'"`
	Target  string `json:"target"`
	Code    string `json:"code"`
}

// WebEditRequest is PUT /api/v1/web/{domain}: the settings section of the edit form
// (SSL is saved separately with WebSSLRequest, Parity 4). PUT is a full replace:
// send every field with the form's current state, because an omitted field reads as
// its zero value (no aliases deletes every alias; Redirect, Docroot and StatsAuth
// false turn those off). Unchanged values run no command (Parity 6). Body values that start a command's positional slot (aliases,
// StatsUser, RedirectTarget, FTP names) must not start with "-" or hold control
// characters or newlines: 400 invalid, zero commands.
type WebEditRequest struct {
	// IP is the domain's internal IP (a WebIP.IP); a change cascades to DNS and mail.
	IP string `json:"ip"`
	// Aliases is the raw textarea text, split on newlines, commas and whitespace.
	Aliases string `json:"aliases"`
	// Stats is "none" or an engine from WebOptions.StatsTypes.
	Stats string `json:"stats"`
	// StatsAuth turns the statistics login on. StatsUser is required then (blank-field
	// error "Username"). StatsPassword "" keeps the stored password. When StatsAuth is
	// false, StatsUser and StatsPassword are ignored and a stored login is deleted (as
	// PHP).
	StatsAuth     bool   `json:"statsAuth"`
	StatsUser     string `json:"statsUser"`
	StatsPassword string `json:"statsPassword"`
	// Redirect turns redirection on. RedirectTarget is resolved by the UI (www.<d>,
	// <d>, or the custom text); "" runs no command. RedirectCode is "301" or "302".
	Redirect       bool   `json:"redirect"`
	RedirectTarget string `json:"redirectTarget"`
	RedirectCode   string `json:"redirectCode"`
	// Templates is absent unless WebOptions.CanEditTemplates; the server ignores it
	// from users without the policy.
	Templates *WebTemplatesChange `json:"templates,omitempty"`
	// Docroot turns the custom document root on. DocrootDomain is one of the user's
	// web domains (the domain itself means public_html of this domain); DocrootFolder
	// is a folder under its public_html ("" for public_html itself).
	Docroot       bool   `json:"docroot"`
	DocrootDomain string `json:"docrootDomain"`
	DocrootFolder string `json:"docrootFolder"`
	// FTP is the FTP rows of the form, in order ([] when FTP is not offered). Only the
	// rows sent are acted on: a stored account missing from the list is left alone (as
	// PHP, which only looped over the posted rows). A row with both IsNew and Delete
	// set is skipped (PHP would still have added it; its UI removed such rows instead).
	FTP []WebFTPChange `json:"ftp"`
}

// WebTemplatesChange is the template section. Template is the web template. The
// pointer fields are absent when not offered: Backend without WEB_BACKEND, FastCGI
// unless WebOptions.Nginx, ProxyTemplate and ProxyExt without PROXY_SYSTEM or when the
// domain has no proxy. ProxyExt is raw text, split like Aliases.
type WebTemplatesChange struct {
	Template      string      `json:"template"`
	Backend       *string     `json:"backend,omitempty"`
	FastCGI       *WebFastCGI `json:"fastcgi,omitempty"`
	ProxyTemplate *string     `json:"proxyTemplate,omitempty"`
	ProxyExt      *string     `json:"proxyExt,omitempty"`
}

// WebFastCGI is the nginx FastCGI cache box. Duration is Hestia's Ns, Nm or Nd; ""
// means "2m".
type WebFastCGI struct {
	Enabled  bool   `json:"enabled"`
	Duration string `json:"duration"`
}

// WebFTPChange is one FTP row. Name is the short name (a typed "<user>_" prefix is
// stripped from new names). Password "" keeps the stored one on existing rows and is
// required on new ones. Path is relative to the domain root; PrevPath is the stored
// path ("" on new rows). Email "" sends no credentials email; otherwise it must be a
// valid address, and an existing row is emailed only when Password is set. IsNew rows are added; Delete rows (existing only) are removed.
type WebFTPChange struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Path     string `json:"path"`
	PrevPath string `json:"prevPath"`
	Email    string `json:"email"`
	IsNew    bool   `json:"isNew"`
	Delete   bool   `json:"delete"`
}

// WebSSLRequest is PUT /api/v1/web/{domain}/ssl: the whole SSL section's state, sent
// after the settings save, only when the section changed (Parity 4). Each field says
// when it counts; a field that doesn't count is ignored, whatever its value.
type WebSSLRequest struct {
	// SSL is the "Enable SSL for this domain" box. False means "turn SSL off": every
	// other field is ignored (nothing runs when SSL is already off).
	SSL bool `json:"ssl"`
	// LetsEncrypt is the "Use Let's Encrypt" box. It counts only when SSL is true.
	// Turning it off while SSL stays on needs Crt and Key (Parity 7).
	LetsEncrypt bool `json:"letsencrypt"`
	// KeyAlgo is one of WebOptions.KeyAlgos ("ecdsa-256" when it isn't). It counts only
	// when SSL and LetsEncrypt are true; a change from the stored type re-issues the
	// certificate.
	KeyAlgo string `json:"keyAlgo"`
	// ForceSSL and HSTS are the boxes; they count only when SSL is true. Each runs a
	// command only when it differs from the stored state (Parity 6).
	ForceSSL bool `json:"forceSsl"`
	HSTS     bool `json:"hsts"`
	// Crt, Key and CA are the full textarea text, which the UI prefills from
	// WebDomainView.ssl. Blank means blank, never "unchanged". They count only when SSL
	// is true and LetsEncrypt is false. Crt and Key are then required when adding a
	// certificate or turning Let's Encrypt off (blank-field args "ssl certificate",
	// "ssl key", before any command); a stored custom certificate is replaced only when
	// the PEM text differs after \r\n normalisation. CA may be empty.
	Crt string `json:"crt"`
	Key string `json:"key"`
	CA  string `json:"ca"`
	// Reissue is true when the settings save changed the alias list. It counts only
	// when SSL and LetsEncrypt are true and Let's Encrypt was already on: the server
	// then requests a new certificate for the stored (just saved) aliases. Otherwise it
	// is ignored (turning Let's Encrypt on issues for the stored aliases anyway).
	Reissue bool `json:"reissue"`
}

// WebGenerateSSLRequest is POST /api/v1/web/generate-ssl, the self-signed certificate
// generator. It needs no web domain of that name. Domain, Country, State, Locality
// (blank-field name "City") and Org are required; Aliases is raw text, one name per
// line; Email may be empty. KeyAlgo must be one of WebOptions.KeyAlgos ("Invalid key
// algorithm selected." otherwise, checked first).
type WebGenerateSSLRequest struct {
	Domain   string `json:"domain"`
	Aliases  string `json:"aliases"`
	Email    string `json:"email"`
	Country  string `json:"country"`
	State    string `json:"state"`
	Locality string `json:"locality"`
	Org      string `json:"org"`
	KeyAlgo  string `json:"keyAlgo"`
}

// WebGeneratedSSL answers the generator: PEM certificate, private key and CSR.
type WebGeneratedSSL struct {
	Crt string `json:"crt"`
	Key string `json:"key"`
	CSR string `json:"csr"`
}

// WebLog answers GET /api/v1/web/{domain}/logs?type=: the last 70 lines, oldest
// first. Type is "error" for ?type=error, else "access". Lines is [] when the log is
// empty or the command failed.
type WebLog struct {
	Type  string   `json:"type" tstype:"'access' | 'error'"`
	Lines []string `json:"lines"`
}

// WebApps answers GET /api/v1/web/{domain}/apps: the installers in catalogue order
// (checklist section 2.4, osTicket last).
type WebApps struct {
	Apps []WebAppInfo `json:"apps"`
}

// WebAppInfo is one installer. ID is the lowercased directory name used in paths
// (wordpress, osticket). Version may be "latest". Installable is false when none of
// the app's supported PHP versions is installed.
type WebAppInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Version     string `json:"version"`
	Installable bool   `json:"installable"`
}

// WebAppForm answers GET /api/v1/web/{domain}/apps/{app}: the setup form.
type WebAppForm struct {
	App    WebAppInfo    `json:"app"`
	Fields []WebAppField `json:"fields"`
	// PHPVersions is installed ∩ supported, highest last; the form preselects the last.
	PHPVersions []string `json:"phpVersions"`
	// Database is null when the app has no database.
	Database *WebAppDatabaseDefaults `json:"database" tstype:"WebAppDatabaseDefaults | null,required"`
	// DocrootClean is false when public_html holds anything besides index.html and
	// robots.txt (the data-loss warning).
	DocrootClean bool `json:"docrootClean"`
}

// WebAppField is one installer option. Label is PHP's (ucwords of the key with "." and
// "_" turned into spaces). Value is the default ("true" or "" for boolean).
// Options is [] unless Type is "select".
type WebAppField struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Type        string         `json:"type" tstype:"'text' | 'password' | 'select' | 'boolean'"`
	Value       string         `json:"value"`
	Placeholder string         `json:"placeholder"`
	Options     []WebAppOption `json:"options"`
}

// WebAppOption is one select option.
type WebAppOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// WebAppDatabaseDefaults prefills the database block. Hosts is the MySQL hosts, the
// first preselected. Name is the generated default for both the database and its user,
// without the "<user>_" prefix.
type WebAppDatabaseDefaults struct {
	Hosts []string `json:"hosts"`
	Name  string   `json:"name"`
}

// WebAppInstallRequest is POST /api/v1/web/{domain}/apps/{app}. Values maps each
// WebAppField.Key to its value (booleans are "true" or ""). PHPVersion is one of
// WebAppForm.PHPVersions. Database is absent for apps without one.
type WebAppInstallRequest struct {
	Values     map[string]string `json:"values"`
	PHPVersion string            `json:"phpVersion"`
	Database   *WebAppDatabase   `json:"database,omitempty"`
}

// WebAppDatabase is the install's database block. Create false uses no new database.
// Name and User are without the "<user>_" prefix ("" takes the default); Password ""
// gets 20 random hex characters. Host is one of WebAppDatabaseDefaults.Hosts.
type WebAppDatabase struct {
	Create   bool   `json:"create"`
	Host     string `json:"host"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}
