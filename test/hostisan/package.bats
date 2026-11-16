#!/usr/bin/env bats
# The Hostisan debs (Plan 6, B7): control files, maintainer scripts and the
# scripts they run, and the built packages.
#
# Tests named "deb: ..." inspect built debs and skip unless DEB_DIR holds them:
#   src/hst_autocompile.sh --hestia --web-terminal --noinstall --keepbuild \
#       --dontinstalldeps '~localsrc'            (debs land in $BUILD_DIR/deb)
#   DEB_DIR=<that dir> test/test_helper/bats-core/bin/bats test/hostisan/package.bats
# The other tests run the maintainer scripts and install/common/hostisan/*.sh in
# a temp root, with systemctl, pkill and the v-* scripts stubbed.

bats_require_minimum_version 1.5.0

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/package.XXXXXX")
	# shellcheck source=install/common/hostisan/product.env
	source "$repo/install/common/hostisan/product.env"
}

teardown() {
	rm -rf "$work"
}

# ---------------------------------------------------------------- helpers

# need_debs: skip unless DEB_DIR holds the hestia and terminal debs.
need_debs() {
	[ -n "${DEB_DIR:-}" ] || skip "DEB_DIR not set"
	core=$(ls "$DEB_DIR"/hestia_*.deb 2>/dev/null | tail -n1)
	term=$(ls "$DEB_DIR"/hestia-web-terminal_*.deb 2>/dev/null | tail -n1)
	if [ -z "$core" ] || [ -z "$term" ]; then
		skip "no hestia/hestia-web-terminal deb in $DEB_DIR"
	fi
}

# field FILE NAME: the value of control field NAME, continuation lines included.
field() {
	awk -v name="$2" '
		/^[^ \t]/ { on = (index($0, name ":") == 1); if (on) { sub(/^[^:]*:[ \t]*/, ""); print }; next }
		on { print }' "$1"
}

# sandbox: a temp root $R with Hestia under $H, stub tools in $R/stub that log
# "NAME args" to $R/calls, and stub v-* scripts in $H/bin.
sandbox() {
	R=$work/root
	H=$R/usr/local/hestia
	mkdir -p "$R/stub" "$H"/{bin,conf,install/common/hostisan,data/users/admin} \
		"$R/etc/apt/sources.list.d" "$R/usr/share/keyrings" "$R/etc/hestiacp"
	: >"$R/calls"
	stub pkill
	# systemctl logs and succeeds, except is-active on a deferred restart's
	# timer or service (hostisan-*-restart), which are active only when
	# STUB_TIMER_ACTIVE=1.
	cat >"$R/stub/systemctl" <<-EOF
		#!/bin/bash
		echo "systemctl \$*" >>"$R/calls"
		[[ "\$*" == "is-active --quiet hostisan-"*"-restart."* ]] && [ "\${STUB_TIMER_ACTIVE:-0}" != 1 ] && exit 3
		exit 0
	EOF
	chmod 755 "$R/stub/systemctl"
	# dpkg-query -S succeeds (path owned) only when STUB_OWNED=1.
	cat >"$R/stub/dpkg-query" <<-EOF
		#!/bin/bash
		echo "dpkg-query \$*" >>"$R/calls"
		[ "\${STUB_OWNED:-0}" = 1 ]
	EOF
	cat >"$R/stub/id" <<-'EOF'
		#!/bin/bash
		echo 0
	EOF
	# getent passwd hestiaweb fails when STUB_NO_HESTIAWEB=1.
	cat >"$R/stub/getent" <<-'EOF'
		#!/bin/bash
		[ "${STUB_NO_HESTIAWEB:-0}" = 1 ] && exit 2
		echo "$2:x:998:998::/nonexistent:/usr/sbin/nologin"
	EOF
	# visudo logs the file it checks, keeps a copy of it in $R/visudo.seen and
	# exits STUB_VISUDO_RC (default 0).
	cat >"$R/stub/visudo" <<-EOF
		#!/bin/bash
		echo "visudo \$*" >>"$R/calls"
		cp "\${!#}" "$R/visudo.seen"
		exit "\${STUB_VISUDO_RC:-0}"
	EOF
	# sudo -V prints STUB_SUDO_V (default: the C sudo's first line).
	cat >"$R/stub/sudo" <<-'EOF'
		#!/bin/bash
		[ "$1" = -V ] || exit 1
		echo "${STUB_SUDO_V:-Sudo version 1.9.15p5}"
	EOF
	chmod 755 "$R"/stub/*
	mkdir -p "$H/install/common/sudo"
	cp "$repo/install/common/sudo/hestiaweb" "$H/install/common/sudo/hestiaweb"
	rewrite "$repo/install/common/hostisan/sudoers.sh" "$H/install/common/hostisan/sudoers.sh"
	local s
	for s in restart-terminal restart-unit in-terminal in-update update-running; do
		rewrite "$repo/install/common/hostisan/$s.sh" "$H/install/common/hostisan/$s.sh"
	done
	stub systemd-run
	# hostname -f prints STUB_HOSTNAME (default box.example)
	cat >"$R/stub/hostname" <<-'EOF'
		#!/bin/bash
		echo "${STUB_HOSTNAME:-box.example}"
	EOF
	chmod 755 "$R/stub/hostname"
	# the scripts run from an ordinary login, not from the web terminal
	echo '0::/user.slice/user-0.slice/session-1.scope' >"$R/cgroup"
	export HOSTISAN_CGROUP_FILE=$R/cgroup
	# where restart-unit.sh keeps the mode a deferred restart asks for
	export HOSTISAN_RESTART_DIR=$R/run/hostisan-restart
	# no update script runs (fake_procs adds some)
	mkdir -p "$R/proc"
	export HOSTISAN_PROC_DIR=$R/proc
	for t in v-delete-sys-pma-sso v-add-sys-pma-sso; do
		printf '#!/bin/bash\necho "%s $*" >>"%s/calls"\n' "$t" "$R" >"$H/bin/$t"
		chmod 755 "$H/bin/$t"
	done
	# hidepid.sh has its own tests (units.bats); here it only logs.
	printf '#!/bin/bash\necho "hidepid.sh $*" >>"%s/calls"\n' "$R" >"$H/install/common/hostisan/hidepid.sh"
	chmod 755 "$H/install/common/hostisan/hidepid.sh"
	cp "$repo/install/common/hostisan/hostisan.env" "$H/install/common/hostisan/hostisan.env"
	cp "$repo/install/common/hostisan/needrestart.conf" "$H/install/common/hostisan/needrestart.conf"
	cp "$repo"/install/common/hostisan/{signing-key.sh,hostisan-archive-keyring.fpr,hostisan-archive-keyring.gpg} \
		"$H/install/common/hostisan/"
	rewrite "$repo/install/common/hostisan/setup.sh" "$H/install/common/hostisan/setup.sh"
	rewrite "$repo/install/common/hostisan/remove-node-terminal.sh" \
		"$H/install/common/hostisan/remove-node-terminal.sh"
	cat >"$H/conf/hestia.conf" <<-'EOF'
		ROOT_USER='admin'
		WEB_TERMINAL='true'
		PHPMYADMIN_KEY=''
	EOF
}

# stub NAME: a PATH tool that logs "NAME args" to $R/calls.
stub() {
	printf '#!/bin/bash\necho "%s $*" >>"%s/calls"\n' "$1" "$R" >"$R/stub/$1"
	chmod 755 "$R/stub/$1"
}

# stub_v NAME: a v-* script in $H/bin that logs "NAME args" to $R/calls.
stub_v() {
	printf '#!/bin/bash\necho "%s $*" >>"%s/calls"\n' "$1" "$R" >"$H/bin/$1"
	chmod 755 "$H/bin/$1"
}

# rewrite SRC DST: copy a script with its absolute paths moved under $R.
rewrite() {
	sed -e "s#/usr/local/hestia#$H#g" -e "s#/etc/#$R/etc/#g" \
		-e "s#/usr/share/keyrings#$R/usr/share/keyrings#g" \
		-e "s#/usr/share/phpmyadmin#$R/usr/share/phpmyadmin#g" "$1" >"$2"
	chmod 755 "$2"
}

# in_sandbox CMD...: run CMD with the stub tools first in PATH.
in_sandbox() {
	PATH="$R/stub:$PATH" run "$@"
}

# line_of PATTERN: the first line number of $R/calls matching PATTERN.
line_of() {
	grep -n -m1 -E "$1" "$R/calls" | cut -d: -f1
}

# old_node_terminal: what hestia-web-terminal 1.x and the installer leave.
old_node_terminal() {
	mkdir -p "$H/web-terminal/node_modules/ws"
	echo '#!/usr/bin/env node' >"$H/web-terminal/server.js"
	echo "deb [arch=amd64 signed-by=/usr/share/keyrings/nodejs.gpg] https://deb.nodesource.com/node_24.x nodistro main" \
		>"$R/etc/apt/sources.list.d/nodejs.list"
	echo key >"$R/usr/share/keyrings/nodejs.gpg"
}

# ---------------------------------------------------------------- sources

@test "version strip" {
	local sed_expr
	sed_expr=$(grep -o "sed -E '[^']*'" "$repo/src/deb/hestia/postinst" | head -n1 | sed -E "s/^sed -E '(.*)'$/\1/")
	[ -n "$sed_expr" ]
	[ "$(echo 1.10.5-101+ubuntu24.04 | sed -E "$sed_expr")" = 1.10.5 ]
	[ "$(echo 1.10.5-101+debian13 | sed -E "$sed_expr")" = 1.10.5 ]
	# Hostisan builds carry epoch 1 (dpkg -l prints it); upgrade.sh compares
	# plain x.y.z versions
	[ "$(echo 1:1.10.5-101+ubuntu24.04 | sed -E "$sed_expr")" = 1.10.5 ]
	[ "$(echo 1:1.10.5-101+debian13~rc1 | sed -E "$sed_expr")" = 1.10.5~rc1 ]
}

# distro_version SUFFIX VERSION: the Version apply_distro_version (from the
# build script, with its Hostisan defaults) gives a control file at VERSION.
distro_version() {
	local s=$repo/src/hst_autocompile.sh
	(
		eval "$(sed -n '/^apply_distro_version() {/,/^}/p' "$s")"
		eval "$(grep -E '^HOSTISAN_(EPOCH|BUILD)=' "$s")"
		eval "$(grep -E '^BUILD_PKG_REV=' "$s")"
		printf 'Package: x\nVersion: %s\n' "$2" >"$work/control"
		apply_distro_version "$work/control" "$1"
		field "$work/control" Version
	)
}

@test "every package gets epoch 1" {
	[ "$(distro_version debian13 1.10.5)" = 1:1.10.5-105+debian13 ]
	[ "$(distro_version ubuntu24.04 1.30.5)" = 1:1.30.5-105+ubuntu24.04 ]
	[ "$(BUILD_RELEASE=rc1 distro_version debian12 2.0.0)" = 1:2.0.0-105+debian12~rc1 ]
	# so upstream Hestia's newer releases never win an apt upgrade
	dpkg --compare-versions "$(distro_version debian13 1.30.5)" gt 1.30.6-1
	dpkg --compare-versions "$(distro_version debian13 8.5.11)" gt 8.5.12-1
	dpkg --compare-versions "$(distro_version debian13 1.10.5)" gt 1.99.0-1
	# all four packages go through it
	[ "$(grep -c 'apply_distro_version "$BUILD_DIR_' "$repo/src/hst_autocompile.sh")" -eq 4 ]
}

@test "a build below 105 is refused" {
	local s=$repo/src/hst_autocompile.sh msg='must be 105 or more'
	# the terminal Depends on hestia (>= 1:1.10.5-105)
	grep -Fq 'Depends: hestia (>= 1:1.10.5-105)' "$repo/src/deb/web-terminal/control"
	grep -Fq -- '--pkgrev <n>    Set the package revision number (default: 105).' "$s"
	run ! grep -Fq '(default: 1)' "$s"
	TERM=dumb run bash "$s" --hestia --noinstall --pkgrev 104 '~localsrc'
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"$msg"* ]]
	TERM=dumb HOSTISAN_BUILD=2 run bash "$s" --hestia --noinstall '~localsrc'
	[ "$status" -eq 1 ]
	[[ "$output" == *"$msg"* ]]
	TERM=dumb run bash "$s" --hestia --noinstall --pkgrev 1a '~localsrc'
	[ "$status" -eq 1 ]
	[[ "$output" == *"$msg"* ]]
	# 105 and CI's 2xx pass
	eval "$(sed -n '/^hostisan_build_ok() {/,/^}/p' "$s")"
	eval "$(grep -E '^HOSTISAN_MIN_BUILD=' "$s")"
	hostisan_build_ok 105
	hostisan_build_ok 231
	run ! hostisan_build_ok 104
	run ! hostisan_build_ok ''
}

@test "a build from a remote branch is refused" {
	local s=$repo/src/hst_autocompile.sh t
	# the remote mode fetches upstream hestiacp, which has no product.env and
	# no Hostisan code; stubs keep a regression from reaching the network
	mkdir -p "$work/stub"
	for t in wget curl apt-get npm go git dpkg sudo clear; do
		printf '#!/bin/sh\necho "%s $*" >>"%s/calls"\nexit 1\n' "$t" "$work" >"$work/stub/$t"
		chmod 755 "$work/stub/$t"
	done
	PATH="$work/stub:$PATH" BUILD_DIR="$work/build" TERM=dumb \
		run bash "$s" --hestia --noinstall --dontinstalldeps main
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"Error: Hostisan builds only from this source tree: pass '~localsrc' (not 'main')"* ]]
	# nothing but clear ran: no download, no package tool
	touch "$work/calls"
	run ! grep -v '^clear ' "$work/calls"
	[ ! -e "$work/build" ]
}

@test "control branding" {
	local c
	for c in "$repo"/src/deb/{hestia,nginx,php,web-terminal}/control; do
		echo "$c"
		# the build puts in the product values (product.env)
		[ "$(field "$c" Maintainer)" = 'Hostisan <@HOSTISAN_INFO_EMAIL@>' ]
		[ "$(field "$c" Homepage)" = '@HOSTISAN_URL@' ]
		field "$c" Description | grep -q 'Hostisan'
		run ! grep -Eiq 'hestia' <<<"$(field "$c" Maintainer; field "$c" Homepage; field "$c" Description)"
	done
	# the temporary allow entry for the control files is gone
	run ! grep -q 'src/deb' "$repo/tools/branding-allow.txt"
	run bash "$repo/tools/check-branding.sh" "$repo"
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "the build puts the product values into the control files" {
	local s=$repo/src/hst_autocompile.sh c out
	eval "$(sed -n '/^hostisan_render_control() {/,/^}/p' "$s")"
	declare -F hostisan_render_control >/dev/null
	# the values of a product.env with another domain
	sed 's/^HOSTISAN_DOMAIN=.*/HOSTISAN_DOMAIN=example.test/' "$repo/install/common/hostisan/product.env" >"$work/product.env"
	# shellcheck source=/dev/null
	source "$work/product.env"
	for c in "$repo"/src/deb/{hestia,nginx,php,web-terminal}/control; do
		out=$work/$(basename "$(dirname "$c")").control
		cp "$c" "$out"
		hostisan_render_control "$out"
		[ "$(field "$out" Maintainer)" = 'Hostisan <info@example.test>' ]
		[ "$(field "$out" Homepage)" = 'https://example.test' ]
		run ! grep -q '@HOSTISAN_' "$out"
		run ! grep -qiF "$(sed -n 's/^HOSTISAN_DOMAIN=//p' "$repo/install/common/hostisan/product.env")" "$out"
	done
	# a placeholder without a value fails the build
	printf 'Homepage: @HOSTISAN_NOPE@\n' >"$work/bad"
	run hostisan_render_control "$work/bad"
	[ "$status" -ne 0 ]
	[[ "$output" == *"HOSTISAN_NOPE"* ]]
	# each control file is rendered in the build
	[ "$(grep -c 'hostisan_render_control "\$BUILD_DIR_' "$s")" -eq 4 ]
	grep -q 'install/common/hostisan/product.env' "$s"
}

@test "the build refuses a product value sed or a control file would misread" {
	local s=$repo/src/hst_autocompile.sh v
	eval "$(sed -n '/^hostisan_render_control() {/,/^}/p' "$s")"
	# the characters product.bats allows in product.env pass through as they are
	printf 'Homepage: @HOSTISAN_URL@\n' >"$work/ok"
	HOSTISAN_URL='https://a-b_c.example.test:8443/x@y' hostisan_render_control "$work/ok"
	[ "$(cat "$work/ok")" = 'Homepage: https://a-b_c.example.test:8443/x@y' ]
	# anything else (sed's \ & |, spaces, newlines) fails, naming the key
	for v in 'https://a&b.test' 'https://a|b.test' 'https://a\b.test' 'https://a b.test' $'https://a\nb.test' '"x"'; do
		printf 'Homepage: @HOSTISAN_URL@\n' >"$work/bad"
		HOSTISAN_URL=$v run hostisan_render_control "$work/bad"
		echo "$v: $output"
		[ "$status" -ne 0 ]
		[[ "$output" == *"HOSTISAN_URL"*"product.env"* ]]
		[ "$(cat "$work/bad")" = 'Homepage: @HOSTISAN_URL@' ]
	done
}

@test "terminal control has no node" {
	local c=$repo/src/deb/web-terminal/control
	[[ "$(field "$c" Version)" == 2.* ]]
	# build 105 is the first hestia with restart-terminal.sh, which the terminal
	# postinst uses (apt then configures hestia first); a default build meets it
	[ "$(field "$c" Depends)" = 'hestia (>= 1:1.10.5-105)' ]
	dpkg --compare-versions "$(distro_version ubuntu24.04 1.10.5)" ge 1:1.10.5-105
	run ! grep -qi 'node' "$c"
	# the Go terminal ships in the hestia deb: an old Node terminal must go too
	[ "$(field "$repo/src/deb/hestia/control" Breaks)" = 'hestia-web-terminal (<< 1:2.0.0)' ]
	local f
	for f in server.js package.json package-lock.json web-terminal-session-auth.php; do
		[ ! -e "$repo/src/deb/web-terminal/$f" ]
	done
}

@test "the hestia deb depends on gnupg" {
	# setup.sh reads and renews the release keyring with gpg (signing-key.sh)
	field "$repo/src/deb/hestia/control" Depends | tr ',' '\n' | sed 's/^ *//; s/ *$//' | grep -qx gnupg
}

@test "build has no npm step for the terminal" {
	local section
	section=$(sed -n '/^# Building hestia-web-terminal/,/^# Building hestia$/p' "$repo/src/hst_autocompile.sh")
	[ -n "$section" ]
	run ! grep -Eq 'npm|server\.js|package(-lock)?\.json|session-auth' <<<"$section"
}

@test "build installs the debs in one C-ordered dpkg call" {
	# dpkg unpacks in argument order: hestia-web-terminal 2.x must come before
	# the hestia that breaks 1.x (the Docker upgrade check in the B7 report).
	grep -q 'dpkg -i $(ls $DEB_DIR/\*.deb | LC_ALL=C sort)' "$repo/src/hst_autocompile.sh"
	run ! grep -q 'for i in $DEB_DIR/\*.deb' "$repo/src/hst_autocompile.sh"
	[ "$(printf '%s\n' hestia_1.deb hestia-web-terminal_2.deb | LC_ALL=C sort | head -n1)" = hestia-web-terminal_2.deb ]
}

@test "go build is reproducible and static" {
	run grep -E '^go-build:' -A3 "$repo/Makefile"
	echo "$output"
	[[ "$output" == *"CGO_ENABLED=0"* ]]
	[[ "$output" == *"-trimpath"* ]]
	[[ "$output" == *"-buildvcs=false"* ]]
	[[ "$output" == *"-X main.version="* ]]
}

# ---------------------------------------------------------------- setup.sh

@test "setup writes hostisan.env once" {
	sandbox
	in_sandbox "$H/install/common/hostisan/setup.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$H/conf/hostisan.env" "$repo/install/common/hostisan/hostisan.env"
	[ "$(stat -c %a "$H/conf/hostisan.env")" = 640 ]

	# an existing file (the admin turned restore off) is never overwritten
	echo 'HOSTISAN_FILE_RESTORE=off' >"$H/conf/hostisan.env"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	[ "$(cat "$H/conf/hostisan.env")" = 'HOSTISAN_FILE_RESTORE=off' ]
}

@test "setup starts hostisan after hidepid" {
	sandbox
	in_sandbox "$H/install/common/hostisan/setup.sh"
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	local hide reload enable start web term
	hide=$(line_of '^hidepid\.sh')
	reload=$(line_of '^systemctl daemon-reload$')
	enable=$(line_of '^systemctl enable hostisan\.service$')
	start=$(line_of '^systemctl restart hostisan\.service$')
	web=$(line_of '^systemctl try-restart hestia\.service$')
	term=$(line_of '^systemctl try-restart hestia-web-terminal\.service$')
	[ -n "$hide" ]
	[ -n "$reload" ]
	[ -n "$enable" ]
	[ -n "$start" ]
	[ -n "$web" ]
	[ -n "$term" ]
	# hestiaweb's services start or restart only once it is in the /proc group
	[ "$hide" -lt "$start" ]
	[ "$hide" -lt "$web" ]
	[ "$hide" -lt "$term" ]
	[ "$reload" -lt "$enable" ]
	[ "$enable" -lt "$start" ]
}

@test "setup without the terminal package" {
	sandbox
	# systemctl fails for the missing unit, as the real one does (cat: 1, try-restart: 5).
	cat >"$R/stub/systemctl" <<-EOF
		#!/bin/bash
		echo "systemctl \$*" >>"$R/calls"
		case "\$*" in *hestia-web-terminal*) [ "\$1" = cat ] && exit 1; exit 5 ;; esac
		exit 0
	EOF
	in_sandbox "$H/install/common/hostisan/setup.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	run ! grep -q 'try-restart hestia-web-terminal' "$R/calls"
	grep -q '^systemctl restart hostisan\.service$' "$R/calls"
}

