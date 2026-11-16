package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Both runners satisfy AnyRunner (the legacy API's pass-through, decision 22).
var (
	_ AnyRunner = Sudo{}
	_ AnyRunner = (*Fixture)(nil)
)

func TestRunAny(t *testing.T) {
	s, marker := fakeHestia(t, map[string]string{"v-add-database-temp-user": `echo "$#|$1"`})
	ctx := context.Background()

	if Allowed("v-add-database-temp-user") {
		t.Fatal("the test command must be off the allowlist")
	}
	if !s.Exists("v-add-database-temp-user") {
		t.Fatal("Exists = false for a regular file in Bin")
	}
	res, err := s.RunAny(ctx, "v-add-database-temp-user", "bob", "a b")
	if err != nil || string(res.Stdout) != "2|bob\n" {
		t.Fatalf("RunAny = %q, %v", res.Stdout, err)
	}
	// Sudo.Run still refuses the same name.
	if _, err := s.Run(ctx, "v-add-database-temp-user", "bob"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("Run = %v, want ErrNotAllowed", err)
	}

	// Refusals: nothing reaches sudo.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Bin, "v-a-directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	write(t, target, "#!/bin/sh\necho linked\n")
	if err := os.Symlink(target, filepath.Join(s.Bin, "v-a-symlink")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(filepath.Dir(s.Bin), "v-outside"), "#!/bin/sh\necho out\n")
	for _, cmd := range []string{"v-missing", "v-a-directory", "v-a-symlink", "../x", "../v-outside",
		"v-x/../v-add-database-temp-user", "rm", "v-", "v-a;b", "v-a b", "", "-v-x"} {
		if s.Exists(cmd) {
			t.Errorf("Exists(%q) = true", cmd)
		}
		if _, err := s.RunAny(ctx, cmd); err == nil {
			t.Errorf("RunAny(%q) ran", cmd)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("sudo was invoked for a refused command")
	}
	if _, err := s.RunAny(ctx, "v-missing"); !errors.Is(err, ErrNoCommand) {
		t.Fatalf("missing file: %v, want ErrNoCommand", err)
	}
	if _, err := s.RunAny(ctx, "../x"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("bad name: %v, want ErrNotAllowed", err)
	}
}

func TestFixtureRunAny(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-add-database-temp-user": `[{"args":["bob","*","mysql","localhost"],"stdout":"{\"login\":{}}\n","exit":0}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !f.Exists("v-add-database-temp-user") || f.Exists("v-delete-database-temp-user") || f.Exists("../x") {
		t.Fatal("Exists is wrong")
	}
	res, err := f.RunAny(ctx, "v-add-database-temp-user", "bob", "bob_db", "mysql", "localhost")
	if err != nil || string(res.Stdout) != "{\"login\":{}}\n" {
		t.Fatalf("RunAny = %q, %v", res.Stdout, err)
	}
	if _, err := f.Run(ctx, "v-add-database-temp-user", "bob", "bob_db", "mysql", "localhost"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("Run = %v, want ErrNotAllowed", err)
	}
	if _, err := f.RunAny(ctx, "v-delete-database-temp-user", "bob"); !errors.Is(err, ErrNoCommand) {
		t.Fatalf("no fixture file: %v, want ErrNoCommand", err)
	}
	if _, err := f.RunAny(ctx, "../x"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("bad name: %v, want ErrNotAllowed", err)
	}
	want := []Call{{Cmd: "v-add-database-temp-user", Args: []string{"bob", "bob_db", "mysql", "localhost"}}}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Calls() = %v, want %v", got, want)
	}
}
