#!/bin/bash
# preflight.sh: sourced, not run, by install/hst-install-ubuntu.sh. It checks
# the server and the options before anything is installed or changed, and
# reports every problem at once instead of stopping at the first.
#
# What it finds is one of:
#   error    stops the install: a bad option (hostname, e-mail, username,
#            port), or a refusal (under 5 GB free disk, a busy port, other
#            panels' packages on a non-interactive install);
#   warning  printed, the install goes on (low RAM, DNS not ready);
#   note     printed for information.
# --force ($force) turns refusals into warnings, to install anyway on a test
# server. It never excuses a bad option: the install would fail later on it.
#
# hostisan_preflight reads the installer's globals (servername, email,
# username, port, exim, clamd, iptables, interactive, force, vpass,
# hostisan_clamd_given, hostisan_password_given) and sets hostisan_purge_ufw
# (yes: ufw goes just before the installer sets up its own firewall) and
# hostisan_purge_ask (an array: the apt-get patterns of the packages to offer
# to remove). The hostisan_* readers
# below look at the real server; test/hostisan/preflight.bats replaces them.

hostisan_pf_reset() {
	hostisan_pf_errors=()
	hostisan_pf_warnings=()
	hostisan_pf_notes=()
	hostisan_purge_ufw=
	hostisan_purge_ask=()
}
hostisan_pf_reset

hostisan_pf_error() { hostisan_pf_errors+=("$1"); }
hostisan_pf_warn() { hostisan_pf_warnings+=("$1"); }
hostisan_pf_note() { hostisan_pf_notes+=("$1"); }

# hostisan_pf_refuse MSG: an error, or with --force a warning.
hostisan_pf_refuse() {
	if [ -n "${force:-}" ]; then
		hostisan_pf_warn "$1 (--force: installing anyway)"
	else
		hostisan_pf_error "$1 (--force installs anyway, on a test server only)"
	fi
}

# hostisan_gb KB: KB in GB with one decimal.
hostisan_gb() {
	awk -v k="$1" 'BEGIN { printf "%.1f", k / 1048576 }'
}

# ---------------------------------------------------------------- readers

# hostisan_pf_test_path VAR DEFAULT: DEFAULT, or $VAR when
# HOSTISAN_INSTALL_TEST=1 (test/hostisan/install.bats runs the installer
# against a fake /proc that way).
hostisan_pf_test_path() {
	if [ "${HOSTISAN_INSTALL_TEST:-}" = 1 ] && [ -n "${!1:-}" ]; then
		printf '%s\n' "${!1}"
	else
		printf '%s\n' "$2"
	fi
}

hostisan_mem_kb() {
	awk '/^MemTotal:/ { print $2 }' "$(hostisan_pf_test_path HOSTISAN_MEMINFO /proc/meminfo)"
}

hostisan_disk_kb() {
	df -Pk / | awk 'NR == 2 { print $4 }'
}

# hostisan_resolve4 NAME: NAME's IPv4 addresses, from DNS when dig or host is
# installed (so /etc/hosts does not answer), else from the system resolver.
hostisan_resolve4() {
	if command -v dig > /dev/null; then
		dig +short +time=3 +tries=1 A "$1" 2> /dev/null | grep -E '^[0-9.]+$'
	elif command -v host > /dev/null; then
		host -W 3 -t A "$1" 2> /dev/null | awk '/has address/ { print $NF }'
	else
		getent ahostsv4 "$1" 2> /dev/null | awk '{ print $1 }' | sort -u
	fi
}

hostisan_local_ips() {
	hostname -I 2> /dev/null
}

# hostisan_listening_ports [DIR]: the TCP ports something listens on, from
# DIR/tcp and DIR/tcp6 (default /proc/net), one per line.
hostisan_listening_ports() {
	local dir=${1:-/proc/net}
	cat "$dir/tcp" "$dir/tcp6" 2> /dev/null | awk '$4 == "0A" { split($2, a, ":"); print a[2] }' |
		while read -r hex; do printf '%d\n' "0x$hex"; done | sort -un
}

hostisan_listening_port_list() {
	hostisan_listening_ports "$(hostisan_pf_test_path HOSTISAN_PROC_NET /proc/net)" | tr '\n' ' '
}

# hostisan_port_owner PORT: the program listening on PORT, when ss can say.
hostisan_port_owner() {
	command -v ss > /dev/null || return 0
	ss -Hltnp "sport = :$1" 2> /dev/null | grep -o 'users:(("[^"]*"' | head -n1 | cut -d'"' -f2
}

