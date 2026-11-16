package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

const (
	webSSLCrt = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"
	webSSLKey = "-----BEGIN PRIVATE KEY-----\nSECRETKEYBYTES\n-----END PRIVATE KEY-----"
	webSSLCA  = "-----BEGIN CERTIFICATE-----\nCCCC\n-----END CERTIFICATE-----"
)

// webSSLState is the stored state of the test domain ssl.example (user admin).
type webSSLState struct {
	SSL, LE, Force, HSTS, Algo, Home string
}

// webSSLSnap is what a spied SSL command found in its temp directory.
type webSSLSnap struct {
	Cmd     string
	Dir     string
	DirMode os.FileMode
	Files   map[string]string
	Modes   map[string]os.FileMode
}

type webSSLSpy struct {
	runner.Runner
	snaps    []webSSLSnap
	timeouts map[string]time.Duration
}

func (s *webSSLSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if s.timeouts == nil {
		s.timeouts = map[string]time.Duration{}
	}
	s.timeouts[cmd], _ = runner.Timeout(ctx)
	if cmd == "v-add-web-domain-ssl" || cmd == "v-change-web-domain-sslcert" {
		sn := webSSLSnap{Cmd: cmd, Dir: args[2], Files: map[string]string{}, Modes: map[string]os.FileMode{}}
		if st, err := os.Stat(args[2]); err == nil {
			sn.DirMode = st.Mode().Perm()
		}
		ents, _ := os.ReadDir(args[2])
		for _, e := range ents {
			b, _ := os.ReadFile(filepath.Join(args[2], e.Name()))
			sn.Files[e.Name()] = string(b)
			if st, err := e.Info(); err == nil {
				sn.Modes[e.Name()] = st.Mode().Perm()
			}
		}
		s.snaps = append(s.snaps, sn)
	}
	return s.Runner.Run(ctx, cmd, args...)
}

var webSSLWriteCmds = []string{
	"v-add-letsencrypt-domain", "v-delete-letsencrypt-domain", "v-add-web-domain-ssl", "v-change-web-domain-sslcert",
	"v-delete-web-domain-ssl", "v-add-web-domain-ssl-force", "v-delete-web-domain-ssl-force",
	"v-add-web-domain-ssl-hsts", "v-delete-web-domain-ssl-hsts",
}

// webSSLHarness serves ssl.example (SSL state st, stored aliases a.ssl.example and
// b.ssl.example) with every SSL write succeeding, except those in exits.
func webSSLHarness(t *testing.T, st webSSLState, exits map[string]int) (*harness, *webSSLSpy) {
	t.Helper()
	switch st.Home {
	case "":
		st.Home = "same"
	case "-": // a stored empty SSL_HOME
		st.Home = ""
	}
	if st.Force == "" {
		st.Force = "no"
	}
	if st.HSTS == "" {
		st.HSTS = "no"
	}
	dj, _ := json.Marshal(map[string]any{"ssl.example": map[string]string{
		"IP": "203.0.113.10", "ALIAS": "a.ssl.example,b.ssl.example", "SSL": st.SSL, "LETSENCRYPT": st.LE,
		"SSL_FORCE": st.Force, "SSL_HSTS": st.HSTS, "SSL_KEY_ALGO": st.Algo, "SSL_HOME": st.Home, "SUSPENDED": "no",
	}})
	sj, _ := json.Marshal(map[string]any{"ssl.example": map[string]string{"CRT": webSSLCrt, "KEY": webSSLKey, "CA": webSSLCA}})
	fx := map[string][]fixtureEntry{
		"v-list-web-domain":     {{Args: []string{"admin", "ssl.example", "json"}, Stdout: string(dj)}},
		"v-list-web-domain-ssl": {{Args: []string{"admin", "ssl.example", "json"}, Stdout: string(sj)}},
	}
	for _, cmd := range webSSLWriteCmds {
		var es []fixtureEntry
		for n := 2; n <= 6; n++ {
			args := make([]string, n)
			for i := range args {
				args[i] = "*"
			}
			e := fixtureEntry{Args: args}
			if code := exits[cmd]; code != 0 {
				e.Stdout, e.Exit = "Error: boom\n", code
			}
			es = append(es, e)
		}
		fx[cmd] = es
	}
	spy := &webSSLSpy{}
	h := newHarnessWith(t, overrides{Fixtures: fx, Config: map[string]string{"PROXY_SYSTEM": "nginx"}}, func(d *Deps) {
		spy.Runner = d.R
		d.R = spy
	})
	h.loginAdmin()
	return h, spy
}

