package hestia

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/hostisan/hostisan/internal/runner"
)

// User is one entry from `v-list-user <name> json`: Hestia's fields, all strings.
type User map[string]string

func (u User) Role() string        { return u["ROLE"] }
func (u User) Suspended() bool     { return u["SUSPENDED"] == "yes" }
func (u User) TwoFAEnabled() bool  { return u["TWOFA"] != "" }
func (u User) LoginDisabled() bool { return u["LOGIN_DISABLED"] == "yes" }
func (u User) UsesIPList() bool    { return u["LOGIN_USE_IPLIST"] == "yes" }
func (u User) Language() string    { return u["LANGUAGE"] }
func (u User) Theme() string       { return u["THEME"] }

func (u User) SortOrder() string {
	if s := u["PREF_UI_SORT"]; s != "" {
		return s
	}
	return "name"
}

func (u User) AllowedIPs() []string {
	var ips []string
	for _, ip := range strings.Split(u["LOGIN_ALLOW_IPS"], ",") {
		if ip = strings.TrimSpace(ip); ip != "" {
			ips = append(ips, ip)
		}
	}
	return ips
}

// LandingPath is the first section the user's package allows, as in web/login/index.php.
// Admins land on the users list instead; the caller decides that.
func (u User) LandingPath() string {
	for _, s := range []struct{ field, path string }{
		{"WEB_DOMAINS", "/web"},
		{"DNS_DOMAINS", "/dns"},
		{"MAIL_DOMAINS", "/mail"},
		{"DATABASES", "/db"},
		{"CRON_JOBS", "/cron"},
		{"BACKUPS", "/backup"},
	} {
		if u[s.field] != "0" {
			return s.path
		}
	}
	return "/error"
}

// User returns the named user; ok is false when Hestia says the user doesn't exist.
func (c *Client) User(ctx context.Context, name string) (User, bool, error) {
	var out map[string]User
	err := c.runJSON(ctx, &out, "v-list-user", name, "json")
	var cmdErr *runner.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Exit == runner.ExitNotExist {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	u, ok := out[name]
	if !ok {
		return nil, false, nil
	}
	return u, true, nil
}

// Hestia's is_user_format_valid: 1–30 ASCII characters, letters or digits at both ends.
// Hestia's bash class also lets "|" and "\" through, but useradd rejects them, so no
// real user can have them; they're excluded here.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,28}[A-Za-z0-9])?$`)

func ValidUsername(s string) bool {
	return usernameRe.MatchString(s)
}
