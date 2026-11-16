package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// closerLog records the reasons each fake closer received.
type closerLog struct {
	mu  sync.Mutex
	got map[string][]string
}

func (l *closerLog) closer(name string) func(reason string) {
	return func(reason string) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.got == nil {
			l.got = map[string][]string{}
		}
		l.got[name] = append(l.got[name], reason)
	}
}

func (l *closerLog) calls(name string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.got[name])
}

func TestToolSessionsClose(t *testing.T) {
	ts := newToolSessions()
	var l closerLog
	ts.register("s1", "terminal", l.closer("a"))
	ts.register("s1", "files", l.closer("b"))
	unregister := ts.register("s1", "terminal", l.closer("gone"))
	unregister()
	unregister() // twice is harmless
	ts.register("s2", "terminal", l.closer("other"))

	ts.lookChanged("s3")
	if len(l.calls("a")) != 0 || len(l.calls("other")) != 0 {
		t.Fatal("lookChanged on another id closed something")
	}

	ts.sessionEnded("s1")
	for _, name := range []string{"a", "b"} {
		if got := l.calls(name); !slices.Equal(got, []string{"Your session has expired."}) {
			t.Errorf("closer %s got %v", name, got)
		}
	}
	if got := l.calls("gone"); len(got) != 0 {
		t.Errorf("an unregistered closer was called: %v", got)
	}
	if got := l.calls("other"); len(got) != 0 {
		t.Errorf("another session's closer was called: %v", got)
	}
	// Closed entries are dropped: ending the session again calls nothing more.
	ts.sessionEnded("s1")
	if got := l.calls("a"); len(got) != 1 {
		t.Errorf("closer a called %d times", len(got))
	}

	ts.lookChanged("s2")
	if got := l.calls("other"); !slices.Equal(got, []string{"Your session has expired."}) {
		t.Errorf("lookChanged: %v", got)
	}
	// A closer may unregister itself (or register another) while being called.
	var un func()
	un = ts.register("s4", "terminal", func(string) { un(); ts.register("s5", "files", func(string) {}) })
	ts.sessionEnded("s4")
}

// Every way the look user changes or the session ends closes that session's tools.
func TestLookChangeClosesTools(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var l closerLog
	h.api.tools.register(h.sessionID(), "terminal", l.closer("login-as"))

	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	if got := l.calls("login-as"); !slices.Equal(got, []string{"Your session has expired."}) {
		t.Fatalf("login-as: closer got %v", got)
	}

	h.api.tools.register(h.sessionID(), "files", l.closer("return"))
	var reply StatusReply
	if code := h.call("POST", "/api/v1/auth/logout", nil, &reply); code != 200 || reply.Status != "returned" {
		t.Fatalf("return from look: %d %+v", code, reply)
	}
	if got := l.calls("return"); !slices.Equal(got, []string{"Your session has expired."}) {
		t.Fatalf("look return: closer got %v", got)
	}

	h.api.tools.register(h.sessionID(), "terminal", l.closer("logout"))
	res := h.rawPost("/api/v1/auth/logout")
	if res.StatusCode != 200 {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	if got := l.calls("logout"); !slices.Equal(got, []string{"Your session has expired."}) {
		t.Fatalf("logout: closer got %v", got)
	}
	// Logout also expires FileGator's PHP session cookie (decision 16).
	var expired bool
	for _, c := range res.Cookies() {
		if c.Name == "HESTIASID" && c.MaxAge < 0 && c.Path == "/" && c.HttpOnly {
			expired = true
		}
	}
	if !expired {
		t.Fatalf("logout cookies = %v, want HESTIASID expired", res.Cookies())
	}

	// A signed-in session ending any other way (here: a new login replacing the old id)
	// reaches the closers through the store's end hook.
	h.loginAdmin()
	h.api.tools.register(h.sessionID(), "terminal", l.closer("store"))
	h.store.Delete(h.sessionID())
	if got := l.calls("store"); !slices.Equal(got, []string{"Your session has expired."}) {
		t.Fatalf("store delete: closer got %v", got)
	}
}

// rawPost sends a CSRF-protected POST with no body and returns the raw reply.
func (h *harness) rawPost(path string) *http.Response {
	h.t.Helper()
	req, _ := http.NewRequest("POST", h.srv.URL+path, nil)
	req.Header.Set("X-CSRF-Token", h.token)
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestBootstrapToolsFeatures(t *testing.T) {
	type want struct{ terminal, files bool }
	cases := []struct {
		name  string
		o     overrides
		login func(h *harness)
		want  want
	}{
		{name: "admin (bash)", login: (*harness).loginAdmin, want: want{true, true}},
		{name: "bob (nologin)", login: (*harness).loginBob, want: want{false, true}},
		{name: "admin looking at bob", login: func(h *harness) {
			h.loginAdmin()
			h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
		}, want: want{false, true}},
		{name: "WEB_TERMINAL=false", o: overrides{Config: map[string]string{"WEB_TERMINAL": "false"}},
			login: (*harness).loginAdmin, want: want{false, true}},
		{name: "FILE_MANAGER=false", o: overrides{Config: map[string]string{"FILE_MANAGER": "false"}},
			login: (*harness).loginAdmin, want: want{true, false}},
		{name: "read-only susp", o: overrides{
			Config: map[string]string{"POLICY_USER_VIEW_SUSPENDED": "yes"},
			Fixtures: map[string][]fixtureEntry{
				"v-check-user-hash": {{Args: []string{"*", "*", "*"}}},
				"v-get-user-salt": {{Args: []string{"susp", "*", "json"},
					Stdout: `{"susp": {"METHOD": "yescrypt", "SALT": "j9T$abcdefghijklmnop", "TIME": "", "DATE": ""}}`}},
				"v-list-user": hookUserFixtures(t, map[string]map[string]string{"susp": {"SHELL": "bash"}}),
			},
		}, login: func(h *harness) {
			h.bootstrap()
			if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "susp", Password: "pw"}, nil); code != 200 {
				h.t.Fatalf("susp login: %d", code)
			}
		}, want: want{false, false}},
		{name: "ops looking at the protected admin", o: overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}},
			login: func(h *harness) {
				h.loginOps()
				if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil); code != 200 {
					h.t.Fatalf("login-as admin: %d", code)
				}
			}, want: want{false, false}},
		{name: "admin looking at ops (not protected)", o: overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}},
			login: func(h *harness) {
				h.loginAdmin()
				h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "ops"}, nil)
			}, want: want{true, true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarnessWith(t, c.o)
			c.login(h)
			b := h.bootstrap()
			if b.User == nil {
				t.Fatal("not signed in")
			}
			got := want{b.User.Features.Terminal, b.User.Features.FileManager}
			if got != c.want {
				t.Fatalf("terminal, fileManager = %v, want %v", got, c.want)
			}
		})
	}

	h := newHarness(t)
	h.loginBob()
	if b := h.bootstrap(); b.User.Package != "default" || b.HideDocs {
		t.Fatalf("bob: package %q, hideDocs %v", b.User.Package, b.HideDocs)
	}
	h = newHarnessWith(t, overrides{Config: map[string]string{"HIDE_DOCS": "yes"}})
	if b := h.bootstrap(); !b.HideDocs {
		t.Fatal("HIDE_DOCS=yes: hideDocs false (signed out)")
	}
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if b := h.bootstrap(); !b.HideDocs || b.User.Package != "default" {
		t.Fatalf("admin as bob: package %q (the viewed account's), hideDocs %v", b.User.Package, b.HideDocs)
	}
}

