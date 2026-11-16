package httpapi

// Contract between the accounts UI data layer (ui/src/api/{users,account,cron,backup}.ts)
// and the Plan 3 Go routes: every call the UI makes reaches a registered route, every
// Plan 3 route is called by the UI, and every body the UI sends decodes into its Go
// struct with no unknown fields.

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

// acctUIFiles are the UI modules that call the Plan 3 API.
var acctUIFiles = []string{"users.ts", "account.ts", "cron.ts", "backup.ts"}

// acctRouteFiles are the Go files that register the Plan 3 routes.
var acctRouteFiles = []string{"users.go", "profile.go", "recovery.go", "keys.go", "stats.go", "cron.go", "backup.go"}

// acctUICalls reads the accounts UI modules and returns every api call as
// "METHOD /api/v1/path", with the path helpers expanded to their route parameters and
// query strings dropped. A call whose path it can't resolve fails the test, so a new
// helper or call shape has to be taught here. The download link (fileUrl) is not an
// api call, so it is added by hand.
func acctUICalls(t *testing.T) []string {
	t.Helper()
	fill := strings.NewReplacer(
		"${userPath(name)}", "/users/{user}",
		"${packagePath(name)}", "/packages/{package}",
		"${jobPath(id)}", "/cron/{job}",
		"${backupPath(name)}", "/backup/{backup}",
		"${snapshotPath(id)}", "/backup/incremental/{snapshot}",
		"/account/access-keys/${enc(id)}", "/account/access-keys/{key}",
		"/account/ssh-keys/${enc(id)}", "/account/ssh-keys/{id}",
		"/notifications/${enc(id)}", "/notifications/{id}",
		"${userQuery(user)}", "",
		"?path=${enc(path)}", "",
	)
	helpers := map[string]string{
		"userPath(name)":    "/users/{user}",
		"packagePath(name)": "/packages/{package}",
		"jobPath(id)":       "/cron/{job}",
		"backupPath(name)":  "/backup/{backup}",
		"snapshotPath(id)":  "/backup/incremental/{snapshot}",
	}
	methods := map[string]string{"get": "GET", "post": "POST", "put": "PUT", "del": "DELETE"}
	call := regexp.MustCompile(`api\.(get|post|put|del)<[^>]*>\(\s*('[^']*'|` + "`[^`]*`" + `|[A-Za-z]+\([^)]*\))`)
	anyCall := regexp.MustCompile(`api\.(get|post|put|del)\b`)
	var out []string
	for _, f := range acctUIFiles {
		src, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "src", "api", f))
		if err != nil {
			t.Fatal(err)
		}
		found := call.FindAllStringSubmatch(string(src), -1)
		if n := len(anyCall.FindAllString(string(src), -1)); n != len(found) {
			t.Fatalf("%s: %d api calls, %d of a known shape", f, n, len(found))
		}
		for _, m := range found {
			arg := m[2]
			var path string
			switch arg[0] {
			case '\'', '`':
				path = fill.Replace(arg[1 : len(arg)-1])
			default:
				path = helpers[arg]
			}
			if path == "" || strings.ContainsAny(path, "$()?") {
				t.Fatalf("%s: can't resolve the path of %q", f, m[0])
			}
			out = append(out, methods[m[1]]+" /api/v1"+path)
		}
		if f == "backup.ts" {
			if !strings.Contains(string(src), "`/api/v1/backup/file/${enc(name)}`") {
				t.Fatal("backup.ts: fileUrl changed; teach acctUICalls its path")
			}
			out = append(out, "GET /api/v1/backup/file/{backup}")
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// acctGoRoutes reads the Plan 3 route files and returns every pattern passed to
// mux.Handle, with the local path constants expanded.
func acctGoRoutes(t *testing.T) []string {
	t.Helper()
	consts := map[string]string{
		"ssh": "/api/v1/account/ssh-keys",
		"ak":  "/api/v1/account/access-keys",
		"n":   "/api/v1/notifications",
		"b":   "/api/v1/backup",
	}
	handle := regexp.MustCompile(`mux\.Handle\(((?:"[^"]*"|\w+)(?:\s*\+\s*(?:"[^"]*"|\w+))*)\s*,`)
	var out []string
	for _, f := range acctRouteFiles {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		found := handle.FindAllStringSubmatch(string(src), -1)
		if n := strings.Count(string(src), "mux.Handle("); n != len(found) {
			t.Fatalf("%s: %d mux.Handle calls, %d of a known shape", f, n, len(found))
		}
		for _, m := range found {
			var b strings.Builder
			for _, part := range strings.Split(m[1], "+") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, `"`) {
					b.WriteString(strings.Trim(part, `"`))
					continue
				}
				v, ok := consts[part]
				if !ok {
					t.Fatalf("%s: unknown path constant %q in %q", f, part, m[0])
				}
				b.WriteString(v)
			}
			out = append(out, b.String())
		}
	}
	slices.Sort(out)
	return out
}

