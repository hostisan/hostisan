package i18n

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hostisan/hostisan/locale"
)

var localeDir = filepath.Join("..", "..", "..", "web", "locale")

func TestMessagesFrench(t *testing.T) {
	c := &Catalogs{Dir: localeDir}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	got := m["Invalid username or password"]
	if got == "" || got == "Invalid username or password" {
		t.Fatalf("French translation = %q", got)
	}
	if len(m) < 500 {
		t.Fatalf("only %d French messages", len(m))
	}
}

func TestMessagesEnglishIsEmpty(t *testing.T) {
	m, err := (&Catalogs{Dir: localeDir}).Messages("en")
	if err != nil || len(m) != 0 {
		t.Fatalf("en = %d messages, %v", len(m), err)
	}
}

func TestMessagesRejectsUnknownAndTraversal(t *testing.T) {
	c := &Catalogs{Dir: localeDir}
	for _, lang := range []string{"../etc", "xx", "", "fr/../../x", "."} {
		if _, err := c.Messages(lang); err == nil {
			t.Errorf("Messages(%q) succeeded", lang)
		}
	}
}

func TestMessagesAreCached(t *testing.T) {
	c := &Catalogs{Dir: localeDir}
	a, _ := c.Messages("de")
	b, _ := c.Messages("de")
	if len(a) == 0 || &a == nil || len(a) != len(b) {
		t.Fatal("unexpected catalog")
	}
	c.Dir = "/nonexistent" // a cached language must not touch the disk again
	if _, err := c.Messages("de"); err != nil {
		t.Fatalf("cached read failed: %v", err)
	}
}

func TestNoMsgstrIsEmpty(t *testing.T) {
	m, _ := (&Catalogs{Dir: localeDir}).Messages("fr")
	for k, v := range m {
		if strings.TrimSpace(v) == "" {
			t.Fatalf("empty translation kept for %q", k)
		}
	}
}

func TestEveryShippedCatalogParses(t *testing.T) {
	langs, err := filepath.Glob(filepath.Join(localeDir, "*", "LC_MESSAGES", "hestiacp.mo"))
	if err != nil || len(langs) < 40 {
		t.Fatalf("found %d catalogs, %v", len(langs), err)
	}
	c := &Catalogs{Dir: localeDir}
	for _, path := range langs {
		lang := filepath.Base(filepath.Dir(filepath.Dir(path)))
		m, err := c.Messages(lang)
		if err != nil {
			t.Errorf("%s: %v", lang, err)
		} else if len(m) == 0 {
			t.Errorf("%s: no messages", lang)
		}
	}
}

// overlayPO is a Hostisan catalogue for the overlay tests.
const overlayPO = `# A Hostisan catalogue.
msgid ""
msgstr ""
"Content-Type: text/plain; charset=UTF-8\n"
"Plural-Forms: nplurals=2; plural=(n > 1);\n"

#: ui/src/x.tsx:1
msgid "Two-factor authentication enabled."
msgstr "Authentification à deux facteurs activée."

# Hostisan's wording wins over Hestia's for a msgid both have.
msgid "Invalid username or password"
msgstr "Identifiants refusés"

msgid "Untranslated here"
msgstr ""

#, fuzzy
msgid "Fuzzy entry"
msgstr "Pas sûr"

msgid ""
"Line one\n"
"line \"two\""
msgstr ""
"Ligne un\n"
"ligne \"deux\""

msgid "%d rule"
msgid_plural "%d rules"
msgstr[0] "%d règle"
msgstr[1] "%d règles"
`

