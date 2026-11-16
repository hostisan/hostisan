package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func mailDomainsHarness(t *testing.T, o overrides, opts ...func(*Deps)) *harness {
	t.Helper()
	return newHarnessWith(t, o, opts...)
}

func mailDomainsBool(v bool) *bool    { return &v }
func mailDomainsStr(v string) *string { return &v }

// mailDomainsSame is an edit request that matches the stored state of example.com.
func mailDomainsSame() EditMailDomainRequest {
	return EditMailDomainRequest{
		Webmail: mailDomainsStr("roundcube"), Rate: mailDomainsStr(""),
		Antispam: mailDomainsBool(true), Reject: mailDomainsBool(true), Antivirus: mailDomainsBool(true),
		DKIM: true, DKIMSelector: "mail",
	}
}

func mailDomainsWrites(t *testing.T, h *harness, want ...string) {
	t.Helper()
	got := h.mailWrites()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("writes:\n%q\nwant:\n%q", got, want)
	}
}

func mailDomainsEdit(t *testing.T, h *harness, domain string, req EditMailDomainRequest) (int, statusReply) {
	t.Helper()
	var r statusReply
	code := h.call("PUT", "/api/v1/mail/"+domain, req, &r)
	return code, r
}

func TestMailDomainsList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r MailDomains
	if code := h.call("GET", "/api/v1/mail", nil, &r); code != 200 {
		t.Fatalf("got %d", code)
	}
	if len(r.Domains) != 2 || r.Domains[0].NAME != "example.com" || r.Domains[1].NAME != "example.net" {
		t.Fatalf("domains = %+v", r.Domains)
	}
	want := MailUsage{Domains: "2", DomainsLimit: "unlimited", Suspended: "1", Accounts: "3", AccountsLimit: "unlimited"}
	if r.Usage != want {
		t.Fatalf("usage = %+v, want %+v", r.Usage, want)
	}
	h = newHarness(t)
	h.loginBob()
	if code := h.call("GET", "/api/v1/mail", nil, &r); code != 200 {
		t.Fatalf("bob: %d", code)
	}
	if len(r.Domains) != 1 || r.Domains[0].NAME != "bakery.example" || r.Usage.AccountsLimit != "5" {
		t.Fatalf("bob: %+v", r)
	}
}

func TestMailDomainsOptions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var o MailOptions
	if code := h.call("GET", "/api/v1/mail/options", nil, &o); code != 200 {
		t.Fatalf("got %d", code)
	}
	if len(o.WebmailClients) != 2 || o.WebmailClients[0] != "roundcube" || o.WebmailAlias != "webmail" ||
		!o.Antispam || !o.Antivirus || !o.Webmail || !o.Imap || !o.CanSetRate || !o.AdminGate ||
		strings.Join(o.KeyAlgos, ",") != "ecdsa-256,ecdsa-384,rsa" {
		t.Fatalf("options = %+v", o)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{
		"WEBMAIL_SYSTEM": "", "WEBMAIL_ALIAS": "", "ANTISPAM_SYSTEM": "", "ANTIVIRUS_SYSTEM": "", "IMAP_SYSTEM": ""}})
	h.loginBob()
	if code := h.call("GET", "/api/v1/mail/options", nil, &o); code != 200 {
		t.Fatalf("bob: %d", code)
	}
	if o.WebmailClients == nil || len(o.WebmailClients) != 0 || o.WebmailAlias != "webmail" ||
		o.Antispam || o.Antivirus || o.Webmail || o.Imap || o.CanSetRate || o.AdminGate {
		t.Fatalf("bob options = %+v", o)
	}
}

