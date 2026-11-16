#!/usr/bin/env bats
# The installer's preflight (install/common/hostisan/preflight.sh): every
# check on the server and the options runs before anything is installed or
# changed, and all problems are reported together.
#
# The checks take their inputs as arguments; the readers that look at the
# real server (memory, disk, DNS, ports, packages) are replaced with shell
# functions here. preflight-run tests in install.bats run the real installer
# up to the end of its preflight.

# shellcheck disable=SC2034 # installer globals the helpers read

bats_require_minimum_version 1.5.0

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/preflight.XXXXXX")
	# shellcheck source=install/common/hostisan/preflight.sh
	source "$repo/install/common/hostisan/preflight.sh"
	hostisan_pf_reset
	force=
	interactive=no
	iptables=yes
	port=8083
}

teardown() {
	rm -rf "$work"
}

GB=1048576

# errors, warnings, notes: what the checks collected, one per line.
errors() { printf '%s\n' "${hostisan_pf_errors[@]}"; }
warnings() { printf '%s\n' "${hostisan_pf_warnings[@]}"; }
notes() { printf '%s\n' "${hostisan_pf_notes[@]}"; }

# ---------------------------------------------------------------- RAM

@test "RAM: under 2 GB is a warning that says ClamAV is off below 3 GB" {
	hostisan_pf_ram $((GB * 3 / 2)) no ''
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
	warnings | grep -q '1.5 GB of RAM'
	warnings | grep -q '2 GB'
	warnings | grep -q 'ClamAV is off by default under 3 GB'
}

@test "RAM: 2 to 3 GB only notes ClamAV is off; 4 GB says nothing" {
	hostisan_pf_ram $((GB * 5 / 2)) no ''
	[ "${#hostisan_pf_warnings[@]}" -eq 0 ]
	notes | grep -q 'ClamAV is off by default under 3 GB.*--clamav yes'
	hostisan_pf_reset
	hostisan_pf_ram $((GB * 4)) yes ''
	[ "${#hostisan_pf_warnings[@]}" -eq 0 ]
	[ "${#hostisan_pf_notes[@]}" -eq 0 ]
}

@test "RAM: --clamav yes under 3 GB is a warning" {
	hostisan_pf_ram $((GB * 5 / 2)) yes yes
	warnings | grep -q 'ClamAV.*about 1 GB'
}

# ---------------------------------------------------------------- disk

@test "disk: under 5 GB free stops the install, --force makes it a warning" {
	hostisan_pf_disk $((GB * 4)) /
	errors | grep -q '4.0 GB free on /.*5 GB'
	errors | grep -q -- '--force'
	hostisan_pf_reset
	force=yes
	hostisan_pf_disk $((GB * 4)) /
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
	warnings | grep -q '4.0 GB free'
	warnings | grep -q 'installing anyway'
}

@test "disk: 5 to 20 GB is a warning, 20 GB or more is fine" {
	# a server with the documented minimum disk (10 GB) has about 7 to 8 GB free
	local g
	for g in 5 7 15; do
		hostisan_pf_reset
		hostisan_pf_disk $((GB * g)) /
		[ "${#hostisan_pf_errors[@]}" -eq 0 ]
		warnings | grep -q "$g.0 GB free.*20 GB"
	done
	hostisan_pf_reset
	hostisan_pf_disk $((GB * 20)) /
	[ "${#hostisan_pf_warnings[@]}" -eq 0 ]
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
}

@test "disk: the documented minimum and the check agree" {
	local doc=$repo/docs/docs/introduction/getting-started.md
	if [ -f "$doc" ]; then
		grep -qE '^\| \*\*Disk\*\* +\| 10 GB' "$doc"
		grep -q 'refuses to install with under 5 GB free' "$doc"
		grep -q 'under 20 GB free' "$doc"
	fi
	grep -q 'under 5 GB of free disk' "$repo/README.md"
}

# ---------------------------------------------------------------- hostname

