package httpapi

// Server admin wire types (Plan 4). Generated into ui/src/api/types.gen.ts with the
// rest. Paths are under /api/v1/server; see ui/src/api/server.ts for the routes.
//
// Conventions (as in 2c):
//   - A pointer field with omitempty (TS "field?") in a request means "not offered:
//     leave as is". The UI leaves it out when the server or the signed-in role doesn't
//     offer that control, and the handler runs no command for it.
//   - Write-only secrets (passwords, keys) are plain strings with omitempty: "" or
//     absent means "keep the stored one". They are never sent back.
//   - Hestia's yes/no and true/false values are bools on the wire, decoded the PHP
//     way: a yes/no key is true unless it is "no"; a true/false key is true only when
//     it is "true" (each field says which).
//   - Writes that run long work answer 202 JobRef (types.go) and are polled at
//     GET /api/v1/server/jobs/{id} (JobStatus); JobStatus.Result holds the job's own
//     reply type named on each request below.

import "github.com/hostisan/hostisan/internal/hestia"

// SysLockoutCode is the error code of every firewall lockout refusal (409, decision
// 37); each guard pairs it with its own msgid.
const SysLockoutCode = "lockout"

// SysLockoutFirewallMsg refuses turning the firewall on (Plugins.Firewall) unless an
// active ACCEPT rule covers the panel port and another the SSH port (decision 37).
const SysLockoutFirewallMsg = "The firewall can't be turned on until active ACCEPT rules keep the panel and SSH ports open."

// SysFwToolsMissingCode and SysFwToolsMissingMsg refuse turning the firewall on
// (toggle or Plugins.Firewall, 409) while the iptables or ipset binary is missing:
// the core would report the firewall on and filter nothing (D6). Args: the missing
// names, comma separated.
const (
	SysFwToolsMissingCode = "firewall_tools_missing"
	SysFwToolsMissingMsg  = "The firewall can't be turned on because %s is not installed. Install the iptables and ipset packages over SSH first."
)

// ---- Services and config editors (Slice A) ----

// ServicesView answers GET /api/v1/server/services.
type ServicesView struct {
	// Info is v-list-sys-info, with every value through brand.Rewrite.
	Info hestia.SysInfo `json:"info"`
	// Release is the release branch as the summary shows it (brand-rewritten RELEASE;
	// "release" is a production release).
	Release string `json:"release"`
	// Services are sorted by name as PHP's ksort did.
	Services []ServiceRow `json:"services"`
	// Firewall: FIREWALL_SYSTEM is iptables (the toolbar's Firewall link).
	Firewall bool `json:"firewall"`
}

// ServiceRow is one service. CPU is the real ps figure in percent (decision 9), MEM
// is MB and RTIME minutes, as Hestia strings. Editor is the {service} value for
// GET /editors/{service} (decision 13: phpX.Y-fpm rows keep their own name and open
// that version's php.ini), or "" when the row has no editor: the iptables row links
// to the firewall page instead, and web-terminal has none.
type ServiceRow struct {
	Name   string `json:"name"`
	System string `json:"system"`
	State  string `json:"state"` // "running" | "stopped"
	CPU    string `json:"cpu"`
	Mem    string `json:"mem"`
	RTime  string `json:"rtime"`
	Editor string `json:"editor"`
}

// ServiceActionRequest is POST /services/{service}/{action} ({action} is start, stop
// or restart). Stopping ssh or iptables needs Confirm equal to the service name
// (decision 8), else 400 confirm_required. Answers 202 JobRef; the job's result is OK.
// Bulk start/stop/restart is POST /services/bulk with BulkRequest (Names are service
// names); answers 202 JobRef and the job's result is BulkReply.
type ServiceActionRequest struct {
	Confirm string `json:"confirm,omitempty"`
}

