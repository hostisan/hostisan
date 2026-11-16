#!/usr/bin/env bats
# Tests for tools/vps/check-security.sh: every check's PASS/FAIL logic against a fake box
# (box) and a fake panel (http). No root, no network, no VPS.
# Run: bats test/hostisan/check-security.bats
# shellcheck source-path=SCRIPTDIR
# shellcheck disable=SC2034,SC2031,SC2317 # settings read by the sourced script; fakes called by it

bats_require_minimum_version 1.5.0

OWNER=203.0.113.7

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	script=$repo/tools/vps/check-security.sh
	tmp=$(mktemp -d "${BATS_TMPDIR:-/tmp}/check-security.XXXXXX")
	# shellcheck source=../../tools/vps/check-security.sh
	source "$script"
	WORK=$tmp/work
	mkdir -p "$WORK"
	VPS=root@$OWNER
	OWNER_IP=$OWNER
	PANEL_URL=https://$OWNER:8083
	TEST_USER=bob
	printf 'Adm1n-Secret-PW\n' >"$tmp/admin.pw"
	printf 'B0b-Secret-PW\n' >"$tmp/bob.pw"
	ADMIN_PASSWORD_FILE=$tmp/admin.pw
	TEST_PASSWORD_FILE=$tmp/bob.pw
	BAN_WAIT=3
	POLL=1
	# The script defines the real transport; the fakes replace it.
	box() { _box "$@"; }
	box_copy() { _box_copy "$@"; }
	http() { _http "$@"; }
	pause() { :; }
	: >"$tmp/authlog"
	echo 0 >"$tmp/unban_mark"
	echo 0 >"$tmp/twofa_fails"
}

teardown() {
	rm -rf "$tmp"
}

# ---------------------------------------------------------------- fake box

_box() {
	echo "$*" >>"$tmp/calls"
	"fake_$1" "${@:2}"
}

_box_copy() {
	echo "copy $*" >>"$tmp/calls"
	cp "$1" "$tmp/copied"
}

fake_ping() { printf 'uid=0\nclient=%s\n' "${FAKE_CLIENT:-$OWNER}"; }
fake_test_user() { echo exists=yes; }
fake_s1() {
	printf 'listener=%s\ndir=%s\nnodejs=%s\nshell=%s\nspa=200\nsame_as_spa=%s\n' \
		"${S1_LISTENER:-no}" "${S1_DIR:-absent}" "${S1_NODE:-not-installed}" "${S1_SHELL:-404}" "${S1_SAME:-yes}"
}
fake_authlog_mark() { wc -l <"$tmp/authlog"; }
fake_authlog_since() { tail -n "+$(($1 + 1))" "$tmp/authlog"; }
fake_s3_secret() {
	printf 'api_system=1\napi_allow=already\ncalls_ok=%s\nsamples=%s\nhits=%s\nlog_source=auth.log\nsudo_cmd=%s\n' \
		"${S3_OK:-200}" "${S3_SAMPLES:-900}" "${S3_HITS:-0}" "${S3_SUDO:-0}"
	printf 'secret_logged=0\nsecret_journal=0\nsecret_hestia_log=0\n'
}
fake_s3_proc() {
	printf 'owners=%s\nuser_pids=3\nall_pids=120\nmount=%s\ngid=997\n' \
		"${S3_OWNERS:-bob}" "${S3_MOUNT:-rw,nosuid,nodev,noexec,relatime,gid=997,hidepid=invisible}"
}
fake_s3_tools() {
	printf 'root_sudo_l=ok\nroot_systemctl=ok\nroot_loginctl=ok\nroot_crontab=ok\n'
	printf 'user_systemctl=ok\nuser_loginctl=%s\nuser_crontab=ok\nhidepid=present\n' "${S3_LOGINCTL:-ok}"
	if [[ -n ${S3_NO_PKEXEC:-} ]]; then echo pkexec=absent; else printf 'root_pkexec=ok\nuser_pkexec=ok\n'; fi
}
fake_s4() { printf 'rundir=%s\nsock=%s\ntcp=0\n' "${S4_DIR:-700 hestiaweb}" "${S4_SOCK:-660}"; }
fake_s6() { printf 'NoNewPrivileges=%s\nPrivateTmp=no\nActiveState=active\n' "${S6_NNP:-no}"; }
fake_s7_dir() { echo dir=/tmp/hostisan-s7.abc; }
fake_s7_run() { printf 'verdict=%s\nout=--- %s: TestSudoTimeoutKillsChildren\n' "${S7_VERDICT:-pass}" "${S7_VERDICT:-pass}"; }

