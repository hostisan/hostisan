#!/usr/bin/env bats
# Tests for the allowed core edits (hostisan/CORE-EDITS.md). Most tests run the
# installed scripts, so they need root on a Hestia install (Docker CI, Phase C)
# and skip elsewhere. Checks that only read the repository run everywhere.

setup() {
	repo="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
	HESTIA=/usr/local/hestia
	if [ -r /etc/hestiacp/hestia.conf ]; then
		# shellcheck disable=SC1091
		# (its last line returns 1 when /etc/hestiacp/local.conf is absent)
		source /etc/hestiacp/hestia.conf || true
	fi
	hbin="$HESTIA/bin"
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/core-edits.XXXXXX")
}

teardown() {
	# A failed restic test must not leave restic.conf replaced or a link to /etc.
	if [ -e "$work/restic-user" ]; then
		restic_teardown "$(cat "$work/restic-user")"
	fi
	rm -rf "$work"
}

# need_hestia: skip unless this is a Hestia install and we are root.
need_hestia() {
	if [ "$(id -u)" -ne 0 ] || [ ! -x "$hbin/v-list-users" ] || [ ! -r /etc/hestiacp/hestia.conf ]; then
		skip "needs root on a Hestia install"
	fi
}

# other S: S with its first character changed, so never equal to S.
other() {
	if [ "${1:0:1}" = x ]; then echo "y${1:1}"; else echo "x${1:1}"; fi
}

# B2a
@test "access keys from urandom" {
	[ "$(grep -c RANDOM "$repo/bin/v-add-access-key")" -eq 0 ]
	need_hestia
	local i out ids=() secrets=()
	for i in $(seq 50); do
		out=$("$hbin/v-add-access-key" admin '' test json)
		ids+=("$(jq -r .ACCESS_KEY_ID <<<"$out")")
		secrets+=("$(jq -r .SECRET_ACCESS_KEY <<<"$out")")
	done
	for i in "${ids[@]}"; do "$hbin/v-delete-access-key" "$i" >/dev/null; done
	[ "$(printf '%s\n' "${ids[@]}" | sort -u | wc -l)" -eq 50 ]
	[ "$(printf '%s\n' "${secrets[@]}" | sort -u | wc -l)" -eq 50 ]
	for i in "${ids[@]}"; do [[ $i =~ ^[0-9A-Za-z]{20}$ ]]; done
	for i in "${secrets[@]}"; do [[ $i =~ ^[0-9A-Za-z_=-]{40}$ ]]; done
}

# B2b
@test "access key secret from file" {
	need_hestia
	local out id sec f
	out=$("$hbin/v-add-access-key" admin '' test json)
	id=$(jq -r .ACCESS_KEY_ID <<<"$out")
	sec=$(jq -r .SECRET_ACCESS_KEY <<<"$out")
	f=$(mktemp /tmp/hostisan-XXXXXX)
	echo "$sec" >"$f"
	run "$hbin/v-check-access-key" "$id" "$f" v-list-users 127.0.0.1 json
	[ "$status" -eq 0 ]
	[ "$(jq -r .USER <<<"$output")" = admin ]
	# A wrong secret in the file is refused like a wrong one in argv.
	other "$sec" >"$f"
	run "$hbin/v-check-access-key" "$id" "$f" v-list-users 127.0.0.1 json
	[ "$status" -eq 9 ]
	# CLI callers still pass the secret itself.
	run "$hbin/v-check-access-key" "$id" "$sec" v-list-users 127.0.0.1 json
	[ "$status" -eq 0 ]
	# The ../ rule: a path that climbs out of /tmp is not read.
	echo "$sec" >"$f"
	run "$hbin/v-check-access-key" "$id" "/tmp/../$f" v-list-users 127.0.0.1 json
	[ "$status" -eq 9 ]
	"$hbin/v-delete-access-key" "$id" >/dev/null

	# The legacy API key: the file holds what argv held, the key's path.
	local key
	key=$(LC_ALL=C tr -dc a-zA-Z0-9 </dev/urandom | head -c 32)
	mkdir -p "$HESTIA/data/keys" # absent until v-generate-api-key
	touch "$HESTIA/data/keys/$key"
	echo "$HESTIA/data/keys/$key" >"$f"
	run "$hbin/v-check-api-key" "$f" 127.0.0.1
	[ "$status" -eq 0 ]
	echo "$HESTIA/data/keys/$(other "$key")" >"$f"
	run "$hbin/v-check-api-key" "$f" 127.0.0.1
	[ "$status" -eq 9 ]
	run "$hbin/v-check-api-key" "$HESTIA/data/keys/$key" 127.0.0.1
	[ "$status" -eq 0 ]
	rm -f "$HESTIA/data/keys/$key" "$f"
}

