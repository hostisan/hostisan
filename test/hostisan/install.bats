#!/usr/bin/env bats
# The Hostisan installers (Plan 6, B8): install/hst-install.sh and
# install/hst-install-{debian,ubuntu}.sh.
#
# Static checks read the installers. The sudo guard tests source the guard
# block both installers carry (between the "hostisan helpers" markers) and
# run it on temp sudoers files; visudo runs for real when it is installed, and
# the "docker:" tests run it on real images (skipped without docker).
# install-smoke.sh checks a finished install inside a container or on a box.

# The helpers read variables these tests set (installer globals).
# shellcheck disable=SC2034,SC2120

bats_require_minimum_version 1.5.0

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	deb="$repo/install/hst-install-debian.sh"
	ubu="$repo/install/hst-install-ubuntu.sh"
	wrapper="$repo/install/hst-install.sh"
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/install.XXXXXX")
	# shellcheck source=install/common/hostisan/product.env
	source "$repo/install/common/hostisan/product.env"
}

teardown() {
	rm -rf "$work"
}

# ---------------------------------------------------------------- helpers

# code FILE: FILE without comment lines (a line whose first non-blank is #).
code() {
	grep -vE '^[[:space:]]*#' "$1"
}

# line_of FILE ERE: the number of the first line of FILE matching ERE.
line_of() {
	grep -nE "$2" "$1" | head -n1 | cut -d: -f1
}

# guard_block FILE: the sudo guard block of installer FILE.
guard_block() {
	awk '/^# >>> hostisan helpers/ { on = 1 } on { print } /^# <<< hostisan helpers/ { on = 0 }' "$1"
}

# guard_env [INSTALLER]: source the guard of INSTALLER (default Ubuntu) and
# point it at temp sudoers, group and passwd files under $work.
guard_env() {
	local src=${1:-$ubu}
	guard_block "$src" >"$work/guard.sh"
	[ -s "$work/guard.sh" ] || return 1
	mkdir -p "$work/sudoers.d" "$work/backup"
	printf 'Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n%%sudo ALL=(ALL:ALL) ALL\n' >"$work/sudoers"
	printf 'root:x:0:\nsudo:x:27:\n' >"$work/group"
	printf 'root:x:0:0:root:/root:/bin/bash\n' >"$work/passwd"
	export HOSTISAN_INSTALL_TEST=1 HOSTISAN_SUDOERS="$work/sudoers" HOSTISAN_SUDOERS_D="$work/sudoers.d" \
		HOSTISAN_SUDOERS_BACKUP="$work/backup" HOSTISAN_GROUP_FILE="$work/group" \
		HOSTISAN_PASSWD_FILE="$work/passwd"
	# shellcheck disable=SC1091
	source "$work/guard.sh"
}

# stub_visudo RC: a visudo on PATH that logs its arguments and exits RC.
stub_visudo() {
	mkdir -p "$work/bin"
	cat >"$work/bin/visudo" <<-EOF
		#!/bin/sh
		echo "visudo \$*" >>"$work/visudo.calls"
		exit $1
	EOF
	chmod +x "$work/bin/visudo"
	PATH="$work/bin:$PATH"
}

have_docker() {
	command -v docker >/dev/null && docker info >/dev/null 2>&1
}

# ---------------------------------------------------------------- repository

@test "installers enable the panel service at boot" {
	# update-rc.d defaults keeps existing K links, so a box where hestia was once
	# disabled would never start it again after a reboot.
	local f
	for f in "$deb" "$ubu"; do
		grep -qE '^systemctl enable hestia( |$)' <(code "$f")
	done
}

@test "installers use no network source until O7" {
	local f
	for f in "$deb" "$ubu" "$wrapper"; do
		run ! grep -nE 'hestiacp\.com/|apt\.hestiacp|hestiacp/hestiacp|raw\.githubusercontent|HOSTISAN_RAW_URL|HOSTISAN_APT_KEY_URL|apt\.'"${HOSTISAN_DOMAIN//./\\.}" <(code "$f" | grep -v 'ip\.hestiacp\.com')
	done
	for f in "$deb" "$ubu"; do
		grep -qE '^hostisan_apt_setup "\$ARCH" "\$codename"$' "$f"
		# The key fingerprint gets pinned once the repository exists.
		grep -q 'TODO(O7)' "$f"
	done
}

@test "installers drop any upstream hestiacp apt source" {
	local f
	guard_env
	mkdir -p "$work/apt/sources.list.d" "$work/keyrings"
	echo 'deb [signed-by=/usr/share/keyrings/hestia-keyring.gpg] https://apt.hestiacp.com/ noble main' >"$work/apt/sources.list.d/hestia.list"
	echo 'deb https://deb.debian.org/debian trixie main' >"$work/apt/sources.list.d/debian.list"
	HOSTISAN_APT_DIR="$work/apt" hostisan_drop_upstream_apt
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
	[ -e "$work/apt/sources.list.d/debian.list" ]
}

@test "installers install hestia unpinned and check its epoch" {
	local f drop
	for f in "$deb" "$ubu"; do
		# any build number a repository carries installs (200 + CI run number)
		run ! grep -nE 'hestia=|HOSTISAN_BUILD|HESTIA_INSTALL_BUILD' <(code "$f")
		grep -qE '^  exim4 .* git hestia hestia-nginx hestia-php hestia-web-terminal$' "$f"
		grep -qx "HOSTISAN_EPOCH='1'" "$f"
		[ "$(line_of "$f" '^\s*hostisan_check_epoch hestia')" -gt "$(line_of "$f" '^\s*hostisan_check_installed hestia')" ]
		# the terminal too, when it is installed
		[ "$(grep -B1 -xF '	hostisan_check_epoch hestia hestia-nginx hestia-php hestia-web-terminal' "$f" | head -n1)" = \
			'if [ "$webterminal" = "yes" ]; then' ]
		grep -qxF '	hostisan_check_epoch hestia hestia-nginx hestia-php' "$f"
		# --with-debs still takes hestia (and only hestia) out of the apt list
		drop=$(grep -F 'software=$(echo "$software" | sed -E' "$f" | grep -F '(^|[[:space:]])hestia(' | sed -e 's/^\s*//')
		[ -n "$drop" ]
		software="git hestia hestia-nginx hestia-php hestia-web-terminal idn2"
		eval "$drop"
		[ "$software" = "git  hestia-nginx hestia-php hestia-web-terminal idn2" ]
	done
}

@test "the epoch check refuses a package without the Hostisan epoch" {
	guard_env
	HOSTISAN_EPOCH=1
	mkdir -p "$work/bin"
	cat >"$work/bin/dpkg-query" <<-'EOF'
		#!/bin/sh
		case "$3" in
			hestia) echo "1:1.10.5-237+debian13" ;;
			hestia-php) echo "8.5.11-1+debian13" ;;
			*) exit 1 ;;
		esac
	EOF
	chmod +x "$work/bin/dpkg-query"
	PATH="$work/bin:$PATH"
	hostisan_check_epoch hestia
	run hostisan_check_epoch hestia hestia-php
	[ "$status" -ne 0 ]
	[[ "$output" == *"hestia-php 8.5.11-1+debian13"* ]]
	[[ "$output" != *"hestia 1:"* ]]
	run hostisan_check_epoch hestia-nginx
	[ "$status" -ne 0 ]
}

# The pinned Hostisan release key (install/common/hostisan).
PINNED=9C25AACF8D6449319AD52FE2D5DD6BC64A10C174

# apt_env: the helpers with a stub curl, a gpg that logs its calls to
# $work/calls and runs the real one, a temp apt dir and keyring, and the
# release key from this tree.
apt_env() {
	guard_env
	mkdir -p "$work/bin" "$work/apt/sources.list.d" "$work/debs"
	mkdir -m 700 "$work/gnupg"
	export GNUPGHOME=$work/gnupg
	printf '#!/bin/sh\necho "curl $*" >>"%s/calls"; echo KEYDATA\n' "$work" >"$work/bin/curl"
	printf '#!/bin/sh\necho "gpg $*" >>"%s/calls"; exec %s "$@"\n' "$work" "$(command -v gpg)" >"$work/bin/gpg"
	chmod +x "$work/bin/curl" "$work/bin/gpg"
	PATH="$work/bin:$PATH"
	keys=$repo/install/common/hostisan
	export HOSTISAN_APT_DIR="$work/apt" HOSTISAN_KEYRING="$work/keyring.gpg" HOSTISAN_KEY_DIR=$keys
	RHOST=apt.$HOSTISAN_DOMAIN
	HOSTISAN_APT_KEY_URL=https://$RHOST/hostisan-archive-keyring.asc
	withdebs='' aptrepo='' aptkey='' aptkeyunpinned='' webterminal=no
}

# test_key: a throwaway signing key in $work/tk (keygen.sh), as a test
# repository uses; its fingerprint in $TK_FPR.
test_key() {
	GNUPGHOME= "$repo/tools/apt-repo/keygen.sh" "$work/tk" >/dev/null
	TK_FPR=$(cat "$work/tk/fingerprint.txt")
}

