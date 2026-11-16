// Package session holds signed-in browser sessions, ported from the PHP panel's
// $_SESSION handling in web/inc/main.php and web/login/index.php.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

type Session struct {
	ID    string
	Token string // CSRF token; also Hestia's login "fingerprint"

	User        string
	Look        string // user an admin is viewing as ("log in as"); "" otherwise
	UserContext string // Hestia ROLE of User: "admin" or "user"

	LastActivity time.Time
	CombinedIP   string
	Language     string
	Theme        string
	SortOrder    string

	Pending       *PendingLogin // password checked, 2FA code still needed
	LoginAsReturn bool

	// PasswordMark is a hash of User's stored password hash when the session was
	// first checked (see Manager.Validate); "" until then. A password changed
	// outside the panel changes it and ends the session.
	PasswordMark string
}

// PendingLogin is held in memory only, between the password and the 2FA step.
type PendingLogin struct {
	Username string
	Password string
	Expires  time.Time
}

// PendingLoginTTL is how long a password stays valid while the 2FA code is entered.
const PendingLoginTTL = 5 * time.Minute

type Store interface {
	Get(id string) (*Session, bool)
	Save(s *Session)
	Delete(id string)
	// Update changes a stored session in place and reports whether it still exists; it
	// never recreates a deleted one. Handlers change sessions through Update, so a
	// parallel request can't overwrite their change or undo a logout.
	Update(id string, f func(*Session)) bool
	// Regenerate gives the session a new ID (after login) and drops the old one.
	Regenerate(s *Session) string
	// Sweep deletes signed-in sessions idle for longer than idle (0: keep them) and
	// signed-out ones whose 2FA step has expired.
	Sweep(now time.Time, idle time.Duration)
	// DeleteUser signs user out everywhere except the session exceptID ("" for none):
	// it deletes every session whose signed-in User is user. Sessions of admins
	// looking at user are kept. An empty user matches nothing. It returns the
	// key (see key) of each session it deleted.
	DeleteUser(user, exceptID string) []string
	// Users lists the signed-in users of the stored sessions, each once.
	Users() []string
}

// MemoryStore keeps sessions in memory. With Persist, the signed-in ones are also
// saved to a file, so a panel restart (an update restarts hostisan.service) signs
// nobody out; without it a restart signs everyone out. So a restart is no longer a
// way to end sessions, and removing the file before one doesn't work either (Close
// writes it as the panel stops): admins run v-delete-sys-hostisan-sessions all|USER,
// which stops the panel, edits the file (ForgetUser, `hostisan sessions forget`)
// and starts it again. A password changed through Hestia ends sessions by itself
// (Manager.Validate's password mark).
//
// Sessions are held by key(id), the SHA-256 of the cookie id: the file stores only
// that. A session loaded from the file has no ID until its browser comes back with
// the cookie (Get, Update fill it in).
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]*Session // by key(ID)
	onEnd    func(id string)

	// Persist; see persist.go. mu guards path and dirty.
	saveMu    sync.Mutex
	path      string
	dirty     bool                                    // changed since the last save
	writeFile func(path string, f sessionsFile) error // tests make it fail; nil: writeFile
}

// maxSessionsPerUser is how many signed-in sessions one user keeps: a login past it
// ends the user's least recently used one, so logins in a loop can't grow the store
// and its file without end.
const maxSessionsPerUser = 20

// SetOnEnd registers f, called once for every stored session the store removes:
// Delete, Sweep, DeleteUser, and the old id in Regenerate. It runs after the store's
// lock is released, so f may use the store. Plan 5 closes a session's web terminals
// and file-manager handoff through it. A session loaded from the file and not yet
// seen again has no id and nothing open, so f is not called for it.
func (m *MemoryStore) SetOnEnd(f func(id string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEnd = f
}

// ended calls the end hook for ids; the caller must not hold the lock.
func (m *MemoryStore) ended(f func(id string), ids []string) {
	if f == nil {
		return
	}
	for _, id := range ids {
		f(id)
	}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[string]*Session{}}
}

// key is the map and file key of a session id: hex SHA-256. The id is the cookie, a
// bearer credential, so the file never holds it.
func key(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// lookup returns the stored session of id, filling in the id of one loaded from
// the file. The caller holds the lock.
func (m *MemoryStore) lookup(id string) (*Session, bool) {
	s, ok := m.sessions[key(id)]
	if ok && s.ID == "" {
		s.ID = id
	}
	return s, ok
}

// Get returns a copy, so concurrent requests on one session never share a struct.
func (m *MemoryStore) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.lookup(id)
	if !ok {
		return nil, false
	}
	return clone(s), true
}

func (m *MemoryStore) Save(s *Session) {
	m.mu.Lock()
	k := key(s.ID)
	old, had := m.sessions[k]
	m.sessions[k] = clone(s)
	save := persisted(s) || had && persisted(old)
	var gone []string
	if persisted(s) {
		gone = m.capUser(s.User)
	}
	f := m.onEnd
	m.mu.Unlock()
	if save {
		m.saveLogged()
	}
	m.ended(f, gone)
}

