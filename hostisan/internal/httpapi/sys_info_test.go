package httpapi

import (
	"context"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

var sysInfoDir = filepath.Join("..", "..", "testdata", "fixtures", "sys_info")

// sysInfoServer logs in as admin and serves the server routes.
func sysInfoServer(t *testing.T, o overrides, opts ...func(*Deps)) (*harness, string) {
	t.Helper()
	h := newHarnessWith(t, o, opts...)
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, nil)
	return h, srv.URL
}

func TestSysStatusWebHasNoTags(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Dir: sysInfoDir})
	var got StatusText
	if code := h.sysDo(base, "GET", "/api/v1/server/status/web", nil, &got); code != 200 || got.Kind != "web" {
		t.Fatalf("%d %+v", code, got)
	}
	if strings.ContainsAny(got.Text, "<>") && !strings.Contains(got.Text, "<Debian>") {
		t.Errorf("tags left: %q", got.Text)
	}
	for _, bad := range []string{"<h2>", "<script", "<table", "&lt;", "&amp;", "alert(1)", "color:red", "Hestia"} {
		if strings.Contains(got.Text, bad) {
			t.Errorf("%q left in %q", bad, got.Text)
		}
	}
	for _, want := range []string{"APACHE2 STATUS", "Apache/2.4.62 <Debian>", "a & b", "Hostisan"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("missing %q in %q", want, got.Text)
		}
	}
}

func TestSysStatusMailFailureShowsMessage(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Dir: sysInfoDir})
	var got StatusText
	if code := h.sysDo(base, "GET", "/api/v1/server/status/mail", nil, &got); code != 200 || got.Text != "Error: exim4 is not running" {
		t.Fatalf("%d %+v", code, got)
	}
}

func TestSysStatusPlainKinds(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	want := map[string]string{"cpu": "v-list-sys-cpu-status", "mem": "v-list-sys-memory-status", "disk": "v-list-sys-disk-status",
		"net": "v-list-sys-network-status", "dns": "v-list-sys-info json\nv-list-sys-dns-status", "mail": "v-list-sys-mail-status", "db": "v-list-sys-db-status"}
	for kind, cmd := range want {
		from := len(h.fixture.Calls())
		var got StatusText
		if code := h.sysDo(base, "GET", "/api/v1/server/status/"+kind, nil, &got); code != 200 || got.Text == "" {
			t.Errorf("%s: %d %+v", kind, code, got)
		}
		if ran := h.sysCommands(from); strings.Join(ran, "\n") != cmd+" " {
			t.Errorf("%s ran %q", kind, ran)
		}
	}
}

func TestSysStatusNotInstalledAndUnknown(t *testing.T) {
	cfg := map[string]string{"DNS_SYSTEM": "", "WEB_SYSTEM": "", "MAIL_SYSTEM": "", "DB_SYSTEM": ""}
	h, base := sysInfoServer(t, overrides{Config: cfg})
	for _, kind := range []string{"dns", "web", "mail", "db", "bogus", "CPU"} {
		from := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "GET", "/api/v1/server/status/"+kind, nil, &e); code != 404 {
			t.Errorf("%s: %d", kind, code)
		}
		if ran := h.sysCommands(from); len(ran) != 0 {
			t.Errorf("%s ran %q", kind, ran)
		}
	}
	var got StatusText
	if code := h.sysDo(base, "GET", "/api/v1/server/status/cpu", nil, &got); code != 200 {
		t.Errorf("cpu with nothing installed: %d", code)
	}
}

func TestSysStatusSystemNoIsNotInstalled(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Config: map[string]string{"DNS_SYSTEM": "no"}})
	from := len(h.fixture.Calls())
	if code := h.sysDo(base, "GET", "/api/v1/server/status/dns", nil, nil); code != 404 {
		t.Errorf("dns with DNS_SYSTEM=no: %d", code)
	}
	if ran := h.sysCommands(from); len(ran) != 0 {
		t.Errorf("ran %q", ran)
	}
}

func TestSysTruncateTextInvalidByte(t *testing.T) {
	in := strings.Repeat("é", 350000) + "\xff" + strings.Repeat("é", 350000)
	start := time.Now()
	got := truncateText(in, 1<<20)
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
	if len(got) < (1<<20)-3 || len(got) > 1<<20 || !strings.Contains(got, "\xff") || !strings.HasSuffix(got, "é") {
		t.Errorf("len = %d", len(got))
	}
	if truncateText("abc", 10) != "abc" {
		t.Error("short text changed")
	}
}