banned() {
	[[ -z ${S8_NO_BAN:-} ]] && (($(wc -l <"$tmp/authlog") - $(cat "$tmp/unban_mark") >= 5))
}
fake_s8_state() {
	echo jail=active
	if banned; then echo "f2b_banned=$OWNER"; else echo f2b_banned=; fi
	echo jumps=1
	echo "jumps_not_panel=${S8_JUMPS_OTHER:-0}"
	if banned; then echo chain_has_ip=1; else echo chain_has_ip=0; fi
	echo chain_has_lo=0
	echo ignored=no
	echo recidive=active
	echo recidive_banned=no
}
fake_s8_guard() { echo recidive_guard=added; }
fake_s8_unban() {
	wc -l <"$tmp/authlog" >"$tmp/unban_mark"
	echo unban=done
}
fake_api_allow() { printf 'api_system=1\napi_allow=added\n'; }
fake_api_restore() { echo api_restore=done; }
fake_twofa_on() {
	touch "$tmp/twofa"
	echo twofa=added
}
fake_twofa_off() { rm -f "$tmp/twofa"; }

# ---------------------------------------------------------------- fake panel

failed_line() { echo "2026-10-29 10:00:00 $1 $2 failed to login" >>"$tmp/authlog"; }

reply() {
	HTTP_STATUS=$1
	HTTP_LOCATION=${3:-}
	printf '%s' "${2:-}" >"$WORK/body"
}

_http() {
	local jar=$1 method=$2 path=$3 origin='' realip=$OWNER cookie='' data='' u p c n
	shift 3
	while (($#)); do
		case $1 in
		-H)
			case $2 in
			Origin:*) origin=${2#Origin: } ;;
			X-Real-IP:*) [[ -n ${S4_LEAK:-} ]] && realip=${2#X-Real-IP: } ;;
			Cookie:*) cookie=${2#Cookie: } ;;
			esac
			shift 2
			;;
		--data-binary)
			data=$(cat "${2#@}")
			shift 2
			;;
		*) shift ;;
		esac
	done
	echo "$method $path" >>"$tmp/http"
	if banned; then
		reply 000
		return
	fi
	case "$method $path" in
	"GET /api/v1/bootstrap") reply 200 '{"csrfToken":"tok"}' ;;
	"POST /api/v1/auth/login")
		if [[ $origin != "https://$OWNER:8083" ]]; then
			reply "${S5_BAD_ORIGIN:-400}" '{"error":{"code":"csrf","message":""}}'
			return
		fi
		u=$(jq -r .username <<<"$data")
		p=$(jq -r .password <<<"$data")
		c=$(jq -r '.twofa // ""' <<<"$data")
		if [[ $u == admin && $p == Adm1n-Secret-PW ]]; then
			reply 200 '{"status":"ok"}'
		elif [[ $u == bob && $p == B0b-Secret-PW && -e $tmp/twofa && -z $c ]]; then
			reply 200 '{"status":"twofa_required"}'
		elif [[ $u == bob && $p == B0b-Secret-PW && -e $tmp/twofa ]]; then
			n=$(($(cat "$tmp/twofa_fails") + 1))
			echo "$n" >"$tmp/twofa_fails"
			((n > 3)) && failed_line bob "$realip"
			reply 401 '{"error":{"code":"login_failed"}}'
		else
			failed_line "$u" "$realip"
			reply 401 '{"error":{"code":"login_failed"}}'
		fi
		;;
	"POST /api/v1/files/handoff")
		printf '#HttpOnly_%s\tFALSE\t/\tTRUE\t0\tHESTIASID\t%s\n' "$OWNER" "$(printf 'a%.0s' {1..64})" >>"$jar"
		reply 200 '{"url":"/fm/"}'
		;;
	"POST /api/v1/auth/logout") reply 200 '{}' ;;
	"GET /api/v1/users") reply 200 '{"users":[{"USER":"admin"},{"USER":"bob"}]}' ;;
	"POST /api/index.php")
		failed_line apikey "$OWNER"
		reply 200 '3'
		;;
	"GET /fm/")
		if [[ $cookie == HESTIASID=* ]]; then reply 200 filegator; else reply 302; fi
		;;
	"GET /fm/../api/index.php")
		if [[ -n ${S2_REDIRECT:-} ]]; then reply 301 '' "$PANEL_URL$S2_REDIRECT"; else reply 404; fi
		;;
	"GET "*)
		if [[ $path == "${S2_LEAK:-none}" ]]; then reply 200 'old panel'; else reply 404; fi
		;;
	*) reply 404 ;;
	esac
}

