package hestia

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

func devClient(t *testing.T) (*Client, *runner.Fixture) {
	t.Helper()
	f, err := runner.LoadFixtures(filepath.Join("..", "..", "testdata", "fixtures", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	return &Client{R: f, ConfigTTL: time.Minute}, f
}

func TestConfigCachesWithinTTL(t *testing.T) {
	c, f := devClient(t)
	for range 2 {
		cfg, err := c.Config(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if cfg["VERSION"] != "1.10.5" || cfg["ROOT_USER"] != "admin" {
			t.Fatalf("unexpected config: VERSION=%q ROOT_USER=%q", cfg["VERSION"], cfg["ROOT_USER"])
		}
	}
	if n := len(f.Calls()); n != 1 {
		t.Fatalf("v-list-sys-config ran %d times, want 1 (cached)", n)
	}
}

func TestUserFound(t *testing.T) {
	c, _ := devClient(t)
	u, ok, err := c.User(context.Background(), "bob")
	if err != nil || !ok {
		t.Fatalf("User(bob) = ok %v, err %v", ok, err)
	}
	if u.Role() != "user" || !u.TwoFAEnabled() || u.Suspended() {
		t.Fatalf("bob: role %q 2fa %v suspended %v", u.Role(), u.TwoFAEnabled(), u.Suspended())
	}
}

func TestUserMissingReturnsFalse(t *testing.T) {
	c, _ := devClient(t)
	u, ok, err := c.User(context.Background(), "ghost")
	if err != nil || ok || u != nil {
		t.Fatalf("User(ghost) = %v, %v, %v; want nil, false, nil", u, ok, err)
	}
}

func TestLandingPathOrder(t *testing.T) {
	cases := []struct {
		user User
		want string
	}{
		{User{"WEB_DOMAINS": "1"}, "/web"},
		{User{"WEB_DOMAINS": "0", "DNS_DOMAINS": "5"}, "/dns"},
		{User{"WEB_DOMAINS": "0", "DNS_DOMAINS": "0", "MAIL_DOMAINS": "0", "DATABASES": "0", "CRON_JOBS": "0", "BACKUPS": "2"}, "/backup"},
		{User{"WEB_DOMAINS": "0", "DNS_DOMAINS": "0", "MAIL_DOMAINS": "0", "DATABASES": "0", "CRON_JOBS": "0", "BACKUPS": "0"}, "/error"},
	}
	for _, tc := range cases {
		if got := tc.user.LandingPath(); got != tc.want {
			t.Errorf("LandingPath(%v) = %q, want %q", tc.user, got, tc.want)
		}
	}
}

func TestUserHelpers(t *testing.T) {
	u := User{"LOGIN_USE_IPLIST": "yes", "LOGIN_ALLOW_IPS": " 203.0.113.24, 10.0.0.1 ", "PREF_UI_SORT": ""}
	if !u.UsesIPList() {
		t.Fatal("UsesIPList should be true")
	}
	if got := u.AllowedIPs(); len(got) != 2 || got[0] != "203.0.113.24" || got[1] != "10.0.0.1" {
		t.Fatalf("AllowedIPs = %v", got)
	}
	if u.SortOrder() != "name" {
		t.Fatalf("SortOrder default = %q, want name", u.SortOrder())
	}
}

func TestLanguages(t *testing.T) {
	c, _ := devClient(t)
	langs, err := c.Languages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(langs) < 40 || langs[0] != "en" {
		t.Fatalf("languages = %v", langs)
	}
}

func TestValidUsername(t *testing.T) {
	for _, ok := range []string{"admin", "bob.smith", "a1", "a", "web_user-2", strings.Repeat("a", 30)} {
		if !ValidUsername(ok) {
			t.Errorf("ValidUsername(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"-h", "admin;id", "../x", "", strings.Repeat("a", 31), "a|b", `a\b`, "bob.", "ünï", " admin"} {
		if ValidUsername(bad) {
			t.Errorf("ValidUsername(%q) = true, want false", bad)
		}
	}
}
