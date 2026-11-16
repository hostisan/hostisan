package httpapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const goodMailPW = "Passw0rdOK"

var pwFileRE = regexp.MustCompile(`\S*/hostisan-\d+`)

// maWrites is the state-changing mail commands with any password file path replaced by <pw>.
func maWrites(h *harness) []string {
	out := []string{}
	for _, c := range h.mailWrites() {
		out = append(out, pwFileRE.ReplaceAllString(c, "<pw>"))
	}
	return out
}

func maHarness(t *testing.T, o overrides) (*harness, *fakeMailer, *fakeTemplates, string) {
	t.Helper()
	mm, tp, tmp := &fakeMailer{}, &fakeTemplates{}, t.TempDir()
	return newHarnessWith(t, o, withMail(mm, tp, tmp)), mm, tp, tmp
}

func maEq(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls\n got  %q\n want %q", got, want)
	}
}

const maBase = "/api/v1/mail/example.com/accounts"

// maAccount overrides v-list-mail-account for admin/example.com/<name> with the given fields.
func maAccount(name string, fields map[string]string) overrides {
	base := map[string]string{"ALIAS": "", "FWD": "", "FWD_ONLY": "", "AUTOREPLY": "no", "QUOTA": "unlimited",
		"RATE_LIMIT": "", "U_DISK": "0", "SUSPENDED": "no", "TIME": "10:00:00", "DATE": "2026-01-01",
		"WEBMAIL_ALIAS": "webmail.example.com"}
	for k, v := range fields {
		base[k] = v
	}
	b, err := json.Marshal(map[string]any{name: base})
	if err != nil {
		panic(err)
	}
	return overrides{Fixtures: map[string][]fixtureEntry{"v-list-mail-account": {{
		Args: []string{"admin", "example.com", name, "json"}, Stdout: string(b)}}}}
}

func ptr(s string) *string { return &s }

func failMail(cmd string, exit int, out string, argc int) overrides {
	return failDB(cmd, exit, out, argc)
}

func TestMailAccountsList(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	var got MailAccounts
	if code := h.call("GET", maBase, nil, &got); code != 200 {
		t.Fatalf("%d", code)
	}
	if got.Domain.NAME != "example.com" || len(got.Accounts) != 2 || got.Accounts[0].NAME != "info" || got.Accounts[1].NAME != "sales" {
		t.Fatalf("%+v", got)
	}
	if code := h.call("GET", "/api/v1/mail/missing.example/accounts", nil, nil); code != 404 {
		t.Fatalf("missing domain: %d", code)
	}
}

func TestMailAccountView(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	var v MailAccountView
	if code := h.call("GET", maBase+"/sales", nil, &v); code != 200 {
		t.Fatalf("%d", code)
	}
	if v.Account.NAME != "sales" || v.AutoreplyMessage != "Thanks for your message.\nWe reply within one business day.\nSales team" {
		t.Fatalf("%+v", v)
	}
	v = MailAccountView{}
	if code := h.call("GET", maBase+"/info", nil, &v); code != 200 || v.AutoreplyMessage != "" {
		t.Fatalf("info: %d %+v", code, v)
	}
	for _, c := range h.mailCalls() {
		if strings.HasPrefix(c, "v-list-mail-account-autoreply admin example.com info") {
			t.Fatalf("autoreply listed although it is off: %q", c)
		}
	}
}

func TestMailAccountHostile(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	for _, a := range []string{"-h", "a;b", "a%20b", strings.Repeat("a", 65)} {
		for _, m := range []string{"GET", "PUT", "DELETE"} {
			if code := h.call(m, maBase+"/"+a, EditMailAccountRequest{}, nil); code != 400 {
				t.Errorf("%s %q: %d", m, a, code)
			}
		}
		if code := h.call("POST", maBase+"/"+a+"/suspend", nil, nil); code != 400 {
			t.Errorf("suspend %q: %d", a, code)
		}
	}
	if code := h.call("GET", "/api/v1/mail/-evil.com/accounts", nil, nil); code != 400 {
		t.Errorf("domain: %d", code)
	}
	if calls := h.mailCalls(); len(calls) != 0 {
		t.Fatalf("calls ran: %v", calls)
	}
}

