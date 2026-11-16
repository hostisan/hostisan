package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/runner"
)

const (
	acctRecPW   = "Passw0rdOK"
	acctRecIP   = "127.0.0.1"
	acctRecUA   = "Go-http-client/1.1"
	acctRecCode = "ABCDEFGHJKLMNPQRSTUVWXYZ23"
)

var (
	acctRecT0     = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	acctRecFileRE = regexp.MustCompile(`\S*/hostisan-\d+`)
	acctRecCodeRE = regexp.MustCompile(`code=([A-Z2-7]{26})`)
)

type acctRecEnv struct {
	*harness
	mm      *fakeMailer
	tp      *fakeTemplates
	sleeps  []time.Duration
	onSleep func()
	now     time.Time
	files   []string // contents of every file passed to a rkey/password command, in order
	logs    *bytes.Buffer
}

func acctRecHash(prefix, code string) string {
	sum := sha256.Sum256([]byte(prefix + code))
	return hex.EncodeToString(sum[:])
}

// acctRecUser is a v-list-user entry carrying rkey.
func acctRecUser(name, rkey, twofa, susp string) fixtureEntry {
	return fixtureEntry{Args: []string{name, "json"}, Stdout: fmt.Sprintf(
		`{%q:{"NAME":"Rec Tester","CONTACT":"rec@acme.test","LANGUAGE":"en","ROLE":"user","SUSPENDED":%q,"TWOFA":%q,"RKEY":%q}}`,
		name, susp, twofa, rkey)}
}

// acctRecConfirmFixtures: user "rec" holds rkey, set at issued.
func acctRecConfirmFixtures(rkey string, issued time.Time) overrides {
	return overrides{
		Dir: "../../testdata/fixtures/recovery",
		Fixtures: map[string][]fixtureEntry{
			"v-list-user": {acctRecUser("rec", rkey, "JBSWY3DPEHPK3PXP", "no"), acctRecUser("bob", "", "", "no"),
				{Args: []string{"*", "json"}, Stdout: "Error: user doesn't exist\n", Exit: 3}},
			"v-get-user-value": {{Args: []string{"rec", "RKEYEXP"}, Stdout: strconv.FormatInt(issued.Unix(), 10) + "\n"},
				{Args: []string{"*", "*"}, Stdout: "\n"}},
		},
	}
}

func acctRecHarness(t *testing.T, o overrides, extra ...func(*Deps)) *acctRecEnv {
	t.Helper()
	e := &acctRecEnv{mm: &fakeMailer{}, tp: &fakeTemplates{}, now: acctRecT0, logs: &bytes.Buffer{}}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(e.logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	tmp := t.TempDir()
	e.harness = newHarnessWith(t, o, withMail(e.mm, e.tp, tmp), func(d *Deps) {
		d.Now = func() time.Time { return e.now }
		d.Background = func(f func()) { f() } // mail is checked right after the reply
		d.Auth = &auth.Service{R: d.R, H: d.H, TmpDir: tmp, Now: d.Now,
			Sleep: func(x time.Duration) {
				if e.onSleep != nil {
					e.onSleep()
				}
				e.sleeps = append(e.sleeps, x)
			}}
		d.R = spyRunner{Runner: d.R, before: func(cmd string, args []string) {
			if cmd == "v-change-user-rkey" || cmd == "v-change-user-password" {
				if n := len(args); n > 1 {
					b, _ := os.ReadFile(args[n-1])
					e.files = append(e.files, strings.TrimSpace(string(b)))
				}
			}
		}}
		for _, x := range extra {
			x(d)
		}
	})
	e.bootstrap()
	return e
}

// writes is every command run since mark, file paths shown as <f>.
func (e *acctRecEnv) calls(from int) []string {
	var out []string
	for _, c := range e.fixture.Calls()[from:] {
		out = append(out, acctRecFileRE.ReplaceAllString(c.Cmd+" "+strings.Join(c.Args, " "), "<f>"))
	}
	return out
}

func (e *acctRecEnv) mark() int { return len(e.fixture.Calls()) }

func acctRecEq(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls\n got  %q\n want %q", got, want)
	}
}

const acctRecSent = "sent"

func (e *acctRecEnv) post(path string, body any) (int, statusReply) {
	e.t.Helper()
	var r statusReply
	code := e.call("POST", path, body, &r)
	return code, r
}