@test "installers take --apt-repo and --apt-key" {
	local f
	for f in "$deb" "$ubu"; do
		grep -qF -- '--apt-repo) args="${args}-R " ;;' "$f"
		grep -qF -- '--apt-key) args="${args}-K " ;;' "$f"
		grep -qF -- '--apt-key-unpinned) args="${args}-U " ;;' "$f"
		grep -qE 'getopts "[^"]*R:K:[^"]*"' "$f"
		# -U takes no value
		grep -qE 'getopts "[^"]*[^:A-Za-z]?U[A-Za-z][^"]*"' "$f"
		grep -qE '^\s*R\) aptrepo=\$OPTARG' "$f"
		grep -qE '^\s*K\) aptkey=\$OPTARG' "$f"
		grep -qE "^\s*U\) aptkeyunpinned='yes'" "$f"
		grep -qE '^  -U, --apt-key-unpinned ' "$f"
		grep -qE '^hostisan_check_sources$' "$f"
	done
}

@test "local debs alone: no apt repository at all" {
	apt_env
	touch "$work/debs/hestia_1.10.5-101+debian13_amd64.deb" \
		"$work/debs/hestia-nginx_1.30.5-101+debian13_amd64.deb" \
		"$work/debs/hestia-php_8.5.11-101+debian13_amd64.deb"
	echo 'deb https://apt.hestiacp.com/ trixie main' >"$work/apt/sources.list.d/hestia.list"
	withdebs="$work/debs/../debs"
	hostisan_check_sources
	[ "$withdebs" = "$work/debs" ]
	run hostisan_apt_setup amd64 trixie
	[ "$status" -eq 0 ]
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
	[ ! -e "$work/calls" ]
}

@test "local debs must hold every Hostisan package" {
	apt_env
	touch "$work/debs/hestia_1.10.5-101+debian13_amd64.deb" "$work/debs/hestia-php_8.5.11-101+debian13_amd64.deb"
	withdebs="$work/debs"
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	[[ "$output" == *"hestia-nginx_"* ]]
	touch "$work/debs/hestia-nginx_1.30.5-101+debian13_amd64.deb"
	webterminal=yes
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	[[ "$output" == *"hestia-web-terminal_"* ]]
	withdebs="$work/nosuch"
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	# One version per package.
	touch "$work/debs/hestia-web-terminal_2.0.0-101+debian13_amd64.deb" "$work/debs/hestia_1.10.5-102+debian13_amd64.deb"
	withdebs="$work/debs"
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	[[ "$output" == *"hestia_"* ]]
}

@test "a repository without --apt-key gets the release key" {
	apt_env
	aptrepo="file:$work/repo"
	hostisan_check_sources
	[ -z "$aptkey" ]
	run hostisan_apt_setup amd64 trixie
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$keys/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
	[ "$(stat -c %a "$work/keyring.gpg")" = 644 ]
	[ "$(cat "$work/apt/sources.list.d/hestia.list")" = "deb [arch=amd64 signed-by=$work/keyring.gpg] file:$work/repo trixie main" ]
	[[ "$output" == *"$PINNED"* ]]
	[[ "$output" != *"WARNING"* ]]
	run grep -q curl "$work/calls"
	[ "$status" -ne 0 ]
	# no release key in the tree: refused before anything is written
	rm -f "$work/keyring.gpg" "$work/apt/sources.list.d/hestia.list"
	HOSTISAN_KEY_DIR=$work/nokeys run hostisan_check_sources
	[ "$status" -ne 0 ]
	HOSTISAN_KEY_DIR=$work/nokeys run hostisan_apt_setup amd64 trixie
	[ "$status" -ne 0 ]
	[ ! -e "$work/keyring.gpg" ]
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
}

@test "a repository's options are checked" {
	apt_env
	aptrepo="ftp://x/"
	aptkey="$work/key.asc"
	touch "$aptkey"
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	aptrepo='' 
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	# --apt-key-unpinned only goes with --apt-key
	aptrepo="file:$work/repo" aptkey='' aptkeyunpinned=yes
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	[[ "$output" == *"--apt-key-unpinned"* ]]
}

@test "a local signed repository becomes the hestia.list source" {
	apt_env
	mkdir -p "$work/keys"
	cp "$keys/hostisan-archive-keyring.asc" "$work/keys/repo.asc"
	aptrepo="file:$work/repo"
	aptkey="$work/keys/../keys/repo.asc"
	hostisan_check_sources
	[ "$aptkey" = "$work/keys/repo.asc" ]
	hostisan_apt_setup amd64 trixie
	[ "$(cat "$work/apt/sources.list.d/hestia.list")" = "deb [arch=amd64 signed-by=$work/keyring.gpg] file:$work/repo trixie main" ]
	grep -q -- '--dearmor' "$work/calls"
	# an armored key is dearmored
	cmp "$keys/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
	run grep -q curl "$work/calls"
	[ "$status" -ne 0 ]
	# A binary key goes through a throwaway GNUPGHOME and comes out unchanged.
	cp "$keys/hostisan-archive-keyring.gpg" "$work/keys/repo.gpg"
	aptkey="$work/keys/repo.gpg"
	rm -f "$work/calls"
	hostisan_apt_setup amd64 trixie
	cmp "$work/keys/repo.gpg" "$work/keyring.gpg"
	run grep -q -- --dearmor "$work/calls"
	[ "$status" -ne 0 ]
}

@test "no source given: the install is refused" {
	apt_env
	run hostisan_check_sources
	[ "$status" -ne 0 ]
	[[ "$output" == *"--with-debs"* && "$output" == *"--apt-repo"* ]]
	run hostisan_apt_setup amd64 noble
	[ "$status" -ne 0 ]
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
	[ ! -e "$work/calls" ]
}

@test "the apt key is shown before it is trusted" {
	apt_env
	aptrepo="file:$work/repo" aptkey="$keys/hostisan-archive-keyring.asc"
	run hostisan_apt_setup amd64 noble
	[ "$status" -eq 0 ]
	[[ "$output" == *"$aptkey"* && "$output" == *"$PINNED"* ]]
	[[ "$output" == *"release key"* ]]
}

@test "an --apt-key that is not the release key is refused" {
	apt_env
	test_key
	aptrepo="file:$work/repo" aptkey="$work/tk/public.asc"
	# caught up front when gpg is there
	run hostisan_check_sources
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"$TK_FPR"* && "$output" == *"$PINNED"* && "$output" == *"--apt-key-unpinned"* ]]
	run hostisan_apt_setup amd64 noble
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"$TK_FPR"* && "$output" == *"$PINNED"* && "$output" == *"--apt-key-unpinned"* ]]
	[ ! -e "$work/keyring.gpg" ]
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
	# nor the release key with another one beside it
	cat "$keys/hostisan-archive-keyring.gpg" "$work/tk/hostisan-archive-keyring.gpg" >"$work/two.gpg"
	aptkey="$work/two.gpg"
	run hostisan_apt_setup amd64 noble
	[ "$status" -ne 0 ]
	[ ! -e "$work/keyring.gpg" ]
	# nor a file that holds no key
	echo k >"$work/junk.gpg"
	aptkey="$work/junk.gpg" aptkeyunpinned=yes
	run hostisan_apt_setup amd64 noble
	[ "$status" -ne 0 ]
	[ ! -e "$work/keyring.gpg" ]
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
}

@test "--apt-key-unpinned trusts a test key, loudly" {
	apt_env
	test_key
	aptrepo="file:$work/repo" aptkey="$work/tk/public.asc" aptkeyunpinned=yes
	hostisan_check_sources
	run hostisan_apt_setup amd64 noble
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"WARNING"* && "$output" == *"$TK_FPR"* && "$output" == *"$PINNED"* ]]
	cmp "$work/tk/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
	[ "$(cat "$work/apt/sources.list.d/hestia.list")" = "deb [arch=amd64 signed-by=$work/keyring.gpg] file:$work/repo noble main" ]
}

# swap_keydir SWAP: $work/kd, a key dir whose signing-key.sh runs the real one,
# keeps the file it checked as $work/checked, then overwrites $aptkey with SWAP
# (a key file swapped right after the check).
swap_keydir() {
	mkdir -p "$work/kd"
	cp "$keys"/hostisan-archive-keyring.* "$keys/signing-key.sh" "$work/kd/"
	mv "$work/kd/signing-key.sh" "$work/kd/real-signing-key.sh"
	cat >"$work/kd/signing-key.sh" <<-EOF
		#!/bin/bash
		"$work/kd/real-signing-key.sh" "\$@"
		rc=\$?
		if [ "\$1" = check ]; then
			cp "\$2" "$work/checked"
			cp "$1" "$aptkey"
		fi
		exit \$rc
	EOF
	chmod +x "$work/kd/signing-key.sh"
	export HOSTISAN_KEY_DIR=$work/kd
}

