package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

// mailExtrasSnap is what a spied SSL command found in its temp directory.
type mailExtrasSnap struct {
	Cmd     string
	Dir     string
	DirMode os.FileMode
	Files   map[string]string
	Modes   map[string]os.FileMode
}

// mailExtrasSpy wraps a runner and snapshots the SSL temp directory at call time.
type mailExtrasSpy struct {
	runner.Runner
	snaps    []mailExtrasSnap
	timeouts map[string]time.Duration // per command, the timeout its context carried
}

func (s *mailExtrasSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if s.timeouts == nil {
		s.timeouts = map[string]time.Duration{}
	}
	s.timeouts[cmd], _ = runner.Timeout(ctx)
	if cmd == "v-add-mail-domain-ssl" || cmd == "v-change-mail-domain-sslcert" {
		sn := mailExtrasSnap{Cmd: cmd, Dir: args[2], Files: map[string]string{}, Modes: map[string]os.FileMode{}}
		if st, err := os.Stat(args[2]); err == nil {
			sn.DirMode = st.Mode().Perm()
		}
		ents, _ := os.ReadDir(args[2])
		for _, e := range ents {
			b, _ := os.ReadFile(filepath.Join(args[2], e.Name()))
			sn.Files[e.Name()] = string(b)
			if st, err := e.Info(); err == nil {
				sn.Modes[e.Name()] = st.Mode().Perm()
			}
		}
		s.snaps = append(s.snaps, sn)
	}
	return s.Runner.Run(ctx, cmd, args...)
}

const (
	mailExtrasCrt = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"
	mailExtrasKey = "-----BEGIN PRIVATE KEY-----\nSECRETKEYBYTES\n-----END PRIVATE KEY-----"
	mailExtrasCA  = "-----BEGIN CERTIFICATE-----\nCCCC\n-----END CERTIFICATE-----"
)

// mailExtrasHarness serves domain "ssl.example" (user admin) in the given SSL state.
// exits maps a write command to a failing exit code.
func mailExtrasHarness(t *testing.T, ssl, le, algo string, exits map[string]int) (*harness, *mailExtrasSpy) {
	t.Helper()
	domain := map[string]string{
		"SSL": ssl, "LETSENCRYPT": le, "SSL_KEY_ALGO": algo, "DKIM": "yes", "DKIM_SELECTOR": "mail",
		"ANTIVIRUS": "no", "ANTISPAM": "no", "SUSPENDED": "no", "WEBMAIL": "roundcube",
		"WEBMAIL_ALIAS": "webmail.ssl.example",
	}
	dj, _ := json.Marshal(map[string]any{"ssl.example": domain})
	sj, _ := json.Marshal(map[string]any{"ssl.example": map[string]string{"CRT": mailExtrasCrt, "KEY": mailExtrasKey, "CA": mailExtrasCA}})
	fx := map[string][]fixtureEntry{
		"v-list-mail-domain":     {{Args: []string{"admin", "ssl.example", "json"}, Stdout: string(dj)}},
		"v-list-mail-domain-ssl": {{Args: []string{"admin", "ssl.example", "json"}, Stdout: string(sj)}},
	}
	for cmd, code := range exits {
		var es []fixtureEntry
		for n := 2; n <= 5; n++ {
			args := make([]string, n)
			for i := range args {
				args[i] = "*"
			}
			es = append(es, fixtureEntry{Args: args, Stdout: "Error: boom\n", Exit: code})
		}
		fx[cmd] = es
	}
	spy := &mailExtrasSpy{}
	h := newHarnessWith(t, overrides{Fixtures: fx}, func(d *Deps) {
		spy.Runner = d.R
		d.R = spy
	})
	h.loginAdmin()
	return h, spy
}

func (h *harness) mailExtrasPut(req MailSSLRequest) (int, APIError) {
	var r APIError
	code := h.call("PUT", "/api/v1/mail/ssl.example/ssl", req, &r)
	return code, r
}

func mailExtrasWrites(t *testing.T, h *harness, want ...string) {
	t.Helper()
	got := h.mailWrites()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("writes:\n got  %q\n want %q", got, want)
	}
}

