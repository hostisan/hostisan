package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeHestia builds a temp bin dir with the given scripts, a copy of the real
// bin/v-hostisan-run, and a fake sudo that records each call in a marker file and
// then execs its arguments.
func fakeHestia(t *testing.T, scripts map[string]string) (Sudo, string) {
	return fakeHestiaSudo(t, scripts, `exec "$@"`)
}

// fakeHestiaSudo is fakeHestia with the fake sudo's last line given.
func fakeHestiaSudo(t *testing.T, scripts map[string]string, sudoExec string) (Sudo, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "sudo-called")
	sudo := filepath.Join(dir, "fake-sudo")
	write(t, sudo, "#!/bin/sh\nprintf '%s\\n' \"$@\" >'"+marker+"'\n"+sudoExec+"\n")
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper, err := os.ReadFile(filepath.Join("..", "..", "..", "bin", Wrapper))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, Wrapper), string(wrapper))
	for name, body := range scripts {
		write(t, filepath.Join(bin, name), "#!/bin/sh\n"+body+"\n")
	}
	return Sudo{Bin: bin, SudoPath: sudo}, marker
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSudoRunsArgvWithoutShell(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{"v-list-user": `echo "$#|$1|$2"`})
	res, err := s.Run(context.Background(), "v-list-user", "a b; echo pwned", "json")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(res.Stdout), "2|a b; echo pwned|json\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if res.Exit != 0 {
		t.Fatalf("exit = %d, want 0", res.Exit)
	}
}

func TestSudoRejectsUnlisted(t *testing.T) {
	s, marker := fakeHestia(t, map[string]string{"rm": "exit 0"})
	_, err := s.Run(context.Background(), "rm", "-rf", "/")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("sudo was invoked for a command that is not on the allowlist")
	}
}

func TestSudoNonZeroExitIsResult(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{"v-list-user": "echo 'Error: user bob doesn'\\''t exist'; exit 3"})
	res, err := s.Run(context.Background(), "v-list-user", "bob", "json")
	if err != nil {
		t.Fatalf("a non-zero exit must not be a Go error, got %v", err)
	}
	if res.Exit != 3 {
		t.Fatalf("exit = %d, want 3", res.Exit)
	}
	var cmdErr *CommandError
	if !errors.As(res.Err("v-list-user"), &cmdErr) || cmdErr.Exit != ExitNotExist {
		t.Fatalf("Err() = %v, want *CommandError with ExitNotExist", res.Err("v-list-user"))
	}
}

func TestSudoTimeout(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{"v-list-user": "sleep 5"})
	start := time.Now()
	_, err := s.Run(WithTimeout(context.Background(), 100*time.Millisecond), "v-list-user", "admin")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	// The wrapper's deadline is in whole seconds: 100 ms rounds up to 1 s.
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Fatalf("took %v, want about 1s", elapsed)
	}
}

// sudo runs the wrapper with the deadline in whole seconds (rounded up), then the
// command and its arguments as they are.
func TestSudoRunsThroughWrapper(t *testing.T) {
	s, marker := fakeHestia(t, map[string]string{"v-list-user": `echo "$#|$1|$2"`})
	res, err := s.Run(WithTimeout(context.Background(), 1500*time.Millisecond), "v-list-user", "a b", "")
	if err != nil || string(res.Stdout) != "2|a b|\n" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
	argv, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{filepath.Join(s.Bin, Wrapper), "2", "v-list-user", "a b", ""}, "\n") + "\n"
	if string(argv) != want {
		t.Fatalf("sudo argv = %q, want %q", argv, want)
	}
	if _, err := (Sudo{Bin: s.Bin, SudoPath: s.SudoPath}).Run(context.Background(), "v-list-user"); err != nil {
		t.Fatal(err)
	}
	argv, _ = os.ReadFile(marker)
	if got := strings.SplitN(string(argv), "\n", 3)[1]; got != "60" {
		t.Fatalf("default deadline = %q, want 60", got)
	}
}

// Under real sudo the panel (hestiaweb) can't signal the root command, so its own
// group kill never reaches the script's children: the wrapper (root) must stop them.
// The fake sudo here moves the command into its own session, out of the runner's
// reach, the way the uid change does.
func TestSudoTimeoutKillsChildrenItCannotSignal(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	s, _ := fakeHestiaSudo(t, map[string]string{
		"v-list-user": "sleep 300 & echo $! >'" + pidFile + "'; wait",
	}, `exec setsid -w "$@"`)
	_, err := s.Run(WithTimeout(context.Background(), time.Second), "v-list-user")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("background child %d still runs after the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A command that ignores SIGTERM is stopped with SIGKILL 5 s after the deadline and
// reported as a timeout, not as a result.
func TestSudoTimeoutTermIgnoringCommand(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{"v-list-user": "trap '' TERM; while :; do sleep 0.1; done"})
	start := time.Now()
	res, err := s.Run(WithTimeout(context.Background(), time.Second), "v-list-user")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("got exit %d, err %v; want ErrTimeout", res.Exit, err)
	}
	if elapsed := time.Since(start); elapsed > 9*time.Second {
		t.Fatalf("took %v, want about 6 s (1 s + 5 s kill-after)", elapsed)
	}
}

