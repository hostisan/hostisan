#!/usr/bin/env bats
# Function core edits (Plan 6 B3, hostisan/CORE-EDITS.md). The core scripts
# run in a sandbox (core_sandbox.bash) with stubbed system tools. Tests named
# "live:" run only on an installed server as root with HOSTISAN_LIVE=1.

# shellcheck disable=SC2154 # root, repo and HESTIA_DIR come from core_sandbox.bash
load core_sandbox

setup() {
	sandbox_init
}

teardown() {
	sandbox_teardown
}

live() {
	[ "${HOSTISAN_LIVE:-}" = 1 ] && [ "$(id -u)" = 0 ] && [ -d /usr/local/hestia ] ||
		skip "needs HOSTISAN_LIVE=1 on an installed server as root"
}

# --- web terminal (B3a, D5) ---

@test "web terminal installs no node" {
	sandbox_bin v-add-sys-web-terminal
	STUB_DPKG_STATUS='' run "$HESTIA_DIR/bin/v-add-sys-web-terminal"
	[ "$status" -eq 0 ]
	[ ! -e "$root/etc/apt/sources.list.d/nodejs.list" ]
	not_called 'nodesource|nodejs|^curl |^gpg '
	called '^dpkg-query -W -f=\$\{Status\} hestia-web-terminal$'
	called '^apt-get .*install hestia-web-terminal'
	called '^systemctl enable --now hestia-web-terminal$'
	called '^v-change-sys-config-value WEB_TERMINAL true$'
}

@test "web terminal already installed is only enabled" {
	sandbox_bin v-add-sys-web-terminal
	run "$HESTIA_DIR/bin/v-add-sys-web-terminal"
	[ "$status" -eq 0 ]
	not_called '^apt-get '
	called '^systemctl enable --now hestia-web-terminal$'
	if grep -q 'server.js' "$repo/bin/v-add-sys-web-terminal"; then false; fi
}

@test "web terminal reinstalls a removed package" {
	# "apt remove" leaves the package in the config-files state.
	sandbox_bin v-add-sys-web-terminal
	STUB_DPKG_STATUS='deinstall ok config-files' run "$HESTIA_DIR/bin/v-add-sys-web-terminal"
	[ "$status" -eq 0 ]
	called '^apt-get .*install hestia-web-terminal'
	called '^systemctl enable --now hestia-web-terminal$'
}

@test "web terminal install failure leaves it off" {
	sandbox_bin v-add-sys-web-terminal
	STUB_DPKG_STATUS='' STUB_EXIT_apt_get=100 run "$HESTIA_DIR/bin/v-add-sys-web-terminal"
	[ "$status" -ne 0 ]
	not_called '^v-change-sys-config-value WEB_TERMINAL true$'
	not_called '^systemctl enable'
}

# apt_candidate VERSION: apt-cache policy shows VERSION as the terminal's
# candidate ("(none)": no apt source has it).
apt_candidate() {
	cat >"$root/stub/apt-cache" <<-EOF
		#!/bin/bash
		echo "apt-cache \$*" >>"$root/calls"
		printf 'hestia-web-terminal:\n  Installed: (none)\n  Candidate: %s\n' "$1"
	EOF
	chmod 755 "$root/stub/apt-cache"
}

@test "web terminal install failure without an apt source says how to install the deb" {
	# a --with-debs install has no apt source for the Hostisan packages
	sandbox_bin v-add-sys-web-terminal
	apt_candidate '(none)'
	STUB_DPKG_STATUS='' STUB_EXIT_apt_get=100 run "$HESTIA_DIR/bin/v-add-sys-web-terminal"
	echo "$output"
	[ "$status" -ne 0 ]
	[ "${#lines[@]}" -eq 1 ]
	[[ "$output" == *"no apt source"* ]]
	[[ "$output" == *"dpkg -i hestia-web-terminal_"*".deb"* ]]
	not_called '^v-change-sys-config-value WEB_TERMINAL true$'

	# with a source, apt's own failure: no deb hint
	apt_candidate '1:2.0.0-105'
	STUB_DPKG_STATUS='' STUB_EXIT_apt_get=100 run "$HESTIA_DIR/bin/v-add-sys-web-terminal"
	[ "$status" -ne 0 ]
	[[ "$output" == *"hestia-web-terminal installation failed"* ]]
	[[ "$output" != *"dpkg -i"* ]]
}

@test "web terminal delete stops and disables the unit" {
	echo "WEB_TERMINAL='true'" >>"$HESTIA_DIR/conf/hestia.conf"
	sandbox_bin v-delete-sys-web-terminal
	run "$HESTIA_DIR/bin/v-delete-sys-web-terminal"
	[ "$status" -eq 0 ]
	called '^systemctl disable --now hestia-web-terminal$'
	called '^v-change-sys-config-value WEB_TERMINAL false$'
}

@test "live: web terminal installs no node" {
	live
	/usr/local/hestia/bin/v-add-sys-web-terminal
	if command -v node; then false; fi
	[ ! -e /etc/apt/sources.list.d/nodejs.list ]
	systemctl is-active --quiet hestia-web-terminal
	if ss -ltn | grep -q ':8085 '; then false; fi
	[ "$(stat -c '%A %U %G' /run/hestia-web-terminal/terminal.sock)" = "srw-rw---- root hestiaweb" ]
}

# --- php.ini save (B3b, Plan 4 decision 18) ---

php_setup() {
	for v in 8.2 8.3; do
		mkdir -p "$root/etc/php/$v/fpm"
		printf 'memory_limit = 128M\n; PHP %s FPM\n' "$v" >"$root/etc/php/$v/fpm/php.ini"
	done
	src=$(mktemp /tmp/hostisan-php-ini.XXXXXX)
	printf 'memory_limit = 512M\n; PHP 8.3 FPM\n' >"$src"
	sandbox_bin v-change-sys-php-ini
	stub php-fpm8.3
}

php_backups() {
	find "$root/etc/php/$1/fpm" -name 'php.ini.hostisan-*' | sort
}

@test "php ini writes one file" {
	php_setup
	before82=$(cat "$root/etc/php/8.2/fpm/php.ini")
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" yes
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'memory_limit = 512M' "$root/etc/php/8.3/fpm/php.ini"
	[ "$(cat "$root/etc/php/8.2/fpm/php.ini")" = "$before82" ]
	[ -z "$(php_backups 8.2)" ]
	[ "$(php_backups 8.3 | wc -l)" -eq 1 ]
	[[ "$(php_backups 8.3)" =~ php\.ini\.hostisan-[0-9]{14}$ ]]
	grep -qx 'memory_limit = 128M' "$(php_backups 8.3)"
	called '^v-restart-service php8\.3-fpm$'
	not_called '^v-restart-service php-fpm'
	called '^v-log-action system Info System PHP configuration changed \(Version: 8\.3\)\.$'
}

@test "php ini rolls back a broken directive" {
	php_setup
	STUB_EXIT_v_restart_service=1 run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	[[ "$output" == *"ERROR: php8.3-fpm failed to start with new configuration."* ]]
	grep -qx 'memory_limit = 128M' "$root/etc/php/8.3/fpm/php.ini"
	not_called '^v-log-action '
}

@test "php ini without restart only writes" {
	php_setup
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" no
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'memory_limit = 512M' "$root/etc/php/8.3/fpm/php.ini"
	not_called '^v-restart-service '
}

@test "php ini keeps the five newest backups" {
	php_setup
	for i in 1 2 3 4 5 6; do
		echo "old $i" >"$root/etc/php/8.3/fpm/php.ini.hostisan-2026010100000$i"
	done
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src"
	rm -f "$src"
	[ "$status" -eq 0 ]
	[ "$(php_backups 8.3 | wc -l)" -eq 5 ]
	[ ! -e "$root/etc/php/8.3/fpm/php.ini.hostisan-20260101000001" ]
	[ ! -e "$root/etc/php/8.3/fpm/php.ini.hostisan-20260101000002" ]
	[ -e "$root/etc/php/8.3/fpm/php.ini.hostisan-20260101000006" ]
}

