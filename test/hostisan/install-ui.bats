#!/usr/bin/env bats
# What the Ubuntu installer shows and keeps (install/common/hostisan/
# install-ui.sh): the generated admin password and its root-only file, the
# progress lines during the long package phase, the version in the banner,
# and the final summary.

# shellcheck disable=SC2034 # installer globals the helpers read

bats_require_minimum_version 1.5.0

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	ubu="$repo/install/hst-install-ubuntu.sh"
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/install-ui.XXXXXX")
	# shellcheck source=install/common/hostisan/install-ui.sh
	source "$repo/install/common/hostisan/install-ui.sh"
}

teardown() {
	# a background apt-get stub a failed test left running
	if [ -s "$work/apt.pid" ]; then
		kill "$(cat "$work/apt.pid")" 2>/dev/null || true
	fi
	rm -rf "$work"
}

code() {
	grep -vE '^[[:space:]]*#' "$1"
}

line_of() {
	grep -nE "$2" "$1" | head -n1 | cut -d: -f1
}

# ---------------------------------------------------------------- password

@test "password: a generated one is strong" {
	local a b
	a=$(hostisan_new_password)
	b=$(hostisan_new_password)
	[[ $a =~ ^[A-Za-z0-9]{20}$ ]]
	[ "$a" != "$b" ]
}

@test "password: the file is root-only (0600) and names the login" {
	umask 022
	hostisan_save_password "$work/pw.txt" https://panel.example.com:8083 admin 'S3cret-x'
	[ "$(stat -c %a "$work/pw.txt")" = 600 ]
	grep -qx 'Password: S3cret-x' "$work/pw.txt"
	grep -qx 'Username: admin' "$work/pw.txt"
	grep -qx 'Panel:    https://panel.example.com:8083' "$work/pw.txt"
	grep -qi 'delete this file' "$work/pw.txt"
}

@test "password: an existing file or a link in its place is replaced, not followed" {
	echo keep >"$work/other"
	chmod 644 "$work/other"
	ln -s "$work/other" "$work/pw.txt"
	hostisan_save_password "$work/pw.txt" https://x:1 admin 'new'
	[ ! -L "$work/pw.txt" ]
	[ "$(cat "$work/other")" = keep ]
	[ "$(stat -c %a "$work/pw.txt")" = 600 ]
}

# stub_add_user: a v-add-user in $work/hestia/bin that records its argv (one
# argument per line), and the mode, owner dir and first line of the file its
# PASSWORD argument names, then exits $STUB_RC.
stub_add_user() {
	mkdir -p "$work/hestia/bin"
	cat >"$work/hestia/bin/v-add-user" <<-EOF
		#!/bin/bash
		printf '%s\\n' "\$@" >"$work/argv"
		if [ -f "\$2" ]; then
			stat -c %a "\$2" >"$work/mode"
			head -n1 "\$2" >"$work/read"
		fi
		exit \${STUB_RC:-0}
	EOF
	chmod +x "$work/hestia/bin/v-add-user"
}

@test "password: v-add-user gets it in a 0600 file under /tmp, never in its argv" {
	stub_add_user
	local pw='S3cret-pass*;$x'
	run hostisan_add_admin "$work/hestia" admin "$pw" 'you@example.com'
	echo "$output"
	[ "$status" -eq 0 ]
	cat "$work/argv"
	[ "$(wc -l <"$work/argv")" -eq 5 ]
	run ! grep -F -- "$pw" "$work/argv"
	[ "$(sed -n 1p "$work/argv")" = admin ]
	[[ "$(sed -n 2p "$work/argv")" == /tmp/hostisan-admin.* ]]
	[ "$(sed -n 3p "$work/argv")" = you@example.com ]
	[ "$(sed -n 4p "$work/argv")" = default ]
	[ "$(sed -n 5p "$work/argv")" = 'System Administrator' ]
	# v-add-user read it from the file, which was 0600 and is gone now
	[ "$(cat "$work/mode")" = 600 ]
	[ "$(cat "$work/read")" = "$pw" ]
	[ ! -e "$(sed -n 2p "$work/argv")" ]
}