# ---------------------------------------------------------------- tests

@test "script is shellcheck clean" {
	command -v shellcheck >/dev/null || skip "no shellcheck"
	run shellcheck -x "$script" "$BATS_TEST_FILENAME"
	[ "$status" -eq 0 ]
}

@test "box ships the remote functions" {
	mkdir -p "$tmp/bin"
	# A fake ssh that runs the shipped script with a local bash.
	printf '#!/bin/sh\nexec bash -s\n' >"$tmp/bin/ssh"
	chmod +x "$tmp/bin/ssh"
	unset -f box
	# shellcheck source=../../tools/vps/check-security.sh
	source "$script"
	SSH_CLIENT="$OWNER 5555 22" PATH=$tmp/bin:$PATH run box ping
	[ "$status" -eq 0 ]
	[[ $output == *"uid=$(id -u)"* ]]
	[[ $output == *"client=$OWNER"* ]]
}

@test "bad usage exits 2" {
	run "$script" --bogus
	[ "$status" -eq 2 ]
	run "$script" S9
	[ "$status" -eq 2 ]
}

@test "S1 passes when the terminal is gone" {
	run check_s1
	[ "$output" = "PASS S1 legacy terminal gone: no :8085, no web-terminal dir, no nodejs, /_shell/ 404" ]
	S1_SHELL=200 run check_s1
	[[ $output == "PASS S1"* ]]
}

@test "S1 fails on any leftover" {
	S1_LISTENER=yes S1_NODE="install ok installed" run check_s1
	[[ $output == "FAIL S1 something listens on :8085; nodejs is installed" ]]
	S1_SHELL=200 S1_SAME=no run check_s1
	[[ $output == "FAIL S1 /_shell/ gave 200 and is not the SPA fallback" ]]
}

@test "S2 passes and logs the admin out" {
	run check_s2
	[[ $output == "PASS S2"* ]]
	grep -qx 'GET /fm/' "$tmp/http"
	grep -qx 'POST /api/v1/auth/logout' "$tmp/http"
}

@test "S2 fails when an old page answers" {
	S2_LEAK=/index.php run check_s2
	[ "$output" = "FAIL S2 /index.php gave HTTP 200 with the FM session" ]
}

@test "S2 passes when nginx's cleaned /fm/../api/index.php redirects to the legacy API" {
	S2_REDIRECT=/api/index.php run check_s2
	[[ $output == "PASS S2"* ]]
}

@test "S2 fails when /fm/../api/index.php redirects anywhere else" {
	S2_REDIRECT=/list/user/index.php run check_s2
	[[ $output == "FAIL S2 /fm/../api/index.php gave HTTP 301"* ]]
}

@test "S3 passes with a note when pkexec is not installed" {
	S3_NO_PKEXEC=1 run check_s3
	[[ ${lines[-1]} == "PASS S3"* ]]
	[[ $output == *"NOTE S3 pkexec is not installed"* ]]
}

@test "S3 passes" {
	run check_s3
	[[ ${lines[-1]} == "PASS S3"* ]]
	[[ $output == *"NOTE S3 rollback: "*"hidepid.sh --rollback"* ]]
}

