// Package runner runs Hestia's v-* commands. Every server action in Hostisan goes
// through a Runner: Sudo in production, Fixture in development and tests.
package runner

import (
	"context"
	"errors"
	"regexp"
	"time"
)

// Result is a finished command. A non-zero Exit is a normal result (Hestia uses exit
// codes for "not found", "exists", …); use Err to turn it into an error.
// Stderr is what the command wrote to stderr, at most MaxStderr bytes; Hestia's
// scripts report on stdout, but the tools they run (restic) may not.
type Result struct {
	Stdout []byte
	Stderr []byte
	Exit   int
}

// MaxStderr is the most stderr a Result keeps; the rest is read and dropped.
const MaxStderr = 64 << 10

type Runner interface {
	Run(ctx context.Context, cmd string, args ...string) (Result, error)
}

// AnyRunner runs any existing v-* command, allowlisted or not. Only the legacy /api/
// handler uses it (parity decisions 10 and 22); everything else goes through Run and
// the allowlist. RunAny refuses a name that isn't ^v-[A-Za-z0-9_-]+$ (ErrNotAllowed)
// or has no command behind it (ErrNoCommand) without running anything.
type AnyRunner interface {
	RunAny(ctx context.Context, cmd string, args ...string) (Result, error)
	// Exists reports whether RunAny would run cmd.
	Exists(cmd string) bool
}

// anyNameRe is the command names RunAny accepts.
var anyNameRe = regexp.MustCompile(`^v-[A-Za-z0-9_-]+$`)

var (
	// ErrNoCommand means RunAny found no command of that name; nothing was run.
	ErrNoCommand = errors.New("runner: no such command")
	// ErrNotAllowed means the command is not on the allowlist; nothing was run.
	ErrNotAllowed = errors.New("runner: command not allowed")
	// ErrTimeout means the command ran past its deadline and was killed.
	ErrTimeout = errors.New("runner: command timed out")
)

type timeoutKey struct{}

// WithTimeout overrides the runner's default timeout for calls made with ctx.
func WithTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, timeoutKey{}, d)
}

// Timeout is the timeout WithTimeout set on ctx, if any.
func Timeout(ctx context.Context) (time.Duration, bool) {
	d, ok := ctx.Value(timeoutKey{}).(time.Duration)
	return d, ok && d > 0
}

func timeoutFrom(ctx context.Context, fallback time.Duration) time.Duration {
	if d, ok := ctx.Value(timeoutKey{}).(time.Duration); ok && d > 0 {
		return d
	}
	return fallback
}
