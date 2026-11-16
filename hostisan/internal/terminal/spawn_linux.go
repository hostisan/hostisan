package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// LoginPath is Debian's login.defs ENV_PATH for ordinary users (parity decision 10).
const LoginPath = "/usr/local/bin:/usr/bin:/bin:/usr/local/games:/usr/games"

// killGrace is how long a hung-up shell gets before its process group is killed.
const killGrace = 3 * time.Second

// loginCmd builds the login shell for a (parity decision 10): argv0 "-<shell>",
// cwd home, a new session with the pty as controlling terminal, and an environment
// built from scratch. asRoot sets the uid, gid and supplementary groups; without root
// they can't change, so the shell runs as the caller.
func loginCmd(a Account, asRoot bool) *exec.Cmd {
	cmd := &exec.Cmd{
		Path: a.Shell,
		Args: []string{"-" + filepath.Base(a.Shell)},
		Dir:  a.Home,
		Env: []string{
			"HOME=" + a.Home,
			"USER=" + a.Name,
			"LOGNAME=" + a.Name,
			"SHELL=" + a.Shell,
			"PATH=" + LoginPath,
			"TERM=xterm-256color",
			"LANG=C.UTF-8",
			"HESTIA=/usr/local/hestia",
		},
		SysProcAttr: &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0},
	}
	if asRoot {
		groups := make([]uint32, len(a.Groups))
		copy(groups, a.Groups)
		// A non-nil, possibly empty list: setgroups always runs, so root's own
		// supplementary groups never reach the shell.
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: a.UID, Gid: a.GID, Groups: groups}
	}
	return cmd
}

// Spawn starts a's login shell on a new pty of cols x rows. As root it drops to the
// account; otherwise it only runs the caller's own account (--dev and tests).
func Spawn(a Account, cols, rows uint16) (Session, error) {
	// Defence in depth (CheckAccount already refuses these): never root's uid or gid.
	if a.UID == 0 || a.GID == 0 || slices.Contains(a.Groups, 0) {
		return nil, errors.New("terminal: refusing a shell with root's uid or groups")
	}
	asRoot := os.Geteuid() == 0
	if !asRoot && int(a.UID) != os.Geteuid() {
		return nil, fmt.Errorf("terminal: cannot run uid %d without root", a.UID)
	}
	cmd := loginCmd(a, asRoot)
	f, err := pty.StartWithAttrs(cmd, &pty.Winsize{Cols: cols, Rows: rows}, cmd.SysProcAttr)
	if err != nil {
		return nil, err
	}
	if f, err = pollable(f); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		return nil, err
	}
	s := &ptySession{f: f, cmd: cmd, done: make(chan struct{})}
	go func() {
		s.waitErr = cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

// pollable reopens the pty master in non-blocking mode. creack/pty calls Fd(), which
// leaves the file blocking, so Close would not interrupt a pending Read: a background
// job holding the pty open after the shell exits would pin the relay forever.
func pollable(f *os.File) (*os.File, error) {
	defer f.Close()
	fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return nil, errno
	}
	if err := syscall.SetNonblock(int(fd), true); err != nil {
		syscall.Close(int(fd))
		return nil, err
	}
	return os.NewFile(fd, "/dev/ptmx"), nil
}

// ptySession is a running shell: reads and writes go to the pty master.
type ptySession struct {
	f       *os.File
	cmd     *exec.Cmd
	done    chan struct{} // closed once the shell is reaped
	waitErr error
	once    sync.Once
}

func (s *ptySession) Read(p []byte) (int, error) {
	n, err := s.f.Read(p)
	// Linux reports the slave side going away as EIO.
	if errors.Is(err, syscall.EIO) {
		err = os.ErrClosed
	}
	return n, err
}

func (s *ptySession) Write(p []byte) (int, error) { return s.f.Write(p) }

func (s *ptySession) Resize(cols, rows uint16) error {
	return pty.Setsize(s.f, &pty.Winsize{Cols: cols, Rows: rows})
}

// Wait blocks until the shell exits.
func (s *ptySession) Wait() error {
	<-s.done
	return s.waitErr
}

// Close hangs up: SIGHUP to the shell's process group and the pty master closed (the
// kernel hangs up the session's terminal too), then SIGKILL to the group if the shell
// is still running after killGrace. Signals go only to a shell not yet reaped, so the
// group id can't have been reused.
//
// Only the shell's own process group is signalled. Jobs a job-control shell put in
// other groups get the kernel's hangup through the terminal (bash also forwards
// SIGHUP to its jobs), but a job that ignores SIGHUP, or anything that left the
// session (setsid, nohup + disown), outlives the terminal, as after an SSH logout.
func (s *ptySession) Close() error {
	s.once.Do(func() {
		pgid := s.cmd.Process.Pid // Setsid: the shell leads its own session and group
		s.signal(pgid, syscall.SIGHUP)
		s.signal(pgid, syscall.SIGCONT) // wake a stopped group so it sees the hangup
		s.f.Close()
		go func() {
			select {
			case <-s.done:
			case <-time.After(killGrace):
				s.signal(pgid, syscall.SIGKILL)
			}
		}()
	})
	return nil
}

func (s *ptySession) signal(pgid int, sig syscall.Signal) {
	select {
	case <-s.done:
	default:
		_ = syscall.Kill(-pgid, sig)
	}
}
