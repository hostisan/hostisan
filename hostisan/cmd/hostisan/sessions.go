package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

const sessionsUsage = `usage: hostisan sessions forget --state-dir DIR [--user USER]

Ends signed-in panel sessions in DIR/sessions.json: all of them (the file is
removed), or USER's. The panel keeps sessions across restarts, so restarting it
signs nobody out. Run it only while hostisan.service is stopped: a running panel
rewrites the file on stop. v-delete-sys-hostisan-sessions all|USER stops the panel,
runs this and starts it again.
`

// sessionsCmd is `hostisan sessions forget`.
func sessionsCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "forget" {
		fmt.Fprint(stderr, sessionsUsage)
		return 2
	}
	fl := flag.NewFlagSet("sessions forget", flag.ContinueOnError)
	fl.SetOutput(stderr)
	stateDir := fl.String("state-dir", "", "the panel's state directory (/var/lib/hostisan)")
	user := fl.String("user", "", "end only this user's sessions")
	if err := fl.Parse(args[1:]); err != nil {
		return 2
	}
	if *stateDir == "" || fl.NArg() != 0 || *user != "" && !hestia.ValidUsername(*user) {
		fmt.Fprint(stderr, sessionsUsage)
		return 2
	}
	path := filepath.Join(*stateDir, "sessions.json")
	if *user == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "hostisan: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "all panel sessions ended")
		return 0
	}
	n, err := session.ForgetUser(path, *user)
	if err != nil {
		fmt.Fprintf(stderr, "hostisan: %v\n", err)
		return 1
	}
	noun := "sessions"
	if n == 1 {
		noun = "session"
	}
	fmt.Fprintf(stdout, "%d %s of %s ended\n", n, noun, *user)
	return 0
}
