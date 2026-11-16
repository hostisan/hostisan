//go:build vps

package runner

// VPS-only check (Plan 6 B6 step 5, C2 S7): a v-* command that times out under real
// sudo must not leave root children running. hestiaweb can't signal them, so the
// runner calls bin/v-hostisan-run, which stops the command's whole process group as
// root (GNU timeout). The wrapper must be installed in /usr/local/hestia/bin.
//
// Run as root on the server (it installs a probe script into Hestia's bin dir and
// re-runs itself as hestiaweb):
//
//	cd hostisan && go test -c -tags vps -o /tmp/runner.vps ./internal/runner
//	/tmp/runner.vps -test.run '^TestSudoTimeoutKillsChildren$' -test.v

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	vpsChildEnv = "HOSTISAN_VPS_CHILD"
	vpsCmdEnv   = "HOSTISAN_VPS_CMD"
	vpsSleep    = "^sleep 300$"
)

func TestSudoTimeoutKillsChildren(t *testing.T) {
	if os.Getenv(vpsChildEnv) == "1" {
		sudoTimeoutChild(t)
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("run as root on the VPS: it installs a probe into /usr/local/hestia/bin")
	}
	if _, err := user.Lookup("hestiaweb"); err != nil {
		t.Skipf("no hestiaweb user: %v", err)
	}
	if left := pgrepSleep(); left != "" {
		t.Fatalf("a 'sleep 300' is already running (pids %s); stop it first", left)
	}

	// The probe: a temp v-* script whose child outlives a plain SIGTERM to the script.
	name := fmt.Sprintf("v-hostisan-vps-probe-%d", os.Getpid())
	probe := filepath.Join("/usr/local/hestia/bin", name)
	if err := os.WriteFile(probe, []byte("#!/bin/bash\nsleep 300 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Remove(probe)
		exec.Command("pkill", "-x", "-f", "sleep 300").Run()
	})

	// hestiaweb can't read go test's private build dir: run a copy of this binary.
	self := copySelf(t)
	child := exec.Command("runuser", "-u", "hestiaweb", "--", "/usr/bin/env",
		vpsChildEnv+"=1", vpsCmdEnv+"="+name,
		self, "-test.run", "^TestSudoTimeoutKillsChildren$", "-test.v")
	child.Dir = filepath.Dir(self)
	out, err := child.CombinedOutput()
	t.Logf("child as hestiaweb:\n%s", out)
	if err != nil {
		t.Fatalf("child run failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		left := pgrepSleep()
		if left == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("'sleep 300' (pids %s) still runs 5 s after the runner timed out: "+
				"the command's children outlived it; is bin/v-hostisan-run installed?", left)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// sudoTimeoutChild runs as hestiaweb: the real runner, real sudo, a 2 s timeout.
func sudoTimeoutChild(t *testing.T) {
	cmd := os.Getenv(vpsCmdEnv)
	start := time.Now()
	_, err := Sudo{Timeout: 2 * time.Second}.RunAny(context.Background(), cmd)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("RunAny(%s) err = %v, want ErrTimeout", cmd, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("RunAny took %v, want about 2 s (+1 s WaitDelay)", elapsed)
	}
}

// pgrepSleep returns the pids of any `sleep 300`, space separated ("" when none).
// It runs as root, so hidepid doesn't hide anything.
func pgrepSleep() string {
	out, _ := exec.Command("pgrep", "-f", vpsSleep).Output()
	return strings.Join(strings.Fields(string(out)), " ")
}

func copySelf(t *testing.T) string {
	t.Helper()
	src, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "hostisan-vps-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "runner.test")
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	outf, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(outf, in); err != nil {
		t.Fatal(err)
	}
	if err := outf.Close(); err != nil {
		t.Fatal(err)
	}
	return dst
}
