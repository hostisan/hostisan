#!/usr/bin/env bats
# make debs (tools/build-debs.sh): the four Hostisan debs built in a clean
# ubuntu:24.04 container from a git archive of HEAD.
#
# These tests run without Docker: DOCKER points at a stub that records how it
# was called and writes fake debs the way the real build would. The --verify
# tests build tiny debs with dpkg-deb (skipped without it). The real build
# takes about 20 minutes, so it runs only with docker and
# HOSTISAN_TEST_REAL_BUILD=1.

bats_require_minimum_version 1.5.0

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	script=$repo/tools/build-debs.sh
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/build-debs.XXXXXX")
	out=$work/out
	stub_docker
	export DOCKER=$work/bin/docker
	unset HOSTISAN_BUILD HOSTISAN_VERSION STUB_MODE
}

teardown() {
	rm -rf "$work"
}

# stub_docker: a docker that logs its arguments and environment, saves what it
# reads on stdin (the source archive), and writes into the dir mounted on /out:
# by STUB_MODE, all four debs and the .verified mark (ok), a set without
# hestia-php (no-php), all four but no mark (unverified), or nothing and exit 1
# (fail).
stub_docker() {
	mkdir -p "$work/bin"
	cat >"$work/bin/docker" <<-'EOF'
		#!/bin/bash
		log=$(dirname "$0")/../docker.log
		echo "docker $*" >>"$log"
		[ "$1" = info ] && exit 0
		[ "$1" = rm ] && exit 0
		dir=
		prev=
		for a in "$@"; do
			if [ "$prev" = -v ] && [[ $a == *:/out ]]; then dir=${a%:/out}; fi
			if [ "$prev" = -e ]; then echo "env $a" >>"$log"; fi
			prev=$a
		done
		cat >"$(dirname "$0")/../stdin.tar"
		[ -n "$dir" ] || { echo "stub: no /out mount" >&2; exit 9; }
		b=${HOSTISAN_BUILD:-105}
		mk() { echo "$1" >"$dir/$1_${2#*:}-${b}+ubuntu24.04_amd64.deb"; }
		case ${STUB_MODE:-ok} in
			fail) exit 1 ;;
			no-php) mk hostisan 1:1.10.5; mk hostisan-nginx 1:1.30.5; mk hostisan-web-terminal 1:2.0.0 ;;
			*) mk hostisan 1:1.10.5; mk hostisan-nginx 1:1.30.5; mk hostisan-php 1:8.5.11; mk hostisan-web-terminal 1:2.0.0 ;;
		esac
		[ "${STUB_MODE:-ok}" = unverified ] || [ "${STUB_MODE:-ok}" = no-php ] || touch "$dir/.verified"
		exit 0
	EOF
	chmod +x "$work/bin/docker"
}

# mkdeb DIR PACKAGE VERSION [PATH SIZE]: a deb with PATH holding SIZE bytes.
mkdeb() {
	local d=$work/deb-$2
	rm -rf "$d"
	mkdir -p "$d/DEBIAN"
	printf 'Package: %s\nVersion: %s\nArchitecture: amd64\nMaintainer: t <t@t>\nDescription: t\n' "$2" "$3" >"$d/DEBIAN/control"
	if [ -n "${4:-}" ]; then
		mkdir -p "$d/$(dirname "$4")"
		head -c "${5:-10}" /dev/zero >"$d/$4"
		chmod 755 "$d/$4"
	fi
	dpkg-deb --root-owner-group -b "$d" "$1/$2_${3#*:}_amd64.deb" >/dev/null
}

