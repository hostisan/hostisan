package httpapi

// Contract between the UI's tools data layer (ui/src/api/tools.ts, the terminal page
// and its socket codec, the search page's login-as) and the Go tools routes (Plan 5):
// every call the UI makes reaches a registered route, every /api/v1 tools route is
// used by the UI, every body and websocket message the UI sends decodes with no
// unknown fields, and the non-JSON entry points (/api/, /reset/mail/) reach their
// handlers while the JSON 404 still answers everything else.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// toolsRouteFiles are the files the tools slices register their routes in.
var toolsRouteFiles = []string{"search.go", "restore_files.go", "terminal.go", "files.go", "legacy_api.go", "reset_mail.go"}

// toolsPatterns reads every pattern the tools route files register with mux.Handle.
// TestToolsContractPatternsAreRegistered proves the list matches the real mux.
func toolsPatterns(t *testing.T) []string {
	t.Helper()
	re := regexp.MustCompile(`mux\.Handle\("([^"]+)"`)
	var out []string
	for _, f := range toolsRouteFiles {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			out = append(out, m[1])
		}
	}
	if len(out) < 9 {
		t.Fatalf("tools patterns: %q", out)
	}
	return out
}

// toolsV1Patterns is the /api/v1 subset: the routes the UI must use.
func toolsV1Patterns(t *testing.T) []string {
	var out []string
	for _, p := range toolsPatterns(t) {
		if _, path, ok := strings.Cut(p, " "); ok && strings.HasPrefix(path, "/api/v1/") {
			out = append(out, p)
		}
	}
	return out
}

func toolsUISource(t *testing.T, rel ...string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "..", "ui", "src"}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// toolsUICalls is every call the UI's tools code makes, as "METHOD /api/v1/path" with
// template parameters left as ${...}: tools.ts's api.* calls and its restore-job
// poller, and the terminal page's websocket URL (a GET upgrade).
func toolsUICalls(t *testing.T) []string {
	t.Helper()
	src := toolsUISource(t, "api", "tools.ts")
	methods := map[string]string{"get": "GET", "post": "POST", "put": "PUT", "del": "DELETE"}
	call := regexp.MustCompile(`api\.(get|post|put|del)<[^>]*>\(\s*('[^']*'|` + "`[^`]*`" + `|[^,)]+)`)
	var out []string
	for _, m := range call.FindAllStringSubmatch(src, -1) {
		arg := m[2]
		if arg[0] != '\'' && arg[0] != '`' {
			t.Fatalf("tools.ts: can't resolve the path of %q", m[0])
		}
		path, _, _ := strings.Cut(arg[1:len(arg)-1], "?")
		out = append(out, methods[m[1]]+" /api/v1"+path)
	}
	job := regexp.MustCompile("useJobStatus\\([^`]*`([^`]*)`")
	jm := job.FindStringSubmatch(src)
	if jm == nil {
		t.Fatal("tools.ts: no job poller found")
	}
	out = append(out, "GET /api/v1"+jm[1])

	page := toolsUISource(t, "designs", "tools", "TerminalPage.tsx")
	sock := regexp.MustCompile(`\$\{location\.host\}(/api/v1/[^` + "`" + `]+)` + "`").FindStringSubmatch(page)
	if sock == nil {
		t.Fatal("TerminalPage.tsx: no socket URL found")
	}
	out = append(out, "GET "+sock[1])
	return out
}

// toolsUIPatterns maps every UI call to the one tools pattern of the same shape.
func toolsUIPatterns(t *testing.T) map[string]string {
	t.Helper()
	byShape := map[string][]string{}
	for _, p := range toolsV1Patterns(t) {
		byShape[sysShape(p)] = append(byShape[sysShape(p)], p)
	}
	out := map[string]string{}
	for _, c := range toolsUICalls(t) {
		ps := byShape[sysShape(c)]
		if len(ps) != 1 {
			t.Errorf("UI call %q: tools patterns of its shape %q, want exactly one", c, ps)
			continue
		}
		out[c] = ps[0]
	}
	return out
}

