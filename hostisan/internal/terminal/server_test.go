package terminal

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeSession is a shell backed by pipes: the test reads what the panel typed from
// typed, writes shell output to output, and ends the shell with exit.
type fakeSession struct {
	typedR  *io.PipeReader
	typedW  *io.PipeWriter
	outputR *io.PipeReader
	outputW *io.PipeWriter
	resizes chan [2]uint16
	exited  chan struct{}
	closed  chan struct{}
	exitMu  sync.Once
	closeMu sync.Once
}

func newFakeSession() *fakeSession {
	f := &fakeSession{resizes: make(chan [2]uint16, 4), exited: make(chan struct{}), closed: make(chan struct{})}
	f.typedR, f.typedW = io.Pipe()
	f.outputR, f.outputW = io.Pipe()
	return f
}

func (f *fakeSession) Read(p []byte) (int, error)  { return f.outputR.Read(p) }
func (f *fakeSession) Write(p []byte) (int, error) { return f.typedW.Write(p) }
func (f *fakeSession) Resize(c, r uint16) error    { f.resizes <- [2]uint16{c, r}; return nil }
func (f *fakeSession) Wait() error                 { <-f.exited; return nil }
func (f *fakeSession) exit()                       { f.exitMu.Do(func() { close(f.exited) }) }
func (f *fakeSession) Close() error {
	f.closeMu.Do(func() {
		close(f.closed)
		f.outputR.Close()
		f.typedW.Close()
		f.exit()
	})
	return nil
}

type spawnCall struct {
	a          Account
	cols, rows uint16
}

type harness struct {
	t      *testing.T
	srv    *Server
	addr   string
	spawns chan spawnCall
	nspawn atomic.Int32
	sess   *fakeSession
}

func newHarness(t *testing.T, peer int, opts ...func(*Server)) *harness {
	t.Helper()
	h := &harness{t: t, spawns: make(chan spawnCall, 4), sess: newFakeSession()}
	h.srv = &Server{
		AllowUID: 990,
		LookupAccount: func(name string) (Account, error) {
			switch name {
			case "bob":
				return Account{Name: "bob", Home: "/home/bob", Shell: "/bin/bash", UID: 1001, GID: 1001}, nil
			case "nolog":
				return Account{}, ErrNoShell
			}
			return Account{}, ErrNotAllowed
		},
		Spawn: func(a Account, cols, rows uint16) (Session, error) {
			h.nspawn.Add(1)
			h.spawns <- spawnCall{a, cols, rows}
			return h.sess, nil
		},
		PeerUID: func(net.Conn) (int, error) { return peer, nil },
	}
	for _, o := range opts {
		o(h.srv)
	}
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "t.sock"))
	if err != nil {
		t.Fatal(err)
	}
	h.addr = ln.Addr().String()
	done := make(chan error, 1)
	go func() { done <- h.srv.Serve(ln) }()
	t.Cleanup(func() {
		ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return after the listener closed")
		}
		h.srv.CloseAll()
	})
	return h
}

func (h *harness) dial() net.Conn {
	h.t.Helper()
	c, err := net.Dial("unix", h.addr)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	return c
}

func (h *harness) hello(c net.Conn, user string, cols, rows uint16) {
	h.t.Helper()
	b, _ := json.Marshal(Hello{User: user, Cols: cols, Rows: rows})
	if err := WriteFrame(c, FrameHello, b); err != nil {
		h.t.Fatal(err)
	}
}

func expectFrame(t *testing.T, c net.Conn, typ byte, payload string) {
	t.Helper()
	gt, gp, err := ReadFrame(c)
	if err != nil || gt != typ || string(gp) != payload {
		t.Fatalf("frame = %c %q, %v; want %c %q", gt, gp, err, typ, payload)
	}
}

func expectClosed(t *testing.T, c net.Conn) {
	t.Helper()
	if typ, p, err := ReadFrame(c); err != io.EOF {
		t.Fatalf("after close: %c %q %v, want EOF", typ, p, err)
	}
}

// expectHangup: the connection ends with no frame. Closing a unix socket that still
// holds unread bytes resets it, which proves the hello was never read.
func expectHangup(t *testing.T, c net.Conn) {
	t.Helper()
	typ, p, err := ReadFrame(c)
	if err != io.EOF && !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("after hangup: %c %q %v, want EOF or reset", typ, p, err)
	}
}

