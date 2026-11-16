package httpapi

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// hookUserFixtures is the dev v-list-user fixture with fields changed per user.
func hookUserFixtures(t *testing.T, patches map[string]map[string]string) []fixtureEntry {
	t.Helper()
	var entries []fixtureEntry
	readEntries(t, filepath.Join("..", "..", "testdata", "fixtures", "dev", "v-list-user.json"), &entries)
	for i, e := range entries {
		if len(e.Args) != 2 || patches[e.Args[0]] == nil {
			continue
		}
		var doc map[string]map[string]string
		if err := json.Unmarshal([]byte(e.Stdout), &doc); err != nil {
			t.Fatal(err)
		}
		for k, v := range patches[e.Args[0]] {
			doc[e.Args[0]][k] = v
		}
		out, _ := json.Marshal(doc)
		entries[i].Stdout = string(out)
	}
	return entries
}

// hookFlags reads every flag the shared hook step added, by its JSON name.
var hookFlags = map[string]func(u *SessionUser) bool{
	"web":            func(u *SessionUser) bool { return u.Features.Web },
	"cron":           func(u *SessionUser) bool { return u.Features.Cron },
	"backup":         func(u *SessionUser) bool { return u.Features.Backup },
	"incremental":    func(u *SessionUser) bool { return u.Features.Incremental },
	"logs":           func(u *SessionUser) bool { return u.Features.Logs },
	"accessKeys":     func(u *SessionUser) bool { return u.Features.AccessKeys },
	"server":         func(u *SessionUser) bool { return u.Features.Server },
	"firewall":       func(u *SessionUser) bool { return u.Features.Firewall },
	"firewallBans":   func(u *SessionUser) bool { return u.Features.FirewallBans },
	"canEditDetails": func(u *SessionUser) bool { return u.CanEditDetails },
	"canChangeTheme": func(u *SessionUser) bool { return u.CanChangeTheme },
	"canDeleteLogs":  func(u *SessionUser) bool { return u.CanDeleteLogs },
	"notifications":  func(u *SessionUser) bool { return u.Notifications },
}