@test "setup skips the services without hestiaweb" {
	sandbox
	STUB_NO_HESTIAWEB=1 in_sandbox "$H/install/common/hostisan/setup.sh"
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"hestiaweb"* ]]
	run ! grep -q 'hostisan\.service' "$R/calls"
}

@test "setup removes the node terminal" {
	sandbox
	old_node_terminal
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	[ ! -e "$H/web-terminal" ]
	[ ! -e "$R/etc/apt/sources.list.d/nodejs.list" ]
	[ ! -e "$R/usr/share/keyrings/nodejs.gpg" ]
	grep -q "^pkill -f $H/web-terminal/server" "$R/calls"
}

@test "node terminal kept while its package owns it" {
	sandbox
	old_node_terminal
	STUB_OWNED=1 in_sandbox "$H/install/common/hostisan/remove-node-terminal.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	[ -f "$H/web-terminal/server.js" ]
	run ! grep -q '^pkill' "$R/calls"
	# the apt source goes anyway: the package stays, it just isn't updated from NodeSource
	[ ! -e "$R/etc/apt/sources.list.d/nodejs.list" ]
}

@test "node terminal removal keeps other node sources" {
	sandbox
	echo 'deb https://example.org/node stable main' >"$R/etc/apt/sources.list.d/nodejs.list"
	echo key >"$R/usr/share/keyrings/nodejs.gpg"
	echo 'deb [signed-by=/usr/share/keyrings/nodejs.gpg] https://mirror.example/node x main' \
		>"$R/etc/apt/sources.list.d/other.list"
	in_sandbox "$H/install/common/hostisan/remove-node-terminal.sh"
	[ "$status" -eq 0 ]
	[ -f "$R/etc/apt/sources.list.d/nodejs.list" ]
	# the keyring is still used by another source
	[ -f "$R/usr/share/keyrings/nodejs.gpg" ]
	# a second run on a clean box is a no-op
	in_sandbox "$H/install/common/hostisan/remove-node-terminal.sh"
	[ "$status" -eq 0 ]
}

# pma_sso KIND: phpMyAdmin with SSO on. KIND current: hestia-sso.php is this
# build's, keys filled in (key abc123, box.example:8083, access key "key");
# old: an older one (upstream's bcrypt version).
pma_sso() {
	sed -i "s/^PHPMYADMIN_KEY=.*/PHPMYADMIN_KEY='abc123'/" "$H/conf/hestia.conf"
	grep -q '^BACKEND_PORT=' "$H/conf/hestia.conf" || echo "BACKEND_PORT='8083'" >>"$H/conf/hestia.conf"
	mkdir -p "$H/data/access-keys"
	printf "SECRET_ACCESS_KEY='secret'\nUSER='admin'\nPERMISSIONS='phpmyadmin-sso'\nCOMMENT='phpMyAdmin'\n" \
		>"$H/data/access-keys/key"
	mkdir -p "$H/install/deb/phpmyadmin" "$R/usr/share/phpmyadmin" "$R/etc/phpmyadmin"
	cp "$repo/install/deb/phpmyadmin/hestia-sso.php" "$H/install/deb/phpmyadmin/hestia-sso.php"
	echo "include ('/etc/phpmyadmin/hestia-sso.inc.php');" >"$R/etc/phpmyadmin/config.inc.php"
	sed -e 's/%PHPMYADMIN_KEY%/abc123/' -e 's/%API_KEY%/key:secret/' \
		-e 's/%API_HOST_NAME%/box.example/' -e 's/%API_HESTIA_PORT%/8083/' \
		"$repo/install/deb/phpmyadmin/hestia-sso.php" >"$R/usr/share/phpmyadmin/hestia-sso.php"
	if [ "$1" = old ]; then
		sed -i 's/hash_hmac("sha256", /password_hash(/' "$R/usr/share/phpmyadmin/hestia-sso.php"
	fi
}

@test "setup refreshes an outdated phpmyadmin sso on upgrade" {
	sandbox
	pma_sso old
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	local del add
	del=$(line_of '^v-delete-sys-pma-sso quiet$')
	add=$(line_of '^v-add-sys-pma-sso quiet$')
	[ -n "$del" ]
	[ -n "$add" ]
	[ "$del" -lt "$add" ]

	# not without --upgrade (installer) and not when SSO is off
	: >"$R/calls"
	in_sandbox "$H/install/common/hostisan/setup.sh"
	run ! grep -q 'pma-sso' "$R/calls"
	sed -i "s/^PHPMYADMIN_KEY=.*/PHPMYADMIN_KEY=''/" "$H/conf/hestia.conf"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	run ! grep -q 'pma-sso' "$R/calls"
}

