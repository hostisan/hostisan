package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/session"
	"github.com/hostisan/hostisan/internal/totp"
)

// profWrites is the commands that change state, with password files shown as <pw>.
func profWrites(h *harness) []string {
	out := []string{}
	for _, c := range h.fixture.Calls() {
		if strings.HasPrefix(c.Cmd, "v-change-") || strings.HasPrefix(c.Cmd, "v-add-") ||
			strings.HasPrefix(c.Cmd, "v-delete-") || c.Cmd == "v-log-action" {
			out = append(out, pwFileRE.ReplaceAllString(c.Cmd+" "+strings.Join(c.Args, " "), "<pw>"))
		}
	}
	return out
}

func profLoginLogs(h *harness) int {
	n := 0
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-log-user-login" && len(c.Args) > 2 && c.Args[2] == "failed" {
			n++
		}
	}
	return n
}

func profEq(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("writes\n got  %q\n want %q", got, want)
	}
}

// profFlip fails every password check once on is set, after the login worked.
type profFlip struct {
	runner.Runner
	on *atomic.Bool
}

func (f profFlip) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == "v-check-user-hash" && f.on.Load() {
		return runner.Result{Stdout: []byte("Error: wrong\n"), Exit: 9}, nil
	}
	return f.Runner.Run(ctx, cmd, args...)
}

// profBadPassword is an option that returns the switch to flip after login.
func profBadPassword(on *atomic.Bool) func(*Deps) {
	return func(d *Deps) { d.Auth.R = profFlip{d.Auth.R, on} }
}

// profUsers puts an extra v-list-user entry ahead of the dev ones.
func profUsers(t *testing.T, name string, fields map[string]string) overrides {
	t.Helper()
	base := map[string]string{"NAME": "Old Name", "CONTACT": "old@example.com", "ROLE": "user", "PACKAGE": "default",
		"SHELL": "nologin", "LANGUAGE": "en", "THEME": "", "PREF_UI_SORT": "name", "LOGIN_DISABLED": "no",
		"LOGIN_USE_IPLIST": "no", "LOGIN_ALLOW_IPS": "", "NS": "", "PHPCLI": "", "TWOFA": "", "SUSPENDED": "no",
		"WEB_DOMAINS": "1", "DNS_DOMAINS": "1", "MAIL_DOMAINS": "1", "DATABASES": "1", "CRON_JOBS": "1", "BACKUPS": "1"}
	for k, v := range fields {
		base[k] = v
	}
	b, _ := json.Marshal(map[string]any{name: base})
	dir := t.TempDir()
	e, _ := json.Marshal([]fixtureEntry{{Args: []string{name, "json"}, Stdout: string(b)}})
	if err := os.WriteFile(filepath.Join(dir, "v-list-user.json"), e, 0o600); err != nil {
		t.Fatal(err)
	}
	return overrides{Dir: dir}
}

func profSame() UserEditRequest {
	return UserEditRequest{AccountRequest: AccountRequest{Name: "System Administrator", Email: "admin@acme-hosting.net",
		Language: "en", SortOrder: "name", AllowedIPs: []string{}}}
}

