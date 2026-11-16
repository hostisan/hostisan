package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var persistNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// persistedStore is a store saving to sessions.json in a fresh temp dir.
func persistedStore(t *testing.T) (*MemoryStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.json")
	st := NewMemoryStore()
	t.Cleanup(st.Close) // runs before the temp dir is removed
	if err := st.Persist(path, persistNow, time.Hour); err != nil {
		t.Fatalf("Persist on a missing file: %v", err)
	}
	return st, path
}

// reload is a new store loading path, as the panel's next start does.
func reload(t *testing.T, path string, now time.Time, idle time.Duration) *MemoryStore {
	t.Helper()
	st := NewMemoryStore()
	t.Cleanup(st.Close)
	if err := st.Persist(path, now, idle); err != nil {
		t.Fatalf("Persist after a restart: %v", err)
	}
	return st
}

// sameSession compares two sessions, their times by instant.
func sameSession(a, b *Session) bool {
	x, y := *a, *b
	if !x.LastActivity.Equal(y.LastActivity) {
		return false
	}
	x.LastActivity, y.LastActivity = time.Time{}, time.Time{}
	return x == y
}

func signedIn(user string, last time.Time) *Session {
	return &Session{ID: NewToken(), Token: NewToken(), User: user, UserContext: "user",
		LastActivity: last, CombinedIP: "203.0.113.7", Language: "de", Theme: "dark", SortOrder: "name"}
}

// A panel restart (an update restarts hostisan.service) kept nobody signed in: the
// sessions were in memory only. Now a signed-in session, with its CSRF token, "log
// in as" state and preferences, answers to the same cookie after a restart.
func TestPersistedSessionSurvivesARestart(t *testing.T) {
	st, path := persistedStore(t)
	admin := &Session{ID: NewToken(), Token: NewToken(), User: "admin", UserContext: "admin", Look: "bob",
		LoginAsReturn: true, LastActivity: persistNow.Add(-time.Minute), CombinedIP: "198.51.100.2"}
	st.Save(admin)
	bob := signedIn("bob", persistNow.Add(-2*time.Minute))
	st.Save(bob)
	st.Close()

	again := reload(t, path, persistNow, time.Hour)
	for _, want := range []*Session{admin, bob} {
		got, ok := again.Get(want.ID)
		if !ok {
			t.Fatalf("session of %s lost in the restart", want.User)
		}
		if !sameSession(got, want) {
			t.Fatalf("session after restart = %+v, want %+v", got, want)
		}
	}
	// the same store, once more: still there, and Update and Delete work on it
	if !again.Update(bob.ID, func(s *Session) { s.Theme = "light" }) {
		t.Fatal("Update of a restored session failed")
	}
	again.Delete(admin.ID)
	again.Close()
	third := reload(t, path, persistNow, time.Hour)
	if got, ok := third.Get(bob.ID); !ok || got.Theme != "light" {
		t.Fatalf("bob after a second restart = %+v, %v", got, ok)
	}
	if _, ok := third.Get(admin.ID); ok {
		t.Fatal("a session deleted after a restart came back")
	}
}

// The session id is the cookie, a bearer credential: the file holds only its hash.
// A 2FA step holds the password, so it never reaches the file; neither does a
// signed-out session.
func TestPersistedFileHoldsNoSecretsItNeedNot(t *testing.T) {
	st, path := persistedStore(t)
	s := signedIn("bob", persistNow)
	st.Save(s)
	pending := &Session{ID: NewToken(), Token: NewToken(),
		Pending: &PendingLogin{Username: "carol", Password: "hunter2-secret", Expires: persistNow.Add(time.Minute)}}
	st.Save(pending)
	st.Save(&Session{ID: NewToken(), Token: NewToken()})
	st.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{s.ID, pending.ID, "hunter2-secret", "carol"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("sessions file holds %q: %s", secret, b)
		}
	}
	again := reload(t, path, persistNow, time.Hour)
	if _, ok := again.Get(pending.ID); ok {
		t.Fatal("a 2FA step survived the restart")
	}
	if _, ok := again.Get(s.ID); !ok {
		t.Fatal("the signed-in session is lost")
	}
}

