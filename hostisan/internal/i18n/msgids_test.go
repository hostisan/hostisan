package i18n_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/i18n"
	"github.com/hostisan/hostisan/internal/i18n/extract"
	"github.com/hostisan/hostisan/locale"
)

// repoRoot is the repository root, seen from this package.
var repoRoot = filepath.Join("..", "..", "..")

func readRepo(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The committed hostisan.pot is generated: it must be what the code makes now.
func TestHostisanPotIsCurrent(t *testing.T) {
	fresh, _, err := extract.Run(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	committed := readRepo(t, extract.TemplatePath)
	if string(fresh) != string(committed) {
		t.Fatalf("%s is out of date: run make i18n-extract\n%s", extract.TemplatePath, extract.Diff(committed, fresh))
	}
	embedded, err := fs.ReadFile(locale.FS, "hostisan.pot")
	if err != nil || string(embedded) != string(committed) {
		t.Fatalf("the embedded template differs from the file: %v", err)
	}
}

// A msgid built at run time can't be extracted or translated.
func TestNoNonLiteralTemplateMsgids(t *testing.T) {
	_, problems, err := extract.Code(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	var goSide, uiSide []string
	for _, p := range problems {
		line := fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Text)
		if why, ok := pendingProblems[p.File+": "+p.Text]; ok {
			t.Logf("pending (%s): %s", why, line)
			continue
		}
		if strings.HasPrefix(p.File, extract.TSDir+"/") {
			uiSide = append(uiSide, line)
		} else {
			goSide = append(goSide, line)
		}
	}
	t.Run("go", func(t *testing.T) {
		if len(goSide) > 0 {
			t.Errorf("msgids built at run time; use fmt.Sprintf(i18n.N(\"… %%s …\"), arg):\n%s", strings.Join(goSide, "\n"))
		}
	})
	t.Run("ui", func(t *testing.T) {
		if len(uiSide) > 0 {
			t.Errorf("msgids built at run time; use t('… %%s …', arg):\n%s", strings.Join(uiSide, "\n"))
		}
	})
}

// pendingProblems are known run-time msgids, by "file: expression", with what
// fixes them. Empty: add an entry only with a reason and a plan to remove it.
var pendingProblems = map[string]string{}

// carried are msgids the plan ledgers (p2d, p3, p4, p5) collected for this step,
// sampled. Each must be in Hostisan's template or Hestia's.
var carried = []struct {
	id string
	ui bool // shown by the UI's own t(), so the extractor finds it in ui/src
}{
	{id: "The current password is not correct."},
	{id: "The code is not correct."},
	{id: "Start setting up two-factor authentication again."},
	{id: "Two-factor authentication enabled."},
	{id: "invalid %s format :: %s"},
	{id: "Invalid cron command format"},
	{id: `Field "%s" is invalid.`},
	{id: `Field "%s" can not be blank.`},
	{id: "Invalid port: %s"},
	{id: "Invalid comment: %s"},
	{id: "IP list %s must exist, be IPv4 and not be suspended."},
	{id: "Invalid data source: %s"},
	{id: "phpMyAdmin can only be restricted with Single Sign On enabled."},
	{id: "The firewall can't be turned on until active ACCEPT rules keep the panel and SSH ports open."},
	{id: "Too many terminals are open. Close one and try again."},
	{id: "A restore is already running."},
	{id: "Invalid search query"},
	{id: "You have no shell access."},
	{id: "Your session has expired."},
	{id: "Two-factor authentication disabled.", ui: true},
	{id: "Cron job has been created successfully.", ui: true},
	{id: "Installation status is unknown", ui: true},
	{id: "Starting the installer", ui: true},
	{id: "Domain {%s} has been created successfully.", ui: true},
	{id: "Certificate has been generated successfully.", ui: true},
	{id: "NGINX cache has been purged successfully.", ui: true},
	{id: "Unsaved changes", ui: true},
}

func TestCarriedMsgidsAreExtracted(t *testing.T) {
	known := map[string]bool{}
	for _, rel := range []string{extract.TemplatePath, extract.HestiaTemplatePath} {
		ids, err := i18n.MsgIDs(readRepo(t, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			known[id] = true
		}
	}
	for _, c := range carried {
		t.Run(c.id, func(t *testing.T) {
			if !known[c.id] {
				t.Errorf("%q is in neither template", c.id)
			}
		})
	}
}

// shippedPOs lists the embedded hostisan.po files by language.
func shippedPOs(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := fs.Glob(locale.FS, "*/LC_MESSAGES/hostisan.po")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, p := range paths {
		data, err := fs.ReadFile(locale.FS, p)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.SplitN(p, "/", 2)[0]] = data
	}
	return out
}

func TestEveryShippedHostisanPOParses(t *testing.T) {
	c := &i18n.Catalogs{Dir: filepath.Join(repoRoot, "web", "locale"), Overlay: locale.FS}
	for lang, data := range shippedPOs(t) {
		if _, err := i18n.ReadPO(data); err != nil {
			t.Errorf("%s: %v", lang, err)
			continue
		}
		if _, err := c.Messages(lang); err != nil {
			t.Errorf("%s: %v", lang, err)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, "web", "locale", lang)); err != nil {
			t.Errorf("%s: not one of web/locale's languages", lang)
		}
	}
}

// A language's msgids are the template's: run make i18n-merge after extracting.
func TestHostisanPOMsgidsAreInTheTemplate(t *testing.T) {
	tmpl, err := i18n.MsgIDs(readRepo(t, extract.TemplatePath))
	if err != nil {
		t.Fatal(err)
	}
	inTemplate := map[string]bool{}
	for _, id := range tmpl {
		inTemplate[id] = true
	}
	for lang, data := range shippedPOs(t) {
		entries, err := i18n.ReadPO(data)
		if err != nil {
			continue // TestEveryShippedHostisanPOParses reports it
		}
		for _, e := range entries {
			if e.ID != "" && !e.Obsolete && !inTemplate[e.ID] {
				t.Errorf("%s: %q is not in hostisan.pot: run make i18n-merge", lang, e.ID)
			}
		}
	}
}

// Hestia's de catalogue gives "Web Domain" and "Mail Domain" as English plurals ("Web
// Domains"), which the case twins would carry into Hostisan's nouns. The de overlay
// corrects them.
func TestGermanOverlayFixesPluralDomainNouns(t *testing.T) {
	c := &i18n.Catalogs{Dir: filepath.Join(repoRoot, "web", "locale"), Overlay: locale.FS}
	m, err := c.Messages("de")
	if err != nil {
		t.Fatal(err)
	}
	hestia, err := (&i18n.Catalogs{Dir: filepath.Join(repoRoot, "web", "locale")}).Messages("de")
	if err != nil {
		t.Fatal(err)
	}
	if hestia["Web Domain"] != "Web Domains" || hestia["Mail Domain"] != "Mail Domains" {
		t.Logf("Hestia's de changed: Web Domain = %q, Mail Domain = %q", hestia["Web Domain"], hestia["Mail Domain"])
	}
	for id, want := range map[string]string{"web domain": "Web-Domain", "mail domain": "Mail-Domain", "Mail domain": "Mail-Domain"} {
		if m[id] != want {
			t.Errorf("%q = %q, want %q", id, m[id], want)
		}
	}
}
