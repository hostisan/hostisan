package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

// acctLog is the commands run since mark as "cmd arg arg", without the session
// bookkeeping every request does (config and user reads, login log lines).
func (h *harness) acctMark() int { return len(h.fixture.Calls()) }

func (h *harness) acctLog(from int) []string {
	out := []string{}
	for _, c := range h.fixture.Calls()[from:] {
		line := c.Cmd + " " + strings.Join(c.Args, " ")
		switch {
		case c.Cmd == "v-list-sys-config", c.Cmd == "v-log-user-login", c.Cmd == "v-log-action":
			continue
		case c.Cmd == "v-list-user" && len(c.Args) == 2 && c.Args[1] == "json":
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	return out
}

func acctWant(t *testing.T, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls:\n got  %q\n want %q", got, want)
	}
}

// acctUsersJSON is a v-list-users reply: name -> package, all active users.
func acctUsersFixture(order []string, pkgs map[string]string) map[string][]fixtureEntry {
	var b strings.Builder
	b.WriteString("{")
	for i, n := range order {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `%q:{"NAME":"N","PACKAGE":%q,"ROLE":"user","SUSPENDED":"no","RKEY":"secret"}`, n, pkgs[n])
	}
	b.WriteString("}")
	return map[string][]fixtureEntry{"v-list-users": {{Args: []string{"json"}, Stdout: b.String()}}}
}

func TestAcctUsersList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var raw json.RawMessage
	if code := h.call("GET", "/api/v1/users", nil, &raw); code != 200 {
		t.Fatalf("code %d", code)
	}
	if strings.Contains(strings.ToLower(string(raw)), "rkey") {
		t.Fatalf("reply carries RKEY: %s", raw)
	}
	var l UserList
	_ = json.Unmarshal(raw, &l)
	var names []string
	for _, u := range l.Users {
		names = append(names, u.USER)
	}
	if !reflect.DeepEqual(names, []string{"admin", "bob", "susp", "ops", "carol"}) || l.RootUser != "admin" || l.Self != "admin" {
		t.Fatalf("list = %+v", l)
	}
}

func TestAcctUsersListHideAdmin(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_HIDE_ADMIN": "yes"}})
	h.loginOps()
	var l UserList
	if code := h.call("GET", "/api/v1/users", nil, &l); code != 200 {
		t.Fatalf("code %d", code)
	}
	var names []string
	for _, u := range l.Users {
		names = append(names, u.USER)
	}
	if !reflect.DeepEqual(names, []string{"bob", "susp", "ops", "carol"}) || l.Self != "ops" {
		t.Fatalf("list = %+v", l)
	}
	// Root still sees itself.
	h2 := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_HIDE_ADMIN": "yes"}})
	h2.loginAdmin()
	h2.call("GET", "/api/v1/users", nil, &l)
	if len(l.Users) != 5 {
		t.Fatalf("root list = %d", len(l.Users))
	}
}

func TestAcctUsersListForbiddenForUsers(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	if code := h.call("GET", "/api/v1/users", nil, nil); code != 403 {
		t.Fatalf("code %d", code)
	}
}

func TestAcctUserProtected(t *testing.T) {
	for _, who := range []string{"admin", "ops"} {
		h := newHarness(t)
		if who == "admin" {
			h.loginAdmin()
		} else {
			h.loginOps()
		}
		m := h.acctMark()
		targets := []string{"admin"}
		if who == "ops" {
			targets = append(targets, "ops")
		}
		for _, target := range targets {
			for _, rt := range [][2]string{{"DELETE", ""}, {"POST", "/suspend"}, {"POST", "/unsuspend"}} {
				var r APIError
				if code := h.call(rt[0], "/api/v1/users/"+target+rt[1], nil, &r); code != 403 || r.Error.Code != "forbidden" {
					t.Fatalf("%s as %s: %s%s: %d %+v", who, who, target, rt[1], code, r)
				}
			}
			for _, a := range []string{"delete", "suspend", "unsuspend"} {
				if code := h.call("POST", "/api/v1/users/bulk", BulkRequest{Action: a, Names: []string{target}}, nil); code != 403 {
					t.Fatalf("bulk %s %s: %d", a, target, code)
				}
			}
		}
		acctWant(t, h.acctLog(m))
	}
}

