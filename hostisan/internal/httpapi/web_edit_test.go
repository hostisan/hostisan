package httpapi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Slice B (Plan 2d, Task 3): the web edit view and the ordered settings save. The
// fixtures of testdata/fixtures/web_edit/ go ahead of the dev ones.

func webEditHarness(t *testing.T, o overrides, opts ...func(*Deps)) *harness {
	t.Helper()
	o.Dir = filepath.Join("..", "..", "testdata", "fixtures", "web_edit")
	return newHarnessWith(t, o, opts...)
}

func webEditStr(v string) *string { return &v }

// webEditSame is the save that matches example.com's stored state.
func webEditSame() WebEditRequest {
	return WebEditRequest{
		IP: "203.0.113.10", Aliases: "www.example.com\nblog.example.com", Stats: "awstats", RedirectCode: "301",
		FTP: []WebFTPChange{{Name: "dev", Path: "/public_html", PrevPath: "/public_html"}},
	}
}

// webEditBase matches a domain with no aliases, stats, FTP, docroot or redirect.
func webEditBase() WebEditRequest {
	return WebEditRequest{IP: "203.0.113.10", Stats: "none", RedirectCode: "301"}
}

// webEditMulti matches multi.example.
func webEditMulti() WebEditRequest {
	return WebEditRequest{
		IP: "203.0.113.10", Aliases: "a.multi.example\nb.multi.example\nc.multi.example", Stats: "awstats",
		StatsAuth: true, StatsUser: "viewer", RedirectCode: "301",
		Docroot: true, DocrootDomain: "other.example", DocrootFolder: "app/sub",
		FTP: []WebFTPChange{
			{Name: "dev", Path: "/public_html", PrevPath: "/public_html"},
			{Name: "cdn", Path: "/assets", PrevPath: "/assets"},
		},
	}
}

func webEditPut(h *harness, domain string, req WebEditRequest) (int, statusReply) {
	h.t.Helper()
	var r statusReply
	code := h.call("PUT", "/api/v1/web/"+domain, req, &r)
	return code, r
}

func webEditOK(t *testing.T, h *harness, domain string, req WebEditRequest) {
	t.Helper()
	if code, r := webEditPut(h, domain, req); code != 200 || r.Status != "ok" {
		t.Fatalf("PUT %s: %d %+v", domain, code, r)
	}
}

