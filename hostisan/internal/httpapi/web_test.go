package httpapi

// Shared web test helpers (Task 1). The slices' own test files use these and must not
// redefine them; any helper a slice adds carries its own prefix (webDomains…, webEdit…,
// webSSL…, webApps…) so the merged package still compiles.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
)

// webCalls is every command run so far, as "cmd arg arg…", without the session's own
// reads (v-list-sys-config, v-list-user, v-list-users, the language list sign-in reads
// and the login commands).
func (h *harness) webCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		switch c.Cmd {
		case "v-list-sys-config", "v-list-user", "v-list-users", "v-list-sys-languages", "v-get-user-salt", "v-check-user-password",
			"v-check-user-hash", "v-check-user-2fa", "v-log-user-login", "v-log-user-logout", "v-log-action":
			continue
		}
		out = append(out, strings.TrimSpace(c.Cmd+" "+strings.Join(c.Args, " ")))
	}
	return out
}

// webWrites is webCalls without the v-list-* reads: the commands that change state.
func (h *harness) webWrites() []string {
	var out []string
	for _, c := range h.webCalls() {
		if !strings.HasPrefix(c, "v-list-") {
			out = append(out, c)
		}
	}
	return out
}

// webServer is the server behind h, for calling handler helpers directly.
func (h *harness) webServer() *server {
	return &server{Deps: h.deps}
}

func TestSplitWebList(t *testing.T) {
	for in, want := range map[string][]string{
		"a, b\nc  d,,":                     {"a", "b", "c", "d"},
		"":                                 {},
		" , \n ":                           {},
		"www.example.com,blog.example.com": {"www.example.com", "blog.example.com"},
		"css\r\njs\tpng":                   {"css", "js", "png"},
	} {
		got := splitWebList(in)
		if got == nil || !slices.Equal(got, want) {
			t.Errorf("splitWebList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestWebKeyAlgo(t *testing.T) {
	for _, c := range []struct{ stored, ssl, want string }{
		{"ecdsa-256", "yes", "ecdsa-256"},
		{"ecdsa-384", "yes", "ecdsa-384"},
		{"rsa", "no", "rsa"},
		{" RSA ", "yes", "rsa"},
		{"ECDSA-384", "no", "ecdsa-384"},
		{"ecdsa", "yes", "ecdsa-256"},
		{"", "yes", "rsa"},
		{"", "no", "ecdsa-256"},
		{"", "", "ecdsa-256"},
		{"dsa", "yes", "rsa"},
		{"dsa", "no", "ecdsa-256"},
	} {
		if got := webKeyAlgo(c.stored, c.ssl); got != c.want {
			t.Errorf("webKeyAlgo(%q, %q) = %q, want %q", c.stored, c.ssl, got, c.want)
		}
	}
}

func TestWebJobKey(t *testing.T) {
	if got := webJobKey("admin", "example.com"); got != "web:admin:example.com" {
		t.Fatalf("webJobKey = %q", got)
	}
}

func TestWebRestart(t *testing.T) {
	for _, c := range []struct {
		name  string
		cfg   map[string]string
		flags webRestartFlags
		want  []string
	}{
		{"none", nil, webRestartFlags{}, nil},
		{"web", nil, webRestartFlags{Web: true}, []string{"v-restart-web"}},
		{"proxy", nil, webRestartFlags{Proxy: true}, []string{"v-restart-proxy"}},
		{"dns", nil, webRestartFlags{DNS: true}, []string{"v-restart-dns"}},
		{"all, in order", nil, webRestartFlags{Web: true, Proxy: true, DNS: true},
			[]string{"v-restart-web", "v-restart-proxy", "v-restart-dns"}},
		{"no proxy system", map[string]string{"PROXY_SYSTEM": ""}, webRestartFlags{Web: true, Proxy: true, DNS: true},
			[]string{"v-restart-web", "v-restart-dns"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarnessWith(t, overrides{Config: c.cfg})
			w := httptest.NewRecorder()
			ok := h.webServer().webRestart(w, httptest.NewRequest("PUT", "/", nil), c.flags)
			if !ok || w.Body.Len() != 0 {
				t.Fatalf("ok=%v, wrote %d %s", ok, w.Code, w.Body)
			}
			if got := h.webCalls(); !slices.Equal(got, c.want) {
				t.Errorf("calls = %q, want %q", got, c.want)
			}
		})
	}
}

func TestWebRestartStopsAtFailure(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-restart-web": {{Args: []string{}, Stdout: "Error: apache2 restart failed\n", Exit: 20}},
	}})
	w := httptest.NewRecorder()
	ok := h.webServer().webRestart(w, httptest.NewRequest("PUT", "/", nil), webRestartFlags{Web: true, Proxy: true, DNS: true})
	if ok || w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "restart_failed") {
		t.Fatalf("ok=%v %d %s", ok, w.Code, w.Body)
	}
	if got := h.webCalls(); !slices.Equal(got, []string{"v-restart-web"}) {
		t.Errorf("calls = %q", got)
	}
}

