package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

// Config editors (decisions 12-20). One editor per real service; sysEditorFor maps a
// {service} value (a list key or a legacy alias, decision 13) to it.

const (
	sysPHPBlockedMsg   = "Saving would overwrite every php.ini on this server. Edit this file over SSH."
	sysCronInfoMsg     = "Info (read-only mode): Crontab can only be edited via SSH."
	sysRestartFailMsg  = "The new configuration failed to start, so the previous file was restored."
	sysConfigBadMsg    = "The new configuration has errors, so the previous file was restored."
	sysEditorChanged   = "The file changed on the server since it was read. Reload and try again."
	sysEditorUnreadMsg = "Could not read %s."
	sysDirectiveMsg    = "Unknown directive: %s"
	sysDirValueMsg     = "Invalid value for %s"
)

// sysEditorFileSpec is one file of an editor: a fixed path, or one a v-list-sys-*-config
// json reply names under key. Optional files are left out when the reply has no such key
// (the RHEL dovecot layout).
type sysEditorFileSpec struct {
	id       string
	path     string
	cmd, key string
	optional bool
}

type sysEditorDef struct {
	family   string
	title    string
	files    []sysEditorFileSpec
	restart  bool   // the restart checkbox is offered
	readOnly bool   // cron
	blocked  string // SaveBlocked
	svc      string // service the core restarts after a save
	// checked: v-change-sys-service-config tests the file with the service's own
	// checker before keeping it (sysCheckedServices).
	checked bool
	// phpVersion is set on a per-version PHP row: its one php.ini is saved with
	// v-change-sys-php-ini instead of v-change-sys-service-config.
	phpVersion string
	// directives are the basic-mode names of the first file and how its lines are
	// shaped: "nginx" (key value;) or "ini" (key = value).
	directives []string
	style      string
}

var (
	sysNginxDirectives = []string{"worker_processes", "worker_connections", "client_max_body_size", "send_timeout",
		"proxy_connect_timeout", "proxy_send_timeout", "proxy_read_timeout", "gzip", "gzip_comp_level", "charset"}
	sysPHPDirectives = []string{"max_execution_time", "max_input_time", "memory_limit", "error_reporting",
		"display_errors", "post_max_size", "upload_max_filesize"}
	sysMySQLDirectives = []string{"max_connections", "max_user_connections", "wait_timeout", "interactive_timeout",
		"max_allowed_packet"}

	sysPHPVersionName = regexp.MustCompile(`^php([0-9]\.[0-9]{1,2})-fpm$`)
	sysDirectiveValue = regexp.MustCompile(`^[A-Za-z0-9._~&|: -]{1,64}$`)
)

// sysCheckedServices are the editors whose files v-change-sys-service-config tests
// with the service's own checker (nginx -t, apache2ctl -t, httpd -t, exim -bV,
// proftpd -t, doveconf -n, named-checkconf, fail2ban-client -t, sshd -t). Keep in step
// with check_cmd in that script. php-fpm -t is run on a php.ini too but does not
// fail on a bad ini line, so PHP is not listed.
var sysCheckedServices = []string{"nginx", "apache2", "httpd", "exim", "exim4", "proftpd", "dovecot", "named",
	"bind9", "fail2ban", "ssh"}

// sysPlainEditor is a single-file editor.
func sysPlainEditor(title, id, path string) sysEditorDef {
	return sysEditorDef{family: "plain", title: title, restart: true, svc: id,
		files: []sysEditorFileSpec{{id: id, path: path}}}
}

// sysConfigEditor is a single-file editor whose path a v-list-sys-<x>-config names.
func sysConfigEditor(title, id, cmd string) sysEditorDef {
	return sysEditorDef{family: "plain", title: title, restart: true, svc: id,
		files: []sysEditorFileSpec{{id: id, cmd: cmd, key: "config_path"}}}
}

