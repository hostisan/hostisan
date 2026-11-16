package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hostisan/hostisan/internal/session"
)

// realIPOver serves one request through listen(addr) and returns the RealIP it saw for
// a request claiming X-Real-IP 198.51.100.7.
func realIPOver(t *testing.T, addr string, dial func(context.Context, string, string) (net.Conn, error)) string {
	t.Helper()
	ln, connContext, err := listen(addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler:     http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, session.RealIP(r)) }),
		ConnContext: connContext,
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dial(ctx, ln.Addr().Network(), ln.Addr().String())
	}}}
	req, _ := http.NewRequest("GET", "http://hostisan/", nil)
	req.Header.Set("X-Real-IP", "198.51.100.7")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func TestListenUnixSocketTrustsProxy(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "hostisan.sock")
	if got := realIPOver(t, "unix:"+sock, (&net.Dialer{}).DialContext); got != "198.51.100.7" {
		t.Fatalf("RealIP over the socket = %q, want the proxy's X-Real-IP", got)
	}
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o660 {
		t.Fatalf("socket mode = %o, want 660 (owner and the proxy's group only)", perm)
	}
}

func TestListenTCPIgnoresForwardedIP(t *testing.T) {
	if got := realIPOver(t, "127.0.0.1:0", (&net.Dialer{}).DialContext); got != "127.0.0.1" {
		t.Fatalf("RealIP over loopback TCP = %q, want 127.0.0.1", got)
	}
}

func TestListenUnixReplacesStaleSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "hostisan.sock")
	// A crash leaves the socket file behind.
	old, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	old.Close()
	ln, _, err := listen("unix:" + sock)
	if err != nil {
		t.Fatalf("listen over a stale socket file: %v", err)
	}
	ln.Close()
}

func TestListenUnixKeepsOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := listen("unix:" + path); err == nil {
		t.Fatal("listen replaced a regular file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the regular file was removed")
	}
}
