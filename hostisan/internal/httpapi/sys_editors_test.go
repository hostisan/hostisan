package httpapi

// Slice A tests: config editors.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

func sysStat(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return st.Mode().Perm().String(), nil
}

func sysReadFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func sysHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (e *sysSvcEnv) editor(name string) EditorView {
	e.t.Helper()
	var v EditorView
	if code := e.do("GET", "/editors/"+name, nil, &v); code != 200 {
		e.t.Fatalf("GET editor %s: %d", name, code)
	}
	return v
}

func sysFile(v EditorView, id string) EditorFile {
	for _, f := range v.Files {
		if f.ID == id {
			return f
		}
	}
	return EditorFile{}
}

func TestSysEditorReadPlain(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	from := e.mark()
	v := e.editor("ssh")
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/ssh/sshd_config")
	f := sysFile(v, "ssh")
	if v.Family != "plain" || v.Title != "SSH" || !v.Restart || v.ReadOnly || v.SaveBlocked != "" ||
		len(v.Files) != 1 || f.Path != "/etc/ssh/sshd_config" || !strings.HasPrefix(f.Content, "Include ") ||
		f.Hash != sysHash(f.Content) || f.Error != "" {
		t.Fatalf("view = %+v", v)
	}
}

func TestSysEditorReadFailureIsShownAndRefusesSave(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-open-fs-config": {
			{Args: []string{"/etc/postgresql/16/main/pg_hba.conf"}, Stdout: "Error: /etc/postgresql/16/main/pg_hba.conf file doesn't exist\n", Exit: 3},
			{Args: []string{"/etc/postgresql/16/main/postgresql.conf"}, Stdout: "port = 5432\n", Exit: 0},
		},
	}}, nil)
	v := e.editor("postgresql")
	hba, main := sysFile(v, "postgresql-hba"), sysFile(v, "postgresql")
	if hba.Error == "" || hba.Content != "" || hba.Hash != "" || main.Error != "" || main.Content != "port = 5432\n" {
		t.Fatalf("files = %+v", v.Files)
	}
	if v.Family != "multi" || len(v.Files) != 2 || v.Files[0].ID != "postgresql-hba" {
		t.Errorf("view = %+v", v)
	}
	from := e.mark()
	var er APIError
	code := e.do("PUT", "/editors/postgresql", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "postgresql", Content: "port = 5433\n", Hash: main.Hash}}, Restart: true}, &er)
	if code != 409 || er.Error.Code != "read_failed" {
		t.Fatalf("save: %d %+v", code, er)
	}
	for _, l := range e.log(from) {
		if strings.HasPrefix(l, "v-change-") {
			t.Errorf("ran %q", l)
		}
	}
}

func TestSysEditorStaleHashIs409(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	from := e.mark()
	var er APIError
	code := e.do("PUT", "/editors/ssh", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "ssh", Content: f.Content + "# x\n", Hash: sysHash("older text")}}, Restart: true}, &er)
	if code != 409 || er.Error.Code != "changed" {
		t.Fatalf("%d %+v", code, er)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/ssh/sshd_config")
}

func TestSysEditorUnchangedRunsNoCommand(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	from := e.mark()
	// CRLF is turned into LF first, so this text is the same as the file.
	st := e.job("PUT", "/editors/ssh", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "ssh", Content: strings.ReplaceAll(f.Content, "\n", "\r\n"), Hash: f.Hash}}, Restart: true})
	var res EditorSaveResult
	sysResult(t, st, &res)
	if st.State != "done" || len(res.Changed) != 0 || !slices.Equal(res.Unchanged, []string{"ssh"}) {
		t.Fatalf("job = %+v result %+v", st, res)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/ssh/sshd_config")
}

func TestSysEditorBlankIs400(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	from := e.mark()
	var er APIError
	code := e.do("PUT", "/editors/ssh", EditorSaveRequest{Files: []EditorSaveFile{{ID: "ssh", Content: " \r\n\n", Hash: f.Hash}}}, &er)
	if code != 400 || er.Error.Code != "invalid" || er.Error.Message != `Field "%s" can not be blank.` ||
		!slices.Equal(er.Error.Args, []string{"/etc/ssh/sshd_config"}) {
		t.Fatalf("%d %+v", code, er)
	}
	sysWantLog(t, e.log(from))
}

