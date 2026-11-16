package httpapi

// Configure Server (Plan 4 Slice B): GET /api/v1/server/settings reads the form;
// PUT validates the request, then runs the ordered save (sys_settings_steps.go) as a
// server job and answers 202 JobRef. The job's result is SettingsResult.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
	"github.com/hostisan/hostisan/internal/session"
)

// settingsRoutes mounts the Configure Server routes.
func (sys *sysAPI) settingsRoutes(m *sysMux) {
	m.handle("GET /api/v1/server/settings", sys.read(sys.settingsView))
	m.handle("PUT /api/v1/server/settings", sys.write(sys.settingsSave))
}

// sysDefaultPHP is the system PHP version ("8.2") from v-list-default-php: the
// version holding the FPM www.conf, which v-change-sys-php keeps in step with the
// php alternative. (PHP's panel read its own runtime's version instead.) It fails
// closed: a failed read or an empty list is an error, never "no default", because
// the default must stay protected from removal (decision 27).
func (sys *sysAPI) sysDefaultPHP(ctx context.Context) (string, error) {
	list, err := sys.sysReadList(ctx, "v-list-default-php", "json")
	if err != nil {
		return "", err
	}
	if len(list) == 0 || !slices.Contains(sysPHPCandidates, list[0]) {
		return "", errors.New("v-list-default-php: no default PHP version")
	}
	return list[0], nil
}

// sysSettingsNeeds says which reads a settings load runs.
type sysSettingsNeeds struct {
	hostname, timezone, languages, themes, php, dns, db, remote, restic, ssl bool
}

func sysSettingsAll() sysSettingsNeeds {
	return sysSettingsNeeds{true, true, true, true, true, true, true, true, true, true}
}

// sysSettingsNeedsFor is what a save must read to compare and validate req: only the
// sections it sends, and only those installed (decision 25).
func sysSettingsNeedsFor(cfg hestia.SysConfig, req *SettingsRequest) sysSettingsNeeds {
	return sysSettingsNeeds{
		hostname:  req.Hostname != nil,
		timezone:  req.Timezone != nil,
		languages: req.Language != nil,
		themes:    req.Theme != nil,
		php:       req.PHPVersions != nil || req.DefaultPHP != nil,
		db: req.PMAAlias != nil || req.PGAAlias != nil || req.PMASSO != nil || req.PMARestrict != nil ||
			req.MySQLPassword != "",
		remote: req.BackupRemote != nil,
		restic: req.Incremental != nil || req.Restic != nil,
		ssl:    req.SSLCrt != nil,
	}
}

// sysRead runs a read command that must exit 0.
func (sys *sysAPI) sysRead(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), cmd, args...)
	if err == nil {
		err = res.Err(cmd)
	}
	return res.Stdout, err
}

func (sys *sysAPI) sysReadList(ctx context.Context, cmd string, args ...string) ([]string, error) {
	out, err := sys.sysRead(ctx, cmd, args...)
	if err != nil {
		return nil, err
	}
	return hestia.DecodeStringList(out)
}

// sysName decodes only an object's keys (v-list-users).
type sysName struct{ NAME string }

func (n *sysName) SetName(s string) { n.NAME = s }

// sysWebBackend is the one value Plan 4 needs from v-list-web-domains (2d owns the
// full type).
type sysWebBackend struct {
	NAME    string
	BACKEND string `json:"BACKEND"`
}

func (d *sysWebBackend) SetName(s string) { d.NAME = s }

// sysDBHost is the part of v-list-database-hosts (a JSON array) the form shows.
type sysDBHost struct {
	HOST       string `json:"HOST"`
	TYPE       string `json:"TYPE"`
	MAX_DB     string `json:"MAX_DB"`
	U_DB_BASES string `json:"U_DB_BASES"`
}

var sysPHPTemplate = regexp.MustCompile(`PHP-([0-9]+)_([0-9]+)$`)