func TestMailDomainsOne(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v MailDomainView
	if code := h.call("GET", "/api/v1/mail/example.com", nil, &v); code != 200 {
		t.Fatalf("got %d", code)
	}
	if v.Domain.NAME != "example.com" || v.SSL == nil || !strings.Contains(v.SSL.KEY, "PRIVATE KEY") {
		t.Fatalf("view = %+v", v)
	}
	// SSL "no": the certificate is not read.
	v = MailDomainView{}
	if code := h.call("GET", "/api/v1/mail/example.net", nil, &v); code != 200 || v.SSL != nil {
		t.Fatalf("example.net: %d %+v", code, v)
	}
	for _, c := range h.mailCalls() {
		if strings.HasPrefix(c, "v-list-mail-domain-ssl admin example.net") {
			t.Fatalf("read the ssl of a domain without ssl: %q", h.mailCalls())
		}
	}
	var r statusReply
	if code := h.call("GET", "/api/v1/mail/missing.example", nil, &r); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code := h.call("GET", "/api/v1/mail/-h", nil, &r); code != 400 {
		t.Fatalf("-h: %d", code)
	}
}

func addAll() AddMailDomainRequest {
	return AddMailDomainRequest{
		Domain: "  WWW.Example.ORG ", Webmail: "roundcube", Antispam: true, Reject: true, Antivirus: true,
		DKIM: true, DKIMSelector: "s1",
		SMTPRelay: &MailSMTPRelay{Host: "smtp.relay.example", User: "ru", Password: "S3cretPw"},
	}
}

func TestMailDomainsAddAllOptions(t *testing.T) {
	tmp := t.TempDir()
	var path, content string
	var mode os.FileMode
	h := mailDomainsHarness(t, overrides{}, func(d *Deps) {
		d.TmpDir = tmp
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-add-mail-domain-smtp-relay" {
				return
			}
			path = args[4]
			if fi, err := os.Stat(path); err == nil {
				mode = fi.Mode().Perm()
			}
			b, _ := os.ReadFile(path)
			content = string(b)
		}}
	})
	h.loginAdmin()
	var r OK
	if code := h.call("POST", "/api/v1/mail", addAll(), &r); code != 200 || r.Status != "ok" {
		t.Fatalf("got %d %+v", code, r)
	}
	if !strings.HasPrefix(path, tmp+"/") || mode != 0o600 || content != "S3cretPw\n" {
		t.Fatalf("relay file %q mode %v content %q", path, mode, content)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("relay file still there: %v", err)
	}
	mailDomainsWrites(t, h,
		cmdLine("v-add-mail-domain", "admin", "example.org", "yes", "yes", "yes", "2048", "", "", "s1"),
		cmdLine("v-add-mail-domain-reject", "admin", "example.org", "yes"),
		cmdLine("v-add-mail-domain-webmail", "admin", "example.org", "roundcube", "yes"),
		cmdLine("v-add-mail-domain-smtp-relay", "admin", "example.org", "smtp.relay.example", "ru", path, "587"),
	)
	if strings.Contains(strings.Join(h.mailCalls(), "\n"), "S3cretPw") {
		t.Fatalf("the password is in the call log: %q", h.mailCalls())
	}
}

func TestMailDomainsAddMinimal(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := AddMailDomainRequest{Domain: "example.org"}
	var r OK
	if code := h.call("POST", "/api/v1/mail", req, &r); code != 200 {
		t.Fatalf("got %d", code)
	}
	// No webmail client chosen: the client is removed. No relay: nothing more.
	mailDomainsWrites(t, h,
		cmdLine("v-add-mail-domain", "admin", "example.org", "no", "no", "no", "2048", "", "", "mail"),
		cmdLine("v-delete-mail-domain-webmail", "admin", "example.org", "yes"),
	)
}

func TestMailDomainsAddNoAntispamSystem(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"ANTISPAM_SYSTEM": "", "WEBMAIL_SYSTEM": ""}})
	h.loginAdmin()
	req := AddMailDomainRequest{Domain: "example.org", Antispam: true, Reject: true, Antivirus: true, DKIM: true, Webmail: "roundcube"}
	if code := h.call("POST", "/api/v1/mail", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	// Reject is not sent, and without a webmail system there is no webmail call.
	mailDomainsWrites(t, h,
		cmdLine("v-add-mail-domain", "admin", "example.org", "no", "yes", "yes", "2048", "", "", "mail"),
	)
}