func TestSysStatusBrandsText(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-cpu-status":  {{Args: []string{}, Stdout: "Hestia Control Panel load\n"}},
		"v-list-sys-mail-status": {{Args: []string{}, Stdout: "Error: Hestia exim is down\n", Exit: 1}},
	}})
	for kind, want := range map[string]string{"cpu": "Hostisan load\n", "mail": "Error: Hostisan exim is down"} {
		var got StatusText
		if code := h.sysDo(base, "GET", "/api/v1/server/status/"+kind, nil, &got); code != 200 || got.Text != want {
			t.Errorf("%s: %d %q", kind, code, got.Text)
		}
	}
}

func TestSysStatusWebFailureStripped(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-web-status": {{Args: []string{}, Stdout: "<b>boom</b>\n", Exit: 1}},
	}})
	var got StatusText
	if code := h.sysDo(base, "GET", "/api/v1/server/status/web", nil, &got); code != 200 || got.Text != "boom" {
		t.Errorf("%d %q", code, got.Text)
	}
}

func TestSysStatusTimeoutAndCap(t *testing.T) {
	var seen time.Duration
	big := strings.Repeat("é", 700000) // 1.4 MB
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-cpu-status": {{Args: []string{}, Stdout: big}},
	}}, func(d *Deps) { d.R = sysTimeoutSpy{Runner: d.R, cmd: "v-list-sys-cpu-status", seen: &seen} })
	var got StatusText
	if code := h.sysDo(base, "GET", "/api/v1/server/status/cpu", nil, &got); code != 200 {
		t.Fatalf("%d", code)
	}
	if seen != 30*time.Second {
		t.Errorf("timeout = %v", seen)
	}
	if len(got.Text) > 1<<20 || len(got.Text) < (1<<20)-3 || strings.ContainsRune(got.Text, '�') {
		t.Errorf("len = %d", len(got.Text))
	}
}

type sysTimeoutSpy struct {
	runner.Runner
	cmd  string
	seen *time.Duration
}

func (r sysTimeoutSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == r.cmd {
		*r.seen, _ = runner.Timeout(ctx)
	}
	return r.Runner.Run(ctx, cmd, args...)
}

func TestSysGraphCatalog(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	var got GraphCatalog
	from := len(h.fixture.Calls())
	if code := h.sysDo(base, "GET", "/api/v1/server/graphs", nil, &got); code != 200 || got.Error != "" {
		t.Fatalf("%d %+v", code, got)
	}
	var ids []string
	for _, c := range got.Charts {
		ids = append(ids, c.ID)
	}
	if !slices.Equal(ids, []string{"la", "mem", "net_eth0", "nginx", "mail", "mysql_localhost", "ssh"}) {
		t.Errorf("ids = %v", ids)
	}
	if got.Charts[2].Type != "net" || got.Charts[2].Title != "Bandwidth Usage eth0" {
		t.Errorf("chart = %+v", got.Charts[2])
	}
	if ran := h.sysCommands(from); !slices.Equal(ran, []string{"v-list-sys-rrd json"}) {
		t.Errorf("ran %q", ran)
	}
}

func TestSysGraphCatalogTitleBranded(t *testing.T) {
	rrd := `{"1":{"TYPE":"la","RRD":"la","TITLE":"Hestia Load Average"}}`
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-rrd": {{Args: []string{"json"}, Stdout: rrd}},
	}})
	var got GraphCatalog
	if code := h.sysDo(base, "GET", "/api/v1/server/graphs", nil, &got); code != 200 || len(got.Charts) != 1 || got.Charts[0].Title != "Hostisan Load Average" {
		t.Errorf("%d %+v", code, got)
	}
}

func TestSysGraphCatalogFailure(t *testing.T) {
	for name, e := range map[string]fixtureEntry{
		"exit":     {Args: []string{"json"}, Stdout: "Error: rrd is broken\n", Exit: 99},
		"not json": {Args: []string{"json"}, Stdout: "no charts here"},
	} {
		h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-sys-rrd": {e}}})
		var raw map[string]any
		if code := h.sysDo(base, "GET", "/api/v1/server/graphs", nil, &raw); code != 200 {
			t.Fatalf("%s: %d", name, code)
		}
		charts, ok := raw["charts"].([]any)
		if !ok || len(charts) != 0 || raw["error"] == "" || raw["error"] == nil {
			t.Errorf("%s: %+v", name, raw)
		}
		if name == "exit" && raw["error"] != "Error: rrd is broken" {
			t.Errorf("error = %v", raw["error"])
		}
	}
}

