package httpapi

// Slice D: IP address routes (FW §1.1–1.3, §3.1–3.4, decision 45).

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/hostisan/hostisan/internal/session"
)

// sysNetCalls is every command run since index from, as argv lists, without the
// session's own reads (see sysCommands). Empty arguments stay visible.
func (h *harness) sysNetCalls(from int) [][]string {
	var out [][]string
	for _, c := range h.fixture.Calls()[from:] {
		switch c.Cmd {
		case "v-list-sys-config", "v-list-user", "v-log-user-logout":
			continue
		}
		out = append(out, append([]string{c.Cmd}, c.Args...))
	}
	return out
}

func sysNetWantCalls(t *testing.T, got [][]string, want ...[]string) {
	t.Helper()
	if want == nil {
		want = [][]string{}
	}
	if got == nil {
		got = [][]string{}
	}
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("commands:\n got %q\nwant %q", got, want)
	}
}

// sysNetHarness signs in admin on the dev fixtures with o applied and serves the
// server routes.
func sysNetHarness(t *testing.T, o overrides) (*harness, string) {
	t.Helper()
	h := newHarnessWith(t, o)
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, nil)
	return h, srv.URL
}

// sysNetJobResult waits for the job and decodes its result into out.
func (h *harness) sysNetJobResult(base, id string, out any) JobStatus {
	h.t.Helper()
	st := h.sysWaitJob(base, id)
	if out != nil {
		b, _ := json.Marshal(st.Result)
		if err := json.Unmarshal(b, out); err != nil {
			h.t.Fatalf("job result %s: %v", b, err)
		}
	}
	return st
}

func TestSysIPList(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var v IPsView
	if code := h.sysDo(base, "GET", "/api/v1/server/ips", nil, &v); code != 200 {
		t.Fatalf("list: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-sys-ips", "json"})
	var ips []string
	for _, r := range v.IPs {
		ips = append(ips, r.IP)
	}
	want := []string{"10.0.0.5", "203.0.113.10", "203.0.113.11"}
	switch v.SortOrder {
	case "name":
	case "date":
		slices.Reverse(want)
	default:
		t.Fatalf("sortOrder = %q", v.SortOrder)
	}
	if !slices.Equal(ips, want) {
		t.Fatalf("order %q (%s), want %q", ips, v.SortOrder, want)
	}
	i := slices.IndexFunc(v.IPs, func(r IPRow) bool { return r.IP == "10.0.0.5" })
	nat := v.IPs[i]
	if nat.NAT != "198.51.100.7" || nat.Display != "198.51.100.7" || nat.Status != "dedicated" || nat.Owner != "bob" ||
		nat.Interface != "eth1" || nat.Netmask != "255.255.255.0" || nat.Users != "bob" || nat.WebDomains != "1" ||
		nat.Date != "2026-01-10" || nat.Time != "12:00:00" {
		t.Errorf("10.0.0.5 = %+v", nat)
	}
	i = slices.IndexFunc(v.IPs, func(r IPRow) bool { return r.IP == "203.0.113.10" })
	if r := v.IPs[i]; r.Display != "203.0.113.10" || r.Status != "shared" || r.Name != "main" {
		t.Errorf("203.0.113.10 = %+v", r)
	}
}

func TestSysIPListSortOrder(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	for _, order := range []string{"name", "date"} {
		h.sysSetSortOrder(order)
		var v IPsView
		h.sysDo(base, "GET", "/api/v1/server/ips", nil, &v)
		want := []string{"10.0.0.5", "203.0.113.10", "203.0.113.11"}
		if order == "date" {
			slices.Reverse(want)
		}
		var got []string
		for _, r := range v.IPs {
			got = append(got, r.IP)
		}
		if v.SortOrder != order || !slices.Equal(got, want) {
			t.Errorf("%s: %s %q", order, v.SortOrder, got)
		}
	}
}

