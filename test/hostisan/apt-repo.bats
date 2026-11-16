#!/usr/bin/env bats
# The signed Hostisan APT repo (Plan 6, B9): tools/apt-repo/keygen.sh makes a
# throwaway key in a temp dir, tools/apt-repo/publish.sh builds the repo from
# fake debs. The "docker:" test points apt in a debian:trixie container at the
# repo (file:, signed-by); it skips without docker.

bats_require_minimum_version 1.5.0

setup_file() {
	export KEYDIR
	KEYDIR=$(mktemp -d "${BATS_TMPDIR:-/tmp}/aptkey.XXXXXX")
	rmdir "$KEYDIR"
	"$BATS_TEST_DIRNAME/../../tools/apt-repo/keygen.sh" "$KEYDIR" >/dev/null
}

teardown_file() {
	rm -rf "$KEYDIR"
}

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/aptrepo.XXXXXX")
	publish=$repo/tools/apt-repo/publish.sh
	# shellcheck source=install/common/hostisan/product.env
	source "$repo/install/common/hostisan/product.env"
	key=$KEYDIR/private.asc
	# the throwaway key is not the release key publish.sh pins: name it
	export HOSTISAN_APT_KEY_UNPINNED=1
	HOSTISAN_APT_KEY_FPR=$(cat "$KEYDIR/fingerprint.txt")
	export HOSTISAN_APT_KEY_FPR
	export GNUPGHOME=$work/gnupg
	mkdir -m 700 "$GNUPGHOME"
}

teardown() {
	gpgconf --kill gpg-agent 2>/dev/null || true
	rm -rf "$work"
}

# mkdeb DIR PACKAGE VERSION: a minimal amd64 deb in DIR.
mkdeb() {
	local d=$work/src/$2-$3
	mkdir -p "$d/DEBIAN" "$d/usr/share/doc/$2" "$1"
	printf 'Package: %s\nVersion: %s\nArchitecture: amd64\nMaintainer: Hostisan <%s>\nDescription: test %s\n' \
		"$2" "$3" "$HOSTISAN_INFO_EMAIL" "$2" >"$d/DEBIAN/control"
	echo "$2 $3" >"$d/usr/share/doc/$2/build"
	dpkg-deb --root-owner-group -Zxz --build "$d" "$1" >/dev/null
}

# build DIR REV DISTRO: a Hostisan build (hestia + its 2.x terminal).
build() {
	mkdeb "$1" hestia "1:1.10.5-$2+$3"
	mkdeb "$1" hestia-web-terminal "1:2.0.0-$2+$3"
}

# verify FILE...: gpgv against the published keyring.
verify() {
	gpgv --keyring "$work/repo/hostisan-archive-keyring.gpg" "$@"
}

# packages_entry CODENAME: package=version lines of its Packages index.
packages_of() {
	awk '/^Package:/ { p = $2 } /^Version:/ { print p "=" $2 }' \
		"$work/repo/dists/$1/main/binary-amd64/Packages" | sort
}

@test "keygen makes an ed25519 signing key the owner keeps" {
	[ "$(stat -c %a "$KEYDIR")" = 700 ]
	local f
	for f in private.asc public.asc revoke.asc hostisan-archive-keyring.gpg fingerprint.txt; do
		[ "$(stat -c %a "$KEYDIR/$f")" = 600 ]
	done
	grep -q 'BEGIN PGP PRIVATE KEY BLOCK' "$KEYDIR/private.asc"
	run gpg --batch --with-colons --show-keys "$KEYDIR/public.asc"
	echo "$output"
	# pub: ed25519 (algo 22), sign only, with an expiry date
	[[ "$(grep '^pub' <<<"$output")" =~ ^pub:[^:]*:255:22:[^:]*:[0-9]+:[0-9]+: ]]
	local caps
	caps=$(grep '^pub' <<<"$output" | cut -d: -f12)
	[[ "$caps" == *s* ]] && [[ "$caps" != *[eE]* ]]
	[ "$(grep '^fpr' <<<"$output" | head -n1 | cut -d: -f10)" = "$(cat "$KEYDIR/fingerprint.txt")" ]
	# named with the contact address of product.env
	[[ "$(grep '^uid' <<<"$output")" == *"Hostisan Archive Signing Key <$HOSTISAN_INFO_EMAIL>"* ]]
	# unprotected without a passphrase in the environment
	run gpg --batch --list-packets "$KEYDIR/private.asc"
	[[ "$output" == *"protection: 0"* ]] || [[ "$output" != *"protected"* ]]
}