@test "S3 fails on a seen secret, sudo lines, other processes, broken tools" {
	S3_HITS=2 S3_SUDO=400 S3_OWNERS=bob,root S3_LOGINCTL='fail(1)' S3_MOUNT=rw,hidepid=invisible run check_s3
	[[ ${lines[-1]} == "FAIL S3 "* ]]
	[[ $output == *"the secret showed up in ps 2 times"* ]]
	[[ $output == *"400 'hestiaweb : ... COMMAND=' lines"* ]]
	[[ $output == *"bob sees processes of: bob,root"* ]]
	[[ $output == *"no gid=hostisan-proc exemption"* ]]
	[[ $output == *"/proc is mounted without nosuid,nodev,noexec (rw,hidepid=invisible)"* ]]
	[[ $output == *"broken under hidepid: user_loginctl=fail(1)"* ]]
}

@test "S3 makes its key for admin when the panel login is another user" {
	# Hestia gives a key without a permission list only to the user named admin,
	# so S3's box-side key never follows --admin-user (the UI login).
	run main --owner-ip "$OWNER" --admin-user rc5adm --admin-password-file "$tmp/admin.pw" S3
	[ "$status" -eq 0 ]
	[[ ${lines[-1]} == "SUMMARY 1 passed, 0 failed" ]]
	grep -qx 's3_secret 8083 admin' "$tmp/calls"
	: >"$tmp/calls"
	run main --owner-ip "$OWNER" --admin-user rc5adm --key-user root2 S3
	[ "$status" -eq 0 ]
	grep -qx 's3_secret 8083 root2' "$tmp/calls"
	run main --key-user
	[ "$status" -eq 2 ]
}

@test "S3 fails when the sampler did not run long" {
	S3_SAMPLES=4 S3_OK=150 run check_s3
	[[ $output == *"only 150/200 access-key calls succeeded"* ]]
	[[ $output == *"the ps sampler took only 4 samples"* ]]
}

@test "S4 passes and the auth log has the owner IP" {
	run check_s4
	[[ $output == "PASS S4"* ]]
	grep -q " bob $OWNER failed to login" "$tmp/authlog"
}

@test "S4 fails on a spoofed IP or a loose socket" {
	S4_LEAK=1 S4_SOCK=666 run check_s4
	[[ $output == "FAIL S4 "* ]]
	[[ $output == *"the socket mode is '666'"* ]]
	[[ $output == *"no failed-login line with the owner IP"* ]]
	[[ $output == *"the spoofed 9.9.9.9 reached the auth log"* ]]
}

@test "S5 passes on 400 csrf and 403" {
	run check_s5
	[[ $output == "PASS S5"* ]]
	S5_BAD_ORIGIN=403 run check_s5
	[[ $output == "PASS S5"* ]]
}

@test "S5 fails when a wrong Origin gets through" {
	S5_BAD_ORIGIN=500 run check_s5
	[[ $output == "FAIL S5 a wrong Origin gave HTTP 500"* ]]
}

@test "S6 passes and fails on NoNewPrivileges" {
	run check_s6
	[ "$output" = "PASS S6 NoNewPrivileges=no PrivateTmp=no, the panel lists 2 users through sudo" ]
	S6_NNP=yes run check_s6
	[[ $output == "FAIL S6 NoNewPrivileges=yes" ]]
}

@test "S6 fails on a wrong admin password" {
	printf 'nope\n' >"$tmp/admin.pw"
	run check_s6
	[[ $output == "FAIL S6 admin login gave 'login_failed' (HTTP 401)" ]]
}

@test "S7 copies the binary and reads the verdict" {
	S7_BINARY=$tmp/runner.vps
	echo bin >"$S7_BINARY"
	run check_s7
	[[ $output == "PASS S7"* ]]
	grep -q "copy $tmp/runner.vps /tmp/hostisan-s7.abc/runner.vps" "$tmp/calls"
	S7_VERDICT=skip run check_s7
	[[ $output == "FAIL S7 TestSudoTimeoutKillsChildren: skip"* ]]
}

@test "S7 fails without a binary" {
	S7_BINARY=
	run check_s7
	[[ $output == "FAIL S7 no test binary"* ]]
}