func TestMailDomainsAddPartialFailure(t *testing.T) {
	h := mailDomainsHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-add-mail-domain-webmail": {{Args: []string{"*", "*", "*", "*"}, Stdout: "Error: WEB_SYSTEM is not enabled\n", Exit: 11}},
	}})
	h.loginAdmin()
	var r statusReply
	code := h.call("POST", "/api/v1/mail", addAll(), &r)
	if code != 409 || r.Error.Code != "disabled" || r.Error.Message != "Error: WEB_SYSTEM is not enabled" {
		t.Fatalf("got %d %+v", code, r)
	}
	// The domain stays; the relay is not tried.
	mailDomainsWrites(t, h,
		cmdLine("v-add-mail-domain", "admin", "example.org", "yes", "yes", "yes", "2048", "", "", "s1"),
		cmdLine("v-add-mail-domain-reject", "admin", "example.org", "yes"),
		cmdLine("v-add-mail-domain-webmail", "admin", "example.org", "roundcube", "yes"),
	)
}

func TestMailDomainsAddStopsAtFirstError(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-add-mail-domain": {{Args: []string{"*", "*", "*", "*", "*", "*", "*", "*", "*"}, Stdout: "Error: mail domain exists\n", Exit: 4}},
	}})
	h.loginAdmin()
	var r statusReply
	if code := h.call("POST", "/api/v1/mail", addAll(), &r); code != 409 || r.Error.Code != "exists" {
		t.Fatalf("got %d %+v", code, r)
	}
	if w := h.mailWrites(); len(w) != 1 {
		t.Fatalf("writes = %q", w)
	}
}

func TestMailDomainsAddValidation(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r APIError
	if code := h.call("POST", "/api/v1/mail", AddMailDomainRequest{Domain: "  "}, &r); code != 400 ||
		r.Error.Message != blankFieldMsg || len(r.Error.Args) != 1 || r.Error.Args[0] != "Domain" {
		t.Fatalf("blank: %d %+v", code, r)
	}
	for name, req := range map[string]AddMailDomainRequest{
		"bad domain":    {Domain: "-x.example"},
		"bad selector":  {Domain: "example.org", DKIMSelector: "-bad-"},
		"relay no host": {Domain: "example.org", SMTPRelay: &MailSMTPRelay{User: "u", Password: "p"}},
		"relay no pass": {Domain: "example.org", SMTPRelay: &MailSMTPRelay{Host: "h", User: "u"}},
		"pass has \\n":  {Domain: "example.org", SMTPRelay: &MailSMTPRelay{Host: "h", User: "u", Password: "a\nb"}},
	} {
		r = APIError{}
		if code := h.call("POST", "/api/v1/mail", req, &r); code != 400 {
			t.Fatalf("%s: %d %+v", name, code, r)
		}
	}
	r = APIError{}
	h.call("POST", "/api/v1/mail", AddMailDomainRequest{Domain: "example.org", SMTPRelay: &MailSMTPRelay{Host: "h", User: "u"}}, &r)
	if r.Error.Message != blankFieldMsg || len(r.Error.Args) != 1 || r.Error.Args[0] != "Password" {
		t.Fatalf("relay password: %+v", r)
	}
	if w := h.mailWrites(); len(w) != 0 {
		t.Fatalf("writes = %q", w)
	}
}

func TestMailDomainsAddUnauthenticatedRelay(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := AddMailDomainRequest{Domain: "example.org", Webmail: "roundcube", SMTPRelay: &MailSMTPRelay{Host: "smtp.relay.example", Port: "2525"}}
	if code := h.call("POST", "/api/v1/mail", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	w := h.mailWrites()
	if len(w) != 3 || w[2] != cmdLine("v-add-mail-domain-smtp-relay", "admin", "example.org", "smtp.relay.example", "", "", "2525") {
		t.Fatalf("writes = %q", w)
	}
}

func TestMailDomainsEditNothingChanged(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code, r := mailDomainsEdit(t, h, "example.com", mailDomainsSame()); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h)
	// Reads are not wasted either: one domain listing.
	if c := h.mailCalls(); len(c) != 1 || c[0] != cmdLine("v-list-mail-domain", "admin", "example.com", "json") {
		t.Fatalf("calls = %q", c)
	}
}

