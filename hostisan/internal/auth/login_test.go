package auth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

const (
	ip = "203.0.113.9"
	ua = "Mozilla/5.0 Test"
	fp = "f1ngerpr1nt"

	goodCode = "111111" // the 2FA code the fixtures always accept
)

type entry struct {
	Args   []string `json:"args"`
	Stdout string   `json:"stdout"`
	Exit   int      `json:"exit"`
}

// spy wraps a Fixture and remembers what the temp files held when each command read them.
type spy struct {
	*runner.Fixture
	fileContents map[string]string // command -> content of its /tmp file argument
}

func (s *spy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	for _, a := range args {
		if strings.HasPrefix(a, os.TempDir()) {
			if b, err := os.ReadFile(a); err == nil {
				s.fileContents[cmd] = string(b)
			}
		}
	}
	return s.Fixture.Run(ctx, cmd, args...)
}

func userJSON(name string, fields map[string]string) string {
	u := map[string]string{"ROLE": "user", "SUSPENDED": "no", "TWOFA": "", "LOGIN_DISABLED": "no", "LOGIN_USE_IPLIST": "no", "LOGIN_ALLOW_IPS": "", "WEB_DOMAINS": "1"}
	for k, v := range fields {
		u[k] = v
	}
	b, _ := json.Marshal(map[string]map[string]string{name: u})
	return string(b)
}

// setup builds a service over fixtures. saltExit / passExit / hashExit / twofaExit are the
// exit codes for bob's commands; user holds bob's v-list-user fields.
func setup(t *testing.T, saltExit, passExit, hashExit, twofaExit int, user map[string]string) (*Service, *spy, *[]time.Duration) {
	t.Helper()
	salt := `{"bob":{"METHOD":"yescrypt","SALT":"x"}}`
	pass := "the-hash\n"
	if passExit != 0 {
		pass = "Error: password missmatch\n"
	}
	files := map[string][]entry{
		"v-get-user-salt":       {{Args: []string{"bob", ip, "json"}, Stdout: salt, Exit: saltExit}},
		"v-check-user-password": {{Args: []string{"bob", "*", ip, "yes"}, Stdout: pass, Exit: passExit}},
		"v-check-user-hash":     {{Args: []string{"bob", "*", ip}, Exit: hashExit}},
		"v-list-user":           {{Args: []string{"bob", "json"}, Stdout: userJSON("bob", user)}},
		"v-check-user-2fa":      {{Args: []string{"bob", goodCode}}, {Args: []string{"bob", "*"}, Exit: twofaExit}},
		"v-log-user-login":      {{Args: []string{"*", "*", "*", "*", "*"}}, {Args: []string{"*", "*", "*", "*", "*", "*", "*"}}},
	}
	dir := t.TempDir()
	for name, es := range files {
		b, _ := json.Marshal(es)
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := runner.LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	sp := &spy{Fixture: f, fileContents: map[string]string{}}
	var slept []time.Duration
	return &Service{R: sp, H: &hestia.Client{R: sp}, Sleep: func(d time.Duration) { slept = append(slept, d) }, TmpDir: os.TempDir()}, sp, &slept
}

func in(user, password, twofa string) LoginInput {
	return LoginInput{Username: user, Password: password, TwoFA: twofa, IP: ip, UserAgent: ua, Fingerprint: fp}
}

func cmds(calls []runner.Call) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Cmd
	}
	return out
}

func loginCalls(calls []runner.Call) [][]string {
	var out [][]string
	for _, c := range calls {
		if c.Cmd == "v-log-user-login" {
			out = append(out, c.Args)
		}
	}
	return out
}

func assertFailed(t *testing.T, res LoginResult, err error, msg string, slept []time.Duration) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != LoginFailed || res.Message != msg {
		t.Fatalf("got outcome %v message %q, want failed %q", res.Outcome, res.Message, msg)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("slept %v, want one 2s pause", slept)
	}
}

func TestLoginRejectsBadUsernameWithoutCommands(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, nil)
	res, err := s.Login(context.Background(), in("-h", "pw", ""))
	assertFailed(t, res, err, "Invalid username or password", *slept)
	if n := len(sp.Calls()); n != 0 {
		t.Fatalf("%d commands ran for an invalid username", n)
	}
}

func TestLoginSaltExit5Suspended(t *testing.T) {
	s, sp, slept := setup(t, 5, 0, 0, 0, nil)
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	assertFailed(t, res, err, "Account has been suspended", *slept)
	if l := loginCalls(sp.Calls()); len(l) != 0 {
		t.Fatalf("unexpected login log: %v", l)
	}
}

func TestLoginSaltExit1Unsupported(t *testing.T) {
	s, _, slept := setup(t, 1, 0, 0, 0, nil)
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	assertFailed(t, res, err, "Unsupported hash method", *slept)
}

