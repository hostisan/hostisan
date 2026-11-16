#!/usr/bin/env bats
# Roundcube install and upgrade (bin/v-add-sys-roundcube, func/upgrade.sh
# upgrade_roundcube; hostisan/CORE-EDITS.md). A Roundcube core left in
# /var/lib/roundcube without its /etc/roundcube config (a reinstall over a
# cleaned box) used to count as installed: the installer and every upgrade
# stopped at "is equal" / "up to date" and webmail answered HTTP 500.

# shellcheck disable=SC2154 # root, repo and HESTIA_DIR come from core_sandbox.bash
load core_sandbox

bats_require_minimum_version 1.5.0

rc_v=1.7.4

setup() {
	sandbox_init
	sandbox_rewrite "$repo/func/db.sh" "$HESTIA_DIR/func/db.sh"
	sandbox_bin v-add-sys-roundcube
	# Paths sandbox_rewrite leaves alone.
	sed -i -e "s#/var/#$root/var/#g" -e "s#/usr/share/roundcube#$root/usr/share/roundcube#g" \
		-e "s#/usr/bin/php#$root/stub/php#g" "$HESTIA_DIR/bin/v-add-sys-roundcube"
	mkdir -p "$root/var/lib" "$root/var/log" "$root/etc/logrotate.d" "$HESTIA_DIR/install/upgrade"
	cp -r "$repo/install/common" "$HESTIA_DIR/install/common"
	echo "rc_v='$rc_v'" >"$HESTIA_DIR/install/upgrade/upgrade.conf"
	echo "DB_SYSTEM='mysql'" >>"$HESTIA_DIR/conf/hestia.conf"
	echo "HOST='localhost' USER='root' PASSWORD='rootpw' PORT='3306'" >"$HESTIA_DIR/conf/mysql.conf"
	# mysql_connect parses that row with Hestia's php; the row is plain
	mkdir -p "$HESTIA_DIR/php/bin"
	printf '#!/bin/bash\nprintf "%%s" "$2"\n' >"$HESTIA_DIR/php/bin/php"
	chmod 755 "$HESTIA_DIR/php/bin/php"

	make_tarball
	# wget -O FILE copies the fake release there.
	cat >"$root/stub/wget" <<-EOF
		#!/bin/bash
		echo "wget \$*" >>"$root/calls"
		while [ \$# -gt 0 ]; do [ "\$1" = -O ] && cp "$root/rc.tar.gz" "\$2"; shift; done
	EOF
	# mariadb/mysql: log each query; a version for mysql_connect; the
	# roundcube database exists when STUB_RC_DB=1.
	for tool in mariadb mysql; do
		cat >"$root/stub/$tool" <<-EOF
			#!/bin/bash
			case "\$*" in *SELECT\ VERSION*) echo 11.8.9-MariaDB; exit ;; esac
			sql=\$(cat)
			echo "$tool \$sql" >>"$root/calls"
			case "\$sql" in "SHOW DATABASES LIKE 'roundcube'"*) [ "\${STUB_RC_DB:-0}" = 1 ] && echo roundcube ;; esac
			exit 0
		EOF
	done
	cat >"$root/stub/id" <<-'EOF'
		#!/bin/bash
		echo 0
	EOF
	chmod 755 "$root/stub/wget" "$root/stub/mariadb" "$root/stub/mysql" "$root/stub/id"
	stub chown
	stub phpenmod
	stub php
	# dpkg --compare-versions PHP lt 8.1: false
	export STUB_EXIT_dpkg=1
	stub_bin v-add-user-composer 1
	cat >"$HESTIA_DIR/bin/v-list-default-php" <<-'EOF'
		#!/bin/bash
		echo 8.3
	EOF
	chmod 755 "$HESTIA_DIR/bin/v-list-default-php"
	rc_dir=$root/var/lib/roundcube
	rc_conf=$root/etc/roundcube
}

teardown() {
	sandbox_teardown
}