@test "the key installed is the copy that was checked" {
	apt_env
	test_key
	mkdir "$work/tmp"
	export TMPDIR=$work/tmp
	mkdir -p "$work/keys"
	for f in hostisan-archive-keyring.asc hostisan-archive-keyring.gpg; do
		cp "$keys/$f" "$work/keys/$f"
		aptrepo="file:$work/repo" aptkey="$work/keys/$f" aptkeyunpinned=''
		rm -f "$work/keyring.gpg" "$work/checked"
		swap_keydir "$work/tk/hostisan-archive-keyring.gpg"
		run hostisan_apt_setup amd64 trixie
		echo "$output"
		[ "$status" -eq 0 ]
		# the file was swapped for the test key after the check ...
		cmp "$work/tk/hostisan-archive-keyring.gpg" "$aptkey"
		# ... yet the keyring holds the bytes that were checked: the release key
		cmp "$work/checked" "$work/keyring.gpg"
		cmp "$keys/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
		[ "$(stat -c %a "$work/keyring.gpg")" = 644 ]
	done
	# the shipped key, with no --apt-key, the same way
	aptkey='' 
	rm -f "$work/keyring.gpg" "$work/checked"
	swap_keydir "$work/tk/hostisan-archive-keyring.gpg"
	run hostisan_apt_setup amd64 trixie
	[ "$status" -eq 0 ]
	cmp "$work/checked" "$work/keyring.gpg"
	cmp "$keys/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
	# and --apt-key-unpinned: the test key, swapped for the release key after
	# the check, is still what gets installed
	cp "$work/tk/public.asc" "$work/keys/test.asc"
	aptkey="$work/keys/test.asc" aptkeyunpinned=yes
	rm -f "$work/keyring.gpg" "$work/checked"
	swap_keydir "$keys/hostisan-archive-keyring.gpg"
	run hostisan_apt_setup amd64 trixie
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$work/checked" "$work/keyring.gpg"
	cmp "$work/tk/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
	# no temp copy is left behind
	[ -z "$(ls -A "$work/tmp")" ]
}

@test "a key file mixing armored and binary parts installs only what was checked" {
	apt_env
	test_key
	# armored release key, then the binary test key: only the release key is
	# read, checked and installed
	cat "$keys/hostisan-archive-keyring.asc" "$work/tk/hostisan-archive-keyring.gpg" >"$work/mixed"
	aptrepo="file:$work/repo" aptkey="$work/mixed"
	swap_keydir "$work/mixed"
	run hostisan_apt_setup amd64 trixie
	echo "$output"
	[ "$status" -eq 0 ]
	cmp "$work/checked" "$work/keyring.gpg"
	cmp "$keys/hostisan-archive-keyring.gpg" "$work/keyring.gpg"
	[ "$("$keys/signing-key.sh" fpr "$work/keyring.gpg")" = "$PINNED" ]
	# binary release key, then the armored test key: refused, nothing written
	rm -f "$work/keyring.gpg" "$work/apt/sources.list.d/hestia.list"
	cat "$keys/hostisan-archive-keyring.gpg" "$work/tk/public.asc" >"$work/mixed"
	run hostisan_apt_setup amd64 trixie
	echo "$output"
	[ "$status" -ne 0 ]
	[ ! -e "$work/keyring.gpg" ]
	[ ! -e "$work/apt/sources.list.d/hestia.list" ]
}

@test "--apt-key-unpinned refuses a file with two keys" {
	apt_env
	test_key
	local f
	cat "$keys/hostisan-archive-keyring.gpg" "$work/tk/hostisan-archive-keyring.gpg" >"$work/two.gpg"
	cat "$work/tk/public.asc" "$keys/hostisan-archive-keyring.asc" >"$work/two.asc"
	for f in "$work/two.gpg" "$work/two.asc"; do
		aptrepo="file:$work/repo" aptkey="$f" aptkeyunpinned=yes
		run hostisan_apt_setup amd64 noble
		echo "$output"
		[ "$status" -ne 0 ]
		[[ "$output" == *"$TK_FPR"* && "$output" == *"$PINNED"* ]]
		[ ! -e "$work/keyring.gpg" ]
		[ ! -e "$work/apt/sources.list.d/hestia.list" ]
	done
}

@test "hestiacp lines leave sources.list too" {
	apt_env
	printf 'deb http://archive.ubuntu.com/ubuntu noble main\ndeb https://apt.hestiacp.com/ noble main\n' >"$work/apt/sources.list"
	aptrepo="file:$work/repo"
	hostisan_apt_setup amd64 noble
	[ "$(cat "$work/apt/sources.list")" = 'deb http://archive.ubuntu.com/ubuntu noble main' ]
}

@test "installers check every Hostisan package got installed" {
	local f
	for f in "$deb" "$ubu"; do
		[ "$(line_of "$f" '^\s*hostisan_check_installed hestia')" -gt "$(line_of "$f" 'for deb in "\$withdebs"/hestia_\*\.deb')" ]
		grep -qF 'sha256sum "$deb" >> $LOG' "$f"
		grep -qF 'dpkg -i "$deb" >> $LOG 2>&1' "$f"
		run ! grep -nE 'dpkg -i \$withdebs' "$f"
	done
	apt_env
	mkdir -p "$work/bin"
	printf '#!/bin/sh\n[ "$3" = hestia-php ] && exit 1\necho "install ok installed"\n' >"$work/bin/dpkg-query"
	chmod +x "$work/bin/dpkg-query"
	hostisan_check_installed hestia hestia-nginx
	run hostisan_check_installed hestia hestia-nginx hestia-php
	[ "$status" -ne 0 ]
	[[ "$output" == *hestia-php* ]]
}

# wrapper_copy: the wrapper in $work/src, with the helpers it sources.
wrapper_copy() {
	mkdir -p "$work/src/common/hostisan" "$work/bin"
	cp "$wrapper" "$work/src/hst-install.sh"
	cp "$repo/install/common/hostisan/os-support.sh" "$work/src/common/hostisan/"
}

@test "the wrapper runs the OS installer next to it, without a download" {
	wrapper_copy
	. /etc/os-release
	type=$ID
	printf '#!/bin/bash\necho "LOCAL $*"\n' >"$work/src/hst-install-$type.sh"
	printf '#!/bin/sh\n[ "$1" = -u ] && { echo 0; exit 0; }\nexec /usr/bin/id "$@"\n' >"$work/bin/id"
	printf '#!/bin/sh\necho "DOWNLOAD $*"; exit 1\n' >"$work/bin/wget"
	cp "$work/bin/wget" "$work/bin/curl"
	chmod +x "$work/bin/"*
	case "$type" in ubuntu | debian) ;; *) skip "not a Debian or Ubuntu host" ;; esac
	cd "$work"
	LANG=C.UTF-8 LC_ALL=C.UTF-8 PATH="$work/bin:$PATH" run bash src/hst-install.sh --with-debs /x
	[[ "$output" == *"LOCAL --with-debs /x"* ]]
	[[ "$output" != *DOWNLOAD* ]]
}

@test "the wrapper downloads nothing" {
	wrapper_copy
	printf '#!/bin/sh\n[ "$1" = -u ] && { echo 0; exit 0; }\nexec /usr/bin/id "$@"\n' >"$work/bin/id"
	printf '#!/bin/sh\necho "DOWNLOAD $*"; exit 0\n' >"$work/bin/wget"
	cp "$work/bin/wget" "$work/bin/curl"
	chmod +x "$work/bin/"*
	. /etc/os-release
	case "$ID" in ubuntu | debian) ;; *) skip "not a Debian or Ubuntu host" ;; esac
	cd "$work"
	LANG=C.UTF-8 LC_ALL=C.UTF-8 PATH="$work/bin:$PATH" run bash src/hst-install.sh --with-debs /x
	[ "$status" -ne 0 ]
	[[ "$output" != *DOWNLOAD* ]]
	[[ "$output" == *"install/hst-install"* ]]
}

# ---------------------------------------------------------------- supported systems

# Hostisan v1 ships for Ubuntu 24.04 only; the code for the other systems stays.
@test "only Ubuntu 24.04 is supported" {
	# shellcheck source=install/common/hostisan/os-support.sh
	source "$repo/install/common/hostisan/os-support.sh"
	run hostisan_os_check ubuntu 24.04 ''
	[ "$status" -eq 0 ]
	[ -z "$output" ]
	local os
	for os in "ubuntu 22.04" "ubuntu 26.04" "debian 12" "debian 13" "ubuntu 20.04" "centos 9"; do
		# shellcheck disable=SC2086
		run hostisan_os_check $os ''
		echo "$output"
		[ "$status" -eq 1 ]
		[[ "$output" == *"Hostisan v1 supports only Ubuntu 24.04"* ]]
		[[ "$output" == *"--force"* ]]
	done
	run hostisan_os_check debian 13 ''
	[[ "$output" == *"Debian 13"* ]]
	# --force installs anyway, saying it is untested
	run hostisan_os_check debian 13 yes
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"Debian 13"*"not supported"* ]]
	[[ "$output" == *"--force"*"testing only"* ]]
	# --force covers only the releases the installers have code for
	for os in "ubuntu 22.04" "ubuntu 26.04" "debian 12" "debian 13"; do
		# shellcheck disable=SC2086
		run hostisan_os_check $os yes
		[ "$status" -eq 0 ]
	done
	for os in "ubuntu 20.04" "ubuntu 24.10" "ubuntu 12" "debian 11" "debian 24.04" "centos 9" "ubuntu 2404"; do
		# shellcheck disable=SC2086
		run hostisan_os_check $os yes
		echo "$os: $output"
		[ "$status" -eq 1 ]
		[[ "$output" == *"not supported"* ]]
		[[ "$output" != *"Installing anyway"* ]]
	done
}