func TestMailDomainsEditAntispamOnFromEmpty(t *testing.T) {
	h := mailDomainsHarness(t, overrides{})
	h.loginAdmin()
	req := EditMailDomainRequest{Antispam: mailDomainsBool(true), Antivirus: mailDomainsBool(true), DKIM: true}
	if code, r := mailDomainsEdit(t, h, "blank.example", req); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	// The stored values are "" (not "no"); Parity 2 still turns them on.
	mailDomainsWrites(t, h,
		cmdLine("v-add-mail-domain-antispam", "admin", "blank.example"),
		cmdLine("v-add-mail-domain-antivirus", "admin", "blank.example"),
		cmdLine("v-add-mail-domain-dkim", "admin", "blank.example", "", "mail"),
	)
}

func TestMailDomainsEditAntispamAbsent(t *testing.T) {
	h := mailDomainsHarness(t, overrides{})
	h.loginAdmin()
	// Not offered: antispam, reject, antivirus, webmail and rate are all left out.
	if code, r := mailDomainsEdit(t, h, "example.com", EditMailDomainRequest{DKIM: true, DKIMSelector: "mail"}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h)
}

func TestMailDomainsEditTurnOff(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := mailDomainsSame()
	req.Antispam, req.Reject, req.Antivirus, req.DKIM = mailDomainsBool(false), mailDomainsBool(false), mailDomainsBool(false), false
	if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h,
		cmdLine("v-delete-mail-domain-antispam", "admin", "example.com"),
		cmdLine("v-delete-mail-domain-antivirus", "admin", "example.com"),
		cmdLine("v-delete-mail-domain-dkim", "admin", "example.com"),
		cmdLine("v-delete-mail-domain-reject", "admin", "example.com", "yes"),
	)
}

func TestMailDomainsEditRejectOn(t *testing.T) {
	h := mailDomainsHarness(t, overrides{})
	h.loginAdmin()
	// relay.example has reject "no" and antispam on.
	req := EditMailDomainRequest{Antispam: mailDomainsBool(true), Reject: mailDomainsBool(true), Antivirus: mailDomainsBool(true),
		DKIM: true, DKIMSelector: "mail", SMTPRelay: &MailSMTPRelay{Host: "smtp.relay.example", Port: "587", User: "relayuser"}}
	if code, r := mailDomainsEdit(t, h, "relay.example", req); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h, cmdLine("v-add-mail-domain-reject", "admin", "relay.example", "yes"))
}

func TestMailDomainsEditDKIMSelectorChange(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := mailDomainsSame()
	req.DKIMSelector = "s2"
	if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h,
		cmdLine("v-delete-mail-domain-dkim", "admin", "example.com"),
		cmdLine("v-add-mail-domain-dkim", "admin", "example.com", "", "s2"),
	)
}

func TestMailDomainsEditWebmail(t *testing.T) {
	for _, c := range []struct {
		webmail string
		want    []string
	}{
		{"roundcube", nil},
		{"snappymail", []string{cmdLine("v-add-mail-domain-webmail", "admin", "example.com", "snappymail", "yes")}},
		{"disabled", []string{cmdLine("v-add-mail-domain-webmail", "admin", "example.com", "disabled", "yes")}},
		{"", []string{cmdLine("v-delete-mail-domain-webmail", "admin", "example.com", "yes")}},
	} {
		h := newHarness(t)
		h.loginAdmin()
		req := mailDomainsSame()
		req.Webmail = mailDomainsStr(c.webmail)
		if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
			t.Fatalf("%q: %d %+v", c.webmail, code, r)
		}
		mailDomainsWrites(t, h, c.want...)
	}
}

