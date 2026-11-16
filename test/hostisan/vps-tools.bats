#!/usr/bin/env bats
# Tests for tools/vps: guard.sh, seed-plan.sh, overlay-fixtures.sh.
# Run: bats test/hostisan/vps-tools.bats  (plain bats; no helper libraries needed)

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	vps=$repo/tools/vps
	dev=$repo/hostisan/testdata/fixtures/dev
	tmp=$(mktemp -d "${BATS_TMPDIR:-/tmp}/vps-tools.XXXXXX")
}

teardown() {
	rm -rf "$tmp"
}

# "nft -c" needs netlink access for "delete table"; a private user+net namespace gives it.
nft_check() {
	if unshare -rn true 2>/dev/null; then
		unshare -rn nft -c -f "$1"
	else
		nft -c -f "$1"
	fi
}

@test "seed plan has no clash" {
	run "$vps/seed-plan.sh" "$dev"
	[ "$status" -eq 0 ]
	plan=$output
	# No line whose user (field 2) is bob mentions example.com as a word.
	run awk '$1 ~ /^v-/ && $2 == "bob" && /(^|[^a-z.-])example\.com([^a-z.-]|$)/' <<<"$plan"
	[ -z "$output" ]
	# Every domain is added by exactly one user.
	run awk '$1 ~ /^v-add-(web|dns|mail)-domain$/ { print $3, $2 }' <<<"$plan"
	[ -n "$output" ]
	dupes=$(sort -u <<<"$output" | awk '{ print $1 }' | sort | uniq -d)
	[ -z "$dupes" ]
}

@test "seed plan covers users" {
	run "$vps/seed-plan.sh" "$dev"
	[ "$status" -eq 0 ]
	for u in bob carol ops susp; do
		grep -Eq "^v-add-user $u /tmp/seedpass " <<<"$output"
	done
	grep -Eq '^v-suspend-user susp( |$)' <<<"$output"
	grep -Eq '^v-change-user-role ops admin$' <<<"$output"
	# admin exists after install and is never re-added.
	if grep -Eq '^v-add-user admin ' <<<"$output"; then false; fi
}

@test "seed plan builds the search world on bakery.example" {
	run "$vps/seed-plan.sh" "$dev"
	[ "$status" -eq 0 ]
	grep -Eq '^v-add-web-domain bob bakery\.example ' <<<"$output"
	grep -Eq '^v-add-dns-domain bob bakery\.example ' <<<"$output"
	grep -Eq '^v-add-mail-domain bob bakery\.example ' <<<"$output"
	grep -Eq '^v-add-mail-account bob bakery\.example info /tmp/seedpass' <<<"$output"
	grep -Eq "^v-add-cron-job bob .*wget[^']*bakery\.example.* 2( |$)" <<<"$output"
	grep -Eq '^v-add-web-domain admin example\.net ' <<<"$output"
}

@test "seed plan passes secrets only as /tmp/seedpass" {
	run "$vps/seed-plan.sh" "$dev"
	[ "$status" -eq 0 ]
	run grep -E '^v-add-(user|database|mail-account) ' <<<"$output"
	[ -n "$output" ]
	while read -r line; do
		[[ $line == *" /tmp/seedpass"* ]]
	done <<<"$output"
}