# argv_holds SECRET CMD ARGS...: runs CMD under strace and succeeds when SECRET shows
# up in the argv of any program it starts (itself included).
argv_holds() {
	local secret=$1
	shift
	strace -f -qq -s 4096 -e trace=execve -o "$work/execve" "$@" >/dev/null 2>&1 || true
	grep -qF -- "$secret" "$work/execve"
}

# argv_clean SECRET CMD ARGS...: the opposite of argv_holds.
argv_clean() {
	if argv_holds "$@"; then
		echo "argv holds the secret:" >&2
		grep -F -- "$1" "$work/execve" >&2
		return 1
	fi
}

@test "key checks never put the key in argv" {
	need_hestia
	command -v strace >/dev/null || skip "needs strace"
	local out id sec f key
	f=$(mktemp /tmp/hostisan-XXXXXX)
	key=$(LC_ALL=C tr -dc a-zA-Z0-9 </dev/urandom | head -c 32)
	mkdir -p "$HESTIA/data/keys" # absent until v-generate-api-key
	touch "$HESTIA/data/keys/$key"
	echo "$HESTIA/data/keys/$key" >"$f"
	# The tracer works: the argv form is seen.
	argv_holds "$key" "$hbin/v-check-api-key" "$HESTIA/data/keys/$key" 127.0.0.1
	argv_clean "$key" "$hbin/v-check-api-key" "$f" 127.0.0.1
	# A wrong key read from the file stays out of argv too.
	echo "$HESTIA/data/keys/$(other "$key")" >"$f"
	argv_clean "$(other "$key")" "$hbin/v-check-api-key" "$f" 127.0.0.1
	rm -f "$HESTIA/data/keys/$key"

	out=$("$hbin/v-add-access-key" admin '' test json)
	id=$(jq -r .ACCESS_KEY_ID <<<"$out")
	sec=$(jq -r .SECRET_ACCESS_KEY <<<"$out")
	echo "$sec" >"$f"
	argv_clean "$sec" "$hbin/v-check-access-key" "$id" "$f" v-list-users 127.0.0.1 json
	"$hbin/v-delete-access-key" "$id" >/dev/null
	rm -f "$f"
}

# B2c
@test "cli args stay literal" {
	need_hestia
	local f="$work/lines"
	printf 'a b*\nzzz\n' >"$f"
	chmod 755 "$work"
	chmod 644 "$f"
	# A file a glob of b* would pick up from the current folder.
	cd "$work"
	touch bglob
	run "$hbin/v-run-cli-cmd" admin grep -c 'a b*' "$f"
	[ "$status" -eq 0 ]
	[ "$output" = 1 ]
	run "$hbin/v-run-cli-cmd" admin grep -c -F 'a b*' "$f"
	[ "$status" -eq 0 ]
	[ "$output" = 1 ]
}

