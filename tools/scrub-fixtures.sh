#!/usr/bin/env bash
# Replaces secrets in recorded Hostisan fixtures with placeholders, in place.
# Usage: scrub-fixtures.sh [--ip REAL=PLACEHOLDER]... [--host REAL=PLACEHOLDER]... <dir>
#        (every *.json under <dir>, recursively)
# Run it on every recording before it is committed; the Go test
# TestRecordedFixturesHoldNoSecrets fails on anything it missed.
#
# Inside each entry's stdout and stderr:
#   - non-empty PHPMYADMIN_KEY, SERVER_SMTP_PASSWD, RKEY, TWOFA, QRCODE,
#     SECRET_ACCESS_KEY, MD5 and PASSWORD values become REDACTED (and KEY, in
#     v-list-*-ssl output); empty values stay empty, since "" carries meaning;
#   - -----BEGIN ... PRIVATE KEY----- blocks become REDACTED-KEY.
# JSON stdout is parsed and walked; a changed document is written back compact.
# Other stdout gets the same rules as text (KEY='v' / KEY="v" lines, key blocks).
#
# --ip and --host (repeatable) rewrite the recording box's real address and name
# everywhere in args, stdout and stderr. An IP placeholder must be an RFC 5737
# documentation address (192.0.2.x, 198.51.100.x, 203.0.113.x). A host also matches
# as the tail of a longer name (mail.<host>), case-insensitively; look-alikes such as
# 1.2.3.45 for 1.2.3.4, or x<host>, are left alone.
set -euo pipefail

usage() {
	echo "usage: $0 [--ip REAL=PLACEHOLDER]... [--host REAL=PLACEHOLDER]... <dir>" >&2
	exit 2
}
die() {
	echo "$0: $*" >&2
	exit 2
}

ip_re='^([0-9]{1,3}\.){3}[0-9]{1,3}$'
doc_ip_re='^(192\.0\.2|198\.51\.100|203\.0\.113)\.[0-9]{1,3}$'
label='[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?'
host_re="^$label(\\.$label)+\$"
subs='[]'
add_sub() { # regex replacement
	subs=$(jq -c --arg re "$1" --arg to "$2" '. + [{re: $re, to: $to}]' <<< "$subs")
}
while [[ $# -gt 0 ]]; do
	case $1 in
		--ip | --host)
			[[ $# -ge 2 && $2 == *=* ]] || die "$1 needs REAL=PLACEHOLDER"
			real=${2%%=*} to=${2#*=}
			if [[ $1 == --ip ]]; then
				[[ $real =~ $ip_re ]] || die "--ip: $real is not an IPv4 address"
				[[ $to =~ $doc_ip_re ]] || die "--ip: placeholder $to is not an RFC 5737 address"
				add_sub "(?<![0-9.])${real//./\\.}(?![0-9])" "$to"
			else
				[[ $real =~ $host_re && $to =~ $host_re ]] || die "--host: $2 is not NAME=NAME"
				add_sub "(?<![A-Za-z0-9-])${real//./\\.}(?![A-Za-z0-9-])" "$to"
			fi
			shift 2
			;;
		-*) usage ;;
		*) break ;;
	esac
done
[[ $# -eq 1 && -d $1 ]] || usage
command -v jq > /dev/null || {
	echo "jq is required" >&2
	exit 1
}

program=$(
	cat << 'JQ'
def keys_re: "PHPMYADMIN_KEY|SERVER_SMTP_PASSWD|RKEY|TWOFA|QRCODE|SECRET_ACCESS_KEY|MD5|PASSWORD"
  + (if $ssl then "|KEY" else "" end);
def secret_key: test("^(" + keys_re + ")$");
def unkey: gsub("-----BEGIN [A-Z ]*PRIVATE KEY-----[\\s\\S]*?-----END [A-Z ]*PRIVATE KEY-----"; "REDACTED-KEY");
def scrub_doc: walk(
  if type == "object" then
    with_entries(if (.key | secret_key) and (.value | type) == "string" and .value != ""
      then .value = "REDACTED" else . end)
  elif type == "string" then unkey
  else . end);
def scrub_text: unkey
  | gsub("(?<p>\\A|\\n)(?<k>" + keys_re + ")=(?<q>['\"])(?!\\k<q>)[^'\"\\n]*\\k<q>";
      "\(.p)\(.k)=\(.q)REDACTED\(.q)");
def scrub_out:
  if . == null or . == "" then .
  else . as $s
    | ((try ($s | fromjson) catch null) as $doc
       | if ($doc | type) == "object" or ($doc | type) == "array" then
           ($doc | scrub_doc) as $new
           | if $new == $doc then $s
             else ($new | tojson) + (if ($s | endswith("\n")) then "\n" else "" end) end
         else $s | scrub_text end)
  end;
def rewrite: reduce $subs[] as $s (.; gsub($s.re; $s.to; "i"));
map(.args |= map(rewrite)
  | .stdout |= (if type == "string" then rewrite else . end | scrub_out)
  | if has("stderr") then .stderr |= (rewrite | scrub_out) else . end)
JQ
)

count=0
failed=0
while IFS= read -r -d '' file; do
	count=$((count + 1))
	ssl=false
	[[ $(basename "$file") == v-list-*-ssl.json ]] && ssl=true
	if ! jq --argjson ssl "$ssl" --argjson subs "$subs" "$program" "$file" > "$file.tmp"; then
		rm -f "$file.tmp"
		echo "error: $file is not a fixture list; left unchanged" >&2
		failed=$((failed + 1))
		continue
	fi
	# Rewrite only on a content change, so formatting-only diffs never appear.
	if [[ $(jq -c . "$file") == "$(jq -c . "$file.tmp")" ]]; then
		rm "$file.tmp"
	else
		mv "$file.tmp" "$file"
		echo "scrubbed: $file"
	fi
done < <(find "$1" -type f -name '*.json' -print0)
echo "checked $count fixture file(s)"
[[ $failed -eq 0 ]] || exit 1