// RebootRequest is POST /reboot. Hostname must equal sysinfo HOSTNAME (decision 10),
// else 400. Answers 202 JobRef.
type RebootRequest struct {
	Hostname string `json:"hostname"`
}

// EditorView answers GET /editors/{service}.
//
// Family picks the editor layout: "plain" (one file), "nginx", "php" and "mysql"
// (one file with basic-mode Directives), "multi" (bind9 and postgresql: options or
// hba first, then the main file), "dovecot" (dovecot.conf then conf.d files), "cron"
// (read-only /etc/crontab) and "panel-cron" (the hestiaweb crontab).
//
// Restart says whether the restart checkbox is offered (false for the crontabs).
// ReadOnly is set for cron: saving runs nothing and answers the info message.
// SaveBlocked, when set, is the msgid explaining why Save is disabled (409
// php_multi_ini on the php/php-fpm rows, whose save would write every php.ini;
// decision 18). A file whose read failed has Error set and no Content; saving is
// refused while any file has an Error (decision 12).
// SyntaxCheck: the core tests a saved file with the service's own checker (nginx -t,
// sshd -t, named-checkconf...) and keeps the old one when it fails, restart or not.
// Without it an unticked Restart leaves the new file untested (C7 S8).
// Directives are the basic-mode values by directive name (nginx, php, mysql only).
type EditorView struct {
	Service     string            `json:"service"`
	Title       string            `json:"title"`
	Family      string            `json:"family"`
	Restart     bool              `json:"restart"`
	ReadOnly    bool              `json:"readOnly"`
	SaveBlocked string            `json:"saveBlocked,omitempty"`
	SyntaxCheck bool              `json:"syntaxCheck"`
	Directives  map[string]string `json:"directives,omitempty"`
	Files       []EditorFile      `json:"files"`
}

// EditorFile is one file of an editor. ID is the config id passed to
// v-change-sys-service-config (nginx, bind9-opt, dovecot-3, mysqld…); Hash is the
// hex sha256 of Content as read, sent back on save (409 changed when the file moved
// on, decision 12).
type EditorFile struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
	Error   string `json:"error,omitempty"`
}

// EditorSaveRequest is PUT /editors/{service}. Files are the advanced-mode texts (only
// the ones shown; an unchanged text runs nothing). Directives are the basic-mode
// edits (only the changed ones), applied to the freshly read file (decision 17).
// Restart is the checkbox. Answers 202 JobRef; the job's result is EditorSaveResult.
type EditorSaveRequest struct {
	Files      []EditorSaveFile  `json:"files"`
	Directives map[string]string `json:"directives,omitempty"`
	Restart    bool              `json:"restart"`
}

type EditorSaveFile struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

// EditorSaveResult lists the file ids written and the ones left as they were.
// ServiceState is the service's state after the save ("running", "stopped", or ""
// when not checked). A failed restart (exit 20, decision 15) fails the job instead:
// its error is restart_failed with the message "The new configuration failed to
// start, so the previous file was restored." and Args[0] the service state. A file
// the service's checker refuses (exit 12) fails it with config_invalid, "The new
// configuration has errors, so the previous file was restored.\n%s", Args[0] the
// checker's output (no %s and no args when it printed nothing). Untested is true when
// a file was written that neither a checker nor a restart tested.
type EditorSaveResult struct {
	Changed      []string `json:"changed"`
	Unchanged    []string `json:"unchanged"`
	ServiceState string   `json:"serviceState"`
	Untested     bool     `json:"untested"`
}

// ---- Configure Server (Slice B) ----

