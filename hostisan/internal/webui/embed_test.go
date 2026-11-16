package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"index.html":       {Data: []byte("<!doctype html><title>Hostisan</title>")},
	"assets/app-1.js":  {Data: []byte("console.log(1)")},
	"assets/app-1.css": {Data: []byte("body{}")},
}

func get(t *testing.T, fs fstest.MapFS, path string) (int, string, string) {
	t.Helper()
	w := httptest.NewRecorder()
	NewHandler(fs).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w.Code, w.Body.String(), w.Header().Get("Cache-Control")
}

func TestSPARoutesGetIndex(t *testing.T) {
	for _, path := range []string{"/", "/web", "/web/acme-bakery.com/logs", "/login"} {
		code, body, _ := get(t, built, path)
		if code != 200 || !strings.Contains(body, "<title>Hostisan</title>") {
			t.Errorf("%s: %d %q", path, code, body)
		}
	}
}

func TestAssetsServedWithLongCache(t *testing.T) {
	code, body, cache := get(t, built, "/assets/app-1.js")
	if code != 200 || body != "console.log(1)" || !strings.Contains(cache, "immutable") {
		t.Fatalf("asset: %d %q cache %q", code, body, cache)
	}
	if _, _, cache := get(t, built, "/web"); !strings.Contains(cache, "no-cache") {
		t.Fatalf("index.html must not be cached long, got %q", cache)
	}
}

func TestMissingAssetIs404NotIndex(t *testing.T) {
	if code, _, _ := get(t, built, "/assets/gone.js"); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}
}

func TestAPIPathsAreNeverTheSPA(t *testing.T) {
	if code, body, _ := get(t, built, "/api/v1/bootstrap"); code != 404 || strings.Contains(body, "<title>") {
		t.Fatalf("/api/ was served by the SPA: %d %q", code, body)
	}
}

func TestUnbuiltUIExplainsItself(t *testing.T) {
	code, body, _ := get(t, fstest.MapFS{".keep": {}}, "/web")
	if code != 503 || !strings.Contains(body, "make ui-build") {
		t.Fatalf("unbuilt: %d %q", code, body)
	}
}
