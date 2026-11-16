package httpapi

// Account management wire types (Plan 3). Generated into ui/src/api/types.gen.ts with
// the rest; see ui/src/api/{users,account,cron,backup}.ts for the routes.
//
// Conventions, as in types_mail.go: a pointer field with omitempty (TS "field?") means
// "not offered: leave as is", and the handler runs no command for it. Lists are [] when
// empty, never null. Hestia values stay strings ("unlimited" or a number). No type
// here has a field for a secret, except TwoFASetup.Secret and AccessKeyCreated.Secret
// (TestNoSecretFields).

import "github.com/hostisan/hostisan/internal/hestia"

// ---------------------------------------------------------------- Users and packages

// UserList answers GET /api/v1/users, in v-list-users order. RootUser is ROOT_USER;
// Self is the signed-in user. With POLICY_SYSTEM_HIDE_ADMIN == "yes" a non-root admin
// gets no ROOT_USER row.
type UserList struct {
	Users    []hestia.UserEntry `json:"users"`
	RootUser string             `json:"rootUser"`
	Self     string             `json:"self"`
}

// UserBulkRequest is POST /api/v1/users/bulk. Actions: rebuild, rebuild-user,
// rebuild-web, rebuild-dns, rebuild-mail, rebuild-db, rebuild-cron, update-counters,
// suspend, unsuspend, delete. Every action but update-counters takes one name per
// request (the UI sends a selection one user at a time).
type UserBulkRequest = BulkRequest

// PackageList answers GET /api/v1/packages, in v-list-user-packages order.
type PackageList struct {
	Packages []hestia.PackageEntry `json:"packages"`
}

// PackageOptions feeds the package form. The template and shell lists are [] when the
// system has none. WebBackend is WEB_BACKEND ("" hides the backend template),
// ProxySystem PROXY_SYSTEM ("" hides the proxy template), DNSSystem DNS_SYSTEM (""
// hides the DNS template and name servers). ResourcesLimit: RESOURCES_LIMIT == "yes"
// shows the CPU and memory limits.
type PackageOptions struct {
	WebTemplates     []string `json:"webTemplates"`
	BackendTemplates []string `json:"backendTemplates"`
	ProxyTemplates   []string `json:"proxyTemplates"`
	DNSTemplates     []string `json:"dnsTemplates"`
	Shells           []string `json:"shells"`
	WebSystem        string   `json:"webSystem"`
	WebBackend       string   `json:"webBackend"`
	ProxySystem      string   `json:"proxySystem"`
	DNSSystem        string   `json:"dnsSystem"`
	ResourcesLimit   bool     `json:"resourcesLimit"`
}

// PackageRequest is POST /api/v1/packages and PUT /api/v1/packages/{package}. Limits
// are a number or "unlimited". Name is the package's (new) name. Nameservers are up to
// 8; blanks are dropped and the rest dot-trimmed.
type PackageRequest struct {
	Name               string   `json:"name"`
	WebTemplate        string   `json:"webTemplate"`
	BackendTemplate    string   `json:"backendTemplate"`
	ProxyTemplate      string   `json:"proxyTemplate"`
	DNSTemplate        string   `json:"dnsTemplate"`
	WebDomains         string   `json:"webDomains"`
	WebAliases         string   `json:"webAliases"`
	DNSDomains         string   `json:"dnsDomains"`
	DNSRecords         string   `json:"dnsRecords"`
	MailDomains        string   `json:"mailDomains"`
	MailAccounts       string   `json:"mailAccounts"`
	RateLimit          string   `json:"rateLimit"`
	Databases          string   `json:"databases"`
	CronJobs           string   `json:"cronJobs"`
	DiskQuota          string   `json:"diskQuota"`
	Bandwidth          string   `json:"bandwidth"`
	CPUQuota           string   `json:"cpuQuota"`
	CPUQuotaPeriod     string   `json:"cpuQuotaPeriod"`
	MemoryLimit        string   `json:"memoryLimit"`
	SwapLimit          string   `json:"swapLimit"`
	Backups            string   `json:"backups"`
	BackupsIncremental string   `json:"backupsIncremental"` // "yes" or "no"
	Shell              string   `json:"shell"`
	Nameservers        []string `json:"nameservers"`
}

// PackageSaved answers add and edit package: Name is the package's name after the
// save (edit may rename it), for the next request.
type PackageSaved struct {
	Status string `json:"status"`
	Name   string `json:"name"`
}

// ---------------------------------------------------------------- Profile

// AccountView answers GET /api/v1/account (the effective user) and GET
// /api/v1/users/{user}. TwoFA says whether 2FA is on; the secret is never sent. Self:
// the account is the signed-in user's own (no login-as, not another user's page).
type AccountView struct {
	User     hestia.UserDetail `json:"user"`
	TwoFA    bool              `json:"twofa"`
	Self     bool              `json:"self"`
	Editable AccountEditable   `json:"editable"`
}