func (e *acctRecEnv) assertSent(path string, body any) {
	e.t.Helper()
	if code, r := e.post(path, body); code != 200 || r.Status != acctRecSent {
		e.t.Fatalf("%s: %d %+v", path, code, r)
	}
}

func acctRecDevUsers(n int) overrides {
	entries := []fixtureEntry{}
	for i := 1; i <= n; i++ {
		entries = append(entries, acctRecUser(fmt.Sprintf("u%d", i), "", "", "no"))
	}
	entries = append(entries, fixtureEntry{Args: []string{"*", "json"}, Stdout: "Error: user doesn't exist\n", Exit: 3})
	return overrides{Dir: "../../testdata/fixtures/recovery", Fixtures: map[string][]fixtureEntry{"v-list-user": entries}}
}

// ---------------------------------------------------------------- reset request

func TestResetRequestUniformReplies(t *testing.T) {
	cases := []struct {
		name string
		user string
		mail string
		send bool
	}{
		{"unknown user", "ghost", "rec@acme.test", false},
		{"wrong email", "rec", "other@acme.test", false},
		{"suspended", "recsusp", "recsusp@acme.test", false},
		{"success", "rec", "rec@acme.test", true},
	}
	var bodies []string
	for _, c := range cases {
		e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery", Config: map[string]string{"BACKEND_PORT": "8083"}})
		before := e.mark()
		var raw string
		code, raw := rawCall(e.harness, "POST", "/api/v1/auth/reset/request", "", ResetRequest{User: c.user, Email: c.mail})
		if code != 200 {
			t.Fatalf("%s: status %d", c.name, code)
		}
		bodies = append(bodies, raw)
		calls := e.calls(before)
		if c.send {
			acctRecEq(t, calls, []string{"v-list-user rec json", "v-change-user-rkey rec <f>"})
			if len(e.mm.sent) != 1 || len(e.files) != 1 || len(e.sleeps) != 1 || e.sleeps[0] != 2*time.Second {
				t.Fatalf("%s: sent=%d files=%d sleeps=%v", c.name, len(e.mm.sent), len(e.files), e.sleeps)
			}
			m := acctRecCodeRE.FindStringSubmatch(e.mm.sent[0].Body)
			if m == nil || e.files[0] != acctRecHash("reset:", m[1]) {
				t.Fatalf("stored hash %q does not match the emailed code in %q", e.files[0], e.mm.sent[0].Body)
			}
			if !strings.Contains(e.mm.sent[0].Body, "https://server1.acme-hosting.net:8083/reset/?action=confirm&user=rec&code="+m[1]) ||
				e.mm.sent[0].To != "rec@acme.test" {
				t.Fatalf("email = %+v", e.mm.sent[0])
			}
			// Hestia's subject: "Password Reset at <date>" (owner ruling 10).
			if !strings.Contains(e.mm.sent[0].Subject, "Password Reset at 2026-10-21 12:00:00") {
				t.Fatalf("subject = %q", e.mm.sent[0].Subject)
			}
			if strings.Contains(raw, m[1]) || strings.Contains(e.logs.String(), m[1]) {
				t.Fatal("the code leaked into the reply or the log")
			}
		} else {
			for _, c := range calls {
				if strings.HasPrefix(c, "v-change-user-rkey") {
					t.Fatalf("%s: rkey ran: %q", c, calls)
				}
			}
			if len(e.mm.sent) != 0 || len(e.files) != 0 || len(e.sleeps) != 1 || e.sleeps[0] != 2*time.Second {
				t.Fatalf("%s: sent=%d files=%d sleeps=%v", c.name, len(e.mm.sent), len(e.files), e.sleeps)
			}
		}
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("bodies differ: %q vs %q", b, bodies[0])
		}
	}
	if !strings.Contains(bodies[0], `"status":"sent"`) {
		t.Fatalf("body = %s", bodies[0])
	}
}

