package terminal

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestPeerUID(t *testing.T) {
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "p.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := net.Dial("unix", ln.Addr().String())
		if err == nil {
			defer c.Close()
			buf := make([]byte, 1)
			c.Read(buf)
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	uid, err := PeerUID(c)
	if err != nil || uid != os.Getuid() {
		t.Fatalf("PeerUID = %d, %v; want %d", uid, err, os.Getuid())
	}

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if _, err := PeerUID(a); err == nil {
		t.Fatal("a non-unix connection has a peer uid")
	}
}
