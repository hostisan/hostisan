package httpapi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

// Legacy /api/ (Slice D, parity API1-API36, decisions 18-23). Expected bodies are
// web/api/index.php's output for the same request, byte for byte (D11).

const (
	legacyTestRootKey   = "Zx9yWv8uTs7rQp6oNm5l"                     // admin's key: USER_ARG_POSITION 0
	legacyTestRootSec   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // its secret
	legacyTestBobKey    = "Bb0bKey0123456789abc"                     // bob's key: USER_ARG_POSITION 1
	legacyTestBobSec    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	legacyTestAPIKey    = "devapikey0123456789" // a legacy data/keys/ key v-check-api-key accepts
	legacyTestHTML      = "text/html; charset=UTF-8"
	legacyTestJSON      = "application/json; charset=utf-8"
	legacyTestNoDataMsg = "Error: data received is null or invalid, check https://hestiacp.com/docs/server-administration/rest-api.html"
)

// legacyEnv is a harness with the API switched on and a private TmpDir (secret files
// and v-make-tmp-file), plus a client over a unix socket that hestia-nginx would use.
type legacyEnv struct {
	t     *testing.T
	h     *harness
	tmp   string
	sock  *http.Client // over the proxy socket: X-Real-IP is trusted
	start int          // calls before this index are ignored
}

func newLegacyEnv(t *testing.T, cfg map[string]string, fixtures map[string][]fixtureEntry, opts ...func(*Deps)) *legacyEnv {
	t.Helper()
	conf := map[string]string{"API": "yes", "API_SYSTEM": "2", "API_ALLOWED_IP": "allow-all"}
	for k, v := range cfg {
		conf[k] = v
	}
	tmp := t.TempDir()
	fx := map[string][]fixtureEntry{"v-test-args": legacyTestArgs}
	for k, v := range fixtures {
		fx[k] = v
	}
	o := overrides{Dir: legacyFixtures, Fixtures: fx, Config: conf}
	opts = append([]func(*Deps){func(d *Deps) { d.TmpDir = tmp; d.Auth.TmpDir = tmp }}, opts...)
	h := newHarnessWith(t, o, opts...)
	e := &legacyEnv{t: t, h: h, tmp: tmp}
	e.sock = serveOnSocket(t, h.srv.Config.Handler)
	return e
}

var legacyFixtures = filepath.Join("..", "..", "testdata", "fixtures", "legacy_api")

// legacyTestArgs is a test-only command (no core script) that shows the argv it got,
// "|"-joined, for each argument list the tests send.
var legacyTestArgs = func() []fixtureEntry {
	var out []fixtureEntry
	for _, args := range [][]string{{"1", "1", "", "1.5"}, {"a", "c"}, {"x", "y"}, {"a"}, {"a", "", "c"}, {"$(id); `x` | y"}, {"b"}, {"q"}, {"slow"}, {"a", "bob"}} {
		out = append(out, fixtureEntry{Args: args, Stdout: "args: " + strings.Join(args, "|") + "\n"})
	}
	return out
}()

// serveOnSocket serves handler on a unix socket marked as the proxy's
// (session.FromProxy), as `hostisan serve --listen unix:…` does, and returns a client
// for it. Its URLs use the host "hostisan".
func serveOnSocket(t *testing.T, handler http.Handler) *http.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ConnContext: func(ctx context.Context, _ net.Conn) context.Context { return session.FromProxy(ctx) }}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
}

// legacyReply is one reply: what a caller of /api/ can see.
type legacyReply struct {
	Status   int
	ExitCode []string // Hestia-Exit-Code values (none: absent)
	Type     string
	Body     string
	Header   http.Header
}

// form builds an urlencoded body from key, value pairs, in order.
func form(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, url.QueryEscape(kv[i])+"="+url.QueryEscape(kv[i+1]))
	}
	return strings.Join(parts, "&")
}

func (e *legacyEnv) send(c *http.Client, base, method, path, ctype, body string, headers map[string]string) legacyReply {
	e.t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return legacyReply{Status: res.StatusCode, ExitCode: res.Header.Values("Hestia-Exit-Code"),
		Type: res.Header.Get("Content-Type"), Body: string(b), Header: res.Header}
}

// post sends a form to /api/ directly (RealIP 127.0.0.1).
func (e *legacyEnv) post(kv ...string) legacyReply {
	e.t.Helper()
	return e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/x-www-form-urlencoded", form(kv...), nil)
}

// postFrom sends a form to /api/ over the proxy socket with X-Real-IP ip.
func (e *legacyEnv) postFrom(ip string, kv ...string) legacyReply {
	e.t.Helper()
	return e.send(e.sock, "http://hostisan", "POST", "/api/", "application/x-www-form-urlencoded", form(kv...), map[string]string{"X-Real-IP": ip})
}

// postJSON sends a raw JSON body to /api/.
func (e *legacyEnv) postJSON(body string) legacyReply {
	e.t.Helper()
	return e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/json", body, nil)
}

// calls is every command run since the last reset, as "cmd arg arg…", without the
// cached v-list-sys-config read; temp file paths read "<tmp>", or "<tmp:line>" for the
// key checks' secret file (its first line, which the fixture matched on).
func (e *legacyEnv) calls() []string {
	out := []string{}
	for _, c := range e.h.fixture.Calls()[e.start:] {
		if c.Cmd == "v-list-sys-config" {
			continue
		}
		args := make([]string, len(c.Args))
		for i, a := range c.Args {
			if c.Secrets != nil && c.Secrets[i] != "" && strings.HasPrefix(c.Cmd, "v-check-a") {
				a = "<tmp:" + c.Secrets[i] + ">"
			} else if strings.HasPrefix(a, e.tmp+"/") {
				a = "<tmp>"
			}
			args[i] = a
		}
		if len(args) == 0 {
			out = append(out, c.Cmd)
			continue
		}
		out = append(out, c.Cmd+" "+strings.Join(args, " "))
	}
	return out
}

func (e *legacyEnv) reset() { e.start = len(e.h.fixture.Calls()) }

// tmpFiles lists TmpDir.
func (e *legacyEnv) tmpFiles() []string {
	entries, _ := os.ReadDir(e.tmp)
	out := []string{}
	for _, en := range entries {
		out = append(out, en.Name())
	}
	return out
}