// Ruling R1: the restore job status route is GET /api/v1/backup/restore-jobs/{id}. The
// planned /api/v1/backup/incremental/jobs/{id} conflicts with the backup section's
// /incremental/{snapshot}/files and panics the router; the new pattern and the
// restore-files route register next to every existing route.
func TestRestoreJobRoutePattern(t *testing.T) {
	h := newHarness(t)
	routes := []string{
		"GET /api/v1/backup/restore-jobs/{id}",
		"POST /api/v1/backup/incremental/{snapshot}/restore-files",
	}
	probe := map[string]*http.Request{
		routes[0]: httptestRequest("GET", "/api/v1/backup/restore-jobs/0123456789abcdef0123456789abcdef"),
		routes[1]: httptestRequest("POST", "/api/v1/backup/incremental/3c9b8a7e/restore-files"),
	}
	_, handler := newServer(h.deps)
	mux := handler.(referrerHandler).ServeMux
	for _, p := range routes {
		// Slice A registers them itself; until then the test adds them.
		if _, got := mux.Handler(probe[p]); got == p {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panics next to the existing routes: %v", p, r)
				}
			}()
			mux.Handle(p, http.NotFoundHandler())
		}()
		if _, got := mux.Handler(probe[p]); got != p {
			t.Errorf("%s: request matched %q", p, got)
		}
	}

	_, handler = newServer(h.deps)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the planned /backup/incremental/jobs/{id} no longer conflicts; ruling R1 can be revisited")
			}
		}()
		handler.(referrerHandler).ServeMux.Handle("GET /api/v1/backup/incremental/jobs/{id}", http.NotFoundHandler())
	}()
}

func httptestRequest(method, path string) *http.Request {
	r, _ := http.NewRequest(method, "http://example.test"+path, nil)
	return r
}

// Ruling R8 (decision 22): only the legacy API may run commands off the allowlist.
// Across the whole module, RunAny and AnyRunner may be named only in internal/runner/,
// in internal/httpapi/legacy_api*.go, and in the methods of the --dev `logged`
// forwarder in cmd/hostisan/serve.go (tests excepted).
func TestAnyRunnerOnlyInLegacyAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found: %v", err)
	}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "internal/runner" || rel == "testdata" || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if strings.HasPrefix(rel, "internal/httpapi/legacy_api") {
			return nil
		}
		scanned++
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			if rel == "cmd/hostisan/serve.go" && isLoggedMethod(decl) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && (id.Name == "RunAny" || id.Name == "AnyRunner") {
					t.Errorf("%s names %s; only internal/runner, legacy_api*.go and serve.go's logged forwarder may", rel, id.Name)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d files; wrong directory?", scanned)
	}
}

// isLoggedMethod reports whether decl is a method of serve.go's logged type.
func isLoggedMethod(decl ast.Decl) bool {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	id, ok := fn.Recv.List[0].Type.(*ast.Ident)
	return ok && id.Name == "logged"
}
