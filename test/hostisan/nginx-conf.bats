#!/usr/bin/env bats
# Tests for src/deb/nginx/nginx.conf, hestia-nginx's config (Plan 6, B5).
# Static checks on the vhost, plus "nginx -t" in Docker (skipped without Docker).
# Run: bats test/hostisan/nginx-conf.bats  (plain bats; no helper libraries needed)

# The "live:" tests run against one container: hestia-nginx with this config and
# a stub Hostisan (a second nginx) on /run/hostisan/hostisan.sock. The stub
# echoes the proxy headers under /echo, /api/ and /reset/mail/, answers 404
# elsewhere, and hands off X-Accel-Redirect from /accel-bad and /accel-good.
setup_file() {
	have_docker || return 0
	local dir name conf
	dir=$(mktemp -d "${BATS_TMPDIR:-/tmp}/nginx-live.XXXXXX")
	name=nginx-conf-bats-$$
	conf=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/src/deb/nginx/nginx.conf
	echo "$dir" >"$BATS_FILE_TMPDIR/dir"
	stub_certs "$dir/ssl"
	cat >"$dir/stub.conf" <<'STUB'
pid /tmp/stub.pid;
error_log /tmp/stub-error.log;
events {}
http {
	access_log off;
	server {
		listen unix:/run/hostisan/hostisan.sock;
		default_type text/plain;
		set $echo "host=$http_host|xri=$http_x_real_ip|xff=$http_x_forwarded_for|sa=$http_x_hostisan_server_addr|cri=$http_x_hostisan_client_real_ip|cff=$http_x_hostisan_client_forwarded_for|up=$http_upgrade|uri=$request_uri\n";
		location /echo { return 200 $echo; }
		location /api/ { return 200 $echo; }
		location /reset/mail/ { return 200 $echo; }
		location = /accel-bad { add_header X-Accel-Redirect /__accel/etc/passwd; return 200 "stub\n"; }
		location = /accel-good { add_header X-Accel-Redirect /__accel/backup/x.tar; return 200 "stub\n"; }
		location / { return 404 "stub-404\n"; }
	}
}
STUB
	docker run -d --rm --name "$name" \
		-v "$conf:/etc/nginx/nginx.conf:ro" \
		-v "$dir/stub.conf:/etc/nginx/stub.conf:ro" \
		-v "$dir/ssl:/usr/local/hestia/ssl:ro" \
		-v "$dir/ssl/dhparam.pem:/etc/ssl/dhparam.pem:ro" \
		--entrypoint sh nginx:1.27.3 -c '
			echo "hestiaweb:x:990:990::/nonexistent:/usr/sbin/nologin" >>/etc/passwd
			echo "hestiaweb:x:990:" >>/etc/group
			mkdir -p /run/hostisan /backup /var/log/hestia
			echo BACKUP-OK >/backup/x.tar
			mkdir -p /usr/local/hestia/web/fm/dist
			for x in app.js.map .env x.php.bak; do echo SECRET >"/usr/local/hestia/web/fm/dist/$x"; done
			nginx -c /etc/nginx/stub.conf && nginx && exec sleep 600' >/dev/null || return 1
	echo "$name" >"$BATS_FILE_TMPDIR/container"
	for _ in $(seq 50); do
		docker exec "$name" curl -sk -o /dev/null https://127.0.0.1:8083/ && return 0
		sleep 0.2
	done
}

teardown_file() {
	if [ -f "$BATS_FILE_TMPDIR/container" ]; then
		docker rm -f "$(cat "$BATS_FILE_TMPDIR/container")" >/dev/null 2>&1
	fi
	if [ -f "$BATS_FILE_TMPDIR/dir" ]; then
		rm -rf "$(cat "$BATS_FILE_TMPDIR/dir")"
	fi
	return 0
}

# need_live: skip unless setup_file started the container (call before "run").
need_live() {
	[ -f "$BATS_FILE_TMPDIR/container" ] || skip "docker not available"
}

# req ARGS...: curl inside the live container; prints the body, then
# "code=NNN loc=REDIRECT".
req() {
	docker exec "$(cat "$BATS_FILE_TMPDIR/container")" \
		curl -sk --path-as-is -w '\ncode=%{http_code} loc=%{redirect_url}\n' "$@"
}

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	conf=$repo/src/deb/nginx/nginx.conf
	tmp=$(mktemp -d "${BATS_TMPDIR:-/tmp}/nginx-conf.XXXXXX")
}