func TestMailSSLManualAdd(t *testing.T) {
	h, spy := mailExtrasHarness(t, "no", "no", "", nil)
	code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, KeyAlgo: "rsa", Crt: mailExtrasCrt, Key: mailExtrasKey, CA: mailExtrasCA})
	if code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	if len(spy.snaps) != 1 {
		t.Fatalf("snaps = %d", len(spy.snaps))
	}
	sn := spy.snaps[0]
	mailExtrasWrites(t, h, "v-add-mail-domain-ssl admin ssl.example "+sn.Dir+" no")
	if sn.DirMode != 0o700 {
		t.Errorf("dir mode %v", sn.DirMode)
	}
	for name, want := range map[string]string{"ssl.example.crt": mailExtrasCrt, "ssl.example.key": mailExtrasKey, "ssl.example.ca": mailExtrasCA} {
		if strings.TrimSpace(sn.Files[name]) != want {
			t.Errorf("%s = %q", name, sn.Files[name])
		}
		if sn.Modes[name] != 0o600 {
			t.Errorf("%s mode %v", name, sn.Modes[name])
		}
	}
	if _, err := os.Stat(sn.Dir); !os.IsNotExist(err) {
		t.Errorf("temp dir left behind: %v", err)
	}
}

func TestMailSSLManualAddWithoutCA(t *testing.T) {
	h, spy := mailExtrasHarness(t, "no", "no", "", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: mailExtrasCrt, Key: mailExtrasKey}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	if _, ok := spy.snaps[0].Files["ssl.example.ca"]; ok || len(spy.snaps[0].Files) != 2 {
		t.Errorf("files = %v", spy.snaps[0].Files)
	}
}

func TestMailSSLManualAddBlank(t *testing.T) {
	h, spy := mailExtrasHarness(t, "no", "no", "", nil)
	code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: " ", Key: ""})
	if code != 400 || r.Error.Message != `Field "%s" can not be blank.` || !reflect.DeepEqual(r.Error.Args, []string{"ssl certificate", "ssl key"}) {
		t.Fatalf("got %d %+v", code, r)
	}
	if w := h.mailWrites(); len(w) != 0 || len(spy.snaps) != 0 {
		t.Fatalf("writes %q", w)
	}
}

func TestMailSSLManualAddFailureCleansUpAndHidesKey(t *testing.T) {
	h, spy := mailExtrasHarness(t, "no", "no", "", map[string]int{"v-add-mail-domain-ssl": 2})
	var raw map[string]any
	code := h.call("PUT", "/api/v1/mail/ssl.example/ssl",
		MailSSLRequest{SSL: true, Crt: mailExtrasCrt, Key: mailExtrasKey}, &raw)
	if code != 400 {
		t.Fatalf("got %d %v", code, raw)
	}
	if b, _ := json.Marshal(raw); strings.Contains(string(b), "SECRETKEYBYTES") {
		t.Errorf("key in reply: %s", b)
	}
	if _, err := os.Stat(spy.snaps[0].Dir); !os.IsNotExist(err) {
		t.Errorf("temp dir left behind: %v", err)
	}
	for _, c := range h.mailCalls() {
		if strings.Contains(c, "SECRETKEYBYTES") {
			t.Errorf("key in call: %s", c)
		}
	}
}

func TestMailSSLUnchangedCertRunsNothing(t *testing.T) {
	h, _ := mailExtrasHarness(t, "yes", "no", "", nil)
	crlf := strings.ReplaceAll(mailExtrasCrt, "\n", "\r\n")
	if code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: crlf, Key: mailExtrasKey, CA: mailExtrasCA}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h)
}

func TestMailSSLManualReplace(t *testing.T) {
	h, spy := mailExtrasHarness(t, "yes", "no", "", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: "NEWCRT", Key: mailExtrasKey, CA: mailExtrasCA}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	sn := spy.snaps[0]
	mailExtrasWrites(t, h, "v-change-mail-domain-sslcert admin ssl.example "+sn.Dir+" no")
	if sn.Cmd != "v-change-mail-domain-sslcert" || strings.TrimSpace(sn.Files["ssl.example.crt"]) != "NEWCRT" || sn.DirMode != 0o700 {
		t.Errorf("snap = %+v", sn)
	}
	if _, err := os.Stat(sn.Dir); !os.IsNotExist(err) {
		t.Errorf("temp dir left behind")
	}
}

func TestMailSSLReplaceFailureCleansUp(t *testing.T) {
	h, spy := mailExtrasHarness(t, "yes", "no", "", map[string]int{"v-change-mail-domain-sslcert": 3})
	if code, _ := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: "NEWCRT", Key: mailExtrasKey}); code != 404 {
		t.Fatalf("got %d", code)
	}
	if _, err := os.Stat(spy.snaps[0].Dir); !os.IsNotExist(err) {
		t.Errorf("temp dir left behind")
	}
}

func TestMailSSLLetsEncryptKeyChange(t *testing.T) {
	h, _ := mailExtrasHarness(t, "yes", "yes", "ecdsa-256", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "rsa"}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h, "v-add-letsencrypt-domain admin ssl.example   yes rsa")
	// An invalid type defaults to ecdsa-256, which is the stored one: nothing to do.
	h, _ = mailExtrasHarness(t, "yes", "yes", "ecdsa-256", nil)
	h.mailExtrasPut(MailSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "bogus"})
	mailExtrasWrites(t, h)
}