func TestSysEditorRejectsBadBodies(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	from := e.mark()
	for name, req := range map[string]EditorSaveRequest{
		"nul":        {Files: []EditorSaveFile{{ID: "ssh", Content: "a\x00b\n", Hash: f.Hash}}},
		"unknown id": {Files: []EditorSaveFile{{ID: "nginx", Content: "a\n", Hash: f.Hash}}},
		"duplicate":  {Files: []EditorSaveFile{{ID: "ssh", Content: "a\n", Hash: f.Hash}, {ID: "ssh", Content: "b\n", Hash: f.Hash}}},
		"too big":    {Files: []EditorSaveFile{{ID: "ssh", Content: strings.Repeat("a", 1<<20+10) + "\n", Hash: f.Hash}}},
	} {
		if code := e.do("PUT", "/editors/ssh", req, nil); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	sysWantLog(t, e.log(from))
}

func TestSysEditorNginxDirectiveSave(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	v := e.editor("nginx")
	if v.Family != "nginx" || v.Directives["worker_connections"] != "1024" || v.Directives["gzip"] != "on" ||
		v.Directives["client_max_body_size"] != "256m" || len(v.Directives) != 10 {
		t.Fatalf("directives = %v", v.Directives)
	}
	orig := sysFile(v, "nginx").Content
	from := e.mark()
	st := e.job("PUT", "/editors/nginx", EditorSaveRequest{
		Directives: map[string]string{"worker_connections": "2048", "client_max_body_size": "512m"}, Restart: true})
	var res EditorSaveResult
	sysResult(t, st, &res)
	if st.State != "done" || !slices.Equal(res.Changed, []string{"nginx"}) || len(res.Unchanged) != 0 {
		t.Fatalf("job = %+v (%+v) result %+v", st, st.Error, res)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/nginx/nginx.conf", "v-change-sys-service-config <tmp> nginx yes")
	want := strings.Replace(strings.Replace(orig, "worker_connections 1024;", "worker_connections 2048;", 1),
		"client_max_body_size 256m;", "client_max_body_size 512m;", 1)
	if len(e.save.saves) != 1 || e.save.saves[0].Content != want || e.save.saves[0].Mode != "-rw-------" {
		t.Fatalf("saved = %+v", e.save.saves)
	}
	if _, err := os.Stat(e.save.saves[0].Path); !os.IsNotExist(err) {
		t.Errorf("temp file still there: %v", err)
	}
	if left, _ := os.ReadDir(e.tmp); len(left) != 0 {
		t.Errorf("temp dir not empty: %v", left)
	}
}

func TestSysEditorDirectiveRules(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for name, c := range map[string]struct {
		svc string
		d   map[string]string
		arg string
	}{
		"unknown":         {"nginx", map[string]string{"bogus": "1"}, "bogus"},
		"not in the file": {"nginx", map[string]string{"proxy_buffering": "on"}, "proxy_buffering"},
		"bad value":       {"nginx", map[string]string{"worker_connections": "10;\nevil"}, "worker_connections"},
		"empty value":     {"nginx", map[string]string{"worker_connections": ""}, "worker_connections"},
		"long value":      {"nginx", map[string]string{"worker_connections": strings.Repeat("1", 65)}, "worker_connections"},
		"wrong family":    {"ssh", map[string]string{"Port": "22"}, "Port"},
		"commented only":  {"nginx", map[string]string{"multi_accept": "off"}, ""}, // present: allowed? no, not a basic directive
	} {
		from := e.mark()
		var er APIError
		code := e.do("PUT", "/editors/"+c.svc, EditorSaveRequest{Directives: c.d, Restart: true}, &er)
		if code != 400 || er.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", name, code, er)
			continue
		}
		if c.arg != "" && !slices.Equal(er.Error.Args, []string{c.arg}) {
			t.Errorf("%s: args %v", name, er.Error.Args)
		}
		for _, l := range e.log(from) {
			if strings.HasPrefix(l, "v-change-") {
				t.Errorf("%s ran %q", name, l)
			}
		}
	}
}

func TestSysEditorPHPAndMySQLDirectives(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	v := e.editor("mysqld")
	if v.Family != "mysql" || v.Directives["max_connections"] != "300" || v.Directives["max_allowed_packet"] != "128M" || len(v.Directives) != 5 {
		t.Fatalf("mysql directives = %v", v.Directives)
	}
	st := e.job("PUT", "/editors/mysqld", EditorSaveRequest{Directives: map[string]string{"max_allowed_packet": "64M"}, Restart: false})
	if st.State != "done" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	// The first uncommented line wins: [mysqld] comes before [mysqldump].
	got := e.save.saves[0].Content
	if !strings.Contains(got, "max_allowed_packet = 64M\ntable_open_cache") || !strings.Contains(got, "quick\nmax_allowed_packet = 16M") {
		t.Errorf("saved = %q", got)
	}
	php := e.editor("php8.3-fpm")
	if php.Family != "php" || php.Directives["memory_limit"] != "256M" || php.Directives["display_errors"] != "Off" ||
		php.Directives["error_reporting"] != "E_ALL & ~E_DEPRECATED & ~E_STRICT" || len(php.Directives) != 7 {
		t.Fatalf("php directives = %v", php.Directives)
	}
}

func TestSysEditorRestartFlagAndLog(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	f := sysFile(e.editor("fail2ban"), "fail2ban")
	from := e.mark()
	st := e.job("PUT", "/editors/fail2ban", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "fail2ban", Content: f.Content + "# note\n", Hash: f.Hash}}, Restart: false})
	if st.State != "done" || !slices.Equal(st.Steps, []string{"Save /etc/fail2ban/jail.local"}) {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/fail2ban/jail.local", "v-change-sys-service-config <tmp> fail2ban no")
	if e.save.saves[0].Content != f.Content+"# note\n" {
		t.Errorf("content = %q", e.save.saves[0].Content)
	}
}

