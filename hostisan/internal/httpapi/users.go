package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
	"github.com/hostisan/hostisan/internal/session"
)

// userRoutes registers users and packages (Plan 3 Slice A). Called from registerAccountRoutes.
func (s *server) userRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/users", s.acctPageRead(s.acctUsersList))
	// The literal /bulk wins over the {user} wildcard (and POST /users/{user}/... never clashes).
	mux.Handle("POST /api/v1/users/bulk", s.acctPageWrite(s.acctUsersBulk))
	mux.Handle("DELETE /api/v1/users/{user}", s.acctPageWrite(s.acctUserDelete))
	mux.Handle("POST /api/v1/users/{user}/suspend", s.acctPageWrite(s.acctUserSuspend))
	mux.Handle("POST /api/v1/users/{user}/unsuspend", s.acctPageWrite(s.acctUserUnsuspend))

	mux.Handle("GET /api/v1/packages", s.acctPageRead(s.acctPackagesList))
	mux.Handle("GET /api/v1/packages/options", s.acctPageRead(s.acctPackageOptions))
	mux.Handle("POST /api/v1/packages/bulk", s.acctPageWrite(s.acctPackagesBulk))
	mux.Handle("GET /api/v1/packages/{package}", s.acctAdminRead(s.acctPackageGet))
	mux.Handle("POST /api/v1/packages", s.acctPageWrite(s.acctPackageAdd))
	mux.Handle("PUT /api/v1/packages/{package}", s.acctPageWrite(s.acctPackageEdit))
	mux.Handle("POST /api/v1/packages/{package}/copy", s.acctPageWrite(s.acctPackageCopy))
	mux.Handle("DELETE /api/v1/packages/{package}", s.acctPageWrite(s.acctPackageDelete))
}

// ---------------------------------------------------------------- users

// acctListUsers runs v-list-users in command order.
func (s *server) acctListUsers(r *http.Request) ([]hestia.UserEntry, error) {
	res, err := s.R.Run(r.Context(), "v-list-users", "json")
	if err == nil {
		err = res.Err("v-list-users")
	}
	if err != nil {
		return nil, err
	}
	return hestia.DecodeObjects[hestia.UserEntry](res.Stdout)
}

func (s *server) acctUsersList(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	users, err := s.acctListUsers(r)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	sess := session.FromContext(r.Context())
	root := rootUser(cfg)
	// Parity 15: a non-root admin doesn't see ROOT_USER when the policy hides it.
	if cfg["POLICY_SYSTEM_HIDE_ADMIN"] == "yes" && sess.User != root {
		kept := users[:0]
		for _, u := range users {
			if u.USER != root {
				kept = append(kept, u)
			}
		}
		users = kept
	}
	writeJSON(w, http.StatusOK, UserList{Users: users, RootUser: root, Self: sess.User})
}

// acctProtected reports whether name is ROOT_USER or the signed-in admin: delete,
// suspend and unsuspend refuse both (Parity 6).
func (s *server) acctProtected(r *http.Request, names ...string) (bool, error) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		return false, err
	}
	self := session.FromContext(r.Context()).User
	for _, n := range names {
		if n == rootUser(cfg) || n == self {
			return true, nil
		}
	}
	return false, nil
}

// acctUserDeleted signs a just-deleted user out everywhere. The admin can't be logged
// in as anyone here: these routes are refused then (owner ruling 12).
func (s *server) acctUserDeleted(names ...string) {
	for _, n := range names {
		s.Sessions.Store.DeleteUser(n, "")
	}
}

// acctUserTarget validates {user} and refuses protected accounts; ok false means the
// reply was written.
func (s *server) acctUserTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := acctUserParam(w, r)
	if !ok {
		return "", false
	}
	prot, err := s.acctProtected(r, u)
	if err != nil {
		serverError(w, err)
		return "", false
	}
	if prot {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return "", false
	}
	return u, true
}