// sudo dying by SIGKILL after the deadline (an exec'd timeout's group kill takes sudo
// down with it) is a timeout too; before the deadline it is an error-free result.
func TestSudoKilledAfterDeadlineIsTimeout(t *testing.T) {
	s, _ := fakeHestiaSudo(t, nil, `sleep 1.2; kill -KILL $$`)
	if _, err := s.Run(WithTimeout(context.Background(), time.Second), "v-list-user"); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	s, _ = fakeHestiaSudo(t, nil, `kill -KILL $$`)
	if res, err := s.Run(WithTimeout(context.Background(), 5*time.Second), "v-list-user"); errors.Is(err, ErrTimeout) {
		t.Fatalf("got ErrTimeout before the deadline (exit %d)", res.Exit)
	}
}

// The wrapper accepts at most 86400 s; a longer timeout is capped there.
func TestSudoCapsTimeout(t *testing.T) {
	s, marker := fakeHestia(t, map[string]string{"v-list-user": "echo ok"})
	res, err := s.Run(WithTimeout(context.Background(), 48*time.Hour), "v-list-user")
	if err != nil || string(res.Stdout) != "ok\n" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
	argv, _ := os.ReadFile(marker)
	if got := strings.SplitN(string(argv), "\n", 3)[1]; got != "86400" {
		t.Fatalf("seconds = %q, want 86400", got)
	}
}

// A command that exits 124 by itself, before the deadline, is a result, not a timeout.
func TestSudoExit124BeforeDeadlineIsResult(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{"v-list-user": "exit 124"})
	res, err := s.Run(WithTimeout(context.Background(), 5*time.Second), "v-list-user")
	if err != nil || res.Exit != 124 {
		t.Fatalf("got exit %d, %v; want exit 124 and no error", res.Exit, err)
	}
}

// The wrapper itself is never a command: not through Run, RunAny or Exists.
func TestSudoRefusesWrapperAsCommand(t *testing.T) {
	s, marker := fakeHestia(t, nil)
	if s.Exists(Wrapper) {
		t.Fatal("Exists(wrapper) = true")
	}
	if _, err := s.RunAny(context.Background(), Wrapper, "5", "v-list-user"); err == nil {
		t.Fatal("RunAny ran the wrapper as a command")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("sudo was invoked")
	}
}

// A browser that disconnects cancels its request's context. The command it started
// must still finish: stopping a v-add-* or v-change-* halfway leaves a half-made change.
func TestSudoFinishesWhenCallerGoesAway(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{"v-list-user": "sleep 0.3; echo done"})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	res, err := s.Run(ctx, "v-list-user", "admin")
	if err != nil || res.Exit != 0 || string(res.Stdout) != "done\n" {
		t.Fatalf("got %q exit %d err %v; want the command to run to the end", res.Stdout, res.Exit, err)
	}
}

// The command's stderr is kept apart from stdout, capped at MaxStderr.
func TestSudoKeepsStderr(t *testing.T) {
	s, _ := fakeHestia(t, map[string]string{
		"v-list-user":           "echo out; echo 'Fatal: broken' >&2; exit 0",
		"v-list-users":          "head -c 100000 /dev/zero | tr '\\0' x >&2; exit 0",
		"v-restore-file-restic": "echo 'error: lchown x' >&2; exit 2",
	})
	res, err := s.Run(context.Background(), "v-list-user", "bob", "json")
	if err != nil || string(res.Stdout) != "out\n" || string(res.Stderr) != "Fatal: broken\n" {
		t.Fatalf("got %q / %q, %v", res.Stdout, res.Stderr, err)
	}
	res, err = s.Run(context.Background(), "v-list-users", "json")
	if err != nil || len(res.Stderr) != MaxStderr {
		t.Fatalf("stderr %d bytes, want %d (%v)", len(res.Stderr), MaxStderr, err)
	}
	res, err = s.Run(context.Background(), "v-restore-file-restic", "bob", "s", "/home/bob/x")
	if err != nil || res.Exit != 2 || string(res.Stderr) != "error: lchown x\n" {
		t.Fatalf("got exit %d stderr %q, %v", res.Exit, res.Stderr, err)
	}
}