@test "setup keeps a current phpmyadmin sso and its keys" {
	sandbox
	pma_sso current
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	run ! grep -q 'pma-sso' "$R/calls"
}

# sso_refreshed: setup --upgrade deleted then re-added the SSO, with REASON in its output.
sso_refreshed() {
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"refreshing phpMyAdmin SSO"*"$1"* ]]
	local del add
	del=$(line_of '^v-delete-sys-pma-sso quiet$')
	add=$(line_of '^v-add-sys-pma-sso quiet$')
	[ -n "$del" ]
	[ -n "$add" ]
	[ "$del" -lt "$add" ]
}

@test "setup refreshes a phpmyadmin sso whose key is not hestia.conf's" {
	sandbox
	pma_sso current
	sed -i "s/^PHPMYADMIN_KEY=.*/PHPMYADMIN_KEY='other'/" "$H/conf/hestia.conf"
	sso_refreshed "PHPMYADMIN_KEY"
}

@test "setup refreshes a phpmyadmin sso for another host name" {
	sandbox
	pma_sso current
	STUB_HOSTNAME=new.example sso_refreshed "host name"
}

@test "setup refreshes a phpmyadmin sso for another panel port" {
	sandbox
	pma_sso current
	sed -i "s/^BACKEND_PORT=.*/BACKEND_PORT='2083'/" "$H/conf/hestia.conf"
	sso_refreshed "port"
}

@test "setup refreshes a phpmyadmin sso whose access key is gone" {
	sandbox
	pma_sso current
	rm "$H/data/access-keys/key"
	sso_refreshed "access key"

	# or is no longer the phpmyadmin-sso key it was
	sandbox
	pma_sso current
	sed -i "s/^PERMISSIONS=.*/PERMISSIONS=''/" "$H/data/access-keys/key"
	sso_refreshed "access key"

	sandbox
	pma_sso current
	sed -i "s/^SECRET_ACCESS_KEY=.*/SECRET_ACCESS_KEY='changed'/" "$H/data/access-keys/key"
	sso_refreshed "access key"
}

@test "setup leaves phpmyadmin sso alone when it could not add it back" {
	sandbox
	pma_sso old
	rm "$R/etc/phpmyadmin/config.inc.php"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"phpMyAdmin SSO"*"left as it is"* ]]
	run ! grep -q 'pma-sso' "$R/calls"

	pma_sso old
	rm "$H/install/deb/phpmyadmin/hestia-sso.php"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -ne 0 ]
	run ! grep -q 'pma-sso' "$R/calls"

	# the same for a current file whose access key is gone
	sandbox
	pma_sso current
	rm "$H/data/access-keys/key" "$R/etc/phpmyadmin/config.inc.php"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"phpMyAdmin SSO"*"left as it is"* ]]
	run ! grep -q 'pma-sso' "$R/calls"
}

@test "setup is idempotent" {
	sandbox
	old_node_terminal
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	cp "$H/conf/hostisan.env" "$work/env.first"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$H/conf/hostisan.env" "$work/env.first"
}

@test "setup leaves a keyring with the release key alone" {
	sandbox
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg
	cp "$repo/install/common/hostisan/hostisan-archive-keyring.gpg" "$kr"
	touch -d '2026-01-01 00:00' "$kr"
	local before
	before=$(stat -c '%Y %a' "$kr")
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$repo/install/common/hostisan/hostisan-archive-keyring.gpg" "$kr"
	[ "$(stat -c '%Y %a' "$kr")" = "$before" ]
	[[ "$output" == *"9C25AACF8D6449319AD52FE2D5DD6BC64A10C174"*"release key"* ]]
}

@test "setup says when the release key has expired" {
	sandbox
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg old
	# a key made in 2020 to last a year, pinned in the sandbox's copy
	mkdir -m 700 "$work/old"
	GNUPGHOME=$work/old gpg --batch --quiet --faked-system-time 20200101T000000 --pinentry-mode loopback \
		--passphrase '' --quick-gen-key 'Old <old@example.invalid>' ed25519 sign 1y 2>/dev/null
	old=$(GNUPGHOME=$work/old gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
	GNUPGHOME=$work/old gpg --batch --export "$old" >"$kr"
	GNUPGHOME=$work/old gpgconf --kill gpg-agent 2>/dev/null || true
	echo "$old" >"$H/install/common/hostisan/hostisan-archive-keyring.fpr"
	cp "$kr" "$work/kr.before"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$work/kr.before" "$kr"
	[[ "$output" == *"$old"*"expired"* ]]
	[[ "$output" != *"not the Hostisan release key"* ]]
	run ! grep -q 'notification' "$R/calls"
}

@test "setup keeps a test key and only says so" {
	sandbox
	GNUPGHOME= "$repo/tools/apt-repo/keygen.sh" "$work/tk" >/dev/null
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg
	cp "$work/tk/hostisan-archive-keyring.gpg" "$kr"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	# a test box keeps working: no failure, so no "setup failed" notification
	[ "$status" -eq 0 ]
	cmp "$work/tk/hostisan-archive-keyring.gpg" "$kr"
	[[ "$output" == *"$(cat "$work/tk/fingerprint.txt")"* ]]
	[[ "$output" == *"9C25AACF8D6449319AD52FE2D5DD6BC64A10C174"* ]]
	[[ "$output" == *"left as it is"* ]]
	run ! grep -q 'notification' "$R/calls"
	# a box without a Hostisan keyring (--with-debs) hears nothing about keys
	rm "$kr"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	[[ "$output" != *"APT key"* ]]
	[ ! -e "$kr" ]
}

# renew_keys: a throwaway release key, pinned in the sandbox's copy, exported
# as $work/short.gpg (one year), $work/long.gpg (its expiry moved on to two
# years: a renewal) and $work/expired.gpg / $work/expired-later.gpg /
# $work/renewed.gpg (made in 2020 for a year, its expiry then moved to 2022,
# still past, then to 2040). Prints nothing; sets $fpr and $oldfpr.
renew_keys() {
	local g=$work/rk
	mkdir -m 700 "$g" "$g.old"
	GNUPGHOME=$g gpg --batch --quiet --pinentry-mode loopback --passphrase '' \
		--quick-gen-key 'Renew <renew@example.invalid>' ed25519 sign 1y 2>/dev/null
	fpr=$(GNUPGHOME=$g gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
	GNUPGHOME=$g gpg --batch --export "$fpr" >"$work/short.gpg"
	GNUPGHOME=$g gpg --batch --quiet --pinentry-mode loopback --passphrase '' --quick-set-expire "$fpr" 2y 2>/dev/null
	GNUPGHOME=$g gpg --batch --export "$fpr" >"$work/long.gpg"
	GNUPGHOME=$g gpgconf --kill gpg-agent 2>/dev/null || true
	g=$g.old
	GNUPGHOME=$g gpg --batch --quiet --faked-system-time 20200101T000000 --pinentry-mode loopback --passphrase '' \
		--quick-gen-key 'Old <old@example.invalid>' ed25519 sign 1y 2>/dev/null
	oldfpr=$(GNUPGHOME=$g gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
	GNUPGHOME=$g gpg --batch --export "$oldfpr" >"$work/expired.gpg"
	GNUPGHOME=$g gpg --batch --quiet --faked-system-time 20200601T000000 --pinentry-mode loopback --passphrase '' \
		--quick-set-expire "$oldfpr" 2022-01-01 2>/dev/null
	GNUPGHOME=$g gpg --batch --export "$oldfpr" >"$work/expired-later.gpg"
	GNUPGHOME=$g gpg --batch --quiet --pinentry-mode loopback --passphrase '' --quick-set-expire "$oldfpr" 2040-01-01 2>/dev/null
	GNUPGHOME=$g gpg --batch --export "$oldfpr" >"$work/renewed.gpg"
	GNUPGHOME=$g gpgconf --kill gpg-agent 2>/dev/null || true
	echo "$fpr" >"$H/install/common/hostisan/hostisan-archive-keyring.fpr"
}

# revoked FILE OUT: OUT is the key in FILE (the one renew_keys made as $fpr)
# with the revocation certificate gpg made for it applied: a revoked key.
revoked() {
	local g
	g=$(mktemp -d "$work/rev.XXXXXX")
	GNUPGHOME=$g gpg --batch --quiet --import "$1" 2>/dev/null
	sed 's/^://' "$work/rk/openpgp-revocs.d/$fpr.rev" | GNUPGHOME=$g gpg --batch --quiet --import 2>/dev/null
	GNUPGHOME=$g gpg --batch --export "$fpr" >"$2"
	GNUPGHOME=$g gpgconf --kill gpg-agent 2>/dev/null || true
}

# wrap_signing_key: the sandbox's signing-key.sh becomes a wrapper that logs
# "signing-key ARGS" to $R/calls and runs the real one (kept beside it, so it
# reads the same .fpr). With STUB_EXPIRES set, `expires` prints that instead
# for the file STUB_EXPIRES_FOR names: "box" (the box's keyring) or "shipped"
# (any other file).
wrap_signing_key() {
	local d=$H/install/common/hostisan
	mv "$d/signing-key.sh" "$d/signing-key.real.sh"
	cat >"$d/signing-key.sh" <<-EOF
		#!/bin/bash
		echo "signing-key \$*" >>"$R/calls"
		if [ "\$1" = expires ] && [ -n "\${STUB_EXPIRES+x}" ]; then
			case \$2 in
				*/hestia-keyring.gpg) w=box ;;
				*) w=shipped ;;
			esac
			if [ "\$w" = "\${STUB_EXPIRES_FOR:-box}" ]; then
				printf '%s\n' "\$STUB_EXPIRES"
				exit 0
			fi
		fi
		exec "$d/signing-key.real.sh" "\$@"
	EOF
	chmod 755 "$d/signing-key.sh"
}

# nogpg: $work/nogpg holds a link to every tool on PATH but gpg.
nogpg() {
	local d f
	mkdir -p "$work/nogpg"
	local IFS=:
	for d in $PATH; do
		for f in "$d"/*; do
			[ -x "$f" ] && [ ! -d "$f" ] || continue
			[ "${f##*/}" != gpg ] || continue
			[ -e "$work/nogpg/${f##*/}" ] || ln -s "$f" "$work/nogpg/${f##*/}"
		done
	done
}

# ship FILE: FILE as this build's release keyring; server FILE: as the box's.
ship() {
	cp "$1" "$H/install/common/hostisan/hostisan-archive-keyring.gpg"
}
server() {
	install -m 0644 "$1" "$R/usr/share/keyrings/hestia-keyring.gpg"
	touch -d '2001-01-01' "$R/usr/share/keyrings/hestia-keyring.gpg"
}

# kept FILE: setup left the box's keyring as FILE, untouched, and said nothing
# about renewing it.
kept() {
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg
	cmp "$1" "$kr"
	[ "$(stat -c %Y "$kr")" = "$(date -d 2001-01-01 +%s)" ]
	[[ "$output" != *"renewed"* ]]
	[ "$(ls -A "$R/usr/share/keyrings")" = hestia-keyring.gpg ]
}

@test "setup renews the release key when this build ships a later expiry" {
	sandbox
	renew_keys
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg
	server "$work/short.gpg"
	ship "$work/long.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$work/long.gpg" "$kr"
	[ "$(stat -c %a "$kr")" = 644 ]
	[ "$(ls -A "$R/usr/share/keyrings")" = hestia-keyring.gpg ]
	# one line, with the new date
	[ "$(grep -c 'renewed' <<<"$output")" -eq 1 ]
	[[ "$output" == *"APT key $fpr renewed"*"$(date -u -d '+2 years' +%Y)"* ]]
	[ "$(grep -c "$fpr" <<<"$output")" -eq 2 ]
	# done: the next upgrade finds the same files and says nothing about it
	touch -d '2001-01-01' "$kr"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	kept "$work/long.gpg"
	# the installer's run (no --upgrade) renews too
	server "$work/short.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh"
	[ "$status" -eq 0 ]
	cmp "$work/long.gpg" "$kr"
}

@test "setup renews an expired release key" {
	sandbox
	renew_keys
	echo "$oldfpr" >"$H/install/common/hostisan/hostisan-archive-keyring.fpr"
	server "$work/expired.gpg"
	ship "$work/renewed.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$work/renewed.gpg" "$R/usr/share/keyrings/hestia-keyring.gpg"
	[[ "$output" == *"APT key $oldfpr renewed"*"2040-01-01"* ]]
	[[ "$output" != *"expired"* ]]
}