@test "hostname: a name with three or more labels passes" {
	local n
	for n in panel.example.com a.b.co host-1.sub.example.org panel.example.com.; do
		run hostisan_hostname_check "$n"
		echo "$n: $output"
		[ "$status" -eq 0 ]
	done
}

@test "hostname: two labels are refused with the reason and an example" {
	run hostisan_hostname_check example.com
	[ "$status" -eq 1 ]
	[[ "$output" == *"panel.example.com"* ]]
	[[ "$output" == *"website"* ]]
}

@test "hostname: bad names are refused with an example" {
	local n
	for n in '' localhost 192.168.1.10 'pan el.example.com' -x.example.com x-.example.com \
		'a..example.com' 'panel.example.c' 'panel.example.123' "$(printf 'a%.0s' {1..64}).example.com"; do
		run hostisan_hostname_check "$n"
		echo "'$n': $output"
		[ "$status" -eq 1 ]
		[[ "$output" == *"panel.example.com"* ]]
	done
}

@test "hostname: the preflight reports a bad --hostname as an error, --force or not" {
	force=yes
	hostisan_pf_hostname example.com
	errors | grep -q 'example.com'
}

# ---------------------------------------------------------------- DNS

@test "DNS: resolving to this server is a note that Let's Encrypt can run" {
	hostisan_pf_dns panel.example.com "203.0.113.5" "10.0.0.2 203.0.113.5"
	[ "${#hostisan_pf_warnings[@]}" -eq 0 ]
	notes | grep -q 'panel.example.com resolves to this server'
	notes | grep -q "Let's Encrypt"
}

@test "DNS: not resolving or resolving elsewhere is only a warning" {
	hostisan_pf_dns panel.example.com "" "203.0.113.5"
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
	warnings | grep -q 'panel.example.com does not resolve'
	warnings | grep -q 'A record.*203.0.113.5'
	warnings | grep -q 'v-add-letsencrypt-host'
	hostisan_pf_reset
	hostisan_pf_dns panel.example.com "198.51.100.7" "203.0.113.5"
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
	warnings | grep -q 'resolves to 198.51.100.7, not to this server (203.0.113.5)'
	warnings | grep -q 'NAT'
}

@test "DNS: a loopback answer from /etc/hosts does not count as this server" {
	hostisan_pf_dns panel.example.com "127.0.1.1" "203.0.113.5"
	warnings | grep -q 'does not resolve'
}

# ---------------------------------------------------------------- ports

@test "listening ports are read from /proc/net/tcp and tcp6" {
	mkdir -p "$work/net"
	cat >"$work/net/tcp" <<-'EOF'
		  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
		   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 1 1 0000000000000000 100 0 0 10 0
		   1: 0100007F:1F93 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 2 1 0000000000000000 100 0 0 10 0
		   2: 0A000002:0019 0A000003:D431 01 00000000:00000000 00:00000000 00000000     0        0 3 1 0000000000000000 100 0 0 10 0
	EOF
	cat >"$work/net/tcp6" <<-'EOF'
		  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
		   0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 4 1 0000000000000000 100 0 0 10 0
	EOF
	run hostisan_listening_ports "$work/net"
	[ "$status" -eq 0 ]
	[ "$(sort -n <<<"$output" | tr '\n' ' ')" = "80 443 8083 " ]
}

@test "ports: a busy port stops the install, naming the port and its use" {
	hostisan_port_owner() { [ "$1" = 80 ] && echo modguards; }
	hostisan_pf_ports "80 22" "" 80 443 25 8083
	errors | grep -q 'port 80 (web) is already in use by modguards'
	[ "$(errors | wc -l)" -eq 1 ]
	hostisan_pf_reset
	force=yes
	hostisan_pf_ports "8083" "" 80 443 8083
	warnings | grep -q 'port 8083 (panel)'
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
}

@test "ports: a port held by a conflicting package to be removed is only a warning" {
	hostisan_pf_ports "80 443" "apache2" 80 443 8083
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
	warnings | grep -q 'port 80.*apache2'
}

# ---------------------------------------------------------------- conflicting packages