// AccountEditable says which parts of the form the server accepts from this session:
// Details (name and email: admins, or POLICY_USER_EDIT_DETAILS == "yes"), Theme
// (POLICY_USER_CHANGE_THEME != "no") and Admin (package, role, shell, PHP CLI, name
// servers, login-disabled: admins only).
type AccountEditable struct {
	Details bool `json:"details"`
	Theme   bool `json:"theme"`
	Admin   bool `json:"admin"`
}

// AccountOptions feeds the account and user forms. Packages, Shells, PHPVersions and
// DefaultPHP are empty for non-admins; Themes is empty when the theme can't change.
// DNSSystem shows the name servers, APIAccess the access-keys link, LogsVisible the
// logs link.
type AccountOptions struct {
	Languages   []string `json:"languages"`
	Themes      []string `json:"themes"`
	Packages    []string `json:"packages"`
	Shells      []string `json:"shells"`
	PHPVersions []string `json:"phpVersions"`
	DefaultPHP  string   `json:"defaultPhp"`
	DNSSystem   bool     `json:"dnsSystem"`
	APIAccess   bool     `json:"apiAccess"`
	LogsVisible bool     `json:"logsVisible"`
	// SystemLanguage is hestia.conf LANGUAGE ("en" when unset): a new user's default.
	SystemLanguage string `json:"systemLanguage"`
}

// AccountRequest is PUT /api/v1/account. Password "" keeps the current one;
// CurrentPassword is required when changing your own password. Only changed values
// run a command.
type AccountRequest struct {
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Password        string   `json:"password"`
	CurrentPassword string   `json:"currentPassword"`
	Language        string   `json:"language"`
	Theme           string   `json:"theme"`
	SortOrder       string   `json:"sortOrder"` // "name" or "date"
	UseIPList       bool     `json:"useIpList"`
	AllowedIPs      []string `json:"allowedIps"`
}

// UserEditRequest is PUT /api/v1/users/{user}: AccountRequest plus the admin fields.
// Each admin field left out is "not offered"; any of them from a non-admin is 403.
type UserEditRequest struct {
	AccountRequest `tstype:",extends"`
	LoginDisabled  *bool    `json:"loginDisabled,omitempty"`
	Package        *string  `json:"package,omitempty"`
	Role           *string  `json:"role,omitempty"`
	Shell          *string  `json:"shell,omitempty"`
	PHPCli         *string  `json:"phpCli,omitempty"`
	Nameservers    []string `json:"nameservers,omitempty"`
}

// AddUserRequest is POST /api/v1/users. Notify is the address for the welcome email
// ("" sends none).
type AddUserRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Language      string `json:"language"`
	Role          string `json:"role"`
	Package       string `json:"package"`
	LoginDisabled bool   `json:"loginDisabled"`
	Notify        string `json:"notify"`
}

// AccountSaved answers a profile or user save. LanguageChanged: reload the UI strings.
type AccountSaved struct {
	Status          string `json:"status"`
	LanguageChanged bool   `json:"languageChanged"`
}

// TwoFASetup answers POST /api/v1/account/2fa/setup: a new TOTP secret (base32), its
// otpauth:// URL and the QR code as a PNG data URI. Nothing is saved until confirm.
type TwoFASetup struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
	QR         string `json:"qr"`
}

// TwoFAConfirmRequest is POST /api/v1/account/2fa/confirm. Code is the 6-digit TOTP
// code from the authenticator app for the pending secret. CurrentPassword is always
// required: enrolment works only on your own account (never through login-as or for
// another user), and a wrong password is logged as a failed login (Parity 10, 11).
type TwoFAConfirmRequest struct {
	Code            string `json:"code"`
	CurrentPassword string `json:"currentPassword"`
}

// PasswordConfirmRequest re-authenticates a sensitive change (turning your 2FA off).
type PasswordConfirmRequest struct {
	CurrentPassword string `json:"currentPassword"`
}

// ---------------------------------------------------------------- Recovery

// ResetRequest is POST /api/v1/auth/reset/request.
type ResetRequest struct {
	User  string `json:"user"`
	Email string `json:"email"`
}

// ResetConfirmRequest is POST /api/v1/auth/reset/confirm. Code is the password-reset
// code from the email (base32, valid 15 minutes, single use), never a TOTP code; an
// unlock code from the 2FA flow is refused (Parity 16).
type ResetConfirmRequest struct {
	User            string `json:"user"`
	Code            string `json:"code"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"passwordConfirm"`
}

// UnlockRequest is POST /api/v1/auth/reset-2fa/request.
type UnlockRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