# hostisan_deb DIR BUILD [css|ui]: the hostisan deb as the build makes it: the
# panel binary with the built web UI embedded (its index.html names a Vite
# asset) and the old pages' CSS themes. css or ui leaves that one out.
hostisan_deb() {
	local d=$work/deb-hostisan
	rm -rf "$d"
	mkdir -p "$d/DEBIAN" "$d/usr/local/hestia/hostisan" "$d/usr/local/hestia/web/css/themes"
	printf 'Package: hostisan\nVersion: 1:1.10.5-%s+ubuntu24.04\nArchitecture: amd64\nMaintainer: t <t@t>\nDescription: t\n' "$2" >"$d/DEBIAN/control"
	{
		head -c 4096 /dev/urandom
		[ "${3:-}" = ui ] || printf '<script type="module" crossorigin src="/assets/index-AbC12_-x.js"></script>'
		head -c 4096 /dev/urandom
	} >"$d/usr/local/hestia/hostisan/hostisan"
	chmod 755 "$d/usr/local/hestia/hostisan/hostisan"
	[ "${3:-}" = css ] || echo 'body{}' >"$d/usr/local/hestia/web/css/themes/default.min.css"
	dpkg-deb --root-owner-group -b "$d" "$1/hostisan_1.10.5-$2+ubuntu24.04_amd64.deb" >/dev/null
}

# good_debs DIR BUILD: the four debs as the build makes them.
good_debs() {
	mkdir -p "$1"
	hostisan_deb "$1" "$2"
	mkdeb "$1" hostisan-nginx "1:1.30.5-$2+ubuntu24.04" usr/local/hestia/nginx/sbin/hestia-nginx
	mkdeb "$1" hostisan-php "1:8.5.11-$2+ubuntu24.04" usr/local/hestia/php/sbin/hestia-php
	mkdeb "$1" hostisan-web-terminal "1:2.0.0-$2+ubuntu24.04"
}

@test "--help prints the usage and exits 0" {
	run "$script" --help
	[ "$status" -eq 0 ]
	[[ "$output" == *"Usage: tools/build-debs.sh"* ]]
	[[ "$output" == *"HOSTISAN_BUILD"* ]]
	[[ "$output" == *"HOSTISAN_VERSION"* ]]
	[ ! -e "$work/docker.log" ]
}

@test "an unknown option is a usage error" {
	run "$script" --nope
	[ "$status" -eq 2 ]
	[[ "$output" == *"Usage:"* ]]
	run "$script" --out
	[ "$status" -eq 2 ]
	[ ! -e "$work/docker.log" ]
}

@test "a build number under 105 or not a number is refused before docker runs" {
	HOSTISAN_BUILD=104 run "$script" --out "$out"
	[ "$status" -eq 1 ]
	[[ "$output" == *"HOSTISAN_BUILD"*"105"* ]]
	HOSTISAN_BUILD=12a run "$script" --out "$out"
	[ "$status" -eq 1 ]
	[ ! -e "$work/docker.log" ]
}

@test "an unsafe version or an unknown ref is refused" {
	HOSTISAN_VERSION='v1 $(id)' run "$script" --out "$out"
	[ "$status" -eq 1 ]
	[[ "$output" == *"HOSTISAN_VERSION"* ]]
	run "$script" --out "$out" --ref no-such-ref-xyz
	[ "$status" -eq 1 ]
	[[ "$output" == *"no-such-ref-xyz"* ]]
	[ ! -e "$work/docker.log" ]
}

@test "no docker: a clear error" {
	DOCKER=$work/none/docker run "$script" --out "$out"
	[ "$status" -eq 1 ]
	[[ "$output" == *"needs Docker"* ]]
}

@test "builds in ubuntu:24.04 from a git archive of HEAD with --all and ~localsrc" {
	HOSTISAN_BUILD=120 HOSTISAN_VERSION=v9.9.9-test run "$script" --out "$out"
	echo "$output"
	[ "$status" -eq 0 ]
	grep -q '^docker run .* ubuntu:24.04 bash -c' "$work/docker.log"
	grep -q -- "--all --noinstall --keepbuild '~localsrc'" "$work/docker.log"
	grep -qx 'env HOSTISAN_BUILD=120' "$work/docker.log"
	grep -qx 'env HOSTISAN_VERSION=v9.9.9-test' "$work/docker.log"
	# the source is HEAD as git archive gives it (no .git, no node_modules)
	tar -tf "$work/stdin.tar" | grep -qx 'src/hst_autocompile.sh'
	run ! tar -tf "$work/stdin.tar" .git
	[ "$(tar -xOf "$work/stdin.tar" Makefile | sha256sum)" = "$(git -C "$repo" show HEAD:Makefile | sha256sum)" ]
}

