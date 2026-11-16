// Command i18n keeps Hostisan's message catalogue (hostisan/locale) in step with the
// code. It is a development tool: the Makefile's i18n-* targets run it, and it is not
// packaged.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/hostisan/hostisan/internal/i18n"
	"github.com/hostisan/hostisan/internal/i18n/extract"
)

const usage = `usage: go run ./cmd/i18n <command> [-repo DIR] [args]

commands:
  extract [-check]   regenerate hostisan/locale/hostisan.pot from the code;
                     -check only reports whether it is current
  merge <lang>|-all  update a language's hostisan.po to the template
  init <lang>        start hostisan/locale/<lang>/LC_MESSAGES/hostisan.po
  stats              print each language's coverage of the template

-repo is the repository root (default "..", for running from hostisan/).
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", "..", "repository root")
	check := fs.Bool("check", false, "extract: report drift, write nothing")
	all := fs.Bool("all", false, "merge: every language")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	var err error
	switch args[0] {
	case "extract":
		err = extractCmd(*repo, *check, stderr)
	case "merge":
		err = mergeCmd(*repo, *all, fs.Args(), stdout)
	case "init":
		if fs.NArg() != 1 {
			err = errors.New("init needs one language")
		} else {
			err = initCmd(*repo, fs.Arg(0), stdout)
		}
	case "stats":
		err = statsCmd(*repo, stdout)
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "i18n:", err)
		return 1
	}
	return 0
}

func path(repo, rel string) string { return filepath.Join(repo, filepath.FromSlash(rel)) }

func extractCmd(repo string, check bool, stderr io.Writer) error {
	pot, problems, err := extract.Run(repo)
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Fprintf(stderr, "%s:%d: msgid built at run time: %s\n", p.File, p.Line, p.Text)
	}
	target := path(repo, extract.TemplatePath)
	if check {
		old, err := os.ReadFile(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !bytes.Equal(old, pot) {
			fmt.Fprint(stderr, extract.Diff(old, pot))
			return fmt.Errorf("%s is out of date: run make i18n-extract", extract.TemplatePath)
		}
		return nil
	}
	return os.WriteFile(target, pot, 0o644)
}

// langRE is the shape of web/locale's language directories: "fr", "pt-br".
var langRE = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]{2,4})?$`)

// poPath is a language's catalogue; the language must be one Hestia ships.
func poPath(repo, lang string) (string, error) {
	if !langRE.MatchString(lang) || lang == "en" {
		return "", fmt.Errorf("unknown language %q", lang)
	}
	if st, err := os.Stat(path(repo, "web/locale/"+lang)); err != nil || !st.IsDir() {
		return "", fmt.Errorf("unknown language %q: web/locale has no such directory", lang)
	}
	return path(repo, "hostisan/locale/"+lang+"/LC_MESSAGES/hostisan.po"), nil
}

func initCmd(repo, lang string, stdout io.Writer) error {
	target, err := poPath(repo, lang)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s exists: run make i18n-merge to update it", target)
	}
	pot, err := os.ReadFile(path(repo, extract.TemplatePath))
	if err != nil {
		return err
	}
	header := fmt.Sprintf(`# Hostisan's messages in %[1]s. Translations here win over Hestia's catalogue.
msgid ""
msgstr ""
"Project-Id-Version: Hostisan\n"
"Language: %[1]s\n"
"MIME-Version: 1.0\n"
"Content-Type: text/plain; charset=UTF-8\n"
"Content-Transfer-Encoding: 8bit\n"
`, lang)
	po, err := extract.Merge(pot, []byte(header))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, po, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "wrote", target)
	return nil
}

// languages lists the languages that have a hostisan.po.
func languages(repo string) ([]string, error) {
	matches, err := filepath.Glob(path(repo, "hostisan/locale/*/LC_MESSAGES/hostisan.po"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		out = append(out, filepath.Base(filepath.Dir(filepath.Dir(m))))
	}
	sort.Strings(out)
	return out, nil
}

func mergeCmd(repo string, all bool, args []string, stdout io.Writer) error {
	langs := args
	if all {
		var err error
		if langs, err = languages(repo); err != nil {
			return err
		}
	} else if len(langs) != 1 {
		return errors.New("merge needs one language or -all")
	}
	pot, err := os.ReadFile(path(repo, extract.TemplatePath))
	if err != nil {
		return err
	}
	for _, lang := range langs {
		target, err := poPath(repo, lang)
		if err != nil {
			return err
		}
		po, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("%s: %w (start it with init)", lang, err)
		}
		merged, err := extract.Merge(pot, po)
		if err != nil {
			return fmt.Errorf("%s: %w", lang, err)
		}
		if err := os.WriteFile(target, merged, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "merged", lang)
	}
	return nil
}

func statsCmd(repo string, stdout io.Writer) error {
	pot, err := os.ReadFile(path(repo, extract.TemplatePath))
	if err != nil {
		return err
	}
	po := map[string][]byte{}
	langs, err := languages(repo)
	if err != nil {
		return err
	}
	for _, lang := range langs {
		if po[lang], err = os.ReadFile(path(repo, "hostisan/locale/"+lang+"/LC_MESSAGES/hostisan.po")); err != nil {
			return err
		}
	}
	mos, err := filepath.Glob(path(repo, "web/locale/*/LC_MESSAGES/hestiacp.mo"))
	if err != nil {
		return err
	}
	cat := &i18n.Catalogs{Dir: path(repo, "web/locale")}
	hestia := map[string]map[string]string{}
	for _, mo := range mos {
		lang := filepath.Base(filepath.Dir(filepath.Dir(mo)))
		if hestia[lang], err = cat.Messages(lang); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "%-6s %6s %6s %6s %8s %7s\n", "lang", "total", "po", "twin", "covered", "hestia")
	for _, s := range extract.Stats(pot, po, hestia) {
		pct := 0
		if s.Total > 0 {
			pct = (s.PO + s.Twin) * 100 / s.Total
		}
		fmt.Fprintf(stdout, "%-6s %6d %6d %6d %7d%% %7d\n", s.Lang, s.Total, s.PO, s.Twin, pct, s.Hestia)
	}
	return nil
}
