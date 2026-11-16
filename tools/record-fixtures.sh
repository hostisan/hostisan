#!/usr/bin/env bash
# Records real Hestia command output as Hostisan fixtures.
# Run as root on a disposable Hestia VPS:  record-fixtures.sh <manifest> <outdir>
# Each manifest line is "<v-command> <args...>", split with shell-style quoting: '' is an
# empty argument and 'a b' is one argument. Output is appended to <outdir>/<cmd>.json as
# {"args": [...], "stdout": "...", "stderr": "...", "exit": N}, with stdout and stderr kept
# byte for byte and "stderr" left out when it is empty.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <manifest> <outdir>" >&2
	exit 2
fi
manifest=$1
outdir=$2
# HESTIA_BIN points at fake commands when testing this script; then root isn't required.
bin=${HESTIA_BIN:-/usr/local/hestia/bin}

command -v jq >/dev/null || {
	echo "jq is required" >&2
	exit 1
}
[[ $EUID -eq 0 || -n ${HESTIA_BIN:-} ]] || {
	echo "run as root" >&2
	exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# split_line LINE: writes the line's words, NUL-separated, to $tmp/argv.
split_line() {
	xargs printf '%s\0' <<<"$1" >"$tmp/argv" 2>/dev/null
}

# Check every line first so a typo can't stop the run halfway through the commands.
n=0
while IFS= read -r line || [[ -n $line ]]; do
	n=$((n + 1))
	[[ -z "${line//[[:space:]]/}" || $line =~ ^[[:space:]]*# ]] && continue
	split_line "$line" || {
		echo "$manifest: line $n: unmatched quote" >&2
		exit 1
	}
done <"$manifest"

mkdir -p "$outdir"
while IFS= read -r line || [[ -n $line ]]; do
	[[ -z "${line//[[:space:]]/}" || $line =~ ^[[:space:]]*# ]] && continue
	split_line "$line"
	mapfile -d '' parts <"$tmp/argv"
	cmd=${parts[0]}
	args=("${parts[@]:1}")
	if [[ ! $cmd =~ ^v-[a-z0-9-]+$ || ! -x $bin/$cmd ]]; then
		echo "skip: $cmd is not a Hestia command" >&2
		continue
	fi
	set +e
	timeout 300 "$bin/$cmd" "${args[@]}" </dev/null >"$tmp/out" 2>"$tmp/err"
	code=$?
	set -e
	if [[ $code -eq 124 ]]; then
		echo "timeout: $line (not recorded)" >&2
		continue
	fi
	file="$outdir/$cmd.json"
	[[ -f $file ]] || echo '[]' >"$file"
	jq --rawfile out "$tmp/out" --rawfile err "$tmp/err" --argjson exit "$code" \
		'. + [{args: $ARGS.positional, stdout: $out}
			+ (if $err == "" then {} else {stderr: $err} end)
			+ {exit: $exit}]' \
		"$file" --args -- "${args[@]}" >"$file.tmp"
	mv "$file.tmp" "$file"
	echo "recorded: $line (exit $code)"
done <"$manifest"
