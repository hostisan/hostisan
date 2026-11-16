package session

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestRealIP(t *testing.T) {
	cases := []struct {
		name, remote string
		headers      map[string]string
		want         string
	}{
		{"plain peer", "203.0.113.9:51000", nil, "203.0.113.9"},
		// Any customer's PHP or cron job can connect to a loopback port, so only the
		// proxy socket is trusted to set X-Real-IP (see TestRealIPFromProxySocket).
		{"loopback peer cannot spoof X-Real-IP", "127.0.0.1:40000", map[string]string{"X-Real-IP": "198.51.100.7"}, "127.0.0.1"},
		{"remote peer cannot spoof X-Real-IP", "203.0.113.9:51000", map[string]string{"X-Real-IP": "10.0.0.1"}, "203.0.113.9"},
		{"Cloudflare edge passes visitor IP", "173.245.48.10:443", map[string]string{"CF-Connecting-IP": "198.51.100.8"}, "198.51.100.8"},
		{"non-Cloudflare peer cannot spoof CF header", "203.0.113.9:51000", map[string]string{"CF-Connecting-IP": "10.0.0.1"}, "203.0.113.9"},
		{"Cloudflare IPv6 edge", "[2606:4700::1]:443", map[string]string{"CF-Connecting-IP": "2001:db8::9"}, "2001:db8::9"},
		{"IPv4-mapped IPv6 is unwrapped", "[::ffff:10.0.0.1]:5000", nil, "10.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := RealIP(r); got != tc.want {
				t.Fatalf("RealIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRealIPFromProxySocket(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"proxy sets X-Real-IP", map[string]string{"X-Real-IP": "198.51.100.7"}, "198.51.100.7"},
		{"proxy forwards a Cloudflare edge", map[string]string{"X-Real-IP": "173.245.48.10", "CF-Connecting-IP": "198.51.100.8"}, "198.51.100.8"},
		{"non-Cloudflare client cannot spoof CF header", map[string]string{"X-Real-IP": "203.0.113.9", "CF-Connecting-IP": "10.0.0.1"}, "203.0.113.9"},
		{"invalid X-Real-IP gives no address", map[string]string{"X-Real-IP": "not-an-ip"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "@" // what net/http reports for a Unix socket peer
			r = r.WithContext(FromProxy(context.Background()))
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := RealIP(r); got != tc.want {
				t.Fatalf("RealIP = %q, want %q", got, tc.want)
			}
		})
	}
}
