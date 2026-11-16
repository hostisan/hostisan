//go:build unix

package session

import (
	"os"
	"syscall"
)

// fileOwner is fi's uid and gid, or -1, -1 when the system doesn't say.
func fileOwner(fi os.FileInfo) (int, int) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid)
	}
	return -1, -1
}