func TestAcctUserSingleActions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	m := h.acctMark()
	if code := h.call("DELETE", "/api/v1/users/bob", nil, nil); code != 200 {
		t.Fatalf("delete %d", code)
	}
	if code := h.call("POST", "/api/v1/users/bob/suspend", nil, nil); code != 200 {
		t.Fatalf("suspend %d", code)
	}
	if code := h.call("POST", "/api/v1/users/susp/unsuspend", nil, nil); code != 200 {
		t.Fatalf("unsuspend %d", code)
	}
	acctWant(t, h.acctLog(m), "v-delete-user bob", "v-suspend-user bob", "v-list-users json", "v-unsuspend-user susp")
	for _, bad := range []string{"-h", "a;b"} {
		if code := h.call("DELETE", "/api/v1/users/"+bad, nil, nil); code != 400 && code != 404 {
			t.Fatalf("%s: %d", bad, code)
		}
	}
}

func TestAcctUnsuspendActiveUser409(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	m := h.acctMark()
	var r APIError
	if code := h.call("POST", "/api/v1/users/bob/unsuspend", nil, &r); code != 409 || r.Error.Code != "not_suspended" {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json")
}

func TestAcctUnsuspendMissingUser404(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/users/nobody/unsuspend", nil, nil); code != 404 {
		t.Fatalf("%d", code)
	}
}