// The four slice route hooks registerWebRoutes calls exist with this signature (a
// compile-time check; the slices fill them).
var _ = []func(*server, *http.ServeMux){
	(*server).webDomainRoutes, (*server).webEditRoutes, (*server).webSSLRoutes, (*server).webAppRoutes,
}

func TestWebDomainHelper(t *testing.T) {
	h := newHarness(t)
	s := h.webServer()
	w := httptest.NewRecorder()
	d, ok := s.webDomain(w, httptest.NewRequest("GET", "/", nil), "admin", "example.com")
	if !ok || d.NAME != "example.com" || d.IP != "203.0.113.10" || d.SSL_KEY_ALGO != "ecdsa-256" || d.SSL_FORCE != "yes" {
		t.Fatalf("detail = %+v, %v (%d %s)", d, ok, w.Code, w.Body)
	}
	for _, c := range []struct{ user, domain string }{{"admin", "missing.example"}, {"bob", "missing.example"}, {"bob", "example.com"}} {
		w = httptest.NewRecorder()
		if _, ok := s.webDomain(w, httptest.NewRequest("GET", "/", nil), c.user, c.domain); ok || w.Code != http.StatusNotFound {
			t.Errorf("%s %s: ok=%v code=%d", c.user, c.domain, ok, w.Code)
		}
	}

	w = httptest.NewRecorder()
	ssl, ok := s.webDomainSSL(w, httptest.NewRequest("GET", "/", nil), "admin", "example.com")
	if !ok || ssl.SUBJECT != "example.com" || !strings.Contains(ssl.KEY, "PRIVATE KEY") || !strings.Contains(ssl.ISSUER, "Let's Encrypt") {
		t.Fatalf("ssl = %+v, %v (%d)", ssl, ok, w.Code)
	}
	w = httptest.NewRecorder()
	ssl, ok = s.webDomainSSL(w, httptest.NewRequest("GET", "/", nil), "bob", "bakery.example")
	if !ok || ssl.SUBJECT != "bakery.example" || strings.Contains(ssl.ISSUER, "Let's Encrypt") || ssl.CA == "" {
		t.Fatalf("bob ssl = %+v, %v (%d)", ssl, ok, w.Code)
	}
	w = httptest.NewRecorder()
	if _, ok := s.webDomainSSL(w, httptest.NewRequest("GET", "/", nil), "admin", "missing.example"); ok || w.Code != http.StatusNotFound {
		t.Fatalf("missing ssl: ok=%v code=%d", ok, w.Code)
	}
}

// webFixture runs one dev fixture command and fails the test unless it exits 0.
func webFixture(t *testing.T, h *harness, cmd string, args ...string) []byte {
	t.Helper()
	res, err := h.fixture.Run(context.Background(), cmd, args...)
	if err != nil || res.Exit != 0 {
		t.Fatalf("%s %v: %+v, %v", cmd, args, res, err)
	}
	return res.Stdout
}

