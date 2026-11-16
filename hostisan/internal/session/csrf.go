package session

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
)

// CheckOrigin ports Hestia's prevent_post_csrf(). For state-changing requests with an
// Origin header it grades the origin: 2 = the server's own hostname, 1 = the host the
// request was sent to, 0 = anything else. It passes when the grade is at least
// POLICY_CSRF_STRICTNESS. Requests without Origin pass, as in Hestia.
func CheckOrigin(r *http.Request, systemHostname string, strictness int) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return strictness <= 0
	}
	reqHost, reqPort := splitHostPort(r.Host, "443")
	originPort := u.Port()
	if originPort == "" {
		originPort = map[string]string{"http": "80"}[u.Scheme]
		if originPort == "" {
			originPort = "443"
		}
	}
	level := 0
	if originPort == reqPort {
		switch u.Hostname() {
		case systemHostname:
			level = 2
		case reqHost:
			level = 1
		}
	}
	return level >= strictness
}

func splitHostPort(hostport, defaultPort string) (string, string) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport, defaultPort
	}
	return host, port
}

// CheckToken compares the X-CSRF-Token header with the session's token in constant time.
func CheckToken(s *Session, r *http.Request) bool {
	got := r.Header.Get("X-CSRF-Token")
	if s == nil || s.Token == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}