@test "setup keeps the box's keyring unless it is the same key with a later expiry" {
	sandbox
	renew_keys
	# an earlier expiry
	server "$work/long.gpg"
	ship "$work/short.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	kept "$work/long.gpg"
	# identical files
	server "$work/long.gpg"
	ship "$work/long.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	kept "$work/long.gpg"
	# a different key: a test box's throwaway key is never replaced
	GNUPGHOME= "$repo/tools/apt-repo/keygen.sh" "$work/tk" >/dev/null
	server "$work/tk/hostisan-archive-keyring.gpg"
	ship "$work/long.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	kept "$work/tk/hostisan-archive-keyring.gpg"
	[[ "$output" == *"not the Hostisan release key"*"left as it is"* ]]
	# the release key with another key beside it
	cat "$work/short.gpg" "$work/tk/hostisan-archive-keyring.gpg" >"$work/two.gpg"
	server "$work/two.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	kept "$work/two.gpg"
	# a shipped keyring that is not (only) the pinned key is never installed
	server "$work/short.gpg"
	cat "$work/long.gpg" "$work/tk/hostisan-archive-keyring.gpg" >"$work/ship-two.gpg"
	ship "$work/ship-two.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	kept "$work/short.gpg"
	ship "$work/tk/hostisan-archive-keyring.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	kept "$work/short.gpg"
	# a box's keyring that is a link is left to the admin
	ship "$work/long.gpg"
	cp "$work/short.gpg" "$work/linked.gpg"
	rm "$R/usr/share/keyrings/hestia-keyring.gpg"
	ln -s "$work/linked.gpg" "$R/usr/share/keyrings/hestia-keyring.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	[ -L "$R/usr/share/keyrings/hestia-keyring.gpg" ]
	cmp "$work/short.gpg" "$work/linked.gpg"
	[[ "$output" != *"renewed"* ]]
}

@test "setup checks the shipped keyring it installs, through one temp file" {
	sandbox
	renew_keys
	wrap_signing_key
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg
	server "$work/short.gpg"
	ship "$work/long.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	cmp "$work/long.gpg" "$kr"
	# the shipped keyring is read once, into the temp file; that copy is the
	# one checked and the one renamed into place
	grep -qE '^signing-key check .*/\.hestia-keyring\.[^/]+$' "$R/calls"
	grep -qE '^signing-key expires .*/\.hestia-keyring\.[^/]+$' "$R/calls"
	run ! grep -qE '^signing-key (check|expires) .*hostisan-archive-keyring\.gpg$' "$R/calls"
	[ "$(ls -A "$R/usr/share/keyrings")" = hestia-keyring.gpg ]
}

@test "an interrupted renewal leaves no temp keyring behind" {
	sandbox
	renew_keys
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg
	server "$work/short.gpg"
	ship "$work/long.gpg"
	# setup.sh is killed once the temp file is checked, before it is renamed
	cat >"$R/stub/chmod" <<-'EOF'
		#!/bin/bash
		case "${!#}" in
			*/.hestia-keyring.*)
				kill -TERM "$PPID"
				sleep 2
				;;
		esac
		exec /bin/chmod "$@"
	EOF
	/bin/chmod 755 "$R/stub/chmod"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -ne 0 ]
	cmp "$work/short.gpg" "$kr"
	[ "$(ls -A "$R/usr/share/keyrings")" = hestia-keyring.gpg ]
}

@test "setup skips the renewal when an expiry is not a number" {
	sandbox
	renew_keys
	wrap_signing_key
	local kr=$R/usr/share/keyrings/hestia-keyring.gpg v w
	for w in box shipped; do
		for v in soon '' 1.5 -1 '12 34' 0x10 '$(id)'; do
			server "$work/short.gpg"
			ship "$work/long.gpg"
			STUB_EXPIRES=$v STUB_EXPIRES_FOR=$w in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
			echo "$w [$v]: $output"
			kept "$work/short.gpg"
			# one warning, and no shell error
			[ "$(grep -c '^\[ ! \]' <<<"$output")" -eq 1 ]
			[[ "$output" == *"[ ! ] Hostisan: "*"expiry"* ]]
			run ! grep -qiE 'integer|syntax|unbound|command not found|line [0-9]+:' <<<"$output"
		done
	done
	# "never" is an expiry: a key that no longer expires is a renewal
	server "$work/short.gpg"
	ship "$work/long.gpg"
	STUB_EXPIRES=never STUB_EXPIRES_FOR=shipped in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$work/long.gpg" "$kr"
	[[ "$output" == *"renewed"*"does not expire"* ]]
}

@test "setup never renews a revoked release key on the box" {
	sandbox
	renew_keys
	revoked "$work/short.gpg" "$work/short-revoked.gpg"
	server "$work/short-revoked.gpg"
	ship "$work/long.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	kept "$work/short-revoked.gpg"
	[[ "$output" == *"$fpr"*"revoked"*"left as it is"* ]]
}

@test "setup never installs an expired shipped keyring" {
	sandbox
	renew_keys
	echo "$oldfpr" >"$H/install/common/hostisan/hostisan-archive-keyring.fpr"
	# the same key, with a later expiry that has passed too
	run ! cmp -s "$work/expired.gpg" "$work/expired-later.gpg"
	server "$work/expired.gpg"
	ship "$work/expired-later.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	kept "$work/expired.gpg"
	[[ "$output" == *"$oldfpr"*"expired"* ]]
}

@test "setup never installs a revoked shipped keyring" {
	sandbox
	renew_keys
	# the same key with a later expiry, but revoked
	revoked "$work/long.gpg" "$work/long-revoked.gpg"
	run ! cmp -s "$work/long.gpg" "$work/long-revoked.gpg"
	server "$work/short.gpg"
	ship "$work/long-revoked.gpg"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	kept "$work/short.gpg"
	[[ "$output" == *"APT key $fpr (the Hostisan release key)"* ]]
}

@test "setup warns when gpg is missing and leaves the keyring alone" {
	sandbox
	renew_keys
	nogpg
	server "$work/short.gpg"
	ship "$work/long.gpg"
	PATH="$R/stub:$work/nogpg" run "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	# a failed step: the release key is neither checked nor renewed
	[ "$status" -eq 1 ]
	kept "$work/short.gpg"
	[ "$(grep -c '^\[ ! \]' <<<"$output")" -eq 1 ]
	[[ "$output" == *"[ ! ] Hostisan: "*"gpg"*"$R/usr/share/keyrings/hestia-keyring.gpg"* ]]
	# a box without a Hostisan keyring (--with-debs) needs no gpg here
	rm "$R/usr/share/keyrings/hestia-keyring.gpg"
	PATH="$R/stub:$work/nogpg" run "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" != *"gpg"* ]]
}

@test "setup installs the hestiaweb sudoers file on upgrade" {
	sandbox
	# an upgraded box keeps upstream's file, without !log_allowed
	mkdir -p "$R/etc/sudoers.d"
	printf 'Defaults:root !requiretty\nhestiaweb   ALL=NOPASSWD:/usr/local/hestia/bin/*\n' >"$R/etc/sudoers.d/hestiaweb"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	local dst=$R/etc/sudoers.d/hestiaweb checked
	cmp "$dst" "$repo/install/common/sudo/hestiaweb"
	grep -qx 'Defaults:hestiaweb !log_allowed' "$dst"
	[ "$(stat -c %a "$dst")" = 440 ]
	# visudo checked a temp file sudo ignores (a dot in its name), never the live one
	checked=$(sed -n 's/^visudo -c -q -f //p' "$R/calls")
	[ -n "$checked" ]
	[ "$checked" != "$dst" ]
	[[ "$(basename "$checked")" == .* ]]
	cmp "$R/visudo.seen" "$dst"
	[ "$(ls -A "$R/etc/sudoers.d")" = hestiaweb ]
	# before hostisan (which sudoes as hestiaweb) restarts
	[ "$(line_of '^visudo ')" -lt "$(line_of '^systemctl restart hostisan\.service$')" ]
}

# nr_restarts NAME...: for each unit NAME, "NAME=V", where V is what
# needrestart's override_rc gives it once it read the drop-in at
# $R/etc/needrestart/conf.d/hostisan.conf the way needrestart.conf does (a
# string eval under strict), or "NAME=default" when no regex matches.
nr_restarts() {
	perl -e '
		use strict;
		use warnings;
		my %nrconf = (override_rc => { qr(^dbus) => 0 });
		my $fn = shift @ARGV;
		eval do { local(@ARGV, $/) = $fn; <> };
		die "Error parsing $fn: $@" if $@;
		for my $rc (@ARGV) {
			my $v = "default";
			for my $re (keys %{$nrconf{override_rc}}) {
				next unless $rc =~ /$re/;
				$v = $nrconf{override_rc}->{$re};
				last;
			}
			print "$rc=$v\n";
		}' "$R/etc/needrestart/conf.d/hostisan.conf" "$@"
}

@test "setup keeps needrestart off the panel, hestia and the terminal" {
	command -v perl >/dev/null || skip "no perl"
	sandbox
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	local dst=$R/etc/needrestart/conf.d/hostisan.conf
	cmp "$dst" "$repo/install/common/hostisan/needrestart.conf"
	[ "$(stat -c %a "$dst")" = 644 ]
	# needrestart (apt's DPkg::Post-Invoke on Ubuntu) restarted hostisan.service
	# in line while its deferred restart waited, and hung up the terminal (rc6 P1)
	perl -c "$dst"
	run nr_restarts hostisan.service hestia.service hestia-web-terminal.service \
		nginx.service hostisan-update-1.service hostisan-panel-restart.service \
		myhestia.service hestia.service.d hostisan.socket dbus.service
	echo "$output"
	[ "$status" -eq 0 ]
	[ "$output" = "$(printf '%s\n' hostisan.service=0 hestia.service=0 hestia-web-terminal.service=0 \
		nginx.service=default hostisan-update-1.service=default hostisan-panel-restart.service=default \
		myhestia.service=default hestia.service.d=default hostisan.socket=default dbus.service=0)" ]
	# an edited file is put back; an unchanged one is left alone
	echo '# edited' >"$dst"
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	cmp "$dst" "$repo/install/common/hostisan/needrestart.conf"
	touch -d '2001-01-01' "$dst"
	in_sandbox "$H/install/common/hostisan/setup.sh"
	[ "$status" -eq 0 ]
	[ "$(stat -c %Y "$dst")" = "$(date -d 2001-01-01 +%s)" ]
}

@test "sudoers drops the settings sudo-rs rejects" {
	sandbox
	STUB_SUDO_V='sudo-rs 0.2.8-1ubuntu5' in_sandbox "$H/install/common/hostisan/sudoers.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	local dst=$R/etc/sudoers.d/hestiaweb
	run ! grep -nE 'requiretty|log_allowed' "$dst"
	grep -qx 'hestiaweb   ALL=NOPASSWD:/usr/local/hestia/bin/\*' "$dst"
	[ "$(stat -c %a "$dst")" = 440 ]
	# the C sudo keeps both
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	[ "$status" -eq 0 ]
	cmp "$dst" "$repo/install/common/sudo/hestiaweb"
}

@test "sudoers leaves an unchanged file alone and says nothing" {
	sandbox
	local dst=$R/etc/sudoers.d/hestiaweb
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	[ "$status" -eq 0 ]
	[[ "$output" == *"installed $dst"* ]]
	touch -d '2001-01-01' "$dst"
	: >"$R/calls"
	# every upgrade logged "installed" though nothing changed
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
	[ "$(stat -c %Y "$dst")" = "$(date -d 2001-01-01 +%s)" ]
	run ! grep -q '^visudo ' "$R/calls"
	[ "$(ls -A "$R/etc/sudoers.d")" = hestiaweb ]
	# the same through setup.sh
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	[[ "$output" != *"installed $dst"* ]]
	# other content, or other permissions, are installed again and logged
	chmod u+w "$dst"
	echo '# local edit' >>"$dst"
	chmod 0440 "$dst"
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	[ "$status" -eq 0 ]
	[[ "$output" == *"installed $dst"* ]]
	cmp "$dst" "$repo/install/common/sudo/hestiaweb"
	chmod 0640 "$dst"
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	[ "$status" -eq 0 ]
	[[ "$output" == *"installed $dst"* ]]
	[ "$(stat -c %a "$dst")" = 440 ]
	# under sudo-rs the file it compares with is the edited one
	STUB_SUDO_V='sudo-rs 0.2.8-1ubuntu5' in_sandbox "$H/install/common/hostisan/sudoers.sh"
	[[ "$output" == *"installed $dst"* ]]
	STUB_SUDO_V='sudo-rs 0.2.8-1ubuntu5' in_sandbox "$H/install/common/hostisan/sudoers.sh"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "sudoers installs again over a link or a file of another owner" {
	sandbox
	local dst=$R/etc/sudoers.d/hestiaweb
	mkdir -p "$R/etc/sudoers.d"
	# a link to a file with the very same content and mode
	cp "$repo/install/common/sudo/hestiaweb" "$work/linked"
	chmod 0440 "$work/linked"
	ln -s "$work/linked" "$dst"
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"installed $dst"* ]]
	[ ! -L "$dst" ]
	cmp "$dst" "$repo/install/common/sudo/hestiaweb"
	grep -q '^visudo ' "$R/calls"
	cmp "$work/linked" "$repo/install/common/sudo/hestiaweb"
	# the same content and mode, another owner
	: >"$R/calls"
	if [ "$(id -u)" = 0 ]; then
		chown 12345:12345 "$dst"
	else
		# not root: stat says so (the uid and gid of $dst are 12345)
		cat >"$R/stub/stat" <<-EOF
			#!/bin/bash
			if [ "\${!#}" = "$dst" ] && [ "\$2" = '%a %u %g' ]; then
				echo "\$(/usr/bin/stat -c %a "$dst") 12345 12345"
				exit 0
			fi
			exec /usr/bin/stat "\$@"
		EOF
		chmod 755 "$R/stub/stat"
	fi
	in_sandbox "$H/install/common/hostisan/sudoers.sh"
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"installed $dst"* ]]
	grep -q '^visudo ' "$R/calls"
	rm -f "$R/stub/stat"
	[ "$(stat -c '%u %a' "$dst")" = "$(id -u) 440" ]
	[ "$(ls -A "$R/etc/sudoers.d")" = hestiaweb ]
}

