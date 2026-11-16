package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

func webDomainsOverrides(o overrides) overrides {
	o.Dir = "../../testdata/fixtures/web_domains"
	return o
}

func webDomainsHarness(t *testing.T, o overrides) *harness {
	t.Helper()
	return newHarnessWith(t, webDomainsOverrides(o))
}

func webDomainsCalls(t *testing.T, h *harness, want ...string) {
	t.Helper()
	if got := h.webCalls(); !slices.Equal(got, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", got, want)
	}
}

func webDomainsGet(h *harness, path string) *http.Response {
	h.t.Helper()
	return getNoFollow(h, path, "")
}

var webDomainsRestarts = []string{"v-restart-web", "v-restart-proxy", "v-restart-dns", "v-restart-web-backend"}

func TestWebDomainsList(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var r WebDomains
	if code := h.call("GET", "/api/v1/web", nil, &r); code != 200 {
		t.Fatalf("got %d", code)
	}
	if len(r.Domains) != 2 || r.Domains[0].NAME != "example.com" || r.Domains[1].NAME != "example.net" {
		t.Fatalf("domains = %+v", r.Domains)
	}
	// Only the listed IPs: 203.0.113.11 and 10.0.0.5 are left out.
	if len(r.DisplayIPs) != 1 || r.DisplayIPs["203.0.113.10"] != "203.0.113.10" {
		t.Fatalf("displayIps = %v", r.DisplayIPs)
	}
	want := WebUsage{Domains: "2", DomainsLimit: "unlimited", Suspended: "1", Aliases: "2", AliasesLimit: "unlimited"}
	if r.Usage != want {
		t.Fatalf("usage = %+v, want %+v", r.Usage, want)
	}
	webDomainsCalls(t, h, "v-list-web-domains admin json", "v-list-sys-ips json")

	h = webDomainsHarness(t, overrides{})
	h.loginBob()
	r = WebDomains{}
	if code := h.call("GET", "/api/v1/web", nil, &r); code != 200 {
		t.Fatalf("bob: %d", code)
	}
	if len(r.Domains) != 1 || r.Domains[0].NAME != "bakery.example" || len(r.DisplayIPs) != 1 || r.DisplayIPs["10.0.0.5"] != "198.51.100.7" {
		t.Fatalf("bob: %+v", r)
	}
	// WEB_ALIASES (5) times WEB_DOMAINS (3).
	want = WebUsage{Domains: "1", DomainsLimit: "3", Suspended: "0", Aliases: "1", AliasesLimit: "15"}
	if r.Usage != want {
		t.Fatalf("bob usage = %+v, want %+v", r.Usage, want)
	}
	webDomainsCalls(t, h, "v-list-web-domains bob json", "v-list-sys-ips json")
}

func TestWebDomainsOptionsAdmin(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var o WebOptions
	if code := h.call("GET", "/api/v1/web/options", nil, &o); code != 200 {
		t.Fatalf("got %d", code)
	}
	if len(o.IPs) != 2 || o.IPs[0] != (WebIP{IP: "203.0.113.10", Display: "203.0.113.10"}) {
		t.Fatalf("ips = %+v", o.IPs)
	}
	if o.WebSystem != "apache2" || o.WebBackend != "php-fpm" || o.ProxySystem != "nginx" ||
		o.Nginx || !o.Purge || !o.FTP || !o.DNS || !o.Mail || !o.AppInstaller || !o.CanEditTemplates ||
		!o.CanRebuild || !o.AdminGate || o.FTPPrefix != "admin_" || o.WebRoot != "/home/admin/web/" ||
		strings.Join(o.KeyAlgos, ",") != "ecdsa-256,ecdsa-384,rsa" ||
		strings.Join(o.StatsTypes, ",") != "none,awstats" ||
		strings.Join(o.Templates, ",") != "default,hosting-php,wordpress" ||
		len(o.BackendTemplates) == 0 || len(o.ProxyTemplates) == 0 {
		t.Fatalf("options = %+v", o)
	}
	if len(o.DefaultProxyExt) < 50 || o.DefaultProxyExt[0] != "css" || o.DefaultProxyExt[len(o.DefaultProxyExt)-1] != "webmanifest" {
		t.Fatalf("defaultProxyExt = %v", o.DefaultProxyExt)
	}
	webDomainsCalls(t, h, "v-list-user-ips admin json", "v-list-web-stats json",
		"v-list-web-templates json", "v-list-web-templates-backend json", "v-list-web-templates-proxy json")
}