func (s *server) acctUserDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.acctUserTarget(w, r)
	if !ok {
		return
	}
	ctx := runner.WithTimeout(r.Context(), acctUserCommandTimeout)
	if err := s.run(ctx, "v-delete-user", u); err != nil {
		writeCommandError(w, err)
		return
	}
	s.acctUserDeleted(u)
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) acctUserSuspend(w http.ResponseWriter, r *http.Request) {
	u, ok := s.acctUserTarget(w, r)
	if !ok {
		return
	}
	ctx := runner.WithTimeout(r.Context(), acctUserCommandTimeout)
	if err := s.run(ctx, "v-suspend-user", u); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) acctUserUnsuspend(w http.ResponseWriter, r *http.Request) {
	u, ok := s.acctUserTarget(w, r)
	if !ok {
		return
	}
	// The bin doesn't check, and it decrements SUSPENDED_USERS anyway (Parity 6).
	users, err := s.acctListUsers(r)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	found := false
	for _, e := range users {
		if e.USER == u {
			found = true
			if e.SUSPENDED != "yes" {
				writeJSON(w, http.StatusConflict, APIError{Error: ErrorBody{Code: "not_suspended", Message: acctNotSuspendedMsgid, Args: []string{u}}})
				return
			}
		}
	}
	if !found {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	ctx := runner.WithTimeout(r.Context(), acctUserCommandTimeout)
	if err := s.run(ctx, "v-unsuspend-user", u); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// acctNotSuspendedMsgid is the one message for unsuspending an active user, single and bulk.
const acctNotSuspendedMsgid = "User %s is not suspended."

// acctBulkUserCmds is users §3.6: the command per action and its restart argument.
var acctBulkUserCmds = map[string]struct{ cmd, restart string }{
	"delete":          {"v-delete-user", "no"},
	"suspend":         {"v-suspend-user", "yes"},
	"unsuspend":       {"v-unsuspend-user", "yes"},
	"update-counters": {"v-update-user-counters", ""},
	"rebuild":         {"v-rebuild-all", "no"},
	"rebuild-user":    {"v-rebuild-user", "no"},
	"rebuild-web":     {"v-rebuild-web-domains", ""},
	"rebuild-dns":     {"v-rebuild-dns-domains", ""},
	"rebuild-mail":    {"v-rebuild-mail-domains", ""},
	"rebuild-db":      {"v-rebuild-databases", ""},
	"rebuild-cron":    {"v-rebuild-cron-jobs", ""},
}

func (s *server) acctUsersBulk(w http.ResponseWriter, r *http.Request) {
	var req UserBulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	c, known := acctBulkUserCmds[req.Action]
	if !known {
		invalidFormat(w, "action", req.Action)
		return
	}
	// Parity 7: one user per request, except the cheap counters.
	limit := 1
	if req.Action == "update-counters" {
		limit = maxBulk
	}
	if len(req.Names) == 0 || len(req.Names) > limit {
		bulkCount(w, limit)
		return
	}
	for _, n := range req.Names {
		if !hestia.ValidUsername(n) {
			invalidFormat(w, "user", n)
			return
		}
	}
	protectedAction := req.Action == "delete" || req.Action == "suspend" || req.Action == "unsuspend"
	if protectedAction {
		prot, err := s.acctProtected(r, req.Names...)
		if err != nil {
			serverError(w, err)
			return
		}
		if prot {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
	}
	var users []hestia.UserEntry
	if req.Action == "unsuspend" {
		var err error
		if users, err = s.acctListUsers(r); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	ctx := runner.WithTimeout(r.Context(), acctUserCommandTimeout)
	failed := []BulkFailure{}
	for _, n := range req.Names {
		if req.Action == "unsuspend" {
			active := false
			for _, e := range users {
				active = active || (e.USER == n && e.SUSPENDED != "yes")
			}
			if active {
				failed = append(failed, BulkFailure{Name: n, Message: acctNotSuspendedMsgid, Args: []string{n}})
				continue
			}
		}
		args := []string{n}
		if c.restart != "" {
			args = append(args, c.restart)
		}
		if err := s.run(ctx, c.cmd, args...); err != nil {
			failed = append(failed, BulkFailure{Name: n, Message: errorMessage(err)})
		} else if req.Action == "delete" {
			s.acctUserDeleted(n)
		}
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}

// ---------------------------------------------------------------- packages

// acctStrings decodes a JSON array of strings; never nil.
func acctStrings(out []byte) ([]string, error) {
	list := []string{}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []string{}
	}
	return list, nil
}

func (s *server) acctPackagesList(w http.ResponseWriter, r *http.Request) {
	out, ok := s.output(w, r, "v-list-user-packages", "json")
	if !ok {
		return
	}
	list, err := hestia.DecodeObjects[hestia.PackageEntry](out)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PackageList{Packages: list})
}

func (s *server) acctPackageOptions(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	get := func(cmd string) ([]string, bool) {
		out, ok := s.output(w, r, cmd, "json")
		if !ok {
			return nil, false
		}
		list, err := acctStrings(out)
		if err != nil {
			serverError(w, err)
			return nil, false
		}
		return list, true
	}
	o := PackageOptions{
		WebSystem: cfg["WEB_SYSTEM"], WebBackend: cfg["WEB_BACKEND"], ProxySystem: cfg["PROXY_SYSTEM"], DNSSystem: cfg["DNS_SYSTEM"],
		ResourcesLimit:   cfg["RESOURCES_LIMIT"] == "yes",
		BackendTemplates: []string{}, ProxyTemplates: []string{},
	}
	var ok bool
	if o.WebTemplates, ok = get("v-list-web-templates"); !ok {
		return
	}
	if o.WebBackend != "" {
		if o.BackendTemplates, ok = get("v-list-web-templates-backend"); !ok {
			return
		}
	}
	if o.ProxySystem != "" {
		if o.ProxyTemplates, ok = get("v-list-web-templates-proxy"); !ok {
			return
		}
	}
	if o.DNSTemplates, ok = get("v-list-dns-templates"); !ok {
		return
	}
	if o.Shells, ok = get("v-list-sys-shells"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// acctPackageName is the {package} path value for reads and writes of one package;
// "system" can't be read or edited (404).
func acctEditablePackage(w http.ResponseWriter, r *http.Request) (string, bool) {
	p, ok := acctPackageParam(w, r)
	if !ok {
		return "", false
	}
	if p == "system" {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return "", false
	}
	return p, true
}

// acctStoredPackage reads one package; a missing one answers 404 itself.
func (s *server) acctStoredPackage(w http.ResponseWriter, r *http.Request, name string) (hestia.Package, bool) {
	out, ok := s.output(w, r, "v-list-user-package", name, "json")
	if !ok {
		return hestia.Package{}, false
	}
	list, err := hestia.DecodeObjects[hestia.Package](out)
	if err != nil || len(list) == 0 {
		if err == nil {
			WriteError(w, http.StatusNotFound, "not_found", "")
			return hestia.Package{}, false
		}
		serverError(w, err)
		return hestia.Package{}, false
	}
	return list[0], true
}

func (s *server) acctPackageGet(w http.ResponseWriter, r *http.Request) {
	p, ok := acctEditablePackage(w, r)
	if !ok {
		return
	}
	pkg, ok := s.acctStoredPackage(w, r, p)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

// acctSafeValue reports whether v can sit inside single quotes in a package file: the
// bin sources the file, so a quote or a line break would end the value.
func acctSafeValue(v string) bool {
	for _, c := range v {
		if c == '\'' || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// acctNameServers drops blanks and trims dots from each name server.
func acctNameServers(in []string) []string {
	out := []string{}
	for _, n := range in {
		if n = strings.Trim(strings.TrimSpace(n), "."); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// acctPackageInput is a validated package request.
type acctPackageInput struct {
	req PackageRequest
	ns  []string
	cfg hestia.SysConfig
}

// acctPackageRequest decodes and validates the body (users §3.8/§3.9, Parity 14): blank
// checks in the PHP order, then format safety. ok false means the reply was written and
// no command ran.
func (s *server) acctPackageRequest(w http.ResponseWriter, r *http.Request) (acctPackageInput, bool) {
	var in acctPackageInput
	if !decodeBody(w, r, &in.req) {
		return in, false
	}
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return in, false
	}
	in.cfg = cfg
	q := &in.req
	in.ns = acctNameServers(q.Nameservers)
	pairs := []string{"Package", q.Name, "Web Template", q.WebTemplate}
	if cfg["WEB_BACKEND"] != "" {
		pairs = append(pairs, "Backend Template", q.BackendTemplate)
	}
	if cfg["PROXY_SYSTEM"] != "" {
		pairs = append(pairs, "Proxy Template", q.ProxyTemplate)
	}
	pairs = append(pairs, "DNS Template", q.DNSTemplate, "Shell", q.Shell, "Web Domains", q.WebDomains, "Web Aliases", q.WebAliases,
		"DNS Zones", q.DNSDomains, "DNS Records", q.DNSRecords, "Mail Domains", q.MailDomains, "Mail Accounts", q.MailAccounts,
		"Databases", q.Databases, "Cron Jobs", q.CronJobs, "Backups", q.Backups, "Incremental Backups", q.BackupsIncremental,
		"Quota", q.DiskQuota, "Bandwidth", q.Bandwidth, "Rate Limit", q.RateLimit)
	if cfg["RESOURCES_LIMIT"] == "yes" {
		pairs = append(pairs, "CPU quota", q.CPUQuota, "CPU quota period", q.CPUQuotaPeriod, "Memory Limit", q.MemoryLimit, "Swap Limit", q.SwapLimit)
	}
	if cfg["DNS_SYSTEM"] != "" {
		ns1, ns2 := "", ""
		if len(in.ns) > 0 {
			ns1 = in.ns[0]
		}
		if len(in.ns) > 1 {
			ns2 = in.ns[1]
		}
		pairs = append(pairs, "Nameserver 1", ns1, "Nameserver 2", ns2)
	}
	if !blankFields(w, pairs...) {
		return in, false
	}
	if len(in.ns) > 8 {
		WriteError(w, http.StatusBadRequest, "invalid", "Up to 8 name servers are allowed.")
		return in, false
	}
	// Refused before any command, with the message the bin would print (owner ruling 16).
	invalid := func(key, v string) {
		writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: "invalid %s format :: %s", Args: []string{key, v}}})
	}
	if !hestia.ValidPackageArg(q.Name) {
		invalid("package", q.Name)
		return in, false
	}
	for i := 1; i < len(pairs); i += 2 {
		if !acctSafeValue(pairs[i]) {
			invalid(acctPackageKeys[pairs[i-1]], pairs[i])
			return in, false
		}
	}
	extra := [][2]string{{"BACKEND_TEMPLATE", q.BackendTemplate}, {"PROXY_TEMPLATE", q.ProxyTemplate}, {"CPU_QUOTA", q.CPUQuota},
		{"CPU_QUOTA_PERIOD", q.CPUQuotaPeriod}, {"MEMORY_LIMIT", q.MemoryLimit}, {"SWAP_LIMIT", q.SwapLimit}}
	for i, ns := range in.ns {
		extra = append(extra, [2]string{fmt.Sprintf("ns%d", i+1), ns})
	}
	for _, kv := range extra {
		if !acctSafeValue(kv[1]) {
			invalid(kv[0], kv[1])
			return in, false
		}
	}
	return in, true
}

// acctPackageKeys names each package form field by its package-file key, as the
// bin's "invalid <KEY> format" messages do.
var acctPackageKeys = map[string]string{
	"Package": "package", "Web Template": "WEB_TEMPLATE", "Backend Template": "BACKEND_TEMPLATE",
	"Proxy Template": "PROXY_TEMPLATE", "DNS Template": "DNS_TEMPLATE", "Shell": "SHELL", "Web Domains": "WEB_DOMAINS",
	"Web Aliases": "WEB_ALIASES", "DNS Zones": "DNS_DOMAINS", "DNS Records": "DNS_RECORDS", "Mail Domains": "MAIL_DOMAINS",
	"Mail Accounts": "MAIL_ACCOUNTS", "Databases": "DATABASES", "Cron Jobs": "CRON_JOBS", "Backups": "BACKUPS",
	"Incremental Backups": "BACKUPS_INCREMENTAL", "Quota": "DISK_QUOTA", "Bandwidth": "BANDWIDTH", "Rate Limit": "RATE_LIMIT",
	"CPU quota": "CPU_QUOTA", "CPU quota period": "CPU_QUOTA_PERIOD", "Memory Limit": "MEMORY_LIMIT", "Swap Limit": "SWAP_LIMIT",
	"Nameserver 1": "ns1", "Nameserver 2": "ns2",
}

// acctPackageFile renders KEY='value' lines; a pair whose raw flag is set is written
// as KEY= (Hestia's empty, unquoted value).
type acctKV struct {
	k, v string
	raw  bool
}

func acctPackageFile(kv []acctKV) string {
	lines := make([]string, len(kv))
	for i, e := range kv {
		if e.raw {
			lines[i] = e.k + "="
		} else {
			lines[i] = e.k + "='" + e.v + "'"
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// acctWithFile writes content to a private temp file, runs the steps built from its path
// and removes it again.
func (s *server) acctWithFile(w http.ResponseWriter, r *http.Request, content string, steps func(path string) [][]string) bool {
	// secret.TempFile adds the final newline itself.
	path, cleanup, err := secret.TempFile("/tmp", strings.TrimSuffix(content, "\n"))
	if err != nil {
		serverError(w, err)
		return false
	}
	defer cleanup()
	ctx := runner.WithTimeout(r.Context(), acctUserCommandTimeout)
	return s.steps(w, r.WithContext(ctx), steps(path))
}

func (s *server) acctPackageAdd(w http.ResponseWriter, r *http.Request) {
	in, ok := s.acctPackageRequest(w, r)
	if !ok {
		return
	}
	q, cfg := in.req, in.cfg
	ns := strings.Join(in.ns, ",")
	if cfg["DNS_SYSTEM"] == "" {
		ns = "" // without a DNS system the name servers aren't offered; Hestia wrote ","
	}
	if ns == "" {
		ns = "," // as Hestia wrote it
	}
	res := cfg["RESOURCES_LIMIT"] == "yes"
	kv := []acctKV{{k: "WEB_TEMPLATE", v: q.WebTemplate}}
	if cfg["WEB_BACKEND"] != "" {
		kv = append(kv, acctKV{k: "BACKEND_TEMPLATE", v: q.BackendTemplate})
	}
	if cfg["PROXY_SYSTEM"] != "" {
		kv = append(kv, acctKV{k: "PROXY_TEMPLATE", v: q.ProxyTemplate})
	}
	now := s.now()
	kv = append(kv,
		acctKV{k: "DNS_TEMPLATE", v: q.DNSTemplate}, acctKV{k: "WEB_DOMAINS", v: q.WebDomains}, acctKV{k: "WEB_ALIASES", v: q.WebAliases},
		acctKV{k: "DNS_DOMAINS", v: q.DNSDomains}, acctKV{k: "DNS_RECORDS", v: q.DNSRecords}, acctKV{k: "MAIL_DOMAINS", v: q.MailDomains},
		acctKV{k: "MAIL_ACCOUNTS", v: q.MailAccounts}, acctKV{k: "DATABASES", v: q.Databases}, acctKV{k: "CRON_JOBS", v: q.CronJobs},
		acctKV{k: "DISK_QUOTA", v: q.DiskQuota},
		acctKV{k: "CPU_QUOTA", v: q.CPUQuota, raw: !res}, acctKV{k: "CPU_QUOTA_PERIOD", v: q.CPUQuotaPeriod, raw: !res},
		acctKV{k: "MEMORY_LIMIT", v: q.MemoryLimit, raw: !res}, acctKV{k: "SWAP_LIMIT", v: q.SwapLimit, raw: !res},
		acctKV{k: "BANDWIDTH", v: q.Bandwidth}, acctKV{k: "RATE_LIMIT", v: q.RateLimit}, acctKV{k: "NS", v: ns},
		acctKV{k: "SHELL", v: q.Shell}, acctKV{k: "BACKUPS", v: q.Backups}, acctKV{k: "BACKUPS_INCREMENTAL", v: q.BackupsIncremental},
		acctKV{k: "TIME", v: now.Format("15:04:05")}, acctKV{k: "DATE", v: now.Format("2006-01-02")})
	if !s.acctWithFile(w, r, acctPackageFile(kv), func(p string) [][]string { return [][]string{{"v-add-user-package", p, q.Name}} }) {
		return
	}
	writeJSON(w, http.StatusOK, PackageSaved{Status: "ok", Name: q.Name})
}

func (s *server) acctPackageEdit(w http.ResponseWriter, r *http.Request) {
	old, ok := acctEditablePackage(w, r)
	if !ok {
		return
	}
	in, ok := s.acctPackageRequest(w, r)
	if !ok {
		return
	}
	q, cfg := in.req, in.cfg
	stored, ok := s.acctStoredPackage(w, r, old)
	if !ok {
		return
	}
	pick := func(on bool, posted, kept string) string {
		if on {
			return posted
		}
		return kept
	}
	res := cfg["RESOURCES_LIMIT"] == "yes"
	ns := pick(cfg["DNS_SYSTEM"] != "", strings.Join(in.ns, ","), stored.NS)
	now := s.now()
	kv := []acctKV{
		{k: "WEB_TEMPLATE", v: q.WebTemplate},
		{k: "BACKEND_TEMPLATE", v: pick(cfg["WEB_BACKEND"] != "", q.BackendTemplate, stored.BACKEND_TEMPLATE)},
		{k: "PROXY_TEMPLATE", v: pick(cfg["PROXY_SYSTEM"] != "", q.ProxyTemplate, stored.PROXY_TEMPLATE)},
		{k: "DNS_TEMPLATE", v: q.DNSTemplate}, {k: "WEB_DOMAINS", v: q.WebDomains}, {k: "WEB_ALIASES", v: q.WebAliases},
		{k: "DNS_DOMAINS", v: q.DNSDomains}, {k: "DNS_RECORDS", v: q.DNSRecords}, {k: "MAIL_DOMAINS", v: q.MailDomains},
		{k: "MAIL_ACCOUNTS", v: q.MailAccounts}, {k: "RATE_LIMIT", v: q.RateLimit}, {k: "DATABASES", v: q.Databases},
		{k: "CRON_JOBS", v: q.CronJobs}, {k: "DISK_QUOTA", v: q.DiskQuota},
		{k: "CPU_QUOTA", v: pick(res, q.CPUQuota, stored.CPU_QUOTA)},
		{k: "CPU_QUOTA_PERIOD", v: pick(res, q.CPUQuotaPeriod, stored.CPU_QUOTA_PERIOD)},
		{k: "MEMORY_LIMIT", v: pick(res, q.MemoryLimit, stored.MEMORY_LIMIT)},
		{k: "SWAP_LIMIT", v: pick(res, q.SwapLimit, stored.SWAP_LIMIT)},
		{k: "BANDWIDTH", v: q.Bandwidth}, {k: "NS", v: ns}, {k: "SHELL", v: q.Shell}, {k: "BACKUPS", v: q.Backups},
		{k: "BACKUPS_INCREMENTAL", v: q.BackupsIncremental},
		{k: "TIME", v: now.Format("15:04:05")}, {k: "DATE", v: now.Format("2006-01-02")},
	}
	for _, e := range kv {
		if !acctSafeValue(e.v) {
			serverError(w, errors.New("package "+old+": stored value for "+e.k+" is not safe to rewrite"))
			return
		}
	}
	steps := func(p string) [][]string {
		st := [][]string{{"v-add-user-package", p, old, "yes"}, {"v-update-user-package", old}}
		if q.Name != old {
			st = append(st, []string{"v-rename-user-package", old, q.Name})
		}
		return st
	}
	if !s.acctWithFile(w, r, acctPackageFile(kv), steps) {
		return
	}
	writeJSON(w, http.StatusOK, PackageSaved{Status: "ok", Name: q.Name})
}

func (s *server) acctPackageCopy(w http.ResponseWriter, r *http.Request) {
	p, ok := acctPackageParam(w, r)
	if !ok {
		return
	}
	if err := s.run(r.Context(), "v-copy-user-package", p, p+"-copy"); err != nil {
		// Hestia's message when the command printed nothing (owner ruling 16).
		var ce *runner.CommandError
		if errors.As(err, &ce) && strings.TrimSpace(ce.Output) == "" {
			ce.Output = "Error: unable to copy package."
		}
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// acctPackageInUse means another package whose name starts with the target's has users.
type acctPackageInUse struct{ pkg, other string }

func (e *acctPackageInUse) Error() string { return acctInUseMsg(e.pkg, e.other) }

const acctInUseMsgid = "Package %s can't be deleted while package %s has users."

// acctBuiltinPackageMsg refuses deleting the default or system package.
const acctBuiltinPackageMsg = "Package %s is built in and can't be deleted."

func acctInUseMsg(pkg, other string) string { return fmt.Sprintf(acctInUseMsgid, pkg, other) }

// acctDeletePackage is Parity 13: refuse when a prefix-sharing package has users, move
// the package's own users to default (stopping at the first failure), then delete.
func (s *server) acctDeletePackage(r *http.Request, name string) error {
	users, err := s.acctListUsers(r)
	if err != nil {
		return err
	}
	// The bin finds users with grep "PACKAGE='<name>", where a dot matches any
	// character; match the same way so we refuse whenever it would touch them.
	re, err := regexp.Compile("^" + name)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.PACKAGE != name && re.MatchString(u.PACKAGE) {
			return &acctPackageInUse{pkg: name, other: u.PACKAGE}
		}
	}
	ctx := runner.WithTimeout(r.Context(), acctUserCommandTimeout)
	for _, u := range users {
		if u.PACKAGE == name {
			if err := s.run(ctx, "v-change-user-package", u.USER, "default"); err != nil {
				return err
			}
		}
	}
	return s.run(ctx, "v-delete-user-package", name)
}

func (s *server) acctPackageDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := acctPackageParam(w, r)
	if !ok {
		return
	}
	if p == "default" || p == "system" {
		invalid(w, acctBuiltinPackageMsg, p)
		return
	}
	err := s.acctDeletePackage(r, p)
	var inUse *acctPackageInUse
	switch {
	case errors.As(err, &inUse):
		writeJSON(w, http.StatusConflict, APIError{Error: ErrorBody{Code: "in_use", Message: acctInUseMsgid, Args: []string{inUse.pkg, inUse.other}}})
	case err != nil:
		writeCommandError(w, err)
	default:
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

func (s *server) acctPackagesBulk(w http.ResponseWriter, r *http.Request) {
	var req BulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !bulkShapeOK(w, req.Action == "delete", req.Action, len(req.Names), maxBulk) {
		return
	}
	for _, n := range req.Names {
		if !hestia.ValidPackageArg(n) {
			invalidFormat(w, "package", n)
			return
		}
		if n == "default" || n == "system" {
			invalid(w, acctBuiltinPackageMsg, n)
			return
		}
	}
	failed := []BulkFailure{}
	for _, n := range req.Names {
		if err := s.acctDeletePackage(r, n); err != nil {
			f := BulkFailure{Name: n, Message: errorMessage(err)}
			var inUse *acctPackageInUse
			if errors.As(err, &inUse) {
				f.Message, f.Args = acctInUseMsgid, []string{inUse.pkg, inUse.other}
			}
			failed = append(failed, f)
		}
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}