@test "a sudoers file visudo refuses is never installed" {
	sandbox
	mkdir -p "$R/etc/sudoers.d"
	echo 'old rules' >"$R/etc/sudoers.d/hestiaweb"
	STUB_VISUDO_RC=1 in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"visudo"* ]]
	[ "$(cat "$R/etc/sudoers.d/hestiaweb")" = 'old rules' ]
	[ "$(ls -A "$R/etc/sudoers.d")" = hestiaweb ]
	# the other steps still run
	grep -q '^systemctl restart hostisan\.service$' "$R/calls"
}

@test "setup refuses non-root" {
	sandbox
	printf '#!/bin/bash\necho 1000\n' >"$R/stub/id"
	in_sandbox "$H/install/common/hostisan/setup.sh"
	[ "$status" -ne 0 ]
	[[ "$output" == *"root"* ]]
	[ ! -e "$H/conf/hostisan.env" ]
}

# ---------------------------------------------------------------- maintainer scripts

@test "hestia postinst runs setup before restarting services" {
	local p=$repo/src/deb/hestia/postinst setup restart pma
	setup=$(grep -n 'install/common/hostisan/setup\.sh --upgrade' "$p" | cut -d: -f1)
	restart=$(grep -n '^upgrade_restart_services' "$p" | cut -d: -f1)
	pma=$(grep -n '^upgrade_phpmyadmin' "$p" | cut -d: -f1)
	[ -n "$setup" ]
	[ -n "$restart" ]
	[ -n "$pma" ]
	[ "$pma" -lt "$setup" ]
	[ "$setup" -lt "$restart" ]
	bash -n "$p"
}

# postinst_setup_lines: the hestia postinst's setup.sh run and the check of
# its exit status, as a script ($work/setup-part.sh) to run with stubs.
postinst_setup_lines() {
	local p=$repo/src/deb/hestia/postinst
	{
		sed -n '/install\/common\/hostisan\/setup\.sh --upgrade/,/hostisan_setup_rc=/p' "$p"
		sed -n '/^if \[ "\$hostisan_setup_rc" -ne 0 \]; then$/,/^fi$/p' "$p"
	} >"$work/setup-part.sh"
}

@test "hestia postinst tells the admin in the panel when setup fails" {
	sandbox
	postinst_setup_lines
	grep -q 'PIPESTATUS' "$work/setup-part.sh"
	grep -q 'v-add-user-notification' "$work/setup-part.sh"
	stub_v v-add-user-notification
	printf '#!/bin/bash\necho "[ ! ] Hostisan: hidepid.sh failed"\nexit 1\n' >"$H/install/common/hostisan/setup.sh"
	HESTIA=$H BIN=$H/bin LOG=$work/upgrade.log ROOT_USER=admin new_version=1.10.5 \
		in_sandbox bash -c "source '$work/setup-part.sh'; echo configure-continues"
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *configure-continues* ]]
	grep -q 'hidepid.sh failed' "$work/upgrade.log"
	grep -E "^v-add-user-notification admin .*setup.*$work/upgrade.log" "$R/calls"

	# a setup that succeeds adds no notification
	: >"$R/calls"
	printf '#!/bin/bash\nexit 0\n' >"$H/install/common/hostisan/setup.sh"
	HESTIA=$H BIN=$H/bin LOG=$work/upgrade.log ROOT_USER=admin new_version=1.10.5 \
		in_sandbox bash -c "source '$work/setup-part.sh'"
	[ "$status" -eq 0 ]
	run ! grep -q 'v-add-user-notification' "$R/calls"
}

@test "terminal preinst stops the node terminal on upgrade only" {
	sandbox
	rewrite "$repo/src/deb/web-terminal/preinst" "$work/preinst"
	in_sandbox "$work/preinst" upgrade 1.0.6-1+ubuntu24.04
	echo "$output"
	[ "$status" -eq 0 ]
	grep -qx 'systemctl stop hestia-web-terminal.service' "$R/calls"

	: >"$R/calls"
	in_sandbox "$work/preinst" upgrade 2.0.0-101+ubuntu24.04
	[ "$status" -eq 0 ]
	in_sandbox "$work/preinst" install
	[ "$status" -eq 0 ]
	[ ! -s "$R/calls" ]
}

@test "terminal postinst starts the go terminal when it is on" {
	sandbox
	old_node_terminal
	rewrite "$repo/src/deb/web-terminal/postinst" "$work/postinst"
	in_sandbox "$work/postinst" configure 1.0.6-1+ubuntu24.04
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	[ ! -e "$H/web-terminal" ]
	local reload reenable restart
	reload=$(line_of '^systemctl daemon-reload$')
	reenable=$(line_of '^systemctl reenable hestia-web-terminal\.service$')
	restart=$(line_of '^systemctl restart hestia-web-terminal\.service$')
	[ -n "$reload" ]
	[ -n "$reenable" ]
	[ -n "$restart" ]
	[ "$reload" -lt "$reenable" ]
	[ "$reenable" -lt "$restart" ]

	# terminal off: nothing is started
	: >"$R/calls"
	sed -i "s/^WEB_TERMINAL=.*/WEB_TERMINAL='false'/" "$H/conf/hestia.conf"
	in_sandbox "$work/postinst" configure 2.0.0-101+ubuntu24.04
	[ "$status" -eq 0 ]
	run ! grep -Eq 'systemctl (re)?(start|restart|enable)' "$R/calls"

	# fresh install (no hestia.conf yet): the installer enables it later
	: >"$R/calls"
	rm "$H/conf/hestia.conf"
	in_sandbox "$work/postinst" configure
	[ "$status" -eq 0 ]
	run ! grep -Eq 'systemctl (re)?(start|restart|enable)' "$R/calls"
}

# in_terminal: the scripts run from a shell of the web terminal (cgroup v2).
in_terminal() {
	echo '0::/system.slice/hestia-web-terminal.service' >"$R/cgroup"
}

@test "terminal postinst defers the restart when run from the terminal" {
	sandbox
	in_terminal
	rewrite "$repo/src/deb/web-terminal/postinst" "$work/postinst"
	in_sandbox "$work/postinst" configure 2.0.0-104+ubuntu24.04
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	[[ "$output" == *"restarts once dpkg is done; this terminal closes then"* ]]
	# restarting the unit in line would kill this shell, apt, dpkg and the postinst
	run ! grep -E '^systemctl (try-)?restart hestia-web-terminal' "$R/calls"
	# the transient unit waits for dpkg (restart-terminal.sh --deferred)
	grep -qx "systemd-run --on-active=30 --unit=hostisan-terminal-restart --collect --timer-property=RemainAfterElapse=no /bin/bash $H/install/common/hostisan/restart-unit.sh hestia-web-terminal.service --deferred restart" "$R/calls"
	# a failed unit left by an earlier upgrade must not block the name
	grep -qx 'systemctl reset-failed hostisan-terminal-restart.service hostisan-terminal-restart.timer' "$R/calls"
	[ "$(line_of '^systemctl reset-failed ')" -lt "$(line_of '^systemd-run ')" ]
}

# dpkg_locks HELD: fake dpkg lock files under $R/dpkg, and a $R/locks in the
# /proc/locks format that lists them as locked when HELD=1.
dpkg_locks() {
	mkdir -p "$R/dpkg"
	: >"$R/dpkg/lock-frontend"
	: >"$R/dpkg/lock"
	: >"$R/locks"
	if [ "$1" = 1 ]; then
		echo "1: POSIX  ADVISORY  WRITE 4321 08:01:$(stat -c %i "$R/dpkg/lock-frontend") 0 EOF" >>"$R/locks"
		echo "2: POSIX  ADVISORY  WRITE 4322 08:01:$(stat -c %i "$R/dpkg/lock") 0 EOF" >>"$R/locks"
	fi
	echo "3: FLOCK  ADVISORY  WRITE 99 08:01:1 0 EOF" >>"$R/locks"
	export HOSTISAN_DPKG_DIR=$R/dpkg HOSTISAN_PROC_LOCKS=$R/locks
}

@test "the deferred terminal restart waits until dpkg is idle" {
	sandbox
	dpkg_locks 1
	# sleep logs; on its third call the upgrade ends and dpkg drops its locks
	cat >"$R/stub/sleep" <<-EOF
		#!/bin/bash
		echo "sleep \$*" >>"$R/calls"
		[ "\$(grep -c '^sleep ' "$R/calls")" -ge 3 ] && : >"$R/locks"
		exit 0
	EOF
	chmod 755 "$R/stub/sleep"
	in_sandbox "$H/install/common/hostisan/restart-terminal.sh" --deferred try-restart
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	[ "$(grep -c '^sleep 5$' "$R/calls")" -eq 3 ]
	grep -qx 'systemctl try-restart hestia-web-terminal.service' "$R/calls"
	[ "$(line_of '^systemctl try-restart ')" -gt "$(grep -n '^sleep ' "$R/calls" | tail -n1 | cut -d: -f1)" ]
	# dpkg idle from the start: no wait
	: >"$R/calls"
	dpkg_locks 0
	in_sandbox "$H/install/common/hostisan/restart-terminal.sh" --deferred restart
	[ "$status" -eq 0 ]
	run ! grep -q '^sleep ' "$R/calls"
	grep -qx 'systemctl restart hestia-web-terminal.service' "$R/calls"
}

@test "the deferred terminal restart stops waiting after an hour" {
	sandbox
	dpkg_locks 1
	stub sleep
	in_sandbox "$H/install/common/hostisan/restart-terminal.sh" --deferred restart
	echo "$output"
	[ "$status" -eq 0 ]
	# 3600 s in 5 s steps, then the restart anyway
	[ "$(grep -c '^sleep 5$' "$R/calls")" -eq 720 ]
	grep -qx 'systemctl restart hestia-web-terminal.service' "$R/calls"
	# only restart and try-restart
	in_sandbox "$H/install/common/hostisan/restart-terminal.sh" --deferred stop
	[ "$status" -eq 2 ]
}

@test "setup defers the panel, hestia and terminal restarts when run from the terminal" {
	sandbox
	in_terminal
	in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	[[ "$output" == *"hostisan.service restarts once dpkg is done; this terminal closes then"* ]]
	# every terminal session goes through hestia-nginx (hestia.service) and the
	# panel (hostisan.service): restarting either in line hung up this shell
	# and the update it ran (rc5 P1)
	run ! grep -E '^systemctl (try-)?restart (hostisan|hestia|hestia-web-terminal)\.service' "$R/calls"
	local d="--collect --timer-property=RemainAfterElapse=no /bin/bash $H/install/common/hostisan/restart-unit.sh"
	grep -qx "systemd-run --on-active=30 --unit=hostisan-panel-restart $d hostisan.service --deferred restart" "$R/calls"
	grep -qx "systemd-run --on-active=30 --unit=hostisan-hestia-restart $d hestia.service --deferred try-restart" "$R/calls"
	grep -qx "systemd-run --on-active=30 --unit=hostisan-terminal-restart $d hestia-web-terminal.service --deferred try-restart" "$R/calls"
	grep -qx 'systemctl reset-failed hostisan-panel-restart.service hostisan-panel-restart.timer' "$R/calls"
	# the steps after it still run, and hostisan is still enabled
	grep -q '^systemctl enable hostisan\.service$' "$R/calls"
}

@test "the deferred panel restart waits until dpkg is idle" {
	sandbox
	dpkg_locks 1
	cat >"$R/stub/sleep" <<-EOF
		#!/bin/bash
		echo "sleep \$*" >>"$R/calls"
		[ "\$(grep -c '^sleep ' "$R/calls")" -ge 2 ] && : >"$R/locks"
		exit 0
	EOF
	chmod 755 "$R/stub/sleep"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service --deferred restart
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	[ "$(grep -c '^sleep 5$' "$R/calls")" -eq 2 ]
	grep -qx 'systemctl restart hostisan.service' "$R/calls"
	[ "$(line_of '^systemctl restart ')" -gt "$(grep -n '^sleep ' "$R/calls" | tail -n1 | cut -d: -f1)" ]
}

# fake_procs CMDLINE...: the fake /proc ($R/proc) holds one process per
# CMDLINE, its arguments split on spaces, from pid 100 up.
fake_procs() {
	local i=100 c
	rm -rf "$R/proc"
	mkdir -p "$R/proc"
	for c in "$@"; do
		mkdir -p "$R/proc/$i"
		# shellcheck disable=SC2086 # split on purpose
		printf '%s\0' $c >"$R/proc/$i/cmdline"
		i=$((i + 1))
	done
}