func TestMailSSLLetsEncryptOn(t *testing.T) {
	h, _ := mailExtrasHarness(t, "no", "no", "", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "ecdsa-384"}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h, "v-add-letsencrypt-domain admin ssl.example   yes ecdsa-384")
	h, _ = mailExtrasHarness(t, "no", "no", "", nil)
	h.mailExtrasPut(MailSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "weird"})
	mailExtrasWrites(t, h, "v-add-letsencrypt-domain admin ssl.example   yes ecdsa-256")
}

func TestMailSSLLetsEncryptOffKeepsSSL(t *testing.T) {
	h, spy := mailExtrasHarness(t, "yes", "yes", "ecdsa-256", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: mailExtrasCrt, Key: mailExtrasKey, CA: mailExtrasCA}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h, "v-delete-letsencrypt-domain admin ssl.example  yes",
		"v-add-mail-domain-ssl admin ssl.example "+spy.snaps[0].Dir+" no")
}

// Let's Encrypt issues two names with retries and restarts; the runner's default would
// kill it mid-issue. Other commands keep the default.
func TestMailSSLLetsEncryptCommandsGetLongerTimeout(t *testing.T) {
	cases := []struct {
		ssl, le string
		req     MailSSLRequest
		le85    []string
	}{
		{"no", "no", MailSSLRequest{SSL: true, LetsEncrypt: true}, []string{"v-add-letsencrypt-domain"}},
		{"yes", "yes", MailSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "rsa"}, []string{"v-add-letsencrypt-domain"}},
		{"yes", "yes", MailSSLRequest{}, []string{"v-delete-letsencrypt-domain"}},
	}
	for _, c := range cases {
		h, spy := mailExtrasHarness(t, c.ssl, c.le, "ecdsa-256", nil)
		if code, r := h.mailExtrasPut(c.req); code != 200 {
			t.Fatalf("got %d %+v", code, r)
		}
		for _, cmd := range c.le85 {
			if got := spy.timeouts[cmd]; got < 85*time.Second || got >= 90*time.Second {
				t.Errorf("%s timeout = %v, want 85s..90s", cmd, got)
			}
		}
		for cmd, got := range spy.timeouts {
			if !strings.Contains(cmd, "letsencrypt") && got != 0 {
				t.Errorf("%s timeout = %v, want the default", cmd, got)
			}
		}
	}
	h, spy := mailExtrasHarness(t, "yes", "no", "", nil)
	h.mailExtrasPut(MailSSLRequest{})
	if got, ok := spy.timeouts["v-delete-mail-domain-ssl"]; !ok || got != 0 {
		t.Errorf("v-delete-mail-domain-ssl timeout = %v (ran %v), want the default", got, ok)
	}
}

func TestMailSSLLetsEncryptOffNeedsCertificate(t *testing.T) {
	h, _ := mailExtrasHarness(t, "yes", "yes", "ecdsa-256", nil)
	code, r := h.mailExtrasPut(MailSSLRequest{SSL: true})
	if code != 400 || !reflect.DeepEqual(r.Error.Args, []string{"ssl certificate", "ssl key"}) {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h)
}

func TestMailSSLLetsEncryptOffSSLOff(t *testing.T) {
	h, _ := mailExtrasHarness(t, "yes", "yes", "ecdsa-256", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h, "v-delete-letsencrypt-domain admin ssl.example  yes")
}

