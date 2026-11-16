// Package httpapi is Hostisan's JSON API. The structs in this file are the wire
// format; the UI's TypeScript types are generated from them (see tygo.yaml).
package httpapi

import "github.com/hostisan/hostisan/internal/hestia"

// Bootstrap is everything the UI needs on load: whether someone is signed in, the
// CSRF token for writes, and display settings.
type Bootstrap struct {
	Authenticated bool         `json:"authenticated"`
	CSRFToken     string       `json:"csrfToken"`
	AppName       string       `json:"appName"`
	Version       string       `json:"version"`
	Language      string       `json:"language"`
	Languages     []string     `json:"languages"`
	Theme         string       `json:"theme"`
	User          *SessionUser `json:"user"`
	// SignedOutReason is set when this request ended an expired session: the English
	// msgid to show on the login page, or "" for no message.
	SignedOutReason string `json:"signedOutReason,omitempty"`
	// PasswordReset: the login page offers password reset: POLICY_SYSTEM_PASSWORD_RESET isn't "no".
	PasswordReset bool `json:"passwordReset"`
	// HideDocs: HIDE_DOCS is "yes", so Help stays hidden even when there are docs.
	HideDocs bool `json:"hideDocs"`
	// ProductURL is Hostisan's site, without a trailing slash, and SecurityEmail the
	// address for security reports (install/common/hostisan/product.env, set at build
	// time); either is "" in a build without them, and the UI then leaves its link out.
	ProductURL    string `json:"productUrl"`
	SecurityEmail string `json:"securityEmail"`
}

type SessionUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"` // Hestia's NAME, for the user menu
	Contact     string `json:"contact"`
	Role        string `json:"role"`
	Look        string `json:"look"` // user an admin is viewing as; "" otherwise
	ReadOnly    bool   `json:"readOnly"`
	Landing     string `json:"landing"`
	Usage       Usage  `json:"usage"` // of the account being viewed
	// SortOrder is the viewed account's PREF_UI_SORT: "name" or "date".
	SortOrder string   `json:"sortOrder"`
	Features  Features `json:"features"`
	// CanEditDetails: the user may edit their own contact details (admins always).
	CanEditDetails bool `json:"canEditDetails"`
	// CanChangeTheme: POLICY_USER_CHANGE_THEME isn't "no".
	CanChangeTheme bool `json:"canChangeTheme"`
	// CanDeleteLogs: the user may delete their own logs (admins always).
	CanDeleteLogs bool `json:"canDeleteLogs"`
	// Notifications: the viewed account receives panel notifications.
	Notifications bool `json:"notifications"`
	// Package is the viewed account's package name (the empty-package page names it).
	Package string `json:"package"`
}

// Features says which sections the viewed account's package, the server and the signed-in role allow.
type Features struct {
	// Web: the server has a web system and the package allows web domains.
	Web bool `json:"web"`
	// DNS: the server has a DNS system and the package allows DNS domains.
	DNS bool `json:"dns"`
	// DB: the server has a database system and the package allows databases.
	DB bool `json:"db"`
	// Mail: the server has a mail system and the package allows mail domains.
	Mail bool `json:"mail"`
	// Cron: the server has a cron system and the package allows cron jobs.
	Cron bool `json:"cron"`
	// Backup: the server has a backup system and the package allows backups.
	Backup bool `json:"backup"`
	// Incremental: the package has incremental (restic) backups.
	Incremental bool `json:"incremental"`
	// Logs: the user may view logs (admins always).
	Logs bool `json:"logs"`
	// LoginHistory: the login history is shown (not in DEMO_MODE, like Hestia).
	LoginHistory bool `json:"loginHistory"`
	// AccessKeys: API_SYSTEM allows access keys for this account (1 for root, 2 for everyone).
	AccessKeys bool `json:"accessKeys"`
	// Server: the signed-in admin may manage the server (root, or services aren't hidden).
	Server bool `json:"server"`
	// Firewall: Server, and the firewall is iptables.
	Firewall bool `json:"firewall"`
	// FirewallBans: Firewall, and a firewall extension (fail2ban) is installed.
	FirewallBans bool `json:"firewallBans"`
	// Terminal: WEB_TERMINAL is "true", the viewed account's shell isn't nologin, the
	// session isn't read-only and isn't an admin viewing the protected root admin.
	Terminal bool `json:"terminal"`
	// FileManager: FILE_MANAGER is "true", the session isn't read-only and isn't an
	// admin viewing the protected root admin (any shell: SFTP works without one).
	FileManager bool `json:"fileManager"`
}

