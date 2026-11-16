package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixtureDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFixtureExactMatch(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-list-user": `[{"args":["admin","json"],"stdout":"{\"admin\":{}}\n","exit":0},
		                 {"args":["ghost","json"],"stdout":"Error: no user\n","exit":3}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Run(context.Background(), "v-list-user", "ghost", "json")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "Error: no user\n" || res.Exit != 3 {
		t.Fatalf("got %q exit %d", res.Stdout, res.Exit)
	}
}

func TestFixtureWildcard(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-check-user-password": `[{"args":["admin","*","*","yes"],"stdout":"fixture-hash\n","exit":0}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Run(context.Background(), "v-check-user-password", "admin", "/tmp/x", "1.2.3.4", "yes")
	if err != nil || string(res.Stdout) != "fixture-hash\n" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
}

func TestFixtureMissingIsLoud(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-list-user": `[{"args":["admin","json"],"stdout":"{}","exit":0}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Run(context.Background(), "v-list-user", "bob", "json")
	if !errors.Is(err, ErrNoFixture) {
		t.Fatalf("err = %v, want ErrNoFixture", err)
	}
	if !strings.Contains(err.Error(), "v-list-user [bob json]") {
		t.Fatalf("message %q should name the command and args", err)
	}
}

func TestFixtureHonoursAllowlist(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"rm": `[{"args":["*","*"],"stdout":"","exit":0}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Run(context.Background(), "rm", "-rf", "/"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
}

func TestFixtureRecordsCalls(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-list-user":       `[{"args":["*","json"],"stdout":"{}","exit":0}]`,
		"v-list-sys-config": `[{"args":["json"],"stdout":"{}","exit":0}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _ = f.Run(ctx, "v-list-sys-config", "json")
	_, _ = f.Run(ctx, "v-list-user", "admin", "json")
	want := []Call{{Cmd: "v-list-sys-config", Args: []string{"json"}}, {Cmd: "v-list-user", Args: []string{"admin", "json"}}}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Calls() = %v, want %v", got, want)
	}
}

// The hand-written dev fixtures must parse and cover every allowed command, so
// `hostisan serve --fixtures` never hits a missing command during development.
func TestDevFixturesCoverAllowlist(t *testing.T) {
	f, err := LoadFixtures(filepath.Join("..", "..", "testdata", "fixtures", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	for cmd := range allowed {
		if len(f.entries[cmd]) == 0 {
			t.Errorf("no dev fixture for %s", cmd)
		}
	}
}

func TestFixtureStderr(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-list-user": `[{"args":["bob","json"],"stdout":"{}\n","stderr":"Fatal: x\n","exit":0}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Run(context.Background(), "v-list-user", "bob", "json")
	if err != nil || string(res.Stdout) != "{}\n" || string(res.Stderr) != "Fatal: x\n" {
		t.Fatalf("got %q / %q, %v", res.Stdout, res.Stderr, err)
	}
}

// A secret temp file matches on its first line, as the scripts read it; Calls keeps
// the path. Other paths are matched as they are.
func TestFixtureSecretFileMatchesContent(t *testing.T) {
	f, err := LoadFixtures(fixtureDir(t, map[string]string{
		"v-check-api-key": `[{"args":["/usr/local/hestia/data/keys/good","*"],"stdout":"","exit":0},
		                     {"args":["*","*"],"stdout":"Error: key missmatch\n","exit":9}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	run := func(name, content string) Result {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := f.Run(context.Background(), "v-check-api-key", p, "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run("hostisan-1", "/usr/local/hestia/data/keys/good\n"); res.Exit != 0 {
		t.Errorf("secret file: exit %d", res.Exit)
	}
	if res := run("hostisan-2", "/usr/local/hestia/data/keys/bad\n"); res.Exit != 9 {
		t.Errorf("wrong secret file: exit %d", res.Exit)
	}
	if res := run("other", "/usr/local/hestia/data/keys/good\n"); res.Exit != 9 {
		t.Errorf("non-secret file read: exit %d", res.Exit)
	}
	if c := f.Calls()[0]; c.Args[0] != filepath.Join(dir, "hostisan-1") {
		t.Errorf("Calls() = %q, want the path", c.Args)
	}
}
