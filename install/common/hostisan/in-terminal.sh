#!/usr/bin/env bash
# Usage: in-terminal.sh [PID]
#
# Exits 0 when the caller runs from a shell of the panel's web terminal, and 1
# otherwise; 2 = usage. Such a process dies with the terminal: when that unit
# restarts, and when hostisan.service or hestia.service (hestia-nginx) restart,
# since every terminal session goes through both and the shell gets SIGHUP
# then. restart-unit.sh, upgrade_restart_services (func/upgrade.sh) and
# v-update-sys-hestia(-all), which then run in a unit of their own
# (run-detached.sh), ask this script.
#
# The caller is in the terminal when its cgroup, or the cgroup of one of its
# ancestors, is hestia-web-terminal.service or below it. plain sudo keeps the
# cgroup, but `sudo -i`, `su -` and any PAM stack with pam_systemd move the
# root shell into user-0.slice/session-N.scope while it is still a child of
# the terminal's shell (rc6 P2), so the parents are walked up through the PPid
# line of /proc/<pid>/status, up to pid 1 (a unit of its own, as systemd-run
# starts, is pid 1's child). A gone or unreadable entry, a loop or more than
# 256 steps end the walk with 1.
#
# With PID, that process and its ancestors are checked. Without, the caller's
# own cgroup (HOSTISAN_CGROUP_FILE, default /proc/self/cgroup) and then its
# parent ($PPID) and theirs. HOSTISAN_PROC_DIR (default /proc) points the tests
# at a fake process table. A v1 or v2 cgroup line is "N:controllers:/path",
# and the path is the unit or below it. Run as root: with /proc mounted
# hidepid=invisible, other users see only their own processes.
set -uo pipefail

proc=${HOSTISAN_PROC_DIR:-/proc}

# in_unit FILE: the cgroup file FILE puts its process in the terminal's unit.
in_unit() {
	grep -Eq '^[^:]*:[^:]*:(/.*)?/hestia-web-terminal\.service(/.*)?$' "$1" 2> /dev/null
}

# parent_of PID: prints PID's parent from its status file.
parent_of() {
	local key value
	while IFS=$':\t ' read -r key value; do
		if [ "$key" = PPid ]; then
			echo "$value"
			return 0
		fi
	done 2> /dev/null < "$proc/$1/status"
	return 1
}

is_pid() {
	case "$1" in
		'' | *[!0-9]*) return 1 ;;
	esac
}

if [ $# -gt 1 ] || { [ $# -eq 1 ] && ! is_pid "$1"; }; then
	echo "Usage: in-terminal.sh [PID]" >&2
	exit 2
fi
if [ $# -eq 1 ]; then
	pid=$1
else
	in_unit "${HOSTISAN_CGROUP_FILE:-/proc/self/cgroup}" && exit 0
	pid=$PPID
fi

seen=' ' steps=0
while is_pid "$pid" && [ "$pid" -gt 1 ] && [ "$steps" -lt 256 ]; do
	case "$seen" in
		*" $pid "*) exit 1 ;;
	esac
	seen="$seen$pid "
	steps=$((steps + 1))
	in_unit "$proc/$pid/cgroup" && exit 0
	pid=$(parent_of "$pid") || exit 1
done
exit 1