func TestResetRequestThrottledPerUser(t *testing.T) {
	e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery"})
	req := ResetRequest{User: "rec", Email: "rec@acme.test"}
	e.assertSent("/api/v1/auth/reset/request", req)
	e.now = acctRecT0.Add(14 * time.Minute)
	before := e.mark()
	e.assertSent("/api/v1/auth/reset/request", req)
	if len(e.mm.sent) != 1 {
		t.Fatalf("second request sent mail: %d", len(e.mm.sent))
	}
	for _, c := range e.calls(before) {
		if strings.HasPrefix(c, "v-change-user-rkey") {
			t.Fatalf("rkey ran on a throttled request: %s", c)
		}
	}
	e.now = acctRecT0.Add(16 * time.Minute)
	e.assertSent("/api/v1/auth/reset/request", req)
	if len(e.mm.sent) != 2 {
		t.Fatalf("a request after 15 minutes should send: %d", len(e.mm.sent))
	}
}

func TestResetRequestThrottledPerIP(t *testing.T) {
	e := acctRecHarness(t, acctRecDevUsers(11))
	for i := 1; i <= 11; i++ {
		e.assertSent("/api/v1/auth/reset/request", ResetRequest{User: fmt.Sprintf("u%d", i), Email: "rec@acme.test"})
	}
	if len(e.mm.sent) != 10 {
		t.Fatalf("sent %d, want 10 (the 11th is throttled)", len(e.mm.sent))
	}
	// An hour later the window is open again.
	e.now = acctRecT0.Add(61 * time.Minute)
	e.assertSent("/api/v1/auth/reset/request", ResetRequest{User: "u11", Email: "rec@acme.test"})
	if len(e.mm.sent) != 11 {
		t.Fatalf("sent %d, want 11 after the window", len(e.mm.sent))
	}
}

func TestResetPolicyOff(t *testing.T) {
	o := overrides{Dir: "../../testdata/fixtures/recovery", Config: map[string]string{"POLICY_SYSTEM_PASSWORD_RESET": "no"}}
	e := acctRecHarness(t, o)
	before := e.mark()
	for _, p := range []struct {
		path string
		body any
	}{
		{"/api/v1/auth/reset/request", ResetRequest{User: "rec", Email: "rec@acme.test"}},
		{"/api/v1/auth/reset/confirm", ResetConfirmRequest{User: "rec", Code: acctRecCode, Password: acctRecPW, PasswordConfirm: acctRecPW}},
	} {
		var er APIError
		if code := e.call("POST", p.path, p.body, &er); code != 404 || er.Error.Code != "disabled" {
			t.Fatalf("%s: %d %+v", p.path, code, er)
		}
	}
	acctRecEq(t, e.calls(before), nil)
	if len(e.mm.sent) != 0 {
		t.Fatal("mail sent")
	}
	// 2FA unlock is not gated by the policy.
	e.assertSent("/api/v1/auth/reset-2fa/request", UnlockRequest{User: "rec2fa", Password: acctRecPW})
	if len(e.mm.sent) != 1 {
		t.Fatalf("unlock mail not sent with the policy off: %d", len(e.mm.sent))
	}
}

func TestRecoveryNeedsCSRF(t *testing.T) {
	e := acctRecHarness(t, overrides{})
	e.token = ""
	for _, p := range []string{"/api/v1/auth/reset/request", "/api/v1/auth/reset/confirm",
		"/api/v1/auth/reset-2fa/request", "/api/v1/auth/reset-2fa/confirm"} {
		var er APIError
		if code := e.call("POST", p, map[string]string{}, &er); code != 400 || er.Error.Code != "csrf" {
			t.Fatalf("%s: %d %+v", p, code, er)
		}
	}
}

// ---------------------------------------------------------------- reset confirm

func acctConfirmReq(code string) ResetConfirmRequest {
	return ResetConfirmRequest{User: "rec", Code: code, Password: acctRecPW, PasswordConfirm: acctRecPW}
}

func TestResetConfirmSuccess(t *testing.T) {
	e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
	before := e.mark()
	e.now = acctRecT0.Add(10 * time.Minute)
	var r statusReply
	if code := e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(strings.ToLower(acctRecCode)), &r); code != 200 || r.Status != "ok" {
		t.Fatalf("confirm: %d %+v", code, r)
	}
	acctRecEq(t, e.calls(before), []string{"v-list-user rec json", "v-get-user-value rec RKEYEXP", "v-change-user-password rec <f>"})
	if len(e.files) != 1 || e.files[0] != acctRecPW || len(e.sleeps) != 0 {
		t.Fatalf("files=%v sleeps=%v", e.files, e.sleeps)
	}
	if b := e.bootstrap(); b.Authenticated {
		t.Fatal("a reset must not sign in")
	}
}

