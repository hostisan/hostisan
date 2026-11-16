package httpapi

// Contract between ui/src/api/server.ts and the Go server-admin routes: every call the
// UI makes reaches a registered route, every registered route is used by the UI, and
// every body the UI sends decodes into its Go struct with no unknown fields.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// sysPathParam matches a path parameter in either spelling: {name} in a Go pattern,
// ${...} in a TS template literal.
var sysPathParam = regexp.MustCompile(`\$\{[^}]*\}|\{[^}]*\}`)

// sysShape replaces every path parameter with {} so a TS call and a Go pattern compare
// by shape ("POST /api/v1/server/services/{}/{}").
func sysShape(call string) string {
	return sysPathParam.ReplaceAllString(call, "{}")
}

// sysUICalls reads ui/src/api/server.ts and returns every call as "METHOD /api/v1/path"
// with its template parameters left as ${...}. The job poller goes through
// useJobStatus, not api.*, so it is read separately. A call whose path it can't
// resolve fails the test, so a new call shape has to be taught here.
func sysUICalls(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "src", "api", "server.ts"))
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]string{"get": "GET", "post": "POST", "put": "PUT", "del": "DELETE"}
	call := regexp.MustCompile(`api\.(get|post|put|del)<[^>]*>\(\s*('[^']*'|` + "`[^`]*`" + `|[^,)]+)`)
	var out []string
	for _, m := range call.FindAllStringSubmatch(string(src), -1) {
		arg := m[2]
		if arg[0] != '\'' && arg[0] != '`' {
			t.Fatalf("server.ts: can't resolve the path of %q", m[0])
		}
		path, _, _ := strings.Cut(arg[1:len(arg)-1], "?")
		if !strings.HasPrefix(path, "/server/") {
			t.Fatalf("server.ts: path outside /server: %q", m[0])
		}
		out = append(out, methods[m[1]]+" /api/v1"+path)
	}
	job := regexp.MustCompile("useJobStatus\\([^`]*`([^`]*)`")
	jm := job.FindStringSubmatch(string(src))
	if jm == nil {
		t.Fatal("server.ts: no job poller found")
	}
	out = append(out, "GET /api/v1"+jm[1])
	if len(out) < 2 {
		t.Fatal("server.ts: no api calls found")
	}
	return out
}

// sysUIPatterns maps every UI call to the one registered pattern of the same shape.
func sysUIPatterns(t *testing.T, patterns []string) map[string]string {
	t.Helper()
	byShape := map[string][]string{}
	for _, p := range patterns {
		byShape[sysShape(p)] = append(byShape[sysShape(p)], p)
	}
	out := map[string]string{}
	for _, c := range sysUICalls(t) {
		ps := byShape[sysShape(c)]
		if len(ps) != 1 {
			t.Errorf("server.ts %q: registered patterns of its shape %q, want exactly one", c, ps)
			continue
		}
		out[c] = ps[0]
	}
	return out
}

func TestSysContractUICallsCoverEveryRoute(t *testing.T) {
	h := newHarness(t)
	_, _, patterns := sysTestServer(h, nil)
	used := map[string]bool{}
	for _, p := range sysUIPatterns(t, patterns) {
		used[p] = true
	}
	for _, p := range patterns {
		if !used[p] {
			t.Errorf("route %q is registered but ui/src/api/server.ts never calls it", p)
		}
	}
}

func TestSysContractUICallsAreRegistered(t *testing.T) {
	h := newHarness(t)
	mux := http.NewServeMux()
	(&server{Deps: h.deps}).registerSysRoutes(mux)
	_, _, patterns := sysTestServer(h, nil)
	calls := sysUIPatterns(t, patterns)
	if len(calls) == 0 {
		t.Fatal("no calls")
	}
	for c, pattern := range calls {
		method, path := sysConcrete(t, pattern)

		// The real registration resolves the concrete path to exactly this pattern (not,
		// say, {ip} for /ips/bulk).
		if _, got := mux.Handler(httptest.NewRequest(method, path, nil)); got != pattern {
			t.Errorf("%s (%s %s): matched %q, want %q", c, method, path, got, pattern)
		}

		// The real server has it wired: signed out, it answers 401, not the JSON 404
		// catch-all or a 405.
		req, _ := http.NewRequest(method, h.srv.URL+path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: status %d, want 401 from a registered route", method, path, res.StatusCode)
		}
	}
}

