package httpapi

// The Configure Server save as an ordered list of steps (decisions 25–36). Pure:
// sysSettingsPlan reads the current state and the request and returns the commands
// to run in PHP's order (settings checklist §3.1 steps 1–36), or the refusal to send
// before any job starts. Unchanged values, and sections whose software isn't
// installed, produce no step.

import (
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
)

// sysSecretArg marks the argument that becomes the path of a step's private temp
// file (secret) or directory (files) when the step runs.
const sysSecretArg = "\x00secret"

// sysPHPCandidates are the PHP versions the form offers, ascending (PHP's hard-coded
// list, the core's multiphp_v).
var sysPHPCandidates = []string{"5.6", "7.0", "7.1", "7.2", "7.3", "7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

// sysSettingsState is what a save is compared with. cfg is hestia.conf; the other
// fields are filled only when the request needs them (see sysSettingsNeeds).
type sysSettingsState struct {
	cfg        hestia.SysConfig
	root       bool // the signed-in user is ROOT_USER
	hostname   string
	os         string // v-list-sys-info OS, read with the hostname
	timezone   string
	timezones  []string
	languages  []string
	themes     []string
	php        []SettingsPHP // empty unless WEB_BACKEND is php-fpm and backend templates exist
	defaultPHP string
	dbHosts    []SettingsDBHost
	dnsCluster []string
	remote     SettingsRemote
	restic     SettingsRestic
	ssl        hestia.HestiaSSL
}

func (st *sysSettingsState) hasDB(typ, host string) bool {
	for _, h := range st.dbHosts {
		if h.Type == typ && (host == "" || h.Host == host) {
			return true
		}
	}
	return false
}

// sysSettingStep is one command of the save. name is the English msgid recorded as
// the job step (and in SettingsResult.applied).
type sysSettingStep struct {
	name    string
	cmd     string
	args    []string
	timeout time.Duration
	secret  *string           // written to a 0600 temp file whose path replaces sysSecretArg
	files   map[string]string // written to a 0700 temp dir whose path replaces sysSecretArg
	verify  string            // a PHP version v-list-sys-php must list after the command
	note    bool              // the command's output is returned in SettingsResult.notes
	order   int               // its SET §3.1 step number
}

type sysSettingsPlanned struct {
	steps      []sysSettingStep
	refresh    bool // the default PHP version or the theme for all users changed
	firewallOn bool // turns the firewall on: the lockout guard runs first (decision 37)
}

// sysSettingsReject is a refusal answered before any job starts.
type sysSettingsReject struct {
	status int
	body   ErrorBody
}

func sysReject(status int, code, msg string, args ...string) *sysSettingsReject {
	return &sysSettingsReject{status: status, body: ErrorBody{Code: code, Message: msg, Args: args}}
}

// sysInvalid refuses value for the named field with the bin's "invalid %s format :: %s".
func sysInvalid(name, value string) *sysSettingsReject {
	return sysReject(http.StatusBadRequest, "invalid", invalidFormatMsg, name, value)
}

func sysBlank(label string) *sysSettingsReject {
	return sysReject(http.StatusBadRequest, "invalid", blankFieldMsg, label)
}

const sysForbiddenCharsMsg = "Invalid value contains forbidden characters"

// sysPastedKeyMsg refuses a private key in the remote backup password (D8). The core
// takes an SFTP key only as the path of a key file on the server (it stores the path
// and reads the file at every backup), and the panel's secret file is deleted after
// the step, so a pasted key could never work.
// sysPHPNotInstalledMsg is v-change-sys-php's refusal of a version that isn't installed.
const sysPHPNotInstalledMsg = "ERROR: Specified PHP version is not installed."

const sysPastedKeyMsg = "SFTP keys can't be pasted here. Copy the key file to the server over SSH and add the backup host with v-add-backup-host."

// sysPastedKey says whether v looks like a pasted PEM or OpenSSH private key, with or
// without its newlines.
func sysPastedKey(v string) bool {
	return strings.Contains(v, "-----BEGIN ") || strings.Contains(v, "PRIVATE KEY-----")
}

// sysArg checks a free-text value that reaches a command or hestia.conf (decision
// 34): no ' " | & \ or control characters, and no leading "-".
func sysArg(v string) *sysSettingsReject {
	if !hestia.ValidConfigValue(v) {
		return sysReject(http.StatusBadRequest, "invalid", sysForbiddenCharsMsg)
	}
	if strings.HasPrefix(v, "-") {
		return sysReject(http.StatusBadRequest, "invalid", invalidValueMsg, v)
	}
	return nil
}

// sysSecret checks a password or key going through a temp file: the core reads only
// its first line.
func sysSecret(v string) *sysSettingsReject {
	for _, r := range v {
		if r < ' ' || r == 0x7f {
			return sysReject(http.StatusBadRequest, "invalid", sysForbiddenCharsMsg)
		}
	}
	return nil
}

var (
	sysAliasRe   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,62}[A-Za-z0-9])?$`)
	sysNameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	sysCountRe   = regexp.MustCompile(`^(?:-1|[0-9]{1,6})$`)
	sysMinutesRe = regexp.MustCompile(`^[0-9]{1,6}$`)
)

func sysPort(v string) bool {
	n, err := strconv.Atoi(v)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == v
}

// sysHost accepts a host name or an IPv4 address.
func sysHost(v string) bool {
	return hestia.ValidIPv4(v) || (validHostname(v) && strings.Trim(v, "0123456789.") != "")
}

// sysPMARestricted: phpMyAdmin is restricted only while its flag is yes and SSO is on.
func sysPMARestricted(cfg hestia.SysConfig) bool {
	return cfg["PMA_RESTRICT_ACCESS"] == "yes" && cfg["PHPMYADMIN_KEY"] != ""
}

func sysYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func sysTrueFalse(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// sysBackupTypes splits BACKUP_SYSTEM.
func sysBackupTypes(cfg hestia.SysConfig) []string {
	var out []string
	for _, t := range strings.Split(cfg["BACKUP_SYSTEM"], ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// sysRemoteType is the remote the form shows: the last non-local BACKUP_SYSTEM entry
// (PHP kept the last one).
func sysRemoteType(cfg hestia.SysConfig) string {
	typ := ""
	for _, t := range sysBackupTypes(cfg) {
		if t != "local" {
			typ = t
		}
	}
	return typ
}

// The values the form shows when the config has none (PHP's defaults).
func sysBackupMode(cfg hestia.SysConfig) string {
	if cfg["BACKUP_MODE"] == "" {
		return "gzip"
	}
	return cfg["BACKUP_MODE"]
}

func sysBackupLevel(cfg hestia.SysConfig) string {
	if cfg["BACKUP_GZIP"] == "" {
		return "5"
	}
	return cfg["BACKUP_GZIP"]
}

func sysRelayPort(p string) string {
	if p == "" {
		return "587"
	}
	return p
}

// sysAllowedIPs turns the textarea into the stored comma list: lines trimmed, blank
// lines dropped, each an IPv4 or IPv6 address or allow-all (decision 30). bad is the
// first invalid line.
func sysAllowedIPs(text string) (list, bad string) {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line != "allow-all" {
			a, err := netip.ParseAddr(line)
			if err != nil || a.Zone() != "" {
				return "", line
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, ","), ""
}

type sysPlanner struct {
	st  *sysSettingsState
	out sysSettingsPlanned
	at  int // the SET §3.1 step the next commands belong to
}

func (p *sysPlanner) add(name string, timeout time.Duration, cmd string, args ...string) *sysSettingStep {
	if args == nil {
		args = []string{}
	}
	p.out.steps = append(p.out.steps, sysSettingStep{name: name, cmd: cmd, args: args, timeout: timeout, order: p.at})
	return &p.out.steps[len(p.out.steps)-1]
}

func (p *sysPlanner) config(name, key, value string) {
	p.add(name, sysTimeoutDefault, "v-change-sys-config-value", key, value)
}

// trueFalse sets a true/false key (true only when "true") when want differs.
func (p *sysPlanner) trueFalse(name, key string, want *bool) {
	if want != nil && *want != (p.st.cfg[key] == "true") {
		p.config(name, key, sysTrueFalse(*want))
	}
}

// yesNo sets a yes/no key that is on unless "no".
func (p *sysPlanner) yesNo(name, key string, want *bool) {
	if want != nil && *want != (p.st.cfg[key] != "no") {
		p.config(name, key, sysYesNo(*want))
	}
}

// yesOnly sets a yes/no key that is on only when "yes".
func (p *sysPlanner) yesOnly(name, key string, want *bool) {
	if want != nil && *want != (p.st.cfg[key] == "yes") {
		p.config(name, key, sysYesNo(*want))
	}
}

// toggle runs on or off when want differs from cur.
func (p *sysPlanner) toggle(want *bool, cur bool, timeout time.Duration, onName, onCmd, offName, offCmd string) *sysSettingStep {
	if want == nil || *want == cur {
		return nil
	}
	if *want {
		return p.add(onName, timeout, onCmd)
	}
	return p.add(offName, timeout, offCmd)
}

// enum sets key when want is present and differs from the stored value; want must be
// one of allowed.
func (p *sysPlanner) enum(name, key string, want *string, allowed []string) *sysSettingsReject {
	if want == nil || *want == p.st.cfg[key] {
		return nil
	}
	if !slices.Contains(allowed, *want) {
		return sysInvalid(key, *want)
	}
	p.config(name, key, *want)
	return nil
}

func sysPluginField(pl *SettingsPlugins, f func(*SettingsPlugins) bool) *bool {
	if pl == nil {
		return nil
	}
	v := f(pl)
	return &v
}

func sysPolicyField(pol *SettingsPolicies, f func(*SettingsPolicies) bool) *bool {
	if pol == nil {
		return nil
	}
	v := f(pol)
	return &v
}

// sysSettingsPlan builds the save. A reject means nothing may run.
func sysSettingsPlan(st *sysSettingsState, req *SettingsRequest) (sysSettingsPlanned, *sysSettingsReject) {
	p := &sysPlanner{st: st}
	for _, f := range []func(*sysPlanner, *SettingsRequest) *sysSettingsReject{
		sysPlanRootOnly, sysPlanBasics, sysPlanPlugins, sysPlanMail, sysPlanDB, sysPlanBackups, sysPlanSecurity, sysPlanSSL,
	} {
		if rej := f(p, req); rej != nil {
			return sysSettingsPlanned{}, rej
		}
	}
	// The sections interleave in PHP's order; sort the steps back into it. Steps of
	// one SET step keep the order they were added in.
	slices.SortStableFunc(p.out.steps, func(a, b sysSettingStep) int { return a.order - b.order })
	return p.out, nil
}

// The three System Protection policies are root-only (decision 2): a changed value
// from any other admin is 403 before anything is read or run.
func sysPlanRootOnly(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	if p.st.root {
		return nil
	}
	cfg := p.st.cfg
	for key, want := range map[string]*bool{
		"POLICY_SYSTEM_PROTECTED_ADMIN": req.ProtectedAdmin,
		"POLICY_SYSTEM_HIDE_ADMIN":      req.HideAdmin,
		"POLICY_SYSTEM_HIDE_SERVICES":   req.HideServices,
	} {
		if want != nil && *want != (cfg[key] == "yes") {
			return sysReject(http.StatusForbidden, "forbidden", "")
		}
	}
	return nil
}

// Steps 1–8: hostname, PHP, time zone, language, theme, debug, app installer,
// preview features.
func sysPlanBasics(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	st, cfg := p.st, p.st.cfg
	p.at = 1
	if v := req.Hostname; v != nil && *v != st.hostname {
		if *v == "" {
			return sysBlank("Hostname")
		}
		if rej := sysArg(*v); rej != nil {
			return rej
		}
		if !sysHost(*v) || hestia.ValidIPv4(*v) {
			return sysInvalid("hostname", *v)
		}
		p.add("Change hostname", sysTimeoutDefault, "v-change-sys-hostname", *v)
	}
	if rej := sysPlanPHP(p, req); rej != nil {
		return rej
	}
	p.at = 3
	if v := req.Timezone; v != nil && *v != st.timezone {
		if !slices.Contains(st.timezones, *v) {
			return sysInvalid("timezone", *v)
		}
		p.add("Change time zone", sysTimeoutDefault, "v-change-sys-timezone", *v)
	}
	p.at = 4
	if v := req.Language; v != nil && *v != cfg["LANGUAGE"] {
		if !slices.Contains(st.languages, *v) {
			return sysInvalid("language", *v)
		}
		args := []string{*v}
		if req.LanguageForAll {
			args = append(args, "yes") // once (decision 33)
		}
		p.add("Change language", sysTimeoutDefault, "v-change-sys-language", args...)
	}
	p.at = 5
	if v := req.Theme; v != nil && *v != cfg["THEME"] {
		if !slices.Contains(st.themes, *v) {
			return sysInvalid("theme", *v)
		}
		p.config("Change theme", "THEME", *v)
	}
	p.at = 6
	p.trueFalse("Change debug mode", "DEBUG_MODE", req.DebugMode)
	p.at = 7
	p.trueFalse("Change Quick App Installer", "PLUGIN_APP_INSTALLER",
		sysPluginField(req.Plugins, func(pl *SettingsPlugins) bool { return pl.AppInstaller }))
	p.at = 8
	if v := req.PreviewFeatures; v != nil && *v != (cfg["POLICY_SYSTEM_ENABLE_BACON"] == "true") {
		p.config("Change preview features", "POLICY_SYSTEM_ENABLE_BACON", sysTrueFalse(*v))
		// Turning preview off also turns off the preview policy (decision 32).
		if !*v && cfg["POLICY_USER_VIEW_SUSPENDED"] == "yes" {
			p.config("Turn off read-only access for suspended users", "POLICY_USER_VIEW_SUSPENDED", "no")
		}
	}
	return nil
}

// Step 2: PHP versions, then the default version (decision 27). Offered only with
// php-fpm and backend templates.
func sysPlanPHP(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	st := p.st
	if st.cfg["WEB_BACKEND"] != "php-fpm" || len(st.php) == 0 || (req.PHPVersions == nil && req.DefaultPHP == nil) {
		return nil
	}
	p.at = 2
	installed := map[string]bool{}
	for _, row := range st.php {
		installed[row.Version] = row.Installed
	}
	if req.PHPVersions != nil {
		want := map[string]bool{}
		for _, v := range *req.PHPVersions {
			if !slices.Contains(sysPHPCandidates, v) {
				return sysInvalid("PHP version", v)
			}
			want[v] = true
		}
		for _, row := range st.php {
			switch v := row.Version; {
			case want[v] && !row.Installed:
				// "Install PHP %s" / "Remove PHP %s" with the version filled in: a job
				// step is one string, so the UI finds the msgid through StepMsgids.
				p.add(stepName(StepInstallPHPMsgid, v), sysTimeoutInstall, "v-add-web-php", v).verify = v
				installed[v] = true
			case !want[v] && row.Installed:
				if row.Protected {
					return sysReject(http.StatusConflict, "in_use", "PHP %s is in use and can't be removed.", v)
				}
				p.add(stepName(StepRemovePHPMsgid, v), sysTimeoutInstall, "v-delete-web-php", v)
				installed[v] = false
			}
		}
	}
	if v := req.DefaultPHP; v != nil && *v != "" && *v != st.defaultPHP {
		if !installed[*v] {
			return sysReject(http.StatusBadRequest, "invalid", sysPHPNotInstalledMsg)
		}
		p.add("Change system PHP version", sysTimeoutService, "v-change-sys-php", *v)
		p.out.refresh = true
	}
	return nil
}

// Steps 9, 10, 13, 14, 15: the plugin toggles (app installer is step 7, above).
func sysPlanPlugins(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	pl, cfg := req.Plugins, p.st.cfg
	if pl == nil {
		return nil
	}
	p.at = 9
	p.toggle(&pl.FileManager, cfg["FILE_MANAGER"] == "true", sysTimeoutInstall,
		"Install File Manager", "v-add-sys-filemanager", "Remove File Manager", "v-delete-sys-filemanager")
	p.at = 10
	p.toggle(&pl.WebTerminal, cfg["WEB_TERMINAL"] == "true", sysTimeoutInstall,
		"Install Web Terminal", "v-add-sys-web-terminal", "Remove Web Terminal", "v-delete-sys-web-terminal")
	p.at = 13
	if s := p.toggle(&pl.DiskQuota, cfg["DISK_QUOTA"] == "yes", sysTimeoutInstall,
		"Turn on disk quota", "v-add-sys-quota", "Turn off disk quota", "v-delete-sys-quota"); s != nil && pl.DiskQuota {
		s.note = true // "A system reboot is required…" (decision 36)
	}
	p.at = 14
	p.toggle(&pl.ResourcesLimit, cfg["RESOURCES_LIMIT"] == "yes", sysTimeoutDefault,
		"Turn on resource limits", "v-add-sys-cgroups", "Turn off resource limits", "v-delete-sys-cgroups")
	p.at = 15
	if s := p.toggle(&pl.Firewall, cfg["FIREWALL_SYSTEM"] == "iptables", sysTimeoutDefault,
		"Turn on firewall", "v-add-sys-firewall", "Turn off firewall", "v-delete-sys-firewall"); s != nil && pl.Firewall {
		p.out.firewallOn = true
	}
	return nil
}

// Steps 17 and 18: webmail alias and SMTP relay, only with a mail system.
func sysPlanMail(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	cfg := p.st.cfg
	if cfg["MAIL_SYSTEM"] == "" {
		return nil
	}
	p.at = 17
	if v := req.WebmailAlias; v != nil && cfg["WEBMAIL_SYSTEM"] != "" && *v != cfg["WEBMAIL_ALIAS"] {
		if rej := sysAlias("Webmail Alias", *v); rej != nil {
			return rej
		}
		p.add("Change webmail alias", sysTimeoutService, "v-change-sys-webmail", *v)
	}
	p.at = 18
	cur := cfg["SMTP_RELAY"] == "true"
	on := cur
	if req.SMTPRelay != nil {
		on = *req.SMTPRelay
	}
	if !on {
		// Only a relay that is on is turned off (decision 29; "false" is off).
		if cur {
			p.add("Turn off SMTP relay", sysTimeoutDefault, "v-delete-sys-smtp-relay")
		}
		return nil
	}
	pick := func(v *string, stored string) string {
		if v != nil {
			return *v
		}
		return stored
	}
	host := pick(req.SMTPRelayHost, cfg["SMTP_RELAY_HOST"])
	port := sysRelayPort(pick(req.SMTPRelayPort, cfg["SMTP_RELAY_PORT"]))
	user := pick(req.SMTPRelayUser, cfg["SMTP_RELAY_USER"])
	pw := req.SMTPRelayPassword
	if cur && host == cfg["SMTP_RELAY_HOST"] && port == sysRelayPort(cfg["SMTP_RELAY_PORT"]) &&
		user == cfg["SMTP_RELAY_USER"] && pw == "" {
		return nil
	}
	if host == "" {
		return sysBlank("Host")
	}
	for _, v := range []string{host, port, user} {
		if rej := sysArg(v); rej != nil {
			return rej
		}
	}
	switch {
	case !sysHost(host):
		return sysInvalid("host", host)
	case !sysPort(port):
		return sysInvalid("port", port)
	case strings.ContainsAny(user, " \t"):
		return sysInvalid("username", user)
	}
	// The stored password can't be read back: a change with a user needs it again.
	if user != "" && pw == "" {
		return sysBlank("Password")
	}
	if rej := sysSecret(pw); rej != nil {
		return rej
	}
	name := "Turn on SMTP relay"
	if cur {
		name = "Change SMTP relay"
	}
	p.add(name, sysTimeoutDefault, "v-add-sys-smtp-relay", host, user, sysSecretArg, port).secret = &pw
	return nil
}

func sysAlias(label, v string) *sysSettingsReject {
	if v == "" {
		return sysBlank(label)
	}
	if rej := sysArg(v); rej != nil {
		return rej
	}
	if !sysAliasRe.MatchString(v) || strings.Contains(v, "--") || strings.Contains(v, "__") {
		return sysInvalid(label, v)
	}
	return nil
}

// Steps 11, 12, 16, 19: phpMyAdmin SSO and restrict (decision 35), the root password
// and the two aliases, each only when its database type has a host (decision 25).
func sysPlanDB(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	st, cfg := p.st, p.st.cfg
	if cfg["DB_SYSTEM"] == "" {
		return nil
	}
	if st.hasDB("mysql", "") {
		p.at = 11
		ssoCur, restrictCur := cfg["PHPMYADMIN_KEY"] != "", sysPMARestricted(cfg)
		// Gotcha 19: SSO turned off left PMA_RESTRICT_ACCESS=yes behind. The
		// restriction is off then (v-delete-sys-pma-restrict would fail); the flag is
		// cleared before SSO comes back, so the core can restrict again.
		stale := cfg["PMA_RESTRICT_ACCESS"] == "yes" && !ssoCur
		sso, restrict := ssoCur, restrictCur
		if req.PMASSO != nil {
			sso = *req.PMASSO
		}
		if req.PMARestrict != nil {
			restrict = *req.PMARestrict
		}
		if restrict && !sso {
			if req.PMARestrict != nil && *req.PMARestrict && !restrictCur {
				return sysReject(http.StatusBadRequest, "invalid", "phpMyAdmin can only be restricted with Single Sign On enabled.")
			}
			restrict = false // SSO going off takes the restriction with it
		}
		switch {
		case sso && !ssoCur:
			if stale {
				p.config("Clear the stale phpMyAdmin restriction", "PMA_RESTRICT_ACCESS", "no")
			}
			p.add("Turn on phpMyAdmin Single Sign On", sysTimeoutDefault, "v-add-sys-pma-sso", "quiet")
		case !sso && ssoCur:
			if restrictCur {
				p.add("Turn off phpMyAdmin restriction", sysTimeoutDefault, "v-delete-sys-pma-restrict", "quiet")
				restrictCur = false
			}
			p.add("Turn off phpMyAdmin Single Sign On", sysTimeoutDefault, "v-delete-sys-pma-sso", "quiet")
		}
		switch {
		case restrict && !restrictCur:
			p.add("Restrict phpMyAdmin", sysTimeoutDefault, "v-add-sys-pma-restrict", "quiet")
		case !restrict && restrictCur:
			p.add("Turn off phpMyAdmin restriction", sysTimeoutDefault, "v-delete-sys-pma-restrict", "quiet")
		}
		p.at = 19
		if v := req.PMAAlias; v != nil && *v != cfg["DB_PMA_ALIAS"] {
			if rej := sysAlias("phpMyAdmin Alias", *v); rej != nil {
				return rej
			}
			p.add("Change phpMyAdmin alias", sysTimeoutService, "v-change-sys-db-alias", "pma", *v)
		}
	}
	p.at = 16
	if pw := req.MySQLPassword; pw != "" && st.hasDB("mysql", "localhost") {
		if rej := sysSecret(pw); rej != nil {
			return rej
		}
		p.add("Change database root password", sysTimeoutDefault, "v-change-database-host-password",
			"mysql", "localhost", "root", sysSecretArg).secret = &pw
	}
	p.at = 19
	if v := req.PGAAlias; v != nil && st.hasDB("pgsql", "") && *v != cfg["DB_PGA_ALIAS"] {
		if rej := sysAlias("phpPgAdmin Alias", *v); rej != nil {
			return rej
		}
		p.add("Change phpPgAdmin alias", sysTimeoutService, "v-change-sys-db-alias", "pga", *v)
	}
	return nil
}

// Steps 20–29: update mails, local backup, compression, remote host, restic.
func sysPlanBackups(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	cfg := p.st.cfg
	p.at = 20
	p.trueFalse("Change update notifications", "UPGRADE_SEND_EMAIL", req.UpgradeEmail)
	p.trueFalse("Change update log emails", "UPGRADE_SEND_EMAIL_LOG", req.UpgradeEmailLog)
	p.at = 21
	if s := p.toggle(req.BackupLocal, slices.Contains(sysBackupTypes(cfg), "local"), sysTimeoutDefault,
		"Turn on local backup", "v-add-backup-host", "Turn off local backup", "v-delete-backup-host"); s != nil {
		s.args = []string{"local"}
	}

	// Compression: gzip forces level 9 (kept as in PHP), and only a changed value runs.
	curMode, curLevel := sysBackupMode(cfg), sysBackupLevel(cfg)
	mode, level := curMode, curLevel
	if v := req.BackupMode; v != nil {
		if *v != "gzip" && *v != "zstd" {
			return sysInvalid("BACKUP_MODE", *v)
		}
		mode = *v
	}
	if v := req.BackupLevel; v != nil {
		if n, err := strconv.Atoi(*v); err != nil || n < 1 || n > 19 || strconv.Itoa(n) != *v {
			return sysInvalid("BACKUP_GZIP", *v)
		}
		level = *v
	}
	modeChanged := mode != curMode
	if level != curLevel || (modeChanged && mode == "gzip") {
		if mode == "gzip" {
			level = "9"
		}
		p.at = 22
		if level != cfg["BACKUP_GZIP"] {
			p.config("Change backup compression level", "BACKUP_GZIP", level)
		}
	}
	p.at = 23
	if modeChanged {
		p.config("Change backup compression", "BACKUP_MODE", mode)
	}

	if rej := sysPlanRemote(p, req); rej != nil {
		return rej
	}
	return sysPlanRestic(p, req)
}

// Steps 25–28: the remote backup host (decision 28). Passwords and the B2 key go
// through temp files; a type switch deletes the old type first; rclone edits are a
// delete then an add.
func sysPlanRemote(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	if req.BackupRemote == nil {
		return nil
	}
	p.at = 25
	cur, n := p.st.remote, *req.BackupRemote
	switch n.Type {
	case "":
		if cur.Type != "" {
			p.add("Remove remote backup host", sysTimeoutDefault, "v-delete-backup-host", cur.Type)
		}
		return nil
	case "ftp", "sftp", "b2", "rclone":
	default:
		return sysInvalid("backup type", n.Type)
	}
	same := n.Type == cur.Type
	var changed bool
	switch n.Type {
	case "ftp", "sftp":
		changed = !same || n.Host != cur.Host || n.Username != cur.Username || n.Port != cur.Port ||
			n.Path != cur.Path || req.BackupPassword != ""
	case "b2":
		changed = !same || n.Bucket != cur.Bucket || n.KeyID != cur.KeyID || req.B2Key != ""
	case "rclone":
		changed = !same || n.Host != cur.Host || n.Path != cur.Path
	}
	if !changed {
		return nil
	}
	var step sysSettingStep
	switch n.Type {
	case "ftp", "sftp":
		if rej := sysRemoteFields([2]string{"Host", n.Host}, [2]string{"Username", n.Username}); rej != nil {
			return rej
		}
		for _, v := range []string{n.Port, n.Path} {
			if rej := sysArg(v); rej != nil {
				return rej
			}
		}
		switch {
		case !sysHost(n.Host):
			return sysInvalid("host", n.Host)
		case strings.ContainsAny(n.Username, " \t"):
			return sysInvalid("username", n.Username)
		case n.Port != "" && !sysPort(n.Port):
			return sysInvalid("port", n.Port)
		case n.Path != "" && !strings.HasPrefix(n.Path, "/"):
			return sysInvalid("path", n.Path)
		}
		if req.BackupPassword == "" {
			return sysBlank("Password")
		}
		if sysPastedKey(req.BackupPassword) {
			return sysReject(http.StatusBadRequest, "invalid", sysPastedKeyMsg)
		}
		if rej := sysSecret(req.BackupPassword); rej != nil {
			return rej
		}
		path := n.Path
		if path == "" {
			path = "/backup"
		}
		pw := req.BackupPassword
		step = sysSettingStep{cmd: "v-add-backup-host", args: []string{n.Type, n.Host, n.Username, sysSecretArg, path, n.Port}, secret: &pw}
	case "b2":
		if rej := sysRemoteFields([2]string{"Bucket", n.Bucket}, [2]string{"Key ID", n.KeyID}); rej != nil {
			return rej
		}
		if !sysNameRe.MatchString(n.Bucket) {
			return sysInvalid("bucket", n.Bucket)
		}
		if !sysNameRe.MatchString(n.KeyID) {
			return sysInvalid("key id", n.KeyID)
		}
		if req.B2Key == "" {
			return sysBlank("Application Key")
		}
		if rej := sysSecret(req.B2Key); rej != nil {
			return rej
		}
		key := req.B2Key
		step = sysSettingStep{cmd: "v-add-backup-host", args: []string{"b2", n.Bucket, n.KeyID, sysSecretArg}, secret: &key}
	case "rclone":
		if rej := sysRemoteFields([2]string{"Host", n.Host}); rej != nil {
			return rej
		}
		if rej := sysArg(n.Path); rej != nil {
			return rej
		}
		if !sysNameRe.MatchString(n.Host) {
			return sysInvalid("host", n.Host)
		}
		step = sysSettingStep{cmd: "v-add-backup-host", args: []string{"rclone", n.Host, "", "", n.Path}}
	}
	if cur.Type != "" && (!same || n.Type == "rclone") {
		p.add("Remove remote backup host", sysTimeoutDefault, "v-delete-backup-host", cur.Type)
	}
	step.name, step.timeout, step.order = "Add remote backup host", sysTimeoutDefault, p.at
	p.out.steps = append(p.out.steps, step)
	return nil
}

// sysRemoteFields checks label/value pairs: required and free of forbidden characters.
func sysRemoteFields(pairs ...[2]string) *sysSettingsReject {
	for _, f := range pairs {
		if f[1] == "" {
			return sysBlank(f[0])
		}
		if rej := sysArg(f[1]); rej != nil {
			return rej
		}
	}
	return nil
}

// Step 29: restic (decision 31). It runs only when something changed, and an empty
// repository is refused on every path.
func sysPlanRestic(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	if req.Incremental == nil && req.Restic == nil {
		return nil
	}
	p.at = 29
	curOn := p.st.cfg["BACKUP_INCREMENTAL"] == "yes"
	on := curOn
	if req.Incremental != nil {
		on = *req.Incremental
	}
	if !on {
		if curOn {
			p.add("Turn off incremental backups", sysTimeoutInstall, "v-delete-backup-host-restic")
		}
		return nil
	}
	r := p.st.restic
	if req.Restic != nil {
		r = *req.Restic
	}
	if curOn && r == p.st.restic {
		return nil
	}
	if r.Repo == "" {
		return sysReject(http.StatusBadRequest, "invalid", "Repository can not be empty")
	}
	if rej := sysArg(r.Repo); rej != nil {
		return rej
	}
	counts := [][2]string{{"snapshots", r.Snapshots}, {"daily", r.KeepDaily}, {"weekly", r.KeepWeekly},
		{"monthly", r.KeepMonthly}, {"yearly", r.KeepYearly}}
	for _, c := range counts {
		if !sysCountRe.MatchString(c[1]) {
			return sysInvalid(c[0], c[1])
		}
	}
	if curOn {
		p.add("Turn off incremental backups", sysTimeoutInstall, "v-delete-backup-host-restic")
	}
	// The repository goes on argv as PHP sent it (parity), so a repository URL with
	// credentials is visible in the process list; Plan 5 may move it to a file.
	p.add("Turn on incremental backups", sysTimeoutInstall, "v-add-backup-host-restic", r.Repo, r.Snapshots, r.KeepDaily, r.KeepWeekly, r.KeepMonthly, r.KeepYearly)
	return nil
}

// Steps 30–35: session timeout, CSRF, policies, API, login style.
func sysPlanSecurity(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	cfg := p.st.cfg
	p.at = 30
	if v := req.SessionTimeout; v != nil && *v != cfg["INACTIVE_SESSION_TIMEOUT"] {
		n, err := strconv.Atoi(*v)
		if err != nil || !sysMinutesRe.MatchString(*v) || n < 1 {
			return sysReject(http.StatusBadRequest, "invalid", "Inactive session timeout can not lower than 1 minute.")
		}
		if canon := strconv.Itoa(n); canon != cfg["INACTIVE_SESSION_TIMEOUT"] {
			p.config("Change inactive session timeout", "INACTIVE_SESSION_TIMEOUT", canon)
		}
	}
	p.at = 31
	if rej := p.enum("Change CSRF protection", "POLICY_CSRF_STRICTNESS", req.CSRFStrictness, []string{"0", "1", "2"}); rej != nil {
		return rej
	}
	pol := req.Policies
	field := func(f func(*SettingsPolicies) bool) *bool { return sysPolicyField(pol, f) }
	p.at = 32
	p.yesNo("Change subdomain ownership policy", "ENFORCE_SUBDOMAIN_OWNERSHIP", field(func(x *SettingsPolicies) bool { return x.EnforceSubdomain }))
	p.yesNo("Change account details policy", "POLICY_USER_EDIT_DETAILS", field(func(x *SettingsPolicies) bool { return x.EditDetails }))
	p.yesNo("Change web template policy", "POLICY_USER_EDIT_WEB_TEMPLATES", field(func(x *SettingsPolicies) bool { return x.EditWebTemplates }))
	p.yesNo("Change DNS template policy", "POLICY_USER_EDIT_DNS_TEMPLATES", field(func(x *SettingsPolicies) bool { return x.EditDNSTemplates }))

	p.at = 33
	// API (decision 30): config values only, never v-change-sys-api.
	if rej := p.enum("Change API access", "API_SYSTEM", req.APISystem, []string{"0", "1", "2"}); rej != nil {
		return rej
	}
	if v := req.LegacyAPI; v != nil && *v != (cfg["API"] == "yes") {
		p.config("Change legacy API access", "API", sysYesNo(*v))
	}
	if v := req.APIAllowedIP; v != nil {
		list, bad := sysAllowedIPs(*v)
		if bad != "" {
			return sysReject(http.StatusBadRequest, "invalid", "Invalid IP address: %s", bad)
		}
		if list != cfg["API_ALLOWED_IP"] {
			p.config("Change API allowed IP addresses", "API_ALLOWED_IP", list)
		}
	}

	p.at = 34
	p.yesNo("Change log view policy", "POLICY_USER_VIEW_LOGS", field(func(x *SettingsPolicies) bool { return x.ViewLogs }))
	p.yesNo("Change log deletion policy", "POLICY_USER_DELETE_LOGS", field(func(x *SettingsPolicies) bool { return x.DeleteLogs }))
	p.yesNo("Change password reset policy", "POLICY_SYSTEM_PASSWORD_RESET", req.PasswordReset)
	p.yesOnly("Change administrator protection", "POLICY_SYSTEM_PROTECTED_ADMIN", req.ProtectedAdmin)
	// Offered only while preview features are (or are being turned) on.
	preview := cfg["POLICY_SYSTEM_ENABLE_BACON"] == "true"
	if req.PreviewFeatures != nil {
		preview = *req.PreviewFeatures
	}
	if preview {
		p.yesOnly("Change read-only access for suspended users", "POLICY_USER_VIEW_SUSPENDED",
			field(func(x *SettingsPolicies) bool { return x.ViewSuspended }))
	}
	// "Set as selected theme for all users" is stored inverted.
	if v := req.ThemeForAll; v != nil && *v != (cfg["POLICY_USER_CHANGE_THEME"] == "no") {
		p.config("Change theme policy", "POLICY_USER_CHANGE_THEME", sysYesNo(!*v))
		p.out.refresh = true
	}
	p.yesOnly("Change administrator visibility", "POLICY_SYSTEM_HIDE_ADMIN", req.HideAdmin)
	p.yesOnly("Change server settings access", "POLICY_SYSTEM_HIDE_SERVICES", req.HideServices)
	p.yesOnly("Change suspended user backup policy", "POLICY_BACKUP_SUSPENDED_USERS",
		field(func(x *SettingsPolicies) bool { return x.BackupSuspendedUsers }))
	p.yesNo("Change error document sync policy", "POLICY_SYNC_ERROR_DOCUMENTS", field(func(x *SettingsPolicies) bool { return x.SyncErrorDocuments }))
	p.yesNo("Change skeleton sync policy", "POLICY_SYNC_SKELETON", field(func(x *SettingsPolicies) bool { return x.SyncSkeleton }))

	p.at = 35
	return p.enum("Change login screen style", "LOGIN_STYLE", req.LoginStyle, []string{"default", "old"})
}

// Step 36, last: the panel certificate (decision 36). The key never leaves the
// server: "" keeps the stored one, which the core still needs beside the certificate.
func sysPlanSSL(p *sysPlanner, req *SettingsRequest) *sysSettingsReject {
	if req.SSLCrt == nil {
		return nil
	}
	crt, key := pemText(*req.SSLCrt), pemText(req.SSLKey)
	curCrt, curKey := pemText(p.st.ssl.CRT), pemText(p.st.ssl.KEY)
	if crt == "" {
		if key != "" {
			return sysBlank("SSL Certificate")
		}
		return nil
	}
	if crt == curCrt && (key == "" || key == curKey) {
		return nil
	}
	if key == "" {
		key = curKey
	}
	if key == "" {
		return sysBlank("SSL Private Key")
	}
	p.at = 36
	s := p.add("Change panel SSL certificate", sysTimeoutService, "v-change-sys-hestia-ssl", sysSecretArg)
	s.files = map[string]string{"certificate.crt": crt + "\n", "certificate.key": key + "\n"}
	return nil
}
