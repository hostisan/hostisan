#!/usr/bin/env bats
# Upgrade routine (func/upgrade.sh upgrade_start_routine, hostisan/CORE-EDITS.md).
# Hostisan builds stay at upstream's VERSION (1.10.5) while carrying later
# upstream version scripts (1.10.6.sh), so every script must run exactly once.
# The hestia postinst's version handling runs in the core sandbox.

# shellcheck disable=SC2154 # root, repo and HESTIA_DIR come from core_sandbox.bash
load core_sandbox

bats_require_minimum_version 1.5.0

setup() {
	sandbox_init
	sandbox_rewrite "$repo/func/upgrade.sh" "$HESTIA_DIR/func/upgrade.sh"
	sandbox_rewrite "$repo/func/syshealth.sh" "$HESTIA_DIR/func/syshealth.sh"
	steps=$HESTIA_DIR/install/upgrade/versions
	mkdir -p "$steps"
	for v in 1.10.4 1.10.5 1.10.6; do
		add_step "$v"
	done
	echo "VERSION='1.10.5'" >>"$HESTIA_DIR/conf/hestia.conf"
	# A config setter that keeps the value, as the real one does.
	cat >"$HESTIA_DIR/bin/v-change-sys-config-value" <<-EOF
		#!/bin/bash
		echo "v-change-sys-config-value \$*" >>"$root/calls"
		conf=$HESTIA_DIR/conf/hestia.conf
		grep -v "^\$1=" "\$conf" >"\$conf.new"
		echo "\$1='\$2'" >>"\$conf.new"
		mv "\$conf.new" "\$conf"
	EOF
	# The postinst's version steps: read hestia.conf, run the scripts, store
	# the package version (epoch and revision already stripped).
	cat >"$root/upgrade" <<-EOF
		#!/bin/bash
		source $root/etc/hestiacp/hestia.conf
		source \$HESTIA/func/main.sh
		source \$HESTIA/func/upgrade.sh
		source_conf "\$HESTIA/conf/hestia.conf"
		new_version=\$1
		upgrade_start_routine
		upgrade_set_version "\$new_version"
	EOF
	chmod 755 "$root/upgrade" "$HESTIA_DIR/bin/v-change-sys-config-value"
	: >"$root/ran"
}

teardown() {
	sandbox_teardown
}

# add_step VERSION: a version script that records each run.
add_step() {
	echo "echo $1 >>'$root/ran'" >"$steps/$1.sh"
}

ran() {
	paste -sd' ' "$root/ran"
}

conf() {
	sed -n "s/^$1='\(.*\)'$/\1/p" "$HESTIA_DIR/conf/hestia.conf"
}

@test "two upgrades of the same version run 1.10.6 once" {
	run "$root/upgrade" 1.10.5
	[ "$status" -eq 0 ]
	run "$root/upgrade" 1.10.5
	[ "$status" -eq 0 ]
	[ "$(ran)" = "1.10.6" ]
	[ "$(conf VERSION)" = 1.10.5 ]
	[ "$(conf UPGRADE_LAST_STEP)" = 1.10.6 ]
}

@test "an upgrade at the latest step re-runs no script" {
	# Upstream re-sourced the installed version's script on every upgrade.
	sed -i "s/^VERSION=.*/VERSION='1.10.6'/" "$HESTIA_DIR/conf/hestia.conf"
	run "$root/upgrade" 1.10.6
	[ "$status" -eq 0 ]
	[ "$(ran)" = "" ]
}

@test "an old box runs each newer script once, in order" {
	sed -i "s/^VERSION=.*/VERSION='1.10.3'/" "$HESTIA_DIR/conf/hestia.conf"
	run "$root/upgrade" 1.10.5
	run "$root/upgrade" 1.10.5
	[ "$(ran)" = "1.10.4 1.10.5 1.10.6" ]
}

@test "crossing a new version runs only its script" {
	run "$root/upgrade" 1.10.5
	add_step 1.10.7
	run "$root/upgrade" 1.10.7
	[ "$status" -eq 0 ]
	run "$root/upgrade" 1.10.7
	[ "$(ran)" = "1.10.6 1.10.7" ]
	[ "$(conf VERSION)" = 1.10.7 ]
	[ "$(conf UPGRADE_LAST_STEP)" = 1.10.7 ]
}