// sysEditorDefFor is sysEditorFor without the checked flag.
func sysEditorDefFor(name string) (sysEditorDef, bool) {
	if m := sysPHPVersionName.FindStringSubmatch(name); m != nil {
		return sysEditorDef{family: "php", title: "PHP", restart: true, svc: name, phpVersion: m[1],
			directives: sysPHPDirectives, style: "ini",
			files: []sysEditorFileSpec{{id: "php", path: "/etc/php/" + m[1] + "/fpm/php.ini"}}}, true
	}
	switch name {
	case "nginx":
		return sysEditorDef{family: "nginx", title: "NGINX", restart: true, svc: "nginx", directives: sysNginxDirectives,
			style: "nginx", files: []sysEditorFileSpec{{id: "nginx", path: "/etc/nginx/nginx.conf"}}}, true
	case "apache2":
		return sysPlainEditor("APACHE2", "apache2", "/etc/apache2/apache2.conf"), true
	case "httpd":
		return sysPlainEditor("HTTPD", "httpd", "/etc/httpd/conf/httpd.conf"), true
	case "php", "php-fpm", "php5-fpm":
		return sysEditorDef{family: "php", title: "PHP", restart: true, svc: "php", blocked: sysPHPBlockedMsg,
			directives: sysPHPDirectives, style: "ini",
			files: []sysEditorFileSpec{{id: "php", cmd: "v-list-sys-php-config", key: "config_path"}}}, true
	case "mysql", "mysqld", "mariadb":
		title := "MYSQL"
		if name == "mariadb" {
			title = "MARIADB"
		}
		return sysEditorDef{family: "mysql", title: title, restart: true, svc: name, directives: sysMySQLDirectives, style: "ini",
			files: []sysEditorFileSpec{{id: name, cmd: "v-list-sys-mysql-config", key: "config_path"}}}, true
	case "postgresql":
		return sysEditorDef{family: "multi", title: "POSTGRESQL", restart: true, svc: "postgresql", files: []sysEditorFileSpec{
			{id: "postgresql-hba", cmd: "v-list-sys-pgsql-config", key: "pg_hba_path"},
			{id: "postgresql", cmd: "v-list-sys-pgsql-config", key: "config_path"}}}, true
	case "bind9":
		return sysEditorDef{family: "multi", title: "BIND9", restart: true, svc: "bind9", files: []sysEditorFileSpec{
			{id: "bind9-opt", path: "/etc/bind/named.conf.options"}, {id: "bind9", path: "/etc/bind/named.conf"}}}, true
	case "named":
		return sysPlainEditor("NAMED", "named", "/etc/named.conf"), true
	case "dovecot":
		d := sysEditorDef{family: "dovecot", title: "DOVECOT", restart: true, svc: "dovecot",
			files: []sysEditorFileSpec{{id: "dovecot", cmd: "v-list-sys-dovecot-config", key: "config_path"}}}
		for i := 1; i <= 8; i++ {
			n := string(rune('0' + i))
			d.files = append(d.files, sysEditorFileSpec{id: "dovecot-" + n, cmd: "v-list-sys-dovecot-config", key: "config_path" + n, optional: true})
		}
		return d, true
	case "exim":
		return sysPlainEditor("EXIM", "exim", "/etc/exim/exim.conf"), true
	case "exim4":
		return sysPlainEditor("EXIM", "exim4", "/etc/exim4/exim4.conf.template"), true
	case "fail2ban":
		return sysPlainEditor("FAIL2BAN", "fail2ban", "/etc/fail2ban/jail.local"), true
	case "ssh":
		return sysPlainEditor("SSH", "ssh", "/etc/ssh/sshd_config"), true
	case "spamd", "spamassassin":
		return sysConfigEditor("SPAMASSASSIN", name, "v-list-sys-spamd-config"), true
	case "clamd", "clamav-daemon":
		return sysConfigEditor("CLAMAV", "clamd", "v-list-sys-clamd-config"), true
	case "vsftpd":
		return sysConfigEditor("VSFTPD", "vsftpd", "v-list-sys-vsftpd-config"), true
	case "proftpd":
		return sysConfigEditor("PROFTPD", "proftpd", "v-list-sys-proftpd-config"), true
	case "cron", "crond":
		return sysEditorDef{family: "cron", title: "CRON", readOnly: true,
			files: []sysEditorFileSpec{{id: "cron", path: "/etc/crontab"}}}, true
	case "hestiaweb":
		return sysEditorDef{family: "panel-cron", title: "Panel Cronjobs",
			files: []sysEditorFileSpec{{id: "hestiaweb", path: "/var/spool/cron/crontabs/hestiaweb"}}}, true
	}
	return sysEditorDef{}, false
}

// sysEditorFor is the editor for a {service} value; false when it has none (including
// iptables and web-terminal, see sysNoEditor).
func sysEditorFor(name string) (sysEditorDef, bool) {
	def, ok := sysEditorDefFor(name)
	def.checked = ok && slices.Contains(sysCheckedServices, def.svc)
	return def, ok
}

// sysNoEditor are list rows that exist but have no editor (decision 13).
func sysNoEditor(name string) bool { return name == "iptables" || name == "web-terminal" }

