package httpapi

// Server admin (Plan 4): the shared pieces every slice uses. All handlers are methods
// on *sysAPI and are mounted under /api/v1/server by one route file per slice:
// sys_services.go (services and editors), sys_settings.go (Configure Server),
// sys_info.go (info, status, graphs, updates, white label, preview) and
// sys_network.go (IPs and firewall). Each registers with m.handle and wraps its
// handler in sys.read or sys.write; never the 2a read/write guards.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

// sysJobKey is the registry key of every server job: one runs at a time, which also
// keeps two writes from rewriting hestia.conf at once. Other tracks use other keys.
const sysJobKey = "server"

// sysJobLimit caps a whole server job; each step has its own, shorter timeout.
const sysJobLimit = 2 * time.Hour

// Per-step timeouts (runner.WithTimeout through sysAPI.step).
const (
	// Package installs and removals: v-add/delete-web-php, file manager, web terminal,
	// restic, quota.
	sysTimeoutInstall = 20 * time.Minute
	// Service restarts, config saves, v-change-sys-webmail/-db-alias/-php, IP add,
	// delete and refresh.
	sysTimeoutService = 5 * time.Minute
	// IP-set downloads.
	sysTimeoutDownload = 3 * time.Minute
	// Everything else.
	sysTimeoutDefault = 2 * time.Minute
	// Status dumps (decision 21).
	sysTimeoutStatus = 30 * time.Second
)

const sysBusyMsg = "Another server task is still running."

// sysAPI holds the server-admin handlers. It owns no job registry (jobs run on the
// shared s.jobs); fwMu serializes firewall read–check–write sequences (decision 37).
type sysAPI struct {
	*server
	fwMu sync.Mutex

	// firewallEnableCheck is the decision 37 "enabling the firewall" lockout guard
	// (sys_lockout.go). Slice D's networkRoutes sets it; Slice B's settings save calls
	// it, through checkFirewallEnable, before turning Plugins.Firewall on, and refuses
	// the save with the error it returns. A nil value fails closed: the save refuses
	// with the lockout error.
	firewallEnableCheck func(ctx context.Context, r *http.Request) error
}

// checkFirewallEnable runs firewallEnableCheck, or refuses with the lockout failure
// (409 SysLockoutCode, SysLockoutFirewallMsg) while it is unset.
func (sys *sysAPI) checkFirewallEnable(ctx context.Context, r *http.Request) error {
	if sys.firewallEnableCheck == nil {
		return &jobs.Failure{Code: SysLockoutCode, Message: SysLockoutFirewallMsg}
	}
	return sys.firewallEnableCheck(ctx, r)
}

// sysMux registers server routes and remembers their patterns, so the guard table
// can walk every route.
type sysMux struct {
	mux      *http.ServeMux
	patterns []string
}

func (m *sysMux) handle(pattern string, h http.Handler) {
	m.patterns = append(m.patterns, pattern)
	m.mux.Handle(pattern, h)
}

// registerSysRoutes registers the server admin routes (Plan 4).
func (s *server) registerSysRoutes(mux *http.ServeMux) {
	sys := &sysAPI{server: s}
	sys.routes(&sysMux{mux: mux})
}

func (sys *sysAPI) routes(m *sysMux) {
	m.handle("GET /api/v1/server/jobs/{id}", sys.read(sys.jobStatus))
	sys.servicesRoutes(m)
	sys.settingsRoutes(m)
	sys.infoRoutes(m)
	sys.networkRoutes(m)
}

// sysPatterns lists every server route s registers, as "METHOD /path".
func sysPatterns(s *server) []string {
	m := &sysMux{mux: http.NewServeMux()}
	(&sysAPI{server: s}).routes(m)
	return m.patterns
}

// read guards a server-admin read: signed in, the real role is admin (impersonation
// doesn't change it), and when POLICY_SYSTEM_HIDE_SERVICES is "yes" the signed-in
// user is ROOT_USER (decision 1). Anyone else gets 403 forbidden before any command.
func (sys *sysAPI) read(h http.HandlerFunc) http.Handler {
	m := sys.Sessions
	return m.Load(m.RequireAuth(sys.admitted(h)))
}

// write is read plus RequireWritable (read-only sessions get 403 read_only) and
// RequireCSRF (400 csrf).
func (sys *sysAPI) write(h http.HandlerFunc) http.Handler {
	m := sys.Sessions
	return m.Load(m.RequireAuth(sys.admitted(m.RequireWritable(m.RequireCSRF(h)))))
}

func (sys *sysAPI) admitted(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := session.FromContext(r.Context())
		if sess == nil || sess.UserContext != "admin" {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		cfg, err := sys.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		if cfg["POLICY_SYSTEM_HIDE_SERVICES"] == "yes" && sess.User != rootUser(cfg) {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// startJob starts fn as the server job and answers 202 JobRef, or 409 busy while
// another server job runs. fn's context outlives the request; give each command its
// own timeout with sys.step.
func (sys *sysAPI) startJob(w http.ResponseWriter, r *http.Request, title string,
	fn func(ctx context.Context, j *jobs.Job) (any, error)) {
	sess := session.FromContext(r.Context())
	id, err := sys.jobs.Start(r.Context(), jobs.Spec{Key: sysJobKey, Owner: sess.User, Title: title, Limit: sysJobLimit}, fn)
	switch {
	case errors.Is(err, jobs.ErrBusy):
		WriteError(w, http.StatusConflict, "busy", sysBusyMsg)
	case err != nil:
		serverError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, JobRef{JobID: id})
	}
}

// step records name as the job's current step and runs cmd with timeout. A non-zero
// exit is a *runner.CommandError, a timeout wraps runner.ErrTimeout; either fails the
// job with jobErrorBody's message when returned from fn.
func (sys *sysAPI) step(ctx context.Context, j *jobs.Job, name string, timeout time.Duration,
	cmd string, args ...string) (runner.Result, error) {
	j.Step(name)
	res, err := sys.R.Run(runner.WithTimeout(ctx, timeout), cmd, args...)
	if err != nil {
		return res, err
	}
	return res, res.Err(cmd)
}

// services lists the services fresh (v-list-sys-services json), keyed by name, for
// checking a {service} path value.
func (sys *sysAPI) services(ctx context.Context) (map[string]hestia.SysService, error) {
	res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), "v-list-sys-services", "json")
	if err == nil {
		err = res.Err("v-list-sys-services")
	}
	if err != nil {
		return nil, err
	}
	list, err := hestia.DecodeObjects[hestia.SysService](res.Stdout)
	if err != nil {
		return nil, err
	}
	out := make(map[string]hestia.SysService, len(list))
	for _, s := range list {
		out[s.NAME] = s
	}
	return out, nil
}

// jobStatus is GET /api/v1/server/jobs/{id}: a server job's status. Malformed,
// unknown, expired and other tracks' job ids are all 404.
func (sys *sysAPI) jobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !jobs.ValidID(id) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	st, ok := sys.jobs.Get(id)
	if !ok || st.Key != sysJobKey {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	writeJSON(w, http.StatusOK, jobStatusBody(st))
}
