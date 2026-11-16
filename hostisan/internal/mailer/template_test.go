package mailer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateLookupOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fr", "greet.html"), "<subject>Salut {{u}}</subject>\nBonjour")
	write(t, filepath.Join(dir, "greet.html"), "<subject>Hello</subject>\nHi there")
	write(t, filepath.Join(dir, "only-root.html"), "no subject here")
	tp := Templates{Dir: dir}

	subj, body, ok := tp.Load("fr", "greet")
	if !ok || subj != "Salut {{u}}" || strings.Contains(body, "subject") || !strings.Contains(body, "Bonjour") {
		t.Fatalf("lang file: %q %q %v", subj, body, ok)
	}
	subj, body, ok = tp.Load("de", "greet")
	if !ok || subj != "Hello" || strings.Contains(body, "<subject>") || !strings.Contains(body, "Hi there") {
		t.Fatalf("fallback: %q %q %v", subj, body, ok)
	}
	subj, body, ok = tp.Load("en", "only-root")
	if !ok || subj != "" || body != "no subject here" {
		t.Fatalf("no subject: %q %q %v", subj, body, ok)
	}
	if _, _, ok = tp.Load("en", "missing"); ok {
		t.Fatal("missing template should give ok=false")
	}
}

func TestTemplateRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "x.html"), "secret")
	tp := Templates{Dir: filepath.Join(dir, "sub")}
	if _, _, ok := tp.Load("..", "x"); ok {
		t.Fatal("lang traversal accepted")
	}
	if _, _, ok := tp.Load("en", "../x"); ok {
		t.Fatal("name traversal accepted")
	}
}