func TestLoginSaltExit3Invalid(t *testing.T) {
	s, sp, slept := setup(t, 3, 0, 0, 0, nil)
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	assertFailed(t, res, err, "Invalid username or password", *slept)
	if l := loginCalls(sp.Calls()); len(l) != 0 {
		t.Fatalf("unexpected login log: %v", l)
	}
}

func TestLoginHashFlow(t *testing.T) {
	s, sp, _ := setup(t, 0, 0, 0, 0, nil)
	if _, err := s.Login(context.Background(), in("bob", "s3cret", "")); err != nil {
		t.Fatal(err)
	}
	calls := sp.Calls()
	if got := cmds(calls)[:3]; !reflect.DeepEqual(got, []string{"v-get-user-salt", "v-check-user-password", "v-check-user-hash"}) {
		t.Fatalf("first calls = %v", got)
	}
	if sp.fileContents["v-check-user-password"] != "s3cret\n" {
		t.Fatalf("password file held %q", sp.fileContents["v-check-user-password"])
	}
	if sp.fileContents["v-check-user-hash"] != "the-hash\n" {
		t.Fatalf("hash file held %q", sp.fileContents["v-check-user-hash"])
	}
	for _, c := range calls[1:3] {
		path := c.Args[1]
		if !strings.HasPrefix(path, os.TempDir()) {
			t.Fatalf("%s got %q, want a temp file path", c.Cmd, path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temp file %s was not removed", path)
		}
	}
}

func TestLoginWrongPassword(t *testing.T) {
	s, sp, slept := setup(t, 0, 9, 9, 0, nil)
	res, err := s.Login(context.Background(), in("bob", "wrong", ""))
	assertFailed(t, res, err, "Invalid username or password", *slept)
	if got, ok := sp.fileContents["v-check-user-hash"]; !ok || got != "\n" {
		t.Fatalf("v-check-user-hash must still run with an empty hash (fail2ban log); file held %q, ran %v", got, ok)
	}
	want := [][]string{{"bob", ip, "failed", fp, ua}}
	if got := loginCalls(sp.Calls()); !reflect.DeepEqual(got, want) {
		t.Fatalf("login log = %v, want %v", got, want)
	}
}

func TestLoginDisabled(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, map[string]string{"LOGIN_DISABLED": "yes"})
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	assertFailed(t, res, err, "Invalid username or password", *slept)
	want := [][]string{{"bob", ip, "failed", fp, ua, "yes", "Login disabled for this user"}}
	if got := loginCalls(sp.Calls()); !reflect.DeepEqual(got, want) {
		t.Fatalf("login log = %v, want %v", got, want)
	}
}

func TestLoginIPNotAllowed(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, map[string]string{"LOGIN_USE_IPLIST": "yes", "LOGIN_ALLOW_IPS": "10.0.0.1, 10.0.0.2"})
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	assertFailed(t, res, err, "Invalid username or password", *slept)
	want := [][]string{{"bob", ip, "failed", fp, ua, "yes", "IP address not in allowed list"}}
	if got := loginCalls(sp.Calls()); !reflect.DeepEqual(got, want) {
		t.Fatalf("login log = %v, want %v", got, want)
	}
}

func TestLoginIPAllowed(t *testing.T) {
	s, _, _ := setup(t, 0, 0, 0, 0, map[string]string{"LOGIN_USE_IPLIST": "yes", "LOGIN_ALLOW_IPS": "10.0.0.1," + ip})
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	if err != nil || res.Outcome != LoginOK {
		t.Fatalf("got %v, %v; want ok", res.Outcome, err)
	}
}

func TestLoginTwoFARequired(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, map[string]string{"TWOFA": "SECRET"})
	res, err := s.Login(context.Background(), in("bob", "pw", ""))
	if err != nil || res.Outcome != LoginNeedTwoFA {
		t.Fatalf("got %v, %v; want need-2FA", res.Outcome, err)
	}
	if len(*slept) != 0 || len(loginCalls(sp.Calls())) != 0 {
		t.Fatalf("needing 2FA must not pause or log: slept %v, log %v", *slept, loginCalls(sp.Calls()))
	}
}

// twoFAAttempt sends one wrong 2FA code and reports whether it was logged for fail2ban
// and whether v-check-user-2fa ran.
func twoFAAttempt(t *testing.T, s *Service, sp *spy, code string) (logged, checked bool) {
	t.Helper()
	before := len(sp.Calls())
	if _, err := s.Login(context.Background(), in("bob", "pw", code)); err != nil {
		t.Fatal(err)
	}
	for _, c := range sp.Calls()[before:] {
		logged = logged || c.Cmd == "v-log-user-login"
		checked = checked || c.Cmd == "v-check-user-2fa"
	}
	return logged, checked
}