// settingsState reads what need asks for, in the order PHP's page load did (settings
// checklist §3.1). The per-user web domain reads run concurrently, 8 at a time.
func (sys *sysAPI) settingsState(ctx context.Context, cfg hestia.SysConfig, root bool, need sysSettingsNeeds) (*sysSettingsState, error) {
	st := &sysSettingsState{cfg: cfg, root: root}
	var err error
	if need.hostname {
		out, err := sys.sysRead(ctx, "v-list-sys-info", "json")
		if err != nil {
			return nil, err
		}
		info, err := hestia.DecodeSysInfo(out)
		if err != nil {
			return nil, err
		}
		st.hostname = info.HOSTNAME
		st.os = info.OS
	}
	if need.timezone {
		out, err := sys.sysRead(ctx, "v-get-sys-timezone")
		if err != nil {
			return nil, err
		}
		st.timezone, _, _ = strings.Cut(strings.TrimSpace(string(out)), "\n")
		if st.timezones, err = sys.sysReadList(ctx, "v-get-sys-timezones", "json"); err != nil {
			return nil, err
		}
	}
	if need.php && cfg["WEB_BACKEND"] == "php-fpm" {
		if err := sys.settingsPHP(ctx, st); err != nil {
			return nil, err
		}
	}
	if need.languages {
		if st.languages, err = sys.sysReadList(ctx, "v-list-sys-languages", "json"); err != nil {
			return nil, err
		}
	}
	if need.themes {
		if st.themes, err = sys.sysReadList(ctx, "v-list-sys-themes", "json"); err != nil {
			return nil, err
		}
	}
	st.dnsCluster = []string{}
	if need.dns && cfg["DNS_SYSTEM"] != "" {
		out, err := sys.sysRead(ctx, "v-list-remote-dns-hosts", "json")
		if err != nil {
			return nil, err
		}
		hosts, err := hestia.DecodeOptional[hestia.RemoteDNSHost](out)
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			st.dnsCluster = append(st.dnsCluster, h.HOST)
		}
	}
	st.dbHosts = []SettingsDBHost{}
	if need.db && cfg["DB_SYSTEM"] != "" {
		out, err := sys.sysRead(ctx, "v-list-database-hosts", "json")
		if err != nil {
			return nil, err
		}
		var hosts []sysDBHost
		if len(strings.TrimSpace(string(out))) > 0 {
			if err := json.Unmarshal(out, &hosts); err != nil {
				return nil, err
			}
		}
		for _, h := range hosts {
			st.dbHosts = append(st.dbHosts, SettingsDBHost{Type: h.TYPE, Host: h.HOST, MaxDB: h.MAX_DB, Current: h.U_DB_BASES})
		}
	}
	if need.remote {
		for _, typ := range sysBackupTypes(cfg) {
			if typ == "local" {
				continue
			}
			out, err := sys.sysRead(ctx, "v-list-backup-host", typ, "json")
			if err != nil {
				return nil, err
			}
			hosts, err := hestia.DecodeOptional[hestia.BackupHost](out)
			if err != nil {
				return nil, err
			}
			// The form shows one remote: the last type (as PHP did).
			st.remote = SettingsRemote{Type: typ}
			if len(hosts) > 0 {
				b := hosts[0]
				st.remote = SettingsRemote{Type: typ, Host: b.HOST, Port: b.PORT, Username: b.USERNAME, Path: b.BPATH,
					Bucket: b.BUCKET, KeyID: b.B2_KEY_ID}
			}
		}
	}
	// PHP's defaults while incremental backups are off.
	st.restic = SettingsRestic{Snapshots: "30", KeepDaily: "-1", KeepWeekly: "-1", KeepMonthly: "-1", KeepYearly: "-1"}
	if need.restic && cfg["BACKUP_INCREMENTAL"] == "yes" {
		out, err := sys.sysRead(ctx, "v-list-backup-host-restic", "json")
		if err != nil {
			return nil, err
		}
		hosts, err := hestia.DecodeOptional[hestia.ResticHost](out)
		if err != nil {
			return nil, err
		}
		st.restic = SettingsRestic{}
		if len(hosts) > 0 {
			r := hosts[0]
			st.restic = SettingsRestic{Repo: r.REPO, Snapshots: r.SNAPSHOTS, KeepDaily: r.KEEP_DAILY,
				KeepWeekly: r.KEEP_WEEKLY, KeepMonthly: r.KEEP_MONTHLY, KeepYearly: r.KEEP_YEARLY}
		}
	}
	if need.ssl {
		out, err := sys.sysRead(ctx, "v-list-sys-hestia-ssl", "json")
		if err != nil {
			return nil, err
		}
		certs, err := hestia.DecodeObjects[hestia.HestiaSSL](out)
		if err != nil {
			return nil, err
		}
		if len(certs) > 0 {
			st.ssl = certs[0]
		}
	}
	return st, nil
}

