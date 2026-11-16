package httpapi

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func (h *harness) cronCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.Contains(c.Cmd, "cron") {
			out = append(out, strings.TrimSpace(c.Cmd+" "+strings.Join(c.Args, " ")))
		}
	}
	return out
}

func goodCron() CronRequest {
	return CronRequest{Min: "*/5", Hour: "*", Day: "1-31/2", Month: "*", WDay: "0,6", Cmd: "php /home/admin/x.php >/dev/null 2>&1"}
}

func TestCronList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r CronJobs
	if code := h.call("GET", "/api/v1/cron", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(r.Jobs) != 2 || r.Jobs[0].JOB != "1" || r.Jobs[1].JOB != "2" || r.Jobs[1].SUSPENDED != "yes" ||
		r.Jobs[0].CMD != "php /home/admin/web/example.com/public_html/cron.php >/dev/null 2>&1" {
		t.Fatalf("jobs = %+v", r.Jobs)
	}
	if !r.Reports || r.Usage.Jobs != "2" || r.Usage.Limit != "unlimited" {
		t.Fatalf("reply = %+v", r)
	}
	// MAILTO is the viewed account's contact address.
	if r.Contact != "admin@acme-hosting.net" {
		t.Fatalf("contact = %q", r.Contact)
	}
	want := []string{"v-list-cron-jobs admin json"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronListInvalidJSONFallsBack(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var r CronJobs
	if code := h.call("GET", "/api/v1/cron", nil, &r); code != 200 || len(r.Jobs) != 1 {
		t.Fatalf("%d %+v", code, r)
	}
	if j := r.Jobs[0]; j.CMD != `find /home/bob/tmp -name '*.tmp' -mtime +7 -exec rm {} \;` || j.MIN != "0" || j.HOUR != "*/6" || j.JOB != "1" {
		t.Fatalf("job = %+v", j)
	}
	if r.Reports {
		t.Fatal("bob has reports off")
	}
	want := []string{"v-list-cron-jobs bob json", "v-list-cron-jobs bob plain"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronOneFallsBack(t *testing.T) {
	// The real v-list-cron-job plain output has no JOB column, and shows " as \".
	cmd := `mysqldump x > \"/b/db-$(date +\%F).sql\"; find . -exec rm {} \;`
	o := overrides{Fixtures: map[string][]fixtureEntry{"v-list-cron-job": {
		{Args: []string{"bob", "1", "json"}, Stdout: `{"1": {"CMD": "a\;"}}`},
		{Args: []string{"bob", "1", "plain"}, Stdout: "0\t*/6\t*\t*\t*\t" + cmd + "\tno\t12:00:00\t2026-01-10\n"},
	}}}
	h := newHarnessWith(t, o)
	h.loginBob()
	var j struct{ CMD, JOB, MIN, HOUR, SUSPENDED string }
	if code := h.call("GET", "/api/v1/cron/1", nil, &j); code != 200 || j.CMD != `mysqldump x > "/b/db-$(date +\%F).sql"; find . -exec rm {} \;` || j.JOB != "1" {
		t.Fatalf("%d %+v", code, j)
	}
	want := []string{"v-list-cron-job bob 1 json", "v-list-cron-job bob 1 plain"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronOneNotFoundAndBadParam(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/cron/99", nil, &e); code != 404 || e.Error.Code != "not_found" {
		t.Fatalf("%d %+v", code, e)
	}
	n := len(h.cronCalls())
	for _, p := range []string{"-h", "x", "01"} {
		if code := h.call("GET", "/api/v1/cron/"+p, nil, &e); code != 400 {
			t.Errorf("%s: %d", p, code)
		}
	}
	if len(h.cronCalls()) != n {
		t.Fatalf("commands ran: %q", h.cronCalls())
	}
}

func TestCronOptions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var o CronOptions
	if code := h.call("GET", "/api/v1/cron/options", nil, &o); code != 200 || o.Timezone != "UTC" {
		t.Fatalf("%d %+v", code, o)
	}
	if _, err := time.Parse(time.RFC3339, o.Now); err != nil {
		t.Fatalf("now = %q", o.Now)
	}
}

func TestCronAddCommands(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/cron", goodCron(), nil); code != 200 {
		t.Fatalf("code %d", code)
	}
	want := []string{"v-add-cron-job admin */5 * 1-31/2 * 0,6 php /home/admin/x.php >/dev/null 2>&1"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronCommandReachesCoreIntact(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := goodCron()
	r.Cmd = `echo "a b" ; rm -f '/x' $(id) \ | cat`
	if code := h.call("POST", "/api/v1/cron", r, nil); code != 200 {
		t.Fatalf("code %d", code)
	}
	calls := h.fixture.Calls()
	last := calls[len(calls)-1]
	if last.Cmd != "v-add-cron-job" || len(last.Args) != 7 || last.Args[6] != r.Cmd {
		t.Fatalf("call = %+v", last)
	}
}

func TestCronEditCommands(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("PUT", "/api/v1/cron/2", goodCron(), nil); code != 200 {
		t.Fatalf("code %d", code)
	}
	want := []string{"v-change-cron-job admin 2 */5 * 1-31/2 * 0,6 php /home/admin/x.php >/dev/null 2>&1"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronHostileFieldsRunNothing(t *testing.T) {
	bad := map[string]string{
		"newline": "1\n* * * * * x", "name": "foo", "99": "99", "mon": "mon", "5-1": "5-1", "step0": "*/0",
		"long": strings.Repeat("1,", 32) + "1", "space": "1 2",
	}
	for field := 0; field < 5; field++ {
		for name, v := range bad {
			h := newHarness(t)
			h.loginAdmin()
			r := goodCron()
			p := []*string{&r.Min, &r.Hour, &r.Day, &r.Month, &r.WDay}[field]
			*p = v
			for _, m := range []struct{ method, path string }{{"POST", "/api/v1/cron"}, {"PUT", "/api/v1/cron/1"}} {
				var e APIError
				code := h.call(m.method, m.path, r, &e)
				kinds := []string{"min", "hour", "day", "month", "wday"}
				if code != 400 || e.Error.Code != "invalid" || e.Error.Message != "invalid %s format :: %s" ||
					!reflect.DeepEqual(e.Error.Args, []string{kinds[field], v}) {
					t.Errorf("%s field %d %s: %d %+v", m.method, field, name, code, e)
				}
			}
			if got := h.cronCalls(); len(got) != 0 {
				t.Errorf("field %d %s: commands ran: %q", field, name, got)
			}
		}
	}
}

func TestCronDayAndMonthZeroRefused(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := goodCron()
	r.Day = "0"
	if code := h.call("POST", "/api/v1/cron", r, nil); code != 400 {
		t.Fatalf("day 0: %d", code)
	}
	r = goodCron()
	r.Month = "0"
	if code := h.call("POST", "/api/v1/cron", r, nil); code != 400 {
		t.Fatalf("month 0: %d", code)
	}
	r = goodCron()
	r.WDay = "7"
	if code := h.call("POST", "/api/v1/cron", r, nil); code != 200 {
		t.Fatalf("wday 7: %d", code)
	}
}

func TestCronHostileCommandsRunNothing(t *testing.T) {
	for _, c := range []string{"a`id`", "a\rb", "a\x00b", "a\nb", "-e", "-n", "-E", "-ne"} {
		h := newHarness(t)
		h.loginAdmin()
		r := goodCron()
		r.Cmd = c
		var e APIError
		if code := h.call("POST", "/api/v1/cron", r, &e); code != 400 || e.Error.Message != "Invalid cron command format" {
			t.Errorf("%q: %d %+v", c, code, e)
		}
		if code := h.call("PUT", "/api/v1/cron/1", r, nil); code != 400 {
			t.Errorf("%q edit: %d", c, code)
		}
		if got := h.cronCalls(); len(got) != 0 {
			t.Errorf("%q: commands ran: %q", c, got)
		}
	}
}

func TestCronBlankFields(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		h := newHarness(t)
		h.loginAdmin()
		path := "/api/v1/cron"
		if method == "PUT" {
			path += "/1"
		}
		var e APIError
		code := h.call(method, path, CronRequest{Hour: "*", Month: "*", WDay: "*"}, &e)
		if code != 400 || e.Error.Message != blankFieldMsg || !reflect.DeepEqual(e.Error.Args, []string{"Minute", "Day", "Command"}) {
			t.Errorf("%s: %d %+v", method, code, e)
		}
		if got := h.cronCalls(); len(got) != 0 {
			t.Errorf("%s: commands ran: %q", method, got)
		}
	}
}

func TestCronBadJSON(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/cron", "nope", nil); code != 400 {
		t.Fatalf("code %d", code)
	}
}

func TestCronSingleActions(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, a := range []struct{ method, path string }{
		{"DELETE", "/api/v1/cron/1"}, {"POST", "/api/v1/cron/1/suspend"}, {"POST", "/api/v1/cron/2/unsuspend"},
	} {
		if code := h.call(a.method, a.path, nil, nil); code != 200 {
			t.Fatalf("%s %s: %d", a.method, a.path, code)
		}
	}
	want := []string{"v-delete-cron-job admin 1", "v-suspend-cron-job admin 1", "v-unsuspend-cron-job admin 2"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronSuspendAdminOnlyDeleteOpen(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, p := range []string{"/api/v1/cron/1/suspend", "/api/v1/cron/1/unsuspend"} {
		var e APIError
		if code := h.call("POST", p, nil, &e); code != 403 || e.Error.Code != "forbidden" {
			t.Errorf("%s: %d %+v", p, code, e)
		}
	}
	if len(h.cronCalls()) != 0 {
		t.Fatalf("calls = %q", h.cronCalls())
	}
	if code := h.call("DELETE", "/api/v1/cron/1", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if got := h.cronCalls(); !reflect.DeepEqual(got, []string{"v-delete-cron-job bob 1"}) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronBulkAdmin(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/cron/bulk", BulkRequest{Action: "suspend", Names: []string{"1", "2"}}, &r); code != 200 ||
		r.Status != "ok" || r.Failed == nil || len(r.Failed) != 0 {
		t.Fatalf("%d %+v", code, r)
	}
	want := []string{"v-suspend-cron-job admin 1 no", "v-suspend-cron-job admin 2 no", "v-restart-cron"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronBulkUsers(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, a := range []string{"suspend", "unsuspend"} {
		var e APIError
		if code := h.call("POST", "/api/v1/cron/bulk", BulkRequest{Action: a, Names: []string{"1"}}, &e); code != 403 {
			t.Errorf("%s: %d", a, code)
		}
	}
	if len(h.cronCalls()) != 0 {
		t.Fatalf("calls = %q", h.cronCalls())
	}
	if code := h.call("POST", "/api/v1/cron/bulk", BulkRequest{Action: "delete", Names: []string{"1"}}, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	want := []string{"v-delete-cron-job bob 1 no", "v-restart-cron"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronBulkRejectsBadNames(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/cron/bulk", BulkRequest{Action: "delete", Names: []string{"1", "-h"}}, nil); code != 400 {
		t.Fatalf("code %d", code)
	}
	if code := h.call("POST", "/api/v1/cron/bulk", BulkRequest{Action: "explode", Names: []string{"1"}}, nil); code != 400 {
		t.Fatalf("code %d", code)
	}
	if len(h.cronCalls()) != 0 {
		t.Fatalf("calls = %q", h.cronCalls())
	}
}

func TestCronBulkListsFailures(t *testing.T) {
	o := overrides{Fixtures: map[string][]fixtureEntry{"v-delete-cron-job": {
		{Args: []string{"*", "99", "*"}, Stdout: "Error: cron job 99 doesn't exist\n", Exit: 3},
		{Args: []string{"*", "*", "*"}},
	}}}
	h := newHarnessWith(t, o)
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/cron/bulk", BulkRequest{Action: "delete", Names: []string{"99", "1"}}, &r); code != 200 ||
		len(r.Failed) != 1 || r.Failed[0].Name != "99" || r.Failed[0].Message != "Error: cron job 99 doesn't exist" {
		t.Fatalf("%d %+v", code, r)
	}
	want := []string{"v-delete-cron-job admin 99 no", "v-delete-cron-job admin 1 no", "v-restart-cron"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronReports(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	if code := h.call("POST", "/api/v1/cron/reports", CronReportsRequest{Enabled: true}, nil); code != 200 {
		t.Fatalf("on: %d", code)
	}
	if code := h.call("POST", "/api/v1/cron/reports", CronReportsRequest{Enabled: false}, nil); code != 200 {
		t.Fatalf("off: %d", code)
	}
	want := []string{"v-add-cron-reports bob", "v-delete-cron-reports bob"}
	if got := h.cronCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronReportsFailureReported(t *testing.T) {
	o := overrides{Fixtures: map[string][]fixtureEntry{"v-add-cron-reports": {
		{Args: []string{"*"}, Stdout: "Error: user admin is suspended\n", Exit: 5},
	}}}
	h := newHarnessWith(t, o)
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/cron/reports", CronReportsRequest{Enabled: true}, &e); code != 409 || e.Error.Code != "suspended" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestCronWritesReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	routes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/cron", goodCron()},
		{"PUT", "/api/v1/cron/1", goodCron()},
		{"DELETE", "/api/v1/cron/1", nil},
		{"POST", "/api/v1/cron/1/suspend", nil},
		{"POST", "/api/v1/cron/1/unsuspend", nil},
		{"POST", "/api/v1/cron/bulk", BulkRequest{Action: "delete", Names: []string{"1"}}},
		{"POST", "/api/v1/cron/reports", CronReportsRequest{Enabled: true}},
		{"POST", "/api/v1/cron/reports", CronReportsRequest{Enabled: false}},
	}
	for _, rt := range routes {
		var e APIError
		if code := h.call(rt.method, rt.path, rt.body, &e); code != 403 || e.Error.Code != "read_only" {
			t.Errorf("%s %s: %d %+v", rt.method, rt.path, code, e)
		}
	}
	if got := h.cronCalls(); len(got) != 0 {
		t.Fatalf("calls = %q", got)
	}
}

func TestCronRequiresLogin(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	for _, p := range []string{"/api/v1/cron", "/api/v1/cron/options", "/api/v1/cron/1"} {
		if code := h.call("GET", p, nil, nil); code != 401 {
			t.Errorf("%s: %d", p, code)
		}
	}
}