@test "a fresh install stamps the latest step" {
	for f in "$repo"/install/hst-install-{debian,ubuntu}.sh; do
		grep -q '^write_config_value "UPGRADE_LAST_STEP" ' "$f"
	done
	# so a fresh 1.10.5 install never runs 1.10.6.sh (its templates have it)
	echo "UPGRADE_LAST_STEP='1.10.6'" >>"$HESTIA_DIR/conf/hestia.conf"
	run "$root/upgrade" 1.10.5
	[ "$(ran)" = "" ]
}

# --- Git autoupdate cron migration (final review I3) ---

# git_cron_step: the real 1.10.6.1.sh in the sandbox, with hestiaweb's
# crontab under $root, on a box that already ran 1.10.6.
git_cron_step() {
	sandbox_rewrite "$repo/install/upgrade/versions/1.10.6.1.sh" "$steps/1.10.6.1.sh"
	sed -i "s#/var/spool/#$root/var/spool/#g" "$steps/1.10.6.1.sh"
	mkdir -p "$root/var/spool/cron/crontabs"
	cron=$root/var/spool/cron/crontabs/hestiaweb
	echo "UPGRADE_LAST_STEP='1.10.6'" >>"$HESTIA_DIR/conf/hestia.conf"
	echo "CRON_SYSTEM='cron'" >>"$HESTIA_DIR/conf/hestia.conf"
	# upgrade messages go to $HESTIA_BACKUP/message.log (main.sh sets it)
	HESTIA_BACKUP=$root/backup
	mkdir -p "$HESTIA_BACKUP"
	sed -i "s#^HESTIA_BACKUP=.*#HESTIA_BACKUP=$HESTIA_BACKUP#" "$HESTIA_DIR/func/main.sh"
	# the real one adds the apt line; STUB_EXIT_v_add_cron_hestia_autoupdate=1
	# fails without adding it
	cat >"$HESTIA_DIR/bin/v-add-cron-hestia-autoupdate" <<-EOF
		#!/bin/bash
		echo "v-add-cron-hestia-autoupdate \$*" >>"$root/calls"
		[ "\${STUB_EXIT_v_add_cron_hestia_autoupdate:-0}" = 0 ] || exit 1
		echo '12 3 * * * sudo /usr/local/hestia/bin/v-update-sys-hestia-all' >>"$cron"
	EOF
	chmod 755 "$HESTIA_DIR/bin/v-add-cron-hestia-autoupdate"
}

git_cron='0 0 * * * sudo /usr/local/hestia/bin/v-update-sys-hestia-git'
apt_cron='12 3 * * * sudo /usr/local/hestia/bin/v-update-sys-hestia-all'

@test "the git autoupdate cron job becomes the apt one, once" {
	git_cron_step
	printf '%s\n' 'MAILTO=""' '*/2 * * * * sudo /usr/local/hestia/bin/v-update-sys-queue restart' \
		'0 0 * * * sudo /usr/local/hestia/bin/v-update-sys-hestia-git' >"$cron"
	run "$root/upgrade" 1.10.5
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	[ "$(cat "$cron")" = "$(printf '%s\n' 'MAILTO=""' '*/2 * * * * sudo /usr/local/hestia/bin/v-update-sys-queue restart' "$apt_cron")" ]
	called '^v-add-cron-hestia-autoupdate apt$'
	called '^v-log-action system Info Updates .*Git autoupdate cron job replaced'
	grep -q 'was replaced with the apt automatic update' "$HESTIA_BACKUP/message.log"
	[ "$(conf UPGRADE_LAST_STEP)" = 1.10.6.1 ]
	# it runs once
	: >"$root/calls"
	echo '0 0 * * * sudo /usr/local/hestia/bin/v-update-sys-hestia-git' >>"$cron"
	run "$root/upgrade" 1.10.5
	[ "$status" -eq 0 ]
	not_called 'v-add-cron-hestia-autoupdate'
}

