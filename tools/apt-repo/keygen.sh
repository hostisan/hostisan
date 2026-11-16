#!/bin/bash
# Creates the Hostisan APT signing key in a new directory the owner keeps.
#
# Usage: tools/apt-repo/keygen.sh [--rsa4096] [--expire 3y] [--uid "NAME <EMAIL>"] OUT_DIR
#
# OUT_DIR must not exist yet (or be empty); it is created mode 0700 with:
#   private.asc                    the armored secret key: the CI secret
#                                  HOSTISAN_APT_KEY, and the owner's offline copy
#   revoke.asc                     revocation certificate (keep offline)
#   public.asc                     the armored public key
#   hostisan-archive-keyring.gpg   the public key for signed-by= (installer)
#   fingerprint.txt                the fingerprint to publish
#   gnupg/                         the GnuPG home the key was made in
#
# The key is ed25519 (sign only) unless --rsa4096 is given; it expires after
# --expire (default 3y; extend it with gpg --quick-set-expire before then, and
# republish the public key).
#
# Passphrase: none is asked for, ever. With HOSTISAN_APT_KEY_PASSPHRASE set,
# the key is protected with it (then CI needs the same value as the secret
# HOSTISAN_APT_KEY_PASSPHRASE); unset, the key has no passphrase and the
# files above are the only protection (keep them on encrypted storage).
set -euo pipefail

algo=ed25519
expire=3y
# The default uid's address is the contact address of product.env.
# shellcheck source=install/common/hostisan/product.env
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/install/common/hostisan/product.env"
uid="Hostisan Archive Signing Key <$HOSTISAN_INFO_EMAIL>"

usage() {
	echo "usage: $0 [--rsa4096] [--expire 3y] [--uid \"NAME <EMAIL>\"] OUT_DIR" >&2
	exit 2
}

while [ $# -gt 0 ]; do
	case "$1" in
		--rsa4096) algo=rsa4096 && shift ;;
		--expire | --uid)
			[ $# -ge 2 ] || usage
			if [ "$1" = --expire ]; then expire=$2; else uid=$2; fi
			shift 2
			;;
		--) shift && break ;;
		-*) usage ;;
		*) break ;;
	esac
done
[ $# -eq 1 ] || usage
out=$1

if [ -e "$out" ] && [ -n "$(ls -A "$out")" ]; then
	echo "keygen.sh: $out is not empty; refusing to overwrite a key" >&2
	exit 1
fi
umask 077
mkdir -p "$out"
chmod 700 "$out"
out=$(cd "$out" && pwd)

export GNUPGHOME=$out/gnupg
mkdir -m 700 "$GNUPGHOME"
trap 'gpgconf --kill gpg-agent 2>/dev/null || true' EXIT

pass=${HOSTISAN_APT_KEY_PASSPHRASE:-}
# gpg ARGS...: batch gpg that takes the passphrase (maybe empty) on fd 3.
g() {
	gpg --batch --quiet --pinentry-mode loopback --passphrase-fd 3 "$@" 3<<< "$pass"
}

g --quick-gen-key "$uid" "$algo" sign "$expire"
fpr=$(gpg --batch --with-colons --list-secret-keys 2> /dev/null | awk -F: '$1 == "fpr" { print $10; exit }')

g --armor --export-secret-keys "$fpr" > "$out/private.asc"
gpg --batch --armor --export "$fpr" > "$out/public.asc"
gpg --batch --export "$fpr" > "$out/hostisan-archive-keyring.gpg"
cp "$GNUPGHOME/openpgp-revocs.d/$fpr.rev" "$out/revoke.asc"
echo "$fpr" > "$out/fingerprint.txt"
chmod 600 "$out"/*.asc "$out"/*.gpg "$out/fingerprint.txt"

protected=no
[ -n "$pass" ] && protected=yes
cat << MSG
Created the Hostisan APT signing key in $out
  fingerprint: $fpr
  algorithm:   $algo, expires in $expire, passphrase: $protected
Next: add private.asc as the GitHub secret HOSTISAN_APT_KEY$([ -n "$pass" ] && echo " (and the passphrase as HOSTISAN_APT_KEY_PASSPHRASE)"),
publish the fingerprint, and keep this directory offline (see RELEASING.md).
MSG