func TestWebDomainsOptionsUserTemplatesPolicy(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginBob()
	var o WebOptions
	if code := h.call("GET", "/api/v1/web/options", nil, &o); code != 200 {
		t.Fatalf("got %d", code)
	}
	if o.CanEditTemplates || o.CanRebuild || o.AdminGate || len(o.Templates) != 0 || len(o.BackendTemplates) != 0 || len(o.ProxyTemplates) != 0 ||
		o.Templates == nil || o.BackendTemplates == nil || o.ProxyTemplates == nil {
		t.Fatalf("without policy = %+v", o)
	}
	if len(o.IPs) != 2 || o.IPs[0] != (WebIP{IP: "10.0.0.5", Display: "198.51.100.7"}) || o.FTPPrefix != "bob_" {
		t.Fatalf("ips = %+v", o.IPs)
	}
	webDomainsCalls(t, h, "v-list-user-ips bob json", "v-list-web-stats json")

	h = webDomainsHarness(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_WEB_TEMPLATES": "yes"}})
	h.loginBob()
	o = WebOptions{}
	h.call("GET", "/api/v1/web/options", nil, &o)
	if !o.CanEditTemplates || o.CanRebuild || len(o.Templates) != 3 || len(o.BackendTemplates) == 0 || len(o.ProxyTemplates) == 0 {
		t.Fatalf("with policy = %+v", o)
	}
	webDomainsCalls(t, h, "v-list-user-ips bob json", "v-list-web-stats json",
		"v-list-web-templates json", "v-list-web-templates-backend json", "v-list-web-templates-proxy json")
}

func TestWebDomainsOptionsSystems(t *testing.T) {
	h := webDomainsHarness(t, overrides{Config: map[string]string{
		"WEB_SYSTEM": "nginx", "PROXY_SYSTEM": "", "WEB_BACKEND": "", "FTP_SYSTEM": "", "DNS_SYSTEM": "",
		"IMAP_SYSTEM": "", "PLUGIN_APP_INSTALLER": "false"}})
	h.loginAdmin()
	var o WebOptions
	h.call("GET", "/api/v1/web/options", nil, &o)
	if !o.Nginx || !o.Purge || o.FTP || o.DNS || o.Mail || o.AppInstaller || o.WebBackend != "" || o.ProxySystem != "" ||
		len(o.BackendTemplates) != 0 || len(o.ProxyTemplates) != 0 || len(o.Templates) == 0 {
		t.Fatalf("nginx only = %+v", o)
	}
	webDomainsCalls(t, h, "v-list-user-ips admin json", "v-list-web-stats json", "v-list-web-templates json")

	h = webDomainsHarness(t, overrides{Config: map[string]string{"WEB_SYSTEM": "apache2", "PROXY_SYSTEM": ""}})
	h.loginAdmin()
	o = WebOptions{}
	h.call("GET", "/api/v1/web/options", nil, &o)
	if o.Nginx || o.Purge || !o.AppInstaller {
		t.Fatalf("apache only = %+v", o)
	}
}

func TestWebDomainsOptionsPackageLimits(t *testing.T) {
	// A package limit of 0 hides the DNS and Mail boxes even when the systems are on.
	data, err := os.ReadFile("../../testdata/fixtures/dev/v-list-user.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []fixtureEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if len(entries[i].Args) > 0 && entries[i].Args[0] == "bob" {
			entries[i].Stdout = strings.Replace(entries[i].Stdout, `"DNS_DOMAINS": "5"`, `"DNS_DOMAINS": "0"`, 1)
			entries[i].Stdout = strings.Replace(entries[i].Stdout, `"MAIL_DOMAINS": "1"`, `"MAIL_DOMAINS": "0"`, 1)
		}
	}
	h := webDomainsHarness(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-user": entries}})
	h.loginBob()
	var o WebOptions
	h.call("GET", "/api/v1/web/options", nil, &o)
	if o.DNS || o.Mail {
		t.Fatalf("bob = %+v", o)
	}
	// The boxes are ignored on add as well.
	if code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "new.example", IP: "10.0.0.5", DNS: true, Mail: true}, nil); code != 200 {
		t.Fatalf("add: %d", code)
	}
	webDomainsCalls(t, h, "v-list-user-ips bob json", "v-list-web-stats json",
		"v-list-user-ips bob json", "v-add-web-domain bob new.example 10.0.0.5 yes")
}