func TestProfileNoChangeRunsNothing(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var got AccountSaved
	if code := h.call("PUT", "/api/v1/account", profSame(), &got); code != 200 || got.Status != "ok" || got.LanguageChanged {
		t.Fatalf("%d %+v", code, got)
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileFullOrder(t *testing.T) {
	h := newHarnessWith(t, profUsers(t, "carl", map[string]string{"PACKAGE": "system", "ROLE": "admin"}))
	h.loginAdmin()
	h.store.Save(&session.Session{ID: "carl-session", Token: "t", User: "carl", UserContext: "user"})
	yes, pkg, role, shell, php := true, "default", "user", "bash", "8.3"
	req := UserEditRequest{
		AccountRequest: AccountRequest{Name: "New Name", Email: "new@example.com", Password: "Passw0rdOK", Language: "de",
			Theme: "dark", SortOrder: "date", UseIPList: true, AllowedIPs: []string{"10.0.0.1", "192.168.0.5"}},
		LoginDisabled: &yes, Package: &pkg, Role: &role, Shell: &shell, PHPCli: &php,
		Nameservers: []string{"ns1.example.com.", " ns2.example.com ", "", "ns3.example.com"},
	}
	var got AccountSaved
	if code := h.call("PUT", "/api/v1/users/carl", req, &got); code != 200 || !got.LanguageChanged {
		t.Fatalf("%d %+v", code, got)
	}
	profEq(t, profWrites(h), []string{
		"v-change-user-password carl <pw>",
		"v-change-user-sort-order carl date",
		"v-change-user-config-value carl LOGIN_DISABLED yes",
		"v-change-user-config-value carl LOGIN_ALLOW_IPS 10.0.0.1,192.168.0.5",
		"v-change-user-config-value carl LOGIN_USE_IPLIST yes",
		"v-change-user-package carl default",
		"v-change-user-php-cli carl 8.3",
		"v-change-user-role carl user",
		"v-change-user-shell carl bash",
		"v-change-user-language carl de",
		"v-change-user-contact carl new@example.com",
		"v-change-user-name carl New Name",
		"v-change-user-theme carl dark",
		"v-change-user-ns carl ns1.example.com ns2.example.com ns3.example.com",
	})
	// An admin editing another user needs no current password; that user's sessions end.
	if _, ok := h.store.Get("carl-session"); ok {
		t.Fatal("carl's session survived the password change")
	}
}

func TestProfileSortOrderStoredBare(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := profSame()
	r.SortOrder = "date"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 200 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{"v-change-user-sort-order admin date"})
}

func TestProfileValidationRunsNothing(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	cases := map[string]func(*UserEditRequest){
		"email":    func(r *UserEditRequest) { r.Email = "nope" },
		"name":     func(r *UserEditRequest) { r.Name = " " },
		"password": func(r *UserEditRequest) { r.Password = "weak" },
		"sort":     func(r *UserEditRequest) { r.SortOrder = "size" },
		"language": func(r *UserEditRequest) { r.Language = "zz" },
		"ip":       func(r *UserEditRequest) { r.AllowedIPs = []string{"x;y"} },
		// The core's check refuses ":" and "/", and login compares exact addresses.
		"ipv6": func(r *UserEditRequest) { r.UseIPList, r.AllowedIPs = true, []string{"::1"} },
		"cidr": func(r *UserEditRequest) { r.UseIPList, r.AllowedIPs = true, []string{"10.0.0.0/8"} },
	}
	for name, mod := range cases {
		r := profSame()
		mod(&r)
		if code := h.call("PUT", "/api/v1/account", r, nil); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileLanguageDoesNotStopLaterSteps(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := profSame()
	r.Language, r.Name = "de", "Someone Else"
	var got AccountSaved
	if code := h.call("PUT", "/api/v1/account", r, &got); code != 200 || !got.LanguageChanged {
		t.Fatalf("%d %+v", code, got)
	}
	profEq(t, profWrites(h), []string{"v-change-user-language admin de", "v-change-user-name admin Someone Else"})
	if b := h.bootstrap(); b.Language != "de" {
		t.Fatalf("session language = %q", b.Language)
	}
}

func TestProfileStopsAtFirstError(t *testing.T) {
	o := overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-user-config-value": {{Args: []string{"*", "*", "*"}, Stdout: "Error: boom\n", Exit: 2}},
	}}
	h := newHarnessWith(t, o)
	h.loginAdmin()
	r := profSame()
	r.UseIPList, r.AllowedIPs, r.Name = true, []string{"10.0.0.1"}, "Other"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 400 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{"v-change-user-config-value admin LOGIN_ALLOW_IPS 10.0.0.1"})
}

