package httpapi

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

const listRecords = "v-list-dns-records admin example.com json"

func recReq() RecordRequest {
	return RecordRequest{Record: "api", Type: "A", Value: "203.0.113.55", Priority: "", TTL: ""}
}

func TestAddRecordArgs(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r OK
	if code := h.call("POST", "/api/v1/dns/example.com/records", recReq(), &r); code != 200 || r.Status != "ok" {
		t.Fatalf("got %d %+v", code, r)
	}
	req := RecordRequest{Record: "@", Type: "MX", Value: "mail.example.com", Priority: "10", TTL: "300"}
	if code := h.call("POST", "/api/v1/dns/example.com/records", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h,
		cmdLine("v-add-dns-record", "admin", "example.com", "api", "A", "203.0.113.55", "", "", "yes", ""),
		cmdLine("v-add-dns-record", "admin", "example.com", "@", "MX", "mail.example.com", "10", "", "yes", "300"))
}

func TestAddRecordBlank(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r struct{ Error ErrorBody }
	req := RecordRequest{Type: "A"}
	if code := h.call("POST", "/api/v1/dns/example.com/records", req, &r); code != 400 || r.Error.Code != "invalid" ||
		r.Error.Message != `Field "%s" can not be blank.` || strings.Join(r.Error.Args, "|") != "Record|IP or Value" {
		t.Fatalf("got %d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestAddRecordReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var r statusReply
	if code := h.call("POST", "/api/v1/dns/example.com/records", recReq(), &r); code != 403 || r.Error.Code != "read_only" {
		t.Fatalf("got %d %+v", code, r)
	}
	for _, p := range []string{"/example.com/records/10/suspend", "/example.com/records/10/unsuspend", "/example.com/records/bulk", "/bulk"} {
		if code := h.call("POST", "/api/v1/dns"+p, BulkRequest{Action: "delete", Names: []string{"10"}}, nil); code != 403 {
			t.Fatalf("%s: %d", p, code)
		}
	}
	if code := h.call("PUT", "/api/v1/dns/example.com/records/10", recReq(), nil); code != 403 {
		t.Fatalf("put: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/dns/example.com/records/10", nil, nil); code != 403 {
		t.Fatalf("delete: %d", code)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

// Stored record 10 in the dev fixtures: www A 203.0.113.10, no priority, ttl 14400.
func storedReq() RecordRequest {
	return RecordRequest{Record: "www", Type: "A", Value: "203.0.113.10", Priority: "", TTL: "14400"}
}

func TestEditRecordUnchangedRunsNothing(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r OK
	if code := h.call("PUT", "/api/v1/dns/example.com/records/10", storedReq(), &r); code != 200 || r.Status != "ok" {
		t.Fatalf("got %d %+v", code, r)
	}
	assertCalls(t, h, listRecords)
}

func TestEditRecordChanged(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := storedReq()
	req.Value = "203.0.113.99"
	if code := h.call("PUT", "/api/v1/dns/example.com/records/10", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, listRecords,
		cmdLine("v-change-dns-record", "admin", "example.com", "10", "www", "A", "203.0.113.99", "", "yes", "14400"))
}

func TestEditRecordUnknownID(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r statusReply
	if code := h.call("PUT", "/api/v1/dns/example.com/records/9999", storedReq(), &r); code != 404 || r.Error.Code != "not_found" {
		t.Fatalf("got %d %+v", code, r)
	}
	assertCalls(t, h, listRecords)
}

func TestEditRecordNoPendingChanges(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-dns-record": {{Args: []string{"*", "*", "*", "*", "*", "*", "*", "*", "*"}, Stdout: "Error: No pending changes in DNS entry.\n", Exit: 4}},
	}})
	h.loginAdmin()
	req := storedReq()
	req.TTL = "300"
	var r statusReply
	if code := h.call("PUT", "/api/v1/dns/example.com/records/10", req, &r); code != 409 || r.Error.Code != "exists" ||
		r.Error.Message != "Error: No pending changes in DNS entry." {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestBadRecordID(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, id := range []string{"0", "01", "abc", "12345678901", "-1"} {
		for _, rt := range []struct{ method, suffix string }{
			{"PUT", ""}, {"DELETE", ""}, {"POST", "/suspend"}, {"POST", "/unsuspend"},
		} {
			if code := h.call(rt.method, "/api/v1/dns/example.com/records/"+id+rt.suffix, recReq(), nil); code != 400 {
				t.Fatalf("%s %s%s: %d", rt.method, id, rt.suffix, code)
			}
		}
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestRecordDeleteSuspendUnsuspend(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, c := range []struct{ method, path string }{
		{"POST", "/records/10/suspend"}, {"POST", "/records/10/unsuspend"}, {"DELETE", "/records/10"},
	} {
		var r OK
		if code := h.call(c.method, "/api/v1/dns/example.com"+c.path, nil, &r); code != 200 || r.Status != "ok" {
			t.Fatalf("%s: %d", c.path, code)
		}
	}
	assertCalls(t, h,
		"v-suspend-dns-record admin example.com 10",
		"v-unsuspend-dns-record admin example.com 10",
		"v-delete-dns-record admin example.com 10")
}

func TestDeleteLastARecord(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-dns-record": {{Args: []string{"*", "*", "*"}, Stdout: "Error: at least one A record should remain active\n", Exit: 2}},
	}})
	h.loginAdmin()
	var r statusReply
	if code := h.call("DELETE", "/api/v1/dns/example.com/records/9", nil, &r); code != 400 || r.Error.Code != "invalid" ||
		r.Error.Message != "Error: at least one A record should remain active" {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestBulkMixedResults(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-dns-domain": {
			{Args: []string{"*", "b.net", "*"}, Stdout: "Error: dns b.net doesn't exist\n", Exit: 3},
			{Args: []string{"*", "*", "*"}},
		},
	}})
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "delete", Names: []string{"a.net", "b.net", "c.net"}}, &r); code != 200 {
		t.Fatalf("got %d", code)
	}
	if r.Status != "ok" || len(r.Failed) != 1 || r.Failed[0].Name != "b.net" || r.Failed[0].Message != "Error: dns b.net doesn't exist" {
		t.Fatalf("reply = %+v", r)
	}
	assertCalls(t, h,
		"v-delete-dns-domain admin a.net no",
		"v-delete-dns-domain admin b.net no",
		"v-delete-dns-domain admin c.net no",
		cmdLine("v-restart-dns"))
}