func TestWebDomainsAddWithDNSAndMail(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginBob()
	var r statusReply
	code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "bakery2.example", IP: "10.0.0.5", DNS: true, Mail: true}, &r)
	if code != 200 || r.Status != "ok" {
		t.Fatalf("%d %+v", code, r)
	}
	// Web gets the internal address, DNS the NAT address.
	webDomainsCalls(t, h,
		"v-list-user-ips bob json",
		"v-add-web-domain bob bakery2.example 10.0.0.5 yes",
		"v-add-dns-domain bob bakery2.example 198.51.100.7         no",
		"v-add-mail-domain bob bakery2.example")
}

func TestWebDomainsAddNoBoxes(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "new.example", IP: "203.0.113.10"}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	webDomainsCalls(t, h, "v-list-user-ips admin json", "v-add-web-domain admin new.example 203.0.113.10 yes")
}

func TestWebDomainsAddNotOfferedIgnoresBoxes(t *testing.T) {
	h := webDomainsHarness(t, overrides{Config: map[string]string{"DNS_SYSTEM": "", "IMAP_SYSTEM": ""}})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "new.example", IP: "203.0.113.10", DNS: true, Mail: true}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	webDomainsCalls(t, h, "v-list-user-ips admin json", "v-add-web-domain admin new.example 203.0.113.10 yes")
}

func TestWebDomainsAddNormalisesDomain(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "  WWW.Example.ORG ", IP: "203.0.113.10"}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	webDomainsCalls(t, h, "v-list-user-ips admin json", "v-add-web-domain admin example.org 203.0.113.10 yes")
}

func TestWebDomainsAddBlankFields(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/web", WebAddRequest{}, &e); code != 400 || e.Error.Code != "invalid" ||
		e.Error.Message != blankFieldMsg || !slices.Equal(e.Error.Args, []string{"Domain", "IP Address"}) {
		t.Fatalf("%d %+v", code, e)
	}
	webDomainsCalls(t, h)
}

func TestWebDomainsAddInvalidInputs(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	for _, req := range []WebAddRequest{
		{Domain: "-h", IP: "203.0.113.10"},
		{Domain: "a b.example", IP: "203.0.113.10"},
		{Domain: "new.example", IP: "-x"},
		{Domain: "new.example", IP: "not an ip"},
	} {
		if code := h.call("POST", "/api/v1/web", req, nil); code != 400 {
			t.Errorf("%+v: %d", req, code)
		}
	}
	webDomainsCalls(t, h)
}

func TestWebDomainsAddDNSFailsAfterWeb(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "dnsfail.example", IP: "203.0.113.10", DNS: true, Mail: true}, &e)
	if code != 409 || e.Error.Code != "exists" || !strings.Contains(e.Error.Message, "DNS domain dnsfail.example exists") {
		t.Fatalf("%d %+v", code, e)
	}
	// The web domain stays; mail is not attempted.
	webDomainsCalls(t, h,
		"v-list-user-ips admin json",
		"v-add-web-domain admin dnsfail.example 203.0.113.10 yes",
		"v-add-dns-domain admin dnsfail.example 203.0.113.10         no")
}

func TestWebDomainsAddWebFailureStops(t *testing.T) {
	h := webDomainsHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-add-web-domain": {{Args: []string{"*", "*", "*", "*"}, Stdout: "Error: Web domain new.example exists\n", Exit: 4}},
	}})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "new.example", IP: "203.0.113.10", DNS: true, Mail: true}, nil); code != 409 {
		t.Fatalf("got %d", code)
	}
	webDomainsCalls(t, h, "v-list-user-ips admin json", "v-add-web-domain admin new.example 203.0.113.10 yes")
}

