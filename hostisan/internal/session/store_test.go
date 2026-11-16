package session

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestNewTokenIs32Hex(t *testing.T) {
	tok := NewToken()
	if len(tok) != 32 {
		t.Fatalf("len = %d, want 32", len(tok))
	}
	for _, r := range tok {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			t.Fatalf("token %q is not lowercase hex", tok)
		}
	}
	if NewToken() == tok {
		t.Fatal("two tokens are equal")
	}
}

func TestStoreGetReturnsCopy(t *testing.T) {
	st := NewMemoryStore()
	s := &Session{ID: "a", User: "admin"}
	st.Save(s)
	got, _ := st.Get("a")
	got.User = "mallory"
	again, _ := st.Get("a")
	if again.User != "admin" {
		t.Fatal("changing a session returned by Get changed the stored one")
	}
}

func TestStoreRegenerateMovesSession(t *testing.T) {
	st := NewMemoryStore()
	st.Save(&Session{ID: "old", User: "bob", Token: "t"})
	s, _ := st.Get("old")
	newID := st.Regenerate(s)
	if newID == "old" || newID == "" {
		t.Fatalf("new id = %q", newID)
	}
	if _, ok := st.Get("old"); ok {
		t.Fatal("old id still resolves after Regenerate")
	}
	moved, ok := st.Get(newID)
	if !ok || moved.User != "bob" || moved.Token != "t" {
		t.Fatalf("regenerated session = %+v, %v", moved, ok)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	st := NewMemoryStore()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("s%d", i)
			st.Save(&Session{ID: id, User: id})
			s, ok := st.Get(id)
			if !ok {
				t.Errorf("lost session %s", id)
				return
			}
			newID := st.Regenerate(s)
			if got, ok := st.Get(newID); !ok || got.User != id {
				t.Errorf("lost session %s after regenerate", id)
			}
		}(i)
	}
	wg.Wait()
}

func TestStoreUpdateChangesOnlyWhatItSets(t *testing.T) {
	st := NewMemoryStore()
	st.Save(&Session{ID: "a", User: "admin", Look: "bob"})
	if !st.Update("a", func(s *Session) { s.Language = "fr" }) {
		t.Fatal("Update of a stored session returned false")
	}
	got, _ := st.Get("a")
	if got.Look != "bob" || got.Language != "fr" {
		t.Fatalf("after Update: %+v", got)
	}
}

func TestStoreUpdateNeverRecreatesDeletedSession(t *testing.T) {
	st := NewMemoryStore()
	st.Save(&Session{ID: "a", User: "admin"})
	st.Delete("a")
	if st.Update("a", func(s *Session) { s.User = "admin" }) {
		t.Fatal("Update of a deleted session returned true")
	}
	if _, ok := st.Get("a"); ok {
		t.Fatal("Update brought a deleted session back")
	}
}

func TestStoreDeleteUser(t *testing.T) {
	st := NewMemoryStore()
	st.Save(&Session{ID: "current", User: "bob"})
	st.Save(&Session{ID: "phone", User: "bob"})
	st.Save(&Session{ID: "laptop", User: "bob", Language: "fr"})
	st.Save(&Session{ID: "admin", User: "admin"})
	st.Save(&Session{ID: "looking", User: "admin", Look: "bob", UserContext: "admin"})
	st.Save(&Session{ID: "anon"})

	var s Store = st
	s.DeleteUser("bob", "current")

	for id, want := range map[string]bool{"current": true, "phone": false, "laptop": false, "admin": true, "looking": true, "anon": true} {
		if _, ok := st.Get(id); ok != want {
			t.Errorf("session %s present = %v, want %v", id, ok, want)
		}
	}
	// No exception: every session of the user goes.
	st.DeleteUser("bob", "")
	if _, ok := st.Get("current"); ok {
		t.Error("DeleteUser with no exception kept a session")
	}
	// An empty user never matches the signed-out sessions.
	st.DeleteUser("", "")
	if _, ok := st.Get("anon"); !ok {
		t.Error("DeleteUser(\"\") removed a signed-out session")
	}
}

func TestStoreOnEnd(t *testing.T) {
	m := NewMemoryStore()
	var ended []string
	m.SetOnEnd(func(id string) {
		// Called outside the lock: the store must be usable from the callback.
		m.Get(id)
		ended = append(ended, id)
	})
	now := time.Now()
	add := func(id, user string, last time.Time) {
		m.Save(&Session{ID: id, User: user, LastActivity: last})
	}
	expect := func(step string, want ...string) {
		t.Helper()
		slices.Sort(ended)
		slices.Sort(want)
		if !slices.Equal(ended, want) {
			t.Fatalf("%s: ended %v, want %v", step, ended, want)
		}
		ended = nil
	}

	add("a", "bob", now)
	m.Delete("a")
	expect("Delete", "a")
	m.Delete("a")
	expect("Delete of a missing id")

	add("idle", "bob", now.Add(-2*time.Hour))
	add("fresh", "bob", now)
	m.Sweep(now, time.Hour)
	expect("Sweep", "idle")

	add("b1", "carol", now)
	add("b2", "carol", now)
	add("b3", "carol", now)
	add("other", "alice", now)
	m.DeleteUser("carol", "b3")
	expect("DeleteUser", "b1", "b2")

	s := &Session{ID: "old", User: "bob", LastActivity: now}
	m.Save(s)
	newID := m.Regenerate(s)
	expect("Regenerate", "old")
	if newID == "old" {
		t.Fatal("Regenerate kept the id")
	}

	m.Update("fresh", func(s *Session) { s.Look = "x" })
	m.Save(&Session{ID: "saved", User: "bob"})
	m.Get("fresh")
	expect("Update/Save/Get")

	// An anonymous session that was never stored isn't reported by Regenerate.
	m.Regenerate(&Session{ID: "never-stored"})
	expect("Regenerate of an unstored session")
}