# os_installer_on OS RELEASE ARGS...: a copy of hst-install-OS.sh that stops
# right after its system check, run with lsb_release saying RELEASE, as root.
os_installer_on() {
	local os=$1 rel=$2
	shift 2
	. /etc/os-release
	[ "$ID" = "$os" ] || skip "not a $os host"
	[ ! -e /usr/local/hestia ] || skip "/usr/local/hestia exists"
	mkdir -p "$work/src/common/hostisan" "$work/bin" "$work/debs"
	cp "$repo"/install/common/hostisan/*.sh "$repo"/install/common/hostisan/product.env "$work/src/common/hostisan/"
	sed '/^hostisan_os_check "\$type" "\$release" "\$force" || exit 1$/a echo PASSED; exit 0' \
		"$repo/install/hst-install-$os.sh" >"$work/src/hst-install-$os.sh"
	grep -q '^echo PASSED; exit 0$' "$work/src/hst-install-$os.sh"
	touch "$work/debs/hestia_1.10.5-105+ubuntu24.04_amd64.deb" \
		"$work/debs/hestia-nginx_1.30.5-105+ubuntu24.04_amd64.deb" \
		"$work/debs/hestia-php_8.5.11-105+ubuntu24.04_amd64.deb"
	printf '#!/bin/sh\n[ "$1" = -u ] && { echo 0; exit 0; }\nexec /usr/bin/id "$@"\n' >"$work/bin/id"
	printf '#!/bin/sh\ncase "$*" in *-r*) echo "%s" ;; *) echo codename ;; esac\n' "$rel" >"$work/bin/lsb_release"
	printf '#!/bin/sh\n' >"$work/bin/clear"
	printf '#!/bin/sh\necho LANG=C\n' >"$work/bin/locale"
	chmod +x "$work/bin/"*
	LANG=C.UTF-8 LC_ALL=C.UTF-8 PATH="$work/bin:$PATH" run bash "$work/src/hst-install-$os.sh" -D "$work/debs" "$@"
}

@test "the Ubuntu installer run directly refuses an unknown release even with --force" {
	os_installer_on ubuntu 20.04 --force
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" != *PASSED* ]]
	[[ "$output" == *"Ubuntu 20.04"*"not supported"* ]]
	os_installer_on ubuntu 22.04 --force
	echo "$output"
	[[ "$output" == *PASSED* ]]
	os_installer_on ubuntu 22.04
	[ "$status" -ne 0 ]
	[[ "$output" != *PASSED* ]]
	os_installer_on ubuntu 24.04
	[[ "$output" == *PASSED* ]]
}

@test "the OS installers check the system with --force as the override" {
	local f n
	for f in "$deb" "$ubu"; do
		# loaded from the source tree, like product.sh; the installer stops without it
		grep -qE '^for hostisan_f in .*\bos-support\.sh\b' "$f"
		grep -qxF 'source "$HOSTISAN_SRC/common/hostisan/os-support.sh"' "$f"
		# after the options are read (-f sets force) and before anything is installed
		n=$(line_of "$f" '^hostisan_os_check "\$type" "\$release" "\$force" \|\| exit 1$')
		[ -n "$n" ]
		[ "$n" -gt "$(line_of "$f" '^\s+f\) force=.yes. ;;')" ]
		[ "$n" -lt "$(line_of "$f" '^hostisan_apt_setup ')" ]
		[ "$n" -lt "$(line_of "$f" '^# Welcome message')" ]
	done
	grep -qE '^  -f, --force .*Ubuntu 24\.04' "$ubu"
	grep -qE '^  -f, --force .*Ubuntu 24\.04' "$deb"
}

# wrapper_on RELEASE ARGS...: the wrapper on this Ubuntu host, with lsb_release
# saying RELEASE; the OS installer next to it only says it ran.
wrapper_on() {
	local rel=$1
	shift
	. /etc/os-release
	[ "$ID" = ubuntu ] || skip "not an Ubuntu host"
	wrapper_copy
	printf '#!/bin/bash\necho "LOCAL $*"\n' >"$work/src/hst-install-ubuntu.sh"
	printf '#!/bin/sh\n[ "$1" = -u ] && { echo 0; exit 0; }\nexec /usr/bin/id "$@"\n' >"$work/bin/id"
	printf '#!/bin/sh\necho "%s"\n' "$rel" >"$work/bin/lsb_release"
	chmod +x "$work/bin/"*
	cd "$work"
	LANG=C.UTF-8 LC_ALL=C.UTF-8 PATH="$work/bin:$PATH" run bash src/hst-install.sh "$@"
}

@test "the wrapper refuses other systems unless forced" {
	wrapper_on 22.04 --with-debs /x
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"Hostisan v1 supports only Ubuntu 24.04"* ]]
	[[ "$output" != *LOCAL* ]]
	wrapper_on 26.04 --with-debs /x
	[ "$status" -ne 0 ]
	[[ "$output" != *LOCAL* ]]
	wrapper_on 22.04 --with-debs /x --force
	echo "$output"
	[[ "$output" == *"LOCAL --with-debs /x --force"* ]]
	[[ "$output" == *"testing only"* ]]
	wrapper_on 22.04 -f --with-debs /x
	[[ "$output" == *"LOCAL -f --with-debs /x"* ]]
	# a release the installers have no code for stays refused
	wrapper_on 20.04 --force
	[ "$status" -ne 0 ]
	[[ "$output" != *LOCAL* ]]
	[[ "$output" == *"Ubuntu 24.04"* ]]
	wrapper_on 24.04 --with-debs /x
	[[ "$output" == *"LOCAL --with-debs /x"* ]]
	[[ "$output" != *"supports only"* ]]
}

@test "the wrapper checks the system before it changes the locale" {
	# rc10 N3: a refused minimal 22.04 printed locale-gen errors above the refusal
	mkdir -p "$work/bin"
	printf '#!/bin/sh\necho LANG=C\n' >"$work/bin/locale"
	printf '#!/bin/sh\necho LOCALEGEN; exit 1\n' >"$work/bin/locale-gen"
	chmod +x "$work/bin/locale" "$work/bin/locale-gen"
	wrapper_on 22.04 --with-debs /x
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"Hostisan v1 supports only Ubuntu 24.04"* ]]
	[[ "$output" != *LOCALEGEN* ]]
	[[ "$output" != *"UTF-8 locale"* ]]
	# on Ubuntu the OS installer sets the locale itself, after its preflight
	# (preflight: the locale changes only after the checks pass)
	wrapper_on 24.04 --with-debs /x
	[[ "$output" != *LOCALEGEN* ]]
	[[ "$output" == *"LOCAL --with-debs /x"* ]]
}

# help_tree: the wrapper with the real Ubuntu installer and its helpers, and
# lsb_release saying 22.04 (help works on any system).
help_tree() {
	. /etc/os-release
	[ "$ID" = ubuntu ] || skip "not an Ubuntu host"
	wrapper_copy
	cp "$ubu" "$work/src/"
	cp "$repo"/install/common/hostisan/*.sh "$repo"/install/common/hostisan/product.env "$work/src/common/hostisan/"
	printf '#!/bin/sh\necho 22.04\n' >"$work/bin/lsb_release"
	printf '#!/bin/sh\necho LOCALEGEN; exit 1\n' >"$work/bin/locale-gen"
	printf '#!/bin/sh\necho LANG=C\n' >"$work/bin/locale"
	chmod +x "$work/bin/"*
	cd "$work"
}

@test "--help names the wrapper, exits 0, and needs neither root nor a supported system" {
	help_tree
	local h
	for h in --help -h; do
		PATH="$work/bin:$PATH" run bash src/hst-install.sh "$h"
		echo "$output"
		[ "$status" -eq 0 ]
		[[ "$output" == *"Usage: bash src/hst-install.sh [OPTIONS]"* ]]
		[[ "$output" != *"hst-install-ubuntu.sh"* ]]
		[[ "$output" != *"supports only"* ]]
		[[ "$output" != *LOCALEGEN* ]]
		[[ "$output" != *"Error"* ]]
	done
	# the example passes no password on the command line
	[[ "$output" == *"Example: bash src/hst-install.sh --with-debs"* ]]
	run ! grep -E 'Example:.*(-p |--password )' <<<"$output"
	# the OS installer run directly names itself; a bad option is an error
	PATH="$work/bin:$PATH" run bash src/hst-install-ubuntu.sh --help
	[ "$status" -eq 0 ]
	[[ "$output" == *"Usage: bash src/hst-install-ubuntu.sh [OPTIONS]"* ]]
	PATH="$work/bin:$PATH" run bash src/hst-install-ubuntu.sh --bogus
	[ "$status" -eq 1 ]
	[[ "$output" == *"Usage:"* ]]
}

# ---------------------------------------------------------------- preflight

# preflight_tree: a copy of the Ubuntu installer that stops (REACHED) where it
# would configure apt, right after the preflight and the questions, with
# fake system tools that log to $work/calls. STUB_MEM (kB), STUB_DISK (kB
# free), STUB_PKGS (installed packages), STUB_DNS (the hostname's A records)
# and STUB_BUSY (a hex port something listens on) describe the server.
preflight_tree() {
	. /etc/os-release
	[ "$ID" = ubuntu ] || skip "not an Ubuntu host"
	[ ! -e /usr/local/hestia ] || skip "/usr/local/hestia exists"
	mkdir -p "$work/src/common/hostisan" "$work/bin" "$work/debs" "$work/logs" "$work/net"
	cp "$repo"/install/common/hostisan/*.sh "$repo"/install/common/hostisan/product.env "$work/src/common/hostisan/"
	# $work/netplan stands in for /etc/netplan: one config file, unless the
	# test empties it
	sed -e '/^# Configure apt to retry downloading on error$/i echo REACHED; exit 0' \
		-e "s#/etc/netplan#$work/netplan#g" "$ubu" >"$work/src/hst-install-ubuntu.sh"
	grep -q '^echo REACHED; exit 0$' "$work/src/hst-install-ubuntu.sh"
	mkdir -p "$work/netplan"
	touch "$work/netplan/50-cloud-init.yaml"
	touch "$work/debs/hestia_1.10.5-105+ubuntu24.04_amd64.deb" \
		"$work/debs/hestia-nginx_1.30.5-105+ubuntu24.04_amd64.deb" \
		"$work/debs/hestia-php_8.5.11-105+ubuntu24.04_amd64.deb"
	printf '#!/bin/sh\n[ "$1" = -u ] && { echo 0; exit 0; }\nexec /usr/bin/id "$@"\n' >"$work/bin/id"
	printf '#!/bin/sh\ncase "$*" in *-r*) echo 24.04 ;; *) echo noble ;; esac\n' >"$work/bin/lsb_release"
	printf '#!/bin/sh\n' >"$work/bin/clear"
	printf '#!/bin/sh\necho LANG=C\n' >"$work/bin/locale"
	local t
	for t in apt-get dpkg ss locale-gen update-locale; do
		printf '#!/bin/sh\necho "%s $*" >>"%s/calls"\n' "$t" "$work" >"$work/bin/$t"
	done
	printf '#!/bin/sh\nprintf "%%b" "$STUB_PKGS" | sed "s/^/ii  /"\n' >"$work/bin/dpkg-query"
	printf '#!/bin/sh\nprintf "%%b" "$STUB_DNS"\n' >"$work/bin/dig"
	printf '#!/bin/sh\ncase "$1" in -I) echo "203.0.113.5 " ;; -f) echo box ;; *) echo box ;; esac\n' >"$work/bin/hostname"
	printf '#!/bin/sh\necho "Filesystem 1024-blocks Used Available Capacity Mounted on"\necho "/dev/x 99 1 $STUB_DISK 1%% /"\n' >"$work/bin/df"
	chmod +x "$work/bin/"*
	printf 'MemTotal: %s kB\n' "${STUB_MEM:-4194304}" >"$work/meminfo"
	printf '  sl  local_address rem_address   st\n' >"$work/net/tcp6"
	printf '  sl  local_address rem_address   st\n' >"$work/net/tcp"
	if [ -n "${STUB_BUSY:-}" ]; then
		printf '   0: 00000000:%s 00000000:0000 0A 0\n' "$STUB_BUSY" >>"$work/net/tcp"
	fi
	: >"$work/calls"
}

# preflight_run [ARGS...]: run that copy as root, with --with-debs and ARGS,
# reading answers from $work/answers (empty: none).
preflight_run() {
	touch "$work/answers"
	HOSTISAN_INSTALL_TEST=1 HOSTISAN_LOG_DIR="$work/logs" HOSTISAN_MEMINFO="$work/meminfo" \
		HOSTISAN_PROC_NET="$work/net" STUB_DISK="${STUB_DISK:-52428800}" STUB_PKGS="${STUB_PKGS:-}" \
		STUB_DNS="${STUB_DNS:-}" LANG=C.UTF-8 LC_ALL=C.UTF-8 PATH="$work/bin:$PATH" \
		run bash "$work/src/hst-install-ubuntu.sh" -D "$work/debs" "$@" <"$work/answers"
}

@test "preflight: every problem is reported together before anything is installed" {
	STUB_MEM=1500000 STUB_BUSY=0050 preflight_tree
	STUB_DISK=4000000 STUB_PKGS='ufw\napache2-bin\n' preflight_run --interactive no \
		--hostname example.com --email not-an-email
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" != *REACHED* ]]
	[[ "$output" == *"[ x ] --hostname example.com"*"panel.example.com"* ]]
	[[ "$output" == *"[ x ] --email not-an-email"* ]]
	[[ "$output" == *"[ x ] Only 3.8 GB free on /"* ]]
	[[ "$output" == *"[ x ] apache2 is already installed"* ]]
	[[ "$output" == *"[ ! ] port 80 (web) is already in use"*"apache2"* ]]
	[[ "$output" == *"[ ! ] This server has 1.4 GB of RAM"*"ClamAV is"*"off by default"* ]]
	[[ "$output" == *"4 problems stop the install; nothing was installed or changed"* ]]
	# nothing installed, removed or written
	run ! grep -E '^(apt-get|dpkg) ' "$work/calls"
	[ -z "$(ls -A "$work/logs")" ]
}

@test "preflight: ufw stays until the firewall is set up, and nobody is asked" {
	preflight_tree
	STUB_PKGS='ufw\nopenssh-server\n' STUB_DNS='203.0.113.5\n' preflight_run --interactive no \
		--hostname panel.example.com --email you@example.com
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *REACHED* ]]
	[ "$(grep -c 'ufw' <<<"$output")" -eq 1 ] # the preflight note only
	[[ "$output" == *"ufw is installed: it will be removed just before Hostisan sets up"* ]]
	[[ "$output" != *"Would you like"* ]]
	[[ "$output" == *"panel.example.com resolves to this server"* ]]
	run ! grep -q purge "$work/calls"
	# the admin defaults to "admin" without a prompt
	[[ "$output" != *"enter administrator username"* ]]
}

@test "preflight: an interactive install is not asked about ufw (it stopped the install)" {
	preflight_tree
	printf 'y\n' >"$work/answers"
	STUB_PKGS='ufw\n' preflight_run --interactive yes --username admin \
		--hostname panel.example.com --email you@example.com
	echo "$output"
	[[ "$output" == *REACHED* ]]
	[[ "$output" != *"remove the conflicting packages"* ]]
	[[ "$output" != *"clean server"* ]]
	run ! grep -q purge "$work/calls"
	# --force too
	preflight_tree
	printf 'y\n' >"$work/answers"
	STUB_PKGS='ufw\n' preflight_run --interactive yes --force --username admin \
		--hostname panel.example.com --email you@example.com
	[[ "$output" == *REACHED* ]]
	[[ "$output" != *"remove the conflicting packages"* ]]
	run ! grep -q purge "$work/calls"
}

@test "preflight: the packages to purge are not glob-expanded" {
	preflight_tree
	# a file in the current dir that apache2* would match
	mkdir -p "$work/cwd"
	touch "$work/cwd/apache2-not-a-package" "$work/cwd/nginx.conf"
	printf 'y\ny\n' >"$work/answers"
	cd "$work/cwd"
	STUB_PKGS='apache2-bin\nnginx\n' preflight_run --interactive yes --username admin \
		--hostname panel.example.com --email you@example.com
	echo "$output"
	cat "$work/calls"
	[[ "$output" == *REACHED* ]]
	grep -qxF 'apt-get -qq purge apache2* nginx* -y' "$work/calls"
}

@test "preflight: --iptables no keeps ufw, the server's only firewall" {
	preflight_tree
	STUB_PKGS='ufw\n' preflight_run --interactive no --iptables no \
		--hostname panel.example.com --email you@example.com
	echo "$output"
	[[ "$output" == *REACHED* ]]
	[[ "$output" == *"ufw is installed and stays (--iptables no)"* ]]
	[[ "$output" != *"removed"* ]]
	run ! grep -q purge "$work/calls"
}

# firewall_block: the installer's "Configuring firewall" block, on its own.
firewall_block() {
	sed -n '/^# Configuring firewall/,/^fi$/p' "$ubu"
}

@test "installer: ufw is purged right before v-update-firewall, only with iptables" {
	mkdir -p "$work/bin" "$work/hestia/bin"
	firewall_block >"$work/fw.sh"
	grep -q 'v-update-firewall' "$work/fw.sh"
	printf '#!/bin/sh\necho "apt-get $*" >>"%s/calls"\n' "$work" >"$work/bin/apt-get"
	printf '#!/bin/sh\necho v-update-firewall >>"%s/calls"\n' "$work" >"$work/hestia/bin/v-update-firewall"
	chmod +x "$work/bin/apt-get" "$work/hestia/bin/v-update-firewall"
	local fw="check_result() { [ \"\$1\" -eq 0 ] || { echo \"\$2\"; exit 1; }; }; HESTIA=$work/hestia LOG=$work/log; . $work/fw.sh"
	# ufw found by the preflight, iptables yes: purged, then the firewall
	: >"$work/calls"
	PATH="$work/bin:$PATH" run bash -c "iptables=yes hostisan_purge_ufw=yes; $fw"
	echo "$output"
	[ "$status" -eq 0 ]
	[ "$(cat "$work/calls")" = "$(printf 'apt-get -qq purge ufw -y\nv-update-firewall')" ]
	[ "$output" = "[ * ] Removing ufw: Hostisan manages the firewall with iptables itself." ]
	# no ufw: the firewall only
	: >"$work/calls"
	PATH="$work/bin:$PATH" run bash -c "iptables=yes hostisan_purge_ufw=; $fw"
	[ "$(cat "$work/calls")" = v-update-firewall ]
	# --iptables no: neither
	: >"$work/calls"
	PATH="$work/bin:$PATH" run bash -c "iptables=no hostisan_purge_ufw=; $fw"
	[ ! -s "$work/calls" ]
	# and nothing else in the installer removes ufw
	[ "$(grep -c '^[[:space:]]*apt-get .*purge ufw' "$ubu")" -eq 1 ]
	run ! grep -n 'hostisan_purge_now' "$ubu"
}

@test "preflight: the locale changes only after the checks pass" {
	preflight_tree
	STUB_DISK=4000000 preflight_run --interactive no --hostname panel.example.com --email you@example.com
	[ "$status" -eq 1 ]
	[[ "$output" == *"nothing was installed or changed"* ]]
	[ ! -s "$work/calls" ]
	preflight_run --interactive no --hostname panel.example.com --email you@example.com
	echo "$output"
	[[ "$output" == *REACHED* ]]
	grep -qx 'locale-gen C.UTF-8' "$work/calls"
	grep -qx 'update-locale LANG=C.UTF-8' "$work/calls"
}

@test "preflight: the netplan question comes before the confirmation" {
	preflight_tree
	rm -f "$work/netplan"/*
	printf 'n\n' >"$work/answers"
	preflight_run --interactive yes --username admin --hostname panel.example.com --email you@example.com
	echo "$output"
	[ "$status" -eq 1 ]
	# (read -p prints no prompt without a terminal) "n" answered the netplan
	# question: the confirmation would have said Goodbye
	[[ "$output" == *"Unable to detect netplan configuration."* ]]
	[[ "$output" != *Goodbye* ]]
	[ ! -s "$work/calls" ]
	[ -z "$(ls -A "$work/logs")" ]
	# yes to both
	printf 'y\ny\n' >"$work/answers"
	preflight_run --interactive yes --username admin --hostname panel.example.com --email you@example.com
	[[ "$output" == *REACHED* ]]
}

# password_tree: preflight_tree, but the copy runs on past the preflight to
# where the admin password is generated and saved, and stops (REACHED) at the
# sudo guard. The apt retries file and /etc/hosts are in $work. apt-get
# writes its arguments to the install log too, as the real one would.
password_tree() {
	preflight_tree
	sed -e '/^hostisan_sudo_guard "\$username"$/i echo REACHED; exit 0' \
		-e "s#/etc/netplan#$work/netplan#g" \
		-e "s#/etc/apt/apt.conf.d/80-retries#$work/80-retries#g" \
		-e "s#/etc/hosts#$work/hosts#g" "$ubu" >"$work/src/hst-install-ubuntu.sh"
	grep -q '^echo REACHED; exit 0$' "$work/src/hst-install-ubuntu.sh"
	run ! grep -nE '(/etc/hosts|80-retries)' <(sed -n '1,/^echo REACHED; exit 0$/p' "$work/src/hst-install-ubuntu.sh" | grep "/etc/")
	touch "$work/hosts"
	printf '#!/bin/sh\necho "apt-get $*" >>"%s/calls"\necho "apt-get $*"\n' "$work" >"$work/bin/apt-get"
	chmod +x "$work/bin/apt-get"
}

@test "installer: the admin password never reaches the install log" {
	password_tree
	preflight_run --interactive no --hostname panel.example.com --email you@example.com
	echo "$output"
	[[ "$output" == *REACHED* ]]
	local pwfile=$work/logs/hostisan-admin-password.txt pw log
	pw=$(sed -n 's/^Password: //p' "$pwfile")
	[[ $pw =~ ^[A-Za-z0-9]{20}$ ]]
	log=$(ls "$work"/logs/hst_install-*.log)
	[ -s "$log" ] # the log was written to
	# -1: grep ran and found nothing (2 would be an error)
	run -1 grep -rF --exclude=hostisan-admin-password.txt -e "$pw" "$work/logs"
	run -1 grep -F -e "$pw" "$work/calls"
	[[ "$output" != *"$pw"* ]]
	# nor does a given one
	password_tree
	rm -rf "$work/logs"/*
	preflight_run --interactive no --hostname panel.example.com --email you@example.com \
		--password 'Given-pass-123'
	[[ "$output" == *REACHED* ]]
	run -1 grep -rF -e 'Given-pass-123' "$work/logs"
	[ ! -e "$work/logs/hostisan-admin-password.txt" ]
}

@test "preflight: the installer runs it after the system check and before apt" {
	local n
	n=$(line_of "$ubu" '^hostisan_preflight \|\| exit 1$')
	[ -n "$n" ]
	[ "$n" -gt "$(line_of "$ubu" '^hostisan_os_check ')" ]
	[ "$n" -lt "$(line_of "$ubu" '^# Asking for confirmation to proceed')" ]
	[ "$n" -lt "$(line_of "$ubu" '^apt-get -qq update')" ]
	[ "$n" -lt "$(line_of "$ubu" '^apt-get -y install \$installer_dependencies')" ]
	[ "$n" -lt "$(line_of "$ubu" '80-retries')" ]
	[ "$n" -lt "$(line_of "$ubu" '^mkdir -p "\$hst_backups"')" ]
	[ "$n" -lt "$(line_of "$ubu" '^hostisan_sudo_guard "\$username"$')" ]
	# the netplan question comes before the confirmation, so before any
	# change; the locale after it, before the backup dir and any purge
	local netplan confirm locale
	netplan=$(line_of "$ubu" '^# Check network configuration')
	confirm=$(line_of "$ubu" '^# Asking for confirmation to proceed')
	locale=$(line_of "$ubu" '^ensure_utf8_locale$')
	[ "$netplan" -gt "$n" ]
	[ "$netplan" -lt "$confirm" ]
	[ "$locale" -gt "$confirm" ]
	[ "$locale" -lt "$(line_of "$ubu" '^mkdir -p "\$hst_backups"')" ]
	[ "$locale" -lt "$(line_of "$ubu" 'apt-get -qq purge')" ]
	# no old substring match on dpkg --get-selections
	run ! grep -n 'dpkg --get-selections' "$ubu"
}

@test "the docs say Ubuntu 24.04 only" {
	grep -qF 'Ubuntu 24.04' "$repo/README.md"
	run ! grep -nE '^- \*\*Debian:\*\*' "$repo/README.md"
	grep -qF 'Ubuntu 24.04' "$repo/RELEASING.md"
	grep -qE 'Ubuntu 24\.04 LTS only|only Ubuntu 24\.04' "$wrapper"
	# the wrapper finds -f/--force only as an argument of its own (not in -yf)
	grep -qE -- '`-f`.*`--force`.*own argument' "$repo/README.md"
}

# ---------------------------------------------------------------- no Node, no old terminal

@test "installers install no Node and no Node terminal port" {
	local f
	for f in "$deb" "$ubu"; do
		run ! grep -nE 'nodejs|nodesource|node_v|WEB_TERMINAL_PORT|8085' <(code "$f")
		# The web terminal option still installs the (unit-only) terminal package.
		grep -qE '^  .*hestia-web-terminal' "$f"
		grep -qF '"$withdebs/hestia-web-terminal_*.deb"' "$f"
	done
}

@test "installers keep no upstream hidepid=2 block" {
	local f
	for f in "$deb" "$ubu"; do
		run ! grep -nE 'hidepid=2|hestia-proc' "$f"
	done
}

# ---------------------------------------------------------------- Hostisan setup

@test "installers run setup.sh after sudoers and before hestia starts" {
	local f sudo conf setup start
	for f in "$deb" "$ubu"; do
		sudo=$(line_of "$f" '^\$HESTIA_COMMON_DIR/hostisan/sudoers\.sh$')
		conf=$(line_of "$f" '^write_config_value "VERSION"')
		setup=$(line_of "$f" '^\$HESTIA_COMMON_DIR/hostisan/setup\.sh')
		start=$(line_of "$f" '^systemctl start hestia$')
		[ -n "$sudo" ] && [ -n "$conf" ] && [ -n "$setup" ] && [ -n "$start" ]
		[ "$sudo" -lt "$setup" ]
		[ "$conf" -lt "$setup" ]
		[ "$setup" -lt "$start" ]
		# A fresh install: never --upgrade (that re-installs phpMyAdmin SSO).
		run ! grep -nE 'hostisan/setup\.sh.*--upgrade' "$f"
		# The terminal unit is enabled after setup.sh made the panel's socket dir user.
		[ "$(line_of "$f" '^\s*systemctl enable hestia-web-terminal')" -gt "$sudo" ]
	done
}

@test "installers leave HOSTISAN_FILE_RESTORE to hostisan.env (on)" {
	run ! grep -n 'HOSTISAN_FILE_RESTORE' <(code "$deb") <(code "$ubu")
	grep -qx 'HOSTISAN_FILE_RESTORE=on' "$repo/install/common/hostisan/hostisan.env"
}

@test "installers install the hestiaweb sudoers file with sudoers.sh" {
	local f
	for f in "$deb" "$ubu"; do
		# one implementation (temp file, sudo-rs check, visudo -c -f, 0440),
		# shared with setup.sh, which runs it on every upgrade; its own tests
		# are in package.bats (sudo-rs included)
		grep -qx '\$HESTIA_COMMON_DIR/hostisan/sudoers.sh' "$f"
		grep -qx 'check_result \$? "could not install /etc/sudoers.d/hestiaweb"' "$f"
		run ! grep -nE 'cp .*sudo/hestiaweb|sed .*(requiretty|log_allowed)' <(code "$f")
	done
	grep -qF 'visudo -c -q -f "$tmp"' "$repo/install/common/hostisan/sudoers.sh"
}

# ---------------------------------------------------------------- branding

@test "installers say Hostisan" {
	local f
	for f in "$deb" "$ubu" "$wrapper"; do
		# Technical identifiers that may stay: the public IP lookup service
		# (as bin/v-update-sys-ip) and proftpd's AppArmor marker.
		run ! grep -nE 'HestiaCP|Hestia|hestiacp\.com' <(code "$f" |
			grep -vE 'https://ip\.hestiacp\.com/|# Configuration added by Hestia')
		grep -q 'Hostisan' "$f"
	done
	# the site and docs links come from product.env (product.bats); the Ubuntu
	# installer's summary is in install-ui.sh (installer-ux)
	grep -qF '$HOSTISAN_URL' "$repo/install/common/hostisan/install-ui.sh"
	grep -qF '$HOSTISAN_DOCS_URL' "$ubu"
	grep -qF '$HOSTISAN_URL' "$deb"
}

@test "installers have no old PHP panel or legacy terminal option" {
	local f
	for f in "$deb" "$ubu"; do
		run ! grep -nE '_shell|web-terminal/server\.js|oldpanel|old-panel|legacy-panel' "$f"
	done
}

# ---------------------------------------------------------------- sudo guard

@test "sudo guard is the same in both installers" {
	guard_block "$deb" >"$work/a"
	guard_block "$ubu" >"$work/b"
	[ -s "$work/a" ]
	diff "$work/a" "$work/b"
}

@test "installers run the sudo guard before creating the admin" {
	local f early add after del
	for f in "$deb" "$ubu"; do
		early=$(line_of "$f" '^hostisan_sudo_guard "\$username"$')
		del=$(line_of "$f" '^\s*groupdel "\$username"')
		add=$(line_of "$f" '^(\$HESTIA/bin/v-add-user "?\$username|hostisan_add_admin )')
		after=$(line_of "$f" '^hostisan_sudo_guard "\$username" "\$\(id -g "\$username"\)" "\$\(id -u "\$username"\)"')
		[ -n "$early" ] && [ -n "$del" ] && [ -n "$add" ] && [ -n "$after" ]
		# The early run is before any package is installed.
		[ "$early" -lt "$(line_of "$f" '^apt-get -y install \$software')" ]
		[ "$del" -lt "$add" ]
		[ "$add" -lt "$after" ]
	done
}

@test "sudo guard disables a %admin rule the admin group would inherit" {
	guard_env
	printf '# Members of the admin group may gain root privileges\n%%admin ALL=(ALL) ALL\n' >>"$work/sudoers"
	cp "$work/sudoers" "$work/orig"
	command -v visudo >/dev/null || stub_visudo 0
	run hostisan_sudo_guard admin
	[ "$status" -eq 0 ]
	[[ "$output" == *"%admin"* ]]
	# Commented out, not deleted; everything else untouched.
	run grep -nE '^[[:space:]]*%admin' "$work/sudoers"
	[ "$status" -eq 1 ]
	grep -qE '^# Hostisan: .*%admin ALL=\(ALL\) ALL$' "$work/sudoers"
	grep -qx '%sudo ALL=(ALL:ALL) ALL' "$work/sudoers"
	grep -qx 'root ALL=(ALL:ALL) ALL' "$work/sudoers"
	# Backed up first.
	cmp "$work/orig" "$work"/backup/*/sudoers
	if command -v visudo >/dev/null && [ ! -e "$work/visudo.calls" ]; then
		visudo -c -q -f "$work/sudoers"
	fi
}

