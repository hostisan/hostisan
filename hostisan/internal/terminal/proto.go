// Package terminal is Hostisan's web terminal: the root process (`hostisan terminal`)
// that spawns login shells, and the frame protocol the panel speaks to it over a unix
// socket (parity decision 8).
package terminal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame types. Every frame is a 1-byte type, a 4-byte big-endian payload length and
// the payload.
const (
	// FrameHello (panel → terminal) is the first frame only: a JSON Hello.
	FrameHello byte = 'H'
	// FrameData carries terminal bytes, both ways.
	FrameData byte = 'D'
	// FrameResize (panel → terminal) is 4 bytes: cols, rows as big-endian uint16.
	FrameResize byte = 'R'
	// FrameClose (terminal → panel) carries the reason as an English msgid in UTF-8
	// ("" when the shell exited); the terminal then closes the connection.
	FrameClose byte = 'C'
)

// MaxPayload is the largest payload a frame may carry (64 KiB).
const MaxPayload = 64 * 1024

// ErrFrameTooLarge means a frame announced or carried more than MaxPayload bytes.
var ErrFrameTooLarge = errors.New("terminal: frame too large")

// Hello names the account to open a shell for and the initial window size.
type Hello struct {
	User string `json:"user"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// WriteFrame writes one frame in a single Write call. A payload over MaxPayload
// writes nothing and returns ErrFrameTooLarge.
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return ErrFrameTooLarge
	}
	buf := make([]byte, 5+len(payload))
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads one frame. io.EOF means the stream ended cleanly between frames; a
// stream that ends inside a frame gives io.ErrUnexpectedEOF. A header announcing more
// than MaxPayload returns ErrFrameTooLarge without reading the payload.
func ReadFrame(r io.Reader) (typ byte, payload []byte, err error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:5])
	if n > MaxPayload {
		return 0, nil, ErrFrameTooLarge
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	if n == 0 {
		payload = nil
	}
	return hdr[0], payload, nil
}

// ResizePayload is a FrameResize payload.
func ResizePayload(cols, rows uint16) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:2], cols)
	binary.BigEndian.PutUint16(b[2:4], rows)
	return b
}

// ParseResize reads a FrameResize payload.
func ParseResize(p []byte) (cols, rows uint16, err error) {
	if len(p) != 4 {
		return 0, 0, fmt.Errorf("terminal: resize payload of %d bytes", len(p))
	}
	return binary.BigEndian.Uint16(p[0:2]), binary.BigEndian.Uint16(p[2:4]), nil
}