func TestAcctUserBulk(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	m := h.acctMark()
	post := func(a string, names ...string) (int, BulkReply) {
		var r BulkReply
		code := h.call("POST", "/api/v1/users/bulk", BulkRequest{Action: a, Names: names}, &r)
		return code, r
	}
	if code, _ := post("rebuild", "bob", "susp"); code != 400 {
		t.Fatalf("cap: %d", code)
	}
	if code, _ := post("delete", "bob", "susp"); code != 400 {
		t.Fatalf("cap delete: %d", code)
	}
	if code, _ := post("nope", "bob"); code != 400 {
		t.Fatalf("unknown: %d", code)
	}
	if code, _ := post("rebuild"); code != 400 {
		t.Fatalf("empty: %d", code)
	}
	if code, _ := post("rebuild", "-h"); code != 400 {
		t.Fatalf("bad name: %d", code)
	}
	acctWant(t, h.acctLog(m))

	for _, c := range []struct{ action, want string }{
		{"rebuild", "v-rebuild-all bob no"},
		{"rebuild-user", "v-rebuild-user bob no"},
		{"rebuild-web", "v-rebuild-web-domains bob"},
		{"rebuild-dns", "v-rebuild-dns-domains bob"},
		{"rebuild-mail", "v-rebuild-mail-domains bob"},
		{"rebuild-db", "v-rebuild-databases bob"},
		{"rebuild-cron", "v-rebuild-cron-jobs bob"},
		{"delete", "v-delete-user bob no"},
		{"suspend", "v-suspend-user bob yes"},
	} {
		m = h.acctMark()
		if code, r := post(c.action, "bob"); code != 200 || len(r.Failed) != 0 || r.Status != "ok" {
			t.Fatalf("%s: %d %+v", c.action, code, r)
		}
		acctWant(t, h.acctLog(m), c.want)
	}

	m = h.acctMark()
	if code, r := post("update-counters", "bob", "susp", "ops"); code != 200 || len(r.Failed) != 0 {
		t.Fatalf("counters: %d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-update-user-counters bob", "v-update-user-counters susp", "v-update-user-counters ops")

	m = h.acctMark()
	code, r := post("suspend", "susp")
	if code != 200 || len(r.Failed) != 1 || r.Failed[0].Name != "susp" || r.Failed[0].Message != "Error: user susp is suspended" {
		t.Fatalf("failure: %d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-suspend-user susp yes")

	m = h.acctMark()
	code, r = post("unsuspend", "bob")
	if code != 200 || len(r.Failed) != 1 || r.Failed[0].Name != "bob" || r.Failed[0].Message != "User %s is not suspended." ||
		!slices.Equal(r.Failed[0].Args, []string{"bob"}) {
		t.Fatalf("unsuspend active: %d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json")

	m = h.acctMark()
	if code, r := post("unsuspend", "susp"); code != 200 || len(r.Failed) != 0 {
		t.Fatalf("unsuspend: %d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json", "v-unsuspend-user susp yes")
}

func TestAcctUserBulkUpdateCountersCap(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	names := make([]string, 501)
	for i := range names {
		names[i] = fmt.Sprintf("u%d", i)
	}
	if code := h.call("POST", "/api/v1/users/bulk", BulkRequest{Action: "update-counters", Names: names}, nil); code != 400 {
		t.Fatalf("%d", code)
	}
}

func TestAcctUserBulkNonAdmin(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	m := h.acctMark()
	var r APIError
	if code := h.call("POST", "/api/v1/users/bulk", BulkRequest{Action: "update-counters", Names: []string{"bob"}}, &r); code != 403 {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m))
}

func TestAcctUserCommandTimeout(t *testing.T) {
	var got = map[string]time.Duration{}
	h := newHarnessWith(t, overrides{}, func(d *Deps) {
		d.R = acctTimeoutSpy{Runner: d.R, got: got}
	})
	h.loginAdmin()
	h.call("DELETE", "/api/v1/users/bob", nil, nil)
	if got["v-delete-user"] != acctUserCommandTimeout {
		t.Fatalf("timeouts = %v", got)
	}
}

type acctTimeoutSpy struct {
	runner.Runner
	got map[string]time.Duration
}

func (s acctTimeoutSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	s.got[cmd], _ = runner.Timeout(ctx)
	return s.Runner.Run(ctx, cmd, args...)
}

// ---------------------------------------------------------------- packages

func TestAcctPackagesListAndOptions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var l PackageList
	if code := h.call("GET", "/api/v1/packages", nil, &l); code != 200 {
		t.Fatalf("%d", code)
	}
	var names []string
	for _, p := range l.Packages {
		names = append(names, p.NAME)
	}
	if !reflect.DeepEqual(names, []string{"starter-plus", "starter", "default", "system"}) {
		t.Fatalf("names = %v", names)
	}
	m := h.acctMark()
	var o PackageOptions
	if code := h.call("GET", "/api/v1/packages/options", nil, &o); code != 200 {
		t.Fatalf("%d", code)
	}
	acctWant(t, h.acctLog(m), "v-list-web-templates json", "v-list-web-templates-backend json",
		"v-list-web-templates-proxy json", "v-list-dns-templates json", "v-list-sys-shells json")
	if o.WebSystem != "apache2" || o.WebBackend != "php-fpm" || o.ProxySystem != "nginx" || o.DNSSystem != "bind9" || o.ResourcesLimit ||
		len(o.WebTemplates) != 3 || len(o.Shells) != 6 {
		t.Fatalf("options = %+v", o)
	}

	h2 := newHarnessWith(t, overrides{Config: map[string]string{"WEB_BACKEND": "", "PROXY_SYSTEM": "", "RESOURCES_LIMIT": "yes"}})
	h2.loginAdmin()
	m = h2.acctMark()
	h2.call("GET", "/api/v1/packages/options", nil, &o)
	acctWant(t, h2.acctLog(m), "v-list-web-templates json", "v-list-dns-templates json", "v-list-sys-shells json")
	if !o.ResourcesLimit || o.BackendTemplates == nil || len(o.BackendTemplates) != 0 || o.ProxyTemplates == nil {
		t.Fatalf("options = %+v", o)
	}
}

func TestAcctPackageGet(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var p struct{ NAME, BACKEND_TEMPLATE string }
	if code := h.call("GET", "/api/v1/packages/starter-plus", nil, &p); code != 200 || p.NAME != "starter-plus" {
		t.Fatalf("%d %+v", code, p)
	}
	m := h.acctMark()
	if code := h.call("GET", "/api/v1/packages/system", nil, nil); code != 404 {
		t.Fatalf("system: %d", code)
	}
	if code := h.call("GET", "/api/v1/packages/missing", nil, nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code := h.call("GET", "/api/v1/packages/-h", nil, nil); code != 400 {
		t.Fatalf("bad: %d", code)
	}
	acctWant(t, h.acctLog(m), "v-list-user-package missing json")
}

func acctPkgReq() PackageRequest {
	return PackageRequest{
		Name: "gold", WebTemplate: "default", BackendTemplate: "PHP-8_3", ProxyTemplate: "caching", DNSTemplate: "gmail",
		WebDomains: "3", WebAliases: "unlimited", DNSDomains: "2", DNSRecords: "unlimited", MailDomains: "4", MailAccounts: "6",
		RateLimit: "200", Databases: "5", CronJobs: "7", DiskQuota: "9000", Bandwidth: "unlimited", CPUQuota: "50%", CPUQuotaPeriod: "10ms",
		MemoryLimit: "2G", SwapLimit: "1G", Backups: "3", BackupsIncremental: "yes", Shell: "bash",
		Nameservers: []string{"ns1.example.com.", "", "ns2.example.com", ".ns3.example.com"},
	}
}

// acctFileSpy records the package file named by the first argument of a call.
type acctFileSpy struct {
	runner.Runner
	file, mode []string
	left       []string
}

func (s *acctFileSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == "v-add-user-package" {
		b, _ := os.ReadFile(args[0])
		s.file = append(s.file, string(b))
		if st, err := os.Stat(args[0]); err == nil {
			s.mode = append(s.mode, st.Mode().Perm().String())
		}
		s.left = append(s.left, args[0])
	}
	return s.Runner.Run(ctx, cmd, args...)
}

func acctPkgHarness(t *testing.T, o overrides) (*harness, *acctFileSpy) {
	t.Helper()
	spy := &acctFileSpy{}
	h := newHarnessWith(t, o, func(d *Deps) {
		spy.Runner = d.R
		d.R = spy
	})
	h.loginAdmin()
	return h, spy
}

func TestAcctPackageAdd(t *testing.T) {
	h, spy := acctPkgHarness(t, overrides{})
	m := h.acctMark()
	var r PackageSaved
	if code := h.call("POST", "/api/v1/packages", acctPkgReq(), &r); code != 200 || r.Name != "gold" || r.Status != "ok" {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-add-user-package "+spy.left[0]+" gold")
	if spy.mode[0] != "-rw-------" {
		t.Errorf("mode %s", spy.mode[0])
	}
	if _, err := os.Stat(spy.left[0]); !os.IsNotExist(err) {
		t.Errorf("temp file left: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(spy.file[0]), "\n")
	var keys []string
	vals := map[string]string{}
	for _, l := range lines {
		k, v, _ := strings.Cut(l, "=")
		keys = append(keys, k)
		vals[k] = v
	}
	wantKeys := []string{"WEB_TEMPLATE", "BACKEND_TEMPLATE", "PROXY_TEMPLATE", "DNS_TEMPLATE", "WEB_DOMAINS", "WEB_ALIASES", "DNS_DOMAINS",
		"DNS_RECORDS", "MAIL_DOMAINS", "MAIL_ACCOUNTS", "DATABASES", "CRON_JOBS", "DISK_QUOTA", "CPU_QUOTA", "CPU_QUOTA_PERIOD",
		"MEMORY_LIMIT", "SWAP_LIMIT", "BANDWIDTH", "RATE_LIMIT", "NS", "SHELL", "BACKUPS", "BACKUPS_INCREMENTAL", "TIME", "DATE"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys:\n got  %v\n want %v", keys, wantKeys)
	}
	for k, want := range map[string]string{
		"WEB_TEMPLATE": "'default'", "BACKEND_TEMPLATE": "'PHP-8_3'", "PROXY_TEMPLATE": "'caching'", "WEB_ALIASES": "'unlimited'",
		"NS": "'ns1.example.com,ns2.example.com,ns3.example.com'", "SHELL": "'bash'", "BACKUPS_INCREMENTAL": "'yes'",
		"CPU_QUOTA": "", "CPU_QUOTA_PERIOD": "", "MEMORY_LIMIT": "", "SWAP_LIMIT": "", "RATE_LIMIT": "'200'", "DISK_QUOTA": "'9000'",
	} {
		if vals[k] != want {
			t.Errorf("%s = %q, want %q", k, vals[k], want)
		}
	}
}

func TestAcctPackageAddResourcesOnAndNoBackendProxy(t *testing.T) {
	h, spy := acctPkgHarness(t, overrides{Config: map[string]string{"RESOURCES_LIMIT": "yes", "WEB_BACKEND": "", "PROXY_SYSTEM": ""}})
	if code := h.call("POST", "/api/v1/packages", acctPkgReq(), nil); code != 200 {
		t.Fatalf("%d", code)
	}
	f := spy.file[0]
	if strings.Contains(f, "BACKEND_TEMPLATE") || strings.Contains(f, "PROXY_TEMPLATE") {
		t.Errorf("file = %q", f)
	}
	for _, want := range []string{"CPU_QUOTA='50%'\n", "CPU_QUOTA_PERIOD='10ms'\n", "MEMORY_LIMIT='2G'\n", "SWAP_LIMIT='1G'\n"} {
		if !strings.Contains(f, want) {
			t.Errorf("missing %q in %q", want, f)
		}
	}
}

func TestAcctPackageAddValidation(t *testing.T) {
	h, _ := acctPkgHarness(t, overrides{})
	m := h.acctMark()
	var r APIError
	if code := h.call("POST", "/api/v1/packages", PackageRequest{}, &r); code != 400 || r.Error.Message != blankFieldMsg {
		t.Fatalf("%d %+v", code, r)
	}
	wantBlank := []string{"Package", "Web Template", "Backend Template", "Proxy Template", "DNS Template", "Shell", "Web Domains", "Web Aliases",
		"DNS Zones", "DNS Records", "Mail Domains", "Mail Accounts", "Databases", "Cron Jobs", "Backups", "Incremental Backups", "Quota",
		"Bandwidth", "Rate Limit", "Nameserver 1", "Nameserver 2"}
	if !reflect.DeepEqual(r.Error.Args, wantBlank) {
		t.Fatalf("args = %v", r.Error.Args)
	}
	bad := func(mod func(*PackageRequest)) {
		t.Helper()
		req := acctPkgReq()
		mod(&req)
		if code := h.call("POST", "/api/v1/packages", req, nil); code != 400 {
			t.Fatalf("%+v: %d", req, code)
		}
	}
	bad(func(r *PackageRequest) { r.Name = "-h" })
	bad(func(r *PackageRequest) { r.Name = "a b" })
	bad(func(r *PackageRequest) { r.WebDomains = "1'\nDATE='x" })
	bad(func(r *PackageRequest) { r.Shell = "a\nb" })
	bad(func(r *PackageRequest) { r.Nameservers = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} })
	// Refused before any command, with the message the bin would have printed (owner ruling 16).
	msg := func(mod func(*PackageRequest), args ...string) {
		t.Helper()
		req := acctPkgReq()
		mod(&req)
		var e APIError
		if code := h.call("POST", "/api/v1/packages", req, &e); code != 400 || e.Error.Message != "invalid %s format :: %s" ||
			!reflect.DeepEqual(e.Error.Args, args) {
			t.Fatalf("%d %+v", code, e)
		}
	}
	msg(func(r *PackageRequest) { r.Name = "a b" }, "package", "a b")
	msg(func(r *PackageRequest) { r.WebDomains = "1'x" }, "WEB_DOMAINS", "1'x")
	msg(func(r *PackageRequest) { r.Shell = "a\nb" }, "SHELL", "a\nb")
	acctWant(t, h.acctLog(m))
}

func TestAcctPackageAddStopsOnCommandError(t *testing.T) {
	h, _ := acctPkgHarness(t, overrides{})
	req := acctPkgReq()
	req.Name = "starter"
	var r APIError
	if code := h.call("POST", "/api/v1/packages", req, &r); code != 409 || r.Error.Code != "exists" {
		t.Fatalf("%d %+v", code, r)
	}
}

func acctStoredPackage(name string) map[string][]fixtureEntry {
	body := fmt.Sprintf(`{%q:{"WEB_TEMPLATE":"old","BACKEND_TEMPLATE":"oldbe","PROXY_TEMPLATE":"oldpx","DNS_TEMPLATE":"old","WEB_DOMAINS":"1",`+
		`"WEB_ALIASES":"1","DNS_DOMAINS":"1","DNS_RECORDS":"1","MAIL_DOMAINS":"1","MAIL_ACCOUNTS":"1","RATE_LIMIT":"1","DATABASES":"1",`+
		`"CRON_JOBS":"1","DISK_QUOTA":"1","CPU_QUOTA":"20%%","CPU_QUOTA_PERIOD":"5ms","MEMORY_LIMIT":"1G","SWAP_LIMIT":"512M","BANDWIDTH":"1",`+
		`"NS":"keep1.example.com,keep2.example.com","SHELL":"nologin","BACKUPS":"1","BACKUPS_INCREMENTAL":"no","TIME":"01:00:00","DATE":"2026-01-01"}}`, name)
	return map[string][]fixtureEntry{"v-list-user-package": {{Args: []string{name, "json"}, Stdout: body}}}
}

func TestAcctPackageEditOrder(t *testing.T) {
	h, spy := acctPkgHarness(t, overrides{Fixtures: acctStoredPackage("silver")})
	m := h.acctMark()
	var r PackageSaved
	if code := h.call("PUT", "/api/v1/packages/silver", acctPkgReq(), &r); code != 200 || r.Name != "gold" {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-user-package silver json", "v-add-user-package "+spy.left[0]+" silver yes",
		"v-update-user-package silver", "v-rename-user-package silver gold")
	keys := []string{}
	for _, l := range strings.Split(strings.TrimSpace(spy.file[0]), "\n") {
		k, _, _ := strings.Cut(l, "=")
		keys = append(keys, k)
	}
	want := []string{"WEB_TEMPLATE", "BACKEND_TEMPLATE", "PROXY_TEMPLATE", "DNS_TEMPLATE", "WEB_DOMAINS", "WEB_ALIASES", "DNS_DOMAINS", "DNS_RECORDS",
		"MAIL_DOMAINS", "MAIL_ACCOUNTS", "RATE_LIMIT", "DATABASES", "CRON_JOBS", "DISK_QUOTA", "CPU_QUOTA", "CPU_QUOTA_PERIOD", "MEMORY_LIMIT",
		"SWAP_LIMIT", "BANDWIDTH", "NS", "SHELL", "BACKUPS", "BACKUPS_INCREMENTAL", "TIME", "DATE"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v", keys)
	}
	// RESOURCES_LIMIT is off: the stored limits are kept.
	for _, w := range []string{"CPU_QUOTA='20%'\n", "MEMORY_LIMIT='1G'\n", "SWAP_LIMIT='512M'\n", "CPU_QUOTA_PERIOD='5ms'\n"} {
		if !strings.Contains(spy.file[0], w) {
			t.Errorf("missing %q in %q", w, spy.file[0])
		}
	}
	if !strings.Contains(spy.file[0], "NS='ns1.example.com,ns2.example.com,ns3.example.com'\n") {
		t.Errorf("NS: %q", spy.file[0])
	}
}

func TestAcctPackageEditNoRename(t *testing.T) {
	h, _ := acctPkgHarness(t, overrides{Fixtures: acctStoredPackage("silver")})
	req := acctPkgReq()
	req.Name = "silver"
	m := h.acctMark()
	var r PackageSaved
	if code := h.call("PUT", "/api/v1/packages/silver", req, &r); code != 200 || r.Name != "silver" {
		t.Fatalf("%d %+v", code, r)
	}
	if got := h.acctLog(m); len(got) != 3 {
		t.Fatalf("calls = %q", got)
	}
}

func TestAcctPackageEditKeepsStoredWhenSystemsOff(t *testing.T) {
	h, spy := acctPkgHarness(t, overrides{
		Fixtures: acctStoredPackage("silver"),
		Config:   map[string]string{"DNS_SYSTEM": "", "WEB_BACKEND": "", "PROXY_SYSTEM": "", "RESOURCES_LIMIT": "yes"},
	})
	req := acctPkgReq()
	req.Nameservers = nil
	req.BackendTemplate, req.ProxyTemplate = "", ""
	if code := h.call("PUT", "/api/v1/packages/silver", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	f := spy.file[0]
	for _, w := range []string{"NS='keep1.example.com,keep2.example.com'\n", "BACKEND_TEMPLATE='oldbe'\n", "PROXY_TEMPLATE='oldpx'\n", "CPU_QUOTA='50%'\n"} {
		if !strings.Contains(f, w) {
			t.Errorf("missing %q in %q", w, f)
		}
	}
}

func TestAcctPackageEditValidationRunsNothing(t *testing.T) {
	h, _ := acctPkgHarness(t, overrides{Fixtures: acctStoredPackage("silver")})
	m := h.acctMark()
	req := acctPkgReq()
	req.Shell = ""
	req.WebTemplate = ""
	var r APIError
	if code := h.call("PUT", "/api/v1/packages/silver", req, &r); code != 400 || !reflect.DeepEqual(r.Error.Args, []string{"Web Template", "Shell"}) {
		t.Fatalf("%d %+v", code, r)
	}
	req = acctPkgReq()
	req.Nameservers = []string{"ns1.example.com"}
	if code := h.call("PUT", "/api/v1/packages/silver", req, &r); code != 400 || !reflect.DeepEqual(r.Error.Args, []string{"Nameserver 2"}) {
		t.Fatalf("%d %+v", code, r)
	}
	if code := h.call("PUT", "/api/v1/packages/system", acctPkgReq(), nil); code != 404 {
		t.Fatalf("system: %d", code)
	}
	acctWant(t, h.acctLog(m))
}

func TestAcctPackageEditStopsWhenUpdateFails(t *testing.T) {
	fx := acctStoredPackage("silver")
	fx["v-update-user-package"] = []fixtureEntry{{Args: []string{"silver"}, Stdout: "Error: limit\n", Exit: 8}}
	h, spy := acctPkgHarness(t, overrides{Fixtures: fx})
	m := h.acctMark()
	var r APIError
	if code := h.call("PUT", "/api/v1/packages/silver", acctPkgReq(), &r); code != 409 || r.Error.Code != "limit" {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-user-package silver json", "v-add-user-package "+spy.left[0]+" silver yes", "v-update-user-package silver")
}

func TestAcctPackageEditMissing(t *testing.T) {
	h, _ := acctPkgHarness(t, overrides{})
	if code := h.call("PUT", "/api/v1/packages/missing", acctPkgReq(), nil); code != 404 {
		t.Fatalf("%d", code)
	}
}

func TestAcctPackageCopy(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	m := h.acctMark()
	if code := h.call("POST", "/api/v1/packages/starter-plus/copy", nil, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	acctWant(t, h.acctLog(m), "v-copy-user-package starter-plus starter-plus-copy")
	h2 := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-copy-user-package": {{Args: []string{"starter", "starter-copy"}, Stdout: "Error: package starter-copy already exists.\n", Exit: 4}},
	}})
	h2.loginAdmin()
	var r APIError
	if code := h2.call("POST", "/api/v1/packages/starter/copy", nil, &r); code != 409 || r.Error.Code != "exists" {
		t.Fatalf("exists: %d %+v", code, r)
	}
	// A failure without output reads as Hestia's (owner ruling 16).
	h3 := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-copy-user-package": {{Args: []string{"starter", "starter-copy"}, Stdout: "", Exit: 1}},
	}})
	h3.loginAdmin()
	r = APIError{}
	if code := h3.call("POST", "/api/v1/packages/starter/copy", nil, &r); code < 400 || r.Error.Message != "Error: unable to copy package." {
		t.Fatalf("no output: %d %+v", code, r)
	}
}

func TestAcctPackageDeleteProtected(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	m := h.acctMark()
	for _, n := range []string{"default", "system"} {
		if code := h.call("DELETE", "/api/v1/packages/"+n, nil, nil); code != 400 {
			t.Fatalf("%s: %d", n, code)
		}
	}
	if code := h.call("POST", "/api/v1/packages/bulk", BulkRequest{Action: "delete", Names: []string{"starter", "default"}}, nil); code != 400 {
		t.Fatalf("bulk: %d", code)
	}
	acctWant(t, h.acctLog(m))
}

func TestAcctPackageDeleteInUseByPrefix(t *testing.T) {
	fx := acctUsersFixture([]string{"admin", "a", "b"}, map[string]string{"admin": "system", "a": "starter-plus", "b": "starter"})
	h := newHarnessWith(t, overrides{Fixtures: fx})
	h.loginAdmin()
	m := h.acctMark()
	var r APIError
	if code := h.call("DELETE", "/api/v1/packages/starter", nil, &r); code != 409 || r.Error.Code != "in_use" ||
		r.Error.Message != "Package %s can't be deleted while package %s has users." || !reflect.DeepEqual(r.Error.Args, []string{"starter", "starter-plus"}) {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json")
}

func TestAcctPackageDeleteMovesUsers(t *testing.T) {
	fx := acctUsersFixture([]string{"admin", "a", "b", "c"}, map[string]string{"admin": "system", "a": "gold", "b": "silver", "c": "gold"})
	h := newHarnessWith(t, overrides{Fixtures: fx})
	h.loginAdmin()
	m := h.acctMark()
	if code := h.call("DELETE", "/api/v1/packages/gold", nil, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	acctWant(t, h.acctLog(m), "v-list-users json", "v-change-user-package a default", "v-change-user-package c default", "v-delete-user-package gold")
}

func TestAcctPackageDeleteMoveFails(t *testing.T) {
	fx := acctUsersFixture([]string{"admin", "a", "c"}, map[string]string{"admin": "system", "a": "gold", "c": "gold"})
	fx["v-change-user-package"] = []fixtureEntry{{Args: []string{"a", "default"}, Stdout: "Error: Package doesn't cover DISK usage\n", Exit: 8}}
	h := newHarnessWith(t, overrides{Fixtures: fx})
	h.loginAdmin()
	m := h.acctMark()
	var r APIError
	if code := h.call("DELETE", "/api/v1/packages/gold", nil, &r); code != 409 || r.Error.Code != "limit" {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json", "v-change-user-package a default")
}

func TestAcctPackageBulkDelete(t *testing.T) {
	fx := acctUsersFixture([]string{"admin", "a"}, map[string]string{"admin": "system", "a": "gold-plus"})
	h := newHarnessWith(t, overrides{Fixtures: fx})
	h.loginAdmin()
	m := h.acctMark()
	var r BulkReply
	code := h.call("POST", "/api/v1/packages/bulk", BulkRequest{Action: "delete", Names: []string{"gold", "silver", "missing"}}, &r)
	if code != 200 || len(r.Failed) != 2 || r.Failed[0].Name != "gold" || r.Failed[1].Name != "missing" ||
		r.Failed[0].Message != "Package %s can't be deleted while package %s has users." ||
		!slices.Equal(r.Failed[0].Args, []string{"gold", "gold-plus"}) {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json", "v-list-users json", "v-delete-user-package silver", "v-list-users json", "v-delete-user-package missing")
	if code := h.call("POST", "/api/v1/packages/bulk", BulkRequest{Action: "copy", Names: []string{"x1"}}, nil); code != 400 {
		t.Fatalf("action: %d", code)
	}
}

func TestAcctUsersAndPackagesReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	m := h.acctMark()
	for _, rt := range []struct {
		method, path string
		body         any
	}{
		{"DELETE", "/api/v1/users/bob", nil},
		{"POST", "/api/v1/users/bob/suspend", nil},
		{"POST", "/api/v1/users/susp/unsuspend", nil},
		{"POST", "/api/v1/users/bulk", BulkRequest{Action: "update-counters", Names: []string{"bob"}}},
		{"POST", "/api/v1/packages", acctPkgReq()},
		{"PUT", "/api/v1/packages/starter", acctPkgReq()},
		{"POST", "/api/v1/packages/starter/copy", nil},
		{"DELETE", "/api/v1/packages/starter", nil},
		{"POST", "/api/v1/packages/bulk", BulkRequest{Action: "delete", Names: []string{"starter"}}},
	} {
		var r APIError
		if code := h.call(rt.method, rt.path, rt.body, &r); code != 403 || r.Error.Code != "read_only" {
			t.Fatalf("%s %s: %d %+v", rt.method, rt.path, code, r)
		}
	}
	acctWant(t, h.acctLog(m))
}

func TestAcctPackageWritesForbiddenForUsers(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	m := h.acctMark()
	for _, rt := range [][2]string{{"POST", "/api/v1/packages"}, {"DELETE", "/api/v1/packages/starter"}, {"DELETE", "/api/v1/users/ops"}, {"GET", "/api/v1/packages"}} {
		if code := h.call(rt[0], rt[1], nil, nil); code != 403 {
			t.Fatalf("%v: %d", rt, code)
		}
	}
	acctWant(t, h.acctLog(m))
}

func TestAcctPackageAddIgnoresNameserversWithoutDNS(t *testing.T) {
	h, spy := acctPkgHarness(t, overrides{Config: map[string]string{"DNS_SYSTEM": ""}})
	if code := h.call("POST", "/api/v1/packages", acctPkgReq(), nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if !strings.Contains(spy.file[0], "NS=','\n") {
		t.Errorf("file = %q", spy.file[0])
	}
}

// The bin matches users with grep, where the dot in a package name matches any
// character; the in-use check must refuse whenever the bin would touch those users.
func TestAcctPackageDeleteInUseDotIsWildcard(t *testing.T) {
	fx := acctUsersFixture([]string{"admin", "a"}, map[string]string{"admin": "system", "a": "axb"})
	h := newHarnessWith(t, overrides{Fixtures: fx})
	h.loginAdmin()
	m := h.acctMark()
	var r APIError
	if code := h.call("DELETE", "/api/v1/packages/a.b", nil, &r); code != 409 || r.Error.Code != "in_use" {
		t.Fatalf("%d %+v", code, r)
	}
	acctWant(t, h.acctLog(m), "v-list-users json")
}

// Deleting a user signs them out everywhere, single and bulk.
func TestAcctDeleteEndsUserSessions(t *testing.T) {
	for _, del := range []func(h *harness){
		func(h *harness) { h.call("DELETE", "/api/v1/users/bob", nil, nil) },
		func(h *harness) {
			h.call("POST", "/api/v1/users/bulk", UserBulkRequest{Action: "delete", Names: []string{"bob"}}, nil)
		},
	} {
		h := newHarness(t)
		h.store.Save(&session.Session{ID: "bobs-session", Token: "t", User: "bob", UserContext: "user"})
		h.loginAdmin()
		del(h)
		if _, ok := h.store.Get("bobs-session"); ok {
			t.Fatal("the deleted user's session survived")
		}
		if _, ok := h.store.Get(h.sessionID()); !ok {
			t.Fatal("the admin's own session was ended")
		}
	}
}

// While an admin is logged in as another user the Users and Packages pages are refused,
// as Hestia drops them (owner ruling 12). The package detail read stays: the account
// form shows the chosen package's limits.
func TestUsersAndPackagesRefusedWhileLoggedInAs(t *testing.T) {
	h := newHarnessWith(t, overrides{})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	routes := []struct{ method, path string }{
		{"GET", "/api/v1/users"}, {"POST", "/api/v1/users"}, {"POST", "/api/v1/users/bulk"},
		{"GET", "/api/v1/users/bob"}, {"PUT", "/api/v1/users/bob"}, {"DELETE", "/api/v1/users/bob"},
		{"DELETE", "/api/v1/users/bob/2fa"}, {"POST", "/api/v1/users/bob/suspend"}, {"POST", "/api/v1/users/bob/unsuspend"},
		{"GET", "/api/v1/packages"}, {"GET", "/api/v1/packages/options"}, {"POST", "/api/v1/packages"},
		{"POST", "/api/v1/packages/bulk"}, {"PUT", "/api/v1/packages/default"}, {"POST", "/api/v1/packages/default/copy"},
		{"DELETE", "/api/v1/packages/default"},
	}
	for _, r := range routes {
		from := h.acctMark()
		var e APIError
		if code := h.call(r.method, r.path, map[string]any{}, &e); code != 403 || e.Error.Code != "forbidden" {
			t.Errorf("%s %s: %d %+v", r.method, r.path, code, e)
		}
		if ran := h.acctLog(from); len(ran) != 0 {
			t.Errorf("%s %s ran %q", r.method, r.path, ran)
		}
	}
	if code := h.call("GET", "/api/v1/packages/default", nil, nil); code != 200 {
		t.Errorf("package read while logged in as: %d", code)
	}
}