// sendHello writes a hello the server may refuse to read.
func sendHello(c net.Conn) {
	b, _ := json.Marshal(Hello{User: "bob", Cols: 80, Rows: 24})
	_ = WriteFrame(c, FrameHello, b)
}

func TestServeRejectsPeer(t *testing.T) {
	h := newHarness(t, 1234)
	c := h.dial()
	// The panel would send a hello; the terminal must not read it.
	sendHello(c)
	expectHangup(t, c)
	if h.nspawn.Load() != 0 {
		t.Fatal("spawned for a foreign peer")
	}
}

func TestServeNoPeerCheckFailsClosed(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) { s.PeerUID = nil })
	c := h.dial()
	sendHello(c)
	expectHangup(t, c)
	if h.nspawn.Load() != 0 {
		t.Fatal("spawned without a peer check")
	}
}

func TestServePeerError(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) {
		s.PeerUID = func(net.Conn) (int, error) { return 990, errors.New("no cred") }
	})
	c := h.dial()
	sendHello(c)
	expectHangup(t, c)
	if h.nspawn.Load() != 0 {
		t.Fatal("spawned")
	}
}

func TestServeHelloFirst(t *testing.T) {
	h := newHarness(t, 990)
	c := h.dial()
	WriteFrame(c, FrameData, []byte("ls\r"))
	expectFrame(t, c, FrameClose, "You are not authenticated.")
	expectClosed(t, c)

	c = h.dial()
	WriteFrame(c, FrameHello, []byte("{not json"))
	expectFrame(t, c, FrameClose, "You are not authenticated.")
	expectClosed(t, c)
	if h.nspawn.Load() != 0 {
		t.Fatal("spawned before a hello")
	}
}

func TestServeHelloTimeout(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) { s.HelloTimeout = 50 * time.Millisecond })
	c := h.dial()
	expectFrame(t, c, FrameClose, "You are not authenticated.")
	expectClosed(t, c)
}

func TestServeAccountRefused(t *testing.T) {
	h := newHarness(t, 990)
	c := h.dial()
	h.hello(c, "nolog", 80, 24)
	expectFrame(t, c, FrameClose, "You have no shell access.")
	expectClosed(t, c)

	c = h.dial()
	h.hello(c, "root", 80, 24)
	expectFrame(t, c, FrameClose, "You are not allowed to access this server.")
	expectClosed(t, c)
	if h.nspawn.Load() != 0 {
		t.Fatal("spawned for a refused account")
	}
}

func TestServeSpawnFails(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) {
		s.Spawn = func(Account, uint16, uint16) (Session, error) { return nil, errors.New("no pty") }
	})
	c := h.dial()
	h.hello(c, "bob", 80, 24)
	expectFrame(t, c, FrameClose, "")
	expectClosed(t, c)
}