// sysEditorLookup resolves the {service} path value: 400 for an unknown or malformed
// name, 404 for a row without an editor. It runs no command.
func sysEditorLookup(w http.ResponseWriter, r *http.Request) (string, sysEditorDef, bool) {
	name := r.PathValue("service")
	if sysNoEditor(name) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return "", sysEditorDef{}, false
	}
	def, ok := sysEditorFor(name)
	if !ok || !sysServiceName.MatchString(name) {
		WriteError(w, http.StatusBadRequest, "invalid", sysUnknownMsg)
		return "", sysEditorDef{}, false
	}
	return name, def, true
}

// sysResolved is one editor file with its path known, or the error that stopped that.
type sysResolved struct {
	id, path string
	err      string
}

// editorFiles resolves the editor's files in order, running each v-list-sys-*-config
// at most once.
func (sys *sysAPI) editorFiles(ctx context.Context, def sysEditorDef) []sysResolved {
	type reply struct {
		cfg map[string]string
		err string
	}
	cache := map[string]reply{}
	lookup := func(f sysEditorFileSpec) reply {
		k := f.cmd
		if r, ok := cache[k]; ok {
			return r
		}
		var r reply
		res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), f.cmd, "json")
		if err == nil {
			err = res.Err(f.cmd)
		}
		switch {
		case err != nil:
			r.err = errorMessage(err)
		default:
			cf, derr := hestia.DecodeConfigFile(res.Stdout)
			if derr != nil {
				r.err = errorMessage(derr)
			} else {
				r.cfg = cf.CONFIG
			}
		}
		cache[k] = r
		return r
	}
	var out []sysResolved
	for _, f := range def.files {
		if f.path != "" {
			out = append(out, sysResolved{id: f.id, path: f.path})
			continue
		}
		r := lookup(f)
		switch {
		case r.err != "":
			if !f.optional {
				out = append(out, sysResolved{id: f.id, err: r.err})
			}
		case r.cfg[f.key] == "":
			if !f.optional {
				out = append(out, sysResolved{id: f.id, err: "Error: config file path not found"})
			}
		default:
			out = append(out, sysResolved{id: f.id, path: r.cfg[f.key]})
		}
	}
	return out
}

func sysSum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// openFsConfigLogOnly matches the one stderr line upstream v-open-fs-config prints
// after a successful read: it never sources func/main.sh, so its closing
// "$BIN/v-log-action" runs as /v-log-action and the script exits 127.
var openFsConfigLogOnly = regexp.MustCompile(`\A[^\n]*: /v-log-action: No such file or directory\n?\z`)

// openFsConfigRead reports whether res is a successful v-open-fs-config read: exit 0,
// or upstream's exit 127 whose only stderr is the failed v-log-action call and whose
// stdout is not an Error line (the read checks all exit before that call).
func openFsConfigRead(res runner.Result) bool {
	if res.Exit == 0 {
		return true
	}
	return res.Exit == 127 && openFsConfigLogOnly.Match(res.Stderr) &&
		!bytes.HasPrefix(res.Stdout, []byte("Error:"))
}

// readEditorFile reads one file with v-open-fs-config. A failure is the file's Error,
// never its content (decision 12).
func (sys *sysAPI) readEditorFile(ctx context.Context, f sysResolved) EditorFile {
	out := EditorFile{ID: f.id, Path: f.path}
	if f.err != "" {
		out.Error = f.err
		return out
	}
	res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), "v-open-fs-config", f.path)
	if err == nil && !openFsConfigRead(res) {
		err = res.Err("v-open-fs-config")
	}
	if err != nil {
		out.Error = errorMessage(err)
		return out
	}
	out.Content = string(res.Stdout)
	out.Hash = sysSum(out.Content)
	return out
}

// editorRead is GET /api/v1/server/editors/{service}.
func (sys *sysAPI) editorRead(w http.ResponseWriter, r *http.Request) {
	name, def, ok := sysEditorLookup(w, r)
	if !ok {
		return
	}
	v := EditorView{
		Service: name, Title: def.title, Family: def.family, Restart: def.restart, ReadOnly: def.readOnly,
		SaveBlocked: def.blocked, SyntaxCheck: def.checked, Files: []EditorFile{},
	}
	for _, f := range sys.editorFiles(r.Context(), def) {
		v.Files = append(v.Files, sys.readEditorFile(r.Context(), f))
	}
	if len(def.directives) > 0 && len(v.Files) > 0 && v.Files[0].Error == "" {
		v.Directives = map[string]string{}
		for _, d := range def.directives {
			if _, val, found := sysFindDirective(v.Files[0].Content, def.style, d); found {
				v.Directives[d] = val
			}
		}
	}
	writeJSON(w, http.StatusOK, v)
}