// sysSetSortOrder sets the signed-in session's sort order (PREF_UI_SORT).
func (h *harness) sysSetSortOrder(order string) {
	h.t.Helper()
	if !h.store.Update(h.sessionID(), func(s *session.Session) { s.SortOrder = order }) {
		h.t.Fatal("no session")
	}
}

func TestSysIPOptions(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var o IPOptions
	if code := h.sysDo(base, "GET", "/api/v1/server/ips/options", nil, &o); code != 200 {
		t.Fatalf("options: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-sys-interfaces", "json"}, []string{"v-list-sys-users", "json"})
	if !slices.Equal(o.Interfaces, []string{"eth0", "eth1"}) || !slices.Equal(o.Users, []string{"admin", "bob", "susp", "ops"}) || o.RootUser != "admin" {
		t.Fatalf("options = %+v", o)
	}
}

func TestSysIPGet(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var r IPRow
	if code := h.sysDo(base, "GET", "/api/v1/server/ips/10.0.0.5", nil, &r); code != 200 {
		t.Fatalf("get: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-sys-ip", "10.0.0.5", "json"})
	if r.IP != "10.0.0.5" || r.NAT != "198.51.100.7" || r.Display != "198.51.100.7" || r.Owner != "bob" {
		t.Fatalf("row = %+v", r)
	}
	var e APIError
	if code := h.sysDo(base, "GET", "/api/v1/server/ips/203.0.113.99", nil, &e); code != 404 || e.Error.Code != "not_found" {
		t.Fatalf("unknown ip: %d %+v", code, e)
	}
}

func TestSysIPHostilePath(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	for _, ip := range []string{"-h", "1.2.3", "1.2.3.4%3Bx", "1.2.3.4%2F24", "::1", "01.2.3.4", "1.2.3.256", "a%20b"} {
		for _, method := range []string{"GET", "PUT", "DELETE"} {
			before := len(h.fixture.Calls())
			var e APIError
			body := any(nil)
			if method == "PUT" {
				body = IPRequest{Shared: true}
			}
			code := h.sysDo(base, method, "/api/v1/server/ips/"+ip, body, &e)
			if code != 400 || e.Error.Code != "invalid" {
				t.Errorf("%s %s: %d %+v", method, ip, code, e)
			}
			sysNetWantCalls(t, h.sysNetCalls(before))
		}
	}
}

func TestSysIPAdd(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})

	// Shared: ROOT_USER is the owner, whatever owner the form posted (FW gotcha 7).
	before := len(h.fixture.Calls())
	var ref JobRef
	req := IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "eth0", Shared: true, Owner: "bob"}
	if code := h.sysDo(base, "POST", "/api/v1/server/ips", req, &ref); code != 202 {
		t.Fatalf("add shared: %d", code)
	}
	var ok OK
	if st := h.sysNetJobResult(base, ref.JobID, &ok); st.State != "done" || ok.Status != "ok" ||
		!slices.Equal(st.Steps, []string{"Add IP address"}) {
		t.Fatalf("job = %+v", st)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-add-sys-ip", "203.0.113.20", "255.255.255.0", "eth0", "admin", "shared", "", ""})

	// Dedicated: the chosen owner, with name and NAT.
	before = len(h.fixture.Calls())
	req = IPRequest{IP: "203.0.113.21", Netmask: "255.255.255.0", Interface: "eth1", Owner: "bob", Name: "bob.example.com", NAT: "198.51.100.21"}
	if code := h.sysDo(base, "POST", "/api/v1/server/ips", req, &ref); code != 202 {
		t.Fatalf("add dedicated: %d", code)
	}
	h.sysWaitJob(base, ref.JobID)
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-add-sys-ip", "203.0.113.21", "255.255.255.0", "eth1", "bob", "dedicated", "bob.example.com", "198.51.100.21"})
}