func (h *harness) webSSLPut(req WebSSLRequest) (int, APIError) {
	var r APIError
	code := h.call("PUT", "/api/v1/web/ssl.example/ssl", req, &r)
	return code, r
}

// webSSLLog is every command run except the reads and the session's own, with exact
// arguments (an empty argument shows as ”).
func (h *harness) webSSLLog() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		switch c.Cmd {
		case "v-list-sys-config", "v-list-user", "v-list-users", "v-get-user-salt", "v-check-user-password",
			"v-check-user-hash", "v-check-user-2fa", "v-log-user-login", "v-log-user-logout", "v-log-action":
			continue
		}
		if strings.HasPrefix(c.Cmd, "v-list-") {
			continue
		}
		parts := []string{c.Cmd}
		for _, a := range c.Args {
			parts = append(parts, fmt.Sprintf("%q", a))
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

func webSSLExpect(t *testing.T, h *harness, want ...string) {
	t.Helper()
	got := h.webSSLLog()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls:\n got  %q\n want %q", got, want)
	}
}

func webSSLDone(t *testing.T, h *harness, want ...string) {
	t.Helper()
	webSSLExpect(t, h, append(want, "v-restart-web", "v-restart-proxy")...)
}

func webSSLNoLeftover(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("temp dir left behind: %v", err)
	}
}

func TestWebSSLReissueFlag(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "ecdsa-256", Reissue: true}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLDone(t, h, `v-add-letsencrypt-domain "admin" "ssl.example" "a.ssl.example,b.ssl.example" "" "ecdsa-256"`)
}

func TestWebSSLReissueOnKeyTypeChange(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "rsa"}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLDone(t, h, `v-add-letsencrypt-domain "admin" "ssl.example" "a.ssl.example,b.ssl.example" "" "rsa"`)
}

func TestWebSSLNoReissueWhenNothingChanged(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256"}, nil)
	for _, algo := range []string{"ecdsa-256", "bogus", ""} {
		if code, r := h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: algo}); code != 200 {
			t.Fatalf("got %d %+v", code, r)
		}
	}
	webSSLExpect(t, h)
}

func TestWebSSLLegacyEcdsaReadsAsP256(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa"}, nil)
	h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "ecdsa-256"})
	webSSLExpect(t, h)
}

func TestWebSSLReissueIgnoredWhenLetsEncryptWasOff(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "no", LE: "no"}, nil)
	h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "ecdsa-384", Reissue: true})
	// One Let's Encrypt command only: the add, not a re-issue on top.
	webSSLDone(t, h, `v-add-letsencrypt-domain "admin" "ssl.example" "a.ssl.example,b.ssl.example" "" "ecdsa-384"`)
}

func TestWebSSLReplaceCustomCertificate(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, nil)
	crlf := strings.ReplaceAll("NEWCRT\nline2", "\n", "\r\n")
	if code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: crlf, Key: webSSLKey, CA: webSSLCA}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	sn := spy.snaps[0]
	webSSLDone(t, h, fmt.Sprintf(`v-change-web-domain-sslcert "admin" "ssl.example" %q "no"`, sn.Dir))
	if sn.DirMode != 0o700 {
		t.Errorf("dir mode %v", sn.DirMode)
	}
	// PHP's extra trailing newline on the replace path.
	want := map[string]string{
		"ssl.example.crt": "NEWCRT\nline2\n",
		"ssl.example.key": webSSLKey + "\n",
		"ssl.example.ca":  webSSLCA + "\n",
	}
	if len(sn.Files) != 3 {
		t.Errorf("files = %v", sn.Files)
	}
	for name, w := range want {
		if sn.Files[name] != w {
			t.Errorf("%s = %q, want %q", name, sn.Files[name], w)
		}
		if sn.Modes[name] != 0o600 {
			t.Errorf("%s mode %v", name, sn.Modes[name])
		}
	}
	webSSLNoLeftover(t, sn.Dir)
}

func TestWebSSLReplaceWithoutCA(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, nil)
	h.webSSLPut(WebSSLRequest{SSL: true, Crt: "NEWCRT", Key: webSSLKey})
	if _, ok := spy.snaps[0].Files["ssl.example.ca"]; ok || len(spy.snaps[0].Files) != 2 {
		t.Errorf("files = %v", spy.snaps[0].Files)
	}
}

