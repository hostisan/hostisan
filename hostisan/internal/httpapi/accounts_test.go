package httpapi

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/i18n"
	"github.com/hostisan/hostisan/internal/session"
)

// acctWireTypes is every type declared in types_accounts.go (TestAcctWireTypesListed
// keeps the list complete).
var acctWireTypes = []any{
	UserList{}, UserBulkRequest{}, PackageList{}, PackageOptions{}, PackageRequest{}, PackageSaved{},
	AccountView{}, AccountEditable{}, AccountOptions{}, AccountRequest{}, UserEditRequest{}, AddUserRequest{},
	AccountSaved{}, TwoFASetup{}, TwoFAConfirmRequest{}, PasswordConfirmRequest{},
	ResetRequest{}, ResetConfirmRequest{}, UnlockRequest{}, UnlockConfirmRequest{},
	SSHKeys{}, AddSSHKeyRequest{}, AccessKeys{}, AddAccessKeyRequest{}, AccessKeyCreated{},
	Notifications{}, Logs{}, AuthLog{}, Stats{}, StatsMonth{},
	CronJobs{}, CronUsage{}, CronOptions{}, CronRequest{}, CronReportsRequest{},
	Backups{}, BackupUsage{}, BackupView{}, QueuedReply{}, DownloadReply{}, RestoreRequest{},
	ExclusionsView{}, ExclusionsText{}, ExclusionsRequest{}, Snapshots{}, SnapshotView{}, SnapshotFiles{},
}

func TestAcctWireTypesListed(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "types_accounts.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.TYPE {
			for _, s := range g.Specs {
				declared = append(declared, s.(*ast.TypeSpec).Name.Name)
			}
		}
	}
	var listed []string
	for _, v := range acctWireTypes {
		listed = append(listed, reflect.TypeOf(v).Name())
	}
	// UserBulkRequest is an alias of BulkRequest, so reflection names it BulkRequest.
	listed = slices.DeleteFunc(listed, func(s string) bool { return s == "BulkRequest" })
	listed = append(listed, "UserBulkRequest")
	sort.Strings(declared)
	sort.Strings(listed)
	if !slices.Equal(declared, listed) {
		t.Fatalf("types_accounts.go declares %v\nthe test lists        %v", declared, listed)
	}
}

var acctSecretName = regexp.MustCompile(`(?i)RKEY|TWOFA|QRCODE|SESSION|SECRET`)

// TestNoSecretFields walks every Plan 3 wire type, including the Hestia types inside
// them, and fails on a field whose Go or JSON name looks like a secret. The two
// exceptions carry a secret on purpose, once (Global Constraints). A bool can't hold a
// secret, so AccountView.TwoFA (2FA on or off) is fine.
func TestNoSecretFields(t *testing.T) {
	allowed := map[string]bool{"TwoFASetup.Secret": true, "AccessKeyCreated.Secret": true}
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := range rt.NumField() {
			f := rt.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			id := rt.Name() + "." + f.Name
			if f.Type.Kind() != reflect.Bool && !allowed[id] &&
				(acctSecretName.MatchString(f.Name) || acctSecretName.MatchString(name)) {
				t.Errorf("%s (json %q) looks like a secret", id, name)
			}
			walk(f.Type)
		}
	}
	for _, v := range acctWireTypes {
		walk(reflect.TypeOf(v))
	}
	for id := range allowed {
		typ, field, _ := strings.Cut(id, ".")
		found := false
		for rt := range seen {
			if rt.Name() == typ {
				_, found = rt.FieldByName(field)
			}
		}
		if !found {
			t.Errorf("allowed secret %s no longer exists", id)
		}
	}
}

// The wire types emit [] for empty lists when built by the slices; here only the JSON
// names the UI relies on are pinned.
func TestAcctWireNames(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{UserList{}, "users rootUser self"},
		{PackageRequest{}, "name webTemplate backendTemplate proxyTemplate dnsTemplate webDomains webAliases dnsDomains dnsRecords mailDomains mailAccounts rateLimit databases cronJobs diskQuota bandwidth cpuQuota cpuQuotaPeriod memoryLimit swapLimit backups backupsIncremental shell nameservers"},
		{AccountView{}, "user twofa self editable"},
		{AccountRequest{}, "name email password currentPassword language theme sortOrder useIpList allowedIps"},
		{UserEditRequest{}, "AccountRequest loginDisabled package role shell phpCli nameservers"},
		{AddUserRequest{}, "username password email name language role package loginDisabled notify"},
		{TwoFASetup{}, "secret otpauthUrl qr"},
		{StatsMonth{}, "date time package ipOwned diskQuota uDisk uDiskDirs uDiskWeb uDiskMail uDiskDb bandwidth uBandwidth uWebDomains uWebSsl uWebAliases uDnsDomains uDnsRecords uMailDomains uMailDkim uMailAccounts uDatabases uCronJobs uBackups uUsers"},
		{CronJobs{}, "jobs reports usage contact"},
		{CronUsage{}, "jobs limit suspended"},
		{RestoreRequest{}, "all web dns mail db udir cron"},
		{ExclusionsText{}, "web mail db userdir"},
		{SnapshotFiles{}, "path parent nodes"},
	} {
		var got []string
		rt := reflect.TypeOf(c.v)
		for i := range rt.NumField() {
			name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
			if name == "" {
				name = rt.Field(i).Name // embedded
			}
			got = append(got, name)
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%T = %s\nwant %s", c.v, strings.Join(got, " "), c.want)
		}
	}
}