func TestSysEditorRestartFailureRestoresAndReportsState(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-service-config": {{Args: []string{"*", "ssh", "yes"}, Stdout: "Error: ERROR: ssh failed to start with new configuration.\n", Exit: 20}},
	}}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	from := e.mark()
	st := e.job("PUT", "/editors/ssh", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "ssh", Content: f.Content + "Bogus yes\n", Hash: f.Hash}}, Restart: true})
	if st.State != "failed" || st.Error == nil || st.Error.Code != "restart_failed" ||
		st.Error.Message != "The new configuration failed to start, so the previous file was restored." ||
		!slices.Equal(st.Error.Args, []string{"running"}) {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/ssh/sshd_config", "v-change-sys-service-config <tmp> ssh yes",
		"v-restart-service ssh", "v-list-sys-services json")
	if left, _ := os.ReadDir(e.tmp); len(left) != 0 {
		t.Errorf("temp dir not empty: %v", left)
	}
}

func TestSysEditorOtherFailureIsNotARestartFailure(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-service-config": {{Args: []string{"*", "*", "*"}, Stdout: "Error: boom\n", Exit: 3}},
	}}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	from := e.mark()
	st := e.job("PUT", "/editors/ssh", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "ssh", Content: f.Content + "# x\n", Hash: f.Hash}}, Restart: true})
	if st.State != "failed" || st.Error.Code != "not_found" || st.Error.Message != "Error: boom" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/ssh/sshd_config", "v-change-sys-service-config <tmp> ssh yes")
}

