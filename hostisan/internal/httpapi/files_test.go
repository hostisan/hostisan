package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// filesHarness is a harness with a temp PHPSessionDir and a fixed clock.
func newFilesHarness(t *testing.T, o overrides) (*harness, string, *testClock) {
	t.Helper()
	dir := t.TempDir()
	clock := &testClock{t: time.Unix(1700000000, 0)}
	h := newHarnessWith(t, o, func(d *Deps) {
		d.PHPSessionDir = dir
		d.Now = clock.Now
	})
	return h, dir, clock
}

// filesPost runs the handoff and returns the reply with its decoded body.
func (h *harness) filesPost(out any) *http.Response {
	h.t.Helper()
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/files/handoff", nil)
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
	return res
}

func (h *harness) sftpKeyCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-add-user-sftp-key" {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// sessFiles lists the PHP session files in dir.
func sessFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func hestiaSIDCookie(res *http.Response) string {
	for _, v := range res.Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, "HESTIASID=") {
			return v
		}
	}
	return ""
}

func TestFilesHandoff(t *testing.T) {
	h, dir, clock := newFilesHarness(t, overrides{})
	h.loginAdmin()
	var reply FilesHandoff
	res := h.filesPost(&reply)
	if res.StatusCode != 200 || reply.URL != "/fm/" {
		t.Fatalf("handoff: %d %+v", res.StatusCode, reply)
	}
	if got := h.sftpKeyCalls(); !slices.Equal(got, []string{"v-add-user-sftp-key admin 30"}) {
		t.Fatalf("calls = %v", got)
	}
	files := sessFiles(t, dir)
	if len(files) != 1 || !regexp.MustCompile(`^sess_[0-9a-f]{64}$`).MatchString(files[0]) {
		t.Fatalf("files = %v", files)
	}
	id := strings.TrimPrefix(files[0], "sess_")
	fi, _ := os.Lstat(filepath.Join(dir, files[0]))
	if fi.Mode().Perm() != 0o600 || !fi.Mode().IsRegular() {
		t.Fatalf("mode = %v", fi.Mode())
	}
	if c := hestiaSIDCookie(res); c != "HESTIASID="+id+"; Path=/; HttpOnly; SameSite=Strict" {
		t.Fatalf("cookie = %q", c)
	}
	data, _ := os.ReadFile(filepath.Join(dir, files[0]))
	content := string(data)
	for _, want := range []string{
		`user|s:5:"admin";`, `look|s:0:"";`, `userContext|s:5:"admin";`, `ROOT_USER|s:5:"admin";`,
		`POLICY_SYSTEM_PROTECTED_ADMIN|s:2:"no";`, `INACTIVE_SESSION_TIMEOUT|s:2:"60";`,
		`LAST_ACTIVITY|i:` + strconv.FormatInt(clock.Now().Unix(), 10) + `;`, `language|s:2:"en";`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("session lacks %s:\n%s", want, content)
		}
	}
	m := regexp.MustCompile(`token\|s:32:"([0-9a-f]{32})";`).FindStringSubmatch(content)
	if m == nil || m[1] == h.token {
		t.Fatalf("token: %v (CSRF %s)\n%s", m, h.token, content)
	}
	if strings.Contains(content, "_sf2_") {
		t.Fatalf("FileGator's own keys written:\n%s", content)
	}
}

func TestFilesHandoffLook(t *testing.T) {
	h, dir, _ := newFilesHarness(t, overrides{})
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as: %d", code)
	}
	if res := h.filesPost(nil); res.StatusCode != 200 {
		t.Fatalf("handoff: %d", res.StatusCode)
	}
	if got := h.sftpKeyCalls(); !slices.Equal(got, []string{"v-add-user-sftp-key bob 30"}) {
		t.Fatalf("calls = %v", got)
	}
	files := sessFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	data, _ := os.ReadFile(filepath.Join(dir, files[0]))
	for _, want := range []string{`user|s:5:"admin";`, `look|s:3:"bob";`, `userContext|s:5:"admin";`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("session lacks %s:\n%s", want, data)
		}
	}
}

func TestFilesHandoffReplaces(t *testing.T) {
	h, dir, _ := newFilesHarness(t, overrides{})
	h.loginAdmin()
	h.filesPost(nil)
	first := sessFiles(t, dir)
	h.filesPost(nil)
	second := sessFiles(t, dir)
	if len(first) != 1 || len(second) != 1 || first[0] == second[0] {
		t.Fatalf("first %v, second %v", first, second)
	}
	// The first file's closer is gone too: logging out deletes only what exists.
	h.call("POST", "/api/v1/auth/logout", nil, nil)
	if got := sessFiles(t, dir); len(got) != 0 {
		t.Fatalf("after logout: %v", got)
	}
}