// The bodies below are copied from the UI tests' assertions (ui/src/designs/server/*.test.tsx).
func TestSysContractUIBodiesDecode(t *testing.T) {
	cases := []struct {
		name string
		into any
		body string
	}{
		{"settings (ServerConfigPage)", &SettingsRequest{}, `{"hostname":"server1.acme-hosting.net","timezone":"UTC",
			"themeForAll":false,"language":"en","phpVersions":["8.2","8.4"],"defaultPhp":"8.2",
			"protectedAdmin":false,"hideAdmin":true,"hideServices":false,"smtpRelayPassword":"relay-secret",
			"mysqlPassword":"root-secret","backupPassword":"backup-secret","sslKey":"-----BEGIN PRIVATE KEY-----",
			"b2Key":"b2-secret","backupRemote":{"type":"b2","bucket":"bk","keyId":"id1"},"policies":{"viewSuspended":true}}`},
		{"settings, remote removed", &SettingsRequest{}, `{"backupRemote":{"type":""}}`},
		{"editor save, basic (ServiceEditPage)", &EditorSaveRequest{}, `{"files":[],"directives":{"worker_connections":"2048"},"restart":true}`},
		{"editor save, advanced", &EditorSaveRequest{}, `{"files":[{"id":"nginx","content":"events {}\n","hash":"` +
			strings.Repeat("a", 64) + `"}],"restart":false}`},
		{"service action (ServicesPage)", &ServiceActionRequest{}, `{}`},
		{"service stop with confirm", &ServiceActionRequest{}, `{"confirm":"ssh"}`},
		{"services bulk", &BulkRequest{}, `{"action":"start","names":["php8.3-fpm","vsftpd"]}`},
		{"reboot", &RebootRequest{}, `{"hostname":"server1.acme-hosting.net"}`},
		{"IP add (IpFormPage)", &IPRequest{}, `{"ip":"203.0.113.20","netmask":"255.255.255.0","interface":"eth0",
			"shared":true,"owner":"admin","name":"","nat":""}`},
		{"IP edit", &IPRequest{}, `{"ip":"10.0.0.5","netmask":"255.255.255.0","interface":"eth0","shared":false,
			"owner":"bob","name":"shop.example.com","nat":"198.51.100.7"}`},
		{"IP bulk (IpListPage)", &BulkRequest{}, `{"action":"delete","names":["10.0.0.5"]}`},
		{"rule add (FirewallRuleFormPage)", &FirewallRuleRequest{}, `{"action":"DROP","protocol":"TCP","port":"8080",
			"ip":"203.0.113.0/24","comment":"Office"}`},
		{"rule edit", &FirewallRuleRequest{}, `{"action":"ACCEPT","protocol":"TCP","port":"80,443,8443","ip":"0.0.0.0/0",
			"comment":"WEB","expect":"expect-9"}`},
		{"rule action", &RuleActionRequest{}, `{"expect":"expect-9"}`},
		{"rule bulk (FirewallRulesPage)", &RuleBulkRequest{}, `{"action":"delete","items":[{"id":"9","expect":"expect-9"},
			{"id":"8","expect":"expect-8"}]}`},
		{"firewall toggle", &FirewallToggleRequest{}, `{"enabled":true}`},
		{"ban add (FirewallBanlistPage)", &BanRequest{}, `{"ip":"198.51.100.7","chain":"MAIL"}`},
		{"ban delete", &BanDeleteRequest{}, `{"items":[{"ip":"203.0.113.9","chain":"MAIL"},{"ip":"198.51.100.0/24","chain":"WEB"}]}`},
		{"ipset add (FirewallIpsetsPage)", &IpsetRequest{}, `{"name":"country-cn",
			"source":"https://raw.githubusercontent.com/ipverse/rir-ip/master/country/cn/ipv4-aggregated.txt",
			"ipVersion":"v4","autoupdate":true}`},
		{"ipset bulk", &BulkRequest{}, `{"action":"delete","names":["blacklist"]}`},
		{"white label (WhiteLabelPage)", &WhiteLabelRequest{}, `{"appName":"Acme Panel","title":"{{page}} - {{hostname}} - {{appname}}",
			"fromName":"","fromEmail":"","subjectEmail":"{{subject}}","hideDocs":false,"updateLogo":true}`},
		{"autoupdate (UpdatesPage)", &AutoupdateRequest{}, `{"enabled":false}`},
	}
	for _, c := range cases {
		dec := json.NewDecoder(bytes.NewReader([]byte(c.body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(c.into); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestSysContractShape(t *testing.T) {
	if got := sysShape("POST /api/v1/server/services/${enc(service)}/${action}"); got != "POST /api/v1/server/services/{}/{}" {
		t.Fatal(got)
	}
	if got := sysShape("POST /api/v1/server/services/{service}/{action}"); got != "POST /api/v1/server/services/{}/{}" {
		t.Fatal(got)
	}
	if !slices.Contains(sysUICalls(t), "GET /api/v1/server/jobs/${enc(id)}") {
		t.Fatal("job poller not read")
	}
}