func TestResetConfirmExpired(t *testing.T) {
	e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
	e.now = acctRecT0.Add(16 * time.Minute)
	before := e.mark()
	var er APIError
	if code := e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode), &er); code != 400 || er.Error.Message != "Code has been expired" {
		t.Fatalf("%d %+v", code, er)
	}
	if len(e.sleeps) != 1 || e.sleeps[0] != 5*time.Second {
		t.Fatalf("expired sleeps = %v", e.sleeps)
	}
	for _, c := range e.calls(before) {
		if strings.HasPrefix(c, "v-change-user-password") {
			t.Fatal("password changed with an expired code")
		}
	}
}

func TestResetConfirmWrongCodes(t *testing.T) {
	for name, stored := range map[string]string{
		"wrong code":       acctRecHash("reset:", "SOMETHINGELSE"),
		"2fa unlock code":  acctRecHash("2fa:", acctRecCode),
		"no pending reset": "",
	} {
		e := acctRecHarness(t, acctRecConfirmFixtures(stored, acctRecT0))
		before := e.mark()
		var er APIError
		if code := e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode), &er); code != 400 || er.Error.Message != "Invalid username or code" {
			t.Fatalf("%s: %d %+v", name, code, er)
		}
		if len(e.sleeps) != 1 || e.sleeps[0] != 5*time.Second {
			t.Fatalf("%s: sleeps = %v", name, e.sleeps)
		}
		got := e.calls(before)
		want := "v-log-user-login rec " + acctRecIP + " failed " + e.token + " " + acctRecUA + " yes Failed to enter correct password reset code"
		if len(got) == 0 || got[len(got)-1] != want {
			t.Fatalf("%s: calls = %q, want last %q", name, got, want)
		}
		for _, c := range got {
			if strings.HasPrefix(c, "v-change-user-password") {
				t.Fatalf("%s: password changed", name)
			}
		}
	}
}

func TestResetConfirmUnknownUser(t *testing.T) {
	e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
	var er APIError
	req := acctConfirmReq(acctRecCode)
	req.User = "ghost"
	if code := e.call("POST", "/api/v1/auth/reset/confirm", req, &er); code != 400 || er.Error.Message != "Invalid username or code" {
		t.Fatalf("%d %+v", code, er)
	}
	if len(e.sleeps) != 1 || e.sleeps[0] != 5*time.Second {
		t.Fatalf("sleeps = %v", e.sleeps)
	}
}

func TestResetConfirmValidation(t *testing.T) {
	e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
	before := e.mark()
	req := acctConfirmReq(acctRecCode)
	req.PasswordConfirm = "different1A"
	var er APIError
	if code := e.call("POST", "/api/v1/auth/reset/confirm", req, &er); code != 400 || er.Error.Message != "Passwords do not match" {
		t.Fatalf("mismatch: %d %+v", code, er)
	}
	req = acctConfirmReq(acctRecCode)
	req.Password, req.PasswordConfirm = "weak", "weak"
	er = APIError{}
	if code := e.call("POST", "/api/v1/auth/reset/confirm", req, &er); code != 400 || er.Error.Message != "Password does not match the minimum requirements." {
		t.Fatalf("weak: %d %+v", code, er)
	}
	acctRecEq(t, e.calls(before), nil)
	if len(e.sleeps) != 0 {
		t.Fatalf("sleeps = %v", e.sleeps)
	}
}

func TestResetConfirmCommandFails(t *testing.T) {
	o := acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0)
	o.Fixtures["v-change-user-password"] = []fixtureEntry{{Args: []string{"*", "*"}, Stdout: "Error: boom\n", Exit: 2}}
	e := acctRecHarness(t, o)
	var er APIError
	if code := e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode), &er); code != 500 || er.Error.Message != "An internal error occurred" {
		t.Fatalf("%d %+v", code, er)
	}
}

func TestResetConfirmThrottledPerIP(t *testing.T) {
	// The 21st is refused even with the right code, and runs nothing.
	e2 := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
	for i := 0; i < 20; i++ {
		e2.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq("WRONG"), nil)
	}
	before := e2.mark()
	var er APIError
	if code := e2.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode), &er); code != 400 || er.Error.Message != "Invalid username or code" {
		t.Fatalf("%d %+v", code, er)
	}
	for _, c := range e2.calls(before) {
		if strings.HasPrefix(c, "v-change-user-password") {
			t.Fatal("password changed while throttled")
		}
	}
}