teardown() {
	rm -rf "$tmp"
}

# block HEADER [FILE]: print the block whose opening line contains HEADER
# (fixed string), up to its closing brace.
block() {
	H="$1" awk '
		!on && index($0, ENVIRON["H"]) && /\{[[:space:]]*$/ { on = 1; depth = 0 }
		on {
			print
			depth += gsub(/\{/, "{") - gsub(/\}/, "}")
			if (depth == 0) exit
		}' "${2:-$conf}"
}

# hostisan_headers: the opening line of every location that proxies to Hostisan.
hostisan_headers() {
	awk '
		/^[[:space:]]*location[[:space:]].*\{[[:space:]]*$/ { cur = $0 }
		/proxy_pass[[:space:]]+http:\/\/hostisan;/ { print cur }' "$conf"
}

have_docker() {
	command -v docker >/dev/null && docker info >/dev/null 2>&1
}

# stub_certs DIR: a self-signed certificate and a dhparam file in DIR.
stub_certs() {
	mkdir -p "$1"
	[ -f "$1/certificate.crt" ] && return 0
	openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=t \
		-keyout "$1/certificate.key" -out "$1/certificate.crt" 2>/dev/null
	openssl dhparam -dsaparam -out "$1/dhparam.pem" 2048 2>/dev/null
}

# nginx_t FILE: "nginx -t" on FILE in the pinned nginx image, with a stub
# certificate, dhparam, log dir and the hestiaweb user.
nginx_t() {
	have_docker || skip "docker not available"
	stub_certs "$tmp/ssl"
	mkdir -p "$tmp/log"
	docker run --rm \
		-v "$1:/etc/nginx/nginx.conf:ro" \
		-v "$tmp/ssl:/usr/local/hestia/ssl:ro" \
		-v "$tmp/ssl/dhparam.pem:/etc/ssl/dhparam.pem:ro" \
		-v "$tmp/log:/var/log/hestia" \
		--entrypoint sh nginx:1.27.3 \
		-c 'echo "hestiaweb:x:990:990::/nonexistent:/usr/sbin/nologin" >>/etc/passwd && echo "hestiaweb:x:990:" >>/etc/group && nginx -t'
}

# The exact sed from bin/v-change-sys-port.
port_sed() {
	local line
	line=$(grep -n 'sed -i "s/\\(listen' "$repo/bin/v-change-sys-port")
	[ -n "$line" ] || return 1
	PORT=$2 NGINX_CONFIG=$1 bash -c "$(sed 's/^[0-9]*:[[:space:]]*//' <<<"$line")"
}

# Pull one sed expression out of func/syshealth.sh's listen-port function.
syshealth_expr() {
	awk '/^function syshealth_adapt_hestia_nginx_listen_ports/,/^}/' "$repo/func/syshealth.sh" |
		grep "sed -i '" | sed -n "${1}p" | sed "s/.*sed -i '\\(.*\\)' .*/\\1/"
}

@test "exactly two listen lines" {
	run grep -c listen "$conf"
	[ "$output" = 2 ]
	grep -qx $'\t\tlisten              8083 ssl;' "$conf"
	grep -qx $'\t\tlisten              \[::\]:8083 ssl;' "$conf"
}

@test "no _shell, no localhost proxy_pass" {
	! grep -n '_shell' "$conf" || false
	! grep -n 'proxy_pass http://localhost:' "$conf" || false
	! grep -n '8085' "$conf" || false
}

@test "upstream is the unix socket" {
	run block 'upstream hostisan'
	[ -n "$output" ]
	grep -qx '[[:space:]]*server unix:/run/hostisan/hostisan.sock;' <<<"$output"
	[ "$(grep -c 'server ' <<<"$output")" = 1 ]
	# Every proxy_pass goes to that upstream: no TCP backend at all.
	[ "$(grep -c 'proxy_pass' "$conf")" -ge 1 ]
	! grep -n 'proxy_pass[[:space:]]' "$conf" | grep -v 'proxy_pass[[:space:]]*http://hostisan;' || false
}

@test "Host with port" {
	run hostisan_headers
	[ "${#lines[@]}" -ge 4 ]
	for h in "${lines[@]}"; do
		b=$(block "$h")
		grep -q 'proxy_set_header[[:space:]]*Host[[:space:]]*\$http_host;' <<<"$b" || { echo "no Host in: $h"; return 1; }
		grep -q 'proxy_set_header[[:space:]]*X-Real-IP[[:space:]]*\$remote_addr;' <<<"$b" || { echo "no X-Real-IP in: $h"; return 1; }
		grep -q 'proxy_set_header[[:space:]]*X-Forwarded-For[[:space:]]*\$remote_addr;' <<<"$b" || { echo "no XFF in: $h"; return 1; }
		grep -q 'proxy_set_header[[:space:]]*Early-Data[[:space:]]*\$rfc_early_data;' <<<"$b" || { echo "no Early-Data in: $h"; return 1; }
	done
	# The SPA, /api/, /login/, /reset/ and /images/logo.svg all fall to "location /".
	block 'location / {' | grep -q 'proxy_pass[[:space:]]*http://hostisan;'
	block 'location = /api/index.php' | grep -q 'proxy_pass[[:space:]]*http://hostisan;'
	! grep -n 'location.*images' "$conf" || false
}

@test "terminal websocket" {
	b=$(block 'location = /api/v1/terminal/socket')
	[ -n "$b" ]
	grep -q 'proxy_pass[[:space:]]*http://hostisan;' <<<"$b"
	grep -q 'proxy_http_version[[:space:]]*1.1;' <<<"$b"
	grep -q 'proxy_set_header[[:space:]]*Upgrade[[:space:]]*\$http_upgrade;' <<<"$b"
	grep -q 'proxy_set_header[[:space:]]*Connection[[:space:]]*"upgrade";' <<<"$b"
	grep -q 'proxy_read_timeout[[:space:]]*3600s;' <<<"$b"
	grep -q 'proxy_buffering[[:space:]]*off;' <<<"$b"
}

@test "reset/mail headers" {
	b=$(block 'location ^~ /reset/mail/')
	[ -n "$b" ]
	grep -q 'proxy_pass[[:space:]]*http://hostisan;' <<<"$b"
	grep -q 'proxy_set_header[[:space:]]*X-Hostisan-Server-Addr[[:space:]]*\$server_addr;' <<<"$b"
	grep -q 'proxy_set_header[[:space:]]*X-Hostisan-Client-Real-IP[[:space:]]*\$http_x_real_ip;' <<<"$b"
	grep -q 'proxy_set_header[[:space:]]*X-Hostisan-Client-Forwarded-For[[:space:]]*\$http_x_forwarded_for;' <<<"$b"
	# Everywhere else a client-sent copy is dropped (an empty value is not passed).
	b=$(block 'location / {')
	for h in X-Hostisan-Server-Addr X-Hostisan-Client-Real-IP X-Hostisan-Client-Forwarded-For; do
		grep -q "proxy_set_header[[:space:]]*$h[[:space:]]*\"\";" <<<"$b" || { echo "$h not cleared"; return 1; }
	done
}

@test "php only under /fm/" {
	fm=$(block 'location /fm/')
	[ -n "$fm" ]
	[ "$(grep -c fastcgi_pass "$conf")" = "$(grep -c fastcgi_pass <<<"$fm")" ]
	[ "$(grep -c fastcgi_pass <<<"$fm")" = 1 ]
	grep -q 'fastcgi_pass[[:space:]]*unix:/run/hestia-php.sock;' <<<"$fm"
	b=$(block 'location ~ \.php$')
	grep -q 'return[[:space:]]*404;' <<<"$b"
	! grep -q 'fastcgi\|proxy_pass' <<<"$b" || false
	# No other regex location could catch an old-panel URL first.
	run grep -c '^[[:space:]]*location[[:space:]]*~' "$conf"
	[ "$output" = 3 ] # \.php$, and the PHP and deny rules nested in /fm/
	[ "$(grep -c '^[[:space:]]*location[[:space:]]*~' <<<"$fm")" = 2 ]
	# No disk-served panel: no index.php and no web root outside /fm/ and /error/.
	[ "$(grep -c 'index[[:space:]]*index.php' "$conf")" = "$(grep -c 'index[[:space:]]*index.php' <<<"$fm")" ]
	[ "$(grep -c 'root[[:space:]]' "$conf")" = 1 ]
	block 'location /error/' | grep -q 'root[[:space:]]*/usr/local/hestia/web;'
}

@test "internal locations" {
	b=$(block 'location /__accel/backup/')
	grep -q 'internal;' <<<"$b"
	grep -q 'alias[[:space:]]*/backup/;' <<<"$b"
	block 'location /error/' | grep -q 'internal;'
	# No other internal alias: only /fm/'s dist and the backup dir are aliased,
	# and only /__accel/backup/ and /error/ are internal.
	run grep -c 'alias[[:space:]]' "$conf"
	[ "$output" = 2 ]
	block 'location /fm/' | grep -q 'alias[[:space:]]*/usr/local/hestia/web/fm/dist/;'
	! grep -n 'alias[[:space:]]*/;' "$conf" || false
	run grep -c 'internal;' "$conf"
	[ "$output" = 2 ]
	! grep -n 'location /backup/' "$conf" || false
	if grep -rqn '/rrd/' "$repo/hostisan/internal"; then
		block 'location /rrd/' | grep -q 'internal;'
	else
		! grep -n '/rrd/' "$conf" || false
	fi
}

@test "port change keeps routing" {
	cp "$conf" "$tmp/nginx.conf"
	port_sed "$tmp/nginx.conf" 8443
	run diff "$conf" "$tmp/nginx.conf"
	[ "$(grep -c '^<' <<<"$output")" = 2 ]
	[ "$(grep -c '^>' <<<"$output")" = 2 ]
	grep -qx $'> \t\tlisten              8443 ssl;' <<<"$output"
	grep -qx $'> \t\tlisten              \[::\]:8443 ssl;' <<<"$output"
	# v-change-sys-port reads the current port with this sed.
	orig=$(sed -ne "/listen/{s/.*listen[^0-9]*\([0-9][0-9]*\)[[:space:]]*ssl\;/\1/p;q}" "$conf")
	[ "$orig" = 8083 ]
	nginx_t "$tmp/nginx.conf"
}

@test "syshealth listen toggle" {
	off=$(syshealth_expr 1)
	on=$(syshealth_expr 2)
	[ -n "$off" ] && [ -n "$on" ]
	cp "$conf" "$tmp/nginx.conf"
	sed -i "$off" "$tmp/nginx.conf"
	run diff "$conf" "$tmp/nginx.conf"
	[ "$(grep -c '^>' <<<"$output")" = 1 ]
	grep -qx $'> #\t\tlisten              8083 ssl;' <<<"$output"
	sed -i "$on" "$tmp/nginx.conf"
	cmp "$conf" "$tmp/nginx.conf"
	# Same for the IPv6 pair.
	sed -i "$(syshealth_expr 3)" "$tmp/nginx.conf"
	run diff "$conf" "$tmp/nginx.conf"
	[ "$(grep -c '^>' <<<"$output")" = 1 ]
	grep -qx $'> #\t\tlisten              \[::\]:8083 ssl;' <<<"$output"
	sed -i "$(syshealth_expr 4)" "$tmp/nginx.conf"
	cmp "$conf" "$tmp/nginx.conf"
}

@test "nginx -t accepts the config" {
	have_docker || skip "docker not available"
	run nginx_t "$conf"
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "body size limits" {
	# Hostisan's own caps: 1 MiB JSON bodies, 16 MiB on the legacy /api/index.php.
	for h in 'location / {' 'location = /api/v1/terminal/socket' 'location ^~ /reset/mail/'; do
		block "$h" | grep -q 'client_max_body_size[[:space:]]*2m;' || { echo "no 2m in: $h"; return 1; }
	done
	block 'location = /api/index.php' | grep -q 'client_max_body_size[[:space:]]*16m;'
	block 'location /fm/' | grep -q 'client_max_body_size[[:space:]]*1024m;'
}

@test "fm denies sensitive files" {
	fm=$(block 'location /fm/')
	b=$(block 'location ~* \.(map|log|sh|sql|bak|env)$' <(echo "$fm"))
	grep -q 'return[[:space:]]*404;' <<<"$b"
	# Listed before the PHP location (nested regexes match in order).
	deny=$(grep -n 'map|log|sh|sql|bak|env' <<<"$fm" | cut -d: -f1)
	php=$(grep -n 'fastcgi\|\.php)\$' <<<"$fm" | head -1 | cut -d: -f1)
	[ -n "$deny" ] && [ -n "$php" ] && [ "$deny" -lt "$php" ]
}

@test "live: php outside /fm/ is 404" {
	need_live
	for u in /x.php /index.php /list/user/index.php /fm/../web/x.php /fm/%2e%2e/x.php /fm/%2E%2E/list/user/index.php; do
		run req "https://127.0.0.1:8083$u"
		[[ "$output" == *code=404* ]] || { echo "$u: $output"; return 1; }
	done
}

@test "live: fm sensitive files are 404" {
	need_live
	for u in /fm/app.js.map /fm/.env /fm/x.php.bak; do
		run req "https://127.0.0.1:8083$u"
		[[ "$output" == *code=404* && "$output" != *SECRET* ]] || { echo "$u: $output"; return 1; }
	done
}

@test "live: __accel is internal and backup-only" {
	need_live
	run req https://127.0.0.1:8083/__accel/backup/x.tar
	[[ "$output" == *code=404* && "$output" != *BACKUP-OK* ]] || { echo "$output"; return 1; }
	run req https://127.0.0.1:8083/__accel/etc/passwd
	[[ "$output" == *code=404* && "$output" != *root:* ]] || { echo "$output"; return 1; }
	# A handoff from Hostisan: /backup/ is served, anything else is not.
	run req https://127.0.0.1:8083/accel-good
	[[ "$output" == *BACKUP-OK*code=200* ]] || { echo "$output"; return 1; }
	run req https://127.0.0.1:8083/accel-bad
	[[ "$output" == *code=404* && "$output" != *root:* ]] || { echo "$output"; return 1; }
}

@test "live: proxy headers are overwritten" {
	need_live
	run req -H 'Host: panel.test:8083' -H 'X-Real-IP: 9.9.9.9' -H 'X-Forwarded-For: 8.8.8.8' \
		-H 'X-Hostisan-Server-Addr: 6.6.6.6' -H 'X-Hostisan-Client-Real-IP: 6.6.6.7' \
		-H 'X-Hostisan-Client-Forwarded-For: 6.6.6.8' https://127.0.0.1:8083/echo
	[[ "$output" == *"host=panel.test:8083|xri=127.0.0.1|xff=127.0.0.1|sa=|cri=|cff=|"*code=200* ]] || { echo "$output"; return 1; }
	for u in /api/index.php /api/v1/terminal/socket; do
		run req -H 'X-Real-IP: 9.9.9.9' -H 'X-Forwarded-For: 8.8.8.8' -H 'X-Hostisan-Server-Addr: 6.6.6.6' \
			-H 'X-Hostisan-Client-Real-IP: 6.6.6.7' "https://127.0.0.1:8083$u"
		[[ "$output" == *"host=127.0.0.1:8083|xri=127.0.0.1|xff=127.0.0.1|sa=|cri=|cff=|"* ]] || { echo "$u: $output"; return 1; }
	done
}

@test "live: reset/mail passes the caller's headers" {
	need_live
	run req -H 'Host: panel.test:8083' -H 'X-Real-IP: 9.9.9.9' -H 'X-Forwarded-For: 8.8.8.8' \
		-H 'X-Hostisan-Server-Addr: 6.6.6.6' https://127.0.0.1:8083/reset/mail/
	[[ "$output" == *"host=panel.test:8083|xri=127.0.0.1|xff=127.0.0.1|sa=127.0.0.1|cri=9.9.9.9|cff=8.8.8.8|"* ]] || { echo "$output"; return 1; }
	# Without client proxy headers none reach Hostisan; an empty one is dropped too.
	run req -H 'X-Real-IP;' https://127.0.0.1:8083/reset/mail/index.php
	[[ "$output" == *"sa=127.0.0.1|cri=|cff=|"* ]] || { echo "$output"; return 1; }
}

@test "live: terminal upgrade is forwarded" {
	need_live
	run req --http1.1 -H 'Upgrade: websocket' -H 'Connection: Upgrade' https://127.0.0.1:8083/api/v1/terminal/socket
	[[ "$output" == *"up=websocket|"* ]] || { echo "$output"; return 1; }
}

@test "live: plain http on the TLS port redirects" {
	need_live
	run req http://127.0.0.1:8083/login/
	[[ "$output" == *"loc=https://127.0.0.1:8083/login/"* ]] || { echo "$output"; return 1; }
	[[ "$output" =~ code=30[1278] ]]
}