func TestProfileNonAdminForbidden(t *testing.T) {
	yes := true
	for name, mod := range map[string]func(*UserEditRequest){
		"loginDisabled": func(r *UserEditRequest) { r.LoginDisabled = &yes },
		"package":       func(r *UserEditRequest) { p := "x1"; r.Package = &p },
		"nameservers":   func(r *UserEditRequest) { r.Nameservers = []string{"a.example.com", "b.example.com"} },
	} {
		h := newHarness(t)
		h.loginBob()
		r := UserEditRequest{AccountRequest: AccountRequest{Name: "Bob Baker", Email: "bob@acme-bakery.com", Language: "en", SortOrder: "name"}}
		mod(&r)
		if code := h.call("PUT", "/api/v1/account", r, nil); code != 403 {
			t.Errorf("%s: %d", name, code)
		}
		profEq(t, profWrites(h), []string{})
	}
}

func TestProfileDetailsPolicy(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_DETAILS": "no"}})
	h.loginBob()
	r := UserEditRequest{AccountRequest: AccountRequest{Name: "Bob Baker", Email: "other@acme-bakery.com", Language: "en", SortOrder: "name"}}
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 403 {
		t.Fatal(code)
	}
	r.Email, r.Name = "bob@acme-bakery.com", "Robert"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 403 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{})
	var v AccountView
	if code := h.call("GET", "/api/v1/account", nil, &v); code != 200 || v.Editable.Details || !v.Editable.Theme || v.Editable.Admin || !v.Self || !v.TwoFA {
		t.Fatalf("%d %+v", code, v)
	}
	// The package limits come from the user's own record (non-admins can't read packages).
	if u := v.User; u.WEB_DOMAINS != "3" || u.DNS_DOMAINS != "5" || u.DATABASES != "1" || u.BACKUPS != "1" {
		t.Fatalf("limits: %+v", u)
	}
}

func TestProfileThemePolicy(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_CHANGE_THEME": "no"}})
	h.loginBob()
	r := UserEditRequest{AccountRequest: AccountRequest{Name: "Bob Baker", Email: "bob@acme-bakery.com", Language: "en", SortOrder: "name", Theme: "dark"}}
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 403 {
		t.Fatal(code)
	}
	var o AccountOptions
	if code := h.call("GET", "/api/v1/account/options", nil, &o); code != 200 || len(o.Themes) != 0 {
		t.Fatalf("%d %+v", code, o)
	}
}

func TestProfileOwnPasswordNeedsCurrent(t *testing.T) {
	var bad atomic.Bool
	h := newHarnessWith(t, overrides{}, profBadPassword(&bad))
	h.loginAdmin()
	bad.Store(true)
	r := profSame()
	r.Password = "Passw0rdOK"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 400 {
		t.Fatalf("missing: %d", code)
	}
	r.CurrentPassword = "wrong"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 400 {
		t.Fatalf("wrong: %d", code)
	}
	profEq(t, profWrites(h), []string{})
	if n := profLoginLogs(h); n != 1 {
		t.Fatalf("failed logins logged = %d", n)
	}
}

func TestProfileOwnPasswordGood(t *testing.T) {
	tmp := t.TempDir()
	h := newHarnessWith(t, overrides{}, func(d *Deps) { d.TmpDir = tmp })
	h.loginAdmin()
	// A second session of the same user, which must be signed out.
	h.store.Save(&session.Session{ID: "other-session", Token: "t", User: "admin", UserContext: "admin"})
	r := profSame()
	r.Password, r.CurrentPassword = "Passw0rdOK", "pw"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 200 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{"v-change-user-password admin <pw>"})
	if _, ok := h.store.Get("other-session"); ok {
		t.Fatal("other session survived")
	}
	if _, ok := h.store.Get(h.sessionID()); !ok {
		t.Fatal("own session was removed")
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	for _, c := range h.fixture.Calls() {
		for _, a := range c.Args {
			if strings.Contains(a, "Passw0rdOK") {
				t.Fatalf("password in argv of %s", c.Cmd)
			}
		}
	}
}

