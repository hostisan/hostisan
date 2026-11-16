package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hostisan/hostisan/internal/runner"
)

// fixtureEntry mirrors one recorded call in a fixture file.
type fixtureEntry struct {
	Args   []string `json:"args"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr,omitempty"`
	Exit   int      `json:"exit"`
}

// overrides changes the dev fixtures for one test: Dir's <cmd>.json entries go ahead
// of the dev entries for that command (a slice's own fixture directory, so its entries
// win), Fixtures replaces every entry of the named commands, and Config sets keys in
// v-list-sys-config's "config" object. They apply in that order.
type overrides struct {
	Dir      string
	Fixtures map[string][]fixtureEntry
	Config   map[string]string
	// BackupDir becomes Deps.BackupDir (where the core writes backups and dumps).
	BackupDir string
}

// fixtureDir copies the dev fixtures to a temp dir and applies o.
func fixtureDir(t *testing.T, o overrides) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "fixtures", "dev")
	dst := t.TempDir()
	files, err := filepath.Glob(filepath.Join(src, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dst, filepath.Base(f)), data)
	}
	if o.Dir != "" {
		extra, err := filepath.Glob(filepath.Join(o.Dir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range extra {
			var first, rest []fixtureEntry
			readEntries(t, f, &first)
			if dev := filepath.Join(dst, filepath.Base(f)); fileExists(dev) {
				readEntries(t, dev, &rest)
			}
			data, _ := json.Marshal(append(first, rest...))
			writeFile(t, filepath.Join(dst, filepath.Base(f)), data)
		}
	}
	for cmd, entries := range o.Fixtures {
		data, _ := json.Marshal(entries)
		writeFile(t, filepath.Join(dst, cmd+".json"), data)
	}
	if len(o.Config) > 0 {
		path := filepath.Join(dst, "v-list-sys-config.json")
		data, _ := os.ReadFile(path)
		var entries []fixtureEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			t.Fatal(err)
		}
		for i := range entries {
			var doc struct {
				Config map[string]string `json:"config"`
			}
			if err := json.Unmarshal([]byte(entries[i].Stdout), &doc); err != nil {
				t.Fatal(err)
			}
			for k, v := range o.Config {
				doc.Config[k] = v
			}
			out, _ := json.Marshal(doc)
			entries[i].Stdout = string(out)
		}
		data, _ = json.Marshal(entries)
		writeFile(t, path, data)
	}
	return dst
}

func readEntries(t *testing.T, path string, into *[]fixtureEntry) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessOverrides(t *testing.T) {
	h := newHarnessWith(t, overrides{
		Fixtures: map[string][]fixtureEntry{
			"v-list-dns-domain": {{Args: []string{"*", "*", "json"}, Stdout: "Error: boom\n", Exit: 4}},
		},
		Config: map[string]string{"POLICY_USER_EDIT_DNS_TEMPLATES": "yes"},
	})
	cfg, err := h.deps.H.Config(context.Background())
	if err != nil || cfg["POLICY_USER_EDIT_DNS_TEMPLATES"] != "yes" || cfg["DNS_SYSTEM"] != "bind9" {
		t.Fatalf("config = %v, %v", cfg, err)
	}
	res, err := h.fixture.Run(context.Background(), "v-list-dns-domain", "admin", "example.com", "json")
	if err != nil || res.Exit != 4 || string(res.Stdout) != "Error: boom\n" {
		t.Fatalf("override not applied: %+v, %v", res, err)
	}
}

// loginOps signs in as ops, a non-root admin (ROOT_USER is admin) without 2FA.
func (h *harness) loginOps() {
	h.t.Helper()
	h.bootstrap()
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "ops", Password: "pw"}, nil); code != 200 {
		h.t.Fatalf("ops login: %d", code)
	}
}

// recordedEntry is the first entry for args in the real recording of cmd (Ubuntu
// 24.04, everything on: testdata/fixtures/recorded/everything-ubuntu2404).
func recordedEntry(t *testing.T, cmd string, args ...string) fixtureEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "recorded", "everything-ubuntu2404", cmd+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var list []fixtureEntry
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if slices.Equal(e.Args, args) {
			return e
		}
	}
	t.Fatalf("no recorded %s %q", cmd, args)
	return fixtureEntry{}
}

// stablePasswords answers the session check's `v-get-user-value USER MD5` (the
// password mark, session.Manager.Validate) itself, with a hash that never changes,
// so the call lists the tests compare hold only the commands under test. Every
// other command goes to the fixtures. The session package tests the mark.
type stablePasswords struct{ runner.Runner }

func (p stablePasswords) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == "v-get-user-value" && len(args) == 2 && args[1] == "MD5" {
		return runner.Result{Stdout: []byte("$6$test$" + args[0] + "\n")}, nil
	}
	return p.Runner.Run(ctx, cmd, args...)
}
