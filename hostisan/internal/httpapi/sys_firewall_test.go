package httpapi

// Slice D: firewall rules, toggle, bans and IP sets (FW §1.4–1.11, decisions 37–44).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/session"
)

var sysNetFixtures = filepath.Join("..", "..", "testdata", "fixtures", "sys_network")

// sysNetProxied serves the server routes as if over the proxy's unix socket, so
// X-Real-IP names the caller (session.FromProxy). The session's IP binding is off,
// because the sign-in came over TCP.
func sysNetProxied(t *testing.T, o overrides) (*harness, string) {
	t.Helper()
	cfg := map[string]string{"DISABLE_IP_CHECK": "yes"}
	for k, v := range o.Config {
		cfg[k] = v
	}
	o.Config = cfg
	h := newHarnessWith(t, o)
	h.loginAdmin()
	sys := &sysAPI{server: &server{Deps: h.deps, jobs: jobs.NewRegistry(0, nil)}}
	mux := http.NewServeMux()
	sys.routes(&sysMux{mux: mux})
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context { return session.FromProxy(ctx) }
	srv.Start()
	t.Cleanup(srv.Close)
	return h, srv.URL
}

// sysNetDoFrom is sysDo with X-Real-IP set to realIP.
func (h *harness) sysNetDoFrom(base, realIP, method, path string, body, out any) int {
	h.t.Helper()
	return h.sysNetDoHeaders(base, map[string]string{"X-Real-IP": realIP}, method, path, body, out)
}

// sysNetDoHeaders is sysDo with extra request headers.
func (h *harness) sysNetDoHeaders(base string, headers map[string]string, method, path string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", h.token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

// sysFwRulesOut renders rules as v-list-firewall json prints them.
func sysFwRulesOut(rules ...hestia.FirewallRule) string {
	var parts []string
	for _, r := range rules {
		parts = append(parts, fmt.Sprintf("    %q: {\n        \"ACTION\": %q,\n        \"PROTOCOL\": %q,\n        \"PORT\": %q,\n"+
			"        \"IP\": %q,\n        \"COMMENT\": %q,\n        \"SUSPENDED\": %q,\n        \"TIME\": \"12:00:00\",\n        \"DATE\": \"2026-01-10\"\n    }",
			r.RULE, r.ACTION, r.PROTOCOL, r.PORT, r.IP, r.COMMENT, r.SUSPENDED))
	}
	return "{\n" + strings.Join(parts, ",\n") + "\n}\n"
}

// sysFwDevRule is a rule of the dev fixture, for its fingerprint. It reads the file,
// not the runner, so it adds nothing to the command log.
func sysFwDevRule(t *testing.T, _ *harness, id string) hestia.FirewallRule {
	t.Helper()
	var entries []fixtureEntry
	readEntries(t, filepath.Join("..", "..", "testdata", "fixtures", "dev", "v-list-firewall-rule.json"), &entries)
	for _, e := range entries {
		if len(e.Args) == 2 && e.Args[0] == id {
			rules, err := hestia.DecodeObjects[hestia.FirewallRule]([]byte(e.Stdout))
			if err != nil || len(rules) != 1 {
				t.Fatalf("rule %s: %v", id, err)
			}
			return rules[0]
		}
	}
	t.Fatalf("no dev rule %s", id)
	return hestia.FirewallRule{}
}

func sysFwWantError(t *testing.T, name string, code int, e APIError, wantCode int, errCode, msg string, args ...string) {
	t.Helper()
	if code != wantCode || e.Error.Code != errCode || e.Error.Message != msg || !slices.Equal(e.Error.Args, args) {
		t.Errorf("%s: %d %+v, want %d %s %q %q", name, code, e, wantCode, errCode, msg, args)
	}
}

func TestSysFwList(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var v FirewallView
	if code := h.sysDo(base, "GET", "/api/v1/server/firewall", nil, &v); code != 200 {
		t.Fatalf("list: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall", "json"})
	if !v.Enabled || !v.Bans || len(v.Rules) != 10 {
		t.Fatalf("view = %+v", v)
	}
	var ids []string
	for _, r := range v.Rules {
		ids = append(ids, r.ID)
	}
	// Evaluation order: highest id first (decision 39).
	if !slices.Equal(ids, []string{"10", "9", "8", "7", "6", "5", "4", "3", "2", "1"}) {
		t.Fatalf("order = %q", ids)
	}
	for _, r := range v.Rules {
		want := map[string]string{"2": "panel", "10": "ssh"}[r.ID]
		if r.Protected != want {
			t.Errorf("rule %s protected = %q, want %q", r.ID, r.Protected, want)
		}
		if r.Expect != sysFwExpect(sysFwDevRule(t, h, r.ID)) {
			t.Errorf("rule %s expect = %q", r.ID, r.Expect)
		}
	}
	if r := v.Rules[8]; r.ID != "2" || r.Action != "ACCEPT" || r.Protocol != "TCP" || r.Port != "8083" ||
		r.IP != "0.0.0.0/0" || r.Comment != "HESTIA" || r.Suspended || r.Date != "2026-01-10" {
		t.Errorf("rule 2 = %+v", r)
	}
}

func TestSysFwDisabledEmptyState(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Config: map[string]string{"FIREWALL_SYSTEM": "", "FIREWALL_EXTENSION": ""}})
	before := len(h.fixture.Calls())
	var raw json.RawMessage
	if code := h.sysDo(base, "GET", "/api/v1/server/firewall", nil, &raw); code != 200 ||
		string(bytes.TrimSpace(raw)) != `{"enabled":false,"bans":false,"rules":[]}` {
		t.Fatalf("rules: %d %s", code, raw)
	}
	if code := h.sysDo(base, "GET", "/api/v1/server/firewall/ipsets", nil, &raw); code != 200 ||
		string(bytes.TrimSpace(raw)) != `{"enabled":false,"rows":[]}` {
		t.Fatalf("ipsets: %d %s", code, raw)
	}
	sysNetWantCalls(t, h.sysNetCalls(before))
}