func TestProfileRoleGuards(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	role := "user"
	for _, path := range []string{"/api/v1/users/admin", "/api/v1/account"} {
		r := profSame()
		r.Role = &role
		if code := h.call("PUT", path, r, nil); code != 403 {
			t.Errorf("%s: %d", path, code)
		}
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileRootUserGuardForOps(t *testing.T) {
	h := newHarness(t)
	h.loginOps()
	if code := h.call("GET", "/api/v1/users/admin", nil, nil); code != 403 {
		t.Fatalf("GET %d", code)
	}
	if code := h.call("PUT", "/api/v1/users/admin", profSame(), nil); code != 403 {
		t.Fatalf("PUT %d", code)
	}
	if code := h.call("DELETE", "/api/v1/users/admin/2fa", nil, nil); code != 403 {
		t.Fatalf("DELETE %d", code)
	}
	profEq(t, profWrites(h), []string{})
	if code := h.call("GET", "/api/v1/users/ops", nil, nil); code != 200 {
		t.Fatalf("own page %d", code)
	}
}

func TestProfileUserGetAndHostile(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v AccountView
	if code := h.call("GET", "/api/v1/users/bob", nil, &v); code != 200 || v.User.USER != "bob" || !v.TwoFA || v.Self || !v.Editable.Admin {
		t.Fatalf("%d %+v", code, v)
	}
	if code := h.call("GET", "/api/v1/users/missing", nil, nil); code != 404 {
		t.Fatalf("missing %d", code)
	}
	if code := h.call("GET", "/api/v1/users/-h", nil, nil); code != 400 {
		t.Fatalf("hostile %d", code)
	}
	h2 := newHarness(t)
	h2.loginBob()
	if code := h2.call("GET", "/api/v1/users/admin", nil, nil); code != 403 {
		t.Fatalf("bob %d", code)
	}
	if code := h2.call("PUT", "/api/v1/users/bob", profSame(), nil); code != 403 {
		t.Fatalf("bob put %d", code)
	}
}

func TestProfileNameservers(t *testing.T) {
	h := newHarnessWith(t, profUsers(t, "carl", nil))
	h.loginAdmin()
	r := UserEditRequest{AccountRequest: AccountRequest{Name: "Old Name", Email: "old@example.com", Language: "en", SortOrder: "name"},
		Nameservers: []string{"ns1.example.com", "ns2.example.com", " . ", "ns3.example.com"}}
	if code := h.call("PUT", "/api/v1/users/carl", r, nil); code != 200 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{"v-change-user-ns carl ns1.example.com ns2.example.com ns3.example.com"})

	var e APIError
	r.Nameservers = []string{"", "ns2.example.com"}
	if code := h.call("PUT", "/api/v1/users/carl", r, &e); code != 400 || e.Error.Message != `Field "%s" can not be blank.` ||
		!reflect.DeepEqual(e.Error.Args, []string{"Nameserver 1"}) {
		t.Fatalf("%d %+v", code, e)
	}
	r.Nameservers = []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com", "e.example.com", "f.example.com", "g.example.com", "h.example.com", "i.example.com"}
	if code := h.call("PUT", "/api/v1/users/carl", r, &e); code != 400 || e.Error.Message != "Up to 8 name servers are allowed." {
		t.Fatalf("%d %+v", code, e)
	}
	if len(profWrites(h)) != 1 {
		t.Fatalf("extra commands: %v", profWrites(h))
	}
	// Unchanged list: nothing runs.
	h2 := newHarnessWith(t, profUsers(t, "carl", map[string]string{"NS": "ns1.example.com,ns2.example.com"}))
	h2.loginAdmin()
	r.Nameservers = []string{"ns1.example.com", "ns2.example.com"}
	if code := h2.call("PUT", "/api/v1/users/carl", r, nil); code != 200 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h2), []string{})
}

