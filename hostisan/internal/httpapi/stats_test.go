package httpapi

import (
	"strings"
	"testing"
)

func TestStatsAdminOverall(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r Stats
	if code := h.call("GET", "/api/v1/stats", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if !r.Overall || r.User != "" || len(r.Months) == 0 || len(r.Users) != 4 {
		t.Fatalf("stats = %+v", r)
	}
	for i := 1; i < len(r.Months); i++ {
		if r.Months[i-1].Date < r.Months[i].Date {
			t.Fatalf("not newest first: %v", r.Months)
		}
	}
	if r.Months[0].UUsers == "" || r.Months[0].UDisk == "" {
		t.Fatalf("fields not decoded: %+v", r.Months[0])
	}
	acctWantCalls(t, h.keyCalls(), "v-list-users-stats json", "v-list-sys-users json")
}

func TestStatsAdminOneUser(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r Stats
	if code := h.call("GET", "/api/v1/stats?user=bob", nil, &r); code != 200 || r.Overall || r.User != "bob" || r.Months[0].UUsers != "" {
		t.Fatalf("stats: %d %+v", code, r)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-stats bob json", "v-list-sys-users json")
}

func TestStatsUserOwn(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var r Stats
	if code := h.call("GET", "/api/v1/stats", nil, &r); code != 200 || r.Overall || r.User != "bob" || r.Users == nil || len(r.Users) != 0 || len(r.Months) == 0 {
		t.Fatalf("stats: %d %+v", code, r)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-stats bob json")
	if code := h.call("GET", "/api/v1/stats?user=admin", nil, nil); code != 403 {
		t.Fatalf("user param: %d", code)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-stats bob json")
}

func TestStatsAdminLookingGetsOwnerData(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if code := h.call("GET", "/api/v1/stats?user=admin", nil, nil); code != 403 {
		t.Fatalf("look + user: %d", code)
	}
	acctWantCalls(t, h.keyCalls())
	var r Stats
	if code := h.call("GET", "/api/v1/stats", nil, &r); code != 200 || r.User != "bob" || r.Overall || len(r.Users) != 0 {
		t.Fatalf("look: %d %+v", code, r)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-stats bob json")
}

func TestStatsInvalidUser(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, bad := range []string{"-x", "a%20b", "x;y"} {
		if code := h.call("GET", "/api/v1/stats?user="+bad, nil, nil); code != 400 {
			t.Errorf("user=%q = %d", bad, code)
		}
	}
	acctWantCalls(t, h.keyCalls())
}

func TestStatsHideAdmin(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_HIDE_ADMIN": "yes"}})
	h.loginAdmin()
	var r Stats
	h.call("GET", "/api/v1/stats", nil, &r)
	if strings.Join(r.Users, ",") != "bob,susp,ops" || len(r.Months) == 0 {
		t.Fatalf("users = %v", r.Users)
	}
	// Typing the root user still works.
	if code := h.call("GET", "/api/v1/stats?user=admin", nil, &r); code != 200 || r.User != "admin" {
		t.Fatalf("root: %d %+v", code, r)
	}
}

func TestStatsEmptyAndError(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-users-stats": {{Args: []string{"json"}, Stdout: ""}},
	}})
	h.loginAdmin()
	var r Stats
	if code := h.call("GET", "/api/v1/stats", nil, &r); code != 200 || r.Months == nil || len(r.Months) != 0 {
		t.Fatalf("empty: %d %+v", code, r)
	}
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user-stats": {{Args: []string{"*", "json"}, Stdout: "Error: boom\n", Exit: 4}},
	}})
	h.loginBob()
	var e APIError
	if code := h.call("GET", "/api/v1/stats", nil, &e); code == 200 || e.Error.Message != "Error: boom" {
		t.Fatalf("error: %d %+v", code, e)
	}
}

func TestStatsEmptyUserIsOverall(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r Stats
	if code := h.call("GET", "/api/v1/stats?user=", nil, &r); code != 200 || !r.Overall {
		t.Fatalf("%d %+v", code, r)
	}
	h = newHarness(t)
	h.loginBob()
	if code := h.call("GET", "/api/v1/stats?user=", nil, &r); code != 200 || r.User != "bob" {
		t.Fatalf("user: %d %+v", code, r)
	}
}