// ---------------------------------------------------------------- unlock

func TestUnlockRequest(t *testing.T) {
	t.Run("wrong password", func(t *testing.T) {
		e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery"})
		before := e.mark()
		e.assertSent("/api/v1/auth/reset-2fa/request", UnlockRequest{User: "ghost", Password: "nope"})
		got := e.calls(before)
		if len(e.mm.sent) != 0 || len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] != 2*time.Second {
			t.Fatalf("sent=%d sleeps=%v", len(e.mm.sent), e.sleeps)
		}
		want := "v-log-user-login ghost " + acctRecIP + " failed " + e.token + " " + acctRecUA
		found := false
		for _, c := range got {
			found = found || c == want
			if strings.HasPrefix(c, "v-change-user-rkey") {
				t.Fatal("rkey ran")
			}
		}
		if !found {
			t.Fatalf("failed login not logged: %q", got)
		}
	})
	t.Run("2fa off", func(t *testing.T) {
		e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery"})
		before := e.mark()
		e.assertSent("/api/v1/auth/reset-2fa/request", UnlockRequest{User: "rec", Password: acctRecPW})
		for _, c := range e.calls(before) {
			if strings.HasPrefix(c, "v-change-user-rkey") {
				t.Fatal("rkey ran")
			}
		}
		if len(e.mm.sent) != 0 {
			t.Fatal("mail sent for an account without 2FA")
		}
	})
	t.Run("suspended", func(t *testing.T) {
		e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery"})
		e.assertSent("/api/v1/auth/reset-2fa/request", UnlockRequest{User: "recsusp", Password: acctRecPW})
		if len(e.mm.sent) != 0 {
			t.Fatal("mail sent for a suspended account")
		}
	})
	t.Run("success", func(t *testing.T) {
		e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery"})
		before := e.mark()
		_, raw := rawCall(e.harness, "POST", "/api/v1/auth/reset-2fa/request", "", UnlockRequest{User: "rec2fa", Password: acctRecPW})
		acctRecEq(t, e.calls(before), []string{
			"v-check-user-password rec2fa <f> " + acctRecIP + " yes", "v-check-user-hash rec2fa <f> " + acctRecIP,
			"v-list-user rec2fa json", "v-change-user-rkey rec2fa <f>"})
		if len(e.mm.sent) != 1 || len(e.files) != 1 {
			t.Fatalf("sent=%d files=%d", len(e.mm.sent), len(e.files))
		}
		m := regexp.MustCompile(`[A-Z2-7]{26}`).FindString(e.mm.sent[0].Body)
		if m == "" || e.files[0] != acctRecHash("2fa:", m) || e.mm.sent[0].To != "rec2fa@acme.test" {
			t.Fatalf("hash %q, email %+v", e.files[0], e.mm.sent[0])
		}
		if strings.Contains(raw, m) || strings.Contains(e.logs.String(), m) {
			t.Fatal("the code leaked into the reply or the log")
		}
		// A second request in 15 minutes sends nothing.
		e.assertSent("/api/v1/auth/reset-2fa/request", UnlockRequest{User: "rec2fa", Password: acctRecPW})
		if len(e.mm.sent) != 1 {
			t.Fatal("second unlock request sent mail")
		}
	})
}

func acctUnlockFixtures(rkey string, issued time.Time) overrides {
	o := acctRecConfirmFixtures(rkey, issued)
	o.Fixtures["v-list-user"][0] = acctRecUser("rec", rkey, "JBSWY3DPEHPK3PXP", "no")
	return o
}