func TestAddMailAccountHostileName(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	for _, a := range []string{"-h", "a b", "a;b", strings.Repeat("a", 65)} {
		req := AddMailAccountRequest{Account: a, Password: goodMailPW}
		var e APIError
		if code := h.call("POST", maBase, req, &e); code != 400 || e.Error.Message != "Error: invalid account format :: "+a {
			t.Errorf("%q: %d %q", a, code, e.Error.Message)
		}
	}
	if calls := h.mailCalls(); len(calls) != 0 {
		t.Fatalf("calls ran: %v", calls)
	}
}

func TestAddMailAccountValidationOrder(t *testing.T) {
	h, mm, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", maBase, AddMailAccountRequest{}, &e); code != 400 || e.Error.Message != `Field "%s" can not be blank.` ||
		strings.Join(e.Error.Args, ",") != "Account,Password" {
		t.Fatalf("blank: %d %+v", code, e)
	}
	e = APIError{}
	if code := h.call("POST", maBase, AddMailAccountRequest{ForwardOnly: true}, &e); code != 400 ||
		strings.Join(e.Error.Args, ",") != "Account" {
		t.Fatalf("blank fwd-only: %d %+v", code, e)
	}
	req := AddMailAccountRequest{Account: "new", Password: "weak", SendTo: "nope"}
	e = APIError{}
	if code := h.call("POST", maBase, req, &e); code != 400 || e.Error.Message != "Please enter a valid email address." {
		t.Fatalf("email: %d %+v", code, e)
	}
	req.SendTo = "me@example.org"
	e = APIError{}
	if code := h.call("POST", maBase, req, &e); code != 400 || e.Error.Message != "Password does not match the minimum requirements." {
		t.Fatalf("password: %d %+v", code, e)
	}
	if calls := h.mailCalls(); len(calls) != 0 || len(mm.sent) != 0 {
		t.Fatalf("calls %v mails %v", calls, mm.sent)
	}
}