@test "the git autoupdate cron job stays when apt cannot replace it" {
	# v-add-cron-hestia-autoupdate refuses with the cron system off or in demo mode
	local conf=$HESTIA_DIR/conf/hestia.conf extra
	for extra in "CRON_SYSTEM=''" "DEMO_MODE='yes'"; do
		git_cron_step
		echo "$extra" >>"$conf"
		echo "$git_cron" >"$cron"
		: >"$root/calls"
		rm -f "$HESTIA_BACKUP/message.log"
		run "$root/upgrade" 1.10.5
		echo "$output"
		cat "$root/calls"
		[ "$status" -eq 0 ]
		[ "$(cat "$cron")" = "$git_cron" ]
		not_called 'v-add-cron-hestia-autoupdate'
		not_called 'Info Updates .*replaced'
		called '^v-log-action system Warning Updates '
		grep -q 'could not be replaced' "$HESTIA_BACKUP/message.log"
		# the step is done all the same
		[ "$(conf UPGRADE_LAST_STEP)" = 1.10.6.1 ]
		sed -i -e '/^UPGRADE_LAST_STEP=/d' -e '/^CRON_SYSTEM=/d' -e '/^DEMO_MODE=/d' "$conf"
	done
}

@test "a failed apt autoupdate says autoupdate is off" {
	git_cron_step
	echo "$git_cron" >"$cron"
	STUB_EXIT_v_add_cron_hestia_autoupdate=1 run "$root/upgrade" 1.10.5
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	called '^v-add-cron-hestia-autoupdate apt$'
	run ! grep -q 'v-update-sys-hestia' "$cron"
	not_called 'Info Updates .*replaced'
	called '^v-log-action system Warning Updates .*removed'
	grep -q 'Automatic updates are off' "$HESTIA_BACKUP/message.log"
	run ! grep -q 'was replaced' "$HESTIA_BACKUP/message.log"
}

@test "no git autoupdate cron job: the step changes nothing" {
	git_cron_step
	printf '%s\n' 'MAILTO=""' '41 4 * * * sudo /usr/local/hestia/bin/v-update-sys-hestia-all' >"$cron"
	cp "$cron" "$root/cron.before"
	run "$root/upgrade" 1.10.5
	[ "$status" -eq 0 ]
	cmp "$cron" "$root/cron.before"
	not_called 'v-add-cron-hestia-autoupdate|v-log-action'
	[ "$(conf UPGRADE_LAST_STEP)" = 1.10.6.1 ]
}

# --- service restarts after an upgrade (final review I4) ---

@test "upgrade restarts no web terminal in line" {
	# setup.sh restarts the terminal through restart-terminal.sh, which defers
	# it when apt runs in the terminal; a plain restart here killed that dpkg.
	local v
	for v in v-restart-service v-restart-mail v-restart-web v-restart-proxy v-restart-dns v-restart-ftp v-list-sys-php; do
		stub_bin "$v"
	done
	stub sleep
	cat >>"$HESTIA_DIR/conf/hestia.conf" <<-'EOF'
		UPGRADE_RESTART_SERVICES='true'
		WEB_TERMINAL='true'
		MAIL_SYSTEM='exim4'
	EOF
	cat >"$root/restart" <<-EOF
		#!/bin/bash
		source $root/etc/hestiacp/hestia.conf
		source \$HESTIA/func/main.sh
		source \$HESTIA/func/upgrade.sh
		source_conf "\$HESTIA/conf/hestia.conf"
		upgrade_restart_services
	EOF
	chmod 755 "$root/restart"
	run "$root/restart"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	called '^v-restart-mail '
	called '^v-restart-service hestia$'
	not_called 'hestia-web-terminal'
}

