package httpapi

// Slice B (Plan 4 Task 3): Configure Server. GET /api/v1/server/settings and the
// ordered settings job behind PUT. Exact call logs; the slice's own fixtures are in
// testdata/fixtures/sys_settings (the PHP "used by" domains).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

func sysP[T any](v T) *T { return &v }

type sysSettingsOpts struct {
	cfg      map[string]string
	fixtures map[string][]fixtureEntry
	deps     func(*Deps)
	ops      bool // sign in as ops (non-root admin) instead of admin
	extra    func(sys *sysAPI, m *sysMux)
}

// sysSettingsHarness serves the server routes on their own registry with the slice's
// fixture directory ahead of the dev fixtures, and signs in. The config cache never
// expires on its own, so a re-read can only come from InvalidateConfig.
func sysSettingsHarness(t *testing.T, o sysSettingsOpts) (*harness, *httptest.Server, *sysAPI) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "fixtures", "sys_settings")
	h := newHarnessWith(t, overrides{Dir: dir, Fixtures: o.fixtures, Config: o.cfg}, func(d *Deps) {
		d.H.ConfigTTL = time.Hour
		if o.deps != nil {
			o.deps(d)
		}
	})
	if o.ops {
		h.loginOps()
	} else {
		h.loginAdmin()
	}
	srv, sys, _ := sysTestServer(h, o.extra)
	return h, srv, sys
}

// sysSettingsCalls is sysCommands with each run of concurrent v-list-web-domains
// reads sorted, so the log is deterministic.
func (h *harness) sysSettingsCalls(from int) []string {
	out := h.sysCommands(from)
	for i := 0; i < len(out); {
		j := i
		for j < len(out) && strings.HasPrefix(out[j], "v-list-web-domains ") {
			j++
		}
		if j > i {
			sort.Strings(out[i:j])
			i = j
			continue
		}
		i++
	}
	return out
}

// sysSettingsWrites drops the reads from a call log.
func sysSettingsWrites(calls []string) []string {
	out := []string{}
	for _, c := range calls {
		if strings.HasPrefix(c, "v-list-") || strings.HasPrefix(c, "v-get-") {
			continue
		}
		out = append(out, c)
	}
	return out
}

type sysSettingsReply struct {
	code   int
	err    APIError
	status JobStatus
	result SettingsResult
	raw    string // the job status as sent
}