func webEditLog(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// webEditFail makes every call of cmd with argc arguments exit with the code.
func webEditFail(cmd string, exit int, out string, argc int) overrides {
	args := make([]string, argc)
	for i := range args {
		args[i] = "*"
	}
	return overrides{Fixtures: map[string][]fixtureEntry{cmd: {{Args: args, Stdout: out, Exit: exit}}}}
}

func webEditGet(t *testing.T, h *harness, domain string) WebDomainView {
	t.Helper()
	var v WebDomainView
	if code := h.call("GET", "/api/v1/web/"+domain, nil, &v); code != 200 {
		t.Fatalf("GET %s: %d", domain, code)
	}
	return v
}

func TestWebEditViewExample(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	v := webEditGet(t, h, "example.com")
	if v.Domain.NAME != "example.com" || v.SSL == nil || !strings.Contains(v.SSL.KEY, "PRIVATE KEY") {
		t.Fatalf("domain/ssl = %+v / %+v", v.Domain, v.SSL)
	}
	if strings.Join(v.Aliases, ",") != "www.example.com,blog.example.com" || v.DisplayIP != "203.0.113.10" || v.KeyAlgo != "ecdsa-256" {
		t.Fatalf("view = %+v", v)
	}
	if len(v.FTP) != 1 || v.FTP[0] != (WebFTPAccount{Name: "dev", Path: "/public_html"}) {
		t.Fatalf("ftp = %+v", v.FTP)
	}
	if v.Docroot != (WebDocroot{}) || v.Redirect != (WebRedirectView{Mode: "custom", Code: "301"}) {
		t.Fatalf("docroot/redirect = %+v / %+v", v.Docroot, v.Redirect)
	}
	webEditLog(t, h.webCalls(), "v-list-web-domain admin example.com json", "v-list-web-domain-ssl admin example.com json", "v-list-user-ips admin json")
}

func TestWebEditViewNoSSLOmitsIt(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	var raw map[string]any
	if code := h.call("GET", "/api/v1/web/example.net", nil, &raw); code != 200 {
		t.Fatalf("got %d", code)
	}
	if _, has := raw["ssl"]; has {
		t.Fatalf("ssl present: %v", raw["ssl"])
	}
	if a, ok := raw["aliases"].([]any); !ok || len(a) != 0 {
		t.Fatalf("aliases = %#v, want []", raw["aliases"])
	}
	if f, ok := raw["ftp"].([]any); !ok || len(f) != 0 {
		t.Fatalf("ftp = %#v, want []", raw["ftp"])
	}
	webEditLog(t, h.webCalls(), "v-list-web-domain admin example.net json", "v-list-user-ips admin json")
}

func TestWebEditViewFTPAndDocroot(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	v := webEditGet(t, h, "multi.example")
	if len(v.FTP) != 2 || v.FTP[0] != (WebFTPAccount{"dev", "/public_html"}) || v.FTP[1] != (WebFTPAccount{"cdn", "/assets"}) {
		t.Fatalf("ftp = %+v", v.FTP)
	}
	if v.Docroot != (WebDocroot{Enabled: true, Domain: "other.example", Folder: "app/sub"}) {
		t.Fatalf("docroot = %+v", v.Docroot)
	}
	if len(v.Aliases) != 3 {
		t.Fatalf("aliases = %v", v.Aliases)
	}
	for domain, want := range map[string]WebDocroot{
		"root.example":    {Enabled: true, Domain: "root.example", Folder: ""},
		"foreign.example": {Enabled: true}, // another user's directory is blanked
		"odd.example":     {Enabled: true}, // ".." escapes are blanked
	} {
		if got := webEditGet(t, h, domain).Docroot; got != want {
			t.Errorf("%s docroot = %+v, want %+v", domain, got, want)
		}
	}
}

func TestWebEditViewRedirect(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	for domain, want := range map[string]WebRedirectView{
		"foreign.example":  {Enabled: true, Mode: "custom", Target: "https://elsewhere.example/page", Code: "301"}, // empty code reads 301
		"wwwredir.example": {Enabled: true, Mode: "www", Target: "www.wwwredir.example", Code: "302"},
		"bare.example":     {Enabled: true, Mode: "bare", Target: "bare.example", Code: "301"},
		"example.net":      {Enabled: false, Mode: "custom", Target: "", Code: "301"},
	} {
		if got := webEditGet(t, h, domain).Redirect; got != want {
			t.Errorf("%s redirect = %+v, want %+v", domain, got, want)
		}
	}
}

func TestWebEditViewKeyAlgo(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	for domain, want := range map[string]string{
		"example.com":    "ecdsa-256", // stored type
		"upper.example":  "ecdsa-256", // "ECDSA" lowercased reads as ecdsa-256
		"legacy.example": "rsa",       // SSL on, no stored type
		"example.net":    "ecdsa-256", // SSL off
	} {
		if got := webEditGet(t, h, domain).KeyAlgo; got != want {
			t.Errorf("%s keyAlgo = %q, want %q", domain, got, want)
		}
	}
}

func TestWebEditViewDisplayIPAndUser(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginBob()
	v := webEditGet(t, h, "bakery.example")
	if v.DisplayIP != "198.51.100.7" || v.Domain.IP != "10.0.0.5" || v.SSL == nil {
		t.Fatalf("view = %+v", v)
	}
	if v.Docroot != (WebDocroot{Enabled: true, Domain: "bakery.example", Folder: "shop"}) {
		t.Fatalf("docroot = %+v", v.Docroot)
	}
	webEditLog(t, h.webCalls(), "v-list-web-domain bob bakery.example json", "v-list-web-domain-ssl bob bakery.example json", "v-list-user-ips bob json")
}

func TestWebEditViewMissingAndHostile(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("GET", "/api/v1/web/missing.example", nil, nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code, _ := webEditPut(h, "missing.example", webEditBase()); code != 404 {
		t.Fatalf("PUT missing: %d", code)
	}
	for _, w := range h.webWrites() {
		t.Errorf("write before the 404: %s", w)
	}
	before := len(h.webCalls())
	for _, d := range []string{"-h", "a%20b", "a%0Ab"} {
		if code := h.call("GET", "/api/v1/web/"+d, nil, nil); code != 400 {
			t.Errorf("GET %q: %d", d, code)
		}
		if code, _ := webEditPut(h, d, webEditBase()); code != 400 {
			t.Errorf("PUT %q: %d", d, code)
		}
	}
	if got := len(h.webCalls()); got != before {
		t.Errorf("hostile domains ran %d commands", got-before)
	}
}

func TestWebEditUnchangedRunsNothing(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditSame()
	req.Templates = &WebTemplatesChange{
		Template: "default", Backend: webEditStr("PHP-8_2"), ProxyTemplate: webEditStr("default"),
		ProxyExt: webEditStr("css, htm\nhtml,js mjs json xml apng avif bmp cur gif ico jfif jpg jpeg png svg webp"),
	}
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webCalls(), "v-list-web-domain admin example.com json")
}

func TestWebEditIPChangeCascades(t *testing.T) {
	h := webEditHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user-ips": {{Args: []string{"admin", "json"}, Exit: 0, Stdout: `{"203.0.113.10":{"OWNER":"admin","STATUS":"shared","NAME":"main","NAT":""},"203.0.113.11":{"OWNER":"admin","STATUS":"exclusive","NAME":"secondary","NAT":"198.51.100.11"}}`}},
	}})
	h.loginAdmin()
	req := webEditSame()
	req.IP = "203.0.113.11"
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webCalls(),
		"v-list-web-domain admin example.com json",
		"v-list-user-ips admin json",
		"v-change-web-domain-ip admin example.com 203.0.113.11 no",
		"v-list-dns-domain admin example.com json",
		"v-change-dns-domain-ip admin example.com 198.51.100.11 no",
		"v-list-dns-domain admin www.example.com json",
		"v-list-dns-domain admin blog.example.com json",
		"v-change-dns-domain-ip admin blog.example.com 198.51.100.11",
		"v-list-mail-domain admin example.com json",
		"v-rebuild-mail-domain admin example.com",
		"v-restart-web", "v-restart-proxy", "v-restart-dns")
}

func TestWebEditIPChangeWithoutZones(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditBase()
	req.IP = "203.0.113.11"
	req.Aliases = "www.plain.example"
	req.Stats = "none"
	webEditOK(t, h, "plain.example", req)
	webEditLog(t, h.webCalls(),
		"v-list-web-domain admin plain.example json",
		"v-list-user-ips admin json",
		"v-change-web-domain-ip admin plain.example 203.0.113.11 no",
		"v-list-dns-domain admin plain.example json",
		"v-list-dns-domain admin www.plain.example json",
		"v-list-mail-domain admin plain.example json",
		"v-restart-web", "v-restart-proxy")
}

