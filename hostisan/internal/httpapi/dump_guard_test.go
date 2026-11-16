package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestDumpRefusesCrossSiteGET(t *testing.T) {
	for _, p := range []string{"/api/v1/db/admin_wordpress/download", "/api/v1/web/example.com/download"} {
		h := newHarness(t)
		h.loginAdmin()
		get := func(site string) *http.Response {
			req, _ := http.NewRequest("GET", h.srv.URL+p, nil)
			if site != "" {
				req.Header.Set("Sec-Fetch-Site", site)
			}
			c := *h.client
			c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			res, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			return res
		}
		before := len(dumpCalls(h))
		// A customer site on a sibling subdomain is "same-site", not "same-origin".
		for _, bad := range []string{"cross-site", "same-site"} {
			if res := get(bad); res.StatusCode != 403 {
				t.Errorf("%s %s: %d", p, bad, res.StatusCode)
			}
		}
		if n := len(dumpCalls(h)); n != before {
			t.Errorf("%s: dump ran on cross-site", p)
		}
		for _, ok := range []string{"", "same-origin", "none"} {
			if res := get(ok); res.StatusCode != 200 {
				t.Errorf("%s %q: %d", p, ok, res.StatusCode)
			}
		}
	}
}

func dumpCalls(h *harness) []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.HasPrefix(c.Cmd, "v-dump-") {
			out = append(out, c.Cmd)
		}
	}
	return out
}
