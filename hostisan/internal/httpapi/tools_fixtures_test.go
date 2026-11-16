package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

// The Plan 5 dev fixtures, checked against the shapes the core prints. Each case
// names the bin lines it was written from (_source).
func TestToolsDevFixtures(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run := func(cmd string, args ...string) runner.Result {
		t.Helper()
		res, err := h.fixture.RunAny(ctx, cmd, args...)
		if err != nil {
			t.Fatalf("%s %v: %v", cmd, args, err)
		}
		return res
	}
	expect := func(res runner.Result, stdout string, exit int) {
		t.Helper()
		if string(res.Stdout) != stdout || res.Exit != exit {
			t.Fatalf("got %q exit %d, want %q exit %d", res.Stdout, res.Exit, stdout, exit)
		}
	}

	// _source: bin/v-search-object:126 (is_format_valid 'object' ->
	// is_object_name_format_valid, func/main.sh check_result E_INVALID), and
	// parse_object_kv_list_non_eval's refusal (func/main.sh:449-450) for a valid query
	// that hits a conf line it can't read ("unreadable", illustrative);
	// bin/v-search-user-object: is_object_valid 'user'. Values with colons no longer
	// stop the search (SR32, core edit B2g): "2001" only matches a record value, so
	// it finds nothing.
	expect(run("v-search-object", "bad:x", "json"), "Error: invalid object format :: bad:x\n", 2)
	expect(run("v-search-user-object", "bob", "bad:x", "json"), "Error: invalid object format :: bad:x\n", 2)
	if res := run("v-search-object", "unreadable", "json"); res.Exit != 2 ||
		!strings.HasPrefix(string(res.Stdout), "Error: Invalid key value format [") {
		t.Fatalf("unreadable: %+v", res)
	}
	expect(run("v-search-object", "2001", "json"), "{\n\n}\n", 0)
	expect(run("v-search-user-object", "bob", "2001", "json"), "{\n\n}\n", 0)
	expect(run("v-search-user-object", "admin", "zzz-none", "json"), "{\n\n}\n", 0)
	expect(run("v-search-user-object", "nobody", "example", "json"), "Error: user nobody doesn't exist\n", 3)
	for user, n := range map[string]int{"admin": 1, "bob": 4} {
		if out := string(run("v-search-user-object", user, "", "json").Stdout); out != "{\n\n}\n" {
			t.Fatalf(`%s "" = %q`, user, out)
		}
		hits, err := hestia.DecodeSearch(run("v-search-user-object", user, "example", "json").Stdout)
		if err != nil || len(hits) != n || hits[0].ID != "1" {
			t.Fatalf("%s: %d hits (%v), want %d numbered from 1", user, len(hits), err, n)
		}
		for _, hit := range hits {
			if hit.USER != user {
				t.Fatalf("%s's search returned %s's row", user, hit.USER)
			}
		}
	}
	if _, err := h.fixture.Run(ctx, "v-search-object", "-v", "json"); err == nil {
		t.Fatal(`"-v" has a fixture; the API refuses it before any command`)
	}

	// _source: bin/v-search-domain-owner:47-53: the owner and a newline; none: exit 3, no output.
	expect(run("v-search-domain-owner", "bakery.example", "mail"), "bob\n", 0)
	expect(run("v-search-domain-owner", "example.com", "mail"), "admin\n", 0)
	expect(run("v-search-domain-owner", "nowhere.example", "mail"), "", 3)

	// _source: bin/v-get-mail-account-value:47-56 (get_object_value echoes $MD5). The
	// hash was generated with: openssl passwd -6 -salt devsalt devpass
	res := run("v-get-mail-account-value", "bob", "bakery.example", "info", "md5")
	if !strings.HasPrefix(string(res.Stdout), "{SHA512-CRYPT}$6$devsalt$") || res.Exit != 0 ||
		!strings.HasSuffix(string(res.Stdout), "\n") {
		t.Fatalf("mail hash = %q", res.Stdout)
	}

	// _source: bin/v-restore-file-restic:51-56 (check_result E_NOTEXIST).
	expect(run("v-restore-file-restic", "bob", "deadbeef", "/home/bob/x"), "Error: Unable to download snapshot data\n", 3)
	if res := run("v-restore-file-restic", "bob", "3c9b8a7e", "/home/bob/x"); res.Exit != 0 {
		t.Fatalf("restore: %+v", res)
	}

	// _source: bin/v-check-access-key:36-44 (json_list) and abort_missmatch (exit 10
	// for E_FORBIDEN, else 9).
	expect(run("v-check-access-key", "Zx9yWv8uTs7rQp6oNm5l", strings.Repeat("a", 40), "v-list-users", "127.0.0.1", "json"),
		`{"USER": "admin", "USER_ARG_POSITION": 0}`+"\n", 0)
	expect(run("v-check-access-key", "Bb0bKey0123456789abc", strings.Repeat("b", 40), "v-add-web-domain", "127.0.0.1", "json"),
		`{"USER": "bob", "USER_ARG_POSITION": 1}`+"\n", 0)
	expect(run("v-check-access-key", "Bb0bKey0123456789abc", strings.Repeat("b", 40), "v-list-users", "127.0.0.1", "json"),
		"Error: Key Bb0bKey0123456789abc don't have permission to run the command v-list-users\n", 10)
	// A non-root key whose permissions don't list v-make-tmp-file (ruling R7).
	expect(run("v-check-access-key", "Bb0bKey0123456789abc", strings.Repeat("b", 40), "v-make-tmp-file", "127.0.0.1", "json"),
		"Error: Key Bb0bKey0123456789abc don't have permission to run the command v-make-tmp-file\n", 10)
	// is_object_valid 'key' (func/main.sh:419-424) names the key.
	expect(run("v-check-access-key", "Mi55ingKey0123456789", strings.Repeat("a", 40), "v-list-users", "127.0.0.1", "json"),
		"Error: key Mi55ingKey0123456789 doesn't exist\n", 9)
	expect(run("v-check-access-key", "Zx9yWv8uTs7rQp6oNm5l", "wrong", "v-list-users", "127.0.0.1", "json"),
		"Error: Invalid secret key for key Zx9yWv8uTs7rQp6oNm5l\n", 9)
	var ok struct {
		USER              string
		USER_ARG_POSITION int
	}
	if err := json.Unmarshal(run("v-check-access-key", "Zx9yWv8uTs7rQp6oNm5l", strings.Repeat("a", 40), "v-x", "1.2.3.4", "json").Stdout, &ok); err != nil || ok.USER != "admin" {
		t.Fatalf("access key json: %+v, %v", ok, err)
	}

	// _source: bin/v-check-api-key:23-27 (abort_missmatch, E_PASSWORD).
	expect(run("v-check-api-key", "/usr/local/hestia/data/keys/devapikey0123456789", "127.0.0.1"), "", 0)
	expect(run("v-check-api-key", "/usr/local/hestia/data/keys/x", "127.0.0.1"), "Error: key missmatch\n", 9)

	// _source: bin/v-add-user-sftp-key:76-83.
	expect(run("v-add-user-sftp-key", "susp", "30"), "Error: 'setfacl' is not available.\n", 1)
	expect(run("v-add-user-sftp-key", "admin", "30"), "", 0)

	// bob's DNS zone bakery.example holds record 5 "www" (the search's record row);
	// example.com is admin's, never bob's.
	recs, err := hestia.DecodeObjects[hestia.DNSRecord](run("v-list-dns-records", "bob", "bakery.example", "json").Stdout)
	if err != nil || len(recs) != 5 || recs[4].ID != "5" || recs[4].RECORD != "www" {
		t.Fatalf("bob bakery.example records = %+v, %v", recs, err)
	}
	if res := run("v-list-dns-domain", "bob", "example.com", "json"); res.Exit == 0 {
		t.Fatalf("bob example.com zone: %+v", res)
	}
	zones, err := hestia.DecodeObjects[hestia.DNSDomain](run("v-list-dns-domains", "bob", "json").Stdout)
	if err != nil || len(zones) != 1 || zones[0].Name != "bakery.example" {
		t.Fatalf("bob zones = %+v, %v", zones, err)
	}

	// _source: bin/v-add-database-temp-user:73-78; off the allowlist, RunAny only.
	if _, err := h.fixture.Run(ctx, "v-add-database-temp-user", "bob", "bob_db", "mysql", "localhost"); err == nil {
		t.Fatal("v-add-database-temp-user ran through Run")
	}
	var temp struct {
		Login struct{ User, Password string } `json:"login"`
	}
	if err := json.Unmarshal(run("v-add-database-temp-user", "bob", "bob_db", "mysql", "localhost").Stdout, &temp); err != nil ||
		!strings.HasPrefix(temp.Login.User, "hestia_sso_") || temp.Login.Password == "" {
		t.Fatalf("temp user = %+v, %v", temp, err)
	}
	expect(run("v-delete-database-temp-user", "bob", "bob_db", "hestia_sso_Xq7Lm2Pz9K", "mysql", "localhost"), "", 0)
}

// carol has a bash shell and the "empty" package: every section off.
func TestDevCarol(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "carol", Password: "pw"}, nil); code != 200 {
		t.Fatalf("carol login: %d", code)
	}
	b := h.bootstrap()
	f := b.User.Features
	if b.User.Package != "empty" || f.Web || f.DNS || f.Mail || f.DB || f.Cron || f.Backup || !f.Terminal || !f.FileManager {
		t.Fatalf("carol = %q %+v", b.User.Package, f)
	}
	res, err := h.fixture.Run(context.Background(), "v-list-user-package", "empty", "json")
	if err != nil || res.Exit != 0 {
		t.Fatalf("empty package: %+v, %v", res, err)
	}
	var pkg map[string]map[string]string
	if err := json.Unmarshal(res.Stdout, &pkg); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"WEB_DOMAINS", "DNS_DOMAINS", "MAIL_DOMAINS", "DATABASES", "CRON_JOBS", "BACKUPS"} {
		if pkg["empty"][k] != "0" {
			t.Errorf("empty package %s = %q", k, pkg["empty"][k])
		}
	}
}