func (e *legacyEnv) want(name string, got legacyReply, status int, exit, ctype, body string) {
	e.t.Helper()
	var wantExit []string
	if exit != "" {
		wantExit = []string{exit}
	}
	if got.Status != status || !reflect.DeepEqual(got.ExitCode, wantExit) || got.Type != ctype || got.Body != body {
		e.t.Errorf("%s: got %d exit=%q type=%q body=%q; want %d exit=%q type=%q body=%q",
			name, got.Status, got.ExitCode, got.Type, got.Body, status, wantExit, ctype, body)
	}
}

func (e *legacyEnv) wantCalls(name string, want ...string) {
	e.t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := e.calls(); !reflect.DeepEqual(got, want) {
		e.t.Errorf("%s: calls\n got %q\nwant %q", name, got, want)
	}
	e.reset()
}

func TestLegacyDispatch(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	r := e.send(e.h.client, e.h.srv.URL, "GET", "/api/", "", "", nil)
	e.want("GET", r, 405, "405", legacyTestHTML, legacyTestNoDataMsg)
	r = e.send(e.h.client, e.h.srv.URL, "GET", "/api/index.php", "", "", nil)
	e.want("GET index.php", r, 405, "405", legacyTestHTML, legacyTestNoDataMsg)
	for _, body := range []string{"{}", "null", "[]", "0", `"text"`, "[1,2]", "{\"cmd\":\"v-list-users\"}", "not json", `{"user":"admin"} x`,
		`{"user":"admin","cmd":["v-list-users"]}`, `{"user":{"a":1}}`, "{\"user\":\"\xff\"}"} {
		e.want("JSON "+body, e.postJSON(body), 405, "405", legacyTestHTML, legacyTestNoDataMsg)
	}
	e.want("form without user/hash/access_key", e.post("cmd", "v-list-users", "password", "x"), 405, "405", legacyTestHTML, legacyTestNoDataMsg)
	e.want("form with an array field", e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/x-www-form-urlencoded",
		"hash[]=x&cmd=v-list-users", nil), 405, "405", legacyTestHTML, legacyTestNoDataMsg)
	e.wantCalls("refusals")

	// hash=<20>:<40> is an access key (DNS cluster, phpMyAdmin SSO).
	r = e.post("hash", legacyTestRootKey+":"+legacyTestRootSec, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 || r.Type != legacyTestJSON {
		t.Errorf("hash as access key: %+v", r)
	}
	e.wantCalls("hash as access key",
		"v-check-access-key "+legacyTestRootKey+" <tmp:"+legacyTestRootSec+"> v-list-users 127.0.0.1 json",
		"v-list-users json")
	// Not 20:40: a legacy key name (v-check-api-key refuses it).
	r = e.post("hash", legacyTestRootKey+":"+legacyTestRootSec+"x", "cmd", "v-list-users")
	e.want("hash 20:41", r, 401, "9", legacyTestHTML, "Error: authentication failed")
	e.wantCalls("hash 20:41", "v-check-api-key <tmp:/usr/local/hestia/data/keys/"+legacyTestRootKey+":"+legacyTestRootSec+"x> 127.0.0.1")

	// A JSON body works like the form (WHMCS-style callers).
	r = e.postJSON(`{"access_key":"` + legacyTestRootKey + `","secret_key":"` + legacyTestRootSec + `","cmd":"v-list-users","arg1":"json"}`)
	if r.Status != 200 || r.Type != legacyTestJSON {
		t.Errorf("JSON access key: %+v", r)
	}
	e.wantCalls("JSON access key", "v-check-access-key "+legacyTestRootKey+" <tmp:"+legacyTestRootSec+"> v-list-users 127.0.0.1 json", "v-list-users json")
	// JSON scalars read as PHP strings: 1 -> "1", true -> "1", false -> "", 1.5 -> "1.5".
	r = e.postJSON(`{"hash":"` + legacyTestAPIKey + `","cmd":"v-test-args","arg1":1,"arg2":true,"arg3":false,"arg4":1.5,"arg5":null}`)
	e.want("JSON scalars", r, 200, "", legacyTestHTML, "args: 1|1||1.5\n")
	e.wantCalls("JSON scalars", "v-check-api-key <tmp:/usr/local/hestia/data/keys/"+legacyTestAPIKey+"> 127.0.0.1", "v-test-args 1 1  1.5")
	// Multipart forms are $_POST too (file parts are not).
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	mw.WriteField("hash", legacyTestAPIKey)
	mw.WriteField("cmd", "v-test-args")
	fw, _ := mw.CreateFormFile("arg2", "x.txt")
	fw.Write([]byte("file"))
	mw.WriteField("arg1", "q")
	mw.Close()
	e.want("multipart", e.send(e.h.client, e.h.srv.URL, "POST", "/api/", mw.FormDataContentType(), mp.String(), nil), 200, "", legacyTestHTML, "args: q\n")
	e.wantCalls("multipart", "v-check-api-key <tmp:/usr/local/hestia/data/keys/"+legacyTestAPIKey+"> 127.0.0.1", "v-test-args q")
	// A form request with another method isn't read as a form (PHP fills $_POST on POST only).
	r = e.send(e.h.client, e.h.srv.URL, "PUT", "/api/", "application/x-www-form-urlencoded", form("hash", legacyTestAPIKey, "cmd", "v-list-users"), nil)
	e.want("PUT form", r, 405, "405", legacyTestHTML, legacyTestNoDataMsg)
	e.wantCalls("PUT form")

	// The JSON 404 for unknown /api/v1/ paths is untouched.
	r = e.send(e.h.client, e.h.srv.URL, "GET", "/api/v1/nope", "", "", nil)
	if r.Status != 404 || !strings.Contains(r.Body, "not_found") {
		t.Errorf("/api/v1/nope = %+v", r)
	}
}

func TestLegacyReplyHeaders(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	// The bodies echo request text (e.g. "<cmd> command invalid") as text/html, as PHP
	// did: the page must never run as a document on the panel's origin.
	r := e.post("access_key", "x", "secret_key", "y", "cmd", "<script>alert(1)</script>")
	e.want("reflected cmd", r, 422, "2", legacyTestHTML, "Error: <script>alert(1)</script> command invalid")
	if r.Header.Get("X-Content-Type-Options") != "nosniff" || r.Header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Errorf("security headers = %v", r.Header)
	}
}