@test "conflicts: whole package names or their -parts, not substrings" {
	run hostisan_conflicting_packages "ufw
apache2-bin
libnginx-mod-http-geoip2
mariadb-server-core
postfix
python3-hestia-tools
exim4-base" yes
	[ "$output" = "exim4 mariadb-server apache2 postfix ufw" ]
	# postfix only matters with Exim
	run hostisan_conflicting_packages "postfix" no
	[ -z "$output" ]
}

@test "conflicts: ufw goes late, without asking, when iptables replaces it" {
	local i f
	for i in no yes; do
		for f in '' yes; do
			hostisan_pf_reset
			interactive=$i force=$f iptables=yes
			hostisan_pf_conflicts "ufw"
			[ "$hostisan_purge_ufw" = yes ]
			[ "${#hostisan_purge_ask[@]}" -eq 0 ]
			[ "${#hostisan_pf_errors[@]}" -eq 0 ]
			[ "${#hostisan_pf_warnings[@]}" -eq 0 ]
			notes | grep -q '^ufw is installed: it will be removed just before Hostisan sets up its own firewall'
		done
	done
	# the default is --iptables yes
	hostisan_pf_reset
	unset iptables
	interactive=yes force=
	hostisan_pf_conflicts "ufw"
	[ "$hostisan_purge_ufw" = yes ]
}

@test "conflicts: with --iptables no, ufw is not a conflict and stays" {
	local i
	for i in no yes; do
		hostisan_pf_reset
		interactive=$i iptables=no
		hostisan_pf_conflicts "ufw"
		[ -z "$hostisan_purge_ufw" ]
		[ "${#hostisan_purge_ask[@]}" -eq 0 ]
		[ "${#hostisan_pf_errors[@]}" -eq 0 ]
		notes | grep -q '^ufw is installed and stays (--iptables no)'
		[ -z "$(notes | grep removed)" ]
	done
}

@test "conflicts: ufw is removed with --force, which leaves the others in place" {
	interactive=yes
	force=yes
	hostisan_pf_conflicts "apache2 ufw"
	[ "$hostisan_purge_ufw" = yes ]
	[ "${#hostisan_purge_ask[@]}" -eq 0 ]
	warnings | grep -q 'apache2 is already installed; --force leaves it in place'
}

@test "conflicts: interactive without --force asks about the others, not ufw" {
	interactive=yes
	hostisan_pf_conflicts "apache2 ufw"
	[ "$hostisan_purge_ufw" = yes ]
	[ "${hostisan_purge_ask[*]}" = "apache2*" ]
	[ "${#hostisan_purge_ask[@]}" -eq 1 ]
	[ "${#hostisan_pf_errors[@]}" -eq 0 ]
	notes | grep -q '^apache2 is already installed: you will be asked to remove it\.$'
	hostisan_pf_reset
	hostisan_pf_conflicts "apache2 nginx"
	[ "${#hostisan_purge_ask[@]}" -eq 2 ]
	[ "${hostisan_purge_ask[1]}" = 'nginx*' ]
	notes | grep -q 'apache2 nginx are already installed: you will be asked to remove them'
	hostisan_pf_reset
	interactive=no
	hostisan_pf_conflicts "apache2"
	errors | grep -q '^apache2 is already installed\. .*remove it with'
	hostisan_pf_reset
	force=yes
	hostisan_pf_conflicts "apache2"
	warnings | grep -q '^apache2 is already installed; --force leaves it in place'
}

@test "conflicts: other packages stop a non-interactive install with the fix" {
	interactive=no
	hostisan_pf_conflicts "apache2 mariadb-server ufw"
	[ "$hostisan_purge_ufw" = yes ]
	errors | grep -q 'apache2 mariadb-server are already installed'
	errors | grep -q "apt-get purge 'apache2\*' 'mariadb-server\*'"
}

# ---------------------------------------------------------------- the report

