package httpapi

import (
	"net/http"
	"testing"
)

func TestReferrerPolicyNoReferrer(t *testing.T) {
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) })
	h := newHarnessWith(t, overrides{}, func(d *Deps) { d.UI = ui })
	for _, p := range []string{"/api/v1/bootstrap", "/", "/api/nope"} {
		res, _ := bkGet(h, p)
		if got := res.Header.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s Referrer-Policy = %q", p, got)
		}
	}
}