@test "a good build leaves the four debs, a version stamp, sums and the install tarball" {
	HOSTISAN_BUILD=120 HOSTISAN_VERSION=v9.9.9-test run "$script" --out "$out"
	[ "$status" -eq 0 ]
	ls "$out"
	[ -f "$out"/hostisan_1.10.5-120+ubuntu24.04_amd64.deb ]
	[ -f "$out"/hostisan-nginx_*.deb ]
	[ -f "$out"/hostisan-php_*.deb ]
	[ -f "$out"/hostisan-web-terminal_*.deb ]
	[ "$(cat "$out/VERSION")" = 'v9.9.9-test (build 120)' ]
	(cd "$out" && sha256sum -c SHA256SUMS)
	grep -q 'hostisan-install.tar.gz' "$out/SHA256SUMS"
	# the install/ tree the server needs, stamped with the same version
	tar -xzf "$out/hostisan-install.tar.gz" -C "$work"
	[ -f "$work/install/hst-install.sh" ]
	[ "$(cat "$work/install/common/hostisan/VERSION")" = 'v9.9.9-test (build 120)' ]
	[ "$(find "$work/install" -maxdepth 0 -printf '%u')" = "$(id -un)" ]
	# no build leftovers, and the output says where things are
	[ -z "$(find "$out" -name '.build*')" ]
	[[ "$output" == *"$out"* ]]
	[[ "$output" == *"--with-debs"* ]]
	# the list names each file in full
	grep -qE '^ +[0-9]+  hostisan-install\.tar\.gz$' <<<"$output"
	grep -qE '^ +[0-9]+  hostisan-php_8\.5\.11-120\+ubuntu24\.04_amd64\.deb$' <<<"$output"
}

@test "the build number defaults to 105 and the version to git describe" {
	run "$script" --out "$out"
	[ "$status" -eq 0 ]
	grep -qx 'env HOSTISAN_BUILD=105' "$work/docker.log"
	grep -qx "env HOSTISAN_VERSION=$(git -C "$repo" describe --always HEAD)" "$work/docker.log"
}

@test "a missing deb fails loudly and names it" {
	mkdir -p "$out"
	echo old >"$out/hostisan_old.deb"
	STUB_MODE=no-php run "$script" --out "$out"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"missing: hostisan-php_*.deb"* ]]
	# a failed build replaces nothing in the output dir (only build.log is new)
	[ "$(ls "$out" | grep -vx build.log)" = hostisan_old.deb ]
}

@test "a build whose checks did not pass fails even though the build exited 0" {
	STUB_MODE=unverified run "$script" --out "$out"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"compile failed"* ]]
	[ ! -e "$out/VERSION" ]
}

@test "a failed container run fails" {
	STUB_MODE=fail run "$script" --out "$out"
	[ "$status" -eq 1 ]
	[[ "$output" == *"missing: hostisan_*.deb"* ]]
	[[ "$output" == *"build.log"* ]]
}

