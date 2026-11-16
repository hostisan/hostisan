package extract

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/i18n"
)

// repo builds a repository tree in a temp dir: files maps a repo path to a fixture
// under testdata.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for dst, src := range files {
		data, err := os.ReadFile(filepath.Join("testdata", src))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(root, filepath.FromSlash(dst))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func ids(msgs []Msg) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func refsOf(msgs []Msg, id string) []string {
	for _, m := range msgs {
		if m.ID == id {
			return m.Refs
		}
	}
	return nil
}

func goRepo(t *testing.T) string {
	return repo(t, map[string]string{
		"hostisan/internal/fx/sinks.go":      "go/sinks.go.txt",
		"hostisan/internal/fx/sinks_test.go": "go/sinks_test.go.txt",
		"hostisan/internal/jobs/jobs.go":     "go/jobs.go.txt",
	})
}

func TestGoFindsEverySink(t *testing.T) {
	msgs, problems, err := Go(goRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Account message",
		"Alpha message.",
		"Bulk failure message.",
		"Check %s",
		"Error body message.",
		"Failure message %s.",
		"Grouped message.",
		"Inside jobs.",
		"Marked message",
		"Msgid-named message.",
		"Nested error body.",
		"Other message.",
		"Pointer failure message.",
		"Qualified write error.",
		"Search needs a query.",
		"Step message",
		"Write error message.",
	}
	if got := ids(msgs); !reflect.DeepEqual(got, want) {
		t.Errorf("ids =\n%q\nwant\n%q", got, want)
	}
	var texts []string
	for _, p := range problems {
		if p.File != "hostisan/internal/fx/sinks.go" || p.Line == 0 {
			t.Errorf("problem at %s:%d", p.File, p.Line)
		}
		texts = append(texts, p.Text)
	}
	if want := []string{`"Check " + svc`, `name`}; !reflect.DeepEqual(texts, want) {
		t.Errorf("problems = %q, want %q", texts, want)
	}
}

