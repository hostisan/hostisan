#!/usr/bin/env bash
# install-smoke.sh [ADMIN [PASSWORD]]: checks a finished Hostisan install
# (Plan 6, B8), as root on the box or inside the install container.
#
# Prints "PASS|FAIL|SKIP <id> <detail>" per check; exit 1 when any check
# failed. ADMIN is the panel admin account (default: admin); with PASSWORD it
# also logs in through hestia-nginx and lists the web domains. The branding
# check runs tools/check-branding.sh from the source tree next to this script
# (or $CHECK_BRANDING) and is skipped when neither exists. In a container the
# /proc hidepid check is skipped (hidepid.sh skips containers too).
set -uo pipefail

admin=${1:-admin}
password=${2:-}
hestia=/usr/local/hestia
here=$(cd "$(dirname "$0")" && pwd)
branding=${CHECK_BRANDING:-$here/../../tools/check-branding.sh}
failed=0

pass() { echo "PASS $1 ${2:-}"; }
fail() {
	echo "FAIL $1 ${2:-}"
	failed=1
}
skip() { echo "SKIP $1 ${2:-}"; }
check() { # check ID DETAIL COMMAND...: PASS when COMMAND succeeds
	local id=$1 detail=$2
	shift 2
	if "$@" > /dev/null 2>&1; then pass "$id" "$detail"; else fail "$id" "$detail"; fi
}

if [ "$(id -u)" != 0 ]; then
	echo "install-smoke.sh: run as root" >&2
	exit 2
fi

port=$(grep -oP "^BACKEND_PORT='\K[0-9]+" "$hestia/conf/hestia.conf" 2> /dev/null)
port=${port:-8083}
base="https://localhost:$port"

# services
for unit in hostisan hestia; do
	state=$(systemctl is-active "$unit" 2>&1)
	if [ "$state" = active ]; then pass "unit-$unit" active; else fail "unit-$unit" "$state"; fi
done
check unit-enabled "hostisan.service enabled" systemctl is-enabled hostisan.service

# the panel answers through hestia-nginx and the unix socket
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
code=$(curl -sk -o "$tmp/boot.json" -w '%{http_code}' "$base/api/v1/bootstrap")
if [ "$code" = 200 ] && [ "$(jq -r .appName "$tmp/boot.json" 2> /dev/null)" = Hostisan ]; then
	pass api-bootstrap "HTTP 200, appName Hostisan"
else
	fail api-bootstrap "HTTP $code, body: $(head -c 200 "$tmp/boot.json")"
fi
# the old PHP panel is gone
for path in /list/user/index.php /index.php /login/index.php; do
	code=$(curl -sk -o /dev/null -w '%{http_code}' "$base$path")
	if [ "$code" = 404 ]; then pass "gone$path" 404; else fail "gone$path" "HTTP $code"; fi
done
# /_shell/ reaches no Node terminal: 404, or the panel's page for any unknown
# path (the UI's client-side router)
code=$(curl -sk -o "$tmp/shell" -w '%{http_code}' "$base/_shell/")
curl -sk -o "$tmp/unknown" "$base/hostisan-smoke-no-such-page/"
if [ "$code" = 404 ] || { [ "$code" = 200 ] && cmp -s "$tmp/shell" "$tmp/unknown"; }; then
	pass no-shell-proxy "HTTP $code"
else
	fail no-shell-proxy "HTTP $code, body: $(head -c 200 "$tmp/shell")"