func TestMailDomainsEditCatchall(t *testing.T) {
	// example.com has none: add.
	h := newHarness(t)
	h.loginAdmin()
	req := mailDomainsSame()
	req.Catchall = "all@example.com"
	if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
		t.Fatalf("add: %d %+v", code, r)
	}
	mailDomainsWrites(t, h, cmdLine("v-add-mail-domain-catchall", "admin", "example.com", "all@example.com"))

	// example.net has boss@example.net: change, keep, delete.
	same := EditMailDomainRequest{Catchall: "boss@example.net", Rate: mailDomainsStr("100")}
	for _, c := range []struct {
		catchall string
		want     []string
	}{
		{"boss@example.net", nil},
		{"new@example.net", []string{cmdLine("v-change-mail-domain-catchall", "admin", "example.net", "new@example.net")}},
		{"", []string{cmdLine("v-delete-mail-domain-catchall", "admin", "example.net")}},
	} {
		h = newHarness(t)
		h.loginAdmin()
		req := same
		req.Catchall = c.catchall
		if code, r := mailDomainsEdit(t, h, "example.net", req); code != 200 {
			t.Fatalf("%q: %d %+v", c.catchall, code, r)
		}
		mailDomainsWrites(t, h, c.want...)
	}
}

func TestMailDomainsEditRate(t *testing.T) {
	// Admin: "" means system, a number is sent as is, the stored value is not re-sent.
	for _, c := range []struct {
		rate string
		want []string
	}{
		{"", []string{cmdLine("v-change-mail-domain-rate-limit", "admin", "example.net", "system")}},
		{"250", []string{cmdLine("v-change-mail-domain-rate-limit", "admin", "example.net", "250")}},
		{"100", nil},
	} {
		h := newHarness(t)
		h.loginAdmin()
		req := EditMailDomainRequest{Catchall: "boss@example.net", Rate: mailDomainsStr(c.rate)}
		if code, r := mailDomainsEdit(t, h, "example.net", req); code != 200 {
			t.Fatalf("%q: %d %+v", c.rate, code, r)
		}
		mailDomainsWrites(t, h, c.want...)
	}
	// Bob: ignored.
	h := newHarness(t)
	h.loginBob()
	req := EditMailDomainRequest{Rate: mailDomainsStr("999"), Antispam: mailDomainsBool(true), Antivirus: mailDomainsBool(true),
		Reject: mailDomainsBool(false), DKIM: true, DKIMSelector: "mail", Webmail: mailDomainsStr("snappymail"),
		SMTPRelay: &MailSMTPRelay{Host: "smtp.relay.example", Port: "587", User: "bakery"}}
	if code, r := mailDomainsEdit(t, h, "bakery.example", req); code != 200 {
		t.Fatalf("bob: %d %+v", code, r)
	}
	mailDomainsWrites(t, h)
	// An admin looking at bob is still the admin.
	h = newHarness(t)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if code, r := mailDomainsEdit(t, h, "bakery.example", req); code != 200 {
		t.Fatalf("admin as bob: %d %+v", code, r)
	}
	mailDomainsWrites(t, h, cmdLine("v-change-mail-domain-rate-limit", "bob", "bakery.example", "999"))
}

func TestMailDomainsEditRelayOff(t *testing.T) {
	// "false" (example.com): nothing runs.
	h := newHarness(t)
	h.loginAdmin()
	if code, r := mailDomainsEdit(t, h, "example.com", mailDomainsSame()); code != 200 {
		t.Fatalf("%d %+v", code, r)
	}
	mailDomainsWrites(t, h)
	// "true" (bakery.example): deleted.
	h = newHarness(t)
	h.loginBob()
	req := EditMailDomainRequest{Antispam: mailDomainsBool(true), Antivirus: mailDomainsBool(true), Reject: mailDomainsBool(false),
		DKIM: true, DKIMSelector: "mail", Webmail: mailDomainsStr("snappymail")}
	if code, r := mailDomainsEdit(t, h, "bakery.example", req); code != 200 {
		t.Fatalf("%d %+v", code, r)
	}
	mailDomainsWrites(t, h, cmdLine("v-delete-mail-domain-smtp-relay", "bob", "bakery.example"))
}