func TestBulkAllOKFailedIsEmptyArray(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	raw := map[string]any{}
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "rebuild", Names: []string{"example.com"}}, &raw); code != 200 {
		t.Fatalf("got %d", code)
	}
	f, ok := raw["failed"].([]any)
	if !ok || len(f) != 0 {
		t.Fatalf("failed = %#v", raw["failed"])
	}
	assertCalls(t, h, "v-rebuild-dns-domain admin example.com no", cmdLine("v-restart-dns"))
}

func TestBulkZoneActionsAdmin(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, a := range []string{"suspend", "unsuspend"} {
		h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: a, Names: []string{"example.com"}}, nil)
	}
	assertCalls(t, h,
		"v-suspend-dns-domain admin example.com no", cmdLine("v-restart-dns"),
		"v-unsuspend-dns-domain admin example.com no", cmdLine("v-restart-dns"))
}

func TestBulkUserCanDelete(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "delete", Names: []string{"bakery.example"}}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, "v-delete-dns-domain bob bakery.example no", cmdLine("v-restart-dns"))
}

func TestBulkUserForbiddenAction(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, a := range []string{"suspend", "unsuspend", "rebuild"} {
		var r statusReply
		if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: a, Names: []string{"bakery.example"}}, &r); code != 403 || r.Error.Code != "forbidden" {
			t.Fatalf("%s: %d %+v", a, code, r)
		}
		if a == "rebuild" {
			continue // not a record action: unknown there
		}
		if code := h.call("POST", "/api/v1/dns/bakery.example/records/bulk", BulkRequest{Action: a, Names: []string{"1"}}, nil); code != 403 {
			t.Fatalf("records %s: %d", a, code)
		}
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestBulkUnknownAction(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "explode", Names: []string{"example.com"}}, nil); code != 400 {
		t.Fatalf("got %d", code)
	}
	// rebuild is a zone action only.
	if code := h.call("POST", "/api/v1/dns/example.com/records/bulk", BulkRequest{Action: "rebuild", Names: []string{"1"}}, nil); code != 400 {
		t.Fatalf("records rebuild: %d", code)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestBulkValidation(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, req := range []BulkRequest{
		{Action: "delete"},
		{Action: "delete", Names: []string{"a.net", "-bad"}},
	} {
		if code := h.call("POST", "/api/v1/dns/bulk", req, nil); code != 400 {
			t.Fatalf("%+v: %d", req, code)
		}
	}
	if code := h.call("POST", "/api/v1/dns/example.com/records/bulk", BulkRequest{Action: "delete", Names: []string{"1", "x"}}, nil); code != 400 {
		t.Fatalf("records: %d", code)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestBulkRecordsAdminSuspend(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/dns/example.com/records/bulk", BulkRequest{Action: "suspend", Names: []string{"10", "11"}}, &r); code != 200 || r.Status != "ok" || len(r.Failed) != 0 {
		t.Fatalf("got %d %+v", code, r)
	}
	assertCalls(t, h,
		"v-suspend-dns-record admin example.com 10 no",
		"v-suspend-dns-record admin example.com 11 no",
		cmdLine("v-restart-dns"))
}

func TestBulkRecordsUserDelete(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	if code := h.call("POST", "/api/v1/dns/bakery.example/records/bulk", BulkRequest{Action: "delete", Names: []string{"3"}}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, "v-delete-dns-record bob bakery.example 3 no", cmdLine("v-restart-dns"))
}

func TestSingleSuspendIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, p := range []string{
		"/bakery.example/suspend", "/bakery.example/unsuspend",
		"/bakery.example/records/3/suspend", "/bakery.example/records/3/unsuspend",
	} {
		var r statusReply
		if code := h.call("POST", "/api/v1/dns"+p, nil, &r); code != 403 || r.Error.Code != "forbidden" {
			t.Fatalf("%s: %d %+v", p, code, r)
		}
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
	// Delete stays open to users.
	if code := h.call("DELETE", "/api/v1/dns/bakery.example/records/3", nil, nil); code != 200 {
		t.Fatalf("record delete: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/dns/bakery.example", nil, nil); code != 200 {
		t.Fatalf("zone delete: %d", code)
	}
}

func TestBulkTooManyNames(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	names := make([]string, 501)
	for i := range names {
		names[i] = "example.com"
	}
	var r statusReply
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "delete", Names: names}, &r); code != 400 || r.Error.Code != "invalid" {
		t.Fatalf("got %d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("%d calls", len(calls))
	}
}

func TestBodyTooLarge(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	body := map[string]any{"action": "delete", "names": []string{"example.com"}, "pad": strings.Repeat("x", 1<<20)}
	var r statusReply
	if code := h.call("POST", "/api/v1/dns/bulk", body, &r); code != 400 || r.Error.Code != "invalid" {
		t.Fatalf("got %d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestBulkPlainErrorIsFailedAndLogged(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	// No fixture for b.net: the runner returns a plain error, not a CommandError.
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-dns-domain": {{Args: []string{"*", "a.net", "*"}}},
	}})
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "delete", Names: []string{"a.net", "b.net"}}, &r); code != 200 {
		t.Fatalf("got %d", code)
	}
	if len(r.Failed) != 1 || r.Failed[0].Name != "b.net" || r.Failed[0].Message != "Error: internal error" {
		t.Fatalf("reply = %+v", r)
	}
	if !strings.Contains(logs.String(), "no fixture") {
		t.Fatalf("not logged: %q", logs.String())
	}
}

func TestBulkWhileLookingActsAsLookedAtUser(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if code := h.call("POST", "/api/v1/dns/bulk", BulkRequest{Action: "suspend", Names: []string{"bakery.example"}}, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, "v-suspend-dns-domain bob bakery.example no", cmdLine("v-restart-dns"))
}