func TestWebDomainsSimpleActions(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	for _, rt := range []struct{ method, path string }{
		{"DELETE", "/api/v1/web/example.net"},
		{"POST", "/api/v1/web/example.net/suspend"},
		{"POST", "/api/v1/web/example.net/unsuspend"},
		{"POST", "/api/v1/web/example.com/purge-cache"},
	} {
		var r statusReply
		if code := h.call(rt.method, rt.path, nil, &r); code != 200 || r.Status != "ok" {
			t.Errorf("%s %s: %d %+v", rt.method, rt.path, code, r)
		}
	}
	// Unsuspend is the web-only command (Parity 2).
	webDomainsCalls(t, h,
		"v-delete-web-domain admin example.net yes",
		"v-suspend-web-domain admin example.net",
		"v-unsuspend-web-domain admin example.net",
		"v-purge-nginx-cache admin example.com")
}

func TestWebDomainsSuspendAdminOnly(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginBob()
	var e APIError
	for _, p := range []string{"suspend", "unsuspend"} {
		if code := h.call("POST", "/api/v1/web/bakery.example/"+p, nil, &e); code != 403 || e.Error.Code != "forbidden" {
			t.Errorf("%s: %d %+v", p, code, e)
		}
	}
	webDomainsCalls(t, h)
	// Users may delete and purge their own.
	if code := h.call("POST", "/api/v1/web/bakery.example/purge-cache", nil, nil); code != 200 {
		t.Fatalf("purge: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/web/bakery.example", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	webDomainsCalls(t, h, "v-purge-nginx-cache bob bakery.example", "v-delete-web-domain bob bakery.example yes")
}

func TestWebDomainsPurgeFailureIsReported(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/web/missing.example/purge-cache", nil, &e); code != 404 || e.Error.Code != "not_found" ||
		!strings.Contains(e.Error.Message, "missing.example doesn't exist") {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestWebDomainsBulkAdmin(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var r BulkReply
	code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "suspend", Names: []string{"example.com", "example.net"}}, &r)
	if code != 200 || r.Status != "ok" || len(r.Failed) != 0 {
		t.Fatalf("%d %+v", code, r)
	}
	webDomainsCalls(t, h,
		"v-suspend-web-domain admin example.com no",
		"v-suspend-web-domain admin example.net no",
		"v-restart-web", "v-restart-proxy", "v-restart-dns", "v-restart-web-backend")
}

func TestWebDomainsBulkReportsFailedItem(t *testing.T) {
	h := webDomainsHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-suspend-web-domain": {
			{Args: []string{"*", "missing.example", "*"}, Stdout: "Error: web domain missing.example doesn't exist\n", Exit: 3},
			{Args: []string{"*", "*", "*"}},
		}}})
	h.loginAdmin()
	var r BulkReply
	h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "suspend", Names: []string{"missing.example", "example.net"}}, &r)
	if len(r.Failed) != 1 || r.Failed[0].Name != "missing.example" || !strings.Contains(r.Failed[0].Message, "doesn't exist") {
		t.Fatalf("failed = %+v", r.Failed)
	}
	// Every item is attempted, and the restarts still run.
	if got := h.webCalls(); len(got) != 6 || got[1] != "v-suspend-web-domain admin example.net no" {
		t.Fatalf("calls = %q", got)
	}
}

func TestWebDomainsBulkUserActions(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginBob()
	var e APIError
	if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "suspend", Names: []string{"bakery.example"}}, &e); code != 403 {
		t.Fatalf("suspend: %d", code)
	}
	if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "rebuild", Names: []string{"bakery.example"}}, &e); code != 403 {
		t.Fatalf("rebuild: %d", code)
	}
	webDomainsCalls(t, h)
	if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "purge", Names: []string{"bakery.example"}}, nil); code != 200 {
		t.Fatalf("purge: %d", code)
	}
	if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "delete", Names: []string{"bakery.example"}}, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	want := []string{"v-purge-nginx-cache bob bakery.example no"}
	want = append(want, webDomainsRestarts...)
	want = append(want, "v-delete-web-domain bob bakery.example no")
	want = append(want, webDomainsRestarts...)
	webDomainsCalls(t, h, want...)
}