@test "sudo guard leaves an image without such rules alone" {
	guard_env
	cp "$work/sudoers" "$work/orig"
	stub_visudo 0
	run hostisan_sudo_guard admin
	[ "$status" -eq 0 ]
	cmp "$work/orig" "$work/sudoers"
	[ -z "$(ls -A "$work/backup")" ]
}

@test "sudo guard finds user, uid, gid and sudoers.d rules" {
	guard_env
	stub_visudo 0
	printf 'boss ALL=(ALL) NOPASSWD: ALL\n' >"$work/sudoers.d/90-cloud"
	printf '%%#1001 ALL=(ALL) ALL\n#1001 ALL=(ALL) ALL\n%%wheel ALL=(ALL) ALL\n' >"$work/sudoers.d/91-ids"
	printf '%%boss ALL=(ALL) ALL\n' >"$work/sudoers.d/README.txt"
	run hostisan_sudo_guard boss 1001 1001
	[ "$status" -eq 0 ]
	grep -q '^# Hostisan: .*boss ALL=(ALL) NOPASSWD: ALL' "$work/sudoers.d/90-cloud"
	grep -q '^# Hostisan: .*%#1001 ALL' "$work/sudoers.d/91-ids"
	grep -qx '# Hostisan: disabled, gave the panel admin root sudo: #1001 ALL=(ALL) ALL' "$work/sudoers.d/91-ids"
	grep -qx '%wheel ALL=(ALL) ALL' "$work/sudoers.d/91-ids"
	# sudo never reads a sudoers.d file with a dot in its name.
	grep -qx '%boss ALL=(ALL) ALL' "$work/sudoers.d/README.txt"
	compgen -G "$work/backup/*/sudoers.d/90-cloud" >/dev/null
}