func TestWebEditIPChangeStopsOnError(t *testing.T) {
	h := webEditHarness(t, webEditFail("v-change-web-domain-ip", 10, "Error: admin user can't use IP 203.0.113.11\n", 4))
	h.loginAdmin()
	req := webEditSame()
	req.IP = "203.0.113.11"
	code, r := webEditPut(h, "example.com", req)
	if code != 403 || r.Error.Message != "Error: admin user can't use IP 203.0.113.11" {
		t.Fatalf("got %d %+v", code, r)
	}
	webEditLog(t, h.webWrites(), "v-change-web-domain-ip admin example.com 203.0.113.11 no")
}

func TestWebEditTemplatesNeedThePolicy(t *testing.T) {
	req := webEditBase()
	req.IP = "10.0.0.5"
	req.Aliases = "www.bakery.example"
	req.Stats = "awstats"
	req.StatsAuth, req.StatsUser = true, "baker"
	req.Redirect, req.RedirectTarget = true, "www.bakery.example"
	req.Docroot, req.DocrootDomain, req.DocrootFolder = true, "bakery.example", "shop"
	req.Templates = &WebTemplatesChange{Template: "other", Backend: webEditStr("PHP-8_2"), ProxyTemplate: webEditStr("caching")}

	h := webEditHarness(t, overrides{})
	h.loginBob()
	webEditOK(t, h, "bakery.example", req)
	webEditLog(t, h.webWrites())

	h = webEditHarness(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_WEB_TEMPLATES": "yes"}})
	h.loginBob()
	webEditOK(t, h, "bakery.example", req)
	webEditLog(t, h.webWrites(),
		"v-change-web-domain-tpl bob bakery.example other no",
		"v-change-web-domain-backend-tpl bob bakery.example PHP-8_2",
		"v-change-web-domain-proxy-tpl bob bakery.example caching css,htm,html,js,mjs,json,xml,apng,avif,bmp,cur,gif,ico,jfif,jpg,jpeg,png,svg,webp no",
		"v-restart-web", "v-restart-proxy")
}

func TestWebEditBackendAloneRestartsNothing(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditSame()
	req.Templates = &WebTemplatesChange{Template: "default", Backend: webEditStr("PHP-8_3")}
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webWrites(), "v-change-web-domain-backend-tpl admin example.com PHP-8_3")

	// Without WEB_BACKEND the backend is ignored.
	h = webEditHarness(t, overrides{Config: map[string]string{"WEB_BACKEND": ""}})
	h.loginAdmin()
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webWrites())
}

func TestWebEditFastCGI(t *testing.T) {
	on := func(d string) *WebTemplatesChange {
		return &WebTemplatesChange{Template: "default", FastCGI: &WebFastCGI{Enabled: true, Duration: d}}
	}
	off := &WebTemplatesChange{Template: "default", FastCGI: &WebFastCGI{}}
	for _, c := range []struct {
		name   string
		cfg    map[string]string
		domain string
		tpl    *WebTemplatesChange
		want   []string
	}{
		{"apache runs nothing", nil, "example.com", on("5m"), nil},
		{"nginx on, default duration", map[string]string{"WEB_SYSTEM": "nginx"}, "example.com", on(""),
			[]string{"v-add-fastcgi-cache admin example.com 2m", "v-restart-web"}},
		{"nginx on, typed duration", map[string]string{"WEB_SYSTEM": "nginx"}, "example.com", on("30s"),
			[]string{"v-add-fastcgi-cache admin example.com 30s", "v-restart-web"}},
		{"nginx off, was off", map[string]string{"WEB_SYSTEM": "nginx"}, "example.com", off, nil},
		{"nginx off, was on", map[string]string{"WEB_SYSTEM": "nginx"}, "cache.example", off,
			[]string{"v-delete-fastcgi-cache admin cache.example", "v-restart-web"}},
		{"nginx on, same duration", map[string]string{"WEB_SYSTEM": "nginx"}, "cache.example", on("5m"), nil},
		{"nginx on, duration change", map[string]string{"WEB_SYSTEM": "nginx"}, "cache.example", on("1d"),
			[]string{"v-add-fastcgi-cache admin cache.example 1d", "v-restart-web"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{Config: c.cfg})
			h.loginAdmin()
			req := webEditSame()
			if c.domain == "cache.example" {
				req = webEditBase()
			}
			req.Templates = c.tpl
			webEditOK(t, h, c.domain, req)
			webEditLog(t, h.webWrites(), c.want...)
		})
	}
}

func TestWebEditProxyTemplate(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditBase()
	req.Aliases = "www.plain.example"
	req.Templates = &WebTemplatesChange{Template: "default", ProxyTemplate: webEditStr("default"), ProxyExt: webEditStr("css, js\nhtml")}
	webEditOK(t, h, "plain.example", req) // stored css,js,html: the same list
	webEditLog(t, h.webWrites())

	req.Templates.ProxyExt = webEditStr("css, js\npng")
	webEditOK(t, h, "plain.example", req)
	webEditLog(t, h.webWrites(), "v-change-web-domain-proxy-tpl admin plain.example default css,js,png no", "v-restart-proxy")

	// A template change alone, with the extensions left out.
	h = webEditHarness(t, overrides{})
	h.loginAdmin()
	req.Templates = &WebTemplatesChange{Template: "default", ProxyTemplate: webEditStr("caching")}
	webEditOK(t, h, "plain.example", req)
	webEditLog(t, h.webWrites(), "v-change-web-domain-proxy-tpl admin plain.example caching css,js,html no", "v-restart-proxy")
}

