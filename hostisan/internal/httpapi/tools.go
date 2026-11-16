package httpapi

// Tools (Plan 5): the shared pieces every tools slice uses. Each slice fills its own
// route file: search.go and restore_files.go (Slice A), terminal.go and files.go
// (Slice C), legacy_api.go and reset_mail.go (Slice D). New identifiers carry the
// slice prefix: search…, restore…, term…, files…, legacy…, resetMail….

import (
	"net/http"
	"sync"

	"github.com/hostisan/hostisan/internal/i18n"
)

// toolsSessionExpired is the close reason every open tool gets when its panel
// session ends or its look user changes (parity decision 11, WT27).
var toolsSessionExpired = i18n.N("Your session has expired.")

// phpSessionCookie is hestia-php's session cookie, which FileGator reads (FM5).
const phpSessionCookie = "HESTIASID"

// Tool kinds registered with toolSessions.
const (
	toolKindTerminal = "terminal"
	toolKindFiles    = "files"
)

// toolSessions tracks what each panel session has open outside the panel: terminal
// relays and the file manager's PHP session file. Logout, a look change and every way
// the session store drops a session (expiry, sweep, DeleteUser, Regenerate) close them
// at once.
type toolSessions struct {
	mu   sync.Mutex
	next uint64
	open map[string]map[uint64]toolEntry // session id -> entry id -> entry
}

type toolEntry struct {
	kind  string
	close func(reason string)
}

func newToolSessions() *toolSessions {
	return &toolSessions{open: map[string]map[uint64]toolEntry{}}
}

// register records closer for the session sessID; kind is toolKindTerminal or
// toolKindFiles. The returned func removes it (call it when the tool closes on its
// own); calling it again, or after the session ended, does nothing.
func (t *toolSessions) register(sessID, kind string, closer func(reason string)) (unregister func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	id := t.next
	if t.open[sessID] == nil {
		t.open[sessID] = map[uint64]toolEntry{}
	}
	t.open[sessID][id] = toolEntry{kind: kind, close: closer}
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if m := t.open[sessID]; m != nil {
			delete(m, id)
			if len(m) == 0 {
				delete(t.open, sessID)
			}
		}
	}
}

// sessionEnded closes every tool of session id: the session store removed it.
func (t *toolSessions) sessionEnded(id string) { t.closeAll(id) }

// lookChanged closes every tool of session id: the admin started or stopped viewing
// as another user, so what was opened for the old user must not stay open.
func (t *toolSessions) lookChanged(id string) { t.closeAll(id) }

// closeAll removes the session's entries, then calls each closer outside the lock
// (a closer may register or unregister).
func (t *toolSessions) closeAll(id string) {
	t.mu.Lock()
	entries := t.open[id]
	delete(t.open, id)
	t.mu.Unlock()
	for _, e := range entries {
		e.close(toolsSessionExpired)
	}
}

// registerToolsRoutes mounts every tools slice's routes. It runs before the /api/
// catch-all is registered.
func (s *server) registerToolsRoutes(mux *http.ServeMux) {
	s.searchRoutes(mux)
	s.restoreRoutes(mux)
	s.termRoutes(mux)
	s.filesRoutes(mux)
	s.legacyRoutes(mux)
	s.resetMailRoutes(mux)
}
