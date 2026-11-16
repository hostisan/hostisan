//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/hostisan/hostisan/internal/terminal"
)

// geteuid is a test seam.
var geteuid = os.Geteuid

// defaultTerminalSocket lives in a directory of its own, owned by root (Plan 6: the
// hestia-web-terminal unit's RuntimeDirectory=hestia-web-terminal, mode 0755), not
// one shared with the panel.
const defaultTerminalSocket = "unix:/run/hestia-web-terminal/terminal.sock"

// terminalCmd runs the web terminal (parity decisions 8–10): as root, it serves
// login shells over a unix socket only root and the panel's user can open.
func terminalCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("terminal", flag.ContinueOnError)
	fl.SetOutput(stderr)
	listenAddr := fl.String("listen", defaultTerminalSocket, "unix socket to listen on")
	allowUser := fl.String("allow-user", "hestiaweb", "the only user allowed to connect (the panel)")
	dev := fl.Bool("dev", false, "development: run as yourself, skip the peer check, spawn your own shell for any name")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	path, ok := strings.CutPrefix(*listenAddr, "unix:")
	if !ok || !filepath.IsAbs(path) {
		fmt.Fprintln(stderr, "hostisan terminal: --listen must be unix:/absolute/path")
		return 2
	}

	srv := &terminal.Server{Spawn: terminal.Spawn, PeerUID: terminal.PeerUID}
	sockGID := -1
	if *dev {
		if geteuid() == 0 {
			fmt.Fprintln(stderr, "hostisan terminal: --dev must not run as root")
			return 1
		}
		srv.Dev = true
		srv.LookupAccount = devAccount
	} else {
		if geteuid() != 0 {
			fmt.Fprintln(stderr, "hostisan terminal must run as root (use --dev for development)")
			return 1
		}
		u, err := user.Lookup(*allowUser)
		if err != nil {
			fmt.Fprintf(stderr, "hostisan terminal: %v\n", err)
			return 1
		}
		uid, err1 := strconv.Atoi(u.Uid)
		gid, err2 := strconv.Atoi(u.Gid)
		if err1 != nil || err2 != nil || uid == 0 {
			fmt.Fprintf(stderr, "hostisan terminal: unusable --allow-user %q\n", *allowUser)
			return 1
		}
		srv.AllowUID, sockGID = uid, gid
		srv.LookupAccount = terminal.LookupAccount
	}

	ln, err := listenTerminal(path, sockGID)
	if err != nil {
		fmt.Fprintf(stderr, "hostisan terminal: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	slog.Info("terminal: listening", "socket", path, "allow_user", *allowUser, "dev", *dev)
	fmt.Fprintf(stdout, "hostisan %s terminal listening on %s\n", version, *listenAddr)
	err = srv.Serve(ln)
	srv.CloseAll()
	if err != nil {
		fmt.Fprintf(stderr, "hostisan terminal: %v\n", err)
		return 1
	}
	return 0
}

// listenTerminal creates the socket 0660 (bound under umask 0117, never chmod-ed
// through a path) and, when gid >= 0, owned root:gid via Lchown. The directory must be
// a real directory (no symlink anywhere in its path), owned by this process's user
// and not writable by group or others, so nobody else can swap the socket.
func listenTerminal(path string, gid int) (net.Listener, error) {
	dir := filepath.Dir(path)
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(dir); err != nil || real != filepath.Clean(dir) {
		return nil, fmt.Errorf("socket directory %s must not be or contain a symlink", dir)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	if int(st.Uid) != geteuid() || fi.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("socket directory %s must be owned by uid %d and not group or other writable", dir, geteuid())
	}
	// Replace a socket left behind by a crash, but never another kind of file.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	old := syscall.Umask(0o117)
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if gid >= 0 {
		if err := os.Lchown(path, 0, gid); err != nil {
			ln.Close()
			return nil, err
		}
	}
	return ln, nil
}

// devAccount is the --dev account: the current user, whatever name was asked for.
func devAccount(string) (terminal.Account, error) {
	u, err := user.Current()
	if err != nil {
		return terminal.Account{}, err
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)
	shell := os.Getenv("SHELL")
	if !filepath.IsAbs(shell) {
		shell = "/bin/sh"
	}
	return terminal.Account{Name: u.Username, Home: u.HomeDir, Shell: shell, UID: uint32(uid), GID: uint32(gid)}, nil
}
