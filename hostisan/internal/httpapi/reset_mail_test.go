package httpapi

import (
	"io"
	"net/http"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// /reset/mail/ (parity RM1-RM11, decisions 24-26, ruling R6). The dev fixtures:
// v-search-domain-owner bakery.example mail -> bob; v-get-mail-account-value bob
// bakery.example info md5 -> {SHA512-CRYPT} of "devpass" (openssl passwd -6 -salt devsalt
// devpass); v-list-sys-ips: 10.0.0.5 (NAT 198.51.100.7), 203.0.113.10, 203.0.113.11.

const resetMailText = "text/plain; charset=utf-8"

type resetMailEnv struct {
	*legacyEnv
	now time.Time
}

func newResetMailEnv(t *testing.T, fixtures map[string][]fixtureEntry) *resetMailEnv {
	t.Helper()
	r := &resetMailEnv{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	r.legacyEnv = newLegacyEnv(t, nil, fixtures, func(d *Deps) { d.Now = func() time.Time { return r.now } })
	return r
}

// post sends the Roundcube driver's form directly (RealIP 127.0.0.1).
func (r *resetMailEnv) post(headers map[string]string, kv ...string) legacyReply {
	r.t.Helper()
	return r.send(r.h.client, r.h.srv.URL, "POST", "/reset/mail/", "application/x-www-form-urlencoded", form(kv...), headers)
}

// postFrom sends it over the proxy socket; headers are what hestia-nginx sets.
func (r *resetMailEnv) postFrom(headers map[string]string, kv ...string) legacyReply {
	r.t.Helper()
	return r.send(r.sock, "http://hostisan", "POST", "/reset/mail/", "application/x-www-form-urlencoded", form(kv...), headers)
}

func (r *resetMailEnv) want(name string, got legacyReply, body string) {
	r.t.Helper()
	if got.Status != 200 || got.Type != resetMailText || got.Body != body || len(got.ExitCode) != 0 {
		r.t.Errorf("%s: got %d %q %q; want 200 %q", name, got.Status, got.Type, got.Body, body)
	}
}

var resetMailNoFields = "error email address not provided"

func TestResetMailSource(t *testing.T) {
	e := newResetMailEnv(t, nil)
	from := func(ip string, extra ...string) map[string]string {
		// hestia-nginx always sends the address the connection reached (Plan 6, P1).
		h := map[string]string{"X-Real-IP": ip, "X-Hostisan-Server-Addr": "127.0.0.1"}
		for i := 0; i+1 < len(extra); i += 2 {
			h[extra[i]] = extra[i+1]
		}
		return h
	}
	// Proxied without X-Hostisan-Server-Addr: nginx isn't passing the client's headers
	// through, so the no-proxy rule can't be checked; refuse before anything runs.
	e.want("no server address", e.postFrom(map[string]string{"X-Real-IP": "127.0.0.1"}), "")
	e.wantCalls("no server address")
	e.want("foreign IP", e.postFrom(from("192.0.2.5")), "")
	e.wantCalls("foreign IP", "v-list-sys-ips json")
	e.want("::1", e.postFrom(from("::1")), "")
	e.wantCalls("::1", "v-list-sys-ips json")

	e.want("127.0.0.1 direct", e.post(nil), resetMailNoFields)
	e.want("127.0.0.1 proxied", e.postFrom(from("127.0.0.1")), resetMailNoFields)
	e.wantCalls("loopback")
	e.want("NAT IP", e.postFrom(from("198.51.100.7")), resetMailNoFields)
	e.want("system IP", e.postFrom(from("203.0.113.10")), resetMailNoFields)
	e.wantCalls("system IPs", "v-list-sys-ips json", "v-list-sys-ips json")
	e.want("index.php", e.send(e.h.client, e.h.srv.URL, "POST", "/reset/mail/index.php", "", "", nil), resetMailNoFields)
	e.want("GET", e.send(e.h.client, e.h.srv.URL, "GET", "/reset/mail/", "", "", nil), resetMailNoFields)

	// The caller's own proxy headers: passed through by nginx, or sent directly.
	e.want("proxied X-Real-IP", e.postFrom(from("127.0.0.1", "X-Hostisan-Client-Real-IP", "10.9.9.9")), "")
	e.want("proxied X-Forwarded-For", e.postFrom(from("127.0.0.1", "X-Hostisan-Client-Forwarded-For", "10.9.9.9")), "")
	e.want("direct X-Forwarded-For", e.post(map[string]string{"X-Forwarded-For": "10.9.9.9"}), "")
	e.want("direct X-Real-IP", e.post(map[string]string{"X-Real-IP": "127.0.0.1"}), "")
	e.wantCalls("proxy headers")

	// SERVER_ADDR: behind nginx it comes from X-Hostisan-Server-Addr, trusted only when it
	// names one of this machine's addresses.
	old := resetMailInterfaceAddrs
	t.Cleanup(func() { resetMailInterfaceAddrs = old })
	resetMailInterfaceAddrs = func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("192.0.2.77")} }
	e.want("server address", e.postFrom(from("192.0.2.77", "X-Hostisan-Server-Addr", "192.0.2.77")), resetMailNoFields)
	e.want("spoofed server address", e.postFrom(from("192.0.2.5", "X-Hostisan-Server-Addr", "192.0.2.5")), "")
	if e.h.fixture.Calls() == nil {
		t.Fatal("no calls")
	}
	e.reset()

	for _, r := range []legacyReply{e.postFrom(from("192.0.2.5")), e.post(nil)} {
		if r.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("headers = %v", r.Header)
		}
	}
}