func TestGoSkipsTestFiles(t *testing.T) {
	msgs, _, err := Go(goRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids(msgs) {
		if id == "Test only message" {
			t.Fatal("a _test.go msgid was extracted")
		}
	}
}

func tsRepo(t *testing.T) string {
	return repo(t, map[string]string{
		"ui/src/pages/calls.tsx":      "ts/calls.tsx",
		"ui/src/pages/calls.test.tsx": "ts/calls.test.tsx",
		"ui/src/lib/other.ts":         "ts/other.ts",
		"ui/src/test/helper.ts":       "ts/test/helper.ts",
	})
}

func TestTSFindsCalls(t *testing.T) {
	msgs, _, err := TS(tsRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Line one\nline two",
		"Method form",
		"Multi-line call %s",
		`Say 'hi' to "them"`,
		"Shared",
		"Unicode été",
		"a",
		"b",
		"c",
		"d",
	}
	if got := ids(msgs); !reflect.DeepEqual(got, want) {
		t.Errorf("ids =\n%q\nwant\n%q", got, want)
	}
	if got := refsOf(msgs, "Multi-line call %s"); !reflect.DeepEqual(got, []string{"ui/src/pages/calls.tsx:20"}) {
		t.Errorf("multi-line call refs = %q", got)
	}
}

func TestTSReportsTemplateWithExpr(t *testing.T) {
	_, problems, err := TS(tsRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{
		{File: "ui/src/pages/calls.tsx", Line: 23, Text: "`Hello ${name}`"},
		{File: "ui/src/pages/calls.tsx", Line: 24, Text: "'a' + …"},
	}
	if !reflect.DeepEqual(problems, want) {
		t.Errorf("problems = %+v, want %+v", problems, want)
	}
}

func TestTSSkipsTests(t *testing.T) {
	msgs, _, err := TS(tsRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids(msgs) {
		if strings.HasPrefix(id, "Test") || strings.HasPrefix(id, "In a") {
			t.Errorf("extracted %q", id)
		}
	}
}

func TestRefsAreRepoRelativeAndSorted(t *testing.T) {
	msgs, _, err := TS(tsRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ui/src/lib/other.ts:1", "ui/src/pages/calls.tsx:6"}
	if got := refsOf(msgs, "Shared"); !reflect.DeepEqual(got, want) {
		t.Errorf("Shared refs = %q, want %q", got, want)
	}
	gm, _, err := Go(goRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := refsOf(gm, "Step message"); !reflect.DeepEqual(got, []string{"hostisan/internal/fx/sinks.go:28"}) {
		t.Errorf("Step message refs = %q", got)
	}
}

const hestiaPot = `msgid ""
msgstr ""

msgid "Log out"
msgstr ""

msgid "Cron Jobs"
msgstr ""

msgid "CRON JOBS"
msgstr ""

msgctxt "menu"
msgid "Web"
msgstr ""
`

func TestTemplateExcludesExactHestia(t *testing.T) {
	out, err := Template([]byte(hestiaPot), []Msg{
		{ID: "Log out", Refs: []string{"ui/src/a.tsx:1"}},
		{ID: "Web", Refs: []string{"ui/src/a.tsx:2"}},
		{ID: "New thing", Refs: []string{"ui/src/a.tsx:3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := i18n.MsgIDs(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"New thing"}) {
		t.Fatalf("template ids = %q", got)
	}
}

func TestTemplateNotesCaseTwin(t *testing.T) {
	out, err := Template([]byte(hestiaPot), []Msg{{ID: "Cron  jobs", Refs: []string{"ui/src/a.tsx:9", "hostisan/internal/x.go:3"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "#. Hestia: \"CRON JOBS\"\n#: hostisan/internal/x.go\n#: ui/src/a.tsx\nmsgid \"Cron  jobs\"\nmsgstr \"\"\n"
	if !strings.Contains(string(out), want) {
		t.Fatalf("template:\n%s\nwant entry:\n%s", out, want)
	}
}

func TestTemplateIsDeterministic(t *testing.T) {
	msgs := []Msg{
		{ID: "b", Refs: []string{"ui/src/b.tsx:2", "ui/src/a.tsx:1"}},
		{ID: "a", Refs: []string{"ui/src/a.tsx:5"}},
		{ID: "c", Refs: []string{"ui/src/a.tsx:1"}},
	}
	first, err := Template([]byte(hestiaPot), msgs)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID > msgs[j].ID })
	second, _ := Template([]byte(hestiaPot), msgs)
	if string(first) != string(second) {
		t.Fatalf("order changed the output:\n%s\n---\n%s", first, second)
	}
	if got, _ := i18n.MsgIDs(first); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("not sorted: %q", got)
	}
	if !strings.HasPrefix(string(first), "# Hostisan") || !strings.Contains(string(first), "\"Content-Type: text/plain; charset=UTF-8\\n\"\n") {
		t.Fatalf("header:\n%s", first)
	}
}

func TestTemplateEscapesLikeGettext(t *testing.T) {
	long := "This message is long enough that gettext wraps it over several lines, breaking after spaces."
	msgs := []Msg{
		{ID: `Say "hi" \ there`, Refs: []string{"ui/src/a.tsx:1"}},
		{ID: "One\nTwo", Refs: []string{"ui/src/a.tsx:2"}},
		{ID: long, Refs: []string{"ui/src/a.tsx:3"}},
		{ID: "Tab\there", Refs: []string{"ui/src/a.tsx:4"}},
	}
	out, err := Template([]byte(hestiaPot), msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"msgid \"Say \\\"hi\\\" \\\\ there\"\n",
		"msgid \"\"\n\"One\\n\"\n\"Two\"\n",
		"msgid \"\"\n\"This message is long enough that gettext wraps it over several lines, \"\n\"breaking after spaces.\"\n",
		"msgid \"Tab\\there\"\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing\n%s\nin\n%s", want, s)
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if len(line) > 79 {
			t.Errorf("line over 79 columns: %q", line)
		}
	}
	got, err := i18n.MsgIDs(out)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"One\nTwo", `Say "hi" \ there`, "Tab\there", long}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %q", got)
	}
}

const mergePot = `# Hostisan messages
msgid ""
msgstr ""
"Content-Type: text/plain; charset=UTF-8\n"

#: ui/src/a.tsx
msgid "Apple"
msgstr ""

#. Hestia: "BANANA"
#: ui/src/b.tsx
msgid "Banana"
msgstr ""

#: ui/src/c.tsx
msgid "Cherry"
msgstr ""
`

const mergePO = `# French translation.
msgid ""
msgstr ""
"Language: fr\n"
"Content-Type: text/plain; charset=UTF-8\n"

# Checked by Marie.
#: ui/src/old.tsx
msgid "Apple"
msgstr "Pomme"

msgid "Durian"
msgstr "Durian"

msgid "Gone untranslated"
msgstr ""

#~ msgid "Cherry"
#~ msgstr "Cerise"
`

func TestMergeKeepsTranslationsAddsNewMarksObsolete(t *testing.T) {
	out, err := Merge([]byte(mergePot), []byte(mergePO))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := i18n.ReadPO(out)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id, str  string
		obsolete bool
	}
	var got []row
	for _, e := range entries {
		got = append(got, row{e.ID, strings.Join(e.Strs, "|"), e.Obsolete})
	}
	want := []row{
		{"", "Language: fr\nContent-Type: text/plain; charset=UTF-8\n", false},
		{"Apple", "Pomme", false},
		{"Banana", "", false},
		{"Cherry", "Cerise", false}, // an obsolete translation comes back
		{"Durian", "Durian", true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged =\n%+v\nwant\n%+v\n%s", got, want, out)
	}
	if !strings.Contains(string(out), "#~ msgid \"Durian\"\n#~ msgstr \"Durian\"\n") {
		t.Fatalf("obsolete entry not written with #~:\n%s", out)
	}
	if !strings.Contains(string(out), "#: ui/src/a.tsx\nmsgid \"Apple\"") || strings.Contains(string(out), "old.tsx") {
		t.Fatalf("references not taken from the template:\n%s", out)
	}
	again, _ := Merge([]byte(mergePot), out)
	if string(again) != string(out) {
		t.Fatalf("merge is not idempotent:\n%s\n---\n%s", out, again)
	}
}

func TestMergeKeepsTranslatorComments(t *testing.T) {
	out, err := Merge([]byte(mergePot), []byte(mergePO))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "# French translation.\n") {
		t.Errorf("header comment lost:\n%s", s)
	}
	if !strings.Contains(s, "# Checked by Marie.\n#: ui/src/a.tsx\nmsgid \"Apple\"\nmsgstr \"Pomme\"\n") {
		t.Errorf("translator comment lost:\n%s", s)
	}
	if !strings.Contains(s, "#. Hestia: \"BANANA\"\n#: ui/src/b.tsx\nmsgid \"Banana\"") {
		t.Errorf("extracted comment lost:\n%s", s)
	}
}

func TestStatsCountsHestiaTwinsAsCovered(t *testing.T) {
	pot := "msgid \"\"\nmsgstr \"\"\n\nmsgid \"Cron jobs\"\nmsgstr \"\"\n\nmsgid \"New\"\nmsgstr \"\"\n\nmsgid \"Other\"\nmsgstr \"\"\n"
	po := map[string][]byte{"fr": []byte("msgid \"New\"\nmsgstr \"Nouveau\"\n\n#, fuzzy\nmsgid \"Other\"\nmsgstr \"Autre\"\n")}
	hestia := map[string]map[string]string{
		"fr": {"Cron Jobs": "Tâches cron", "Log out": "Déconnexion"},
		"de": {"Log out": "Abmelden"},
	}
	got := Stats([]byte(pot), po, hestia)
	want := []LangStat{
		{Lang: "de", Total: 3, PO: 0, Twin: 0, Hestia: 1},
		{Lang: "fr", Total: 3, PO: 1, Twin: 1, Hestia: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Stats = %+v, want %+v", got, want)
	}
}

func TestGoFindsHelperSinks(t *testing.T) {
	root := repo(t, map[string]string{
		"hostisan/internal/fx/helpers.go":        "go/helpers.go.txt",
		"hostisan/internal/terminal/terminal.go": "go/terminal.go.txt",
	})
	msgs, problems, err := Go(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Bad request helper.",
		"Blank label",
		"Fail helper message.",
		"First label",
		"Invalid helper message %s.",
		"Job step",
		"Job title",
		"Literal close reason.",
		"Network helper message.",
		"Planner add",
		"Planner config",
		"Planner yes no",
		"Reject helper message.",
		"Restore files",
		"Second label",
		"Toggle off",
		"Toggle on",
		"Too many terminals.",
		"Used by a sink.",
	}
	if got := ids(msgs); !reflect.DeepEqual(got, want) {
		t.Errorf("ids =\n%q\nwant\n%q", got, want)
	}
	if len(problems) != 0 {
		t.Errorf("problems = %+v", problems)
	}
	// A constant is referenced where the sink uses it.
	if got := refsOf(msgs, "Restore files"); !reflect.DeepEqual(got, []string{"hostisan/internal/fx/helpers.go:31"}) {
		t.Errorf("Restore files refs = %q", got)
	}
}
