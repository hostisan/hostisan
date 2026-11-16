package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/webapp"
)

// webAppHostHook, when set (tests only), adjusts each install job's Host.
var webAppHostHook func(*webapp.Host)

const (
	webAppBusyMsg       = "An installation is already running for this domain."
	webAppNoPHPMsg      = "Unable to install %s, required php version is not available."
	webAppBadPHPMsg     = "Required PHP version is not supported"
	webAppNoDatabaseMsg = "This app needs a database. Fill in the database details."
	webAppDBLimitMsg    = "Unable to add database! Limit reached!"
	webAppJobLimit      = 30 * time.Minute
	webAppMaxValueLen   = 4096
)

// webAppRoutes registers Slice D's routes: the quick-install catalogue, the setup
// forms, install jobs and their status.
func (s *server) webAppRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/web/{domain}/apps", s.read(s.webAppList))
	mux.Handle("GET /api/v1/web/{domain}/apps/jobs/{job}", s.read(s.webAppJobStatus))
	mux.Handle("GET /api/v1/web/{domain}/apps/{app}", s.read(s.webAppForm))
	mux.Handle("POST /api/v1/web/{domain}/apps/{app}", s.write(s.webAppInstall))
}

// webAppGate answers 403 when PLUGIN_APP_INSTALLER is exactly "false" (Parity 19) and
// returns the system config otherwise.
func (s *server) webAppGate(w http.ResponseWriter, r *http.Request) (hestia.SysConfig, bool) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	if cfg["PLUGIN_APP_INSTALLER"] == "false" {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return nil, false
	}
	return cfg, true
}

func webAppInfo(inst webapp.Installer, installed []string) WebAppInfo {
	i := inst.Info()
	return WebAppInfo{ID: i.ID, Name: i.Name, Group: i.Group, Version: i.Version,
		Installable: len(webapp.SupportedPHP(installed, inst.Config().PHP)) > 0}
}

// webAppInstalledPHP lists the installed PHP versions, answering the request itself on
// failure.
func (s *server) webAppInstalledPHP(w http.ResponseWriter, r *http.Request, user string) ([]string, bool) {
	h := &webapp.Host{R: s.R, User: user, TmpDir: s.TmpDir}
	v, err := h.InstalledPHP(r.Context())
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	return v, true
}

func (s *server) webAppList(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	if !hestia.ValidDomainArg(domain) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if _, ok := s.webAppGate(w, r); !ok {
		return
	}
	user := s.actingUser(r)
	if _, ok := s.webDomain(w, r, user, domain); !ok {
		return
	}
	installed, ok := s.webAppInstalledPHP(w, r, user)
	if !ok {
		return
	}
	out := WebApps{Apps: []WebAppInfo{}}
	for _, inst := range webapp.All() {
		out.Apps = append(out.Apps, webAppInfo(inst, installed))
	}
	writeJSON(w, http.StatusOK, out)
}

// webAppMySQLHosts lists the MySQL hosts, answering the request itself on failure.
func (s *server) webAppMySQLHosts(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var rows []hestia.DatabaseHost
	if !s.listJSON(w, r.Context(), &rows, "v-list-database-hosts", "json") {
		return nil, false
	}
	hosts := []string{}
	for _, h := range rows {
		if h.TYPE == "mysql" {
			hosts = append(hosts, h.HOST)
		}
	}
	return hosts, true
}