func TestSysEditorDovecotStopsAtFirstFailure(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-service-config": {
			{Args: []string{"*", "dovecot-2", "*"}, Stdout: "Error: boom\n", Exit: 1},
			{Args: []string{"*", "*", "*"}, Exit: 0},
		},
	}}, nil)
	v := e.editor("dovecot")
	if v.Family != "dovecot" || len(v.Files) != 9 || v.Files[0].ID != "dovecot" || v.Files[8].ID != "dovecot-8" ||
		v.Files[3].Path != "/etc/dovecot/conf.d/10-mail.conf" {
		t.Fatalf("view = %+v", v)
	}
	var req EditorSaveRequest
	req.Restart = true
	// Send them in reverse: the save order is the editor's, not the request's.
	for _, id := range []string{"dovecot-3", "dovecot-2", "dovecot-1", "dovecot"} {
		f := sysFile(v, id)
		req.Files = append(req.Files, EditorSaveFile{ID: id, Content: f.Content + "# edit\n", Hash: f.Hash})
	}
	from := e.mark()
	st := e.job("PUT", "/editors/dovecot", req)
	if st.State != "failed" || st.Error == nil || st.Error.Message != "Error: boom" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	var reads []string
	for _, c := range e.h.fixture.Calls()[from:] {
		if c.Cmd == "v-open-fs-config" {
			reads = append(reads, c.Args[0])
		}
	}
	if len(reads) != 9 {
		t.Errorf("reads = %v", reads)
	}
	var writes []string
	for _, l := range e.log(from) {
		if strings.HasPrefix(l, "v-change-") {
			writes = append(writes, l)
		}
	}
	sysWantLog(t, writes, "v-change-sys-service-config <tmp> dovecot yes",
		"v-change-sys-service-config <tmp> dovecot-1 yes", "v-change-sys-service-config <tmp> dovecot-2 yes")
	if first := e.log(from)[0]; first != "v-list-sys-dovecot-config json" {
		t.Errorf("first command = %q", first)
	}
}

func TestSysEditorDovecotRHELLayoutHasOneFile(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-dovecot-config": {{Args: []string{"json"}, Stdout: `{"CONFIG":{"config_path":"/etc/dovecot/dovecot.conf","config_path1":""}}`}},
	}}, nil)
	if v := e.editor("dovecot"); len(v.Files) != 1 || v.Files[0].ID != "dovecot" {
		t.Fatalf("files = %+v", v.Files)
	}
}

func TestSysEditorBind9SavesOptionsFirst(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	v := e.editor("bind9")
	if v.Family != "multi" || len(v.Files) != 2 || v.Files[0].ID != "bind9-opt" || v.Files[1].ID != "bind9" {
		t.Fatalf("view = %+v", v)
	}
	req := EditorSaveRequest{Restart: true}
	for _, id := range []string{"bind9", "bind9-opt"} {
		f := sysFile(v, id)
		req.Files = append(req.Files, EditorSaveFile{ID: id, Content: f.Content + "// x\n", Hash: f.Hash})
	}
	from := e.mark()
	if st := e.job("PUT", "/editors/bind9", req); st.State != "done" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from),
		"v-open-fs-config /etc/bind/named.conf.options", "v-open-fs-config /etc/bind/named.conf",
		"v-change-sys-service-config <tmp> bind9-opt yes", "v-change-sys-service-config <tmp> bind9 yes")
}

// TestPHPMultiIniStillBlocked: the bare php/php-fpm rows edit "every php.ini"
// (v-change-sys-service-config php), so their save stays refused.
func TestPHPMultiIniStillBlocked(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for _, name := range []string{"php-fpm", "php5-fpm", "php"} {
		v := e.editor(name)
		if v.Family != "php" || v.Title != "PHP" || v.SaveBlocked != "Saving would overwrite every php.ini on this server. Edit this file over SSH." {
			t.Errorf("%s view = %+v", name, v)
		}
	}
	from := e.mark()
	var er APIError
	code := e.do("PUT", "/editors/php-fpm", EditorSaveRequest{Directives: map[string]string{"memory_limit": "512M"}, Restart: true}, &er)
	if code != 409 || er.Error.Code != "php_multi_ini" ||
		er.Error.Message != "Saving would overwrite every php.ini on this server. Edit this file over SSH." {
		t.Fatalf("%d %+v", code, er)
	}
	sysWantLog(t, e.log(from))
}

