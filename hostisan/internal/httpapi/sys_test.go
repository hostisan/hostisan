package httpapi

// Plan 4 foundation tests: the server-admin guard table over every registered
// /api/v1/server route, the job helpers, and the Plan 4 dev fixtures. Shared helpers
// here carry the sys prefix; slices reuse them and must not redefine them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

// sysTestServer serves every server-admin route (sys.routes) plus the routes extra
// registers, on its own registry, sharing h's sessions and fixtures. It returns the
// server, the sysAPI behind it and every registered pattern.
func sysTestServer(h *harness, extra func(sys *sysAPI, m *sysMux)) (*httptest.Server, *sysAPI, []string) {
	h.t.Helper()
	s := &server{Deps: h.deps, jobs: jobs.NewRegistry(0, nil)}
	sys := &sysAPI{server: s}
	mux := http.NewServeMux()
	m := &sysMux{mux: mux}
	sys.routes(m)
	if extra != nil {
		extra(sys, m)
	}
	srv := httptest.NewServer(mux)
	h.t.Cleanup(srv.Close)
	return srv, sys, m.patterns
}

// sysDo sends a JSON request to base+path with the current CSRF token.
func (h *harness) sysDo(base, method, path string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("X-CSRF-Token", h.token)
	}
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

// sysCommands is every command run since call index from, without the session's own
// reads (config, the signed-in user and the logout log): what a handler ran.
func (h *harness) sysCommands(from int) []string {
	var out []string
	for _, c := range h.fixture.Calls()[from:] {
		switch c.Cmd {
		case "v-list-sys-config", "v-list-user", "v-log-user-logout":
			continue
		}
		out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
	}
	return out
}

// sysPathValues fill the path parameters of the server routes for the guard table.
var sysPathValues = map[string]string{
	"{id}": "0123456789abcdef0123456789abcdef", "{service}": "nginx", "{action}": "restart",
	"{ip}": "203.0.113.10", "{name}": "blacklist", "{chart}": "la", "{kind}": "cpu",
}

func sysConcrete(t *testing.T, pattern string) (method, path string) {
	t.Helper()
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		t.Fatalf("pattern without a method: %q", pattern)
	}
	for k, v := range sysPathValues {
		path = strings.ReplaceAll(path, k, v)
	}
	// The job id doubles as a rule id on firewall routes; a 32-digit id is still a
	// valid path value for the guard, which runs first.
	if strings.Contains(path, "{") {
		t.Fatalf("pattern %q has a path parameter the guard table doesn't fill", pattern)
	}
	return method, path
}