func TestResetMailFields(t *testing.T) {
	e := newResetMailEnv(t, nil)
	e.want("none", e.post(nil), "error email address not provided")
	e.want("email 0", e.post(nil, "email", "0", "password", "x", "new", "y"), "error email address not provided")
	e.want("no old", e.post(nil, "email", "info@bakery.example", "new", "y"), "error old password provided")
	e.want("old 0", e.post(nil, "email", "info@bakery.example", "password", "0", "new", "y"), "error old password provided")
	e.want("no new", e.post(nil, "email", "info@bakery.example", "password", "x"), "error new password not provided")
	e.want("email array", e.send(e.h.client, e.h.srv.URL, "POST", "/reset/mail/", "application/x-www-form-urlencoded",
		"email[]=info@bakery.example&password=x&new=y", nil), "error email address not provided")
	// Fields come from a POSTed form only, never JSON.
	e.want("json", e.send(e.h.client, e.h.srv.URL, "POST", "/reset/mail/", "application/json",
		`{"email":"info@bakery.example","password":"devpass","new":"x"}`, nil), "error email address not provided")
	e.wantCalls("fields")
}

var resetMailFP = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestResetMailFlow(t *testing.T) {
	const newPW = "Brand-N3w-pass"
	e := newResetMailEnv(t, nil)
	e.want("ok", e.post(nil, "email", "info@bakery.example", "password", "devpass", "new", newPW), "==ok==")
	e.wantCalls("ok",
		"v-search-domain-owner bakery.example mail",
		"v-get-mail-account-value bob bakery.example info md5",
		"v-change-mail-account-password bob bakery.example info <tmp>")
	if files := e.tmpFiles(); len(files) != 0 {
		t.Errorf("temp files left: %v", files)
	}
	for _, c := range e.h.fixture.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), newPW) || strings.Contains(strings.Join(c.Args, " "), "devpass") {
			t.Errorf("password in argv: %v", c)
		}
	}

	e.want("second @", e.post(nil, "email", "a@b@bakery.example", "password", "devpass", "new", newPW), "error domain owner not found")
	e.wantCalls("second @", "v-search-domain-owner b mail")
	e.want("unknown domain", e.post(nil, "email", "info@nowhere.example", "password", "devpass", "new", newPW), "error domain owner not found")
	e.wantCalls("unknown domain", "v-search-domain-owner nowhere.example mail")
	for _, email := range []string{"info", "info@", "info@-x.com", "info@a b", "info@a;b"} {
		e.want("bad domain "+email, e.post(nil, "email", email, "password", "devpass", "new", newPW), "error domain owner not found")
	}
	e.wantCalls("bad domains")
	e.want("no hash", e.post(nil, "email", "nobody@bakery.example", "password", "devpass", "new", newPW), "error unable to get current account password hash")
	e.wantCalls("no hash", "v-search-domain-owner bakery.example mail", "v-get-mail-account-value bob bakery.example nobody md5")
	e.want("bad account", e.post(nil, "email", "-x@bakery.example", "password", "devpass", "new", newPW), "error unable to get current account password hash")
	e.wantCalls("bad account", "v-search-domain-owner bakery.example mail")

	e5 := newResetMailEnv(t, map[string][]fixtureEntry{
		"v-change-mail-account-password": {{Args: []string{"*", "*", "*", "*"}, Stdout: "Error: invalid password format\n", Exit: 5}},
	})
	e5.want("change fails", e5.post(nil, "email", "info@bakery.example", "password", "devpass", "new", newPW),
		"error v-change-mail-account-password returned non-zero: 5")
	if files := e5.tmpFiles(); len(files) != 0 {
		t.Errorf("temp files left: %v", files)
	}
}