func TestAcctParams(t *testing.T) {
	for _, c := range []struct {
		param  func(http.ResponseWriter, *http.Request) (string, bool)
		name   string
		good   string
		badOne string
	}{
		{acctUserParam, "user", "bob", "-h"},
		{acctPackageParam, "package", "starter-plus", "../x"},
	} {
		for _, v := range []string{c.good, c.badOne} {
			r := httptest.NewRequest("GET", "/", nil)
			r.SetPathValue(c.name, v)
			w := httptest.NewRecorder()
			got, ok := c.param(w, r)
			if v == c.good && (!ok || got != v || w.Code != 200) {
				t.Errorf("%s %q: %q, %v, %d", c.name, v, got, ok, w.Code)
			}
			if v == c.badOne && (ok || w.Code != 400) {
				t.Errorf("%s %q accepted (%d)", c.name, v, w.Code)
			}
		}
	}
	if acctUserCommandTimeout.Seconds() != 85 {
		t.Errorf("acctUserCommandTimeout = %v", acctUserCommandTimeout)
	}
}

func TestAcctIsSelf(t *testing.T) {
	for _, c := range []struct {
		s    *session.Session
		want bool
	}{
		{&session.Session{User: "bob", UserContext: "user"}, true},
		{&session.Session{User: "admin", UserContext: "admin"}, true},
		{&session.Session{User: "admin", UserContext: "admin", Look: "bob"}, false},
		{nil, false},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if c.s != nil {
			r = r.WithContext(session.WithSession(r.Context(), c.s))
		}
		if got := acctIsSelf(r); got != c.want {
			t.Errorf("acctIsSelf(%+v) = %v, want %v", c.s, got, c.want)
		}
	}
}

// The admin-read guard: anonymous 401, a user 403, an admin through.
func TestAcctAdminRead(t *testing.T) {
	h := newHarness(t)
	s := &server{Deps: h.deps}
	mux := http.NewServeMux()
	mux.Handle("GET /probe", s.acctAdminRead(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, OK{Status: "ok"})
	}))
	probe := httptest.NewServer(mux)
	t.Cleanup(probe.Close)
	h.bootstrap()
	if code := h.callURL(probe.URL, "GET", "/probe", nil); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
	h.loginBob()
	if code := h.callURL(probe.URL, "GET", "/probe", nil); code != 403 {
		t.Errorf("bob: %d", code)
	}
	h = newHarness(t)
	s = &server{Deps: h.deps}
	mux = http.NewServeMux()
	mux.Handle("GET /probe", s.acctAdminRead(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, OK{Status: "ok"})
	}))
	probe2 := httptest.NewServer(mux)
	t.Cleanup(probe2.Close)
	h.loginAdmin()
	if code := h.callURL(probe2.URL, "GET", "/probe", nil); code != 200 {
		t.Errorf("admin: %d", code)
	}
}

func emailServer(t *testing.T, tp *fakeTemplates, localeDir string) *server {
	t.Helper()
	d := Deps{Sessions: &session.Manager{SystemHostname: "panel.acme-hosting.net"}}
	if tp != nil {
		d.EmailTemplates = tp
	}
	return &server{Deps: d, catalogs: &i18n.Catalogs{Dir: localeDir}}
}

var emailCfg = hestia.SysConfig{"APP_NAME": "Hestia Control Panel", "SUBJECT_EMAIL": "[{{hostname}}] {{subject}}", "LANGUAGE": "en"}

const resetBody = "Hello {{name}},\n\nTo reset your {{appname}} password, please follow this link:\nhttps://{{hostname}}/reset/?action=confirm&user={{user}}&code={{resetcode}}\n\nAlternatively, you may go to https://{{hostname}}/reset/?action=code&user={{user}} and enter the following reset code:\n{{resetcode}}\n\nIf you did not request password reset, please ignore this message and accept our apologies.\n\nBest regards,\n\n--\n{{appname}}"

