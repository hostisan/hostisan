package terminal

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const testPasswd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
bob:x:1001:1001::/home/bob:/bin/bash
ghost:x:1002:1002::/home/ghost:/bin/bash
nolog:x:1003:1003::/home/nolog:/usr/sbin/nologin
wheel:x:1004:0::/home/wheel:/bin/bash
rel:x:1005:1005::/home/rel:bash
broken:x:1006
`

// failReader fails the test if anything reads it.
type failReader struct{ t *testing.T }

func (f failReader) Read([]byte) (int, error) {
	f.t.Error("passwd was read")
	return 0, errors.New("read")
}

func hestiaUsers(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, n, "user.conf"), []byte("NAME=''\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckAccount(t *testing.T) {
	users := hestiaUsers(t, "root", "bob", "nolog", "wheel", "rel", "broken")
	if err := os.MkdirAll(filepath.Join(users, "ghost", "user.conf"), 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}

	a, err := CheckAccount("bob", strings.NewReader(testPasswd), users)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	if want := (Account{Name: "bob", Home: "/home/bob", Shell: "/bin/bash", UID: 1001, GID: 1001}); a.Name != want.Name || a.Home != want.Home ||
		a.Shell != want.Shell || a.UID != want.UID || a.GID != want.GID || a.Groups != nil {
		t.Fatalf("bob = %+v", a)
	}

	cases := []struct {
		name string
		want error
	}{
		{"root", ErrNotAllowed},   // uid 0
		{"wheel", ErrNotAllowed},  // gid 0
		{"ghost", ErrNotAllowed},  // user.conf is not a regular file
		{"carol", ErrNotAllowed},  // not in passwd
		{"daemon", ErrNotAllowed}, // no user.conf (checked before the shell)
		{"broken", ErrNotAllowed}, // malformed passwd line
		{"nolog", ErrNoShell},
		{"rel", ErrNoShell}, // not an absolute shell path
	}
	for _, c := range cases {
		if _, err := CheckAccount(c.name, strings.NewReader(testPasswd), users); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := CheckAccount("bob", strings.NewReader(testPasswd), t.TempDir()); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("bob without user.conf: %v", err)
	}
	for _, bad := range []string{"bad name!", "", "../bob", "bob/..", "-bob", "bob\n"} {
		if _, err := CheckAccount(bad, failReader{t}, users); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if ErrNotAllowed.Error() != "You are not allowed to access this server." || ErrNoShell.Error() != "You have no shell access." {
		t.Fatal("refusal texts changed")
	}
}

func TestGroupsOfMatchesOSUser(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	ids, err := u.GroupIds()
	if err != nil {
		t.Skip(err)
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)
	got, err := groupsOf(Account{Name: u.Username, UID: uint32(uid), GID: uint32(gid)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0] != uint32(gid) {
		t.Fatalf("groups %v: primary gid %d not first", got, gid)
	}
	want := map[uint32]bool{}
	for _, id := range ids {
		g, _ := strconv.ParseUint(id, 10, 32)
		if g != 0 {
			want[uint32(g)] = true
		}
	}
	seen := map[uint32]bool{}
	for _, g := range got {
		if !want[g] || seen[g] || g == 0 {
			t.Fatalf("groups %v, os/user %v", got, ids)
		}
		seen[g] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("groups %v, os/user %v", got, ids)
	}
}