func TestResetMailWrongPassword(t *testing.T) {
	e := newResetMailEnv(t, nil)
	r := e.post(map[string]string{"User-Agent": `Roundcube "Webmail" | 1.6`}, "email", "info@bakery.example", "password", "wrong", "new", "x")
	e.want("wrong", r, "error old password does not match")
	calls := e.calls()
	want := []string{"v-search-domain-owner bakery.example mail", "v-get-mail-account-value bob bakery.example info md5"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[:2], want) {
		t.Fatalf("calls = %q", calls)
	}
	log := e.h.fixture.Calls()[len(e.h.fixture.Calls())-1]
	if log.Cmd != "v-log-user-login" || len(log.Args) != 7 || log.Args[0] != "bob" || log.Args[1] != "127.0.0.1" || log.Args[2] != "failed" ||
		!resetMailFP.MatchString(log.Args[3]) || log.Args[4] != "Roundcube Webmail  1.6" || log.Args[5] != "yes" ||
		log.Args[6] != "mail password change info@bakery.example" {
		t.Errorf("log = %q", log.Args)
	}
}

func TestResetMailThrottle(t *testing.T) {
	e := newResetMailEnv(t, nil)
	lookups := []string{"v-search-domain-owner bakery.example mail", "v-get-mail-account-value bob bakery.example info md5"}
	for i := 0; i < 5; i++ {
		e.now = e.now.Add(time.Minute)
		e.want("wrong", e.post(nil, "email", "info@bakery.example", "password", "wrong", "new", "x"), "error old password does not match")
		if c := e.calls(); len(c) != 3 || !reflect.DeepEqual(c[:2], lookups) || !strings.HasPrefix(c[2], "v-log-user-login bob ") {
			t.Fatalf("attempt %d calls = %q", i+1, c)
		}
		e.reset()
	}
	// Over the limit: the same reply, even for the right password (and another case of
	// the address), only the failed-login line runs.
	for _, email := range []string{"info@bakery.example", "INFO@Bakery.Example"} {
		e.want("over limit", e.post(nil, "email", email, "password", "devpass", "new", "x"), "error old password does not match")
		if c := e.calls(); len(c) != 1 || !strings.HasPrefix(c[0], "v-log-user-login bob 127.0.0.1 failed ") {
			t.Fatalf("over limit calls = %q", c)
		}
		e.reset()
	}
	// Another account isn't affected.
	e.want("other account", e.post(nil, "email", "nobody@bakery.example", "password", "devpass", "new", "x"), "error unable to get current account password hash")
	e.reset()
	// 15 minutes after the first failure, one attempt frees up.
	e.now = e.now.Add(11 * time.Minute)
	e.want("after window", e.post(nil, "email", "info@bakery.example", "password", "devpass", "new", "x"), "==ok==")
	e.reset()
	// A success clears the account's failures.
	for i := 0; i < 2; i++ {
		e.want("after success", e.post(nil, "email", "info@bakery.example", "password", "wrong", "new", "x"), "error old password does not match")
		if c := e.calls(); len(c) != 3 {
			t.Errorf("after success, attempt %d calls = %q", i+1, c)
		}
		e.reset()
	}
}

// The throttle holds only accounts whose hash was read (they exist), and drops them
// once their failures age out.
func TestResetMailThrottleSweep(t *testing.T) {
	th := newResetMailThrottle()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		th.fail("info@bakery.example", "bob", now)
	}
	if owner, over := th.over("info@bakery.example", now); !over || owner != "bob" {
		t.Fatalf("over = %q %v", owner, over)
	}
	if _, over := th.over("sales@bakery.example", now); over {
		t.Fatal("unknown account over the limit")
	}
	th.fail("sales@bakery.example", "bob", now)
	th.clear("sales@bakery.example")
	if _, over := th.over("info@bakery.example", now.Add(resetMailFailWindow)); over {
		t.Fatal("still over after the window")
	}
	th.fail("other@bakery.example", "bob", now.Add(resetMailFailWindow+2*time.Minute))
	if n := len(th.fails); n != 1 {
		t.Errorf("throttle holds %d accounts after the sweep, want 1", n)
	}
}