func TestAddMailAccountPasswordFile(t *testing.T) {
	tmp := t.TempDir()
	var path, content string
	var mode os.FileMode
	h := newHarnessWith(t, overrides{}, withMail(&fakeMailer{}, &fakeTemplates{}, tmp), func(d *Deps) {
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd != "v-add-mail-account" {
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
	if code := h.call("POST", maBase, AddMailAccountRequest{Account: "new", Password: goodMailPW, Quota: "500"}, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if mode != 0o600 || content != goodMailPW+"\n" || filepath.Dir(path) != tmp {
		t.Fatalf("mode %v content %q path %q", mode, content, path)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	for _, c := range h.mailCalls() {
		if strings.Contains(c, goodMailPW) {
			t.Fatalf("password in log: %q", c)
		}
	}
	maEq(t, maWrites(h), []string{"v-add-mail-account admin example.com new <pw> 500"})
}

func TestAddMailAccountQuota(t *testing.T) {
	for quota, want := range map[string]string{"": "0", "0": "0", "unlimited": "unlimited", " 250 ": "250"} {
		h, _, _, _ := maHarness(t, overrides{})
		h.loginAdmin()
		if code := h.call("POST", maBase, AddMailAccountRequest{Account: "q", Password: goodMailPW, Quota: quota}, nil); code != 200 {
			t.Fatalf("%q: %d", quota, code)
		}
		maEq(t, maWrites(h), []string{"v-add-mail-account admin example.com q <pw> " + want})
	}
}

func TestAddMailAccountAliasForwardSplit(t *testing.T) {
	h, mm, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	req := AddMailAccountRequest{Account: "new", Password: goodMailPW, Aliases: "a, b\nc  d", Forwards: "x@e.org,\ny@e.org", ForwardOnly: true,
		Rate: ptr("100"), SendTo: "me@example.org"}
	if code := h.call("POST", maBase, req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-add-mail-account admin example.com new <pw> 0",
		"v-add-mail-account-alias admin example.com new a",
		"v-add-mail-account-alias admin example.com new b",
		"v-add-mail-account-alias admin example.com new c",
		"v-add-mail-account-alias admin example.com new d",
		"v-add-mail-account-forward admin example.com new x@e.org",
		"v-add-mail-account-forward admin example.com new y@e.org",
		"v-add-mail-account-fwd-only admin example.com new",
		"v-change-mail-account-rate-limit admin example.com new 100",
	})
	if len(mm.sent) != 1 || mm.sent[0].To != "me@example.org" {
		t.Fatalf("sent %+v", mm.sent)
	}
}

func TestAddMailAccountDiscard(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	req := AddMailAccountRequest{Account: "new", Password: goodMailPW, Discard: true, Forwards: "x@e.org", ForwardOnly: true}
	if code := h.call("POST", maBase, req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-add-mail-account admin example.com new <pw> 0",
		"v-add-mail-account-forward admin example.com new :blackhole:",
		"v-add-mail-account-fwd-only admin example.com new",
	})
}

func TestAddMailAccountForwardOnlyNoPassword(t *testing.T) {
	h, _, _, tmp := maHarness(t, overrides{})
	h.loginAdmin()
	req := AddMailAccountRequest{Account: "fwd", ForwardOnly: true, Forwards: "x@e.org"}
	if code := h.call("POST", maBase, req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-add-mail-account admin example.com fwd <pw> 0",
		"v-add-mail-account-forward admin example.com fwd x@e.org",
		"v-add-mail-account-fwd-only admin example.com fwd",
	})
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestAddMailAccountStopsAtFirstError(t *testing.T) {
	h, mm, _, _ := maHarness(t, failMail("v-add-mail-account-alias", 4, "Error: alias b exists\n", 4))
	h.loginAdmin()
	req := AddMailAccountRequest{Account: "new", Password: goodMailPW, Aliases: "a b c", Forwards: "x@e.org", ForwardOnly: true,
		Rate: ptr("5"), SendTo: "me@example.org"}
	var e APIError
	if code := h.call("POST", maBase, req, &e); code != 409 || e.Error.Message != "Error: alias b exists" {
		t.Fatalf("%d %+v", code, e)
	}
	maEq(t, maWrites(h), []string{
		"v-add-mail-account admin example.com new <pw> 0",
		"v-add-mail-account-alias admin example.com new a",
	})
	if len(mm.sent) != 0 {
		t.Fatalf("sent %v", mm.sent)
	}
}

func TestAddMailAccountRateIgnoredForUser(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginBob()
	var e APIError
	if code := h.call("POST", "/api/v1/mail/bakery.example/accounts", AddMailAccountRequest{Account: "n", Password: goodMailPW, Rate: ptr("50")}, &e); code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	maEq(t, maWrites(h), []string{"v-add-mail-account bob bakery.example n <pw> 0"})
}

func TestAddMailAccountEmail(t *testing.T) {
	mm, tp, tmp := &fakeMailer{err: errors.New("smtp down")}, &fakeTemplates{ok: true,
		subject: "<b>{{account}}@{{domain}} on {{hostname}} by {{appname}}",
		body:    "U {{account}}@{{domain}} P {{password}} W {{webmail}} H {{hostname}} A {{appname}}"}, t.TempDir()
	h := newHarnessWith(t, overrides{}, withMail(mm, tp, tmp))
	h.loginAdmin()
	req := AddMailAccountRequest{Account: "New", Password: "Pa<ss>w0rd", SendTo: "me@example.org"}
	if code := h.call("POST", maBase, req, nil); code != 200 {
		t.Fatalf("email failure must not fail the add: %d", code)
	}
	if len(mm.sent) != 1 {
		t.Fatalf("sent %v", mm.sent)
	}
	m := mm.sent[0]
	if m.To != "me@example.org" || m.Subject != "<b>new@example.com on server1.acme-hosting.net by Hostisan" ||
		m.Body != "U new@example.com P Pa&lt;ss&gt;w0rd W https://webmail.example.com/ H mail.example.com A Hostisan" {
		t.Fatalf("%+v", m)
	}
	if len(tp.asked) != 1 || tp.asked[0][1] != "email_credentials" {
		t.Fatalf("asked %v", tp.asked)
	}
}

func TestAddMailAccountEmailFallback(t *testing.T) {
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	if code := h.call("POST", maBase, AddMailAccountRequest{Account: "New", Password: goodMailPW, SendTo: "me@example.org"}, nil); code != 200 {
		t.Fatal(code)
	}
	m := mm.sent[0]
	if !strings.Contains(m.Subject, "Email Credentials: new@example.com") ||
		!strings.Contains(m.Body, "Username: new@example.com") || !strings.Contains(m.Body, "Password: "+goodMailPW) ||
		!strings.Contains(m.Body, "Webmail: https://webmail.example.com/") || strings.Contains(m.Body, "{{") {
		t.Fatalf("%+v", m)
	}
}

func TestEditMailAccountDiscardTwoForwards(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	// sales has two forwards, fwd-only on and autoreply on with a stored message.
	req := EditMailAccountRequest{Quota: "unlimited", Discard: true, ForwardOnly: true, Autoreply: true,
		AutoreplyMessage: "Thanks for your message.\r\nWe reply within one business day.\r\nSales team", Rate: ptr("200")}
	if code := h.call("PUT", maBase+"/sales", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-delete-mail-account-forward admin example.com sales owner@gmail.com",
		"v-delete-mail-account-forward admin example.com sales archive@example.org",
		"v-add-mail-account-forward admin example.com sales :blackhole:",
	})
}

func TestEditMailAccountDiscardOff(t *testing.T) {
	h, _, _, _ := maHarness(t, maAccount("info", map[string]string{"FWD": ":blackhole:", "FWD_ONLY": "yes"}))
	h.loginAdmin()
	req := EditMailAccountRequest{Quota: "unlimited", Forwards: "a@e.org\nb@e.org", ForwardOnly: false}
	if code := h.call("PUT", maBase+"/info", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-delete-mail-account-forward admin example.com info :blackhole:",
		"v-add-mail-account-forward admin example.com info a@e.org",
		"v-add-mail-account-forward admin example.com info b@e.org",
		"v-delete-mail-account-fwd-only admin example.com info",
	})
}

func TestEditMailAccountOrder(t *testing.T) {
	h, mm, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	// info: quota 1024, aliases contact,hello, no forwards, autoreply off, rate "" (system).
	req := EditMailAccountRequest{Password: goodMailPW, Quota: "2048", Aliases: "hello, new1", Forwards: "z@e.org", ForwardOnly: true,
		Autoreply: true, AutoreplyMessage: "Away\r\nback soon", Rate: ptr(""), SendTo: "me@example.org"}
	if code := h.call("PUT", maBase+"/info", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-change-mail-account-password admin example.com info <pw>",
		"v-change-mail-account-quota admin example.com info 2048",
		"v-delete-mail-account-alias admin example.com info contact",
		"v-add-mail-account-alias admin example.com info new1",
		"v-add-mail-account-forward admin example.com info z@e.org",
		"v-add-mail-account-fwd-only admin example.com info",
		"v-add-mail-account-autoreply admin example.com info Away\nback soon",
	})
	if len(mm.sent) != 1 || !strings.Contains(mm.sent[0].Body, goodMailPW) {
		t.Fatalf("sent %+v", mm.sent)
	}
}