# B2d
@test "self-signed workdir removed" {
	need_hestia
	local before after jobs out dir
	before=$(find /tmp -maxdepth 2 -path '/tmp/tmp.*' -name '*.key' | sort)
	jobs=$(atq 2>/dev/null | wc -l)
	out=$("$hbin/v-generate-ssl-cert" b2d.example.test '' US State City Org IT '' json)
	[[ $(jq -r '."b2d.example.test".KEY' <<<"$out") == *"PRIVATE KEY"* ]]
	dir=$(jq -r '."b2d.example.test".DIR' <<<"$out")
	[ ! -e "$dir" ]
	after=$(find /tmp -maxdepth 2 -path '/tmp/tmp.*' -name '*.key' | sort)
	[ "$after" = "$before" ]
	[ "$(atq 2>/dev/null | wc -l)" -eq "$jobs" ]
	# Stopped while the (slow) RSA key is being made: the folder goes too.
	local pid i
	"$hbin/v-generate-ssl-cert" b2d.example.test '' US State City Org IT '' json rsa >/dev/null 2>&1 &
	pid=$!
	for i in $(seq 100); do
		[ "$(find /tmp -maxdepth 2 -path '/tmp/tmp.*' -name '*.key' | sort)" = "$before" ] || break
		sleep 0.05
	done
	kill -TERM "$pid"
	wait "$pid" || true
	sleep 0.5
	[ "$(find /tmp -maxdepth 2 -path '/tmp/tmp.*' -name '*.key' | sort)" = "$before" ]
	# v-add-letsencrypt-domain reads the CSR and key from the shell format's
	# "Directory:" line and removes the folder itself: that folder still exists.
	dir=$("$hbin/v-generate-ssl-cert" b2d.example.test '' US State City Org IT '' shell | tail -n1 | awk '{print $2}')
	[ -s "$dir/b2d.example.test.csr" ] && [ -s "$dir/b2d.example.test.key" ]
	rm -rf "$dir"
}

# B2e (PF4b): restic_setup USER builds a restic repo for USER holding
# /home/USER/b2e/marker and /home/USER/backup/backup.conf, and prints the snapshot
# id. teardown() calls restic_teardown, which puts the configs back.
restic_setup() {
	local user=$1
	command -v restic >/dev/null || skip "needs restic"
	repo_dir="$work/restic"
	mkdir -p "$work/conf"
	if [ -e "$HESTIA/conf/restic.conf" ]; then
		cp -a "$HESTIA/conf/restic.conf" "$work/conf/restic.conf"
	fi
	if [ -e "$HESTIA/data/users/$user/restic.conf" ]; then
		cp -a "$HESTIA/data/users/$user/restic.conf" "$work/conf/user-restic.conf"
	fi
	if [ -e "$HESTIA/data/queue/backup.pipe" ]; then
		cp -a "$HESTIA/data/queue/backup.pipe" "$work/conf/backup.pipe"
	fi
	# Both configs are saved: from here teardown() puts them back, even when the
	# test fails.
	echo "$user" >"$work/restic-user"
	echo "REPO='$repo_dir/'" >"$HESTIA/conf/restic.conf"
	echo "b2e-test-password" >"$HESTIA/data/users/$user/restic.conf"
	restic init --repo "$repo_dir/$user" --password-file "$HESTIA/data/users/$user/restic.conf" -q
	mkdir -p "/home/$user/b2e" "/home/$user/backup"
	echo hostisan-b2e >"/home/$user/b2e/hostisan-b2e-marker"
	touch "/home/$user/backup/backup.conf"
	chown -R "$user:" "/home/$user/b2e"
	restic backup --repo "$repo_dir/$user" --password-file "$HESTIA/data/users/$user/restic.conf" -q \
		"/home/$user/b2e" "/home/$user/backup/backup.conf" >/dev/null
	restic snapshots --repo "$repo_dir/$user" --password-file "$HESTIA/data/users/$user/restic.conf" --json |
		jq -r '.[0].short_id'
}

restic_teardown() {
	local user=$1
	rm -rf "/home/$user/b2e" /etc/hostisan-b2e-marker
	if [ -e "$work/conf/restic.conf" ]; then
		cp -a "$work/conf/restic.conf" "$HESTIA/conf/restic.conf"
	else
		rm -f "$HESTIA/conf/restic.conf"
	fi
	if [ -e "$work/conf/backup.pipe" ]; then
		cp -a "$work/conf/backup.pipe" "$HESTIA/data/queue/backup.pipe"
	else
		rm -f "$HESTIA/data/queue/backup.pipe"
	fi
	if [ -e "$work/conf/user-restic.conf" ]; then
		cp -a "$work/conf/user-restic.conf" "$HESTIA/data/users/$user/restic.conf"
	else
		rm -f "$HESTIA/data/users/$user/restic.conf"
	fi
}

