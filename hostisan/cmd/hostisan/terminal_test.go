//go:build linux

package main

import (
	"bytes"
	"net"
	"os"
	"strings"
	"testing"
)

func withEUID(t *testing.T, uid int) {
	t.Helper()
	old := geteuid
	geteuid = func() int { return uid }
	t.Cleanup(func() { geteuid = old })
}

func TestTerminalCommandFlags(t *testing.T) {
	withEUID(t, 1000)
	var out, errOut bytes.Buffer
	sock := t.TempDir() + "/t.sock"
	if code := run([]string{"terminal", "--listen", "unix:" + sock}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "hostisan terminal must run as root") {
		t.Fatalf("non-root: exit %d, stderr %q", code, errOut.String())
	}
	if _, err := os.Lstat(sock); err == nil {
		t.Fatal("a refused start created the socket")
	}

	withEUID(t, 0)
	errOut.Reset()
	if code := run([]string{"terminal", "--dev", "--listen", "unix:" + sock}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "--dev") {
		t.Fatalf("--dev as root: exit %d, stderr %q", code, errOut.String())
	}

	errOut.Reset()
	if code := run([]string{"terminal", "--nope"}, &out, &errOut); code != 2 {
		t.Fatalf("bad flag: exit %d", code)
	}
	// Only unix sockets: the terminal must never listen on TCP.
	withEUID(t, 1000)
	errOut.Reset()
	if code := run([]string{"terminal", "--dev", "--listen", "127.0.0.1:0"}, &out, &errOut); code != 2 ||
		!strings.Contains(errOut.String(), "unix:") {
		t.Fatalf("tcp listen: exit %d, stderr %q", code, errOut.String())
	}
}

func TestTerminalUsageListed(t *testing.T) {
	var out, errOut bytes.Buffer
	run(nil, &out, &errOut)
	if !strings.Contains(errOut.String(), "terminal") {
		t.Fatalf("usage %q", errOut.String())
	}
}

func TestDevAccountIsCurrentUser(t *testing.T) {
	a, err := devAccount("anyone")
	if err != nil {
		t.Fatal(err)
	}
	if int(a.UID) != os.Getuid() || a.Name == "anyone" && os.Getenv("USER") != "anyone" {
		t.Fatalf("dev account %+v", a)
	}
	if a.Shell == "" || a.Home == "" {
		t.Fatalf("dev account %+v", a)
	}
}

func TestListenTerminalSocket(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sub/t.sock"
	ln, err := listenTerminal(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o660 {
		t.Fatalf("socket %v %v", fi.Mode(), err)
	}
	ln.Close()
	// A stale socket is replaced.
	l2, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l2.(*net.UnixListener).SetUnlinkOnClose(false)
	l2.Close()
	ln, err = listenTerminal(path, -1)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	ln.Close()
	// Any other file is left alone.
	other := dir + "/file"
	os.WriteFile(other, []byte("keep"), 0o600)
	if _, err := listenTerminal(other, -1); err == nil {
		t.Fatal("listened over a regular file")
	}
	if b, _ := os.ReadFile(other); string(b) != "keep" {
		t.Fatal("regular file changed")
	}
}

func TestListenTerminalRefusesUnsafeParent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := listenTerminal(dir+"/t.sock", -1); err == nil {
		t.Fatal("listened in a group-writable directory")
	}
	os.Chmod(dir, 0o757)
	if _, err := listenTerminal(dir+"/t.sock", -1); err == nil {
		t.Fatal("listened in an other-writable directory")
	}
	// A symlinked parent is refused, even to a safe directory.
	safe := t.TempDir()
	link := t.TempDir() + "/link"
	if err := os.Symlink(safe, link); err != nil {
		t.Fatal(err)
	}
	if _, err := listenTerminal(link+"/t.sock", -1); err == nil {
		t.Fatal("listened through a symlinked directory")
	}
	// A symlink where the socket goes is never followed or removed.
	target := safe + "/target"
	os.WriteFile(target, []byte("keep"), 0o600)
	os.Symlink(target, safe+"/t.sock")
	if _, err := listenTerminal(safe+"/t.sock", -1); err == nil {
		t.Fatal("listened over a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatal("symlink target changed")
	}
}

func TestListenTerminalRefusesForeignParentAsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	dir := t.TempDir()
	if err := os.Chown(dir, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if _, err := listenTerminal(dir+"/t.sock", -1); err == nil {
		t.Fatal("listened in a directory root does not own")
	}
}

func TestTerminalDefaultSocket(t *testing.T) {
	if defaultTerminalSocket != "unix:/run/hestia-web-terminal/terminal.sock" {
		t.Fatalf("default %q", defaultTerminalSocket)
	}
}