hostisan_installed_packages() {
	dpkg-query -W -f='${db:Status-Abbrev} ${Package}\n' 2> /dev/null | awk '$1 ~ /^[ih]/ { print $2 }'
}

# hostisan_user_problem NAME: why NAME cannot be the admin account, if it
# cannot (the installer's validate_username).
hostisan_user_problem() {
	local msg
	msg=$(validate_username) || return 1
	printf '%s\n' "$msg" | sed '/^$/d'
}

# ---------------------------------------------------------------- checks

# hostisan_pf_ram KB CLAMD CLAMD_GIVEN: under 2 GB is a warning. The installer
# already turns ClamAV off by default under 3 GB (and SpamAssassin under
# 1.5 GB); an explicit --clamav yes there is a warning.
hostisan_pf_ram() {
	local kb=$1 gb
	gb=$(hostisan_gb "$kb")
	if [ "$kb" -lt 2097152 ]; then
		hostisan_pf_warn "This server has $gb GB of RAM; 2 GB or more is recommended. ClamAV is off by default under 3 GB of RAM, and SpamAssassin under 1.5 GB."
	elif [ "$kb" -lt 3000000 ] && [ "$2" != yes ]; then
		hostisan_pf_note "This server has $gb GB of RAM: ClamAV is off by default under 3 GB (--clamav yes turns it on; it uses about 1 GB)."
	fi
	if [ "$kb" -lt 3000000 ] && [ "$2" = yes ] && [ "$3" = yes ]; then
		hostisan_pf_warn "--clamav yes with $gb GB of RAM: ClamAV alone uses about 1 GB."
	fi
}

# hostisan_pf_disk KB PATH: under 5 GB free is refused (the packages alone
# take about 3 GB), under 20 GB a warning. The documented minimum disk,
# 10 GB, leaves about 7 to 8 GB free on a fresh server: enough to install.
hostisan_pf_disk() {
	local kb=$1 gb
	gb=$(hostisan_gb "$kb")
	if [ "$kb" -lt 5242880 ]; then
		hostisan_pf_refuse "Only $gb GB free on $2: Hostisan needs 5 GB free to install, and 20 GB or more for sites, mail and backups"
	elif [ "$kb" -lt 20971520 ]; then
		hostisan_pf_warn "$gb GB free on $2: enough to install, but 20 GB or more is recommended for sites, mail and backups."
	fi
}

# hostisan_hostname_check NAME: 0 when NAME can be the server's hostname, else
# 1 with the reason. It must be a fully qualified name of three or more labels,
# such as panel.example.com: the installer makes the hostname a web domain of
# the admin (with the panel's Let's Encrypt certificate) and, with mail, a mail
# host, so a two-label name such as example.com would take that domain from
# the websites and mail domains the server is meant to host.
hostisan_hostname_check() {
	local name=${1%.} label labels
	local eg="Use a name such as panel.example.com, with its A record pointing at this server."
	if [ -z "$name" ]; then
		echo "No hostname. $eg"
		return 1
	fi
	if [[ $name =~ ^[0-9]+(\.[0-9]+){3}$ ]]; then
		echo "$1 is an IP address, not a hostname. $eg"
		return 1
	fi
	IFS=. read -r -a labels <<< "$name"
	for label in "${labels[@]}"; do
		if [[ ! $label =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$ ]]; then
			echo "$1 is not a valid hostname: each part between dots must be 1 to 63 letters, digits or inner hyphens. $eg"
			return 1
		fi
	done
	if [ "${#name}" -gt 253 ] || [[ ! ${labels[-1]} =~ ^[A-Za-z][A-Za-z0-9-]*[A-Za-z0-9]$ ]]; then
		echo "$1 is not a valid hostname: it must end in a domain suffix such as .com. $eg"
		return 1
	fi
	case ${#labels[@]} in
		1)
			echo "$1 is not a fully qualified name. $eg"
			return 1
			;;
		2)
			echo "$1 has only two parts: use a name under it, such as panel.$name. The installer makes the hostname the admin's own web domain, with the panel's certificate, so $name itself could then not be added as a website or mail domain."
			return 1
			;;
	esac
	return 0
}

# hostisan_pf_hostname NAME: a bad hostname is an error, --force or not.
hostisan_pf_hostname() {
	local why
	if ! why=$(hostisan_hostname_check "$1"); then
		hostisan_pf_error "--hostname ${1:-(none)}: $why"
	fi
}