func TestMailSSLOff(t *testing.T) {
	h, _ := mailExtrasHarness(t, "yes", "no", "", nil)
	if code, r := h.mailExtrasPut(MailSSLRequest{}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailExtrasWrites(t, h, "v-delete-mail-domain-ssl admin ssl.example")
	h, _ = mailExtrasHarness(t, "no", "no", "", nil)
	h.mailExtrasPut(MailSSLRequest{})
	mailExtrasWrites(t, h)
}

func TestMailSSLStopsAtFirstError(t *testing.T) {
	h, spy := mailExtrasHarness(t, "yes", "yes", "ecdsa-256", map[string]int{"v-delete-letsencrypt-domain": 4})
	code, _ := h.mailExtrasPut(MailSSLRequest{SSL: true, Crt: mailExtrasCrt, Key: mailExtrasKey})
	if code != 409 || len(spy.snaps) != 0 {
		t.Fatalf("got %d, snaps %d", code, len(spy.snaps))
	}
	mailExtrasWrites(t, h, "v-delete-letsencrypt-domain admin ssl.example  yes")
}

func TestMailSSLGuards(t *testing.T) {
	h, _ := mailExtrasHarness(t, "no", "no", "", nil)
	if code := h.call("PUT", "/api/v1/mail/-bad/ssl", MailSSLRequest{}, nil); code != 400 {
		t.Fatalf("bad domain: %d", code)
	}
	h = newHarnessWith(t, overrides{})
	h.loginAdmin()
	if code := h.call("PUT", "/api/v1/mail/missing.example/ssl", MailSSLRequest{SSL: true}, nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var r APIError
	if code := h.call("PUT", "/api/v1/mail/example.com/ssl", MailSSLRequest{}, &r); code != 403 || r.Error.Code != "read_only" {
		t.Fatalf("read-only: %d %+v", code, r)
	}
	if w := h.mailCalls(); len(w) != 0 {
		t.Fatalf("calls %q", w)
	}
}

func dnsRow(rec, typ, prio, ttl, val string) MailDNSRow {
	return MailDNSRow{Record: rec, Type: typ, Priority: prio, TTL: ttl, Value: val}
}

func TestMailDNSRecords(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v MailDNSView
	if code := h.call("GET", "/api/v1/mail/example.com/dns", nil, &v); code != 200 {
		t.Fatalf("got %d", code)
	}
	want := []MailDNSRow{
		dnsRow("mail.example.com", "A", "", "14400", "203.0.113.11"),
		dnsRow("webmail.example.com", "A", "", "14400", "203.0.113.11"),
		dnsRow("example.com", "MX", "10", "14400", "mail.example.com."),
		dnsRow("example.com", "TXT", "", "14400", "v=spf1 a mx ip4:203.0.113.11 -all"),
		dnsRow("_dmarc", "TXT", "", "14400", "v=DMARC1; p=quarantine; pct=100"),
		dnsRow("mail._domainkey", "TXT", "", "3600", "v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAdevExampleCom...IDAQAB"),
	}
	if !reflect.DeepEqual(v.Records, want) {
		t.Fatalf("records:\n got  %+v\n want %+v", v.Records, want)
	}
}

func TestMailDNSNoWebmailNATAndDKIMOff(t *testing.T) {
	ips := `{"203.0.113.10":{"OWNER":"admin","STATUS":"shared","NAME":"a","NAT":""},"10.0.0.9":{"OWNER":"admin","STATUS":"shared","NAME":"b","NAT":"198.51.100.9"}}`
	dkim := `{"mail._domainkey":{"TTL":"3600","TXT":"'v=DKIM1; k=rsa; p=DKIM-SUPPORT-IS-NOT-ACTIVATED'"}}`
	h := newHarnessWith(t, overrides{
		Config: map[string]string{"WEBMAIL_SYSTEM": ""},
		Fixtures: map[string][]fixtureEntry{
			"v-list-user-ips":             {{Args: []string{"admin", "json"}, Stdout: ips}},
			"v-list-mail-domain-dkim-dns": {{Args: []string{"admin", "example.com", "json"}, Stdout: dkim}},
			"v-list-mail-domain":          {{Args: []string{"admin", "example.com", "json"}, Stdout: `{"example.com":{"DKIM":"no"}}`}},
		},
	})
	h.loginAdmin()
	var v MailDNSView
	if code := h.call("GET", "/api/v1/mail/example.com/dns", nil, &v); code != 200 {
		t.Fatalf("got %d", code)
	}
	want := []MailDNSRow{
		dnsRow("mail.example.com", "A", "", "14400", "198.51.100.9"),
		dnsRow("example.com", "MX", "10", "14400", "mail.example.com."),
		dnsRow("example.com", "TXT", "", "14400", "v=spf1 a mx ip4:198.51.100.9 -all"),
		dnsRow("_dmarc", "TXT", "", "14400", "v=DMARC1; p=quarantine; pct=100"),
		dnsRow("mail._domainkey", "TXT", "", "3600", "v=DKIM1; k=rsa; p=DKIM-SUPPORT-IS-NOT-ACTIVATED"),
	}
	if !reflect.DeepEqual(v.Records, want) {
		t.Fatalf("records:\n got  %+v\n want %+v", v.Records, want)
	}
}

func TestMailDNSCustomWebmailAliasAndErrors(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"WEBMAIL_ALIAS": "mx-web"}})
	h.loginAdmin()
	var v MailDNSView
	h.call("GET", "/api/v1/mail/example.com/dns", nil, &v)
	if len(v.Records) < 2 || v.Records[1].Record != "mx-web.example.com" {
		t.Fatalf("records %+v", v.Records)
	}
	for path, want := range map[string]int{"/api/v1/mail/missing.example/dns": 404, "/api/v1/mail/-bad/dns": 400} {
		if code := h.call("GET", path, nil, nil); code != want {
			t.Errorf("%s: %d want %d", path, code, want)
		}
	}
}

func TestMailDNSAllowedReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	if code := h.call("GET", "/api/v1/mail/example.com/dns", nil, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
}