fi
if [ -n "$password" ]; then
	csrf=$(curl -sk -c "$tmp/jar" -b "$tmp/jar" "$base/api/v1/bootstrap" | jq -r .csrfToken)
	code=$(jq -n --arg u "$admin" --arg p "$password" '{username: $u, password: $p}' |
		curl -sk -c "$tmp/jar" -b "$tmp/jar" -o "$tmp/login" -w '%{http_code}' \
			-H "Origin: $base" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
			--data-binary @- "$base/api/v1/auth/login")
	if [ "$code" = 200 ] && [ "$(curl -sk -b "$tmp/jar" "$base/api/v1/bootstrap" | jq -r .authenticated)" = true ]; then
		pass login "$admin"
	else
		fail login "HTTP $code, $(head -c 200 "$tmp/login")"
	fi
	code=$(curl -sk -b "$tmp/jar" -o "$tmp/web" -w '%{http_code}' "$base/api/v1/web")
	if [ "$code" = 200 ] && jq -e . "$tmp/web" > /dev/null 2>&1; then
		pass api-web "HTTP 200 (sudo v-list-* as hestiaweb)"
	else
		fail api-web "HTTP $code, $(head -c 200 "$tmp/web")"
	fi
else
	skip login "no password given"
fi

# no Node terminal, no panel TCP socket
listen=$(ss -Hltnp 2> /dev/null)
if grep -q ':8085\b' <<< "$listen"; then fail no-8085 "$(grep ':8085' <<< "$listen")"; else pass no-8085; fi
if grep -q '"hostisan"' <<< "$listen"; then fail no-hostisan-tcp "$(grep '"hostisan"' <<< "$listen")"; else pass no-hostisan-tcp; fi
check unix-socket "/run/hostisan/hostisan.sock" test -S /run/hostisan/hostisan.sock
if [ -e "$hestia/web-terminal" ] || pgrep -f 'web-terminal/server.js' > /dev/null; then
	fail no-node-terminal "$hestia/web-terminal or its server.js still there"
else
	pass no-node-terminal
fi
if dpkg-query -W -f='${Status}' nodejs 2> /dev/null | grep -q 'install ok installed'; then
	fail no-nodejs "nodejs is installed"
else
	pass no-nodejs
fi

# /proc
if systemd-detect-virt --container > /dev/null 2>&1; then
	skip proc-hidepid "container ($(systemd-detect-virt --container))"
elif grep -E '^proc /proc proc .*hidepid=(invisible|2)' /proc/mounts | grep -q 'gid='; then
	pass proc-hidepid "$(grep '^proc /proc ' /proc/mounts)"
else
	fail proc-hidepid "$(grep '^proc /proc ' /proc/mounts)"
fi

# apt: Hostisan's source (or none with local debs), never HestiaCP's
if grep -rq 'hestiacp\.com' /etc/apt/ 2> /dev/null; then
	fail apt-no-hestiacp "$(grep -rl 'hestiacp\.com' /etc/apt/ | tr '\n' ' ')"
else
	pass apt-no-hestiacp
fi
if dpkg-query -W -f='${Version}' hestia 2> /dev/null | grep -q '^1:'; then
	pass hestia-epoch "$(dpkg-query -W -f='${Version}' hestia)"
else
	fail hestia-epoch "$(dpkg-query -W -f='${Version}' hestia 2>&1)"
fi

# sudo
check sudoers-hestiaweb "visudo -c" visudo -c
out=$(sudo -n -l -U "$admin" 2>&1)
if grep -q 'is not allowed to run sudo' <<< "$out"; then
	pass admin-no-sudo "$admin"
else
	fail admin-no-sudo "$(tr '\n' ' ' <<< "$out")"
fi

# files
check env-restore-on "HOSTISAN_FILE_RESTORE=on" grep -qx 'HOSTISAN_FILE_RESTORE=on' "$hestia/conf/hostisan.env"
check web-error "$hestia/web/error kept" test -d "$hestia/web/error"
check web-fm "$hestia/web/fm kept" test -d "$hestia/web/fm"
if [ -e "$hestia/web/list" ] || [ -e "$hestia/web/login" ]; then
	fail no-old-panel "$hestia/web/list or web/login exists"
else
	pass no-old-panel
fi

# branding
if [ -x "$branding" ]; then
	if out=$("$branding" "$hestia" 2>&1); then
		pass branding
	else
		fail branding "$(head -n 5 <<< "$out" | tr '\n' ' ')"
	fi
else
	skip branding "no $branding"
fi

exit "$failed"