# make_tarball: a fake roundcubemail-$rc_v-complete.tar.gz.
make_tarball() {
	local t=$root/build/roundcubemail-$rc_v
	mkdir -p "$t"/{program/include,config,SQL,bin,plugins/password/drivers,plugins/newmail_notifier,plugins/zipdownload}
	echo "define('RCMAIL_VERSION', '$rc_v');" >"$t/program/include/iniset.php"
	echo '<?php // defaults' >"$t/config/defaults.inc.php"
	echo 'CREATE TABLE users (id int);' >"$t/SQL/mysql.initial.sql"
	tar czf "$root/rc.tar.gz" -C "$root/build" "roundcubemail-$rc_v"
}

# installed_core: the core at $rc_v in /var/lib/roundcube, config -> /etc/roundcube.
installed_core() {
	mkdir -p "$rc_dir/program/include"
	echo "define('RCMAIL_VERSION', '$rc_v');" >"$rc_dir/program/include/iniset.php"
	ln -s "$rc_conf/" "$rc_dir/config"
}

@test "roundcube reinstalls a core whose config is missing" {
	installed_core
	run "$HESTIA_DIR/bin/v-add-sys-roundcube"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	[ -f "$rc_conf/config.inc.php" ]
	[ -f "$rc_conf/plugins/password/config.inc.php" ]
	[ "$(readlink "$rc_dir/config")" = "$rc_conf/" ]
	! grep -q '%password%\|%des_key%' "$rc_conf/config.inc.php"
	called "CREATE DATABASE \`roundcube\`"
	called 'CREATE TABLE users'
}

@test "roundcube keeps the database when it rebuilds a lost config" {
	installed_core
	STUB_RC_DB=1 run "$HESTIA_DIR/bin/v-add-sys-roundcube"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	[ -f "$rc_conf/config.inc.php" ]
	not_called 'CREATE DATABASE|CREATE TABLE|DROP '
	# The kept user gets the password written into the new config.
	local pass
	pass=$(sed -n "s#.*mysql://roundcube:\([^@]*\)@.*#\1#p" "$rc_conf/config.inc.php")
	[ -n "$pass" ]
	called 'GRANT ALL ON `roundcube`\.\* TO `roundcube`@localhost$'
	called "^ +IDENTIFIED BY '$pass'$"
}

@test "roundcube with its config at the same version changes nothing" {
	installed_core
	mkdir -p "$rc_conf"
	echo '<?php // kept' >"$rc_conf/config.inc.php"
	run "$HESTIA_DIR/bin/v-add-sys-roundcube"
	[ "$status" -eq 2 ]
	[[ "$output" == *"is equal"* ]]
	[ "$(cat "$rc_conf/config.inc.php")" = '<?php // kept' ]
	not_called 'wget|mariadb|mysql'
}

# --- upgrade_roundcube (func/upgrade.sh) ---

upgrade_rc() {
	sandbox_rewrite "$repo/func/upgrade.sh" "$HESTIA_DIR/func/upgrade.sh"
	sandbox_rewrite "$repo/func/syshealth.sh" "$HESTIA_DIR/func/syshealth.sh"
	sed -i "s#/var/lib/roundcube#$rc_dir#g; s#/usr/share/roundcube#$root/usr/share/roundcube#g" \
		"$HESTIA_DIR/func/upgrade.sh"
	stub_bin v-add-sys-roundcube
	echo "WEBMAIL_SYSTEM='roundcube'" >>"$HESTIA_DIR/conf/hestia.conf"
	cat >"$root/upgrade-rc" <<-EOF
		#!/bin/bash
		source $root/etc/hestiacp/hestia.conf
		source \$HESTIA/func/main.sh
		source \$HESTIA/func/upgrade.sh
		source_conf "\$HESTIA/conf/hestia.conf"
		rc_v=$rc_v
		upgrade_roundcube
	EOF
	chmod 755 "$root/upgrade-rc"
	run "$root/upgrade-rc"
	echo "$output"
}

@test "an upgrade reinstalls an up-to-date roundcube that lost its config" {
	installed_core
	upgrade_rc
	[ "$status" -eq 0 ]
	called '^v-add-sys-roundcube'
}

@test "an upgrade leaves an up-to-date roundcube with its config alone" {
	installed_core
	mkdir -p "$rc_conf"
	echo '<?php' >"$rc_conf/config.inc.php"
	upgrade_rc
	[ "$status" -eq 0 ]
	[[ "$output" == *"up to date"* ]]
	not_called 'v-add-sys-roundcube'
}