func TestWebSSLReplaceSkippedWhenSameAfterNormalising(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, nil)
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	if code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: crlf(webSSLCrt), Key: crlf(webSSLKey), CA: crlf(webSSLCA)}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebSSLReplaceFailureCleansUpAndHidesKey(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, map[string]int{"v-change-web-domain-sslcert": 3})
	var raw map[string]any
	code := h.call("PUT", "/api/v1/web/ssl.example/ssl", WebSSLRequest{SSL: true, Crt: "NEWCRT", Key: webSSLKey}, &raw)
	if code != 404 {
		t.Fatalf("got %d %v", code, raw)
	}
	webSSLNoLeftover(t, spy.snaps[0].Dir)
	if b, _ := json.Marshal(raw); strings.Contains(string(b), "SECRETKEYBYTES") {
		t.Errorf("key in reply: %s", b)
	}
	// Stop at the first error: no restarts.
	webSSLExpect(t, h, fmt.Sprintf(`v-change-web-domain-sslcert "admin" "ssl.example" %q "no"`, spy.snaps[0].Dir))
}

func TestWebSSLReplaceBlankKey(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, nil)
	code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: "NEWCRT", Key: " "})
	if code != 400 || !reflect.DeepEqual(r.Error.Args, []string{"ssl key"}) || len(spy.snaps) != 0 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebSSLLetsEncryptOffNeedsCertificate(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256"}, nil)
	code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: " ", Key: ""})
	if code != 400 || r.Error.Message != `Field "%s" can not be blank.` || !reflect.DeepEqual(r.Error.Args, []string{"ssl certificate", "ssl key"}) {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebSSLLetsEncryptOffKeepsSSL(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256", Home: "single"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: webSSLCrt, Key: webSSLKey, CA: webSSLCA}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	sn := spy.snaps[0]
	webSSLDone(t, h, `v-delete-letsencrypt-domain "admin" "ssl.example" ""`,
		fmt.Sprintf(`v-add-web-domain-ssl "admin" "ssl.example" %q "single" "no"`, sn.Dir))
	if sn.Files["ssl.example.crt"] != webSSLCrt+"\n" || sn.DirMode != 0o700 {
		t.Errorf("snap = %+v", sn)
	}
	webSSLNoLeftover(t, sn.Dir)
}

func TestWebSSLOffOnLetsEncryptDomain(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	// No v-delete-web-domain-ssl: deleting the Let's Encrypt certificate removes SSL.
	webSSLDone(t, h, `v-delete-letsencrypt-domain "admin" "ssl.example" ""`)
}

func TestWebSSLOffOnCustomDomain(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{Crt: webSSLCrt, Key: webSSLKey}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLDone(t, h, `v-delete-web-domain-ssl "admin" "ssl.example" "no"`)
}

func TestWebSSLAlreadyOffRunsNothing(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "no", LE: "no"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{ForceSSL: true, HSTS: true, LetsEncrypt: true}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebSSLLetsEncryptOn(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "no", LE: "no"}, nil)
	if code, r := h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "weird"}); code != 200 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLDone(t, h, `v-add-letsencrypt-domain "admin" "ssl.example" "a.ssl.example,b.ssl.example" "" "ecdsa-256"`)
}

func TestWebSSLCustomAddUsesStoredSSLHome(t *testing.T) {
	for stored, want := range map[string]string{"single": "single", "same": "same", "-": "same"} {
		h, spy := webSSLHarness(t, webSSLState{SSL: "no", LE: "no", Home: stored}, nil)
		if code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: webSSLCrt + "\r\n", Key: webSSLKey, CA: webSSLCA}); code != 200 {
			t.Fatalf("got %d %+v", code, r)
		}
		sn := spy.snaps[0]
		webSSLDone(t, h, fmt.Sprintf(`v-add-web-domain-ssl "admin" "ssl.example" %q %q "no"`, sn.Dir, want))
		if sn.Files["ssl.example.crt"] != webSSLCrt+"\n" || sn.Modes["ssl.example.key"] != 0o600 || sn.DirMode != 0o700 {
			t.Errorf("snap = %+v", sn)
		}
		webSSLNoLeftover(t, sn.Dir)
	}
}

