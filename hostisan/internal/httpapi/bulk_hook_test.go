package httpapi

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
)

// bulkHookMux serves POST /bulk: the DNS zone bulk table plus "purge", with delete
// and purge open to users.
func bulkHookMux(h *harness) *httptest.Server {
	s := &server{Deps: h.deps}
	cmds := maps.Clone(zoneBulk)
	cmds["purge"] = "v-rebuild-dns-domain"
	mux := http.NewServeMux()
	mux.Handle("POST /bulk", s.write(func(w http.ResponseWriter, r *http.Request) {
		s.bulkFor(w, r, cmds, []string{"delete", "purge"}, hestia.ValidDomainArg,
			func(user, n string) []string { return []string{user, n, "no"} }, nil)
	}))
	srv := httptest.NewServer(mux)
	h.t.Cleanup(srv.Close)
	return srv
}

func (h *harness) postJSONTo(base, path string, body any) int {
	h.t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", h.token)
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func (h *harness) ran(cmd string) int {
	n := 0
	for _, c := range h.fixture.Calls() {
		if c.Cmd == cmd {
			n++
		}
	}
	return n
}

func TestBulkForUserActions(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	g := bulkHookMux(h)
	if code := h.postJSONTo(g.URL, "/bulk", BulkRequest{Action: "purge", Names: []string{"bakery.example"}}); code != 200 {
		t.Fatalf("bob purge: %d", code)
	}
	if h.ran("v-rebuild-dns-domain") != 1 {
		t.Errorf("bob purge didn't run the command: %v", h.fixture.Calls())
	}
	if code := h.postJSONTo(g.URL, "/bulk", BulkRequest{Action: "suspend", Names: []string{"bakery.example"}}); code != 403 {
		t.Fatalf("bob suspend: %d, want 403", code)
	}
	if n := h.ran("v-suspend-dns-domain"); n != 0 {
		t.Errorf("bob suspend ran %d commands", n)
	}

	h = newHarness(t)
	h.loginAdmin()
	g = bulkHookMux(h)
	if code := h.postJSONTo(g.URL, "/bulk", BulkRequest{Action: "suspend", Names: []string{"example.com"}}); code != 200 {
		t.Fatalf("admin suspend: %d", code)
	}
	var calls []string
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-suspend-dns-domain" {
			calls = append(calls, strings.Join(c.Args, " "))
		}
	}
	if len(calls) != 1 || calls[0] != "admin example.com no" {
		t.Errorf("admin suspend calls = %v", calls)
	}
}