// sysDirectivePattern matches the first uncommented line that sets key. Group 1 is
// everything up to the value and group 2 the value.
func sysDirectivePattern(style, key string) *regexp.Regexp {
	k := regexp.QuoteMeta(key)
	if style == "nginx" {
		return regexp.MustCompile(`(?m)^([ \t]*` + k + `[ \t]+)([^;\n]*?)[ \t]*;`)
	}
	return regexp.MustCompile(`(?m)^([ \t]*` + k + `[ \t]*=[ \t]*)(.*?)[ \t]*$`)
}

// sysFindDirective returns the value span start of the first uncommented key line.
func sysFindDirective(content, style, key string) (start int, value string, found bool) {
	loc := sysDirectivePattern(style, key).FindStringSubmatchIndex(content)
	if loc == nil {
		return 0, "", false
	}
	return loc[4], content[loc[4]:loc[5]], true
}

// sysSetDirective replaces the value of the first uncommented key line.
func sysSetDirective(content, style, key, value string) (string, bool) {
	loc := sysDirectivePattern(style, key).FindStringSubmatchIndex(content)
	if loc == nil {
		return content, false
	}
	return content[:loc[4]] + value + content[loc[5]:], true
}

// sysEditorNormalize turns CRLF into LF and gives the panel crontab its final newline.
func sysEditorNormalize(def sysEditorDef, s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if def.family == "panel-cron" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

func sysBadRequest(w http.ResponseWriter, msg string, args ...string) {
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: msg, Args: args}})
}

func sysConflict(w http.ResponseWriter, code, msg string, args ...string) {
	writeJSON(w, http.StatusConflict, APIError{Error: ErrorBody{Code: code, Message: msg, Args: args}})
}

// sysEditorWrite is one file the save job writes.
type sysEditorWrite struct {
	id, path, content string
}

// editorSave is PUT /api/v1/server/editors/{service}. Everything that can be refused
// is checked before the job starts; the job then writes the changed files in order.
// The hash check and the writes aren't atomic: an admin who saves in the gap between
// them is covered by the 409 busy of a second job, not by a lock on the file. When the
// job fails, the files already written are its steps minus the last (the failing one).
func (sys *sysAPI) editorSave(w http.ResponseWriter, r *http.Request) {
	name, def, ok := sysEditorLookup(w, r)
	if !ok {
		return
	}
	if def.readOnly {
		// The UI doesn't need this reply: EditorView.ReadOnly tells it to show the info
		// text and disable Save. It is still answered, with the extra message field.
		writeJSON(w, http.StatusOK, struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}{"info", sysCronInfoMsg})
		return
	}
	if def.blocked != "" {
		sysConflict(w, "php_multi_ini", def.blocked)
		return
	}
	var req EditorSaveRequest
	if !decodeBody(w, r, &req) {
		return
	}
	known := map[string]bool{}
	for _, f := range def.files {
		known[f.id] = true
	}
	submitted := map[string]EditorSaveFile{}
	for _, f := range req.Files {
		if !known[f.ID] || submitted[f.ID].ID != "" {
			sysBadRequest(w, "Invalid value for %s", f.ID)
			return
		}
		if strings.ContainsRune(f.Content, 0) {
			sysBadRequest(w, "Invalid value for %s", f.ID)
			return
		}
		submitted[f.ID] = f
	}
	// Directives are checked in the editor's order, unknown names (sorted) first, so
	// the error names the same directive every time. Values lose surrounding space and
	// a stray \r (CRLF from a browser).
	directives := map[string]string{}
	var unknown []string
	for d := range req.Directives {
		if !slices.Contains(def.directives, d) {
			unknown = append(unknown, d)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		sysBadRequest(w, sysDirectiveMsg, unknown[0])
		return
	}
	for _, d := range def.directives {
		v, set := req.Directives[d]
		if !set {
			continue
		}
		v = strings.TrimSpace(v)
		if !sysDirectiveValue.MatchString(v) {
			sysBadRequest(w, sysDirValueMsg, d)
			return
		}
		directives[d] = v
	}
	files := sys.editorFiles(r.Context(), def)
	// An id that didn't resolve (an optional dovecot file this server lacks) is refused.
	resolved := map[string]bool{}
	for _, f := range files {
		resolved[f.id] = true
	}
	for id := range submitted {
		if !resolved[id] {
			sysBadRequest(w, "Invalid value for %s", id)
			return
		}
	}
	// Blank texts are refused before anything is read.
	for _, f := range files {
		if s, ok := submitted[f.id]; ok && strings.TrimSpace(s.Content) == "" {
			sysBadRequest(w, blankFieldMsg, f.path)
			return
		}
	}
	var writes []sysEditorWrite
	var unchanged []string
	for i, rf := range files {
		fresh := sys.readEditorFile(r.Context(), rf)
		if fresh.Error != "" {
			sysConflict(w, "read_failed", sysEditorUnreadMsg, rf.path)
			return
		}
		content := fresh.Content
		sub, hasSub := submitted[rf.id]
		if hasSub {
			if sub.Hash != fresh.Hash {
				sysConflict(w, "changed", sysEditorChanged, rf.path)
				return
			}
			content = sysEditorNormalize(def, sub.Content)
		}
		if i == 0 && len(directives) > 0 {
			for _, d := range def.directives {
				v, set := directives[d]
				if !set {
					continue
				}
				patched, found := sysSetDirective(content, def.style, d, v)
				if !found {
					sysBadRequest(w, sysDirectiveMsg, d)
					return
				}
				content = patched
				hasSub = true
			}
		}
		switch {
		case content != fresh.Content:
			writes = append(writes, sysEditorWrite{id: rf.id, path: rf.path, content: content})
		case hasSub:
			unchanged = append(unchanged, rf.id)
		}
	}
	restart := "no"
	if req.Restart || def.family == "panel-cron" {
		restart = "yes"
	}
	sys.startJob(w, r, stepName(StepSaveConfigMsgid, def.title), func(ctx context.Context, j *jobs.Job) (any, error) {
		res := EditorSaveResult{Changed: []string{}, Unchanged: append([]string{}, unchanged...)}
		for _, wf := range writes {
			if err := sys.saveEditorFile(ctx, j, def, wf, restart); err != nil {
				return nil, sys.editorFailure(ctx, j, name, def, err)
			}
			res.Changed = append(res.Changed, wf.id)
		}
		res.Untested = len(res.Changed) > 0 && restart == "no" && !def.checked
		return res, nil
	})
}

