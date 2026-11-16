package session

import (
	"net/http/httptest"
	"testing"
)

func TestCheckOrigin(t *testing.T) {
	const sysHost = "server1.acme-hosting.net"
	cases := []struct {
		name, method, host, origin string
		// pass[s] = expected result at POLICY_CSRF_STRICTNESS s
		pass [3]bool
	}{
		{"GET is never checked", "GET", "panel.example.org:8083", "https://evil.example", [3]bool{true, true, true}},
		{"no Origin header passes", "POST", "panel.example.org:8083", "", [3]bool{true, true, true}},
		{"system hostname = level 2", "POST", sysHost + ":8083", "https://" + sysHost + ":8083", [3]bool{true, true, true}},
		{"same host as request = level 1", "POST", "203.0.113.24:8083", "https://203.0.113.24:8083", [3]bool{true, true, false}},
		{"default 443 on both sides", "POST", "panel.example.org", "https://panel.example.org", [3]bool{true, true, false}},
		{"other site = level 0", "POST", "203.0.113.24:8083", "https://evil.example", [3]bool{true, false, false}},
		{"same host, other port = level 0", "POST", "203.0.113.24:8083", "https://203.0.113.24:9999", [3]bool{true, false, false}},
	}
	for _, tc := range cases {
		for strictness, want := range tc.pass {
			r := httptest.NewRequest(tc.method, "/api/v1/x", nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := CheckOrigin(r, sysHost, strictness); got != want {
				t.Errorf("%s at strictness %d: got %v, want %v", tc.name, strictness, got, want)
			}
		}
	}
}

func TestCheckToken(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("X-CSRF-Token", "abc123")
	if !CheckToken(&Session{Token: "abc123"}, r) {
		t.Error("matching token rejected")
	}
	if CheckToken(&Session{Token: "zzz999"}, r) {
		t.Error("wrong token accepted")
	}
	if CheckToken(&Session{Token: ""}, httptest.NewRequest("POST", "/", nil)) {
		t.Error("empty session token with empty header accepted")
	}
}
