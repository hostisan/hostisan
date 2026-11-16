package httpapi

// /reset/mail/ (Slice D): Hestia's web/reset/mail/index.php, the password change the
// Roundcube and SnappyMail plugins call on this server (parity RM1-RM11, decisions
// 24-26, ruling R6). No session, no CSRF: only this machine may call it, and the old
// password is the authentication. Replies are Hestia's plain-text lines, always 200.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailpw"
	"github.com/hostisan/hostisan/internal/secret"
	"github.com/hostisan/hostisan/internal/session"
)

const (
	resetMailMaxBody     = 64 << 10
	resetMailFailLimit   = 5 // failed attempts per account…
	resetMailFailWindow  = 15 * time.Minute
	resetMailContentType = "text/plain; charset=utf-8"

	resetMailMsgNoEmail   = "error email address not provided"
	resetMailMsgNoOld     = "error old password provided" // sic
	resetMailMsgNoNew     = "error new password not provided"
	resetMailMsgNoOwner   = "error domain owner not found"
	resetMailMsgNoHash    = "error unable to get current account password hash"
	resetMailMsgMismatch  = "error old password does not match"
	resetMailMsgOK        = "==ok=="
	resetMailMsgChangeErr = "error v-change-mail-account-password returned non-zero: "

	// resetMailMaxPassword caps both passwords (review I2): longer ones are never hashed.
	resetMailMaxPassword = mailpw.MaxPassword
	// resetMailExitInvalid is the core's E_INVALID, the code an unusable new password
	// gets without the change command running.
	resetMailExitInvalid = 2

	// What hestia-nginx passes on /reset/mail/ (Plan 6, P1): the caller's own proxy
	// headers and the address the connection reached.
	resetMailHdrRealIP     = "X-Hostisan-Client-Real-IP"
	resetMailHdrForwarded  = "X-Hostisan-Client-Forwarded-For"
	resetMailHdrServerAddr = "X-Hostisan-Server-Addr"
)

// resetMailVerify checks a password against the stored hash; a variable for tests.
var resetMailVerify = mailpw.Verify

// resetMailInterfaceAddrs lists this machine's addresses; X-Hostisan-Server-Addr is
// only believed when it names one of them. A variable for tests.
var resetMailInterfaceAddrs = func() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Addr().Unmap())
		}
	}
	return out
}

// resetMailRoutes mounts /reset/mail/, the webmail password change (Slice D, parity decisions 24-26).
func (s *server) resetMailRoutes(mux *http.ServeMux) {
	th := newResetMailThrottle()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.resetMail(w, r, th) })
	mux.Handle("/reset/mail/{$}", h)
	mux.Handle("/reset/mail/index.php", h)
}