// saveEditorFile hands content to the core through a private temp file, flushed and
// closed before the command runs, and removes it afterwards (gotcha 21).
func (sys *sysAPI) saveEditorFile(ctx context.Context, j *jobs.Job, def sysEditorDef, wf sysEditorWrite, restart string) error {
	f, err := os.CreateTemp(sys.tmpDir(), "hostisan-conf-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(wf.content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if def.phpVersion != "" {
		_, err = sys.step(ctx, j, stepName(StepSaveMsgid, wf.path), sysTimeoutService, "v-change-sys-php-ini", def.phpVersion, f.Name(), restart)
		return err
	}
	_, err = sys.step(ctx, j, stepName(StepSaveMsgid, wf.path), sysTimeoutService, "v-change-sys-service-config", f.Name(), wf.id, restart)
	return err
}

// editorFailure turns a failed save into the job's error. Exit 12 means the service's
// checker refused the new file and the core put the old one back: its output, after
// the core's own "configuration test failed" line, is shown (C7 S8). Exit 20 means the
// new file would not start and the core put the old one back: the service is
// restarted once more and its state reported (decision 15).
func (sys *sysAPI) editorFailure(ctx context.Context, j *jobs.Job, name string, def sysEditorDef, err error) error {
	var ce *runner.CommandError
	if errors.As(err, &ce) && ce.Exit == runner.ExitParsing {
		_, details, _ := strings.Cut(strings.TrimSpace(ce.Output), "\n")
		if details = strings.TrimSpace(details); details == "" {
			return &jobs.Failure{Code: "config_invalid", Message: sysConfigBadMsg}
		}
		return &jobs.Failure{Code: "config_invalid", Message: sysConfigBadMsg + "\n%s", Args: []string{details}}
	}
	if !errors.As(err, &ce) || ce.Exit != runner.ExitRestart || def.svc == "" {
		return err
	}
	_, _ = sys.step(ctx, j, stepName(StepRestartMsgid, def.svc), sysTimeoutService, "v-restart-service", def.svc)
	j.Step(stepName(StepCheckMsgid, def.svc))
	state := ""
	if rows, lerr := sys.services(ctx); lerr == nil {
		if s, ok := rows[name]; ok {
			state = s.STATE
		} else if s, ok := rows[def.svc]; ok {
			state = s.STATE
		}
	}
	return &jobs.Failure{Code: "restart_failed", Message: sysRestartFailMsg, Args: []string{state}}
}