@test "php ini refuses bad versions and files" {
	php_setup
	before=$(cat "$root/etc/php/8.3/fpm/php.ini")
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.4 "$src"
	[ "$status" -ne 0 ]
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" '8.3/../8.2' "$src"
	[ "$status" -ne 0 ]
	outside=$(mktemp /var/tmp/hostisan-php-ini.XXXXXX)
	cp "$src" "$outside"
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$outside"
	rm -f "$outside"
	[ "$status" -ne 0 ]
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "/tmp/../$src"
	[ "$status" -ne 0 ]
	link=$(mktemp -u /tmp/hostisan-php-link.XXXXXX)
	ln -s "$src" "$link"
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$link"
	rm -f "$src" "$link"
	[ "$status" -ne 0 ]
	[ "$(cat "$root/etc/php/8.3/fpm/php.ini")" = "$before" ]
	not_called '^v-restart-service '
}

@test "php ini refuses a file through a linked directory" {
	php_setup
	link=$(mktemp -u /tmp/hostisan-etc.XXXXXX)
	ln -s /etc "$link"
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$link/passwd"
	rm -f "$link" "$src"
	[ "$status" -ne 0 ]
	grep -qx 'memory_limit = 128M' "$root/etc/php/8.3/fpm/php.ini"
	not_called '^install '
}

@test "php ini refuses a file owned by another user" {
	php_setup
	STUB_OWNER=bob run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src"
	[ "$status" -ne 0 ]
	STUB_OWNER=hestiaweb run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src"
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'memory_limit = 512M' "$root/etc/php/8.3/fpm/php.ini"
}

@test "php ini is written atomically as root" {
	php_setup
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" no
	rm -f "$src"
	[ "$status" -eq 0 ]
	called "^install -m 644 -o root -g root /tmp/[^ ]+ $root/etc/php/8\.3/fpm/\.php\.ini\.[A-Za-z0-9]+$"
	[ -z "$(find "$root/etc/php/8.3/fpm" -name '.php.ini.*')" ]
	[ "$(stat -c %a "$root/etc/php/8.3/fpm/php.ini")" = 644 ]
}

@test "php ini rollback keeps the oldest backup" {
	php_setup
	for i in 1 2 3 4 5; do
		echo "old $i" >"$root/etc/php/8.3/fpm/php.ini.hostisan-2026010100000$i"
	done
	STUB_EXIT_v_restart_service=1 run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	[ -e "$root/etc/php/8.3/fpm/php.ini.hostisan-20260101000001" ]
}

# --- config editor save (B3c, post-merge ruling 2) ---

conf_setup() {
	mkdir -p "$root/etc/ssh"
	echo 'Port 22' >"$root/etc/ssh/sshd_config"
	src=$(mktemp /tmp/hostisan-conf.XXXXXX)
	echo 'Port 2222' >"$src"
	sandbox_bin v-change-sys-service-config
	stub sshd
}

# checker NAME EXIT [MESSAGE]: a syntax checker stub that logs its argv,
# prints MESSAGE on stderr and exits EXIT.
checker() {
	printf '%s\n' "${3:-}" >"$root/stub/$1.msg"
	cat >"$root/stub/$1" <<-EOF
		#!/bin/bash
		echo "$1 \$*" >>"$root/calls"
		cat "$root/stub/$1.msg" >&2
		exit $2
	EOF
	chmod 755 "$root/stub/$1"
}

# unit_state STATE: systemctl is-active prints STATE (other calls only log).
unit_state() {
	cat >"$root/stub/systemctl" <<-EOF
		#!/bin/bash
		echo "systemctl \$*" >>"$root/calls"
		[ "\$1" = is-active ] && echo "$1"
		exit 0
	EOF
	chmod 755 "$root/stub/systemctl"
}

conf_backups() {
	find "$root/etc/ssh" -name 'sshd_config.hostisan-*' | sort
}

@test "config save keeps timestamped backup and log" {
	conf_setup
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh yes
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'Port 2222' "$root/etc/ssh/sshd_config"
	[ "$(conf_backups | wc -l)" -eq 1 ]
	[[ "$(conf_backups)" =~ sshd_config\.hostisan-[0-9]{14}$ ]]
	grep -qx 'Port 22' "$(conf_backups)"
	called "^v-log-action system Info System Configuration file changed \(Service: ssh, File: $root/etc/ssh/sshd_config\)\.$"
}

@test "config save backs up what a symlinked config points to" {
	# Debian's /etc/mysql/my.cnf is a link chain to the real file.
	conf_setup
	mv "$root/etc/ssh/sshd_config" "$root/etc/ssh/sshd_config.real"
	ln -s sshd_config.real "$root/etc/ssh/sshd_config"
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh no
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'Port 2222' "$root/etc/ssh/sshd_config.real"
	[ -L "$root/etc/ssh/sshd_config" ]
	[ "$(conf_backups | wc -l)" -eq 1 ]
	[ ! -L "$(conf_backups)" ]
	grep -qx 'Port 22' "$(conf_backups)"
}

@test "config save keeps the five newest backups" {
	conf_setup
	for i in 1 2 3 4 5 6; do
		echo "old $i" >"$root/etc/ssh/sshd_config.hostisan-2026010100000$i"
	done
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh no
	rm -f "$src"
	[ "$status" -eq 0 ]
	[ "$(conf_backups | wc -l)" -eq 5 ]
	[ ! -e "$root/etc/ssh/sshd_config.hostisan-20260101000002" ]
	[ -e "$root/etc/ssh/sshd_config.hostisan-20260101000006" ]
}

@test "config save rollback is unchanged and not logged" {
	conf_setup
	STUB_EXIT_v_restart_service=1 run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	[[ "$output" == *"ERROR: ssh failed to start with new configuration."* ]]
	grep -qx 'Port 22' "$root/etc/ssh/sshd_config"
	[ ! -e "$root/etc/ssh/sshd_config.vst.back" ]
	not_called '^v-log-action '
}

@test "config save of an unchanged file logs nothing" {
	conf_setup
	echo 'Port 22' >"$src"
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh yes
	rm -f "$src"
	[ "$status" -eq 0 ]
	[ -z "$(conf_backups)" ]
	not_called '^v-log-action '
}

# --- config editor syntax check and liveness (C7 S8) ---

@test "config save refuses a file its checker rejects" {
	conf_setup
	checker sshd 255 'line 1: Bad configuration option: Bogus'
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh yes
	rm -f "$src"
	[ "$status" -eq 12 ]
	[[ "$output" == *"ERROR: ssh configuration test failed."* ]]
	[[ "$output" == *"Bad configuration option: Bogus"* ]]
	grep -qx 'Port 22' "$root/etc/ssh/sshd_config"
	[ ! -e "$root/etc/ssh/sshd_config.vst.back" ]
	called "^sshd -t -f $root/etc/ssh/sshd_config$"
	not_called '^v-restart-service '
	not_called '^v-log-action '
}

@test "config save without restart still runs the checker" {
	conf_setup
	checker sshd 255 'line 1: Bad configuration option: Bogus'
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh no
	rm -f "$src"
	[ "$status" -eq 12 ]
	grep -qx 'Port 22' "$root/etc/ssh/sshd_config"
	not_called '^v-restart-service '
}

@test "config save keeps a file its checker accepts" {
	conf_setup
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh no
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'Port 2222' "$root/etc/ssh/sshd_config"
	called "^sshd -t -f $root/etc/ssh/sshd_config$"
}

@test "config save tests nginx before the reload" {
	conf_setup
	mkdir -p "$root/etc/nginx"
	echo 'worker_processes 1;' >"$root/etc/nginx/nginx.conf"
	echo 'bogus_directive on;' >"$src"
	checker nginx 1 'nginx: [emerg] unknown directive "bogus_directive"'
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" nginx yes
	rm -f "$src"
	[ "$status" -eq 12 ]
	[[ "$output" == *'unknown directive "bogus_directive"'* ]]
	grep -qx 'worker_processes 1;' "$root/etc/nginx/nginx.conf"
	called "^nginx -t -c $root/etc/nginx/nginx.conf$"
	not_called '^v-restart-service '
}