func TestWebEditProxyIgnoredWithoutProxy(t *testing.T) {
	req := webEditBase()
	req.Templates = &WebTemplatesChange{Template: "default", ProxyTemplate: webEditStr("caching"), ProxyExt: webEditStr("png")}
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	webEditOK(t, h, "noproxy.example", req) // the domain has no proxy
	webEditLog(t, h.webWrites())

	h = webEditHarness(t, overrides{Config: map[string]string{"PROXY_SYSTEM": ""}})
	h.loginAdmin()
	req.Aliases = "www.plain.example"
	webEditOK(t, h, "plain.example", req) // the server has no proxy system
	webEditLog(t, h.webWrites())
}

func TestWebEditAliasDiff(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditMulti()
	req.Aliases = "b.multi.example, d.multi.example"
	webEditOK(t, h, "multi.example", req)
	webEditLog(t, h.webCalls(),
		"v-list-web-domain admin multi.example json",
		"v-delete-web-domain-alias admin multi.example a.multi.example no",
		"v-list-dns-domain admin multi.example",
		"v-delete-dns-on-web-alias admin multi.example a.multi.example no",
		"v-delete-web-domain-alias admin multi.example c.multi.example no",
		"v-list-dns-domain admin multi.example",
		"v-delete-dns-on-web-alias admin multi.example c.multi.example no",
		"v-add-web-domain-alias admin multi.example d.multi.example no",
		"v-list-dns-domain admin multi.example",
		"v-list-user-ips admin json",
		"v-add-dns-on-web-alias admin d.multi.example 203.0.113.10 no",
		"v-change-web-domain-stats admin multi.example awstats",
		"v-restart-web", "v-restart-proxy", "v-restart-dns")
}

func TestWebEditAliasWithoutZone(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditBase()
	req.Aliases = "www.plain.example\nnew.plain.example\nnew.plain.example"
	webEditOK(t, h, "plain.example", req)
	// Stats are off: no refresh. The repeated name is added once.
	webEditLog(t, h.webCalls(),
		"v-list-web-domain admin plain.example json",
		"v-add-web-domain-alias admin plain.example new.plain.example no",
		"v-list-dns-domain admin plain.example",
		"v-restart-web", "v-restart-proxy")

	h = webEditHarness(t, overrides{})
	h.loginAdmin()
	req.Aliases = ""
	webEditOK(t, h, "plain.example", req)
	webEditLog(t, h.webWrites(), "v-delete-web-domain-alias admin plain.example www.plain.example no", "v-restart-web", "v-restart-proxy")
}

func TestWebEditAliasChangeRefreshesStats(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditSame()
	req.Aliases += "\nshop.example.com"
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webWrites(),
		"v-add-web-domain-alias admin example.com shop.example.com no",
		"v-add-dns-on-web-alias admin shop.example.com 203.0.113.10 no",
		"v-change-web-domain-stats admin example.com awstats",
		"v-restart-web", "v-restart-proxy", "v-restart-dns")
}

func TestWebEditStatsEngine(t *testing.T) {
	for _, c := range []struct {
		name, domain string
		req          func() WebEditRequest
		want         []string
	}{
		{"awstats to none", "example.com", func() WebEditRequest { r := webEditSame(); r.Stats = "none"; return r },
			[]string{"v-delete-web-domain-stats admin example.com"}},
		{"none to awstats", "example.net", func() WebEditRequest { r := webEditBase(); r.Stats = "awstats"; return r },
			[]string{"v-add-web-domain-stats admin example.net awstats"}},
		{"engine change", "example.com", func() WebEditRequest { r := webEditSame(); r.Stats = "webalizer"; return r },
			[]string{"v-change-web-domain-stats admin example.com webalizer"}},
		{"none to none", "example.net", webEditBase, nil},
		{"none with a login ticked is none", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.StatsAuth, r.StatsUser = true, "  "
			return r
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{})
			h.loginAdmin()
			webEditOK(t, h, c.domain, c.req())
			webEditLog(t, h.webWrites(), c.want...)
		})
	}
}

func TestWebEditStatsLogin(t *testing.T) {
	tmp := t.TempDir()
	var content, path string
	var mode os.FileMode
	h := webEditHarness(t, overrides{}, func(d *Deps) {
		d.TmpDir = tmp
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-add-web-domain-stats-user" {
				return
			}
			path = args[3]
			if fi, err := os.Stat(path); err == nil {
				mode = fi.Mode().Perm()
			}
			b, _ := os.ReadFile(path)
			content = string(b)
		}}
	})
	h.loginAdmin()
	req := webEditSame()
	req.StatsAuth, req.StatsUser, req.StatsPassword = true, " viewer ", "S3cretPw"
	webEditOK(t, h, "example.com", req)
	// Once, even though the login is new and a password was typed.
	webEditLog(t, h.webWrites(), "v-add-web-domain-stats-user admin example.com viewer "+path)
	if !strings.HasPrefix(path, tmp+"/") || mode != 0o600 || content != "S3cretPw\n" {
		t.Fatalf("password file %q mode %v content %q", path, mode, content)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("password file still there: %v", err)
	}
	for _, c := range h.fixture.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), "S3cretPw") {
			t.Fatalf("password in call %v", c)
		}
	}
}