// TestPHPIniSaveUsesSingleFileScript: a per-version row saves only its own php.ini,
// through v-change-sys-php-ini (Plan 6 B3b).
func TestPHPIniSaveUsesSingleFileScript(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	v := e.editor("php8.3-fpm")
	if v.SaveBlocked != "" || len(v.Files) != 1 || v.Files[0].Path != "/etc/php/8.3/fpm/php.ini" ||
		!strings.Contains(v.Files[0].Content, "PHP 8.3 FPM") {
		t.Fatalf("view = %+v", v)
	}
	orig := v.Files[0].Content
	from := e.mark()
	st := e.job("PUT", "/editors/php8.3-fpm", EditorSaveRequest{Directives: map[string]string{"memory_limit": "512M"}, Restart: true})
	var res EditorSaveResult
	sysResult(t, st, &res)
	if st.State != "done" || !slices.Equal(res.Changed, []string{"php"}) {
		t.Fatalf("job = %+v (%+v) result %+v", st, st.Error, res)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/php/8.3/fpm/php.ini", "v-change-sys-php-ini 8.3 <tmp> yes")
	_, old, _ := sysFindDirective(orig, "ini", "memory_limit")
	want, _ := sysSetDirective(orig, "ini", "memory_limit", "512M")
	if old == "512M" || len(e.save.saves) != 1 || e.save.saves[0].Content != want || e.save.saves[0].Mode != "-rw-------" ||
		!strings.HasPrefix(e.save.saves[0].Path, e.tmp+"/") {
		t.Fatalf("saved = %+v", e.save.saves)
	}
	if left, _ := os.ReadDir(e.tmp); len(left) != 0 {
		t.Errorf("temp dir not empty: %v", left)
	}

	// Without the restart box the script is told not to restart.
	v = e.editor("php8.3-fpm")
	from = e.mark()
	st = e.job("PUT", "/editors/php8.3-fpm", EditorSaveRequest{Directives: map[string]string{"memory_limit": "64M"}})
	if st.State != "done" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/php/8.3/fpm/php.ini", "v-change-sys-php-ini 8.3 <tmp> no")
}

// TestPHPIniRestartFailureRestartsThatVersion: exit 20 from the script means the old
// php.ini is back; the job restarts that version's FPM, not every one.
func TestPHPIniRestartFailureRestartsThatVersion(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-php-ini": {{Args: []string{"8.3", "*", "yes"}, Stdout: "Error: ERROR: php8.3-fpm failed to start with new configuration.\n", Exit: 20}},
	}}, nil)
	from := e.mark()
	st := e.job("PUT", "/editors/php8.3-fpm", EditorSaveRequest{Directives: map[string]string{"memory_limit": "512M"}, Restart: true})
	if st.State != "failed" || st.Error == nil || st.Error.Code != "restart_failed" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/php/8.3/fpm/php.ini", "v-change-sys-php-ini 8.3 <tmp> yes",
		"v-restart-service php8.3-fpm", "v-list-sys-services json")
}

func TestSysEditorPanelCronEndsWithNewline(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	v := e.editor("hestiaweb")
	f := sysFile(v, "hestiaweb")
	if v.Family != "panel-cron" || v.Restart || v.ReadOnly || v.Title != "Panel Cronjobs" || f.Path != "/var/spool/cron/crontabs/hestiaweb" {
		t.Fatalf("view = %+v", v)
	}
	text := strings.TrimSuffix(f.Content, "\n") + "\n# more"
	from := e.mark()
	st := e.job("PUT", "/editors/hestiaweb", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "hestiaweb", Content: strings.ReplaceAll(text, "\n", "\r\n"), Hash: f.Hash}}, Restart: false})
	if st.State != "done" {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	// The checkbox is ignored: the core always gets yes, as in PHP.
	sysWantLog(t, e.log(from), "v-open-fs-config /var/spool/cron/crontabs/hestiaweb", "v-change-sys-service-config <tmp> hestiaweb yes")
	if got := e.save.saves[0].Content; got != text+"\n" || strings.Contains(got, "\r") {
		t.Errorf("content = %q", got)
	}
}

func TestSysEditorCronIsReadOnly(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for _, name := range []string{"cron", "crond"} {
		v := e.editor(name)
		if v.Family != "cron" || !v.ReadOnly || v.Restart || len(v.Files) != 1 || v.Files[0].Path != "/etc/crontab" || v.Title != "CRON" {
			t.Fatalf("%s view = %+v", name, v)
		}
		from := e.mark()
		var out struct{ Status, Message string }
		if code := e.do("PUT", "/editors/"+name, EditorSaveRequest{
			Files: []EditorSaveFile{{ID: "cron", Content: "x\n", Hash: v.Files[0].Hash}}}, &out); code != 200 ||
			out.Status != "info" || out.Message != "Info (read-only mode): Crontab can only be edited via SSH." {
			t.Fatalf("%s save: %d %+v", name, code, out)
		}
		sysWantLog(t, e.log(from))
	}
}