@test "config save checks the whole dovecot and bind configs" {
	conf_setup
	mkdir -p "$root/etc/dovecot/conf.d" "$root/etc/bind"
	echo old >"$root/etc/dovecot/conf.d/10-mail.conf"
	echo old >"$root/etc/bind/named.conf.options"
	echo new >"$src"
	stub doveconf
	stub named-checkconf
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" dovecot-3 no
	[ "$status" -eq 0 ]
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" bind9-opt no
	rm -f "$src"
	[ "$status" -eq 0 ]
	called "^doveconf -n -c $root/etc/dovecot/dovecot.conf$"
	called "^named-checkconf $root/etc/bind/named.conf$"
}

@test "config save rolls back a service that dies after the restart" {
	conf_setup
	echo 'listen=YES' >"$root/etc/vsftpd.conf"
	echo 'bogus=YES' >"$src"
	unit_state failed
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" vsftpd yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	[[ "$output" == *"ERROR: vsftpd failed to start with new configuration."* ]]
	grep -qx 'listen=YES' "$root/etc/vsftpd.conf"
	called '^v-restart-service vsftpd$'
	called '^systemctl is-active vsftpd$'
	not_called '^v-log-action '
}

@test "config save keeps a service that stays up" {
	conf_setup
	echo 'listen=YES' >"$root/etc/vsftpd.conf"
	echo 'listen=NO' >"$src"
	unit_state active
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" vsftpd yes
	rm -f "$src"
	[ "$status" -eq 0 ]
	grep -qx 'listen=NO' "$root/etc/vsftpd.conf"
	called '^systemctl is-active vsftpd$'
}

@test "php ini refuses a file php-fpm rejects" {
	php_setup
	checker php-fpm8.3 78 'ERROR: failed to load configuration file'
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" no
	rm -f "$src"
	[ "$status" -eq 12 ]
	[[ "$output" == *"ERROR: php8.3-fpm configuration test failed."* ]]
	grep -qx 'memory_limit = 128M' "$root/etc/php/8.3/fpm/php.ini"
	called '^php-fpm8\.3 -t$'
	not_called '^v-log-action '
}

@test "php ini rolls back a pool that dies after the reload" {
	php_setup
	unit_state failed
	run "$HESTIA_DIR/bin/v-change-sys-php-ini" 8.3 "$src" yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	grep -qx 'memory_limit = 128M' "$root/etc/php/8.3/fpm/php.ini"
	called '^systemctl is-active php8\.3-fpm$'
}

# --- firewall rules without iptables (B3e, O8: no core edit needed) ---

@test "firewall rules seeded without iptables" {
	# Both installers copy the default rules at top level, outside any
	# "$iptables" test, so --iptables no still leaves rules.conf in place.
	for installer in hst-install-debian.sh hst-install-ubuntu.sh; do
		grep -qx 'cp -rf $HESTIA_COMMON_DIR/firewall $HESTIA/data/' "$repo/install/$installer"
	done
	[ -s "$repo/install/common/firewall/rules.conf" ]
	# Turning the firewall on later seeds an empty rules directory.
	mkdir -p "$HESTIA_DIR/install/common" "$HESTIA_DIR/data/firewall"
	cp -r "$repo/install/common/firewall" "$HESTIA_DIR/install/common/"
	stub_bin v-update-firewall
	sandbox_bin v-add-sys-firewall
	run "$HESTIA_DIR/bin/v-add-sys-firewall"
	[ "$status" -eq 0 ]
	cmp "$repo/install/common/firewall/rules.conf" "$HESTIA_DIR/data/firewall/rules.conf"
	called '^v-change-sys-config-value FIREWALL_SYSTEM iptables$'
	called '^v-update-firewall'
}

@test "live: firewall rules seeded without iptables" {
	live
	[ -s /usr/local/hestia/data/firewall/rules.conf ]
}

@test "config save rollback keeps the oldest backup" {
	conf_setup
	for i in 1 2 3 4 5; do
		echo "old $i" >"$root/etc/ssh/sshd_config.hostisan-2026010100000$i"
	done
	STUB_EXIT_v_restart_service=1 run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	[ -e "$root/etc/ssh/sshd_config.hostisan-20260101000001" ]
}

@test "config save leaves no rollback copy after a good save" {
	conf_setup
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh no
	rm -f "$src"
	[ "$status" -eq 0 ]
	[ -z "$(find "$root/etc/ssh" -name '*.vst.back*')" ]
}

@test "config save rollback restores each file as it was before that save" {
	conf_setup
	local fpm="$root/etc/php/8.3/fpm/php.ini" cli="$root/etc/php/8.3/cli/php.ini"
	mkdir -p "${fpm%/*}" "${cli%/*}"
	echo 'memory_limit = 128M' >"$fpm"
	echo 'memory_limit = 128M' >"$cli"
	# an older build left a stale copy behind
	echo 'memory_limit = 32M' >"$fpm.vst.back"
	echo 'memory_limit = 256M' >"$src"
	# save 1 changes both files and succeeds
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" php no
	[ "$status" -eq 0 ]
	grep -qx 'memory_limit = 256M' "$fpm"
	grep -qx 'memory_limit = 256M' "$cli"
	[ -z "$(find "$root/etc/php" -name '*.vst.back*')" ]

	# save 2 changes only cli, and the restart fails
	echo 'memory_limit = 64M' >"$cli"
	STUB_EXIT_v_restart_service=1 run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" php yes
	rm -f "$src"
	[ "$status" -eq 20 ]
	# each file is as it was right before save 2, not before save 1
	grep -qx 'memory_limit = 256M' "$fpm"
	grep -qx 'memory_limit = 64M' "$cli"
	[ -z "$(find "$root/etc/php" -name '*.vst.back*')" ]
}

@test "config save refuses a dangling link" {
	conf_setup
	rm "$root/etc/ssh/sshd_config"
	ln -s "$root/etc/ssh/sshd_config.real" "$root/etc/ssh/sshd_config"
	run "$HESTIA_DIR/bin/v-change-sys-service-config" "$src" ssh no
	rm -f "$src"
	[ "$status" -eq 3 ]
	[[ "$output" == *"dangling link"* ]]
	[ -L "$root/etc/ssh/sshd_config" ]
	[ ! -e "$root/etc/ssh/sshd_config.real" ]
	[ -z "$(conf_backups)" ]
}

# --- panel port (C5 D4) ---

@test "port change without HESTIA in env moves hestia-nginx too" {
	sandbox_bin v-change-sys-port
	stub lsof 1
	mkdir -p "$HESTIA_DIR/nginx/conf" "$HESTIA_DIR/data/firewall"
	printf 'server {\n\tlisten 8083 ssl;\n}\n' >"$HESTIA_DIR/nginx/conf/nginx.conf"
	echo "RULE='2' ACTION='ACCEPT' PROTOCOL='TCP' PORT='8083' IP='0.0.0.0/0' COMMENT='HESTIA' SUSPENDED='no' TIME='07:40:16' DATE='2014-05-25'" >"$HESTIA_DIR/data/firewall/rules.conf"
	# A plain `ssh root@box v-change-sys-port …` has no HESTIA in its environment.
	run env -u HESTIA "$HESTIA_DIR/bin/v-change-sys-port" 8444
	[ "$status" -eq 0 ]
	grep -q 'listen 8444 ssl;' "$HESTIA_DIR/nginx/conf/nginx.conf"
	grep -q "PORT='8444'" "$HESTIA_DIR/data/firewall/rules.conf"
	called '^v-change-sys-config-value BACKEND_PORT 8444$'
}