func TestWebEditStatsLoginCases(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*WebEditRequest)
		want []string
	}{
		{"same user, no password", func(r *WebEditRequest) {}, nil},
		{"same user, new password", func(r *WebEditRequest) { r.StatsPassword = "x1" },
			[]string{"v-add-web-domain-stats-user admin multi.example viewer *"}},
		{"other user", func(r *WebEditRequest) { r.StatsUser = "other" },
			[]string{"v-add-web-domain-stats-user admin multi.example other *"}},
		{"login off", func(r *WebEditRequest) { r.StatsAuth, r.StatsUser = false, "ignored" },
			[]string{"v-delete-web-domain-stats-user admin multi.example"}},
		{"stats off deletes the engine, then the login", func(r *WebEditRequest) { r.Stats, r.StatsAuth = "none", true },
			[]string{"v-delete-web-domain-stats admin multi.example", "v-delete-web-domain-stats-user admin multi.example"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{}, func(d *Deps) { d.TmpDir = t.TempDir() })
			h.loginAdmin()
			req := webEditMulti()
			c.edit(&req)
			webEditOK(t, h, "multi.example", req)
			got := h.webWrites()
			for i, g := range got {
				if strings.HasPrefix(g, "v-add-web-domain-stats-user") {
					f := strings.Fields(g)
					got[i] = strings.Join(f[:len(f)-1], " ") + " *"
				}
			}
			webEditLog(t, got, c.want...)
		})
	}
}

func TestWebEditStatsUserBlank(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditSame()
	req.StatsAuth, req.StatsUser = true, "   "
	req.IP = "203.0.113.11" // a change that would run first
	var e APIError
	if code := h.call("PUT", "/api/v1/web/example.com", req, &e); code != 400 || e.Error.Code != "invalid" ||
		e.Error.Message != `Field "%s" can not be blank.` || len(e.Error.Args) != 1 || e.Error.Args[0] != "Username" {
		t.Fatalf("got %d %+v", code, e)
	}
	webEditLog(t, h.webCalls())
}

func TestWebEditFTPAdd(t *testing.T) {
	tmp := t.TempDir()
	var path, content string
	var mode os.FileMode
	mm := &fakeMailer{}
	h := webEditHarness(t, overrides{}, withMail(mm, &fakeTemplates{}, tmp), func(d *Deps) {
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-add-web-domain-ftp" {
				return
			}
			path = args[3]
			if fi, err := os.Stat(path); err == nil {
				mode = fi.Mode().Perm()
			}
			b, _ := os.ReadFile(path)
			content = string(b)
		}}
	})
	h.loginAdmin()
	req := webEditSame()
	req.FTP = append(req.FTP, WebFTPChange{Name: "Admin_Shop", Password: "FtpPass1", Path: " /uploads ", Email: "dev@example.org", IsNew: true})
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webWrites(), "v-add-web-domain-ftp admin example.com Shop "+path+" /uploads")
	if !strings.HasPrefix(path, tmp+"/") || mode != 0o600 || content != "FtpPass1\n" {
		t.Fatalf("password file %q mode %v content %q", path, mode, content)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("password file still there: %v", err)
	}
	for _, c := range h.fixture.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), "FtpPass1") {
			t.Fatalf("password in call %v", c)
		}
	}
	if len(mm.sent) != 1 || mm.sent[0].To != "dev@example.org" || mm.sent[0].Subject != "FTP Account Credentials: admin_Shop" ||
		!strings.Contains(mm.sent[0].Body, "Hostname: example.com") || !strings.Contains(mm.sent[0].Body, "Username: admin_Shop") ||
		!strings.Contains(mm.sent[0].Body, "Password: FtpPass1") || !strings.HasSuffix(mm.sent[0].Body, "--\nHostisan") {
		t.Fatalf("mail = %+v", mm.sent)
	}
}

func TestWebEditFTPEmailUsesTheTemplate(t *testing.T) {
	mm := &fakeMailer{}
	tp := &fakeTemplates{ok: true, subject: "Your FTP {{username}}", body: "{{domain}}|{{username}}|{{password}}|{{appname}}"}
	h := webEditHarness(t, overrides{}, withMail(mm, tp, t.TempDir()))
	h.loginAdmin()
	req := webEditSame()
	req.FTP = []WebFTPChange{{Name: "dev", Password: "a<b&c", Path: "/public_html", PrevPath: "/public_html", Email: "dev@example.org"}}
	webEditOK(t, h, "example.com", req)
	if len(tp.asked) != 1 || tp.asked[0][1] != "ftpaccount_created" || len(mm.sent) != 1 {
		t.Fatalf("asked %v sent %+v", tp.asked, mm.sent)
	}
	// Raw values on the change path too: nothing HTML-escaped.
	if mm.sent[0].Body != "example.com|admin_dev|a<b&c|Hostisan" || mm.sent[0].Subject != "Your FTP admin_dev" {
		t.Fatalf("mail = %+v", mm.sent[0])
	}
}