@test "seed plan lines parse as manifest args" {
	run "$vps/seed-plan.sh" "$dev"
	[ "$status" -eq 0 ]
	n=0
	while IFS= read -r line; do
		[[ -z $line || $line == \#* ]] && continue
		mapfile -d '' parts < <(xargs printf '%s\0' <<<"$line")
		[[ ${parts[0]} =~ ^v-[a-z0-9-]+$ ]]
		n=$((n + 1))
	done <<<"$output"
	[ "$n" -gt 10 ]
	# The cron command with quotes survives as one argument.
	line=$(grep -E "^v-add-cron-job bob .*find" <<<"$output")
	mapfile -d '' parts < <(xargs printf '%s\0' <<<"$line")
	[ "${parts[7]}" = "find /home/bob/tmp -name '*.tmp' -mtime +7 -exec rm {} \;" ]
}

@test "seed plan writes package files" {
	run "$vps/seed-plan.sh" "$dev" "$tmp/pkg"
	[ "$status" -eq 0 ]
	grep -Eq '^v-add-user-package /tmp/seed-pkg/starter\.pkg starter$' <<<"$output"
	if grep -Eq '^v-add-user-package /tmp/seed-pkg/(default|system)\.pkg' <<<"$output"; then false; fi
	[ -f "$tmp/pkg/starter.pkg" ]
	[ -f "$tmp/pkg/empty.pkg" ]
	grep -qx "WEB_DOMAINS='1'" "$tmp/pkg/starter.pkg"
	# Package files are shell-sourcable KEY='value' lines.
	( set -e; . "$tmp/pkg/starter-plus.pkg"; [ "$DISK_QUOTA" = 5000 ] )
}

@test "seed plan rejects a missing fixture dir" {
	run "$vps/seed-plan.sh" "$tmp/nope"
	[ "$status" -ne 0 ]
}

@test "overlay prefers recorded" {
	mkdir -p "$tmp/dev" "$tmp/rec"
	echo '[{"args":["json"],"stdout":"dev\n","exit":0},{"args":["*"],"stdout":"devstar\n","exit":0}]' >"$tmp/dev/v-list-users.json"
	echo '[{"args":["json"],"stdout":"rec\n","exit":0}]' >"$tmp/rec/v-list-users.json"
	echo '[{"args":["admin","pw"],"stdout":"","exit":0}]' >"$tmp/dev/v-check-user-password.json"
	echo '[{"args":["x"],"stdout":"r","exit":0}]' >"$tmp/rec/v-list-only-recorded.json"
	run "$vps/overlay-fixtures.sh" "$tmp/dev" "$tmp/rec" "$tmp/out"
	[ "$status" -eq 0 ]
	run jq -r '[.[] | select(.args == ["json"])][0].stdout' "$tmp/out/v-list-users.json"
	[ "$output" = "rec" ]
	run jq -r 'map(.stdout | rtrimstr("\n")) | join(",")' "$tmp/out/v-list-users.json"
	[ "$output" = "rec,dev,devstar" ]
	# Commands only in dev (login/auth) and only in recorded both survive.
	cmp <(jq -S . "$tmp/dev/v-check-user-password.json") <(jq -S . "$tmp/out/v-check-user-password.json")
	cmp <(jq -S . "$tmp/rec/v-list-only-recorded.json") <(jq -S . "$tmp/out/v-list-only-recorded.json")
}

@test "overlay refuses to write into an input dir" {
	mkdir -p "$tmp/dev" "$tmp/rec"
	run "$vps/overlay-fixtures.sh" "$tmp/dev" "$tmp/rec" "$tmp/dev"
	[ "$status" -ne 0 ]
}

@test "guard script is valid" {
	bash -n "$vps/guard.sh"
	run "$vps/guard.sh" ruleset 203.0.113.5
	[ "$status" -eq 0 ]
	printf '%s\n' "$output" >"$tmp/rules.nft"
	grep -q 'table inet hostisan_guard' "$tmp/rules.nft"
	grep -Eq 'tcp dport \{ ?25, 465, 587 ?\} .*reject' "$tmp/rules.nft"
	grep -q 'saddr != 203.0.113.5' "$tmp/rules.nft"
	command -v nft >/dev/null || skip "nft not installed"
	nft_check "$tmp/rules.nft"
}

@test "guard ruleset never drops ssh and allows loopback" {
	run "$vps/guard.sh" ruleset 2001:db8::5
	[ "$status" -eq 0 ]
	if grep -E 'dport[^;]*\b22\b[^;]*(drop|reject)' <<<"$output"; then false; fi
	grep -Eq 'iif(name)? "?lo"? accept' <<<"$output"
	grep -Eq 'oif(name)? "?lo"? accept' <<<"$output"
	grep -q 'ip6 saddr != 2001:db8::5' <<<"$output"
	command -v nft >/dev/null || skip "nft not installed"
	printf '%s\n' "$output" >"$tmp/rules6.nft"
	nft_check "$tmp/rules6.nft"
}

@test "guard rejects a bad owner ip" {
	for bad in '' '1.2.3.4; flush ruleset' 'example.com' '1.2.3' '999.1.1.1' '1.2.3.4 }'; do
		run "$vps/guard.sh" ruleset "$bad"
		[ "$status" -ne 0 ]
	done
	run "$vps/guard.sh" on
	[ "$status" -ne 0 ]
	run "$vps/guard.sh" bogus 1.2.3.4
	[ "$status" -ne 0 ]
}

@test "guard dry run prints the plan and changes nothing" {
	run env GUARD_DRY_RUN=1 GUARD_ETC_DIR="$tmp/etc" "$vps/guard.sh" on 203.0.113.5
	[ "$status" -eq 0 ]
	grep -q 'would: nft -f' <<<"$output"
	grep -q 'would: systemctl enable hostisan-guard.service' <<<"$output"
	[ ! -e "$tmp/etc" ]
}

# Applies the guard for real inside a throwaway user+network namespace.
@test "guard on, status, re-on and off in a private netns" {
	command -v nft >/dev/null || skip "nft not installed"
	unshare -rn true 2>/dev/null || skip "user namespaces unavailable"
	run unshare -rn env GUARD_ETC_DIR="$tmp/etc" GUARD_SYSTEMCTL=true bash -c '
		set -e
		g=$1
		$g status
		echo ---
		$g on 203.0.113.5
		$g on 198.51.100.7
		$g status
		echo "tables=$(nft list tables | grep -c hostisan_guard)"
		echo ---
		$g off
		$g off
		$g status
	' _ "$vps/guard.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	[ "${lines[0]}" = "smtp-out: open" ]
	[ "${lines[1]}" = "panel: open" ]
	grep -qx 'smtp-out: blocked' <<<"$output"
	grep -qx 'panel: 198.51.100.7' <<<"$output"
	grep -qx 'tables=1' <<<"$output"
	[ "${lines[-2]}" = "smtp-out: open" ]
	[ "${lines[-1]}" = "panel: open" ]
	[ ! -e "$tmp/etc/hostisan-guard.nft" ]
}

@test "guard rejects outbound smtp in a private netns" {
	command -v nft >/dev/null || skip "nft not installed"
	command -v ip >/dev/null || skip "iproute2 not installed"
	unshare -rn true 2>/dev/null || skip "user namespaces unavailable"
	# A dummy link swallows packets, so without the guard a connect times out;
	# with it the kernel answers with a reset at once.
	run unshare -rn env GUARD_ETC_DIR="$tmp/etc" GUARD_SYSTEMCTL=true bash -c '
		ip link add d0 type dummy && ip addr add 192.0.2.1/24 dev d0 && ip link set d0 up
		ip link set lo up
		probe() { timeout 2 bash -c "exec 3<>/dev/tcp/192.0.2.9/$1" 2>/dev/null; echo "$1:$?"; }
		probe 25
		$1 on 203.0.113.5 >/dev/null
		probe 25; probe 465; probe 587; probe 2525
	' _ "$vps/guard.sh"
	echo "$output"
	[ "${lines[0]}" = "25:124" ]
	[ "${lines[1]}" = "25:1" ]
	[ "${lines[2]}" = "465:1" ]
	[ "${lines[3]}" = "587:1" ]
	[ "${lines[4]}" = "2525:124" ]
}

@test "guard on writes the boot unit" {
	command -v nft >/dev/null || skip "nft not installed"
	unshare -rn true 2>/dev/null || skip "user namespaces unavailable"
	run unshare -rn env GUARD_ETC_DIR="$tmp/etc" GUARD_SYSTEMCTL=true "$vps/guard.sh" on 203.0.113.5
	[ "$status" -eq 0 ]
	[ -f "$tmp/etc/hostisan-guard.nft" ]
	[ -f "$tmp/etc/systemd/system/hostisan-guard.service" ]
	grep -q "ExecStart=.*nft -f /etc/hostisan-guard.nft" "$tmp/etc/systemd/system/hostisan-guard.service"
	nft_check "$tmp/etc/hostisan-guard.nft"
}

@test "vps tools are shellcheck clean" {
	command -v shellcheck >/dev/null || skip "shellcheck not installed"
	shellcheck "$vps/guard.sh" "$vps/seed-plan.sh" "$vps/overlay-fixtures.sh"
}