func TestSysFwGetRule(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var r FirewallRuleRow
	if code := h.sysDo(base, "GET", "/api/v1/server/firewall/rules/10", nil, &r); code != 200 ||
		r.ID != "10" || r.Protected != "ssh" || r.Port != "22" || r.Expect != sysFwExpect(sysFwDevRule(t, h, "10")) {
		t.Fatalf("get: %d %+v", code, r)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "10", "json"})
	for _, id := range []string{"0", "02", "abc", "1234567890", "-1", "1%3B2"} {
		before := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "GET", "/api/v1/server/firewall/rules/"+id, nil, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("id %q: %d %+v", id, code, e)
		}
		if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/"+id+"/delete", RuleActionRequest{Expect: "x"}, &e); code != 400 {
			t.Errorf("action on id %q: %d", id, code)
		}
		sysNetWantCalls(t, h.sysNetCalls(before))
	}
	var e APIError
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/4/explode", RuleActionRequest{Expect: "x"}, &e); code != 400 {
		t.Errorf("unknown action: %d", code)
	}
}

func TestSysFwProtectedRulesLockout(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	for _, id := range []string{"2", "10"} {
		expect := sysFwExpect(sysFwDevRule(t, h, id))
		for _, action := range []string{"delete", "suspend", "move-up", "move-down"} {
			before := len(h.fixture.Calls())
			var e APIError
			code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/"+id+"/"+action, RuleActionRequest{Expect: expect}, &e)
			sysFwWantError(t, id+" "+action, code, e, 409, SysLockoutCode, sysFwLockoutProtectedMsg)
			sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", id, "json"})
		}
	}
	// Bulk: one protected rule refuses the whole batch before any write.
	for _, action := range []string{"delete", "suspend"} {
		before := len(h.fixture.Calls())
		var e APIError
		body := RuleBulkRequest{Action: action, Items: []RuleBulkItem{
			{ID: "3", Expect: sysFwExpect(sysFwDevRule(t, h, "3"))},
			{ID: "2", Expect: sysFwExpect(sysFwDevRule(t, h, "2"))},
		}}
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/bulk", body, &e)
		sysFwWantError(t, "bulk "+action, code, e, 409, SysLockoutCode, sysFwLockoutProtectedMsg)
		sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall", "json"})
	}
	// Editing the SSH rule's port.
	before := len(h.fixture.Calls())
	var e APIError
	body := FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "2200", IP: "0.0.0.0/0", Comment: "SSH",
		Expect: sysFwExpect(sysFwDevRule(t, h, "10"))}
	code := h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/10", body, &e)
	sysFwWantError(t, "edit ssh port", code, e, 409, SysLockoutCode, sysFwLockoutProtectedMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "10", "json"})
	// The panel rule's port as well.
	before = len(h.fixture.Calls())
	body = FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "8443", IP: "0.0.0.0/0", Comment: "HESTIA",
		Expect: sysFwExpect(sysFwDevRule(t, h, "2"))}
	e = APIError{}
	code = h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/2", body, &e)
	sysFwWantError(t, "edit panel port", code, e, 409, SysLockoutCode, sysFwLockoutProtectedMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "2", "json"})
}

func TestSysFwDropLockouts(t *testing.T) {
	const caller = "192.0.2.50"
	h, base := sysNetProxied(t, overrides{})
	sshd := []string{"v-list-sys-sshd-port", "plain"}
	cases := []struct {
		name string
		req  FirewallRuleRequest
		msg  string
		args []string
	}{
		{"caller", FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "25", IP: "192.0.2.0/24", Comment: "Spam"},
			sysFwLockoutCallerMsg, []string{caller}},
		{"everything", FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "0", IP: "0.0.0.0/0"},
			sysFwLockoutAllMsg, nil},
		{"panel port", FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "8080-8090", IP: "0.0.0.0/0"},
			sysFwLockoutPanelMsg, []string{"8083"}},
		{"ssh port", FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "21,22", IP: "0.0.0.0/0"},
			sysFwLockoutSSHMsg, []string{"22"}},
	}
	for _, c := range cases {
		before := len(h.fixture.Calls())
		var e APIError
		code := h.sysNetDoFrom(base, caller, "POST", "/api/v1/server/firewall/rules", c.req, &e)
		sysFwWantError(t, "add "+c.name, code, e, 409, SysLockoutCode, c.msg, c.args...)
		sysNetWantCalls(t, h.sysNetCalls(before), sshd)

		// The same rule edited in over rule 9 (WEB).
		before = len(h.fixture.Calls())
		req := c.req
		req.Expect = sysFwExpect(sysFwDevRule(t, h, "9"))
		e = APIError{}
		code = h.sysNetDoFrom(base, caller, "PUT", "/api/v1/server/firewall/rules/9", req, &e)
		sysFwWantError(t, "edit "+c.name, code, e, 409, SysLockoutCode, c.msg, c.args...)
		sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "9", "json"}, sshd)
	}

	// A DROP elsewhere is fine.
	before := len(h.fixture.Calls())
	var ok OK
	req := FirewallRuleRequest{Action: "drop", Protocol: "tcp", Port: "25 , 587", IP: "198.51.100.0/24", Comment: "Spam"}
	if code := h.sysNetDoFrom(base, caller, "POST", "/api/v1/server/firewall/rules", req, &ok); code != 200 || ok.Status != "ok" {
		t.Fatalf("add drop: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), sshd,
		[]string{"v-add-firewall-rule", "DROP", "198.51.100.0/24", "25,587", "TCP", "Spam"})
}

func TestSysFwUnsuspendDropLockout(t *testing.T) {
	drop := sysFwTestRule("11", "DROP", "TCP", "0", "0.0.0.0/0", "Block", "yes")
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-firewall-rule": {{Args: []string{"11", "json"}, Stdout: sysFwRulesOut(drop)}},
	}})
	before := len(h.fixture.Calls())
	var e APIError
	code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/11/unsuspend", RuleActionRequest{Expect: sysFwExpect(drop)}, &e)
	sysFwWantError(t, "unsuspend", code, e, 409, SysLockoutCode, sysFwLockoutAllMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "11", "json"}, []string{"v-list-sys-sshd-port", "plain"})
}

func TestSysFwBanCallerLockout(t *testing.T) {
	h, base := sysNetProxied(t, overrides{})
	before := len(h.fixture.Calls())
	var e APIError
	code := h.sysNetDoFrom(base, "192.0.2.50", "POST", "/api/v1/server/firewall/bans", BanRequest{IP: "192.0.2.0/24", Chain: "SSH"}, &e)
	sysFwWantError(t, "ban /24", code, e, 409, SysLockoutCode, sysFwLockoutBanMsg, "192.0.2.50")
	sysNetWantCalls(t, h.sysNetCalls(before))
}