@test "port change can leave the hestia restart to its caller" {
	sandbox_bin v-change-sys-port
	stub lsof 1
	# hestia runs
	printf '#!/bin/bash\necho "root 1 hestia-nginx: master"\necho "root 2 hestia-php"\n' >"$root/stub/ps"
	chmod 755 "$root/stub/ps"
	mkdir -p "$HESTIA_DIR/nginx/conf"
	printf 'server {\n\tlisten 8083 ssl;\n}\n' >"$HESTIA_DIR/nginx/conf/nginx.conf"
	# the hestia-nginx postinst restarts hestia itself at its end, deferred
	# when it runs in the web terminal or an update
	run "$HESTIA_DIR/bin/v-change-sys-port" 8444 no
	[ "$status" -eq 0 ]
	grep -q 'listen 8444 ssl;' "$HESTIA_DIR/nginx/conf/nginx.conf"
	called '^v-change-sys-config-value BACKEND_PORT 8444$'
	not_called '^v-restart-service hestia'
	# anyone else (the panel's port setting, the installer): restarted here
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-change-sys-port" 8445
	[ "$status" -eq 0 ]
	grep -q 'listen 8445 ssl;' "$HESTIA_DIR/nginx/conf/nginx.conf"
	called '^v-restart-service hestia$'
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-change-sys-port" 8446 yes
	[ "$status" -eq 0 ]
	called '^v-restart-service hestia$'
	# anything else is refused before the port moves (a typo of "no" used to
	# restart in line)
	local bad
	for bad in No nO 0 false "no " restart; do
		: >"$root/calls"
		run "$HESTIA_DIR/bin/v-change-sys-port" 8447 "$bad"
		echo "$bad: $status $output"
		[ "$status" -eq 2 ]
		grep -q 'listen 8446 ssl;' "$HESTIA_DIR/nginx/conf/nginx.conf"
		not_called '^v-change-sys-config-value BACKEND_PORT'
		not_called '^v-restart-service'
	done
}

# --- panel sessions (restarts round, review fixes) ---

# sessions_sandbox: v-delete-sys-hostisan-sessions with a stub hostisan binary
# that logs its argv, and systemctl is-active saying "active" unless
# STUB_PANEL_ACTIVE=0.
sessions_sandbox() {
	sandbox_bin v-delete-sys-hostisan-sessions
	mkdir -p "$HESTIA_DIR/hostisan"
	cat >"$HESTIA_DIR/hostisan/hostisan" <<-EOF
		#!/bin/bash
		echo "hostisan \$*" >>"$root/calls"
		exit "\${STUB_EXIT_hostisan:-0}"
	EOF
	cat >"$root/stub/systemctl" <<-EOF
		#!/bin/bash
		echo "systemctl \$*" >>"$root/calls"
		[ "\$1" = is-active ] && [ "\${STUB_PANEL_ACTIVE:-1}" = 0 ] && exit 3
		exit "\${STUB_EXIT_systemctl:-0}"
	EOF
	chmod 755 "$HESTIA_DIR/hostisan/hostisan" "$root/stub/systemctl"
}

@test "panel sessions end for everyone or one user with the panel stopped" {
	# a restart no longer signs anyone out (sessions.json), so an admin needs
	# a way to end them after a password set behind Hestia's back
	sessions_sandbox
	run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" all
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	called '^hostisan sessions forget --state-dir /var/lib/hostisan$'
	# stopped first (its stop would rewrite the file), started after
	[ "$(grep -n '^systemctl stop hostisan.service' "$root/calls" | cut -d: -f1)" -lt \
		"$(grep -n '^hostisan sessions' "$root/calls" | cut -d: -f1)" ]
	[ "$(grep -n '^hostisan sessions' "$root/calls" | cut -d: -f1)" -lt \
		"$(grep -n '^systemctl start hostisan.service' "$root/calls" | cut -d: -f1)" ]
	called '^v-log-action system Warning Security All panel sessions signed out'
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" bob
	[ "$status" -eq 0 ]
	called '^hostisan sessions forget --state-dir /var/lib/hostisan --user bob$'
	called '^v-log-action system Warning Security Panel sessions signed out [(]User: bob[)]'
	called '^systemctl start hostisan.service'
}

@test "panel sessions: a stopped panel stays stopped, failures and bad users" {
	sessions_sandbox
	STUB_PANEL_ACTIVE=0 run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" all
	[ "$status" -eq 0 ]
	called '^hostisan sessions forget'
	not_called '^systemctl start'
	# the panel is started again even when the edit fails
	: >"$root/calls"
	STUB_EXIT_hostisan=1 run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" bob
	[ "$status" -ne 0 ]
	called '^systemctl start hostisan.service'
	# a panel that does not stop is not edited behind its back
	: >"$root/calls"
	STUB_EXIT_systemctl=1 run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" all
	[ "$status" -ne 0 ]
	not_called '^hostisan sessions'
	# usage and user format
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" '../x'
	[ "$status" -eq 2 ]
	run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" bob extra
	[ "$status" -eq 1 ]
	not_called '^systemctl stop'
}

@test "panel sessions: no argument prints the usage and signs nobody out" {
	# rc10 N1: an operator who only wanted the usage text signed everyone out
	sessions_sandbox
	run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions"
	echo "$output"
	[ "$status" -eq 1 ] # E_ARGS
	[[ "$output" == *"Usage: v-delete-sys-hostisan-sessions all|USER"* ]]
	not_called '^systemctl stop'
	not_called '^hostisan sessions'
	not_called '^v-log-action'
	# an empty argument is no shortcut to "all"
	run "$HESTIA_DIR/bin/v-delete-sys-hostisan-sessions" ''
	[ "$status" -eq 1 ]
	not_called '^systemctl stop'
}

# --- download dumps (C6 D10) ---

# dump_setup: a user admin with web domain example.com and database admin_wp, BACKUP
# moved under $root, and chgrp/at stubs. chgrp logs the file's mode at that moment,
# so a test sees whether the dump was ever readable by others.
dump_setup() {
	sed -i "s#^BACKUP='/backup'#BACKUP='$root/backup'#" "$HESTIA_DIR/func/main.sh"
	mkdir -p "$root/backup" "$HESTIA_DIR/data/users/admin" "$root/home/admin/web/example.com/public_html"
	chmod 755 "$root/backup"
	echo "SUSPENDED='no'" >"$HESTIA_DIR/data/users/admin/user.conf"
	echo "DOMAIN='example.com' SUSPENDED='no'" >"$HESTIA_DIR/data/users/admin/web.conf"
	echo "DB='admin_wp' DBUSER='admin_wp' TYPE='mysql' HOST='localhost' SUSPENDED='no'" >"$HESTIA_DIR/data/users/admin/db.conf"
	echo "DB_SYSTEM='mysql'" >>"$HESTIA_DIR/conf/hestia.conf"
	# dump_mysql_database writes the plain dump to $dump (func/db.sh's contract);
	# db.sh is sourced after main.sh, so it also replaces main.sh's PHP parser.
	cat >"$HESTIA_DIR/func/db.sh" <<-'EOF'
		dump_mysql_database() { echo "-- dump" >"$dump"; }
		parse_object_kv_list() { eval "$1"; }
	EOF
	stub at
	cat >"$root/stub/chgrp" <<-EOF
		#!/bin/bash
		echo "chgrp \$1 \$(/usr/bin/stat -c %a "\$2") \$2" >>"$root/calls"
	EOF
	# zip -rq FILE . creates FILE as zip would (honouring the umask).
	cat >"$root/stub/zip" <<-'EOF'
		#!/bin/bash
		echo zip >"$2"
	EOF
	chmod 755 "$root/stub/chgrp" "$root/stub/zip"
}

@test "database dump download is not world-readable" {
	dump_setup
	sandbox_bin v-dump-database
	run "$HESTIA_DIR/bin/v-dump-database" admin admin_wp file gzip
	[ "$status" -eq 0 ]
	file=${lines[0]}
	[[ "$file" == "$root/backup/admin_admin_wp_mysql_"*.sql.gz ]]
	[ "$(stat -c %a "$file")" = 640 ]
	called "^chgrp hestiaweb 600 $file$"
}