// SettingsView answers GET /api/v1/server/settings: every value of the Configure
// Server form (settings checklist §2.1) and its option lists. Sections whose system
// isn't installed hold zero values and their flag is false (decision 25).
type SettingsView struct {
	// Basic options.
	Hostname    string `json:"hostname"`
	Timezone    string `json:"timezone"`
	Theme       string `json:"theme"`
	ThemeForAll bool   `json:"themeForAll"` // POLICY_USER_CHANGE_THEME == "no"
	Language    string `json:"language"`
	DebugMode   bool   `json:"debugMode"` // DEBUG_MODE == "true"

	// Updates.
	Version             string `json:"version"`
	ReleaseBranch       string `json:"releaseBranch"`
	PreviewFeatures     bool   `json:"previewFeatures"`     // POLICY_SYSTEM_ENABLE_BACON == "true"
	UpgradeSendEmail    bool   `json:"upgradeSendEmail"`    // UPGRADE_SEND_EMAIL == "true"
	UpgradeSendEmailLog bool   `json:"upgradeSendEmailLog"` // UPGRADE_SEND_EMAIL_LOG == "true"

	// Web server (read-only facts) and PHP versions. PHP is offered when WebBackend is
	// "php-fpm". DefaultPHP is the system version ("8.2").
	ProxySystem string        `json:"proxySystem"`
	WebSystem   string        `json:"webSystem"`
	WebBackend  string        `json:"webBackend"`
	PHPVersions []SettingsPHP `json:"phpVersions"`
	DefaultPHP  string        `json:"defaultPhp"`
	DNSSystem   string        `json:"dnsSystem"`
	DNSCluster  []string      `json:"dnsCluster"` // cluster host names; read-only
	// DNSStatus: Server information offers the DNS status tab. False on RHEL, where
	// v-list-sys-dns-status rewrites named.conf and restarts named (until Plan 6).
	DNSStatus  bool              `json:"dnsStatus"`
	Mail       SettingsMail      `json:"mail"`
	DB         SettingsDB        `json:"db"`
	Backup     SettingsBackup    `json:"backup"`
	SSL        SettingsSSL       `json:"ssl"`
	Security   SettingsSecurity  `json:"security"`
	Policies   SettingsPolicies  `json:"policies"`
	Plugins    SettingsPlugins   `json:"plugins"`
	Options    SettingsOptions   `json:"options"`
	Protection SettingsProtected `json:"protection"`
	// CanEditProtection: the signed-in user is ROOT_USER, so the System Protection
	// section is shown and its fields are accepted (decision 2).
	CanEditProtection bool `json:"canEditProtection"`
}

// SettingsPHP is one PHP version row (5.6 … 8.5). Installed: its backend template
// exists. Protected: installed and used by a web domain or the system default; it
// can't be removed (409 in_use). UsedBy lists the domains on it.
type SettingsPHP struct {
	Version   string           `json:"version"` // "8.2"
	Installed bool             `json:"installed"`
	Protected bool             `json:"protected"`
	UsedBy    []SettingsPHPUse `json:"usedBy"`
}

type SettingsPHPUse struct {
	User   string `json:"user"`
	Domain string `json:"domain"`
}

// SettingsMail is the Mail Server section; System is MAIL_SYSTEM ("" = not shown).
// The relay password is never listed.
type SettingsMail struct {
	System        string `json:"system"`
	Antivirus     string `json:"antivirus"`
	Antispam      string `json:"antispam"`
	WebmailSystem string `json:"webmailSystem"`
	WebmailAlias  string `json:"webmailAlias"`
	Relay         bool   `json:"relay"` // SMTP_RELAY == "true"
	RelayHost     string `json:"relayHost"`
	RelayPort     string `json:"relayPort"`
	RelayUser     string `json:"relayUser"`
}

// SettingsDB is the Databases section; System is DB_SYSTEM ("" = not shown). MySQL
// and PgSQL say whether a host of that type exists (phpMyAdmin and phpPgAdmin are
// offered only then); MySQLLocal: a mysql localhost host exists (root password).
type SettingsDB struct {
	System      string           `json:"system"`
	MySQL       bool             `json:"mysql"`
	PgSQL       bool             `json:"pgsql"`
	MySQLLocal  bool             `json:"mysqlLocal"`
	Hosts       []SettingsDBHost `json:"hosts"`
	PMAAlias    string           `json:"pmaAlias"`
	PGAAlias    string           `json:"pgaAlias"`
	PMASSO      bool             `json:"pmaSso"`      // PHPMYADMIN_KEY != ""
	PMARestrict bool             `json:"pmaRestrict"` // PMA_RESTRICT_ACCESS == "yes"
}