// Delete is a sign-out (logout, or Validate ending the session): saved at once.
func (m *MemoryStore) Delete(id string) {
	m.mu.Lock()
	s, ok := m.lookup(id)
	delete(m.sessions, key(id))
	f := m.onEnd
	m.mu.Unlock()
	if ok {
		if persisted(s) {
			m.saveRevocation(s.User)
		}
		m.ended(f, []string{id})
	}
}

// Update saves at once unless only the activity changed: that is written by the
// next Sweep (every minute) or Close, not on every request.
func (m *MemoryStore) Update(id string, f func(*Session)) bool {
	m.mu.Lock()
	s, ok := m.lookup(id)
	save := false
	if ok {
		before := recordOf("", s)
		f(s)
		after := recordOf("", s)
		if !before.LastActivity.Equal(after.LastActivity) || before.PasswordMark != after.PasswordMark {
			m.dirty = m.dirty || persisted(s)
		}
		before.LastActivity, after.LastActivity = time.Time{}, time.Time{}
		before.PasswordMark, after.PasswordMark = "", ""
		save = before != after
	}
	m.mu.Unlock()
	if save {
		m.saveLogged()
	}
	return ok
}

// Regenerate is a login. The new session is written by the next Sweep or Close,
// not at once, so logins in a loop don't rewrite and sync the file each time (a
// crash before then costs that user one more login). Dropping a signed-in session
// is a sign-out and is saved at once.
func (m *MemoryStore) Regenerate(s *Session) string {
	m.mu.Lock()
	old := s.ID
	prev, ok := m.lookup(old)
	delete(m.sessions, key(old))
	s.ID = NewToken()
	m.sessions[key(s.ID)] = clone(s)
	var gone []string
	if persisted(s) {
		gone = m.capUser(s.User)
		m.dirty = true
	}
	f := m.onEnd
	revoked := ok && persisted(prev)
	m.mu.Unlock()
	if revoked {
		m.saveRevocation(prev.User)
	}
	if ok {
		gone = append(gone, old)
	}
	m.ended(f, gone)
	return s.ID
}

// capUser drops user's least recently used signed-in sessions past
// maxSessionsPerUser and returns the ids of those that had one. The caller holds
// the lock; the drop is saved with the caller's change.
func (m *MemoryStore) capUser(user string) []string {
	var keys []string
	for k, s := range m.sessions {
		if s.User == user {
			keys = append(keys, k)
		}
	}
	if len(keys) <= maxSessionsPerUser {
		return nil
	}
	sort.Slice(keys, func(i, j int) bool {
		return m.sessions[keys[i]].LastActivity.Before(m.sessions[keys[j]].LastActivity)
	})
	var gone []string
	for _, k := range keys[:len(keys)-maxSessionsPerUser] {
		if id := m.sessions[k].ID; id != "" {
			gone = append(gone, id)
		}
		delete(m.sessions, k)
		m.dirty = true
	}
	return gone
}

func (m *MemoryStore) Sweep(now time.Time, idle time.Duration) {
	m.mu.Lock()
	var gone []string
	save := m.dirty
	for k, s := range m.sessions {
		if s.User == "" && (s.Pending == nil || now.After(s.Pending.Expires)) ||
			s.User != "" && idle > 0 && now.After(s.LastActivity.Add(idle)) {
			delete(m.sessions, k)
			save = save || persisted(s)
			if s.ID != "" {
				gone = append(gone, s.ID)
			}
		}
	}
	f := m.onEnd
	m.mu.Unlock()
	if save {
		m.saveLogged()
	}
	m.ended(f, gone)
}

func (m *MemoryStore) DeleteUser(user, exceptID string) []string {
	if user == "" {
		return nil
	}
	m.mu.Lock()
	var gone, keys []string
	except := ""
	if exceptID != "" {
		except = key(exceptID)
	}
	removed := false
	for k, s := range m.sessions {
		if s.User == user && k != except {
			delete(m.sessions, k)
			removed = true
			keys = append(keys, k)
			if s.ID != "" {
				gone = append(gone, s.ID)
			}
		}
	}
	f := m.onEnd
	m.mu.Unlock()
	if removed {
		m.saveRevocation(user)
	}
	m.ended(f, gone)
	return keys
}

func (m *MemoryStore) Users() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var users []string
	for _, s := range m.sessions {
		if s.User != "" && !seen[s.User] {
			seen[s.User] = true
			users = append(users, s.User)
		}
	}
	sort.Strings(users)
	return users
}

// Len is the number of stored sessions.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func clone(s *Session) *Session {
	c := *s
	if s.Pending != nil {
		p := *s.Pending
		c.Pending = &p
	}
	return &c
}

// NewToken returns 16 random bytes as hex, like PHP's bin2hex(random_bytes(16)).
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("session: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)
}