func TestSysIPAddInvalid(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	cases := []struct {
		name string
		req  IPRequest
		msg  string
		args []string
	}{
		{"blank", IPRequest{}, blankFieldMsg, []string{"IP Address", "Netmask", "Interface", "Assigned User"}},
		{"blank owner, dedicated", IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "eth0"}, blankFieldMsg, []string{"Assigned User"}},
		{"bad ip", IPRequest{IP: "203.0.113", Netmask: "255.255.255.0", Interface: "eth0", Shared: true}, sysNetInvalidIPMsg, []string{"203.0.113"}},
		{"bad netmask", IPRequest{IP: "203.0.113.20", Netmask: "255.0.255.0", Interface: "eth0", Shared: true}, sysNetInvalidMsg, []string{"Netmask"}},
		{"bad interface", IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "-eth0", Shared: true}, sysNetInvalidMsg, []string{"Interface"}},
		{"bad owner", IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "eth0", Owner: "a b"}, sysNetInvalidMsg, []string{"Assigned User"}},
		{"bad name", IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "eth0", Shared: true, Name: "a b"}, sysNetInvalidMsg, []string{"Assigned Domain"}},
		{"bad nat", IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "eth0", Shared: true, NAT: "x"}, sysNetInvalidIPMsg, []string{"x"}},
	}
	for _, c := range cases {
		before := len(h.fixture.Calls())
		var e APIError
		code := h.sysDo(base, "POST", "/api/v1/server/ips", c.req, &e)
		if code != 400 || e.Error.Code != "invalid" || e.Error.Message != c.msg || !slices.Equal(e.Error.Args, c.args) {
			t.Errorf("%s: %d %+v", c.name, code, e)
		}
		sysNetWantCalls(t, h.sysNetCalls(before))
	}
}

func TestSysIPEdit(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})

	// 203.0.113.11 is dedicated to admin, named "secondary", without NAT. Making it
	// shared skips the owner step (the core moves it to ROOT_USER); the name is
	// unchanged; the NAT is new.
	before := len(h.fixture.Calls())
	var ok OK
	req := IPRequest{Shared: true, Owner: "bob", Name: "secondary", NAT: "198.51.100.30"}
	if code := h.sysDo(base, "PUT", "/api/v1/server/ips/203.0.113.11", req, &ok); code != 200 || ok.Status != "ok" {
		t.Fatalf("edit: %d %+v", code, ok)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-sys-ip", "203.0.113.11", "json"},
		[]string{"v-change-sys-ip-status", "203.0.113.11", "shared"},
		[]string{"v-change-sys-ip-nat", "203.0.113.11", "198.51.100.30"})

	// 203.0.113.10 is shared: dedicating it to bob runs status, then owner, then name,
	// then clears nothing (NAT stays empty).
	before = len(h.fixture.Calls())
	req = IPRequest{Owner: "bob", Name: "bob.example.com"}
	if code := h.sysDo(base, "PUT", "/api/v1/server/ips/203.0.113.10", req, &ok); code != 200 {
		t.Fatalf("edit 2: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-sys-ip", "203.0.113.10", "json"},
		[]string{"v-change-sys-ip-status", "203.0.113.10", "dedicated"},
		[]string{"v-change-sys-ip-owner", "203.0.113.10", "bob"},
		[]string{"v-change-sys-ip-name", "203.0.113.10", "bob.example.com"})

	// Clearing the NAT of 10.0.0.5 sends an empty NAT.
	before = len(h.fixture.Calls())
	req = IPRequest{Owner: "bob"}
	if code := h.sysDo(base, "PUT", "/api/v1/server/ips/10.0.0.5", req, &ok); code != 200 {
		t.Fatalf("edit 3: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-sys-ip", "10.0.0.5", "json"},
		[]string{"v-change-sys-ip-nat", "10.0.0.5", ""})

	// Nothing changed: only the read.
	before = len(h.fixture.Calls())
	req = IPRequest{Owner: "bob", NAT: "198.51.100.7"}
	if code := h.sysDo(base, "PUT", "/api/v1/server/ips/10.0.0.5", req, &ok); code != 200 {
		t.Fatalf("edit 4: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-sys-ip", "10.0.0.5", "json"})
}

