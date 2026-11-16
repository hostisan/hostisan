//go:build !linux

package main

import (
	"fmt"
	"io"
)

// terminalCmd needs Linux (ptys via creack/pty with Setctty, SO_PEERCRED).
func terminalCmd(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "hostisan terminal: only supported on Linux")
	return 1
}
