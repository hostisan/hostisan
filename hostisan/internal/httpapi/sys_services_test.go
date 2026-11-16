package httpapi

// Slice A tests: services list and actions, bulk, reboot. Exact ordered call logs.
// The editor tests are in sys_editors_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

const sysSvcFixtureDir = "../../testdata/fixtures/sys_services"

// sysSvcEnv is a signed-in admin, the server routes on their own job registry and the
// command log.
type sysSvcEnv struct {
	t    *testing.T
	h    *harness
	sys  *sysAPI
	srv  *httptest.Server
	tmp  string
	save *sysCaptureRunner
}

func sysSvcNew(t *testing.T, o overrides, wrap func(runner.Runner) runner.Runner) *sysSvcEnv {
	t.Helper()
	if o.Dir == "" {
		o.Dir = sysSvcFixtureDir
	}
	tmp := t.TempDir()
	cap := &sysCaptureRunner{}
	h := newHarnessWith(t, o, func(d *Deps) {
		d.TmpDir = tmp
		cap.Runner = d.R
		d.R = cap
		if wrap != nil {
			d.R = wrap(d.R)
		}
	})
	h.loginAdmin()
	srv, sys, _ := sysTestServer(h, nil)
	return &sysSvcEnv{t: t, h: h, sys: sys, srv: srv, tmp: tmp, save: cap}
}

func (e *sysSvcEnv) do(method, path string, body, out any) int {
	e.t.Helper()
	return e.h.sysDo(e.srv.URL, method, "/api/v1/server"+path, body, out)
}

// mark is the call index to pass to log later.
func (e *sysSvcEnv) mark() int { return len(e.h.fixture.Calls()) }

var sysTmpArg = regexp.MustCompile(`^(v-change-sys-service-config |v-change-sys-php-ini \S+ )\S+`)

// log is every command run since mark, with the temp file path of a config save
// replaced by <tmp>.
func (e *sysSvcEnv) log(from int) []string {
	out := e.h.sysCommands(from)
	for i, l := range out {
		out[i] = sysTmpArg.ReplaceAllString(strings.TrimSpace(l), "${1}<tmp>")
	}
	return out
}

func (e *sysSvcEnv) wait(ref JobRef) JobStatus {
	e.t.Helper()
	select {
	case <-e.sys.jobs.Done(ref.JobID):
	case <-time.After(5 * time.Second):
		e.t.Fatal("job did not finish")
	}
	st, ok := e.sys.jobs.Get(ref.JobID)
	if !ok {
		e.t.Fatal("job gone")
	}
	return jobStatusBody(st)
}

// job starts a write that answers 202 and waits for the job.
func (e *sysSvcEnv) job(method, path string, body any) JobStatus {
	e.t.Helper()
	var ref JobRef
	if code := e.do(method, path, body, &ref); code != 202 {
		e.t.Fatalf("%s %s: %d", method, path, code)
	}
	return e.wait(ref)
}

func sysResult(t *testing.T, st JobStatus, into any) {
	t.Helper()
	b, _ := json.Marshal(st.Result)
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("result %s: %v", b, err)
	}
}

func sysWantLog(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("commands =\n  %q\nwant\n  %q", got, want)
	}
}

// sysCaptureRunner records the content and mode of each config temp file at the moment
// v-change-sys-service-config or v-change-sys-php-ini runs.
type sysCaptureRunner struct {
	runner.Runner
	mu    sync.Mutex
	saves []sysCapturedSave
}

type sysCapturedSave struct {
	Path    string
	Mode    string
	Content string
}

func (c *sysCaptureRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	file := -1
	switch {
	case cmd == "v-change-sys-service-config" && len(args) > 0:
		file = 0
	case cmd == "v-change-sys-php-ini" && len(args) > 1:
		file = 1
	}
	if file >= 0 {
		c.mu.Lock()
		s := sysCapturedSave{Path: args[file]}
		if st, err := sysStat(args[file]); err == nil {
			s.Mode = st
		}
		s.Content = sysReadFile(args[file])
		c.saves = append(c.saves, s)
		c.mu.Unlock()
	}
	return c.Runner.Run(ctx, cmd, args...)
}