@test "the deferred restarts wait while an update loop runs" {
	sandbox
	dpkg_locks 0
	# an older v-update-sys-hestia-all loop runs in the terminal: dpkg is idle
	# between its packages, and a restart then would hang the loop up
	fake_procs '/usr/sbin/cron -f' "/bin/bash $H/bin/v-update-sys-hestia-all" \
		"/bin/bash $H/bin/v-update-sys-hestia hestia-nginx"
	cat >"$R/stub/sleep" <<-EOF
		#!/bin/bash
		echo "sleep \$*" >>"$R/calls"
		n=\$(grep -c '^sleep ' "$R/calls")
		[ "\$n" -ge 2 ] && rm -rf "$R/proc/102"
		[ "\$n" -ge 3 ] && rm -rf "$R/proc/101"
		exit 0
	EOF
	chmod 755 "$R/stub/sleep"
	local u
	for u in hostisan.service hestia.service hestia-web-terminal.service; do
		: >"$R/calls"
		fake_procs '/usr/sbin/cron -f' "/bin/bash $H/bin/v-update-sys-hestia-all" \
			"/bin/bash $H/bin/v-update-sys-hestia hestia-nginx"
		in_sandbox "$H/install/common/hostisan/restart-unit.sh" "$u" --deferred restart
		echo "$output"
		cat "$R/calls"
		[ "$status" -eq 0 ]
		# waits for the package, then for the loop
		[ "$(grep -c '^sleep 5$' "$R/calls")" -eq 3 ]
		grep -qx "systemctl restart $u" "$R/calls"
		[ "$(line_of '^systemctl restart ')" -gt "$(grep -n '^sleep ' "$R/calls" | tail -n1 | cut -d: -f1)" ]
	done
	# run through sudo (the nightly cron) it waits too, at most an hour
	stub sleep
	: >"$R/calls"
	fake_procs "sudo /usr/local/hestia/bin/v-update-sys-hestia-all" \
		"/bin/bash /usr/local/hestia/bin/v-update-sys-hestia-all"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service --deferred restart
	[ "$status" -eq 0 ]
	[ "$(grep -c '^sleep 5$' "$R/calls")" -eq 720 ]
	grep -qx 'systemctl restart hostisan.service' "$R/calls"
}

@test "the deferred restarts ignore processes that only name an update" {
	sandbox
	dpkg_locks 0
	stub sleep
	# a grep, an editor, the Git updater, the log follower of a detached update
	# (run-detached.sh: the update itself runs in its own unit) and the
	# deferred restart itself
	# and sudo alone (its child is the update, when there is one)
	fake_procs "grep -r v-update-sys-hestia-all /etc" "vim $H/bin/v-update-sys-hestia" \
		"sudo $H/bin/v-update-sys-hestia-all" "env $H/bin/v-update-sys-hestia" \
		"/bin/bash $H/bin/v-update-sys-hestia-git" \
		"bash $H/install/common/hostisan/run-detached.sh $H/bin/v-update-sys-hestia-all" \
		"tail -f /var/log/hestia/hostisan-update-1.log" \
		"/bin/bash $H/install/common/hostisan/restart-unit.sh hestia.service --deferred restart"
	mkdir -p "$R/proc/2" "$R/proc/self"
	: >"$R/proc/2/cmdline"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hestia.service --deferred restart
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	run ! grep -q '^sleep ' "$R/calls"
	grep -qx 'systemctl restart hestia.service' "$R/calls"
}

@test "update-running sees an update script in the real process table" {
	sandbox
	unset HOSTISAN_PROC_DIR
	mkdir -p "$work/bin"
	printf '#!/bin/bash\nsleep 30\n:\n' >"$work/bin/v-update-sys-hestia-all"
	chmod 755 "$work/bin/v-update-sys-hestia-all"
	"$work/bin/v-update-sys-hestia-all" &
	local pid=$!
	run "$H/install/common/hostisan/update-running.sh"
	local any=$status
	run "$H/install/common/hostisan/update-running.sh" "$pid"
	local this=$status
	run "$H/install/common/hostisan/update-running.sh" "$$"
	local other=$status
	kill "$pid"
	wait "$pid" 2>/dev/null || true
	[ "$any" -eq 0 ]
	[ "$this" -eq 0 ]
	# bats is not an update
	[ "$other" -eq 1 ]
	# with a pid: only that process; sudo typed in the terminal is the parent
	# of the new v-update-sys-hestia-all, which must still detach
	fake_procs "sudo $H/bin/v-update-sys-hestia-all" "/bin/bash $H/bin/v-update-sys-hestia-all" \
		"$H/bin/v-update-sys-hestia hestia" "-bash"
	HOSTISAN_PROC_DIR=$R/proc run "$H/install/common/hostisan/update-running.sh" 100
	[ "$status" -eq 1 ]
	HOSTISAN_PROC_DIR=$R/proc run "$H/install/common/hostisan/update-running.sh" 101
	[ "$status" -eq 0 ]
	HOSTISAN_PROC_DIR=$R/proc run "$H/install/common/hostisan/update-running.sh" 102
	[ "$status" -eq 0 ]
	HOSTISAN_PROC_DIR=$R/proc run "$H/install/common/hostisan/update-running.sh" 103
	[ "$status" -eq 1 ]
	# a pid that is gone, or not a pid
	run "$H/install/common/hostisan/update-running.sh" "$pid"
	[ "$status" -eq 1 ]
	run "$H/install/common/hostisan/update-running.sh" '../1'
	[ "$status" -eq 2 ]
}

@test "restart-unit restarts only the panel, hestia and the terminal" {
	sandbox
	local u
	for u in nginx.service hostisan hestia-web-terminal ''; do
		: >"$R/calls"
		in_sandbox "$H/install/common/hostisan/restart-unit.sh" "$u" restart
		[ "$status" -eq 2 ]
		[ ! -s "$R/calls" ]
	done
	for u in hostisan.service hestia.service hestia-web-terminal.service; do
		: >"$R/calls"
		in_sandbox "$H/install/common/hostisan/restart-unit.sh" "$u" try-restart
		[ "$status" -eq 0 ]
		grep -qx "systemctl try-restart $u" "$R/calls"
	done
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service --deferred stop
	[ "$status" -eq 2 ]
}

@test "terminal cgroup check matches only the terminal unit" {
	sandbox
	local cg
	for cg in '0::/system.slice/hestia-web-terminal.service' \
		'1:name=systemd:/system.slice/hestia-web-terminal.service' \
		'0::/system.slice/hestia-web-terminal.service/sub'; do
		echo "$cg" >"$R/cgroup"
		: >"$R/calls"
		in_sandbox "$H/install/common/hostisan/restart-terminal.sh" restart
		[ "$status" -eq 0 ]
		grep -q '^systemd-run ' "$R/calls"
		in_sandbox "$H/install/common/hostisan/in-terminal.sh"
		[ "$status" -eq 0 ]
	done
	for cg in '0::/system.slice/hostisan.service' '0::/system.slice/hestia-web-terminal.service.d' \
		'0::/user.slice/user-0.slice/session-1.scope'; do
		echo "$cg" >"$R/cgroup"
		: >"$R/calls"
		in_sandbox "$H/install/common/hostisan/restart-terminal.sh" restart
		[ "$status" -eq 0 ]
		grep -qx 'systemctl restart hestia-web-terminal.service' "$R/calls"
		run ! grep -q '^systemd-run ' "$R/calls"
		in_sandbox "$H/install/common/hostisan/in-terminal.sh"
		[ "$status" -eq 1 ]
	done
	# no cgroup file: not in the terminal
	HOSTISAN_CGROUP_FILE=$R/missing in_sandbox "$H/install/common/hostisan/in-terminal.sh"
	[ "$status" -eq 1 ]
	# only restart and try-restart
	in_sandbox "$H/install/common/hostisan/restart-terminal.sh" stop
	[ "$status" -eq 2 ]
}

# proc_entry PID PPID CGROUP: a process in the fake /proc ($R/proc) with that
# parent (the PPid line of its status file) and cgroup.
proc_entry() {
	mkdir -p "$R/proc/$1"
	printf 'Name:\tbash\nUmask:\t0022\nState:\tS (sleeping)\nTgid:\t%s\nNgid:\t0\nPid:\t%s\nPPid:\t%s\n' \
		"$1" "$1" "$2" >"$R/proc/$1/status"
	echo "$3" >"$R/proc/$1/cgroup"
}

# terminal_chain: the fake /proc of a web-terminal shell (pid 100, a child of
# the terminal's hostisan process, pid 50) where root ran `sudo -i`: pam_systemd
# moved sudo (200) and root's shell (300) into a session scope (rc6 P2).
terminal_chain() {
	local term='0::/system.slice/hestia-web-terminal.service'
	local scope='0::/user.slice/user-0.slice/session-c1.scope'
	rm -rf "$R/proc"
	proc_entry 1 0 '0::/init.scope'
	proc_entry 50 1 "$term"
	proc_entry 100 50 "$term"
	proc_entry 200 100 "$scope"
	proc_entry 300 200 "$scope"
}

# in_terminal_from PPID: run in-terminal.sh as the child of a process whose own
# fake /proc entry has parent PPID and a session-scope cgroup, as a postinst
# run by apt is; HOSTISAN_CGROUP_FILE puts in-terminal.sh itself there too.
in_terminal_from() {
	cat >"$R/from" <<-EOF
		#!/bin/bash
		mkdir -p "$R/proc/\$\$"
		printf 'Name:\tbash\nPPid:\t%s\n' "$1" >"$R/proc/\$\$/status"
		echo '0::/user.slice/user-0.slice/session-c1.scope' >"$R/proc/\$\$/cgroup"
		"$H/install/common/hostisan/in-terminal.sh"
	EOF
	chmod 755 "$R/from"
	echo '0::/user.slice/user-0.slice/session-c1.scope' >"$R/cgroup"
	in_sandbox "$R/from"
}

@test "terminal check sees a sudo -i shell of the terminal" {
	sandbox
	terminal_chain
	# root's shell after sudo -i, and the commands it runs
	in_sandbox "$H/install/common/hostisan/in-terminal.sh" 300
	[ "$status" -eq 0 ]
	in_terminal_from 300
	[ "$status" -eq 0 ]
	# a longer chain: apt, dpkg and the postinst under that shell
	proc_entry 400 300 '0::/user.slice/user-0.slice/session-c1.scope'
	proc_entry 401 400 '0::/user.slice/user-0.slice/session-c1.scope'
	proc_entry 402 401 '0::/user.slice/user-0.slice/session-c1.scope'
	in_terminal_from 402
	[ "$status" -eq 0 ]
	# the session scope alone is not the terminal
	in_sandbox "$H/install/common/hostisan/in-terminal.sh"
	[ "$status" -eq 1 ]
}

@test "terminal check sees a direct child of the terminal" {
	sandbox
	terminal_chain
	in_sandbox "$H/install/common/hostisan/in-terminal.sh" 100
	[ "$status" -eq 0 ]
	in_terminal_from 100
	[ "$status" -eq 0 ]
	# in the cgroup itself: no walk needed (no fake /proc entry for it)
	rm -rf "$R/proc"
	mkdir -p "$R/proc"
	in_terminal
	in_sandbox "$H/install/common/hostisan/in-terminal.sh"
	[ "$status" -eq 0 ]
}

@test "terminal check says no outside the terminal" {
	sandbox
	terminal_chain
	local scope='0::/user.slice/user-1000.slice/session-4.scope'
	# an ssh login: sshd (20), its session (21), sudo -i (22)
	proc_entry 20 1 '0::/system.slice/ssh.service'
	proc_entry 21 20 "$scope"
	proc_entry 22 21 "$scope"
	in_sandbox "$H/install/common/hostisan/in-terminal.sh" 22
	[ "$status" -eq 1 ]
	in_terminal_from 22
	[ "$status" -eq 1 ]
	# a unit started from the terminal (systemd-run) is a child of pid 1
	proc_entry 30 1 '0::/system.slice/hostisan-update-1.service'
	proc_entry 31 30 '0::/system.slice/hostisan-update-1.service'
	in_terminal_from 31
	[ "$status" -eq 1 ]
	# pid 1 ends the walk, even in the terminal cgroup (it is never its child)
	proc_entry 1 0 '0::/system.slice/hestia-web-terminal.service'
	in_sandbox "$H/install/common/hostisan/in-terminal.sh" 22
	[ "$status" -eq 1 ]
	proc_entry 1 0 '0::/init.scope'
	# a loop of parents ends
	proc_entry 60 61 "$scope"
	proc_entry 61 60 "$scope"
	run timeout 10 env PATH="$R/stub:$PATH" "$H/install/common/hostisan/in-terminal.sh" 60
	[ "$status" -eq 1 ]
	# an unreadable or broken entry ends the walk: a gone parent, no PPid line,
	# a PPid that is not a number, no status file
	proc_entry 70 999 "$scope"
	mkdir -p "$R/proc/71"
	printf 'Name:\tbash\n' >"$R/proc/71/status"
	echo "$scope" >"$R/proc/71/cgroup"
	proc_entry 72 100 "$scope"
	printf 'Name:\tbash\nPPid:\t1x0\n' >"$R/proc/72/status"
	mkdir -p "$R/proc/73"
	echo "$scope" >"$R/proc/73/cgroup"
	local p
	for p in 70 71 72 73 999; do
		in_sandbox "$H/install/common/hostisan/in-terminal.sh" "$p"
		[ "$status" -eq 1 ]
	done
	# usage
	in_sandbox "$H/install/common/hostisan/in-terminal.sh" abc
	[ "$status" -eq 2 ]
	in_sandbox "$H/install/common/hostisan/in-terminal.sh" 1 2
	[ "$status" -eq 2 ]
}

