package terminal

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Session is a running shell on a pty.
type Session interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	// Wait blocks until the shell exits.
	Wait() error
}

// Window size bounds: a hello or resize of 0 means the default; larger values are
// clamped.
const (
	DefaultCols, DefaultRows = 80, 24
	MaxCols, MaxRows         = 1000, 500
)

// Close reasons (parity decision 11).
const reasonNotAuthenticated = "You are not authenticated."

// ReasonTooMany is the close reason when MaxSessions shells are already running
// (Hostisan-only msgid).
const ReasonTooMany = "Too many terminals are open. Close one and try again."

// DefaultMaxSessions caps the shells running at once across all accounts.
const DefaultMaxSessions = 128

// DefaultWriteTimeout bounds each frame written to the panel, so a panel that stops
// reading can't pin a shell; closeTimeout bounds the final close frame.
const (
	DefaultWriteTimeout = 10 * time.Second
	closeTimeout        = 2 * time.Second
)

// DefaultHelloTimeout bounds the wait for the first frame.
const DefaultHelloTimeout = 10 * time.Second

// drainTimeout is how long output may still flow after the shell exits (a background
// job can keep the pty open forever).
const drainTimeout = 500 * time.Millisecond

// Server accepts the panel's connections and runs one shell per connection.
//
// The shell is not closed on idle or after a maximum lifetime: Hestia never did, and
// the panel ends terminals when its session ends (parity decisions 8 and 12).
//
// Shells get no PAM session: no pam_limits, pam_env, utmp/wtmp or lastlog entry, as
// with Hestia's node-pty terminal (WT15).
type Server struct {
	// AllowUID is the only peer uid accepted (hestiaweb), unless Dev.
	AllowUID int
	// LookupAccount applies the account rules; ErrNoShell and ErrNotAllowed become
	// the close reason.
	LookupAccount func(name string) (Account, error)
	Spawn         func(a Account, cols, rows uint16) (Session, error)
	// PeerUID reports the connecting process's uid; nil refuses every peer unless Dev.
	PeerUID func(c net.Conn) (int, error)
	// Dev skips the peer check (fixture-mode smoke only).
	Dev bool
	// HelloTimeout bounds the wait for the hello frame (DefaultHelloTimeout if 0).
	HelloTimeout time.Duration
	// WriteTimeout bounds each frame write (DefaultWriteTimeout if 0).
	WriteTimeout time.Duration
	// MaxSessions caps concurrent shells (DefaultMaxSessions if 0).
	MaxSessions int

	mu       sync.Mutex
	sessions map[Session]struct{}
	reserved int
	wg       sync.WaitGroup // connection handlers
}

// Serve accepts connections until ln is closed.
func (s *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(c)
		}()
	}
}

// CloseAll hangs up every running shell (shutdown).
func (s *Server) CloseAll() {
	s.mu.Lock()
	open := make([]Session, 0, len(s.sessions))
	for sess := range s.sessions {
		open = append(open, sess)
	}
	s.mu.Unlock()
	for _, sess := range open {
		sess.Close()
	}
}

// reserve takes a shell slot; false when MaxSessions are running.
func (s *Server) reserve() bool {
	max := s.MaxSessions
	if max <= 0 {
		max = DefaultMaxSessions
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved >= max {
		return false
	}
	s.reserved++
	return true
}

func (s *Server) release() {
	s.mu.Lock()
	s.reserved--
	s.mu.Unlock()
}

func (s *Server) track(sess Session, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[Session]struct{}{}
	}
	if on {
		s.sessions[sess] = struct{}{}
	} else {
		delete(s.sessions, sess)
	}
}

// connWriter serialises frames written from the output and exit goroutines; every
// write has a deadline.
type connWriter struct {
	mu sync.Mutex
	c  net.Conn
}

func (w *connWriter) frame(typ byte, p []byte, timeout time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.c.SetWriteDeadline(time.Now().Add(timeout))
	return WriteFrame(w.c, typ, p)
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	if !s.Dev {
		if s.PeerUID == nil {
			slog.Warn("terminal: refused connection", "reason", "no peer check")
			return
		}
		uid, err := s.PeerUID(c)
		if err != nil || uid != s.AllowUID {
			slog.Warn("terminal: refused connection", "peer_uid", uid, "err", err)
			return
		}
	}
	w := &connWriter{c: c}
	writeTimeout := s.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	refuse := func(reason string) { _ = w.frame(FrameClose, []byte(reason), min(closeTimeout, writeTimeout)) }

	timeout := s.HelloTimeout
	if timeout <= 0 {
		timeout = DefaultHelloTimeout
	}
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	typ, payload, err := ReadFrame(c)
	var h Hello
	if err != nil || typ != FrameHello || json.Unmarshal(payload, &h) != nil {
		refuse(reasonNotAuthenticated)
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	a, err := s.LookupAccount(h.User)
	if err != nil {
		reason := ErrNotAllowed.Error()
		if errors.Is(err, ErrNoShell) {
			reason = ErrNoShell.Error()
		}
		slog.Info("terminal: refused account", "user", h.User, "reason", reason, "err", err)
		refuse(reason)
		return
	}
	if !s.reserve() {
		slog.Warn("terminal: refused shell", "user", a.Name, "reason", ReasonTooMany)
		refuse(ReasonTooMany)
		return
	}
	defer s.release()
	cols, rows := clampSize(h.Cols, h.Rows)
	sess, err := s.Spawn(a, cols, rows)
	if err != nil {
		slog.Error("terminal: spawn failed", "user", a.Name, "err", err)
		refuse("")
		return
	}
	s.track(sess, true)
	defer s.track(sess, false)
	defer sess.Close()
	start := time.Now()
	slog.Info("terminal: shell started", "user", a.Name, "uid", a.UID, "shell", a.Shell)

	// Shell output -> FrameData.
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				if w.frame(FrameData, buf[:n], writeTimeout) != nil {
					c.Close()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Panel frames -> shell. Any error or unexpected frame hangs up.
	inDone := make(chan struct{})
	go func() {
		defer close(inDone)
		defer sess.Close()
		for {
			typ, p, err := ReadFrame(c)
			if err != nil {
				return
			}
			switch typ {
			case FrameData:
				if _, err := sess.Write(p); err != nil {
					return
				}
			case FrameResize:
				cl, rw, err := ParseResize(p)
				if err != nil {
					return
				}
				cl, rw = clampSize(cl, rw)
				_ = sess.Resize(cl, rw)
			default:
				return
			}
		}
	}()

	waitErr := sess.Wait()
	select {
	case <-outDone:
	case <-time.After(drainTimeout):
	}
	refuse("")
	sess.Close()
	c.Close()
	<-inDone
	<-outDone
	slog.Info("terminal: shell ended", "user", a.Name, "duration", time.Since(start).Round(time.Second), "exit", waitErr)
}

func clampSize(cols, rows uint16) (uint16, uint16) {
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}
	return min(cols, MaxCols), min(rows, MaxRows)
}
