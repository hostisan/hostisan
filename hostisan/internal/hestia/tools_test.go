package hestia

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// devSearch reads the dev v-search-object fixture recorded for args.
func devSearch(t *testing.T, args ...string) (string, int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "dev", "v-search-object.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Args   []string `json:"args"`
		Stdout string   `json:"stdout"`
		Exit   int      `json:"exit"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Join(e.Args, "\x00") == strings.Join(args, "\x00") {
			return e.Stdout, e.Exit
		}
	}
	t.Fatalf("no v-search-object fixture for %q", args)
	return "", 0
}

// _source: bin/v-search-object:31-85 (json_list_search): the ID field is shifted off
// and used only as the object key, values have "," replaced by ", ", and an empty
// result prints "{", an empty line and "}".
func TestDecodeSearchRealShape(t *testing.T) {
	out, exit := devSearch(t, "example", "json")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if strings.Contains(out, `"ID"`) {
		t.Fatal("the core's search JSON has no ID field")
	}
	if !strings.HasPrefix(out, "{\n\t\"1\": {\n\t\t\"USER\": ") || !strings.HasSuffix(out, "\n\t}\n}\n") {
		t.Fatalf("not json_list_search's byte shape: %q", out)
	}
	hits, err := DecodeSearch([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	// Only object names match (DOMAIN, RECORD, ACCOUNT, DB, CMD): no record or account
	// of bob's is named "example".
	want := []string{
		"admin web DOMAIN example.net", "bob web DOMAIN bakery.example", "bob dns DOMAIN bakery.example",
		"bob mail DOMAIN bakery.example", "bob cron JOB wget -q -O /dev/null https://bakery.example/",
	}
	if len(hits) != len(want) {
		t.Fatalf("%d hits, want %d", len(hits), len(want))
	}
	for i, h := range hits {
		if got := h.USER + " " + h.TYPE + " " + h.KEY + " " + h.RESULT; got != want[i] || h.ID != string(rune('1'+i)) {
			t.Errorf("hit %d = %q (ID %q), want %q", i, got, h.ID, want[i])
		}
	}
	if hits[0].SUSPENDED != "yes" || hits[1].ALIAS != "www.bakery.example" || hits[4].LINK != "2" {
		t.Errorf("hits = %+v", hits)
	}

	// "www": bob's web alias, and record 3 of example.com whose zone the core takes
	// from the line parsed before it (SR33: bakery.example, from web.conf).
	www, _ := devSearch(t, "www", "json")
	hits, err = DecodeSearch([]byte(www))
	if err != nil || len(hits) != 2 {
		t.Fatalf("www: %v, %v", hits, err)
	}
	if r := hits[1]; r.KEY != "RECORD" || r.RESULT != "www.bakery.example" || r.LINK != "3" || r.PARENT != "bakery.example" {
		t.Errorf("www record = %+v", r)
	}
	// "info": the account row takes its domain from the file name, not a stale value.
	info, _ := devSearch(t, "info", "json")
	hits, err = DecodeSearch([]byte(info))
	if err != nil || len(hits) != 1 || hits[0].RESULT != "info@bakery.example" || hits[0].LINK != "info" || hits[0].PARENT != "bakery.example" {
		t.Fatalf("info: %+v, %v", hits, err)
	}

	// The core turns every "," in a value into ", " (json_list_search, "${value//,/, }").
	hits, err = DecodeSearch([]byte("{\n\t\"1\": {\n\t\t\"USER\": \"bob\",\n\t\t\"ALIAS\": \"a.example, b.example\"\n\t}\n}\n"))
	if err != nil || len(hits) != 1 || hits[0].ALIAS != "a.example, b.example" || hits[0].ID != "1" {
		t.Fatalf("aliases: %+v, %v", hits, err)
	}

	// An empty query prints the empty object (the unquoted grep matches nothing).
	if all, _ := devSearch(t, "", "json"); all != "{\n\n}\n" {
		t.Errorf(`"" = %q`, all)
	}

	empty, _ := devSearch(t, "zzz-none", "json")
	if empty != "{\n\n}\n" {
		t.Fatalf("empty fixture = %q", empty)
	}
	hits, err = DecodeSearch([]byte(empty))
	if err != nil || len(hits) != 0 || hits == nil {
		t.Fatalf("empty = %v, %v; want [] and no error", hits, err)
	}
}

func TestValidSearchQuery(t *testing.T) {
	for _, q := range []string{"", "example.com", "a|b", "a b", "under_score-x", strings.Repeat("a", 50)} {
		if !ValidSearchQuery(q) {
			t.Errorf("%q refused", q)
		}
	}
	for _, q := range []string{"-v", "x;y", "ä", strings.Repeat("a", 51), "a:b", "a/b", "a\tb", "*"} {
		if ValidSearchQuery(q) {
			t.Errorf("%q accepted", q)
		}
	}
}

func TestValidRestorePath(t *testing.T) {
	for _, p := range []string{
		"/home/bob/web/x.com/public_html/index.php",
		"/home/bob/a",
		"/home/bob/web/x.com/public_html",
		"/home/bob/.bashrc",
		"/home/bob/web/a-b/c.d",
	} {
		if !ValidRestorePath("bob", p) {
			t.Errorf("%q refused", p)
		}
	}
	long := "/home/bob/" + strings.Repeat("a", 4097-len("/home/bob/"))
	for _, p := range []string{
		"/home/bob", "/home/bob/", "/home/bob/conf", "/home/bob/conf/x", "/home/bob2/x", "/home/bob/../alice",
		"/home/bob/./x", "/home/bob//x", "/home/bob/a b", "/home/bob/*.php", "/home/bob/a?", "/home/bob/[a]",
		`/home/bob/a\b`, "/home/bob/-x/y", "/home/bob/x/-y", "home/bob/x", "/home/bob/x/", long,
		"/home/bob/a\nb", "/home/bob/a\x00b", "/home/bob/a\tb", "/home/alice/x", "",
	} {
		if ValidRestorePath("bob", p) {
			t.Errorf("%q accepted", p)
		}
	}
	if !ValidRestorePath("bob", "/home/bob/"+strings.Repeat("a", 4096-len("/home/bob/"))) {
		t.Error("a 4096-byte path refused")
	}
	if ValidRestorePath("", "/home//x") || ValidRestorePath("../x", "/home/../x/y") {
		t.Error("a bad user accepted")
	}
}

func TestUserShell(t *testing.T) {
	if got := (User{"SHELL": "bash"}).Shell(); got != "bash" {
		t.Fatalf("Shell() = %q", got)
	}
}