func TestWebSSLCustomAddBlank(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "no", LE: "no"}, nil)
	code, r := h.webSSLPut(WebSSLRequest{SSL: true, Crt: webSSLCrt})
	if code != 400 || !reflect.DeepEqual(r.Error.Args, []string{"ssl key"}) || len(spy.snaps) != 0 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebSSLCustomAddFailureCleansUpAndHidesKey(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "no", LE: "no"}, map[string]int{"v-add-web-domain-ssl": 2})
	var raw map[string]any
	if code := h.call("PUT", "/api/v1/web/ssl.example/ssl", WebSSLRequest{SSL: true, Crt: webSSLCrt, Key: webSSLKey}, &raw); code != 400 {
		t.Fatalf("got %d %v", code, raw)
	}
	webSSLNoLeftover(t, spy.snaps[0].Dir)
	if b, _ := json.Marshal(raw); strings.Contains(string(b), "SECRETKEYBYTES") {
		t.Errorf("key in reply: %s", b)
	}
	for _, c := range h.webSSLLog() {
		if strings.Contains(c, "SECRETKEYBYTES") {
			t.Errorf("key in call: %s", c)
		}
	}
}

func TestWebSSLLetsEncryptOffFailureStopsBeforeAdd(t *testing.T) {
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Algo: "ecdsa-256"}, map[string]int{"v-delete-letsencrypt-domain": 3})
	if code, _ := h.webSSLPut(WebSSLRequest{SSL: true, Crt: webSSLCrt, Key: webSSLKey}); code != 404 {
		t.Fatalf("got %d", code)
	}
	webSSLExpect(t, h, `v-delete-letsencrypt-domain "admin" "ssl.example" ""`)
	if len(spy.snaps) != 0 {
		t.Error("add ran")
	}
}

func TestWebSSLForceAndHSTS(t *testing.T) {
	on := func(f, hs string) webSSLState {
		return webSSLState{SSL: "yes", LE: "no", Force: f, HSTS: hs}
	}
	same := WebSSLRequest{SSL: true, Crt: webSSLCrt, Key: webSSLKey, CA: webSSLCA}
	with := func(f, hs bool) WebSSLRequest { r := same; r.ForceSSL, r.HSTS = f, hs; return r }

	// on to on: nothing.
	h, _ := webSSLHarness(t, on("yes", "yes"), nil)
	h.webSSLPut(with(true, true))
	webSSLExpect(t, h)

	// off to on.
	h, _ = webSSLHarness(t, on("no", "no"), nil)
	h.webSSLPut(with(true, true))
	webSSLDone(t, h, `v-add-web-domain-ssl-force "admin" "ssl.example"`, `v-add-web-domain-ssl-hsts "admin" "ssl.example"`)

	// on to off.
	h, _ = webSSLHarness(t, on("yes", "yes"), nil)
	h.webSSLPut(with(false, false))
	webSSLDone(t, h, `v-delete-web-domain-ssl-force "admin" "ssl.example"`, `v-delete-web-domain-ssl-hsts "admin" "ssl.example"`)

	// off to off.
	h, _ = webSSLHarness(t, on("no", "no"), nil)
	h.webSSLPut(with(false, false))
	webSSLExpect(t, h)

	// after SSL off: no stray delete, on a custom and on a Let's Encrypt domain.
	h, _ = webSSLHarness(t, on("yes", "yes"), nil)
	h.webSSLPut(WebSSLRequest{})
	webSSLDone(t, h, `v-delete-web-domain-ssl "admin" "ssl.example" "no"`)
	h, _ = webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Force: "yes", HSTS: "yes", Algo: "rsa"}, nil)
	h.webSSLPut(WebSSLRequest{ForceSSL: true, HSTS: true})
	webSSLDone(t, h, `v-delete-letsencrypt-domain "admin" "ssl.example" ""`)
}

func TestWebSSLForceAfterLetsEncryptAdd(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "no", LE: "no"}, nil)
	h.webSSLPut(WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "rsa", ForceSSL: true, HSTS: true})
	webSSLDone(t, h, `v-add-letsencrypt-domain "admin" "ssl.example" "a.ssl.example,b.ssl.example" "" "rsa"`,
		`v-add-web-domain-ssl-force "admin" "ssl.example"`, `v-add-web-domain-ssl-hsts "admin" "ssl.example"`)
}