func overlay(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func TestOverlayComesFirstThenHestia(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"fr/LC_MESSAGES/hostisan.po": overlayPO})}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Two-factor authentication enabled.": "Authentification à deux facteurs activée.",
		"Invalid username or password":       "Identifiants refusés",
		"Line one\nline \"two\"":             "Ligne un\nligne \"deux\"",
		"%d rule":                            "%d règle", // plurals keep their first form, as for Hestia's
	}
	for id, tr := range want {
		if m[id] != tr {
			t.Errorf("%q = %q, want %q", id, m[id], tr)
		}
	}
	for _, id := range []string{"Untranslated here", "Fuzzy entry", ""} {
		if v, ok := m[id]; ok {
			t.Errorf("%q kept as %q", id, v)
		}
	}
	// Hestia's catalogue still answers what the overlay doesn't have.
	if v := m["Log out"]; v == "" || v == "Log out" {
		t.Errorf("Hestia fallback lost: Log out = %q", v)
	}
}

func TestOverlayAloneMakesALanguage(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"xx/LC_MESSAGES/hostisan.po": overlayPO})}
	m, err := c.Messages("xx")
	if err != nil || m["Two-factor authentication enabled."] == "" {
		t.Fatalf("xx: %v, %d messages", err, len(m))
	}
}

func TestOverlayLanguageWithoutFileUsesHestia(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"de/LC_MESSAGES/hostisan.po": overlayPO})}
	m, err := c.Messages("fr")
	if err != nil || len(m) < 500 {
		t.Fatalf("fr: %v, %d messages", err, len(m))
	}
}

func TestOverlayEnglishStaysEmpty(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"en/LC_MESSAGES/hostisan.po": overlayPO})}
	if m, err := c.Messages("en"); err != nil || len(m) != 0 {
		t.Fatalf("en = %d messages, %v", len(m), err)
	}
}

func TestOverlayRejectsTraversal(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"fr/LC_MESSAGES/hostisan.po": overlayPO})}
	for _, lang := range []string{"../fr", "fr/../fr", "./fr", "fr/", ".."} {
		if _, err := c.Messages(lang); err == nil {
			t.Errorf("Messages(%q) succeeded", lang)
		}
	}
}

func TestOverlayBrokenPOIsAnError(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"fr/LC_MESSAGES/hostisan.po": "msgid \"unterminated\nmsgstr \"x\"\n"})}
	if _, err := c.Messages("fr"); err == nil {
		t.Fatal("broken .po accepted")
	}
}

func TestMsgIDsListsEveryEntry(t *testing.T) {
	ids, err := MsgIDs([]byte(overlayPO))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Two-factor authentication enabled.", "Invalid username or password", "Untranslated here", "Fuzzy entry", "Line one\nline \"two\"", "%d rule"}
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("MsgIDs = %q", ids)
	}
}

// potWith is a Hostisan template listing ids.
func potWith(ids ...string) string {
	var b strings.Builder
	b.WriteString("msgid \"\"\nmsgstr \"\"\n\"Content-Type: text/plain; charset=UTF-8\\n\"\n")
	for _, id := range ids {
		b.WriteString("\nmsgid " + strconv.Quote(id) + "\nmsgstr \"\"\n")
	}
	return b.String()
}

// writeMO writes a Hestia catalogue for lang under dir with the given messages.
func writeMO(t *testing.T, dir, lang string, msgs map[string]string) {
	t.Helper()
	ids := make([]string, 0, len(msgs))
	for id := range msgs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	n := len(ids)
	head := 28
	origTable, transTable := head, head+n*8
	pos := transTable + n*8
	var tables, strs []byte
	put := func(s string) {
		tables = binary.LittleEndian.AppendUint32(tables, uint32(len(s)))
		tables = binary.LittleEndian.AppendUint32(tables, uint32(pos+len(strs)))
		strs = append(strs, s...)
		strs = append(strs, 0)
	}
	for _, id := range ids {
		put(id)
	}
	for _, id := range ids {
		put(msgs[id])
	}
	var data []byte
	for _, v := range []uint32{0x950412de, 0, uint32(n), uint32(origTable), uint32(transTable), 0, 0} {
		data = binary.LittleEndian.AppendUint32(data, v)
	}
	data = append(append(data, tables...), strs...)
	p := filepath.Join(dir, lang, "LC_MESSAGES")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "hestiacp.mo"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCaseFoldFillsHostisanMsgids(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"hostisan.pot": potWith("Cron jobs", "Spam  filter")})}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	hestia, _ := (&Catalogs{Dir: localeDir}).Messages("fr")
	if m["Cron jobs"] == "" || m["Cron jobs"] != hestia["Cron Jobs"] {
		t.Errorf("Cron jobs = %q, want Hestia's Cron Jobs %q", m["Cron jobs"], hestia["Cron Jobs"])
	}
	// Internal whitespace collapses too.
	if m["Spam  filter"] == "" || m["Spam  filter"] != hestia["Spam Filter"] {
		t.Errorf("Spam  filter = %q, want %q", m["Spam  filter"], hestia["Spam Filter"])
	}
}