@test "keygen refuses a directory that holds files" {
	mkdir "$work/k" && echo x >"$work/k/private.asc"
	run "$repo/tools/apt-repo/keygen.sh" "$work/k"
	[ "$status" -eq 1 ]
	[ "$(cat "$work/k/private.asc")" = x ]
}

# ---------------------------------------------------------------- the pinned key

@test "the release key ships in the tree with its fingerprint pinned" {
	local d=$repo/install/common/hostisan
	[ "$(cat "$d/hostisan-archive-keyring.fpr")" = 9C25AACF8D6449319AD52FE2D5DD6BC64A10C174 ]
	[ "$("$d/signing-key.sh" pinned)" = 9C25AACF8D6449319AD52FE2D5DD6BC64A10C174 ]
	# both forms hold that key and nothing else
	[ "$("$d/signing-key.sh" fpr "$d/hostisan-archive-keyring.gpg")" = 9C25AACF8D6449319AD52FE2D5DD6BC64A10C174 ]
	[ "$("$d/signing-key.sh" fpr "$d/hostisan-archive-keyring.asc")" = 9C25AACF8D6449319AD52FE2D5DD6BC64A10C174 ]
	cmp <(gpg --dearmor <"$d/hostisan-archive-keyring.asc") "$d/hostisan-archive-keyring.gpg"
	"$d/signing-key.sh" check "$d/hostisan-archive-keyring.gpg"
	"$d/signing-key.sh" check "$d/hostisan-archive-keyring.asc"
	# public halves only
	run ! grep -l 'PRIVATE KEY' "$d"/hostisan-archive-keyring.*
	run gpg --batch --with-colons --show-keys "$d/hostisan-archive-keyring.gpg"
	# the key's own name, fixed when it was made: a new product domain does not
	# change it (SIGNING-KEY.md)
	[[ "$(grep '^uid' <<<"$output")" == *"Hostisan Release <release@"* ]]
	run ! grep -q '^sec' <<<"$output"
	# the caller's keyring is left alone
	[ -z "$(gpg --batch --list-keys 2>/dev/null)" ]
}

@test "signing-key check refuses another key, two keys, or none" {
	local s=$repo/install/common/hostisan/signing-key.sh
	run "$s" check "$KEYDIR/public.asc"
	[ "$status" -eq 1 ]
	[[ "$output" == *"$(cat "$KEYDIR/fingerprint.txt")"* ]]
	[[ "$output" == *"9C25AACF8D6449319AD52FE2D5DD6BC64A10C174"* ]]
	[ "$("$s" fpr "$KEYDIR/hostisan-archive-keyring.gpg")" = "$(cat "$KEYDIR/fingerprint.txt")" ]
	# the pinned key with another one beside it
	cat "$repo/install/common/hostisan/hostisan-archive-keyring.gpg" "$KEYDIR/hostisan-archive-keyring.gpg" >"$work/two.gpg"
	[ "$("$s" fpr "$work/two.gpg" | wc -l)" -eq 2 ]
	run "$s" check "$work/two.gpg"
	[ "$status" -eq 1 ]
	# the same two keys, armored
	cat "$repo/install/common/hostisan/hostisan-archive-keyring.asc" "$KEYDIR/public.asc" >"$work/two.asc"
	[ "$("$s" fpr "$work/two.asc" | wc -l)" -eq 2 ]
	run "$s" check "$work/two.asc"
	[ "$status" -eq 1 ]
	[[ "$output" == *"$(cat "$KEYDIR/fingerprint.txt")"* ]]
	# a secret key is no public key
	run "$s" fpr "$KEYDIR/private.asc"
	[ "$status" -eq 1 ]
	run "$s" check "$KEYDIR/private.asc"
	[ "$status" -eq 1 ]
	[[ "$output" == *"no OpenPGP public key"* ]]
	echo garbage >"$work/junk"
	run "$s" check "$work/junk"
	[ "$status" -eq 1 ]
	run "$s" check "$work/nosuch"
	[ "$status" -eq 1 ]
	run "$s"
	[ "$status" -eq 2 ]
}