const salesMsg = "Thanks for your message.\nWe reply within one business day.\nSales team"

func TestEditMailAccountRate(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	// sales has RATE_LIMIT 200; "" resets to system.
	if code := h.call("PUT", maBase+"/sales", EditMailAccountRequest{Quota: "unlimited", Forwards: "owner@gmail.com, archive@example.org",
		ForwardOnly: true, Autoreply: true, AutoreplyMessage: salesMsg, Rate: ptr("")}, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{"v-change-mail-account-rate-limit admin example.com sales system"})
	// Not offered: no change.
	h, _, _, _ = maHarness(t, overrides{})
	h.loginAdmin()
	h.call("PUT", maBase+"/sales", EditMailAccountRequest{Quota: "unlimited", Forwards: "owner@gmail.com archive@example.org",
		ForwardOnly: true, Autoreply: true, AutoreplyMessage: salesMsg}, nil)
	maEq(t, maWrites(h), []string{})
}

func TestEditMailAccountRateIgnoredForUser(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginBob()
	if code := h.call("PUT", "/api/v1/mail/bakery.example/accounts/orders", EditMailAccountRequest{Quota: "2048", Aliases: "shop", Rate: ptr("9")}, nil); code != 200 {
		t.Fatal(code)
	}
	maEq(t, maWrites(h), []string{})
}