@test "restic restore never writes through links" {
	need_hestia
	local snap
	snap=$(restic_setup admin)
	# A plain restore works, leaves the file to the user, and takes its own line
	# (only that one) off the backup queue.
	rm -f /home/admin/b2e/hostisan-b2e-marker
	local job="v-restore-file-restic 'admin' '$snap' '/home/admin/b2e/hostisan-b2e-marker' "
	printf '%s\n' "${job}no" "v-restore-file-restic 'admin' '$snap' '/home/admin/b2e/other' no" \
		>>"$HESTIA/data/queue/backup.pipe"
	run "$hbin/v-restore-file-restic" admin "$snap" /home/admin/b2e/hostisan-b2e-marker
	[ "$status" -eq 0 ]
	[ "$(grep -cF -- "$job" "$HESTIA/data/queue/backup.pipe")" -eq 0 ]
	grep -qF -- "/home/admin/b2e/other' no" "$HESTIA/data/queue/backup.pipe"
	[ "$(cat /home/admin/b2e/hostisan-b2e-marker)" = hostisan-b2e ]
	[ "$(stat -c %U /home/admin/b2e/hostisan-b2e-marker)" = admin ]
	# staged on the /home filesystem (not /tmp, a RAM tmpfs on Debian 13), and
	# the staging directory is gone
	[ -z "$(find /home -maxdepth 1 -name '.hostisan-restore.*')" ]
	# The user swaps the folder for a link to /etc: nothing lands in /etc.
	rm -rf /home/admin/b2e
	ln -s /etc /home/admin/b2e
	run "$hbin/v-restore-file-restic" admin "$snap" /home/admin/b2e/hostisan-b2e-marker
	[ "$status" -ne 0 ]
	[ ! -e /etc/hostisan-b2e-marker ]
	# Paths outside the user's home, or climbing out of it, are refused.
	run "$hbin/v-restore-file-restic" admin "$snap" /etc/hostisan-b2e-marker
	[ "$status" -ne 0 ]
	run "$hbin/v-restore-file-restic" admin "$snap" /home/admin/../admin/b2e/hostisan-b2e-marker
	[ "$status" -ne 0 ]
}

@test "restic failure exits non-zero" {
	need_hestia
	local snap
	snap=$(restic_setup admin)
	# A path the snapshot doesn't hold: restic restores nothing.
	run "$hbin/v-restore-file-restic" admin "$snap" /home/admin/b2e/no-such-file
	[ "$status" -ne 0 ]
	# A snapshot that doesn't exist.
	run "$hbin/v-restore-file-restic" admin deadbeef /home/admin/b2e/hostisan-b2e-marker
	[ "$status" -ne 0 ]
}

# B2f (SSO4): sso_try TOKEN EXP runs hestia-sso.php as phpMyAdmin would for
# admin_db/admin from 203.0.113.9, with the key sso_file set and an API that can't be
# reached, and prints the warnings it raises.
ssokey=b2f-key-0123456789

# sso_file KEY: hestia-sso.php as v-add-sys-pma-sso installs it, with KEY ("" leaves
# the placeholder in).
sso_file() {
	local sub=()
	[ -z "$1" ] || sub=(-e "s/%PHPMYADMIN_KEY%/$1/")
	sed "${sub[@]}" -e 's/%API_HOST_NAME%/127.0.0.1/' -e 's/%API_HESTIA_PORT%/1/' \
		"$repo/install/deb/phpmyadmin/hestia-sso.php" >"$work/hestia-sso.php"
}

# hmac KEY DATA: the hex token for DATA.
hmac() {
	printf '%s' "$2" | openssl dgst -sha256 -hmac "$1" -r | cut -d' ' -f1
}