@test "upgrade defers the hestia restart when run from the web terminal" {
	# hestia-nginx carries every terminal session: restarting it in line hung
	# up the shell running the upgrade (rc5 P1), so restart-unit.sh defers it.
	local v s
	for v in v-restart-service v-restart-mail v-list-sys-php; do
		stub_bin "$v"
	done
	stub sleep
	stub systemd-run
	mkdir -p "$HESTIA_DIR/install/common/hostisan"
	for s in restart-unit in-terminal; do
		sandbox_rewrite "$repo/install/common/hostisan/$s.sh" "$HESTIA_DIR/install/common/hostisan/$s.sh"
	done
	echo "UPGRADE_RESTART_SERVICES='true'" >>"$HESTIA_DIR/conf/hestia.conf"
	cat >"$root/restart" <<-EOF
		#!/bin/bash
		source $root/etc/hestiacp/hestia.conf
		source \$HESTIA/func/main.sh
		source \$HESTIA/func/upgrade.sh
		source_conf "\$HESTIA/conf/hestia.conf"
		upgrade_restart_services
	EOF
	chmod 755 "$root/restart"
	echo '0::/system.slice/hestia-web-terminal.service' >"$root/cgroup"
	# systemctl is-active: no restart scheduled yet
	HOSTISAN_CGROUP_FILE=$root/cgroup STUB_EXIT_systemctl=3 run "$root/restart"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	not_called '^v-restart-service hestia$'
	called "^systemd-run --on-active=30 --unit=hostisan-hestia-restart --collect --timer-property=RemainAfterElapse=no /bin/bash $HESTIA_DIR/install/common/hostisan/restart-unit.sh hestia.service --deferred restart\$"
	[[ "$output" == *"hestia.service restarts once dpkg is done"* ]]
	# outside the terminal: v-restart-service as upstream does
	: >"$root/calls"
	echo '0::/user.slice/user-0.slice/session-1.scope' >"$root/cgroup"
	HOSTISAN_CGROUP_FILE=$root/cgroup run "$root/restart"
	[ "$status" -eq 0 ]
	called '^v-restart-service hestia$'
	not_called '^systemd-run '
}

@test "upgrade defers the hestia restart inside an update" {
	# one update asks for hestia from setup.sh, here and from the nginx and php
	# postinsts: each in-line restart hung up every web terminal (rc7), so
	# inside an update they wait for its end and run once.
	local v s
	for v in v-restart-service v-restart-mail v-list-sys-php; do
		stub_bin "$v"
	done
	stub sleep
	stub systemd-run
	mkdir -p "$HESTIA_DIR/install/common/hostisan" "$root/proc"
	for s in restart-unit in-terminal in-update; do
		sandbox_rewrite "$repo/install/common/hostisan/$s.sh" "$HESTIA_DIR/install/common/hostisan/$s.sh"
	done
	echo "UPGRADE_RESTART_SERVICES='true'" >>"$HESTIA_DIR/conf/hestia.conf"
	cat >"$root/restart" <<-EOF
		#!/bin/bash
		source $root/etc/hestiacp/hestia.conf
		source \$HESTIA/func/main.sh
		source \$HESTIA/func/upgrade.sh
		source_conf "\$HESTIA/conf/hestia.conf"
		upgrade_restart_services
	EOF
	chmod 755 "$root/restart"
	# an ssh session or the nightly cron: not the terminal
	echo '0::/user.slice/user-0.slice/session-1.scope' >"$root/cgroup"
	export HOSTISAN_CGROUP_FILE=$root/cgroup HOSTISAN_PROC_DIR=$root/proc
	HOSTISAN_UPDATE=1 STUB_EXIT_systemctl=3 run "$root/restart"
	echo "$output"
	cat "$root/calls"
	[ "$status" -eq 0 ]
	not_called '^v-restart-service hestia$'
	called "^systemd-run --on-active=30 --unit=hostisan-hestia-restart --collect --timer-property=RemainAfterElapse=no /bin/bash $HESTIA_DIR/install/common/hostisan/restart-unit.sh hestia.service --deferred restart\$"
	[[ "$output" == *"hestia.service restarts once this update is done"* ]]
	# a raw apt upgrade over ssh: v-restart-service as upstream does
	: >"$root/calls"
	run "$root/restart"
	[ "$status" -eq 0 ]
	called '^v-restart-service hestia$'
	not_called '^systemd-run '
}