func TestWebDevFixtures(t *testing.T) {
	h := newHarness(t)

	list := func(user string) []hestia.WebDomain {
		ds, err := hestia.DecodeObjects[hestia.WebDomain](webFixture(t, h, "v-list-web-domains", user, "json"))
		if err != nil {
			t.Fatal(err)
		}
		return ds
	}
	admin := list("admin")
	if len(admin) != 2 || admin[0].NAME != "example.com" || admin[1].NAME != "example.net" {
		t.Fatalf("admin list = %+v", admin)
	}
	if com := admin[0]; com.IP != "203.0.113.10" || com.SSL != "yes" || com.LETSENCRYPT != "yes" ||
		com.ALIAS != "www.example.com,blog.example.com" || com.STATS != "awstats" || com.FTP_USER != "admin_dev" ||
		com.FTP_PATH != "/public_html" || com.BACKEND != "PHP-8_2" || com.PROXY != "default" || com.SUSPENDED != "no" {
		t.Errorf("example.com = %+v", com)
	}
	if net := admin[1]; net.IP != "203.0.113.10" || net.SSL != "no" || net.SUSPENDED != "yes" {
		t.Errorf("example.net = %+v", net)
	}
	bob := list("bob")
	if len(bob) != 1 || bob[0].NAME != "bakery.example" || bob[0].IP != "10.0.0.5" {
		t.Fatalf("bob list = %+v", bob)
	}
	if got := list("ops"); len(got) != 0 {
		t.Errorf("ops list = %+v", got)
	}

	detail := func(user, domain string) hestia.WebDomainDetail {
		ds, err := hestia.DecodeObjects[hestia.WebDomainDetail](webFixture(t, h, "v-list-web-domain", user, domain, "json"))
		if err != nil || len(ds) != 1 || ds[0].NAME != domain {
			t.Fatalf("%s %s: %+v, %v", user, domain, ds, err)
		}
		return ds[0]
	}
	if com := detail("admin", "example.com"); com.SSL_FORCE != "yes" || com.SSL_KEY_ALGO != "ecdsa-256" || com.CUSTOM_DOCROOT != "" {
		t.Errorf("example.com detail = %+v", com)
	}
	if net := detail("admin", "example.net"); net.SSL != "no" || net.SUSPENDED != "yes" {
		t.Errorf("example.net detail = %+v", net)
	}
	bake := detail("bob", "bakery.example")
	if bake.SSL != "yes" || bake.LETSENCRYPT != "no" || bake.REDIRECT != "www.bakery.example" || bake.REDIRECT_CODE != "301" ||
		bake.CUSTOM_DOCROOT != "/home/bob/web/bakery.example/public_html/shop/" || bake.STATS_USER == "" {
		t.Errorf("bakery.example detail = %+v", bake)
	}

	if lines := string(webFixture(t, h, "v-list-web-domain-accesslog", "admin", "example.com")); strings.Count(lines, "\n") < 3 {
		t.Errorf("example.com access log = %q", lines)
	}
	if lines := string(webFixture(t, h, "v-list-web-domain-errorlog", "admin", "example.com", "5000")); strings.TrimSpace(lines) == "" {
		t.Errorf("example.com error log download is empty")
	}
	if lines := string(webFixture(t, h, "v-list-web-domain-accesslog", "admin", "example.net")); strings.TrimSpace(lines) != "" {
		t.Errorf("example.net access log = %q", lines)
	}

	dump := strings.Split(strings.TrimSpace(string(webFixture(t, h, "v-dump-site", "admin", "example.com", "full"))), "\n")
	if len(dump) != 2 || !strings.HasSuffix(dump[0], ".zip") || !strings.HasPrefix(dump[1], "/backup/") || !strings.HasSuffix(dump[1], dump[0]) {
		t.Errorf("v-dump-site = %q", dump)
	}

	out := webFixture(t, h, "v-generate-ssl-cert", "example.com", "admin@example.com", "US", "California",
		"San Francisco", "MyCompany Inc", "IT", "www.example.com", "json", "ecdsa-256")
	for _, k := range []string{`"CRT"`, `"KEY"`, `"CSR"`, `"DIR"`, "BEGIN CERTIFICATE REQUEST"} {
		if !strings.Contains(string(out), k) {
			t.Errorf("v-generate-ssl-cert lacks %s: %s", k, out)
		}
	}

	var stats []string
	if err := json.Unmarshal(webFixture(t, h, "v-list-web-stats", "json"), &stats); err != nil || !slices.Equal(stats, []string{"none", "awstats"}) {
		t.Errorf("v-list-web-stats = %v, %v", stats, err)
	}
}

