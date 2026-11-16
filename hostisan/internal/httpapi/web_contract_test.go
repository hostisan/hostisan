package httpapi

// Contract between ui/src/api/web.ts and the Go web routes: every call the UI makes
// reaches a registered route, every registered web route is called by the UI, and
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

// webUICalls reads ui/src/api/web.ts and returns every request it makes as
// "METHOD /api/v1/path": the api calls, the job status poll and the download URLs,
// with the path helpers expanded to {domain}, {app} and {job} and the query dropped.
// A path it can't resolve fails the test, so a new helper or call shape has to be
// taught here.
func webUICalls(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "src", "api", "web.ts"))
	if err != nil {
		t.Fatal(err)
	}
	helpers := map[string]string{
		"domainPath(domain)": "/web/{domain}",
		"appsPath(domain)":   "/web/{domain}/apps",
		"enc(app)":           "{app}",
		"enc(job)":           "{job}",
	}
	resolve := func(arg string) string {
		var path string
		switch arg[0] {
		case '\'', '`':
			path = arg[1 : len(arg)-1]
			for h, p := range helpers {
				path = strings.ReplaceAll(path, "${"+h+"}", p)
			}
		default:
			path = helpers[arg]
		}
		path, _, _ = strings.Cut(path, "?")
		if path == "" || strings.ContainsAny(path, "$()") {
			t.Fatalf("web.ts: can't resolve the path %q", arg)
		}
		return strings.TrimPrefix(path, "/api/v1")
	}
	methods := map[string]string{"get": "GET", "post": "POST", "put": "PUT", "del": "DELETE"}
	str := `('[^']*'|` + "`[^`]*`" + `|[A-Za-z]+\([^)]*\))`
	var out []string
	for _, m := range regexp.MustCompile(`api\.(get|post|put|del)<[^>]*>\(\s*`+str).FindAllStringSubmatch(string(src), -1) {
		out = append(out, methods[m[1]]+" /api/v1"+resolve(m[2]))
	}
	// The job poll goes through useJobStatus; the downloads are plain links.
	for _, re := range []string{`useJobStatus\([^?]*\?\s*` + str, `export const \w+Url = [^=]*=>\s*` + str} {
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(string(src), -1) {
			out = append(out, "GET /api/v1"+resolve(m[1]))
		}
	}
	if len(out) == 0 {
		t.Fatal("web.ts: no api calls found")
	}
	return out
}