func TestUnlockConfirm(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		e := acctRecHarness(t, acctUnlockFixtures(acctRecHash("2fa:", acctRecCode), acctRecT0))
		e.now = acctRecT0.Add(5 * time.Minute)
		before := e.mark()
		var r statusReply
		if code := e.call("POST", "/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}, &r); code != 200 || r.Status != "ok" {
			t.Fatalf("%d %+v", code, r)
		}
		acctRecEq(t, e.calls(before), []string{"v-list-user rec json", "v-get-user-value rec RKEYEXP", "v-delete-user-2fa rec", "v-change-user-rkey rec"})
	})
	t.Run("reset code refused", func(t *testing.T) {
		e := acctRecHarness(t, acctUnlockFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
		before := e.mark()
		var er APIError
		if code := e.call("POST", "/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}, &er); code != 400 || er.Error.Message != "Invalid username or code" {
			t.Fatalf("%d %+v", code, er)
		}
		got := e.calls(before)
		want := "v-log-user-login rec " + acctRecIP + " failed " + e.token + " " + acctRecUA + " yes Failed to enter correct 2FA unlock code"
		if got[len(got)-1] != want || len(e.sleeps) != 1 || e.sleeps[0] != 5*time.Second {
			t.Fatalf("calls %q sleeps %v", got, e.sleeps)
		}
		for _, c := range got {
			if strings.HasPrefix(c, "v-delete-user-2fa") {
				t.Fatal("2FA deleted")
			}
		}
	})
	t.Run("expired", func(t *testing.T) {
		e := acctRecHarness(t, acctUnlockFixtures(acctRecHash("2fa:", acctRecCode), acctRecT0))
		e.now = acctRecT0.Add(16 * time.Minute)
		var er APIError
		if code := e.call("POST", "/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}, &er); code != 400 || er.Error.Message != "Code has been expired" {
			t.Fatalf("%d %+v", code, er)
		}
		if len(e.sleeps) != 1 || e.sleeps[0] != 5*time.Second {
			t.Fatalf("sleeps = %v", e.sleeps)
		}
	})
	t.Run("delete fails", func(t *testing.T) {
		o := acctUnlockFixtures(acctRecHash("2fa:", acctRecCode), acctRecT0)
		o.Fixtures["v-delete-user-2fa"] = []fixtureEntry{{Args: []string{"*"}, Stdout: "Error: x\n", Exit: 2}}
		e := acctRecHarness(t, o)
		before := e.mark()
		var er APIError
		if code := e.call("POST", "/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}, &er); code != 500 || er.Error.Message != "An internal error occurred" {
			t.Fatalf("%d %+v", code, er)
		}
		for _, c := range e.calls(before) {
			if strings.HasPrefix(c, "v-change-user-rkey") {
				t.Fatal("rkey rotated after a failed delete")
			}
		}
	})
}

// acctRotRunner rotates a user's RKEY once v-change-user-password or v-change-user-rkey
// succeeds, as Hestia does.
type acctRotRunner struct {
	runner.Runner
	rotated map[string]bool
}

func (r *acctRotRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	res, err := r.Runner.Run(ctx, cmd, args...)
	if err != nil {
		return res, err
	}
	switch {
	case (cmd == "v-change-user-password" || cmd == "v-change-user-rkey") && res.Exit == 0:
		r.rotated[args[0]] = true
	case cmd == "v-list-user" && res.Exit == 0 && r.rotated[args[0]]:
		res.Stdout = []byte(regexp.MustCompile(`"RKEY":"[0-9a-f]+"`).ReplaceAllString(string(res.Stdout), `"RKEY":"rotated"`))
	}
	return res, err
}

func acctRotating(d *Deps) {
	w := &acctRotRunner{Runner: d.R, rotated: map[string]bool{}}
	d.R = w
	d.H = &hestia.Client{R: w}
}

func TestResetConfirmIsSingleUse(t *testing.T) {
	e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0), acctRotating)
	if code, r := e.post("/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode)); code != 200 || r.Status != "ok" {
		t.Fatalf("first: %d %+v", code, r)
	}
	var er APIError
	if code := e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode), &er); code != 400 || er.Error.Message != "Invalid username or code" {
		t.Fatalf("second: %d %+v", code, er)
	}
}

func TestUnlockConfirmIsSingleUse(t *testing.T) {
	e := acctRecHarness(t, acctUnlockFixtures(acctRecHash("2fa:", acctRecCode), acctRecT0), acctRotating)
	if code, r := e.post("/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}); code != 200 || r.Status != "ok" {
		t.Fatalf("first: %d %+v", code, r)
	}
	var er APIError
	if code := e.call("POST", "/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}, &er); code != 400 || er.Error.Message != "Invalid username or code" {
		t.Fatalf("second: %d %+v", code, er)
	}
}