func TestRenderEmailTemplate(t *testing.T) {
	tp := &fakeTemplates{ok: true, subject: " Reset for {{user}} on {{appname}} at {{hostname}} ",
		body: "<p>Hi {{name}}, code {{resetcode}} for {{appname}}</p>"}
	s := emailServer(t, tp, "")
	msg := s.renderEmail(context.Background(), emailCfg, "de", "reset_password", resetBody, "Password Reset",
		map[string]string{"user": "bob"},
		map[string]string{"name": `Bob <b>"&`, "resetcode": "ABC123"})
	if !reflect.DeepEqual(tp.asked, [][2]string{{"de", "reset_password"}}) {
		t.Errorf("templates asked %v", tp.asked)
	}
	if msg.Subject != "Reset for bob on Hostisan at panel.acme-hosting.net" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if msg.Body != "<p>Hi Bob &lt;b&gt;&#34;&amp;, code ABC123 for Hostisan</p>" {
		t.Errorf("body = %q", msg.Body)
	}
	if msg.To != "" {
		t.Errorf("To = %q; the caller sets it", msg.To)
	}
}

func TestRenderEmailFallback(t *testing.T) {
	tp := &fakeTemplates{}
	s := emailServer(t, tp, "")
	msg := s.renderEmail(context.Background(), emailCfg, "", "reset_password", resetBody, "Password Reset at 2026-10-21 10:00:00",
		nil, map[string]string{"name": "Bob", "user": "bob", "hostname": "panel.acme-hosting.net", "resetcode": "X<Y"})
	// No language: the system language, then English.
	if !reflect.DeepEqual(tp.asked, [][2]string{{"en", "reset_password"}}) {
		t.Errorf("templates asked %v", tp.asked)
	}
	// SUBJECT_EMAIL wraps the default subject, and its own placeholders are filled.
	if msg.Subject != "[panel.acme-hosting.net] Password Reset at 2026-10-21 10:00:00" {
		t.Errorf("subject = %q", msg.Subject)
	}
	for _, want := range []string{"Hello Bob,", "To reset your Hostisan password",
		"https://panel.acme-hosting.net/reset/?action=confirm&user=bob&code=X&lt;Y", "--\nHostisan"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, msg.Body)
		}
	}
	if strings.Contains(msg.Body, "{{") {
		t.Errorf("unfilled placeholder:\n%s", msg.Body)
	}

	// An empty SUBJECT_EMAIL means the subject alone; no templates configured is fine.
	s = emailServer(t, nil, "")
	msg = s.renderEmail(context.Background(), hestia.SysConfig{}, "en", "two_factor_unlock", "Code: {{code}}", "Unlock", nil,
		map[string]string{"code": "123"})
	if msg.Subject != "Unlock" || msg.Body != "Code: 123" {
		t.Errorf("message = %+v", msg)
	}
}

func TestRenderEmailTranslatesFallback(t *testing.T) {
	locale := filepath.Join("..", "..", "..", "web", "locale")
	s := emailServer(t, &fakeTemplates{}, locale)
	msg := s.renderEmail(context.Background(), emailCfg, "fr", "reset_password", resetBody, "Password Reset", nil,
		map[string]string{"name": "Bob", "user": "bob", "hostname": "h", "resetcode": "C"})
	if strings.Contains(msg.Body, "To reset your") || !strings.Contains(msg.Body, "Bob") {
		t.Errorf("French body not translated:\n%s", msg.Body)
	}
	// An unknown language falls back to the English msgid.
	msg = s.renderEmail(context.Background(), emailCfg, "xx", "reset_password", resetBody, "Password Reset", nil,
		map[string]string{"name": "Bob"})
	if !strings.Contains(msg.Body, "To reset your") {
		t.Errorf("unknown language body:\n%s", msg.Body)
	}
}

// The body's {{hostname}} is the system hostname unless the caller passes its own.
func TestRenderEmailBodyHostname(t *testing.T) {
	s := emailServer(t, &fakeTemplates{}, "")
	msg := s.renderEmail(context.Background(), emailCfg, "en", "x", "Go to https://{{hostname}}/reset/", "S", nil, nil)
	if msg.Body != "Go to https://panel.acme-hosting.net/reset/" {
		t.Errorf("default body = %q", msg.Body)
	}
	msg = s.renderEmail(context.Background(), emailCfg, "en", "x", "Go to https://{{hostname}}/reset/", "S", nil,
		map[string]string{"hostname": "other.example"})
	if msg.Body != "Go to https://other.example/reset/" {
		t.Errorf("overridden body = %q", msg.Body)
	}
}