func (s *server) webAppForm(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	inst, found := webapp.Get(r.PathValue("app"))
	if !hestia.ValidDomainArg(domain) || !found {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if _, ok := s.webAppGate(w, r); !ok {
		return
	}
	user := s.actingUser(r)
	if _, ok := s.webDomain(w, r, user, domain); !ok {
		return
	}
	installed, ok := s.webAppInstalledPHP(w, r, user)
	if !ok {
		return
	}
	info := inst.Info()
	form := WebAppForm{
		App:         webAppInfo(inst, installed),
		Fields:      []WebAppField{},
		PHPVersions: webapp.SupportedPHP(installed, inst.Config().PHP),
	}
	for _, f := range inst.Form() {
		wf := WebAppField{Key: f.Key, Label: f.Label(), Type: f.Type, Value: f.Value,
			Placeholder: f.Placeholder, Options: []WebAppOption{}}
		for _, o := range f.Options {
			wf.Options = append(wf.Options, WebAppOption{Value: o.Value, Label: o.Label})
		}
		form.Fields = append(form.Fields, wf)
	}
	h := &webapp.Host{R: s.R, User: user, TmpDir: s.TmpDir}
	files, err := h.ListFiles(r.Context(), webAppTarget(user, domain, hestia.WebDomainDetail{}).DocRoot())
	if err != nil {
		// PHP: the web domain's folder is missing.
		writeJSON(w, http.StatusNotFound, APIError{Error: ErrorBody{Code: "not_found",
			Message: `Web domain path "%s" not found for domain "%s"`, Args: []string{"/home/" + user + "/web/" + domain, domain}}})
		return
	}
	form.DocrootClean = true
	for _, f := range files {
		if f != "index.html" && f != "robots.txt" {
			form.DocrootClean = false
		}
	}
	if info.Database {
		hosts, ok := s.webAppMySQLHosts(w, r)
		if !ok {
			return
		}
		form.Database = &WebAppDatabaseDefaults{Hosts: hosts, Name: webapp.DefaultDatabaseName(info.Name)}
	}
	writeJSON(w, http.StatusOK, form)
}

func webAppTarget(user, domain string, d hestia.WebDomainDetail) *webapp.Target {
	return &webapp.Target{Domain: domain, Home: "/home/" + user, IP: d.IP, SSL: d.SSL == "yes"}
}

// webAppValuesOK says whether a submitted value is safe to put in an argument: no
// control characters, and not absurdly long.
func webAppValueOK(v string) bool {
	if len(v) > webAppMaxValueLen {
		return false
	}
	for _, r := range v {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *server) webAppInstall(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	inst, found := webapp.Get(r.PathValue("app"))
	if !hestia.ValidDomainArg(domain) || !found {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	cfg, ok := s.webAppGate(w, r)
	if !ok {
		return
	}
	var req WebAppInstallRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ctx := r.Context()
	user := s.actingUser(r)
	detail, ok := s.webDomain(w, r, user, domain)
	if !ok {
		return
	}
	info := inst.Info()
	installed, ok := s.webAppInstalledPHP(w, r, user)
	if !ok {
		return
	}
	versions := webapp.SupportedPHP(installed, inst.Config().PHP)
	if len(versions) == 0 {
		writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: webAppNoPHPMsg, Args: []string{info.Name}}})
		return
	}
	found = false
	for _, v := range versions {
		found = found || v == req.PHPVersion
	}
	if !found {
		WriteError(w, http.StatusBadRequest, "invalid", webAppBadPHPMsg)
		return
	}

	// Form values: only the installer's own keys; a blank select takes its default.
	opts := map[string]string{"php_version": req.PHPVersion}
	for _, f := range inst.Form() {
		v := req.Values[f.Key]
		if f.Type == "select" {
			if v == "" {
				v = f.Value
			}
			valid := false
			for _, o := range f.Options {
				valid = valid || o.Value == v
			}
			if !valid {
				invalidFormat(w, f.Key, v)
				return
			}
		}
		if !webAppValueOK(v) {
			invalidFormat(w, f.Key, v)
			return
		}
		opts[f.Key] = v
	}

	target := webAppTarget(user, domain, detail)
	if info.Database {
		db := req.Database
		if db == nil {
			WriteError(w, http.StatusBadRequest, "invalid", webAppNoDatabaseMsg)
			return
		}
		hosts, ok := s.webAppMySQLHosts(w, r)
		if !ok {
			return
		}
		hostOK := false
		for _, h := range hosts {
			hostOK = hostOK || h == db.Host
		}
		switch {
		case !hostOK:
			invalidFormat(w, "host", db.Host)
			return
		case !webAppValueOK(db.Name) || strings.HasPrefix(db.Name, "-"):
			invalidFormat(w, "database", db.Name)
			return
		case !webAppValueOK(db.User) || strings.HasPrefix(db.User, "-"):
			invalidFormat(w, "dbuser", db.User)
			return
		case !webAppValueOK(db.Password): // never echoed
			WriteError(w, http.StatusBadRequest, "invalid", sysForbiddenCharsMsg)
			return
		}
		// One generated value serves both blanks, as in PHP.
		name, dbUser, pw := db.Name, db.User, db.Password
		if name == "" || dbUser == "" {
			def := webapp.DefaultDatabaseName(info.Name)
			if name == "" {
				name = def
			}
			if dbUser == "" {
				dbUser = def
			}
		}
		if pw == "" {
			pw = webapp.RandomHex(10)
		}
		target.DB = webapp.Database{Host: db.Host, Name: user + "_" + name, User: user + "_" + dbUser, Password: pw, Create: db.Create}
		if db.Create {
			u, found, err := s.H.User(ctx, user)
			if err != nil || !found {
				serverError(w, firstErr(err, "web apps: user not found"))
				return
			}
			if u["DATABASES"] != "unlimited" && webAppAtoi(u["DATABASES"])-webAppAtoi(u["U_DATABASES"]) <= 0 {
				WriteError(w, http.StatusConflict, "limit", webAppDBLimitMsg)
				return
			}
		}
	}

	env := webapp.Env{WebSystem: cfg["WEB_SYSTEM"], WebBackend: cfg["WEB_BACKEND"]}
	id, err := s.jobs.Start(ctx, jobs.Spec{Key: webJobKey(user, domain), Owner: user, Title: info.Name, Limit: webAppJobLimit},
		func(jctx context.Context, j *jobs.Job) (any, error) {
			h := webAppHost(j, s.R, user, s.TmpDir)
			if webAppHostHook != nil {
				webAppHostHook(h)
			}
			return nil, webapp.Install(jctx, inst, target, opts, h, env, j.Step)
		})
	if errors.Is(err, jobs.ErrBusy) {
		WriteError(w, http.StatusConflict, "busy", webAppBusyMsg)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, JobRef{JobID: id})
}

// webAppHost is the installer host for job j: its undo record (webapp.Undo) is saved
// with the job, for RecoverInterrupted after a kill (Plan 6 C6 D2).
func webAppHost(j *jobs.Job, r runner.Runner, user, tmp string) *webapp.Host {
	return &webapp.Host{R: r, User: user, TmpDir: tmp, Undo: func(u webapp.Undo) error { return j.SetData(u) }}
}

func webAppAtoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (s *server) webAppJobStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.webAppGate(w, r); !ok {
		return
	}
	domain, id := r.PathValue("domain"), r.PathValue("job")
	user := s.actingUser(r)
	if !hestia.ValidDomainArg(domain) || !jobs.ValidID(id) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	st, ok := s.jobs.Get(id)
	if !ok || st.Key != webJobKey(user, domain) || st.Owner != user {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	writeJSON(w, http.StatusOK, jobStatusBody(st))
}