func TestBootstrapHookFlags(t *testing.T) {
	all := func(want bool, flags ...string) map[string]bool {
		m := map[string]bool{}
		for _, f := range flags {
			m[f] = want
		}
		return m
	}
	every := []string{"web", "cron", "backup", "incremental", "logs", "accessKeys", "server", "firewall",
		"firewallBans", "canEditDetails", "canChangeTheme", "canDeleteLogs", "notifications"}
	bobWant := all(true, "web", "cron", "backup", "logs", "accessKeys", "canEditDetails", "canChangeTheme", "canDeleteLogs")
	for f, v := range all(false, "incremental", "notifications", "server", "firewall", "firewallBans") {
		bobWant[f] = v
	}
	hide := map[string]string{"POLICY_SYSTEM_HIDE_SERVICES": "yes"}

	cases := []struct {
		name  string
		cfg   map[string]string
		users map[string]map[string]string
		who   string // admin, bob, ops, or admin-as-bob
		want  map[string]bool
	}{
		{name: "dev admin", who: "admin", want: all(true, every...)},
		{name: "dev bob", who: "bob", want: bobWant},
		{name: "dev ops", who: "ops", want: all(true, "server", "firewall", "firewallBans")},

		{name: "no web system", cfg: map[string]string{"WEB_SYSTEM": ""}, who: "admin", want: all(false, "web")},
		{name: "no cron system", cfg: map[string]string{"CRON_SYSTEM": ""}, who: "admin", want: all(false, "cron")},
		{name: "no cron jobs", users: map[string]map[string]string{"bob": {"CRON_JOBS": "0"}}, who: "bob", want: all(false, "cron")},
		{name: "no backup system", cfg: map[string]string{"BACKUP_SYSTEM": ""}, who: "admin", want: all(false, "backup")},
		{name: "no backups", users: map[string]map[string]string{"bob": {"BACKUPS": "0", "U_BACKUPS": "0", "BACKUPS_INCREMENTAL": "no"}},
			who: "bob", want: all(false, "backup", "incremental")},
		{name: "incremental only", users: map[string]map[string]string{"bob": {"BACKUPS": "0", "U_BACKUPS": "0", "BACKUPS_INCREMENTAL": "yes"}},
			who: "bob", want: all(true, "backup", "incremental")},

		{name: "logs hidden from users", cfg: map[string]string{"POLICY_USER_VIEW_LOGS": "no"}, who: "bob", want: all(false, "logs")},
		{name: "logs hidden, admin", cfg: map[string]string{"POLICY_USER_VIEW_LOGS": "no"}, who: "admin", want: all(true, "logs")},

		{name: "API 1 root", cfg: map[string]string{"API_SYSTEM": "1"}, who: "admin", want: all(true, "accessKeys")},
		{name: "API 1 user", cfg: map[string]string{"API_SYSTEM": "1"}, who: "bob", want: all(false, "accessKeys")},
		{name: "API 2 user", cfg: map[string]string{"API_SYSTEM": "2"}, who: "bob", want: all(true, "accessKeys")},
		{name: "API 0 root", cfg: map[string]string{"API_SYSTEM": "0"}, who: "admin", want: all(false, "accessKeys")},

		{name: "hidden services, non-root admin", cfg: hide, who: "ops", want: all(false, "server", "firewall", "firewallBans")},
		{name: "hidden services, root", cfg: hide, who: "admin", want: all(true, "server", "firewall", "firewallBans")},
		{name: "hidden services, user", cfg: hide, who: "bob", want: all(false, "server")},
		{name: "admin looking at bob", who: "admin-as-bob", want: all(true, "server")},

		{name: "no firewall", cfg: map[string]string{"FIREWALL_SYSTEM": ""}, who: "admin",
			want: map[string]bool{"server": true, "firewall": false, "firewallBans": false}},
		{name: "no firewall extension", cfg: map[string]string{"FIREWALL_EXTENSION": ""}, who: "admin",
			want: map[string]bool{"firewall": true, "firewallBans": false}},

		{name: "details locked, user", cfg: map[string]string{"POLICY_USER_EDIT_DETAILS": ""}, who: "bob", want: all(false, "canEditDetails")},
		{name: "details locked, admin", cfg: map[string]string{"POLICY_USER_EDIT_DETAILS": ""}, who: "admin", want: all(true, "canEditDetails")},
		{name: "theme locked", cfg: map[string]string{"POLICY_USER_CHANGE_THEME": "no"}, who: "bob", want: all(false, "canChangeTheme")},
		{name: "log deletion locked, user", cfg: map[string]string{"POLICY_USER_DELETE_LOGS": "no"}, who: "bob", want: all(false, "canDeleteLogs")},
		{name: "log deletion locked, admin", cfg: map[string]string{"POLICY_USER_DELETE_LOGS": "no"}, who: "admin", want: all(true, "canDeleteLogs")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := overrides{Config: c.cfg}
			if c.users != nil {
				o.Fixtures = map[string][]fixtureEntry{"v-list-user": hookUserFixtures(t, c.users)}
			}
			h := newHarnessWith(t, o)
			switch c.who {
			case "admin":
				h.loginAdmin()
			case "bob":
				h.loginBob()
			case "ops":
				h.loginOps()
			case "admin-as-bob":
				h.loginAdmin()
				if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
					t.Fatalf("login-as: %d", code)
				}
			}
			b := h.bootstrap()
			if b.User == nil {
				t.Fatal("not signed in")
			}
			for flag, want := range c.want {
				if got := hookFlags[flag](b.User); got != want {
					t.Errorf("%s = %v, want %v", flag, got, want)
				}
			}
		})
	}
}

func TestBootstrapPasswordReset(t *testing.T) {
	if b := newHarness(t).bootstrap(); !b.PasswordReset {
		t.Error("default: passwordReset = false")
	}
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PASSWORD_RESET": "no"}})
	if b := h.bootstrap(); b.PasswordReset {
		t.Error("POLICY_SYSTEM_PASSWORD_RESET=no: passwordReset = true")
	}
	h.loginAdmin()
	if b := h.bootstrap(); b.PasswordReset {
		t.Error("signed in, POLICY_SYSTEM_PASSWORD_RESET=no: passwordReset = true")
	}
	h = newHarness(t)
	h.loginAdmin()
	if b := h.bootstrap(); !b.PasswordReset {
		t.Error("signed in, default: passwordReset = false")
	}
}

// TestBootstrapHookFlagsJSON pins the wire names.
func TestBootstrapHookFlagsJSON(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var doc map[string]any
	if code := h.call("GET", "/api/v1/bootstrap", nil, &doc); code != 200 {
		t.Fatal(code)
	}
	if _, ok := doc["passwordReset"].(bool); !ok {
		t.Errorf("passwordReset missing: %v", doc)
	}
	user := doc["user"].(map[string]any)
	for _, k := range []string{"canEditDetails", "canChangeTheme", "canDeleteLogs", "notifications"} {
		if _, ok := user[k].(bool); !ok {
			t.Errorf("user.%s missing", k)
		}
	}
	features := user["features"].(map[string]any)
	for _, k := range []string{"web", "dns", "db", "mail", "cron", "backup", "incremental", "logs", "accessKeys", "server", "firewall", "firewallBans"} {
		if _, ok := features[k].(bool); !ok {
			t.Errorf("features.%s missing", k)
		}
	}
}