func TestSysFwToggle(t *testing.T) {
	// Turning on with no ACCEPT rule for the panel port (8083) is refused.
	locked := sysFwRulesOut(
		sysFwTestRule("1", "ACCEPT", "ICMP", "0", "0.0.0.0/0", "PING", "no"),
		sysFwTestRule("2", "ACCEPT", "TCP", "8443", "0.0.0.0/0", "HESTIA", "no"),
		sysFwTestRule("3", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no"))
	h, base := sysNetHarness(t, overrides{
		Config:   map[string]string{"FIREWALL_SYSTEM": ""},
		Fixtures: map[string][]fixtureEntry{"v-list-firewall": {{Args: []string{"json"}, Stdout: locked}}},
	})
	before := len(h.fixture.Calls())
	var e APIError
	code := h.sysDo(base, "POST", "/api/v1/server/firewall/toggle", FirewallToggleRequest{Enabled: true}, &e)
	sysFwWantError(t, "enable", code, e, 409, SysLockoutCode, SysLockoutFirewallMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"})

	// Turning off needs no guard.
	before = len(h.fixture.Calls())
	var ref JobRef
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/toggle", FirewallToggleRequest{}, &ref); code != 202 {
		t.Fatalf("disable: %d", code)
	}
	if st := h.sysWaitJob(base, ref.JobID); st.State != "done" || !slices.Equal(st.Steps, []string{"Turn off the firewall"}) {
		t.Fatalf("job = %+v", st)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-delete-sys-firewall"})
}

func TestSysFwToggleOn(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Config: map[string]string{"FIREWALL_SYSTEM": ""}})
	before := len(h.fixture.Calls())
	var ref JobRef
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/toggle", FirewallToggleRequest{Enabled: true}, &ref); code != 202 {
		t.Fatalf("enable: %d", code)
	}
	if st := h.sysWaitJob(base, ref.JobID); st.State != "done" || !slices.Equal(st.Steps, []string{"Turn on the firewall"}) {
		t.Fatalf("job = %+v", st)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"}, []string{"v-add-sys-firewall"})
}

// The settings save (Slice B) calls the same guard through the seam.
func TestSysFwEnableSeam(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-firewall": {{Args: []string{"json"}, Stdout: sysFwRulesOut(
			sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "yes"),
			sysFwTestRule("3", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no"))}},
	}})
	sys := &sysAPI{server: &server{Deps: h.deps, jobs: jobs.NewRegistry(0, nil)}}
	sys.networkRoutes(&sysMux{mux: http.NewServeMux()})
	if sys.firewallEnableCheck == nil {
		t.Fatal("networkRoutes left the seam unset")
	}
	r := httptest.NewRequest("PUT", "/api/v1/server/settings", nil)
	err := sys.checkFirewallEnable(context.Background(), r)
	var f *jobs.Failure
	if !errors.As(err, &f) || f.Code != SysLockoutCode || f.Message != SysLockoutFirewallMsg {
		t.Fatalf("seam: %v", err)
	}

	h2 := newHarness(t)
	sys2 := &sysAPI{server: &server{Deps: h2.deps, jobs: jobs.NewRegistry(0, nil)}}
	sys2.networkRoutes(&sysMux{mux: http.NewServeMux()})
	if err := sys2.checkFirewallEnable(context.Background(), r); err != nil {
		t.Fatalf("dev rules: %v", err)
	}
}

func TestSysFwFingerprintMismatch(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	stale := "0000000000000000"
	for _, action := range []string{"delete", "suspend", "unsuspend", "move-up", "move-down"} {
		before := len(h.fixture.Calls())
		var e APIError
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/4/"+action, RuleActionRequest{Expect: stale}, &e)
		sysFwWantError(t, action, code, e, 409, "changed", sysFwChangedMsg)
		sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "4", "json"})
	}
	before := len(h.fixture.Calls())
	var e APIError
	body := FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "143", IP: "0.0.0.0/0", Comment: "IMAP", Expect: stale}
	code := h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/4", body, &e)
	sysFwWantError(t, "edit", code, e, 409, "changed", sysFwChangedMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "4", "json"})

	before = len(h.fixture.Calls())
	bulk := RuleBulkRequest{Action: "delete", Items: []RuleBulkItem{
		{ID: "4", Expect: sysFwExpect(sysFwDevRule(t, h, "4"))}, {ID: "3", Expect: stale}}}
	e = APIError{}
	code = h.sysDo(base, "POST", "/api/v1/server/firewall/rules/bulk", bulk, &e)
	sysFwWantError(t, "bulk", code, e, 409, "changed", sysFwChangedMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall", "json"})

	// A missing expect is a bad request, not a match.
	e = APIError{}
	code = h.sysDo(base, "POST", "/api/v1/server/firewall/rules/4/delete", RuleActionRequest{}, &e)
	if code != 400 {
		t.Errorf("no expect: %d", code)
	}
}

func TestSysFwBulkDeleteDescending(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	before := len(h.fixture.Calls())
	var rep BulkReply
	body := RuleBulkRequest{Action: "delete", Items: []RuleBulkItem{
		{ID: "3", Expect: sysFwExpect(sysFwDevRule(t, h, "3"))},
		{ID: "4", Expect: sysFwExpect(sysFwDevRule(t, h, "4"))},
	}}
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/bulk", body, &rep); code != 200 || rep.Status != "ok" || len(rep.Failed) != 0 {
		t.Fatalf("bulk: %d %+v", code, rep)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall", "json"},
		[]string{"v-list-sys-sshd-port", "plain"}, // keep-open guard (D5)
		[]string{"v-delete-firewall-rule", "4"},
		[]string{"v-delete-firewall-rule", "3"},
		[]string{"v-list-firewall", "json"})

	// Suspend lists each failure and tries every item.
	h2, base2 := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-suspend-firewall-rule": {
			{Args: []string{"5"}, Stdout: "Error: rules 5 is suspended\n", Exit: 5},
			{Args: []string{"*"}},
		},
	}})
	before = len(h2.fixture.Calls())
	body = RuleBulkRequest{Action: "suspend", Items: []RuleBulkItem{
		{ID: "5", Expect: sysFwExpect(sysFwDevRule(t, h2, "5"))},
		{ID: "6", Expect: sysFwExpect(sysFwDevRule(t, h2, "6"))},
	}}
	if code := h2.sysDo(base2, "POST", "/api/v1/server/firewall/rules/bulk", body, &rep); code != 200 ||
		len(rep.Failed) != 1 || rep.Failed[0].Name != "5" || rep.Failed[0].Message != "Error: rules 5 is suspended" {
		t.Fatalf("bulk suspend: %d %+v", code, rep)
	}
	sysNetWantCalls(t, h2.sysNetCalls(before),
		[]string{"v-list-firewall", "json"},
		[]string{"v-list-sys-sshd-port", "plain"},
		[]string{"v-suspend-firewall-rule", "6"},
		[]string{"v-suspend-firewall-rule", "5"})

	for name, b := range map[string]RuleBulkRequest{
		"unknown action": {Action: "move-up", Items: []RuleBulkItem{{ID: "3", Expect: "x"}}},
		"no items":       {Action: "delete"},
		"bad id":         {Action: "delete", Items: []RuleBulkItem{{ID: "03", Expect: "x"}}},
		"duplicate":      {Action: "delete", Items: []RuleBulkItem{{ID: "3", Expect: "x"}, {ID: "3", Expect: "x"}}},
		"too many":       {Action: "delete", Items: sysNetRepeat(RuleBulkItem{ID: "3", Expect: "x"}, 501)},
	} {
		before := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/bulk", b, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", name, code, e)
		}
		sysNetWantCalls(t, h.sysNetCalls(before))
	}
}