@test "terminal postinst warns when the restart fails" {
	sandbox
	rewrite "$repo/src/deb/web-terminal/postinst" "$work/postinst"
	cat >"$R/stub/systemctl" <<-EOF
		#!/bin/bash
		echo "systemctl \$*" >>"$R/calls"
		[ "\$1" = restart ] && exit 1
		[ "\$1" = is-active ] && exit 3
		exit 0
	EOF
	in_sandbox "$work/postinst" configure 2.0.0-104+ubuntu24.04
	echo "$output"
	# a terminal that will not start must not leave the package unconfigured
	[ "$status" -eq 0 ]
	grep -qx 'systemctl restart hestia-web-terminal.service' "$R/calls"
	[[ "$output" == *"journalctl -u hestia-web-terminal"* ]]
	# the same with an older hestia that has no restart-terminal.sh
	mv "$H/install/common/hostisan/restart-terminal.sh" "$work/restart-terminal.sh"
	: >"$R/calls"
	in_sandbox "$work/postinst" configure 2.0.0-104+ubuntu24.04
	echo "$output"
	[ "$status" -eq 0 ]
	grep -qx 'systemctl restart hestia-web-terminal.service' "$R/calls"
	[[ "$output" == *"journalctl -u hestia-web-terminal"* ]]
	mv "$work/restart-terminal.sh" "$H/install/common/hostisan/restart-terminal.sh"
	# a deferred restart that cannot be scheduled warns too
	in_terminal
	printf '#!/bin/bash\necho "systemd-run $*" >>"%s/calls"\nexit 1\n' "$R" >"$R/stub/systemd-run"
	in_sandbox "$work/postinst" configure 2.0.0-104+ubuntu24.04
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"systemctl restart hestia-web-terminal"* ]]
}

@test "a terminal restart already scheduled is not scheduled again" {
	sandbox
	in_terminal
	# a second package (or a second upgrade) while the first one waits
	STUB_TIMER_ACTIVE=1 in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"already scheduled"* ]]
	[[ "$output" != *"[ ! ]"* ]]
	run ! grep -E '^systemd-run |^systemctl (try-)?restart (hostisan|hestia|hestia-web-terminal)\.service' "$R/calls"
	rewrite "$repo/src/deb/web-terminal/postinst" "$work/postinst"
	STUB_TIMER_ACTIVE=1 in_sandbox "$work/postinst" configure 2.0.0-105+ubuntu24.04
	[ "$status" -eq 0 ]
	[[ "$output" == *"already scheduled"* ]]
	[[ "$output" != *"[ ! ]"* ]]
}

# hestia_deb_postinst PKG: the PKG (nginx or php) postinst at $work/PKG.postinst,
# with the Hestia files it sources stubbed and hestia's init script present.
hestia_deb_postinst() {
	mkdir -p "$H/func" "$H/install/upgrade" "$R/etc/init.d"
	printf 'syshealth_adapt_hestia_nginx_listen_ports() { :; }\nsyshealth_adapt_nginx_resolver() { :; }\n' \
		>"$H/func/syshealth.sh"
	: >"$H/func/main.sh"
	: >"$H/func/upgrade.sh"
	: >"$H/install/upgrade/upgrade.conf"
	: >"$R/etc/init.d/hestia"
	stub update-rc.d
	stub invoke-rc.d
	rewrite "$repo/src/deb/$1/postinst" "$work/$1.postinst"
}

@test "nginx and php postinsts defer the hestia restart when run from the terminal" {
	local p
	for p in nginx php; do
		sandbox
		in_terminal
		hestia_deb_postinst "$p"
		in_sandbox "$work/$p.postinst" configure 1.10.5-101
		echo "$output"
		cat "$R/calls"
		[ "$status" -eq 0 ]
		[[ "$output" == *"hestia.service restarts once dpkg is done"* ]]
		# hestia-nginx carries every terminal session: an in-line restart hung
		# up a raw "apt upgrade" typed in the terminal, and dpkg with it
		run ! grep -E '^systemctl (try-)?restart hestia' "$R/calls"
		grep -qx "systemd-run --on-active=30 --unit=hostisan-hestia-restart --collect --timer-property=RemainAfterElapse=no /bin/bash $H/install/common/hostisan/restart-unit.sh hestia.service --deferred restart" "$R/calls"
		rm -rf "$R"
	done
}

@test "nginx and php postinsts restart hestia in line outside the terminal" {
	local p
	for p in nginx php; do
		sandbox
		hestia_deb_postinst "$p"
		in_sandbox "$work/$p.postinst" configure 1.10.5-101
		echo "$output"
		[ "$status" -eq 0 ]
		grep -qx 'systemctl restart hestia.service' "$R/calls"
		run ! grep -q '^systemd-run ' "$R/calls"
		# an older hestia without restart-unit.sh: the restart as before
		mv "$H/install/common/hostisan/restart-unit.sh" "$work/restart-unit.sh"
		: >"$R/calls"
		in_sandbox "$work/$p.postinst" configure 1.10.5-101
		[ "$status" -eq 0 ]
		grep -qx 'systemctl restart hestia' "$R/calls"
		# no init script (not set up yet): no restart
		: >"$R/calls"
		rm "$R/etc/init.d/hestia"
		in_sandbox "$work/$p.postinst" configure 1.10.5-101
		[ "$status" -eq 0 ]
		run ! grep -q 'restart' "$R/calls"
		rm -rf "$R"
	done
}

@test "nginx and php postinsts only warn when hestia does not restart" {
	local p
	for p in nginx php; do
		sandbox
		hestia_deb_postinst "$p"
		cat >"$R/stub/systemctl" <<-EOF
			#!/bin/bash
			echo "systemctl \$*" >>"$R/calls"
			[ "\$1" = restart ] && exit 1
			exit 0
		EOF
		# a hestia that will not restart must not leave the package unconfigured
		in_sandbox "$work/$p.postinst" configure 1.10.5-101
		echo "$output"
		[ "$status" -eq 0 ]
		[[ "$output" == *"hestia.service did not restart"* ]]
		# the same with an older hestia without restart-unit.sh
		rm "$H/install/common/hostisan/restart-unit.sh"
		in_sandbox "$work/$p.postinst" configure 1.10.5-101
		echo "$output"
		[ "$status" -eq 0 ]
		[[ "$output" == *"hestia did not restart"* ]]
		rm -rf "$R"
	done
}

# --- one restart per update (feature/upgrade-restarts) ---

@test "setup inside an update defers the panel, hestia and terminal restarts" {
	# rc7: inside the detached hostisan-update unit (not the terminal's cgroup)
	# setup.sh restarted hostisan in line during hestia's configure, and
	# hestia three times per update: every restart hangs up every terminal
	local d how
	for how in HOSTISAN_UPDATE HOSTISAN_DETACHED cgroup; do
		sandbox
		case "$how" in
			cgroup) echo '0::/system.slice/hostisan-update-20261104-144838-1-2.service' >"$R/cgroup" ;;
			*) export "$how=1" ;;
		esac
		in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
		unset HOSTISAN_UPDATE HOSTISAN_DETACHED
		echo "$how: $output"
		cat "$R/calls"
		[ "$status" -eq 0 ]
		[[ "$output" == *"hostisan.service restarts once this update is done"* ]]
		run ! grep -E '^systemctl (try-)?restart (hostisan|hestia|hestia-web-terminal)\.service' "$R/calls"
		d="--collect --timer-property=RemainAfterElapse=no /bin/bash $H/install/common/hostisan/restart-unit.sh"
		grep -qx "systemd-run --on-active=30 --unit=hostisan-panel-restart $d hostisan.service --deferred restart" "$R/calls"
		grep -qx "systemd-run --on-active=30 --unit=hostisan-hestia-restart $d hestia.service --deferred try-restart" "$R/calls"
		grep -qx "systemd-run --on-active=30 --unit=hostisan-terminal-restart $d hestia-web-terminal.service --deferred try-restart" "$R/calls"
		rm -rf "$R"
	done
}

@test "a restart an update already scheduled is not scheduled again" {
	sandbox
	# the nginx and php postinsts and upgrade_restart_services ask again
	HOSTISAN_UPDATE=1 STUB_TIMER_ACTIVE=1 in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"the hostisan.service restart is already scheduled for the end of this update"* ]]
	[[ "$output" != *"[ ! ]"* ]]
	run ! grep -E '^systemd-run |^systemctl (try-)?restart (hostisan|hestia|hestia-web-terminal)\.service' "$R/calls"
	local p
	for p in nginx php; do
		hestia_deb_postinst "$p"
		: >"$R/calls"
		HOSTISAN_UPDATE=1 STUB_TIMER_ACTIVE=1 in_sandbox "$work/$p.postinst" configure 1.30.5-109
		echo "$output"
		[ "$status" -eq 0 ]
		[[ "$output" == *"hestia.service restart is already scheduled"* ]]
		run ! grep -E '^systemd-run |^systemctl (try-)?restart hestia' "$R/calls"
	done
}

@test "nginx and php postinsts defer the hestia restart inside an update" {
	local p
	for p in nginx php; do
		sandbox
		hestia_deb_postinst "$p"
		HOSTISAN_UPDATE=1 in_sandbox "$work/$p.postinst" configure 1.30.5-109
		echo "$output"
		cat "$R/calls"
		[ "$status" -eq 0 ]
		[[ "$output" == *"hestia.service restarts once this update is done"* ]]
		run ! grep -E '^systemctl (try-)?restart hestia' "$R/calls"
		grep -qx "systemd-run --on-active=30 --unit=hostisan-hestia-restart --collect --timer-property=RemainAfterElapse=no /bin/bash $H/install/common/hostisan/restart-unit.sh hestia.service --deferred restart" "$R/calls"
		rm -rf "$R"
	done
}

@test "an update restarts in line when it cannot defer" {
	sandbox
	stub systemd-run
	printf '#!/bin/bash\necho "systemd-run $*" >>"%s/calls"\nexit 1\n' "$R" >"$R/stub/systemd-run"
	HOSTISAN_UPDATE=1 in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service restart
	echo "$output"
	cat "$R/calls"
	# outside the terminal an in-line restart only costs the open terminals
	[ "$status" -eq 0 ]
	[[ "$output" == *"could not schedule the hostisan.service restart; restarting it now"* ]]
	grep -qx 'systemctl restart hostisan.service' "$R/calls"
	[ "$(line_of '^systemd-run ')" -lt "$(line_of '^systemctl restart ')" ]
}

@test "a scheduled try-restart becomes a restart when a later caller asks for one" {
	# setup.sh asks hestia for try-restart first; func/upgrade.sh and the
	# nginx/php postinsts ask for restart later and found the timer: a stopped
	# or failed hestia stayed down after the update (try-restart starts nothing)
	sandbox
	dpkg_locks 0
	HOSTISAN_UPDATE=1 in_sandbox "$H/install/common/hostisan/setup.sh" --upgrade
	[ "$status" -eq 0 ]
	grep -q -- 'hestia.service --deferred try-restart$' "$R/calls"
	[ "$(cat "$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode")" = try-restart ]
	hestia_deb_postinst nginx
	HOSTISAN_UPDATE=1 STUB_TIMER_ACTIVE=1 in_sandbox "$work/nginx.postinst" configure 1.30.5-109
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"hestia.service restart is already scheduled"* ]]
	[ "$(cat "$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode")" = restart ]
	# the transient unit still says try-restart; it reads the mode file
	: >"$R/calls"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hestia.service --deferred try-restart
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	grep -qx 'systemctl restart hestia.service' "$R/calls"
	run ! grep -q '^systemctl try-restart' "$R/calls"
	# the file is used once
	[ ! -e "$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode" ]
}

@test "a scheduled restart stays a restart when a later caller asks for try-restart" {
	sandbox
	dpkg_locks 0
	hestia_deb_postinst nginx
	HOSTISAN_UPDATE=1 in_sandbox "$work/nginx.postinst" configure 1.30.5-109
	[ "$status" -eq 0 ]
	grep -q -- 'hestia.service --deferred restart$' "$R/calls"
	[ "$(cat "$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode")" = restart ]
	HOSTISAN_UPDATE=1 STUB_TIMER_ACTIVE=1 in_sandbox "$H/install/common/hostisan/restart-unit.sh" hestia.service try-restart
	[ "$status" -eq 0 ]
	[ "$(cat "$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode")" = restart ]
	: >"$R/calls"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hestia.service --deferred restart
	[ "$status" -eq 0 ]
	grep -qx 'systemctl restart hestia.service' "$R/calls"
}