@test "S8 bans and unbans three times" {
	run check_s8
	[[ $output == "PASS S8"* ]]
	[ "$(grep -c '^s8_unban' "$tmp/calls")" -eq 4 ]
	grep -qx "s8_unban $OWNER final" "$tmp/calls"
	grep -qx "s8_guard $OWNER" "$tmp/calls"
	grep -qx 'api_restore' "$tmp/calls"
	grep -qx 'twofa_off bob' "$tmp/calls"
	# 3 unlogged typos and 5 logged failures, then the ban drops the rest.
	[ "$(cat "$tmp/twofa_fails")" -eq 8 ]
	[ ! -e "$tmp/twofa" ]
}

@test "S8 refuses to ban when the jump covers other ports" {
	S8_JUMPS_OTHER=1 run check_s8
	[[ $output == "FAIL S8 the INPUT jump to fail2ban-HESTIA is not limited to port 8083; not banning" ]]
	run ! grep -q 'auth/login' "$tmp/http"
	run ! grep -q '^s8_guard' "$tmp/calls"
}

@test "S8 fails when no ban comes and still unbans" {
	S8_NO_BAN=1 run check_s8
	[[ $output == "FAIL S8 "* ]]
	[[ $output == *"panel: $OWNER was not banned within 3s"* ]]
	grep -qx "s8_unban $OWNER final" "$tmp/calls"
}

@test "cleanup unbans, restores and logs out after an interrupted S8" {
	S8_STARTED=1 API_TOUCHED=1 TWOFA_TOUCHED=1
	LOGOUT_JARS=("$WORK/jar.x")
	: >"$WORK/jar.x"
	cleanup
	grep -qx "s8_unban $OWNER final" "$tmp/calls"
	grep -qx 'api_restore' "$tmp/calls"
	grep -qx 'twofa_off bob' "$tmp/calls"
	[ ! -d "$tmp/work" ]
}

@test "the EXIT trap unbans when the run is killed mid S8" {
	run bash -c "
		source '$script'
		box() { echo \"\$*\" >>'$tmp/calls'; [[ \$1 != ping ]] || printf 'uid=0\\nclient=$OWNER\\n'; }
		check_s8() { S8_STARTED=1; kill -TERM \$\$; sleep 5; }
		main --vps root@$OWNER --owner-ip $OWNER S8
	"
	[ "$status" -eq 143 ]
	grep -qx "s8_unban $OWNER final" "$tmp/calls"
}

@test "main runs every check, prints a summary and never the passwords" {
	S7_BINARY=$tmp/runner.vps
	echo bin >"$S7_BINARY"
	run main --vps "root@$OWNER" --owner-ip "$OWNER" --admin-password-file "$tmp/admin.pw" \
		--test-user bob --test-password-file "$tmp/bob.pw" --s7-binary "$S7_BINARY"
	[ "$status" -eq 0 ]
	[ "$(grep -c '^PASS S[1-8] ' <<<"$output")" -eq 8 ]
	[ "${lines[-1]}" = "SUMMARY 8 passed, 0 failed" ]
	[[ $output != *Adm1n-Secret-PW* && $output != *B0b-Secret-PW* ]]
}

@test "main exits 1 with the failed ids" {
	S6_NNP=yes run main --owner-ip "$OWNER" --admin-password-file "$tmp/admin.pw" S6 S1
	[ "$status" -eq 1 ]
	[ "${lines[-1]}" = "SUMMARY 1 passed, 1 failed: S6" ]
}

@test "main stops when the box sees another client IP" {
	FAKE_CLIENT=198.51.100.1 run main --owner-ip "$OWNER" S1
	[ "$status" -eq 1 ]
	[[ ${lines[0]} == "FAIL PRE the box sees this machine as '198.51.100.1', not $OWNER" ]]
	run ! grep -q '^s1' "$tmp/calls"
}

@test "prepare-s7 builds the vps-tagged runner test binary" {
	mkdir -p "$tmp/bin"
	# shellcheck disable=SC2016 # expanded by the fake go
	printf '#!/bin/sh\necho "$PWD $GOOS $CGO_ENABLED $*" >"%s/go-args"\n' "$tmp" >"$tmp/bin/go"
	chmod +x "$tmp/bin/go"
	PATH=$tmp/bin:$PATH run "$script" prepare-s7 "$tmp/out.vps"
	[ "$status" -eq 0 ]
	[ "$(cat "$tmp/go-args")" = "$repo/hostisan linux 0 test -c -tags vps -o $tmp/out.vps ./internal/runner" ]
}