// settingsPHP fills the PHP rows: installed from the backend templates, used by from
// every user's web domains (BACKEND, custom names ending PHP-x_y included), and
// protected when installed and used or the system default (decision 27).
func (sys *sysAPI) settingsPHP(ctx context.Context, st *sysSettingsState) error {
	templates, err := sys.sysReadList(ctx, "v-list-web-templates-backend", "json")
	if err != nil || len(templates) == 0 {
		return err
	}
	out, err := sys.sysRead(ctx, "v-list-users", "json")
	if err != nil {
		return err
	}
	users, err := hestia.DecodeObjects[sysName](out)
	if err != nil {
		return err
	}
	domains := make([][]sysWebBackend, len(users))
	errs := make([]error, len(users))
	limit := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			out, err := sys.sysRead(ctx, "v-list-web-domains", u.NAME, "json")
			if err == nil {
				domains[i], err = hestia.DecodeOptional[sysWebBackend](out)
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	usedBy := map[string][]SettingsPHPUse{}
	for i, u := range users {
		for _, d := range domains[i] {
			if m := sysPHPTemplate.FindStringSubmatch(d.BACKEND); m != nil {
				v := m[1] + "." + m[2]
				usedBy[v] = append(usedBy[v], SettingsPHPUse{User: u.NAME, Domain: d.NAME})
			}
		}
	}
	if st.defaultPHP, err = sys.sysDefaultPHP(ctx); err != nil {
		return err
	}
	for _, v := range sysPHPCandidates {
		row := SettingsPHP{Version: v, UsedBy: usedBy[v]}
		if row.UsedBy == nil {
			row.UsedBy = []SettingsPHPUse{}
		}
		row.Installed = slices.Contains(templates, "PHP-"+strings.ReplaceAll(v, ".", "_"))
		row.Protected = row.Installed && (len(row.UsedBy) > 0 || v == st.defaultPHP)
		st.php = append(st.php, row)
	}
	return nil
}

// settingsView is GET /api/v1/server/settings.
func (sys *sysAPI) settingsView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := sys.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	sess := session.FromContext(ctx)
	st, err := sys.settingsState(ctx, cfg, sess.User == rootUser(cfg), sysSettingsAll())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sysSettingsViewOf(st))
}