// save sends PUT /settings; on 202 it waits for the job (Registry.Done, no polling)
// and reads its status once.
func sysSettingsSave(t *testing.T, h *harness, srv *httptest.Server, sys *sysAPI, req any) sysSettingsReply {
	t.Helper()
	var r sysSettingsReply
	var raw json.RawMessage
	r.code = h.sysDo(srv.URL, "PUT", "/api/v1/server/settings", req, &raw)
	if r.code != 202 {
		_ = json.Unmarshal(raw, &r.err)
		return r
	}
	var ref JobRef
	if err := json.Unmarshal(raw, &ref); err != nil || !jobs.ValidID(ref.JobID) {
		t.Fatalf("job ref %s: %v", raw, err)
	}
	select {
	case <-sys.jobs.Done(ref.JobID):
	case <-time.After(10 * time.Second):
		t.Fatal("settings job still running")
	}
	st, ok := sys.jobs.Get(ref.JobID)
	if !ok {
		t.Fatal("job gone")
	}
	r.status = jobStatusBody(st)
	b, _ := json.Marshal(r.status)
	r.raw = string(b)
	if st.Result != nil {
		if err := json.Unmarshal(st.Result, &r.result); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func sysSettingsGet(t *testing.T, h *harness, srv *httptest.Server) (SettingsView, string) {
	t.Helper()
	var raw json.RawMessage
	if code := h.sysDo(srv.URL, "GET", "/api/v1/server/settings", nil, &raw); code != 200 {
		t.Fatalf("GET settings: %d %s", code, raw)
	}
	var v SettingsView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, string(raw)
}

func sysSettingsAssert(t *testing.T, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got == nil {
		got = []string{}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The reads a full settings load runs, in order (dev config, slice fixtures).
var sysSettingsReadsPHP = []string{
	"v-list-web-templates-backend json",
	"v-list-users json",
	"v-list-web-domains admin json",
	"v-list-web-domains bob json",
	"v-list-web-domains carol json",
	"v-list-web-domains ops json",
	"v-list-web-domains susp json",
	"v-list-default-php json",
}

// Dev config as the form shows it.
func sysSettingsBasePlugins() *SettingsPlugins {
	return &SettingsPlugins{AppInstaller: true, FileManager: true, WebTerminal: true, Firewall: true}
}

func sysSettingsBasePolicies() *SettingsPolicies {
	return &SettingsPolicies{EditDetails: true, EditWebTemplates: true, EditDNSTemplates: true, ViewLogs: true,
		DeleteLogs: true, SyncErrorDocuments: true, SyncSkeleton: true, EnforceSubdomain: true}
}

// sysSettingsEcho is the request a form sends back unchanged.
func sysSettingsEcho(v SettingsView) SettingsRequest {
	versions := []string{}
	for _, p := range v.PHPVersions {
		if p.Installed {
			versions = append(versions, p.Version)
		}
	}
	remote := v.Backup.Remote
	restic := v.Backup.Restic
	pol := v.Policies
	plug := v.Plugins
	req := SettingsRequest{
		Hostname: &v.Hostname, Timezone: &v.Timezone, Theme: &v.Theme, ThemeForAll: &v.ThemeForAll,
		Language: &v.Language, DebugMode: &v.DebugMode, PreviewFeatures: &v.PreviewFeatures,
		UpgradeEmail: &v.UpgradeSendEmail, UpgradeEmailLog: &v.UpgradeSendEmailLog,
		PHPVersions: &versions, DefaultPHP: &v.DefaultPHP,
		WebmailAlias: &v.Mail.WebmailAlias, SMTPRelay: &v.Mail.Relay, SMTPRelayHost: &v.Mail.RelayHost,
		SMTPRelayPort: &v.Mail.RelayPort, SMTPRelayUser: &v.Mail.RelayUser,
		PMAAlias: &v.DB.PMAAlias, PGAAlias: &v.DB.PGAAlias, PMASSO: &v.DB.PMASSO, PMARestrict: &v.DB.PMARestrict,
		BackupLocal: &v.Backup.Local, BackupMode: &v.Backup.Mode, BackupLevel: &v.Backup.Level,
		BackupRemote: &remote, Incremental: &v.Backup.Incremental, Restic: &restic,
		SSLCrt:    &v.SSL.Crt,
		APISystem: &v.Security.APISystem, LegacyAPI: &v.Security.LegacyAPI, APIAllowedIP: &v.Security.APIAllowedIP,
		LoginStyle: &v.Security.LoginStyle, PasswordReset: &v.Security.PasswordReset,
		SessionTimeout: &v.Security.SessionTimeout, CSRFStrictness: &v.Security.CSRFStrictness,
		Policies: &pol, Plugins: &plug,
	}
	if v.CanEditProtection {
		req.ProtectedAdmin = &v.Protection.ProtectedAdmin
		req.HideAdmin = &v.Protection.HideAdmin
		req.HideServices = &v.Protection.HideServices
	}
	return req
}

func TestSysSettingsView(t *testing.T) {
	h, srv, _ := sysSettingsHarness(t, sysSettingsOpts{})
	before := len(h.fixture.Calls())
	v, raw := sysSettingsGet(t, h, srv)
	want := append([]string{
		"v-list-sys-info json",
		"v-get-sys-timezone ",
		"v-get-sys-timezones json",
	}, sysSettingsReadsPHP...)
	want = append(want,
		"v-list-sys-languages json",
		"v-list-sys-themes json",
		"v-list-remote-dns-hosts json",
		"v-list-database-hosts json",
		"v-list-backup-host sftp json",
		"v-list-backup-host-restic json",
		"v-list-sys-hestia-ssl json",
	)
	sysSettingsAssert(t, h.sysSettingsCalls(before), want...)

	if strings.Contains(raw, "PRIVATE KEY") || strings.Contains(raw, "dev-pma-key") {
		t.Fatalf("a secret reached the view: %s", raw)
	}
	if v.Hostname != "server1.acme-hosting.net" || v.Timezone != "UTC" || v.Theme != "dark" || v.Language != "en" ||
		v.ThemeForAll || v.DebugMode || v.Version != "1.10.5" || v.ReleaseBranch != "release" || v.PreviewFeatures ||
		!v.UpgradeSendEmail || v.UpgradeSendEmailLog || v.WebBackend != "php-fpm" || v.DefaultPHP != "8.2" ||
		v.DNSSystem != "bind9" || len(v.DNSCluster) != 0 || !v.CanEditProtection {
		t.Fatalf("view = %+v", v)
	}
	if len(v.PHPVersions) != 12 || v.PHPVersions[0].Version != "5.6" || v.PHPVersions[11].Version != "8.5" {
		t.Fatalf("php = %+v", v.PHPVersions)
	}
	php := map[string]SettingsPHP{}
	for _, p := range v.PHPVersions {
		php[p.Version] = p
	}
	use := func(p SettingsPHP) string {
		var s []string
		for _, u := range p.UsedBy {
			s = append(s, u.User+"/"+u.Domain)
		}
		return strings.Join(s, ",")
	}
	if p := php["8.2"]; !p.Installed || !p.Protected || use(p) != "admin/acme-hosting.net,bob/bob.example.net" {
		t.Errorf("8.2 = %+v", p)
	}
	if p := php["8.3"]; !p.Installed || !p.Protected || use(p) != "admin/shop.acme-hosting.net" {
		t.Errorf("8.3 = %+v", p)
	}
	if p := php["7.4"]; p.Installed || p.Protected || use(p) != "bob/legacy.example.net" {
		t.Errorf("7.4 = %+v", p)
	}
	if p := php["8.4"]; p.Installed || p.Protected || p.UsedBy == nil || len(p.UsedBy) != 0 {
		t.Errorf("8.4 = %+v", p)
	}
	if m := v.Mail; m.System != "exim4" || m.WebmailAlias != "webmail" || m.WebmailSystem != "roundcube,snappymail" || m.Relay {
		t.Errorf("mail = %+v", m)
	}
	if d := v.DB; !d.MySQL || !d.PgSQL || !d.MySQLLocal || len(d.Hosts) != 2 || d.Hosts[0] != (SettingsDBHost{Type: "mysql", Host: "localhost", MaxDB: "500", Current: "2"}) ||
		d.PMAAlias != "phpmyadmin" || d.PGAAlias != "phppgadmin" || !d.PMASSO || d.PMARestrict {
		t.Errorf("db = %+v", d)
	}
	if b := v.Backup; !b.Local || b.Mode != "zstd" || b.Level != "4" || b.Directory != "/backup" || !b.Incremental ||
		b.Remote != (SettingsRemote{Type: "sftp", Host: "backup.acme-hosting.net", Port: "22", Username: "backup", Path: "/srv/backups/server1"}) ||
		b.Restic.Repo != "sftp:restic@backup.acme-hosting.net:/srv/restic/server1" || b.Restic.KeepYearly != "1" {
		t.Errorf("backup = %+v", b)
	}
	if s := v.SSL; !strings.HasPrefix(s.Crt, "-----BEGIN CERTIFICATE-----") || s.Subject != "server1.acme-hosting.net" || s.PubKey != "256 bit" {
		t.Errorf("ssl = %+v", s)
	}
	if s := v.Security; s.APISystem != "2" || s.LegacyAPI || s.APIAllowedIP != "allow-all" || s.LoginStyle != "default" ||
		!s.PasswordReset || s.SessionTimeout != "60" || s.CSRFStrictness != "1" {
		t.Errorf("security = %+v", s)
	}
	if *sysSettingsBasePolicies() != v.Policies || *sysSettingsBasePlugins() != v.Plugins {
		t.Errorf("policies %+v plugins %+v", v.Policies, v.Plugins)
	}
	if len(v.Options.Timezones) != 10 || len(v.Options.Themes) != 4 || v.Options.Languages[0] != "en" {
		t.Errorf("options = %+v", v.Options)
	}
}

func TestSysSettingsViewNonRootAndNoSystems(t *testing.T) {
	h, srv, _ := sysSettingsHarness(t, sysSettingsOpts{ops: true, cfg: map[string]string{
		"WEB_BACKEND": "", "MAIL_SYSTEM": "", "DB_SYSTEM": "", "DNS_SYSTEM": "", "BACKUP_SYSTEM": "local",
		"BACKUP_INCREMENTAL": "no", "BACKUP_MODE": "", "BACKUP_GZIP": "",
	}})
	before := len(h.fixture.Calls())
	v, _ := sysSettingsGet(t, h, srv)
	sysSettingsAssert(t, h.sysSettingsCalls(before),
		"v-list-sys-info json",
		"v-get-sys-timezone ",
		"v-get-sys-timezones json",
		"v-list-sys-languages json",
		"v-list-sys-themes json",
		"v-list-sys-hestia-ssl json",
	)
	if v.CanEditProtection || len(v.PHPVersions) != 0 || v.Mail != (SettingsMail{}) || v.DB.System != "" || len(v.DB.Hosts) != 0 ||
		v.Backup.Remote != (SettingsRemote{}) || v.Backup.Mode != "gzip" || v.Backup.Level != "5" ||
		v.Backup.Restic != (SettingsRestic{Snapshots: "30", KeepDaily: "-1", KeepWeekly: "-1", KeepMonthly: "-1", KeepYearly: "-1"}) {
		t.Fatalf("view = %+v", v)
	}
}

func TestSysSettingsSaveUnchangedRunsNothing(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	v, _ := sysSettingsGet(t, h, srv)
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, sysSettingsEcho(v))
	if r.code != 202 || r.status.State != "done" || len(r.status.Steps) != 0 || len(r.result.Applied) != 0 || r.result.Refresh {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, sysSettingsWrites(h.sysSettingsCalls(before)))
}

func TestSysSettingsEachStepAlone(t *testing.T) {
	plugins := func(f func(p *SettingsPlugins)) *SettingsPlugins { p := sysSettingsBasePlugins(); f(p); return p }
	policies := func(f func(p *SettingsPolicies)) *SettingsPolicies { p := sysSettingsBasePolicies(); f(p); return p }
	cfgv := "v-change-sys-config-value "
	cases := []struct {
		name     string
		cfg      map[string]string
		fixtures map[string][]fixtureEntry
		req      SettingsRequest
		want     []string
	}{
		{name: "hostname", req: SettingsRequest{Hostname: sysP("server2.acme-hosting.net")},
			want: []string{"v-list-sys-info json", "v-change-sys-hostname server2.acme-hosting.net"}},
		{name: "php add", req: SettingsRequest{PHPVersions: &[]string{"8.2", "8.3", "8.4"}},
			fixtures: map[string][]fixtureEntry{"v-list-sys-php": {{Args: []string{"json"}, Stdout: `["8.2","8.3","8.4"]`}}},
			want:     append(slices.Clone(sysSettingsReadsPHP), "v-add-web-php 8.4", "v-list-sys-php json")},
		{name: "php remove", req: SettingsRequest{PHPVersions: &[]string{"8.2", "8.3"}},
			fixtures: map[string][]fixtureEntry{"v-list-web-templates-backend": {{Args: []string{"json"}, Stdout: `["default","PHP-7_3","PHP-8_2","PHP-8_3"]`}}},
			want:     append(slices.Clone(sysSettingsReadsPHP), "v-delete-web-php 7.3")},
		{name: "default php", req: SettingsRequest{DefaultPHP: sysP("8.3")},
			want: append(slices.Clone(sysSettingsReadsPHP), "v-change-sys-php 8.3")},
		{name: "timezone", req: SettingsRequest{Timezone: sysP("Europe/Berlin")},
			want: []string{"v-get-sys-timezone ", "v-get-sys-timezones json", "v-change-sys-timezone Europe/Berlin"}},
		{name: "language", req: SettingsRequest{Language: sysP("de")},
			want: []string{"v-list-sys-languages json", "v-change-sys-language de"}},
		{name: "theme", req: SettingsRequest{Theme: sysP("flat")},
			want: []string{"v-list-sys-themes json", cfgv + "THEME flat"}},
		{name: "debug", req: SettingsRequest{DebugMode: sysP(true)}, want: []string{cfgv + "DEBUG_MODE true"}},
		{name: "app installer", req: SettingsRequest{Plugins: plugins(func(p *SettingsPlugins) { p.AppInstaller = false })},
			want: []string{cfgv + "PLUGIN_APP_INSTALLER false"}},
		{name: "preview", req: SettingsRequest{PreviewFeatures: sysP(true)}, want: []string{cfgv + "POLICY_SYSTEM_ENABLE_BACON true"}},
		{name: "file manager", req: SettingsRequest{Plugins: plugins(func(p *SettingsPlugins) { p.FileManager = false })},
			want: []string{"v-delete-sys-filemanager "}},
		{name: "web terminal", req: SettingsRequest{Plugins: plugins(func(p *SettingsPlugins) { p.WebTerminal = false })},
			want: []string{"v-delete-sys-web-terminal "}},
		{name: "pma sso", req: SettingsRequest{PMASSO: sysP(false)},
			want: []string{"v-list-database-hosts json", "v-delete-sys-pma-sso quiet"}},
		{name: "pma restrict", req: SettingsRequest{PMARestrict: sysP(true)},
			want: []string{"v-list-database-hosts json", "v-add-sys-pma-restrict quiet"}},
		{name: "quota", req: SettingsRequest{Plugins: plugins(func(p *SettingsPlugins) { p.DiskQuota = true })},
			want: []string{"v-add-sys-quota "}},
		{name: "cgroups", req: SettingsRequest{Plugins: plugins(func(p *SettingsPlugins) { p.ResourcesLimit = true })},
			want: []string{"v-add-sys-cgroups "}},
		{name: "firewall off", req: SettingsRequest{Plugins: plugins(func(p *SettingsPlugins) { p.Firewall = false })},
			want: []string{"v-delete-sys-firewall "}},
		{name: "webmail alias", req: SettingsRequest{WebmailAlias: sysP("mail")}, want: []string{"v-change-sys-webmail mail"}},
		{name: "relay off", cfg: map[string]string{"SMTP_RELAY": "true", "SMTP_RELAY_HOST": "smtp.example.net"},
			req: SettingsRequest{SMTPRelay: sysP(false)}, want: []string{"v-delete-sys-smtp-relay "}},
		{name: "pma alias", req: SettingsRequest{PMAAlias: sysP("pma2")},
			want: []string{"v-list-database-hosts json", "v-change-sys-db-alias pma pma2"}},
		{name: "pga alias", req: SettingsRequest{PGAAlias: sysP("pga2")},
			want: []string{"v-list-database-hosts json", "v-change-sys-db-alias pga pga2"}},
		{name: "upgrade email", req: SettingsRequest{UpgradeEmail: sysP(false)}, want: []string{cfgv + "UPGRADE_SEND_EMAIL false"}},
		{name: "upgrade log", req: SettingsRequest{UpgradeEmailLog: sysP(true)}, want: []string{cfgv + "UPGRADE_SEND_EMAIL_LOG true"}},
		{name: "local backup", req: SettingsRequest{BackupLocal: sysP(false)}, want: []string{"v-delete-backup-host local"}},
		{name: "backup level", req: SettingsRequest{BackupLevel: sysP("7")}, want: []string{cfgv + "BACKUP_GZIP 7"}},
		{name: "backup mode gzip", req: SettingsRequest{BackupMode: sysP("gzip")},
			want: []string{cfgv + "BACKUP_GZIP 9", cfgv + "BACKUP_MODE gzip"}},
		{name: "remote off", req: SettingsRequest{BackupRemote: &SettingsRemote{}},
			want: []string{"v-list-backup-host sftp json", "v-delete-backup-host sftp"}},
		{name: "restic off", req: SettingsRequest{Incremental: sysP(false)},
			want: []string{"v-list-backup-host-restic json", "v-delete-backup-host-restic "}},
		{name: "session timeout", req: SettingsRequest{SessionTimeout: sysP("30")}, want: []string{cfgv + "INACTIVE_SESSION_TIMEOUT 30"}},
		{name: "csrf", req: SettingsRequest{CSRFStrictness: sysP("2")}, want: []string{cfgv + "POLICY_CSRF_STRICTNESS 2"}},
		{name: "subdomain ownership", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.EnforceSubdomain = false })},
			want: []string{cfgv + "ENFORCE_SUBDOMAIN_OWNERSHIP no"}},
		{name: "edit details", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.EditDetails = false })},
			want: []string{cfgv + "POLICY_USER_EDIT_DETAILS no"}},
		{name: "edit web templates", cfg: map[string]string{"POLICY_USER_EDIT_WEB_TEMPLATES": "no"},
			req:  SettingsRequest{Policies: policies(func(p *SettingsPolicies) {})},
			want: []string{cfgv + "POLICY_USER_EDIT_WEB_TEMPLATES yes"}},
		{name: "edit dns templates", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.EditDNSTemplates = false })},
			want: []string{cfgv + "POLICY_USER_EDIT_DNS_TEMPLATES no"}},
		{name: "api system", req: SettingsRequest{APISystem: sysP("1")}, want: []string{cfgv + "API_SYSTEM 1"}},
		{name: "legacy api", req: SettingsRequest{LegacyAPI: sysP(true)}, want: []string{cfgv + "API yes"}},
		{name: "api allowed ip", req: SettingsRequest{APIAllowedIP: sysP(" 127.0.0.1 \r\n\n::1\n")},
			want: []string{cfgv + "API_ALLOWED_IP 127.0.0.1,::1"}},
		{name: "view logs", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.ViewLogs = false })},
			want: []string{cfgv + "POLICY_USER_VIEW_LOGS no"}},
		{name: "delete logs", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.DeleteLogs = false })},
			want: []string{cfgv + "POLICY_USER_DELETE_LOGS no"}},
		{name: "password reset", req: SettingsRequest{PasswordReset: sysP(false)}, want: []string{cfgv + "POLICY_SYSTEM_PASSWORD_RESET no"}},
		{name: "protected admin", req: SettingsRequest{ProtectedAdmin: sysP(true)}, want: []string{cfgv + "POLICY_SYSTEM_PROTECTED_ADMIN yes"}},
		{name: "view suspended", cfg: map[string]string{"POLICY_SYSTEM_ENABLE_BACON": "true"},
			req:  SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.ViewSuspended = true })},
			want: []string{cfgv + "POLICY_USER_VIEW_SUSPENDED yes"}},
		{name: "theme for all", req: SettingsRequest{ThemeForAll: sysP(true)}, want: []string{cfgv + "POLICY_USER_CHANGE_THEME no"}},
		{name: "hide admin", req: SettingsRequest{HideAdmin: sysP(true)}, want: []string{cfgv + "POLICY_SYSTEM_HIDE_ADMIN yes"}},
		{name: "hide services", req: SettingsRequest{HideServices: sysP(true)}, want: []string{cfgv + "POLICY_SYSTEM_HIDE_SERVICES yes"}},
		{name: "backup suspended", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.BackupSuspendedUsers = true })},
			want: []string{cfgv + "POLICY_BACKUP_SUSPENDED_USERS yes"}},
		{name: "sync error documents", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.SyncErrorDocuments = false })},
			want: []string{cfgv + "POLICY_SYNC_ERROR_DOCUMENTS no"}},
		{name: "sync skeleton", req: SettingsRequest{Policies: policies(func(p *SettingsPolicies) { p.SyncSkeleton = false })},
			want: []string{cfgv + "POLICY_SYNC_SKELETON no"}},
		{name: "login style", req: SettingsRequest{LoginStyle: sysP("old")}, want: []string{cfgv + "LOGIN_STYLE old"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{cfg: c.cfg, fixtures: c.fixtures})
			before := len(h.fixture.Calls())
			r := sysSettingsSave(t, h, srv, sys, c.req)
			if r.code != 202 || r.status.State != "done" || len(r.result.Applied) != len(sysSettingsWrites(c.want)) {
				t.Fatalf("reply = %+v", r)
			}
			sysSettingsAssert(t, h.sysSettingsCalls(before), c.want...)
		})
	}
}