func TestMailDomainsEditRelayWrite(t *testing.T) {
	tmp := t.TempDir()
	var path, content string
	var mode os.FileMode
	spy := func(d *Deps) {
		d.TmpDir = tmp
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd == "v-add-mail-domain-smtp-relay" && len(args) > 4 {
				path = args[4]
				if fi, err := os.Stat(path); err == nil {
					mode = fi.Mode().Perm()
				}
				b, _ := os.ReadFile(path)
				content = string(b)
			}
		}}
	}
	// Enabled from off, with a password.
	h := newHarnessWith(t, overrides{}, spy)
	h.loginAdmin()
	req := mailDomainsSame()
	req.SMTPRelay = &MailSMTPRelay{Host: "smtp.new.example", Port: "", User: "nu", Password: "N3wPass"}
	if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
		t.Fatalf("%d %+v", code, r)
	}
	if mode != 0o600 || content != "N3wPass\n" {
		t.Fatalf("file mode %v content %q", mode, content)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("relay file still there: %v", err)
	}
	mailDomainsWrites(t, h, cmdLine("v-add-mail-domain-smtp-relay", "admin", "example.com", "smtp.new.example", "nu", path, "587"))
	if strings.Contains(strings.Join(h.mailCalls(), "\n"), "N3wPass") {
		t.Fatalf("password in call log: %q", h.mailCalls())
	}

	// relay.example is on with port "" (587): the same values with no password run nothing.
	h = mailDomainsHarness(t, overrides{})
	h.loginAdmin()
	same := EditMailDomainRequest{Antispam: mailDomainsBool(true), Antivirus: mailDomainsBool(true), Reject: mailDomainsBool(false),
		DKIM: true, DKIMSelector: "mail", SMTPRelay: &MailSMTPRelay{Host: "smtp.relay.example", Port: "587", User: "relayuser"}}
	if code, r := mailDomainsEdit(t, h, "relay.example", same); code != 200 {
		t.Fatalf("%d %+v", code, r)
	}
	mailDomainsWrites(t, h)

	// A changed port without a password cannot keep the stored one: blank-field error, nothing runs.
	h = mailDomainsHarness(t, overrides{})
	h.loginAdmin()
	req = same
	req.SMTPRelay = &MailSMTPRelay{Host: "smtp.relay.example", Port: "2525", User: "relayuser"}
	var r APIError
	if code := h.call("PUT", "/api/v1/mail/relay.example", req, &r); code != 400 ||
		r.Error.Message != blankFieldMsg || len(r.Error.Args) != 1 || r.Error.Args[0] != "Password" {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h)

	// A typed password alone rewrites it; no host is a blank-field error too.
	h = mailDomainsHarness(t, overrides{})
	h.loginAdmin()
	req.SMTPRelay = &MailSMTPRelay{Host: "smtp.relay.example", Port: "587", User: "relayuser", Password: "x"}
	if code := h.call("PUT", "/api/v1/mail/relay.example", req, &r); code != 200 {
		t.Fatalf("got %d", code)
	}
	if w := h.mailWrites(); len(w) != 1 || !strings.HasPrefix(w[0], "v-add-mail-domain-smtp-relay admin relay.example smtp.relay.example relayuser ") || !strings.HasSuffix(w[0], " 587") {
		t.Fatalf("writes = %q", w)
	}
	req.SMTPRelay = &MailSMTPRelay{Port: "587", User: "u", Password: "x"}
	r = APIError{}
	if code := h.call("PUT", "/api/v1/mail/relay.example", req, &r); code != 400 || r.Error.Args[0] != "Host" {
		t.Fatalf("no host: %d %+v", code, r)
	}
}

func TestMailDomainsEditStopsAtFirstError(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-mail-domain-antispam": {{Args: []string{"*", "*"}, Stdout: "Error: boom\n", Exit: 3}},
	}})
	h.loginAdmin()
	req := mailDomainsSame()
	req.Antispam, req.DKIM = mailDomainsBool(false), false
	var r statusReply
	if code := h.call("PUT", "/api/v1/mail/example.com", req, &r); code != 404 || r.Error.Message != "Error: boom" {
		t.Fatalf("got %d %+v", code, r)
	}
	mailDomainsWrites(t, h, cmdLine("v-delete-mail-domain-antispam", "admin", "example.com"))
}