@test "site dump download is not world-readable" {
	dump_setup
	sandbox_bin v-dump-site
	sed -i "s#/home/#$root/home/#g" "$HESTIA_DIR/bin/v-dump-site"
	run "$HESTIA_DIR/bin/v-dump-site" admin example.com
	[ "$status" -eq 0 ]
	file=${lines[1]}
	[[ "$file" == "$root/backup/admin_example.com_"*.zip ]]
	[ "$(stat -c %a "$file")" = 640 ]
	called "^chgrp hestiaweb 600 $file$"
}

# --- restore bell (C6 D9) ---

# restore_setup: user admin with a backup archive and backups on; the queued
# commands and v-add-user-notification are stubs that log their argv.
restore_setup() {
	mkdir -p "$HESTIA_DIR/data/users/admin" "$HESTIA_DIR/data/queue" "$root/home/admin/tmp"
	printf "BACKUPS='1'\nSUSPENDED='no'\n" >"$HESTIA_DIR/data/users/admin/user.conf"
	echo "BACKUP='admin.2026-10-30_21-50-07.tar'" >"$HESTIA_DIR/data/users/admin/backup.conf"
	echo "BACKUP_SYSTEM='local'" >>"$HESTIA_DIR/conf/hestia.conf"
	echo "REPO='/srv/restic'" >"$HESTIA_DIR/conf/restic.conf"
	stub restic
	for v in v-add-user-notification v-restore-user v-restore-database-restic v-restore-user-restic; do
		stub_bin "$v"
	done
}

# run_queue: run the queue as v-update-sys-queue does (bash backup.pipe).
run_queue() {
	: >"$root/calls"
	bash "$HESTIA_DIR/data/queue/backup.pipe" || true
}

@test "failed full restore leaves a bell entry" {
	restore_setup
	sandbox_bin v-schedule-user-restore
	run "$HESTIA_DIR/bin/v-schedule-user-restore" admin admin.2026-10-30_21-50-07.tar
	[ "$status" -eq 0 ]
	STUB_EXIT_v_restore_user=4 run_queue
	called '^v-restore-user admin admin.2026-10-30_21-50-07.tar'
	called '^v-add-user-notification admin Backup restore failed <p>.*admin.2026-10-30_21-50-07.tar'
	# v-restore-user adds its own entry on success.
	run_queue
	not_called '^v-add-user-notification'
}

@test "restic restores leave a bell entry" {
	restore_setup
	sandbox_bin v-schedule-user-restore-restic
	sed -i "s#/home/#$root/home/#g" "$HESTIA_DIR/bin/v-schedule-user-restore-restic"
	run "$HESTIA_DIR/bin/v-schedule-user-restore-restic" admin 39d5ca2a db admin_reports
	[ "$status" -eq 0 ]
	run_queue
	called "^v-restore-database-restic admin 39d5ca2a admin_reports yes$"
	called '^v-add-user-notification admin Snapshot restored successfully <p>.*39d5ca2a'
	STUB_EXIT_v_restore_database_restic=3 run_queue
	called '^v-add-user-notification admin Snapshot restore failed <p>.*39d5ca2a'
	not_called 'restored successfully'

	: >"$HESTIA_DIR/data/queue/backup.pipe"
	run "$HESTIA_DIR/bin/v-schedule-user-restore-restic" admin 39d5ca2a
	[ "$status" -eq 0 ]
	STUB_EXIT_v_restore_user_restic=1 run_queue
	called '^v-add-user-notification admin Snapshot restore failed'
}

# --- per-file restic restore staging (final review I5) ---

# file_restore_setup: v-restore-file-restic in the sandbox with /home moved
# under $root. The restic stub writes the item under --target and records the
# target and its parent's mode and owner in $root/staged; runuser runs the
# command without switching user; chown only logs.
file_restore_setup() {
	for f in domain ip rebuild syshealth; do
		sandbox_rewrite "$repo/func/$f.sh" "$HESTIA_DIR/func/$f.sh"
	done
	mkdir -p "$HESTIA_DIR/data/users/admin" "$root/home/admin/web" "$root/tmp"
	echo "REPO='/srv/restic'" >"$HESTIA_DIR/conf/restic.conf"
	sandbox_bin v-restore-file-restic
	sed -i "s#/home\b#$root/home#g" "$HESTIA_DIR/bin/v-restore-file-restic"
	cat >"$root/stub/restic" <<-EOF
		#!/bin/bash
		echo "restic \$*" >>"$root/calls"
		[ "\$5" = dump ] && { echo "BACKUP='x'"; exit 0; }
		inc= target=
		while [ \$# -gt 0 ]; do
			case "\$1" in --include) inc=\$2; shift ;; --target) target=\$2; shift ;; esac
			shift
		done
		mkdir -p "\$target\$(dirname "\$inc")"
		echo restored >"\$target\$inc"
		echo "\$target \$(stat -c '%a %U' "\$(dirname "\$target")")" >"$root/staged"
		# the restore holds its staging dir's lock while it runs
		if flock -n "\$(dirname "\$target")/.lock" true; then echo free; else echo held; fi >"$root/lockstate"
	EOF
	cat >"$root/stub/runuser" <<-EOF
		#!/bin/bash
		echo "runuser \$*" >>"$root/calls"
		shift 3
		exec "\$@"
	EOF
	chmod 755 "$root/stub/restic" "$root/stub/runuser"
	stub chown
	for v in v-update-user-counters v-update-sys-ip-counters; do
		stub_bin "$v"
	done
}

@test "restic file restore stages under /home, not /tmp" {
	file_restore_setup
	TMPDIR=$root/tmp run "$HESTIA_DIR/bin/v-restore-file-restic" admin 39d5ca2a "$root/home/admin/web/index.html"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	[ "$(cat "$root/home/admin/web/index.html")" = restored ]
	local target mode owner
	read -r target mode owner <"$root/staged"
	# on the /home filesystem (Debian 13's /tmp is a RAM tmpfs), root-owned 0700
	[[ "$target" == "$root/home/.hostisan-restore."??????"/root" ]]
	[ "$mode" = 700 ]
	[ "$owner" = "$(id -un)" ]
	called "^restic .* restore 39d5ca2a --include $root/home/admin/web/index.html --target $target$"
	# opened to 0711 only for the user's copy, and removed afterwards
	called "^runuser -u admin -- cp -a --no-preserve=ownership -- $target$root/home/admin/web/index.html $root/home/admin/web/$"
	[ -z "$(ls -A "$root/tmp")" ]
	[ -z "$(find "$root/home" -maxdepth 1 -name '.hostisan-restore.*')" ]
}

@test "restic file restore removes stale staging dirs" {
	file_restore_setup
	# left by restores killed with SIGKILL (v-hostisan-run on a panel stop)
	mkdir -p "$root/home/.hostisan-restore.oldOne/root/x" "$root/home/.hostisan-restore.newOne" "$root/victim"
	touch -d '2 hours ago' "$root/home/.hostisan-restore.oldOne"
	# a running restore's dir is younger than an hour; a link is never followed
	echo keep >"$root/victim/file"
	ln -s "$root/victim" "$root/home/.hostisan-restore.linkOne"
	touch -h -d '2 hours ago' "$root/home/.hostisan-restore.linkOne"
	touch -d '2 hours ago' "$root/victim"
	run "$HESTIA_DIR/bin/v-restore-file-restic" admin 39d5ca2a "$root/home/admin/web/index.html"
	echo "$output"
	[ "$status" -eq 0 ]
	[ ! -e "$root/home/.hostisan-restore.oldOne" ]
	[ -d "$root/home/.hostisan-restore.newOne" ]
	[ -L "$root/home/.hostisan-restore.linkOne" ]
	[ "$(cat "$root/victim/file")" = keep ]
}