type SettingsDBHost struct {
	Type    string `json:"type"`
	Host    string `json:"host"`
	MaxDB   string `json:"maxDb"`
	Current string `json:"current"`
}

// SettingsBackup is the Backups and Incremental Backups sections. Directory is
// read-only. Remote is the configured remote (Type "" = none); its password and B2
// key are never listed.
type SettingsBackup struct {
	Local       bool           `json:"local"` // BACKUP_SYSTEM contains "local"
	Mode        string         `json:"mode"`  // "gzip" | "zstd"
	Level       string         `json:"level"` // BACKUP_GZIP, "1".."19"
	Directory   string         `json:"directory"`
	Remote      SettingsRemote `json:"remote"`
	Incremental bool           `json:"incremental"` // BACKUP_INCREMENTAL == "yes"
	Restic      SettingsRestic `json:"restic"`
}

// SettingsRemote is a remote backup host. Type is "", ftp, sftp, b2 or rclone. ftp
// and sftp use Host, Port, Username and Path; rclone Host (remote name) and Path; b2
// Bucket and KeyID.
type SettingsRemote struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Path     string `json:"path"`
	Bucket   string `json:"bucket"`
	KeyID    string `json:"keyId"`
}

type SettingsRestic struct {
	Repo        string `json:"repo"`
	Snapshots   string `json:"snapshots"`
	KeepDaily   string `json:"keepDaily"`
	KeepWeekly  string `json:"keepWeekly"`
	KeepMonthly string `json:"keepMonthly"`
	KeepYearly  string `json:"keepYearly"`
}

// SettingsSSL is the panel certificate, without its private key (decision 36).
type SettingsSSL struct {
	Crt       string `json:"crt"`
	CA        string `json:"ca"`
	Subject   string `json:"subject"`
	Aliases   string `json:"aliases"`
	NotBefore string `json:"notBefore"`
	NotAfter  string `json:"notAfter"`
	Signature string `json:"signature"`
	PubKey    string `json:"pubKey"`
	Issuer    string `json:"issuer"`
}

// SettingsSecurity is Security > System. APIAllowedIP is one entry per line.
type SettingsSecurity struct {
	APISystem      string `json:"apiSystem"` // "0" | "1" | "2"
	LegacyAPI      bool   `json:"legacyApi"` // API == "yes"
	APIAllowedIP   string `json:"apiAllowedIp"`
	LoginStyle     string `json:"loginStyle"`     // "default" | "old"
	PasswordReset  bool   `json:"passwordReset"`  // POLICY_SYSTEM_PASSWORD_RESET != "no"
	SessionTimeout string `json:"sessionTimeout"` // INACTIVE_SESSION_TIMEOUT, minutes
	CSRFStrictness string `json:"csrfStrictness"` // "0" | "1" | "2"
}

// SettingsProtected is Security > System Protection (root only, decision 2). Each is
// true when the key is "yes".
type SettingsProtected struct {
	ProtectedAdmin bool `json:"protectedAdmin"`
	HideAdmin      bool `json:"hideAdmin"`
	HideServices   bool `json:"hideServices"`
}

