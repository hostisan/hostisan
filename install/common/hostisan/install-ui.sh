#!/bin/bash
# install-ui.sh: sourced, not run, by install/hst-install-ubuntu.sh: what the
# installer shows and keeps for the person installing.
#   - the admin password: a strong generated one when --password is not given,
#     saved in a root-only file and shown once in the summary, and handed to
#     v-add-user in a file, not on its command line;
#   - progress lines during the long package phase;
#   - the Hostisan version for the banner;
#   - the final summary (test/hostisan/install-ui.bats).

# hostisan_new_password: 20 random letters and digits (about 119 bits).
hostisan_new_password() {
	local p
	p=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 20)
	printf '%s\n' "$p"
}

# hostisan_save_password FILE URL USER PASSWORD: write the admin login to
# FILE, readable by root only (0600). Whatever is at FILE (a link, say) is
# replaced, never written through.
hostisan_save_password() {
	local file=$1
	rm -f "$file" || return 1
	(
		umask 077
		set -o noclobber
		{
			echo "# Hostisan admin login, written by the installer on $(date -u '+%Y-%m-%d %H:%M UTC')."
			echo "# Readable by root only. Delete this file once the password is kept somewhere safe."
			echo "Panel:    $2"
			echo "Username: $3"
			echo "Password: $4"
		} > "$file"
	) || return 1
	chmod 600 "$file"
}

# hostisan_add_admin HESTIA USER PASSWORD EMAIL: create the panel admin with
# HESTIA/bin/v-add-user without putting PASSWORD on a command line, where any
# user could read it with ps. It goes in a 0600 file under /tmp (v-add-user
# reads a PASSWORD that is a path under /tmp/ as the file holding it:
# is_password_valid in func/main.sh), deleted straight after. Returns
# v-add-user's exit status.
hostisan_add_admin() {
	local file rc
	file=$(mktemp /tmp/hostisan-admin.XXXXXXXX) || return 1
	if ! chmod 600 "$file" || ! printf '%s\n' "$3" > "$file"; then
		rm -f "$file"
		return 1
	fi
	"$1/bin/v-add-user" "$2" "$file" "$4" "default" "System Administrator"
	rc=$?
	rm -f "$file"
	return "$rc"
}

# hostisan_log_mark LOG: remember where LOG ends, before the installer starts
# an apt-get in the background that appends to it (hostisan_wait reads only
# what comes after).
hostisan_log_mark() {
	hostisan_log_from=$(($(wc -l 2> /dev/null < "$1" || echo 0) + 1))
}

# hostisan_wait PID LABEL LOG [INTERVAL]: wait for PID (that apt-get) and
# print "    LABEL... (N min, X of Y packages set up; log: LOG)" every
# INTERVAL seconds (default 60) while it runs, so a long package phase shows
# signs of life; nothing for a quick one. X counts the "Setting up" lines
# added to LOG since hostisan_log_mark, Y comes from apt's "N upgraded,
# M newly installed" line there. Returns PID's exit status.
hostisan_wait() {
	local pid=$1 label=$2 log=$3 interval=${4:-60} start=$SECONDS next total set_up from=${hostisan_log_from:-1}
	next=$interval
	while kill -0 "$pid" 2> /dev/null; do
		sleep 1
		if [ $((SECONDS - start)) -ge "$next" ] && kill -0 "$pid" 2> /dev/null; then
			total=$(tail -n "+$from" "$log" 2> /dev/null | awk '/^[0-9]+ upgraded, [0-9]+ newly installed/ { t = $1 + $3 } END { print t + 0 }')
			set_up=$(tail -n "+$from" "$log" 2> /dev/null | grep -c '^Setting up ')
			if [ "$total" -gt 0 ]; then
				echo "    $label... ($(((SECONDS - start) / 60)) min, $set_up of $total packages set up; log: $log)"
			else
				echo "    $label... ($(((SECONDS - start) / 60)) min; log: $log)"
			fi
			next=$((next + interval))
		fi
	done
	wait "$pid"
}