func TestProfileOptions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var o AccountOptions
	if code := h.call("GET", "/api/v1/account/options", nil, &o); code != 200 {
		t.Fatal(code)
	}
	if len(o.Languages) < 2 || len(o.Themes) == 0 || len(o.Packages) == 0 || len(o.Shells) == 0 ||
		!reflect.DeepEqual(o.PHPVersions, []string{"8.2", "8.3"}) || !o.DNSSystem || !o.LogsVisible {
		t.Fatalf("%+v", o)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"DEFAULT_PHP_VERSION": "php-8.3"}})
	h.loginAdmin()
	if h.call("GET", "/api/v1/account/options", nil, &o); o.DefaultPHP != "8.3" {
		t.Fatalf("defaultPhp = %q", o.DefaultPHP)
	}
	hb := newHarness(t)
	hb.loginBob()
	o = AccountOptions{}
	if code := hb.call("GET", "/api/v1/account/options", nil, &o); code != 200 || len(o.Packages) != 0 || len(o.Shells) != 0 || len(o.PHPVersions) != 0 || o.DefaultPHP != "" {
		t.Fatalf("%d %+v", code, o)
	}
	for _, c := range hb.fixture.Calls() {
		switch c.Cmd {
		case "v-list-user-packages", "v-list-sys-shells", "v-list-sys-php":
			t.Fatalf("user ran %s", c.Cmd)
		}
	}
}

// ---------------------------------------------------------------- add user

func goodAddUser() AddUserRequest {
	return AddUserRequest{Username: "newbie", Password: "Passw0rdOK", Email: "new@example.com", Name: "New Bie",
		Language: "de", Role: "user", Package: "default", Notify: "boss@example.com"}
}

func TestAddUserOrderAndWelcome(t *testing.T) {
	mm, tp := &fakeMailer{}, &fakeTemplates{}
	h := newHarnessWith(t, overrides{}, withMail(mm, tp, t.TempDir()))
	h.loginAdmin()
	req := goodAddUser()
	req.LoginDisabled = true
	if code := h.call("POST", "/api/v1/users", req, nil); code != 200 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{
		"v-add-user newbie <pw> new@example.com default New Bie",
		"v-change-user-language newbie de",
		"v-change-user-role newbie user",
		"v-change-user-config-value newbie LOGIN_DISABLED yes",
	})
	if len(mm.sent) != 1 || mm.sent[0].To != "boss@example.com" || !strings.Contains(mm.sent[0].Body, "Password: Passw0rdOK") ||
		!strings.Contains(mm.sent[0].Subject, "Hostisan") || strings.Contains(mm.sent[0].Subject, "%s") {
		t.Fatalf("%+v", mm.sent)
	}
	if !reflect.DeepEqual(tp.asked, [][2]string{{"de", "account_ready"}}) {
		t.Fatalf("template asked for %v", tp.asked)
	}
}

func TestAddUserWelcomeLinkHasPanelPort(t *testing.T) {
	// Hestia's welcome body links to host:port; the subject keeps the bare hostname.
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{Config: map[string]string{"BACKEND_PORT": "8083"}}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/users", goodAddUser(), nil); code != 200 {
		t.Fatal(code)
	}
	if len(mm.sent) != 1 || !strings.Contains(mm.sent[0].Body, "https://server1.acme-hosting.net:8083/login/") {
		t.Fatalf("%+v", mm.sent)
	}
}

func TestAddUserMailFailureStill200(t *testing.T) {
	mm := &fakeMailer{err: os.ErrInvalid}
	h := newHarnessWith(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/users", goodAddUser(), nil); code != 200 {
		t.Fatal(code)
	}
	if len(mm.sent) != 1 {
		t.Fatal("no attempt to send")
	}
}

