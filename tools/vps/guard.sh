#!/usr/bin/env bash
# Safety guard for the Hostisan test VPS (Plan 6, Task A3).
#
#   guard.sh on <owner-ip>   block outbound SMTP and limit the panel ports to <owner-ip>
#   guard.sh off             remove the guard (this is the rollback)
#   guard.sh status          print "smtp-out: blocked|open" and "panel: <ip>|open"
#   guard.sh ruleset <ip>    print the nftables ruleset "on" would load, and change nothing
#
# The guard is one nftables table, "inet hostisan_guard", with its own base chains.
# Hestia's firewall (v-update-firewall) only runs iptables/ipset: "iptables -F INPUT" and
# iptables-restore touch the iptables tables ("ip filter" under iptables-nft), never this
# table, and nothing in Hestia runs "nft flush ruleset". A packet passes every base chain on
# a hook, so a drop here holds whatever Hestia accepts, and Hestia's drops still apply.
# The guard only drops: SSH (22) is never matched, so it can't lock anyone out.
#
# "on" also writes /etc/hostisan-guard.nft and a hostisan-guard.service oneshot that loads it
# at boot, so a reboot can't silently reopen SMTP. "on" is idempotent: it replaces the table
# atomically ("on" with a new IP just swaps the address).
#
# Test hooks: GUARD_DRY_RUN=1 prints the actions instead of running them;
# GUARD_ETC_DIR (default /etc) and GUARD_SYSTEMCTL (default systemctl) redirect the boot files.
set -euo pipefail

table=hostisan_guard
panel_ports='8083, 8443'
smtp_ports='25, 465, 587'
etc=${GUARD_ETC_DIR:-/etc}
systemctl=${GUARD_SYSTEMCTL:-systemctl}
dry=${GUARD_DRY_RUN:-}
conf=$etc/hostisan-guard.nft
unit=$etc/systemd/system/hostisan-guard.service

usage() {
	echo "usage: $0 on <owner-ip> | off | status | ruleset <owner-ip>" >&2
	exit 2
}

die() {
	echo "guard: $*" >&2
	exit 1
}

# Accepts one IPv4 or IPv6 address, optionally with a prefix length. Anything else is
# refused, because the value is written into the ruleset.
ip_family() {
	local ip=$1 addr len octet
	addr=${ip%/*}
	len=
	[[ $ip == */* ]] && len=${ip#*/}
	if [[ $addr =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]]; then
		for octet in "${BASH_REMATCH[@]:1}"; do
			((10#$octet <= 255)) || return 1
		done
		[[ -z $len || ($len =~ ^[0-9]{1,2}$ && $((10#$len)) -le 32) ]] || return 1
		echo ip
	elif [[ $addr =~ ^[0-9a-fA-F:]+$ && $addr == *:* && ${#addr} -le 39 ]]; then
		[[ -z $len || ($len =~ ^[0-9]{1,3}$ && $((10#$len)) -le 128) ]] || return 1
		echo ip6
	else
		return 1
	fi
}

ruleset() {
	local ip=$1 fam other
	fam=$(ip_family "$ip") || die "not an IPv4/IPv6 address: '$ip'"
	if [[ $fam == ip ]]; then other=ipv6; else other=ipv4; fi
	# "table; delete table; table {...}" replaces the table in one transaction, whether
	# or not it existed before.
	cat <<-RULES
		table inet $table
		delete table inet $table
		table inet $table {
			chain input {
				type filter hook input priority filter - 10; policy accept;
				iif "lo" accept
				tcp dport 22 accept comment "hostisan-ssh: never blocked"
				$fam saddr != $ip tcp dport { $panel_ports } drop comment "hostisan-panel"
				meta nfproto $other tcp dport { $panel_ports } drop comment "hostisan-panel-other-family"
			}
			chain output {
				type filter hook output priority filter - 10; policy accept;
				oif "lo" accept
				tcp dport { $smtp_ports } reject with tcp reset comment "hostisan-smtp-out"
			}
		}
	RULES
}

unit_text() {
	cat <<-UNIT
		[Unit]
		Description=Hostisan test-box guard (outbound SMTP blocked, panel limited to the owner)
		DefaultDependencies=no
		After=local-fs.target
		Before=network-pre.target
		Wants=network-pre.target

		[Service]
		Type=oneshot
		RemainAfterExit=yes
		ExecStart=/usr/sbin/nft -f /etc/hostisan-guard.nft
		ExecStop=-/usr/sbin/nft delete table inet $table

		[Install]
		WantedBy=multi-user.target
	UNIT
}

run() {
	if [[ -n $dry ]]; then
		echo "would: $*"
	else
		"$@"
	fi
}

need_root() {
	[[ -n $dry || $EUID -eq 0 ]] || die "run as root"
	[[ -n $dry ]] || command -v nft >/dev/null || die "nft is required (apt-get install -y nftables)"
}

has_table() {
	nft list table inet "$table" >/dev/null 2>&1
}

cmd_on() {
	local ip=$1 rules
	rules=$(ruleset "$ip")
	need_root
	if [[ -n $dry ]]; then
		echo "would: write $conf"
		printf '%s\n' "$rules"
		echo "would: nft -f $conf"
		echo "would: write $unit"
		echo "would: $systemctl daemon-reload"
		echo "would: systemctl enable hostisan-guard.service"
		return
	fi
	mkdir -p "$etc" "$(dirname "$unit")"
	printf '%s\n' "$rules" >"$conf.tmp"
	nft -c -f "$conf.tmp"
	mv "$conf.tmp" "$conf"
	nft -f "$conf"
	unit_text >"$unit"
	"$systemctl" daemon-reload
	"$systemctl" enable hostisan-guard.service >/dev/null 2>&1
	echo "guard on: smtp-out blocked, panel limited to $ip"
}

cmd_off() {
	need_root
	if has_table; then
		run nft delete table inet "$table"
	fi
	if [[ -e $unit ]]; then
		run "$systemctl" disable hostisan-guard.service >/dev/null 2>&1 || true
		run rm -f "$unit"
		run "$systemctl" daemon-reload
	fi
	run rm -f "$conf"
	echo "guard off"
}

cmd_status() {
	local listing smtp=open panel=open addr
	need_root
	if listing=$(nft list table inet "$table" 2>/dev/null); then
		if grep -Eq "tcp dport \{ ?${smtp_ports} ?\}.* reject" <<<"$listing"; then
			smtp=blocked
		fi
		addr=$(grep -Eo 'saddr != [0-9a-fA-F:./]+' <<<"$listing" | head -n1 || true)
		[[ -n $addr ]] && panel=${addr#saddr != }
	fi
	echo "smtp-out: $smtp"
	echo "panel: $panel"
}

[[ $# -ge 1 ]] || usage
case $1 in
	on)
		[[ $# -eq 2 ]] || usage
		cmd_on "$2"
		;;
	off)
		[[ $# -le 2 ]] || usage
		cmd_off
		;;
	status)
		[[ $# -le 2 ]] || usage
		cmd_status
		;;
	ruleset)
		[[ $# -eq 2 ]] || usage
		ruleset "$2"
		;;
	*) usage ;;
esac
