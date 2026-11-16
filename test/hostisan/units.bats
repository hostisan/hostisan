#!/usr/bin/env bats
# Static checks for Hostisan's systemd units, the hestiaweb sudoers file, the
# hostisan.env defaults and install/common/hostisan/hidepid.sh (Plan 6, B6).
# systemd-analyze and visudo run when they are installed; nothing here needs root.

bats_require_minimum_version 1.5.0

setup() {
	repo="$BATS_TEST_DIRNAME/../.."
	panel="$repo/src/deb/hestia/hostisan.service"
	term="$repo/src/deb/web-terminal/hestia-web-terminal.service"
	sudoers="$repo/install/common/sudo/hestiaweb"
	envfile="$repo/install/common/hostisan/hostisan.env"
	hidepid="$repo/install/common/hostisan/hidepid.sh"
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/units.XXXXXX")
}

teardown() {
	rm -rf "$work"
}

# ini_values FILE KEY: every value of KEY= in FILE, one per line (comments skipped).
ini_values() {
	awk -v key="$2" '
		/^[[:space:]]*[#;]/ { next }
		{
			line = $0
			sub(/^[[:space:]]+/, "", line)
			eq = index(line, "=")
			if (eq == 0) next
			k = substr(line, 1, eq - 1)
			sub(/[[:space:]]+$/, "", k)
			if (k != key) next
			v = substr(line, eq + 1)
			sub(/^[[:space:]]+/, "", v)
			print v
		}' "$1"
}

# expect FILE KEY VALUE: KEY appears exactly once in FILE, set to VALUE.
expect() {
	local got
	got=$(ini_values "$1" "$2")
	if [ "$got" != "$3" ]; then
		echo "$(basename "$1"): $2: want '$3', got '$got'" >&2
		return 1
	fi
}

# forbid FILE KEY...: none of the KEYs is set in FILE.
forbid() {
	local f=$1 k
	shift
	for k in "$@"; do
		if [ -n "$(ini_values "$f" "$k")" ]; then
			echo "$(basename "$f"): $k must not be set" >&2
			return 1
		fi
	done
}

# verify_unit FILE: systemd-analyze verify on a copy of FILE whose ExecStart
# binary points at a scratch executable, so only the unit itself is judged.
verify_unit() {
	command -v systemd-analyze >/dev/null || skip "systemd-analyze not installed"
	local bin unit
	unit=$work/$(basename "$1")
	bin=$(ini_values "$1" ExecStart | awk '{ print $1 }')
	printf '#!/bin/sh\n' >"$work/fake-bin"
	chmod 755 "$work/fake-bin"
	sed "s#^ExecStart=$bin#ExecStart=$work/fake-bin#" "$1" >"$unit"
	grep -q "^ExecStart=$work/fake-bin" "$unit"
	run systemd-analyze verify --man=no "$unit"
	echo "$output"
	[ "$status" -eq 0 ]
	# systemd only warns (exit 0) about bad lines; any line about the unit fails.
	run ! grep -qF "$unit:" <<<"$output"
}

@test "panel unit keeps sudo working" {
	expect "$panel" User hestiaweb
	expect "$panel" Group hestiaweb
	expect "$panel" RuntimeDirectory hostisan
	expect "$panel" RuntimeDirectoryMode 0700
	# Background jobs survive a restart in /var/lib/hostisan/jobs.json (C6 D2).
	expect "$panel" StateDirectory hostisan
	expect "$panel" StateDirectoryMode 0700
	expect "$panel" ExecStart "/usr/local/hestia/hostisan/hostisan serve --listen unix:/run/hostisan/hostisan.sock"
	expect "$panel" EnvironmentFile -/usr/local/hestia/conf/hostisan.env
	expect "$panel" KillMode mixed
	# Stop waits for running v-* commands (legacy API: up to 300 s).
	expect "$panel" TimeoutStopSec 320
	expect "$panel" Restart always
	# Each of these stops sudo (setuid) or v-make-tmp-file's shared /tmp working.
	forbid "$panel" PrivateTmp NoNewPrivileges ProtectSystem RestrictSUIDSGID \
		CapabilityBoundingSet SystemCallFilter
}

@test "panel listens on unix only" {
	local exec
	exec=$(ini_values "$panel" ExecStart)
	[[ "$exec" == *"--listen "* ]]
	run grep -oE -- '--listen[= ][^ ]+' <<<"$exec"
	[ "$status" -eq 0 ]
	while read -r flag; do
		[[ "${flag#--listen?}" == unix:/* ]] || {
			echo "non-unix listen: $flag" >&2
			return 1
		}
	done <<<"$output"
}

@test "panel unit passes systemd-analyze verify" {
	verify_unit "$panel"
}

@test "terminal unit" {
	expect "$term" User root
	expect "$term" ExecStart "/usr/local/hestia/hostisan/hostisan terminal"
	expect "$term" RuntimeDirectory hestia-web-terminal
	expect "$term" RuntimeDirectoryMode 0755
	# Login shells inherit every one of these, so a user's terminal would have
	# no working sudo/su, a private /tmp or a read-only system.
	forbid "$term" NoNewPrivileges PrivateTmp ProtectSystem ProtectHome
	run ! grep -qiE 'nodejs|server\.js|NODE_ENV' "$term"
	# operators are warned before they restart it
	grep -qi 'restart.*kills every open' "$term"
}

@test "terminal unit passes systemd-analyze verify" {
	verify_unit "$term"
}

@test "sudoers" {
	# Allowed commands' argv stays out of the logs; denied attempts are still logged.
	grep -qx 'Defaults:hestiaweb !log_allowed' "$sudoers"
	run ! grep -qE '!(syslog|log_denied)' "$sudoers"
	grep -qxE 'hestiaweb[[:space:]]+ALL=NOPASSWD:/usr/local/hestia/bin/\*' "$sudoers"
	# Nothing beyond Hestia's scripts is granted.
	[ "$(grep -cE '^[[:space:]]*hestiaweb[[:space:]]' "$sudoers")" -eq 1 ]
	command -v visudo >/dev/null || skip "visudo not installed"
	run visudo -c -f "$sudoers"
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "env defaults on" {
	grep -qx 'HOSTISAN_FILE_RESTORE=on' "$envfile"
	# the off switch stays documented
	grep -q '"off"' "$envfile"
	[ "$(grep -c '^HOSTISAN_FILE_RESTORE=' "$envfile")" -eq 1 ]
}

# fake_system: stub the commands hidepid.sh calls. Every call is logged to
# $work/calls; groups and users live in $work/group and $work/passwd.
fake_system() {
	mkdir -p "$work/bin" "$work/systemd"
	: >"$work/calls"
	printf 'root:x:0:\n' >"$work/group"
	printf 'root:x:0:0::/root:/bin/bash\nhestiaweb:x:998:998::/nonexistent:/usr/sbin/nologin\n' >"$work/passwd"
	local cmd
	cat >"$work/bin/systemd-detect-virt" <<-'EOF'
		#!/bin/sh
		echo none
		exit 1
	EOF
	# systemctl logs and succeeds. show -p LoadState --value UNIT prints
	# "loaded"; try-restart fails (exit 1) when STUB_RESTART_FAIL=1.
	cat >"$work/bin/systemctl" <<-EOF
		#!/bin/sh
		echo "systemctl \$*" >>"$work/calls"
		[ "\$1" = show ] && echo loaded
		[ "\$1" = try-restart ] && [ "\${STUB_RESTART_FAIL:-0}" = 1 ] && exit 1
		exit 0
	EOF
	# usermod -aG GROUP USER adds USER to GROUP's member list.
	cat >"$work/bin/usermod" <<-EOF
		#!/bin/sh
		echo "usermod \$*" >>"$work/calls"
		awk -F: -v OFS=: -v g="\$2" -v u="\$3" '
			\$1 == g && (","\$4",") !~ ","u"," { \$4 = (\$4 == "" ? u : \$4","u) } { print }' \
			"$work/group" >"$work/group.new" && mv "$work/group.new" "$work/group"
	EOF
	# mount -o remount,OPTS /proc: the kernel then shows rw,OPTS for /proc.
	cat >"$work/bin/mount" <<-EOF
		#!/bin/sh
		echo "mount \$*" >>"$work/calls"
		echo "proc /proc proc rw,\${2#remount,} 0 0" >"$work/mounts"
	EOF
	# /proc as booted: the stock options, no hidepid
	echo 'proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0' >"$work/mounts"
	# id -nG USER: USER's primary group and the groups listing it as a member.
	cat >"$work/bin/id" <<-EOF
		#!/bin/sh
		if [ "\$1" = -nG ]; then
			gid=\$(awk -F: -v u="\$2" '\$1 == u { print \$4 }' "$work/passwd")
			awk -F: -v u="\$2" -v g="\$gid" '\$3 == g || (","\$4",") ~ ","u"," { printf "%s ", \$1 }' "$work/group"
			echo
			exit 0
		fi
		echo 0
	EOF
	cat >"$work/bin/getent" <<-EOF
		#!/bin/sh
		db=\$1 key=\$2
		grep "^\$key:" "$work/\$db"
	EOF
	cat >"$work/bin/groupadd" <<-EOF
		#!/bin/sh
		echo "groupadd \$*" >>"$work/calls"
		for name; do :; done
		echo "\$name:x:997:" >>"$work/group"
	EOF
	chmod 755 "$work"/bin/*
	cat >"$work/fstab" <<-'EOF'
		# /etc/fstab: static file system information.
		UUID=1234 / ext4 errors=remount-ro 0 1
		proc /proc proc defaults 0 0
		/swap.img none swap sw 0 0
	EOF
}

# The exact /proc options: hidepid on top of the stock nosuid,nodev,noexec.
proc_fstab='proc /proc proc nosuid,nodev,noexec,relatime,hidepid=invisible,gid=hostisan-proc 0 0'
proc_remount='remount,nosuid,nodev,noexec,relatime,hidepid=invisible,gid=997'

hidepid_run() {
	PATH="$work/bin:$PATH" HOSTISAN_SYSTEMD_DIR="$work/systemd" \
		HOSTISAN_PROC_CRON="$work/cron.d/hestia-proc" \
		HOSTISAN_HIDEPID_PENDING="$work/hidepid.pending" \
		HOSTISAN_PROC_MOUNTS="$work/mounts" run bash "$hidepid" "$@"
}

# upstream_cron: the @reboot remount upstream's installer writes.
upstream_cron() {
	mkdir -p "$work/cron.d"
	echo "@reboot root sleep 5 && mount -o remount,defaults,hidepid=2 /proc" >"$work/cron.d/hestia-proc"
}

@test "hidepid script" {
	fake_system
	printf 'polkitd:x:996:996::/nonexistent:/usr/sbin/nologin\n' >>"$work/passwd"
	local want='proc /proc proc nosuid,nodev,noexec,relatime,hidepid=invisible,gid=hostisan-proc 0 0'

	hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	# exactly one /proc line, ours; every other line kept
	[ "$(awk '$2 == "/proc"' "$work/fstab")" = "$want" ]
	grep -q '^UUID=1234 / ext4' "$work/fstab"
	grep -q '^/swap.img' "$work/fstab"
	# the exemption group, its members and the logind drop-in
	grep -q '^hostisan-proc:' "$work/group"
	grep -qx 'usermod -aG hostisan-proc hestiaweb' "$work/calls"
	grep -qx 'usermod -aG hostisan-proc polkitd' "$work/calls"
	local dropin="$work/systemd/systemd-logind.service.d/hostisan-proc.conf"
	grep -qx '\[Service\]' "$dropin"
	grep -qx 'SupplementaryGroups=hostisan-proc' "$dropin"
	# logind is restarted with the group before /proc is remounted
	grep -q '^systemctl daemon-reload' "$work/calls"
	local restart remount
	restart=$(grep -n '^systemctl try-restart .*systemd-logind' "$work/calls" | cut -d: -f1)
	remount=$(grep -nx "mount -o $proc_remount /proc" "$work/calls" | cut -d: -f1)
	[ -n "$restart" ] && [ -n "$remount" ] && [ "$restart" -lt "$remount" ]
	# the only mount call: the remount keeps nosuid,nodev,noexec
	[ "$(grep -c '^mount ' "$work/calls")" -eq 1 ]
	# the rollback is printed
	[[ "$output" == *"hidepid.sh --rollback"* ]]

	# a second run changes nothing and adds no group
	cp "$work/fstab" "$work/fstab.first"
	: >"$work/calls"
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	cmp "$work/fstab" "$work/fstab.first"
	run ! grep -q '^groupadd' "$work/calls"
	[ "$(grep -c '^hostisan-proc:' "$work/group")" -eq 1 ]
}

@test "hidepid restarts and remounts nothing when all is in place" {
	fake_system
	printf 'polkitd:x:996:996::/nonexistent:/usr/sbin/nologin\n' >>"$work/passwd"
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	# the kernel lists the options in its own order
	echo 'proc /proc proc rw,nosuid,nodev,noexec,relatime,gid=997,hidepid=invisible 0 0' >"$work/mounts"
	: >"$work/calls"

	hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"already"* ]]
	run ! grep -Eq '^(systemctl|mount) ' "$work/calls"
}

@test "hidepid remounts a /proc that lost its options" {
	fake_system
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	# someone remounted /proc by hand without hidepid
	echo 'proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0' >"$work/mounts"
	: >"$work/calls"

	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	grep -qx "mount -o $proc_remount /proc" "$work/calls"
}

@test "hidepid restarts logind when a new member joins the group" {
	fake_system
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	# polkit was installed since: polkitd needs the group
	printf 'polkitd:x:996:996::/nonexistent:/usr/sbin/nologin\n' >>"$work/passwd"
	: >"$work/calls"

	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	grep -qx 'usermod -aG hostisan-proc polkitd' "$work/calls"
	grep -q '^systemctl try-restart systemd-logind.service polkit.service' "$work/calls"
}

@test "hidepid fails and retries when logind could not be restarted" {
	fake_system
	STUB_RESTART_FAIL=1 hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -ne 0 ]
	[[ "$output" == *"could not restart"* ]]
	# /proc is not hidden from a logind without the group
	run ! grep -q '^mount ' "$work/calls"

	# the next run finds the group, members, drop-in and fstab in place, and
	# still restarts and remounts
	: >"$work/calls"
	hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	grep -q '^systemctl try-restart systemd-logind.service' "$work/calls"
	grep -qx "mount -o $proc_remount /proc" "$work/calls"
}

@test "hidepid retries a failed restart even when /proc is already hidden" {
	fake_system
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	# polkit was installed since; its restart fails
	printf 'polkitd:x:996:996::/nonexistent:/usr/sbin/nologin\n' >>"$work/passwd"
	STUB_RESTART_FAIL=1 hidepid_run "$work/fstab"
	[ "$status" -ne 0 ]

	: >"$work/calls"
	hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	grep -q '^systemctl try-restart systemd-logind.service polkit.service' "$work/calls"
	[[ "$output" != *"already"* ]]
	# and once that worked, the run after is a no-op again
	: >"$work/calls"
	hidepid_run "$work/fstab"
	[[ "$output" == *"already"* ]]
	run ! grep -Eq '^(systemctl|mount) ' "$work/calls"
}

@test "hidepid counts a primary group as membership" {
	fake_system
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	# hestiaweb holds the group as its primary group only
	sed -i 's/^hostisan-proc:x:997:.*/hostisan-proc:x:997:/' "$work/group"
	sed -i 's/^hestiaweb:x:998:998:/hestiaweb:x:998:997:/' "$work/passwd"
	: >"$work/calls"

	hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"already"* ]]
	run ! grep -q '^usermod' "$work/calls"
}

@test "hidepid script adds a proc line when fstab has none" {
	fake_system
	sed -i '/^proc /d' "$work/fstab"
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	[ "$(grep -cx "$proc_fstab" "$work/fstab")" -eq 1 ]
	# no polkitd user on this system: it is not added
	run ! grep -q 'polkitd' "$work/calls"
}

@test "hidepid rollback" {
	fake_system
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	: >"$work/calls"

	hidepid_run --rollback "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	run ! grep -q 'hidepid' "$work/fstab"
	grep -q '^UUID=1234 / ext4' "$work/fstab"
	[ ! -e "$work/systemd/systemd-logind.service.d/hostisan-proc.conf" ]
	# the stock options: nosuid,nodev,noexec kept, no hidepid, no group
	grep -qx 'mount -o remount,nosuid,nodev,noexec,relatime,hidepid=off,gid=0 /proc' "$work/calls"
	[ "$(grep -c '^mount ' "$work/calls")" -eq 1 ]
	grep -q '^systemctl daemon-reload' "$work/calls"
}

@test "hidepid replaces the proc line of an older Hostisan" {
	fake_system
	# rc3 and older wrote `defaults`, which dropped nosuid,nodev,noexec
	sed -i 's|^proc .*|proc /proc proc defaults,hidepid=invisible,gid=hostisan-proc 0 0|' "$work/fstab"
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	[ "$(awk '$2 == "/proc"' "$work/fstab")" = "$proc_fstab" ]
	run ! grep -q 'defaults' "$work/fstab"

	sed -i 's|^proc .*|proc /proc proc defaults,hidepid=invisible,gid=hostisan-proc 0 0|' "$work/fstab"
	hidepid_run --rollback "$work/fstab"
	[ "$status" -eq 0 ]
	run ! grep -q 'hidepid' "$work/fstab"
}

@test "hidepid refuses to run unprivileged" {
	fake_system
	printf '#!/bin/sh\necho 1000\n' >"$work/bin/id"
	cp "$work/fstab" "$work/fstab.orig"
	hidepid_run "$work/fstab"
	[ "$status" -ne 0 ]
	cmp "$work/fstab" "$work/fstab.orig"
}

@test "hidepid replaces upstream's reboot remount" {
	fake_system
	upstream_cron
	hidepid_run "$work/fstab"
	[ "$status" -eq 0 ]
	# upstream's hidepid=2 cron would undo the gid exemption at every boot
	[ ! -e "$work/cron.d/hestia-proc" ]
}

@test "hidepid rollback survives a reboot" {
	fake_system
	upstream_cron
	hidepid_run "$work/fstab"
	hidepid_run --rollback "$work/fstab"
	[ "$status" -eq 0 ]
	# nothing left that would hide /proc again at boot
	run ! grep -rq hidepid "$work/fstab" "$work/cron.d"
	[ ! -e "$work/systemd/systemd-logind.service.d/hostisan-proc.conf" ]
}

@test "hidepid skips containers" {
	fake_system
	upstream_cron
	printf '#!/bin/sh\necho lxc\n' >"$work/bin/systemd-detect-virt"
	cp "$work/fstab" "$work/fstab.orig"
	hidepid_run "$work/fstab"
	echo "$output"
	[ "$status" -eq 0 ]
	[[ "$output" == *"container"* ]]
	cmp "$work/fstab" "$work/fstab.orig"
	[ ! -s "$work/calls" ]
	run ! grep -q hostisan-proc "$work/group"
	[ ! -e "$work/systemd/systemd-logind.service.d" ]
	[ -e "$work/cron.d/hestia-proc" ]
}

@test "hidepid leaves no temp files" {
	fake_system
	hidepid_run "$work/fstab"
	hidepid_run --rollback "$work/fstab"
	# a run that fails while rewriting fstab cleans up too
	printf '#!/bin/sh\nexit 2\n' >"$work/bin/awk"
	chmod 755 "$work/bin/awk"
	hidepid_run "$work/fstab"
	[ "$status" -ne 0 ]
	[ -z "$(find "$work" -maxdepth 1 -name 'fstab.hostisan.*')" ]
}
