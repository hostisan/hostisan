#!/usr/bin/env bash
# Prints the recorder manifest lines that build the dev fixture world on a fresh Hestia box.
#
#   seed-plan.sh <devfixtures> [pkgdir] > seed.txt
#
# Reads hostisan/testdata/fixtures/dev (v-list-users, v-list-user-packages / -package,
# v-list-{web,dns,mail}-domains, v-list-databases, v-list-cron-jobs for admin and bob) and
# prints "v-add-*" lines for tools/record-fixtures.sh. admin is created by the installer and
# is not re-added. Every password argument is the file /tmp/seedpass: Hestia reads a
# password from a /tmp file (is_password_valid), so no secret lands in seed.txt or argv.
#
# Packages: v-add-user-package needs a package file, so the lines point at
# /tmp/seed-pkg/<name>.pkg. With [pkgdir], the script also writes those files there; copy
# them to /tmp/seed-pkg/ on the box before running the seed.
#
# Clash rule (Plan 6 A7): admin owns example.com (web, mail, DNS). bob never gets an
# example.com line; his search-world objects (mail account "info", cron job 2) live on
# bakery.example. The dev IPs are documentation addresses, so web domains take the box's
# own IP and DNS zones keep the fixture IP as zone data.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	echo "usage: $0 <devfixtures> [pkgdir]" >&2
	exit 2
fi
dev=$1
pkgdir=${2:-}
pass=/tmp/seedpass
users=(admin bob)
admin_domain=example.com

command -v jq >/dev/null || {
	echo "jq is required" >&2
	exit 1
}
[[ -f $dev/v-list-users.json ]] || {
	echo "not a dev fixture dir: $dev" >&2
	exit 1
}

# Prints the parsed JSON stdout of the entry in <file> whose args equal the given args.
# Some hand-written fixtures hold Hestia's invalid "\;" escapes; those are repaired first.
fixture() {
	local file=$dev/$1.json
	shift
	[[ -f $file ]] || return 0
	jq -c '
		[.[] | select(.args == $ARGS.positional)][0].stdout // "{}"
		| . as $s | try fromjson catch ($s | gsub("\\\\(?<c>[^\"\\\\/bfnrtu])"; "\\\\\(.c)") | fromjson)
	' "$file" --args "$@" </dev/null
}

# Turns a JSON object of objects into one line per entry, fields joined by \x1f (not
# whitespace, so empty fields survive "read"). Missing or null fields become "".
sep=$'\x1f'
rows() {
	jq -r --arg s "$sep" "to_entries[] | $1 | map(. // \"\" | tostring) | join(\$s)"
}