// An existing account is emailed only when a new password was typed (owner ruling 7).
func TestWebEditFTPEmailExistingNeedsNewPassword(t *testing.T) {
	mm := &fakeMailer{}
	h := webEditHarness(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	req := webEditSame()
	req.FTP = []WebFTPChange{{Name: "dev", Path: "/public_html", PrevPath: "/public_html", Email: "dev@example.org"}}
	webEditOK(t, h, "example.com", req)
	if len(mm.sent) != 0 {
		t.Fatalf("sent %+v", mm.sent)
	}
}

func TestWebEditFTPEmailFailureStill200(t *testing.T) {
	mm := &fakeMailer{err: errors.New("smtp down")}
	h := webEditHarness(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	req := webEditSame()
	req.FTP = append(req.FTP, WebFTPChange{Name: "shop", Password: "pw1", IsNew: true, Email: "dev@example.org"})
	webEditOK(t, h, "example.com", req)
	if len(mm.sent) != 1 || len(h.webWrites()) != 1 {
		t.Fatalf("sent %d, writes %q", len(mm.sent), h.webWrites())
	}
}

func TestWebEditFTPInvalidEmail(t *testing.T) {
	for _, row := range []WebFTPChange{
		{Name: "shop", Password: "pw1", IsNew: true, Email: "not-an-email"},
		{Name: "dev", Path: "/public_html", PrevPath: "/public_html", Email: "a@b"},
	} {
		h := webEditHarness(t, overrides{})
		h.loginAdmin()
		req := webEditSame()
		req.IP = "203.0.113.11"
		req.FTP = []WebFTPChange{row}
		var e APIError
		if code := h.call("PUT", "/api/v1/web/example.com", req, &e); code != 400 || e.Error.Message != "Please enter a valid email address." {
			t.Fatalf("%+v: %d %+v", row, code, e)
		}
		webEditLog(t, h.webCalls())
	}
}

func TestWebEditFTPExistingNameBlank(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditSame()
	req.FTP = []WebFTPChange{{Name: "  ", Path: "/x", PrevPath: "/x"}}
	var e APIError
	if code := h.call("PUT", "/api/v1/web/example.com", req, &e); code != 400 || e.Error.Message != `Field "%s" can not be blank.` || e.Error.Args[0] != "Username" {
		t.Fatalf("got %d %+v", code, e)
	}
	webEditLog(t, h.webCalls())
}

func TestWebEditFTPDeletePathPassword(t *testing.T) {
	tmp := t.TempDir()
	h := webEditHarness(t, overrides{}, func(d *Deps) { d.TmpDir = tmp })
	h.loginAdmin()
	req := webEditMulti()
	req.Docroot, req.DocrootDomain, req.DocrootFolder = true, "other.example", "app/sub"
	req.FTP = []WebFTPChange{
		{Name: "dev", Delete: true, Path: "/public_html", PrevPath: "/public_html"},
		{Name: "cdn", Path: " /static ", PrevPath: "/assets", Password: "NewPw1"},
		{Name: "ghost", IsNew: true, Delete: true, Password: "x"},
	}
	webEditOK(t, h, "multi.example", req)
	got := h.webWrites()
	if len(got) != 3 || got[0] != "v-delete-web-domain-ftp admin multi.example admin_dev" ||
		got[1] != "v-change-web-domain-ftp-path admin multi.example admin_cdn /static" ||
		!strings.HasPrefix(got[2], "v-change-web-domain-ftp-password admin multi.example admin_cdn "+tmp+"/") {
		t.Fatalf("writes = %q", got)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("temp files left: %v", entries)
	}
	// Paths equal after trimming run nothing.
	h = webEditHarness(t, overrides{})
	h.loginAdmin()
	req.FTP = []WebFTPChange{{Name: "cdn", Path: " /assets ", PrevPath: "/assets"}}
	webEditOK(t, h, "multi.example", req)
	webEditLog(t, h.webWrites())
}

func TestWebEditFTPPasswordFailureStops(t *testing.T) {
	tmp := t.TempDir()
	o := webEditFail("v-change-web-domain-ftp-password", 2, "Error: Password not accepted due to PAM restrictions\n", 4)
	h := webEditHarness(t, o, func(d *Deps) { d.TmpDir = tmp })
	h.loginAdmin()
	req := webEditSame()
	req.FTP = []WebFTPChange{{Name: "dev", Path: "/public_html", PrevPath: "/public_html", Password: "weak"}}
	req.Redirect, req.RedirectTarget = true, "www.example.com"
	code, r := webEditPut(h, "example.com", req)
	if code != 400 || r.Error.Message != "Error: Password not accepted due to PAM restrictions" {
		t.Fatalf("got %d %+v", code, r)
	}
	for _, w := range h.webWrites() {
		if strings.HasPrefix(w, "v-add-web-domain-redirect") || strings.HasPrefix(w, "v-restart") {
			t.Errorf("ran after the failure: %s", w)
		}
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("temp files left: %v", entries)
	}
}

func TestWebEditDocroot(t *testing.T) {
	for _, c := range []struct {
		name, domain string
		req          func() WebEditRequest
		want         []string
	}{
		{"set", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.Docroot, r.DocrootDomain, r.DocrootFolder = true, "other.example", "app/"
			return r
		}, []string{"v-change-web-domain-docroot admin example.net other.example app yes", "v-restart-web", "v-restart-proxy"}},
		{"own domain, no folder, was off", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.Docroot, r.DocrootDomain = true, "example.net"
			return r
		}, nil},
		{"own domain with a folder", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.Docroot, r.DocrootDomain, r.DocrootFolder = true, "example.net", "site"
			return r
		}, []string{"v-change-web-domain-docroot admin example.net example.net site yes", "v-restart-web", "v-restart-proxy"}},
		{"unchanged", "multi.example", webEditMulti, nil},
		{"folder changed", "multi.example", func() WebEditRequest { r := webEditMulti(); r.DocrootFolder = "app"; return r },
			[]string{"v-change-web-domain-docroot admin multi.example other.example app yes", "v-restart-web", "v-restart-proxy"}},
		{"back to the domain itself", "multi.example", func() WebEditRequest {
			r := webEditMulti()
			r.DocrootDomain, r.DocrootFolder = "multi.example", ""
			return r
		}, []string{"v-change-web-domain-docroot admin multi.example default", "v-restart-web", "v-restart-proxy"}},
		{"reset", "multi.example", func() WebEditRequest { r := webEditMulti(); r.Docroot = false; return r },
			[]string{"v-change-web-domain-docroot admin multi.example default", "v-restart-web", "v-restart-proxy"}},
		{"off, was off", "example.net", webEditBase, nil},
		{"on without a target", "example.net", func() WebEditRequest { r := webEditBase(); r.Docroot = true; return r }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{})
			h.loginAdmin()
			webEditOK(t, h, c.domain, c.req())
			webEditLog(t, h.webWrites(), c.want...)
		})
	}
}