// A request that changes every section, from a config where each change is real.
func TestSysSettingsFullSaveInPHPOrder(t *testing.T) {
	tmp := t.TempDir()
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{
		cfg: map[string]string{
			"PHPMYADMIN_KEY": "", "BACKUP_SYSTEM": "sftp", "BACKUP_INCREMENTAL": "no", "FIREWALL_SYSTEM": "",
			"POLICY_SYSTEM_ENABLE_BACON": "true", "SMTP_RELAY": "false",
		},
		fixtures: map[string][]fixtureEntry{
			"v-list-sys-php":               {{Args: []string{"json"}, Stdout: `["8.2","8.3","8.4"]`}},
			"v-list-web-templates-backend": {{Args: []string{"json"}, Stdout: `["default","PHP-7_3","PHP-8_2","PHP-8_3"]`}},
		},
		deps: func(d *Deps) { d.TmpDir = tmp },
		extra: func(sys *sysAPI, m *sysMux) {
			sys.firewallEnableCheck = func(context.Context, *http.Request) error { return nil }
		},
	})
	req := SettingsRequest{
		Hostname: sysP("server2.acme-hosting.net"), Timezone: sysP("Europe/Berlin"), Theme: sysP("flat"),
		ThemeForAll: sysP(true), Language: sysP("de"), LanguageForAll: true, DebugMode: sysP(true),
		PreviewFeatures: sysP(true), UpgradeEmail: sysP(false), UpgradeEmailLog: sysP(true),
		PHPVersions: &[]string{"8.2", "8.3", "8.4"}, DefaultPHP: sysP("8.4"),
		WebmailAlias: sysP("mail"), SMTPRelay: sysP(true), SMTPRelayHost: sysP("smtp.example.net"),
		SMTPRelayPort: sysP("2525"), SMTPRelayUser: sysP("relay"), SMTPRelayPassword: "RelayPw1",
		PMAAlias: sysP("pma2"), PGAAlias: sysP("pga2"), PMASSO: sysP(true), PMARestrict: sysP(true),
		MySQLPassword: "RootPw1",
		BackupLocal:   sysP(true), BackupMode: sysP("zstd"), BackupLevel: sysP("7"),
		BackupRemote:   &SettingsRemote{Type: "sftp", Host: "backup2.acme-hosting.net", Port: "2222", Username: "backup", Path: "/srv/b"},
		BackupPassword: "BackupPw1",
		Incremental:    sysP(true),
		Restic:         &SettingsRestic{Repo: "/srv/restic", Snapshots: "5", KeepDaily: "1", KeepWeekly: "2", KeepMonthly: "3", KeepYearly: "4"},
		SSLCrt:         sysP("-----BEGIN CERTIFICATE-----\nNEW\n-----END CERTIFICATE-----"),
		SSLKey:         "-----BEGIN PRIVATE KEY-----\nNEWKEY\n-----END PRIVATE KEY-----",
		APISystem:      sysP("1"), LegacyAPI: sysP(true), APIAllowedIP: sysP("127.0.0.1"),
		LoginStyle: sysP("old"), PasswordReset: sysP(false), SessionTimeout: sysP("30"), CSRFStrictness: sysP("2"),
		ProtectedAdmin: sysP(true), HideAdmin: sysP(true), HideServices: sysP(true),
		Policies: &SettingsPolicies{EditDetails: false, EditWebTemplates: false, EditDNSTemplates: false, ViewLogs: false,
			DeleteLogs: false, ViewSuspended: true, BackupSuspendedUsers: true, SyncErrorDocuments: false,
			SyncSkeleton: false, EnforceSubdomain: false},
		Plugins: &SettingsPlugins{AppInstaller: false, FileManager: false, WebTerminal: false, ResourcesLimit: true,
			DiskQuota: true, Firewall: true},
	}
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, req)
	if r.code != 202 || r.status.State != "done" || !r.result.Refresh {
		t.Fatalf("reply = %+v", r)
	}
	calls := sysSettingsWrites(h.sysSettingsCalls(before))
	// Secret paths differ per run: check each is a private temp path, then name it.
	for i, c := range calls {
		for _, f := range strings.Fields(c) {
			if strings.HasPrefix(f, tmp+"/") {
				calls[i] = strings.Replace(c, f, "<tmp>", 1)
			}
		}
	}
	cfgv := "v-change-sys-config-value "
	sysSettingsAssert(t, calls,
		"v-change-sys-hostname server2.acme-hosting.net",
		"v-delete-web-php 7.3",
		"v-add-web-php 8.4",
		"v-change-sys-php 8.4",
		"v-change-sys-timezone Europe/Berlin",
		"v-change-sys-language de yes",
		cfgv+"THEME flat",
		cfgv+"DEBUG_MODE true",
		cfgv+"PLUGIN_APP_INSTALLER false",
		"v-delete-sys-filemanager ",
		"v-delete-sys-web-terminal ",
		"v-add-sys-pma-sso quiet",
		"v-add-sys-pma-restrict quiet",
		"v-add-sys-quota ",
		"v-add-sys-cgroups ",
		"v-add-sys-firewall ",
		"v-change-database-host-password mysql localhost root <tmp>",
		"v-change-sys-webmail mail",
		"v-add-sys-smtp-relay smtp.example.net relay <tmp> 2525",
		"v-change-sys-db-alias pma pma2",
		"v-change-sys-db-alias pga pga2",
		cfgv+"UPGRADE_SEND_EMAIL false",
		cfgv+"UPGRADE_SEND_EMAIL_LOG true",
		"v-add-backup-host local",
		cfgv+"BACKUP_GZIP 7",
		"v-add-backup-host sftp backup2.acme-hosting.net backup <tmp> /srv/b 2222",
		"v-add-backup-host-restic /srv/restic 5 1 2 3 4",
		cfgv+"INACTIVE_SESSION_TIMEOUT 30",
		cfgv+"POLICY_CSRF_STRICTNESS 2",
		cfgv+"ENFORCE_SUBDOMAIN_OWNERSHIP no",
		cfgv+"POLICY_USER_EDIT_DETAILS no",
		cfgv+"POLICY_USER_EDIT_WEB_TEMPLATES no",
		cfgv+"POLICY_USER_EDIT_DNS_TEMPLATES no",
		cfgv+"API_SYSTEM 1",
		cfgv+"API yes",
		cfgv+"API_ALLOWED_IP 127.0.0.1",
		cfgv+"POLICY_USER_VIEW_LOGS no",
		cfgv+"POLICY_USER_DELETE_LOGS no",
		cfgv+"POLICY_SYSTEM_PASSWORD_RESET no",
		cfgv+"POLICY_SYSTEM_PROTECTED_ADMIN yes",
		cfgv+"POLICY_USER_VIEW_SUSPENDED yes",
		cfgv+"POLICY_USER_CHANGE_THEME no",
		cfgv+"POLICY_SYSTEM_HIDE_ADMIN yes",
		cfgv+"POLICY_SYSTEM_HIDE_SERVICES yes",
		cfgv+"POLICY_BACKUP_SUSPENDED_USERS yes",
		cfgv+"POLICY_SYNC_ERROR_DOCUMENTS no",
		cfgv+"POLICY_SYNC_SKELETON no",
		cfgv+"LOGIN_STYLE old",
		"v-change-sys-hestia-ssl <tmp>",
	)
	if len(r.result.Applied) != len(calls) || !slices.Equal(r.result.Applied, r.status.Steps) {
		t.Fatalf("applied %q, steps %q", r.result.Applied, r.status.Steps)
	}
	for _, secret := range []string{"RelayPw1", "RootPw1", "BackupPw1", "NEWKEY", "PRIVATE KEY"} {
		if strings.Contains(strings.Join(h.sysCommands(before), "\n"), secret) || strings.Contains(r.raw, secret) {
			t.Fatalf("secret %q leaked", secret)
		}
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestSysSettingsWithoutPgsql(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{
		cfg: map[string]string{"DB_SYSTEM": "mysql"},
		fixtures: map[string][]fixtureEntry{"v-list-database-hosts": {{Args: []string{"json"},
			Stdout: `[{"HOST":"localhost","PORT":"3306","TYPE":"mysql","MAX_DB":"500","U_DB_BASES":"2"}]`}}},
	})
	before := len(h.fixture.Calls())
	// The UI leaves pgaAlias out; a stray one is still ignored, never sent as ''.
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PMAAlias: sysP("pma2"), PGAAlias: sysP(""), LoginStyle: sysP("old")})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before),
		"v-list-database-hosts json",
		"v-change-sys-db-alias pma pma2",
		"v-change-sys-config-value LOGIN_STYLE old")
}

