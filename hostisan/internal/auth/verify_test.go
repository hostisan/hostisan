package auth

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestVerifyPasswordRight(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, nil)
	ok, err := s.VerifyPassword(context.Background(), in("bob", "s3cret", ""))
	if err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v; want true", ok, err)
	}
	if got := cmds(sp.Calls()); !reflect.DeepEqual(got, []string{"v-check-user-password", "v-check-user-hash"}) {
		t.Fatalf("commands = %v", got)
	}
	if l := loginCalls(sp.Calls()); len(l) != 0 {
		t.Fatalf("a right password was logged: %v", l)
	}
	if len(*slept) != 0 {
		t.Fatalf("slept %v", *slept)
	}
	// The password went through a temp file, never argv, and the hash likewise.
	if strings.TrimSpace(sp.fileContents["v-check-user-password"]) != "s3cret" ||
		strings.TrimSpace(sp.fileContents["v-check-user-hash"]) != "the-hash" {
		t.Fatalf("temp files = %v", sp.fileContents)
	}
	for _, c := range sp.Calls() {
		for _, a := range c.Args {
			if a == "s3cret" || a == "the-hash" {
				t.Fatalf("%s got a secret in argv: %v", c.Cmd, c.Args)
			}
		}
	}
}

func TestVerifyPasswordWrong(t *testing.T) {
	s, sp, slept := setup(t, 0, 1, 1, 0, nil)
	ok, err := s.VerifyPassword(context.Background(), in("bob", "wrong", ""))
	if err != nil || ok {
		t.Fatalf("VerifyPassword = %v, %v; want false", ok, err)
	}
	want := [][]string{{"bob", ip, "failed", fp, ua}}
	if l := loginCalls(sp.Calls()); !reflect.DeepEqual(l, want) {
		t.Fatalf("login log = %v, want %v", l, want)
	}
	if !reflect.DeepEqual(*slept, []time.Duration{2 * time.Second}) {
		t.Fatalf("slept %v, want one 2s pause", *slept)
	}
}

func TestVerifyPasswordWrongStripsUserAgentQuotes(t *testing.T) {
	// v-log-user-login refuses a user agent with ' " | or a newline (format_no_quotes),
	// which would drop the line fail2ban watches.
	s, sp, _ := setup(t, 0, 1, 1, 0, nil)
	input := in("bob", "wrong", "")
	input.UserAgent = "Mozilla/5.0 \"x\" 'y' a|b\nc"
	if ok, err := s.VerifyPassword(context.Background(), input); err != nil || ok {
		t.Fatalf("VerifyPassword = %v, %v; want false", ok, err)
	}
	want := [][]string{{"bob", ip, "failed", fp, "Mozilla/5.0 x y ab c"}}
	if l := loginCalls(sp.Calls()); !reflect.DeepEqual(l, want) {
		t.Fatalf("login log = %v, want %v", l, want)
	}
}

func TestLogSafe(t *testing.T) {
	for in, want := range map[string]string{
		"plain":          "plain",
		"a'b\"c|d":       "abcd",
		"line1\nline2\r": "line1 line2",
	} {
		if got := LogSafe(in); got != want {
			t.Errorf("LogSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerifyPasswordInvalidUsername(t *testing.T) {
	s, sp, slept := setup(t, 0, 0, 0, 0, nil)
	ok, err := s.VerifyPassword(context.Background(), in("-h", "pw", ""))
	if err != nil || ok {
		t.Fatalf("VerifyPassword = %v, %v; want false", ok, err)
	}
	if n := len(sp.Calls()); n != 0 {
		t.Fatalf("%d commands ran for an invalid username", n)
	}
	if len(*slept) != 1 {
		t.Fatalf("slept %v", *slept)
	}
}

func TestDelayUsesSleep(t *testing.T) {
	s, _, slept := setup(t, 0, 0, 0, 0, nil)
	s.Delay(5 * time.Second)
	if !reflect.DeepEqual(*slept, []time.Duration{5 * time.Second}) {
		t.Fatalf("slept %v", *slept)
	}
}

// CheckPassword is the legacy API's password check (decision 20): the same two
// commands through temp files, but no pause and no auth.log line, as web/api/index.php.
func TestCheckPasswordNoLog(t *testing.T) {
	s, sp, slept := setup(t, 0, 1, 1, 0, nil)
	ok, err := s.CheckPassword(context.Background(), "bob", "wrong", ip)
	if err != nil || ok {
		t.Fatalf("CheckPassword = %v, %v; want false", ok, err)
	}
	if got := cmds(sp.Calls()); !reflect.DeepEqual(got, []string{"v-check-user-password", "v-check-user-hash"}) {
		t.Fatalf("commands = %v", got)
	}
	if len(*slept) != 0 {
		t.Fatalf("slept %v", *slept)
	}
	if strings.TrimSpace(sp.fileContents["v-check-user-password"]) != "wrong" {
		t.Fatalf("temp files = %v", sp.fileContents)
	}
	for _, c := range sp.Calls() {
		for _, a := range c.Args {
			if a == "wrong" {
				t.Fatalf("%s got the password in argv: %v", c.Cmd, c.Args)
			}
			if strings.HasPrefix(a, os.TempDir()+"/") {
				if _, err := os.Stat(a); !os.IsNotExist(err) {
					t.Fatalf("temp file %s left behind", a)
				}
			}
		}
	}

	s, sp, slept = setup(t, 0, 0, 0, 0, nil)
	ok, err = s.CheckPassword(context.Background(), "bob", "s3cret", ip)
	if err != nil || !ok {
		t.Fatalf("right password: %v, %v", ok, err)
	}
	if strings.TrimSpace(sp.fileContents["v-check-user-hash"]) != "the-hash" || len(*slept) != 0 {
		t.Fatalf("hash file = %q, slept %v", sp.fileContents["v-check-user-hash"], *slept)
	}

	s, sp, slept = setup(t, 0, 0, 0, 0, nil)
	if ok, err := s.CheckPassword(context.Background(), "-h", "pw", ip); err != nil || ok {
		t.Fatalf("invalid username: %v, %v", ok, err)
	}
	if len(sp.Calls()) != 0 || len(*slept) != 0 {
		t.Fatalf("invalid username ran %v, slept %v", sp.Calls(), *slept)
	}
}