func TestWebEditRedirect(t *testing.T) {
	www := func() WebEditRequest {
		r := webEditBase()
		r.Redirect, r.RedirectTarget, r.RedirectCode = true, "www.wwwredir.example", "302"
		return r
	}
	for _, c := range []struct {
		name, domain string
		req          func() WebEditRequest
		want         []string
	}{
		{"add", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.Redirect, r.RedirectTarget, r.RedirectCode = true, " www.example.net ", "302"
			return r
		}, []string{"v-add-web-domain-redirect admin example.net www.example.net 302", "v-restart-web", "v-restart-proxy"}},
		{"add, empty code is 301", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.Redirect, r.RedirectTarget, r.RedirectCode = true, "example.org", ""
			return r
		}, []string{"v-add-web-domain-redirect admin example.net example.org 301", "v-restart-web", "v-restart-proxy"}},
		{"unchanged", "wwwredir.example", www, nil},
		{"code change", "wwwredir.example", func() WebEditRequest { r := www(); r.RedirectCode = "301"; return r },
			[]string{"v-add-web-domain-redirect admin wwwredir.example www.wwwredir.example 301", "v-restart-web", "v-restart-proxy"}},
		{"target change", "wwwredir.example", func() WebEditRequest { r := www(); r.RedirectTarget = "wwwredir.example"; return r },
			[]string{"v-add-web-domain-redirect admin wwwredir.example wwwredir.example 302", "v-restart-web", "v-restart-proxy"}},
		{"stored code empty reads 301", "foreign.example", func() WebEditRequest {
			r := webEditBase()
			r.Redirect, r.RedirectTarget, r.RedirectCode = true, "https://elsewhere.example/page", "301"
			r.Docroot = true // the stored docroot stays
			return r
		}, nil},
		{"delete", "wwwredir.example", webEditBase,
			[]string{"v-delete-web-domain-redirect admin wwwredir.example", "v-restart-web", "v-restart-proxy"}},
		{"empty custom target", "example.net", func() WebEditRequest {
			r := webEditBase()
			r.Redirect, r.RedirectTarget = true, "   "
			return r
		}, nil},
		{"on with an empty target keeps the stored redirect", "wwwredir.example", func() WebEditRequest {
			r := webEditBase()
			r.Redirect = true
			return r
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{})
			h.loginAdmin()
			webEditOK(t, h, c.domain, c.req())
			webEditLog(t, h.webWrites(), c.want...)
		})
	}
}

func TestWebEditFailureStopsTheRest(t *testing.T) {
	h := webEditHarness(t, webEditFail("v-change-web-domain-tpl", 4, "Error: boom\n", 4))
	h.loginAdmin()
	req := webEditSame()
	req.Aliases += "\nshop.example.com"
	req.Stats = "none"
	req.FTP = append(req.FTP, WebFTPChange{Name: "shop", Password: "pw1", IsNew: true})
	req.Docroot, req.DocrootDomain = true, "other.example"
	req.Redirect, req.RedirectTarget = true, "www.example.com"
	req.Templates = &WebTemplatesChange{Template: "other"}
	code, r := webEditPut(h, "example.com", req)
	if code != 409 || r.Error.Message != "Error: boom" {
		t.Fatalf("got %d %+v", code, r)
	}
	// The failing command is the last one, and no restart ran.
	webEditLog(t, h.webWrites(), "v-change-web-domain-tpl admin example.com other no")
}

func TestWebEditFullOrder(t *testing.T) {
	h := webEditHarness(t, overrides{}, func(d *Deps) { d.TmpDir = t.TempDir() })
	h.loginAdmin()
	req := webEditSame()
	req.Templates = &WebTemplatesChange{Template: "other"}
	req.Aliases = "www.example.com\nshop.example.com"
	req.Stats = "none"
	req.FTP = []WebFTPChange{{Name: "dev", Delete: true}}
	req.Docroot, req.DocrootDomain, req.DocrootFolder = true, "other.example", "x"
	req.Redirect, req.RedirectTarget = true, "example.org"
	webEditOK(t, h, "example.com", req)
	webEditLog(t, h.webWrites(),
		"v-change-web-domain-tpl admin example.com other no",
		"v-delete-web-domain-alias admin example.com blog.example.com no",
		"v-delete-dns-on-web-alias admin example.com blog.example.com no",
		"v-add-web-domain-alias admin example.com shop.example.com no",
		"v-add-dns-on-web-alias admin shop.example.com 203.0.113.10 no",
		"v-delete-web-domain-stats admin example.com",
		"v-delete-web-domain-ftp admin example.com admin_dev",
		"v-change-web-domain-docroot admin example.com other.example x yes",
		"v-add-web-domain-redirect admin example.com example.org 301",
		"v-restart-web", "v-restart-proxy", "v-restart-dns")
}

