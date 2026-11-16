#!/bin/bash
# Builds (or extends) the signed static Hostisan APT repository.
#
# Usage: tools/apt-repo/publish.sh --key-file KEY.asc DEB_DIR REPO_DIR
#
# Every *.deb in DEB_DIR is copied into REPO_DIR/pool/main/<codename>/, where
# the codename comes from the build's distro suffix (1:1.10.5-101+debian13 ->
# trixie). Debs already in the pool stay, so REPO_DIR can be the currently
# published repo (a checkout of the gh-pages branch) and every release adds to
# it. Then the indexes are rebuilt for every codename in the pool:
#
#   dists/<codename>/{Release,Release.gpg,InRelease}
#   dists/<codename>/main/binary-amd64/Packages{,.gz}
#   hostisan-archive-keyring.gpg   the public key (binary, for signed-by=)
#   hostisan-archive-keyring.asc   the same key, armored
#
# Servers use it as:
#   deb [arch=amd64 signed-by=/usr/share/keyrings/hostisan-archive-keyring.gpg] <URL> <codename> main
#
# KEY.asc is the armored secret signing key (tools/apt-repo/keygen.sh). It is
# imported into a throwaway GNUPGHOME, never into the caller's keyring. A
# protected key needs its passphrase in HOSTISAN_APT_KEY_PASSPHRASE.
#
# The key must be the Hostisan release key, whose fingerprint is pinned in
# install/common/hostisan/hostisan-archive-keyring.fpr: servers trust that key
# only. HOSTISAN_APT_KEY_FPR, when set, must be that same fingerprint. For a
# test repository signed by a throwaway key, HOSTISAN_APT_KEY_UNPINNED=1 turns
# the pin off (with a warning); HOSTISAN_APT_KEY_FPR must then name that key.
# KEY.asc must hold one secret key.
#
# Refused (nothing is written): a hestia* deb without the Hostisan epoch (1:),
# an unknown distro suffix, a deb whose file name is already in the pool with
# other bytes, and a hestia deb whose codename has no hestia-web-terminal 2.x.
set -euo pipefail

ARCHES=amd64
SIGNING_KEY_SH=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/install/common/hostisan/signing-key.sh
ORIGIN=${HOSTISAN_APT_ORIGIN:-Hostisan}
KEYRING_NAME=hostisan-archive-keyring

die() {
	echo "publish.sh: $*" >&2
	exit 1
}

usage() {
	echo "usage: $0 --key-file KEY.asc DEB_DIR REPO_DIR" >&2
	exit 2
}

# codename VERSION: the distro codename of a Hostisan build's Debian version.
codename() {
	case "$1" in
		*+debian11 | *+debian11~*) echo bullseye ;;
		*+debian12 | *+debian12~*) echo bookworm ;;
		*+debian13 | *+debian13~*) echo trixie ;;
		*+ubuntu22.04 | *+ubuntu22.04~*) echo jammy ;;
		*+ubuntu24.04 | *+ubuntu24.04~*) echo noble ;;
		*+ubuntu26.04 | *+ubuntu26.04~*) echo resolute ;;
		*) return 1 ;;
	esac
}

key_file=
while [ $# -gt 0 ]; do
	case "$1" in
		--key-file)
			[ $# -ge 2 ] || usage
			key_file=$2
			shift 2
			;;
		--) shift && break ;;
		-*) usage ;;
		*) break ;;
	esac