@test "restic file restore keeps the staging dir of a running restore" {
	file_restore_setup
	local d=$root/home/.hostisan-restore
	# older than an hour: one still running (lock held), one killed (lock
	# free), one from before the lock existed (no lock file)
	mkdir -p "$d.running/root" "$d.killed/root" "$d.nolock/root" "$d.fresh"
	: >"$d.running/.lock"
	: >"$d.killed/.lock"
	touch -d '2 hours ago' "$d.running" "$d.killed" "$d.nolock"
	exec {held}<"$d.running/.lock"
	flock -n "$held"
	run "$HESTIA_DIR/bin/v-restore-file-restic" admin 39d5ca2a "$root/home/admin/web/index.html"
	exec {held}<&-
	echo "$output"
	[ "$status" -eq 0 ]
	[ -d "$d.running/root" ]
	[ ! -e "$d.killed" ]
	[ ! -e "$d.nolock" ]
	# a dir without a lock file is stale only once it is old
	[ -d "$d.fresh" ]
	# this restore held its own dir's lock while restic ran
	[ "$(cat "$root/lockstate")" = held ]
	# a link planted as the lock file is not followed
	mkdir -p "$root/victim"
	echo keep >"$root/victim/file"
	rm "$d.running/.lock"
	ln -s "$root/victim/file" "$d.running/.lock"
	touch -h -d '2 hours ago' "$d.running/.lock"
	touch -d '2 hours ago' "$d.running"
	run "$HESTIA_DIR/bin/v-restore-file-restic" admin 39d5ca2a "$root/home/admin/web/index.html"
	[ "$status" -eq 0 ]
	[ "$(cat "$root/victim/file")" = keep ]
}

@test "restic file restore cleans its staging dir on failure" {
	file_restore_setup
	printf '#!/bin/bash\necho "runuser $*" >>"%s/calls"\nexit 1\n' "$root" >"$root/stub/runuser"
	run "$HESTIA_DIR/bin/v-restore-file-restic" admin 39d5ca2a "$root/home/admin/web/index.html"
	[ "$status" -eq 21 ]
	[ -s "$root/staged" ]
	[ -z "$(find "$root/home" -maxdepth 1 -name '.hostisan-restore.*')" ]
}

# --- product values on a partial install (product domain review) ---

# full_restore_setup: v-restore-user-full-restic in the sandbox, with the func/
# files it sources. restic fails, so the script stops right after v-add-user.
full_restore_setup() {
	local f
	for f in domain ip rebuild syshealth; do
		sandbox_rewrite "$repo/func/$f.sh" "$HESTIA_DIR/func/$f.sh"
	done
	echo "REPO='/srv/restic'" >"$HESTIA_DIR/conf/restic.conf"
	stub restic 1
	stub_bin v-add-user
	sandbox_bin v-restore-user-full-restic
}

@test "full restic restore creates the user without product.sh" {
	full_restore_setup
	[ ! -e "$HESTIA_DIR/install/common/hostisan/product.sh" ]
	run "$HESTIA_DIR/bin/v-restore-user-full-restic" newuser 39d5ca2a secretkey
	echo "$output"
	called '^v-add-user newuser secretkey root@localhost.localdomain$'
	[[ "$output" != *"product.sh"* ]]
	[[ "$output" == *"Invalid Restic repository"* ]]
}

@test "full restic restore takes the contact from product.env" {
	full_restore_setup
	mkdir -p "$HESTIA_DIR/install/common/hostisan"
	cp "$repo/install/common/hostisan/product.sh" "$HESTIA_DIR/install/common/hostisan/"
	sed 's/^HOSTISAN_DOMAIN=.*/HOSTISAN_DOMAIN=example.test/' "$repo/install/common/hostisan/product.env" \
		>"$HESTIA_DIR/install/common/hostisan/product.env"
	run "$HESTIA_DIR/bin/v-restore-user-full-restic" newuser 39d5ca2a secretkey
	called '^v-add-user newuser secretkey info@example.test$'
	# product.sh without its product.env: the same local contact
	rm "$HESTIA_DIR/install/common/hostisan/product.env"
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-restore-user-full-restic" newuser 39d5ca2a secretkey
	called '^v-add-user newuser secretkey root@localhost.localdomain$'
}

# --- updates from Git are refused (final review I3) ---

git_refusal='Updating from Git is not supported in Hostisan; use apt or src/hst_autocompile.sh --with-debs'

@test "git update is refused" {
	sandbox_bin v-update-sys-hestia-git
	stub git
	stub npm
	local args
	for args in '' 'hostisan main install' 'someone staging/beta install-auto'; do
		: >"$root/calls"
		# shellcheck disable=SC2086 # the arguments are meant to split
		run "$HESTIA_DIR/bin/v-update-sys-hestia-git" $args
		[ "$status" -eq 10 ]
		[ "$output" = "Error: $git_refusal" ]
		not_called '^(git|npm|apt-get|dpkg|curl|gpg|wget) '
	done
}

@test "git autoupdate cron is refused" {
	echo "CRON_SYSTEM='cron'" >>"$HESTIA_DIR/conf/hestia.conf"
	mkdir -p "$root/var/spool/cron/crontabs"
	echo 'MAILTO=""' >"$root/var/spool/cron/crontabs/hestiaweb"
	sandbox_bin v-add-cron-hestia-autoupdate
	sed -i "s#/var/spool/#$root/var/spool/#g" "$HESTIA_DIR/bin/v-add-cron-hestia-autoupdate"
	stub_bin v-restart-cron
	run "$HESTIA_DIR/bin/v-add-cron-hestia-autoupdate" git
	[ "$status" -eq 10 ]
	[ "$output" = "Error: $git_refusal" ]
	[ "$(cat "$root/var/spool/cron/crontabs/hestiaweb")" = 'MAILTO=""' ]
	# apt (the default) still works
	run "$HESTIA_DIR/bin/v-add-cron-hestia-autoupdate"
	[ "$status" -eq 0 ]
	# (the sandbox moved /usr/local/hestia under $root)
	grep -qE "^[0-9]+ [0-9]+ \* \* \* sudo $HESTIA_DIR/bin/v-update-sys-hestia-all\$" \
		"$root/var/spool/cron/crontabs/hestiaweb"
}

# --- hestia package updates (C8 F1) ---

@test "update all upgrades an installed terminal after hestia" {
	sandbox_bin v-update-sys-hestia-all
	stub_bin v-update-sys-hestia
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	[ "$status" -eq 0 ]
	[ "$(grep '^v-update-sys-hestia ' "$root/calls" | paste -sd,)" = \
		"v-update-sys-hestia hestia,v-update-sys-hestia hestia-nginx,v-update-sys-hestia hestia-php,v-update-sys-hestia hestia-web-terminal" ]
}

@test "update all skips a terminal that is not installed" {
	sandbox_bin v-update-sys-hestia-all
	stub_bin v-update-sys-hestia
	for status in '' 'deinstall ok config-files'; do
		: >"$root/calls"
		STUB_DPKG_STATUS=$status run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
		[ "$status" -eq 0 ]
		called '^v-update-sys-hestia hestia-php$'
		not_called '^v-update-sys-hestia hestia-web-terminal'
	done
}

@test "update one accepts the terminal" {
	sandbox_bin v-update-sys-hestia
	run "$HESTIA_DIR/bin/v-update-sys-hestia" hestia-web-terminal
	[ "$status" -eq 0 ]
	called '^apt-get install hestia-web-terminal -qq$'
	run "$HESTIA_DIR/bin/v-update-sys-hestia" nodejs
	[ "$status" -ne 0 ]
	not_called '^apt-get install nodejs'
}

@test "an update marks its apt run so the restarts wait for its end" {
	# the postinsts restart the panel, hestia and the terminal through
	# restart-unit.sh, which defers them inside an update (in-update.sh): one
	# restart each after the last package, not one per package script (rc7)
	sandbox_bin v-update-sys-hestia
	sandbox_bin v-update-sys-hestia-all
	cat >"$root/stub/apt-get" <<-EOF
		#!/bin/bash
		echo "apt-get \$* update=\${HOSTISAN_UPDATE:-}" >>"$root/calls"
	EOF
	chmod 755 "$root/stub/apt-get"
	# the nightly cron: not detached, not in the terminal
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	[ "$status" -eq 0 ]
	cat "$root/calls"
	[ "$(grep -c '^apt-get install .* update=1$' "$root/calls")" -eq 4 ]
	not_called '^apt-get install .* update=$'
}