// SettingsPolicies is Security > Policies; each is true unless the key is "no".
// ViewSuspended is offered only with preview features on.
type SettingsPolicies struct {
	EditDetails          bool `json:"editDetails"`
	EditWebTemplates     bool `json:"editWebTemplates"`
	EditDNSTemplates     bool `json:"editDnsTemplates"`
	ViewLogs             bool `json:"viewLogs"`
	DeleteLogs           bool `json:"deleteLogs"`
	ViewSuspended        bool `json:"viewSuspended"` // POLICY_USER_VIEW_SUSPENDED == "yes"
	BackupSuspendedUsers bool `json:"backupSuspendedUsers"`
	SyncErrorDocuments   bool `json:"syncErrorDocuments"`
	SyncSkeleton         bool `json:"syncSkeleton"`
	EnforceSubdomain     bool `json:"enforceSubdomain"`
}

// SettingsPlugins is the Plugins section.
type SettingsPlugins struct {
	AppInstaller   bool `json:"appInstaller"`   // PLUGIN_APP_INSTALLER == "true"
	FileManager    bool `json:"fileManager"`    // FILE_MANAGER == "true"
	WebTerminal    bool `json:"webTerminal"`    // WEB_TERMINAL == "true"
	ResourcesLimit bool `json:"resourcesLimit"` // RESOURCES_LIMIT == "yes"
	DiskQuota      bool `json:"diskQuota"`      // DISK_QUOTA == "yes"
	Firewall       bool `json:"firewall"`       // FIREWALL_SYSTEM == "iptables"
}

// SettingsOptions are the select lists. Languages are codes (the UI names them).
type SettingsOptions struct {
	Timezones []string `json:"timezones"`
	Themes    []string `json:"themes"`
	Languages []string `json:"languages"`
}

// SettingsRequest is PUT /api/v1/server/settings. Every field is "not offered, leave
// as is" when absent; a present field that equals the stored value runs nothing.
// Validation runs before the job (400); the job runs the steps in PHP order
// (decisions 25–36). Answers 202 JobRef; the job's result is SettingsResult.
// On failure, applied = JobStatus.steps without the last (failing) step, each an
// English msgid recorded by sys.step; error is the failure.
type SettingsRequest struct {
	Hostname        *string `json:"hostname,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`
	Theme           *string `json:"theme,omitempty"`
	ThemeForAll     *bool   `json:"themeForAll,omitempty"`
	Language        *string `json:"language,omitempty"`
	LanguageForAll  bool    `json:"languageForAll,omitempty"` // with a changed Language
	DebugMode       *bool   `json:"debugMode,omitempty"`
	PreviewFeatures *bool   `json:"previewFeatures,omitempty"`
	UpgradeEmail    *bool   `json:"upgradeSendEmail,omitempty"`
	UpgradeEmailLog *bool   `json:"upgradeSendEmailLog,omitempty"`

	// PHPVersions is the full set of versions wanted installed ("8.2", …).
	PHPVersions *[]string `json:"phpVersions,omitempty"`
	DefaultPHP  *string   `json:"defaultPhp,omitempty"`

	WebmailAlias  *string `json:"webmailAlias,omitempty"`
	SMTPRelay     *bool   `json:"smtpRelay,omitempty"`
	SMTPRelayHost *string `json:"smtpRelayHost,omitempty"`
	SMTPRelayPort *string `json:"smtpRelayPort,omitempty"`
	SMTPRelayUser *string `json:"smtpRelayUser,omitempty"`
	// SMTPRelayPassword: any relay change while a username is set (stored or sent)
	// needs the password typed again; a blank one is 400 (decision 29).
	SMTPRelayPassword string `json:"smtpRelayPassword,omitempty"`

	PMAAlias      *string `json:"pmaAlias,omitempty"`
	PGAAlias      *string `json:"pgaAlias,omitempty"`
	PMASSO        *bool   `json:"pmaSso,omitempty"`
	PMARestrict   *bool   `json:"pmaRestrict,omitempty"`
	MySQLPassword string  `json:"mysqlPassword,omitempty"` // root@localhost

	BackupLocal  *bool           `json:"backupLocal,omitempty"`
	BackupMode   *string         `json:"backupMode,omitempty"`
	BackupLevel  *string         `json:"backupLevel,omitempty"`
	BackupRemote *SettingsRemote `json:"backupRemote,omitempty"` // Type "" removes the remote
	// BackupPassword: changing the host, user, port or path of an existing ftp/sftp
	// remote needs the password typed again; a blank one is 400 (decision 28).
	BackupPassword string          `json:"backupPassword,omitempty"`
	B2Key          string          `json:"b2Key,omitempty"`
	Incremental    *bool           `json:"incremental,omitempty"`
	Restic         *SettingsRestic `json:"restic,omitempty"`

	// SSLCrt is the panel certificate (PEM); SSLKey "" keeps the current key.
	SSLCrt *string `json:"sslCrt,omitempty"`
	SSLKey string  `json:"sslKey,omitempty"`

	APISystem      *string `json:"apiSystem,omitempty"`
	LegacyAPI      *bool   `json:"legacyApi,omitempty"`
	APIAllowedIP   *string `json:"apiAllowedIp,omitempty"`
	LoginStyle     *string `json:"loginStyle,omitempty"`
	PasswordReset  *bool   `json:"passwordReset,omitempty"`
	SessionTimeout *string `json:"sessionTimeout,omitempty"`
	CSRFStrictness *string `json:"csrfStrictness,omitempty"`

	// Root only (403 forbidden from anyone else when changed).
	ProtectedAdmin *bool `json:"protectedAdmin,omitempty"`
	HideAdmin      *bool `json:"hideAdmin,omitempty"`
	HideServices   *bool `json:"hideServices,omitempty"`

	// Policies.ViewSuspended is offered only with preview features on; when they are
	// off the UI must echo the stored value, never a default.
	Policies *SettingsPolicies `json:"policies,omitempty"`
	// Plugins.Firewall going on is subject to the lockout guard
	// (sysAPI.firewallEnableCheck, decision 37): 409 lockout, nothing runs.
	Plugins *SettingsPlugins `json:"plugins,omitempty"`
}