@test "signing-key check refuses an expired or revoked key" {
	local s=$work/sk/signing-key.sh old
	# a copy of signing-key.sh that pins the throwaway key
	mkdir "$work/sk"
	cp "$repo/install/common/hostisan/signing-key.sh" "$work/sk/"
	cp "$KEYDIR/fingerprint.txt" "$work/sk/hostisan-archive-keyring.fpr"
	"$s" check "$KEYDIR/public.asc"
	# revoked: its revocation certificate applied
	sed 's/^:-----BEGIN/-----BEGIN/' "$KEYDIR/revoke.asc" >"$work/revoke.asc"
	gpg --batch --quiet --import "$KEYDIR/public.asc" "$work/revoke.asc"
	gpg --batch --export "$(cat "$KEYDIR/fingerprint.txt")" >"$work/revoked.gpg"
	run "$s" check "$work/revoked.gpg"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"revoked"* ]]
	# expired: made in 2020 to last a year
	gpg --batch --quiet --faked-system-time 20200101T000000 --pinentry-mode loopback --passphrase '' \
		--quick-gen-key 'Old <old@example.invalid>' ed25519 sign 1y 2>/dev/null
	old=$(gpg --batch --with-colons --list-keys old@example.invalid | awk -F: '$1 == "fpr" { print $10; exit }')
	gpg --batch --export "$old" >"$work/expired.gpg"
	echo "$old" >"$work/sk/hostisan-archive-keyring.fpr"
	[ "$("$s" fpr "$work/expired.gpg")" = "$old" ]
	run "$s" check "$work/expired.gpg"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"expired"* ]]
}