// missing.example is the "no such web domain" sentinel: every web command that takes
// a user and domain exits 3 for it, whatever else follows.
func TestWebMissingSentinel(t *testing.T) {
	h := newHarness(t)
	cmds := map[string][]string{
		"v-list-web-domain":                {"json"},
		"v-list-web-domain-ssl":            {"json"},
		"v-list-web-domain-accesslog":      {},
		"v-list-web-domain-errorlog":       {"5000"},
		"v-add-web-domain":                 {"203.0.113.10", "yes"},
		"v-delete-web-domain":              {"yes"},
		"v-suspend-web-domain":             {},
		"v-unsuspend-web-domain":           {"no"},
		"v-rebuild-web-domain":             {"no"},
		"v-purge-nginx-cache":              {},
		"v-change-web-domain-ip":           {"203.0.113.10", "no"},
		"v-change-web-domain-tpl":          {"default", "no"},
		"v-change-web-domain-backend-tpl":  {"PHP-8_3"},
		"v-add-fastcgi-cache":              {"2m"},
		"v-delete-fastcgi-cache":           {},
		"v-change-web-domain-proxy-tpl":    {"default", "css,js", "no"},
		"v-add-web-domain-alias":           {"www.missing.example", "no"},
		"v-delete-web-domain-alias":        {"www.missing.example", "no"},
		"v-delete-dns-on-web-alias":        {"www.missing.example", "no"},
		"v-add-web-domain-ssl":             {"/tmp/x", "same", "no"},
		"v-change-web-domain-sslcert":      {"/tmp/x", "no"},
		"v-delete-web-domain-ssl":          {"no"},
		"v-add-web-domain-ssl-force":       {},
		"v-delete-web-domain-ssl-force":    {},
		"v-add-web-domain-ssl-hsts":        {},
		"v-delete-web-domain-ssl-hsts":     {},
		"v-add-web-domain-stats":           {"awstats"},
		"v-change-web-domain-stats":        {"awstats"},
		"v-delete-web-domain-stats":        {},
		"v-add-web-domain-stats-user":      {"stats", "/tmp/x"},
		"v-delete-web-domain-stats-user":   {},
		"v-add-web-domain-ftp":             {"dev", "/tmp/x", "/public_html"},
		"v-change-web-domain-ftp-path":     {"admin_dev", "/"},
		"v-change-web-domain-ftp-password": {"admin_dev", "/tmp/x"},
		"v-delete-web-domain-ftp":          {"admin_dev"},
		"v-change-web-domain-docroot":      {"default"},
		"v-add-web-domain-redirect":        {"www.missing.example", "301"},
		"v-delete-web-domain-redirect":     {},
		"v-dump-site":                      {"full"},
		"v-add-letsencrypt-domain":         {"www.missing.example", "", "ecdsa-256"},
		"v-delete-letsencrypt-domain":      {""},
	}
	for cmd, rest := range cmds {
		args := append([]string{"admin", "missing.example"}, rest...)
		res, err := h.fixture.Run(context.Background(), cmd, args...)
		if err != nil || res.Exit != 3 || !strings.Contains(string(res.Stdout), "missing.example") {
			t.Errorf("%s %v: %+v, %v", cmd, args, res, err)
		}
	}
}