@test "sudo guard matches whole names in a user list" {
	guard_env
	stub_visudo 0
	printf '%%admins ALL=(ALL) ALL\nadministrator ALL=(ALL) ALL\nbob, %%admin ALL = (ALL) ALL\n' >>"$work/sudoers"
	run hostisan_sudo_guard admin
	[ "$status" -eq 0 ]
	grep -qx '%admins ALL=(ALL) ALL' "$work/sudoers"
	grep -qx 'administrator ALL=(ALL) ALL' "$work/sudoers"
	grep -q '^# Hostisan: .*bob, %admin ALL = (ALL) ALL' "$work/sudoers"
}

@test "sudo guard refuses rules it cannot disable safely" {
	guard_env
	stub_visudo 0
	printf 'User_Alias ADMINS = %%admin, bob\nADMINS ALL=(ALL) ALL\n' >>"$work/sudoers"
	cp "$work/sudoers" "$work/orig"
	run hostisan_sudo_guard admin
	[ "$status" -ne 0 ]
	[[ "$output" == *"User_Alias ADMINS"* ]]
	cmp "$work/orig" "$work/sudoers"

	guard_env
	printf '%%admin ALL=(ALL) \\\n    ALL\n' >>"$work/sudoers"
	cp "$work/sudoers" "$work/orig"
	run hostisan_sudo_guard admin
	[ "$status" -ne 0 ]
	cmp "$work/orig" "$work/sudoers"
}