// toolsConcrete fills a pattern's path parameters with valid values.
func toolsConcrete(pattern string) (method, path string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		method, path = "POST", pattern
	}
	path = strings.NewReplacer("{snapshot}", "7d1c0b9a", "{id}", strings.Repeat("0", 32), "{$}", "").Replace(path)
	return method, path
}

func TestToolsContractUICallsCoverEveryRoute(t *testing.T) {
	used := map[string]bool{}
	for _, p := range toolsUIPatterns(t) {
		used[p] = true
	}
	for _, p := range toolsV1Patterns(t) {
		if !used[p] {
			t.Errorf("route %q is registered but the UI never calls it", p)
		}
	}
}

// The patterns read from the source are the ones the real server registers: each
// concrete path resolves to exactly its pattern.
func TestToolsContractPatternsAreRegistered(t *testing.T) {
	h := newHarness(t)
	mux := http.NewServeMux()
	(&server{Deps: h.deps}).registerToolsRoutes(mux)
	for _, p := range toolsPatterns(t) {
		method, path := toolsConcrete(p)
		if _, got := mux.Handler(httptest.NewRequest(method, path, nil)); got != p {
			t.Errorf("%s %s: matched %q, want %q", method, path, got, p)
		}
	}
}

// Every UI call reaches its route on the real server: signed out, 401 rather than
// the JSON 404 catch-all or a 405.
func TestToolsContractUICallsAreRegistered(t *testing.T) {
	h := newHarness(t)
	calls := toolsUIPatterns(t)
	if len(calls) != len(toolsUICalls(t)) || len(calls) == 0 {
		t.Fatalf("calls %v", calls)
	}
	for c, pattern := range calls {
		method, path := toolsConcrete(pattern)
		req, _ := http.NewRequest(method, h.srv.URL+path, nil)
		req.Header.Set("Origin", h.srv.URL) // the socket refuses other origins first
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s (%s %s): status %d, want 401 from a registered route", c, method, path, res.StatusCode)
		}
	}
}

