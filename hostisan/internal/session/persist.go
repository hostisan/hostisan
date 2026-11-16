package session

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
)

// The signed-in sessions survive a panel restart in <state-dir>/sessions.json
// (/var/lib/hostisan, hostisan.service's StateDirectory: hestiaweb's, mode 0700).
// Before, every update restarted hostisan.service and signed everyone out, and a
// web terminal could not reconnect without a new login.
//
// What the file holds, and what it does not:
//   - not the session id (the cookie, a bearer credential): only its SHA-256, the
//     store's key, so the file alone does not let anyone in;
//   - the CSRF token as it is: /api/v1/bootstrap must hand it to the reloaded page
//     after a restart, which a hash cannot do. Without the cookie it is worth
//     nothing; Hestia's own auth log already keeps it as the login fingerprint;
//   - nothing of a 2FA step (Pending: the password) and no signed-out session: a
//     restart in the middle of a 2FA login asks for the password again.
// Sign-outs write the file at once, so a restart never brings a signed-out session
// back: logout and Validate's endSession (Delete), DeleteUser (password change,
// deleted or suspended user) and a Regenerate that drops a signed-in session. A
// failed write is tried again at once; if that fails too, the log names the user
// whose sign-out is not on disk yet and every later save tries again. Logins
// (Regenerate), activity and the per-user cap's drops are written by Sweep (every
// minute) and Close.
//   - a mark of the password (PasswordMark: a SHA-256 of Hestia's copy of the
//     shadow hash), so a password changed while the panel was down still ends
//     the session; never the hash itself.

// fileVersion is the sessions file format; a file of another version is dropped.
const fileVersion = 1

type sessionsFile struct {
	Version  int      `json:"version"`
	Sessions []record `json:"sessions"`
}

// record is one signed-in session in the file. Its fields are all comparable, so
// Update can tell what changed.
type record struct {
	Key           string    `json:"key"` // key(ID)
	Token         string    `json:"token"`
	User          string    `json:"user"`
	Look          string    `json:"look,omitempty"`
	Role          string    `json:"role"` // UserContext
	LastActivity  time.Time `json:"last_activity"`
	IP            string    `json:"ip"`
	Language      string    `json:"language,omitempty"`
	Theme         string    `json:"theme,omitempty"`
	SortOrder     string    `json:"sort_order,omitempty"`
	LoginAsReturn bool      `json:"login_as_return,omitempty"`
	PasswordMark  string    `json:"pw,omitempty"` // Session.PasswordMark
}

// persisted reports whether s belongs in the file: signed in.
func persisted(s *Session) bool { return s.User != "" }

func recordOf(k string, s *Session) record {
	return record{Key: k, Token: s.Token, User: s.User, Look: s.Look, Role: s.UserContext,
		LastActivity: s.LastActivity, IP: s.CombinedIP, Language: s.Language, Theme: s.Theme,
		SortOrder: s.SortOrder, LoginAsReturn: s.LoginAsReturn, PasswordMark: s.PasswordMark}
}

// session is the stored session of r; its ID is filled in when its cookie comes back.
func (r record) session() *Session {
	return &Session{Token: r.Token, User: r.User, Look: r.Look, UserContext: r.Role,
		LastActivity: r.LastActivity, CombinedIP: r.IP, Language: r.Language, Theme: r.Theme,
		SortOrder: r.SortOrder, LoginAsReturn: r.LoginAsReturn, PasswordMark: r.PasswordMark}
}

// valid reports whether r is a session this panel could have saved.
func (r record) valid() bool {
	if !isHash(r.Key) || r.PasswordMark != "" && !isHash(r.PasswordMark) {
		return false
	}
	return validID(r.Token) && hestia.ValidUsername(r.User) &&
		(r.Look == "" || hestia.ValidUsername(r.Look)) &&
		(r.Role == "admin" || r.Role == "user") && !r.LastActivity.IsZero()
}

