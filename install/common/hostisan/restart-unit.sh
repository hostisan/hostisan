#!/usr/bin/env bash
# Usage: restart-unit.sh UNIT restart|try-restart
#        restart-unit.sh UNIT --deferred restart|try-restart   (the transient unit)
#
# Restarts UNIT for the package scripts: hostisan.service (the panel),
# hestia.service (hestia-nginx and php-fpm) or hestia-web-terminal.service.
# Every web-terminal shell lives in the terminal unit's cgroup, and its
# session goes through hestia-nginx and the panel. So when this runs from one
# (an admin typed `apt upgrade` in the panel's terminal), restarting any of
# the three in line kills or hangs up the shell, apt, dpkg and the very script
# asking for it, leaving the packages half-configured (in-terminal.sh tells).
# Then the restart goes to a transient unit outside that cgroup,
# hostisan-<panel|hestia|terminal>-restart, and a notice says the terminal
# will close. Until then the old process keeps serving.
#
# Inside an update (in-update.sh: v-update-sys-hestia's apt run, cron or ssh,
# or the detached hostisan-update unit) the restart is deferred the same way,
# so that hostisan, hestia and the terminal each restart once, after the last
# package: one update asks for hestia four times (setup.sh,
# upgrade_restart_services, the nginx and php postinsts) and for the others
# twice, and each in-line restart hung up every open terminal (rc7). There a
# restart that cannot be scheduled happens in line instead.
#
# The transient unit starts after 30 s and runs this script with --deferred,
# which waits while apt or dpkg hold their locks (the rest of the upgrade can
# take longer than 30 s) or while v-update-sys-hestia(-all) runs
# (update-running.sh), for an hour at most, then restarts UNIT. dpkg is idle
# between the packages of an update loop, and an older loop (rc4 and before)
# runs in the web terminal itself: a restart between two packages would hang
# it up. After an update it waits 3 s more, so the terminal following a
# detached update prints its exit status before the restart hangs it up. The
# locks are fcntl locks, which flock(1) does not see, so /proc/locks is read.
#
# The deferred run does the strongest mode any caller asked for. setup.sh asks
# hestia for try-restart first, and func/upgrade.sh and the nginx/php
# postinsts ask for restart later; they find the timer and leave it, and
# try-restart does nothing for a stopped or failed unit, so an update used to
# leave a stopped hestia stopped. The mode goes to $run_dir/NAME.mode
# (root-only, on /run so a reboot clears it): written when the restart is
# scheduled, raised to restart by a later caller that asks for it, read and
# removed by the deferred run just before it restarts. Not /run/hostisan:
# that is hostisan.service's runtime directory, hestiaweb's, removed when the
# panel stops.
#
# HOSTISAN_CGROUP_FILE (in-terminal.sh, in-update.sh), HOSTISAN_DPKG_DIR (/var/lib/dpkg),
# HOSTISAN_PROC_LOCKS (/proc/locks) and HOSTISAN_PROC_DIR (update-running.sh,
# in-terminal.sh) and HOSTISAN_RESTART_DIR (/run/hostisan-restart) point the
# tests at fakes. Exit 0 = restarted or scheduled,
# 1 = failed (a warning is printed), 2 = usage.
set -uo pipefail

dpkg_dir=${HOSTISAN_DPKG_DIR:-/var/lib/dpkg}
proc_locks=${HOSTISAN_PROC_LOCKS:-/proc/locks}
run_dir=${HOSTISAN_RESTART_DIR:-/run/hostisan-restart}
max_wait=3600

usage() {
	echo "Usage: restart-unit.sh hostisan.service|hestia.service|hestia-web-terminal.service [--deferred] restart|try-restart" >&2
	exit 2
}

unit=${1:-}
case "$unit" in
	hostisan.service) name=hostisan-panel-restart ;;
	hestia.service) name=hostisan-hestia-restart ;;
	hestia-web-terminal.service) name=hostisan-terminal-restart ;;
	*) usage ;;
esac
shift
deferred=no
if [ "${1:-}" = --deferred ]; then
	deferred=yes
	shift
fi
mode=${1:-}
case "$mode" in
	restart | try-restart) ;;
	*) usage ;;
esac