func TestEditMailAccountAutoreply(t *testing.T) {
	// Empty message with autoreply on: blank-field error, no command at all.
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	if code := h.call("PUT", maBase+"/info", EditMailAccountRequest{Quota: "1024", Aliases: "contact,hello", Autoreply: true}, &e); code != 400 ||
		e.Error.Message != `Field "%s" can not be blank.` || strings.Join(e.Error.Args, ",") != "Message" {
		t.Fatalf("%d %+v", code, e)
	}
	if calls := h.mailWrites(); len(calls) != 0 {
		t.Fatalf("%v", calls)
	}
	// Too long.
	e = APIError{}
	if code := h.call("PUT", maBase+"/info", EditMailAccountRequest{Quota: "1024", Autoreply: true, AutoreplyMessage: strings.Repeat("x", 10240)}, &e); code != 400 || e.Error.Message != "Autoreply message is too long." || len(e.Error.Args) != 0 {
		t.Fatalf("long: %d %+v", code, e.Error)
	}
	if calls := h.mailWrites(); len(calls) != 0 {
		t.Fatalf("%v", calls)
	}
	// Unchanged message: no command (CRLF from a browser textarea counts as unchanged).
	h, _, _, _ = maHarness(t, overrides{})
	h.loginAdmin()
	req := EditMailAccountRequest{Quota: "unlimited", Forwards: "owner@gmail.com archive@example.org", ForwardOnly: true, Rate: ptr("200"),
		Autoreply: true, AutoreplyMessage: "Thanks for your message.\r\nWe reply within one business day.\r\nSales team"}
	if code := h.call("PUT", maBase+"/sales", req, nil); code != 200 {
		t.Fatal(code)
	}
	maEq(t, maWrites(h), []string{})
	// Turned off.
	req.Autoreply, req.AutoreplyMessage = false, ""
	h, _, _, _ = maHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("PUT", maBase+"/sales", req, nil); code != 200 {
		t.Fatal(code)
	}
	maEq(t, maWrites(h), []string{"v-delete-mail-account-autoreply admin example.com sales"})
	// Changed message.
	req.Autoreply, req.AutoreplyMessage = true, "New text"
	h, _, _, _ = maHarness(t, overrides{})
	h.loginAdmin()
	h.call("PUT", maBase+"/sales", req, nil)
	maEq(t, maWrites(h), []string{"v-add-mail-account-autoreply admin example.com sales New text"})
}

func TestEditMailAccountQuota(t *testing.T) {
	for _, c := range []struct {
		cur, req string
		want     []string
	}{
		{"1024", "", []string{"v-change-mail-account-quota admin example.com info 0"}},
		{"1024", "unlimited", []string{"v-change-mail-account-quota admin example.com info unlimited"}},
		{"1024", "0", []string{"v-change-mail-account-quota admin example.com info 0"}},
		{"1024", "1024", []string{}},
		{"unlimited", "", []string{}},
		{"unlimited", "unlimited", []string{}},
		{"unlimited", "0", []string{}},
		{"unlimited", "300", []string{"v-change-mail-account-quota admin example.com info 300"}},
	} {
		h, _, _, _ := maHarness(t, maAccount("info", map[string]string{"QUOTA": c.cur}))
		h.loginAdmin()
		if code := h.call("PUT", maBase+"/info", EditMailAccountRequest{Quota: c.req}, nil); code != 200 {
			t.Fatalf("%+v: %d", c, code)
		}
		maEq(t, maWrites(h), c.want)
	}
}