func TestServeRelay(t *testing.T) {
	h := newHarness(t, 990)
	c := h.dial()
	h.hello(c, "bob", 100, 30)
	call := <-h.spawns
	if call.a.Name != "bob" || call.cols != 100 || call.rows != 30 {
		t.Fatalf("spawn %+v", call)
	}

	// Typed data reaches the shell.
	WriteFrame(c, FrameData, []byte("ls -la\r"))
	buf := make([]byte, 64)
	n, err := h.sess.typedR.Read(buf)
	if err != nil || string(buf[:n]) != "ls -la\r" {
		t.Fatalf("typed %q, %v", buf[:n], err)
	}
	// Shell output becomes FrameData.
	go h.sess.outputW.Write([]byte("total 0\r\n"))
	expectFrame(t, c, FrameData, "total 0\r\n")
	// Resize.
	WriteFrame(c, FrameResize, ResizePayload(120, 40))
	select {
	case r := <-h.sess.resizes:
		if r != [2]uint16{120, 40} {
			t.Fatalf("resize %v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no resize")
	}
	// The shell exits: FrameClose "" and the connection closes.
	h.sess.exit()
	expectFrame(t, c, FrameClose, "")
	expectClosed(t, c)
	select {
	case <-h.sess.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("session not closed after exit")
	}
}

func TestServeDefaultSize(t *testing.T) {
	h := newHarness(t, 990)
	c := h.dial()
	h.hello(c, "bob", 0, 0)
	if call := <-h.spawns; call.cols != 80 || call.rows != 24 {
		t.Fatalf("spawn %+v", call)
	}
	// An absurd size is clamped rather than handed to the kernel.
	h2 := newHarness(t, 990)
	c2 := h2.dial()
	h2.hello(c2, "bob", 65535, 65535)
	if call := <-h2.spawns; call.cols != MaxCols || call.rows != MaxRows {
		t.Fatalf("spawn %+v", call)
	}
}

func TestServePanelHangupClosesSession(t *testing.T) {
	h := newHarness(t, 990)
	c := h.dial()
	h.hello(c, "bob", 80, 24)
	<-h.spawns
	c.Close()
	select {
	case <-h.sess.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("session not closed after the panel hung up")
	}
}

func TestServeBadFrameClosesSession(t *testing.T) {
	h := newHarness(t, 990)
	c := h.dial()
	h.hello(c, "bob", 80, 24)
	<-h.spawns
	WriteFrame(c, FrameHello, []byte(`{"user":"root"}`)) // a second hello is a protocol error
	select {
	case <-h.sess.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("session not closed after a bad frame")
	}
	if h.nspawn.Load() != 1 {
		t.Fatal("a second hello spawned")
	}
}

// CloseAll hangs up a running shell. The spawn is seen before the handler
// tracks the session; under load the test called CloseAll in that gap, which
// found nothing to close. The spawn here is held back so the test always meets
// that gap, and the test waits until the session is tracked.
func TestServeCloseAll(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) {
		spawn := s.Spawn
		s.Spawn = func(a Account, cols, rows uint16) (Session, error) {
			sess, err := spawn(a, cols, rows)
			time.Sleep(50 * time.Millisecond)
			return sess, err
		}
	})
	c := h.dial()
	h.hello(c, "bob", 80, 24)
	<-h.spawns
	h.waitTracked(1)
	h.srv.CloseAll()
	select {
	case <-h.sess.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("CloseAll left the session open")
	}
}

// waitTracked waits until the server tracks n running sessions.
func (h *harness) waitTracked(n int) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.srv.mu.Lock()
		got := len(h.srv.sessions)
		h.srv.mu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("server tracks %d sessions, want %d", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServeDevSkipsPeerCheck(t *testing.T) {
	h := newHarness(t, 1234, func(s *Server) { s.Dev = true })
	c := h.dial()
	h.hello(c, "bob", 80, 24)
	if call := <-h.spawns; call.a.Name != "bob" {
		t.Fatalf("spawn %+v", call)
	}
}

func TestServeMaxSessions(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) { s.MaxSessions = 1 })
	c := h.dial()
	h.hello(c, "bob", 80, 24)
	<-h.spawns
	c2 := h.dial()
	h.hello(c2, "bob", 80, 24)
	expectFrame(t, c2, FrameClose, ReasonTooMany)
	expectClosed(t, c2)
	if h.nspawn.Load() != 1 {
		t.Fatal("spawned over the cap")
	}
	if DefaultMaxSessions != 128 {
		t.Fatalf("DefaultMaxSessions = %d", DefaultMaxSessions)
	}
}

// A panel that stops reading can't pin a shell: writes time out and the session ends.
func TestServeStalledPeer(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) { s.WriteTimeout = 100 * time.Millisecond })
	c := h.dial()
	c.SetDeadline(time.Time{})
	h.hello(c, "bob", 80, 24)
	<-h.spawns
	// Flood output; the test never reads c.
	go func() {
		chunk := make([]byte, 32*1024)
		for {
			if _, err := h.sess.outputW.Write(chunk); err != nil {
				return
			}
		}
	}()
	select {
	case <-h.sess.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled panel pinned the session")
	}
}

// The final close frame is bounded too: the shell exits while the panel isn't reading.
func TestServeStalledPeerAtExit(t *testing.T) {
	h := newHarness(t, 990, func(s *Server) { s.WriteTimeout = 100 * time.Millisecond })
	c := h.dial()
	c.SetDeadline(time.Time{})
	h.hello(c, "bob", 80, 24)
	<-h.spawns
	stop := make(chan struct{})
	go func() {
		chunk := make([]byte, 32*1024)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := h.sess.outputW.Write(chunk); err != nil {
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	h.sess.exit()
	done := make(chan struct{})
	go func() { h.srv.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection handler did not return")
	}
	close(stop)
}