// Parallel wrong passwords for one account: at most the limit is ever checked
// (review I1).
func TestResetMailThrottleParallel(t *testing.T) {
	e := newResetMailEnv(t, nil)
	var verified atomic.Int32
	old := resetMailVerify
	t.Cleanup(func() { resetMailVerify = old })
	resetMailVerify = func(pw, hash string) bool {
		verified.Add(1)
		time.Sleep(20 * time.Millisecond) // widen the window a racing check would use
		return old(pw, hash)
	}
	var wg sync.WaitGroup
	bodies := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := http.Post(e.h.srv.URL+"/reset/mail/", "application/x-www-form-urlencoded",
				strings.NewReader(form("email", "info@bakery.example", "password", "wrong", "new", "x")))
			if err != nil {
				bodies <- err.Error()
				return
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			bodies <- string(b)
		}()
	}
	wg.Wait()
	close(bodies)
	for b := range bodies {
		if b != "error old password does not match" {
			t.Errorf("reply %q", b)
		}
	}
	if n := verified.Load(); n > resetMailFailLimit {
		t.Errorf("%d passwords checked, limit %d", n, resetMailFailLimit)
	}
}

// Over-long passwords (review I2): an old one gets the mismatch reply without any
// lookup or hashing; a new one is refused after the old one checks out.
func TestResetMailLongPasswords(t *testing.T) {
	e := newResetMailEnv(t, nil)
	var verified atomic.Int32
	old := resetMailVerify
	t.Cleanup(func() { resetMailVerify = old })
	resetMailVerify = func(pw, hash string) bool { verified.Add(1); return old(pw, hash) }

	e.want("long old", e.post(nil, "email", "info@bakery.example", "password", strings.Repeat("p", 1025), "new", "x"), "error old password does not match")
	if verified.Load() != 0 {
		t.Error("an over-long old password was hashed")
	}
	e.wantCalls("long old")
	e.want("long new", e.post(nil, "email", "info@bakery.example", "password", "devpass", "new", strings.Repeat("n", 1025)),
		"error v-change-mail-account-password returned non-zero: 2")
	e.wantCalls("long new", "v-search-domain-owner bakery.example mail", "v-get-mail-account-value bob bakery.example info md5")
	e.want("1024 new", e.post(nil, "email", "info@bakery.example", "password", "devpass", "new", strings.Repeat("n", 1024)), "==ok==")
}

// The throttle key is the account at the domain's ASCII (punycode) form, lowercased:
// spelling the domain another way doesn't buy more attempts.
func TestResetMailThrottleIDN(t *testing.T) {
	const ace = "xn--bcher-kva.example"
	e := newResetMailEnv(t, map[string][]fixtureEntry{
		"v-search-domain-owner":    {{Args: []string{"*", "mail"}, Stdout: "bob\n"}},
		"v-get-mail-account-value": {{Args: []string{"bob", "*", "info", "md5"}, Stdout: "{SHA512-CRYPT}$6$devsalt$6h3F4aYWmAGJQrxirhIyXRggDQbnryF92SIM6pa8nAJjPCllXvq8NolAzgLrE9H/aDkTXdb4DbgkBwGKCA.sD/\n"}},
	})
	for i := 0; i < 5; i++ {
		e.post(nil, "email", "info@"+ace, "password", "wrong", "new", "x")
	}
	e.reset()
	for _, email := range []string{"info@bücher.example", "Info@BÜCHER.example.", "INFO@XN--BCHER-KVA.EXAMPLE"} {
		e.want(email, e.post(nil, "email", email, "password", "devpass", "new", "x"), "error old password does not match")
		if c := e.calls(); len(c) != 1 || !strings.HasPrefix(c[0], "v-log-user-login bob ") {
			t.Errorf("%s: calls = %q", email, c)
		}
		e.reset()
	}
	for in, want := range map[string]string{"bücher": "xn--bcher-kva", "münchen": "xn--mnchen-3ya", "example": "example",
		"bücher.example": "xn--bcher-kva.example", "MÜNCHEN.de.": "xn--mnchen-3ya.de"} {
		if got := resetMailASCIIDomain(in); got != want {
			t.Errorf("resetMailASCIIDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