func TestSysFwMoveAndSuspend(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	expect := sysFwExpect(sysFwDevRule(t, h, "5"))
	for action, want := range map[string][]string{
		"move-up":   {"v-move-firewall-rule", "5", "down"}, // evaluated earlier (decision 39)
		"move-down": {"v-move-firewall-rule", "5", "up"},
		"suspend":   {"v-suspend-firewall-rule", "5"},
		"unsuspend": {"v-unsuspend-firewall-rule", "5"},
		"delete":    {"v-delete-firewall-rule", "5"},
	} {
		before := len(h.fixture.Calls())
		var ok OK
		if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/5/"+action, RuleActionRequest{Expect: expect}, &ok); code != 200 || ok.Status != "ok" {
			t.Fatalf("%s: %d", action, code)
		}
		calls := [][]string{{"v-list-firewall-rule", "5", "json"}}
		if action != "unsuspend" { // keep-open guard (D5), move guard (M4)
			calls = append(calls, []string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"})
		}
		calls = append(calls, want)
		if action == "delete" {
			calls = append(calls, []string{"v-list-firewall", "json"})
		}
		sysNetWantCalls(t, h.sysNetCalls(before), calls...)
	}
}

func TestSysFwEditSuspendedRule(t *testing.T) {
	imap := sysFwTestRule("4", "ACCEPT", "TCP", "143,993", "0.0.0.0/0", "IMAP", "yes")
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-firewall-rule": {{Args: []string{"4", "json"}, Stdout: sysFwRulesOut(imap)}},
	}})
	before := len(h.fixture.Calls())
	var ok OK
	body := FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "993", IP: "0.0.0.0/0", Comment: "IMAPS", Expect: sysFwExpect(imap)}
	if code := h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/4", body, &ok); code != 200 {
		t.Fatalf("edit: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-rule", "4", "json"},
		[]string{"v-change-firewall-rule", "4", "ACCEPT", "0.0.0.0/0", "993", "TCP", "IMAPS"},
		[]string{"v-suspend-firewall-rule", "4"})
}

func TestSysFwDeleteLastRule(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-firewall": {{Args: []string{"json"}, Stdout: "{\n}\n"}},
	}})
	before := len(h.fixture.Calls())
	var ok OK
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/1/delete",
		RuleActionRequest{Expect: sysFwExpect(sysFwDevRule(t, h, "1"))}, &ok); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-rule", "1", "json"},
		[]string{"v-delete-firewall-rule", "1"},
		[]string{"v-list-firewall", "json"},
		[]string{"v-update-firewall"})
}

func TestSysFwRuleValidation(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Dir: sysNetFixtures})
	cases := []struct {
		name string
		req  FirewallRuleRequest
		msg  string
		args []string
	}{
		{"blank", FirewallRuleRequest{}, blankFieldMsg, []string{"Action", "Protocol", "Port", "IP Address"}},
		{"pipe port", FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "80|443", IP: "0.0.0.0/0"}, sysFwInvalidPortMsg, []string{"80|443"}},
		{"huge port", FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "99999", IP: "0.0.0.0/0"}, sysFwInvalidPortMsg, []string{"99999"}},
		{"bad action", FirewallRuleRequest{Action: "REJECT", Protocol: "TCP", Port: "80", IP: "0.0.0.0/0"}, sysNetInvalidMsg, []string{"Action"}},
		{"bad protocol", FirewallRuleRequest{Action: "ACCEPT", Protocol: "SCTP", Port: "80", IP: "0.0.0.0/0"}, sysNetInvalidMsg, []string{"Protocol"}},
		{"bad ip", FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "80", IP: "1.2.3.4/33"}, sysNetInvalidIPMsg, []string{"1.2.3.4/33"}},
		{"bad comment", FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "80", IP: "0.0.0.0/0", Comment: "x"}, sysFwInvalidCommentMsg, []string{"x"}},
	}
	for _, c := range cases {
		before := len(h.fixture.Calls())
		var e APIError
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules", c.req, &e)
		sysFwWantError(t, c.name, code, e, 400, "invalid", c.msg, c.args...)
		sysNetWantCalls(t, h.sysNetCalls(before))
	}

	// ipset: the set must exist, be v4 and not be suspended (decision 42).
	for _, name := range []string{"country-nl6", "paused", "missing", "-x"} {
		before := len(h.fixture.Calls())
		var e APIError
		req := FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "0", IP: "ipset:" + name}
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules", req, &e)
		if name == "-x" {
			sysFwWantError(t, name, code, e, 400, "invalid", sysNetInvalidIPMsg, "ipset:-x")
			sysNetWantCalls(t, h.sysNetCalls(before))
			continue
		}
		sysFwWantError(t, name, code, e, 400, "invalid", sysFwInvalidIpsetMsg, name)
		sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-ipset", "json"})
	}
	before := len(h.fixture.Calls())
	var ok OK
	req := FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "0", IP: "ipset:blacklist"}
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules", req, &ok); code != 200 {
		t.Fatalf("ipset rule: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-ipset", "json"},
		[]string{"v-list-sys-sshd-port", "plain"},
		[]string{"v-add-firewall-rule", "DROP", "ipset:blacklist", "0", "TCP", ""})
}