func TestWebSSLForceAfterLetsEncryptTurnedOffWithSSLKept(t *testing.T) {
	// The delete removes the certificate, so force-HTTPS counts as off and is added back.
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "yes", Force: "yes", Algo: "rsa"}, nil)
	h.webSSLPut(WebSSLRequest{SSL: true, Crt: webSSLCrt, Key: webSSLKey, ForceSSL: true})
	webSSLDone(t, h, `v-delete-letsencrypt-domain "admin" "ssl.example" ""`,
		fmt.Sprintf(`v-add-web-domain-ssl "admin" "ssl.example" %q "same" "no"`, spy.snaps[0].Dir),
		`v-add-web-domain-ssl-force "admin" "ssl.example"`)
}

func TestWebSSLForceFailureStopsRestarts(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, map[string]int{"v-add-web-domain-ssl-force": 3})
	if code, _ := h.webSSLPut(WebSSLRequest{SSL: true, Crt: webSSLCrt, Key: webSSLKey, CA: webSSLCA, ForceSSL: true, HSTS: true}); code != 404 {
		t.Fatalf("got %d", code)
	}
	webSSLExpect(t, h, `v-add-web-domain-ssl-force "admin" "ssl.example"`)
}

func TestWebSSLLetsEncryptCommandsGetLongerTimeout(t *testing.T) {
	cases := []struct {
		st   webSSLState
		req  WebSSLRequest
		want string
	}{
		{webSSLState{SSL: "no", LE: "no"}, WebSSLRequest{SSL: true, LetsEncrypt: true}, "v-add-letsencrypt-domain"},
		{webSSLState{SSL: "yes", LE: "yes", Algo: "rsa"}, WebSSLRequest{SSL: true, LetsEncrypt: true, KeyAlgo: "rsa", Reissue: true}, "v-add-letsencrypt-domain"},
		{webSSLState{SSL: "yes", LE: "yes", Algo: "rsa"}, WebSSLRequest{}, "v-delete-letsencrypt-domain"},
	}
	for _, c := range cases {
		h, spy := webSSLHarness(t, c.st, nil)
		if code, r := h.webSSLPut(c.req); code != 200 {
			t.Fatalf("got %d %+v", code, r)
		}
		if got := spy.timeouts[c.want]; got != letsEncryptTimeout {
			t.Errorf("%s timeout = %v, want %v", c.want, got, letsEncryptTimeout)
		}
		for cmd, got := range spy.timeouts {
			if !strings.Contains(cmd, "letsencrypt") && got != 0 {
				t.Errorf("%s timeout = %v, want the default", cmd, got)
			}
		}
	}
	h, spy := webSSLHarness(t, webSSLState{SSL: "yes", LE: "no"}, nil)
	h.webSSLPut(WebSSLRequest{})
	if got, ok := spy.timeouts["v-delete-web-domain-ssl"]; !ok || got != 0 {
		t.Errorf("v-delete-web-domain-ssl timeout = %v (ran %v)", got, ok)
	}
}