# --- updates started in the web terminal (rc5 P1) ---

# in_terminal_sandbox: the update scripts run from a shell of the panel's web
# terminal (the cgroup file names its unit). systemd-run logs its argv and runs
# the command in line, as a unit of its own would: in another cgroup, with its
# --setenv values and its output not on this terminal. STUB_RUN_NOTHING=1
# starts nothing (a unit killed before it wrote anything).
in_terminal_sandbox() {
	local s
	mkdir -p "$HESTIA_DIR/install/common/hostisan" "$root/log"
	for s in in-terminal run-detached update-running; do
		sandbox_rewrite "$repo/install/common/hostisan/$s.sh" "$HESTIA_DIR/install/common/hostisan/$s.sh"
	done
	echo '0::/system.slice/hestia-web-terminal.service' >"$root/cgroup.term"
	echo '0::/system.slice/hostisan-update.service' >"$root/cgroup.unit"
	export HOSTISAN_CGROUP_FILE=$root/cgroup.term HOSTISAN_LOG_DIR=$root/log
	cat >"$root/stub/systemd-run" <<-EOF
		#!/bin/bash
		echo "systemd-run \$*" >>"$root/calls"
		[ "\${STUB_EXIT_systemd_run:-0}" = 0 ] || exit "\$STUB_EXIT_systemd_run"
		env=()
		while [ \$# -gt 0 ]; do
			case "\$1" in
				--setenv=*) env+=("\${1#--setenv=}") ;;
				--*) ;;
				*) break ;;
			esac
			shift
		done
		[ "\${STUB_RUN_NOTHING:-0}" = 1 ] && exit 0
		env "\${env[@]}" HOSTISAN_CGROUP_FILE=$root/cgroup.unit "\$@" </dev/null >/dev/null 2>&1
		exit 0
	EOF
	chmod 755 "$root/stub/systemd-run"
	stub sleep
	# v-update-sys-hestia prints what it updates and where it runs
	cat >"$HESTIA_DIR/bin/v-update-sys-hestia" <<-EOF
		#!/bin/bash
		echo "v-update-sys-hestia \$* detached=\${HOSTISAN_DETACHED:-} cgroup=\$(cat "\$HOSTISAN_CGROUP_FILE")" >>"$root/calls"
		echo "updating \$1"
		exit "\${STUB_EXIT_v_update_sys_hestia:-0}"
	EOF
	chmod 755 "$HESTIA_DIR/bin/v-update-sys-hestia"
}

# detached_unit: the unit name systemd-run got.
detached_unit() {
	grep -m1 '^systemd-run ' "$root/calls" | grep -oE 'hostisan-update-[0-9]{8}-[0-9]{6}-[0-9]+-[0-9]+' | head -n1
}

@test "update all moves to a unit of its own in the web terminal" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	local unit
	unit=$(detached_unit)
	[ -n "$unit" ]
	# started once (no loop), detached, with the marker
	[ "$(grep -c '^systemd-run ' "$root/calls")" -eq 1 ]
	grep -qxF "systemd-run --unit=$unit --collect --quiet --setenv=HOSTISAN_DETACHED=1 --setenv=DEBIAN_FRONTEND=noninteractive /bin/bash $HESTIA_DIR/install/common/hostisan/run-detached.sh --run $unit $root/log/$unit.log $HESTIA_DIR/bin/v-update-sys-hestia-all" "$root/calls"
	# every package once, in the unit's cgroup
	[ "$(grep '^v-update-sys-hestia ' "$root/calls" | paste -sd,)" = \
		"$(printf 'v-update-sys-hestia %s detached=1 cgroup=0::/system.slice/hostisan-update.service\n' hestia hestia-nginx hestia-php hestia-web-terminal | paste -sd,)" ]
	# the terminal shows the unit, the log, the notice and the update's output
	[[ "$output" == *"$unit"* ]]
	[[ "$output" == *"$root/log/$unit.log"* ]]
	[[ "$output" == *"restart once, after it; this terminal closes then. The update carries on."* ]]
	[[ "$output" == *"updating hestia"*"updating hestia-web-terminal"* ]]
	grep -q "^\[ \* \] Hostisan: $unit finished, exit status 0$" "$root/log/$unit.log"
}

@test "a detached update returns the update's exit status" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	STUB_EXIT_v_update_sys_hestia=9 run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output"
	[ "$status" -eq 9 ]
	[[ "$output" == *"exit status 9"* ]]
}

@test "update one moves to a unit of its own and shows apt's output" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia
	cat >"$root/stub/apt-get" <<-EOF
		#!/bin/bash
		echo "apt-get \$*" >>"$root/calls"
		echo "Setting up \${2:-}"
		echo "[ * ] Hostisan: notice" >&2
		exit "\${STUB_EXIT_apt_get:-0}"
	EOF
	chmod 755 "$root/stub/apt-get"
	run "$HESTIA_DIR/bin/v-update-sys-hestia" hestia
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	[ "$(grep -c '^systemd-run ' "$root/calls")" -eq 1 ]
	called "^systemd-run .* $HESTIA_DIR/bin/v-update-sys-hestia hestia\$"
	[ "$(grep -c '^apt-get install hestia -qq$' "$root/calls")" -eq 1 ]
	# detached, apt's output (the postinst's notices) goes to the log the
	# terminal follows, not to /dev/null
	[[ "$output" == *"Setting up hestia"* ]]
	[[ "$output" == *"Hostisan: notice"* ]]
	# a failed update: E_UPDATE comes back
	: >"$root/calls"
	STUB_EXIT_apt_get=100 run "$HESTIA_DIR/bin/v-update-sys-hestia" hestia
	echo "$output"
	[ "$status" -eq 19 ]
	[[ "$output" == *"Error: hestia update failed"* ]]
	# bad arguments fail at once, without a unit
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-update-sys-hestia" nodejs
	[ "$status" -ne 0 ]
	not_called '^systemd-run '
}

@test "a detached update that ends without a status says so" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	STUB_RUN_NOTHING=1 STUB_EXIT_systemctl=3 run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output"
	[ "$status" -eq 1 ]
	local unit
	unit=$(detached_unit)
	[[ "$output" == *"$unit ended without an exit status"*"$root/log/$unit.log"* ]]
	not_called '^v-update-sys-hestia '
}

@test "an update in the web terminal runs here when systemd-run fails" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	STUB_EXIT_systemd_run=1 run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	[[ "$output" == *"could not start"* ]]
	# in line, with the marker: the packages are not detached again
	[ "$(grep -c '^systemd-run ' "$root/calls")" -eq 1 ]
	[ "$(grep -c '^v-update-sys-hestia .* detached=1 cgroup=0::/system.slice/hestia-web-terminal.service$' "$root/calls")" -eq 4 ]
	# the log it made for the unit is gone
	[ -z "$(ls -A "$root/log")" ]
}

# same_second_date: date prints one timestamp for the unit names, and when
# TAKE_ALL=1 first makes every log the calling run-detached.sh (its parent)
# could pick that second: hostisan-update-<stamp>-<its pid>-<0..32767>.log.
same_second_date() {
	cat >"$root/stub/date" <<-EOF
		#!/bin/bash
		if [ "\${TAKE_ALL:-0}" = 1 ]; then
			for ((i = 0; i < 32768; i++)); do
				echo other >"$root/log/hostisan-update-20261104-120000-\$PPID-\$i.log"
			done
		fi
		echo 20261104-120000
	EOF
	chmod 755 "$root/stub/date"
}

