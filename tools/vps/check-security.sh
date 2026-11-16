#!/usr/bin/env bash
# Plan 6 Task C2: the security MUST checks S1-S8 for a Hostisan box.
#
#   check-security.sh prepare-s7 <out>        build the S7 test binary here (go test -c)
#   check-security.sh [run] [options] [S1 ... S8]
#
# The script runs on the controller, not on the box: the panel requests in S2, S4, S5, S6
# and S8 have to come from the owner IP (the guard only lets that IP reach the panel port,
# and fail2ban must ban a real client address, never 127.0.0.1). Everything that needs
# root on the box goes through "ssh $VPS bash -s" as one of the remote_* functions below.
#
# Options (or the environment variable in brackets):
#   --vps <ssh target>             root ssh target, e.g. root@185.161.209.107      [VPS]
#   --owner-ip <ip>                the IP the panel sees from here                  [OWNER_IP]
#   --admin-user <name>            panel admin for the logins (default admin)       [ADMIN_USER]
#   --key-user <name>              owner of S3's access key on the box (default admin) [KEY_USER]
#                                  Hestia gives a key without a permission list only
#                                  to the user named admin, whoever logs in to the UI.
#   --admin-password-file <path>   local file holding the admin password            [ADMIN_PASSWORD_FILE]
#   --test-user <name>             a hosting user, e.g. bob                         [TEST_USER]
#   --test-password-file <path>    local file holding the test user's password (S8) [TEST_PASSWORD_FILE]
#   --s7-binary <path>             the binary from "prepare-s7" (S7)                [S7_BINARY]
#   --panel-url <url>              default https://<host of --vps>:8083            [PANEL_URL]
# With no check ids every check runs, in order. Each check prints one line,
# "PASS <id> ..." or "FAIL <id> <reasons>", and there is a SUMMARY line at the end.
# The exit status is 1 when any check failed, 2 on bad usage.
#
# What the checks change (everything is put back on exit, also on ^C):
# - S2, S5, S6 log the admin in and out; S2's file-manager handoff adds a 30-minute
#   sftp key for the admin, which atd removes.
# - S3 makes a temporary admin access key and deletes it again.
# - S3 and S8 add 127.0.0.1 (S3) or the owner IP (S8, the /api/ round) to API_ALLOWED_IP
#   when it isn't allowed yet, and restore the exact old value.
# - S4 and S8 write "failed to login" lines for the test user.
# - S8 bans the owner IP in the hestia-iptables jail (the panel port only), three times,
#   and unbans it each time. Before it starts it checks that the jail's INPUT jump covers
#   only the panel port (so ssh can't be cut), and it adds the owner IP to the recidive
#   jail's runtime ignore list for the duration, since three bans in a day count towards
#   recidive's all-ports ban. The EXIT trap always unbans and removes that ignore entry.
# - S8's 2FA round turns 2FA on for the test user when it is off, and off again after.
#   The test user's 2FA step stays locked in Hostisan for 15 minutes afterwards.
# The passwords are read from their files by jq and sent in request bodies; they never
# appear in argv, in the output or on the box.
set -uo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

VPS=${VPS:-}
OWNER_IP=${OWNER_IP:-}
ADMIN_USER=${ADMIN_USER:-admin}
KEY_USER=${KEY_USER:-admin}
ADMIN_PASSWORD_FILE=${ADMIN_PASSWORD_FILE:-}
TEST_USER=${TEST_USER:-}
TEST_PASSWORD_FILE=${TEST_PASSWORD_FILE:-}
S7_BINARY=${S7_BINARY:-}
PANEL_PORT=${PANEL_PORT:-8083}
PANEL_URL=${PANEL_URL:-}
HTTP_TIMEOUT=${HTTP_TIMEOUT:-30}
BAN_WAIT=${BAN_WAIT:-60}
POLL=${POLL:-2}

ALL_CHECKS=(S1 S2 S3 S4 S5 S6 S7 S8)
PASSED=0
FAILED=0
FAILED_IDS=()
PROBLEMS=()
WORK=
HTTP_STATUS=
HTTP_LOCATION=
LOGOUT_JARS=()
S8_STARTED=
API_TOUCHED=
TWOFA_TOUCHED=

usage() {
	sed -n '2,/^set -uo/{/^set -uo/d;s/^# \{0,1\}//;p}' "${BASH_SOURCE[0]}" >&2
	exit "${1:-2}"
}

# ------------------------------------------------------------------ results

pass() {
	echo "PASS $*"
	PASSED=$((PASSED + 1))
}

fail() {
	echo "FAIL $*"
	FAILED=$((FAILED + 1))
	FAILED_IDS+=("$1")
}

note() {
	echo "NOTE $*"
}

problem() {
	PROBLEMS+=("$*")
}

