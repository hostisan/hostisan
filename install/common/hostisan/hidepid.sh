#!/usr/bin/env bash
# Usage: hidepid.sh [FSTAB]               (default /etc/fstab)
#        hidepid.sh --rollback [FSTAB]
#
# Mounts /proc with hidepid=invisible so users see only their own processes:
# a hosting user can no longer read the command lines of root's or hestiaweb's
# processes (access-key secrets, passwords passed to v-* scripts; Plan 6 O6).
#
# Members of the group hostisan-proc still see every process:
#   - systemd-logind (runs as root but without CAP_SYS_PTRACE; gets the group
#     through a drop-in), so ssh logins, loginctl and sessions keep working;
#   - polkitd, when that user exists (Ubuntu 24.04+), so polkit can check the
#     processes asking it for permission (systemctl, pkexec);
#   - hestiaweb, which already runs every v-* script as root through sudo.
# Root, cron and Hestia's root-run ps checks are not affected.
#
# The script is idempotent: it replaces any /proc line in FSTAB with
#   proc /proc proc nosuid,nodev,noexec,relatime,hidepid=invisible,gid=hostisan-proc 0 0
# and remounts /proc at once with the same options. nosuid,nodev,noexec are
# the stock /proc flags; they are spelled out because `defaults` (or a remount
# that merges the fstab line) would turn them off. An older Hostisan line with
# `defaults` is replaced. It also removes upstream Hestia's
# /etc/cron.d/hestia-proc, whose @reboot `hidepid=2` remount would drop the
# exemption group at every boot.
#
# In a container (systemd-detect-virt -c) it changes nothing and exits 0:
# /proc can't be remounted there (LXC needs host AppArmor changes).
#
# Rollback (also printed at the end of a run):
#   hidepid.sh --rollback
# or by hand:
#   sed -i '/hidepid=invisible,gid=hostisan-proc/d' /etc/fstab
#   rm -f /etc/systemd/system/systemd-logind.service.d/hostisan-proc.conf
#   systemctl daemon-reload && systemctl try-restart systemd-logind.service
#   mount -o remount,nosuid,nodev,noexec,relatime,hidepid=off,gid=0 /proc
# which leaves /proc with its stock options (rw,nosuid,nodev,noexec,relatime).
# Neither fstab nor cron hides /proc again at the next boot. The hostisan-proc
# group is kept; it grants nothing without the mount option.
#
# When the group, its members, the drop-in, the fstab line and the cron file
# are already as they should be and /proc is mounted with these options, it
# restarts and remounts nothing (the hestia postinst runs it on every upgrade).
#
# If restarting logind or polkit fails, it warns, leaves /proc as it is and
# exits 1 (setup.sh reports it), and the next run restarts and remounts again
# (/run/hostisan-hidepid.pending), even though everything else is in place.
#
# HOSTISAN_SYSTEMD_DIR overrides /etc/systemd/system, HOSTISAN_PROC_CRON
# /etc/cron.d/hestia-proc, HOSTISAN_PROC_MOUNTS /proc/mounts and
# HOSTISAN_HIDEPID_PENDING /run/hostisan-hidepid.pending (tests).
set -euo pipefail

group=hostisan-proc
systemd_dir=${HOSTISAN_SYSTEMD_DIR:-/etc/systemd/system}
dropin_dir=$systemd_dir/systemd-logind.service.d
dropin=$dropin_dir/$group.conf
proc_cron=${HOSTISAN_PROC_CRON:-/etc/cron.d/hestia-proc}
proc_mounts=${HOSTISAN_PROC_MOUNTS:-/proc/mounts}
# Left by a run whose logind/polkit restart failed. In /run: a reboot starts
# them with the group anyway.
pending=${HOSTISAN_HIDEPID_PENDING:-/run/hostisan-hidepid.pending}
# Stock /proc flags first, so neither fstab nor a remount turns them off.
proc_flags=nosuid,nodev,noexec,relatime
proc_line="proc /proc proc $proc_flags,hidepid=invisible,gid=$group 0 0"

rollback=no
if [ "${1:-}" = "--rollback" ]; then
	rollback=yes
	shift
fi
fstab=${1:-/etc/fstab}

if [ "$(id -u)" != 0 ]; then
	echo "hidepid.sh: must run as root" >&2
	exit 1
fi
if [ ! -f "$fstab" ]; then
	echo "hidepid.sh: no such fstab: $fstab" >&2
	exit 1
fi