@test "two updates started in the same second get their own unit and log" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	same_second_date
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	[ "$status" -eq 0 ]
	local first
	first=$(detached_unit)
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	[ "$status" -eq 0 ]
	local second
	second=$(detached_unit)
	echo "$first $second"
	[[ "$first" == hostisan-update-20261104-120000-* ]]
	[[ "$second" == hostisan-update-20261104-120000-* ]]
	[ "$first" != "$second" ]
	[ "$(grep -c 'finished, exit status 0$' "$root/log/$first.log")" -eq 1 ]
	[ "$(grep -c 'finished, exit status 0$' "$root/log/$second.log")" -eq 1 ]
}

@test "an update whose log name is taken runs here and keeps that log" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	same_second_date
	TAKE_ALL=1 run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output" | head -n5
	[ "$status" -eq 0 ]
	[[ "$output" == *"could not start"* ]]
	# no unit, and the update ran here, not detached again
	not_called '^systemd-run '
	[ "$(grep -c '^v-update-sys-hestia .* detached=1 cgroup=0::/system.slice/hestia-web-terminal.service$' "$root/calls")" -eq 4 ]
	# the log another update owns is neither written nor removed
	[ "$(find "$root/log" -name 'hostisan-update-*.log' | wc -l)" -eq 32768 ]
	[ -z "$(find "$root/log" -name 'hostisan-update-*.log' -exec grep -L '^other$' {} +)" ]
	run ! grep -rq 'finished, exit status' "$root/log"
}

@test "an older update loop in the web terminal runs the new update here" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia
	cat >"$root/stub/apt-get" <<-EOF
		#!/bin/bash
		echo "apt-get \$* cgroup=\$(cat "\$HOSTISAN_CGROUP_FILE")" >>"$root/calls"
		exit 0
	EOF
	chmod 755 "$root/stub/apt-get"
	# rc4's loop: no detach of its own. It is already running when the new
	# hestia lands, and calls the new v-update-sys-hestia for the rest. Each
	# package in a unit of its own would restart hestia in line there (the
	# nginx and php postinsts) and hang this loop up before the terminal
	# package; in line, the restarts are deferred until the loop ends.
	cat >"$HESTIA_DIR/bin/v-update-sys-hestia-all" <<-EOF
		#!/bin/bash
		for package in hestia hestia-nginx hestia-php hestia-web-terminal; do
			$HESTIA_DIR/bin/v-update-sys-hestia "\$package"
		done
	EOF
	chmod 755 "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	not_called '^systemd-run '
	[ "$(grep -c '^apt-get install .* -qq cgroup=0::/system.slice/hestia-web-terminal.service$' "$root/calls")" -eq 4 ]
	[[ "$output" == *"older update loop"* ]]
	[[ "$output" != *"could not start"* ]]
	[ -z "$(ls -A "$root/log")" ]
	# called by hand from a shell, it still detaches
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-update-sys-hestia" hestia
	[ "$status" -eq 0 ]
	[ "$(grep -c '^systemd-run ' "$root/calls")" -eq 1 ]
}

@test "an update in the web terminal runs in line without the hostisan helpers" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	sandbox_bin v-update-sys-hestia
	stub apt-get
	# install/common/hostisan missing (a partial copy): the update runs here, as
	# before round 4
	rm -rf "$HESTIA_DIR/install/common/hostisan"
	run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	echo "$output"
	[ "$status" -eq 0 ]
	not_called '^systemd-run '
	[ "$(grep -c '^apt-get install ' "$root/calls")" -eq 4 ]
	: >"$root/calls"
	run "$HESTIA_DIR/bin/v-update-sys-hestia" hestia-php
	[ "$status" -eq 0 ]
	not_called '^systemd-run '
	called '^apt-get install hestia-php -qq$'
}

@test "an update outside the web terminal or already detached runs in line" {
	in_terminal_sandbox
	sandbox_bin v-update-sys-hestia-all
	sandbox_bin v-update-sys-hestia
	cat >"$root/stub/apt-get" <<-EOF
		#!/bin/bash
		echo "apt-get \$*" >>"$root/calls"
		[ "\$1" = install ] && echo "Setting up"
		exit 0
	EOF
	chmod 755 "$root/stub/apt-get"
	HOSTISAN_CGROUP_FILE=$root/cgroup.unit run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	[ "$status" -eq 0 ]
	# not detached (the nightly cron), apt's output still goes to /dev/null
	echo "$output"
	[ "$output" = "" ]
	HOSTISAN_DETACHED=1 run "$HESTIA_DIR/bin/v-update-sys-hestia-all"
	[ "$status" -eq 0 ]
	not_called '^systemd-run '
	[ "$(grep -c '^apt-get install ' "$root/calls")" -eq 8 ]
}

# updates_stubs INSTALLED CANDIDATE: dpkg-query and apt-cache for the updates list.
# Every package is at 1:1.10.5-101; INSTALLED lists the installed ones.
updates_stubs() {
	cat >"$root/stub/dpkg-query" <<-EOF
		#!/bin/bash
		case " $1 " in *" \${@: -1} "*) ;; *) exit 1 ;; esac
		if [ "\$1" = -s ]; then
			printf 'Package: %s\nArchitecture: amd64\nVersion: 1:1.10.5-101+ubuntu24.04\n' "\$2"
		else
			printf 'install ok installed'
		fi
	EOF
	cat >"$root/stub/apt-cache" <<-EOF
		#!/bin/bash
		printf '%s:\n  Installed: 1:1.10.5-101+ubuntu24.04\n  Candidate: %s\n' "\$2" "$2"
	EOF
	# the real dpkg compares versions; stat reads no /var/lib/dpkg
	cat >"$root/stub/dpkg" <<-'EOF'
		#!/bin/bash
		exec /usr/bin/dpkg "$@"
	EOF
	cat >"$root/stub/stat" <<-'EOF'
		#!/bin/bash
		echo 1761868800
	EOF
	# list scripts parse their rows with Hestia's php; these rows are plain
	mkdir -p "$HESTIA_DIR/php/bin"
	printf '#!/bin/bash\nprintf "%%s" "$2"\n' >"$HESTIA_DIR/php/bin/php"
	chmod 755 "$root"/stub/{dpkg-query,apt-cache,dpkg,stat} "$HESTIA_DIR/php/bin/php"
}

@test "updates list shows an installed terminal" {
	sandbox_bin v-list-sys-hestia-updates
	updates_stubs "hestia hestia-php hestia-nginx hestia-web-terminal" 1:1.10.5-101+ubuntu24.04
	run "$HESTIA_DIR/bin/v-list-sys-hestia-updates" json
	[ "$status" -eq 0 ]
	[ "$(jq -r 'keys_unsorted | join(",")' <<<"$output")" = "hestia,hestia-php,hestia-nginx,hestia-web-terminal" ]
	[ "$(jq -r '."hestia-web-terminal".VERSION' <<<"$output")" = 1:1.10.5-101+ubuntu24.04 ]
	[ "$(jq -r '[.[].UPDATED] | unique | join(",")' <<<"$output")" = yes ]
	updates_stubs "hestia hestia-php hestia-nginx" 1:1.10.5-101+ubuntu24.04
	run "$HESTIA_DIR/bin/v-list-sys-hestia-updates" json
	[ "$(jq -r 'keys_unsorted | join(",")' <<<"$output")" = "hestia,hestia-php,hestia-nginx" ]
}

@test "updates list sees a newer epoch build" {
	# "Candidate: 1:1.10.5-102" was cut to "1" at the colon, so no update showed.
	sandbox_bin v-list-sys-hestia-updates
	updates_stubs "hestia hestia-web-terminal" 1:1.10.5-102+ubuntu24.04
	run "$HESTIA_DIR/bin/v-list-sys-hestia-updates" json
	[ "$status" -eq 0 ]
	[ "$(jq -r '.hestia.UPDATED' <<<"$output")" = no ]
	[ "$(jq -r '."hestia-web-terminal".UPDATED' <<<"$output")" = no ]
	updates_stubs "hestia" '(none)'
	run "$HESTIA_DIR/bin/v-list-sys-hestia-updates" json
	[ "$(jq -r '.hestia.UPDATED' <<<"$output")" = yes ]
}
