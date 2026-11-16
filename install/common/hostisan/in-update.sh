#!/usr/bin/env bash
# Usage: in-update.sh
#
# Exits 0 when the caller runs inside a Hestia package update, and 1
# otherwise; 2 = usage. restart-unit.sh then defers the panel, hestia and
# terminal restarts until the update is done, so each restarts once, after the
# last package, instead of once per package script: one update configures
# hestia (setup.sh and upgrade_restart_services), hestia-nginx, hestia-php and
# hestia-web-terminal, and every restart of the three hangs up every web
# terminal (rc7: hestia restarted three times and hostisan once, mid-loop).
#
# An update is:
#   - v-update-sys-hestia's apt run, which exports HOSTISAN_UPDATE=1 (the
#     nightly cron, ssh, and the detached unit; apt and dpkg pass it on to
#     the maintainer scripts);
#   - HOSTISAN_DETACHED, the marker run-detached.sh sets for an update it runs;
#   - a process in a hostisan-update-*.service unit (run-detached.sh), even if
#     the environment was lost on the way. HOSTISAN_CGROUP_FILE (default
#     /proc/self/cgroup) points the tests at a fake.
# A raw "apt upgrade" is not an update: its restarts stay in line, as upstream
# (in-terminal.sh still defers them when it is typed in the web terminal).
set -uo pipefail

if [ $# -ne 0 ]; then
	echo "Usage: in-update.sh" >&2
	exit 2
fi
[ -n "${HOSTISAN_UPDATE:-}" ] && exit 0
[ -n "${HOSTISAN_DETACHED:-}" ] && exit 0
grep -Eq '^[^:]*:[^:]*:(/.*)?/hostisan-update-[^/]+\.service(/.*)?$' \
	"${HOSTISAN_CGROUP_FILE:-/proc/self/cgroup}" 2> /dev/null