func TestSysSettingsFailurePartWay(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{fixtures: map[string][]fixtureEntry{
		"v-change-sys-config-value": {
			{Args: []string{"DEBUG_MODE", "*"}, Stdout: "Error: boom\n", Exit: 2},
			{Args: []string{"*", "*"}},
		},
	}})
	before := len(h.fixture.Calls())
	configReads := func() int {
		n := 0
		for _, c := range h.fixture.Calls() {
			if c.Cmd == "v-list-sys-config" {
				n++
			}
		}
		return n
	}
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{
		Hostname: sysP("server2.acme-hosting.net"), Timezone: sysP("Europe/Berlin"), Language: sysP("de"),
		Theme: sysP("flat"), DebugMode: sysP(true), LoginStyle: sysP("old"),
	})
	want := []string{"Change hostname", "Change time zone", "Change language", "Change theme", "Change debug mode"}
	if r.code != 202 || r.status.State != "failed" || !slices.Equal(r.status.Steps, want) || r.status.Error == nil ||
		r.status.Error.Message != "Error: boom" || r.status.Result != nil {
		t.Fatalf("reply = %+v", r)
	}
	// applied = the steps without the failing one.
	if applied := r.status.Steps[:len(r.status.Steps)-1]; len(applied) != 4 || applied[3] != "Change theme" {
		t.Fatalf("applied = %q", applied)
	}
	sysSettingsAssert(t, sysSettingsWrites(h.sysSettingsCalls(before)),
		"v-change-sys-hostname server2.acme-hosting.net",
		"v-change-sys-timezone Europe/Berlin",
		"v-change-sys-language de",
		"v-change-sys-config-value THEME flat",
		"v-change-sys-config-value DEBUG_MODE true")
	// The config cache was dropped: the next read runs v-list-sys-config again.
	n := configReads()
	if _, err := sys.H.Config(context.Background()); err != nil || configReads() != n+1 {
		t.Fatalf("config not invalidated after a failed job: %d -> %d, %v", n, configReads(), err)
	}
}