func TestLoginTwoFABad(t *testing.T) {
	s, _, slept := setup(t, 0, 0, 0, 9, map[string]string{"TWOFA": "SECRET"})
	res, err := s.Login(context.Background(), in("bob", "pw", "123456"))
	assertFailed(t, res, err, "Invalid or missing 2FA token", *slept)
}

// PHP counted wrong codes per browser session, so an attacker who never kept the
// session cookie was never counted. Hostisan counts them per user on the server.
func TestLoginTwoFAFailuresCountPerUser(t *testing.T) {
	s, sp, _ := setup(t, 0, 0, 0, 9, map[string]string{"TWOFA": "SECRET"})
	for i := 1; i <= 3; i++ {
		if logged, _ := twoFAAttempt(t, s, sp, "123456"); logged {
			t.Fatalf("wrong code %d was logged; the first 3 are allowed as typos", i)
		}
	}
	logged, _ := twoFAAttempt(t, s, sp, "123456")
	if !logged {
		t.Fatal("wrong code 4 was not logged for fail2ban")
	}
	want := []string{"bob", ip, "failed", fp, ua, "yes", "Invalid or missing 2FA token"}
	if got := loginCalls(sp.Calls()); !reflect.DeepEqual(got[len(got)-1], want) {
		t.Fatalf("login log = %v, want %v", got[len(got)-1], want)
	}
}

func TestLoginTwoFALockout(t *testing.T) {
	s, sp, _ := setup(t, 0, 0, 0, 9, map[string]string{"TWOFA": "SECRET"})
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	for range 5 {
		twoFAAttempt(t, s, sp, "123456")
	}
	logged, checked := twoFAAttempt(t, s, sp, goodCode)
	if checked || !logged {
		t.Fatalf("after 5 wrong codes: checked %v, logged %v; want refused unchecked and logged", checked, logged)
	}
	if res, _ := s.Login(context.Background(), in("bob", "pw", goodCode)); res.Outcome != LoginFailed {
		t.Fatal("the right code was accepted during the lockout")
	}
	now = now.Add(TwoFALockout + time.Second)
	if res, _ := s.Login(context.Background(), in("bob", "pw", goodCode)); res.Outcome != LoginOK {
		t.Fatalf("right code after the lockout: %+v", res)
	}
}

func TestLoginTwoFASuccessResetsCount(t *testing.T) {
	s, sp, _ := setup(t, 0, 0, 0, 9, map[string]string{"TWOFA": "SECRET"})
	for range 3 {
		twoFAAttempt(t, s, sp, "123456")
	}
	if res, _ := s.Login(context.Background(), in("bob", "pw", goodCode)); res.Outcome != LoginOK {
		t.Fatalf("right code: %+v", res)
	}
	if logged, _ := twoFAAttempt(t, s, sp, "123456"); logged {
		t.Fatal("a wrong code after a successful login counted the earlier failures")
	}
}

func TestLoginTwoFATooLongNotChecked(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, map[string]string{"TWOFA": "SECRET"})
	res, err := s.Login(context.Background(), in("bob", "pw", "1234567890"))
	assertFailed(t, res, err, "Invalid or missing 2FA token", *slept)
	for _, c := range sp.Calls() {
		if c.Cmd == "v-check-user-2fa" {
			t.Fatal("v-check-user-2fa ran for a 10-character code")
		}
	}
}

func TestLoginSuccess(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, map[string]string{"TWOFA": "SECRET"})
	res, err := s.Login(context.Background(), in("bob", "pw", "123456"))
	if err != nil || res.Outcome != LoginOK || res.Username != "bob" || res.User.Role() != "user" {
		t.Fatalf("got %+v, %v", res, err)
	}
	want := [][]string{{"bob", ip, "success", fp, ua}}
	if got := loginCalls(sp.Calls()); !reflect.DeepEqual(got, want) {
		t.Fatalf("login log = %v, want %v", got, want)
	}
	if len(*slept) != 0 {
		t.Fatalf("successful login paused %v", *slept)
	}
}

// A quote in the user agent makes v-log-user-login exit before it writes the failed
// line fail2ban reads, so every login log call passes it through LogSafe.
func TestLoginLogsSafeUserAgent(t *testing.T) {
	s, sp, _ := setup(t, 0, 9, 9, 0, nil)
	li := in("bob", "wrong", "")
	li.UserAgent = `Evil "agent" | x`
	if _, err := s.Login(context.Background(), li); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"bob", ip, "failed", fp, "Evil agent  x"}}
	if got := loginCalls(sp.Calls()); !reflect.DeepEqual(got, want) {
		t.Fatalf("login log = %q, want %q", got, want)
	}
}