# hostisan_pf_dns NAME RESOLVED LOCAL: whether NAME resolves to one of this
# server's addresses. Only a warning either way: DNS may not be ready yet.
hostisan_pf_dns() {
	local name=$1 ip resolved=() local_ips
	local_ips=$(echo $3) # one space between addresses
	for ip in $2; do
		case $ip in 127.*) ;; *) resolved+=("$ip") ;; esac
	done
	if [ "${#resolved[@]}" -eq 0 ]; then
		hostisan_pf_warn "$name does not resolve yet. Point its A record at this server (${local_ips:-its public IP}) before the reboot at the end, so the panel gets a Let's Encrypt certificate then; otherwise run v-add-letsencrypt-host once it resolves."
		return 0
	fi
	for ip in "${resolved[@]}"; do
		if [[ " $local_ips " == *" $ip "* ]]; then
			hostisan_pf_note "$name resolves to this server ($ip): the panel gets its Let's Encrypt certificate at the first reboot."
			return 0
		fi
	done
	hostisan_pf_warn "$name resolves to ${resolved[*]}, not to this server (${local_ips:-no address found}). Point its A record here for the panel's Let's Encrypt certificate (v-add-letsencrypt-host). Behind NAT, where the public IP is not on this server, ignore this if that IP is right."
}

# hostisan_port_use PORT: what Hostisan needs PORT for ($port is the panel's).
hostisan_port_use() {
	case $1 in
		"${port:-8083}") echo panel ;;
		80 | 443) echo web ;;
		25) echo SMTP ;;
		*) echo service ;;
	esac
}

# hostisan_pf_ports BUSY CONFLICTS PORT...: each PORT Hostisan's services
# will listen on must be free. BUSY lists the ports in use. When CONFLICTS
# (packages the installer removes or offers to remove) is not empty, a busy
# port is probably theirs, so it is only a warning.
hostisan_pf_ports() {
	local busy=" $1 " conflicts=$2 p owner what seen=" "
	shift 2
	for p in "$@"; do
		[[ $seen == *" $p "* ]] && continue
		seen+="$p "
		[[ $busy == *" $p "* ]] || continue
		owner=$(hostisan_port_owner "$p") || owner=
		what="port $p ($(hostisan_port_use "$p")) is already in use${owner:+ by $owner}"
		if [ -n "$conflicts" ]; then
			hostisan_pf_warn "$what, probably by $conflicts, which must be removed first; anything else on it must be stopped."
		else
			hostisan_pf_refuse "$what: stop or remove that service first (ss -ltnp shows it)"
		fi
	done
}

# hostisan_conflicting_packages INSTALLED EXIM: the packages, of those other
# panels and mail or web stacks install, that INSTALLED (package names, one
# per line) holds: a whole name, or one followed by -part (apache2-bin is
# apache2, libnginx-mod-* is not nginx). postfix counts only with Exim.
hostisan_conflicting_packages() {
	local pkg found=()
	for pkg in exim4 mariadb-server mysql-server apache2 nginx hestia postfix ufw; do
		[ "$pkg" = postfix ] && [ "$2" = no ] && continue
		if grep -qE "^${pkg}(-|$)" <<< "$1"; then
			found+=("$pkg")
		fi
	done
	echo "${found[*]}"
}

# hostisan_pf_conflicts CONFLICTS: what happens to each conflicting package.
# ufw is not asked about: with --iptables yes (the default) Hostisan's own
# firewall replaces it, so it is removed without asking (interactive or not),
# late, just before that firewall is set up, and guards the server until
# then; with --iptables no it is the server's only firewall and stays. The
# others stop a non-interactive install, are left in place with --force, and
# are offered for removal on an interactive one.
hostisan_pf_conflicts() {
	local pkg others=() purge=""
	for pkg in $1; do
		if [ "$pkg" != ufw ]; then
			others+=("$pkg")
		elif [ "${iptables:-yes}" = yes ]; then
			hostisan_purge_ufw=yes
			hostisan_pf_note "ufw is installed: it will be removed just before Hostisan sets up its own firewall (iptables), near the end of the install."
		else
			hostisan_pf_note "ufw is installed and stays (--iptables no): open TCP 80, 443 and ${port:-8083} (panel) in it."
		fi
	done
	[ "${#others[@]}" -gt 0 ] || return 0
	for pkg in "${others[@]}"; do
		purge+=" '$pkg*'"
	done
	local is="is" them="it"
	if [ "${#others[@]}" -gt 1 ]; then
		is="are"
		them="them"
	fi
	if [ -n "${force:-}" ]; then
		hostisan_pf_warn "${others[*]} $is already installed; --force leaves $them in place, untested."
	elif [ "${interactive:-yes}" = yes ]; then
		# an array, used quoted: apt-get gets the patterns, not files of the
		# current dir they would match
		for pkg in "${others[@]}"; do
			hostisan_purge_ask+=("$pkg*")
		done
		hostisan_pf_note "${others[*]} $is already installed: you will be asked to remove $them."
	else
		hostisan_pf_error "${others[*]} $is already installed. Hostisan needs a clean server: remove $them with apt-get purge${purge}, or run the installer with --interactive yes to be asked"
	fi
}