// Sessions idle past INACTIVE_SESSION_TIMEOUT while the panel was down are dropped
// on load, and the file is rewritten without them.
func TestPersistedExpiredSessionsAreDroppedOnLoad(t *testing.T) {
	st, path := persistedStore(t)
	fresh := signedIn("bob", persistNow.Add(-50*time.Minute))
	stale := signedIn("carol", persistNow.Add(-61*time.Minute))
	st.Save(fresh)
	st.Save(stale)
	st.Close()

	again := reload(t, path, persistNow, time.Hour)
	if _, ok := again.Get(stale.ID); ok {
		t.Fatal("an expired session survived the restart")
	}
	if _, ok := again.Get(fresh.ID); !ok {
		t.Fatal("a live session was dropped")
	}
	again.Close()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "carol") {
		t.Fatalf("the expired session is still in the file: %s", b)
	}
	// an unknown timeout (0) keeps them: RequireAuth checks each request
	if _, ok := reload(t, path, persistNow.Add(24*time.Hour), 0).Get(fresh.ID); !ok {
		t.Fatal("idle 0 dropped a session")
	}
}

// Logout, a password change and a deleted user sign sessions out at once: a restart
// right after must not bring them back.
func TestPersistedSignOutsSurviveARestart(t *testing.T) {
	st, path := persistedStore(t)
	loggedOut := signedIn("bob", persistNow)
	deleted := signedIn("carol", persistNow)
	deleted2 := signedIn("carol", persistNow)
	kept := signedIn("carol", persistNow)
	old := signedIn("dave", persistNow)
	for _, s := range []*Session{loggedOut, deleted, deleted2, kept, old} {
		st.Save(s)
	}
	st.Delete(loggedOut.ID)            // logout
	st.DeleteUser("carol", kept.ID)    // password change keeps the changer's session
	newID := st.Regenerate(clone(old)) // login on an existing session
	// no Close: the changes must already be on disk (a crash, a kill -9)

	again := reload(t, path, persistNow, time.Hour)
	for _, gone := range []*Session{loggedOut, deleted, deleted2, old} {
		if _, ok := again.Get(gone.ID); ok {
			t.Fatalf("session of %s came back after the restart", gone.User)
		}
	}
	if _, ok := again.Get(kept.ID); !ok {
		t.Fatal("the session that changed the password is lost")
	}
	if s, ok := again.Get(newID); !ok || s.User != "dave" {
		t.Fatalf("regenerated session after restart = %+v, %v", s, ok)
	}
	// and a user deleted after the restart, on a session the store hasn't seen yet
	again.DeleteUser("carol", "")
	again.Close()
	if _, ok := reload(t, path, persistNow, time.Hour).Get(kept.ID); ok {
		t.Fatal("DeleteUser of a restored session did not reach the file")
	}
}

// A restored session the browser hasn't come back with yet has no open tools, so
// the end hook only ever gets ids it handed out.
func TestRestoredSessionsEndHookGetsKnownIDs(t *testing.T) {
	st, path := persistedStore(t)
	a, b := signedIn("bob", persistNow), signedIn("bob", persistNow)
	st.Save(a)
	st.Save(b)
	st.Close()
	again := reload(t, path, persistNow, time.Hour)
	var ended []string
	again.SetOnEnd(func(id string) { ended = append(ended, id) })
	again.Get(a.ID)
	again.DeleteUser("bob", "")
	if len(ended) != 1 || ended[0] != a.ID {
		t.Fatalf("ended = %q, want only %q", ended, a.ID)
	}
}

// Activity is saved without a write per request: the sweep (every minute) and Close
// (the panel stopping) write it.
func TestPersistedActivityIsSavedBySweepAndClose(t *testing.T) {
	st, path := persistedStore(t)
	s := signedIn("bob", persistNow.Add(-50*time.Minute))
	st.Save(s)
	later := persistNow.Add(-time.Minute)
	st.Update(s.ID, func(cur *Session) { cur.LastActivity = later })
	if got, _ := reload(t, path, persistNow, time.Hour).Get(s.ID); got == nil || !got.LastActivity.Equal(s.LastActivity) {
		t.Fatalf("activity alone was written at once: %+v", got)
	}
	st.Sweep(persistNow, time.Hour)
	if got, _ := reload(t, path, persistNow, time.Hour).Get(s.ID); got == nil || !got.LastActivity.Equal(later) {
		t.Fatalf("sweep did not save the activity: %+v", got)
	}
	even := later.Add(30 * time.Second)
	st.Update(s.ID, func(cur *Session) { cur.LastActivity = even })
	st.Close()
	if got, _ := reload(t, path, persistNow, time.Hour).Get(s.ID); got == nil || !got.LastActivity.Equal(even) {
		t.Fatalf("Close did not save the activity: %+v", got)
	}
}