func TestCaseFoldNeverBeatsOverlay(t *testing.T) {
	po := potWith() + "\nmsgid \"Cron jobs\"\nmsgstr \"Tâches planifiées\"\n"
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{
		"hostisan.pot":               potWith("Cron jobs"),
		"fr/LC_MESSAGES/hostisan.po": po,
	})}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	if m["Cron jobs"] != "Tâches planifiées" {
		t.Fatalf("Cron jobs = %q, want the overlay's", m["Cron jobs"])
	}
}

func TestCaseFoldIgnoresNonTemplateMsgids(t *testing.T) {
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"hostisan.pot": potWith("Spam filter")})}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := m["Cron jobs"]; ok {
		t.Fatalf("Cron jobs filled as %q though the template lacks it", v)
	}
}

func TestCaseFoldAmbiguousTwinPicksFirstSorted(t *testing.T) {
	dir := t.TempDir()
	writeMO(t, dir, "fr", map[string]string{"web server": "serveur web", "Web Server": "Serveur Web", "WEB SERVER": "SERVEUR WEB"})
	c := &Catalogs{Dir: dir, Overlay: overlay(map[string]string{"hostisan.pot": potWith("Web server")})}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	if m["Web server"] != "SERVEUR WEB" { // "WEB SERVER" sorts first
		t.Fatalf("Web server = %q", m["Web server"])
	}
}

func TestOverlayFSHoldsTheTemplate(t *testing.T) {
	ids, err := fs.ReadFile(locale.FS, "hostisan.pot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MsgIDs(ids); err != nil {
		t.Fatalf("hostisan.pot: %v", err)
	}
}

// One entry with an escape that can't be read loses that entry only, never the language,
// and the skipped entry is logged.
func TestOverlayBadEscapeSkipsOnlyThatEntry(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(prev)

	po := `msgid "Before"
msgstr "Avant"

msgid "Broken"
msgstr "Cass\q"

msgid "Gettext's own escapes"
msgstr "l\'heure\? \101\x42"

msgid "After"
msgstr "Après"
`
	c := &Catalogs{Dir: localeDir, Overlay: overlay(map[string]string{"fr/LC_MESSAGES/hostisan.po": po})}
	m, err := c.Messages("fr")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"Before": "Avant", "After": "Après", "Gettext's own escapes": "l'heure? AB"} {
		if m[id] != want {
			t.Errorf("%q = %q, want %q", id, m[id], want)
		}
	}
	if v, ok := m["Broken"]; ok {
		t.Errorf("Broken kept as %q", v)
	}
	// Hestia's catalogue still answers.
	if v := m["Log out"]; v == "" || v == "Log out" {
		t.Errorf("Hestia lost: Log out = %q", v)
	}
	if !strings.Contains(logged.String(), "Broken") || !strings.Contains(logged.String(), "fr") {
		t.Errorf("skipped entry not logged: %q", logged.String())
	}
}

// The tools stay strict: a bad escape is an error there.
func TestReadPORejectsABadEscape(t *testing.T) {
	if _, err := ReadPO([]byte("msgid \"x\"\nmsgstr \"\\q\"\n")); err == nil {
		t.Fatal("bad escape accepted")
	}
}
