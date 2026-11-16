#!/usr/bin/env bash
# Usage: remove-node-terminal.sh
#
# Removes what upstream Hestia's Node web terminal leaves behind once
# hestia-web-terminal 2.x (the Go terminal) replaces it:
#   - /usr/local/hestia/web-terminal (server.js, node_modules and any files
#     npm or Node wrote there), and any server.js process still running from
#     it (it listened on WEB_TERMINAL_PORT, 8085 by default);
#   - the NodeSource apt source the installer wrote for it
#     (/etc/apt/sources.list.d/nodejs.list) and its keyring, unless another
#     apt source still uses that keyring. The nodejs package itself stays
#     installed: apt can't remove it while dpkg runs this script.
# While a hestia-web-terminal 1.x package still owns the directory, the
# directory and its process are left alone (that package's upgrade calls this
# script again). Idempotent. Run by setup.sh and by the hestia-web-terminal
# postinst.
set -uo pipefail

dir=/usr/local/hestia/web-terminal
list=/etc/apt/sources.list.d/nodejs.list
keyring=/usr/share/keyrings/nodejs.gpg

if [ "$(id -u)" != 0 ]; then
	echo "remove-node-terminal.sh: must run as root" >&2
	exit 1
fi

if dpkg-query -S "$dir" > /dev/null 2>&1; then
	echo "node terminal: $dir kept, a hestia-web-terminal 1.x package still owns it"
else
	pkill -f "$dir/server\.js" > /dev/null 2>&1 || true
	if [ -e "$dir" ]; then
		rm -rf "$dir"
		echo "node terminal: removed $dir"
	fi
fi

if [ -f "$list" ] && grep -q 'deb\.nodesource\.com' "$list"; then
	rm -f "$list"
	echo "node terminal: removed the NodeSource apt source"
fi
if [ -f "$keyring" ] && ! grep -rqsF 'keyrings/nodejs.gpg' /etc/apt/sources.list.d /etc/apt/sources.list; then
	rm -f "$keyring"
fi
exit 0
