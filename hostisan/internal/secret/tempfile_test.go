package secret

import (
	"os"
	"testing"
)

func TestTempFileModeAndCleanup(t *testing.T) {
	path, cleanup, err := TempFile(t.TempDir(), "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if string(b) != "s3cret\n" {
		t.Errorf("content = %q", b)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after cleanup: %v", err)
	}
	cleanup() // must not panic
}
