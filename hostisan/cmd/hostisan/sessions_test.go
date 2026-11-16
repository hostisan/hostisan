package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionsFixture writes a sessions.json with one session for each user in users.
func sessionsFixture(t *testing.T, users ...string) string {
	t.Helper()
	dir := t.TempDir()
	var list []string
	for i, u := range users {
		list = append(list, `{"key":"`+strings.Repeat(string(rune('a'+i)), 64)+`","token":"0123456789abcdef0123456789abcdef","user":"`+
			u+`","role":"user","last_activity":"2026-11-04T11:59:00Z","ip":"203.0.113.7"}`)
	}
	body := `{"version":1,"sessions":[` + strings.Join(list, ",") + `]}`
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sessionUsers(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Sessions []struct{ User string } `json:"sessions"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range f.Sessions {
		out = append(out, s.User)
	}
	return out
}

// v-delete-sys-hostisan-sessions USER runs this with the panel stopped: it drops
// USER's sessions from the file and keeps everyone else's, mode 0600.
func TestSessionsForgetUser(t *testing.T) {
	dir := sessionsFixture(t, "bob", "carol", "bob")
	var out, errOut bytes.Buffer
	if code := run([]string{"sessions", "forget", "--state-dir", dir, "--user", "bob"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got := sessionUsers(t, dir); len(got) != 1 || got[0] != "carol" {
		t.Fatalf("left = %v, want [carol]", got)
	}
	if !strings.Contains(out.String(), "2 sessions of bob ended") {
		t.Fatalf("stdout = %q", out.String())
	}
	fi, _ := os.Stat(filepath.Join(dir, "sessions.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
}

// rc8 N3: one session read "1 sessions of rc8usr ended".
func TestSessionsForgetCountsInTheRightNumber(t *testing.T) {
	for _, tc := range []struct {
		users []string
		want  string
	}{
		{[]string{"bob", "carol"}, "1 session of bob ended\n"},
		{[]string{"carol"}, "0 sessions of bob ended\n"},
		{[]string{"bob", "bob", "bob"}, "3 sessions of bob ended\n"},
	} {
		dir := sessionsFixture(t, tc.users...)
		var out, errOut bytes.Buffer
		if code := run([]string{"sessions", "forget", "--state-dir", dir, "--user", "bob"}, &out, &errOut); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		if out.String() != tc.want {
			t.Errorf("%v: stdout = %q, want %q", tc.users, out.String(), tc.want)
		}
	}
}

// Without --user every session goes: the file is removed.
func TestSessionsForgetAll(t *testing.T) {
	dir := sessionsFixture(t, "bob", "carol")
	var out, errOut bytes.Buffer
	if code := run([]string{"sessions", "forget", "--state-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions.json")); !os.IsNotExist(err) {
		t.Fatalf("sessions.json still there: %v", err)
	}
	// nothing to forget is fine
	if code := run([]string{"sessions", "forget", "--state-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("second run: exit %d", code)
	}
	if code := run([]string{"sessions", "forget", "--state-dir", dir, "--user", "bob"}, &out, &errOut); code != 0 {
		t.Fatalf("user on a missing file: exit %d", code)
	}
}

// A file that can't be read is removed: the panel would start empty anyway, and
// keeping it could keep a session that should end.
func TestSessionsForgetUserBadFileRemovesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"sessions", "forget", "--state-dir", dir, "--user", "bob"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("bad file kept: %v", err)
	}
}

func TestSessionsForgetUsage(t *testing.T) {
	dir := sessionsFixture(t, "bob")
	for _, args := range [][]string{
		{"sessions"},
		{"sessions", "list"},
		{"sessions", "forget"},
		{"sessions", "forget", "--state-dir", dir, "--user", "../x"},
		{"sessions", "forget", "--state-dir", dir, "extra"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	if got := sessionUsers(t, dir); len(got) != 1 {
		t.Fatalf("a refused call changed the file: %v", got)
	}
}
