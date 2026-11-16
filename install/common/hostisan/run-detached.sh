#!/usr/bin/env bash
# Usage: run-detached.sh COMMAND [ARG...]
#        run-detached.sh --run UNIT LOG COMMAND [ARG...]   (inside the unit)
#
# Runs an update started from the panel's web terminal (v-update-sys-hestia-all
# or v-update-sys-hestia, when in-terminal.sh says so) in a transient unit of
# its own, hostisan-update-<date>-<time>-<pid>-<random>, so nothing that
# happens to the terminal or the panel can kill it. The hestia postinst restarts the panel,
# which every terminal session goes through: that hung up the shell and the
# update with it, before the terminal package was updated (rc5 P1).
#
# The unit runs COMMAND with HOSTISAN_DETACHED=1 (the scripts then neither
# detach again nor hide apt's output) and appends its output to
# /var/log/hestia/<unit>.log, then a last line with the exit status. This
# terminal prints the unit, the log and a notice, follows the log until that
# line comes and exits with that status. The panel, hestia and terminal
# restarts the update asks for wait until it has ended and written that line
# (in-update.sh, restart-unit.sh), so the status shows before they close this
# terminal. If it closes anyway, the update carries on; ^C only stops
# following.
# When systemd-run fails, or the log name is taken (another update's log is
# never written over or removed), COMMAND runs here as before, with a warning.
#
# Called by an older update loop (rc4 and before: v-update-sys-hestia-all with
# no detach of its own, already running when the new hestia lands, calls the
# new v-update-sys-hestia; update-running.sh tells), COMMAND runs here too. In
# a unit of its own, the nginx and php postinsts would restart hestia in line
# and hang that loop up; here the restarts wait until the loop ends
# (restart-unit.sh).
#
# HOSTISAN_LOG_DIR (default /var/log/hestia) points the tests at a temp dir.
# Exit: COMMAND's status; 1 when the unit ended without one; 2 = usage.
set -uo pipefail

log_dir=${HOSTISAN_LOG_DIR:-/var/log/hestia}

if [ "${1:-}" = --run ]; then
	[ $# -ge 4 ] || exit 2
	unit=$2
	log=$3
	shift 3
	exec >> "$log" 2>&1 || exit 1
	export HOSTISAN_DETACHED=1
	"$@"
	rc=$?
	echo "[ * ] Hostisan: $unit finished, exit status $rc"
	exit "$rc"
fi

if [ $# -lt 1 ]; then
	echo "Usage: run-detached.sh COMMAND [ARG...]" >&2
	exit 2
fi

self=$(realpath -- "${BASH_SOURCE[0]}")
here=$(dirname -- "$self")

if "$here/update-running.sh" "$PPID"; then
	echo "[ * ] Hostisan: an older update loop runs this, so it runs here; the panel and terminal restarts wait until that loop ends"
	export HOSTISAN_DETACHED=1
	exec "$@"
fi

# The pid and a random number: two updates started in the same second must
# not share a unit or a log.
unit=hostisan-update-$(date +%Y%m%d-%H%M%S)-$$-$RANDOM
log=$log_dir/$unit.log
created=no

# run_here: the old way, in this terminal (the marker stops a second detach).
run_here() {
	echo "[ ! ] Hostisan: could not start $unit; the update runs here, and this terminal may close part-way" >&2
	[ "$created" = yes ] && rm -f -- "$log"
	export HOSTISAN_DETACHED=1
	exec "$@"
}

# noclobber: O_EXCL, so a log that exists (another update's) is not taken.
(set -C && umask 077 && : > "$log") 2> /dev/null || run_here "$@"
created=yes
# A script, not "sh -c": systemd expands $VARS in a transient command line.
systemd-run --unit="$unit" --collect --quiet \
	--setenv=HOSTISAN_DETACHED=1 --setenv=DEBIAN_FRONTEND=noninteractive \
	/bin/bash "$self" --run "$unit" "$log" "$@" || run_here "$@"

echo "[ * ] Hostisan: the update runs in $unit.service, outside the web terminal."
echo "      The panel, hestia and the terminal restart once, after it; this terminal closes then. The update carries on."
echo "      Log: $log (tail -f it; systemctl status $unit while it runs)"
trap 'echo; echo "[ * ] Hostisan: stopped following; $unit carries on (log: $log)"; exit 130' INT

# exit_status: the status line the unit wrote last, if any.
exit_status() {
	sed -n "s/^\[ \* \] Hostisan: $unit finished, exit status \([0-9]\{1,\}\)\$/\1/p" -- "$log" 2> /dev/null | tail -n 1
}

off=0
while :; do
	rc=$(exit_status)
	gone=no
	if [ -z "$rc" ] && ! systemctl is-active --quiet "$unit.service"; then
		# it may have finished just now
		rc=$(exit_status)
		[ -n "$rc" ] || gone=yes
	fi
	size=$(stat -c %s -- "$log" 2> /dev/null) || size=$off
	if [ "$size" -gt "$off" ]; then
		tail -c "+$((off + 1))" -- "$log" | head -c "$((size - off))"
		off=$size
	fi
	[ -n "$rc" ] && exit "$rc"
	if [ "$gone" = yes ]; then
		echo "[ ! ] Hostisan: $unit ended without an exit status; see $log and journalctl -u $unit" >&2
		exit 1
	fi
	sleep 1
done
