#!/usr/bin/env bash
# Usage: signing-key.sh pinned | fpr FILE | check FILE | expires FILE
#
# The Hostisan APT signing key. Its public half ships next to this script, as
# hostisan-archive-keyring.gpg (binary, for signed-by=) and .asc (armored).
# Its fingerprint is written down once, in hostisan-archive-keyring.fpr: the
# installers, setup.sh and tools/apt-repo/publish.sh all read it through here.
#
#   pinned      print the pinned fingerprint
#   fpr FILE    print the fingerprint of each primary public key in FILE
#               (armored or binary), one per line; exit 1 when it holds none
#   check FILE  exit 0 when FILE holds the pinned key and no other, and that
#               key has not expired or been revoked; else say why and exit 1
#   expires FILE
#               when FILE holds the pinned key and no other, and it has not
#               been revoked (expired is fine), print when it expires: seconds
#               since the epoch, or "never"; else say why and exit 1. setup.sh
#               compares a server's keyring with the shipped one this way
#
# Exit 2: usage, or no well-formed fingerprint in the .fpr file. Needs gpg.
# FILE is read in a throwaway GNUPGHOME: nothing is imported anywhere.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

usage() {
	echo "usage: signing-key.sh pinned | fpr FILE | check FILE | expires FILE" >&2
	exit 2
}

pinned() {
	local f
	f=$(tr -d ' \t\n' < "$here/hostisan-archive-keyring.fpr" 2> /dev/null) || f=
	if [[ ! "$f" =~ ^[0-9A-F]{40}$ ]]; then
		echo "signing-key.sh: no fingerprint in $here/hostisan-archive-keyring.fpr" >&2
		exit 2
	fi
	echo "$f"
}

# keys FILE: "VALIDITY FINGERPRINT EXPIRY" for each primary public key in
# FILE, read in the throwaway GNUPGHOME $home; exit 1 when it holds none.
# VALIDITY is gpg's field 2: e expired, r revoked. EXPIRY is field 7, seconds
# since the epoch, or "never" when it is empty.
keys() {
	local out
	[ -f "$1" ] && [ -r "$1" ] || return 1
	out=$(GNUPGHOME=$home gpg --batch --no-autostart --with-colons --show-keys "$1" 2> /dev/null) || out=
	# a pub record's own fingerprint is the fpr line right after it
	out=$(awk -F: '$1 == "pub" { v = $2; x = ($7 == "" ? "never" : $7); p = 1; next }
		p && $1 == "fpr" { print v " " $10 " " x } { p = 0 }' <<< "$out")
	[ -n "$out" ] || return 1
	echo "$out"
}

case "${1:-}" in
	pinned)
		[ $# -eq 1 ] || usage
		pinned
		;;
	fpr | check | expires)
		[ $# -eq 2 ] || usage
		[ "$1" = fpr ] || want=$(pinned)
		home=$(mktemp -d)
		trap 'rm -rf "$home"' EXIT
		if ! got=$(keys "$2"); then
			[ "$1" = fpr ] && exit 1
			echo "$2 holds no OpenPGP public key (the pinned Hostisan release key is $want)"
			exit 1
		fi
		fprs=$(cut -d' ' -f2 <<< "$got")
		expiry=$(cut -d' ' -f3 <<< "$got")
		if [ "$1" = fpr ]; then
			echo "$fprs"
			exit 0
		fi
		if [ "$fprs" != "$want" ]; then
			echo "$2 holds $(paste -sd' ' <<< "$fprs"), not the pinned Hostisan release key $want"
			exit 1
		fi
		case "${got%% *}" in
			e)
				if [ "$1" = expires ]; then
					echo "$expiry"
					exit 0
				fi
				echo "$2 holds the Hostisan release key $want, but it has expired"
				exit 1
				;;
			r)
				echo "$2 holds the Hostisan release key $want, but it has been revoked"
				exit 1
				;;
		esac
		if [ "$1" = expires ]; then
			echo "$expiry"
		fi
		;;
	*) usage ;;
esac
