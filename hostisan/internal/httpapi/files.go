package httpapi

// The file-manager handoff (Slice C, parity decisions 14-16): the panel, running as
// hestiaweb like hestia-php, writes the PHP session FileGator reads and points the
// browser at /fm/. The file lives as long as the panel session and its look user.

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/phpsession"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

// filesSftpKeyTTL is the minutes the file manager's SFTP key lives (FM8.5).
const filesSftpKeyTTL = "30"

// filesState is each panel session's current PHP session file (server.files).
type filesState struct {
	mu   sync.Mutex
	open map[string]filesOpen // panel session id -> its PHP session
}

type filesOpen struct {
	id         string // PHP session id
	unregister func()
}

func newFilesState() *filesState { return &filesState{open: map[string]filesOpen{}} }

// filesState is the server's file-manager state, created on first use for a server
// built without newServer (route-only tests).
func (s *server) filesState() *filesState {
	s.toolsInit.Do(s.initToolsState)
	return s.files
}

// filesRoutes mounts the file-manager handoff.
func (s *server) filesRoutes(mux *http.ServeMux) {
	m := s.Sessions
	// Writable is checked in the handler, after the protected-admin rule, so that case
	// answers forbidden rather than read_only.
	mux.Handle("POST /api/v1/files/handoff", m.Load(m.RequireAuth(m.RequireCSRF(http.HandlerFunc(s.filesHandoff)))))
}

// filesHandoff answers POST /api/v1/files/handoff.
func (s *server) filesHandoff(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	if cfg["FILE_MANAGER"] != "true" {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if protectedAdminLook(sess, cfg) {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return
	}
	target := s.Sessions.Effective(sess)
	if !hestia.ValidUsername(target) {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return
	}
	u, ok, err := s.H.User(ctx, target)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	// D6: Hestia gave read-only sessions full write access in FileGator (FM9).
	if session.ReadOnly(sess, u, cfg) {
		WriteError(w, http.StatusForbidden, "read_only", "")
		return
	}

	// Run here rather than from FileGator, so a missing setfacl or a bad key path is
	// a panel error instead of a blank file manager (FM12, FM15).
	if err := s.run(ctx, "v-add-user-sftp-key", target, filesSftpKeyTTL); err != nil {
		var ce *runner.CommandError
		if errors.As(err, &ce) {
			WriteError(w, http.StatusConflict, "files_unavailable", commandMessage(ce))
			return
		}
		writeCommandError(w, err)
		return
	}

	// FM5's keys. No _sf2_* keys: FileGator starts at / after each handoff (FM22).
	id := phpsession.NewID()
	data := phpsession.Encode([]phpsession.KV{
		{Key: "user", Value: sess.User},
		{Key: "look", Value: sess.Look},
		{Key: "userContext", Value: sess.UserContext},
		{Key: "ROOT_USER", Value: rootUser(cfg)},
		{Key: "POLICY_SYSTEM_PROTECTED_ADMIN", Value: cfg["POLICY_SYSTEM_PROTECTED_ADMIN"]},
		{Key: "INACTIVE_SESSION_TIMEOUT", Value: cfg["INACTIVE_SESSION_TIMEOUT"]},
		{Key: "LAST_ACTIVITY", Value: s.now().Unix()},
		// FileGator logs a timed-out session out with it; never the panel's CSRF token.
		{Key: "token", Value: session.NewToken()},
		{Key: "language", Value: sess.Language},
	})
	dir := s.phpSessionDir()
	if err := phpsession.Write(dir, id, data); err != nil {
		serverError(w, err)
		return
	}
	if !s.filesTrack(sess, dir, id) {
		// The session ended or its look user changed while the handoff ran.
		WriteError(w, http.StatusUnauthorized, "session_expired", "")
		return
	}

	http.SetCookie(w, &http.Cookie{Name: phpSessionCookie, Value: id, Path: "/", HttpOnly: true,
		Secure: !s.Sessions.Dev, SameSite: http.SameSiteStrictMode})
	slog.Info("file manager opened", "user", sess.User, "target", target, "ip", session.RealIP(r))
	writeJSON(w, http.StatusOK, FilesHandoff{URL: "/fm/"})
}

// filesTrack makes id the session's PHP session file, deleting the previous one, and
// registers its deletion for when the session ends or its look user changes. It
// reports false, having deleted the file, if the session ended or changed meanwhile.
func (s *server) filesTrack(sess *session.Session, dir, id string) bool {
	st := s.filesState()
	remove := func() {
		if err := phpsession.Delete(dir, id); err != nil {
			slog.Warn("file manager session not deleted", "err", err)
		}
	}
	closer := func(string) {
		st.mu.Lock()
		if st.open[sess.ID].id == id {
			delete(st.open, sess.ID)
		}
		st.mu.Unlock()
		remove()
	}

	st.mu.Lock()
	prev, hadPrev := st.open[sess.ID]
	unregister := s.tools.register(sess.ID, toolKindFiles, closer)
	st.open[sess.ID] = filesOpen{id: id, unregister: unregister}
	st.mu.Unlock()

	if hadPrev {
		prev.unregister()
		if err := phpsession.Delete(dir, prev.id); err != nil {
			slog.Warn("file manager session not deleted", "err", err)
		}
	}
	// A session that ended (or changed look) before register has already run its
	// closers; this file's closer would never be called.
	if cur, ok := s.Sessions.Store.Get(sess.ID); !ok || cur.Look != sess.Look || cur.User != sess.User {
		unregister()
		closer("")
		return false
	}
	return true
}