// Usage is in megabytes as Hestia reports it; quotas may be "unlimited".
type Usage struct {
	DiskUsed       string `json:"diskUsed"`
	DiskQuota      string `json:"diskQuota"`
	BandwidthUsed  string `json:"bandwidthUsed"`
	BandwidthQuota string `json:"bandwidthQuota"`
}

// LoginRequest is the password step, or the 2FA step (twofa only) after
// "twofa_required".
type LoginRequest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TwoFA    string `json:"twofa,omitempty"`
}

type LoginAsRequest struct {
	User string `json:"user"`
}

// StatusReply answers login ("ok" | "twofa_required"), login-as ("ok") and logout
// ("returned" | "logged_out").
type StatusReply struct {
	Status  string `json:"status"`
	Landing string `json:"landing,omitempty"`
}

type APIError struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Args fill the %s in Message; each is an English msgid the UI translates. Hestia's
	// own output is sent as Message with no Args.
	Args []string `json:"args,omitempty"`
}

// JobRef answers a request that started a background job (202).
type JobRef struct {
	JobID string `json:"jobId"`
}

// JobStatus is a background job as the UI polls it. State is "running", "done" or
// "failed". Error is set when failed, in the same shape as an API error. Result is
// the job's own reply when done. Times are RFC 3339; finished is "" while running.
type JobStatus struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	State    string     `json:"state"`
	Step     string     `json:"step"`
	Steps    []string   `json:"steps"` // [] when none, never null
	Error    *ErrorBody `json:"error" tstype:"ErrorBody | null,required"`
	Result   any        `json:"result" tstype:"unknown"`
	Started  string     `json:"started"`
	Finished string     `json:"finished"`
}

// OK answers a write that has nothing else to report.
type OK struct {
	Status string `json:"status"`
}