// isHash reports whether s is a hex SHA-256.
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Persist loads the sessions file at path and saves the signed-in sessions to it
// from now on (mode 0600, written whole and renamed into place). Sessions idle for
// longer than idle at now are dropped (idle 0: kept; RequireAuth still checks
// INACTIVE_SESSION_TIMEOUT on each request, and a user deleted or suspended while
// the panel was down). A missing file is an empty one. A file that can't be read
// starts the store empty and is replaced; the error says why, for the log, and the
// panel starts anyway. Call it once, before serving.
func (m *MemoryStore) Persist(path string, now time.Time, idle time.Duration) error {
	b, rerr := os.ReadFile(path)
	if errors.Is(rerr, os.ErrNotExist) {
		b, rerr = nil, nil
	}
	var f sessionsFile
	if rerr == nil && len(b) > 0 {
		rerr = json.Unmarshal(b, &f)
		if rerr == nil && f.Version != fileVersion {
			rerr = fmt.Errorf("format version %d, want %d", f.Version, fileVersion)
		}
	}

	m.mu.Lock()
	m.path = path
	if rerr == nil {
		for _, r := range f.Sessions {
			if !r.valid() || idle > 0 && now.After(r.LastActivity.Add(idle)) {
				continue
			}
			if _, ok := m.sessions[r.Key]; !ok {
				m.sessions[r.Key] = r.session()
			}
		}
		users := map[string]bool{}
		for _, s := range m.sessions {
			users[s.User] = true
		}
		for u := range users {
			m.capUser(u) // a file from before the cap
		}
	}
	m.mu.Unlock()
	m.saveLogged() // without what was dropped
	if rerr != nil {
		return fmt.Errorf("sessions file %s: %w", path, rerr)
	}
	return nil
}

// Close writes activity not saved yet and stops persisting: later changes stay in
// memory only. A stopping panel calls it once requests have drained; tests call it
// before their temp dir is removed. Safe to call more than once, or without Persist.
func (m *MemoryStore) Close() {
	m.mu.Lock()
	dirty := m.dirty
	m.mu.Unlock()
	if dirty {
		m.saveLogged()
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	m.path = ""
	m.mu.Unlock()
}

// saveLogged saves and logs a failure: the sessions still work, the change just
// won't survive a restart until a later save works.
func (m *MemoryStore) saveLogged() {
	if err := m.save(); err != nil {
		slog.Error("sessions not saved", "err", err)
	}
}

// saveRevocation saves a sign-out of user, trying once more at once when the write
// fails. If both fail the log says so: until a later save (the next sign-out, login
// sweep or Close) works, a restart would bring the session back.
func (m *MemoryStore) saveRevocation(user string) {
	if m.save() == nil {
		return
	}
	if err := m.save(); err != nil {
		slog.Error("sign-out not on disk yet; later saves keep trying", "user", user, "err", err)
	}
}

// save writes every signed-in session to the file, when Persist set one. On a
// failure the store stays dirty, so the next Sweep tries again.
func (m *MemoryStore) save() error {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	if m.path == "" {
		m.mu.Unlock()
		return nil
	}
	path := m.path
	write := m.writeFile
	if write == nil {
		write = writeFile
	}
	f := sessionsFile{Version: fileVersion, Sessions: []record{}}
	for k, s := range m.sessions {
		if persisted(s) {
			f.Sessions = append(f.Sessions, recordOf(k, s))
		}
	}
	m.dirty = false
	m.mu.Unlock()

	if err := write(path, f); err != nil {
		m.mu.Lock()
		m.dirty = true // try again at the next sweep
		m.mu.Unlock()
		return fmt.Errorf("sessions file %s: %w", path, err)
	}
	return nil
}

// ForgetUser ends user's sessions in the sessions file at path: it rewrites the file
// without them (owned as before, mode 0600) and returns how many it dropped. Run it
// only while the panel is stopped: a running panel rewrites the file from memory.
// A missing file has nothing to forget. A file that can't be read is removed, since
// the panel would start without it anyway (v-delete-sys-hostisan-sessions).
func ForgetUser(path, user string) (int, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var f sessionsFile
	if err := json.Unmarshal(b, &f); err != nil || f.Version != fileVersion {
		slog.Warn("sessions file not readable; removed", "file", path)
		return 0, os.Remove(path)
	}
	kept := []record{}
	for _, r := range f.Sessions {
		if r.User != user {
			kept = append(kept, r)
		}
	}
	n := len(f.Sessions) - len(kept)
	if n == 0 {
		return 0, nil
	}
	f.Sessions = kept
	uid, gid := -1, -1
	if fi, err := os.Stat(path); err == nil {
		uid, gid = fileOwner(fi)
	}
	return n, writeFileAs(path, f, uid, gid)
}

// writeFile writes f to path through a private temp file, synced and renamed into
// place (as the jobs file is).
func writeFile(path string, f sessionsFile) error {
	return writeFileAs(path, f, -1, -1)
}

// writeFileAs is writeFile with the new file owned by uid:gid (-1: the caller), so
// root editing hestiaweb's file leaves it hestiaweb's.
func writeFileAs(path string, f sessionsFile, uid, gid int) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sessions-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if uid >= 0 && (uid != os.Geteuid() || gid != os.Getegid()) {
		if err := tmp.Chown(uid, gid); err != nil {
			tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