func TestSysFwBans(t *testing.T) {
	h, base := sysNetProxied(t, overrides{})
	const caller = "192.0.2.50"

	before := len(h.fixture.Calls())
	var v BansView
	if code := h.sysNetDoFrom(base, caller, "GET", "/api/v1/server/firewall/bans", nil, &v); code != 200 {
		t.Fatalf("bans: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-ban", "json"})
	var got []string
	for _, b := range v.Rows {
		got = append(got, b.IP+" "+b.CHAIN)
	}
	// Newest first, and both chains of 203.0.113.9 (decision 43).
	if !slices.Equal(got, []string{"203.0.113.9 MAIL", "203.0.113.9 SSH", "198.51.100.0/24 WEB"}) {
		t.Fatalf("rows = %q", got)
	}

	// A ban the core silently skipped (127.0.0.1 never lists) is a 409.
	before = len(h.fixture.Calls())
	var e APIError
	code := h.sysNetDoFrom(base, caller, "POST", "/api/v1/server/firewall/bans", BanRequest{IP: "127.0.0.1", Chain: "SSH"}, &e)
	sysFwWantError(t, "skipped ban", code, e, 409, "not_banned", sysFwBanMissingMsg)
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-add-firewall-ban", "127.0.0.1", "SSH"}, []string{"v-list-firewall-ban", "json"})

	// A ban that lists is a success.
	before = len(h.fixture.Calls())
	var ok OK
	if code := h.sysNetDoFrom(base, caller, "POST", "/api/v1/server/firewall/bans", BanRequest{IP: "203.0.113.9", Chain: "MAIL"}, &ok); code != 200 {
		t.Fatalf("ban: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-add-firewall-ban", "203.0.113.9", "MAIL"}, []string{"v-list-firewall-ban", "json"})

	for name, b := range map[string]BanRequest{
		"blank":     {},
		"bad chain": {IP: "203.0.113.9", Chain: "RECIDIVE"},
		"bad ip":    {IP: "203.0.113.9/40", Chain: "SSH"},
	} {
		before := len(h.fixture.Calls())
		if code := h.sysNetDoFrom(base, caller, "POST", "/api/v1/server/firewall/bans", b, &e); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
		sysNetWantCalls(t, h.sysNetCalls(before))
	}
}

func TestSysFwBanDelete(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-firewall-ban": {
			{Args: []string{"198.51.100.0/24", "WEB"}, Stdout: "Error: boom\n", Exit: 2},
			{Args: []string{"*", "*"}},
		},
	}})
	before := len(h.fixture.Calls())
	var rep BulkReply
	body := BanDeleteRequest{Items: []BanItem{{IP: "203.0.113.9", Chain: "SSH"}, {IP: "198.51.100.0/24", Chain: "WEB"}, {IP: "203.0.113.9", Chain: "MAIL"}}}
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/bans/delete", body, &rep); code != 200 ||
		len(rep.Failed) != 1 || rep.Failed[0].Name != "198.51.100.0/24 WEB" || rep.Failed[0].Message != "Error: boom" {
		t.Fatalf("delete: %d %+v", code, rep)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-delete-firewall-ban", "203.0.113.9", "SSH"},
		[]string{"v-delete-firewall-ban", "198.51.100.0/24", "WEB"},
		[]string{"v-delete-firewall-ban", "203.0.113.9", "MAIL"})
	for name, b := range map[string]BanDeleteRequest{
		"none":      {},
		"too many":  {Items: sysNetRepeat(BanItem{IP: "203.0.113.9", Chain: "SSH"}, 501)},
		"bad chain": {Items: []BanItem{{IP: "203.0.113.9", Chain: "ALL"}}},
	} {
		before := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "POST", "/api/v1/server/firewall/bans/delete", b, &e); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
		sysNetWantCalls(t, h.sysNetCalls(before))
	}
}

func TestSysFwIpsets(t *testing.T) {
	seen := map[string]time.Duration{}
	h := newHarnessWith(t, overrides{Dir: sysNetFixtures}, func(d *Deps) { d.R = sysTimeoutRunner{Runner: d.R, seen: seen} })
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, nil)
	base := srv.URL

	before := len(h.fixture.Calls())
	var v IpsetsView
	if code := h.sysDo(base, "GET", "/api/v1/server/firewall/ipsets", nil, &v); code != 200 || !v.Enabled || len(v.Rows) != 3 ||
		v.Rows[0].LISTNAME != "blacklist" || v.Rows[1].LISTNAME != "country-nl6" || v.Rows[2].LISTNAME != "paused" {
		t.Fatalf("list: %d %+v", code, v)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-ipset", "json"})

	// The add is a job with the download timeout (3 min).
	before = len(h.fixture.Calls())
	var ref JobRef
	url := "https://raw.githubusercontent.com/ipverse/rir-ip/master/country/nl/ipv4-aggregated.txt"
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/ipsets", IpsetRequest{Name: "country-nl", Source: url, IPVersion: "v4", Autoupdate: true}, &ref); code != 202 {
		t.Fatalf("add: %d", code)
	}
	if st := h.sysWaitJob(base, ref.JobID); st.State != "done" || !slices.Equal(st.Steps, []string{"Add IP list"}) {
		t.Fatalf("job = %+v", st)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-add-firewall-ipset", "country-nl", url, "v4", "yes"})
	if seen["v-add-firewall-ipset"] != 3*time.Minute {
		t.Errorf("timeout = %v", seen["v-add-firewall-ipset"])
	}
	before = len(h.fixture.Calls())
	script := "script:/usr/local/hestia/install/common/firewall/ipset/blacklist.sh"
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/ipsets", IpsetRequest{Name: "bl2", Source: script, IPVersion: "v4"}, &ref); code != 202 {
		t.Fatalf("add script: %d", code)
	}
	h.sysWaitJob(base, ref.JobID)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-add-firewall-ipset", "bl2", script, "v4", "no"})

	cases := []struct {
		name string
		req  IpsetRequest
		msg  string
		args []string
	}{
		{"blank", IpsetRequest{}, blankFieldMsg, []string{"Name", "Data Source", "IP Version"}},
		{"28 characters", IpsetRequest{Name: strings.Repeat("a", 28), Source: url, IPVersion: "v4"}, sysNetInvalidMsg, []string{"Name"}},
		{"file source", IpsetRequest{Name: "local", Source: "file:/etc/passwd", IPVersion: "v4"}, sysFwInvalidSourceMsg, []string{"file:/etc/passwd"}},
		{"other script", IpsetRequest{Name: "local", Source: "script:/tmp/x.sh", IPVersion: "v4"}, sysFwInvalidSourceMsg, []string{"script:/tmp/x.sh"}},
		{"quote in url", IpsetRequest{Name: "local", Source: "https://example.com/a'b", IPVersion: "v4"}, sysFwInvalidSourceMsg, []string{"https://example.com/a'b"}},
		{"bad version", IpsetRequest{Name: "local", Source: url, IPVersion: "v5"}, sysNetInvalidMsg, []string{"IP Version"}},
	}
	for _, c := range cases {
		before := len(h.fixture.Calls())
		var e APIError
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/ipsets", c.req, &e)
		sysFwWantError(t, c.name, code, e, 400, "invalid", c.msg, c.args...)
		sysNetWantCalls(t, h.sysNetCalls(before))
	}

	before = len(h.fixture.Calls())
	var ok OK
	if code := h.sysDo(base, "DELETE", "/api/v1/server/firewall/ipsets/paused", nil, &ok); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-delete-firewall-ipset", "paused"})
	var e APIError
	if code := h.sysDo(base, "DELETE", "/api/v1/server/firewall/ipsets/"+strings.Repeat("a", 28), nil, &e); code != 400 {
		t.Errorf("long name delete: %d", code)
	}

	before = len(h.fixture.Calls())
	var rep BulkReply
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/ipsets/bulk", BulkRequest{Action: "delete", Names: []string{"paused", "country-nl6"}}, &rep); code != 200 || len(rep.Failed) != 0 {
		t.Fatalf("bulk: %d %+v", code, rep)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-delete-firewall-ipset", "paused"}, []string{"v-delete-firewall-ipset", "country-nl6"})
}

