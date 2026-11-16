#!/usr/bin/env bash
# Usage: sudoers.sh
#
# Installs /etc/sudoers.d/hestiaweb from install/common/sudo/hestiaweb. The
# installers run it, and setup.sh runs it on every upgrade, so a box that came
# from upstream Hestia by package upgrade also gets `!log_allowed` (O6).
# Every upgrade undoes local edits to /etc/sudoers.d/hestiaweb: put local
# rules in another file under /etc/sudoers.d instead.
#
# sudo-rs (Ubuntu 26.04) rejects `!requiretty` and `!log_allowed`; when
# `sudo -V` names sudo-rs, both lines are dropped. hestiaweb's sudo command
# lines are then logged; /proc stays hidden (hidepid).
#
# The file is written to a temp file in /etc/sudoers.d whose name has a dot
# (sudo skips such names), checked with `visudo -c -f`, made 0440 and renamed
# over the live file. A file visudo refuses is never installed: the old one
# stays and the exit status is 1. When the live file already has that content,
# mode and owner, it is left alone and nothing is printed. Idempotent.
set -uo pipefail

src=/usr/local/hestia/install/common/sudo/hestiaweb
dir=/etc/sudoers.d
dst=$dir/hestiaweb

fail() {
	echo "[ ! ] Hostisan: $*" >&2
	exit 1
}

[ -r "$src" ] || fail "missing $src"
command -v visudo > /dev/null || fail "no visudo: $dst is not installed"
mkdir -p "$dir" || fail "could not create $dir"

tmp=$(mktemp "$dir/.hestiaweb.XXXXXX") || fail "could not create a temp file in $dir"
trap 'rm -f "$tmp"' EXIT

cp "$src" "$tmp" || fail "could not copy $src"
if sudo -V 2> /dev/null | head -n1 | grep -qi 'sudo-rs'; then
	sed -i -e '/^Defaults:root !requiretty$/d' -e '/^Defaults:hestiaweb !log_allowed$/d' "$tmp" ||
		fail "could not edit $tmp"
fi
chmod 0440 "$tmp" || fail "could not chmod $tmp"
# Already in place (same content, mode and owner): nothing to do or say.
if [ -f "$dst" ] && [ ! -L "$dst" ] && cmp -s "$tmp" "$dst" &&
	[ "$(stat -c '%a %u %g' "$dst")" = "$(stat -c '%a %u %g' "$tmp")" ]; then
	exit 0
fi
if ! visudo -c -q -f "$tmp" > /dev/null 2>&1; then
	fail "visudo refused the new hestiaweb sudoers file; $dst is unchanged"
fi
mv -f "$tmp" "$dst" || fail "could not install $dst"
echo "[ * ] Hostisan: installed $dst"