func TestAddUserNoNotifyNoMail(t *testing.T) {
	mm := &fakeMailer{}
	h := newHarnessWith(t, overrides{}, withMail(mm, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	req := goodAddUser()
	req.Notify = ""
	if code := h.call("POST", "/api/v1/users", req, nil); code != 200 || len(mm.sent) != 0 {
		t.Fatalf("%d %v", code, mm.sent)
	}
	profEq(t, profWrites(h), []string{
		"v-add-user newbie <pw> new@example.com default New Bie",
		"v-change-user-language newbie de",
		"v-change-user-role newbie user",
	})
}

func TestAddUserValidationOrder(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	req := AddUserRequest{Email: "bad", Password: "weak", Role: "user"}
	if code := h.call("POST", "/api/v1/users", req, &e); code != 400 || e.Error.Message != blankFieldMsg ||
		!reflect.DeepEqual(e.Error.Args, []string{"Username", "Package", "Contact Name"}) {
		t.Fatalf("blank: %d %+v", code, e)
	}
	req = goodAddUser()
	req.Email, req.Password = "bad", "weak"
	if code := h.call("POST", "/api/v1/users", req, &e); code != 400 || e.Error.Message != "Please enter a valid email address." {
		t.Fatalf("email: %d %+v", code, e)
	}
	req.Email = "ok@example.com"
	if code := h.call("POST", "/api/v1/users", req, &e); code != 400 || e.Error.Message != "Password does not match the minimum requirements." {
		t.Fatalf("password: %d %+v", code, e)
	}
	for _, mod := range []func(*AddUserRequest){
		func(r *AddUserRequest) { r.Username = "-h" },
		func(r *AddUserRequest) { r.Role = "root" },
		func(r *AddUserRequest) { r.Package = "a b" },
		func(r *AddUserRequest) { r.Language = "zz" },
	} {
		r := goodAddUser()
		mod(&r)
		if code := h.call("POST", "/api/v1/users", r, nil); code != 400 {
			t.Errorf("hostile: %d", code)
		}
	}
	profEq(t, profWrites(h), []string{})
	hb := newHarness(t)
	hb.loginBob()
	if code := hb.call("POST", "/api/v1/users", goodAddUser(), nil); code != 403 {
		t.Fatalf("bob %d", code)
	}
}

func TestAddUserStopsOnError(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-user-role": {{Args: []string{"*", "*"}, Stdout: "Error: nope\n", Exit: 2}},
	}}, withMail(&fakeMailer{}, &fakeTemplates{}, t.TempDir()))
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/users", goodAddUser(), nil); code != 400 {
		t.Fatal(code)
	}
	if got := profWrites(h); len(got) != 3 {
		t.Fatalf("%v", got)
	}
}

// ---------------------------------------------------------------- 2FA

func profSetup(t *testing.T, h *harness) TwoFASetup {
	t.Helper()
	var s TwoFASetup
	if code := h.call("POST", "/api/v1/account/2fa/setup", nil, &s); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	return s
}

func TestTwoFASetupRunsNothing(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	s := profSetup(t, h)
	if len(s.Secret) != 32 || !strings.HasPrefix(s.QR, "data:image/png;base64,") || !strings.HasPrefix(s.OtpauthURL, "otpauth://totp/Hostisan") ||
		!strings.Contains(s.OtpauthURL, "secret="+s.Secret) {
		t.Fatalf("%+v", s)
	}
	profEq(t, profWrites(h), []string{})
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-add-user-2fa" {
			t.Fatal("v-add-user-2fa ran")
		}
	}
}

func TestTwoFASetupOwnAccountOnly(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "susp"}, nil); code != 200 {
		t.Fatal(code)
	}
	if code := h.call("POST", "/api/v1/account/2fa/setup", nil, nil); code != 403 {
		t.Fatalf("setup while looking: %d", code)
	}
	hb := newHarness(t)
	hb.loginBob()
	if code := hb.call("POST", "/api/v1/account/2fa/setup", nil, nil); code != 409 {
		t.Fatalf("already on: %d", code)
	}
}

func TestTwoFAConfirm(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	h := newHarnessWith(t, overrides{}, withClock(now))
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: "000000", CurrentPassword: "pw"}, nil); code != 400 {
		t.Fatalf("no setup: %d", code)
	}
	s := profSetup(t, h)
	if code := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: profWrongCode(s.Secret, now), CurrentPassword: "pw"}, nil); code != 400 {
		t.Fatalf("wrong code: %d", code)
	}
	if code := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: "x"}, nil); code != 400 {
		t.Fatalf("no password: %d", code)
	}
	profEq(t, profWrites(h), []string{})
	code, _ := totp.Code(s.Secret, now)
	if c := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: code, CurrentPassword: "pw"}, nil); c != 200 {
		t.Fatalf("good code: %d", c)
	}
	profEq(t, profWrites(h), []string{
		"v-change-user-config-value admin TWOFA " + s.Secret,
		"v-log-action admin Info System Two-factor authentication enabled.",
	})
	if c := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: code, CurrentPassword: "pw"}, nil); c != 400 {
		t.Fatalf("pending secret reused: %d", c)
	}
}

func TestTwoFAConfirmWrongPasswordAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cur := now
	var bad atomic.Bool
	h := newHarnessWith(t, overrides{}, profBadPassword(&bad), func(d *Deps) { d.Now = func() time.Time { return cur } })
	h.loginAdmin()
	bad.Store(true)
	s := profSetup(t, h)
	code, _ := totp.Code(s.Secret, now)
	if c := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: code, CurrentPassword: "bad"}, nil); c != 400 {
		t.Fatalf("%d", c)
	}
	if profLoginLogs(h) != 1 {
		t.Fatal("wrong password not logged as failed")
	}
	profEq(t, profWrites(h), []string{})
}

func TestTwoFAConfirmExpired(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cur := now
	h := newHarnessWith(t, overrides{}, func(d *Deps) { d.Now = func() time.Time { return cur } })
	h.loginAdmin()
	s := profSetup(t, h)
	cur = now.Add(11 * time.Minute)
	code, _ := totp.Code(s.Secret, cur)
	if c := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: code, CurrentPassword: "pw"}, nil); c != 400 {
		t.Fatalf("%d", c)
	}
	profEq(t, profWrites(h), []string{})
}

func TestTwoFADisable(t *testing.T) {
	var bad atomic.Bool
	h := newHarnessWith(t, overrides{}, profBadPassword(&bad))
	h.loginBob()
	bad.Store(true)
	if code := h.call("DELETE", "/api/v1/account/2fa", PasswordConfirmRequest{}, nil); code != 400 {
		t.Fatalf("no password: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/account/2fa", PasswordConfirmRequest{CurrentPassword: "bad"}, nil); code != 400 {
		t.Fatalf("wrong password: %d", code)
	}
	profEq(t, profWrites(h), []string{})
	if profLoginLogs(h) != 1 {
		t.Fatal("wrong password not logged")
	}
	g := newHarness(t)
	g.loginBob()
	if code := g.call("DELETE", "/api/v1/account/2fa", PasswordConfirmRequest{CurrentPassword: "pw"}, nil); code != 200 {
		t.Fatalf("good: %d", code)
	}
	profEq(t, profWrites(g), []string{"v-delete-user-2fa bob"})
}

func TestTwoFADisableAdminNoPassword(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("DELETE", "/api/v1/users/bob/2fa", nil, nil); code != 200 {
		t.Fatal(code)
	}
	if code := h.call("DELETE", "/api/v1/users/-h/2fa", nil, nil); code != 400 {
		t.Fatal(code)
	}
	profEq(t, profWrites(h), []string{"v-delete-user-2fa bob"})
	// While looking at a user, the own-account route needs no password either.
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if code := h.call("DELETE", "/api/v1/account/2fa", nil, nil); code != 200 {
		t.Fatalf("look: %d", code)
	}
	hb := newHarness(t)
	hb.loginBob()
	if code := hb.call("DELETE", "/api/v1/users/bob/2fa", nil, nil); code != 403 {
		t.Fatalf("bob: %d", code)
	}
}

func TestProfileOpsCannotDisableOwnTwoFAThroughAdminRoute(t *testing.T) {
	h := newHarness(t)
	h.loginOps()
	if code := h.call("DELETE", "/api/v1/users/ops/2fa", nil, nil); code != 403 {
		t.Fatalf("%d", code)
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileOpsRoleOnSelfForbidden(t *testing.T) {
	h := newHarness(t)
	h.loginOps()
	role := "user"
	r := UserEditRequest{AccountRequest: AccountRequest{Name: "Operations Admin", Email: "ops@acme-hosting.net", Language: "en", SortOrder: "name"}, Role: &role}
	if code := h.call("PUT", "/api/v1/users/ops", r, nil); code != 403 {
		t.Fatalf("%d", code)
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileEmptyShellPHPNameserversAreNotOffered(t *testing.T) {
	h := newHarnessWith(t, profUsers(t, "carl", nil))
	h.loginAdmin()
	empty := ""
	r := UserEditRequest{AccountRequest: AccountRequest{Name: "Old Name", Email: "old@example.com", Language: "en", SortOrder: "name"},
		Shell: &empty, PHPCli: &empty, Nameservers: []string{}}
	if code := h.call("PUT", "/api/v1/users/carl", r, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileIPListNeedsAddresses(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := profSame()
	r.UseIPList = true
	var e APIError
	if code := h.call("PUT", "/api/v1/account", r, &e); code != 400 || e.Error.Message != blankFieldMsg ||
		!reflect.DeepEqual(e.Error.Args, []string{"Allowed IP addresses"}) {
		t.Fatalf("%d %+v", code, e)
	}
	profEq(t, profWrites(h), []string{})
}

func TestProfileThemeUpdatesSession(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := profSame()
	r.Theme = "dark"
	if code := h.call("PUT", "/api/v1/account", r, nil); code != 200 {
		t.Fatal(code)
	}
	if b := h.bootstrap(); b.Theme != "dark" {
		t.Fatalf("theme = %q", b.Theme)
	}
}

// profTwoFAFlip makes v-list-user report admin with 2FA on once on is set.
type profTwoFAFlip struct {
	runner.Runner
	on *atomic.Bool
}

func (f profTwoFAFlip) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	res, err := f.Runner.Run(ctx, cmd, args...)
	if cmd == "v-list-user" && len(args) > 0 && args[0] == "admin" && f.on.Load() && err == nil {
		res.Stdout = []byte(strings.Replace(string(res.Stdout), `"TWOFA": ""`, `"TWOFA": "ABCDEFGH"`, 1))
	}
	return res, err
}

func TestTwoFAConfirmRefusesWhenAlreadyOn(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	var on atomic.Bool
	h := newHarnessWith(t, overrides{}, withClock(now), func(d *Deps) { d.H = &hestia.Client{R: profTwoFAFlip{d.R, &on}} })
	h.loginAdmin()
	s := profSetup(t, h)
	on.Store(true)
	code, _ := totp.Code(s.Secret, now)
	if c := h.call("POST", "/api/v1/account/2fa/confirm", TwoFAConfirmRequest{Code: code, CurrentPassword: "pw"}, nil); c != 409 {
		t.Fatalf("%d", c)
	}
	profEq(t, profWrites(h), []string{})
}

// profWrongCode is a 6-digit code that is not valid at now or one step either side.
func profWrongCode(secret string, now time.Time) string {
	for n := 0; ; n++ {
		c := fmt.Sprintf("%06d", n*7919%1000000)
		if !totp.Verify(secret, c, now) {
			return c
		}
	}
}

func TestProfilePasswordChangeEndsSessionsWhenLaterStepFails(t *testing.T) {
	o := profUsers(t, "carl", nil)
	o.Fixtures = map[string][]fixtureEntry{
		"v-change-user-role": {{Args: []string{"*", "*"}, Stdout: "Error: nope\n", Exit: 2}},
	}
	h := newHarnessWith(t, o)
	h.loginAdmin()
	h.store.Save(&session.Session{ID: "carl-session", Token: "t", User: "carl", UserContext: "user"})
	r := profSame()
	role := "admin"
	r.Password, r.Role = "Passw0rdOK", &role
	if code := h.call("PUT", "/api/v1/users/carl", r, nil); code != 400 {
		t.Fatalf("code %d", code)
	}
	if _, ok := h.store.Get("carl-session"); ok {
		t.Fatal("carl's session survived the password change")
	}
}

// A new user's language starts at the system LANGUAGE (owner ruling 13).
func TestAccountOptionsSystemLanguage(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"LANGUAGE": "de"}})
	h.loginAdmin()
	var o AccountOptions
	if code := h.call("GET", "/api/v1/account/options", nil, &o); code != 200 || o.SystemLanguage != "de" {
		t.Fatalf("%d systemLanguage = %q", code, o.SystemLanguage)
	}
}