// UnlockConfirmRequest is POST /api/v1/auth/reset-2fa/confirm. Code is the unlock code
// from the email sent by the request step (valid 15 minutes, single use), not a TOTP
// code and not a password-reset code (Parity 17).
type UnlockConfirmRequest struct {
	User string `json:"user"`
	Code string `json:"code"`
}

// ---------------------------------------------------------------- Keys, notifications, logs, stats

// SSHKeys answers GET /api/v1/account/ssh-keys (filemanager.ssh.key left out).
type SSHKeys struct {
	Keys []hestia.SSHKey `json:"keys"`
}

// AddSSHKeyRequest is POST /api/v1/account/ssh-keys: the public key line.
type AddSSHKeyRequest struct {
	Key string `json:"key"`
}

// AccessKeys answers GET /api/v1/account/access-keys: the effective user's keys by date
// and time, and the APIs the user's role may grant.
type AccessKeys struct {
	Keys []hestia.AccessKey `json:"keys"`
	APIs []hestia.APIInfo   `json:"apis"`
}

// AddAccessKeyRequest is POST /api/v1/account/access-keys: API names to grant.
type AddAccessKeyRequest struct {
	Permissions []string `json:"permissions"`
	Comment     string   `json:"comment"`
}

// AccessKeyCreated answers the create: the only reply that carries the secret.
type AccessKeyCreated struct {
	Key    hestia.AccessKey `json:"key"`
	Secret string           `json:"secret"`
}

// Notifications answers GET /api/v1/notifications: by PRIORITY descending, then
// newest first.
type Notifications struct {
	Items []hestia.Notification `json:"items"`
}

// Logs answers GET /api/v1/logs: User is the log's owner ("system" for the system
// log), entries newest first.
type Logs struct {
	Entries []hestia.LogEntry `json:"entries"`
	User    string            `json:"user"`
}

// AuthLog answers GET /api/v1/logs/auth, newest first.
type AuthLog struct {
	Entries []hestia.AuthLogEntry `json:"entries"`
	User    string                `json:"user"`
}

// Stats answers GET /api/v1/stats. Overall: the totals over all users. User is whose
// months these are ("" in overall mode). Users is the per-user select's list for
// admins, [] for everyone else. Months are newest first.
type Stats struct {
	Months  []StatsMonth `json:"months"`
	Overall bool         `json:"overall"`
	User    string       `json:"user"`
	Users   []string     `json:"users"`
}

// StatsMonth is one month of v-list-user-stats / v-list-users-stats. Date is the
// month's last day (YYYY-MM-DD); disk and bandwidth are MB, quotas may be
// "unlimited". UUsers is "" outside overall mode.
type StatsMonth struct {
	Date          string `json:"date"`
	Time          string `json:"time"`
	Package       string `json:"package"`
	IPOwned       string `json:"ipOwned"`
	DiskQuota     string `json:"diskQuota"`
	UDisk         string `json:"uDisk"`
	UDiskDirs     string `json:"uDiskDirs"`
	UDiskWeb      string `json:"uDiskWeb"`
	UDiskMail     string `json:"uDiskMail"`
	UDiskDB       string `json:"uDiskDb"`
	Bandwidth     string `json:"bandwidth"`
	UBandwidth    string `json:"uBandwidth"`
	UWebDomains   string `json:"uWebDomains"`
	UWebSSL       string `json:"uWebSsl"`
	UWebAliases   string `json:"uWebAliases"`
	UDNSDomains   string `json:"uDnsDomains"`
	UDNSRecords   string `json:"uDnsRecords"`
	UMailDomains  string `json:"uMailDomains"`
	UMailDKIM     string `json:"uMailDkim"`
	UMailAccounts string `json:"uMailAccounts"`
	UDatabases    string `json:"uDatabases"`
	UCronJobs     string `json:"uCronJobs"`
	UBackups      string `json:"uBackups"`
	UUsers        string `json:"uUsers"`
}

// ---------------------------------------------------------------- Cron

// CronJobs answers GET /api/v1/cron, in cron.conf order. Reports: CRON_REPORTS == "yes".
type CronJobs struct {
	Jobs    []hestia.CronJob `json:"jobs"`
	Reports bool             `json:"reports"`
	Usage   CronUsage        `json:"usage"`
	// Contact is the viewed account's CONTACT, the address the job output is mailed to.
	Contact string `json:"contact"`
}

// CronUsage is U_CRON_JOBS / CRON_JOBS ("unlimited" or a number) and SUSPENDED_CRON.
type CronUsage struct {
	Jobs      string `json:"jobs"`
	Limit     string `json:"limit"`
	Suspended string `json:"suspended"`
}