@test "a deferred restart keeps its own mode without a mode file" {
	sandbox
	dpkg_locks 0
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hestia.service --deferred try-restart
	[ "$status" -eq 0 ]
	grep -qx 'systemctl try-restart hestia.service' "$R/calls"
	# a file that says anything else does not change it
	mkdir -p "$HOSTISAN_RESTART_DIR"
	echo 'stop' >"$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode"
	: >"$R/calls"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hestia.service --deferred try-restart
	[ "$status" -eq 0 ]
	grep -qx 'systemctl try-restart hestia.service' "$R/calls"
	# a restart that cannot be scheduled leaves no mode file behind
	printf '#!/bin/bash\necho "systemd-run $*" >>"%s/calls"\nexit 1\n' "$R" >"$R/stub/systemd-run"
	HOSTISAN_UPDATE=1 in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service restart
	[ "$status" -eq 0 ]
	[ ! -e "$HOSTISAN_RESTART_DIR/hostisan-panel-restart.mode" ]
}

@test "in-update says no outside an update" {
	sandbox
	in_sandbox "$H/install/common/hostisan/in-update.sh"
	[ "$status" -eq 1 ]
	# the terminal, an ssh session, the deferred restart units, another unit
	local c
	for c in /system.slice/hestia-web-terminal.service /user.slice/user-0.slice/session-1.scope \
		/system.slice/hostisan-panel-restart.service /system.slice/hostisan-update.socket \
		/system.slice/myhostisan-update-1.service/x; do
		echo "0::$c" >"$R/cgroup"
		in_sandbox "$H/install/common/hostisan/in-update.sh"
		[ "$status" -eq 1 ]
	done
	echo '0::/system.slice/hostisan-update-1-2.service' >"$R/cgroup"
	in_sandbox "$H/install/common/hostisan/in-update.sh"
	[ "$status" -eq 0 ]
	echo '5:cpu,cpuacct:/system.slice/hostisan-update-1-2.service' >"$R/cgroup"
	in_sandbox "$H/install/common/hostisan/in-update.sh"
	[ "$status" -eq 0 ]
	in_sandbox "$H/install/common/hostisan/in-update.sh" x
	[ "$status" -eq 2 ]
}

@test "the deferred restarts wait until a detached update has written its status" {
	sandbox
	dpkg_locks 0
	# the loop has ended; run-detached.sh --run is writing the status line the
	# terminal waits for. The settle lets the terminal print it and return
	# the prompt before the panel restart hangs the terminal up.
	fake_procs "/bin/bash $H/install/common/hostisan/run-detached.sh --run hostisan-update-1 /var/log/hestia/hostisan-update-1.log $H/bin/v-update-sys-hestia-all"
	cat >"$R/stub/sleep" <<-EOF
		#!/bin/bash
		echo "sleep \$*" >>"$R/calls"
		rm -rf "$R/proc/100"
		exit 0
	EOF
	chmod 755 "$R/stub/sleep"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service --deferred restart
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	[ "$(grep -c '^sleep 5$' "$R/calls")" -eq 1 ]
	grep -qx 'sleep 3' "$R/calls"
	[ "$(line_of '^systemctl restart ')" -gt "$(line_of '^sleep 3$')" ]
	# the follower in the terminal (no --run) is not the update
	: >"$R/calls"
	fake_procs "/bin/bash $H/install/common/hostisan/run-detached.sh $H/bin/v-update-sys-hestia-all"
	in_sandbox "$H/install/common/hostisan/restart-unit.sh" hostisan.service --deferred restart
	[ "$status" -eq 0 ]
	run ! grep -q '^sleep ' "$R/calls"
}

@test "nginx postinst leaves the panel port's hestia restart to its own" {
	sandbox
	hestia_deb_postinst nginx
	echo "BACKEND_PORT='8443'" >>"$H/conf/hestia.conf"
	stub_v v-change-sys-port
	in_terminal
	in_sandbox "$work/nginx.postinst" configure 1.30.5-109
	echo "$output"
	cat "$R/calls"
	[ "$status" -eq 0 ]
	# v-change-sys-port's own restart was in line: on a non-default port it
	# hung up a raw "apt upgrade" typed in the terminal
	grep -qx "v-change-sys-port 8443 no" "$R/calls"
	run ! grep -E '^systemctl (try-)?restart hestia' "$R/calls"
	grep -q '^systemd-run .*hestia.service --deferred restart$' "$R/calls"
	# the port moves before the restart the end of the script asks for (the
	# start near its top may schedule it earlier; it still runs after dpkg)
	[ "$(line_of '^v-change-sys-port ')" -lt "$(grep -n '^systemd-run ' "$R/calls" | tail -n1 | cut -d: -f1)" ]
}

@test "terminal prerm disables the unit on remove" {
	sandbox
	rewrite "$repo/src/deb/web-terminal/prerm" "$work/prerm"
	in_sandbox "$work/prerm" remove
	[ "$status" -eq 0 ]
	grep -qx 'systemctl disable --now hestia-web-terminal.service' "$R/calls"
	: >"$R/calls"
	in_sandbox "$work/prerm" upgrade 2.0.1
	[ "$status" -eq 0 ]
	[ ! -s "$R/calls" ]
}

@test "maintainer scripts pass shellcheck" {
	command -v shellcheck >/dev/null || skip "no shellcheck"
	shellcheck -S warning "$repo"/src/deb/web-terminal/{preinst,postinst,prerm} \
		"$repo"/src/deb/{nginx,php}/postinst \
		"$repo"/install/common/hostisan/{setup,remove-node-terminal,sudoers,restart-terminal,restart-unit,in-terminal,in-update,run-detached,update-running,signing-key}.sh
}

# ---------------------------------------------------------------- built debs

@test "deb: core deb carries hostisan" {
	need_debs
	run dpkg-deb -c "$core"
	[ "$status" -eq 0 ]
	grep -Eq '^-rwxr-xr-x root/root .* \./usr/local/hestia/hostisan/hostisan$' <<<"$output"
	grep -Eq '^-rw-r--r-- root/root .* \./lib/systemd/system/hostisan\.service$' <<<"$output"
	grep -Eq ' \./usr/local/hestia/install/common/hostisan/setup\.sh$' <<<"$output"
	grep -Eq ' \./usr/local/hestia/install/common/hostisan/hostisan\.env$' <<<"$output"
	# every file is root's
	run ! grep -Ev '^[^ ]+ root/root ' <<<"$output"
}

@test "deb: hostisan binary is static and stamped" {
	need_debs
	dpkg-deb -x "$core" "$work/x"
	local bin=$work/x/usr/local/hestia/hostisan/hostisan
	run file "$bin"
	echo "$output"
	[[ "$output" == *"statically linked"* ]]
	run "$bin" version
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == "hostisan "* ]]
	[ "$output" != "hostisan dev" ]
	# the UI is embedded
	grep -q 'id="root"' <(strings "$bin")
	cmp "$work/x/lib/systemd/system/hostisan.service" "$repo/src/deb/hestia/hostisan.service"
}

@test "deb: nothing new in bin" {
	need_debs
	local got want added
	got=$(dpkg-deb -c "$core" | awk '{print $NF}' | sed -n 's#^\./usr/local/hestia/bin/\(..*\)#\1#p' | sort)
	want=$(git -C "$repo" ls-tree --name-only HEAD bin/ | sed 's#^bin/##' | sort)
	[ "$got" = "$want" ]
	added=$(comm -13 <(git -C "$repo" ls-tree --name-only 94c1e760f bin/ | sed 's#^bin/##' | sort) <(echo "$got"))
	[ "$added" = "$(printf '%s\n' v-change-sys-php-ini v-delete-sys-hostisan-sessions v-hostisan-run | sort)" ]
}

@test "deb: old php panel is not shipped" {
	need_debs
	local list
	list=$(dpkg-deb -c "$core" | awk '{print $NF}' | sed -n 's#^\./usr/local/hestia/web/##p' | grep -v '^$')
	# kept for the core scripts, FileGator and the error pages
	grep -qx 'inc/mail-wrapper.php' <<<"$list"
	grep -qx 'inc/main.php' <<<"$list"
	grep -qx 'inc/2fa/secret.php' <<<"$list"
	grep -qx 'src/init.php' <<<"$list"
	grep -qx 'error/404.html' <<<"$list"
	grep -qx 'images/logo.svg' <<<"$list"
	grep -qx 'locale/languages.json' <<<"$list"
	grep -qx 'css/themes/default.min.css' <<<"$list"
	grep -qx 'api/index.php' <<<"$list"
	# nothing else at the top of web/
	local top
	top=$(cut -d/ -f1 <<<"$list" | sort -u | tr '\n' ' ')
	[ "$top" = "api css error favicon.ico images inc locale src " ]
	run ! grep -Eq '^css/(src|dist)/|^api/v1/' <<<"$list"
	dpkg-deb -c "$core" | grep -Eq '^-rwxr-xr-x .* \./usr/local/hestia/web/inc/mail-wrapper\.php$'
}

@test "deb: terminal deb is unit only" {
	need_debs
	local files
	files=$(dpkg-deb -c "$term" | awk '$1 !~ /^d/ {print $NF}' | sort | tr '\n' ' ')
	[ "$files" = "./lib/systemd/system/hestia-web-terminal.service ./usr/share/doc/hestia-web-terminal/copyright " ]
	dpkg-deb -f "$term" Version | grep -Eq '^1:2\.[0-9.]+-[0-9]+\+(debian|ubuntu)[0-9.]+$'
	[ "$(dpkg-deb -f "$term" Depends)" = 'hestia (>= 1:1.10.5-105)' ]
	run ! grep -qi node <<<"$(dpkg-deb -I "$term")"
	dpkg-deb -e "$term" "$work/term"
	cmp "$work/term/postinst" "$repo/src/deb/web-terminal/postinst"
	cmp "$work/term/preinst" "$repo/src/deb/web-terminal/preinst"
	cmp "$work/term/prerm" "$repo/src/deb/web-terminal/prerm"
}

@test "deb: control fields" {
	need_debs
	local d
	for d in "$core" "$term"; do
		[ "$(dpkg-deb -f "$d" Maintainer)" = "Hostisan <$HOSTISAN_INFO_EMAIL>" ]
		[ "$(dpkg-deb -f "$d" Homepage)" = "$HOSTISAN_URL" ]
		run ! grep -q '@HOSTISAN_' <<<"$(dpkg-deb -f "$d")"
		run ! grep -qi hestia <<<"$(dpkg-deb -f "$d" Description)"
	done
	dpkg-deb -f "$core" Version | grep -Eq '^1:1\.10\.5-[0-9]+\+(debian|ubuntu)[0-9.]+$'
	dpkg-deb -f "$term" Version | grep -Eq '^1:2\.'
	# the revision is a Hostisan build, above upstream's -1
	local rev
	rev=$(dpkg-deb -f "$core" Version | sed -E 's/^[^-]+-([0-9]+)\+.*/\1/')
	[ "$rev" -ge 101 ]
	[ "$(dpkg-deb -f "$core" Breaks)" = 'hestia-web-terminal (<< 1:2.0.0)' ]
	dpkg-deb -e "$core" "$work/core"
	cmp "$work/core/postinst" "$repo/src/deb/hestia/postinst"
}

@test "build never runs npm ci through a node_modules symlink" {
	# A worktree's ui/node_modules may link to another checkout's; npm ci in the
	# build copy would empty that directory.
	local section
	section=$(sed -n '/^# Building hestia$/,$p' "$repo/src/hst_autocompile.sh" | sed -n '1,/npm ci --ignore-scripts/p')
	grep -q 'for dir in node_modules ui/node_modules; do' <<<"$section"
	grep -q 'rm -f "$dir"' <<<"$section"
}

@test "nginx postinst leaves a stopped hestia's start to the deferred restart" {
	# rc8 D: upstream's "invoke-rc.d hestia start" started a stopped hestia in
	# the middle of the update, and the deferred restart then ran as well
	local how
	for how in update terminal; do
		sandbox
		dpkg_locks 0
		hestia_deb_postinst nginx
		case "$how" in
			update) export HOSTISAN_UPDATE=1 ;;
			terminal) in_terminal ;;
		esac
		in_sandbox "$work/nginx.postinst" configure 1.30.5-112
		unset HOSTISAN_UPDATE
		echo "$how: $output"
		cat "$R/calls"
		[ "$status" -eq 0 ]
		run ! grep -E '^invoke-rc.d hestia start|^systemctl (re)?start hestia' "$R/calls"
		grep -q -- '--unit=hostisan-hestia-restart .* hestia.service --deferred restart$' "$R/calls"
		# the one deferred run starts it: restart starts a stopped unit
		[ "$(cat "$HOSTISAN_RESTART_DIR/hostisan-hestia-restart.mode")" = restart ]
		rm -rf "$R"
	done
}

@test "nginx postinst starts hestia in line outside an update and the terminal" {
	sandbox
	hestia_deb_postinst nginx
	in_sandbox "$work/nginx.postinst" configure 1.30.5-112
	echo "$output"
	[ "$status" -eq 0 ]
	grep -qx 'invoke-rc.d hestia start' "$R/calls"
	# an older hestia without restart-unit.sh, inside an update: as upstream
	rm "$H/install/common/hostisan/restart-unit.sh"
	: >"$R/calls"
	HOSTISAN_UPDATE=1 in_sandbox "$work/nginx.postinst" configure 1.30.5-112
	[ "$status" -eq 0 ]
	grep -qx 'invoke-rc.d hestia start' "$R/calls"
}