func TestMailDomainsEditMissing(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code, _ := mailDomainsEdit(t, h, "missing.example", mailDomainsSame()); code != 404 {
		t.Fatalf("got %d", code)
	}
	mailDomainsWrites(t, h)
}

func TestMailDomainsSuspendUnsuspendDelete(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/mail/example.com/suspend"},
		{"POST", "/api/v1/mail/example.com/unsuspend"},
		{"DELETE", "/api/v1/mail/example.com"},
	} {
		var r OK
		if code := h.call(c.method, c.path, nil, &r); code != 200 || r.Status != "ok" {
			t.Fatalf("%s %s: %d", c.method, c.path, code)
		}
	}
	mailDomainsWrites(t, h,
		cmdLine("v-suspend-mail-domain", "admin", "example.com"),
		cmdLine("v-unsuspend-mail-domain", "admin", "example.com"),
		cmdLine("v-delete-mail-domain", "admin", "example.com"),
	)
}

func TestMailDomainsSuspendAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, p := range []string{"suspend", "unsuspend"} {
		var r statusReply
		if code := h.call("POST", "/api/v1/mail/bakery.example/"+p, nil, &r); code != 403 || r.Error.Code != "forbidden" {
			t.Fatalf("%s: %d %+v", p, code, r)
		}
	}
	// Delete stays open to users.
	if code := h.call("DELETE", "/api/v1/mail/bakery.example", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	mailDomainsWrites(t, h, cmdLine("v-delete-mail-domain", "bob", "bakery.example"))
}

func TestMailDomainsBulk(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r BulkReply
	for _, a := range []string{"rebuild", "suspend", "unsuspend", "delete"} {
		if code := h.call("POST", "/api/v1/mail/bulk", BulkRequest{Action: a, Names: []string{"example.com", "example.net"}}, &r); code != 200 || r.Status != "ok" || len(r.Failed) != 0 {
			t.Fatalf("%s: %d %+v", a, code, r)
		}
	}
	var want []string
	for _, c := range []string{"v-rebuild-mail-domain", "v-suspend-mail-domain", "v-unsuspend-mail-domain", "v-delete-mail-domain"} {
		want = append(want, cmdLine(c, "admin", "example.com"), cmdLine(c, "admin", "example.net"))
	}
	mailDomainsWrites(t, h, want...)

	// A failing item is reported; the rest still run.
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-mail-domain": {
			{Args: []string{"admin", "example.com"}, Stdout: "Error: mail domain example.com doesn't exist\n", Exit: 3},
			{Args: []string{"*", "*"}},
		},
	}})
	h.loginAdmin()
	r = BulkReply{}
	if code := h.call("POST", "/api/v1/mail/bulk", BulkRequest{Action: "delete", Names: []string{"example.com", "example.net"}}, &r); code != 200 ||
		len(r.Failed) != 1 || r.Failed[0].Name != "example.com" {
		t.Fatalf("got %d %+v", code, r)
	}
	if w := h.mailWrites(); len(w) != 2 {
		t.Fatalf("writes = %q", w)
	}
}