@test "signing-key expires gives the pinned key's expiry" {
	local s=$work/sk/signing-key.sh fpr e1 e2 old
	mkdir "$work/sk"
	cp "$repo/install/common/hostisan/signing-key.sh" "$work/sk/"
	# the shipped release key expires 2029-11-04
	run "$repo/install/common/hostisan/signing-key.sh" expires "$repo/install/common/hostisan/hostisan-archive-keyring.gpg"
	[ "$status" -eq 0 ]
	[ "$(date -u -d "@$output" +%F)" = 2029-11-04 ]
	# a throwaway key, then the same key with its expiry moved on a year
	gpg --batch --quiet --pinentry-mode loopback --passphrase '' \
		--quick-gen-key 'Renew <renew@example.invalid>' ed25519 sign 1y 2>/dev/null
	fpr=$(gpg --batch --with-colons --list-keys renew@example.invalid | awk -F: '$1 == "fpr" { print $10; exit }')
	gpg --batch --export "$fpr" >"$work/short.gpg"
	gpg --batch --quiet --pinentry-mode loopback --passphrase '' --quick-set-expire "$fpr" 2y 2>/dev/null
	gpg --batch --export "$fpr" >"$work/long.gpg"
	gpg --batch --pinentry-mode loopback --passphrase '' --quick-set-expire "$fpr" 0 2>/dev/null
	gpg --batch --export "$fpr" >"$work/never.gpg"
	echo "$fpr" >"$work/sk/hostisan-archive-keyring.fpr"
	e1=$("$s" expires "$work/short.gpg")
	e2=$("$s" expires "$work/long.gpg")
	[[ "$e1" =~ ^[0-9]+$ ]]
	[[ "$e2" =~ ^[0-9]+$ ]]
	[ "$e2" -gt "$e1" ]
	[ "$("$s" expires "$work/never.gpg")" = never ]
	# another key, or the pinned key beside another, gives none
	run "$s" expires "$KEYDIR/public.asc"
	[ "$status" -eq 1 ]
	[[ "$output" == *"not the pinned"* ]]
	cat "$work/long.gpg" "$KEYDIR/hostisan-archive-keyring.gpg" >"$work/two.gpg"
	run "$s" expires "$work/two.gpg"
	[ "$status" -eq 1 ]
	run "$s" expires "$work/nosuch"
	[ "$status" -eq 1 ]
	# an expired key still has its date; a revoked one has none
	gpg --batch --quiet --faked-system-time 20200101T000000 --pinentry-mode loopback --passphrase '' \
		--quick-gen-key 'Old <old@example.invalid>' ed25519 sign 1y 2>/dev/null
	old=$(gpg --batch --with-colons --list-keys old@example.invalid | awk -F: '$1 == "fpr" { print $10; exit }')
	gpg --batch --export "$old" >"$work/expired.gpg"
	echo "$old" >"$work/sk/hostisan-archive-keyring.fpr"
	[ "$(date -u -d "@$("$s" expires "$work/expired.gpg")" +%F)" = 2020-12-31 ]
	cp "$KEYDIR/fingerprint.txt" "$work/sk/hostisan-archive-keyring.fpr"
	sed 's/^:-----BEGIN/-----BEGIN/' "$KEYDIR/revoke.asc" >"$work/revoke.asc"
	gpg --batch --quiet --import "$KEYDIR/public.asc" "$work/revoke.asc"
	gpg --batch --export "$(cat "$KEYDIR/fingerprint.txt")" >"$work/revoked.gpg"
	run "$s" expires "$work/revoked.gpg"
	[ "$status" -eq 1 ]
	[[ "$output" == *"revoked"* ]]
	run "$s" expires
	[ "$status" -eq 2 ]
}

@test "publish signs a repo per codename" {
	build "$work/debs" 101 debian13
	build "$work/debs" 101 ubuntu24.04
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	echo "$output"
	[ "$status" -eq 0 ]
	[ "$(ls "$work/repo/dists")" = "$(printf 'noble\ntrixie')" ]
	[ "$(packages_of trixie)" = "$(printf 'hestia-web-terminal=1:2.0.0-101+debian13\nhestia=1:1.10.5-101+debian13')" ]
	[ -f "$work/repo/pool/main/noble/hestia_1.10.5-101+ubuntu24.04_amd64.deb" ]
	grep -q '^Filename: pool/main/trixie/hestia_1.10.5-101+debian13_amd64.deb$' \
		"$work/repo/dists/trixie/main/binary-amd64/Packages"
	local c=$work/repo/dists/trixie
	verify "$c/InRelease"
	verify "$c/Release.gpg" "$c/Release"
	grep -qx 'Codename: trixie' "$c/Release"
	grep -qx 'Origin: Hostisan' "$c/Release"
	grep -qx 'Components: main' "$c/Release"
	grep -qx 'Architectures: amd64' "$c/Release"
	# Release carries the hashes of the indexes
	local f sum
	for f in Packages Packages.gz; do
		sum=$(sha256sum "$c/main/binary-amd64/$f" | cut -d' ' -f1)
		grep -Eq "^ $sum +[0-9]+ main/binary-amd64/$f\$" "$c/Release"
	done
	# the public key is published beside the repo, and is the keygen one
	gpg --batch --with-colons --show-keys "$work/repo/hostisan-archive-keyring.asc" |
		grep -q "^fpr:.*:$(cat "$KEYDIR/fingerprint.txt"):"
}

@test "a tampered Release fails verification" {
	build "$work/debs" 101 debian13
	"$publish" --key-file "$key" "$work/debs" "$work/repo"
	local c=$work/repo/dists/trixie
	sed -i 's/^Origin: Hostisan/Origin: Other/' "$c/Release"
	run verify "$c/Release.gpg" "$c/Release"
	[ "$status" -ne 0 ]
}

