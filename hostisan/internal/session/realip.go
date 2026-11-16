package session

import (
	"context"
	"net"
	"net/http"
	"net/netip"
)

type proxyKey struct{}

// FromProxy marks a connection as coming from the local reverse proxy (hestia-nginx)
// over Hostisan's Unix socket. Only the proxy's group can open that socket; a TCP
// port, even on loopback, is open to every customer's PHP and cron jobs.
func FromProxy(ctx context.Context) context.Context {
	return context.WithValue(ctx, proxyKey{}, true)
}

// RealIP is the visitor's address, ported from Hestia's get_real_user_ip(). Forwarding
// headers are only trusted from the peer that would set them: X-Real-IP from the proxy
// socket (see FromProxy), CF-Connecting-IP from a Cloudflare edge address.
func RealIP(r *http.Request) string {
	host := r.RemoteAddr
	if proxied, _ := r.Context().Value(proxyKey{}).(bool); proxied {
		host = r.Header.Get("X-Real-IP")
	} else if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		if host == r.RemoteAddr {
			return host
		}
		return ""
	}
	ip = ip.Unmap()
	if isCloudflare(ip) {
		if cf, err := netip.ParseAddr(r.Header.Get("CF-Connecting-IP")); err == nil {
			ip = cf.Unmap()
		}
	}
	return ip.String()
}
