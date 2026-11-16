package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/i18n"
)

const hestiaPot = `msgid ""
msgstr ""

msgid "Log out"
msgstr ""

msgid "Cron Jobs"
msgstr ""
`

// tempRepo makes a small repository: Go and UI code, Hestia's template and its
// French and German catalogues (copied from the real repo).
func tempRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"web/locale/hestiacp.pot":      hestiaPot,
		"hostisan/internal/x/x.go":     "package x\n\nconst saveMsg = \"Settings saved.\"\n",
		"ui/src/page.tsx":              "export const P = () => <p>{t('Cron jobs')} {t('Log out')} {t('Brand new')}</p>\n",
		"hostisan/locale/hostisan.pot": "# stale\n",
		"hostisan/locale/locale.go":    "package locale\n",
	}
	for _, lang := range []string{"fr", "de"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "locale", lang, "LC_MESSAGES", "hestiacp.mo"))
		if err != nil {
			t.Fatal(err)
		}
		files["web/locale/"+lang+"/LC_MESSAGES/hestiacp.mo"] = string(data)
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runIn(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestExtractWritesPot(t *testing.T) {
	root := tempRepo(t)
	if code, _, errOut := runIn(t, "extract", "-repo", root); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	pot := readFile(t, root, "hostisan/locale/hostisan.pot")
	ids, err := i18n.MsgIDs([]byte(pot))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, "|") != "Brand new|Cron jobs|Settings saved." {
		t.Fatalf("ids = %q\n%s", ids, pot)
	}
	if !strings.Contains(pot, "#. Hestia: \"Cron Jobs\"\n#: ui/src/page.tsx\nmsgid \"Cron jobs\"") {
		t.Fatalf("pot:\n%s", pot)
	}
}

func TestExtractCheckFailsOnDrift(t *testing.T) {
	root := tempRepo(t)
	code, _, errOut := runIn(t, "extract", "-repo", root, "-check")
	if code == 0 || !strings.Contains(errOut, "make i18n-extract") || !strings.Contains(errOut, "+ \"Brand new\"") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := readFile(t, root, "hostisan/locale/hostisan.pot"); got != "# stale\n" {
		t.Fatal("-check wrote the template")
	}
	runIn(t, "extract", "-repo", root)
	if code, _, errOut := runIn(t, "extract", "-repo", root, "-check"); code != 0 {
		t.Fatalf("current template: exit %d: %s", code, errOut)
	}
}

func TestInitCreatesLanguageFromTemplate(t *testing.T) {
	root := tempRepo(t)
	runIn(t, "extract", "-repo", root)
	if code, _, errOut := runIn(t, "init", "-repo", root, "fr"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	po := readFile(t, root, "hostisan/locale/fr/LC_MESSAGES/hostisan.po")
	if !strings.Contains(po, `"Language: fr\n"`) {
		t.Fatalf("no Language header:\n%s", po)
	}
	ids, _ := i18n.MsgIDs([]byte(po))
	if strings.Join(ids, "|") != "Brand new|Cron jobs|Settings saved." {
		t.Fatalf("ids = %q", ids)
	}
	if code, _, _ := runIn(t, "init", "-repo", root, "fr"); code == 0 {
		t.Fatal("init overwrote an existing catalogue")
	}
	for _, bad := range []string{"xx", "../fr", "FR", ""} {
		if code, _, _ := runIn(t, "init", "-repo", root, bad); code == 0 {
			t.Errorf("init %q succeeded", bad)
		}
	}
}

func TestMergeAllUpdatesEveryLanguage(t *testing.T) {
	root := tempRepo(t)
	runIn(t, "extract", "-repo", root)
	runIn(t, "init", "-repo", root, "fr")
	runIn(t, "init", "-repo", root, "de")
	fr := filepath.Join(root, "hostisan", "locale", "fr", "LC_MESSAGES", "hostisan.po")
	data, _ := os.ReadFile(fr)
	data = bytes.Replace(data, []byte("msgid \"Brand new\"\nmsgstr \"\""), []byte("msgid \"Brand new\"\nmsgstr \"Tout neuf\""), 1)
	os.WriteFile(fr, data, 0o644)
	os.WriteFile(filepath.Join(root, "ui", "src", "page.tsx"), []byte("export const P = () => t('Another')\n"), 0o644)
	runIn(t, "extract", "-repo", root)
	if code, _, errOut := runIn(t, "merge", "-repo", root, "-all"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, lang := range []string{"fr", "de"} {
		po := readFile(t, root, "hostisan/locale/"+lang+"/LC_MESSAGES/hostisan.po")
		ids, _ := i18n.MsgIDs([]byte(po))
		if strings.Join(ids, "|") != "Another|Settings saved." {
			t.Errorf("%s ids = %q", lang, ids)
		}
	}
	if po := readFile(t, root, "hostisan/locale/fr/LC_MESSAGES/hostisan.po"); !strings.Contains(po, "#~ msgid \"Brand new\"\n#~ msgstr \"Tout neuf\"") {
		t.Errorf("translation not kept as obsolete:\n%s", po)
	}
	if code, _, _ := runIn(t, "merge", "-repo", root, "it"); code == 0 {
		t.Error("merge of a language without a catalogue succeeded")
	}
}

func TestStatsPrintsOneLinePerLanguage(t *testing.T) {
	root := tempRepo(t)
	runIn(t, "extract", "-repo", root)
	runIn(t, "init", "-repo", root, "fr")
	code, out, errOut := runIn(t, "stats", "-repo", root)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "lang") || !strings.HasPrefix(lines[1], "de ") || !strings.HasPrefix(lines[2], "fr ") {
		t.Fatalf("stats:\n%s", out)
	}
	// fr: 3 msgids, Cron jobs covered by Hestia's Cron Jobs.
	if f := strings.Fields(lines[2]); len(f) != 6 || f[1] != "3" || f[2] != "0" || f[3] != "1" || f[4] != "33%" {
		t.Fatalf("fr line %q", lines[2])
	}
}
