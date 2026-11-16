package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hostisan/hostisan/internal/runner"
)

func TestCommandErrorMapping(t *testing.T) {
	cases := []struct {
		exit   int
		status int
		code   string
	}{
		{1, 400, "invalid"}, {2, 400, "invalid"}, {12, 400, "invalid"},
		{3, 404, "not_found"}, {4, 409, "exists"}, {5, 409, "suspended"},
		{6, 409, "not_suspended"}, {7, 409, "in_use"}, {8, 409, "limit"},
		{10, 403, "forbidden"}, {11, 409, "disabled"}, {20, 500, "restart_failed"},
		{9, 500, "command_failed"}, {21, 500, "command_failed"},
	}
	for _, c := range cases {
		err := &runner.CommandError{Cmd: "v-x", Exit: c.exit, Output: "Error: nope\n"}
		status, body := mapError(t, err)
		if status != c.status || body.Code != c.code || body.Message != "Error: nope" || len(body.Args) != 0 {
			t.Errorf("exit %d: %d %+v", c.exit, status, body)
		}
	}
}

func TestCommandErrorMessages(t *testing.T) {
	_, b := mapError(t, &runner.CommandError{Cmd: "v-x", Exit: 3, Output: "Error: dns domain x doesn't exist\n"})
	if b.Message != "Error: dns domain x doesn't exist" {
		t.Errorf("message = %q", b.Message)
	}
	_, b = mapError(t, &runner.CommandError{Cmd: "v-x", Exit: 5})
	if b.Message != "Error code: 5" {
		t.Errorf("empty output: %q", b.Message)
	}
	_, b = mapError(t, &runner.CommandError{Cmd: "v-x", Exit: 1, Output: "Error: Hestia said no\nsecond line\n"})
	if b.Message != "Error: Hostisan said no\nsecond line" {
		t.Errorf("brand/multiline: %q", b.Message)
	}
}

func TestCommandErrorTimeoutAndOther(t *testing.T) {
	status, b := mapError(t, fmt.Errorf("v-x: %w", runner.ErrTimeout))
	if status != 504 || b.Code != "timeout" {
		t.Errorf("timeout: %d %+v", status, b)
	}
	status, b = mapError(t, fmt.Errorf("disk on fire"))
	if status != 500 || b.Code != "server_error" || b.Message != "" {
		t.Errorf("other: %d %+v", status, b)
	}
}

func mapError(t *testing.T, err error) (int, ErrorBody) {
	t.Helper()
	w := httptest.NewRecorder()
	writeCommandError(w, err)
	var e APIError
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return w.Code, e.Error
}

func TestRunTurnsExitIntoCommandError(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-delete-dns-domain": {{Args: []string{"*", "gone.example"}, Stdout: "Error: dns domain gone.example doesn't exist\n", Exit: 3},
			{Args: []string{"*", "*"}}},
	}})
	s := &server{Deps: h.deps}
	if err := s.run(context.Background(), "v-delete-dns-domain", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}
	err := s.run(context.Background(), "v-delete-dns-domain", "admin", "gone.example")
	ce, ok := err.(*runner.CommandError)
	if !ok || ce.Exit != 3 || ce.Output == "" {
		t.Fatalf("err = %#v", err)
	}
}

// guardMux serves read and write routes that echo the acting user.
func guardMux(h *harness) *httptest.Server {
	s := &server{Deps: h.deps}
	echo := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, OK{Status: s.actingUser(r)})
	}
	mux := http.NewServeMux()
	mux.Handle("GET /r", s.read(echo))
	mux.Handle("POST /w", s.write(echo))
	srv := httptest.NewServer(mux)
	h.t.Cleanup(srv.Close)
	return srv
}

func (h *harness) callURL(base, method, path string, out any) int {
	h.t.Helper()
	req, _ := http.NewRequest(method, base+path, nil)
	if h.token != "" {
		req.Header.Set("X-CSRF-Token", h.token)
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

func TestGuardsAndActingUser(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	g := guardMux(h)
	var ok OK
	if code := h.callURL(g.URL, "GET", "/r", nil); code != 401 {
		t.Fatalf("anonymous read: %d", code)
	}
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	if code := h.callURL(g.URL, "GET", "/r", &ok); code != 200 || ok.Status != "admin" {
		t.Fatalf("read: %d %+v", code, ok)
	}
	if code := h.callURL(g.URL, "POST", "/w", &ok); code != 200 || ok.Status != "admin" {
		t.Fatalf("write: %d %+v", code, ok)
	}
	token := h.token
	h.token = ""
	if code := h.callURL(g.URL, "POST", "/w", nil); code != 400 {
		t.Fatalf("write without CSRF token: %d", code)
	}
	h.token = token
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if code := h.callURL(g.URL, "POST", "/w", &ok); code != 200 || ok.Status != "bob" {
		t.Fatalf("login-as write: %d %+v", code, ok)
	}
}

func TestWriteGuardBlocksReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil)
	g := guardMux(h)
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var e APIError
	code := h.callURL(g.URL, "POST", "/w", &e)
	if code != 403 || e.Error.Code != "read_only" {
		t.Fatalf("write: %d %+v", code, e)
	}
	if code := h.callURL(g.URL, "GET", "/r", nil); code != 200 {
		t.Fatalf("read still allowed: %d", code)
	}
}
