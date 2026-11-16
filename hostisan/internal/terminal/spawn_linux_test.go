package terminal

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"testing"
	"time"
)

var wantEnv = func(a Account) []string {
	return []string{
		"HOME=" + a.Home,
		"USER=" + a.Name,
		"LOGNAME=" + a.Name,
		"SHELL=" + a.Shell,
		"PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/games:/usr/games",
		"TERM=xterm-256color",
		"LANG=C.UTF-8",
		"HESTIA=/usr/local/hestia",
	}
}

func TestSpawnCredentials(t *testing.T) {
	a := Account{Name: "bob", Home: "/home/bob", Shell: "/bin/bash", UID: 1001, GID: 1001, Groups: []uint32{1001, 33}}
	cmd := loginCmd(a, true)
	if cmd.Path != "/bin/bash" || len(cmd.Args) != 1 || cmd.Args[0] != "-bash" {
		t.Fatalf("path %q args %q", cmd.Path, cmd.Args)
	}
	if cmd.Dir != "/home/bob" {
		t.Fatalf("dir %q", cmd.Dir)
	}
	if strings.Join(cmd.Env, "\n") != strings.Join(wantEnv(a), "\n") {
		t.Fatalf("env %q", cmd.Env)
	}
	p := cmd.SysProcAttr
	if p == nil || !p.Setsid || !p.Setctty || p.Ctty != 0 {
		t.Fatalf("attrs %+v", p)
	}
	c := p.Credential
	if c == nil || c.Uid != 1001 || c.Gid != 1001 || c.NoSetGroups || len(c.Groups) != 2 || c.Groups[0] != 1001 || c.Groups[1] != 33 {
		t.Fatalf("credential %+v", c)
	}
	// An account with no supplementary groups still drops root's: setgroups([]).
	c = loginCmd(Account{Name: "x", Home: "/", Shell: "/bin/sh", UID: 5, GID: 5}, true).SysProcAttr.Credential
	if c == nil || c.Groups == nil || len(c.Groups) != 0 || c.NoSetGroups {
		t.Fatalf("empty groups credential %+v", c)
	}
	// Not root: no credential change is possible, the shell runs as the caller.
	if loginCmd(a, false).SysProcAttr.Credential != nil {
		t.Fatal("credential set without root")
	}
}

func TestSpawnRefusesOtherUserWithoutRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root")
	}
	_, err := Spawn(Account{Name: "x", Home: "/", Shell: "/bin/sh", UID: uint32(os.Geteuid() + 1), GID: 1}, 80, 24)
	if err == nil {
		t.Fatal("spawned another user's shell without root")
	}
}

func TestSpawnLoginShell(t *testing.T) {
	if os.Getenv("HOSTISAN_PTY_TEST") != "1" {
		t.Skip("set HOSTISAN_PTY_TEST=1")
	}
	// Canaries: none of these may reach the shell.
	for _, k := range []string{"HOSTISAN_LEAK", "SSH_AUTH_SOCK", "LD_LIBRARY_PATH", "MAIL", "XAUTHORITY"} {
		t.Setenv(k, "hostisan-canary")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	a := Account{Name: u.Username, Home: t.TempDir(), Shell: "/bin/sh", UID: uint32(uid), GID: uint32(gid)}
	s, err := Spawn(a, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	io.WriteString(s, `echo "A0=$0"; echo "AP=$PATH"; echo "AL=$LOGNAME"; echo "AH=$HOME"; echo "AS=$(stty size)"; echo "AD=$(pwd)"; env | sed 's/^/E:/'; exit 3`+"\n")
	var out bytes.Buffer
	done := make(chan struct{})
	go func() { io.Copy(&out, s); close(done) }()
	waited := make(chan error, 1)
	go func() { waited <- s.Wait() }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("shell did not exit")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	text := strings.ReplaceAll(out.String(), "\r", "")
	for _, want := range []string{
		"A0=-sh\n", "AP=" + LoginPath, // /etc/profile may append (snap adds /snap/bin)
		"\nAL=" + a.Name + "\n", "\nAH=" + a.Home + "\n", "\nAS=40 120\n", "\nAD=" + a.Home + "\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "hostisan-canary") {
		t.Errorf("the test environment leaked:\n%s", text)
	}
	// Every variable we hand over arrives (PATH checked above; profile may extend it).
	for _, kv := range wantEnv(a) {
		if !strings.HasPrefix(kv, "PATH=") && !strings.Contains(text, "\nE:"+kv+"\n") {
			t.Errorf("missing %s", kv)
		}
	}
}

func TestSpawnCloseKillsGroup(t *testing.T) {
	if os.Getenv("HOSTISAN_PTY_TEST") != "1" {
		t.Skip("set HOSTISAN_PTY_TEST=1")
	}
	u, _ := user.Current()
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	s, err := Spawn(Account{Name: u.Username, Home: t.TempDir(), Shell: "/bin/sh", UID: uint32(uid), GID: uint32(gid)}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// A shell that ignores the hangup still dies: SIGKILL after the grace period.
	io.WriteString(s, "trap '' HUP; echo ready; sleep 100\n")
	buf := make([]byte, 4096)
	var seen strings.Builder
	for !strings.Contains(seen.String(), "ready\r\n") {
		n, err := s.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		seen.Write(buf[:n])
	}
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, err := s.Read(buf); err != nil {
				readErr <- err
				return
			}
		}
	}()
	start := time.Now()
	s.Close()
	select {
	case <-readErr:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
	waited := make(chan struct{})
	go func() { s.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(killGrace + 3*time.Second):
		t.Fatal("the shell outlived Close")
	}
	t.Logf("shell gone after %v", time.Since(start))
}

func TestSpawnRefusesRootGroups(t *testing.T) {
	me := uint32(os.Geteuid())
	for _, a := range []Account{
		{Name: "x", Home: "/", Shell: "/bin/sh", UID: me, GID: 0},
		{Name: "x", Home: "/", Shell: "/bin/sh", UID: me, GID: 5, Groups: []uint32{5, 0}},
		{Name: "x", Home: "/", Shell: "/bin/sh", UID: 0, GID: 5},
	} {
		if s, err := Spawn(a, 80, 24); err == nil {
			s.Close()
			t.Fatalf("spawned %+v", a)
		}
	}
}

// TestSpawnDropsPrivileges runs only as root: the shell must end up with exactly the
// account's ids and no capabilities.
func TestSpawnDropsPrivileges(t *testing.T) {
	if os.Getenv("HOSTISAN_PTY_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set HOSTISAN_PTY_TEST=1 and run as root")
	}
	a := Account{Name: "nobody", Home: "/", Shell: "/bin/sh", UID: 65534, GID: 65534, Groups: []uint32{65534, 100}}
	s, err := Spawn(a, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	io.WriteString(s, `echo "U=$(id -u)"; echo "G=$(id -g)"; echo "GS=$(id -G)"; grep CapEff /proc/self/status; exit`+"\n")
	var out bytes.Buffer
	done := make(chan struct{})
	go func() { io.Copy(&out, s); close(done) }()
	waited := make(chan struct{})
	go func() { s.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("shell did not exit")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	text := strings.ReplaceAll(out.String(), "\r", "")
	for _, want := range []string{"U=65534\n", "G=65534\n", "GS=65534 100\n", "CapEff:\t0000000000000000\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}