@test "password: the file goes even when v-add-user fails, and its status is kept" {
	stub_add_user
	STUB_RC=4 run hostisan_add_admin "$work/hestia" admin 'pw12345678' 'you@example.com'
	[ "$status" -eq 4 ]
	[ ! -e "$(sed -n 2p "$work/argv")" ]
}

@test "installer: the admin account is made by hostisan_add_admin, all quoted" {
	run ! grep -nE 'v-add-user .*\$vpass' "$ubu"
	grep -qx 'hostisan_add_admin "\$HESTIA" "\$username" "\$vpass" "\$email"' "$ubu"
	# and v-add-user still reads a password file only under /tmp/
	grep -q 'password" =~ ^/tmp/' "$repo/func/main.sh"
}

@test "installer: no --password means a generated one and no prompt" {
	run ! grep -nE "read -p 'Please enter administrator password" "$ubu"
	grep -qE '^\s+vpass=\$\(hostisan_new_password\)$' "$ubu"
	grep -qE '^\s+hostisan_save_password "\$hostisan_password_file" ' "$ubu"
	# saved before the admin account exists, so a failed install still has it
	[ "$(line_of "$ubu" '^\s+hostisan_save_password ')" -lt "$(line_of "$ubu" '^hostisan_add_admin ')" ]
	# any password prompt that remains does not echo
	run ! grep -nE 'read -p[^|]*password' <(code "$ubu")
}

@test "installer: the password goes to the screen once, never into the mail" {
	grep -qxE 'hostisan_install_summary screen' "$ubu"
	grep -qxE 'hostisan_install_summary mail \| \$send_mail -s "Hostisan" \$email' "$ubu"
	run ! grep -nE 'displaypass|password_line' <(code "$ubu")
}

# ---------------------------------------------------------------- progress

# progress_of SCRIPT LOG INTERVAL: run SCRIPT in the background and wait for
# it with hostisan_wait (in one shell, so wait can see it).
progress_of() {
	hostisan_log_mark "$2"
	(eval "$1") &
	hostisan_wait $! "still installing packages" "$2" "$3"
}

@test "progress: a line every INTERVAL with the minutes and packages set up" {
	local log=$work/log
	# lines from earlier steps do not count
	printf 'Setting up old (1)\n9 upgraded, 9 newly installed, 0 to remove and 0 not upgraded.\n' >"$log"
	run progress_of "echo '1 upgraded, 3 newly installed, 0 to remove and 0 not upgraded.' >>$log
		sleep 1.2; echo 'Setting up a (1)' >>$log; sleep 1; echo 'Setting up b (1)' >>$log; sleep 1.3" "$log" 1
	echo "$output"
	[ "$status" -eq 0 ]
	[ "$(grep -c 'still installing packages' <<<"$output")" -ge 2 ]
	grep -qE 'still installing packages\.\.\. \([0-9]+ min, [12] of 4 packages set up' <<<"$output"
	[[ "$output" == *"log: $log"* ]]
	run ! grep -q 'of 18 packages' <<<"$output"
}