# verdict ID OKTEXT: one PASS line, or one FAIL line with every recorded problem.
verdict() {
	local id=$1 ok=$2 joined
	if ((${#PROBLEMS[@]} == 0)); then
		pass "$id" "$ok"
	else
		joined=$(printf '%s; ' "${PROBLEMS[@]}")
		fail "$id" "${joined%; }"
	fi
	PROBLEMS=()
}

summary() {
	if ((FAILED == 0)); then
		echo "SUMMARY $PASSED passed, 0 failed"
		return 0
	fi
	echo "SUMMARY $PASSED passed, $FAILED failed: ${FAILED_IDS[*]}"
	return 1
}

# kv KEY: the value of the first "KEY=value" line on stdin.
kv() {
	sed -n "s/^$1=//p" | head -n1
}

pause() {
	sleep "$1"
}

# ------------------------------------------------------------------ transport

# box ACTION [ARGS...]: run remote_ACTION as root on $VPS. Every remote_* function is
# sent along, so they can call each other. The call's stdin is /dev/null, so nothing on
# the box can read the rest of the script.
box() {
	local action=$1 call fns
	shift
	call=$(printf ' %q' "$@")
	mapfile -t fns < <(compgen -A function remote_)
	{
		echo 'set -uo pipefail; export LC_ALL=C'
		declare -f "${fns[@]}"
		echo "remote_$action$call </dev/null"
	} | ssh -o BatchMode=yes -o ConnectTimeout=15 "$VPS" bash -s
}

box_copy() {
	scp -q -o BatchMode=yes "$1" "$VPS:$2"
}

# http JAR METHOD PATH [curl args...]: a panel request from this machine (the owner IP).
# Sets HTTP_STATUS (000 when nothing answered) and HTTP_LOCATION (a redirect's absolute
# target, else empty); the body is in $WORK/body. JAR "" sends no cookie jar.
http() {
	local jar=$1 method=$2 path=$3 cookies=() out
	shift 3
	[[ -n $jar ]] && cookies=(-b "$jar" -c "$jar")
	out=$(curl -sk --path-as-is --max-time "$HTTP_TIMEOUT" -o "$WORK/body" -w '%{http_code} %{redirect_url}' \
		"${cookies[@]}" -X "$method" "$@" "$PANEL_URL$path" </dev/null) || true
	HTTP_STATUS=${out%% *}
	HTTP_LOCATION=${out#* }
	[[ $HTTP_STATUS =~ ^[0-9]{3}$ ]] || HTTP_STATUS=000 HTTP_LOCATION=
}

body() {
	cat "$WORK/body" 2>/dev/null
}

panel_origin() {
	local rest=${PANEL_URL#*://}
	echo "${PANEL_URL%%://*}://${rest%%/*}"
}

new_jar() {
	mktemp "$WORK/jar.XXXXXX"
}

# login_body USER PWFILE [TWOFA]: the login JSON. jq reads the password from the file.
login_body() {
	jq -cn --arg u "$1" --rawfile p "$2" --arg c "${3:-}" \
		'{username: $u, password: ($p | rtrimstr("\n") | rtrimstr("\r"))} + (if $c == "" then {} else {twofa: $c} end)'
}

# wrong_password: a password file nobody has.
wrong_password() {
	printf 'c2-wrong-%s%s%s' "$RANDOM" "$RANDOM" "$RANDOM"
}

# panel_login JAR USER PWFILE ORIGIN [TWOFA]: sets CSRF and LOGIN_RESULT to the reply's
# status (ok, twofa_required), its error code, or bootstrap-<http>/http-<http>.
panel_login() {
	local jar=$1 user=$2 pwfile=$3 origin=$4 code=${5:-}
	CSRF='' LOGIN_RESULT=''
	http "$jar" GET /api/v1/bootstrap
	CSRF=$(jq -r '.csrfToken // empty' "$WORK/body" 2>/dev/null)
	if [[ $HTTP_STATUS != 200 || -z $CSRF ]]; then
		LOGIN_RESULT=bootstrap-$HTTP_STATUS
		return 1
	fi
	http "$jar" POST /api/v1/auth/login -H 'Content-Type: application/json' -H "Origin: $origin" \
		-H "X-CSRF-Token: $CSRF" --data-binary @<(login_body "$user" "$pwfile" "$code")
	LOGIN_RESULT=$(jq -r '.status // .error.code // empty' "$WORK/body" 2>/dev/null)
	[[ -n $LOGIN_RESULT ]] || LOGIN_RESULT=http-$HTTP_STATUS
	if [[ $LOGIN_RESULT == ok ]]; then
		LOGOUT_JARS+=("$jar")
		return 0
	fi
	return 1
}

panel_logout() {
	local jar=$1 token
	http "$jar" GET /api/v1/bootstrap
	token=$(jq -r '.csrfToken // empty' "$WORK/body" 2>/dev/null)
	http "$jar" POST /api/v1/auth/logout -H "Origin: $(panel_origin)" -H "X-CSRF-Token: $token"
}

# admin_login JAR: logs the admin in, or records a problem and returns 1.
admin_login() {
	if [[ -z $ADMIN_PASSWORD_FILE || ! -r $ADMIN_PASSWORD_FILE ]]; then
		problem "admin password file missing or unreadable (--admin-password-file)"
		return 1
	fi
	if ! panel_login "$1" "$ADMIN_USER" "$ADMIN_PASSWORD_FILE" "$(panel_origin)"; then
		problem "admin login gave '$LOGIN_RESULT' (HTTP $HTTP_STATUS)"
		return 1
	fi
}

need_test_user() {
	if [[ -z $TEST_USER ]]; then
		problem "no test user (--test-user)"
		return 1
	fi
}

# ------------------------------------------------------------------ remote (root on the box)

remote_ping() {
	echo "uid=$(id -u)"
	echo "client=${SSH_CLIENT%% *}"
}

remote_test_user() {
	local conf=/usr/local/hestia/data/users/$1/user.conf
	if [[ -f $conf ]] && id "$1" >/dev/null 2>&1; then echo exists=yes; else echo exists=no; fi
}

remote_s1() {
	local port=$1 base a b
	if ss -ltnH 'sport = :8085' | grep -q .; then echo listener=yes; else echo listener=no; fi
	if [[ -e /usr/local/hestia/web-terminal ]]; then echo dir=present; else echo dir=absent; fi
	echo "nodejs=$(dpkg-query -W -f='${Status}' nodejs 2>/dev/null || echo not-installed)"
	base=https://127.0.0.1:$port
	echo "shell=$(curl -sk --max-time 20 -o /tmp/.hostisan-c2-s1a -w '%{http_code}' "$base/_shell/")"
	echo "spa=$(curl -sk --max-time 20 -o /tmp/.hostisan-c2-s1b -w '%{http_code}' "$base/hostisan-c2-no-such-page/")"
	a=$(sha256sum </tmp/.hostisan-c2-s1a | cut -d' ' -f1)
	b=$(sha256sum </tmp/.hostisan-c2-s1b | cut -d' ' -f1)
	if [[ $a == "$b" ]]; then echo same_as_spa=yes; else echo same_as_spa=no; fi
	rm -f /tmp/.hostisan-c2-s1a /tmp/.hostisan-c2-s1b
}

remote_authlog_mark() {
	local f=/usr/local/hestia/log/auth.log
	if [[ -f $f ]]; then wc -l <"$f"; else echo 0; fi
}

remote_authlog_since() {
	tail -n "+$(($1 + 1))" /usr/local/hestia/log/auth.log 2>/dev/null
}

# remote_api_allow IP: make sure the legacy API accepts IP; remember the old value once.
remote_api_allow() {
	local ip=$1 conf=/usr/local/hestia/conf/hestia.conf state=/root/.hostisan-c2-api-allowed cur
	cur=$(sed -n "s/^API_ALLOWED_IP='\(.*\)'$/\1/p" "$conf")
	echo "api_system=$(sed -n "s/^API_SYSTEM='\(.*\)'$/\1/p" "$conf")"
	if [[ $cur == allow-all || ,$cur, == *",$ip,"* ]]; then
		echo api_allow=already
		return 0
	fi
	[[ -e $state ]] || printf '%s' "$cur" >"$state"
	if /usr/local/hestia/bin/v-change-sys-config-value API_ALLOWED_IP "${cur:+$cur,}$ip" >/dev/null; then
		sleep 6 # Hostisan caches the config for 5 s
		echo api_allow=added
	else
		echo api_allow=error
	fi
}

remote_api_restore() {
	local state=/root/.hostisan-c2-api-allowed
	[[ -e $state ]] || return 0
	/usr/local/hestia/bin/v-change-sys-config-value API_ALLOWED_IP "$(cat "$state")" >/dev/null && rm -f "$state"
	echo api_restore=done
}

remote_s3_secret() {
	local port=$1 admin=$2 bin=/usr/local/hestia/bin authlog=/var/log/auth.log
	local dir t0 mark=0 pair id secret ok=0 i code out sampler
	dir=$(mktemp -d /root/.hostisan-c2.XXXXXX) || return 1
	t0=$(date +%s)
	[[ -f $authlog ]] && mark=$(wc -l <"$authlog")
	remote_api_allow 127.0.0.1 | grep -E '^(api_allow|api_system)='
	if ! pair=$("$bin/v-add-access-key" "$admin" '' c2check plain 2>/dev/null) || [[ $pair != *:* ]]; then
		echo "error=v-add-access-key failed"
		rm -rf "$dir"
		remote_api_restore >/dev/null
		return 0
	fi
	id=${pair%%:*}
	secret=${pair#*:}
	# shellcheck disable=SC2064 # id and dir are fixed now; the secret is not in the trap
	trap "'$bin/v-delete-access-key' '$id' >/dev/null 2>&1; rm -rf '$dir'; remote_api_restore >/dev/null" EXIT
	printf '%s' "$id" >"$dir/id"
	printf '%s' "$secret" >"$dir/secret"
	# The sampler compares in bash, so the secret is never in any argv itself.
	(
		samples=0 hits=0
		while [[ ! -e $dir/stop ]]; do
			out=$(ps -eo args= 2>/dev/null)
			samples=$((samples + 1))
			[[ $out == *"$secret"* ]] && hits=$((hits + 1))
			sleep 0.01
		done
		echo "$samples $hits" >"$dir/sampler"
	) &
	sampler=$!
	for ((i = 0; i < 200; i++)); do
		code=$(curl -sk --max-time 20 -o "$dir/out" -w '%{http_code}' \
			--data-urlencode "access_key@$dir/id" --data-urlencode "secret_key@$dir/secret" \
			--data-urlencode cmd=v-list-sys-info --data-urlencode returncode=yes \
			"https://127.0.0.1:$port/api/index.php")
		[[ $code == 200 && $(tr -d '[:space:]' <"$dir/out") == 0 ]] && ok=$((ok + 1))
	done
	touch "$dir/stop"
	wait "$sampler"
	echo "calls_ok=$ok"
	echo "samples=$(cut -d' ' -f1 "$dir/sampler")"
	echo "hits=$(cut -d' ' -f2 "$dir/sampler")"
	if [[ -f $authlog ]]; then
		echo log_source=auth.log
		echo "sudo_cmd=$(tail -n "+$((mark + 1))" "$authlog" | grep -c 'hestiaweb : .*COMMAND=')"
		echo "secret_logged=$(tail -n "+$((mark + 1))" "$authlog" | S=$secret awk 'index($0, ENVIRON["S"]) { n++ } END { print n + 0 }')"
	else
		echo log_source=journal
		echo "sudo_cmd=$(journalctl -q --no-pager -o cat --since "@$t0" _COMM=sudo | grep -c 'hestiaweb : .*COMMAND=')"
	fi
	echo "secret_journal=$(journalctl -q --no-pager -o cat --since "@$t0" | S=$secret awk 'index($0, ENVIRON["S"]) { n++ } END { print n + 0 }')"
	echo "secret_hestia_log=$(cat /usr/local/hestia/log/*.log 2>/dev/null | S=$secret awk 'index($0, ENVIRON["S"]) { n++ } END { print n + 0 }')"
}

remote_s3_proc() {
	local u=$1
	# shellcheck disable=SC2016 # the inner bash expands $d
	echo "owners=$(runuser -u "$u" -- bash -c 'for d in /proc/[0-9]*; do stat -c %U "$d"; done 2>/dev/null' | sort -u | paste -sd, -)"
	echo "user_pids=$(runuser -u "$u" -- ls /proc | grep -c '^[0-9]')"
	echo "all_pids=$(find /proc -maxdepth 1 -name '[0-9]*' | wc -l)"
	echo "mount=$(findmnt -no OPTIONS /proc)"
	echo "gid=$(getent group hostisan-proc | cut -d: -f3)"
}

# remote_try NAME CMD...: NAME=ok, or NAME=fail(<rc>).
remote_try() {
	local name=$1 rc out
	shift
	out=$("$@" 2>&1)
	rc=$?
	# "systemctl status" exits 3 when a unit is degraded, "crontab -l" 1 when there is none.
	if ((rc == 0)) || { [[ $name == *systemctl* ]] && ((rc == 3)); } ||
		{ [[ $name == *crontab* ]] && [[ $out == *"no crontab for"* ]]; }; then
		echo "$name=ok"
	else
		echo "$name=fail($rc)"
	fi
}

remote_s3_tools() {
	local u=$1
	remote_try root_sudo_l sudo -n -l
	remote_try root_systemctl systemctl status --no-pager
	remote_try root_loginctl loginctl list-sessions --no-pager
	remote_try root_crontab crontab -l
	remote_try user_systemctl runuser -u "$u" -- systemctl status --no-pager
	remote_try user_loginctl runuser -u "$u" -- loginctl list-sessions --no-pager
	remote_try user_crontab runuser -u "$u" -- crontab -l
	# pkexec is its own package (not polkitd); when it isn't installed hidepid can't break it.
	if command -v pkexec >/dev/null; then
		remote_try root_pkexec pkexec --version
		remote_try user_pkexec runuser -u "$u" -- pkexec --version
	else
		echo pkexec=absent
	fi
	if [[ -x /usr/local/hestia/install/common/hostisan/hidepid.sh ]]; then echo hidepid=present; else echo hidepid=missing; fi
}

remote_s4() {
	echo "rundir=$(stat -c '%a %U' /run/hostisan 2>&1)"
	echo "sock=$(stat -c '%a' /run/hostisan/hostisan.sock 2>&1)"
	echo "tcp=$(ss -ltnpH | grep -c hostisan)"
}

remote_s6() {
	systemctl show hostisan -p NoNewPrivileges -p PrivateTmp -p ActiveState
}

remote_s7_dir() {
	local dir
	dir=$(mktemp -d /tmp/hostisan-s7.XXXXXX) && chmod 755 "$dir" && echo "dir=$dir"
}

remote_s7_run() {
	local dir=$1 out rc
	case $dir in /tmp/hostisan-s7.*) ;; *) echo "verdict=bad-dir" && return 0 ;; esac
	chmod 755 "$dir/runner.vps"
	out=$("$dir/runner.vps" -test.run '^TestSudoTimeoutKillsChildren$' -test.v 2>&1)
	rc=$?
	rm -rf "$dir"
	if ((rc == 0)) && grep -q -- '--- PASS: TestSudoTimeoutKillsChildren' <<<"$out"; then
		echo verdict=pass
	elif grep -q -- '--- SKIP: TestSudoTimeoutKillsChildren' <<<"$out"; then
		echo verdict=skip
	else
		echo "verdict=fail(rc $rc)"
	fi
	awk '{ print "out=" $0 }' <<<"$out" | tail -n 15
}

# remote_s8_state IP PORT: the jail, the INPUT jump and whether IP (or 127.0.0.1) is banned.
remote_s8_state() {
	local ip=$1 port=$2 st jumps chain ignore rst
	if st=$(fail2ban-client status hestia-iptables 2>&1); then echo jail=active; else echo jail=missing; fi
	echo "f2b_banned=$(sed -n 's/.*Banned IP list:[[:space:]]*//p' <<<"$st")"
	jumps=$(iptables -S INPUT | grep -- '-j fail2ban-HESTIA$')
	echo "jumps=$(grep -c . <<<"$jumps")"
	echo "jumps_not_panel=$(grep -vc -- "--dport $port -j" <<<"$jumps")"
	chain=$(iptables -S fail2ban-HESTIA 2>/dev/null)
	echo "chain_has_ip=$(grep -cF -- "-s $ip/32 " <<<"$chain")"
	echo "chain_has_lo=$(grep -cF -- '-s 127.0.0.1/32 ' <<<"$chain")"
	ignore=$(fail2ban-client get hestia-iptables ignoreip 2>/dev/null)
	if grep -qF -- "$ip" <<<"$ignore"; then echo ignored=yes; else echo ignored=no; fi
	if rst=$(fail2ban-client status recidive 2>&1); then
		echo recidive=active
		if grep -qF -- "$ip" <<<"$(sed -n 's/.*Banned IP list:[[:space:]]*//p' <<<"$rst")"; then
			echo recidive_banned=yes
		else
			echo recidive_banned=no
		fi
	else
		echo recidive=missing
	fi
}

# remote_s8_guard IP: keep recidive from turning three panel bans into an all-ports ban.
remote_s8_guard() {
	local ip=$1 marker=/root/.hostisan-c2-recidive
	fail2ban-client status recidive >/dev/null 2>&1 || {
		echo recidive_guard=no-jail
		return 0
	}
	if fail2ban-client get recidive ignoreip 2>/dev/null | grep -qF -- "$ip"; then
		echo recidive_guard=already
	elif fail2ban-client set recidive addignoreip "$ip" >/dev/null; then
		echo "$ip" >"$marker"
		echo recidive_guard=added
	else
		echo recidive_guard=error
	fi
}

remote_s8_unban() {
	local ip=$1 marker=/root/.hostisan-c2-recidive
	fail2ban-client set hestia-iptables unbanip "$ip" >/dev/null 2>&1
	fail2ban-client set recidive unbanip "$ip" >/dev/null 2>&1
	echo unban=done
	if [[ ${2:-} == final && -e $marker ]]; then
		sleep 5 # let recidive read the last Ban lines while the IP is still ignored
		fail2ban-client set recidive delignoreip "$(cat "$marker")" >/dev/null 2>&1
		rm -f "$marker"
		echo recidive_guard=removed
	fi
}

remote_twofa_on() {
	local u=$1 conf=/usr/local/hestia/data/users/$1/user.conf
	if grep -q "^TWOFA='..*'" "$conf"; then
		echo twofa=already
	elif /usr/local/hestia/bin/v-add-user-2fa "$u" >/dev/null; then
		touch "/root/.hostisan-c2-2fa-$u"
		echo twofa=added
	else
		echo twofa=error
	fi
}

remote_twofa_off() {
	local u=$1
	[[ -e /root/.hostisan-c2-2fa-$u ]] || return 0
	/usr/local/hestia/bin/v-delete-user-2fa "$u" >/dev/null && rm -f "/root/.hostisan-c2-2fa-$u"
	echo twofa=removed
}

# ------------------------------------------------------------------ checks

check_s1() {
	local out
	out=$(box s1 "$PANEL_PORT") || problem "ssh failed"
	[[ $(kv listener <<<"$out") == no ]] || problem "something listens on :8085"
	[[ $(kv dir <<<"$out") == absent ]] || problem "/usr/local/hestia/web-terminal exists"
	[[ $(kv nodejs <<<"$out") != *"install ok installed"* ]] || problem "nodejs is installed"
	case "$(kv shell <<<"$out")" in
	404) ;;
	# Hostisan answers unknown paths with the SPA; that is not a terminal.
	200) [[ $(kv spa <<<"$out") == 200 && $(kv same_as_spa <<<"$out") == yes ]] ||
		problem "/_shell/ gave 200 and is not the SPA fallback" ;;
	*) problem "/_shell/ gave HTTP $(kv shell <<<"$out")" ;;
	esac
	verdict S1 "legacy terminal gone: no :8085, no web-terminal dir, no nodejs, /_shell/ $(kv shell <<<"$out")"
}

check_s2() {
	local jar sid path paths
	jar=$(new_jar)
	if admin_login "$jar"; then
		http "$jar" POST /api/v1/files/handoff -H "Origin: $(panel_origin)" -H "X-CSRF-Token: $CSRF"
		sid=$(awk '$6 == "HESTIASID" { print $7 }' "$jar" | tail -n1)
		if [[ $HTTP_STATUS != 200 || -z $sid ]]; then
			problem "file-manager handoff gave HTTP $HTTP_STATUS and no HESTIASID"
		else
			paths=(/list/user/index.php /index.php '/edit/user/index.php?user=admin'
				/fm/../api/index.php /fm/%2e%2e/api/index.php)
			for path in "${paths[@]}"; do
				http "" GET "$path" -H "Cookie: HESTIASID=$sid"
				# hestia-nginx resolves the "..", matches "location = /api/index.php" (Hostisan's
				# legacy API, not PHP) and passes the raw URI on; Go's mux then redirects to the
				# cleaned path. That redirect never reaches PHP.
				[[ $path == /fm/../api/index.php && $HTTP_STATUS == 301 &&
					$HTTP_LOCATION == "$PANEL_URL/api/index.php" ]] && continue
				[[ $HTTP_STATUS == 404 ]] || problem "$path gave HTTP $HTTP_STATUS with the FM session"
			done
			http "" GET /fm/ -H "Cookie: HESTIASID=$sid"
			[[ $HTTP_STATUS == 200 ]] || problem "/fm/ gave HTTP $HTTP_STATUS with the FM session"
		fi
		panel_logout "$jar"
	fi
	verdict S2 "an FM session gets 404 on the old panel pages and 200 on /fm/"
}

check_s3() {
	local out proc tools n bad=() k
	need_test_user || {
		verdict S3 ""
		return
	}
	out=$(box s3_secret "$PANEL_PORT" "$KEY_USER") || problem "ssh failed"
	if [[ -n $(kv error <<<"$out") ]]; then
		problem "$(kv error <<<"$out")"
	else
		[[ $(kv api_allow <<<"$out") == added ]] && note S3 "127.0.0.1 was added to API_ALLOWED_IP for the run and restored"
		[[ $(kv calls_ok <<<"$out") == 200 ]] ||
			problem "only $(kv calls_ok <<<"$out")/200 access-key calls succeeded (API_SYSTEM=$(kv api_system <<<"$out"))"
		n=$(kv samples <<<"$out")
		((${n:-0} >= 50)) || problem "the ps sampler took only ${n:-0} samples"
		[[ $(kv hits <<<"$out") == 0 ]] || problem "the secret showed up in ps $(kv hits <<<"$out") times"
		[[ $(kv sudo_cmd <<<"$out") == 0 ]] ||
			problem "$(kv sudo_cmd <<<"$out") 'hestiaweb : ... COMMAND=' lines in $(kv log_source <<<"$out")"
		for k in secret_logged secret_journal secret_hestia_log; do
			n=$(kv "$k" <<<"$out")
			[[ -z $n || $n == 0 ]] || problem "the secret is in the logs ($k=$n)"
		done
	fi

	proc=$(box s3_proc "$TEST_USER") || problem "ssh failed"
	[[ $(kv owners <<<"$proc") == "$TEST_USER" ]] ||
		problem "$TEST_USER sees processes of: $(kv owners <<<"$proc")"
	[[ $(kv mount <<<"$proc") == *hidepid=invisible* || $(kv mount <<<"$proc") == *hidepid=2* ]] ||
		problem "/proc is mounted without hidepid ($(kv mount <<<"$proc"))"
	[[ -n $(kv gid <<<"$proc") && $(kv mount <<<"$proc") == *"gid=$(kv gid <<<"$proc")"* ]] ||
		problem "/proc has no gid=hostisan-proc exemption"
	local flag lost=
	for flag in nosuid nodev noexec; do
		[[ ,$(kv mount <<<"$proc"), == *",$flag,"* ]] || lost=yes
	done
	[[ -z $lost ]] || problem "/proc is mounted without nosuid,nodev,noexec ($(kv mount <<<"$proc"))"

	box ping >/dev/null || problem "ssh login failed"
	tools=$(box s3_tools "$TEST_USER") || problem "ssh failed"
	mapfile -t bad < <(grep -E '^(root|user)_[a-z_]+=fail' <<<"$tools")
	((${#bad[@]} == 0)) || problem "broken under hidepid: ${bad[*]}"
	[[ $(kv hidepid <<<"$tools") == present ]] || problem "hidepid.sh is missing on the box"
	[[ $(kv pkexec <<<"$tools") == absent ]] && note S3 "pkexec is not installed, so it was not tried"
	note S3 "rollback: /usr/local/hestia/install/common/hostisan/hidepid.sh --rollback"
	verdict S3 "secret never in ps or logs ($(kv samples <<<"$out") samples), no sudo COMMAND lines, $TEST_USER sees only its own processes, tools work"
}

check_s4() {
	local out mark lines jar
	out=$(box s4) || problem "ssh failed"
	[[ $(kv rundir <<<"$out") == "700 hestiaweb" ]] || problem "/run/hostisan is '$(kv rundir <<<"$out")'"
	[[ $(kv sock <<<"$out") == 660 ]] || problem "the socket mode is '$(kv sock <<<"$out")'"
	[[ $(kv tcp <<<"$out") == 0 ]] || problem "hostisan has a TCP listener"
	if need_test_user; then
		mark=$(box authlog_mark)
		jar=$(new_jar)
		http "$jar" GET /api/v1/bootstrap
		CSRF=$(jq -r '.csrfToken // empty' "$WORK/body" 2>/dev/null)
		http "$jar" POST /api/v1/auth/login -H 'Content-Type: application/json' -H "Origin: $(panel_origin)" \
			-H "X-CSRF-Token: $CSRF" -H 'X-Real-IP: 9.9.9.9' -H 'X-Forwarded-For: 9.9.9.9' \
			--data-binary @<(login_body "$TEST_USER" <(wrong_password))
		lines=$(box authlog_since "${mark:-0}")
		grep -qF -- " $TEST_USER $OWNER_IP failed to login" <<<"$lines" ||
			problem "no failed-login line with the owner IP"
		! grep -qF -- 9.9.9.9 <<<"$lines" || problem "the spoofed 9.9.9.9 reached the auth log"
	fi
	verdict S4 "/run/hostisan 700 hestiaweb, socket 660, no TCP, spoofed X-Real-IP replaced by $OWNER_IP"
}

check_s5() {
	local jar code
	jar=$(new_jar)
	if admin_login "$jar"; then
		panel_logout "$jar"
	fi
	jar=$(new_jar)
	if [[ -n $ADMIN_PASSWORD_FILE && -r $ADMIN_PASSWORD_FILE ]]; then
		panel_login "$jar" "$ADMIN_USER" "$ADMIN_PASSWORD_FILE" https://evil.example.com
		code=$(jq -r '.error.code // empty' "$WORK/body" 2>/dev/null)
		# Hostisan's CSRF middleware answers 400 "csrf"; the brief expected 403.
		if [[ $LOGIN_RESULT == ok ]]; then
			problem "a login with Origin https://evil.example.com succeeded"
		elif ! [[ $HTTP_STATUS == 403 || ($HTTP_STATUS == 400 && $code == csrf) ]]; then
			problem "a wrong Origin gave HTTP $HTTP_STATUS '$code', not 403 or 400 csrf"
		fi
	fi
	verdict S5 "login with Origin $(panel_origin) works; a wrong Origin is refused (HTTP $HTTP_STATUS)"
}

check_s6() {
	local out jar n
	out=$(box s6) || problem "ssh failed"
	[[ $(kv NoNewPrivileges <<<"$out") == no ]] || problem "NoNewPrivileges=$(kv NoNewPrivileges <<<"$out")"
	[[ $(kv PrivateTmp <<<"$out") == no ]] || problem "PrivateTmp=$(kv PrivateTmp <<<"$out")"
	jar=$(new_jar)
	if admin_login "$jar"; then
		http "$jar" GET /api/v1/users
		n=$(jq -r '.users | length' "$WORK/body" 2>/dev/null)
		[[ $HTTP_STATUS == 200 && ${n:-0} -ge 1 ]] || problem "GET /api/v1/users gave HTTP $HTTP_STATUS with ${n:-no} users"
		panel_logout "$jar"
	fi
	verdict S6 "NoNewPrivileges=no PrivateTmp=no, the panel lists ${n:-0} users through sudo"
}

check_s7() {
	local dir out
	if [[ -z $S7_BINARY || ! -f $S7_BINARY ]]; then
		problem "no test binary; run '$0 prepare-s7 <out>' and pass --s7-binary"
	else
		dir=$(box s7_dir | kv dir)
		if [[ -z $dir ]] || ! box_copy "$S7_BINARY" "$dir/runner.vps"; then
			problem "could not copy the test binary to the box"
		else
			out=$(box s7_run "$dir")
			[[ $(kv verdict <<<"$out") == pass ]] ||
				problem "TestSudoTimeoutKillsChildren: $(kv verdict <<<"$out") (on FAIL apply the B6 step 5 timeout wrapper): $(sed -n 's/^out=//p' <<<"$out" | tail -n3 | paste -sd'|' -)"
		fi
	fi
	verdict S7 "TestSudoTimeoutKillsChildren passes as hestiaweb"
}

# s8_wait_ban yes|no: polls until the owner IP is (or is no longer) banned.
s8_wait_ban() {
	local want=$1 waited=0 st banned
	while :; do
		st=$(box s8_state "$OWNER_IP" "$PANEL_PORT")
		banned=no
		[[ " $(kv f2b_banned <<<"$st") " == *" $OWNER_IP "* && $(kv chain_has_ip <<<"$st") -ge 1 ]] && banned=yes
		[[ $banned == "$want" ]] && return 0
		((waited >= BAN_WAIT)) && return 1
		pause "$POLL"
		waited=$((waited + POLL))
	done
}

# s8_round NAME ATTEMPT-FUNCTION COUNT: COUNT failures, then the ban, then the unban.
s8_round() {
	local name=$1 attempt=$2 count=$3 mark lines n i st
	mark=$(box authlog_mark)
	for ((i = 0; i < count; i++)); do
		"$attempt"
	done
	lines=$(box authlog_since "${mark:-0}")
	n=$(grep -cF -- " $OWNER_IP failed to login" <<<"$lines")
	# Earlier failures (S4) still count in fail2ban's findtime, so a ban can need fewer.
	((n >= 1)) || problem "$name: no failed-login lines with $OWNER_IP"
	! grep -qF -- ' 127.0.0.1 failed to login' <<<"$lines" || problem "$name: a failed-login line names 127.0.0.1"
	if s8_wait_ban yes; then
		st=$(box s8_state "$OWNER_IP" "$PANEL_PORT")
		[[ $(kv chain_has_lo <<<"$st") == 0 ]] || problem "$name: 127.0.0.1 is banned"
		box ping >/dev/null || problem "$name: ssh stopped working during the ban"
		HTTP_TIMEOUT=5 http "" GET /api/v1/bootstrap
		[[ $HTTP_STATUS == 000 ]] || problem "$name: the panel still answered during the ban (HTTP $HTTP_STATUS)"
	else
		problem "$name: $OWNER_IP was not banned within ${BAN_WAIT}s"
	fi
	box s8_unban "$OWNER_IP" >/dev/null
	s8_wait_ban no || problem "$name: $OWNER_IP is still banned after unbanip"
	http "" GET /api/v1/bootstrap
	[[ $HTTP_STATUS == 200 ]] || problem "$name: the panel gave HTTP $HTTP_STATUS after the unban"
}

s8_bad_panel_login() {
	local jar
	jar=$(new_jar)
	panel_login "$jar" "$TEST_USER" <(wrong_password) "$(panel_origin)"
}

s8_bad_api_key() {
	local id
	id=$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom 2>/dev/null | head -c 20) || true
	http "" POST /api/index.php --data-urlencode "access_key=$id" \
		--data-urlencode "secret_key=c2wrongsecret$RANDOM$RANDOM$RANDOM$RANDOM" \
		--data-urlencode cmd=v-list-sys-info --data-urlencode returncode=yes
}

s8_bad_twofa() {
	local jar
	jar=$(new_jar)
	panel_login "$jar" "$TEST_USER" "$TEST_PASSWORD_FILE" "$(panel_origin)" 000000
}

check_s8() {
	local st out jar
	need_test_user || {
		verdict S8 ""
		return
	}
	st=$(box s8_state "$OWNER_IP" "$PANEL_PORT") || problem "ssh failed"
	# Safety first: a ban must only ever close the panel port.
	[[ $(kv jail <<<"$st") == active ]] || problem "the hestia-iptables jail is not running"
	[[ $(kv jumps <<<"$st") -ge 1 && $(kv jumps_not_panel <<<"$st") == 0 ]] ||
		problem "the INPUT jump to fail2ban-HESTIA is not limited to port $PANEL_PORT; not banning"
	[[ $(kv ignored <<<"$st") == no ]] || problem "$OWNER_IP is in the jail's ignoreip; it can't be banned"
	[[ $(kv chain_has_ip <<<"$st") == 0 ]] || problem "$OWNER_IP is already banned"
	[[ $(kv recidive_banned <<<"$st") != yes ]] || problem "$OWNER_IP is banned by recidive"
	if ((${#PROBLEMS[@]})); then
		verdict S8 ""
		return
	fi
	S8_STARTED=1
	out=$(box s8_guard "$OWNER_IP")
	[[ $(kv recidive_guard <<<"$out") != error ]] || problem "could not add $OWNER_IP to recidive's ignoreip"
	if ((${#PROBLEMS[@]} == 0)); then
		s8_round panel s8_bad_panel_login 6

		out=$(box api_allow "$OWNER_IP")
		API_TOUCHED=1
		if [[ $(kv api_system <<<"$out") =~ ^[1-9] && $(kv api_allow <<<"$out") != error ]]; then
			s8_round api s8_bad_api_key 6
		else
			problem "api: the legacy API can't take access keys (API_SYSTEM=$(kv api_system <<<"$out"), allow=$(kv api_allow <<<"$out"))"
		fi
		box api_restore >/dev/null && API_TOUCHED=

		if [[ -z $TEST_PASSWORD_FILE || ! -r $TEST_PASSWORD_FILE ]]; then
			problem "2fa: no test user password file (--test-password-file)"
		else
			out=$(box twofa_on "$TEST_USER")
			TWOFA_TOUCHED=1
			jar=$(new_jar)
			panel_login "$jar" "$TEST_USER" "$TEST_PASSWORD_FILE" "$(panel_origin)"
			if [[ $(kv twofa <<<"$out") == error ]]; then
				problem "2fa: could not turn 2FA on for $TEST_USER"
			elif [[ $LOGIN_RESULT != twofa_required ]]; then
				problem "2fa: $TEST_USER's password step gave '$LOGIN_RESULT', not twofa_required"
			else
				# 3 unlogged typos, then logged failures and the per-user lockout lines.
				s8_round 2fa s8_bad_twofa 10
			fi
			box twofa_off "$TEST_USER" >/dev/null && TWOFA_TOUCHED=
		fi
	fi
	box s8_unban "$OWNER_IP" final >/dev/null && S8_STARTED=
	verdict S8 "panel, /api/ and 2FA failures each banned $OWNER_IP on port $PANEL_PORT only, then unbanned"
}

# ------------------------------------------------------------------ main

cleanup() {
	local jar
	if [[ -n $S8_STARTED ]]; then
		box s8_unban "$OWNER_IP" final >/dev/null 2>&1
		S8_STARTED=
	fi
	if [[ -n $API_TOUCHED ]]; then
		box api_restore >/dev/null 2>&1
		API_TOUCHED=
	fi
	if [[ -n $TWOFA_TOUCHED ]]; then
		box twofa_off "$TEST_USER" >/dev/null 2>&1
		TWOFA_TOUCHED=
	fi
	for jar in "${LOGOUT_JARS[@]}"; do
		[[ -f $jar ]] && panel_logout "$jar" >/dev/null 2>&1
	done
	LOGOUT_JARS=()
	[[ -n $WORK && -d $WORK ]] && rm -rf "$WORK"
	WORK=
}

preflight() {
	local tool out
	for tool in curl jq ssh scp; do
		command -v "$tool" >/dev/null || {
			fail PRE "$tool is not installed here"
			return 1
		}
	done
	[[ -n $VPS ]] || {
		fail PRE "no --vps"
		return 1
	}
	[[ -n $OWNER_IP ]] || {
		fail PRE "no --owner-ip"
		return 1
	}
	out=$(box ping) || {
		fail PRE "ssh $VPS failed"
		return 1
	}
	[[ $(kv uid <<<"$out") == 0 ]] || {
		fail PRE "ssh $VPS is not root"
		return 1
	}
	# The panel requests come from here; they must arrive as the owner IP.
	[[ $(kv client <<<"$out") == "$OWNER_IP" ]] || {
		fail PRE "the box sees this machine as '$(kv client <<<"$out")', not $OWNER_IP"
		return 1
	}
	if [[ -n $TEST_USER ]]; then
		[[ $(box test_user "$TEST_USER" | kv exists) == yes ]] || {
			fail PRE "test user $TEST_USER does not exist on the box"
			return 1
		}
	fi
}

prepare_s7() {
	local out=${1:-}
	[[ -n $out ]] || usage
	out=$(realpath -m "$out")
	(cd "$REPO/hostisan" && CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
		go test -c -tags vps -o "$out" ./internal/runner) || return 1
	echo "built $out; pass it as --s7-binary"
}

main() {
	local checks=() c host
	case ${1:-} in
	prepare-s7)
		shift
		prepare_s7 "$@"
		return
		;;
	run) shift ;;
	-h | --help) usage 0 ;;
	esac
	while (($#)); do
		case $1 in
		--vps | --owner-ip | --admin-user | --key-user | --admin-password-file | --test-user | --test-password-file | --s7-binary | --panel-url)
			(($# >= 2)) || usage
			case $1 in
			--vps) VPS=$2 ;;
			--owner-ip) OWNER_IP=$2 ;;
			--admin-user) ADMIN_USER=$2 ;;
			--key-user) KEY_USER=$2 ;;
			--admin-password-file) ADMIN_PASSWORD_FILE=$2 ;;
			--test-user) TEST_USER=$2 ;;
			--test-password-file) TEST_PASSWORD_FILE=$2 ;;
			--s7-binary) S7_BINARY=$2 ;;
			--panel-url) PANEL_URL=$2 ;;
			esac
			shift 2
			;;
		S[1-8]) checks+=("$1") && shift ;;
		*) usage ;;
		esac
	done
	((${#checks[@]})) || checks=("${ALL_CHECKS[@]}")
	if [[ -z $PANEL_URL ]]; then
		host=${VPS#*@}
		PANEL_URL=https://$host:$PANEL_PORT
	fi
	PANEL_URL=${PANEL_URL%/}

	WORK=$(mktemp -d "${TMPDIR:-/tmp}/hostisan-c2.XXXXXX") || exit 1
	chmod 700 "$WORK"
	trap cleanup EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM

	if preflight; then
		for c in "${checks[@]}"; do
			"check_${c,,}"
		done
	fi
	summary
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
	main "$@"
fi