// sysStateRunner makes start and stop work: it remembers the state each command
// sets and applies it to v-list-sys-services.
type sysStateRunner struct {
	runner.Runner
	mu    sync.Mutex
	state map[string]string
}

var sysUnitKeys = map[string]string{"hestia-web-terminal": "web-terminal"}

func (r *sysStateRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	res, err := r.Runner.Run(ctx, cmd, args...)
	if err != nil {
		return res, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := func() string {
		if len(args) == 0 {
			return "iptables"
		}
		if k, ok := sysUnitKeys[args[0]]; ok {
			return k
		}
		return args[0]
	}
	switch cmd {
	case "v-start-service", "v-update-firewall":
		r.state[key()] = "running"
	case "v-stop-service", "v-stop-firewall":
		if cmd == "v-stop-firewall" {
			r.state["iptables"] = "stopped"
		} else {
			r.state[key()] = "stopped"
		}
	case "v-list-sys-services":
		var rows map[string]map[string]string
		if json.Unmarshal(res.Stdout, &rows) == nil {
			for k, st := range r.state {
				if row, ok := rows[k]; ok {
					row["STATE"] = st
				}
			}
			res.Stdout, _ = json.Marshal(rows)
		}
	}
	return res, nil
}

func sysStateWrap(r runner.Runner) runner.Runner {
	return &sysStateRunner{Runner: r, state: map[string]string{}}
}

func TestSysServicesList(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	from := e.mark()
	var v ServicesView
	if code := e.do("GET", "/services", nil, &v); code != 200 {
		t.Fatalf("status %d", code)
	}
	sysWantLog(t, e.log(from), "v-list-sys-info json", "v-list-sys-php json", "v-list-sys-services json")
	var names []string
	editors := map[string]string{}
	for _, r := range v.Services {
		names = append(names, r.Name)
		editors[r.Name] = r.Editor
	}
	want := []string{"apache2", "bind9", "clamav-daemon", "cron", "dovecot", "exim4", "fail2ban", "iptables",
		"mariadb", "nginx", "php8.2-fpm", "php8.3-fpm", "spamassassin", "ssh", "vsftpd", "web-terminal"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %v", names)
	}
	for name, ed := range map[string]string{"php8.2-fpm": "php8.2-fpm", "php8.3-fpm": "php8.3-fpm", "nginx": "nginx",
		"mariadb": "mariadb", "clamav-daemon": "clamav-daemon", "iptables": "", "web-terminal": ""} {
		if editors[name] != ed {
			t.Errorf("editor of %s = %q, want %q", name, editors[name], ed)
		}
	}
	if v.Info.HOSTNAME != "server1.acme-hosting.net" || v.Release != "release" || !v.Firewall {
		t.Errorf("view = %+v", v)
	}
	for _, r := range v.Services {
		if r.Name == "nginx" && (r.State != "running" || r.CPU != "0.1" || r.Mem != "12" || r.RTime != "5143" || r.System != "reverse proxy") {
			t.Errorf("nginx row = %+v", r)
		}
	}
}

func TestSysServicesListFirewallOff(t *testing.T) {
	e := sysSvcNew(t, overrides{Config: map[string]string{"FIREWALL_SYSTEM": ""}}, nil)
	var v ServicesView
	if code := e.do("GET", "/services", nil, &v); code != 200 || v.Firewall {
		t.Fatalf("%d firewall=%v", code, v.Firewall)
	}
}

func TestSysServiceRestartOneSendsYes(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	from := e.mark()
	st := e.job("POST", "/services/nginx/restart", ServiceActionRequest{})
	if st.State != "done" || st.Title != "Restart nginx" || !slices.Equal(st.Steps, []string{"Restart nginx"}) {
		t.Fatalf("job = %+v", st)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json", "v-restart-service nginx yes")
}

func TestSysServiceActionNeedsNoBody(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	if st := e.job("POST", "/services/nginx/restart", nil); st.State != "done" {
		t.Fatalf("job = %+v", st)
	}
}

func TestSysServiceStartAndStopVerifyState(t *testing.T) {
	e := sysSvcNew(t, overrides{}, sysStateWrap)
	from := e.mark()
	if st := e.job("POST", "/services/vsftpd/start", nil); st.State != "done" ||
		!slices.Equal(st.Steps, []string{"Start vsftpd", "Check vsftpd"}) {
		t.Fatalf("start = %+v (%+v)", st, st.Error)
	}
	if st := e.job("POST", "/services/nginx/stop", nil); st.State != "done" {
		t.Fatalf("stop = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from),
		"v-list-sys-services json", "v-start-service vsftpd", "v-list-sys-services json",
		"v-list-sys-services json", "v-stop-service nginx", "v-list-sys-services json")
}

func TestSysServiceStartStillStoppedFails(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil) // vsftpd is stopped and the fixture never changes it
	from := e.mark()
	st := e.job("POST", "/services/vsftpd/start", nil)
	if st.State != "failed" || st.Error == nil || st.Error.Message != `Start "%s" failed` ||
		!slices.Equal(st.Error.Args, []string{"vsftpd"}) {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json", "v-start-service vsftpd", "v-list-sys-services json")
	// A stop that leaves the service running fails the same way.
	st = e.job("POST", "/services/nginx/stop", nil)
	if st.State != "failed" || st.Error.Message != `Stop "%s" failed` || !slices.Equal(st.Error.Args, []string{"nginx"}) {
		t.Fatalf("stop = %+v (%+v)", st, st.Error)
	}
}

func TestSysServiceCommandFailureFailsJob(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-restart-service": {{Args: []string{"*", "*"}, Stdout: "Error: ERROR: Restart of nginx failed.\n", Exit: 20}},
	}}, nil)
	st := e.job("POST", "/services/nginx/restart", nil)
	if st.State != "failed" || st.Error == nil || st.Error.Code != "restart_failed" ||
		st.Error.Message != "Error: ERROR: Restart of nginx failed." {
		t.Fatalf("job = %+v (%+v)", st, st.Error)
	}
}

