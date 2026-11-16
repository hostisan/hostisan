Fixtures recorded on a real Hestia box (tools/record-fixtures.sh), one directory
per setup: recorded/<setup>/<v-command>.json.

Before committing a recording, scrub it:

    tools/scrub-fixtures.sh --ip <real-ip>=203.0.113.10 \
        --host <real-hostname>=rec.hostisan.test \
        hostisan/testdata/fixtures/recorded/<setup>

--ip and --host (repeatable) rewrite the box's real address and name in args,
stdout and stderr; IP placeholders must be RFC 5737 documentation addresses.

This replaces PHPMYADMIN_KEY, SERVER_SMTP_PASSWD, RKEY, TWOFA, QRCODE,
SECRET_ACCESS_KEY, MD5 and PASSWORD values (and KEY in v-list-*-ssl output)
with REDACTED, and private key blocks with REDACTED-KEY. Empty values stay empty.

The Go test TestRecordedFixturesHoldNoSecrets (internal/runner) walks this tree
and fails on any secret-shaped value left behind, including crypt(3) password
hashes and any IPv4 outside RFC 5737, RFC 1918, 127/8 and 0.0.0.0 (netmasks
pass), and on any *.json here that is not a fixture list.

TestDevFixturesMatchRecordedShapes compares every setup with the dev fixtures.
Shared exceptions live in shape-exceptions.txt here; a setup's own variation (a
minimal install's "not enabled" errors, say) goes in <setup>/shape-exceptions.txt,
which applies to that setup only.