# dpkg_busy: apt (lock-frontend) or dpkg (lock) holds its lock. /proc/locks
# lines name the locked file as MAJOR:MINOR:INODE.
dpkg_busy() {
	local f ino
	for f in "$dpkg_dir/lock-frontend" "$dpkg_dir/lock"; do
		ino=$(stat -L -c %i -- "$f" 2> /dev/null) || continue
		grep -Eq "(^|[[:space:]])[0-9a-fA-F]+:[0-9a-fA-F]+:$ino([[:space:]]|$)" "$proc_locks" 2> /dev/null && return 0
	done
	return 1
}

mode_file=$run_dir/$name.mode

# write_mode MODE: record MODE for the deferred run (written whole, renamed).
write_mode() {
	if [ ! -d "$run_dir" ]; then
		(umask 077 && mkdir -p -- "$run_dir") 2> /dev/null || return 1
	fi
	printf '%s\n' "$1" > "$mode_file.$$" 2> /dev/null && mv -f -- "$mode_file.$$" "$mode_file" 2> /dev/null
}

self=$(realpath -- "${BASH_SOURCE[0]}")
here=$(dirname -- "$self")

if [ "$deferred" = yes ]; then
	waited=0 settle=0
	while [ "$waited" -lt "$max_wait" ]; do
		if dpkg_busy; then
			:
		elif "$here/update-running.sh"; then
			settle=3
		else
			break
		fi
		sleep 5
		waited=$((waited + 5))
	done
	[ "$settle" -gt 0 ] && sleep "$settle"
	# a caller after the one that scheduled this may have asked for more
	if [ "$(cat -- "$mode_file" 2> /dev/null)" = restart ]; then
		mode=restart
	fi
	rm -f -- "$mode_file"
	exec systemctl "$mode" "$unit"
fi

why=
if "$here/in-terminal.sh"; then
	why=terminal
	when="once dpkg is done; this terminal closes then"
elif "$here/in-update.sh"; then
	why=update
	when="once this update is done"
fi

if [ -n "$why" ]; then
	# One upgrade configures hestia and hestia-web-terminal, so both ask (and
	# an update asks for hestia four times); the later ones find the timer (or
	# the service waiting for dpkg) active and leave it.
	if systemctl is-active --quiet "$name.timer" || systemctl is-active --quiet "$name.service"; then
		# never lower it: restart wins over try-restart
		if [ "$mode" = restart ] && ! write_mode restart; then
			echo "[ ! ] Hostisan: could not ask the scheduled $unit restart to start it if it is stopped" >&2
		fi
		if [ "$why" = terminal ]; then
			echo "[ * ] Hostisan: the $unit restart is already scheduled; this terminal closes then"
		else
			echo "[ * ] Hostisan: the $unit restart is already scheduled for the end of this update"
		fi
		exit 0
	fi
	# A failed unit of that name (left by an earlier upgrade) would make
	# systemd-run refuse the name.
	systemctl reset-failed "$name.service" "$name.timer" 2> /dev/null || true
	# a file left by a run that never came (no unit is scheduled now) is replaced
	write_mode "$mode" || rm -f -- "$mode_file"
	# --collect: the transient units go away once done, even when they failed,
	# so the next upgrade can use the same name. RemainAfterElapse=no: an
	# elapsed timer would stay "active (elapsed)" and make the next upgrade
	# think a restart is still scheduled. The command goes to a script, not
	# "sh -c": systemd expands $VARS in a transient unit's command line.
	if systemd-run --on-active=30 --unit="$name" --collect \
		--timer-property=RemainAfterElapse=no /bin/bash "$self" "$unit" --deferred "$mode" > /dev/null 2>&1; then
		if [ "$why" = terminal ]; then
			echo "[ * ] Hostisan: this runs in the web terminal, so $unit restarts $when"
		else
			echo "[ * ] Hostisan: $unit restarts $when"
		fi
		exit 0
	fi
	rm -f -- "$mode_file"
	if [ "$why" = terminal ]; then
		echo "[ ! ] Hostisan: could not schedule the $unit restart; run \"systemctl $mode $unit\" from outside the web terminal" >&2
		exit 1
	fi
	# outside the terminal an in-line restart only costs the open terminals
	echo "[ ! ] Hostisan: could not schedule the $unit restart; restarting it now" >&2
fi

if ! systemctl "$mode" "$unit"; then
	echo "[ ! ] Hostisan: $unit did not restart (journalctl -u ${unit%.service})" >&2
	exit 1
fi
