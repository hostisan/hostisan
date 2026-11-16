package i18n_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// crowdinFile is one entry of crowdin.yml's files: list.
type crowdinFile struct {
	source, translation string
	mapping             map[string]string // languages_mapping, Crowdin code → directory
}

// readCrowdin reads the files: list of crowdin.yml, which is small and flat enough to
// read line by line (the module has no YAML parser).
func readCrowdin(t *testing.T) []crowdinFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "crowdin.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var out []crowdinFile
	inFiles, inMapping := false, false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent == 0 {
			inFiles = line == "files:"
			continue
		}
		if !inFiles {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			out = append(out, crowdinFile{mapping: map[string]string{}})
			line, inMapping = rest, false
		}
		if len(out) == 0 {
			continue
		}
		f := &out[len(out)-1]
		key, value, _ := strings.Cut(line, ":")
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch {
		case key == "source":
			f.source = value
		case key == "translation":
			f.translation = value
		case key == "languages_mapping":
			inMapping = true
		case inMapping && value != "":
			f.mapping[key] = value
		}
	}
	return out
}

func TestCrowdinMapsHostisanCatalogue(t *testing.T) {
	files := readCrowdin(t)
	if len(files) != 2 || files[0].source != "/web/locale/hestiacp.pot" {
		t.Fatalf("files = %+v, want Hestia's entry first and Hostisan's second", files)
	}
	h := files[1]
	if h.source != "/hostisan/locale/hostisan.pot" ||
		h.translation != "/hostisan/locale/%two_letters_code%/LC_MESSAGES/hostisan.po" {
		t.Fatalf("Hostisan entry = %+v", h)
	}
	want := map[string]string{"pt-BR": "pt-br", "zh-CN": "zh-cn", "zh-TW": "zh-tw"}
	for code, dir := range want {
		if h.mapping[code] != dir {
			t.Errorf("languages_mapping %s = %q, want %q", code, h.mapping[code], dir)
		}
	}
	for code, dir := range h.mapping {
		if st, err := os.Stat(filepath.Join(repoRoot, "web", "locale", dir)); err != nil || !st.IsDir() {
			t.Errorf("languages_mapping %s → %s: not a web/locale language", code, dir)
		}
	}
}
