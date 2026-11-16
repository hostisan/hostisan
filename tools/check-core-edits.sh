#!/usr/bin/env bash
# Usage: tools/check-core-edits.sh [BASE]
#
# Fails when HEAD changes a Hestia core file (bin func install src web) that is
# not listed in the "## Allowed edits" table of hostisan/CORE-EDITS.md, or when
# that table lists a path HEAD does not change ("stale entry"). BASE defaults to
# the upstream fork point 94c1e760f. Rows under "## Planned edits" allow nothing.
#
# Runs on the repository holding the current directory, and reads the table as
# committed in HEAD. Exit 0 = clean, 1 = violations, 2 = usage or setup error.
set -euo pipefail

base=${1:-94c1e760f}
core_dirs=(bin func install src web)
table=hostisan/CORE-EDITS.md

root=$(git rev-parse --show-toplevel 2> /dev/null) || {
	echo "check-core-edits: not inside a git repository" >&2
	exit 2
}
cd "$root"

if ! git rev-parse -q --verify "$base^{commit}" > /dev/null; then
	echo "check-core-edits: base commit not found: $base (fetch full history)" >&2
	exit 2
fi
if ! doc=$(git show "HEAD:$table" 2> /dev/null); then
	echo "check-core-edits: $table is missing from HEAD" >&2
	exit 2
fi

# First-column paths of the table under "## Allowed edits", backticks removed.
allowed=$(awk -F'|' '
	/^## / { in_allowed = ($0 ~ /^## Allowed edits[[:space:]]*$/); next }
	in_allowed && /^\|/ {
		p = $2
		gsub(/[`[:space:]]/, "", p)
		if (p == "" || p == "path" || p ~ /^[-:]+$/) next
		print p
	}' <<< "$doc" | sort -u)

changed=$(git diff --no-renames --name-only "$base" HEAD -- "${core_dirs[@]}" | sort -u)

fail=0
while IFS= read -r path; do
	[ -n "$path" ] || continue
	echo "unlisted core edit: $path"
	fail=1
done < <(comm -23 <(printf '%s\n' "$changed") <(printf '%s\n' "$allowed"))

while IFS= read -r path; do
	[ -n "$path" ] || continue
	echo "stale entry: $path (listed in $table but unchanged since $base)"
	fail=1
done < <(comm -13 <(printf '%s\n' "$changed") <(printf '%s\n' "$allowed"))

exit "$fail"