// The bodies below are what the UI sends (ui/src/api/tools.test.tsx,
// IncrementalPages.test.tsx, SearchPage.tsx, terminalSocket.test.ts).
func TestToolsContractUIBodiesDecode(t *testing.T) {
	cases := []struct {
		name string
		into any
		body string
	}{
		{"restore files (IncrementalPages)", &RestoreFilesRequest{}, `{"paths":["/home/admin/notes.txt","/home/admin/Documents"]}`},
		{"login-as from a search row (SearchPage)", &LoginAsRequest{}, `{"user":"bob"}`},
		{"terminal auth message", &termClientMsg{}, `{"type":"auth","token":"0123456789abcdef0123456789abcdef","cols":100,"rows":30}`},
		{"terminal resize message", &termClientMsg{}, `{"type":"resize","cols":120,"rows":40}`},
	}
	for _, c := range cases {
		dec := json.NewDecoder(bytes.NewReader([]byte(c.body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(c.into); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// The terminal codec's messages: every key terminalSocket.ts puts in its JSON
// messages is a field of the Go message, and the login-as call sends {user}.
func TestToolsContractUIMessageKeys(t *testing.T) {
	src := toolsUISource(t, "designs", "tools", "terminalSocket.ts")
	msgs := regexp.MustCompile(`JSON\.stringify\(\{([^}]*)\}\)`).FindAllStringSubmatch(src, -1)
	if len(msgs) != 2 {
		t.Fatalf("terminalSocket.ts: %d JSON messages, want auth and resize", len(msgs))
	}
	types := []string{}
	for _, m := range msgs {
		obj := map[string]any{}
		for _, part := range strings.Split(m[1], ",") {
			key, val, _ := strings.Cut(strings.TrimSpace(part), ":")
			key = strings.TrimSpace(key)
			if v := strings.Trim(strings.TrimSpace(val), "'"); key == "type" {
				obj[key] = v
				types = append(types, v)
			} else if key == "token" {
				obj[key] = "t"
			} else {
				obj[key] = 1
			}
		}
		b, _ := json.Marshal(obj)
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		var msg termClientMsg
		if err := dec.Decode(&msg); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	slices.Sort(types)
	if !slices.Equal(types, []string{"auth", "resize"}) {
		t.Errorf("message types %q", types)
	}
	if !strings.Contains(toolsUISource(t, "designs", "tools", "SearchPage.tsx"), "'/auth/login-as', { user: row.user }") {
		t.Error("SearchPage.tsx: login-as call changed; update the body case above")
	}
	// The DB page reads Slice D's new options field.
	if !strings.Contains(toolsUISource(t, "designs", "db", "DatabasesPage.tsx"), "options?.ssoHint") {
		t.Error("DatabasesPage.tsx no longer reads DBOptions.ssoHint")
	}
}

// Every /api/v1 tools route: signed out 401; a write without CSRF 400; a read-only
// session's write 403 with nothing run or written.
func TestToolsContractGuards(t *testing.T) {
	writes := map[string]bool{}
	for _, p := range toolsV1Patterns(t) {
		writes[p] = strings.HasPrefix(p, "POST ")
	}
	send := func(h *harness, method, path string, csrf bool) int {
		req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(`{}`))
		req.Header.Set("Origin", h.srv.URL)
		req.Header.Set("Content-Type", "application/json")
		if csrf {
			req.Header.Set("X-CSRF-Token", h.token)
		}
		res, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for p, write := range writes {
		t.Run(p, func(t *testing.T) {
			method, path := toolsConcrete(p)
			h, dir, _ := newFilesHarness(t, overrides{})
			h.bootstrap()
			if code := send(h, method, path, true); code != 401 {
				t.Errorf("signed out: %d", code)
			}
			if !write {
				return
			}
			h.loginAdmin()
			// Loading the session lists its user; nothing else may run.
			ranOnly := func(h *harness, before int, what string) {
				for _, c := range h.fixture.Calls()[before:] {
					if !strings.HasPrefix(c.Cmd, "v-list-") {
						t.Errorf("%s ran %s %v", what, c.Cmd, c.Args)
					}
				}
			}
			before := len(h.fixture.Calls())
			if code := send(h, method, path, false); code != 400 {
				t.Errorf("no CSRF: %d", code)
			}
			ranOnly(h, before, "no CSRF")
			ro, rodir, _ := newFilesHarness(t, suspReadOnlyOverrides(t))
			ro.loginSusp()
			before = len(ro.fixture.Calls())
			if code := send(ro, method, path, true); code != 403 {
				t.Errorf("read-only: %d", code)
			}
			ranOnly(ro, before, "read-only")
			if len(sessFiles(t, dir))+len(sessFiles(t, rodir)) != 0 {
				t.Error("a refused request wrote a session file")
			}
		})
	}
}

// The legacy entry points answer in their own formats; anything else under /api/
// still gets the JSON 404.
func TestToolsContractLegacyEntryPoints(t *testing.T) {
	h := newHarness(t)
	get := func(method, path string, body io.Reader) (int, string, string) {
		req, _ := http.NewRequest(method, h.srv.URL+path, body)
		if body != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Content-Type"), string(b)
	}
	for _, p := range []string{"/api/", "/api/index.php"} {
		code, ct, body := get("GET", p, nil)
		if code != 405 || ct != legacyContentType || body != legacyMsgNoData {
			t.Errorf("GET %s: %d %q %q", p, code, ct, body)
		}
	}
	for _, p := range []string{"/reset/mail/", "/reset/mail/index.php"} {
		code, ct, body := get("POST", p, strings.NewReader(url.Values{}.Encode()))
		if code != 200 || ct != resetMailContentType || body != resetMailMsgNoEmail {
			t.Errorf("POST %s: %d %q %q", p, code, ct, body)
		}
	}
	for _, p := range []string{"/api/v2/x", "/api/index.php/x", "/api/v1/terminal/nope"} {
		code, ct, body := get("GET", p, nil)
		var e APIError
		if code != 404 || !strings.HasPrefix(ct, "application/json") || json.Unmarshal([]byte(body), &e) != nil || e.Error.Code != "not_found" {
			t.Errorf("GET %s: %d %q %q", p, code, ct, body)
		}
	}
}
