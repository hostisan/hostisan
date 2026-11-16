#!/usr/bin/env bash
# Usage: update-running.sh [PID]
#
# Exits 0 when a Hestia update script runs: v-update-sys-hestia-all or
# v-update-sys-hestia, as the command (argv[0]) or as the script a shell runs
# (argv[1]), or run-detached.sh --run, which runs a detached update in its
# unit and writes the exit status the web terminal waits for after it. With
# PID, only that process is checked. Otherwise every process but this one is,
# and 1 means none runs; 2 = usage.
#
# restart-unit.sh --deferred waits while one runs: an older loop (rc4 and
# before) runs in the web terminal and dpkg is idle between its packages, so a
# restart then would hang the loop up before its last package.
# run-detached.sh runs an update in line when its caller's parent is one (an
# older loop calling the new v-update-sys-hestia).
#
# A process that only names the scripts (grep, an editor, the log follower of
# run-detached.sh, which has no --run, v-update-sys-hestia-git) does not count,
# nor does sudo:
# "sudo v-update-sys-hestia-all" typed in the terminal is the parent of the
# new script, which detaches, and the update it runs is a process of its own.
# HOSTISAN_PROC_DIR (default /proc) points the tests at a fake process table.
# Run as root: with /proc mounted hidepid=invisible, other users see only
# their own processes.
set -uo pipefail

proc=${HOSTISAN_PROC_DIR:-/proc}

# is_update FILE: the cmdline FILE (NUL-separated arguments) runs an update.
is_update() {
	local -a argv=()
	local a
	# the process may be gone (or be a kernel thread, with no arguments)
	while [ "${#argv[@]}" -lt 3 ] && { IFS= read -r -d '' a || [ -n "$a" ]; }; do
		argv+=("$a")
	done 2> /dev/null < "$1"
	[ "${#argv[@]}" -gt 0 ] || return 1
	case "${argv[0]##*/}" in
		v-update-sys-hestia | v-update-sys-hestia-all) return 0 ;;
		bash | sh | dash) ;;
		*) return 1 ;;
	esac
	case "${argv[1]:-}" in
		*/v-update-sys-hestia | */v-update-sys-hestia-all | v-update-sys-hestia | v-update-sys-hestia-all) return 0 ;;
		*/run-detached.sh) [ "${argv[2]:-}" = --run ] && return 0 ;;
	esac
	return 1
}

if [ $# -gt 1 ]; then
	echo "Usage: update-running.sh [PID]" >&2
	exit 2
fi
if [ $# -eq 1 ]; then
	case "$1" in
		'' | *[!0-9]*)
			echo "Usage: update-running.sh [PID]" >&2
			exit 2
			;;
	esac
	is_update "$proc/$1/cmdline"
	exit
fi

for f in "$proc"/[0-9]*/cmdline; do
	[ "$f" = "$proc/$$/cmdline" ] && continue
	is_update "$f" && exit 0
done
exit 1
