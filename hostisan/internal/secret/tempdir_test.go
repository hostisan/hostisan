package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTempDirModes(t *testing.T) {
	parent := t.TempDir()
	files := map[string]string{
		"example.com.crt": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
		"example.com.key": "-----BEGIN PRIVATE KEY-----\nMIGH\n-----END PRIVATE KEY-----",
	}
	dir, cleanup, err := TempDir(parent, files)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != parent {
		t.Errorf("dir %s is not in %s", dir, parent)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700 directory", st.Mode())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(files) {
		t.Errorf("dir holds %d files, want %d", len(entries), len(files))
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", name, st.Mode().Perm())
		}
		b, _ := os.ReadFile(p)
		if string(b) != content {
			t.Errorf("%s content = %q, want %q (as given)", name, b, content)
		}
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir still exists after cleanup: %v", err)
	}
	cleanup() // must not panic
}

func TestTempDirRejectsPathNames(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"", ".", "..", "../x.crt", "a/b.crt"} {
		if _, _, err := TempDir(parent, map[string]string{name: "x"}); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("rejected calls left %d entries behind", len(entries))
	}
}