// IP add, delete and refresh steps run with the IP timeout (5 min).
func TestSysIPJobTimeouts(t *testing.T) {
	seen := map[string]time.Duration{}
	h := newHarnessWith(t, overrides{}, func(d *Deps) { d.R = sysTimeoutRunner{Runner: d.R, seen: seen} })
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, nil)
	var ref JobRef
	h.sysDo(srv.URL, "POST", "/api/v1/server/ips/refresh", nil, &ref)
	h.sysWaitJob(srv.URL, ref.JobID)
	h.sysDo(srv.URL, "DELETE", "/api/v1/server/ips/203.0.113.11", nil, &ref)
	h.sysWaitJob(srv.URL, ref.JobID)
	h.sysDo(srv.URL, "POST", "/api/v1/server/ips", IPRequest{IP: "203.0.113.20", Netmask: "255.255.255.0", Interface: "eth0", Shared: true}, &ref)
	h.sysWaitJob(srv.URL, ref.JobID)
	for _, cmd := range []string{"v-update-sys-ip", "v-delete-sys-ip", "v-add-sys-ip"} {
		if seen[cmd] != 5*time.Minute {
			t.Errorf("%s timeout = %v", cmd, seen[cmd])
		}
	}
}

// C1: the core rewrites every rule commented SSH (v-update-firewall) or HESTIA
// (v-change-sys-port), so only the protected rules themselves may carry them.
func TestSysFwReservedComments(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	for _, comment := range []string{"SSH", "HESTIA"} {
		before := len(h.fixture.Calls())
		var e APIError
		req := FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "25", IP: "0.0.0.0/0", Comment: comment}
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules", req, &e)
		sysFwWantError(t, "add "+comment, code, e, 409, SysLockoutCode, sysFwLockoutReservedMsg)
		sysNetWantCalls(t, h.sysNetCalls(before))

		before = len(h.fixture.Calls())
		req = FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "80,443", IP: "0.0.0.0/0", Comment: comment,
			Expect: sysFwExpect(sysFwDevRule(t, h, "9"))}
		e = APIError{}
		code = h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/9", req, &e)
		sysFwWantError(t, "edit web to "+comment, code, e, 409, SysLockoutCode, sysFwLockoutReservedMsg)
		sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall-rule", "9", "json"})
	}
	// The SSH rule saved unchanged keeps its comment.
	before := len(h.fixture.Calls())
	var ok OK
	req := FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "22", IP: "0.0.0.0/0", Comment: "SSH",
		Expect: sysFwExpect(sysFwDevRule(t, h, "10"))}
	if code := h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/10", req, &ok); code != 200 {
		t.Fatalf("ssh unchanged: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-rule", "10", "json"},
		[]string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"}, // keep-open guard (D5)
		[]string{"v-change-firewall-rule", "10", "ACCEPT", "0.0.0.0/0", "22", "TCP", "SSH"})
}