func TestSysEditorLegacyAliases(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	cases := []struct{ name, family, title, id, path string }{
		{"mysqld", "mysql", "MYSQL", "mysqld", "/etc/mysql/my.cnf"},
		{"mariadb", "mysql", "MARIADB", "mariadb", "/etc/mysql/my.cnf"},
		{"mysql", "mysql", "MYSQL", "mysql", "/etc/mysql/my.cnf"},
		{"clamd", "plain", "CLAMAV", "clamd", "/etc/clamav/clamd.conf"},
		{"clamav-daemon", "plain", "CLAMAV", "clamd", "/etc/clamav/clamd.conf"},
		{"spamd", "plain", "SPAMASSASSIN", "spamd", "/etc/spamassassin/local.cf"},
		{"spamassassin", "plain", "SPAMASSASSIN", "spamassassin", "/etc/spamassassin/local.cf"},
		{"exim4", "plain", "EXIM", "exim4", "/etc/exim4/exim4.conf.template"},
		{"exim", "plain", "EXIM", "exim", "/etc/exim/exim.conf"},
		{"named", "plain", "NAMED", "named", "/etc/named.conf"},
		{"httpd", "plain", "HTTPD", "httpd", "/etc/httpd/conf/httpd.conf"},
		{"apache2", "plain", "APACHE2", "apache2", "/etc/apache2/apache2.conf"},
		{"vsftpd", "plain", "VSFTPD", "vsftpd", "/etc/vsftpd.conf"},
		{"proftpd", "plain", "PROFTPD", "proftpd", "/etc/proftpd/proftpd.conf"},
	}
	for _, c := range cases {
		v := e.editor(c.name)
		if v.Service != c.name || v.Family != c.family || v.Title != c.title || len(v.Files) != 1 ||
			v.Files[0].ID != c.id || v.Files[0].Path != c.path || v.Files[0].Error != "" || v.Files[0].Content == "" {
			t.Errorf("%s = %+v", c.name, v)
		}
	}
}

func TestSysEditorUnknownAndNoEditor(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for path, want := range map[string]int{"iptables": 404, "web-terminal": 404, "nosuchsvc": 400, "bad%3Bname": 400} {
		from := e.mark()
		if code := e.do("GET", "/editors/"+path, nil, nil); code != want {
			t.Errorf("GET %s: %d, want %d", path, code, want)
		}
		if code := e.do("PUT", "/editors/"+path, EditorSaveRequest{}, nil); code != want {
			t.Errorf("PUT %s: %d, want %d", path, code, want)
		}
		sysWantLog(t, e.log(from))
	}
}

func TestSysEditorBrandsReadErrors(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-open-fs-config": {{Args: []string{"*"}, Stdout: "Error: Hestia could not open it\n", Exit: 3}},
	}}, nil)
	if f := sysFile(e.editor("ssh"), "ssh"); f.Error != "Error: Hostisan could not open it" {
		t.Errorf("error = %q", f.Error)
	}
}

var _ = hestia.SysInfo{}
var _ runner.Runner

func TestSysEditorPHPPathFromJSONReply(t *testing.T) {
	// The core prints directives first and config_path last; the json reply names it.
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-php-config": {{Args: []string{"json"}, Stdout: `{"CONFIG":{"memory_limit":"256M","config_path":"/etc/php/8.2/fpm/php.ini"}}`}},
	}}, nil)
	for _, name := range []string{"php-fpm", "php5-fpm", "php"} {
		from := e.mark()
		v := e.editor(name)
		sysWantLog(t, e.log(from), "v-list-sys-php-config json", "v-open-fs-config /etc/php/8.2/fpm/php.ini")
		if len(v.Files) != 1 || v.Files[0].Path != "/etc/php/8.2/fpm/php.ini" || v.Files[0].Error != "" {
			t.Errorf("%s files = %+v", name, v.Files)
		}
	}
}