func TestSysGraphDataRejectsBadIDs(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	for _, id := range []string{"la;rm", "..%2Fla", "net_", "net_" + strings.Repeat("a", 16), "mysql_%24x", "LA", "nginx2", "mem%0Ax"} {
		from := len(h.fixture.Calls())
		var e APIError
		code := h.sysDo(base, "GET", "/api/v1/server/graphs/"+id, nil, &e)
		if code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%q: %d %+v", id, code, e)
		}
		if ran := h.sysCommands(from); len(ran) != 0 {
			t.Errorf("%q ran %q", id, ran)
		}
	}
	// Valid grammar, not in the catalogue: 400, and nothing is exported.
	for _, id := range []string{"net_eth9", "apache2", "pgsql_localhost"} {
		from := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "GET", "/api/v1/server/graphs/"+id, nil, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%q: %d %+v", id, code, e)
		}
		if ran := h.sysCommands(from); !slices.Equal(ran, []string{"v-list-sys-rrd json"}) {
			t.Errorf("%q ran %q", id, ran)
		}
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSysGraphDataUnitsAndScaling(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	get := func(id, query string) (GraphData, []string) {
		t.Helper()
		from := len(h.fixture.Calls())
		var g GraphData
		if code := h.sysDo(base, "GET", "/api/v1/server/graphs/"+id+query, nil, &g); code != 200 {
			t.Fatalf("%s: %d", id, code)
		}
		return g, h.sysCommands(from)
	}

	g, ran := get("la", "?period=weekly")
	if !slices.Equal(ran, []string{"v-list-sys-rrd json", "v-export-rrd la weekly"}) {
		t.Errorf("ran %q", ran)
	}
	raw, _ := h.fixture.Run(context.Background(), "v-export-rrd", "la", "weekly")
	if g.ID != "la" || g.Period != "weekly" || g.Unit != "Load" || !slices.Equal(g.Legend, []string{"Load", "Processes"}) {
		t.Fatalf("la = %+v", g)
	}
	if g.Start == 0 || g.Step == 0 || len(g.Series) == 0 || len(raw.Stdout) == 0 {
		t.Fatalf("la = %+v", g)
	}
	// The dev fixture's first row of the daily export is [49.5, 292.85].
	d, _ := get("la", "?period=daily")
	if !approx(*d.Series[0][0], 0.495) || !approx(*d.Series[0][1], 292.85) {
		t.Errorf("first row = %v %v", *d.Series[0][0], *d.Series[0][1])
	}

	for id, unit := range map[string]string{"mem": "MB", "net_eth0": "Bytes/s", "nginx": "Connections", "mail": "Emails in queue",
		"mysql_localhost": "Queries/s", "ssh": "Connections"} {
		if g, _ := get(id, ""); g.Unit != unit || g.Period != "daily" {
			t.Errorf("%s: unit %q period %q", id, g.Unit, g.Period)
		}
	}
	// httpd has a unit too.
	if graphUnit("httpd") != "Connections" || graphUnit("apache2") != "Connections" || graphUnit("ftp") != "Connections" ||
		graphUnit("pgsql_db1") != "Queries/s" {
		t.Error("units")
	}

	// An unknown or hostile period runs as daily.
	for _, q := range []string{"?period=hourly", "?period=daily;rm", "?period=", "?period=Weekly"} {
		g, ran := get("mem", q)
		if g.Period != "daily" || !slices.Equal(ran, []string{"v-list-sys-rrd json", "v-export-rrd mem daily"}) {
			t.Errorf("%s: %q %q", q, g.Period, ran)
		}
	}
	for _, p := range []string{"monthly", "yearly", "biennially", "triennially"} {
		if g, _ := get("mem", "?period="+p); g.Period != p {
			t.Errorf("period %s -> %s", p, g.Period)
		}
	}
}