func TestMailDomainsBulkUser(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, a := range []string{"rebuild", "suspend", "unsuspend"} {
		var r statusReply
		if code := h.call("POST", "/api/v1/mail/bulk", BulkRequest{Action: a, Names: []string{"bakery.example"}}, &r); code != 403 {
			t.Fatalf("%s: %d %+v", a, code, r)
		}
	}
	mailDomainsWrites(t, h)
	if code := h.call("POST", "/api/v1/mail/bulk", BulkRequest{Action: "delete", Names: []string{"bakery.example"}}, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	mailDomainsWrites(t, h, cmdLine("v-delete-mail-domain", "bob", "bakery.example"))
	if code := h.call("POST", "/api/v1/mail/bulk", BulkRequest{Action: "delete", Names: []string{"-h"}}, nil); code != 400 {
		t.Fatalf("bad name: %d", code)
	}
}

func TestMailDomainsWritesReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	for _, rt := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/mail", addAll()},
		{"PUT", "/api/v1/mail/example.com", mailDomainsSame()},
		{"DELETE", "/api/v1/mail/example.com", nil},
		{"POST", "/api/v1/mail/example.com/suspend", nil},
		{"POST", "/api/v1/mail/example.com/unsuspend", nil},
		{"POST", "/api/v1/mail/bulk", BulkRequest{Action: "delete", Names: []string{"example.com"}}},
	} {
		var r statusReply
		if code := h.call(rt.method, rt.path, rt.body, &r); code != 403 || r.Error.Code != "read_only" {
			t.Fatalf("%s %s: %d %+v", rt.method, rt.path, code, r)
		}
	}
	if calls := h.mailCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestMailDomainsRejectQuotedRelayPassword(t *testing.T) {
	const msg = `Invalid Password contains quotes (" or ' or | )`
	for _, pw := range []string{`pa"ss`, `pa'ss`, `pa|ss`, "pa\rss", "pa\nss"} {
		for _, edit := range []bool{false, true} {
			tmp := t.TempDir()
			h := newHarnessWith(t, overrides{}, func(d *Deps) { d.TmpDir = tmp })
			h.loginAdmin()
			rl := &MailSMTPRelay{Host: "smtp.relay.example", User: "u", Password: pw}
			var r APIError
			var code int
			if edit {
				req := mailDomainsSame()
				req.SMTPRelay = rl
				code = h.call("PUT", "/api/v1/mail/example.com", req, &r)
			} else {
				code = h.call("POST", "/api/v1/mail", AddMailDomainRequest{Domain: "example.org", SMTPRelay: rl}, &r)
			}
			body, _ := json.Marshal(r)
			if code != 400 || r.Error.Code != "invalid" || r.Error.Message != msg {
				t.Fatalf("%q edit=%v: %d %s", pw, edit, code, body)
			}
			// The message holds no lower-case "pa", so any echo of the value shows.
			if strings.Contains(string(body), "pa") {
				t.Fatalf("%q edit=%v: reply has the password: %s", pw, edit, body)
			}
			if w := h.mailWrites(); len(w) != 0 {
				t.Fatalf("%q edit=%v: writes = %q", pw, edit, w)
			}
			if left, _ := os.ReadDir(tmp); len(left) != 0 {
				t.Fatalf("%q edit=%v: temp files left: %v", pw, edit, left)
			}
		}
	}
}

func TestMailDomainsNeedCSRF(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.token = ""
	var r statusReply
	if code := h.call("POST", "/api/v1/mail", addAll(), &r); code != 400 || r.Error.Code != "csrf" {
		t.Fatalf("got %d %+v", code, r)
	}
	if calls := h.mailCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

// The webmail step needs both an IMAP and a webmail system, as on add.
func TestMailDomainsEditWebmailNeedsImapAndWebmail(t *testing.T) {
	for _, off := range []string{"IMAP_SYSTEM", "WEBMAIL_SYSTEM"} {
		h := newHarnessWith(t, overrides{Config: map[string]string{off: ""}})
		h.loginAdmin()
		req := mailDomainsSame()
		req.Webmail = mailDomainsStr("snappymail")
		if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
			t.Fatalf("%s off: %d %+v", off, code, r)
		}
		mailDomainsWrites(t, h)
	}
	h := newHarness(t)
	h.loginAdmin()
	req := mailDomainsSame()
	req.Webmail = mailDomainsStr("snappymail")
	if code, r := mailDomainsEdit(t, h, "example.com", req); code != 200 {
		t.Fatalf("%d %+v", code, r)
	}
	mailDomainsWrites(t, h, cmdLine("v-add-mail-domain-webmail", "admin", "example.com", "snappymail", "yes"))
}