func TestUnlockConfirmRotationFailureStillOK(t *testing.T) {
	o := acctUnlockFixtures(acctRecHash("2fa:", acctRecCode), acctRecT0)
	o.Fixtures["v-change-user-rkey"] = []fixtureEntry{{Args: []string{"*"}, Stdout: "Error: x\n", Exit: 2}}
	e := acctRecHarness(t, o)
	if code, r := e.post("/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: acctRecCode}); code != 200 || r.Status != "ok" {
		t.Fatalf("%d %+v", code, r)
	}
}

func TestResetRequestLinkPortAndName(t *testing.T) {
	for port, want := range map[string]string{"8083": "server1.acme-hosting.net:8083/reset/", "443": "server1.acme-hosting.net/reset/", "": "server1.acme-hosting.net/reset/"} {
		e := acctRecHarness(t, overrides{Dir: "../../testdata/fixtures/recovery", Config: map[string]string{"BACKEND_PORT": port}})
		e.assertSent("/api/v1/auth/reset/request", ResetRequest{User: "rec", Email: "rec@acme.test"})
		if len(e.mm.sent) != 1 || !strings.Contains(e.mm.sent[0].Body, "https://"+want) {
			t.Fatalf("port %q: %v", port, e.mm.sent)
		}
	}
	// A user without a display name is greeted by username.
	o := overrides{Dir: "../../testdata/fixtures/recovery", Fixtures: map[string][]fixtureEntry{"v-list-user": {
		{Args: []string{"rec", "json"}, Stdout: `{"rec":{"NAME":"","CONTACT":"rec@acme.test","LANGUAGE":"en","ROLE":"user","SUSPENDED":"no","TWOFA":"","RKEY":""}}`}}}}
	e := acctRecHarness(t, o)
	e.assertSent("/api/v1/auth/reset/request", ResetRequest{User: "rec", Email: "rec@acme.test"})
	if len(e.mm.sent) != 1 || !strings.HasPrefix(e.mm.sent[0].Body, "Hello rec,") {
		t.Fatalf("%v", e.mm.sent)
	}
}

func TestConfirmThrottleReasonIsDistinct(t *testing.T) {
	e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
	for i := 0; i < 20; i++ {
		e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq("WRONG"), nil)
	}
	before := e.mark()
	e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq(acctRecCode), nil)
	got := e.calls(before)
	if !strings.HasSuffix(got[len(got)-1], "yes Too many password reset attempts") {
		t.Fatalf("%q", got)
	}
}

func TestAcctLockUserSerialisesSameUser(t *testing.T) {
	unlock := acctLockUser("bob")
	got := make(chan struct{})
	go func() {
		defer acctLockUser("bob")()
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("a second lock for the same user was granted while the first was held")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the second lock was never granted after unlock")
	}
}

func TestConfirmReleasesLockBeforeDelay(t *testing.T) {
	cases := map[string]func(e *acctRecEnv){
		"reset wrong code": func(e *acctRecEnv) { e.call("POST", "/api/v1/auth/reset/confirm", acctConfirmReq("WRONG"), nil) },
		"unlock wrong code": func(e *acctRecEnv) {
			e.call("POST", "/api/v1/auth/reset-2fa/confirm", UnlockConfirmRequest{User: "rec", Code: "WRONG"}, nil)
		},
	}
	for name, do := range cases {
		e := acctRecHarness(t, acctRecConfirmFixtures(acctRecHash("reset:", acctRecCode), acctRecT0))
		free, slept := true, false
		e.onSleep = func() {
			slept = true
			mu := &acctRecoveryLocks[acctLockIndex("rec")]
			if free = mu.TryLock(); free {
				mu.Unlock()
			}
		}
		do(e)
		if !slept || !free {
			t.Fatalf("%s: slept=%v lock free during delay=%v", name, slept, free)
		}
	}
}