func TestSysSettingsInvalidatesConfigOnSuccess(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{LoginStyle: sysP("old")})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	n := 0
	count := func() int {
		n = 0
		for _, c := range h.fixture.Calls() {
			if c.Cmd == "v-list-sys-config" {
				n++
			}
		}
		return n
	}
	was := count()
	if _, err := sys.H.Config(context.Background()); err != nil || count() != was+1 {
		t.Fatalf("config not invalidated: %d -> %d", was, n)
	}
	// Without a job the cache holds (ConfigTTL is an hour here).
	was = count()
	if _, err := sys.H.Config(context.Background()); err != nil || count() != was {
		t.Fatalf("config re-read without a change")
	}
}

func TestSysSettingsPHPAddVerifies(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{fixtures: map[string][]fixtureEntry{
		"v-add-web-php": {{Args: []string{"*"}, Stdout: "ERROR: Installation failed, please run the following command manually for debugging:\napt-get install php8.4-fpm\n"}},
	}})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PHPVersions: &[]string{"8.2", "8.3", "8.4"}, LoginStyle: sysP("old")})
	if r.code != 202 || r.status.State != "failed" || !slices.Equal(r.status.Steps, []string{"Install PHP 8.4"}) ||
		r.status.Error == nil || !strings.HasPrefix(r.status.Error.Message, "ERROR: Installation failed") {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, sysSettingsWrites(h.sysSettingsCalls(before)), "v-add-web-php 8.4")
	if calls := h.sysSettingsCalls(before); calls[len(calls)-1] != "v-list-sys-php json" {
		t.Fatalf("no verify: %q", calls)
	}
}

func TestSysSettingsProtectedPHP(t *testing.T) {
	for _, keep := range [][]string{{"8.3"}, {"8.2"}} {
		h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
		before := len(h.fixture.Calls())
		r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PHPVersions: &keep})
		removed := "8.2"
		if keep[0] == "8.2" {
			removed = "8.3"
		}
		if r.code != 409 || r.err.Error.Code != "in_use" || r.err.Error.Message != "PHP %s is in use and can't be removed." ||
			!slices.Equal(r.err.Error.Args, []string{removed}) {
			t.Fatalf("keep %v: %+v", keep, r)
		}
		sysSettingsAssert(t, h.sysSettingsCalls(before), sysSettingsReadsPHP...)
	}
	// The system default is protected even when no domain uses it.
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{fixtures: map[string][]fixtureEntry{
		"v-list-web-templates-backend": {{Args: []string{"json"}, Stdout: `["PHP-8_2","PHP-8_3","PHP-8_4"]`}},
		"v-list-default-php":           {{Args: []string{"json"}, Stdout: `["8.4"]`}},
	}})
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PHPVersions: &[]string{"8.2", "8.3"}})
	if r.code != 409 || r.err.Error.Code != "in_use" || !slices.Equal(r.err.Error.Args, []string{"8.4"}) {
		t.Fatalf("default: %+v", r)
	}
}

func TestSysSettingsRemoteBackupPasswordFile(t *testing.T) {
	tmp := t.TempDir()
	var path, content string
	var mode os.FileMode
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{deps: func(d *Deps) {
		d.TmpDir = tmp
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-add-backup-host" {
				return
			}
			path = args[3]
			if fi, err := os.Stat(path); err == nil {
				mode = fi.Mode().Perm()
			}
			b, _ := os.ReadFile(path)
			content = string(b)
		}}
	}})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{
		BackupRemote:   &SettingsRemote{Type: "sftp", Host: "backup2.acme-hosting.net", Port: "22", Username: "backup", Path: "/srv/backups/server1"},
		BackupPassword: "S3cret-Pw",
	})
	if r.code != 202 || r.status.State != "done" || !slices.Equal(r.result.Applied, []string{"Add remote backup host"}) {
		t.Fatalf("reply = %+v", r)
	}
	if !strings.HasPrefix(path, tmp+"/") || mode != 0o600 || content != "S3cret-Pw\n" {
		t.Fatalf("password file %q mode %v content %q", path, mode, content)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("password file still there: %v", err)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before),
		"v-list-backup-host sftp json",
		"v-add-backup-host sftp backup2.acme-hosting.net backup "+path+" /srv/backups/server1 22")
	if strings.Contains(strings.Join(h.sysCommands(before), "\n"), "S3cret-Pw") || strings.Contains(r.raw, "S3cret-Pw") {
		t.Fatal("the password leaked")
	}
}

func TestSysSettingsRemoteBackupNeedsPassword(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{
		BackupRemote: &SettingsRemote{Type: "sftp", Host: "backup2.acme-hosting.net", Port: "22", Username: "backup", Path: "/srv/backups/server1"},
	})
	if r.code != 400 || r.err.Error.Message != `Field "%s" can not be blank.` || !slices.Equal(r.err.Error.Args, []string{"Password"}) {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-backup-host sftp json")
}

// D8: the password field takes no private key. A pasted key, with its newlines or
// flattened to one line, is refused with its own message before any command runs.
func TestSysSettingsRemoteBackupRefusesPastedKey(t *testing.T) {
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----"
	for name, pw := range map[string]string{
		"multi-line": key,
		"one line":   strings.ReplaceAll(key, "\n", ""),
		"rsa":        "-----BEGIN RSA PRIVATE KEY----- MIIEpAIBAAKCAQEA -----END RSA PRIVATE KEY-----",
		"body only":  " b3BlbnNzaC1rZXktdjEAAAAA -----END OPENSSH PRIVATE KEY-----",
	} {
		for _, typ := range []string{"sftp", "ftp"} {
			h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
			before := len(h.fixture.Calls())
			r := sysSettingsSave(t, h, srv, sys, SettingsRequest{
				BackupRemote:   &SettingsRemote{Type: typ, Host: "backup2.acme-hosting.net", Port: "22", Username: "backup", Path: "/srv/backups/server1"},
				BackupPassword: pw,
			})
			if r.code != 400 || r.err.Error.Code != "invalid" || r.err.Error.Message != sysPastedKeyMsg {
				t.Fatalf("%s %s: reply = %+v", name, typ, r)
			}
			sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-backup-host sftp json") // the current host
		}
	}
}

func TestSysSettingsRemoteFtpToRclone(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{
		cfg: map[string]string{"BACKUP_SYSTEM": "local,ftp"},
		fixtures: map[string][]fixtureEntry{"v-list-backup-host": {{Args: []string{"ftp", "json"},
			Stdout: `{"ftp":{"HOST":"ftp.example.net","USERNAME":"u","PORT":"21","TYPE":"ftp","BPATH":"/backup"}}`}}},
	})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{BackupRemote: &SettingsRemote{Type: "rclone", Host: "gdrive", Path: "/backups"}})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before),
		"v-list-backup-host ftp json",
		"v-delete-backup-host ftp",
		"v-add-backup-host rclone gdrive   /backups")
}