func TestSysEditorPHPWrongShapeReplyFailsCleanly(t *testing.T) {
	for name, out := range map[string]string{
		"not json":  "memory_limit = 256M\nconfig_path /etc/php/8.2/fpm/php.ini\n",
		"no path":   `{"CONFIG":{"memory_limit":"256M"}}`,
		"no config": `{}`,
	} {
		e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
			"v-list-sys-php-config": {{Args: []string{"json"}, Stdout: out}},
		}}, nil)
		from := e.mark()
		v := e.editor("php-fpm")
		if len(v.Files) != 1 || v.Files[0].Error == "" || v.Files[0].Content != "" {
			t.Errorf("%s: files = %+v", name, v.Files)
		}
		sysWantLog(t, e.log(from), "v-list-sys-php-config json")
	}
}

func TestSysEditorDirectiveValuesAreTrimmedAndChecked(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for _, v := range []string{"", "  ", "\r", " \r\n"} {
		var er APIError
		if code := e.do("PUT", "/editors/nginx", EditorSaveRequest{Directives: map[string]string{"gzip": v}}, &er); code != 400 ||
			!slices.Equal(er.Error.Args, []string{"gzip"}) {
			t.Errorf("%q: %d %+v", v, code, er)
		}
	}
	if st := e.job("PUT", "/editors/nginx", EditorSaveRequest{Directives: map[string]string{"gzip": " off\r"}}); st.State != "done" ||
		!strings.Contains(e.save.saves[0].Content, "gzip off;") {
		t.Fatalf("job = %+v saved %+v", st, e.save.saves)
	}
}

func TestSysEditorDirectiveErrorsFollowEditorOrder(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	var er APIError
	e.do("PUT", "/editors/nginx", EditorSaveRequest{Directives: map[string]string{"gzip": "bad;", "worker_processes": "bad;"}}, &er)
	if !slices.Equal(er.Error.Args, []string{"worker_processes"}) {
		t.Errorf("args = %v", er.Error.Args)
	}
}

func TestSysEditorUnresolvedOptionalDovecotIDIsRefused(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-dovecot-config": {{Args: []string{"json"}, Stdout: `{"CONFIG":{"config_path":"/etc/dovecot/dovecot.conf","config_path1":""}}`}},
	}}, nil)
	f := sysFile(e.editor("dovecot"), "dovecot")
	from := e.mark()
	var er APIError
	if code := e.do("PUT", "/editors/dovecot", EditorSaveRequest{Files: []EditorSaveFile{
		{ID: "dovecot-3", Content: "x\n", Hash: f.Hash}}}, &er); code != 400 {
		t.Fatalf("%d %+v", code, er)
	}
	for _, l := range e.log(from) {
		if strings.HasPrefix(l, "v-change-") || strings.HasPrefix(l, "v-open-") {
			t.Errorf("ran %q", l)
		}
	}
}

func TestSysEditorReadsUpstreamExit127(t *testing.T) {
	// Upstream's script never sources func/main.sh, so its closing $BIN/v-log-action
	// runs as /v-log-action and it exits 127 after printing the file.
	rec := recordedEntry(t, "v-open-fs-config", "/etc/ssh/sshd_config")
	if rec.Exit != 127 || !strings.Contains(rec.Stderr, "/v-log-action: No such file or directory") {
		t.Fatalf("recording changed: %+v", rec)
	}
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{"v-open-fs-config": {rec}}}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	if f.Error != "" || f.Content != rec.Stdout || f.Hash != sysHash(rec.Stdout) {
		t.Fatalf("file = %+v", f)
	}
}

