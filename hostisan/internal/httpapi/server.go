package httpapi

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/i18n"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
	"github.com/hostisan/hostisan/locale"
)

// Product holds the values of install/common/hostisan/product.env the UI links to;
// the binary gets them at build time (Makefile go-build, -ldflags -X).
type Product struct {
	URL           string // HOSTISAN_URL
	SecurityEmail string // HOSTISAN_SECURITY_EMAIL
}

type Deps struct {
	Auth      *auth.Service
	Sessions  *session.Manager
	H         *hestia.Client
	R         runner.Runner
	LocaleDir string
	UI        http.Handler     // the embedded single-page app; nil in API-only tests
	Version   string           // Hostisan's version, shown in the UI
	Product   Product          // Hostisan's site and security address, for the UI's links
	Now       func() time.Time // the clock; nil means time.Now

	Mailer         CredentialMailer // sends the database credentials email
	EmailTemplates EmailTemplates   // Hestia's email templates
	TmpDir         string           // where secret files go; Hestia only reads /tmp, so "" means /tmp

	// TerminalSocket is the root terminal process's unix socket (Plan 5); "" means
	// DefaultTerminalSocket.
	TerminalSocket string
	// PHPSessionDir is where hestia-php keeps PHP sessions; the file-manager handoff
	// writes FileGator's session there (Plan 5). "" means DefaultPHPSessionDir.
	PHPSessionDir string

	// Jobs is the background job registry, so serve can persist it and stop it on
	// shutdown (Plan 6 C6 D2); nil means a new in-memory one.
	Jobs *jobs.Registry

	// BackupDir is where the core writes backups and dumps; "" means coreBackupDir.
	// Tests point it at a temp dir.
	BackupDir string

	// FirewallToolsMissing names the firewall binaries (iptables, ipset) this server
	// lacks; turning the firewall on is refused while any is missing (D6). nil means
	// look in /usr/sbin and /sbin.
	FirewallToolsMissing func() []string

	// Background runs f after the reply no longer waits for it (the recovery emails);
	// nil means a new goroutine. Tests may run f inline.
	Background func(f func())
}

// Plan 5 defaults for Deps.TerminalSocket and Deps.PHPSessionDir.
const (
	DefaultTerminalSocket = "/run/hestia-web-terminal/terminal.sock"
	DefaultPHPSessionDir  = "/usr/local/hestia/data/sessions"
)

// CredentialMailer sends one email; *mailer.Mailer is the real one.
type CredentialMailer interface {
	Send(ctx context.Context, cfg hestia.SysConfig, msg mailer.Message) error
}

// EmailTemplates loads a Hestia email template; mailer.Templates is the real one.
type EmailTemplates interface {
	Load(lang, name string) (subject, body string, ok bool)
}

type server struct {
	Deps
	catalogs *i18n.Catalogs
	acct     accountsState  // Plan 3's throttles and pending 2FA
	jobs     *jobs.Registry // shared background work (Plans 2d and 4)
	tools    *toolSessions  // open terminals and file-manager handoffs (Plan 5)

	// Plan 5 per-server state: terminal tokens and file-manager PHP sessions. Set by
	// initToolsState through termState/filesState.
	toolsInit sync.Once
	term      *termState
	files     *filesState
}

// initToolsState creates the tools slices' per-server state.
func (s *server) initToolsState() {
	s.term = newTermState()
	s.files = newFilesState()
}

// terminalSocket is Deps.TerminalSocket or its default.
func (s *server) terminalSocket() string {
	return firstNonEmpty(s.TerminalSocket, DefaultTerminalSocket)
}

// phpSessionDir is Deps.PHPSessionDir or its default.
func (s *server) phpSessionDir() string {
	return firstNonEmpty(s.PHPSessionDir, DefaultPHPSessionDir)
}

// background runs f through Deps.Background, or in a new goroutine.
func (s *server) background(f func()) {
	if s.Background != nil {
		s.Background(f)
		return
	}
	go f()
}

