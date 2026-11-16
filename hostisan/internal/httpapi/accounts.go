package httpapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

// registerAccountRoutes mounts account management (Plan 3). Each slice registers its
// own routes from its own file.
func (s *server) registerAccountRoutes(mux *http.ServeMux) {
	s.userRoutes(mux)     // users.go: users and packages (Slice A)
	s.profileRoutes(mux)  // profile.go: account, user editing, 2FA enrolment (Slice B)
	s.recoveryRoutes(mux) // recovery.go: password reset and 2FA reset (Slice C)
	s.keyRoutes(mux)      // keys.go: SSH keys, access keys, notifications, logs (Slice D)
	s.statsRoutes(mux)    // stats.go: statistics (Slice D)
	s.cronRoutes(mux)     // cron.go: cron jobs (Slice E)
	s.backupRoutes(mux)   // backup.go: backups and snapshots (Slice F)
}

// accountsState is Plan 3's in-memory state, on server.acct. Hold mu while reading or
// writing any map; a nil map is created on first write. Windows are measured with
// s.now(), so tests can move the clock.
type accountsState struct {
	mu sync.Mutex

	// Recovery throttles (Parity 16 and 17), Slice C. resetEmails is the last email sent
	// per flow and user, keyed "reset:<user>" or "2fa:<user>" (one per 15 minutes).
	// resetRequests and resetConfirms are the recent request and confirm times per IP
	// (10 and 20 per hour).
	resetEmails   map[string]time.Time
	resetRequests map[string][]time.Time
	resetConfirms map[string][]time.Time
	swept         time.Time // when the three maps above last dropped aged-out keys

	// pending2FA is the 2FA secret being enrolled, by session ID, until it is confirmed
	// or expires (10 minutes), Slice B.
	pending2FA map[string]acctPending2FA
}

// acctPending2FA is one 2FA enrolment waiting for its confirmation code.
type acctPending2FA struct {
	User    string
	Secret  string
	Expires time.Time
}

// acctUserCommandTimeout bounds the long user commands (v-add-user, v-delete-user,
// v-suspend-user, v-unsuspend-user, v-rebuild-*, v-change-user-package): under the
// 90 s HTTP write timeout. Use runner.WithTimeout(ctx, acctUserCommandTimeout).
const acctUserCommandTimeout = 85 * time.Second

// acctAdminRead wraps an admin-only read: signed in and an admin (users get 403).
func (s *server) acctAdminRead(h http.HandlerFunc) http.Handler {
	m := s.Sessions
	return m.Load(m.RequireAuth(m.RequireAdmin(h)))
}

// acctNotLooking refuses (403) a Users or Packages page while the admin is logged in
// as another user: Hestia drops those pages then (owner ruling 12).
func acctNotLooking(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sess := session.FromContext(r.Context()); sess == nil || sess.Look != "" {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		h(w, r)
	}
}

// acctPageRead and acctPageWrite wrap the Users and Packages pages' routes.
func (s *server) acctPageRead(h http.HandlerFunc) http.Handler {
	return s.acctAdminRead(acctNotLooking(h))
}

func (s *server) acctPageWrite(h http.HandlerFunc) http.Handler {
	return s.adminWrite(acctNotLooking(h))
}

// acctUserParam is the {user} path value; it answers 400 itself when unusable.
func acctUserParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	u := r.PathValue("user")
	if !hestia.ValidUsername(u) {
		invalidFormat(w, "user", u)
		return "", false
	}
	return u, true
}

// acctPackageParam is the {package} path value; it answers 400 itself when unusable.
func acctPackageParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := r.PathValue("package")
	if !hestia.ValidPackageArg(p) {
		invalidFormat(w, "package", p)
		return "", false
	}
	return p, true
}

// acctIsSelf reports only whether the effective user is the signed-in user (no
// login-as look). It knows nothing about a {user} path parameter: a handler on a
// /users/{user} route must also compare the target with sess.User, and ask for the
// current password only when both hold. An admin editing another user, at
// /users/{user} or through login-as, is never asked for it (Parity 10). Only valid
// behind a guard.
func acctIsSelf(r *http.Request) bool {
	sess := session.FromContext(r.Context())
	return sess != nil && sess.User != "" && (sess.UserContext != "admin" || sess.Look == "")
}