func TestRecoveryLogFailedStripsUserAgentQuotes(t *testing.T) {
	// v-log-user-login refuses quotes in the user agent, which would drop the line
	// fail2ban watches.
	e := acctRecHarness(t, acctRecConfirmFixtures("", acctRecT0))
	before := e.mark()
	r := httptest.NewRequest("POST", "/api/v1/auth/reset/confirm", nil)
	r.Header.Set("User-Agent", `Mozilla/5.0 "x" 'y'`)
	r.RemoteAddr = acctRecIP + ":1234"
	(&server{Deps: e.deps}).recoveryLogFailed(r, "rec", "Failed to enter correct password reset code")
	got := e.calls(before)
	want := "v-log-user-login rec " + acctRecIP + " failed  Mozilla/5.0 x y yes Failed to enter correct password reset code"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

// blockingMailer holds every Send until release is closed, like a slow SMTP server.
type blockingMailer struct {
	release chan struct{}
	sent    chan mailer.Message
}

func (b *blockingMailer) Send(_ context.Context, _ hestia.SysConfig, m mailer.Message) error {
	<-b.release
	b.sent <- m
	return nil
}

// The email goes out in the background after the code is stored, so the time an SMTP
// server takes never shows in the reply (which only waits out the 2 s pad).
func TestRecoveryMailDoesNotDelayReply(t *testing.T) {
	for _, tc := range []struct {
		path string
		body any
		o    overrides
	}{
		{"/api/v1/auth/reset/request", ResetRequest{User: "rec", Email: "rec@acme.test"}, overrides{Dir: "../../testdata/fixtures/recovery"}},
		{"/api/v1/auth/reset-2fa/request", UnlockRequest{User: "rec2fa", Password: acctRecPW}, overrides{Dir: "../../testdata/fixtures/recovery"}},
	} {
		bm := &blockingMailer{release: make(chan struct{}), sent: make(chan mailer.Message, 1)}
		e := acctRecHarness(t, tc.o, func(d *Deps) { d.Mailer = bm; d.Background = nil })
		replied := make(chan int, 1)
		go func() {
			code, _ := rawCall(e.harness, "POST", tc.path, "", tc.body)
			replied <- code
		}()
		select {
		case code := <-replied:
			if code != 200 {
				t.Fatalf("%s: status %d", tc.path, code)
			}
		case <-time.After(3 * time.Second):
			close(bm.release)
			t.Fatalf("%s: the reply waited for the mail server", tc.path)
		}
		if len(e.files) != 1 {
			t.Fatalf("%s: code not stored before the reply: %d files", tc.path, len(e.files))
		}
		close(bm.release)
		select {
		case m := <-bm.sent:
			if acctRecCodeRE.FindStringSubmatch(m.Body) == nil {
				t.Fatalf("%s: mail without a code: %q", tc.path, m.Body)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: mail never sent", tc.path)
		}
	}
}

// The per-IP throttles forget an address once its attempts age out, so the maps don't
// grow with every IP that ever asked; the email slots age out the same way.
func TestRecoveryThrottlesSweepOldKeys(t *testing.T) {
	var a accountsState
	for i := 0; i < 50; i++ {
		ip := fmt.Sprintf("198.51.100.%d", i)
		a.allowRequest(ip, acctRecT0)
		a.allowConfirm(ip, acctRecT0)
		a.takeEmailSlot("reset:u"+strconv.Itoa(i), acctRecT0)
	}
	later := acctRecT0.Add(2 * time.Hour)
	a.allowRequest("203.0.113.1", later)
	a.allowConfirm("203.0.113.1", later)
	a.takeEmailSlot("reset:x", later)
	if len(a.resetRequests) != 1 || len(a.resetConfirms) != 1 || len(a.resetEmails) != 1 {
		t.Fatalf("maps kept old keys: requests=%d confirms=%d emails=%d",
			len(a.resetRequests), len(a.resetConfirms), len(a.resetEmails))
	}
}

// One IPv6 client owns a whole /64, so its addresses share one limit.
func TestRecoveryThrottleKeysIPv6By64(t *testing.T) {
	var a accountsState
	for i := 1; i <= acctResetRequestLimit; i++ {
		if !a.allowRequest(fmt.Sprintf("2001:db8:1:2::%x", i), acctRecT0) {
			t.Fatalf("request %d refused", i)
		}
	}
	if a.allowRequest("2001:db8:1:2:ffff::1", acctRecT0) {
		t.Fatal("another address in the same /64 was allowed past the limit")
	}
	if !a.allowRequest("2001:db8:1:3::1", acctRecT0) {
		t.Fatal("a different /64 was refused")
	}
	if !a.allowRequest("192.0.2.1", acctRecT0) || !a.allowRequest("192.0.2.2", acctRecT0) {
		t.Fatal("IPv4 addresses must count separately")
	}
}