func TestAccountsContractUICallsCoverEveryRoute(t *testing.T) {
	// The UI calls every Plan 3 route and no other; a route added on either side shows
	// up here.
	ui, routes := acctUICalls(t), acctGoRoutes(t)
	if len(routes) < 70 {
		t.Fatalf("found only %d Plan 3 routes; the route parser missed some", len(routes))
	}
	if !slices.Equal(ui, routes) {
		var onlyUI, onlyGo []string
		for _, c := range ui {
			if !slices.Contains(routes, c) {
				onlyUI = append(onlyUI, c)
			}
		}
		for _, r := range routes {
			if !slices.Contains(ui, r) {
				onlyGo = append(onlyGo, r)
			}
		}
		t.Fatalf("UI calls without a route: %q\nroutes the UI never calls: %q", onlyUI, onlyGo)
	}
}

func TestAccountsContractUICallsAreRegistered(t *testing.T) {
	h := newHarness(t)
	mux := http.NewServeMux()
	(&server{Deps: h.deps}).registerAccountRoutes(mux)
	fill := strings.NewReplacer(
		"{user}", "bob", "{package}", "starter", "{job}", "1", "{id}", "1", "{key}", "aB3dEfGh1JkLmN0pQrSt",
		"{backup}", "bob.2024-01-02_05-10-03.tar", "{snapshot}", "3c9b8a7e",
	)
	for _, c := range acctUICalls(t) {
		method, tmpl, _ := strings.Cut(c, " ")
		path := fill.Replace(tmpl)

		// Each call resolves to exactly its own pattern (not, say, {backup} for exclusions).
		_, pattern := mux.Handler(httptest.NewRequest(method, path, nil))
		if pattern != c {
			t.Errorf("%s %s: matched %q, want %q", method, path, pattern, c)
		}

		// The real server has them wired: signed out and without a CSRF token, they
		// answer 401 (signed-in routes) or 400 "csrf" (the anonymous recovery routes),
		// not the JSON 404 catch-all or a 405.
		req, _ := http.NewRequest(method, h.srv.URL+path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var e APIError
		_ = json.NewDecoder(res.Body).Decode(&e)
		res.Body.Close()
		anon := strings.HasPrefix(tmpl, "/api/v1/auth/")
		switch {
		case anon && (res.StatusCode != http.StatusBadRequest || e.Error.Code != "csrf"):
			t.Errorf("%s %s: status %d %q, want 400 csrf from a registered anonymous route", method, path, res.StatusCode, e.Error.Code)
		case !anon && res.StatusCode != http.StatusUnauthorized:
			t.Errorf("%s %s: status %d, want 401 from a registered route", method, path, res.StatusCode)
		}
	}
}

// The bodies below are copied from the UI tests' assertions (ui/src/designs/{users,
// account,auth,cron,backup}/*.test.tsx) or, where a test checks only part of a body,
// built from the form's submit code with every field it can send.
func TestAccountsContractUIBodiesDecode(t *testing.T) {
	cases := []struct {
		name string
		into any
		body string
	}{
		// Users and packages (Slice A).
		{"users bulk (UsersPage)", &BulkRequest{}, `{"action":"update-counters","names":["bob","ops","susp"]}`},
		{"package add (PackageFormPage)", &PackageRequest{}, `{"name":"gold","webTemplate":"default",
			"backendTemplate":"default","proxyTemplate":"default","dnsTemplate":"default","webDomains":"1","webAliases":"5",
			"dnsDomains":"1","dnsRecords":"unlimited","mailDomains":"1","mailAccounts":"5","rateLimit":"200","databases":"1",
			"cronJobs":"1","diskQuota":"1000","bandwidth":"1000","cpuQuota":"unlimited","cpuQuotaPeriod":"unlimited",
			"memoryLimit":"unlimited","swapLimit":"unlimited","backups":"1","backupsIncremental":"no","shell":"nologin",
			"nameservers":["ns1.example.tld","ns2.example.tld"]}`},
		{"package edit, no DNS", &PackageRequest{}, `{"name":"starter","webTemplate":"default","backendTemplate":"",
			"proxyTemplate":"","dnsTemplate":"","webDomains":"1","webAliases":"5","dnsDomains":"0","dnsRecords":"0",
			"mailDomains":"1","mailAccounts":"5","rateLimit":"200","databases":"1","cronJobs":"1","diskQuota":"1000",
			"bandwidth":"1000","cpuQuota":"unlimited","cpuQuotaPeriod":"unlimited","memoryLimit":"unlimited",
			"swapLimit":"unlimited","backups":"1","backupsIncremental":"no","shell":"nologin","nameservers":[]}`},
		{"packages bulk (PackagesPage)", &BulkRequest{}, `{"action":"delete","names":["starter"]}`},

		// Profile, user editing and 2FA (Slice B).
		{"user add (UserEditPage)", &AddUserRequest{}, `{"username":"newbie","password":"Passw0rdOK",
			"email":"new@example.com","name":"New Bie","language":"en","role":"user","package":"default",
			"loginDisabled":false,"notify":"new@example.com"}`},
		{"account save, user (AccountPage)", &UserEditRequest{}, `{"name":"Bob","email":"bob@acme-bakery.com",
			"password":"Passw0rdOK","currentPassword":"oldpw","language":"en","theme":"","sortOrder":"name",
			"useIpList":true,"allowedIps":["10.0.0.1","10.0.0.2"]}`},
		{"account save, admin", &UserEditRequest{}, `{"name":"System Administrator","email":"admin@example.com",
			"password":"","currentPassword":"","language":"en","theme":"dark","sortOrder":"date","useIpList":false,
			"allowedIps":[],"loginDisabled":false,"package":"system","shell":"bash","phpCli":"8.3",
			"nameservers":["ns1.example.tld","ns2.example.tld"]}`},
		{"user edit (UserEditPage)", &UserEditRequest{}, `{"name":"Bob","email":"bob@acme-bakery.com",
			"password":"Passw0rdOK","currentPassword":"","language":"en","theme":"","sortOrder":"name","useIpList":false,
			"allowedIps":[],"loginDisabled":false,"package":"default","shell":"nologin","role":"admin"}`},
		{"2FA confirm (AccountPage)", &TwoFAConfirmRequest{}, `{"code":"123456","currentPassword":"pw"}`},
		{"2FA disable (AccountPage)", &PasswordConfirmRequest{}, `{"currentPassword":"pw"}`},

		// Recovery (Slice C).
		{"reset request (ResetPasswordPage)", &ResetRequest{}, `{"user":"bob","email":"bob@acme-bakery.com"}`},
		{"reset confirm", &ResetConfirmRequest{}, `{"user":"bob","code":"K7Q2M9XD","password":"Secr3tPass",
			"passwordConfirm":"Secr3tPass"}`},
		{"unlock request (ResetTwofaPage)", &UnlockRequest{}, `{"user":"bob","password":"Secr3tPass"}`},
		{"unlock confirm", &UnlockConfirmRequest{}, `{"user":"bob","code":"ABCDEF"}`},

		// Keys (Slice D); notifications and logs send no body.
		{"ssh key add (SshKeysPage)", &AddSSHKeyRequest{}, `{"key":"ssh-ed25519 AAAAC3 me@laptop"}`},
		{"access key create (AccessKeysPage)", &AddAccessKeyRequest{}, `{"permissions":["mail-accounts"],"comment":"New key"}`},
		{"access keys bulk", &BulkRequest{}, `{"action":"delete","names":["Zx9yWv8uTs7rQp6oNm5l","aB3dEfGh1JkLmN0pQrSt"]}`},

		// Cron (Slice E).
		{"cron add (CronFormPage)", &CronRequest{}, `{"min":"*/5","hour":"*","day":"*","month":"*","wday":"0,6",
			"cmd":"echo 'a' \\; ls"}`},
		{"cron edit", &CronRequest{}, `{"min":"5","hour":"*","day":"*","month":"*","wday":"*","cmd":"php /home/bob/cron.php"}`},
		{"cron bulk (CronJobsPage)", &BulkRequest{}, `{"action":"suspend","names":["1"]}`},
		{"cron reports", &CronReportsRequest{}, `{"enabled":false}`},

		// Backups (Slice F).
		{"backup restore (BackupDetailPage)", &RestoreRequest{}, `{"all":false,"web":["a.com"],"dns":[],"mail":[],
			"db":["x_1","x_2"],"udir":[".ssh"],"cron":true}`},
		{"backup restore all", &RestoreRequest{}, `{"all":true,"web":[],"dns":[],"mail":[],"db":[],"udir":[],"cron":false}`},
		{"snapshot restore (IncrementalPages)", &RestoreRequest{}, `{"all":false,"web":["example.com"],"dns":[],
			"mail":[],"db":[],"udir":[],"cron":false}`},
		{"backups bulk", &BulkRequest{}, `{"action":"delete","names":["bob.2024-01-02_05-10-03.tar"]}`},
		{"snapshots bulk (IncrementalPages)", &BulkRequest{}, `{"action":"delete","names":["3c9b8a7e"]}`},
		{"exclusions save (ExclusionsPages)", &ExclusionsRequest{}, `{"web":"example.com:public_html/cache:public_html/tmp\nshop.example.com",
			"mail":"example.com:info:sales","db":"admin_wp","userdir":"Downloads\n.cache"}`},
	}
	for _, c := range cases {
		dec := json.NewDecoder(bytes.NewReader([]byte(c.body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(c.into); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