// A file that can't be read starts the panel with no sessions (everyone signs in
// again) and is replaced; startup never fails over it.
func TestPersistedCorruptFileStartsEmpty(t *testing.T) {
	for name, body := range map[string]string{
		"garbage":   "{not json",
		"bad entry": `{"version":1,"sessions":[{"key":"zz","user":"bob","role":"user","last_activity":"2026-11-04T11:59:00Z"}]}`,
		"bad user":  `{"version":1,"sessions":[{"key":"` + strings.Repeat("a", 64) + `","user":"../x","role":"user","last_activity":"2026-11-04T11:59:00Z"}]}`,
		"bad role":  `{"version":1,"sessions":[{"key":"` + strings.Repeat("a", 64) + `","user":"bob","role":"root","last_activity":"2026-11-04T11:59:00Z"}]}`,
		"no time":   `{"version":1,"sessions":[{"key":"` + strings.Repeat("a", 64) + `","user":"bob","role":"user"}]}`,
		"version":   `{"version":2,"sessions":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			st := NewMemoryStore()
			defer st.Close()
			err := st.Persist(path, persistNow, time.Hour)
			if name == "garbage" || name == "version" {
				if err == nil {
					t.Fatal("no error for an unreadable file")
				}
			}
			if n := st.Len(); n != 0 {
				t.Fatalf("%d sessions loaded from a bad file", n)
			}
			// it works from now on, and the bad file is gone
			s := signedIn("bob", persistNow)
			st.Save(s)
			if _, ok := reload(t, path, persistNow, time.Hour).Get(s.ID); !ok {
				t.Fatal("sessions are not saved after a bad file")
			}
		})
	}
}

// The file and its temp files are private to the panel user: they hold CSRF tokens
// and who is signed in from where.
func TestPersistedFileIsPrivate(t *testing.T) {
	st, path := persistedStore(t)
	st.Save(signedIn("bob", persistNow))
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	// an existing file with a wider mode is replaced by a private one
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	st.Save(signedIn("carol", persistNow))
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode after a save = %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
}

// Without Persist (development, no --state-dir) the store stays in memory.
func TestMemoryStoreWithoutPersistWritesNothing(t *testing.T) {
	dir := t.TempDir()
	st := NewMemoryStore()
	st.Save(signedIn("bob", persistNow))
	st.Sweep(persistNow, time.Hour)
	st.Close()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files written: %v", entries)
	}
}

// The password mark survives a restart, so a password changed outside the panel
// while it was down still ends the session at its next request.
func TestPersistedPasswordMarkSurvivesARestart(t *testing.T) {
	st, path := persistedStore(t)
	s := signedIn("bob", persistNow)
	s.PasswordMark = strings.Repeat("ab", 32)
	st.Save(s)
	st.Close()
	got, ok := reload(t, path, persistNow, time.Hour).Get(s.ID)
	if !ok || got.PasswordMark != s.PasswordMark {
		t.Fatalf("restored session = %+v, %v", got, ok)
	}
	// an entry with a mark that is not a hash is dropped
	body := `{"version":1,"sessions":[{"key":"` + strings.Repeat("a", 64) + `","token":"` + NewToken() +
		`","user":"bob","role":"user","last_activity":"2026-11-04T11:59:00Z","pw":"plain"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := reload(t, path, persistNow, time.Hour).Len(); n != 0 {
		t.Fatalf("%d sessions with a bad mark loaded", n)
	}
}

// failWrites makes the store's next n file writes fail (n < 0: all of them) and
// returns a counter of the writes tried.
func failWrites(st *MemoryStore, n int) *int {
	tried := 0
	st.writeFile = func(path string, f sessionsFile) error {
		tried++
		if n != 0 {
			n--
			return errors.New("disk full")
		}
		return writeFile(path, f)
	}
	return &tried
}

// captureLog sends slog's output to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// A sign-out whose save fails is tried again at once; when that fails too, the log
// says whose sign-out is not on disk yet, and the next saves keep trying.
func TestPersistedSignOutSaveIsRetried(t *testing.T) {
	logs := captureLog(t)
	st, path := persistedStore(t)
	bob, carol, dave := signedIn("bob", persistNow), signedIn("carol", persistNow), signedIn("dave", persistNow)
	for _, s := range []*Session{bob, carol, dave} {
		st.Save(s)
	}

	tried := failWrites(st, 1)
	st.Delete(bob.ID)
	if *tried != 2 {
		t.Fatalf("%d writes for a logout whose first save failed, want 2", *tried)
	}
	if _, ok := reload(t, path, persistNow, time.Hour).Get(bob.ID); ok {
		t.Fatal("the retried logout is not on disk")
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("a save that worked on the retry was logged as failed: %s", logs)
	}

	tried = failWrites(st, 2)
	st.DeleteUser("carol", "")
	if *tried != 2 {
		t.Fatalf("%d writes for DeleteUser, want 2", *tried)
	}
	if !strings.Contains(logs.String(), "user=carol") || !strings.Contains(logs.String(), "not on disk") {
		t.Fatalf("log does not say whose sign-out is not saved: %s", logs)
	}
	if _, ok := reload(t, path, persistNow, time.Hour).Get(carol.ID); !ok {
		t.Fatal("test setup: the failed save reached the disk")
	}
	// the minute sweep tries again
	st.Sweep(persistNow, time.Hour)
	if _, ok := reload(t, path, persistNow, time.Hour).Get(carol.ID); ok {
		t.Fatal("the sweep did not save the sign-out")
	}
	_ = dave
}

// endSession (Validate's sign-outs) goes through Delete: retried the same way.
func TestPersistedEndSessionSaveIsRetried(t *testing.T) {
	captureLog(t)
	st, path := persistedStore(t)
	s := signedIn("bob", persistNow)
	st.Save(s)
	tried := failWrites(st, 1)
	m := &Manager{Store: st}
	if _, _, err := m.endSession(httptest.NewRequest("GET", "/", nil), s, false, "session_expired", ""); err != nil {
		t.Fatal(err)
	}
	if *tried != 2 {
		t.Fatalf("%d writes, want 2", *tried)
	}
	if _, ok := reload(t, path, persistNow, time.Hour).Get(s.ID); ok {
		t.Fatal("ended session came back")
	}
}

// A login is written by the next sweep or Close, not at once: a loop of logins as
// one user does not rewrite and fsync the file each time. A sign-out still is.
func TestPersistedLoginIsSavedLater(t *testing.T) {
	st, path := persistedStore(t)
	tried := failWrites(st, 0)
	var ids []string
	for i := range 5 {
		s := &Session{ID: NewToken(), Token: NewToken(), User: "bob", UserContext: "user",
			LastActivity: persistNow.Add(time.Duration(i) * time.Second), CombinedIP: "203.0.113.7"}
		ids = append(ids, st.Regenerate(s))
	}
	if *tried != 0 {
		t.Fatalf("%d writes for 5 logins, want 0", *tried)
	}
	st.Sweep(persistNow, time.Hour)
	if *tried != 1 {
		t.Fatalf("%d writes by the sweep, want 1", *tried)
	}
	for _, id := range ids {
		if _, ok := reload(t, path, persistNow, time.Hour).Get(id); !ok {
			t.Fatal("login not saved by the sweep")
		}
	}
	st.Delete(ids[0])
	if *tried != 2 {
		t.Fatalf("logout not written at once: %d writes", *tried)
	}
}

// A user keeps at most maxSessionsPerUser sessions: a new login drops the oldest,
// and its end hook runs. Other users' sessions are untouched.
func TestSessionsPerUserAreCapped(t *testing.T) {
	st, path := persistedStore(t)
	var ended []string
	st.SetOnEnd(func(id string) { ended = append(ended, id) })
	other := signedIn("carol", persistNow.Add(-time.Hour))
	st.Save(other)
	var ids []string
	for i := range maxSessionsPerUser + 3 {
		s := &Session{ID: NewToken(), Token: NewToken(), User: "bob", UserContext: "user",
			LastActivity: persistNow.Add(time.Duration(i) * time.Second), CombinedIP: "203.0.113.7"}
		ids = append(ids, st.Regenerate(s))
	}
	if n := st.Len(); n != maxSessionsPerUser+1 {
		t.Fatalf("%d sessions, want %d", n, maxSessionsPerUser+1)
	}
	for i, id := range ids {
		_, ok := st.Get(id)
		if want := i >= 3; ok != want {
			t.Fatalf("login %d kept = %v, want %v", i, ok, want)
		}
	}
	if len(ended) != 3 || ended[0] != ids[0] {
		t.Fatalf("ended = %v", ended)
	}
	if _, ok := st.Get(other.ID); !ok {
		t.Fatal("another user's session was dropped")
	}
	// a file with more than the cap (an older build) is cut down on load
	st.Close()
	var f sessionsFile
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &f)
	extra := f.Sessions[0]
	for i := range 5 {
		extra.Key = strings.Repeat(string(rune('a'+i)), 64)
		extra.User = "dave"
		extra.LastActivity = persistNow.Add(time.Duration(i) * time.Minute)
		f.Sessions = append(f.Sessions, extra)
	}
	for i := range maxSessionsPerUser {
		extra.Key = fmt.Sprintf("%064x", i+1)
		extra.LastActivity = persistNow.Add(-time.Duration(i+1) * time.Minute)
		f.Sessions = append(f.Sessions, extra)
	}
	b, _ = json.Marshal(f)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	again := reload(t, path, persistNow, 24*time.Hour)
	daves := 0
	again.mu.Lock()
	for _, s := range again.sessions {
		if s.User == "dave" {
			daves++
		}
	}
	_, newest := again.sessions[strings.Repeat("e", 64)]
	again.mu.Unlock()
	if daves != maxSessionsPerUser || !newest {
		t.Fatalf("dave has %d sessions after load (newest kept %v), want %d", daves, newest, maxSessionsPerUser)
	}
}

// rc8 N2: a user deleted over SSH (v-delete-user) kept their saved sessions. Their
// cookie is still refused, before and after a panel restart: Validate asks
// v-list-user, which says the user is gone, and ends the session on disk too.
func TestDeletedUsersCookieIsRefused(t *testing.T) {
	for _, restart := range []bool{false, true} {
		st, path := persistedStore(t)
		ghost := signedIn("ghost", persistNow) // not in newManager's users
		st.Save(ghost)
		if restart {
			st.Close()
			st = reload(t, path, persistNow, time.Hour)
		}
		m, _, now := newManager(t, nil, nil)
		m.Store, *now = st, persistNow
		ghost.CombinedIP = "203.0.113.9"
		st.Update(ghost.ID, func(s *Session) { s.CombinedIP = ghost.CombinedIP })
		expectDestroyed(t, m, serve(m.Load(m.RequireAuth(ok)), request("GET", ghost.ID)), ghost.ID, "session_expired")
		if _, ok := reload(t, path, persistNow, time.Hour).Get(ghost.ID); ok {
			t.Fatalf("restart %v: the deleted user's session is still on file", restart)
		}
	}
}

// The minute sweep drops the saved sessions of users who no longer exist, also
// those whose browser never came back, with one v-list-user per stored user at
// most every userCheckEvery.
func TestSweepDropsDeletedUsersSessions(t *testing.T) {
	st, path := persistedStore(t)
	st.Save(signedIn("ghost", persistNow))
	st.Save(signedIn("ghost", persistNow))
	bob := signedIn("bob", persistNow)
	st.Save(bob)
	st.Close()
	st = reload(t, path, persistNow, time.Hour) // ids unknown until the browsers return
	m, f, now := newManager(t, nil, nil)
	m.Store, *now = st, persistNow
	m.Sweep(context.Background())
	if n := st.Len(); n != 1 {
		t.Fatalf("%d sessions after the sweep, want bob's only", n)
	}
	if _, ok := st.Get(bob.ID); !ok {
		t.Fatal("the sweep dropped an existing user's session")
	}
	if n := reload(t, path, persistNow, time.Hour).Len(); n != 1 {
		t.Fatalf("%d sessions on file after the sweep, want 1", n)
	}
	lists := func() int {
		n := 0
		for _, c := range f.Calls() {
			if c.Cmd == "v-list-user" {
				n++
			}
		}
		return n
	}
	if n := lists(); n != 2 {
		t.Fatalf("%d v-list-user calls, want one per stored user (2)", n)
	}
	*now = now.Add(time.Minute)
	m.Sweep(context.Background())
	if n := lists(); n != 2 {
		t.Fatalf("%d v-list-user calls after a second sweep a minute later, want still 2", n)
	}
	*now = now.Add(userCheckEvery)
	m.Sweep(context.Background())
	if n := lists(); n != 3 {
		t.Fatalf("%d v-list-user calls after userCheckEvery, want 3", n)
	}
}