// CronOptions answers GET /api/v1/cron/options: the server's time zone (IANA name) and
// its current time there, RFC 3339.
type CronOptions struct {
	Timezone string `json:"timezone"`
	Now      string `json:"now"`
}

// CronRequest is POST /api/v1/cron and PUT /api/v1/cron/{job}. Every field is
// required (blank check on add and edit). The five schedule fields accept only "*",
// "N", "A-B", "*/N", "A-B/N" and comma lists of these, at most 64 characters, no
// spaces, control characters or names (Parity 23); out of range or A > B is
// `invalid %s format :: %s`.
type CronRequest struct {
	Min   string `json:"min"`   // 0-59
	Hour  string `json:"hour"`  // 0-23
	Day   string `json:"day"`   // 1-31 (0 refused)
	Month string `json:"month"` // 1-12 (0 refused)
	WDay  string `json:"wday"`  // 0-7 (0 and 7 are Sunday)
	// Cmd is stored and shown verbatim. Refused (Parity 24): a backtick, newline,
	// carriage return, NUL or other control character, and exactly "-n", "-e" or "-E".
	Cmd string `json:"cmd"`
}

// CronReportsRequest is POST /api/v1/cron/reports.
type CronReportsRequest struct {
	Enabled bool `json:"enabled"`
}

// ---------------------------------------------------------------- Backups

// Backups answers GET /api/v1/backup, newest first. Incremental: the package allows
// restic snapshots.
type Backups struct {
	Backups     []hestia.Backup `json:"backups"`
	Usage       BackupUsage     `json:"usage"`
	Incremental bool            `json:"incremental"`
}

// BackupUsage is U_BACKUPS / BACKUPS.
type BackupUsage struct {
	Backups string `json:"backups"`
	Limit   string `json:"limit"`
}

// BackupView answers GET /api/v1/backup/{backup}.
type BackupView struct {
	Backup hestia.Backup `json:"backup"`
}

// QueuedReply answers a scheduled job (backup, snapshot, restore). Message is the
// English msgid to toast, e.g. "Task has been added to the queue. You will receive an
// email notification when your backup is ready for download.".
type QueuedReply struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// DownloadReply answers POST /api/v1/backup/{backup}/download: "ready" means GET
// /api/v1/backup/file/{backup} now; "scheduled" means a remote copy was queued and
// Message (a msgid) says an email follows.
type DownloadReply struct {
	Status  string `json:"status" tstype:"'ready' | 'scheduled'"`
	Message string `json:"message"`
}

// RestoreRequest is POST /api/v1/backup/{backup}/restore and
// /api/v1/backup/incremental/{snapshot}/restore. When All is true the item fields are
// ignored, even if set, and everything is restored (the 2-argument form). Otherwise
// the named items and Cron are restored; no item and Cron false is 400
// `No items were selected to restore.` (Parity 30).
type RestoreRequest struct {
	All  bool     `json:"all"`
	Web  []string `json:"web"`
	DNS  []string `json:"dns"`
	Mail []string `json:"mail"`
	DB   []string `json:"db"`
	UDir []string `json:"udir"`
	Cron bool     `json:"cron"`
}

// ExclusionsView answers GET /api/v1/backup/exclusions: the stored entries, and the
// edit form's text (one entry per line, sub-items after ":").
type ExclusionsView struct {
	Exclusions hestia.BackupExclusions `json:"exclusions"`
	Text       ExclusionsText          `json:"text"`
}

// ExclusionsText is the four editable sections as textarea text.
type ExclusionsText struct {
	Web     string `json:"web"`
	Mail    string `json:"mail"`
	DB      string `json:"db"`
	UserDir string `json:"userdir"`
}

// ExclusionsRequest is PUT /api/v1/backup/exclusions; DNS and CRON keep their stored
// values.
type ExclusionsRequest struct {
	Web     string `json:"web"`
	Mail    string `json:"mail"`
	DB      string `json:"db"`
	UserDir string `json:"userdir"`
}

// Snapshots answers GET /api/v1/backup/incremental, newest first.
type Snapshots struct {
	Snapshots []hestia.Snapshot `json:"snapshots"`
}

// SnapshotView answers GET /api/v1/backup/incremental/{snapshot}.
type SnapshotView struct {
	ID     string                `json:"id"`
	Detail hestia.SnapshotDetail `json:"detail"`
}

// SnapshotFiles answers GET /api/v1/backup/incremental/{snapshot}/files?path=: the
// cleaned folder, its parent ("" at /home/<user>) and its children. Nodes holds the
// children only: hestia.DecodeSnapshotNodes keeps the folder's own node first and
// Slice F drops it before replying.
type SnapshotFiles struct {
	Path   string                `json:"path"`
	Parent string                `json:"parent"`
	Nodes  []hestia.SnapshotNode `json:"nodes"`
}