// I1: behind Cloudflare the proxy forwards the edge address (X-Real-IP), which is
// what iptables sees; session.RealIP is the visitor (CF-Connecting-IP). Both count.
func TestSysFwCloudflareCallers(t *testing.T) {
	cf := map[string]string{"X-Real-IP": "173.245.48.5", "CF-Connecting-IP": "192.0.2.50"}
	h, base := sysNetProxied(t, overrides{})
	sshd := []string{"v-list-sys-sshd-port", "plain"}
	for ip, arg := range map[string]string{"173.245.48.0/20": "173.245.48.5", "192.0.2.0/24": "192.0.2.50"} {
		before := len(h.fixture.Calls())
		var e APIError
		req := FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "443", IP: ip}
		code := h.sysNetDoHeaders(base, cf, "POST", "/api/v1/server/firewall/rules", req, &e)
		sysFwWantError(t, "drop "+ip, code, e, 409, SysLockoutCode, sysFwLockoutCallerMsg, arg)
		sysNetWantCalls(t, h.sysNetCalls(before), sshd)
	}
	before := len(h.fixture.Calls())
	var e APIError
	code := h.sysNetDoHeaders(base, cf, "POST", "/api/v1/server/firewall/bans", BanRequest{IP: "173.245.48.0/24", Chain: "HESTIA"}, &e)
	sysFwWantError(t, "ban edge", code, e, 409, SysLockoutCode, sysFwLockoutBanMsg, "173.245.48.5")
	sysNetWantCalls(t, h.sysNetCalls(before))

	// Turning on with the panel open to the visitor's network only is refused: the
	// panel would see the edge address.
	visitorOnly := sysFwRulesOut(
		sysFwTestRule("2", "ACCEPT", "TCP", "8083", "192.0.2.0/24", "HESTIA", "no"),
		sysFwTestRule("3", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no"))
	h2, base2 := sysNetProxied(t, overrides{
		Config:   map[string]string{"FIREWALL_SYSTEM": ""},
		Fixtures: map[string][]fixtureEntry{"v-list-firewall": {{Args: []string{"json"}, Stdout: visitorOnly}}},
	})
	before = len(h2.fixture.Calls())
	e = APIError{}
	code = h2.sysNetDoHeaders(base2, cf, "POST", "/api/v1/server/firewall/toggle", FirewallToggleRequest{Enabled: true}, &e)
	sysFwWantError(t, "enable", code, e, 409, SysLockoutCode, SysLockoutFirewallMsg)
	sysNetWantCalls(t, h2.sysNetCalls(before), []string{"v-list-firewall", "json"}, sshd)
}

// M1: a proxied request whose address can't be parsed fails closed.
func TestSysFwUnknownCaller(t *testing.T) {
	h, base := sysNetProxied(t, overrides{})
	before := len(h.fixture.Calls())
	var e APIError
	req := FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "25", IP: "198.51.100.0/24"}
	code := h.sysNetDoFrom(base, "garbage", "POST", "/api/v1/server/firewall/rules", req, &e)
	sysFwWantError(t, "narrow drop", code, e, 409, SysLockoutCode, sysFwLockoutUnknownMsg)
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-sys-sshd-port", "plain"})

	before = len(h.fixture.Calls())
	e = APIError{}
	code = h.sysNetDoFrom(base, "garbage", "POST", "/api/v1/server/firewall/bans", BanRequest{IP: "198.51.100.0/24", Chain: "SSH"}, &e)
	sysFwWantError(t, "ban", code, e, 409, SysLockoutCode, sysFwLockoutUnknownMsg)
	sysNetWantCalls(t, h.sysNetCalls(before))

	// A /0 DROP on another port doesn't depend on the caller.
	before = len(h.fixture.Calls())
	var ok OK
	req = FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "25", IP: "0.0.0.0/0", Comment: "No SMTP"}
	if code := h.sysNetDoFrom(base, "garbage", "POST", "/api/v1/server/firewall/rules", req, &ok); code != 200 {
		t.Fatalf("/0 drop: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-sys-sshd-port", "plain"},
		[]string{"v-add-firewall-rule", "DROP", "0.0.0.0/0", "25", "TCP", "No SMTP"})
}

// M4: the DROP guard reads BACKEND_PORT fresh, not from the config cache.
func TestSysFwPanelPortFresh(t *testing.T) {
	h, base := sysNetHarness(t, overrides{})
	h.sysDo(base, "GET", "/api/v1/server/firewall", nil, nil) // warms the config cache
	before := len(h.fixture.Calls())
	var e APIError
	req := FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "8083", IP: "0.0.0.0/0"}
	code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules", req, &e)
	sysFwWantError(t, "panel", code, e, 409, SysLockoutCode, sysFwLockoutPanelMsg, "8083")
	var got []string
	for _, c := range h.fixture.Calls()[before:] {
		if c.Cmd != "v-list-user" { // the session's own read
			got = append(got, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	if !slices.Equal(got, []string{"v-list-sys-config json", "v-list-sys-sshd-port plain"}) {
		t.Fatalf("calls = %q", got)
	}
}

// D5: with the HESTIA and SSH rules already gone (deleted over the CLI), the last
// rule that opens the panel and SSH ports can't be deleted, suspended, edited away or
// bulk-changed; the check reads the rules and ports fresh.
func TestSysFwKeepOpenHandlers(t *testing.T) {
	ping := sysFwTestRule("1", "ACCEPT", "ICMP", "0", "0.0.0.0/0", "PING", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")
	admin := sysFwTestRule("11", "ACCEPT", "TCP", "22,8083", "0.0.0.0/0", "Admin", "no")
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-firewall":      {{Args: []string{"json"}, Stdout: sysFwRulesOut(ping, web, admin)}},
		"v-list-firewall-rule": {{Args: []string{"11", "json"}, Stdout: sysFwRulesOut(admin)}, {Args: []string{"9", "json"}, Stdout: sysFwRulesOut(web)}},
	}})
	reads := [][]string{{"v-list-firewall-rule", "11", "json"}, {"v-list-firewall", "json"}, {"v-list-sys-sshd-port", "plain"}}
	for _, action := range []string{"delete", "suspend"} {
		before := len(h.fixture.Calls())
		var e APIError
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/11/"+action, RuleActionRequest{Expect: sysFwExpect(admin)}, &e)
		sysFwWantError(t, action, code, e, 409, SysLockoutCode, sysFwLockoutClosePanelMsg, "8083")
		sysNetWantCalls(t, h.sysNetCalls(before), reads...)
	}

	before := len(h.fixture.Calls())
	var e APIError
	body := FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "8083", IP: "0.0.0.0/0", Comment: "Admin", Expect: sysFwExpect(admin)}
	code := h.sysDo(base, "PUT", "/api/v1/server/firewall/rules/11", body, &e)
	sysFwWantError(t, "edit", code, e, 409, SysLockoutCode, sysFwLockoutCloseSSHMsg, "22")
	sysNetWantCalls(t, h.sysNetCalls(before), reads...)

	for _, action := range []string{"delete", "suspend"} {
		before := len(h.fixture.Calls())
		e = APIError{}
		bulk := RuleBulkRequest{Action: action, Items: []RuleBulkItem{{ID: "9", Expect: sysFwExpect(web)}, {ID: "11", Expect: sysFwExpect(admin)}}}
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/bulk", bulk, &e)
		sysFwWantError(t, "bulk "+action, code, e, 409, SysLockoutCode, sysFwLockoutClosePanelMsg, "8083")
		sysNetWantCalls(t, h.sysNetCalls(before), []string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"})
	}

	// An unrelated rule still goes.
	before = len(h.fixture.Calls())
	var ok OK
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/9/delete", RuleActionRequest{Expect: sysFwExpect(web)}, &ok); code != 200 {
		t.Fatalf("delete web: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-rule", "9", "json"}, []string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"},
		[]string{"v-delete-firewall-rule", "9"}, []string{"v-list-firewall", "json"})
}