// SettingsResult is the settings job's result: the steps applied (English msgids), the
// notes the core printed (e.g. quota's "reboot is required"), and Refresh when the
// default PHP version or the theme for all users changed (PHP reloaded the page).
// On failure, applied = JobStatus.steps without the last (failing) step, each an
// English msgid recorded by sys.step; error is the failure.
type SettingsResult struct {
	Applied []string `json:"applied"`
	Notes   []string `json:"notes"`
	Refresh bool     `json:"refresh"`
}

// ---- Info, status, graphs, updates, white label, preview (Slice C) ----

// StatusText answers GET /status/{kind} ({kind}: cpu mem disk net web dns mail db):
// the command's text, capped at 1 MiB, HTML removed from the web dump (decision 21).
type StatusText struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// GraphCatalog answers GET /graphs: the charts in file order. Error is set (and
// Charts empty) when v-list-sys-rrd failed or printed something that isn't JSON.
type GraphCatalog struct {
	Charts []GraphChart `json:"charts"`
	Error  string       `json:"error,omitempty"`
}

// GraphChart: ID is net_<RRD> for net charts and the RRD name otherwise (decision 22).
type GraphChart struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// GraphData answers GET /graphs/{chart}?period= (daily, weekly, monthly, yearly,
// biennially, triennially; anything else is daily). Start is unix seconds and Step
// seconds; Series has one row per step, one value per Legend entry, null for a gap.
// Unit labels the y-axis (decision 23).
type GraphData struct {
	ID     string       `json:"id"`
	Period string       `json:"period"`
	Start  int64        `json:"start"`
	Step   int64        `json:"step"`
	Unit   string       `json:"unit"`
	Legend []string     `json:"legend"`
	Series [][]*float64 `json:"series"`
}

// UpdatesView answers GET /updates, in the core's order.
type UpdatesView struct {
	Packages   []UpdatePackage `json:"packages"`
	Autoupdate bool            `json:"autoupdate"`
}

