package terminal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	hello, _ := json.Marshal(Hello{User: "bob", Cols: 100, Rows: 30})
	frames := []struct {
		typ     byte
		payload []byte
	}{
		{FrameHello, hello},
		{FrameData, []byte("ls -la\r")},
		{FrameResize, ResizePayload(120, 40)},
		{FrameData, nil},
		{FrameData, bytes.Repeat([]byte("x"), MaxPayload)},
		{FrameClose, []byte("You have no shell access.")},
	}
	var buf bytes.Buffer
	for _, f := range frames {
		if err := WriteFrame(&buf, f.typ, f.payload); err != nil {
			t.Fatal(err)
		}
	}
	// 1-byte type, 4-byte big-endian length.
	if got := buf.Bytes()[:5]; !bytes.Equal(got, []byte{'H', 0, 0, 0, byte(len(hello))}) {
		t.Fatalf("header = %v", got)
	}
	for i, f := range frames {
		typ, payload, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if typ != f.typ || !bytes.Equal(payload, f.payload) {
			t.Fatalf("frame %d = %c %q, want %c %q", i, typ, payload, f.typ, f.payload)
		}
	}
	if _, _, err := ReadFrame(&buf); err != io.EOF {
		t.Fatalf("after the last frame: %v, want io.EOF", err)
	}

	var h Hello
	if err := json.Unmarshal(hello, &h); err != nil || h != (Hello{User: "bob", Cols: 100, Rows: 30}) {
		t.Fatalf("hello = %+v, %v", h, err)
	}
	cols, rows, err := ParseResize(ResizePayload(120, 40))
	if err != nil || cols != 120 || rows != 40 {
		t.Fatalf("resize = %d x %d, %v", cols, rows, err)
	}
	if !bytes.Equal(ResizePayload(0x0102, 0x0304), []byte{1, 2, 3, 4}) {
		t.Fatal("resize payload is not two big-endian uint16")
	}
	if _, _, err := ParseResize([]byte{1, 2, 3}); err == nil {
		t.Fatal("a 3-byte resize parsed")
	}
}

func TestFrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, FrameData, make([]byte, MaxPayload+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("write: %v, want ErrFrameTooLarge", err)
	}
	if buf.Len() != 0 {
		t.Fatal("a refused frame wrote bytes")
	}
	// A header announcing more than 64 KiB is refused before reading the payload.
	r := bytes.NewReader([]byte{'D', 0, 1, 0, 1})
	if _, _, err := ReadFrame(r); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("read: %v, want ErrFrameTooLarge", err)
	}
	if MaxPayload != 64*1024 {
		t.Fatalf("MaxPayload = %d", MaxPayload)
	}
	// A truncated frame is an error, not a short payload.
	if _, _, err := ReadFrame(bytes.NewReader([]byte{'D', 0, 0, 0, 5, 'a'})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated: %v", err)
	}
	if _, _, err := ReadFrame(bytes.NewReader([]byte{'D', 0})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header: %v", err)
	}
}