@test "a second build adds to the repo" {
	build "$work/b1" 101 debian13
	build "$work/b2" 102 debian13
	"$publish" --key-file "$key" "$work/b1" "$work/repo"
	run "$publish" --key-file "$key" "$work/b2" "$work/repo"
	echo "$output"
	[ "$status" -eq 0 ]
	run packages_of trixie
	[ "${#lines[@]}" -eq 4 ]
	[[ "$output" == *"hestia=1:1.10.5-101+debian13"* ]]
	[[ "$output" == *"hestia=1:1.10.5-102+debian13"* ]]
	verify "$work/repo/dists/trixie/InRelease"
	# publishing the same build again changes nothing in the pool
	run "$publish" --key-file "$key" "$work/b2" "$work/repo"
	[ "$status" -eq 0 ]
	[ "$(ls "$work/repo/pool/main/trixie" | wc -l)" -eq 4 ]
}

@test "publish refuses other bytes under a published name" {
	build "$work/b1" 101 debian13
	"$publish" --key-file "$key" "$work/b1" "$work/repo"
	local before
	before=$(cat "$work/repo/dists/trixie/InRelease")
	rm -rf "$work/src"
	mkdir -p "$work/b2"
	sleep 1 # another build time, so other bytes
	build "$work/b2" 101 debian13
	! cmp -s "$work/b1/hestia_1.10.5-101+debian13_amd64.deb" "$work/b2/hestia_1.10.5-101+debian13_amd64.deb"
	run "$publish" --key-file "$key" "$work/b2" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"already published"* ]]
	cmp "$work/b1/hestia_1.10.5-101+debian13_amd64.deb" "$work/repo/pool/main/trixie/hestia_1.10.5-101+debian13_amd64.deb"
	[ "$(cat "$work/repo/dists/trixie/InRelease")" = "$before" ]
}

@test "publish refuses a hestia deb without the epoch" {
	mkdeb "$work/debs" hestia-nginx 1.30.5-101+debian13
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"no epoch"* ]]
	[ ! -e "$work/repo" ]
}

@test "publish refuses hestia without a 2.x terminal" {
	mkdeb "$work/debs" hestia 1:1.10.5-101+debian13
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"hestia-web-terminal 2.x"* ]]
	# a terminal of another distro does not count
	mkdeb "$work/debs" hestia-web-terminal 1:2.0.0-101+debian12
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	# one already in the pool does
	mkdeb "$work/t" hestia-web-terminal 1:2.0.0-101+debian13
	"$publish" --key-file "$key" "$work/t" "$work/repo"
	rm "$work/debs"/hestia-web-terminal_*
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "publish refuses an unknown distro" {
	mkdeb "$work/debs" hestia-php 1:8.5.11-101+fedora40
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"unknown distro suffix"* ]]
}

@test "publish maps every supported distro" {
	local d
	for d in debian11 debian12 debian13 ubuntu22.04 ubuntu24.04 ubuntu26.04; do
		mkdeb "$work/debs" hestia-nginx "1:1.30.5-101+$d~rc1"
	done
	"$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$(ls "$work/repo/dists" | xargs)" = 'bookworm bullseye jammy noble resolute trixie' ]
}

@test "publish needs the passphrase of a protected key" {
	local k=$work/pkey
	HOSTISAN_APT_KEY_PASSPHRASE='s3cret pass' "$repo/tools/apt-repo/keygen.sh" --rsa4096 "$k" >/dev/null
	gpg --batch --with-colons --show-keys "$k/public.asc" | grep -q '^pub:[^:]*:4096:1:'
	HOSTISAN_APT_KEY_FPR=$(cat "$k/fingerprint.txt")
	build "$work/debs" 101 debian13
	run "$publish" --key-file "$k/private.asc" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"HOSTISAN_APT_KEY_PASSPHRASE"* ]]
	HOSTISAN_APT_KEY_PASSPHRASE='s3cret pass' run "$publish" --key-file "$k/private.asc" "$work/debs" "$work/repo"
	[ "$status" -eq 0 ]
	gpgv --keyring "$k/hostisan-archive-keyring.gpg" "$work/repo/dists/trixie/InRelease"
}

