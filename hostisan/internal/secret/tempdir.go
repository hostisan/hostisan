package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// TempDir creates a private directory (mode 0700) in dir holding one 0600 file per
// entry of files, named by the key, with the value written exactly as given. It
// returns the directory's path and a cleanup func that removes it with its contents.
// Hestia's SSL scripts read <dir>/<domain>.crt, .key and .ca this way, so keys never
// appear in a process listing. Names must be plain file names.
func TempDir(dir string, files map[string]string) (string, func(), error) {
	for name := range files {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return "", nil, errors.New("secret: bad file name " + name)
		}
	}
	path, err := os.MkdirTemp(dir, "hostisan-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(path) }
	if err := os.Chmod(path, 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	for name, content := range files {
		if err := writePrivate(filepath.Join(path, name), content); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return path, cleanup, nil
}

func writePrivate(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil { // the umask may have narrowed it; never widened
		f.Close()
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