func TestSysIPEditStopsAtFirstError(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-ip-owner": {{Args: []string{"*", "*"}, Stdout: "Error: IP is in use / WEB_DOMAINS = 1\n", Exit: 4}},
	}})
	before := len(h.fixture.Calls())
	var e APIError
	req := IPRequest{Owner: "ops", Name: "ops.example.com", NAT: ""}
	if code := h.sysDo(base, "PUT", "/api/v1/server/ips/10.0.0.5", req, &e); code != 409 ||
		e.Error.Code != "exists" || e.Error.Message != "Error: IP is in use / WEB_DOMAINS = 1" {
		t.Fatalf("edit: %d %+v", code, e)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-sys-ip", "10.0.0.5", "json"},
		[]string{"v-change-sys-ip-owner", "10.0.0.5", "ops"})
}

func TestSysIPDelete(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var ref JobRef
	if code := h.sysDo(base, "DELETE", "/api/v1/server/ips/203.0.113.11", nil, &ref); code != 202 {
		t.Fatalf("delete: %d", code)
	}
	if st := h.sysWaitJob(base, ref.JobID); st.State != "done" || st.Title != "Delete IP address" {
		t.Fatalf("job = %+v", st)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-delete-sys-ip", "203.0.113.11"})
}

func TestSysIPBulk(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-sys-ip": {
			{Args: []string{"10.0.0.5"}, Stdout: "Error: IP is in use / WEB_DOMAINS = 1\n", Exit: 4},
			{Args: []string{"*"}, Exit: 0},
		},
	}})
	before := len(h.fixture.Calls())
	var ref JobRef
	body := BulkRequest{Action: "delete", Names: []string{"10.0.0.5", "203.0.113.11"}}
	if code := h.sysDo(base, "POST", "/api/v1/server/ips/bulk", body, &ref); code != 202 {
		t.Fatalf("bulk: %d", code)
	}
	var rep BulkReply
	if st := h.sysNetJobResult(base, ref.JobID, &rep); st.State != "done" {
		t.Fatalf("job = %+v", st)
	}
	if rep.Status != "ok" || len(rep.Failed) != 1 || rep.Failed[0].Name != "10.0.0.5" ||
		rep.Failed[0].Message != "Error: IP is in use / WEB_DOMAINS = 1" {
		t.Fatalf("reply = %+v", rep)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-delete-sys-ip", "10.0.0.5"}, []string{"v-delete-sys-ip", "203.0.113.11"})

	for name, b := range map[string]BulkRequest{
		"unknown action": {Action: "suspend", Names: []string{"203.0.113.11"}},
		"no names":       {Action: "delete"},
		"hostile name":   {Action: "delete", Names: []string{"203.0.113.11", "-h"}},
		"too many":       {Action: "delete", Names: sysNetRepeat("203.0.113.11", 501)},
	} {
		before := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "POST", "/api/v1/server/ips/bulk", b, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", name, code, e)
		}
		sysNetWantCalls(t, h.sysNetCalls(before))
	}
}

func TestSysIPRefresh(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var ref JobRef
	// No body and no names: refresh is global (FW gotcha 11).
	if code := h.sysDo(base, "POST", "/api/v1/server/ips/refresh", nil, &ref); code != 202 {
		t.Fatalf("refresh: %d", code)
	}
	if st := h.sysWaitJob(base, ref.JobID); st.State != "done" || !slices.Equal(st.Steps, []string{"Refresh IP addresses"}) {
		t.Fatalf("job = %+v", st)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-update-sys-ip"})
}

// sysNetRepeat is n copies of v.
func sysNetRepeat[T any](v T, n int) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = v
	}
	return out
}