// now is Deps.Now, or time.Now when it isn't set.
func (s *server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func NewServer(d Deps) http.Handler {
	_, h := newServer(d)
	return h
}

// sessionEndHook is a session store that reports the sessions it removes
// (session.MemoryStore).
type sessionEndHook interface {
	SetOnEnd(f func(id string))
}

// newServer is NewServer, also returning the server for tests.
func newServer(d Deps) (*server, http.Handler) {
	if d.Sessions.WriteError == nil {
		d.Sessions.WriteError = WriteError
	}
	reg := d.Jobs
	if reg == nil {
		reg = jobs.NewRegistry(0, d.Now)
	}
	s := &server{Deps: d, catalogs: &i18n.Catalogs{Dir: d.LocaleDir, Overlay: locale.FS}, jobs: reg, tools: newToolSessions()}
	if hook, ok := d.Sessions.Store.(sessionEndHook); ok {
		hook.SetOnEnd(s.tools.sessionEnded)
	}
	m := d.Sessions
	mux := http.NewServeMux()

	mux.Handle("GET /api/v1/bootstrap", m.Load(http.HandlerFunc(s.bootstrap)))
	mux.HandleFunc("GET /api/v1/i18n/{lang}", s.translations)
	mux.Handle("POST /api/v1/auth/login", m.Load(m.RequireCSRF(http.HandlerFunc(s.login))))
	mux.Handle("POST /api/v1/auth/logout", m.Load(m.RequireAuth(m.RequireCSRF(http.HandlerFunc(s.logout)))))
	mux.Handle("POST /api/v1/auth/login-as", m.Load(m.RequireAuth(m.RequireAdmin(m.RequireCSRF(http.HandlerFunc(s.loginAs))))))

	s.dnsRoutes(mux)
	s.dbRoutes(mux)
	s.registerMailRoutes(mux)
	s.registerWebRoutes(mux)
	s.registerAccountRoutes(mux)
	s.registerSysRoutes(mux)
	s.registerToolsRoutes(mux)

	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "")
	}))
	if d.UI != nil {
		mux.Handle("/", d.UI)
	}
	return s, noReferrer(mux)
}

// coreBackupDir is where the core writes backups and dumps: func/main.sh sets
// BACKUP='/backup' and lists BACKUP among the reserved keys source_conf refuses from
// hestia.conf, so v-change-sys-config-value BACKUP changes the file but not the core.
// hestia-nginx's internal location maps only this tree (B5). The panel uses the same
// fixed path and never reads BACKUP from the config (D3).
const coreBackupDir = "/backup"

// accelPrefix is the internal nginx location for downloads. Every download sends
// X-Accel-Redirect: accelPrefix + the absolute file path, which must lie under the
// backup directory.
const accelPrefix = "/__accel"

// accelRedirect sets X-Accel-Redirect for file, an absolute path that must be clean and
// inside the backup directory (the only tree nginx maps under accelPrefix). On refusal it
// has written a 500 and returns false; call it before any other header is set.
func (s *server) accelRedirect(w http.ResponseWriter, r *http.Request, file string) bool {
	dir := s.backupDir()
	if path.Clean(file) != file || !strings.HasPrefix(file, dir+"/") {
		serverError(w, errors.New("download path is outside the BACKUP directory"))
		return false
	}
	w.Header().Set("X-Accel-Redirect", accelPrefix+file)
	return true
}

// sameOriginGET refuses, with 403, a download that another site's page triggered. Only
// same-origin requests, typed URLs ("none") and clients that send no Sec-Fetch-Site pass;
// a customer site on a sibling subdomain is "same-site" and is refused too.
func sameOriginGET(w http.ResponseWriter, r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	WriteError(w, http.StatusForbidden, "forbidden", "")
	return false
}

// noReferrer sets Referrer-Policy: no-referrer on every response, so URLs that carry
// one-time values (recovery codes) never leave in a Referer header.
func noReferrer(mux *http.ServeMux) referrerHandler { return referrerHandler{mux} }

// referrerHandler is the router with the Referrer-Policy header added.
type referrerHandler struct{ *http.ServeMux }

func (h referrerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	h.ServeMux.ServeHTTP(w, r)
}