func TestSysServiceUnknownIs400(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	var er APIError
	// Well formed but not a service: only the list is read.
	from := e.mark()
	if code := e.do("POST", "/services/nosuchsvc/restart", nil, &er); code != 400 || er.Error.Code != "invalid" {
		t.Fatalf("unknown: %d %+v", code, er)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json")
	// Not a service name at all, or not an action: no command at all.
	for _, p := range []string{"/services/bad%3Bname/restart", "/services/nginx/reboot", "/services/nginx/RESTART"} {
		from = e.mark()
		if code := e.do("POST", p, nil, &er); code != 400 || er.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", p, code, er)
		}
		sysWantLog(t, e.log(from))
	}
}

func TestSysServiceWebTerminalActsOnUnit(t *testing.T) {
	e := sysSvcNew(t, overrides{}, sysStateWrap)
	from := e.mark()
	if st := e.job("POST", "/services/web-terminal/restart", nil); st.State != "done" {
		t.Fatalf("restart = %+v", st)
	}
	if st := e.job("POST", "/services/web-terminal/stop", nil); st.State != "done" {
		t.Fatalf("stop = %+v (%+v)", st, st.Error)
	}
	if st := e.job("POST", "/services/web-terminal/start", nil); st.State != "done" {
		t.Fatalf("start = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from),
		"v-list-sys-services json", "v-restart-service hestia-web-terminal yes",
		"v-list-sys-services json", "v-stop-service hestia-web-terminal", "v-list-sys-services json",
		"v-list-sys-services json", "v-start-service hestia-web-terminal", "v-list-sys-services json")
}

func TestSysServiceIptables(t *testing.T) {
	e := sysSvcNew(t, overrides{}, sysStateWrap)
	from := e.mark()
	if st := e.job("POST", "/services/iptables/stop", ServiceActionRequest{Confirm: "iptables"}); st.State != "done" {
		t.Fatalf("stop = %+v (%+v)", st, st.Error)
	}
	if st := e.job("POST", "/services/iptables/start", nil); st.State != "done" {
		t.Fatalf("start = %+v (%+v)", st, st.Error)
	}
	if st := e.job("POST", "/services/iptables/restart", nil); st.State != "done" {
		t.Fatalf("restart = %+v (%+v)", st, st.Error)
	}
	sysWantLog(t, e.log(from),
		"v-list-sys-services json", "v-stop-firewall", "v-list-sys-services json",
		"v-list-sys-services json", "v-update-firewall", "v-list-sys-services json",
		"v-list-sys-services json", "v-update-firewall")
}

func TestSysServiceStopNeedsTypedName(t *testing.T) {
	e := sysSvcNew(t, overrides{}, sysStateWrap)
	for _, c := range []struct{ svc, confirm string }{{"ssh", ""}, {"ssh", "SSH"}, {"iptables", "ssh"}, {"iptables", ""}} {
		from := e.mark()
		var er APIError
		code := e.do("POST", "/services/"+c.svc+"/stop", ServiceActionRequest{Confirm: c.confirm}, &er)
		if code != 400 || er.Error.Code != "confirm_required" {
			t.Errorf("%s/%q: %d %+v", c.svc, c.confirm, code, er)
		}
		sysWantLog(t, e.log(from))
	}
	// Typed, it goes through; other services need no confirm; restart of ssh needs none.
	from := e.mark()
	if st := e.job("POST", "/services/ssh/stop", ServiceActionRequest{Confirm: "ssh"}); st.State != "done" {
		t.Fatalf("stop ssh = %+v (%+v)", st, st.Error)
	}
	if st := e.job("POST", "/services/ssh/restart", nil); st.State != "done" {
		t.Fatalf("restart ssh = %+v", st)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json", "v-stop-service ssh", "v-list-sys-services json",
		"v-list-sys-services json", "v-restart-service ssh yes")
}

func TestSysServicesBulkRestartSendsNoSecondArg(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	from := e.mark()
	st := e.job("POST", "/services/bulk", BulkRequest{Action: "restart", Names: []string{"nginx", "bind9"}})
	var rep BulkReply
	sysResult(t, st, &rep)
	if st.State != "done" || rep.Status != "ok" || len(rep.Failed) != 0 {
		t.Fatalf("job = %+v reply %+v", st, rep)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json", "v-restart-service nginx", "v-restart-service bind9")
}

func TestSysServicesBulkTriesEveryItemAndListsFailures(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-restart-service": {
			{Args: []string{"bind9"}, Stdout: "Error: ERROR: Restart of bind9 failed.\n", Exit: 20},
			{Args: []string{"*"}, Exit: 0},
		},
	}}, nil)
	from := e.mark()
	st := e.job("POST", "/services/bulk", BulkRequest{Action: "restart", Names: []string{"bind9", "nginx", "dovecot"}})
	var rep BulkReply
	sysResult(t, st, &rep)
	if st.State != "done" || len(rep.Failed) != 1 || rep.Failed[0].Name != "bind9" ||
		rep.Failed[0].Message != "Error: ERROR: Restart of bind9 failed." {
		t.Fatalf("job = %+v reply %+v", st, rep)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json",
		"v-restart-service bind9", "v-restart-service nginx", "v-restart-service dovecot")
}

