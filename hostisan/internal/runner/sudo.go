package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Wrapper is the root-owned script in Bin that runs each command with a deadline:
// `v-hostisan-run <seconds> <cmd> <args...>`. The panel (hestiaweb) can't signal the
// root command it starts with sudo, so the wrapper stops it, children included.
const Wrapper = "v-hostisan-run"

const (
	// wrapperKillAfter is how long the wrapper waits after its SIGTERM before SIGKILL
	// (`timeout --kill-after=5`, and the same grace for the rest of the group).
	wrapperKillAfter = 5 * time.Second
	// backstopGrace is how much longer than the wrapper the runner's own deadline is.
	backstopGrace = 5 * time.Second
	// maxWrapperSeconds is the longest deadline the wrapper accepts.
	maxWrapperSeconds = 86400
	// outputWait is how long the runner waits for the pipes after the process exits.
	outputWait = time.Second
)

// wrapperSeconds is the wrapper's deadline for timeout: whole seconds, rounded up so
// it never stops a command early, at most maxWrapperSeconds.
func wrapperSeconds(timeout time.Duration) int64 {
	secs := int64((timeout + time.Second - 1) / time.Second)
	if secs > maxWrapperSeconds {
		slog.Warn("runner: timeout capped", "timeout", timeout, "cap_seconds", maxWrapperSeconds)
		secs = maxWrapperSeconds
	}
	return max(secs, 1)
}

func backstop(secs int64) time.Duration {
	return time.Duration(secs)*time.Second + wrapperKillAfter + backstopGrace
}

// MaxRunTime is the longest a Sudo command with this timeout can take before Run
// returns: the runner's backstop deadline plus its wait for output.
func MaxRunTime(timeout time.Duration) time.Duration {
	return backstop(wrapperSeconds(timeout)) + outputWait
}

// Sudo runs commands as `sudo <Bin>/v-hostisan-run <seconds> <cmd> <args...>` with an
// argument list, never through a shell, so arguments can't be interpreted as shell
// syntax.
type Sudo struct {
	Bin      string        // default /usr/local/hestia/bin
	SudoPath string        // default /usr/bin/sudo
	Timeout  time.Duration // default 60s; override per call with WithTimeout
}

func (s Sudo) Run(ctx context.Context, cmd string, args ...string) (Result, error) {
	if !Allowed(cmd) {
		return Result{}, fmt.Errorf("%w: %s", ErrNotAllowed, cmd)
	}
	return s.run(ctx, cmd, args)
}

// RunAny runs any v-* command that exists as a regular file in Bin (not a symlink or a
// directory), allowlisted or not: the legacy API's pass-through (AnyRunner).
func (s Sudo) RunAny(ctx context.Context, cmd string, args ...string) (Result, error) {
	if !anyNameRe.MatchString(cmd) || cmd == Wrapper {
		return Result{}, fmt.Errorf("%w: %q", ErrNotAllowed, cmd)
	}
	if !s.Exists(cmd) {
		return Result{}, fmt.Errorf("%w: %s", ErrNoCommand, cmd)
	}
	return s.run(ctx, cmd, args)
}

// Exists reports whether RunAny would run cmd: a valid name and a regular file at
// <Bin>/<cmd> (Lstat, so a symlink doesn't count).
func (s Sudo) Exists(cmd string) bool {
	if !anyNameRe.MatchString(cmd) || cmd == Wrapper {
		return false
	}
	fi, err := os.Lstat(filepath.Join(s.bin(), cmd))
	return err == nil && fi.Mode().IsRegular()
}

func (s Sudo) bin() string {
	if s.Bin == "" {
		return "/usr/local/hestia/bin"
	}
	return s.Bin
}

func (s Sudo) run(ctx context.Context, cmd string, args []string) (Result, error) {
	bin, sudo, timeout := s.bin(), s.SudoPath, s.Timeout
	if sudo == "" {
		sudo = "/usr/bin/sudo"
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	timeout = timeoutFrom(ctx, timeout)
	secs := wrapperSeconds(timeout)
	// Only the deadline stops a command: a caller that goes away (a closed browser tab)
	// must not interrupt a script halfway through a change. The wrapper enforces the
	// deadline as root; this one is a backstop for a wrapper that can't.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backstop(secs))
	defer cancel()

	argv := append([]string{filepath.Join(bin, Wrapper), strconv.FormatInt(secs, 10), cmd}, args...)
	c := exec.CommandContext(ctx, sudo, argv...)
	var stdout bytes.Buffer
	stderr := &cappedBuffer{max: MaxStderr}
	c.Stdout, c.Stderr = &stdout, stderr
	// Backstop: stop sudo's process group (sudo relays the signal to the wrapper). If
	// anything still holds the pipes, stop waiting after 1s.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGTERM) }
	c.WaitDelay = outputWait

	start := time.Now()
	err := c.Run()
	if stderr.Len() > 0 {
		slog.Debug("command stderr", "cmd", cmd, "stderr", stderr.String())
	}
	if ctx.Err() == context.DeadlineExceeded {
		return Result{}, fmt.Errorf("%w: %s", ErrTimeout, cmd)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The wrapper exits 124 (stopped by SIGTERM) or 137 (SIGKILL) on its deadline;
		// sudo itself dying by SIGKILL after the deadline counts too. A command exiting
		// so by itself before the deadline is an ordinary result.
		if timedOut(exitErr) && time.Since(start) >= timeout {
			return Result{}, fmt.Errorf("%w: %s", ErrTimeout, cmd)
		}
		return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Exit: exitErr.ExitCode()}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("runner: run %s: %w", cmd, err)
	}
	return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, nil
}

func timedOut(e *exec.ExitError) bool {
	if ws, ok := e.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL {
		return true
	}
	code := e.ExitCode()
	return code == 124 || code == 137
}

// cappedBuffer keeps the first max bytes written and drops the rest, while reporting
// every write as complete so the command never sees a broken pipe.
// It wraps the buffer rather than embedding it: an embedded bytes.Buffer's ReadFrom
// would let io.Copy bypass Write and the cap.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *cappedBuffer) Len() int       { return b.buf.Len() }
func (b *cappedBuffer) String() string { return b.buf.String() }

// Bytes is what was kept, or nil when nothing was written.
func (b *cappedBuffer) Bytes() []byte {
	if b.buf.Len() == 0 {
		return nil
	}
	return b.buf.Bytes()
}