@test "sudo guard keeps each run's backup" {
	guard_env
	stub_visudo 0
	printf '%%admin ALL=(ALL) ALL\n' >>"$work/sudoers"
	cp "$work/sudoers" "$work/orig"
	hostisan_sudo_guard admin
	printf '#1001 ALL=(ALL) ALL\n' >>"$work/sudoers"
	hostisan_sudo_guard admin 1001 1001
	[ "$(ls "$work/backup" | wc -l)" -eq 2 ]
	# One of them is the untouched original.
	local b found=0
	for b in "$work"/backup/*/sudoers; do
		cmp -s "$work/orig" "$b" && found=1
	done
	[ "$found" -eq 1 ]
	run grep -nE '^(%admin|#1001) ' "$work/sudoers"
	[ "$status" -eq 1 ]
}

@test "sudo guard restores the files when visudo rejects the result" {
	guard_env
	printf '%%admin ALL=(ALL) ALL\n' >>"$work/sudoers"
	cp "$work/sudoers" "$work/orig"
	# visudo passes the original and fails the edited file.
	mkdir -p "$work/bin"
	cat >"$work/bin/visudo" <<-EOF
		#!/bin/sh
		for a; do f=\$a; done
		grep -q '^# Hostisan' "\$f" && exit 1
		exit 0
	EOF
	chmod +x "$work/bin/visudo"
	PATH="$work/bin:$PATH" run hostisan_sudo_guard admin
	[ "$status" -ne 0 ]
	[[ "$output" == *"refus"* || "$output" == *"by hand"* ]]
	cmp "$work/orig" "$work/sudoers"
}

@test "test overrides need the test flag" {
	guard_env
	unset HOSTISAN_INSTALL_TEST
	[ "$(hostisan_sudoers_files | head -n1)" = /etc/sudoers ]
	export HOSTISAN_INSTALL_TEST=1
	[ "$(hostisan_sudoers_files | head -n1)" = "$work/sudoers" ]
}

@test "sudo guard refuses rules for every user or every panel user" {
	local rule
	for rule in 'ALL ALL=(ALL) ALL' '%hestia-users ALL=(ALL) NOPASSWD: ALL' 'bob, ALL ALL=(root) /bin/sh'; do
		guard_env
		stub_visudo 0
		printf '%s\n' "$rule" >>"$work/sudoers"
		cp "$work/sudoers" "$work/orig"
		run hostisan_sudo_guard admin
		[ "$status" -ne 0 ]
		[[ "$output" == *"$rule"* ]]
		cmp "$work/orig" "$work/sudoers"
	done
}

@test "sudo guard refuses includes outside sudoers.d" {
	local inc
	guard_env
	stub_visudo 0
	printf '@includedir %s\n#includedir %s\n' "$work/sudoers.d" "$work/sudoers.d" >>"$work/sudoers"
	hostisan_sudo_guard admin
	for inc in '@includedir /etc/sudoers.local' '#includedir /etc/sudoers.local' '@include /etc/sudoers.extra' '#include /etc/sudoers.extra'; do
		guard_env
		stub_visudo 0
		printf '%s\n' "$inc" >>"$work/sudoers"
		run hostisan_sudo_guard admin
		[ "$status" -ne 0 ]
		[[ "$output" == *"$inc"* ]]
	done
	guard_env
	stub_visudo 0
	printf '@include /etc/other\n' >"$work/sudoers.d/50-inc"
	run hostisan_sudo_guard admin
	[ "$status" -ne 0 ]
}

@test "sudo guard refuses to edit a file visudo already rejects" {
	guard_env
	stub_visudo 1
	printf '%%admin ALL=(ALL) ALL\n' >>"$work/sudoers"
	cp "$work/sudoers" "$work/orig"
	run hostisan_sudo_guard admin
	[ "$status" -ne 0 ]
	[[ "$output" == *visudo* ]]
	cmp "$work/orig" "$work/sudoers"
}

@test "sudo itself must say the admin has no sudo" {
	guard_env
	mkdir -p "$work/bin"
	printf '#!/bin/sh\necho "sudo $*" >>"%s/sudo.calls"\necho "User admin is not allowed to run sudo on h."\n' "$work" >"$work/bin/sudo"
	chmod +x "$work/bin/sudo"
	PATH="$work/bin:$PATH" hostisan_verify_no_sudo admin
	grep -qx 'sudo -n -l -U admin' "$work/sudo.calls"
	printf '#!/bin/sh\nprintf "User admin may run the following commands on h:\\n    (ALL) ALL\\n"\n' >"$work/bin/sudo"
	PATH="$work/bin:$PATH" run hostisan_verify_no_sudo admin
	[ "$status" -ne 0 ]
	[[ "$output" == *"(ALL) ALL"* ]]
	local f
	for f in "$deb" "$ubu"; do
		[ "$(line_of "$f" '^hostisan_verify_no_sudo "\$username"$')" -gt "$(line_of "$f" '^\$HESTIA/bin/v-change-user-role "?\$username"? admin')" ]
		grep -A1 '^hostisan_verify_no_sudo "\$username"$' "$f" | grep -q '^check_result \$? '
	done
}

@test "an empty unused admin group may be replaced" {
	guard_env
	printf 'admin:x:110:\nstaff:x:50:bob\nbob:x:1000:\nusers:x:100:\n' >>"$work/group"
	printf 'bob:x:1000:1000::/home/bob:/bin/bash\ncarl:x:1001:100::/home/carl:/bin/bash\n' >>"$work/passwd"
	hostisan_unused_group admin
	# Only a regular group (gid >= 1000) or one called admin is ever deleted.
	printf 'shadow:x:42:\nsudo2:x:27:\nold:x:1005:\n' >>"$work/group"
	hostisan_unused_group old
	run hostisan_unused_group shadow
	[ "$status" -ne 0 ]
	run hostisan_unused_group sudo2
	[ "$status" -ne 0 ]
	# Members, a user of that name, or a user's primary group: in use.
	run hostisan_unused_group staff
	[ "$status" -ne 0 ]
	run hostisan_unused_group bob
	[ "$status" -ne 0 ]
	run hostisan_unused_group users
	[ "$status" -ne 0 ]
	run hostisan_unused_group nosuch
	[ "$status" -ne 0 ]
}

@test "installers accept an empty admin group and delete it" {
	local f
	for f in "$deb" "$ubu"; do
		awk '/^validate_username\(\)/ { on = 1 } on { print } on && /^}/ { exit }' "$f" >"$work/v.sh"
		grep -q 'hostisan_unused_group "\$username"' "$work/v.sh"
		grep -qE '^\s*if hostisan_unused_group "\$username"; then' "$f"
	done
	# The wrapper leaves the admin group to the OS installer.
	run ! grep -n '\^admin: /etc/group' "$wrapper"
}

# ---------------------------------------------------------------- real sudo

# docker_guard IMAGE SUDOERS_LINE: run the guard as root with the image's real
# sudo/visudo after appending SUDOERS_LINE (may be empty) to /etc/sudoers.
docker_guard() {
	have_docker || skip "docker not available"
	guard_block "$ubu" >"$work/guard.sh"
	docker run --rm -v "$work/guard.sh:/guard.sh:ro" "$1" bash -c '
		set -e
		apt-get -qq update >/dev/null 2>&1
		DEBIAN_FRONTEND=noninteractive apt-get -qq install -y sudo >/dev/null 2>&1
		[ -n "$1" ] && printf "%s\n" "$1" >>/etc/sudoers
		getent group admin >/dev/null || groupadd -r admin
		export HOSTISAN_INSTALL_TEST=1 HOSTISAN_SUDOERS_BACKUP=/root/sudoers-backup
		source /guard.sh
		hostisan_sudo_guard admin
		hostisan_unused_group admin && groupdel admin
		useradd -m -U admin
		hostisan_sudo_guard admin "$(id -g admin)" "$(id -u admin)"
		hostisan_verify_no_sudo admin
		echo VERIFIED
		visudo -c >/dev/null
		echo "LIST:"; sudo -l -U admin 2>&1 || true
		echo "BACKUP:"; ls -R /root/sudoers-backup 2>/dev/null || true
	' _ "$2"
}

# sudo_list: the "sudo -l -U admin" part of a docker_guard output.
sudo_list() {
	local l=${output#*LIST:}
	printf '%s' "${l%%BACKUP:*}"
}

@test "docker: ubuntu 24.04 stock sudoers (%admin ALL=(ALL) ALL) gives the admin no sudo" {
	run docker_guard ubuntu:24.04 ''
	[ "$status" -eq 0 ]
	[[ "$output" == *"Disabling a sudo rule"*"%admin ALL=(ALL) ALL"* ]]
	[[ "$(sudo_list)" != *"(ALL"* ]]
	[[ "${output#*BACKUP:}" == *sudoers* ]]
	[[ "$output" == *VERIFIED* ]]
}

@test "docker: debian 13 without such a rule is left alone" {
	run docker_guard debian:trixie ''
	[ "$status" -eq 0 ]
	[[ "$output" != *"Disabling"* ]]
	[[ "$(sudo_list)" != *"(ALL"* ]]
	[[ "${output#*BACKUP:}" != *sudoers* ]]
	[[ "$output" == *VERIFIED* ]]
}

@test "docker: ubuntu 26.04 (sudo-rs) with %admin gives the admin no sudo" {
	run docker_guard ubuntu:26.04 '%admin ALL=(ALL) ALL'
	[ "$status" -eq 0 ]
	[[ "$(sudo_list)" != *"(ALL"* ]]
	[[ "${output#*BACKUP:}" == *sudoers* ]]
	[[ "$output" == *VERIFIED* ]]
}

@test "docker: ubuntu 24.04 with ALL ALL=(ALL) ALL refuses the install" {
	run docker_guard ubuntu:24.04 'ALL ALL=(ALL) ALL'
	[ "$status" -ne 0 ]
	[[ "$output" == *"ALL ALL=(ALL) ALL"* ]]
	[[ "$output" != *VERIFIED* ]]
}

@test "smoke checks the admin with sudo itself" {
	grep -qF 'is not allowed to run sudo' "$repo/test/hostisan/install-smoke.sh"
}