func (s *server) resetMail(w http.ResponseWriter, r *http.Request, th *resetMailThrottle) {
	legacyHeaders(w, resetMailContentType)
	reply := func(text string) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, text)
	}
	if !s.resetMailLocal(r) {
		reply("")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, resetMailMaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	post, _ := legacyPHPPostFields(r, body)
	field := func(name string) (string, bool) {
		v := post[name]
		return v.val, !v.array && !legacyPHPEmpty(v.val)
	}
	email, ok := field("email")
	if !ok {
		reply(resetMailMsgNoEmail)
		return
	}
	oldPW, ok := field("password")
	if !ok {
		reply(resetMailMsgNoOld)
		return
	}
	newPW, ok := field("new")
	if !ok {
		reply(resetMailMsgNoNew)
		return
	}
	// explode("@") then [$account, $domain]: a second "@" and what follows are dropped.
	parts := strings.Split(email, "@")
	account, domain := parts[0], ""
	if len(parts) > 1 {
		domain = parts[1]
	}
	// An over-long old password can't be right: no lookup, no hashing (review I2).
	if len(oldPW) > resetMailMaxPassword {
		reply(resetMailMsgMismatch)
		return
	}
	ctx := r.Context()
	key := strings.ToLower(account) + "@" + resetMailASCIIDomain(domain)
	// One attempt per account at a time, so parallel guesses can't all pass the limit
	// check before any failure is recorded (review I1).
	defer th.lock(key)()
	now := s.now()

	// Over the limit (ruling R6): the reply of a wrong password, logged as one, and no
	// lookup at all, so the right password can't be found by trying.
	if owner, over := th.over(key, now); over {
		s.resetMailLogFailed(r, owner, account, domain)
		reply(resetMailMsgMismatch)
		return
	}

	if !hestia.ValidDomainArg(domain) {
		reply(resetMailMsgNoOwner)
		return
	}
	res, err := s.R.Run(ctx, "v-search-domain-owner", domain, "mail")
	owner := ""
	if err == nil && res.Exit == 0 {
		if lines := legacyExecLines(res.Stdout); len(lines) > 0 {
			owner = lines[0]
		}
	}
	if legacyPHPEmpty(owner) || !hestia.ValidUsername(owner) {
		reply(resetMailMsgNoOwner)
		return
	}
	if !hestia.ValidMailAccountArg(account) {
		reply(resetMailMsgNoHash)
		return
	}
	res, err = s.R.Run(ctx, "v-get-mail-account-value", owner, domain, account, "md5")
	hash := ""
	if err == nil && res.Exit == 0 {
		if lines := legacyExecLines(res.Stdout); len(lines) > 0 {
			hash = lines[0]
		}
	}
	if legacyPHPEmpty(hash) {
		reply(resetMailMsgNoHash)
		return
	}
	if !resetMailVerify(oldPW, mailpw.StripScheme(hash)) {
		th.fail(key, owner, now)
		s.resetMailLogFailed(r, owner, account, domain)
		reply(resetMailMsgMismatch)
		return
	}

	if len(newPW) > resetMailMaxPassword {
		reply(resetMailMsgChangeErr + strconv.Itoa(resetMailExitInvalid))
		return
	}
	path, cleanup, err := secret.TempFile(s.tmpDir(), newPW)
	if err != nil {
		slog.Error("reset mail: temp file", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	res, err = s.R.Run(ctx, "v-change-mail-account-password", owner, domain, account, path)
	cleanup()
	if err != nil {
		slog.Error("reset mail: v-change-mail-account-password", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if res.Exit != 0 {
		reply(resetMailMsgChangeErr + strconv.Itoa(res.Exit))
		return
	}
	th.clear(key)
	reply(resetMailMsgOK)
}

// resetMailLocal is index.php's source check (RM3, decision 24): the caller must not
// have come through a proxy of its own, and its address must be 127.0.0.1, the address
// it connected to (SERVER_ADDR), or one of the server's IPs or their NAT addresses.
// Proxy headers are checked first: they need no command.
func (s *server) resetMailLocal(r *http.Request) bool {
	_, proxied := r.Context().Value(http.LocalAddrContextKey).(*net.UnixAddr)
	if proxied {
		// Without the server address nginx isn't passing the caller's own headers
		// through either (Plan 6, P1), so the no-proxy rule can't be checked.
		if r.Header.Get(resetMailHdrServerAddr) == "" {
			return false
		}
		if r.Header.Get(resetMailHdrRealIP) != "" || r.Header.Get(resetMailHdrForwarded) != "" {
			return false
		}
	} else if len(r.Header.Values("X-Real-IP")) > 0 || len(r.Header.Values("X-Forwarded-For")) > 0 {
		return false
	}
	ip := session.RealIP(r)
	if ip == "" {
		return false
	}
	if ip == "127.0.0.1" {
		return true
	}
	if addr := resetMailServerAddr(r, proxied); addr != "" && addr == ip {
		return true
	}
	res, err := s.R.Run(r.Context(), "v-list-sys-ips", "json")
	if err != nil || res.Exit != 0 {
		return false
	}
	var ips map[string]struct{ NAT string }
	if json.Unmarshal(res.Stdout, &ips) != nil {
		return false
	}
	for addr, v := range ips {
		if ip == resetMailCanonicalIP(addr) || (v.NAT != "" && ip == resetMailCanonicalIP(v.NAT)) {
			return true
		}
	}
	return false
}

// resetMailServerAddr is PHP's SERVER_ADDR: hestia-nginx's X-Hostisan-Server-Addr when
// proxied (believed only for one of this machine's addresses), else the local end of
// the TCP connection.
func resetMailServerAddr(r *http.Request, proxied bool) string {
	if proxied {
		a, err := netip.ParseAddr(r.Header.Get(resetMailHdrServerAddr))
		if err != nil {
			return ""
		}
		a = a.Unmap()
		for _, local := range resetMailInterfaceAddrs() {
			if local == a {
				return a.String()
			}
		}
		return ""
	}
	if tcp, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
		if a, ok := netip.AddrFromSlice(tcp.IP); ok {
			return a.Unmap().String()
		}
	}
	return ""
}

// resetMailCanonicalIP writes an address the way RealIP does, so the two compare.
func resetMailCanonicalIP(s string) string {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap().String()
	}
	return s
}

// resetMailLogFailed writes the failed-login line fail2ban watches (decision 26) for
// the account's owner. The fingerprint is random: there is no session.
func (s *server) resetMailLogFailed(r *http.Request, owner, account, domain string) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		slog.Warn("reset mail: fingerprint", "err", err)
		return
	}
	reason := auth.LogSafe("mail password change " + account + "@" + domain)
	res, err := s.R.Run(r.Context(), "v-log-user-login", owner, session.RealIP(r), "failed", hex.EncodeToString(b[:]),
		auth.LogSafe(r.UserAgent()), "yes", reason)
	if err == nil {
		err = res.Err("v-log-user-login")
	}
	if err != nil {
		slog.Warn("reset mail: failure log failed", "err", err)
	}
}

