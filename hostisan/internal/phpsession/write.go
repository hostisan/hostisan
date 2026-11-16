package phpsession

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrBadID means a session id is not one NewID makes; nothing is touched.
var ErrBadID = errors.New("phpsession: invalid session id")

var idRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// path is dir/sess_<id>, PHP's files handler naming.
func path(dir, id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", ErrBadID
	}
	return filepath.Join(dir, "sess_"+id), nil
}

// Write stores data as the session id in dir, mode 0600 and owned by the calling
// user (hestiaweb, hestia-php's user). It writes a temp file in dir and renames it
// over sess_<id>, so PHP never reads half a session and a symlink planted at that
// name is replaced, not followed. The temp name doesn't start with sess_, so PHP
// never takes it for a session.
func Write(dir, id string, data []byte) error {
	target, err := path(dir, id)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".hostisan-sess-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	// CreateTemp makes it 0600 already; make sure whatever the umask.
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	ok = true
	return nil
}

// Delete removes the session id from dir. A file already gone is not an error; an id
// NewID couldn't have made is refused with ErrBadID.
func Delete(dir, id string) error {
	target, err := path(dir, id)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Sweep deletes from dir every session file the panel could have written
// (sess_<64 hex>) and any temp file Write left behind, and returns how many it
// removed. serve runs it at start: a restart forgets which panel session owned which
// file, and an orphan would keep FileGator access alive. hestia-php's own sessions
// (26 characters) and anything else are left alone. A missing dir is not an error.
func Sweep(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for _, e := range entries {
		name := e.Name()
		id, isSess := strings.CutPrefix(name, "sess_")
		ours := isSess && idRe.MatchString(id) || strings.HasPrefix(name, ".hostisan-sess-")
		if !ours || e.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}