func TestSysSettingsRelay(t *testing.T) {
	// Off while SMTP_RELAY is the string "false": nothing runs.
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{SMTPRelay: sysP(false), SMTPRelayHost: sysP(""), SMTPRelayPort: sysP(""), SMTPRelayUser: sysP("")})
	if r.code != 202 || r.status.State != "done" || len(r.status.Steps) != 0 {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before))

	// On with a username and no password: 400, nothing runs.
	before = len(h.fixture.Calls())
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{SMTPRelay: sysP(true), SMTPRelayHost: sysP("smtp.example.net"), SMTPRelayUser: sysP("relay")})
	if r.code != 400 || !slices.Equal(r.err.Error.Args, []string{"Password"}) {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before))

	// A stored relay with a user: a port change needs the password again.
	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: map[string]string{
		"SMTP_RELAY": "true", "SMTP_RELAY_HOST": "smtp.example.net", "SMTP_RELAY_PORT": "587", "SMTP_RELAY_USER": "relay"}})
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{SMTPRelay: sysP(true), SMTPRelayHost: sysP("smtp.example.net"),
		SMTPRelayPort: sysP("2525"), SMTPRelayUser: sysP("relay")})
	if r.code != 400 || !slices.Equal(r.err.Error.Args, []string{"Password"}) {
		t.Fatalf("stored user: %+v", r)
	}
	// Unchanged: nothing runs.
	before = len(h.fixture.Calls())
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{SMTPRelay: sysP(true), SMTPRelayHost: sysP("smtp.example.net"),
		SMTPRelayPort: sysP("587"), SMTPRelayUser: sysP("relay")})
	if r.code != 202 || len(r.status.Steps) != 0 {
		t.Fatalf("unchanged: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before))
}

func TestSysSettingsRelayPasswordFile(t *testing.T) {
	tmp := t.TempDir()
	var content string
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{deps: func(d *Deps) {
		d.TmpDir = tmp
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd == "v-add-sys-smtp-relay" {
				b, _ := os.ReadFile(args[2])
				content = string(b)
			}
		}}
	}})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{SMTPRelay: sysP(true), SMTPRelayHost: sysP("smtp.example.net"),
		SMTPRelayPort: sysP(""), SMTPRelayUser: sysP("relay"), SMTPRelayPassword: "Relay-Pw"})
	calls := h.sysSettingsCalls(before)
	if r.code != 202 || r.status.State != "done" || content != "Relay-Pw\n" || len(calls) != 1 ||
		!strings.HasPrefix(calls[0], "v-add-sys-smtp-relay smtp.example.net relay "+tmp+"/") || !strings.HasSuffix(calls[0], " 587") {
		t.Fatalf("reply %+v calls %q content %q", r, calls, content)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 || strings.Contains(r.raw, "Relay-Pw") {
		t.Fatalf("left %v", left)
	}
}

func TestSysSettingsAPI(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{cfg: map[string]string{"API_SYSTEM": "1", "API_ALLOWED_IP": "127.0.0.1,::1"}})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{APISystem: sysP("2"), LegacyAPI: sysP(false), APIAllowedIP: sysP("127.0.0.1\n::1\n")})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-change-sys-config-value API_SYSTEM 2")

	before = len(h.fixture.Calls())
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{APIAllowedIP: sysP("127.0.0.1\n10.0.0.0/8")})
	if r.code != 400 || r.err.Error.Message != "Invalid IP address: %s" || !slices.Equal(r.err.Error.Args, []string{"10.0.0.0/8"}) {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before))
}

func TestSysSettingsRestic(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	cur := SettingsRestic{Repo: "sftp:restic@backup.acme-hosting.net:/srv/restic/server1", Snapshots: "10",
		KeepDaily: "7", KeepWeekly: "4", KeepMonthly: "6", KeepYearly: "1"}
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Incremental: sysP(true), Restic: &cur})
	if r.code != 202 || len(r.status.Steps) != 0 {
		t.Fatalf("unchanged: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-backup-host-restic json")

	changed := cur
	changed.Snapshots = "20"
	before = len(h.fixture.Calls())
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{Incremental: sysP(true), Restic: &changed})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("changed: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-backup-host-restic json",
		"v-delete-backup-host-restic ",
		"v-add-backup-host-restic sftp:restic@backup.acme-hosting.net:/srv/restic/server1 20 7 4 6 1")

	changed.Repo = ""
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{Incremental: sysP(true), Restic: &changed})
	if r.code != 400 || r.err.Error.Message != "Repository can not be empty" {
		t.Fatalf("empty repo: %+v", r)
	}
}

func TestSysSettingsPreviewOffResetsViewSuspended(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{cfg: map[string]string{
		"POLICY_SYSTEM_ENABLE_BACON": "true", "POLICY_USER_VIEW_SUSPENDED": "yes"}})
	pol := sysSettingsBasePolicies()
	pol.ViewSuspended = true // the form still holds the stored value
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PreviewFeatures: sysP(false), Policies: pol})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before),
		"v-change-sys-config-value POLICY_SYSTEM_ENABLE_BACON false",
		"v-change-sys-config-value POLICY_USER_VIEW_SUSPENDED no")
}

func TestSysSettingsLanguageForAllRunsOnce(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Language: sysP("fr"), LanguageForAll: true})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-sys-languages json", "v-change-sys-language fr yes")
}

func TestSysSettingsValidation(t *testing.T) {
	forbidden := "Invalid value contains forbidden characters"
	timeout := "Inactive session timeout can not lower than 1 minute."
	cases := []struct {
		name  string
		req   SettingsRequest
		msg   string
		reads []string
	}{
		{name: "api system", req: SettingsRequest{APISystem: sysP("3")}},
		{name: "csrf", req: SettingsRequest{CSRFStrictness: sysP("-1")}},
		{name: "login style", req: SettingsRequest{LoginStyle: sysP("old;rm")}},
		{name: "backup mode", req: SettingsRequest{BackupMode: sysP("lz4")}},
		{name: "backup level high", req: SettingsRequest{BackupLevel: sysP("20")}},
		{name: "backup level text", req: SettingsRequest{BackupLevel: sysP("-h")}},
		{name: "theme", req: SettingsRequest{Theme: sysP("../../x")}, reads: []string{"v-list-sys-themes json"}},
		{name: "language", req: SettingsRequest{Language: sysP("xx")}, reads: []string{"v-list-sys-languages json"}},
		{name: "timezone", req: SettingsRequest{Timezone: sysP("Mars/Base")}, reads: []string{"v-get-sys-timezone ", "v-get-sys-timezones json"}},
		{name: "remote type", req: SettingsRequest{BackupRemote: &SettingsRemote{Type: "s3", Host: "x"}}, reads: []string{"v-list-backup-host sftp json"}},
		{name: "php version", req: SettingsRequest{PHPVersions: &[]string{"8.2", "8.3", "4.4"}}, reads: sysSettingsReadsPHP},
		{name: "default php", req: SettingsRequest{DefaultPHP: sysP("8.4")}, reads: sysSettingsReadsPHP},
		{name: "hostname", req: SettingsRequest{Hostname: sysP("-h")}, reads: []string{"v-list-sys-info json"}},
		{name: "timeout zero", req: SettingsRequest{SessionTimeout: sysP("0")}, msg: timeout},
		{name: "timeout text", req: SettingsRequest{SessionTimeout: sysP("abc")}, msg: timeout},
		{name: "ampersand", req: SettingsRequest{WebmailAlias: sysP("web&mail")}, msg: forbidden},
		{name: "ampersand repo", req: SettingsRequest{Incremental: sysP(true), Restic: &SettingsRestic{Repo: "/srv/a&b", Snapshots: "1",
			KeepDaily: "1", KeepWeekly: "1", KeepMonthly: "1", KeepYearly: "1"}}, msg: forbidden, reads: []string{"v-list-backup-host-restic json"}},
		{name: "restic count", req: SettingsRequest{Incremental: sysP(true), Restic: &SettingsRestic{Repo: "/srv/r", Snapshots: "x",
			KeepDaily: "1", KeepWeekly: "1", KeepMonthly: "1", KeepYearly: "1"}}, reads: []string{"v-list-backup-host-restic json"}},
		{name: "relay port", req: SettingsRequest{SMTPRelay: sysP(true), SMTPRelayHost: sysP("smtp.example.net"), SMTPRelayPort: sysP("99999")}},
		{name: "pma restrict without sso", req: SettingsRequest{PMASSO: sysP(false), PMARestrict: sysP(true)}, reads: []string{"v-list-database-hosts json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
			before := len(h.fixture.Calls())
			r := sysSettingsSave(t, h, srv, sys, c.req)
			if r.code != 400 || r.err.Error.Code != "invalid" || (c.msg != "" && r.err.Error.Message != c.msg) {
				t.Fatalf("reply = %+v", r)
			}
			sysSettingsAssert(t, h.sysSettingsCalls(before), c.reads...)
		})
	}
}