done
if [ $# -ne 2 ] || [ -z "$key_file" ]; then
	usage
fi
deb_dir=$1
repo=$2
[ -d "$deb_dir" ] || die "no such directory: $deb_dir"
[ -r "$key_file" ] || die "cannot read key file: $key_file"
for t in apt-ftparchive dpkg-deb gpg gzip; do
	command -v "$t" > /dev/null || die "$t is missing (apt-ftparchive is in apt-utils)"
done

work=$(mktemp -d)
cleanup() {
	GNUPGHOME=$work/gnupg gpgconf --kill gpg-agent 2> /dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

# ---------------------------------------------------------------- the key

export GNUPGHOME=$work/gnupg
mkdir -m 700 "$GNUPGHOME"
gpg --batch --quiet --import "$key_file" 2> /dev/null || die "cannot import $key_file"
# a sec record's own fingerprint is the fpr line right after it
mapfile -t fprs < <(gpg --batch --with-colons --list-secret-keys |
	awk -F: '$1 == "sec" { p = 1; next } p && $1 == "fpr" { print $10 } { p = 0 }')
[ "${#fprs[@]}" -gt 0 ] || die "$key_file holds no secret key"
[ "${#fprs[@]}" -eq 1 ] || die "$key_file holds more than one secret key (${fprs[*]}); give it the signing key only"
fpr=${fprs[0]}
# Servers trust the pinned release key only: signing with another one would
# break apt on every server.
pinned=$("$SIGNING_KEY_SH" pinned) || die "no pinned fingerprint ($SIGNING_KEY_SH pinned)"
want=$(tr -d ' \t' <<< "${HOSTISAN_APT_KEY_FPR:-}" | tr a-f A-F)
if [ "${HOSTISAN_APT_KEY_UNPINNED:-}" = 1 ]; then
	# a stray UNPINNED=1 alone must not unpin: the test key is named too
	[ -n "$want" ] || die "HOSTISAN_APT_KEY_UNPINNED=1 needs HOSTISAN_APT_KEY_FPR set to the test key's fingerprint"
	if [ "$want" != "$fpr" ]; then
		die "$key_file holds $fpr, not the pinned key $want (HOSTISAN_APT_KEY_FPR)"
	fi
	if [ "$fpr" != "$pinned" ]; then
		echo "publish.sh: WARNING: HOSTISAN_APT_KEY_UNPINNED=1: signing with $fpr," >&2
		echo "publish.sh: WARNING: not the Hostisan release key $pinned. Servers installed" >&2
		echo "publish.sh: WARNING: with the release key will refuse this repository." >&2
	fi
else
	if [ -n "$want" ] && [ "$want" != "$pinned" ]; then
		die "HOSTISAN_APT_KEY_FPR is $want, not the Hostisan release key $pinned (a test repository needs HOSTISAN_APT_KEY_UNPINNED=1)"
	fi
	if [ "$fpr" != "$pinned" ]; then
		die "$key_file holds $fpr, not the pinned key $pinned, the Hostisan release key (a test repository needs HOSTISAN_APT_KEY_UNPINNED=1)"
	fi
fi

# sign ARGS...: gpg with the signing key, and its passphrase when one is set.
sign() {
	local pass=(--pinentry-mode loopback --passphrase-fd 3)
	gpg --batch --yes --quiet --local-user "$fpr" --digest-algo SHA512 "${pass[@]}" "$@" \
		3<<< "${HOSTISAN_APT_KEY_PASSPHRASE:-}"
}
echo test | sign --clearsign > /dev/null 2>&1 \
	|| die "cannot sign with $fpr (protected key: set HOSTISAN_APT_KEY_PASSPHRASE)"

# ---------------------------------------------------------------- check the debs

shopt -s nullglob
debs=("$deb_dir"/*.deb)
[ "${#debs[@]}" -gt 0 ] || die "no .deb files in $deb_dir"

plan=() # "codename<TAB>path<TAB>package<TAB>version"
for deb in "${debs[@]}"; do
	pkg=$(dpkg-deb -f "$deb" Package) || die "not a deb: $deb"
	ver=$(dpkg-deb -f "$deb" Version)
	c=$(codename "$ver") || die "$(basename "$deb"): unknown distro suffix in $ver"
	case "$pkg" in
		hestia*) [[ "$ver" == [1-9]*:* ]] || die "$(basename "$deb"): $ver has no epoch (Hostisan builds are 1:...)" ;;
	esac
	dest=$repo/pool/main/$c/$(basename "$deb")
	if [ -e "$dest" ] && ! cmp -s "$deb" "$dest"; then
		die "$(basename "$deb") is already published with other contents (bump HOSTISAN_BUILD)"
	fi
	plan+=("$c"$'\t'"$deb"$'\t'"$pkg"$'\t'"$ver")
done

# A hestia without a 2.x terminal beside it would make apt remove the Node
# terminal (Breaks) without the Go one to take over.
for line in "${plan[@]}"; do
	IFS=$'\t' read -r c _ pkg _ <<< "$line"
	[ "$pkg" = hestia ] || continue
	found=
	for other in "${plan[@]}"; do
		IFS=$'\t' read -r c2 _ pkg2 ver2 <<< "$other"
		if [ "$c2" = "$c" ] && [ "$pkg2" = hestia-web-terminal ] && dpkg --compare-versions "$ver2" ge 1:2.0.0; then
			found=1
		fi
	done
	for f in "$repo/pool/main/$c"/hestia-web-terminal_*.deb; do
		dpkg --compare-versions "$(dpkg-deb -f "$f" Version)" ge 1:2.0.0 && found=1
	done
	[ -n "$found" ] || die "$c: hestia needs hestia-web-terminal 2.x published with it"
done

# ---------------------------------------------------------------- pool and indexes

for line in "${plan[@]}"; do
	IFS=$'\t' read -r c deb pkg ver <<< "$line"
	mkdir -p "$repo/pool/main/$c"
	cp -p "$deb" "$repo/pool/main/$c/"
	echo "pool/main/$c: $pkg $ver"
done

rm -rf "$repo/dists"
cd "$repo"
for dir in pool/main/*/; do
	c=$(basename "$dir")
	for arch in $ARCHES; do
		out=dists/$c/main/binary-$arch
		mkdir -p "$out"
		apt-ftparchive --arch "$arch" packages "pool/main/$c" > "$out/Packages"
		gzip -9n < "$out/Packages" > "$out/Packages.gz"
	done
	(
		cd "dists/$c"
		apt-ftparchive \
			-o APT::FTPArchive::Release::Origin="$ORIGIN" \
			-o APT::FTPArchive::Release::Label="$ORIGIN" \
			-o APT::FTPArchive::Release::Suite="$c" \
			-o APT::FTPArchive::Release::Codename="$c" \
			-o APT::FTPArchive::Release::Architectures="$ARCHES" \
			-o APT::FTPArchive::Release::Components=main \
			-o APT::FTPArchive::Release::Description="$ORIGIN packages for $c" \
			release . > "$work/Release"
		mv "$work/Release" Release
		sign --clearsign -o InRelease Release
		sign --armor --detach-sign -o Release.gpg Release
	)
	echo "dists/$c: signed by $fpr"
done

gpg --batch --export "$fpr" > "$KEYRING_NAME.gpg"
gpg --batch --armor --export "$fpr" > "$KEYRING_NAME.asc"