// UpdatePackage: DisplayName is brand.PackageName(Name), Description is the rewritten
// DESCR; Updated is true when the package is up to date.
type UpdatePackage struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Arch        string `json:"arch"`
	Updated     bool   `json:"updated"`
}

// AutoupdateRequest is POST /updates/autoupdate; answers OK.
type AutoupdateRequest struct {
	Enabled bool `json:"enabled"`
}

// WhiteLabelView answers GET /white-label. AppName is brand.Rewrite(APP_NAME).
// Hostname feeds the form's hints ("Default: noreply@<hostname>").
type WhiteLabelView struct {
	AppName      string `json:"appName"`
	Title        string `json:"title"`
	FromName     string `json:"fromName"`
	FromEmail    string `json:"fromEmail"`
	SubjectEmail string `json:"subjectEmail"`
	HideDocs     bool   `json:"hideDocs"` // HIDE_DOCS == "yes"
	Hostname     string `json:"hostname"`
}

// WhiteLabelRequest is PUT /white-label. Changed fields run in PHP order; AppName
// can't be blank, the others can be cleared. UpdateLogo runs
// v-update-white-label-logo. Answers OK.
type WhiteLabelRequest struct {
	AppName      string `json:"appName"`
	Title        string `json:"title"`
	FromName     string `json:"fromName"`
	FromEmail    string `json:"fromEmail"`
	SubjectEmail string `json:"subjectEmail"`
	HideDocs     bool   `json:"hideDocs"`
	UpdateLogo   bool   `json:"updateLogo"`
}

// PreviewFeatures answers GET /preview: the static list (decision 49).
type PreviewFeatures struct {
	Rows []PreviewFeature `json:"rows"`
}

type PreviewFeature struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Status   string `json:"status"`
}

// ---- IPs and firewall (Slice D) ----

// IPsView answers GET /ips, ordered per SortOrder ("name" or "date") as PHP did.
type IPsView struct {
	IPs       []IPRow `json:"ips"`
	SortOrder string  `json:"sortOrder"`
}

// IPRow is one address (hestia.SysIP). Display is the NAT address when set, else the
// IP; Users is U_SYS_USERS (comma-separated).
type IPRow struct {
	IP         string `json:"ip"`
	Owner      string `json:"owner"`
	Status     string `json:"status"` // "shared" | "dedicated"
	Name       string `json:"name"`
	Users      string `json:"users"`
	WebDomains string `json:"webDomains"`
	Interface  string `json:"interface"`
	Netmask    string `json:"netmask"`
	NAT        string `json:"nat"`
	Display    string `json:"display"`
	Time       string `json:"time"`
	Date       string `json:"date"`
}

// IPOptions answers GET /ips/options. RootUser is ROOT_USER, the owner sent for a
// shared IP (decision 45).
type IPOptions struct {
	Interfaces []string `json:"interfaces"`
	Users      []string `json:"users"`
	RootUser   string   `json:"rootUser"`
}