func TestSysGraphDataKeepsNulls(t *testing.T) {
	export := `{"meta":{"start":100,"step":300,"end":1000,"legend":["Load","Processes"]},"data":[[null,2],[150,null],[200,3]]}`
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-export-rrd": {{Args: []string{"la", "daily"}, Stdout: export}},
	}})
	var raw struct {
		Series [][]*float64 `json:"series"`
		Start  int64        `json:"start"`
		Step   int64        `json:"step"`
	}
	if code := h.sysDo(base, "GET", "/api/v1/server/graphs/la", nil, &raw); code != 200 {
		t.Fatalf("%d", code)
	}
	if raw.Start != 100 || raw.Step != 300 || len(raw.Series) != 3 {
		t.Fatalf("%+v", raw)
	}
	s := raw.Series
	if s[0][0] != nil || *s[0][1] != 2 || !approx(*s[1][0], 1.5) || s[1][1] != nil || !approx(*s[2][0], 2) {
		t.Errorf("series = %v", s)
	}
}

func TestSysGraphDataExportFailure(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-export-rrd": {{Args: []string{"*", "*"}, Stdout: "Does not exist\n", Exit: 1}},
	}})
	var e APIError
	if code := h.sysDo(base, "GET", "/api/v1/server/graphs/la", nil, &e); code != 400 || e.Error.Message != "Does not exist" {
		t.Errorf("%d %+v", code, e)
	}
}

func TestSysPreview(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	from := len(h.fixture.Calls())
	var got PreviewFeatures
	if code := h.sysDo(base, "GET", "/api/v1/server/preview", nil, &got); code != 200 || len(got.Rows) != 1 ||
		got.Rows[0] != (PreviewFeature{Category: "System", Name: "Policy: Allow suspended users to log in with read-only access", Status: "Partially implemented"}) {
		t.Fatalf("%d %+v", code, got)
	}
	if ran := h.sysCommands(from); len(ran) != 0 {
		t.Errorf("ran %q", ran)
	}
}

func TestSysInfoRoutesAreGuarded(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	srv, _, _ := sysTestServer(h, nil)
	for _, p := range []string{"/status/cpu", "/graphs", "/graphs/la", "/updates", "/white-label", "/preview"} {
		from := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(srv.URL, "GET", "/api/v1/server"+p, nil, &e); code != http.StatusForbidden {
			t.Errorf("%s: %d", p, code)
		}
		if ran := h.sysCommands(from); len(ran) != 0 {
			t.Errorf("%s ran %q", p, ran)
		}
	}
}

// rhelSysInfo is v-list-sys-info on a RHEL-family server (/etc/sysconfig exists).
func rhelSysInfo(os string) map[string][]fixtureEntry {
	out := `{"sysinfo":{"HOSTNAME":"rhel.example.net","OS":"` + os + `","VERSION":"9.4","ARCH":"x86_64","HESTIA":"1.10.5","RELEASE":"release","UPTIME":"5","LOADAVERAGE":"0.1 / 0.1 / 0.1"}}`
	return map[string][]fixtureEntry{"v-list-sys-info": {{Args: []string{"json"}, Stdout: out}}}
}

// On RHEL v-list-sys-dns-status rewrites named.conf and restarts named (owner ruling 1:
// hidden until Plan 6), so the API refuses it without running it.
func TestSysStatusDNSRefusedOnRHEL(t *testing.T) {
	for _, os := range []string{"CentOS", "Amazon"} {
		h, base := sysInfoServer(t, overrides{Fixtures: rhelSysInfo(os)})
		from := len(h.fixture.Calls())
		if code := h.sysDo(base, "GET", "/api/v1/server/status/dns", nil, nil); code != 404 {
			t.Errorf("%s: dns status %d", os, code)
		}
		if ran := h.sysCommands(from); slices.Contains(ran, "v-list-sys-dns-status ") {
			t.Errorf("%s ran %q", os, ran)
		}
	}
}

func TestSysSettingsDNSStatusOffOnRHEL(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: rhelSysInfo("CentOS")})
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, nil)
	if v, _ := sysSettingsGet(t, h, srv); v.DNSStatus {
		t.Error("dnsStatus on RHEL")
	}
	h2 := newHarnessWith(t, overrides{})
	h2.loginAdmin()
	srv2, _, _ := sysTestServer(h2, nil)
	if v, _ := sysSettingsGet(t, h2, srv2); !v.DNSStatus {
		t.Error("dnsStatus off on Debian")
	}
}