sso_try() {
	# shellcheck disable=SC2016 # PHP code, not shell
	php -r '
		if (!function_exists("curl_init")) {
			function curl_init() { return null; }
			function curl_setopt($c, $o, $v) { return true; }
			function curl_exec($c) { return false; }
			foreach (["URL", "RETURNTRANSFER", "SSL_VERIFYPEER", "SSL_VERIFYHOST", "POST", "POSTFIELDS"] as $o) {
				define("CURLOPT_$o", 0);
			}
		}
		set_error_handler(function ($no, $msg) { echo "warning: $msg\n"; return true; });
		$_SERVER["REMOTE_ADDR"] = "203.0.113.9";
		$_SERVER["SERVER_ADDR"] = "192.0.2.1";
		$_SERVER["PHP_SELF"] = "/phpmyadmin/hestia-sso.php";
		$_GET = ["database" => "admin_db", "user" => "admin", "host" => "localhost",
			"exp" => $argv[2], "hestia_token" => $argv[1]];
		include $argv[3];
	' "$1" "$2" "$work/hestia-sso.php" 2>&1
}

@test "sso verifies hmac, rejects bcrypt" {
	command -v php >/dev/null || skip "needs php"
	sso_file "$ssokey"
	local exp token bcrypt
	exp=$(date +%s)
	# The token the Go panel builds (TestPhpMyAdminTokenHMAC).
	token=$(printf '%s' "admin_db|admin|203.0.113.9|$exp" | openssl dgst -sha256 -hmac "$ssokey" -r | cut -d' ' -f1)
	run sso_try "$token" "$exp"
	[[ $output == *"Unable to connect over API"* ]]
	[[ $output != *"token mismatch"* ]]
	# The old bcrypt token, even a correct one, is refused.
	# shellcheck disable=SC2016
	bcrypt=$(php -r 'echo password_hash($argv[1], PASSWORD_BCRYPT);' "admin_dbadmin203.0.113.9${exp}$ssokey")
	run sso_try "$bcrypt" "$exp"
	[[ $output == *"token mismatch"* ]]
	[[ $output != *"Unable to connect over API"* ]]
	# A wrong token, and a right one older than 60 s.
	run sso_try "${token/?/0}x" "$exp"
	[[ $output == *"token mismatch"* ]]
	local old=$((exp - 61))
	token=$(printf '%s' "admin_db|admin|203.0.113.9|$old" | openssl dgst -sha256 -hmac "$ssokey" -r | cut -d' ' -f1)
	run sso_try "$token" "$old"
	[[ $output == *"Link has been expired"* ]]
	[[ $output != *"Unable to connect over API"* ]]
	# A key never filled in, or a short one, signs nothing.
	sso_file ""
	run sso_try "$(hmac %PHPMYADMIN_KEY% "admin_db|admin|203.0.113.9|$exp")" "$exp"
	[[ $output == *"token mismatch"* ]]
	sso_file short-key-01234
	run sso_try "$(hmac short-key-01234 "admin_db|admin|203.0.113.9|$exp")" "$exp"
	[[ $output == *"token mismatch"* ]]
}

# The SSO plugin's access key goes when the plugin does (B2f fix round).
@test "pma sso removal deletes its access key" {
	need_hestia
	[ ! -e /usr/share/phpmyadmin/hestia-sso.php ] || skip "phpMyAdmin SSO is on here"
	local made=() d key
	for d in /usr/share/phpmyadmin /etc/phpmyadmin; do
		if [ ! -d "$d" ]; then
			mkdir -p "$d"
			made+=("$d")
		fi
	done
	[ -e /etc/phpmyadmin/config.inc.php ] || { touch /etc/phpmyadmin/config.inc.php; made+=(/etc/phpmyadmin/config.inc.php); }
	# As v-add-sys-pma-sso sets it up.
	key=$("$hbin/v-add-access-key" admin '' phpMyAdmin plain)
	sed "s/%API_KEY%/$key/" "$repo/install/deb/phpmyadmin/hestia-sso.php" >/usr/share/phpmyadmin/hestia-sso.php
	touch /etc/phpmyadmin/hestia-sso.inc.php
	run "$hbin/v-delete-sys-pma-sso" quiet
	local gone=yes
	[ ! -e "$HESTIA/data/access-keys/${key%%:*}" ] || gone=no
	"$hbin/v-delete-access-key" "${key%%:*}" >/dev/null 2>&1 || true
	rm -f /usr/share/phpmyadmin/hestia-sso.php /etc/phpmyadmin/hestia-sso.inc.php
	for d in "${made[@]}"; do rm -rf "$d"; done
	[ "$status" -eq 0 ]
	[ "$gone" = yes ]
}

