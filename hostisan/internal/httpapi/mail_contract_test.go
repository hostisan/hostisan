package httpapi

// Contract between ui/src/api/mail.ts and the Go mail routes: every call the UI makes
// reaches a registered route, and every body the UI sends decodes into its Go struct
// with no unknown fields.

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

// mailUICalls reads ui/src/api/mail.ts and returns every api call as "METHOD /path",
// with the path helpers expanded to {domain} and {account}. A call whose path it can't
// resolve fails the test, so a new helper or call shape has to be taught here.
func mailUICalls(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "src", "api", "mail.ts"))
	if err != nil {
		t.Fatal(err)
	}
	helpers := map[string]string{
		"domainPath(domain)":           "/mail/{domain}",
		"accountsPath(domain)":         "/mail/{domain}/accounts",
		"accountPath(domain, account)": "/mail/{domain}/accounts/{account}",
	}
	methods := map[string]string{"get": "GET", "post": "POST", "put": "PUT", "del": "DELETE"}
	call := regexp.MustCompile(`api\.(get|post|put|del)<[^>]*>\(\s*('[^']*'|` + "`[^`]*`" + `|[A-Za-z]+\([^)]*\))`)
	var out []string
	for _, m := range call.FindAllStringSubmatch(string(src), -1) {
		arg := m[2]
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
		if path == "" || strings.ContainsAny(path, "$()") {
			t.Fatalf("mail.ts: can't resolve the path of %q", m[0])
		}
		out = append(out, methods[m[1]]+" /api/v1"+path)
	}
	if len(out) == 0 {
		t.Fatal("mail.ts: no api calls found")
	}
	return out
}

func TestMailContractUICallsCoverEveryRoute(t *testing.T) {
	// The UI calls every mail route; a route added on either side shows up here.
	want := []string{
		"GET /api/v1/mail",
		"GET /api/v1/mail/options",
		"GET /api/v1/mail/{domain}",
		"GET /api/v1/mail/{domain}/dns",
		"GET /api/v1/mail/{domain}/accounts",
		"GET /api/v1/mail/{domain}/accounts/{account}",
		"POST /api/v1/mail",
		"PUT /api/v1/mail/{domain}",
		"PUT /api/v1/mail/{domain}/ssl",
		"DELETE /api/v1/mail/{domain}",
		"POST /api/v1/mail/{domain}/suspend",
		"POST /api/v1/mail/{domain}/unsuspend",
		"POST /api/v1/mail/bulk",
		"POST /api/v1/mail/{domain}/accounts",
		"PUT /api/v1/mail/{domain}/accounts/{account}",
		"DELETE /api/v1/mail/{domain}/accounts/{account}",
		"POST /api/v1/mail/{domain}/accounts/{account}/suspend",
		"POST /api/v1/mail/{domain}/accounts/{account}/unsuspend",
		"POST /api/v1/mail/{domain}/accounts/bulk",
	}
	got := mailUICalls(t)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(slices.Compact(got), want) {
		t.Fatalf("mail.ts calls\n got %q\nwant %q", got, want)
	}
}

func TestMailContractUICallsAreRegistered(t *testing.T) {
	h := newHarness(t)
	mux := http.NewServeMux()
	h.mailServer().registerMailRoutes(mux)
	fill := strings.NewReplacer("{domain}", "example.com", "{account}", "info")
	for _, c := range mailUICalls(t) {
		method, tmpl, _ := strings.Cut(c, " ")
		path := fill.Replace(tmpl)

		// The mail routes resolve to exactly this pattern (not, say, {domain} for bulk).
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

// The bodies below are copied from the UI tests' assertions (ui/src/designs/mail/*.test.tsx).
func TestMailContractUIBodiesDecode(t *testing.T) {
	cases := []struct {
		name string
		into any
		body string
	}{
		{"add domain (MailDomainFormPage)", &AddMailDomainRequest{}, `{"domain":" WWW.Example.org ","webmail":"roundcube",
			"antispam":true,"reject":true,"antivirus":true,"dkim":true,"dkimSelector":"mail"}`},
		{"add domain with relay", &AddMailDomainRequest{}, `{"domain":"example.org","webmail":"roundcube","dkim":true,
			"dkimSelector":"mail","smtpRelay":{"host":"smtp.relay.test","port":"587","user":"relayuser","password":"secret-pass"}}`},
		{"edit domain", &EditMailDomainRequest{}, `{"webmail":"roundcube","catchall":"hello@example.com","rate":"",
			"antispam":true,"reject":true,"antivirus":true,"dkim":true,"dkimSelector":"mail","smtpRelay":null}`},
		{"edit domain, server offers less", &EditMailDomainRequest{}, `{"catchall":"","dkim":true,"dkimSelector":"mail","smtpRelay":null}`},
		{"edit domain, stored relay", &EditMailDomainRequest{}, `{"webmail":"","catchall":"","rate":"100","antispam":false,
			"dkim":true,"dkimSelector":"mail","smtpRelay":{"host":"smtp.relay.test","port":"2525","user":"relayuser","password":""}}`},
		{"ssl", &MailSSLRequest{}, `{"ssl":true,"letsencrypt":false,"keyAlgo":"ecdsa-256","crt":"CRT-PEM","key":"KEY-PEM","ca":"CA-PEM"}`},
		{"add account (MailAccountFormPage)", &AddMailAccountRequest{}, `{"account":"hello","password":"Passw0rdOK","quota":"",
			"aliases":"","forwards":"","discard":false,"forwardOnly":false,"rate":"","sendTo":""}`},
		{"edit account", &EditMailAccountRequest{}, `{"password":"","quota":"unlimited","aliases":"",
			"forwards":"owner@gmail.com\narchive@example.org","discard":false,"forwardOnly":true,"autoreply":true,
			"autoreplyMessage":"Thanks for your message.\nWe reply within one business day.\nSales team","rate":"200","sendTo":""}`},
		{"bulk domains (MailDomainsPage)", &BulkRequest{}, `{"action":"rebuild","names":["example.com"]}`},
		{"bulk accounts (MailAccountsPage)", &BulkRequest{}, `{"action":"suspend","names":["info","sales"]}`},
	}
	for _, c := range cases {
		dec := json.NewDecoder(bytes.NewReader([]byte(c.body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(c.into); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
