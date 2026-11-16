package runner

import (
	"fmt"
	"strings"
)

// Exit codes from Hestia's func/main.sh.
const (
	ExitArgs        = 1
	ExitInvalid     = 2
	ExitNotExist    = 3
	ExitExists      = 4
	ExitSuspended   = 5
	ExitUnsuspended = 6
	ExitInUse       = 7
	ExitLimit       = 8
	ExitPassword    = 9
	ExitForbidden   = 10
	ExitDisabled    = 11
	ExitParsing     = 12
	ExitDisk        = 13
	ExitLA          = 14
	ExitConnect     = 15
	ExitFTP         = 16
	ExitDB          = 17
	ExitRRD         = 18
	ExitUpdate      = 19
	ExitRestart     = 20
	ExitBackup      = 21
)

// CommandError is a command that ran and exited non-zero.
type CommandError struct {
	Cmd    string
	Exit   int
	Output string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("%s exited %d: %s", e.Cmd, e.Exit, strings.TrimSpace(e.Output))
}

// Err returns nil for exit 0, else a *CommandError carrying the command's output.
func (r Result) Err(cmd string) error {
	if r.Exit == 0 {
		return nil
	}
	return &CommandError{Cmd: cmd, Exit: r.Exit, Output: string(r.Stdout)}
}