# An upgrade refreshes an SSO whose access key is gone (setup.sh): removing it
# printed "Error: key <id> doesn't exist" into the upgrade log (polish).
@test "pma sso removal without its access key says nothing" {
	need_hestia
	[ ! -e /usr/share/phpmyadmin/hestia-sso.php ] || skip "phpMyAdmin SSO is on here"
	local made=() d id=polishGone0123456789
	for d in /usr/share/phpmyadmin /etc/phpmyadmin; do
		if [ ! -d "$d" ]; then
			mkdir -p "$d"
			made+=("$d")
		fi
	done
	[ -e /etc/phpmyadmin/config.inc.php ] || { touch /etc/phpmyadmin/config.inc.php; made+=(/etc/phpmyadmin/config.inc.php); }
	[ ! -e "$HESTIA/data/access-keys/$id" ]
	sed "s/%API_KEY%/$id:secret/" "$repo/install/deb/phpmyadmin/hestia-sso.php" >/usr/share/phpmyadmin/hestia-sso.php
	touch /etc/phpmyadmin/hestia-sso.inc.php
	run "$hbin/v-delete-sys-pma-sso" quiet
	local left=no
	[ ! -e /usr/share/phpmyadmin/hestia-sso.php ] || left=yes
	rm -f /usr/share/phpmyadmin/hestia-sso.php /etc/phpmyadmin/hestia-sso.inc.php
	for d in "${made[@]}"; do rm -rf "$d"; done
	echo "$output"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
	[ "$left" = no ]
}

# B2g (SR32): a DNS zone for admin whose TXT and AAAA values hold colons.
@test "search keeps colons" {
	need_hestia
	local u="$HESTIA/data/users/admin" d=b2g-colons.example.test out
	mkdir -p "$u/dns" "$work/dns"
	cp -a "$u/dns.conf" "$work/dns.conf" 2>/dev/null || true
	echo "DOMAIN='$d' IP='192.0.2.1' TPL='default' TTL='14400' EXP='2027-01-01' SOA='ns1.$d' SERIAL='2026102801' SRC='' RECORDS='2' DNSSEC='no' KEY='' SLAVE='' MASTER='' SUSPENDED='no' TIME='12:34:56' DATE='2026-10-28'" >>"$u/dns.conf"
	{
		echo "ID='1' RECORD='b2gtxt' TYPE='TXT' PRIORITY='' VALUE='a:b:c:d:e' SUSPENDED='no' TIME='12:34:56' DATE='2026-10-28' TTL='14400'"
		echo "ID='2' RECORD='b2gaaaa' TYPE='AAAA' PRIORITY='' VALUE='2001:db8::1' SUSPENDED='no' TIME='12:34:57' DATE='2026-10-28' TTL='14400'"
	} >"$u/dns/$d.conf"
	run "$hbin/v-search-user-object" admin b2g json
	local user_status=$status user_out=$output
	run "$hbin/v-search-object" b2g json
	if [ -e "$work/dns.conf" ]; then cp -a "$work/dns.conf" "$u/dns.conf"; else rm -f "$u/dns.conf"; fi
	rm -f "$u/dns/$d.conf"
	[ "$user_status" -eq 0 ]
	[ "$status" -eq 0 ]
	for out in "$user_out" "$output"; do
		[ "$(jq -r '.[] | select(.RESULT == "b2gtxt.'"$d"'") | .TIME' <<<"$out")" = 12:34:56 ]
		[ "$(jq -r '.[] | select(.RESULT == "b2gaaaa.'"$d"'") | .TIME + " " + .SUSPENDED' <<<"$out")" = "12:34:57 no" ]
		[ "$(jq -r '.[] | select(.KEY == "DOMAIN" and .RESULT == "'"$d"'") | .DATE' <<<"$out")" = 2026-10-28 ]
	done
}