@test "--verify passes the four debs of this build" {
	command -v dpkg-deb >/dev/null || skip "no dpkg-deb"
	good_debs "$work/d" 120
	run "$script" --verify "$work/d" 120
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "--verify fails a deb without its compiled binary, an empty one, or another build" {
	command -v dpkg-deb >/dev/null || skip "no dpkg-deb"
	good_debs "$work/d" 120
	# PHP failed to compile but the deb got built anyway
	mkdeb "$work/d" hostisan-php "1:8.5.11-120+ubuntu24.04"
	run "$script" --verify "$work/d" 120
	[ "$status" -eq 1 ]
	[[ "$output" == *"hostisan-php"*"usr/local/hestia/php/sbin/hestia-php"* ]]
	# nginx binary present but empty
	good_debs "$work/d" 120
	mkdeb "$work/d" hostisan-nginx "1:1.30.5-120+ubuntu24.04" usr/local/hestia/nginx/sbin/hestia-nginx 0
	run "$script" --verify "$work/d" 120
	[ "$status" -eq 1 ]
	[[ "$output" == *"hostisan-nginx"* ]]
	# built with another build number
	rm -f "$work/d"/*.deb
	good_debs "$work/d" 119
	run "$script" --verify "$work/d" 120
	[ "$status" -eq 1 ]
	[[ "$output" == *"-120+"* ]]
	# and a missing one
	rm -f "$work/d"/hostisan-web-terminal_*.deb
	run "$script" --verify "$work/d" 119
	[ "$status" -eq 1 ]
	[[ "$output" == *"missing: hostisan-web-terminal_*.deb"* ]]
}

@test "--verify fails a hostisan deb without the built web UI or its CSS" {
	command -v dpkg-deb >/dev/null || skip "no dpkg-deb"
	good_debs "$work/d" 120
	# the UI build failed: the binary serves "isn't built" instead
	hostisan_deb "$work/d" 120 ui
	run "$script" --verify "$work/d" 120
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"hostisan: the hostisan binary holds no built web UI"* ]]
	# npm run build failed: no CSS themes
	hostisan_deb "$work/d" 120 css
	run "$script" --verify "$work/d" 120
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"usr/local/hestia/web/css/themes/default.min.css"* ]]
}

@test "the container runs with --init under a name, and is removed afterwards" {
	run "$script" --out "$out"
	[ "$status" -eq 0 ]
	cat "$work/docker.log"
	local name
	name=$(grep '^docker run ' "$work/docker.log" | grep -o -- '--name hostisan-debs-[0-9]*' | cut -d' ' -f2)
	[ -n "$name" ]
	grep '^docker run ' "$work/docker.log" | grep -q -- ' --init '
	grep -qx "docker rm -f $name" "$work/docker.log"
	# also when the build fails
	: >"$work/docker.log"
	STUB_MODE=fail run "$script" --out "$out"
	[ "$status" -eq 1 ]
	grep -q '^docker rm -f hostisan-debs-' "$work/docker.log"
}

@test "DOCKER may be a command with arguments, such as sudo docker" {
	printf '#!/bin/bash\necho "sudo $*" >>"%s/sudo.log"\nexec "$@"\n' "$work" >"$work/bin/sudo"
	chmod +x "$work/bin/sudo"
	DOCKER="$work/bin/sudo $work/bin/docker" run "$script" --out "$out"
	echo "$output"
	[ "$status" -eq 0 ]
	grep -q "^sudo $work/bin/docker info" "$work/sudo.log"
	grep -q "^sudo $work/bin/docker run " "$work/sudo.log"
	grep -q "^sudo $work/bin/docker rm -f hostisan-debs-" "$work/sudo.log"
	[ -f "$out/VERSION" ]
}

@test "make debs runs the script with the output dir and passes the overrides" {
	run make -s -n -C "$repo" debs
	[ "$status" -eq 0 ]
	[[ "$output" == *"tools/build-debs.sh --out build/debs"* ]]
	run make -s -n -C "$repo" debs DEBS_OUT=/tmp/x HOSTISAN_BUILD=130 HOSTISAN_VERSION=v1.2.3 REF=v1.0.0
	[[ "$output" == *"HOSTISAN_BUILD=130"* ]]
	[[ "$output" == *"HOSTISAN_VERSION=v1.2.3"* ]]
	[[ "$output" == *"--out /tmp/x"* ]]
	[[ "$output" == *"--ref v1.0.0"* ]]
}

@test "the output dir is gitignored" {
	git -C "$repo" check-ignore -q build/debs/hestia_1.deb
	git -C "$repo" check-ignore -q install/common/hostisan/VERSION
}

@test "the README documents make debs" {
	grep -q 'make debs' "$repo/README.md"
	grep -q 'build/debs' "$repo/README.md"
	grep -q 'HOSTISAN_BUILD' "$repo/README.md"
}

@test "docker: a real make debs builds the four debs (HOSTISAN_TEST_REAL_BUILD=1)" {
	command -v docker >/dev/null && docker info >/dev/null 2>&1 || skip "no docker"
	[ "${HOSTISAN_TEST_REAL_BUILD:-}" = 1 ] || skip "takes about 20 minutes: set HOSTISAN_TEST_REAL_BUILD=1"
	unset DOCKER
	run "$script" --out "$out"
	[ "$status" -eq 0 ]
	run "$script" --verify "$out" 105
	[ "$status" -eq 0 ]
}