// resetMailThrottle counts wrong old passwords per mail account (ruling R6): 5 per 15
// minutes. Only accounts whose hash was read are recorded, so the map is bounded by
// the accounts that exist; entries are swept once their failures age out.
type resetMailThrottle struct {
	mu    sync.Mutex
	fails map[string]resetMailFails
	swept time.Time

	// stripes serialise attempts per account: a fixed set, so no state per caller.
	stripes [64]sync.Mutex
}

// lock holds key's stripe until the returned func is called.
func (t *resetMailThrottle) lock(key string) (unlock func()) {
	h := fnv.New32a()
	h.Write([]byte(key))
	m := &t.stripes[h.Sum32()%uint32(len(t.stripes))]
	m.Lock()
	return m.Unlock
}

type resetMailFails struct {
	owner string
	times []time.Time
}

func newResetMailThrottle() *resetMailThrottle {
	return &resetMailThrottle{fails: map[string]resetMailFails{}}
}

// recent drops failures outside the window; hold t.mu.
func (t *resetMailThrottle) recent(key string, now time.Time) resetMailFails {
	e := t.fails[key]
	kept := e.times[:0:0]
	for _, at := range e.times {
		if now.Sub(at) < resetMailFailWindow {
			kept = append(kept, at)
		}
	}
	e.times = kept
	return e
}

// sweepLocked drops, at most once a minute, the accounts with no recent failure.
func (t *resetMailThrottle) sweepLocked(now time.Time) {
	if now.Sub(t.swept) < time.Minute {
		return
	}
	t.swept = now
	for k := range t.fails {
		if len(t.recent(k, now).times) == 0 {
			delete(t.fails, k)
		}
	}
}

// over reports whether key has used up its attempts, with the owner recorded for it.
func (t *resetMailThrottle) over(key string, now time.Time) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now)
	e := t.recent(key, now)
	return e.owner, len(e.times) >= resetMailFailLimit
}

// fail records a wrong password for key.
func (t *resetMailThrottle) fail(key, owner string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now)
	e := t.recent(key, now)
	e.owner = owner
	e.times = append(e.times, now)
	t.fails[key] = e
}

// clear forgets key's failures after a successful change.
func (t *resetMailThrottle) clear(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fails, key)
}

// resetMailASCIIDomain is domain as the throttle keys it: lowercased, without a final
// ".", each non-ASCII label in its punycode ("xn--") form, so the Unicode and ASCII
// spellings of one domain share a key. (Lowercasing stands in for IDNA's full mapping.)
func resetMailASCIIDomain(domain string) string {
	labels := strings.Split(strings.TrimSuffix(strings.ToLower(domain), "."), ".")
	for i, l := range labels {
		for _, c := range l {
			if c >= 0x80 {
				labels[i] = "xn--" + resetMailPunycode([]rune(l))
				break
			}
		}
	}
	return strings.Join(labels, ".")
}

// resetMailPunycode is RFC 3492's encoder.
func resetMailPunycode(in []rune) string {
	const (
		base, tmin, tmax, skew, damp = 36, 1, 26, 38, 700
	)
	adapt := func(delta, points int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / points
		k := 0
		for delta > ((base-tmin)*tmax)/2 {
			delta /= base - tmin
			k += base
		}
		return k + (base-tmin+1)*delta/(delta+skew)
	}
	digit := func(d int) byte {
		if d < 26 {
			return byte('a' + d)
		}
		return byte('0' + d - 26)
	}
	var out []byte
	for _, c := range in {
		if c < 0x80 {
			out = append(out, byte(c))
		}
	}
	b := len(out)
	h := b
	if b > 0 {
		out = append(out, '-')
	}
	n, delta, bias := 128, 0, 72
	for h < len(in) {
		m := int(^uint(0) >> 1)
		for _, c := range in {
			if int(c) >= n && int(c) < m {
				m = int(c)
			}
		}
		delta += (m - n) * (h + 1)
		n = m
		for _, c := range in {
			if int(c) < n {
				delta++
			}
			if int(c) == n {
				q := delta
				for k := base; ; k += base {
					t := k - bias
					if t < tmin {
						t = tmin
					} else if t > tmax {
						t = tmax
					}
					if q < t {
						break
					}
					out = append(out, digit(t+(q-t)%(base-t)))
					q = (q - t) / (base - t)
				}
				out = append(out, digit(q))
				bias = adapt(delta, h+1, h == b)
				delta = 0
				h++
			}
		}
		delta++
		n++
	}
	return string(out)
}