# Quotes one argument for the manifest (shell-style, as the recorder's xargs parser reads it).
q() {
	local s=$1
	if [[ $s =~ ^[A-Za-z0-9@%+=:,./_-]+$ ]]; then
		printf '%s' "$s"
	elif [[ $s != *\'* ]]; then
		printf "'%s'" "$s"
	elif [[ $s != *\"* && $s != *\\* ]]; then
		printf '"%s"' "$s"
	else
		# Backslashes and quotes together: close and reopen single quotes around each '.
		printf "'%s'" "${s//\'/\'\"\'\"\'}"
	fi
}

line() {
	local out=$1 a
	shift
	for a in "$@"; do
		out+=" $(q "$a")"
	done
	printf '%s\n' "$out"
}

yn() {
	case $1 in yes) echo yes ;; *) echo no ;; esac
}

all_users=$(fixture v-list-users json)

# ---- packages ---------------------------------------------------------------------------
echo "# Seed for the Hostisan dev fixture world (tools/vps/seed-plan.sh)."
echo "# Before running: write /tmp/seedpass (one line) and copy the .pkg files to /tmp/seed-pkg/."
echo "# packages"
mapfile -t pkgs < <(
	{
		fixture v-list-user-packages json | jq -r 'keys_unsorted[]'
		jq -r '.[].PACKAGE' <<<"$all_users"
	} | awk '!seen[$0]++'
)
for p in "${pkgs[@]}"; do
	[[ $p == default || $p == system ]] && continue
	[[ $p =~ ^[A-Za-z0-9_.-]+$ ]] || {
		echo "bad package name: $p" >&2
		exit 1
	}
	def=$(fixture v-list-user-package "$p" json | jq -c --arg p "$p" '.[$p] // empty')
	if [[ -z $def ]]; then
		def=$(fixture v-list-user-packages json | jq -c --arg p "$p" '.[$p] // empty')
	fi
	[[ -n $def ]] || {
		echo "package $p has no definition in the fixtures" >&2
		exit 1
	}
	if [[ -n $pkgdir ]]; then
		mkdir -p "$pkgdir"
		jq -r --arg q "'" 'del(.TIME, .DATE) | to_entries[] | "\(.key)=\($q)\(.value | tostring | gsub($q; ""))\($q)"' \
			<<<"$def" >"$pkgdir/$p.pkg"
	fi
	line v-add-user-package "/tmp/seed-pkg/$p.pkg" "$p"
done

# ---- users ------------------------------------------------------------------------------
echo "# users"
while IFS=$sep read -r u pkg role susp email name shell; do
	if [[ $u != admin ]]; then
		line v-add-user "$u" "$pass" "$email" "$pkg" "$name"
		[[ $role == admin ]] && line v-change-user-role "$u" admin
	fi
	[[ -n $shell && $shell != nologin ]] && line v-change-user-shell "$u" "$shell"
	[[ $susp == yes ]] && line v-suspend-user "$u" no
done < <(rows '[.key, .value.PACKAGE, .value.ROLE, .value.SUSPENDED,
	.value.CONTACT, .value.NAME, .value.SHELL]' <<<"$all_users")

# Skips a domain bob would share with admin (the A7 clash rule).
clash() {
	[[ $1 == bob && $2 == "$admin_domain" ]]
}

declare -A owner=()
add_domain() {
	local u=$1 d=$2
	if [[ -n ${owner[$d]:-} && ${owner[$d]} != "$u" ]]; then
		echo "domain $d is owned by both ${owner[$d]} and $u" >&2
		exit 1
	fi
	owner[$d]=$u
}

for u in "${users[@]}"; do
	web=$(fixture v-list-web-domains "$u" json)
	dns=$(fixture v-list-dns-domains "$u" json)
	mail=$(fixture v-list-mail-domains "$u" json)
	dbs=$(fixture v-list-databases "$u" json)
	cron=$(fixture v-list-cron-jobs "$u" json)

	echo "# $u: web"
	while IFS=$sep read -r d alias susp; do
		clash "$u" "$d" && continue
		add_domain "$u" "$d"
		line v-add-web-domain "$u" "$d" '' no "${alias:-none}"
		[[ $susp == yes ]] && line v-suspend-web-domain "$u" "$d" no
	done < <(<<<"$web" rows '[.key, .value.ALIAS, .value.SUSPENDED]')

	echo "# $u: dns"
	while IFS=$sep read -r d ip tpl susp; do
		if clash "$u" "$d"; then
			echo "# skipped: $u $d (owned by admin; bob's zone is bakery.example)"
			continue
		fi
		add_domain "$u" "$d"
		line v-add-dns-domain "$u" "$d" "$ip" '' '' '' '' '' '' '' '' no
		[[ -n $tpl && $tpl != default ]] && line v-change-dns-domain-tpl "$u" "$d" "$tpl" no
		[[ $susp == yes ]] && line v-suspend-dns-domain "$u" "$d" no
	done < <(<<<"$dns" rows '[.key, .value.IP, .value.TPL, .value.SUSPENDED]')

	echo "# $u: mail"
	while IFS=$sep read -r d spam av dkim reject susp; do
		clash "$u" "$d" && continue
		add_domain "$u" "$d"
		line v-add-mail-domain "$u" "$d" "$(yn "$spam")" "$(yn "$av")" "$(yn "$dkim")" 1024 no "$(yn "$reject")"
		[[ $susp == yes ]] && line v-suspend-mail-domain "$u" "$d"
	done < <(<<<"$mail" rows '[.key, .value.ANTISPAM, .value.ANTIVIRUS,
		.value.DKIM, .value.REJECT, .value.SUSPENDED]')

	echo "# $u: databases"
	while IFS=$sep read -r db dbuser type charset susp; do
		# Hestia prefixes "<user>_" itself.
		line v-add-database "$u" "${db#"${u}"_}" "${dbuser#"${u}"_}" "$pass" "$type" localhost "$charset"
		[[ $susp == yes ]] && line v-suspend-database "$u" "$db"
	done < <(<<<"$dbs" rows '[.value.DATABASE, .value.DBUSER, .value.TYPE,
		.value.CHARSET, .value.SUSPENDED]')

	echo "# $u: cron"
	while IFS= read -r job; do
		mapfile -t f < <(jq -r '.JOB, .MIN, .HOUR, .DAY, .MONTH, .WDAY, .CMD, .SUSPENDED' <<<"$job")
		line v-add-cron-job "$u" "${f[1]}" "${f[2]}" "${f[3]}" "${f[4]}" "${f[5]}" "${f[6]}" "${f[0]}" no
		[[ ${f[7]} == yes ]] && line v-suspend-cron-job "$u" "${f[0]}" no
	done < <(<<<"$cron" jq -c '.[]')
done

# ---- search world (fixture-manifest-tools.txt header, after the A7 clash rule) -----------
echo "# search world: bob's objects on bakery.example"
line v-add-mail-account bob bakery.example info "$pass"
line v-add-cron-job bob 15 4 '*' '*' '*' 'wget -q -O /dev/null https://bakery.example/' 2 no