// The web steps that touch DNS and mail have dev entries for their argument shapes.
func TestWebSharedFixtureShapes(t *testing.T) {
	h := newHarness(t)
	webFixture(t, h, "v-add-dns-domain", "admin", "new.example", "203.0.113.10", "", "", "", "", "", "", "", "", "no")
	webFixture(t, h, "v-change-dns-domain-ip", "admin", "blog.example.com", "203.0.113.10")
	webFixture(t, h, "v-change-dns-domain-ip", "admin", "example.com", "203.0.113.10", "no")
	webFixture(t, h, "v-list-dns-domain", "admin", "example.com")
	webFixture(t, h, "v-add-letsencrypt-domain", "admin", "example.com", "www.example.com,blog.example.com", "", "ecdsa-256")
	webFixture(t, h, "v-delete-letsencrypt-domain", "admin", "example.com", "")
	for _, c := range [][]string{{"admin", "blog.example.com", "json"}, {"admin", "nozone.example"}} {
		if res, err := h.fixture.Run(context.Background(), "v-list-dns-domain", c...); err != nil || res.Exit != 3 {
			t.Errorf("v-list-dns-domain %v: %+v, %v (want exit 3)", c, res, err)
		}
	}
	for _, c := range []string{"example.com", "example.net"} {
		webFixture(t, h, "v-list-mail-domain", "admin", c, "json")
	}
}

// webJSONTags is the JSON names of v's fields, in order.
func webJSONTags(v any) []string {
	var out []string
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}

// The wire names are the contract the slices build against (plan Task 1).
func TestWebWireNames(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{WebDomains{}, "domains displayIps usage"},
		{WebUsage{}, "domains domainsLimit suspended aliases aliasesLimit"},
		{WebOptions{}, "ips webSystem webBackend proxySystem nginx purge ftp statsTypes dns mail appInstaller " +
			"canEditTemplates templates backendTemplates proxyTemplates defaultProxyExt canRebuild adminGate ftpPrefix webRoot keyAlgos"},
		{WebIP{}, "ip display"},
		{WebAddRequest{}, "domain ip dns mail"},
		{WebDomainView{}, "domain ssl aliases displayIp keyAlgo ftp docroot redirect"},
		{WebFTPAccount{}, "name path"},
		{WebDocroot{}, "enabled domain folder"},
		{WebRedirectView{}, "enabled mode target code"},
		{WebEditRequest{}, "ip aliases stats statsAuth statsUser statsPassword redirect redirectTarget redirectCode " +
			"templates docroot docrootDomain docrootFolder ftp"},
		{WebTemplatesChange{}, "template backend fastcgi proxyTemplate proxyExt"},
		{WebFastCGI{}, "enabled duration"},
		{WebFTPChange{}, "name password path prevPath email isNew delete"},
		{WebSSLRequest{}, "ssl letsencrypt keyAlgo forceSsl hsts crt key ca reissue"},
		{WebGenerateSSLRequest{}, "domain aliases email country state locality org keyAlgo"},
		{WebGeneratedSSL{}, "crt key csr"},
		{WebLog{}, "type lines"},
		{WebApps{}, "apps"},
		{WebAppInfo{}, "id name group version installable"},
		{WebAppForm{}, "app fields phpVersions database docrootClean"},
		{WebAppField{}, "key label type value placeholder options"},
		{WebAppOption{}, "value label"},
		{WebAppDatabaseDefaults{}, "hosts name"},
		{WebAppInstallRequest{}, "values phpVersion database"},
		{WebAppDatabase{}, "create host name user password"},
	} {
		if got := strings.Join(webJSONTags(c.v), " "); got != c.want {
			t.Errorf("%T = %q\nwant %q", c.v, got, c.want)
		}
	}
}
