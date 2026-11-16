package runner

import "testing"

// mailCommands is every command in mail checklist sections 6.1 and 6.3: Plan 2c's,
// plus v-get-mail-account-value and v-search-domain-owner (Plan 5's /reset/mail/).
var mailCommands = []string{
	"v-list-mail-domains", "v-list-mail-domain", "v-list-mail-accounts", "v-list-mail-account",
	"v-list-mail-account-autoreply", "v-list-mail-domain-ssl", "v-list-mail-domain-dkim-dns",
	"v-list-user-ips", "v-list-sys-webmail",

	"v-add-mail-domain", "v-add-mail-domain-reject", "v-add-mail-domain-antispam",
	"v-add-mail-domain-antivirus", "v-add-mail-domain-dkim", "v-add-mail-domain-catchall",
	"v-add-mail-domain-webmail", "v-add-mail-domain-smtp-relay", "v-add-mail-domain-ssl",
	"v-change-mail-domain-sslcert", "v-delete-mail-domain-ssl", "v-add-letsencrypt-domain",
	"v-delete-letsencrypt-domain", "v-change-mail-domain-catchall", "v-change-mail-domain-rate-limit",
	"v-delete-mail-domain-antispam", "v-delete-mail-domain-antivirus", "v-delete-mail-domain-catchall",
	"v-delete-mail-domain-dkim", "v-delete-mail-domain-reject", "v-delete-mail-domain-smtp-relay",
	"v-delete-mail-domain-webmail", "v-delete-mail-domain", "v-suspend-mail-domain",
	"v-unsuspend-mail-domain", "v-rebuild-mail-domain",

	"v-add-mail-account", "v-add-mail-account-alias", "v-delete-mail-account-alias",
	"v-add-mail-account-forward", "v-delete-mail-account-forward", "v-add-mail-account-fwd-only",
	"v-delete-mail-account-fwd-only", "v-add-mail-account-autoreply", "v-delete-mail-account-autoreply",
	"v-change-mail-account-password", "v-change-mail-account-quota", "v-change-mail-account-rate-limit",
	"v-delete-mail-account", "v-suspend-mail-account", "v-unsuspend-mail-account",

	"v-get-mail-account-value", "v-search-domain-owner",
}

func TestMailCommandsAllowed(t *testing.T) {
	for _, c := range mailCommands {
		if !Allowed(c) {
			t.Errorf("%s is not allowed", c)
		}
	}
}

// TestAllowlistDeliberateGaps guards the commands the shared hook step leaves out on
// purpose: 2FA setup stays in Plan 3's own flow, and the API switch and core updates
// stay in Hestia's CLI.
func TestAllowlistDeliberateGaps(t *testing.T) {
	for _, c := range []string{"v-add-user-2fa", "v-change-sys-api", "v-update-sys-hestia"} {
		if Allowed(c) {
			t.Errorf("%s is allowed", c)
		}
	}
}

// toolsCommands is Plan 5's block (tools.md section 11).
var toolsCommands = []string{
	"v-search-object", "v-search-user-object", "v-search-domain-owner", "v-get-mail-account-value",
	"v-restore-file-restic", "v-check-access-key", "v-check-api-key", "v-add-user-sftp-key",
}

func TestToolsCommandsAllowed(t *testing.T) {
	for _, c := range toolsCommands {
		if !Allowed(c) {
			t.Errorf("%s is not allowed", c)
		}
	}
	// The legacy API's pass-through runs through RunAny, never the allowlist.
	for _, c := range []string{"v-add-database-temp-user", "v-delete-database-temp-user", "v-make-tmp-file"} {
		if Allowed(c) {
			t.Errorf("%s is allowed", c)
		}
	}
}