func TestSysSettingsProtectionRootOnly(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{ops: true})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{HideServices: sysP(true), LoginStyle: sysP("old")})
	if r.code != 403 || r.err.Error.Code != "forbidden" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before))
	// Echoing the stored values is fine.
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{ProtectedAdmin: sysP(false), HideAdmin: sysP(false), HideServices: sysP(false)})
	if r.code != 202 || len(r.status.Steps) != 0 {
		t.Fatalf("echo: %+v", r)
	}
}

func TestSysSettingsPMAOrder(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{cfg: map[string]string{"PMA_RESTRICT_ACCESS": "yes"}})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PMASSO: sysP(false), PMARestrict: sysP(true)})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-database-hosts json",
		"v-delete-sys-pma-restrict quiet", "v-delete-sys-pma-sso quiet")

	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: map[string]string{"PHPMYADMIN_KEY": ""}})
	before = len(h.fixture.Calls())
	r = sysSettingsSave(t, h, srv, sys, SettingsRequest{PMASSO: sysP(true), PMARestrict: sysP(true)})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("both on: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-database-hosts json",
		"v-add-sys-pma-sso quiet", "v-add-sys-pma-restrict quiet")
}

func TestSysSettingsSSLCertOnly(t *testing.T) {
	tmp := t.TempDir()
	var dir string
	files := map[string]string{}
	modes := map[string]os.FileMode{}
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{deps: func(d *Deps) {
		d.TmpDir = tmp
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-change-sys-hestia-ssl" {
				return
			}
			dir = args[0]
			if fi, err := os.Stat(dir); err == nil {
				modes["."] = fi.Mode().Perm()
			}
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
				files[e.Name()] = string(b)
				if fi, err := e.Info(); err == nil {
					modes[e.Name()] = fi.Mode().Perm()
				}
			}
		}}
	}})
	before := len(h.fixture.Calls())
	crt := "-----BEGIN CERTIFICATE-----\r\nNEWCERT\r\n-----END CERTIFICATE-----\r\n"
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{SSLCrt: &crt, LoginStyle: sysP("old"), Hostname: sysP("server2.acme-hosting.net")})
	if r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before),
		"v-list-sys-info json",
		"v-list-sys-hestia-ssl json",
		"v-change-sys-hostname server2.acme-hosting.net",
		"v-change-sys-config-value LOGIN_STYLE old",
		"v-change-sys-hestia-ssl "+dir)
	if !strings.HasPrefix(dir, tmp+"/") || modes["."] != 0o700 {
		t.Fatalf("dir %q mode %v", dir, modes["."])
	}
	// The core needs both files; the stored key is kept as it was (it never reaches the UI).
	if len(files) != 2 || files["certificate.crt"] != "-----BEGIN CERTIFICATE-----\nNEWCERT\n-----END CERTIFICATE-----\n" ||
		!strings.Contains(files["certificate.key"], "-----BEGIN PRIVATE KEY-----") ||
		modes["certificate.crt"] != 0o600 || modes["certificate.key"] != 0o600 {
		t.Fatalf("files %q modes %v", files, modes)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("ssl dir left: %v", err)
	}
	if strings.Contains(r.raw, "PRIVATE KEY") || strings.Contains(strings.Join(h.sysCommands(before), "\n"), "PRIVATE KEY") {
		t.Fatal("the key leaked")
	}
	if last := r.result.Applied[len(r.result.Applied)-1]; last != "Change panel SSL certificate" {
		t.Fatalf("applied %q", r.result.Applied)
	}
	// The same certificate again (as the form echoes it) runs nothing.
	v, _ := sysSettingsGet(t, h, srv)
	before = len(h.fixture.Calls())
	if r := sysSettingsSave(t, h, srv, sys, SettingsRequest{SSLCrt: &v.SSL.Crt}); r.code != 202 || len(r.status.Steps) != 0 {
		t.Fatalf("unchanged: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-list-sys-hestia-ssl json")
}

func TestSysSettingsQuotaNote(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{fixtures: map[string][]fixtureEntry{
		"v-add-sys-quota": {{Args: []string{}, Stdout: "[ ! ] Note: A system reboot is required to complete quota setup.\n"}},
	}})
	p := sysSettingsBasePlugins()
	p.DiskQuota = true
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Plugins: p})
	if r.code != 202 || r.status.State != "done" ||
		!slices.Equal(r.result.Notes, []string{"[ ! ] Note: A system reboot is required to complete quota setup."}) {
		t.Fatalf("reply = %+v", r)
	}
}

func TestSysSettingsFirewallEnableGuard(t *testing.T) {
	cfg := map[string]string{"FIREWALL_SYSTEM": ""}
	on := sysSettingsBasePlugins()
	// The network routes wire the real guard.
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{cfg: cfg})
	if sys.firewallEnableCheck == nil {
		t.Fatal("networkRoutes left the firewall-enable seam unset")
	}
	// With the seam unset: refuse, nothing runs.
	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: cfg, extra: func(sys *sysAPI, m *sysMux) {
		sys.firewallEnableCheck = nil
	}})
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Plugins: on, LoginStyle: sysP("old")})
	if r.code != 409 || r.err.Error.Code != SysLockoutCode || r.err.Error.Message != SysLockoutFirewallMsg {
		t.Fatalf("nil seam: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before))
	if _, running := sys.jobs.Running(sysJobKey); running {
		t.Fatal("a job started")
	}
	// A guard that refuses: its failure is the reply.
	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: cfg, extra: func(sys *sysAPI, m *sysMux) {
		sys.firewallEnableCheck = func(context.Context, *http.Request) error {
			return &jobs.Failure{Code: SysLockoutCode, Message: "nope"}
		}
	}})
	if r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Plugins: on}); r.code != 409 || r.err.Error.Message != "nope" {
		t.Fatalf("refusing seam: %+v", r)
	}
	// Any other error is a failed command, not a lockout.
	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: cfg, extra: func(sys *sysAPI, m *sysMux) {
		sys.firewallEnableCheck = func(context.Context, *http.Request) error {
			return &runner.CommandError{Cmd: "v-list-firewall", Exit: 3, Output: "Error: no rules\n"}
		}
	}})
	if r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Plugins: on}); r.code != 404 || r.err.Error.Code != "not_found" ||
		r.err.Error.Message != "Error: no rules" {
		t.Fatalf("command error: %+v", r)
	}
	// A guard that passes: the firewall is turned on. The guard takes fwMu itself, so
	// the handler must not hold it.
	var fwFree bool
	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: cfg, extra: func(sys *sysAPI, m *sysMux) {
		sys.firewallEnableCheck = func(context.Context, *http.Request) error {
			if fwFree = sys.fwMu.TryLock(); fwFree {
				sys.fwMu.Unlock()
			}
			return nil
		}
	}})
	before = len(h.fixture.Calls())
	if r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Plugins: on}); r.code != 202 || r.status.State != "done" {
		t.Fatalf("passing seam: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-add-sys-firewall ")
	if !fwFree {
		t.Fatal("fwMu was held while the guard ran")
	}
	// Turning it off needs no guard.
	h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{})
	off := sysSettingsBasePlugins()
	off.Firewall = false
	if r := sysSettingsSave(t, h, srv, sys, SettingsRequest{Plugins: off}); r.code != 202 || r.status.State != "done" {
		t.Fatalf("off: %+v", r)
	}
}