func TestWebSSLUnknownDomain(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r APIError
	if code := h.call("PUT", "/api/v1/web/missing.example/ssl", WebSSLRequest{SSL: true, Crt: "a", Key: "b"}, &r); code != 404 {
		t.Fatalf("got %d %+v", code, r)
	}
	if code := h.call("PUT", "/api/v1/web/-x/ssl", WebSSLRequest{}, &r); code != 400 {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebSSLBadBody(t *testing.T) {
	h, _ := webSSLHarness(t, webSSLState{SSL: "no"}, nil)
	var r APIError
	if code := h.call("PUT", "/api/v1/web/ssl.example/ssl", "nope", &r); code != 400 {
		t.Fatalf("got %d", code)
	}
}

// --- generator ---

func webSSLGenReq() WebGenerateSSLRequest {
	return WebGenerateSSLRequest{Domain: "gen.example", Aliases: "www.gen.example\nmail.gen.example", Email: "a@gen.example",
		Country: "US", State: "Ohio", Locality: "Akron", Org: "Gen Inc", KeyAlgo: "ecdsa-256"}
}

func TestWebGenerateSSL(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var out map[string]any
	if code := h.call("POST", "/api/v1/web/generate-ssl", webSSLGenReq(), &out); code != 200 {
		t.Fatalf("got %d %v", code, out)
	}
	webSSLExpect(t, h, `v-generate-ssl-cert "gen.example" "a@gen.example" "US" "Ohio" "Akron" "Gen Inc" "IT" "www.gen.example\nmail.gen.example" "json" "ecdsa-256"`)
	if _, has := out["DIR"]; has || out["dir"] != nil {
		t.Errorf("reply has DIR: %v", out)
	}
	if len(out) != 3 || !strings.Contains(out["crt"].(string), "BEGIN CERTIFICATE") || !strings.Contains(out["key"].(string), "PRIVATE KEY") ||
		!strings.Contains(out["csr"].(string), "REQUEST") {
		t.Errorf("reply = %v", out)
	}
}

func TestWebGenerateSSLNoAliasesNoEmail(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	r := webSSLGenReq()
	r.Aliases, r.Email, r.KeyAlgo = "", "", "rsa"
	if code := h.call("POST", "/api/v1/web/generate-ssl", r, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	webSSLExpect(t, h, `v-generate-ssl-cert "gen.example" "" "US" "Ohio" "Akron" "Gen Inc" "IT" "" "json" "rsa"`)
}

func TestWebGenerateSSLAlgoErrorWinsOverBlanks(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r APIError
	if code := h.call("POST", "/api/v1/web/generate-ssl", WebGenerateSSLRequest{KeyAlgo: "dsa"}, &r); code != 400 ||
		r.Error.Message != "Invalid key algorithm selected." {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebGenerateSSLBlanks(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r APIError
	if code := h.call("POST", "/api/v1/web/generate-ssl", WebGenerateSSLRequest{KeyAlgo: "rsa", Aliases: "x"}, &r); code != 400 ||
		r.Error.Message != `Field "%s" can not be blank.` ||
		!reflect.DeepEqual(r.Error.Args, []string{"Domain", "Country", "State", "City", "Organization"}) {
		t.Fatalf("got %d %+v", code, r)
	}
	webSSLExpect(t, h)
}

func TestWebGenerateSSLUnsafeValues(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for name, c := range map[string]struct {
		mod   func(*WebGenerateSSLRequest)
		field string
	}{
		"domain dash":   {func(r *WebGenerateSSLRequest) { r.Domain = "-x" }, "Domain"},
		"domain space":  {func(r *WebGenerateSSLRequest) { r.Domain = "a b" }, "Domain"},
		"alias dash":    {func(r *WebGenerateSSLRequest) { r.Aliases = "-x.example" }, "Aliases"},
		"email dash":    {func(r *WebGenerateSSLRequest) { r.Email = "-x" }, "Email"},
		"org newline":   {func(r *WebGenerateSSLRequest) { r.Org = "a\nb" }, "Organization"},
		"country dash":  {func(r *WebGenerateSSLRequest) { r.Country = "-U" }, "Country"},
		"city control":  {func(r *WebGenerateSSLRequest) { r.Locality = "a\x00b" }, "City"},
		"state newline": {func(r *WebGenerateSSLRequest) { r.State = "a\nb" }, "State"},
	} {
		r := webSSLGenReq()
		c.mod(&r)
		var e APIError
		// The UI marks the field named by Args[0]; Args[1] is the refused value.
		if code := h.call("POST", "/api/v1/web/generate-ssl", r, &e); code != 400 || e.Error.Code != "invalid" ||
			e.Error.Message != "invalid %s format :: %s" || len(e.Error.Args) != 2 || e.Error.Args[0] != c.field {
			t.Errorf("%s: got %d %+v", name, code, e)
		}
	}
	webSSLExpect(t, h)
}

func TestWebGenerateSSLCommandFailure(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-generate-ssl-cert": {{Args: []string{"*", "*", "*", "*", "*", "*", "*", "*", "*", "*"}, Stdout: "Error: SECRETKEYBYTES nope\n", Exit: 2}},
	}})
	h.loginAdmin()
	var e APIError
	if code := h.call("POST", "/api/v1/web/generate-ssl", webSSLGenReq(), &e); code != 400 {
		t.Fatalf("got %d %+v", code, e)
	}
}

func TestWebSSLReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	for _, rt := range []struct {
		method, path string
		body         any
	}{
		{"PUT", "/api/v1/web/example.com/ssl", WebSSLRequest{}},
		{"POST", "/api/v1/web/generate-ssl", webSSLGenReq()},
	} {
		var r statusReply
		if code := h.call(rt.method, rt.path, rt.body, &r); code != 403 || r.Error.Code != "read_only" {
			t.Fatalf("%s %s: %d %+v", rt.method, rt.path, code, r)
		}
	}
	webSSLExpect(t, h)
}