// sysGuardProbes are test-only routes that run a command, so a guard that lets a
// request through shows up in the command log.
func sysGuardProbes(sys *sysAPI, m *sysMux) {
	probe := func(w http.ResponseWriter, r *http.Request) {
		if err := sys.run(r.Context(), "v-list-sys-info", "json"); err != nil {
			writeCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
	m.handle("GET /api/v1/server/test-read", sys.read(probe))
	m.handle("POST /api/v1/server/test-write", sys.write(probe))
}

func TestSysRoutesRegisterJobStatus(t *testing.T) {
	h := newHarness(t)
	got := sysPatterns(&server{Deps: h.deps, jobs: jobs.NewRegistry(0, nil)})
	if !slices.Contains(got, "GET /api/v1/server/jobs/{id}") {
		t.Fatalf("patterns = %v", got)
	}
	for _, p := range got {
		if _, path, _ := strings.Cut(p, " "); !strings.HasPrefix(path, "/api/v1/server/") {
			t.Errorf("pattern outside /api/v1/server: %q", p)
		}
	}
}

func TestSysGuardTable(t *testing.T) {
	hide := map[string]string{"POLICY_SYSTEM_HIDE_SERVICES": "yes", "ROOT_USER": "admin"}
	cases := []struct {
		name   string
		cfg    map[string]string
		login  func(h *harness)
		noCSRF bool
		// want returns the expected status for a route, or 0 for "allowed" (anything
		// but 401 and 403), and whether the route may run commands.
		want func(method string) (status int, code string)
	}{
		{name: "user", login: (*harness).loginBob,
			want: func(string) (int, string) { return 403, "forbidden" }},
		{name: "non-root admin, services hidden", cfg: hide, login: (*harness).loginOps,
			want: func(string) (int, string) { return 403, "forbidden" }},
		{name: "root admin, services hidden", cfg: hide, login: (*harness).loginAdmin,
			want: func(string) (int, string) { return 0, "" }},
		{name: "non-root admin, services shown", login: (*harness).loginOps,
			want: func(string) (int, string) { return 0, "" }},
		{name: "write without CSRF", login: (*harness).loginAdmin, noCSRF: true,
			want: func(m string) (int, string) {
				if m == "GET" {
					return 0, ""
				}
				return 400, "csrf"
			}},
		{name: "read-only session", cfg: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"},
			login: func(h *harness) {
				h.loginAdmin()
				h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
			},
			want: func(m string) (int, string) {
				if m == "GET" {
					return 0, ""
				}
				return 403, "read_only"
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarnessWith(t, overrides{Config: c.cfg})
			c.login(h)
			if c.noCSRF {
				h.token = ""
			}
			srv, _, patterns := sysTestServer(h, sysGuardProbes)
			for _, p := range patterns {
				method, path := sysConcrete(t, p)
				before := len(h.fixture.Calls())
				var e APIError
				code := h.sysDo(srv.URL, method, path, struct{}{}, &e)
				ran := h.sysCommands(before)
				status, errCode := c.want(method)
				if status == 0 {
					if code == 401 || code == 403 {
						t.Errorf("%s: %d %+v, want allowed", p, code, e)
					}
					if strings.Contains(path, "/test-") && (code != 200 || len(ran) != 1) {
						t.Errorf("%s: %d, ran %q; want 200 and the probe's command", p, code, ran)
					}
					continue
				}
				if code != status || e.Error.Code != errCode {
					t.Errorf("%s: %d %+v, want %d %s", p, code, e, status, errCode)
				}
				if len(ran) != 0 {
					t.Errorf("%s: ran %q, want no commands", p, ran)
				}
			}
		})
	}
}

func TestSysGuardSignedOut(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	srv, _, patterns := sysTestServer(h, sysGuardProbes)
	for _, p := range patterns {
		method, path := sysConcrete(t, p)
		if code := h.sysDo(srv.URL, method, path, struct{}{}, nil); code != 401 && code != 400 {
			t.Errorf("%s signed out: %d", p, code)
		}
	}
}

// sysSlowRunner waits out the timeout set with runner.WithTimeout for slowCmd, then
// fails as the sudo runner does; other commands go to the fixtures.
type sysSlowRunner struct {
	runner.Runner
	slowCmd string
}

func (r sysSlowRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd != r.slowCmd {
		return r.Runner.Run(ctx, cmd, args...)
	}
	d, ok := runner.Timeout(ctx)
	if !ok {
		d = time.Minute
	}
	select {
	case <-time.After(d):
		return runner.Result{}, fmt.Errorf("%w: %s", runner.ErrTimeout, cmd)
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	}
}

// sysWaitJob polls the job status route until the job leaves "running".
func (h *harness) sysWaitJob(base, id string) JobStatus {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var st JobStatus
		if code := h.sysDo(base, "GET", "/api/v1/server/jobs/"+id, nil, &st); code != 200 {
			h.t.Fatalf("job status: %d", code)
		}
		if st.State != "running" {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("job still running")
	return JobStatus{}
}

func TestSysStartJob(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	release := make(chan struct{})
	srv, _, _ := sysTestServer(h, func(sys *sysAPI, m *sysMux) {
		m.handle("POST /api/v1/server/test-job", sys.write(func(w http.ResponseWriter, r *http.Request) {
			sys.startJob(w, r, "Restart services", func(ctx context.Context, j *jobs.Job) (any, error) {
				if _, err := sys.step(ctx, j, "Restart nginx", sysTimeoutService, "v-restart-service", "nginx", "yes"); err != nil {
					return nil, err
				}
				<-release
				return OK{Status: "ok"}, nil
			})
		}))
	})
	var ref JobRef
	if code := h.sysDo(srv.URL, "POST", "/api/v1/server/test-job", nil, &ref); code != 202 || !jobs.ValidID(ref.JobID) {
		t.Fatalf("start: %d %+v", code, ref)
	}
	var e APIError
	if code := h.sysDo(srv.URL, "POST", "/api/v1/server/test-job", nil, &e); code != 409 ||
		e.Error.Code != "busy" || e.Error.Message != "Another server task is still running." {
		t.Fatalf("second start: %d %+v", code, e)
	}
	close(release)
	st := h.sysWaitJob(srv.URL, ref.JobID)
	if st.State != "done" || st.Title != "Restart services" || !slices.Equal(st.Steps, []string{"Restart nginx"}) {
		t.Fatalf("status = %+v", st)
	}
	if b, _ := json.Marshal(st.Result); string(b) != `{"status":"ok"}` {
		t.Errorf("result = %s", b)
	}
	// Free again once the first job ended.
	release = make(chan struct{})
	close(release)
	if code := h.sysDo(srv.URL, "POST", "/api/v1/server/test-job", nil, &ref); code != 202 {
		t.Fatalf("start after done: %d", code)
	}
}

func TestSysJobStepTimeout(t *testing.T) {
	h := newHarnessWith(t, overrides{}, func(d *Deps) { d.R = sysSlowRunner{Runner: d.R, slowCmd: "v-restart-service"} })
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, func(sys *sysAPI, m *sysMux) {
		m.handle("POST /api/v1/server/test-job", sys.write(func(w http.ResponseWriter, r *http.Request) {
			sys.startJob(w, r, "Restart services", func(ctx context.Context, j *jobs.Job) (any, error) {
				if _, err := sys.step(ctx, j, "List services", sysTimeoutDefault, "v-list-sys-services", "json"); err != nil {
					return nil, err
				}
				_, err := sys.step(ctx, j, "Restart nginx", 20*time.Millisecond, "v-restart-service", "nginx", "yes")
				return nil, err
			})
		}))
	})
	var ref JobRef
	if code := h.sysDo(srv.URL, "POST", "/api/v1/server/test-job", nil, &ref); code != 202 {
		t.Fatalf("start: %d", code)
	}
	st := h.sysWaitJob(srv.URL, ref.JobID)
	if st.State != "failed" || st.Error == nil || st.Error.Message != "Error: command timed out" ||
		st.Step != "Restart nginx" || !slices.Equal(st.Steps, []string{"List services", "Restart nginx"}) {
		t.Fatalf("status = %+v (error %+v)", st, st.Error)
	}
}

// sysTimeoutRunner records the timeout each command ran with.
type sysTimeoutRunner struct {
	runner.Runner
	seen map[string]time.Duration
}

func (r sysTimeoutRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	d, _ := runner.Timeout(ctx)
	r.seen[cmd] = d
	return r.Runner.Run(ctx, cmd, args...)
}

func TestSysStepTimeoutAndExit(t *testing.T) {
	seen := map[string]time.Duration{}
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-start-service": {{Args: []string{"*"}, Stdout: "Error: nope\n", Exit: 20}},
	}}, func(d *Deps) { d.R = sysTimeoutRunner{Runner: d.R, seen: seen} })
	sys := &sysAPI{server: &server{Deps: h.deps, jobs: jobs.NewRegistry(0, nil)}}
	ctx := context.Background()
	id, err := sys.jobs.Start(ctx, jobs.Spec{Key: sysJobKey}, func(ctx context.Context, j *jobs.Job) (any, error) {
		res, err := sys.step(ctx, j, "Install PHP 8.4", sysTimeoutInstall, "v-list-sys-info", "json")
		if err != nil || len(res.Stdout) == 0 {
			t.Errorf("list: %+v, %v", res, err)
		}
		_, err = sys.step(ctx, j, "Start nginx", sysTimeoutService, "v-start-service", "nginx")
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	<-sys.jobs.Done(id)
	st, _ := sys.jobs.Get(id)
	var ce *runner.CommandError
	if st.State != jobs.Failed || !errors.As(st.Err, &ce) || ce.Exit != 20 {
		t.Fatalf("status = %+v", st)
	}
	if seen["v-list-sys-info"] != 20*time.Minute || seen["v-start-service"] != 5*time.Minute {
		t.Errorf("timeouts = %v", seen)
	}
	if sysTimeoutDownload != 3*time.Minute || sysTimeoutDefault != 2*time.Minute || sysTimeoutStatus != 30*time.Second {
		t.Error("timeout constants changed")
	}
}

func TestSysJobStatusNotFound(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	srv, sys, _ := sysTestServer(h, nil)
	other, err := sys.jobs.Start(context.Background(), jobs.Spec{Key: "web:bob:example.com", Owner: "admin"},
		func(context.Context, *jobs.Job) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	<-sys.jobs.Done(other)
	for _, id := range []string{"xyz", strings.Repeat("A", 32), "0123456789abcdef0123456789abcdef", other} {
		var e APIError
		if code := h.sysDo(srv.URL, "GET", "/api/v1/server/jobs/"+id, nil, &e); code != 404 || e.Error.Code != "not_found" {
			t.Errorf("job %q: %d %+v", id, code, e)
		}
	}
}

func TestSysServices(t *testing.T) {
	h := newHarness(t)
	sys := &sysAPI{server: &server{Deps: h.deps}}
	got, err := sys.services(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"apache2", "nginx", "php8.2-fpm", "php8.3-fpm", "iptables", "ssh", "web-terminal"} {
		if got[name].NAME != name {
			t.Errorf("missing %s: %+v", name, got)
		}
	}
	if got["php8.3-fpm"].STATE != "stopped" || got["nginx"].STATE != "running" {
		t.Errorf("states: %+v", got)
	}
	if _, ok := got["sshd"]; ok {
		t.Error("unknown service present")
	}
}

// TestSysDevFixtures checks the Plan 4 dev fixtures decode with the hestia types and
// hold the values the slices and the fixture-mode smoke test rely on.
func TestSysDevFixtures(t *testing.T) {
	h := newHarness(t)
	run := func(cmd string, args ...string) runner.Result {
		t.Helper()
		res, err := h.fixture.Run(context.Background(), cmd, args...)
		if err != nil {
			t.Fatalf("%s %v: %v", cmd, args, err)
		}
		return res
	}
	out := func(cmd string, args ...string) []byte {
		t.Helper()
		res := run(cmd, args...)
		if res.Exit != 0 {
			t.Fatalf("%s %v: exit %d", cmd, args, res.Exit)
		}
		return res.Stdout
	}

	info, err := hestia.DecodeSysInfo(out("v-list-sys-info", "json"))
	if err != nil || info.HOSTNAME != "server1.acme-hosting.net" || info.HESTIA != "1.10.5" {
		t.Errorf("sysinfo = %+v, %v", info, err)
	}

	svcs, err := hestia.DecodeObjects[hestia.SysService](out("v-list-sys-services", "json"))
	if err != nil || len(svcs) != 16 || svcs[0].NAME != "apache2" || svcs[len(svcs)-1].NAME != "web-terminal" {
		t.Errorf("services = %+v, %v", svcs, err)
	}

	for cmd, key := range map[string]string{
		"v-list-sys-nginx-config": "worker_connections", "v-list-sys-php-config": "memory_limit",
		"v-list-sys-mysql-config": "max_connections", "v-list-sys-pgsql-config": "pg_hba_path",
		"v-list-sys-dovecot-config": "config_path8",
	} {
		f, err := hestia.DecodeConfigFile(out(cmd, "json"))
		if err != nil || f.CONFIG[key] == "" || f.CONFIG["config_path"] == "" {
			t.Errorf("%s = %+v, %v", cmd, f, err)
		}
	}
	php, _ := hestia.DecodeConfigFile(out("v-list-sys-php-config", "json"))
	for _, cmd := range []string{"v-list-sys-spamd-config", "v-list-sys-clamd-config", "v-list-sys-vsftpd-config", "v-list-sys-proftpd-config"} {
		if p := strings.TrimSpace(string(out(cmd, "plain"))); !strings.HasPrefix(p, "/etc/") {
			t.Errorf("%s plain = %q", cmd, p)
		}
	}

	// Every editor path opens.
	paths := []string{"/etc/nginx/nginx.conf", "/etc/apache2/apache2.conf", php.CONFIG["config_path"], "/etc/mysql/my.cnf",
		"/etc/postgresql/16/main/pg_hba.conf", "/etc/postgresql/16/main/postgresql.conf", "/etc/bind/named.conf.options",
		"/etc/bind/named.conf", "/etc/exim4/exim4.conf.template", "/etc/fail2ban/jail.local", "/etc/spamassassin/local.cf",
		"/etc/clamav/clamd.conf", "/etc/vsftpd.conf", "/etc/ssh/sshd_config", "/etc/crontab",
		"/var/spool/cron/crontabs/hestiaweb", "/etc/dovecot/dovecot.conf", "/etc/dovecot/conf.d/10-auth.conf",
		"/etc/dovecot/conf.d/auth-passwdfile.conf.ext", "/etc/php/8.3/fpm/php.ini"}
	// Upstream prints the file, then exits 127 (its $BIN/v-log-action call fails).
	for _, p := range paths {
		if res := run("v-open-fs-config", p); res.Exit != 127 || len(res.Stdout) == 0 || !openFsConfigRead(res) {
			t.Errorf("v-open-fs-config %s: %+v", p, res)
		}
	}
	if res := run("v-open-fs-config", "/etc/missing.conf"); res.Exit != 3 {
		t.Errorf("missing file: %+v", res)
	}

	for _, kind := range []string{"cpu", "memory", "disk", "network", "web", "dns", "mail", "db"} {
		if len(out("v-list-sys-"+kind+"-status")) == 0 {
			t.Errorf("%s status is empty", kind)
		}
	}

	charts, err := hestia.DecodeObjects[hestia.RrdChart](out("v-list-sys-rrd", "json"))
	if err != nil || len(charts) != 7 {
		t.Fatalf("rrd = %+v, %v", charts, err)
	}
	for _, c := range charts {
		id := c.RRD
		if c.TYPE == "net" {
			id = "net_" + c.RRD
		}
		if !hestia.ValidChartID(id) {
			t.Errorf("chart id %q", id)
		}
		for _, period := range []string{"daily", "weekly", "monthly", "yearly", "biennially", "triennially"} {
			e, err := hestia.DecodeRrdExport(out("v-export-rrd", id, period))
			if err != nil || len(e.Data) == 0 || len(e.Meta.Legend) == 0 || len(e.Data[0]) != len(e.Meta.Legend) {
				t.Errorf("export %s %s = %+v, %v", id, period, e.Meta, err)
			}
		}
	}

	ups, err := hestia.DecodeObjects[hestia.HestiaUpdate](out("v-list-sys-hestia-updates", "json"))
	if err != nil || len(ups) != 3 || ups[1].UPDATED != "no" {
		t.Errorf("updates = %+v, %v", ups, err)
	}
	if on, err := hestia.ParseAutoupdate(out("v-list-sys-hestia-autoupdate", "plain")); err != nil || !on {
		t.Errorf("autoupdate = %v, %v", on, err)
	}

	ssl, err := hestia.DecodeObjects[hestia.HestiaSSL](out("v-list-sys-hestia-ssl", "json"))
	if err != nil || len(ssl) != 1 || !strings.Contains(ssl[0].KEY, "PRIVATE KEY") {
		t.Errorf("ssl = %+v, %v", ssl, err)
	}

	sftp, err := hestia.DecodeOptional[hestia.BackupHost](out("v-list-backup-host", "sftp", "json"))
	if err != nil || len(sftp) != 1 || sftp[0].HOST == "" {
		t.Errorf("sftp = %+v, %v", sftp, err)
	}
	for _, typ := range []string{"ftp", "b2", "rclone"} {
		if hosts, err := hestia.DecodeOptional[hestia.BackupHost](out("v-list-backup-host", typ, "json")); err != nil || len(hosts) != 0 {
			t.Errorf("%s = %+v, %v", typ, hosts, err)
		}
	}
	if r, err := hestia.DecodeOptional[hestia.ResticHost](out("v-list-backup-host-restic", "json")); err != nil || len(r) != 1 {
		t.Errorf("restic = %+v, %v", r, err)
	}
	if d, err := hestia.DecodeOptional[hestia.RemoteDNSHost](out("v-list-remote-dns-hosts", "json")); err != nil {
		t.Errorf("dns cluster = %+v, %v", d, err)
	}
	for _, cmd := range []string{"v-get-sys-timezones", "v-list-sys-interfaces"} {
		if l, err := hestia.DecodeStringList(out(cmd, "json")); err != nil || len(l) == 0 {
			t.Errorf("%s = %v, %v", cmd, l, err)
		}
	}

	ips, _ := hestia.DecodeObjects[hestia.SysIP](out("v-list-sys-ips", "json"))
	for _, ip := range ips {
		one, err := hestia.DecodeObjects[hestia.SysIP](out("v-list-sys-ip", ip.IP, "json"))
		if err != nil || len(one) != 1 || one[0] != ip {
			t.Errorf("v-list-sys-ip %s = %+v, %v; want %+v", ip.IP, one, err, ip)
		}
	}
	if res := run("v-list-sys-ip", "192.0.2.1", "json"); res.Exit != 3 {
		t.Errorf("unknown IP: %+v", res)
	}

	rules, err := hestia.DecodeObjects[hestia.FirewallRule](out("v-list-firewall", "json"))
	if err != nil || len(rules) != 10 || rules[1].RULE != "2" || rules[1].COMMENT != "HESTIA" || rules[1].PORT != "8083" ||
		rules[9].RULE != "10" || rules[9].COMMENT != "SSH" || rules[9].PORT != "22" {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	for _, r := range rules {
		one, err := hestia.DecodeObjects[hestia.FirewallRule](out("v-list-firewall-rule", r.RULE, "json"))
		if err != nil || len(one) != 1 || one[0] != r {
			t.Errorf("rule %s = %+v, %v", r.RULE, one, err)
		}
	}
	bans, err := hestia.DecodeBans(out("v-list-firewall-ban", "json"))
	var chains []string
	for _, b := range bans {
		if b.IP == "203.0.113.9" {
			chains = append(chains, b.CHAIN)
		}
	}
	if err != nil || !slices.Equal(chains, []string{"SSH", "MAIL"}) {
		t.Errorf("bans = %+v, %v", bans, err)
	}
	if sets, err := hestia.DecodeObjects[hestia.FirewallIpset](out("v-list-firewall-ipset", "json")); err != nil || len(sets) != 3 {
		t.Errorf("ipsets = %+v, %v", sets, err)
	}
	if p, err := hestia.ParseSSHDPorts(out("v-list-sys-sshd-port", "plain")); err != nil || !slices.Equal(p, []string{"22"}) {
		t.Errorf("sshd = %v, %v", p, err)
	}

	// Write sentinels.
	if res := run("v-add-backup-host", "sftp", "bad.example", "u", "/tmp/pw", "/backup", "22"); res.Exit != 15 {
		t.Errorf("bad backup host: %+v", res)
	}
	if res := run("v-add-backup-host", "sftp", "good.example", "u", "/tmp/pw", "/backup", "22"); res.Exit != 0 {
		t.Errorf("good backup host: %+v", res)
	}
	if res := run("v-add-firewall-ban", "127.0.0.1", "SSH"); res.Exit != 0 {
		t.Errorf("ban: %+v", res)
	}
	for _, b := range bans {
		if b.IP == "127.0.0.1" {
			t.Error("127.0.0.1 is listed")
		}
	}
	if res := run("v-restart-system", "yes"); res.Exit != 0 {
		t.Errorf("reboot: %+v", res)
	}
}

// TestSysFirewallEnableCheckNilRefuses: until Slice D sets the seam, turning the
// firewall on fails closed with the lockout error (decision 37).
func TestSysFirewallEnableCheckNilRefuses(t *testing.T) {
	sys := &sysAPI{server: &server{}}
	r := httptest.NewRequest("PUT", "/api/v1/server/settings", nil)
	err := sys.checkFirewallEnable(context.Background(), r)
	var f *jobs.Failure
	if !errors.As(err, &f) || f.Code != SysLockoutCode || f.Message != SysLockoutFirewallMsg {
		t.Fatalf("nil seam: err = %v, want lockout failure", err)
	}

	want := errors.New("seam ran")
	sys.firewallEnableCheck = func(context.Context, *http.Request) error { return want }
	if err := sys.checkFirewallEnable(context.Background(), r); err != want {
		t.Fatalf("set seam: err = %v, want %v", err, want)
	}
	sys.firewallEnableCheck = func(context.Context, *http.Request) error { return nil }
	if err := sys.checkFirewallEnable(context.Background(), r); err != nil {
		t.Fatalf("passing seam: err = %v", err)
	}
}