func TestEditMailAccountValidation(t *testing.T) {
	h, mm, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	var e APIError
	if code := h.call("PUT", maBase+"/info", EditMailAccountRequest{SendTo: "bad"}, &e); code != 400 || e.Error.Message != "Please enter a valid email address." {
		t.Fatalf("%d %+v", code, e)
	}
	e = APIError{}
	if code := h.call("PUT", maBase+"/info", EditMailAccountRequest{Password: "weak"}, &e); code != 400 || e.Error.Message != "Password does not match the minimum requirements." {
		t.Fatalf("%d %+v", code, e)
	}
	if code := h.call("PUT", maBase+"/missing", EditMailAccountRequest{}, nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if w := h.mailWrites(); len(w) != 0 || len(mm.sent) != 0 {
		t.Fatalf("%v %v", w, mm.sent)
	}
}

func TestEditMailAccountStopsAtFirstError(t *testing.T) {
	h, mm, _, _ := maHarness(t, failMail("v-change-mail-account-quota", 2, "Error: bad quota\n", 4))
	h.loginAdmin()
	if code := h.call("PUT", maBase+"/info", EditMailAccountRequest{Password: goodMailPW, Quota: "5", Aliases: "x", SendTo: "me@example.org"}, nil); code != 400 {
		t.Fatalf("%d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-change-mail-account-password admin example.com info <pw>",
		"v-change-mail-account-quota admin example.com info 5",
	})
	if len(mm.sent) != 0 {
		t.Fatal("mail sent")
	}
}

func TestMailAccountDeleteSuspend(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginAdmin()
	for _, a := range []string{"", "/suspend", "/unsuspend"} {
		m := "POST"
		if a == "" {
			m = "DELETE"
		}
		if code := h.call(m, maBase+"/info"+a, nil, nil); code != 200 {
			t.Fatalf("%s %s: %d", m, a, code)
		}
	}
	maEq(t, maWrites(h), []string{
		"v-delete-mail-account admin example.com info",
		"v-suspend-mail-account admin example.com info",
		"v-unsuspend-mail-account admin example.com info",
	})
}

func TestMailAccountUserRules(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{})
	h.loginBob()
	base := "/api/v1/mail/bakery.example/accounts/orders"
	for _, a := range []string{"/suspend", "/unsuspend"} {
		if code := h.call("POST", base+a, nil, nil); code != 403 {
			t.Fatalf("%s: %d", a, code)
		}
	}
	if code := h.call("DELETE", base, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	maEq(t, maWrites(h), []string{"v-delete-mail-account bob bakery.example orders"})
}

func TestMailAccountReadOnly(t *testing.T) {
	h, _, _, _ := maHarness(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil); code != 200 {
		t.Fatalf("login-as admin: %d", code)
	}
	for m, p := range map[string]string{"POST": maBase, "PUT": maBase + "/info", "DELETE": maBase + "/info"} {
		if code := h.call(m, p, AddMailAccountRequest{Account: "x", Password: goodMailPW}, nil); code != 403 {
			t.Errorf("%s: %d", m, code)
		}
	}
	if w := h.mailWrites(); len(w) != 0 {
		t.Fatalf("%v", w)
	}
}

func TestMailAccountBulk(t *testing.T) {
	h, _, _, _ := maHarness(t, failMail("v-suspend-mail-account", 5, "Error: suspended\n", 3))
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", maBase+"/bulk", BulkRequest{Action: "suspend", Names: []string{"info", "sales"}}, &r); code != 200 || len(r.Failed) != 2 {
		t.Fatalf("%d %+v", code, r)
	}
	r = BulkReply{}
	if code := h.call("POST", maBase+"/bulk", BulkRequest{Action: "delete", Names: []string{"info"}}, &r); code != 200 || len(r.Failed) != 0 {
		t.Fatalf("%d %+v", code, r)
	}
	if code := h.call("POST", maBase+"/bulk", BulkRequest{Action: "delete", Names: []string{"-x"}}, nil); code != 400 {
		t.Fatalf("hostile: %d", code)
	}
	if code := h.call("POST", maBase+"/bulk", BulkRequest{Action: "rebuild", Names: []string{"info"}}, nil); code != 400 {
		t.Fatalf("rebuild: %d", code)
	}
	maEq(t, maWrites(h), []string{
		"v-suspend-mail-account admin example.com info",
		"v-suspend-mail-account admin example.com sales",
		"v-delete-mail-account admin example.com info",
	})
	hb, _, _, _ := maHarness(t, overrides{})
	hb.loginBob()
	if code := hb.call("POST", "/api/v1/mail/bakery.example/accounts/bulk", BulkRequest{Action: "suspend", Names: []string{"orders"}}, nil); code != 403 {
		t.Fatalf("user suspend: %d", code)
	}
	if code := hb.call("POST", "/api/v1/mail/bakery.example/accounts/bulk", BulkRequest{Action: "delete", Names: []string{"orders"}}, nil); code != 200 {
		t.Fatalf("user delete: %d", code)
	}
	maEq(t, maWrites(hb), []string{"v-delete-mail-account bob bakery.example orders"})
}

func TestAddMailAccountSubjectWrapperHostname(t *testing.T) {
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{Config: map[string]string{"SUBJECT_EMAIL": "[{{hostname}}] {{subject}}"}},
		withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	if code := h.call("POST", maBase, AddMailAccountRequest{Account: "New", Password: goodMailPW, SendTo: "me@example.org"}, nil); code != 200 {
		t.Fatal(code)
	}
	if got := mm.sent[0].Subject; got != "[server1.acme-hosting.net] Email Credentials: new@example.com" {
		t.Fatalf("subject %q", got)
	}
	if !strings.Contains(mm.sent[0].Body, "Hostname: mail.example.com") {
		t.Fatalf("body %q", mm.sent[0].Body)
	}
}