func TestLegacyDisabled(t *testing.T) {
	e := newLegacyEnv(t, map[string]string{"API": "no"}, nil)
	e.want("data", e.post("user", "admin", "password", "pw", "cmd", "v-list-users"), 400, "11", legacyTestHTML,
		"Error: API has been disabledError: API Disabled")
	e.want("code", e.post("user", "admin", "password", "pw", "cmd", "v-list-users", "returncode", "yes"), 400, "11", legacyTestHTML,
		"Error: API has been disabled11")
	e.want("hash", e.post("hash", legacyTestAPIKey, "cmd", "v-list-users"), 400, "11", legacyTestHTML,
		"Error: API has been disabledError: API Disabled")
	e.wantCalls("disabled")
	// Access keys follow API_SYSTEM, not API.
	r := e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 {
		t.Errorf("access key with API=no: %+v", r)
	}
}

func TestLegacyIP(t *testing.T) {
	e := newLegacyEnv(t, map[string]string{"API_ALLOWED_IP": "10.0.0.1"}, nil)
	e.want("legacy", e.postFrom("192.0.2.5", "hash", legacyTestAPIKey, "cmd", "v-list-users"), 401, "10", legacyTestHTML,
		"Error: IP is not allowed to connect with API")
	e.want("legacy code", e.postFrom("192.0.2.5", "hash", legacyTestAPIKey, "cmd", "v-list-users", "returncode", "yes"), 401, "10", legacyTestHTML, "10")
	e.want("access key", e.postFrom("192.0.2.5", "access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users"), 401, "10", legacyTestHTML,
		"Error: IP is not allowed to connect with API")
	e.wantCalls("refused")
	r := e.postFrom("10.0.0.1", "hash", legacyTestAPIKey, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 {
		t.Errorf("listed IP: %+v", r)
	}
	e.wantCalls("listed IP", "v-check-api-key <tmp:/usr/local/hestia/data/keys/"+legacyTestAPIKey+"> 10.0.0.1", "v-list-users json")

	// 0.0.0.0 is a wildcard for access keys only.
	e = newLegacyEnv(t, map[string]string{"API_ALLOWED_IP": "10.0.0.1,0.0.0.0"}, nil)
	e.want("legacy 0.0.0.0", e.postFrom("192.0.2.5", "hash", legacyTestAPIKey, "cmd", "v-list-users"), 401, "10", legacyTestHTML,
		"Error: IP is not allowed to connect with API")
	e.wantCalls("legacy 0.0.0.0")
	r = e.postFrom("192.0.2.5", "access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 {
		t.Errorf("access key 0.0.0.0: %+v", r)
	}
	e.wantCalls("access key 0.0.0.0", "v-check-access-key "+legacyTestRootKey+" <tmp:"+legacyTestRootSec+"> v-list-users 192.0.2.5 json", "v-list-users json")

	// The installer's empty list refuses everyone.
	e = newLegacyEnv(t, map[string]string{"API_ALLOWED_IP": ""}, nil)
	e.want("empty list", e.post("hash", legacyTestAPIKey, "cmd", "v-list-users"), 401, "10", legacyTestHTML,
		"Error: IP is not allowed to connect with API")
	e.wantCalls("empty list")
}

// A request whose client IP can't be read (proxied without a usable X-Real-IP) is
// refused unless every IP is allowed; PHP's appended "" entry let it through.
func TestLegacyEmptyIP(t *testing.T) {
	e := newLegacyEnv(t, map[string]string{"API_ALLOWED_IP": "10.0.0.1,0.0.0.0"}, nil)
	for _, ip := range []string{"", "not-an-ip"} {
		e.want("legacy "+ip, e.postFrom(ip, "hash", legacyTestAPIKey, "cmd", "v-list-users"), 403, "10", legacyTestHTML,
			"Error: IP is not allowed to connect with API")
		e.want("legacy code "+ip, e.postFrom(ip, "hash", legacyTestAPIKey, "cmd", "v-list-users", "returncode", "yes"), 403, "10", legacyTestHTML, "10")
		e.want("access key "+ip, e.postFrom(ip, "access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users"), 403, "10", legacyTestHTML,
			"Error: IP is not allowed to connect with API")
	}
	e.wantCalls("empty IP")
	e = newLegacyEnv(t, nil, nil) // allow-all
	if r := e.postFrom("", "hash", legacyTestAPIKey, "cmd", "v-list-users", "arg1", "json"); r.Status != 200 {
		t.Errorf("allow-all, empty IP: %+v", r)
	}
	e.wantCalls("allow-all", "v-check-api-key <tmp:/usr/local/hestia/data/keys/"+legacyTestAPIKey+"> ", "v-list-users json")
}

func TestLegacyPassword(t *testing.T) {
	const pw = "Sup3r-Secret-pw"
	e := newLegacyEnv(t, nil, nil)
	r := e.post("user", "admin", "password", pw, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 || len(r.ExitCode) != 0 || !strings.HasPrefix(r.Body, "{") || !strings.HasSuffix(r.Body, "}\n") {
		t.Errorf("password mode: %+v", r)
	}
	e.wantCalls("password mode",
		"v-check-user-password admin <tmp> 127.0.0.1 yes",
		"v-check-user-hash admin <tmp> 127.0.0.1",
		"v-list-users json")
	if files := e.tmpFiles(); len(files) != 0 {
		t.Errorf("temp files left: %v", files)
	}
	for _, c := range e.h.fixture.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), pw) {
			t.Errorf("password in argv: %v", c)
		}
	}

	for name, kv := range map[string][]string{
		"wrong user":       {"user", "bob", "password", pw, "cmd", "v-list-users"},
		"no user":          {"hash", "", "password", pw, "cmd", "v-list-users"},
		"hash 0, no user":  {"hash", "0", "password", pw, "cmd", "v-list-users"},
		"user with spaces": {"user", " admin", "password", pw, "cmd", "v-list-users"},
	} {
		e.want(name, e.post(kv...), 401, "10", legacyTestHTML, "Error: authentication failed")
		e.wantCalls(name)
	}
	e.want("wrong user code", e.post("user", "bob", "password", pw, "cmd", "v-list-users", "returncode", "yes"), 401, "10", legacyTestHTML, "10")
	e.want("missing password", e.post("user", "admin", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: authentication failed")
	e.wantCalls("missing password")
	// An empty password is set: it is checked (and fails in the core).
	e2 := newLegacyEnv(t, nil, map[string][]fixtureEntry{"v-check-user-hash": {{Args: []string{"*", "*", "*"}, Stdout: "Error: password missmatch\n", Exit: 9}}})
	e2.want("wrong password", e2.post("user", "admin", "password", "", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: authentication failed")
	e2.wantCalls("wrong password", "v-check-user-password admin <tmp> 127.0.0.1 yes", "v-check-user-hash admin <tmp> 127.0.0.1")

	// user="" never matches an empty ROOT_USER (PHP compared them; decision 20).
	e3 := newLegacyEnv(t, map[string]string{"ROOT_USER": ""}, nil)
	e3.want("empty root", e3.post("user", "", "password", pw, "cmd", "v-list-users"), 401, "10", legacyTestHTML, "Error: authentication failed")
	e3.wantCalls("empty root")
}

func TestLegacyHash(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	e.want("abc", e.post("hash", "abc", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: authentication failed")
	e.wantCalls("abc", "v-check-api-key <tmp:/usr/local/hestia/data/keys/abc> 127.0.0.1")
	e.want("path", e.post("hash", "../../etc/x", "cmd", "v-list-users", "returncode", "yes"), 401, "9", legacyTestHTML, "9")
	e.wantCalls("path", "v-check-api-key <tmp:/usr/local/hestia/data/keys/x> 127.0.0.1")
	e.want("trailing slash", e.post("hash", "a/b/", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: authentication failed")
	e.wantCalls("trailing slash", "v-check-api-key <tmp:/usr/local/hestia/data/keys/b> 127.0.0.1")
	r := e.post("hash", legacyTestAPIKey, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 || len(r.ExitCode) != 0 {
		t.Errorf("good key: %+v", r)
	}
	e.wantCalls("good key", "v-check-api-key <tmp:/usr/local/hestia/data/keys/"+legacyTestAPIKey+"> 127.0.0.1", "v-list-users json")
	// hash=0 is empty() in PHP: the password branch.
	e.want("hash=0", e.post("hash", "0", "user", "admin", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: authentication failed")
	e.wantCalls("hash=0")
}

func TestLegacyArgs(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	auth := "v-check-api-key <tmp:/usr/local/hestia/data/keys/" + legacyTestAPIKey + "> 127.0.0.1"
	e.want("gap", e.post("hash", legacyTestAPIKey, "cmd", "v-test-args", "arg3", "c", "arg1", "a"), 200, "", legacyTestHTML, "args: a|c\n")
	e.wantCalls("gap", auth, "v-test-args a c")
	e.want("trim", e.post("hash", legacyTestAPIKey, "cmd", " v-test-args\n", "arg1", "  x\t", "arg2", "\x00y\x0b"), 200, "", legacyTestHTML, "args: x|y\n")
	e.wantCalls("trim", auth, "v-test-args x y")
	e.want("arg14", e.post("hash", legacyTestAPIKey, "cmd", "v-test-args", "arg1", "a", "arg14", "z", "arg0", "z"), 200, "", legacyTestHTML, "args: a\n")
	e.wantCalls("arg14", auth, "v-test-args a")
	e.want("empty", e.post("hash", legacyTestAPIKey, "cmd", "v-test-args", "arg1", "a", "arg2", "", "arg3", "c"), 200, "", legacyTestHTML, "args: a||c\n")
	e.wantCalls("empty", auth, "v-test-args a  c")
	// Argument values are argv, never shell text.
	e.want("shell text", e.post("hash", legacyTestAPIKey, "cmd", "v-test-args", "arg1", "$(id); `x` | y"), 200, "", legacyTestHTML, "args: $(id); `x` | y\n")
	e.wantCalls("shell text", auth, "v-test-args $(id); `x` | y")
	// A NUL can't be an argument: nothing runs.
	r := e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/x-www-form-urlencoded", "hash="+legacyTestAPIKey+"&cmd=v-test-args&arg1=a%00b", nil)
	if r.Status != 500 {
		t.Errorf("NUL arg: %+v", r)
	}
	e.wantCalls("NUL arg", auth)
	// The last of a repeated field wins, as in PHP; "." and " " in names read as "_".
	e.want("names", e.post("hash", legacyTestAPIKey, "cmd", "v-nope", "cmd", "v-test-args", "arg1", "a", "arg1", "b"), 200, "", legacyTestHTML, "args: b\n")
	e.wantCalls("names", auth, "v-test-args b")
	e.want("dotted", e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/x-www-form-urlencoded",
		"access.key="+legacyTestRootKey+"&secret+key="+legacyTestRootSec+"&cmd=v-test-args&arg1=q", nil), 200, "0", legacyTestHTML, "args: q")
	e.wantCalls("dotted", "v-check-access-key "+legacyTestRootKey+" <tmp:"+legacyTestRootSec+"> v-test-args 127.0.0.1 json", "v-test-args q")
}

func TestLegacyCommandName(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	auth := "v-check-api-key <tmp:/usr/local/hestia/data/keys/" + legacyTestAPIKey + "> 127.0.0.1"
	e.want("empty", e.post("hash", legacyTestAPIKey, "cmd", ""), 422, "2", legacyTestHTML, "Error: Command not provided")
	e.wantCalls("empty", auth)
	e.want("missing", e.post("hash", legacyTestAPIKey), 422, "2", legacyTestHTML, "Error: Command not provided")
	e.wantCalls("missing", auth)
	e.want("zero", e.post("hash", legacyTestAPIKey, "cmd", "0", "returncode", "yes"), 422, "2", legacyTestHTML, "2")
	e.wantCalls("zero", auth)
	e.want("invalid", e.post("hash", legacyTestAPIKey, "cmd", "v-x;y"), 422, "2", legacyTestHTML, "Error: v-x;y command invalid")
	e.wantCalls("invalid", auth)
	e.want("path", e.post("hash", legacyTestAPIKey, "cmd", "../v-x"), 422, "2", legacyTestHTML, "Error: ../v-x command invalid")
	e.wantCalls("path", auth)
	// A valid name with no command: sudo's failure, exit 1, no output.
	e.want("no such command", e.post("hash", legacyTestAPIKey, "cmd", "v-nope"), 200, "", legacyTestHTML, "OK")
	e.wantCalls("no such command", auth)
	e.want("no such command code", e.post("hash", legacyTestAPIKey, "cmd", "v-nope", "returncode", "yes"), 200, "", legacyTestHTML, "1")
	e.wantCalls("no such command code", auth)
	e.want("not v-", e.post("hash", legacyTestAPIKey, "cmd", "ls", "returncode", "yes"), 200, "", legacyTestHTML, "1")
	e.wantCalls("not v-", auth)
	// Access-key mode checks the name before the key.
	e.want("access key invalid", e.post("access_key", "x", "secret_key", "", "cmd", "v-x;y"), 422, "2", legacyTestHTML, "Error: v-x;y command invalid")
	e.want("access key empty", e.post("access_key", "x", "secret_key", "", "cmd", ""), 422, "2", legacyTestHTML, "Error: Command not provided")
	e.wantCalls("access key names")
}

func TestLegacyDataReplies(t *testing.T) {
	e := newLegacyEnv(t, nil, map[string][]fixtureEntry{
		"v-test-out": {
			{Args: []string{"lines"}, Stdout: "a\nb", Exit: 0},
			{Args: []string{"spaces"}, Stdout: "a  \r\n\nb\t\n", Exit: 0},
			{Args: []string{"none"}, Stdout: "", Exit: 0},
			{Args: []string{"fail"}, Stdout: "Error: boom\n", Exit: 3},
			{Args: []string{"silent-fail"}, Stdout: "", Exit: 4},
		},
	})
	run := func(arg string, extra ...string) legacyReply {
		return e.post(append([]string{"hash", legacyTestAPIKey, "cmd", "v-test-out", "arg1", arg}, extra...)...)
	}
	e.want("lines", run("lines"), 200, "", legacyTestHTML, "a\nb\n")
	e.want("trailing whitespace per line", run("spaces"), 200, "", legacyTestHTML, "a\n\nb\n")
	e.want("no output", run("none"), 200, "", legacyTestHTML, "OK")
	e.want("failure with output", run("fail"), 200, "", legacyTestHTML, "Error: boom\n")
	e.want("failure without output", run("silent-fail"), 200, "", legacyTestHTML, "OK")
	e.want("code ok", run("lines", "returncode", "yes"), 200, "", legacyTestHTML, "0")
	e.want("code fail", run("fail", "returncode", "yes"), 200, "", legacyTestHTML, "3")
	e.want("returncode other", run("fail", "returncode", "YES"), 200, "", legacyTestHTML, "Error: boom\n")
}

func TestMakeTmpFile(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	read := func(name string) (string, os.FileMode, bool) {
		fi, err := os.Lstat(filepath.Join(e.tmp, name))
		if err != nil {
			return "", 0, false
		}
		b, _ := os.ReadFile(filepath.Join(e.tmp, name))
		return string(b), fi.Mode(), true
	}
	auth := "v-check-api-key <tmp:/usr/local/hestia/data/keys/" + legacyTestAPIKey + "> 127.0.0.1"

	e.want("legacy", e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "data", "arg2", "zone.conf"), 200, "", legacyTestHTML, "OK")
	if body, mode, ok := read("zone.conf"); !ok || body != "data\n" || mode != 0o600 {
		t.Errorf("zone.conf = %q %v %v", body, mode, ok)
	}
	e.wantCalls("legacy", auth)
	// Truncates an existing file; code mode answers an empty body (PHP's unset exit code).
	e.want("legacy code", e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", " z ", "arg2", "zone.conf", "returncode", "yes"), 200, "", legacyTestHTML, "")
	if body, _, _ := read("zone.conf"); body != "z\n" {
		t.Errorf("zone.conf after rewrite = %q", body)
	}
	// Legacy mode escapes the name the way PHP's escapeshellcmd did; basename after.
	e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "x", "arg2", "a;b")
	if _, _, ok := read(`a\;b`); !ok {
		t.Errorf(`legacy a;b: files = %v, want a\;b`, e.tmpFiles())
	}
	e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "x", "arg2", "../../etc/passwd")
	if _, _, ok := read("passwd"); !ok {
		t.Errorf("basename: files = %v", e.tmpFiles())
	}
	// Access-key mode: no escaping; the core's permission check runs first.
	r := e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-make-tmp-file", "arg1", "k", "arg2", "a;b")
	e.want("access key", r, 204, "0", legacyTestHTML, "")
	if body, _, ok := read("a;b"); !ok || body != "k\n" {
		t.Errorf("access key a;b = %q %v", body, ok)
	}
	e.want("access key code", e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-make-tmp-file", "arg1", "k", "arg2", "c", "returncode", "yes"),
		200, "0", legacyTestHTML, "0")
	e.reset()
	// A key without the permission gets the core's refusal (ruling R7).
	r = e.post("access_key", legacyTestBobKey, "secret_key", legacyTestBobSec, "cmd", "v-make-tmp-file", "arg1", "bob", "arg2", "bobfile")
	e.want("bob key", r, 401, "10", legacyTestHTML, "Error: Key "+legacyTestBobKey+" don't have permission to run the command v-make-tmp-file")
	if _, _, ok := read("bobfile"); ok {
		t.Error("bob's key wrote a file")
	}
	e.wantCalls("bob key", "v-check-access-key "+legacyTestBobKey+" <tmp:"+legacyTestBobSec+"> v-make-tmp-file 127.0.0.1 json")

	// A symlink at the name is never followed.
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(e.tmp, "link")); err != nil {
		t.Fatal(err)
	}
	e.want("symlink", e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "evil", "arg2", "link"), 500, "", legacyTestHTML, "")
	e.want("symlink key", e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-make-tmp-file", "arg1", "evil", "arg2", "link"),
		500, "", legacyTestHTML, "")
	if b, _ := os.ReadFile(target); string(b) != "keep\n" {
		t.Errorf("symlink followed: target = %q", b)
	}
	// A write that fails (a directory at the name) is a 500, not PHP's silent "OK".
	if err := os.Mkdir(filepath.Join(e.tmp, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.want("directory", e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "x", "arg2", "adir", "returncode", "yes"), 500, "", legacyTestHTML, "")
	// Hidden, dot and empty names write nothing and answer as success.
	before := e.tmpFiles()
	for _, name := range []string{".hidden", "..", ".", "", "/", "a/.x"} {
		e.want("name "+name, e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "x", "arg2", name), 200, "", legacyTestHTML, "OK")
		e.want("key name "+name, e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-make-tmp-file", "arg1", "x", "arg2", name),
			204, "0", legacyTestHTML, "")
	}
	e.want("no arg2", e.post("hash", legacyTestAPIKey, "cmd", "v-make-tmp-file", "arg1", "x"), 200, "", legacyTestHTML, "OK")
	if after := e.tmpFiles(); !reflect.DeepEqual(after, before) {
		t.Errorf("files after refused names: %v (before %v)", after, before)
	}
}

func TestAccessKeyMode(t *testing.T) {
	e := newLegacyEnv(t, nil, map[string][]fixtureEntry{
		"v-test-out": {
			{Args: []string{"{}"}, Stdout: "{}\n", Exit: 0},
			{Args: []string{"zero"}, Stdout: "0\n", Exit: 0},
			{Args: []string{"text"}, Stdout: "  hello\n\n", Exit: 0},
		},
	})
	root := func(kv ...string) legacyReply {
		return e.post(append([]string{"access_key", legacyTestRootKey, "secret_key", legacyTestRootSec}, kv...)...)
	}
	bob := func(kv ...string) legacyReply {
		return e.post(append([]string{"access_key", legacyTestBobKey, "secret_key", legacyTestBobSec}, kv...)...)
	}
	check := func(id, sec, cmd string) string {
		return "v-check-access-key " + id + " <tmp:" + sec + "> " + cmd + " 127.0.0.1 json"
	}

	e.want("empty secret", e.post("access_key", legacyTestRootKey, "secret_key", " ", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: Authentication failed")
	e.want("empty key", e.post("access_key", "0", "secret_key", legacyTestRootSec, "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: Authentication failed")
	e.wantCalls("empty key or secret")

	r := e.post("access_key", legacyTestRootKey, "secret_key", "wrong", "cmd", "v-list-users")
	e.want("wrong secret", r, 401, "9", legacyTestHTML, "Error: Invalid secret key for key "+legacyTestRootKey)
	e.wantCalls("wrong secret", check(legacyTestRootKey, "wrong", "v-list-users"))
	e.want("bin refusal", bob("cmd", "v-list-users", "arg1", "json"), 401, "10", legacyTestHTML,
		"Error: Key "+legacyTestBobKey+" don't have permission to run the command v-list-users")
	e.want("bin refusal code", bob("cmd", "v-list-users", "returncode", "yes"), 401, "10", legacyTestHTML, "10")
	e.want("bad id", e.post("access_key", "bad", "secret_key", "s", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: invalid access_key_id format :: bad")
	e.reset()

	// bob's key: the user argument (position 1, by its original name) must be bob.
	msg := `Error: Key ` + legacyTestBobKey + ` - the "user" argument doesn\'t match the key\'s user`
	e.want("other user", bob("cmd", "v-list-web-domains", "arg1", "alice", "arg2", "json"), 401, "10", legacyTestHTML, msg)
	e.wantCalls("other user", check(legacyTestBobKey, legacyTestBobSec, "v-list-web-domains"))
	e.want("user arg missing", bob("cmd", "v-list-web-domains", "arg2", "bob"), 401, "10", legacyTestHTML, msg)
	e.want("user arg case", bob("cmd", "v-list-web-domains", "arg1", "Bob", "arg2", "json"), 401, "10", legacyTestHTML, msg)
	e.reset()
	r = bob("cmd", "v-list-web-domains", "arg1", " bob ", "arg2", "json")
	if r.Status != 200 || r.Type != legacyTestJSON || !reflect.DeepEqual(r.ExitCode, []string{"0"}) || !strings.HasPrefix(r.Body, "{") || strings.HasSuffix(r.Body, "\n") {
		t.Errorf("bob's own: %+v", r)
	}
	e.wantCalls("bob's own", check(legacyTestBobKey, legacyTestBobSec, "v-list-web-domains"), "v-list-web-domains bob json")

	// The USER argument must also be the argv slot the command reads it from: a key
	// whose command takes USER second, with arg1 missing, would otherwise pass "arg2=bob"
	// while argv[1] holds arg3.
	e2 := newLegacyEnv(t, nil, map[string][]fixtureEntry{"v-check-access-key": {
		{Args: []string{"Ca2Key0123456789abcd", legacyTestBobSec, "*", "*", "json"}, Stdout: "{\"USER\": \"bob\", \"USER_ARG_POSITION\": 2}\n"}}})
	carl := func(kv ...string) legacyReply {
		return e2.post(append([]string{"access_key", "Ca2Key0123456789abcd", "secret_key", legacyTestBobSec, "cmd", "v-test-args"}, kv...)...)
	}
	e2.want("shifted user", carl("arg2", "bob", "arg3", "alice"), 401, "10", legacyTestHTML,
		`Error: Key Ca2Key0123456789abcd - the "user" argument doesn\'t match the key\'s user`)
	e2.wantCalls("shifted user", "v-check-access-key Ca2Key0123456789abcd <tmp:"+legacyTestBobSec+"> v-test-args 127.0.0.1 json")
	e2.want("user in place", carl("arg1", "a", "arg2", "bob"), 200, "0", legacyTestHTML, "args: a|bob")

	// A non-root key needs API_SYSTEM 2.
	e1 := newLegacyEnv(t, map[string]string{"API_SYSTEM": "1"}, nil)
	e1.want("API_SYSTEM 1", e1.post("access_key", legacyTestBobKey, "secret_key", legacyTestBobSec, "cmd", "v-list-web-domains", "arg1", "bob"),
		401, "21", legacyTestHTML, "Error: API has been disabled")
	e1.wantCalls("API_SYSTEM 1", "v-check-access-key "+legacyTestBobKey+" <tmp:"+legacyTestBobSec+"> v-list-web-domains 127.0.0.1 json")
	r = e1.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 {
		t.Errorf("root key with API_SYSTEM 1: %+v", r)
	}
	for _, v := range []string{"0", "", "no", "0.0"} {
		e0 := newLegacyEnv(t, map[string]string{"API_SYSTEM": v}, nil)
		e0.want("API_SYSTEM "+v, e0.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users"),
			400, "11", legacyTestHTML, "Error: API has been disabled")
		e0.want("API_SYSTEM code "+v, e0.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users", "returncode", "yes"),
			400, "11", legacyTestHTML, "11")
		e0.wantCalls("API_SYSTEM " + v)
	}

	// The root key runs anything, with no user check.
	r = root("cmd", "v-list-web-domains", "arg1", "bob", "arg2", "json")
	if r.Status != 200 || r.Type != legacyTestJSON {
		t.Errorf("root key: %+v", r)
	}
	e.want("{} stays text/html", root("cmd", "v-test-out", "arg1", "{}"), 200, "0", legacyTestHTML, "{}")
	e.want("0 is empty: 204", root("cmd", "v-test-out", "arg1", "zero"), 204, "0", legacyTestHTML, "")
	e.want("text trimmed", root("cmd", "v-test-out", "arg1", "text"), 200, "0", legacyTestHTML, "hello")
	e.want("empty output", root("cmd", "v-suspend-user", "arg1", "bob"), 204, "0", legacyTestHTML, "")
	e.want("failure", root("cmd", "v-delete-user", "arg1", "missing"), 400, "3", legacyTestHTML, "Error: user missing doesn't exist")
	e.want("failure code", root("cmd", "v-delete-user", "arg1", "missing", "returncode", "yes"), 200, "3", legacyTestHTML, "3")
	e.want("no such command", root("cmd", "v-nope"), 400, "1", legacyTestHTML, "")
	e.want("no such command code", root("cmd", "v-nope", "returncode", "yes"), 200, "1", legacyTestHTML, "1")
	e.want("suspended owner", e.post("access_key", "Su5pKey0123456789abc", "secret_key", legacyTestRootSec, "cmd", "v-list-users"), 401, "9", legacyTestHTML,
		"Error: user susp is suspended")
}

func TestLegacyBodyLimit(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	body := "hash=" + legacyTestAPIKey + "&cmd=v-make-tmp-file&arg2=big&arg1=" + strings.Repeat("a", 16<<20+1-len("hash="+legacyTestAPIKey+"&cmd=v-make-tmp-file&arg2=big&arg1="))
	r := e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/x-www-form-urlencoded", body, nil)
	if r.Status != 413 {
		t.Errorf("16 MiB + 1: %d", r.Status)
	}
	e.wantCalls("too large")
	if len(e.tmpFiles()) != 0 {
		t.Errorf("files: %v", e.tmpFiles())
	}
	// Exactly 16 MiB is read (a zone file through v-make-tmp-file).
	body = body[:len(body)-1]
	if r := e.send(e.h.client, e.h.srv.URL, "POST", "/api/", "application/x-www-form-urlencoded", body, nil); r.Status != 200 || r.Body != "OK" {
		t.Errorf("16 MiB: %d %.40q", r.Status, r.Body)
	}
}

func TestLegacyRealIP(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	const ip = "203.0.113.50"
	e.postFrom(ip, "hash", "nokey", "cmd", "v-list-users")
	e.postFrom(ip, "user", "admin", "password", "pw", "cmd", "v-list-users", "arg1", "json")
	e.postFrom(ip, "access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users", "arg1", "json")
	// IPv4-mapped IPv6 is unmapped, as get_real_user_ip() did.
	e.postFrom("::ffff:"+ip, "hash", "nokey", "cmd", "v-list-users")
	e.wantCalls("real IP",
		"v-check-api-key <tmp:/usr/local/hestia/data/keys/nokey> "+ip,
		"v-check-user-password admin <tmp> "+ip+" yes",
		"v-check-user-hash admin <tmp> "+ip,
		"v-list-users json",
		"v-check-access-key "+legacyTestRootKey+" <tmp:"+legacyTestRootSec+"> v-list-users "+ip+" json",
		"v-list-users json",
		"v-check-api-key <tmp:/usr/local/hestia/data/keys/nokey> "+ip)
}

// plainRunner hides the fixture's RunAny.
type plainRunner struct{ r runner.Runner }

func (p plainRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	return p.r.Run(ctx, cmd, args...)
}

func TestLegacyNeedsAnyRunner(t *testing.T) {
	e := newLegacyEnv(t, nil, nil, func(d *Deps) { d.R = plainRunner{d.R} })
	r := e.post("hash", legacyTestAPIKey, "cmd", "v-list-users")
	if r.Status != 500 {
		t.Errorf("without AnyRunner: %+v", r)
	}
	e.wantCalls("without AnyRunner")
}

func TestLegacyWriteDeadline(t *testing.T) {
	// A pass-through command may run 300 s; the reply must outlive the server's
	// WriteTimeout (90 s in production; 1 s here, with a 1.5 s command).
	e := newLegacyEnv(t, nil, nil, func(d *Deps) { d.R = slowRunner{d.R.(*runner.Fixture), 1500 * time.Millisecond} })
	e.h.srv.Close()
	srv := &http.Server{Handler: e.h.srv.Config.Handler, WriteTimeout: time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	r := e.send(http.DefaultClient, "http://"+ln.Addr().String(), "POST", "/api/", "application/x-www-form-urlencoded",
		form("hash", legacyTestAPIKey, "cmd", "v-test-args", "arg1", "slow"), nil)
	if r.Status != 200 || r.Body != "args: slow\n" {
		t.Errorf("slow command: %+v", r)
	}
}

// slowRunner delays every pass-through command.
type slowRunner struct {
	f *runner.Fixture
	d time.Duration
}

func (s slowRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	return s.f.Run(ctx, cmd, args...)
}

func (s slowRunner) RunAny(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if d, ok := runner.Timeout(ctx); !ok || d != 300*time.Second {
		return runner.Result{}, runner.ErrTimeout
	}
	time.Sleep(s.d)
	return s.f.RunAny(ctx, cmd, args...)
}

func (s slowRunner) Exists(cmd string) bool { return s.f.Exists(cmd) }

func TestLegacyPHPPorts(t *testing.T) {
	for in, want := range map[string]string{
		"zone.conf": "zone.conf", "a;b": `a\;b`, "a b": "a b", "$(id)": `\$\(id\)`, "a'b": `a\'b`, "'ab'": "'ab'",
		`a"b"c'`: `a"b"c\'`, "x\ny": "x\\\ny", "\xff": "\\\xff", "#&`|*?~<>^[]{}\\": `\#\&\` + "`" + `\|\*\?\~\<\>\^\[\]\{\}\\`,
		`'a"b'`: `'a\"b'`,
	} {
		if got := legacyPHPEscapeShellCmd(in); got != want {
			t.Errorf("escapeshellcmd(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"": "", "/": "", "a/b": "b", "a/b/": "b", "..": "..", "../../x": "x", "//x//": "x", `a\b`: `a\b`} {
		if got := legacyPHPBasename(in); got != want {
			t.Errorf("basename(%q) = %q, want %q", in, got, want)
		}
	}
	if legacyPHPTrim(" \t\n\r\x00\x0bx y\x0b") != "x y" {
		t.Error("trim")
	}
	for in, want := range map[string]bool{"": true, "0": true, "00": false, " ": false, "a": false} {
		if legacyPHPEmpty(in) != want {
			t.Errorf("empty(%q) != %v", in, want)
		}
	}
	for in, want := range map[string]string{"1": "1", "-0": "0", "1.5": "1.5", "100.0": "100", "1e20": "1.0E+20", "0.00001": "1.0E-5",
		"0.0001": "0.0001", "12345678901234567890": "1.2345678901235E+19", "1e13": "10000000000000", "1e14": "1.0E+14", "-2.5e-7": "-2.5E-7"} {
		if got := legacyPHPNumberString(in); got != want {
			t.Errorf("number %s = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]bool{`{"a":1}`: true, "{}": false, "[]": false, "0": false, `"0"`: false, `""`: false, "1": true, `"a"`: true,
		"[0]": true, "true": true, "false": false, "null": false, "x": false, "0.0": false, `{"a":1} x`: false} {
		if got := legacyPHPJSONTruthy(in); got != want {
			t.Errorf("json truthy %s = %v", in, got)
		}
	}
	if got := legacyExecLines([]byte("a \nb\r\n\n c\t")); !reflect.DeepEqual(got, []string{"a", "b", "", " c"}) {
		t.Errorf("legacyExecLines = %q", got)
	}
	if got := legacyExecLines(nil); len(got) != 0 {
		t.Errorf("legacyExecLines(nil) = %q", got)
	}
	f := legacyPHPParseForm("a.b=1&+c=2&d[]=3&e[x=4&f=%zz&g&=h&i=1&i[]=2&j[]=1&j=2&k%00l=5")
	want := legacyFields{"a_b": {val: "1"}, "c": {val: "2"}, "d": {array: true}, "e_x": {val: "4"}, "f": {val: "%zz"}, "g": {val: ""},
		"i": {array: true}, "j": {val: "2"}, "k": {val: "5"}}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("legacyPHPParseForm = %+v\nwant %+v", f, want)
	}
}

// The key checks get their secret in a temp file, never in argv (O6, B2b): the
// scripts read a /tmp path's first line. The fixture runner matches on that line.
func TestLegacyKeySecretViaFile(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	check := func(name, cmd string, i int, secret string) {
		t.Helper()
		var seen bool
		for _, c := range e.h.fixture.Calls()[e.start:] {
			for _, a := range c.Args {
				if strings.Contains(a, secret) {
					t.Errorf("%s: %s argv holds the secret: %q", name, c.Cmd, c.Args)
				}
			}
			if c.Cmd != cmd {
				continue
			}
			seen = true
			if p := c.Args[i]; !strings.HasPrefix(p, e.tmp+"/hostisan-") {
				t.Errorf("%s: %s arg %d = %q, want a secret temp file", name, cmd, i+1, p)
			} else if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("%s: %s left behind (%v)", name, p, err)
			}
		}
		if !seen {
			t.Errorf("%s: %s not run", name, cmd)
		}
		e.reset()
	}
	r := e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec, "cmd", "v-list-users", "arg1", "json")
	if r.Status != 200 {
		t.Errorf("access key: %+v", r)
	}
	check("access key", "v-check-access-key", 1, legacyTestRootSec)
	e.want("wrong secret", e.post("access_key", legacyTestRootKey, "secret_key", strings.Repeat("c", 40), "cmd", "v-list-users"),
		401, "9", legacyTestHTML, "Error: Invalid secret key for key "+legacyTestRootKey)
	check("wrong secret", "v-check-access-key", 1, strings.Repeat("c", 40))
	e.want("api key", e.post("hash", legacyTestAPIKey, "cmd", "v-test-args", "arg1", "q"), 200, "", legacyTestHTML, "args: q\n")
	check("api key", "v-check-api-key", 0, legacyTestAPIKey)
	e.want("wrong api key", e.post("hash", "nope0123456789abcdef", "cmd", "v-list-users"), 401, "9", legacyTestHTML, "Error: authentication failed")
	check("wrong api key", "v-check-api-key", 0, "nope0123456789abcdef")
}

// The key checks read their temp file's first line, so a newline in the key or the
// secret would cut it there: refused like a NUL, before anything runs.
func TestLegacyKeyNewlineRefused(t *testing.T) {
	e := newLegacyEnv(t, nil, nil)
	r := e.post("access_key", legacyTestRootKey, "secret_key", legacyTestRootSec[:20]+"\n"+legacyTestRootSec[20:], "cmd", "v-list-users")
	if r.Status != 500 {
		t.Errorf("secret with a newline: %+v", r)
	}
	e.wantCalls("secret with a newline")
	r = e.post("hash", legacyTestAPIKey+"\nx", "cmd", "v-list-users")
	if r.Status != 500 {
		t.Errorf("hash with a newline: %+v", r)
	}
	e.wantCalls("hash with a newline")
}