// webRegisteredRoutes reads the web route patterns from the slices' Go files.
func webRegisteredRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("web*.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`mux\.Handle(?:Func)?\("([A-Z]+ /api/v1/web[^"]*)"`)
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			out = append(out, m[1])
		}
	}
	return out
}

func TestWebContractUICallsCoverEveryRoute(t *testing.T) {
	// The UI calls every web route; a route added on either side shows up here.
	want := []string{
		"GET /api/v1/web",
		"GET /api/v1/web/options",
		"POST /api/v1/web",
		"POST /api/v1/web/bulk",
		"POST /api/v1/web/generate-ssl",
		"GET /api/v1/web/{domain}",
		"PUT /api/v1/web/{domain}",
		"DELETE /api/v1/web/{domain}",
		"PUT /api/v1/web/{domain}/ssl",
		"POST /api/v1/web/{domain}/suspend",
		"POST /api/v1/web/{domain}/unsuspend",
		"POST /api/v1/web/{domain}/purge-cache",
		"GET /api/v1/web/{domain}/logs",
		"GET /api/v1/web/{domain}/logs/download",
		"GET /api/v1/web/{domain}/download",
		"GET /api/v1/web/{domain}/apps",
		"GET /api/v1/web/{domain}/apps/{app}",
		"POST /api/v1/web/{domain}/apps/{app}",
		"GET /api/v1/web/{domain}/apps/jobs/{job}",
	}
	slices.Sort(want)
	got := webUICalls(t)
	slices.Sort(got)
	if got = slices.Compact(got); !slices.Equal(got, want) {
		t.Errorf("web.ts calls\n got %q\nwant %q", got, want)
	}
	routes := webRegisteredRoutes(t)
	slices.Sort(routes)
	if !slices.Equal(routes, want) {
		t.Errorf("registered web routes\n got %q\nwant %q", routes, want)
	}
}

func TestWebContractUICallsAreRegistered(t *testing.T) {
	h := newHarness(t)
	mux := http.NewServeMux()
	h.webServer().registerWebRoutes(mux)
	fill := strings.NewReplacer("{domain}", "example.com", "{app}", "wordpress", "{job}", strings.Repeat("ab", 16))
	for _, c := range webUICalls(t) {
		method, tmpl, _ := strings.Cut(c, " ")
		path := fill.Replace(tmpl)

		// The web routes resolve to exactly this pattern (not, say, {domain} for bulk).
		_, pattern := mux.Handler(httptest.NewRequest(method, path, nil))
		if pattern != c {
			t.Errorf("%s %s: matched %q, want %q", method, path, pattern, c)
		}

		// The real server has them wired: signed out, they answer 401, not the JSON
		// 404 catch-all or a 405.
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

// The bodies below are copied from the UI tests' assertions (ui/src/designs/web/*.test.tsx)
// and, where a test only matches part of a body, completed from the page's body builder.
func TestWebContractUIBodiesDecode(t *testing.T) {
	cases := []struct {
		name string
		into any
		body string
	}{
		{"add domain (AddWebDomainPage)", &WebAddRequest{}, `{"domain":"Fresh.example","ip":"10.0.0.5","dns":true,"mail":false}`},
		{"settings, no template policy (EditWebDomainPage)", &WebEditRequest{}, `{"ip":"203.0.113.10",
			"aliases":"www.example.com\nblog.example.com","stats":"awstats","statsAuth":false,"statsUser":"","statsPassword":"",
			"redirect":true,"redirectTarget":"www.example.com","redirectCode":"302","docroot":true,"docrootDomain":"example.com",
			"docrootFolder":"shop","ftp":[{"name":"dev","password":"","path":"/public_html","prevPath":"/public_html","email":"",
			"isNew":false,"delete":false}]}`},
		{"settings with every template field", &WebEditRequest{}, `{"ip":"203.0.113.10","aliases":"","stats":"none",
			"statsAuth":true,"statsUser":"stats","statsPassword":"Passw0rdOK","redirect":false,"redirectTarget":"",
			"redirectCode":"301","templates":{"template":"default","backend":"PHP-8_3","fastcgi":{"enabled":true,"duration":"2m"},
			"proxyTemplate":"default","proxyExt":"css\njs"},"docroot":false,"docrootDomain":"","docrootFolder":"",
			"ftp":[{"name":"new","password":"Passw0rdOK","path":"/","prevPath":"","email":"me@example.com","isNew":true,"delete":false}]}`},
		{"settings, only the template offered", &WebEditRequest{}, `{"ip":"203.0.113.10","aliases":"","stats":"none",
			"statsAuth":false,"statsUser":"","statsPassword":"","redirect":false,"redirectTarget":"","redirectCode":"301",
			"templates":{"template":"default"},"docroot":false,"docrootDomain":"","docrootFolder":"","ftp":[]}`},
		{"ssl, Let's Encrypt", &WebSSLRequest{}, `{"ssl":true,"letsencrypt":true,"keyAlgo":"ecdsa-256","forceSsl":false,
			"hsts":true,"crt":"","key":"","ca":"","reissue":false}`},
		{"ssl, custom certificate (SslCertificatePage)", &WebSSLRequest{}, `{"ssl":true,"letsencrypt":false,"keyAlgo":"rsa",
			"forceSsl":true,"hsts":false,"crt":"CRT-PEM","key":"KEY-PEM","ca":"CA-PEM","reissue":false}`},
		{"generate (GenerateSslPage)", &WebGenerateSSLRequest{}, `{"domain":"new-site.example","aliases":"","email":"",
			"country":"US","state":"California","locality":"San Francisco","org":"MyCompany Inc","keyAlgo":"ecdsa-256"}`},
		{"bulk (WebDomainsPage)", &BulkRequest{}, `{"action":"rebuild","names":["example.com"]}`},
		{"install (SetupWebAppPage)", &WebAppInstallRequest{}, `{"values":{"site_name":"WordPress Blog","username":"wpadmin",
			"email":"me@example.com","password":"Sup3r-secret-pw","language":"en_US"},"phpVersion":"8.3",
			"database":{"create":true,"host":"localhost","name":"word_d3v9","user":"word_d3v9","password":""}}`},
		{"install, no database", &WebAppInstallRequest{}, `{"values":{"xmlrpc":""},"phpVersion":"8.3"}`},
	}
	for _, c := range cases {
		dec := json.NewDecoder(bytes.NewReader([]byte(c.body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(c.into); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