@test "publish refuses a key other than the pinned one" {
	build "$work/debs" 101 debian13
	HOSTISAN_APT_KEY_FPR=0000000000000000000000000000000000000000 \
		run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"not the pinned key"* ]]
	[ ! -e "$work/repo" ]
	# spaces and lower case, as people paste fingerprints, still match
	local fpr
	fpr=$(sed -E 's/(.{4})/\1 /g' "$KEYDIR/fingerprint.txt" | tr A-F a-f)
	HOSTISAN_APT_KEY_FPR=$fpr run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 0 ]
}

@test "publish refuses any key but the release key by default" {
	build "$work/debs" 101 debian13
	local pinned=9C25AACF8D6449319AD52FE2D5DD6BC64A10C174 own
	own=$(cat "$KEYDIR/fingerprint.txt")
	unset HOSTISAN_APT_KEY_UNPINNED HOSTISAN_APT_KEY_FPR
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"$own"* && "$output" == *"$pinned"* ]]
	[[ "$output" == *"HOSTISAN_APT_KEY_UNPINNED"* ]]
	[ ! -e "$work/repo" ]
	# HOSTISAN_APT_KEY_FPR alone cannot move the pin
	HOSTISAN_APT_KEY_FPR=$own run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"$pinned"* ]]
	[ ! -e "$work/repo" ]
	# HOSTISAN_APT_KEY_FPR may name the release key, but the file holds another
	HOSTISAN_APT_KEY_FPR=$pinned run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[[ "$output" == *"not the pinned key"* ]]
	[ ! -e "$work/repo" ]
	# only 1 turns the pin off
	HOSTISAN_APT_KEY_UNPINNED=yes run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 1 ]
	[ ! -e "$work/repo" ]
}

@test "an unpinned publish says so" {
	build "$work/debs" 101 debian13
	run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ "$status" -eq 0 ]
	[[ "$output" == *"WARNING"*"not the Hostisan release key"* ]]
	verify "$work/repo/dists/trixie/InRelease"
}

@test "an unpinned publish must name its key" {
	build "$work/debs" 101 debian13
	HOSTISAN_APT_KEY_FPR= run "$publish" --key-file "$key" "$work/debs" "$work/repo"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"HOSTISAN_APT_KEY_FPR"* ]]
	[ ! -e "$work/repo" ]
}

@test "publish refuses a key file with more than one secret key" {
	build "$work/debs" 101 debian13
	GNUPGHOME= "$repo/tools/apt-repo/keygen.sh" "$work/k2" >/dev/null
	GNUPGHOME=$work/k2/gnupg gpgconf --kill gpg-agent 2>/dev/null || true
	cat "$key" "$work/k2/private.asc" >"$work/two.asc"
	run "$publish" --key-file "$work/two.asc" "$work/debs" "$work/repo"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"more than one secret key"* ]]
	[ ! -e "$work/repo" ]
}