// RecordRequest is the body of add and edit record. Priority and TTL are strings
// as the forms send them; "" means unset.
type RecordRequest struct {
	Record   string `json:"record"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority string `json:"priority"`
	TTL      string `json:"ttl"`
}

// BulkRequest names the items a bulk action applies to: zone names, or record ids on
// the zone in the path.
type BulkRequest struct {
	Action string   `json:"action"`
	Names  []string `json:"names"`
}

// BulkReply answers a bulk action. Every item is attempted; Failed lists the ones
// Hestia refused, with its message.
type BulkReply struct {
	Status string        `json:"status"`
	Failed []BulkFailure `json:"failed"` // [] when none, never null
}

// BulkFailure is one refused item. Message is Hestia's output as sent, or a msgid whose
// %s are filled from Args (as in ErrorBody), so the UI can translate it.
type BulkFailure struct {
	Name    string   `json:"name"`
	Message string   `json:"message"`
	Args    []string `json:"args,omitempty"`
}

// DNSZones answers GET /api/v1/dns, in Hestia's order.
type DNSZones struct {
	Zones []hestia.DNSDomain `json:"zones"`
	Usage DNSUsage           `json:"usage"`
}

// DNSUsage is the DNS tab's usage summary: counts and limits from v-list-user, as
// Hestia strings. A limit is "unlimited" or a number; recordsLimit is the per-zone
// record limit times the zone limit.
type DNSUsage struct {
	Zones        string `json:"zones"`
	ZonesLimit   string `json:"zonesLimit"`
	Suspended    string `json:"suspended"`
	Records      string `json:"records"`
	RecordsLimit string `json:"recordsLimit"`
}

// DNSRecords answers GET /api/v1/dns/{domain}/records.
type DNSRecords struct {
	Zone    hestia.DNSDomain   `json:"zone"`
	Records []hestia.DNSRecord `json:"records"`
}

// DNSSECView answers GET /api/v1/dns/{domain}/dnssec. The labels are what the key
// page shows for the algorithm and flag.
type DNSSECView struct {
	Zone           hestia.DNSDomain `json:"zone"`
	Key            hestia.DNSSECKey `json:"key"`
	AlgorithmLabel string           `json:"algorithmLabel"`
	FlagLabel      string           `json:"flagLabel"`
}

// DNSOptions feeds the add and edit zone forms.
type DNSOptions struct {
	Templates        []string `json:"templates"`
	IPs              []string `json:"ips"` // display values: the NAT address when there is one
	Nameservers      []string `json:"nameservers"`
	DefaultTemplate  string   `json:"defaultTemplate"`
	DefaultTTL       string   `json:"defaultTTL"`
	DefaultExpiry    string   `json:"defaultExpiry"` // YYYY-MM-DD, a year from the server's today
	CanEditTemplates bool     `json:"canEditTemplates"`
	SupportsDNSSEC   bool     `json:"supportsDnssec"`
	DNSSystem        string   `json:"dnsSystem"`
	// AdminGate is true when the account being viewed is an admin: adding a zone then
	// first shows the "create a standard user" warning.
	AdminGate bool `json:"adminGate"`
}

// AddZoneRequest is POST /api/v1/dns. Template is ignored unless the signed-in role
// may edit templates. Nameservers carries up to 8.
type AddZoneRequest struct {
	Domain      string   `json:"domain"`
	IP          string   `json:"ip"`
	Template    string   `json:"template,omitempty"`
	DNSSEC      bool     `json:"dnssec"`
	Exp         string   `json:"exp"`
	TTL         string   `json:"ttl"`
	Nameservers []string `json:"nameservers"`
}

// EditZoneRequest is PUT /api/v1/dns/{domain}. DNSSEC is left alone when absent.
type EditZoneRequest struct {
	IP       string `json:"ip"`
	Template string `json:"template,omitempty"`
	SOA      string `json:"soa"`
	Exp      string `json:"exp"`
	TTL      string `json:"ttl"`
	DNSSEC   *bool  `json:"dnssec,omitempty"`
}

// Databases answers GET /api/v1/db, in Hestia's order.
type Databases struct {
	Databases []hestia.Database `json:"databases"`
	Usage     DBUsage           `json:"usage"`
}

// DBUsage is the database tab's usage summary from v-list-user: counts and limits as
// Hestia strings ("unlimited" or a number).
type DBUsage struct {
	Databases      string `json:"databases"`
	DatabasesLimit string `json:"databasesLimit"`
	Suspended      string `json:"suspended"`
}

// DBHostOption is one database host row; Charsets is the host's own CHARSETS list.
type DBHostOption struct {
	Host     string   `json:"host"`
	Type     string   `json:"type"`
	Charsets []string `json:"charsets"`
}

// DBOptions feeds the add and edit database forms and the list toolbar. It never
// carries PHPMYADMIN_KEY, only whether single sign-on is possible.
type DBOptions struct {
	Types          []string          `json:"types"`
	Hosts          []DBHostOption    `json:"hosts"`
	MySQLCharsets  []string          `json:"mysqlCharsets"`
	DefaultCharset map[string]string `json:"defaultCharset"`
	Prefix         string            `json:"prefix"` // acting user + "_"
	MaxUserLength  int               `json:"maxUserLength"`
	PHPMyAdminURL  string            `json:"phpMyAdminUrl"`
	PHPPgAdminURL  string            `json:"phpPgAdminUrl"`
	SSOEnabled     bool              `json:"ssoEnabled"`
	AdminGate      bool              `json:"adminGate"`
	// SSOHint (admins only, else "") says why phpMyAdmin single sign-on can't work
	// with the current API settings (parity decision 29).
	SSOHint string `json:"ssoHint"`
}

// AddDBRequest creates a database. The user prefix is added by Hestia, never sent;
// Email is optional and receives the credentials.
type AddDBRequest struct {
	Database string `json:"database"`
	DBUser   string `json:"dbuser"`
	Password string `json:"password"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Charset  string `json:"charset"`
	Email    string `json:"email"`
}

// AddDBReply is the created database's full name (typed case, with the user prefix)
// and the database manager it can be opened in. It never carries the password.
type AddDBReply struct {
	Status     string `json:"status"`
	Database   string `json:"database"`
	DBAdmin    string `json:"dbAdmin"`    // "phpMyAdmin" or "phpPgAdmin"
	DBAdminURL string `json:"dbAdminUrl"` // "" when the manager's host is unknown
}

// EditDBRequest renames the database user and/or changes its password. An empty
// password leaves it as is.
type EditDBRequest struct {
	DBUser   string `json:"dbuser"`
	Password string `json:"password"`
}