@test "progress: it returns the command's exit status and stays quiet when quick" {
	run progress_of 'exit 3' "$work/log" 60
	[ "$status" -eq 3 ]
	[ -z "$output" ]
	run progress_of 'sleep 0.2' "$work/log" 60
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

# wait_blocks: each of the installer's background apt-get runs, from the
# apt-get line through the check_result after its hostisan_wait, as one shell
# snippet per file wait-block.N in $work.
wait_blocks() {
	awk -v dir="$work" '
		/^apt-get .*&$/ { n++; f = dir "/wait-block." n; on = 1 }
		on { print > f }
		on && /^check_result / { on = 0; close(f) }
	' "$ubu"
	# the last upgrade runs on while the install goes on: not waited for
	grep -L 'hostisan_wait ' "$work"/wait-block.* | xargs -r rm -f
	ls "$work"/wait-block.*
}

@test "progress: Ctrl-C (INT) or TERM stops the background apt-get too" {
	wait_blocks
	local b n=0 sig
	mkdir -p "$work/bin"
	# apt-get sleeps for a long time, as a slow install would
	printf '#!/bin/bash\necho $$ >"%s/apt.pid"\nexec sleep 300\n' "$work" >"$work/bin/apt-get"
	chmod +x "$work/bin/apt-get"
	for b in "$work"/wait-block.*; do
		for sig in INT TERM; do
			n=$((n + 1))
			rm -f "$work/apt.pid"
			# the installer's own shell (bash -c) runs the snippet; a
			# background job of a script ignores SIGINT, so only the trap can
			# pass Ctrl-C on to apt-get
			# (env --default-signal: a background job of this test starts with
			# SIGINT ignored, which no trap can undo; a terminal's is not)
			PATH="$work/bin:$PATH" env --default-signal=INT bash -c "
				source '$repo/install/common/hostisan/install-ui.sh'
				check_result() { [ \"\$1\" -eq 0 ] || { echo \"FAILED: \$2\"; exit 1; }; }
				LOG='$work/log' software=x
				$(cat "$b")
				echo AFTER" >"$work/out.$n" 2>&1 3>&- &
			local sh=$! i
			for i in $(seq 50); do [ -s "$work/apt.pid" ] && break; sleep 0.1; done
			[ -s "$work/apt.pid" ]
			sleep 0.3 # the trap is set
			kill -"$sig" "$sh"
			for i in $(seq 50); do kill -0 "$(cat "$work/apt.pid")" 2>/dev/null || break; sleep 0.1; done
			cat "$work/out.$n"
			run ! kill -0 "$(cat "$work/apt.pid")"
			wait "$sh" || true
			grep -q FAILED "$work/out.$n"
			run ! grep -q AFTER "$work/out.$n"
		done
	done
	# and the traps are put back after each wait
	for b in "$work"/wait-block.*; do
		grep -qx "trap 'kill \"\$BACK_PID\" 2> /dev/null' INT TERM" "$b"
		grep -qx 'trap - INT TERM' "$b"
	done
	[ "$n" -eq 4 ]
}

@test "installer: the package phase appends to the log and shows progress" {
	# the main install used to truncate the log ("> $LOG"), losing the earlier
	# steps, and its spinner never ran (apt-get was not in the background)
	run ! grep -nE '[^>]> ?\$LOG' <(code "$ubu")
	grep -qE '^apt-get -y install \$software >> \$LOG 2>&1 &$' "$ubu"
	grep -qE '^apt-get -y upgrade >> \$LOG 2>&1 &$' "$ubu"
	[ "$(grep -cE '^hostisan_wait \$BACK_PID ' "$ubu")" -eq 2 ]
	# the log is marked before each apt-get starts
	[ "$(grep -E -A1 '^hostisan_log_mark "\$LOG"$' "$ubu" | grep -cE '^apt-get -y (upgrade|install \$software) >> ')" -eq 2 ]
}

# ---------------------------------------------------------------- version

@test "version: the build stamp of the tree, then of the debs, then the deb's build" {
	mkdir -p "$work/src/common/hostisan" "$work/debs"
	run hostisan_install_version "$work/src" "$work/debs"
	[ -z "$output" ]
	if command -v dpkg-deb >/dev/null; then
		mkdir -p "$work/d/DEBIAN"
		printf 'Package: hestia\nVersion: 1:1.10.5-117+ubuntu24.04\nArchitecture: amd64\nMaintainer: t <t@t>\nDescription: t\n' >"$work/d/DEBIAN/control"
		dpkg-deb -b "$work/d" "$work/debs/hestia_1.10.5-117+ubuntu24.04_amd64.deb" >/dev/null
		run hostisan_install_version "$work/src" "$work/debs"
		[ "$output" = "build 117" ]
	fi
	echo 'v1.0.1 (build 120)' >"$work/debs/VERSION"
	run hostisan_install_version "$work/src" "$work/debs"
	[ "$output" = 'v1.0.1 (build 120)' ]
	printf 'v1.0.2 (build 121)\nmore\n' >"$work/src/common/hostisan/VERSION"
	run hostisan_install_version "$work/src" "$work/debs"
	[ "$output" = 'v1.0.2 (build 121)' ]
	# a stamp is shown only when it looks like one
	printf 'v1 $(id) \033[31m\n' >"$work/src/common/hostisan/VERSION"
	run hostisan_install_version "$work/src" ""
	[ -z "$output" ]
}

@test "installer: the banner shows the Hostisan version, not upstream's 1.10.5" {
	run ! grep -nE 'DISPLAY_VER' <(sed -n '/^install_welcome_message() {$/,/^}$/p' "$ubu")
	grep -qE 'hostisan_banner_version' <(sed -n '/^install_welcome_message() {$/,/^}$/p' "$ubu")
	grep -qxE 'hostisan_banner_version=\$\(hostisan_install_version "\$HOSTISAN_SRC" "\$withdebs"\)' "$ubu"
	# hestia.conf keeps upstream's version for the upgrade scripts
	grep -qxF 'write_config_value "VERSION" "${HESTIA_INSTALL_VER}"' "$ubu"
}

# ---------------------------------------------------------------- summary

# summary_vars: the installer's globals for a finished install.
summary_vars() {
	servername=panel.example.com
	port=8083
	ip=203.0.113.5
	hostisan_dns_ok=
	username=admin
	vpass=Gen3ratedPassw0rdXYZ
	hostisan_password_generated=yes
	hostisan_password_file=/root/hostisan-admin-password.txt
	LOG=/root/hst_install_backups/hst_install-111120261200.log
	withdebs=/root/hostisan-debs
	aptrepo=
	iptables=yes
	interactive=no
	hostisan_version=v1.0.0
}

@test "summary: URL, IP fallback, login, password, log and next steps" {
	summary_vars
	run hostisan_install_summary screen
	echo "$output"
	[[ "$output" == *"Hostisan v1.0.0 is installed"* ]]
	[[ "$output" == *"https://panel.example.com:8083"* ]]
	[[ "$output" == *"https://203.0.113.5:8083"* ]]
	[[ "$output" == *"Username:"*"admin"* ]]
	[ "$(grep -c 'Gen3ratedPassw0rdXYZ' <<<"$output")" -eq 1 ]
	[[ "$output" == *"/root/hostisan-admin-password.txt"* ]]
	[[ "$output" == *"/root/hst_install_backups/hst_install-111120261200.log"* ]]
	# next steps
	[[ "$output" == *"A record for panel.example.com"*"203.0.113.5"* ]]
	[[ "$output" == *"v-add-letsencrypt-host"* ]]
	[[ "$output" == *"8083"*"provider"* ]]
	[[ "$output" == *"v-change-firewall-rule"* ]]
	[[ "$output" == *"manual"* ]]
	[[ "$output" == *"dpkg -i hestia_*.deb hestia-nginx_*.deb \\"*"hestia-php_*.deb hestia-web-terminal_*.deb"* ]]
	[[ "$output" == *"reboot"* ]]
	[ "$(awk 'length > 80' <<<"$output" | wc -l)" -eq 0 ]
}

@test "summary: the mail never holds the password" {
	summary_vars
	run hostisan_install_summary mail
	echo "$output"
	[[ "$output" != *Gen3ratedPassw0rdXYZ* ]]
	[[ "$output" == *"Password:"*"/root/hostisan-admin-password.txt"* ]]
	# a given password is named, never repeated
	hostisan_password_generated=
	run hostisan_install_summary screen
	[[ "$output" != *Gen3ratedPassw0rdXYZ* ]]
	[[ "$output" == *"the one given with --password"* ]]
}

@test "summary: DNS ready, an APT repository and no firewall" {
	summary_vars
	hostisan_dns_ok=yes
	withdebs=
	aptrepo=https://apt.example.com
	iptables=no
	run hostisan_install_summary screen
	echo "$output"
	[[ "$output" != *"Backup URL"* ]]
	[[ "$output" == *"panel.example.com already points at this server"* ]]
	[[ "$output" == *"automatic"* ]]
	[[ "$output" == *"https://apt.example.com"* ]]
	[[ "$output" == *"v-update-sys-hestia-all"* ]]
	[[ "$output" != *"dpkg -i"* ]]
	[[ "$output" == *"--iptables no"* ]]
	[[ "$output" != *"v-change-firewall-rule"* ]]
}

@test "summary and welcome notification link no missing GitHub page" {
	run ! grep -nE 'github\.com/hostisan' <(code "$ubu")
	run ! grep -nE 'github\.com/hostisan' "$repo/install/common/hostisan/install-ui.sh"
}
