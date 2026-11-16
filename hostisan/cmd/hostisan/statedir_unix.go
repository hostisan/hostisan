//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// checkStateDir refuses a state dir others could plant or change files in: it must
// be a real directory (not a symlink), owned by the panel's user and not writable by
// its group or others. sessions.json lets a cookie back in and jobs.json makes the
// panel undo app installs at start, so a file someone else wrote must not be loaded.
// hostisan.service's StateDirectory is hestiaweb's, mode 0700.
func checkStateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if perm := fi.Mode().Perm(); perm&0o022 != 0 {
		return fmt.Errorf("%s is writable by others (mode %04o, want 0700)", dir, perm)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is owned by uid %d, not the panel's uid %d", dir, st.Uid, os.Geteuid())
	}
	return nil
}