// D6: without the iptables or ipset binaries the core's enable "succeeds" and filters
// nothing, so turning the firewall on (toggle or settings seam) is refused first.
func TestSysFwToolsMissing(t *testing.T) {
	missing := func(d *Deps) { d.FirewallToolsMissing = func() []string { return []string{"iptables", "ipset"} } }
	h := newHarnessWith(t, overrides{Config: map[string]string{"FIREWALL_SYSTEM": ""}}, missing)
	h.loginAdmin()
	srv, _, _ := sysTestServer(h, nil)
	before := len(h.fixture.Calls())
	var e APIError
	code := h.sysDo(srv.URL, "POST", "/api/v1/server/firewall/toggle", FirewallToggleRequest{Enabled: true}, &e)
	sysFwWantError(t, "toggle", code, e, 409, SysFwToolsMissingCode, SysFwToolsMissingMsg, "iptables, ipset")
	sysNetWantCalls(t, h.sysNetCalls(before))

	sys := &sysAPI{server: &server{Deps: h.deps, jobs: jobs.NewRegistry(0, nil)}}
	sys.networkRoutes(&sysMux{mux: http.NewServeMux()})
	err := sys.checkFirewallEnable(context.Background(), httptest.NewRequest("PUT", "/api/v1/server/settings", nil))
	var f *jobs.Failure
	if !errors.As(err, &f) || f.Code != SysFwToolsMissingCode {
		t.Fatalf("seam: %v", err)
	}
}

func TestSysFwToolsMissingPaths(t *testing.T) {
	dir := t.TempDir()
	if got := sysFwToolsMissingIn([]string{dir}); !slices.Equal(got, []string{"iptables", "ipset"}) {
		t.Fatalf("empty dir: %q", got)
	}
	for _, name := range []string{"iptables", "ipset"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := sysFwToolsMissingIn([]string{"/nonexistent", dir}); len(got) != 0 {
		t.Fatalf("present: %q", got)
	}
	if err := os.Chmod(filepath.Join(dir, "ipset"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sysFwToolsMissingIn([]string{dir}); !slices.Equal(got, []string{"ipset"}) {
		t.Fatalf("not executable: %q", got)
	}
}

// D7: "Move up" runs the core's "down" (decision 39), so the core's refusal names the
// other direction; the reply names the one the admin chose.
func TestSysFwMoveEndMessages(t *testing.T) {
	h, base := sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-move-firewall-rule": {
			{Args: []string{"5", "down"}, Stdout: "Cannot move rule down. It's already the last rule.\n", Exit: 1},
			{Args: []string{"5", "up"}, Stdout: "Cannot move rule up. It's already the first rule.\n", Exit: 1},
		},
	}})
	expect := sysFwExpect(sysFwDevRule(t, h, "5"))
	for action, msg := range map[string]string{"move-up": sysFwMoveTopMsg, "move-down": sysFwMoveBottomMsg} {
		var e APIError
		code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/5/"+action, RuleActionRequest{Expect: expect}, &e)
		sysFwWantError(t, action, code, e, 400, "invalid", msg)
	}
}

// Fix round 1: the handlers' side of I1, M3 and M4, and the keep-open guard is
// skipped while the firewall is off (the core refuses the change itself).
func TestSysFwFixRound1Handlers(t *testing.T) {
	office := sysFwTestRule("11", "ACCEPT", "TCP", "0", "ipset:office", "Office", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")
	drop := sysFwTestRule("12", "DROP", "TCP", "22", "0.0.0.0/0", "Block", "no")
	fx := func(rules ...hestia.FirewallRule) map[string][]fixtureEntry {
		one := []fixtureEntry{}
		for _, r := range rules {
			one = append(one, fixtureEntry{Args: []string{r.RULE, "json"}, Stdout: sysFwRulesOut(r)})
		}
		return map[string][]fixtureEntry{
			"v-list-firewall":      {{Args: []string{"json"}, Stdout: sysFwRulesOut(rules...)}},
			"v-list-firewall-rule": one,
		}
	}
	// I1: the ipset rule is the only opener.
	h, base := sysNetHarness(t, overrides{Fixtures: fx(web, office)})
	var e APIError
	code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/11/delete", RuleActionRequest{Expect: sysFwExpect(office)}, &e)
	sysFwWantError(t, "ipset opener", code, e, 409, SysLockoutCode, sysFwLockoutClosePanelMsg, "8083")

	// M4: a CLI-made DROP on the SSH port blocks moves.
	h, base = sysNetHarness(t, overrides{Fixtures: fx(web, office, drop)})
	before := len(h.fixture.Calls())
	e = APIError{}
	code = h.sysDo(base, "POST", "/api/v1/server/firewall/rules/9/move-up", RuleActionRequest{Expect: sysFwExpect(web)}, &e)
	sysFwWantError(t, "move", code, e, 409, SysLockoutCode, sysFwLockoutSSHMsg, "22")
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-rule", "9", "json"}, []string{"v-list-firewall", "json"}, []string{"v-list-sys-sshd-port", "plain"})

	// M3: no sshd port known → refused, not guessed.
	h, base = sysNetHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-sshd-port": {{Args: []string{"plain"}, Stdout: ""}},
	}})
	e = APIError{}
	code = h.sysDo(base, "POST", "/api/v1/server/firewall/rules/5/delete", RuleActionRequest{Expect: sysFwExpect(sysFwDevRule(t, h, "5"))}, &e)
	sysFwWantError(t, "no ssh port, delete", code, e, 409, SysLockoutCode, sysFwLockoutNoSSHPortMsg)
	e = APIError{}
	code = h.sysDo(base, "POST", "/api/v1/server/firewall/rules", FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "25", IP: "198.51.100.0/24"}, &e)
	sysFwWantError(t, "no ssh port, drop", code, e, 409, SysLockoutCode, sysFwLockoutNoSSHPortMsg)

	// Firewall off: no keep-open reads; the core answers.
	h, base = sysNetHarness(t, overrides{Config: map[string]string{"FIREWALL_SYSTEM": ""}, Fixtures: fx(web, office)})
	before = len(h.fixture.Calls())
	var ok OK
	if code := h.sysDo(base, "POST", "/api/v1/server/firewall/rules/11/delete", RuleActionRequest{Expect: sysFwExpect(office)}, &ok); code != 200 {
		t.Fatalf("firewall off: %d", code)
	}
	sysNetWantCalls(t, h.sysNetCalls(before),
		[]string{"v-list-firewall-rule", "11", "json"}, []string{"v-delete-firewall-rule", "11"}, []string{"v-list-firewall", "json"})
}