@test "the release workflow never turns the pin off" {
	local wf=$repo/.github/workflows/hostisan-release.yml
	# set only to empty, in the publish step, so a repository variable or an
	# inherited environment cannot turn it on
	run grep -rn 'HOSTISAN_APT_KEY_UNPINNED' "$repo/.github"
	[ "${#lines[@]}" -eq 1 ]
	[[ "${lines[0]}" =~ hostisan-release\.yml:[0-9]+:\ +HOSTISAN_APT_KEY_UNPINNED:\ \'\'$ ]]
	awk '/^      - name: Add the debs and sign the repo/ { on = 1 } on && /HOSTISAN_APT_KEY_UNPINNED: '"''"'/ { found = 1 } /^      - name: Push/ { on = 0 } END { exit !found }' "$wf"
}

@test "the release workflow checks the published key with signing-key.sh" {
	local wf=$repo/.github/workflows/hostisan-release.yml job
	job=$(awk '/^  check-repo:/ { on = 1; print; next } on && /^  [a-z-]+:$/ { on = 0 } on' "$wf")
	[ -n "$job" ]
	grep -q 'uses: actions/checkout@' <<<"$job"
	grep -qE 'install/common/hostisan/signing-key\.sh check "?\$keyring"?' <<<"$job"
	# not the first fingerprint against a variable
	run ! grep -q 'HOSTISAN_APT_KEY_FPR' <<<"$job"
	run ! grep -q 'show-keys' <<<"$job"
}

@test "the release workflow builds and checks Ubuntu 24.04 only" {
	local wf=$repo/.github/workflows/hostisan-release.yml job
	job=$(awk '/^  build:/ { on = 1; next } on && /^  [a-z0-9-]+:$/ { on = 0 } on' "$wf")
	[ "$(grep -cE '^ +- \{ image: ' <<<"$job")" -eq 1 ]
	grep -qE '^ +- \{ image: "ubuntu:24\.04", name: ubuntu24\.04 \}$' <<<"$job"
	job=$(awk '/^  check-repo:/ { on = 1; next } on && /^  [a-z0-9-]+:$/ { on = 0 } on' "$wf")
	grep -qE '^ +image: \["ubuntu:24\.04"\]$' <<<"$job"
}

@test "publish leaves the caller's keyring alone" {
	build "$work/debs" 101 debian13
	"$publish" --key-file "$key" "$work/debs" "$work/repo"
	[ -z "$(gpg --batch --list-secret-keys 2>/dev/null)" ]
}

@test "docker: apt verifies the repo and takes the newer build" {
	command -v docker >/dev/null && docker info >/dev/null 2>&1 || skip "no docker"
	local image=${HOSTISAN_APT_TEST_IMAGE:-debian:trixie}
	build "$work/b1" 101 debian13
	build "$work/b2" 102 debian13
	"$publish" --key-file "$key" "$work/b1" "$work/repo"
	"$publish" --key-file "$key" "$work/b2" "$work/repo"
	# an unsigned copy, and a copy signed by another key
	cp -a "$work/repo" "$work/unsigned"
	rm "$work/unsigned/dists/trixie/InRelease" "$work/unsigned/dists/trixie/Release.gpg"
	cp -a "$work/repo" "$work/otherkey"
	mkdir "$work/k2"
	"$repo/tools/apt-repo/keygen.sh" "$work/k2/key" >/dev/null
	cp "$work/k2/key/hostisan-archive-keyring.gpg" "$work/otherkey/hostisan-archive-keyring.gpg"
	# apt reads file: repos as _apt
	chmod 755 "$work"
	chmod -R a+rX "$work/repo" "$work/unsigned" "$work/otherkey"
	run docker run --rm --network none -v "$work:/w:ro" "$image" bash -c '
		set -u
		rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
		update() {
			echo "deb [signed-by=/w/$1/hostisan-archive-keyring.gpg] file:/w/$1 trixie main" \
				>/etc/apt/sources.list.d/hostisan.list
			apt-get update 2>&1
		}
		update repo || { echo "FAIL signed update"; exit 1; }
		apt-cache policy hestia hestia-web-terminal
		cd /tmp && apt-get download hestia >/dev/null 2>&1 && ls /tmp/hestia_*.deb
		update unsigned && { echo "FAIL unsigned accepted"; exit 1; }
		update otherkey && { echo "FAIL other key accepted"; exit 1; }
		echo DONE'
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"DONE"* ]]
	grep -A2 '^hestia:' <<<"$output" | grep -q 'Candidate: 1:1.10.5-102+debian13'
	grep -A2 '^hestia-web-terminal:' <<<"$output" | grep -q 'Candidate: 1:2.0.0-102+debian13'
	# apt-get download checks the pool file against the signed index
	[[ "$output" == *"/tmp/hestia_1%3a1.10.5-102+debian13_amd64.deb"* ]]
	[[ "$output" == *"file:/w/unsigned trixie Release' is not signed"* ]]
	[[ "$output" == *"file:/w/otherkey trixie InRelease' is not signed"* ]]
}
