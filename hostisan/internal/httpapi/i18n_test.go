package httpapi

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hostisan/hostisan/locale"
)

func TestI18nEndpoint(t *testing.T) {
	h := newHarnessWithLocale(t, filepath.Join("..", "..", "..", "web", "locale"))
	var fr map[string]string
	res, err := h.client.Get(h.srv.URL + "/api/v1/i18n/fr")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("status %d, cache %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if code := h.call("GET", "/api/v1/i18n/fr", nil, &fr); code != 200 || fr["Invalid username or password"] == "" {
		t.Fatalf("fr: %d, %d messages", code, len(fr))
	}
	for msgid, value := range fr {
		if strings.Contains(value, "Hestia") {
			t.Fatalf("translation of %q still says Hestia: %q", msgid, value)
		}
	}
	if code := h.call("GET", "/api/v1/i18n/xx", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown language: %d", code)
	}
	var en map[string]string
	if code := h.call("GET", "/api/v1/i18n/en", nil, &en); code != 200 || len(en) != 0 {
		t.Fatalf("en: %d, %d messages", code, len(en))
	}
}

const overlayFR = `msgid ""
msgstr ""
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Two-factor authentication enabled."
msgstr "Authentification à deux facteurs activée par Hestia."
`

func TestTranslationsServeOverlay(t *testing.T) {
	h := newHarnessWithLocale(t, filepath.Join("..", "..", "..", "web", "locale"))
	if h.api.catalogs.Overlay != locale.FS {
		t.Fatal("the server does not layer the embedded Hostisan catalogue")
	}
	h.api.catalogs.Overlay = fstest.MapFS{
		"hostisan.pot":               {Data: []byte("msgid \"\"\nmsgstr \"\"\n\nmsgid \"Cron jobs\"\nmsgstr \"\"\n")},
		"fr/LC_MESSAGES/hostisan.po": {Data: []byte(overlayFR)},
	}
	var fr map[string]string
	if code := h.call("GET", "/api/v1/i18n/fr", nil, &fr); code != 200 {
		t.Fatalf("fr: %d", code)
	}
	if fr["Two-factor authentication enabled."] == "" || fr["Cron jobs"] == "" || fr["Invalid username or password"] == "" {
		t.Fatalf("overlay %q, case twin %q, Hestia %q", fr["Two-factor authentication enabled."], fr["Cron jobs"], fr["Invalid username or password"])
	}
}

func TestTranslationsBrandRewriteOverlay(t *testing.T) {
	h := newHarnessWithLocale(t, filepath.Join("..", "..", "..", "web", "locale"))
	h.api.catalogs.Overlay = fstest.MapFS{"fr/LC_MESSAGES/hostisan.po": {Data: []byte(overlayFR)}}
	var fr map[string]string
	h.call("GET", "/api/v1/i18n/fr", nil, &fr)
	if got := fr["Two-factor authentication enabled."]; got == "" || strings.Contains(got, "Hestia") {
		t.Fatalf("overlay text not rewritten: %q", got)
	}
}

func TestTranslationsUnknownLanguage404(t *testing.T) {
	h := newHarnessWithLocale(t, filepath.Join("..", "..", "..", "web", "locale"))
	h.api.catalogs.Overlay = fstest.MapFS{"fr/LC_MESSAGES/hostisan.po": {Data: []byte(overlayFR)}}
	for _, lang := range []string{"xx", "..", "hostisan.pot", "fr%2F..%2F..%2Fetc"} {
		if code := h.call("GET", "/api/v1/i18n/"+lang, nil, nil); code != http.StatusNotFound {
			t.Errorf("%s: %d", lang, code)
		}
	}
}