# ---------------------------------------------------------------- report

# hostisan_pf_line MARK TEXT: TEXT after "  [ MARK ] ", wrapped at 78 columns.
hostisan_pf_line() {
	printf '%s\n' "$2" | fold -s -w 70 | sed -e 's/ *$//' -e "1s/^/  [ $1 ] /" -e '2,$s/^/        /'
}

# hostisan_pf_report: print what the checks found; 1 when an error stops the
# install.
hostisan_pf_report() {
	local m n=${#hostisan_pf_errors[@]}
	echo "Checking this server before anything is installed:"
	for m in "${hostisan_pf_notes[@]}"; do hostisan_pf_line i "$m"; done
	for m in "${hostisan_pf_warnings[@]}"; do hostisan_pf_line '!' "$m"; done
	for m in "${hostisan_pf_errors[@]}"; do hostisan_pf_line x "$m"; done
	if [ "$n" -eq 0 ]; then
		[ "${#hostisan_pf_notes[@]}" -gt 0 ] || [ "${#hostisan_pf_warnings[@]}" -gt 0 ] || echo "  [ * ] All checks passed."
		echo
		return 0
	fi
	echo
	if [ "$n" -eq 1 ]; then
		echo "Error: 1 problem stops the install; nothing was installed or changed."
		echo "Fix it and run the installer again."
	else
		echo "Error: $n problems stop the install; nothing was installed or changed."
		echo "Fix them and run the installer again."
	fi
	return 1
}

# hostisan_preflight: run every check, print the report; 1 when the install
# must stop.
hostisan_preflight() {
	local why conflicts ports=(80 443)
	hostisan_pf_reset

	# the options
	if [ -z "${servername:-}" ] && [ "${interactive:-yes}" = no ]; then
		hostisan_pf_error "No --hostname given and this server's own name is not usable. Add --hostname panel.example.com."
	elif [ -n "${servername:-}" ]; then
		hostisan_pf_hostname "$servername"
	fi
	if [ -z "${email:-}" ]; then
		if [ "${interactive:-yes}" = no ]; then
			hostisan_pf_error "No --email given: the admin account needs one (--email you@example.com)."
		fi
	elif [[ ! $email =~ ^[A-Za-z0-9._%+-]+@[[:alnum:].-]+\.[A-Za-z]{2,63}$ ]]; then
		hostisan_pf_error "--email $email is not a valid e-mail address (such as you@example.com)."
	fi
	if [ -n "${username:-}" ] && why=$(hostisan_user_problem "$username"); then
		hostisan_pf_error "--username $username: $why"
	fi
	if [[ ! ${port:-} =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
		hostisan_pf_error "--port ${port:-}: the panel port must be a number from 1 to 65535."
	fi
	if [ -n "${hostisan_password_given:-}" ]; then
		hostisan_pf_warn "--password puts the admin password in the process list, where any user on this server can read it with ps while the installer runs. Next time leave it out: the installer then makes a strong one and shows it at the end."
	fi
	# never echoed: it is a password
	if [[ ${vpass:-} =~ [[:space:]] ]]; then
		hostisan_pf_error "--password: the admin password may not contain spaces, tabs or line breaks."
	elif [[ ${vpass:-} == /tmp/* ]]; then
		hostisan_pf_error "--password: the admin password may not start with /tmp/, which the panel's scripts read as the name of a file holding the password."
	fi

	# the server
	hostisan_pf_ram "$(hostisan_mem_kb)" "${clamd:-no}" "${hostisan_clamd_given:-}"
	hostisan_pf_disk "$(hostisan_disk_kb)" /
	conflicts=$(hostisan_conflicting_packages "$(hostisan_installed_packages)" "${exim:-yes}")
	hostisan_pf_conflicts "$conflicts"
	[ "${exim:-yes}" = yes ] && ports+=(25)
	hostisan_pf_ports "$(hostisan_listening_port_list)" "$(echo ${conflicts/ufw/})" "${ports[@]}" "${port:-8083}"
	if [ -n "${servername:-}" ] && hostisan_hostname_check "$servername" > /dev/null; then
		hostisan_pf_dns "$servername" "$(hostisan_resolve4 "$servername")" "$(hostisan_local_ips)"
	fi

	hostisan_pf_report
}