func TestFilesCleanup(t *testing.T) {
	cases := []struct {
		name string
		end  func(h *harness)
	}{
		{"logout", func(h *harness) {
			res := h.rawPost("/api/v1/auth/logout")
			if c := hestiaSIDCookie(res); !strings.Contains(c, "Max-Age=0") {
				t.Errorf("logout cookie = %q", c)
			}
		}},
		{"look change", func(h *harness) { h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil) }},
		{"DeleteUser", func(h *harness) { h.store.DeleteUser("admin", "") }},
		{"idle sweep", func(h *harness) { h.store.Sweep(time.Now().Add(2*time.Hour), time.Hour) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, dir, _ := newFilesHarness(t, overrides{})
			h.loginAdmin()
			if res := h.filesPost(nil); res.StatusCode != 200 || len(sessFiles(t, dir)) != 1 {
				t.Fatalf("handoff: %d %v", res.StatusCode, sessFiles(t, dir))
			}
			c.end(h)
			if got := sessFiles(t, dir); len(got) != 0 {
				t.Fatalf("left behind: %v", got)
			}
		})
	}

	// Looking back from bob returns to admin: bob's file goes too.
	h, dir, _ := newFilesHarness(t, overrides{})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	h.filesPost(nil)
	h.call("POST", "/api/v1/auth/logout", nil, nil)
	if got := sessFiles(t, dir); len(got) != 0 {
		t.Fatalf("look return left %v", got)
	}
}

func TestFilesRefused(t *testing.T) {
	cases := []struct {
		name   string
		o      overrides
		login  func(h *harness)
		status int
		code   string
	}{
		{"FILE_MANAGER=false", overrides{Config: map[string]string{"FILE_MANAGER": "false"}}, (*harness).loginAdmin, 404, "not_found"},
		{"read-only", suspReadOnlyOverrides(t), (*harness).loginSusp, 403, "read_only"},
		{"protected admin", overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}},
			(*harness).loginOpsAsProtectedAdmin, 403, "forbidden"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, dir, _ := newFilesHarness(t, c.o)
			c.login(h)
			var e APIError
			res := h.filesPost(&e)
			if res.StatusCode != c.status || e.Error.Code != c.code {
				t.Fatalf("got %d %+v", res.StatusCode, e)
			}
			if calls := h.sftpKeyCalls(); len(calls) != 0 {
				t.Fatalf("commands ran: %v", calls)
			}
			if files := sessFiles(t, dir); len(files) != 0 || hestiaSIDCookie(res) != "" {
				t.Fatalf("files %v, cookie %q", files, hestiaSIDCookie(res))
			}
		})
	}
}

func TestFilesSftpKeyFails(t *testing.T) {
	h, dir, _ := newFilesHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-add-user-sftp-key": {{Args: []string{"*", "30"}, Stdout: "Error: 'setfacl' is not available.\n", Exit: 1}},
	}})
	h.loginAdmin()
	var e APIError
	res := h.filesPost(&e)
	if res.StatusCode != 409 || e.Error.Code != "files_unavailable" || e.Error.Message != "Error: 'setfacl' is not available." {
		t.Fatalf("got %d %+v", res.StatusCode, e)
	}
	if files := sessFiles(t, dir); len(files) != 0 || hestiaSIDCookie(res) != "" {
		t.Fatalf("files %v, cookie %q", files, hestiaSIDCookie(res))
	}
}

// Every route Slice C registers: signed out 401; a write without CSRF 400 csrf; a
// read-only write 403.
func TestToolsGuardTable(t *testing.T) {
	routes := []struct {
		method, path string
		write        bool
	}{
		{"POST", "/api/v1/terminal/token", true},
		{"GET", "/api/v1/terminal/socket", false},
		{"POST", "/api/v1/files/handoff", true},
	}
	send := func(h *harness, method, path string, csrf bool) int {
		req, _ := http.NewRequest(method, h.srv.URL+path, nil)
		req.Header.Set("Origin", h.srv.URL)
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
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			h, dir, _ := newFilesHarness(t, overrides{})
			h.bootstrap()
			if code := send(h, r.method, r.path, true); code != 401 {
				t.Errorf("signed out: %d", code)
			}
			if !r.write {
				return
			}
			h.loginAdmin()
			if code := send(h, r.method, r.path, false); code != 400 {
				t.Errorf("no CSRF: %d", code)
			}
			ro, rodir, _ := newFilesHarness(t, suspReadOnlyOverrides(t))
			ro.loginSusp()
			if code := send(ro, r.method, r.path, true); code != 403 {
				t.Errorf("read-only: %d", code)
			}
			if len(h.sftpKeyCalls())+len(ro.sftpKeyCalls()) != 0 || len(sessFiles(t, dir))+len(sessFiles(t, rodir)) != 0 {
				t.Error("a refused request ran a command or wrote a file")
			}
		})
	}
}