# hostisan_install_version SRC DEBS: the Hostisan version to show before
# anything is installed: the build stamp make debs writes into the install
# tarball (SRC/common/hostisan/VERSION) or next to the debs (DEBS/VERSION),
# else "build N" from the hestia deb in DEBS, else nothing.
hostisan_install_version() {
	local f v deb
	for f in "$1/common/hostisan/VERSION" "${2:+$2/VERSION}"; do
		[ -n "$f" ] && [ -r "$f" ] || continue
		v=$(head -n1 "$f")
		if [[ $v =~ ^[A-Za-z0-9][A-Za-z0-9._+~-]*( \(build [0-9]+\))?$ ]]; then
			printf '%s\n' "$v"
		fi
		return 0
	done
	[ -n "$2" ] || return 0
	deb=$(compgen -G "$2/hestia_*.deb" | head -n1)
	[ -n "$deb" ] && command -v dpkg-deb > /dev/null || return 0
	v=$(dpkg-deb -f "$deb" Version 2> /dev/null)
	if [[ $v =~ -([0-9]+)(\+|$) ]]; then
		echo "build ${BASH_REMATCH[1]}"
	fi
}

# hostisan_install_summary screen|mail: what the installer says at the end.
# "screen" shows a generated password (once: it is not repeated anywhere
# else); "mail" names the root-only file instead, since mail is not a place
# for passwords. Reads the installer's globals: servername, port, ip,
# hostisan_dns_ok (servername resolves to ip), username, vpass,
# hostisan_password_generated, hostisan_password_file, LOG, withdebs, aptrepo,
# iptables, interactive and hostisan_version, and product.env's HOSTISAN_URL
# and HOSTISAN_DOCS_URL.
hostisan_install_summary() {
	local mode=$1 url="https://$servername:$port" n=1
	echo "Congratulations! Hostisan${hostisan_version:+ $hostisan_version} is installed on this server."
	echo
	echo "Sign in to the panel:"
	echo "    URL:         $url"
	if [ -z "${hostisan_dns_ok:-}" ]; then
		echo "    Backup URL:  https://$ip:$port"
		echo "                 (until $servername points at this server)"
	fi
	echo "    Username:    $username"
	if [ -z "${hostisan_password_generated:-}" ]; then
		echo "    Password:    the one given with --password"
	elif [ "$mode" = screen ]; then
		echo "    Password:    $vpass"
		echo "                 (generated; also in $hostisan_password_file,"
		echo "                 root only: delete it once the password is kept elsewhere)"
	else
		echo "    Password:    generated; in $hostisan_password_file on the server"
		echo "                 (root only)"
	fi
	echo "    Install log: $LOG"
	echo
	echo "Next steps:"
	if [ -n "${hostisan_dns_ok:-}" ]; then
		echo " $n. DNS: $servername already points at this server ($ip)."
	else
		echo " $n. DNS: create an A record for $servername pointing at $ip."
	fi
	n=$((n + 1))
	echo " $n. HTTPS for the panel: its certificate is self-signed until Let's Encrypt"
	echo "    runs. That happens by itself at the first reboot when DNS is ready;"
	echo "    otherwise run this as root once $servername resolves here:"
	echo "        v-add-letsencrypt-host"
	n=$((n + 1))
	if [ "${iptables:-yes}" = yes ]; then
		echo " $n. Firewall: Hostisan's firewall opens TCP $port (panel), 80 and 443."
		echo "    If your provider has a cloud firewall, open them there too. To allow"
		echo "    the panel only from your own address, find the HESTIA rule and change it:"
		echo "        v-list-firewall"
		echo "        v-change-firewall-rule RULE ACCEPT YOUR.IP/32 $port TCP HESTIA"
	else
		echo " $n. Firewall: none was installed (--iptables no). Open TCP $port (panel),"
		echo "    80 and 443 in whatever firewall protects this server, and your"
		echo "    provider's cloud firewall."
	fi
	n=$((n + 1))
	if [ -n "${aptrepo:-}" ]; then
		echo " $n. Updates are automatic: every night from $aptrepo"
		echo "    (v-update-sys-hestia-all; Server Settings > Updates turns them off)."
	else
		echo " $n. Updates are manual: this server has no Hostisan package source"
		echo "    (--with-debs). Build new debs with make debs and a higher"
		echo "    HOSTISAN_BUILD, copy them here and install them together:"
		echo "        dpkg -i hestia_*.deb hestia-nginx_*.deb \\"
		echo "            hestia-php_*.deb hestia-web-terminal_*.deb"
	fi
	n=$((n + 1))
	if [ "${interactive:-yes}" = yes ]; then
		echo " $n. The server reboots now to finish the install."
	else
		echo " $n. Reboot to finish the install: reboot"
	fi
	echo
	echo "Documentation: $HOSTISAN_DOCS_URL"
	echo "Website:       $HOSTISAN_URL/"
}