func sysSettingsViewOf(st *sysSettingsState) SettingsView {
	cfg := st.cfg
	yes := func(k string) bool { return cfg[k] == "yes" }
	notNo := func(k string) bool { return cfg[k] != "no" }
	isTrue := func(k string) bool { return cfg[k] == "true" }
	v := SettingsView{
		Hostname: st.hostname, Timezone: st.timezone, Theme: cfg["THEME"], ThemeForAll: cfg["POLICY_USER_CHANGE_THEME"] == "no",
		Language: cfg["LANGUAGE"], DebugMode: isTrue("DEBUG_MODE"),
		Version: cfg["VERSION"], ReleaseBranch: cfg["RELEASE_BRANCH"], PreviewFeatures: isTrue("POLICY_SYSTEM_ENABLE_BACON"),
		UpgradeSendEmail: isTrue("UPGRADE_SEND_EMAIL"), UpgradeSendEmailLog: isTrue("UPGRADE_SEND_EMAIL_LOG"),
		ProxySystem: cfg["PROXY_SYSTEM"], WebSystem: cfg["WEB_SYSTEM"], WebBackend: cfg["WEB_BACKEND"],
		PHPVersions: st.php, DefaultPHP: st.defaultPHP, DNSSystem: cfg["DNS_SYSTEM"], DNSCluster: st.dnsCluster,
		DNSStatus: installed(cfg["DNS_SYSTEM"]) && !sysIsRHEL(st.os),
		Backup: SettingsBackup{
			Local: slices.Contains(sysBackupTypes(cfg), "local"), Mode: sysBackupMode(cfg), Level: sysBackupLevel(cfg),
			Directory: cfg["BACKUP"], Remote: st.remote, Incremental: yes("BACKUP_INCREMENTAL"), Restic: st.restic,
		},
		SSL: SettingsSSL{Crt: st.ssl.CRT, CA: st.ssl.CA, Subject: st.ssl.SUBJECT, Aliases: st.ssl.ALIASES,
			NotBefore: st.ssl.NOT_BEFORE, NotAfter: st.ssl.NOT_AFTER, Signature: st.ssl.SIGNATURE,
			PubKey: st.ssl.PUB_KEY, Issuer: st.ssl.ISSUER},
		Security: SettingsSecurity{
			APISystem: cfg["API_SYSTEM"], LegacyAPI: yes("API"), APIAllowedIP: strings.ReplaceAll(cfg["API_ALLOWED_IP"], ",", "\n"),
			LoginStyle: cfg["LOGIN_STYLE"], PasswordReset: notNo("POLICY_SYSTEM_PASSWORD_RESET"),
			SessionTimeout: cfg["INACTIVE_SESSION_TIMEOUT"], CSRFStrictness: cfg["POLICY_CSRF_STRICTNESS"],
		},
		Policies: SettingsPolicies{
			EditDetails: notNo("POLICY_USER_EDIT_DETAILS"), EditWebTemplates: notNo("POLICY_USER_EDIT_WEB_TEMPLATES"),
			EditDNSTemplates: notNo("POLICY_USER_EDIT_DNS_TEMPLATES"), ViewLogs: notNo("POLICY_USER_VIEW_LOGS"),
			DeleteLogs: notNo("POLICY_USER_DELETE_LOGS"), ViewSuspended: yes("POLICY_USER_VIEW_SUSPENDED"),
			BackupSuspendedUsers: yes("POLICY_BACKUP_SUSPENDED_USERS"), SyncErrorDocuments: notNo("POLICY_SYNC_ERROR_DOCUMENTS"),
			SyncSkeleton: notNo("POLICY_SYNC_SKELETON"), EnforceSubdomain: notNo("ENFORCE_SUBDOMAIN_OWNERSHIP"),
		},
		Plugins: SettingsPlugins{
			AppInstaller: isTrue("PLUGIN_APP_INSTALLER"), FileManager: isTrue("FILE_MANAGER"), WebTerminal: isTrue("WEB_TERMINAL"),
			ResourcesLimit: yes("RESOURCES_LIMIT"), DiskQuota: yes("DISK_QUOTA"), Firewall: cfg["FIREWALL_SYSTEM"] == "iptables",
		},
		Options: SettingsOptions{Timezones: st.timezones, Themes: st.themes, Languages: st.languages},
		Protection: SettingsProtected{ProtectedAdmin: yes("POLICY_SYSTEM_PROTECTED_ADMIN"),
			HideAdmin: yes("POLICY_SYSTEM_HIDE_ADMIN"), HideServices: yes("POLICY_SYSTEM_HIDE_SERVICES")},
		CanEditProtection: st.root,
	}
	if v.PHPVersions == nil {
		v.PHPVersions = []SettingsPHP{}
	}
	if cfg["MAIL_SYSTEM"] != "" {
		v.Mail = SettingsMail{System: cfg["MAIL_SYSTEM"], Antivirus: cfg["ANTIVIRUS_SYSTEM"], Antispam: cfg["ANTISPAM_SYSTEM"],
			WebmailSystem: cfg["WEBMAIL_SYSTEM"], WebmailAlias: cfg["WEBMAIL_ALIAS"], Relay: isTrue("SMTP_RELAY"),
			RelayHost: cfg["SMTP_RELAY_HOST"], RelayPort: cfg["SMTP_RELAY_PORT"], RelayUser: cfg["SMTP_RELAY_USER"]}
	}
	v.DB = SettingsDB{System: cfg["DB_SYSTEM"], Hosts: st.dbHosts}
	if cfg["DB_SYSTEM"] != "" {
		v.DB.MySQL, v.DB.PgSQL, v.DB.MySQLLocal = st.hasDB("mysql", ""), st.hasDB("pgsql", ""), st.hasDB("mysql", "localhost")
		if v.DB.MySQL {
			v.DB.PMAAlias, v.DB.PMASSO = cfg["DB_PMA_ALIAS"], cfg["PHPMYADMIN_KEY"] != ""
			v.DB.PMARestrict = sysPMARestricted(cfg)
		}
		if v.DB.PgSQL {
			v.DB.PGAAlias = cfg["DB_PGA_ALIAS"]
		}
	}
	return v
}