func TestWebDomainsBulkRebuildAdmin(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "rebuild", Names: []string{"example.com"}}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	webDomainsCalls(t, h, append([]string{"v-rebuild-web-domain admin example.com no"}, webDomainsRestarts...)...)
}

func TestWebDomainsBulkPurgeNeedsNginx(t *testing.T) {
	h := webDomainsHarness(t, overrides{Config: map[string]string{"WEB_SYSTEM": "apache2", "PROXY_SYSTEM": ""}})
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "purge", Names: []string{"example.com"}}, &e); code != 400 || e.Error.Code != "invalid" {
		t.Fatalf("%d %+v", code, e)
	}
	webDomainsCalls(t, h)
}

func TestWebDomainsBulkHostileNames(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	for _, n := range []string{"-h", "a b", "../x", strings.Repeat("a", 254)} {
		if code := h.call("POST", "/api/v1/web/bulk", BulkRequest{Action: "delete", Names: []string{"example.com", n}}, nil); code != 400 {
			t.Errorf("%q: %d", n, code)
		}
	}
	webDomainsCalls(t, h)
}

func TestWebDomainsLogs(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var l WebLog
	if code := h.call("GET", "/api/v1/web/example.com/logs?type=access", nil, &l); code != 200 || l.Type != "access" || len(l.Lines) != 5 ||
		!strings.HasPrefix(l.Lines[0], "198.51.100.23 - -") {
		t.Fatalf("access: %d %+v", code, l)
	}
	l = WebLog{}
	if code := h.call("GET", "/api/v1/web/example.com/logs?type=error", nil, &l); code != 200 || l.Type != "error" || len(l.Lines) != 2 ||
		!strings.Contains(l.Lines[0], "wp-login.php") {
		t.Fatalf("error: %d %+v", code, l)
	}
	for _, q := range []string{"", "?type=nonsense", "?type=ERROR"} {
		l = WebLog{}
		if code := h.call("GET", "/api/v1/web/example.com/logs"+q, nil, &l); code != 200 || l.Type != "access" || len(l.Lines) != 5 {
			t.Errorf("%q: %d %+v", q, code, l)
		}
	}
	webDomainsCalls(t, h,
		"v-list-web-domain-accesslog admin example.com", "v-list-web-domain-errorlog admin example.com",
		"v-list-web-domain-accesslog admin example.com", "v-list-web-domain-accesslog admin example.com",
		"v-list-web-domain-accesslog admin example.com")
}

func TestWebDomainsLogsEmptyAndFailing(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	for _, d := range []string{"example.net", "missing.example"} {
		var raw map[string]any
		if code := h.call("GET", "/api/v1/web/"+d+"/logs", nil, &raw); code != 200 {
			t.Fatalf("%s: %d", d, code)
		}
		lines, ok := raw["lines"].([]any)
		if !ok || len(lines) != 0 {
			t.Errorf("%s: lines = %#v", d, raw["lines"])
		}
	}
}

func TestWebDomainsLogsAllowedReadOnly(t *testing.T) {
	h := webDomainsHarness(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var l WebLog
	if code := h.call("GET", "/api/v1/web/example.com/logs", nil, &l); code != 200 || len(l.Lines) != 5 {
		t.Fatalf("%d %+v", code, l)
	}
}

func TestWebDomainsLogDownload(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	res := webDomainsGet(h, "/api/v1/web/example.com/logs/download?type=error")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/octet-stream" ||
		res.Header.Get("Content-Disposition") != `attachment; filename="example.com.error-log.txt"` ||
		!strings.Contains(string(body), "wp-login.php") {
		t.Fatalf("%d %v %q", res.StatusCode, res.Header, body)
	}
	res = webDomainsGet(h, "/api/v1/web/example.com/logs/download?type=weird")
	if res.Header.Get("Content-Disposition") != `attachment; filename="example.com.access-log.txt"` {
		t.Fatalf("headers = %v", res.Header)
	}
	webDomainsCalls(t, h,
		"v-list-web-domain-errorlog admin example.com 5000",
		"v-list-web-domain-accesslog admin example.com 5000")
}

func TestWebDomainsLogDownloadFailure(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	res := webDomainsGet(h, "/api/v1/web/missing.example/logs/download")
	if res.StatusCode != 404 || res.Header.Get("Content-Disposition") != "" {
		t.Fatalf("got %d %v", res.StatusCode, res.Header)
	}
}

func TestWebDomainsSiteDownload(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	res := webDomainsGet(h, "/api/v1/web/example.com/download")
	body, _ := io.ReadAll(res.Body)
	const name = "admin_example.com_2026-10-21_09-41-07.zip"
	if res.StatusCode != 200 || len(body) != 0 ||
		res.Header.Get("X-Accel-Redirect") != accelPrefix+"/backup/"+name ||
		res.Header.Get("Content-Disposition") != `attachment; filename="`+name+`"` ||
		res.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("%d %v %q", res.StatusCode, res.Header, body)
	}
	webDomainsCalls(t, h, "v-dump-site admin example.com full")
}