func TestSysServicesBulkStartChecksState(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil) // vsftpd stays stopped, nginx is running
	from := e.mark()
	st := e.job("POST", "/services/bulk", BulkRequest{Action: "start", Names: []string{"vsftpd", "nginx"}})
	var rep BulkReply
	sysResult(t, st, &rep)
	if len(rep.Failed) != 1 || rep.Failed[0].Name != "vsftpd" || rep.Failed[0].Message != `Start "%s" failed` ||
		!slices.Equal(rep.Failed[0].Args, []string{"vsftpd"}) {
		t.Fatalf("reply = %+v", rep)
	}
	sysWantLog(t, e.log(from), "v-list-sys-services json", "v-start-service vsftpd", "v-start-service nginx", "v-list-sys-services json")
}

func TestSysServicesBulkValidation(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for name, req := range map[string]BulkRequest{
		"no names":      {Action: "restart"},
		"bad action":    {Action: "reboot", Names: []string{"nginx"}},
		"bad name":      {Action: "restart", Names: []string{"nginx", "a b"}},
		"unknown name":  {Action: "restart", Names: []string{"nginx", "nosuchsvc"}},
		"stop ssh":      {Action: "stop", Names: []string{"nginx", "ssh"}},
		"stop iptables": {Action: "stop", Names: []string{"iptables"}},
	} {
		from := e.mark()
		var er APIError
		code := e.do("POST", "/services/bulk", req, &er)
		if code != 400 {
			t.Errorf("%s: %d", name, code)
		}
		for _, l := range e.log(from) {
			if !strings.HasPrefix(l, "v-list-") {
				t.Errorf("%s ran %q", name, l)
			}
		}
	}
}