@test "the report lists every problem and fails when one stops the install" {
	hostisan_pf_warn 'low RAM'
	hostisan_pf_note 'a note'
	hostisan_pf_error 'first problem'
	hostisan_pf_error 'second problem'
	run hostisan_pf_report
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"[ ! ] low RAM"* ]]
	[[ "$output" == *"[ i ] a note"* ]]
	[[ "$output" == *"[ x ] first problem"* ]]
	[[ "$output" == *"[ x ] second problem"* ]]
	[[ "$output" == *"2 problems stop the install"* ]]
	[[ "$output" == *"nothing was installed or changed"* ]]
}

@test "the report passes with warnings only, and wraps long lines" {
	hostisan_pf_warn "$(printf 'word %.0s' {1..40})"
	run hostisan_pf_report
	[ "$status" -eq 0 ]
	[ "$(awk 'length > 80' <<<"$output" | wc -l)" -eq 0 ]
	run ! grep -nE ' $' <<<"$output"
	[[ "$output" != *"stop the install"* ]]
}

# ---------------------------------------------------------------- all together

# fake_server: readers for a small server whose hostname points elsewhere,
# with ufw installed and port 80 taken.
fake_server() {
	hostisan_mem_kb() { echo $((GB * 3 / 2)); }
	hostisan_disk_kb() { echo $((GB * 4)); }
	hostisan_resolve4() { :; }
	hostisan_local_ips() { echo 203.0.113.5; }
	hostisan_listening_port_list() { echo "22 80"; }
	hostisan_port_owner() { echo busybox; }
	hostisan_installed_packages() { printf 'ufw\nopenssh-server\n'; }
	hostisan_user_problem() { return 1; }
	servername=panel.example.com
	email=you@example.com
	username=admin
	port=8083
	exim=yes
	clamd=no
	hostisan_clamd_given=
	hostisan_password_given=
}

@test "hostisan_preflight runs every check and reports them together" {
	fake_server
	run hostisan_preflight
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"1.5 GB of RAM"* ]]
	[[ "$output" == *"4.0 GB free on /"* ]]
	[[ "$output" == *"port 80 (web) is already in use"* ]]
	[[ "$output" == *"does not resolve"* ]]
	[[ "$output" == *"ufw"* ]]
	[[ "$output" == *"2 problems stop the install"* ]]
}

@test "hostisan_preflight: input problems are errors even with --force" {
	fake_server
	force=yes
	servername=example.com
	email=not-an-email
	port=99999
	hostisan_user_problem() { echo "user $1 already exists"; }
	run hostisan_preflight
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"[ x ] --hostname example.com"* ]]
	[[ "$output" == *"[ x ] --email"* ]]
	[[ "$output" == *"[ x ] --port 99999"* ]]
	[[ "$output" == *"[ x ] --username admin: user admin already exists"* ]]
	# the disk and port refusals are warnings with --force
	[[ "$output" == *"[ ! ] Only 4.0 GB free"* ]]
	[[ "$output" == *"4 problems stop the install"* ]]
}

@test "hostisan_preflight: a missing --email stops a non-interactive install" {
	fake_server
	email=
	run hostisan_preflight
	[[ "$output" == *"[ x ] No --email given"* ]]
	# interactive: it is asked for before the preflight, so nothing to report
	interactive=yes
	run hostisan_preflight
	[[ "$output" != *"No --email"* ]]
}

@test "hostisan_preflight: --password may not hold whitespace or start with /tmp/" {
	fake_server
	hostisan_disk_kb() { echo $((GB * 40)); }
	hostisan_listening_port_list() { echo 22; }
	hostisan_password_given=yes
	local p
	for p in 'two words' $'tab\there' '/tmp/pw'; do
		vpass=$p
		run hostisan_preflight
		echo "$output"
		[ "$status" -eq 1 ]
		[[ "$output" == *"[ x ] --password"* ]]
		[[ "$output" != *"$p"* ]] # the password is not echoed
	done
	vpass='Good-pass_123'
	run hostisan_preflight
	[ "$status" -eq 0 ]
}

@test "hostisan_preflight: --password warns that ps shows it" {
	fake_server
	hostisan_password_given=yes
	run hostisan_preflight
	[[ "$output" == *"--password"*"ps"* ]]
	[[ "$output" == *"leave it out"* ]]
}
