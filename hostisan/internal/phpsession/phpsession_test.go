package phpsession

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The expected strings are what PHP 8.3 writes with session.serialize_handler = php
// (hestia-php's setting, src/deb/php/php.ini:1435): `key|` followed by serialize()
// of the value, with no separator between keys. Strings count bytes, not characters,
// and nothing inside the quotes is escaped.
func TestPHPSessionEncode(t *testing.T) {
	cases := []struct {
		name string
		in   []KV
		want string
	}{
		{"brief", []KV{{"user", "bob"}, {"look", ""}, {"LAST_ACTIVITY", int64(1700000000)}},
			`user|s:3:"bob";look|s:0:"";LAST_ACTIVITY|i:1700000000;`},
		{"utf-8 counts bytes", []KV{{"language", "é"}}, `language|s:2:"é";`},
		{"no escaping", []KV{{"x", `a"|b;` + "\n"}}, "x|s:6:\"a\"|b;\n\";"},
		{"negative int", []KV{{"n", int64(-5)}}, `n|i:-5;`},
		{"empty", nil, ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(Encode(c.in)); got != c.want {
				t.Fatalf("Encode = %q, want %q", got, c.want)
			}
		})
	}
}

// A key PHP's php handler can't represent ("|" ends the key, "!" marks an unset
// one) or a value type the handoff never writes is a programming error.
func TestPHPSessionEncodeRefuses(t *testing.T) {
	for _, kv := range []KV{{"a|b", "x"}, {"a!b", "x"}, {"", "x"}, {"n", 5}, {"f", 1.5}, {"b", true}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Encode(%+v) did not panic", kv)
				}
			}()
			Encode([]KV{kv})
		}()
	}
}

func TestPHPSessionNewID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{64}$`)
	a, b := NewID(), NewID()
	if !re.MatchString(a) || !re.MatchString(b) || a == b {
		t.Fatalf("NewID = %q, %q", a, b)
	}
}

func TestPHPSessionWrite(t *testing.T) {
	dir := t.TempDir()
	id := NewID()
	data := Encode([]KV{{"user", "admin"}})
	if err := Write(dir, id, data); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess_"+id)
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want a regular 0600 file", fi.Mode())
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatalf("content = %q", got)
	}
	// Nothing else is left behind (the temp file was renamed), and no temp name
	// starts with sess_ (PHP would take it for a session).
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir holds %d entries", len(entries))
	}

	// Rewriting replaces the content.
	if err := Write(dir, id, []byte("x|i:1;")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x|i:1;" {
		t.Fatalf("rewrite: %q", got)
	}

	// A symlink planted at the target is replaced, not followed.
	victim := filepath.Join(t.TempDir(), "victim")
	writeFile(t, victim, "keep")
	id2 := NewID()
	if err := os.Symlink(victim, filepath.Join(dir, "sess_"+id2)); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, id2, []byte("y|i:2;")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Fatalf("the symlink target was written: %q", got)
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "sess_"+id2)); !fi.Mode().IsRegular() {
		t.Fatalf("sess_%s is %v", id2, fi.Mode())
	}
}

func TestPHPSessionBadIDs(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", "../x", strings.Repeat("a", 63), strings.Repeat("A", 64),
		strings.Repeat("a", 65), strings.Repeat("g", 64), "../" + strings.Repeat("a", 61)} {
		if err := Write(dir, id, []byte("x")); err == nil {
			t.Errorf("Write(%q) accepted", id)
		}
		if err := Delete(dir, id); err == nil {
			t.Errorf("Delete(%q) accepted", id)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("bad ids wrote %d files", len(entries))
	}
}

func TestPHPSessionDelete(t *testing.T) {
	dir := t.TempDir()
	id := NewID()
	if err := Write(dir, id, []byte("x|i:1;")); err != nil {
		t.Fatal(err)
	}
	if err := Delete(dir, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "sess_"+id)); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}
	// Already gone is not an error (logout after PHP removed it).
	if err := Delete(dir, id); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A restart forgets which panel session owned which file, so serve deletes every
// file the panel could have written: sess_<64 hex> and leftover temp files. Hestia's
// own 26-character sessions and anything else stay.
func TestPHPSessionSweep(t *testing.T) {
	dir := t.TempDir()
	ours := []string{"sess_" + NewID(), "sess_" + NewID(), ".hostisan-sess-123456"}
	keep := []string{
		"sess_abcdefghijklmnopqrstuv0123", // hestia-php's 26 characters
		"sess_" + strings.Repeat("a", 63),
		"sess_" + strings.Repeat("A", 64),
		"sess_" + strings.Repeat("a", 64) + ".bak",
		"other",
	}
	for _, n := range append(append([]string{}, ours...), keep...) {
		writeFile(t, filepath.Join(dir, n), "x")
	}
	if err := os.Mkdir(filepath.Join(dir, "sess_"+strings.Repeat("b", 64)+"x"), 0o700); err != nil {
		t.Fatal(err)
	}
	n, err := Sweep(dir)
	if err != nil || n != len(ours) {
		t.Fatalf("Sweep = %d, %v; want %d", n, err, len(ours))
	}
	for _, name := range ours {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s left behind", name)
		}
	}
	for _, name := range keep {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s removed: %v", name, err)
		}
	}
	// A missing directory (a dev machine) is not an error.
	if n, err := Sweep(filepath.Join(dir, "missing")); n != 0 || err != nil {
		t.Fatalf("missing dir: %d, %v", n, err)
	}
}