func TestSysServicesBulkStopRefusalNamesTheService(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	var er APIError
	if code := e.do("POST", "/services/bulk", BulkRequest{Action: "stop", Names: []string{"nginx", "ssh"}}, &er); code != 400 ||
		er.Error.Code != "confirm_required" || er.Error.Message != `Stop "%s" from its own row.` || !slices.Equal(er.Error.Args, []string{"ssh"}) {
		t.Fatalf("%d %+v", code, er)
	}
}

func TestSysReboot(t *testing.T) {
	e := sysSvcNew(t, overrides{}, nil)
	for _, host := range []string{"", "wrong.example.com", "SERVER1.acme-hosting.net"} {
		from := e.mark()
		var er APIError
		if code := e.do("POST", "/reboot", RebootRequest{Hostname: host}, &er); code != 400 || er.Error.Code != "invalid" {
			t.Errorf("%q: %d %+v", host, code, er)
		}
		sysWantLog(t, e.log(from), "v-list-sys-info json")
	}
	from := e.mark()
	var ref JobRef
	if code := e.do("POST", "/reboot", RebootRequest{Hostname: "server1.acme-hosting.net"}, &ref); code != 202 {
		t.Fatalf("status %d", code)
	}
	st := e.wait(ref)
	if st.State != "done" || st.Title != "Restart server" {
		t.Fatalf("job = %+v", st)
	}
	sysWantLog(t, e.log(from), "v-list-sys-info json", "v-restart-system yes")
}

func TestSysServicesBusy(t *testing.T) {
	release := make(chan struct{})
	e := sysSvcNew(t, overrides{}, func(r runner.Runner) runner.Runner {
		return sysBlockRunner{Runner: r, cmd: "v-restart-service", release: release}
	})
	var ref JobRef
	if code := e.do("POST", "/services/nginx/restart", nil, &ref); code != 202 {
		t.Fatalf("first: %d", code)
	}
	var er APIError
	if code := e.do("POST", "/reboot", RebootRequest{Hostname: "server1.acme-hosting.net"}, &er); code != 409 || er.Error.Code != "busy" {
		t.Errorf("second: %d %+v", code, er)
	}
	close(release)
	e.wait(ref)
}

type sysBlockRunner struct {
	runner.Runner
	cmd     string
	release chan struct{}
}

func (b sysBlockRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == b.cmd {
		<-b.release
	}
	return b.Runner.Run(ctx, cmd, args...)
}

var _ = fmt.Sprintf
