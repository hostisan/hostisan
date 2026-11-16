#!/usr/bin/env bats
# Tests for tools/scrub-fixtures.sh. Plain bats; needs jq.

setup() {
	scrub="$BATS_TEST_DIRNAME/../../tools/scrub-fixtures.sh"
	dir=$(mktemp -d)
}

teardown() {
	rm -rf "$dir"
}

@test "scrub removes ssl key" {
	cat >"$dir/v-list-sys-hestia-ssl.json" <<'JSON'
[
  {
    "args": ["json"],
    "stdout": "{\n\t\"HESTIA\": {\n        \"CRT\": \"-----BEGIN CERTIFICATE-----\\nMIIBcert\\n-----END CERTIFICATE-----\",\n        \"KEY\": \"-----BEGIN PRIVATE KEY-----\\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\\n-----END PRIVATE KEY-----\",\n        \"SUBJECT\": \"CN=rec.hostisan.test\"\n\t}\n}\n",
    "exit": 0
  },
  {
    "args": ["plain"],
    "stdout": "-----BEGIN CERTIFICATE-----\nMIIBcert\n-----END CERTIFICATE-----\n\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\nCN=rec.hostisan.test\n",
    "exit": 0
  }
]
JSON
	run bash "$scrub" "$dir"
	[ "$status" -eq 0 ]
	json=$(jq -r '.[0].stdout' "$dir/v-list-sys-hestia-ssl.json")
	[ "$(jq -r .HESTIA.KEY <<<"$json")" = REDACTED ]
	[ "$(jq -r .HESTIA.SUBJECT <<<"$json")" = CN=rec.hostisan.test ]
	[[ $(jq -r .HESTIA.CRT <<<"$json") == *MIIBcert* ]]
	plain=$(jq -r '.[1].stdout' "$dir/v-list-sys-hestia-ssl.json")
	[[ $plain == *$'REDACTED-KEY\nCN=rec.hostisan.test'* ]]
	! grep -q 'MIIE' "$dir/v-list-sys-hestia-ssl.json"
}

@test "scrub redacts secrets and keeps empty values" {
	cat >"$dir/v-list-user.json" <<'JSON'
[{"args":["admin","json"],"stdout":"{\"admin\": {\"RKEY\": \"Zq8x\", \"TWOFA\": \"\", \"NAME\": \"Admin\"}}\n","exit":0},
 {"args":["shell"],"stdout":"RKEY='Zq8x'\nTWOFA=''\n","exit":0}]
JSON
	run bash "$scrub" "$dir"
	[ "$status" -eq 0 ]
	[ "$(jq -r '.[0].stdout | fromjson | .admin.RKEY' "$dir/v-list-user.json")" = REDACTED ]
	[ "$(jq -r '.[0].stdout | fromjson | .admin.TWOFA' "$dir/v-list-user.json")" = "" ]
	[ "$(jq -r '.[0].stdout | fromjson | .admin.NAME' "$dir/v-list-user.json")" = Admin ]
	[ "$(jq -r '.[1].stdout' "$dir/v-list-user.json")" = "RKEY='REDACTED'
TWOFA=''" ]
}

@test "scrub leaves clean files byte-identical" {
	printf '[{"args":[],"stdout":"{\\"A\\": \\"b\\"}\\n","exit":0}]' >"$dir/v-list-x.json"
	before=$(cat "$dir/v-list-x.json")
	run bash "$scrub" "$dir"
	[ "$status" -eq 0 ]
	[ "$(cat "$dir/v-list-x.json")" = "$before" ]
}

@test "scrub fails on a file that is not a fixture list" {
	echo '{"a":1}' >"$dir/v-list-x.json"
	run bash "$scrub" "$dir"
	[ "$status" -ne 0 ]
	[[ $output == *"not a fixture list"* ]]
	[ "$(cat "$dir/v-list-x.json")" = '{"a":1}' ]
}

@test "scrub rewrites real ip and host" {
	cat >"$dir/v-list-sys-ips.json" <<'JSON'
[{"args":["185.161.209.107"],"stdout":"{\"185.161.209.107\": {\"NAT\": \"185.161.209.1070\", \"HOST\": \"VPS1.real.example\"}}\n","stderr":"mail.vps1.real.example down\n","exit":0}]
JSON
	run bash "$scrub" --ip 185.161.209.107=203.0.113.10 --host vps1.real.example=rec.hostisan.test "$dir"
	[ "$status" -eq 0 ]
	[ "$(jq -r '.[0].args[0]' "$dir/v-list-sys-ips.json")" = 203.0.113.10 ]
	[ "$(jq -r '.[0].stdout | fromjson | keys[0]' "$dir/v-list-sys-ips.json")" = 203.0.113.10 ]
	[ "$(jq -r '.[0].stdout | fromjson | .[].NAT' "$dir/v-list-sys-ips.json")" = 185.161.209.1070 ]
	[ "$(jq -r '.[0].stdout | fromjson | .[].HOST' "$dir/v-list-sys-ips.json")" = rec.hostisan.test ]
	[ "$(jq -r '.[0].stderr' "$dir/v-list-sys-ips.json")" = "mail.rec.hostisan.test down" ]
}

@test "scrub rejects a non-documentation ip placeholder" {
	run bash "$scrub" --ip 185.161.209.107=8.8.8.8 "$dir"
	[ "$status" -eq 2 ]
	[[ $output == *"not an RFC 5737 address"* ]]
}