func TestWebEditNoProxyRestartWithoutProxySystem(t *testing.T) {
	h := webEditHarness(t, overrides{Config: map[string]string{"PROXY_SYSTEM": ""}})
	h.loginAdmin()
	req := webEditBase()
	req.Redirect, req.RedirectTarget = true, "example.org"
	webEditOK(t, h, "example.net", req)
	webEditLog(t, h.webWrites(), "v-add-web-domain-redirect admin example.net example.org 301", "v-restart-web")
}

func TestWebEditArgvSafety(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*WebEditRequest)
	}{
		{"alias with a dash", func(r *WebEditRequest) { r.Aliases = "www.example.com -x" }},
		{"alias with a control character", func(r *WebEditRequest) { r.Aliases = "www.example.com\u0001" }},
		{"stats user with a dash", func(r *WebEditRequest) { r.StatsAuth, r.StatsUser = true, "-u" }},
		{"stats user with a newline", func(r *WebEditRequest) { r.StatsAuth, r.StatsUser = true, "a\nb" }},
		{"redirect target with a dash", func(r *WebEditRequest) { r.Redirect, r.RedirectTarget = true, "-bad" }},
		{"redirect target with a newline", func(r *WebEditRequest) { r.Redirect, r.RedirectTarget = true, "a.example\nb" }},
		{"ftp name with a dash", func(r *WebEditRequest) { r.FTP = []WebFTPChange{{Name: "-x", Password: "p", IsNew: true}} }},
		{"ftp name with a space", func(r *WebEditRequest) { r.FTP = []WebFTPChange{{Name: "a b", Password: "p", IsNew: true}} }},
		{"existing ftp name with a dash", func(r *WebEditRequest) { r.FTP = []WebFTPChange{{Name: "-x", Path: "/p", PrevPath: "/p"}} }},
		{"ftp path with a dash", func(r *WebEditRequest) { r.FTP = []WebFTPChange{{Name: "x", Path: "-p", Password: "p", IsNew: true}} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{})
			h.loginAdmin()
			req := webEditSame()
			req.IP = "203.0.113.11"
			c.edit(&req)
			if code, r := webEditPut(h, "example.com", req); code != 400 || r.Error.Code != "invalid" {
				t.Fatalf("got %d %+v", code, r)
			}
			webEditLog(t, h.webCalls())
		})
	}
}

func TestWebEditReadOnly(t *testing.T) {
	h := webEditHarness(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var r statusReply
	if code := h.call("PUT", "/api/v1/web/example.com", webEditSame(), &r); code != 403 || r.Error.Code != "read_only" {
		t.Fatalf("got %d %+v", code, r)
	}
	webEditLog(t, h.webCalls())
	// Reading stays open.
	if code := h.call("GET", "/api/v1/web/example.com", nil, nil); code != 200 {
		t.Fatalf("GET read-only: %d", code)
	}
}

func TestWebEditSignedOut(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.bootstrap()
	if code := h.call("GET", "/api/v1/web/example.com", nil, nil); code != 401 {
		t.Fatalf("GET: %d", code)
	}
	if code, _ := webEditPut(h, "example.com", webEditSame()); code != 401 {
		t.Fatalf("PUT: %d", code)
	}
}

func TestWebEditPasswordsRequiredBeforeAnyCommand(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*WebEditRequest)
	}{
		{"new ftp row", func(r *WebEditRequest) { r.FTP = append(r.FTP, WebFTPChange{Name: "shop", IsNew: true}) }},
		{"new stats login", func(r *WebEditRequest) { r.StatsAuth, r.StatsUser = true, "viewer" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := webEditHarness(t, overrides{})
			h.loginAdmin()
			req := webEditSame()
			req.IP = "203.0.113.11"
			c.edit(&req)
			var e APIError
			if code := h.call("PUT", "/api/v1/web/example.com", req, &e); code != 400 || e.Error.Message != `Field "%s" can not be blank.` ||
				len(e.Error.Args) != 1 || e.Error.Args[0] != "Password" {
				t.Fatalf("got %d %+v", code, e)
			}
			if w := h.webWrites(); len(w) != 0 {
				t.Fatalf("writes = %q", w)
			}
		})
	}
}

func TestWebEditRedirectCodeChecked(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditBase()
	req.Redirect, req.RedirectTarget, req.RedirectCode = true, "example.org", "307"
	req.IP = "203.0.113.11"
	if code, r := webEditPut(h, "example.net", req); code != 400 || r.Error.Code != "invalid" {
		t.Fatalf("got %d %+v", code, r)
	}
	if w := h.webWrites(); len(w) != 0 {
		t.Fatalf("writes = %q", w)
	}
}

func TestWebEditStatsUserChangeBlankPassword(t *testing.T) {
	var content string
	h := webEditHarness(t, overrides{}, func(d *Deps) {
		d.TmpDir = t.TempDir()
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd == "v-add-web-domain-stats-user" {
				b, _ := os.ReadFile(args[3])
				content = string(b)
			}
		}}
	})
	h.loginAdmin()
	req := webEditMulti()
	req.StatsUser = "other"
	webEditOK(t, h, "multi.example", req)
	if w := h.webWrites(); len(w) != 1 || !strings.HasPrefix(w[0], "v-add-web-domain-stats-user admin multi.example other ") || content != "\n" {
		t.Fatalf("writes %q, file %q", w, content)
	}
}