func TestSysSettingsBusy(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	release := make(chan struct{})
	defer close(release)
	if _, err := sys.jobs.Start(context.Background(), jobs.Spec{Key: sysJobKey, Owner: "admin", Title: "x"},
		func(ctx context.Context, j *jobs.Job) (any, error) { <-release; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{LoginStyle: sysP("old")})
	if r.code != 409 || r.err.Error.Code != "busy" {
		t.Fatalf("reply = %+v", r)
	}
}

// The ordered step builder on its own (pure).
func TestSysSettingsPlanCompression(t *testing.T) {
	cases := []struct {
		cfg        map[string]string
		mode, lvl  *string
		wantArgs   [][]string
		wantErrMsg string
	}{
		// Level change under zstd: level only.
		{cfg: map[string]string{"BACKUP_MODE": "zstd", "BACKUP_GZIP": "4"}, lvl: sysP("12"), wantArgs: [][]string{{"BACKUP_GZIP", "12"}}},
		// Level change under gzip is forced to 9.
		{cfg: map[string]string{"BACKUP_MODE": "gzip", "BACKUP_GZIP": "4"}, lvl: sysP("3"), wantArgs: [][]string{{"BACKUP_GZIP", "9"}}},
		// gzip stored with a level other than 9 and nothing sent changed: nothing.
		{cfg: map[string]string{"BACKUP_MODE": "gzip", "BACKUP_GZIP": "4"}, mode: sysP("gzip"), lvl: sysP("4")},
		// Empty config shows gzip and level 5; echoing them runs nothing.
		{cfg: map[string]string{}, mode: sysP("gzip"), lvl: sysP("5")},
		// To zstd with a new level: level, then mode.
		{cfg: map[string]string{"BACKUP_MODE": "gzip", "BACKUP_GZIP": "9"}, mode: sysP("zstd"), lvl: sysP("15"),
			wantArgs: [][]string{{"BACKUP_GZIP", "15"}, {"BACKUP_MODE", "zstd"}}},
		// To gzip while the level is already 9: mode only.
		{cfg: map[string]string{"BACKUP_MODE": "zstd", "BACKUP_GZIP": "9"}, mode: sysP("gzip"),
			wantArgs: [][]string{{"BACKUP_MODE", "gzip"}}},
	}
	for i, c := range cases {
		st := &sysSettingsState{cfg: c.cfg, root: true}
		plan, rej := sysSettingsPlan(st, &SettingsRequest{BackupMode: c.mode, BackupLevel: c.lvl})
		if rej != nil {
			t.Fatalf("%d: %+v", i, rej)
		}
		var got [][]string
		for _, s := range plan.steps {
			if s.cmd != "v-change-sys-config-value" {
				t.Fatalf("%d: %+v", i, s)
			}
			got = append(got, s.args)
		}
		if len(got) != len(c.wantArgs) {
			t.Fatalf("%d: got %q want %q", i, got, c.wantArgs)
		}
		for j := range got {
			if !slices.Equal(got[j], c.wantArgs[j]) {
				t.Fatalf("%d: got %q want %q", i, got, c.wantArgs)
			}
		}
	}
}

func TestSysSettingsPlanTimeouts(t *testing.T) {
	st := &sysSettingsState{cfg: map[string]string{"WEB_BACKEND": "php-fpm", "FILE_MANAGER": "false", "WEB_TERMINAL": "true",
		"DISK_QUOTA": "no", "MAIL_SYSTEM": "exim4", "WEBMAIL_SYSTEM": "roundcube", "WEBMAIL_ALIAS": "webmail"},
		root: true, defaultPHP: "8.2", php: []SettingsPHP{{Version: "8.2", Installed: true, Protected: true}, {Version: "8.3"}}}
	plan, rej := sysSettingsPlan(st, &SettingsRequest{
		PHPVersions: &[]string{"8.2", "8.3"}, DefaultPHP: sysP("8.3"), WebmailAlias: sysP("mail"),
		Plugins: &SettingsPlugins{FileManager: true, WebTerminal: true, DiskQuota: true}, LoginStyle: sysP("old"),
	})
	if rej != nil {
		t.Fatal(rej)
	}
	want := map[string]time.Duration{
		"v-add-web-php": sysTimeoutInstall, "v-change-sys-php": sysTimeoutService, "v-add-sys-filemanager": sysTimeoutInstall,
		"v-add-sys-quota": sysTimeoutInstall, "v-change-sys-webmail": sysTimeoutService, "v-change-sys-config-value": sysTimeoutDefault,
	}
	if len(plan.steps) != len(want) {
		t.Fatalf("steps %+v", plan.steps)
	}
	for _, s := range plan.steps {
		if s.timeout != want[s.cmd] {
			t.Errorf("%s: %v", s.cmd, s.timeout)
		}
	}
	if !plan.refresh || plan.steps[0].verify != "8.3" {
		t.Errorf("plan %+v", plan)
	}
}

// The default PHP version comes from the core; a failed read fails the request
// instead of leaving the default unprotected.
func TestSysSettingsDefaultPHPReadFails(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{fixtures: map[string][]fixtureEntry{
		"v-list-default-php": {{Args: []string{"json"}, Stdout: "Error: boom\n", Exit: 1}},
	}})
	var e APIError
	if code := h.sysDo(srv.URL, "GET", "/api/v1/server/settings", nil, &e); code < 400 || e.Error.Message != "Error: boom" {
		t.Fatalf("GET: %d %+v", code, e)
	}
	before := len(h.fixture.Calls())
	r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PHPVersions: &[]string{"8.2"}})
	if r.code < 400 || r.err.Error.Message != "Error: boom" {
		t.Fatalf("PUT: %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), sysSettingsReadsPHP...)
	// An empty list is not "no default" either.
	h, srv, _ = sysSettingsHarness(t, sysSettingsOpts{fixtures: map[string][]fixtureEntry{
		"v-list-default-php": {{Args: []string{"json"}, Stdout: "[]\n"}},
	}})
	if code := h.sysDo(srv.URL, "GET", "/api/v1/server/settings", nil, &e); code != 500 {
		t.Fatalf("empty: %d %+v", code, e)
	}
}

// Gotcha 19: SSO off with PMA_RESTRICT_ACCESS still yes. The restriction counts as
// off, so a form echo runs nothing (v-delete-sys-pma-restrict would fail).
func TestSysSettingsStalePMARestrict(t *testing.T) {
	stale := map[string]string{"PHPMYADMIN_KEY": "", "PMA_RESTRICT_ACCESS": "yes"}
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{cfg: stale})
	v, _ := sysSettingsGet(t, h, srv)
	if v.DB.PMASSO || v.DB.PMARestrict {
		t.Fatalf("view db = %+v", v.DB)
	}
	before := len(h.fixture.Calls())
	if r := sysSettingsSave(t, h, srv, sys, sysSettingsEcho(v)); r.code != 202 || r.status.State != "done" || len(r.status.Steps) != 0 {
		t.Fatalf("echo: %+v", r)
	}
	sysSettingsAssert(t, sysSettingsWrites(h.sysSettingsCalls(before)))

	// Turning SSO and the restriction on from there clears the stale flag first.
	for _, restrict := range []bool{true, false} {
		h, srv, sys = sysSettingsHarness(t, sysSettingsOpts{cfg: stale})
		before = len(h.fixture.Calls())
		r := sysSettingsSave(t, h, srv, sys, SettingsRequest{PMASSO: sysP(true), PMARestrict: sysP(restrict)})
		if r.code != 202 || r.status.State != "done" {
			t.Fatalf("on %v: %+v", restrict, r)
		}
		want := []string{"v-list-database-hosts json",
			"v-change-sys-config-value PMA_RESTRICT_ACCESS no", "v-add-sys-pma-sso quiet"}
		if restrict {
			want = append(want, "v-add-sys-pma-restrict quiet")
		}
		sysSettingsAssert(t, h.sysSettingsCalls(before), want...)
	}
}

func TestSysSettingsSessionTimeoutCanonical(t *testing.T) {
	h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
	before := len(h.fixture.Calls())
	if r := sysSettingsSave(t, h, srv, sys, SettingsRequest{SessionTimeout: sysP("030")}); r.code != 202 || r.status.State != "done" {
		t.Fatalf("reply = %+v", r)
	}
	sysSettingsAssert(t, h.sysSettingsCalls(before), "v-change-sys-config-value INACTIVE_SESSION_TIMEOUT 30")
}