if [ "$rollback" = no ] && command -v systemd-detect-virt >/dev/null &&
	virt=$(systemd-detect-virt --container); then
	echo "hidepid: skipped, running in a container ($virt): /proc can't be remounted here"
	exit 0
fi

# changed=yes once this run changes anything logind, polkit or the mount
# depend on.
changed=no

# write_fstab AWK_PROGRAM: rewrite FSTAB in place (same inode, owner and mode).
tmp=
trap 'rm -f "$tmp"' EXIT
write_fstab() {
	tmp=$(mktemp "$fstab.hostisan.XXXXXX")
	awk "$1" proc_line="$proc_line" "$fstab" >"$tmp"
	if ! cmp -s "$tmp" "$fstab"; then
		cat "$tmp" >"$fstab"
		changed=yes
	fi
	rm -f "$tmp"
	tmp=
}

if [ "$rollback" = yes ]; then
	# Ours, and the `defaults` line older Hostisan builds wrote.
	# shellcheck disable=SC2016 # awk program
	write_fstab '!($2 == "/proc" && $4 ~ /(^|,)gid=hostisan-proc(,|$)/)'
	rm -f "$dropin" "$proc_cron" "$pending"
	rmdir "$dropin_dir" 2>/dev/null || true
	systemctl daemon-reload
	systemctl try-restart systemd-logind.service || true
	mount -o "remount,$proc_flags,hidepid=off,gid=0" /proc
	echo "hidepid: /proc is mounted without hidepid again"
	exit 0
fi

if ! getent group "$group" >/dev/null; then
	groupadd --system "$group"
	changed=yes
fi
gid=$(getent group "$group" | cut -d: -f3)

# id -nG: the user's groups, primary one included (getent's member list has
# only the supplementary members).
for user in hestiaweb polkitd; do
	if getent passwd "$user" >/dev/null && [[ " $(id -nG "$user") " != *" $group "* ]]; then
		usermod -aG "$group" "$user"
		changed=yes
	fi
done

if [ -e "$proc_cron" ]; then
	rm -f "$proc_cron"
	changed=yes
fi
dropin_text=$(printf '[Service]\nSupplementaryGroups=%s' "$group")
if [ "$(cat "$dropin" 2>/dev/null)" != "$dropin_text" ]; then
	mkdir -p "$dropin_dir"
	printf '%s\n' "$dropin_text" >"$dropin"
	changed=yes
fi

# Drop every other /proc entry, then add ours once at the end.
# shellcheck disable=SC2016 # awk program
write_fstab '
	$0 == proc_line { next }
	$1 !~ /^#/ && $2 == "/proc" && $3 == "proc" { next }
	{ print }
	END { print proc_line }'

# proc_mounted: /proc is mounted now with every option of ours (the kernel
# lists them in its own order).
proc_mounted() {
	local opts want
	opts=,$(awk '$2 == "/proc" && $3 == "proc" { o = $4 } END { print o }' "$proc_mounts" 2>/dev/null),
	for want in ${proc_flags//,/ } hidepid=invisible "gid=$gid"; do
		if [ "$want" = hidepid=invisible ] && [[ "$opts" == *,hidepid=2,* ]]; then
			continue
		fi
		[[ "$opts" == *",$want,"* ]] || return 1
	done
}

if [ "$changed" = no ] && [ ! -e "$pending" ] && proc_mounted; then
	echo "hidepid: /proc is already mounted hidepid=invisible (exempt group $group); nothing to do"
	exit 0
fi

# logind and polkit must hold the group before /proc is hidden from them. Only
# the units that exist (no polkit on Debian 12 without it). When the restart
# fails, /proc is not remounted and $pending makes the next run (the next
# upgrade) restart again, though all else is in place by then.
systemctl daemon-reload
units=()
for unit in systemd-logind.service polkit.service; do
	if [ "$(systemctl show -p LoadState --value "$unit" 2>/dev/null)" = loaded ]; then
		units+=("$unit")
	fi
done
if [ "${#units[@]}" -gt 0 ] && ! systemctl try-restart "${units[@]}"; then
	: >"$pending" 2>/dev/null || true
	echo "hidepid.sh: could not restart ${units[*]}: /proc is not remounted; run hidepid.sh again (the next upgrade does)" >&2
	exit 1
fi
rm -f "$pending"
# The numeric gid: the running kernel takes no group names.
mount -o "remount,$proc_flags,hidepid=invisible,gid=$gid" /proc

echo "hidepid: /proc is mounted hidepid=invisible (exempt group $group)"
echo "hidepid: rollback with: $0 --rollback${1:+ $fstab}"