// settingsSave is PUT /api/v1/server/settings.
func (sys *sysAPI) settingsSave(w http.ResponseWriter, r *http.Request) {
	var req SettingsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx := r.Context()
	cfg, err := sys.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	root := session.FromContext(ctx).User == rootUser(cfg)
	// Root-only fields first, so a refused admin causes no reads either.
	if rej := sysPlanRootOnly(&sysPlanner{st: &sysSettingsState{cfg: cfg, root: root}}, &req); rej != nil {
		writeJSON(w, rej.status, APIError{Error: rej.body})
		return
	}
	st, err := sys.settingsState(ctx, cfg, root, sysSettingsNeedsFor(cfg, &req))
	if err != nil {
		writeCommandError(w, err)
		return
	}
	plan, rej := sysSettingsPlan(st, &req)
	if rej != nil {
		writeJSON(w, rej.status, APIError{Error: rej.body})
		return
	}
	// Turning the firewall on is checked here in the request, before the job starts,
	// never inside it (decision 37, ruling): the guard takes fwMu itself, so it must
	// not be held here. Only a *jobs.Failure is the lockout (409); any other error is
	// a failed command.
	if plan.firewallOn {
		if err := sys.checkFirewallEnable(ctx, r); err != nil {
			var f *jobs.Failure
			if errors.As(err, &f) {
				writeJSON(w, http.StatusConflict, APIError{Error: ErrorBody{Code: f.Code, Message: f.Message, Args: f.Args}})
				return
			}
			writeCommandError(w, err)
			return
		}
	}
	sys.startJob(w, r, "Configure Server", func(ctx context.Context, j *jobs.Job) (any, error) {
		return sys.runSettings(ctx, j, plan)
	})
}

// runSettings runs the planned steps in order and stops at the first failure; the
// steps already run stay applied (decision 26). The config cache is dropped afterwards
// either way, because any step may have changed hestia.conf.
func (sys *sysAPI) runSettings(ctx context.Context, j *jobs.Job, plan sysSettingsPlanned) (any, error) {
	defer sys.H.InvalidateConfig()
	out := SettingsResult{Applied: []string{}, Notes: []string{}, Refresh: plan.refresh}
	for _, st := range plan.steps {
		res, err := sys.runSettingStep(ctx, j, st)
		if err != nil {
			return nil, err
		}
		out.Applied = append(out.Applied, st.name)
		if note := strings.TrimSpace(string(res.Stdout)); st.note && note != "" {
			out.Notes = append(out.Notes, brand.Rewrite(note))
		}
	}
	return out, nil
}

// runSettingStep runs one step, with its secret in a private temp file or directory
// that is removed again whatever happens.
func (sys *sysAPI) runSettingStep(ctx context.Context, j *jobs.Job, st sysSettingStep) (runner.Result, error) {
	args := slices.Clone(st.args)
	var path string
	switch {
	case st.secret != nil:
		p, cleanup, err := secret.TempFile(sys.tmpDir(), *st.secret)
		if err != nil {
			return runner.Result{}, err
		}
		defer cleanup()
		path = p
	case st.files != nil:
		p, cleanup, err := secret.TempDir(sys.tmpDir(), st.files)
		if err != nil {
			return runner.Result{}, err
		}
		defer cleanup()
		path = p
	}
	for i, a := range args {
		if a == sysSecretArg {
			args[i] = path
		}
	}
	res, err := sys.step(ctx, j, st.name, st.timeout, st.cmd, args...)
	if err != nil || st.verify == "" {
		return res, err
	}
	// v-add-web-php exits 0 even when apt failed (decision 27): check it's there.
	installed, err := sys.sysReadList(ctx, "v-list-sys-php", "json")
	if err != nil {
		return res, err
	}
	if !slices.Contains(installed, st.verify) {
		if msg := strings.TrimSpace(string(res.Stdout)); msg != "" {
			return res, &jobs.Failure{Code: "command_failed", Message: brand.Rewrite(msg)}
		}
		return res, &jobs.Failure{Code: "command_failed", Message: "PHP %s was not installed.", Args: []string{st.verify}}
	}
	return res, nil
}
