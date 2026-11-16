package hestia

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func devFixtureStdout(t *testing.T, name string, args ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "dev", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Args   []string `json:"args"`
		Stdout string   `json:"stdout"`
	}
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if slices.Equal(e.Args, args) {
			return e.Stdout
		}
	}
	t.Fatalf("no fixture entry %s %v", name, args)
	return ""
}

func TestValidDatabaseArg(t *testing.T) {
	for _, s := range []string{"admin_wordpress", "bob_shop-2", strings.Repeat("a", 63)} {
		if !ValidDatabaseArg(s) {
			t.Errorf("%q should pass", s)
		}
	}
	for _, s := range []string{"-h", "", "a b", "x;y", "a/b", "a'b", strings.Repeat("a", 64), "a\nb", "a`b", "a%b", "a\\b", "a\tb"} {
		if ValidDatabaseArg(s) {
			t.Errorf("%q should fail", s)
		}
	}
}

func TestDatabaseFields(t *testing.T) {
	out := devFixtureStdout(t, "v-list-databases", "admin", "json")
	dbs, err := DecodeObjects[Database]([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 2 || dbs[0].DATABASE != "admin_wordpress" || dbs[1].DATABASE != "admin_reports" {
		t.Fatalf("got %+v", dbs)
	}
	w := dbs[0]
	if w.DBUSER != "admin_wpuser" || w.HOST != "localhost" || w.TYPE != "mysql" || w.CHARSET != "UTF8MB4" ||
		w.U_DISK != "12" || w.SUSPENDED != "no" || w.TIME != "10:22:41" || w.DATE != "2026-10-01" {
		t.Errorf("wordpress = %+v", w)
	}
	if dbs[1].SUSPENDED != "yes" || dbs[1].TYPE != "pgsql" {
		t.Errorf("reports = %+v", dbs[1])
	}
}

func TestDatabaseHostsDecode(t *testing.T) {
	out := devFixtureStdout(t, "v-list-database-hosts", "json")
	var hosts []DatabaseHost
	if err := json.Unmarshal([]byte(out), &hosts); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].TYPE != "mysql" || hosts[0].PORT != "3306" || hosts[1].TYPE != "pgsql" ||
		hosts[1].TPL != "template1" || hosts[0].MAX_DB != "500" {
		t.Errorf("hosts = %+v", hosts)
	}
}