func TestSysEditorExit127OtherwiseIsAnError(t *testing.T) {
	for name, entry := range map[string]fixtureEntry{
		"other stderr": {Stdout: "Port 22\n", Stderr: "sudo: v-open-fs-config: command not found\n", Exit: 127},
		"no stderr":    {Stdout: "Port 22\n", Exit: 127},
		"log line plus another": {Stdout: "Port 22\n", Exit: 127,
			Stderr: "cat: x: Input/output error\n/usr/local/hestia/bin/v-open-fs-config: line 60: /v-log-action: No such file or directory\n"},
		"error text": {Stdout: "Error: file /etc/ssh/sshd_config was not opened\n", Exit: 127,
			Stderr: "/usr/local/hestia/bin/v-open-fs-config: line 60: /v-log-action: No such file or directory\n"},
	} {
		entry.Args = []string{"/etc/ssh/sshd_config"}
		e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{"v-open-fs-config": {entry}}}, nil)
		if f := sysFile(e.editor("ssh"), "ssh"); f.Error == "" || f.Content != "" {
			t.Errorf("%s: file = %+v", name, f)
		}
	}
}

// C7 S8: the core tests a file with the service's own checker (exit 12 when it
// refuses it, after putting the old file back).
func TestSysEditorCheckerRefusalIsShownWithItsOutput(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-service-config": {{Args: []string{"*", "nginx", "no"},
			Stdout: "Error: ERROR: nginx configuration test failed.\nnginx: [emerg] unknown directive \"bogus\" in /etc/nginx/nginx.conf:3\nnginx: configuration file /etc/nginx/nginx.conf test failed\n",
			Exit:   12}},
	}}, nil)
	f := sysFile(e.editor("nginx"), "nginx")
	from := e.mark()
	st := e.job("PUT", "/editors/nginx", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "nginx", Content: f.Content + "bogus on;\n", Hash: f.Hash}}, Restart: false})
	if st.State != "failed" || st.Error == nil || st.Error.Code != "config_invalid" ||
		st.Error.Message != "The new configuration has errors, so the previous file was restored.\n%s" ||
		!slices.Equal(st.Error.Args, []string{"nginx: [emerg] unknown directive \"bogus\" in /etc/nginx/nginx.conf:3\nnginx: configuration file /etc/nginx/nginx.conf test failed"}) {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	// Nothing is restarted: the old file never left.
	sysWantLog(t, e.log(from), "v-open-fs-config /etc/nginx/nginx.conf", "v-change-sys-service-config <tmp> nginx no")
}

func TestSysEditorCheckerRefusalWithoutOutput(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-service-config": {{Args: []string{"*", "ssh", "*"},
			Stdout: "Error: ERROR: ssh configuration test failed.\n\n", Exit: 12}},
	}}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	st := e.job("PUT", "/editors/ssh", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "ssh", Content: f.Content + "Bogus yes\n", Hash: f.Hash}}, Restart: true})
	if st.State != "failed" || st.Error.Code != "config_invalid" ||
		st.Error.Message != "The new configuration has errors, so the previous file was restored." || len(st.Error.Args) != 0 {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
}

func TestSysEditorSaysWhichEditorsAreSyntaxChecked(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for name, want := range map[string]bool{"nginx": true, "ssh": true, "bind9": true, "dovecot": true,
		"exim4": true, "fail2ban": true, "apache2": true, "vsftpd": false, "mariadb": false, "postgresql": false,
		"php8.3-fpm": false, "hestiaweb": false} {
		if got := e.editor(name).SyntaxCheck; got != want {
			t.Errorf("%s syntaxCheck = %v, want %v", name, got, want)
		}
	}
}

// A file saved with no checker and no restart is untested; the result says so.
func TestSysEditorUntestedSaveIsFlagged(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		restart  bool
		want     bool
	}{
		{"vsftpd", "vsftpd", false, true},
		{"vsftpd", "vsftpd", true, false},
		{"fail2ban", "fail2ban", false, false},
	} {
		e := sysSvcNew(t, overrides{}, nil)
		f := sysFile(e.editor(tc.name), tc.id)
		st := e.job("PUT", "/editors/"+tc.name, EditorSaveRequest{
			Files: []EditorSaveFile{{ID: tc.id, Content: f.Content + "# note\n", Hash: f.Hash}}, Restart: tc.restart})
		var res EditorSaveResult
		sysResult(t, st, &res)
		if res.Untested != tc.want {
			t.Errorf("%s restart=%v: untested = %v, want %v", tc.name, tc.restart, res.Untested, tc.want)
		}
	}
}
