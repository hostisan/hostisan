package secret

import (
	"os"
)

// TempFile writes content plus a newline to a new 0600 file in dir and returns its
// path and a cleanup func. Hestia's scripts read a password or hash from a file when
// the argument is a path under /tmp, so secrets never appear in a process listing.
func TempFile(dir, content string) (string, func(), error) {
	f, err := os.CreateTemp(dir, "hostisan-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if _, err := f.WriteString(content + "\n"); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}