// IPRequest is POST /ips (add, a job) and PUT /ips/{ip} (edit; IP, Netmask and
// Interface are ignored). Shared sends ROOT_USER as the owner and ignores Owner. GET
// /ips/{ip} answers IPRow. DELETE /ips/{ip}, POST /ips/bulk (BulkRequest, Action
// "delete") and POST /ips/refresh answer 202 JobRef. DELETE /ips/{ip} and POST
// /ips/refresh take no body: their handlers must not call decodeBody.
type IPRequest struct {
	IP        string `json:"ip"`
	Netmask   string `json:"netmask"`
	Interface string `json:"interface"`
	Shared    bool   `json:"shared"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	NAT       string `json:"nat"`
}

// FirewallView answers GET /firewall: the rules in evaluation order (highest id
// first, decision 39). Enabled is FIREWALL_SYSTEM == iptables (the lists show an
// empty state when false); Bans says whether the ban list is offered.
type FirewallView struct {
	Enabled bool              `json:"enabled"`
	Bans    bool              `json:"bans"`
	Rules   []FirewallRuleRow `json:"rules"`
}

// FirewallRuleRow is one rule (GET /firewall/rules/{id} answers one). Protected is
// "panel" for COMMENT HESTIA, "ssh" for COMMENT SSH, else "" (decision 37). Expect
// fingerprints ACTION|PROTOCOL|PORT|IP|COMMENT|SUSPENDED and is sent back on every
// action (decision 38). The Comment is stored as is ("HESTIA" shows as "Panel").
type FirewallRuleRow struct {
	ID        string `json:"id"`
	Action    string `json:"action"`   // "ACCEPT" | "DROP"
	Protocol  string `json:"protocol"` // "TCP" | "UDP" | "ICMP"
	Port      string `json:"port"`
	IP        string `json:"ip"`
	Comment   string `json:"comment"`
	Suspended bool   `json:"suspended"`
	Protected string `json:"protected"`
	Expect    string `json:"expect"`
	Time      string `json:"time"`
	Date      string `json:"date"`
}

// FirewallRuleRequest is POST /firewall/rules (add) and PUT /firewall/rules/{id}
// (edit, with Expect). Answers OK.
type FirewallRuleRequest struct {
	Action   string `json:"action"`
	Protocol string `json:"protocol"`
	Port     string `json:"port"`
	IP       string `json:"ip"`
	Comment  string `json:"comment"`
	Expect   string `json:"expect,omitempty"`
}

// RuleActionRequest is POST /firewall/rules/{id}/{action} ({action}: delete, suspend,
// unsuspend, move-up, move-down). Answers OK.
type RuleActionRequest struct {
	Expect string `json:"expect"`
}

// RuleBulkRequest is POST /firewall/rules/bulk (Action: delete, suspend, unsuspend).
// Answers BulkReply (Name is the rule id).
type RuleBulkRequest struct {
	Action string         `json:"action"`
	Items  []RuleBulkItem `json:"items"`
}

type RuleBulkItem struct {
	ID     string `json:"id"`
	Expect string `json:"expect"`
}

// FirewallToggleRequest is POST /firewall/toggle: turn the firewall on (after the
// enable guard, decision 37) or off. Answers 202 JobRef.
type FirewallToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// BansView answers GET /firewall/bans: every (ip, chain) row, newest first.
type BansView struct {
	Rows []hestia.FirewallBan `json:"rows"`
}

// BanRequest is POST /firewall/bans (Chain: SSH WEB FTP DNS MAIL DB HESTIA). Answers OK.
type BanRequest struct {
	IP    string `json:"ip"`
	Chain string `json:"chain"`
}

// BanDeleteRequest is POST /firewall/bans/delete with 1–500 pairs. Answers BulkReply
// (Name is "ip chain").
type BanDeleteRequest struct {
	Items []BanItem `json:"items"`
}

type BanItem struct {
	IP    string `json:"ip"`
	Chain string `json:"chain"`
}

// IpsetsView answers GET /firewall/ipsets. Enabled is false when the firewall is off
// (the page shows an empty state instead of the core's error).
type IpsetsView struct {
	Enabled bool                   `json:"enabled"`
	Rows    []hestia.FirewallIpset `json:"rows"`
}

// IpsetRequest is POST /firewall/ipsets (a job, 3 min timeout). Source is an
// http(s) URL or the shipped blacklist script (decision 44); IPVersion "v4" | "v6".
// POST /firewall/ipsets/bulk (BulkRequest, Action "delete") answers BulkReply and
// DELETE /firewall/ipsets/{name} answers OK.
type IpsetRequest struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	IPVersion  string `json:"ipVersion"`
	Autoupdate bool   `json:"autoupdate"`
}
