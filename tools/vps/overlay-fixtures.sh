#!/usr/bin/env bash
# Merges recorded fixtures over the dev fixtures.
#
#   overlay-fixtures.sh <dev> <recorded> <out>
#
# For every <cmd>.json in either input, <out>/<cmd>.json holds the recorded entries first
# and then the dev entries. The fixture runner takes the first entry whose args match, so
# recorded output wins on equal args, while commands that were never recorded (login, auth,
# anything with a secret) still answer from dev. Inputs are never modified.
set -euo pipefail

if [[ $# -ne 3 ]]; then
	echo "usage: $0 <dev> <recorded> <out>" >&2
	exit 2
fi
dev=$1
rec=$2
out=$3

command -v jq >/dev/null || {
	echo "jq is required" >&2
	exit 1
}
for d in "$dev" "$rec"; do
	[[ -d $d ]] || {
		echo "not a directory: $d" >&2
		exit 1
	}
done
mkdir -p "$out"
outabs=$(cd "$out" && pwd -P)
for d in "$dev" "$rec"; do
	if [[ $(cd "$d" && pwd -P) == "$outabs" ]]; then
		echo "out must differ from the inputs: $out" >&2
		exit 1
	fi
done

empty=$(mktemp)
trap 'rm -f "$empty"' EXIT
echo '[]' >"$empty"

n=0
while IFS= read -r name; do
	d=$dev/$name
	r=$rec/$name
	[[ -f $d ]] || d=$empty
	[[ -f $r ]] || r=$empty
	jq -s '.[0] + .[1]' "$r" "$d" >"$out/$name.tmp"
	mv "$out/$name.tmp" "$out/$name"
	n=$((n + 1))
done < <(
	find "$dev" "$rec" -maxdepth 1 -type f -name '*.json' -printf '%f\n' | sort -u
)
echo "overlay: $n command files written to $out"