func TestWebDomainsSiteDownloadFailures(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/web/missing.example/download", nil, &e); code != 404 || e.Error.Code != "not_found" {
		t.Fatalf("%d %+v", code, e)
	}
	h = webDomainsHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-dump-site": {{Args: []string{"*", "*", "*"}, Stdout: "site.zip\n"}}}})
	h.loginAdmin()
	if code := h.call("GET", "/api/v1/web/example.com/download", nil, &e); code != 500 || e.Error.Code != "server_error" {
		t.Fatalf("relative path: %d %+v", code, e)
	}
}

func TestWebDomainsWritesReadOnly(t *testing.T) {
	h := webDomainsHarness(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	routes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/web", WebAddRequest{Domain: "new.example", IP: "203.0.113.10"}},
		{"DELETE", "/api/v1/web/example.com", nil},
		{"POST", "/api/v1/web/example.com/suspend", nil},
		{"POST", "/api/v1/web/example.com/unsuspend", nil},
		{"POST", "/api/v1/web/example.com/purge-cache", nil},
		{"POST", "/api/v1/web/bulk", BulkRequest{Action: "delete", Names: []string{"example.com"}}},
	}
	for _, rt := range routes {
		var e APIError
		if code := h.call(rt.method, rt.path, rt.body, &e); code != 403 || e.Error.Code != "read_only" {
			t.Errorf("%s %s: %d %+v", rt.method, rt.path, code, e)
		}
	}
	webDomainsCalls(t, h)
}

func TestWebDomainsHostileDomain(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	for _, d := range []string{"-h", "a%20b", "x;y%20z", "..%2Fx", strings.Repeat("a", 254)} {
		for _, rt := range []struct{ method, suffix string }{
			{"DELETE", ""}, {"POST", "/suspend"}, {"POST", "/unsuspend"}, {"POST", "/purge-cache"},
			{"GET", "/logs"}, {"GET", "/logs/download"}, {"GET", "/download"},
		} {
			code := h.call(rt.method, "/api/v1/web/"+d+rt.suffix, nil, nil)
			// The mux itself answers 404 for the encoded slash; everything else reaches the handler.
			if code != 400 && !(d == "..%2Fx" && code == 404) {
				t.Errorf("%s %q%s: %d", rt.method, d, rt.suffix, code)
			}
		}
	}
	webDomainsCalls(t, h)
}

func TestWebDomainsImpersonation(t *testing.T) {
	h := webDomainsHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	var r WebDomains
	if code := h.call("GET", "/api/v1/web", nil, &r); code != 200 || len(r.Domains) != 1 || r.Domains[0].NAME != "bakery.example" {
		t.Fatalf("list: %d %+v", code, r)
	}
	if code := h.call("POST", "/api/v1/web", WebAddRequest{Domain: "new.example", IP: "10.0.0.5"}, nil); code != 200 {
		t.Fatalf("add: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/web/bakery.example", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	// Admin keeps admin-only actions while acting as bob.
	if code := h.call("POST", "/api/v1/web/bakery.example/suspend", nil, nil); code != 200 {
		t.Fatalf("suspend: %d", code)
	}
	webDomainsCalls(t, h,
		"v-list-web-domains bob json", "v-list-sys-ips json",
		"v-list-user-ips bob json", "v-add-web-domain bob new.example 10.0.0.5 yes",
		"v-delete-web-domain bob bakery.example yes",
		"v-suspend-web-domain bob bakery.example")
}
