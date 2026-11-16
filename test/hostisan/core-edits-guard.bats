#!/usr/bin/env bats
# Tests for tools/check-core-edits.sh. Plain bats; each test builds a throwaway
# git repo with a fake core and its own hostisan/CORE-EDITS.md.

setup() {
	guard="$BATS_TEST_DIRNAME/../../tools/check-core-edits.sh"
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/core-edits.XXXXXX")
	cd "$work" || return 1
	git init -q .
	mkdir -p bin func install/deb src/deb/hestia web/images hostisan tools
	echo 'echo users' >bin/v-list-users
	echo 'echo port' >bin/v-change-sys-port
	echo 'main() { :; }' >func/main.sh
	echo 'Package: hestia' >src/deb/hestia/control
	echo 'svg' >web/images/logo.svg
	table ""
	commit "upstream"
	base=$(git rev-parse HEAD)
}

teardown() {
	cd /
	rm -rf "$work"
}

commit() {
	git add -A
	git -c user.name=t -c user.email=t@example.test -c commit.gpgsign=false \
		commit -q -m "$1"
}

# table ROWS: write CORE-EDITS.md with ROWS (one "path" per line) in the
# allowed table and a planned row the guard must ignore.
table() {
	{
		echo '# Allowed core edits'
		echo
		echo '## Allowed edits'
		echo
		echo '| path | kind | reason | decision ref | test |'
		echo '|---|---|---|---|---|'
		local p
		while IFS= read -r p; do
			[ -n "$p" ] && echo "| \`$p\` | security | r | d | t |"
		done <<<"$1"
		echo
		echo '## Planned edits'
		echo
		echo '| path | kind | reason | decision ref | test |'
		echo '|---|---|---|---|---|'
		echo "| \`func/main.sh\` | function | r | d | t |"
	} >hostisan/CORE-EDITS.md
}

@test "no core edits passes" {
	echo 'package x' >hostisan/x.go
	commit "hostisan only"
	run bash "$guard" "$base"
	[ "$status" -eq 0 ]
}

@test "unlisted edit fails" {
	echo 'echo changed' >>bin/v-list-users
	commit "edit core"
	run bash "$guard" "$base"
	[ "$status" -eq 1 ]
	[[ $output == *"unlisted core edit: bin/v-list-users"* ]]
}

@test "listed edit passes" {
	echo 'echo changed' >>bin/v-list-users
	table "bin/v-list-users"
	commit "edit core, listed"
	run bash "$guard" "$base"
	[ "$status" -eq 0 ]
}

@test "stale entry fails" {
	table "bin/v-change-sys-port"
	commit "list unchanged file"
	run bash "$guard" "$base"
	[ "$status" -eq 1 ]
	[[ $output == *"stale entry: bin/v-change-sys-port"* ]]
}

@test "planned rows do not allow an edit" {
	echo 'echo changed' >>func/main.sh
	commit "edit planned file"
	run bash "$guard" "$base"
	[ "$status" -eq 1 ]
	[[ $output == *"unlisted core edit: func/main.sh"* ]]
}

@test "hostisan-owned dirs are ignored" {
	echo 'package x' >hostisan/x.go
	echo 'echo t' >tools/t.sh
	mkdir -p ui/src test/hostisan
	echo 'x' >ui/src/a.ts
	echo 'x' >test/hostisan/a.bats
	commit "own code"
	run bash "$guard" "$base"
	[ "$status" -eq 0 ]
	[ -z "$output" ]
}

@test "every core dir is guarded" {
	echo x >>func/main.sh
	echo x >install/deb/new.conf
	echo x >>src/deb/hestia/control
	echo x >>web/images/logo.svg
	commit "edit every core dir"
	run bash "$guard" "$base"
	[ "$status" -eq 1 ]
	[[ $output == *"unlisted core edit: func/main.sh"* ]]
	[[ $output == *"unlisted core edit: install/deb/new.conf"* ]]
	[[ $output == *"unlisted core edit: src/deb/hestia/control"* ]]
	[[ $output == *"unlisted core edit: web/images/logo.svg"* ]]
}

@test "deleted and renamed core files count as edits" {
	git rm -q bin/v-list-users
	git mv bin/v-change-sys-port bin/v-change-sys-port2
	commit "delete and rename"
	run bash "$guard" "$base"
	[ "$status" -eq 1 ]
	[[ $output == *"unlisted core edit: bin/v-list-users"* ]]
	[[ $output == *"unlisted core edit: bin/v-change-sys-port"$'\n'* ]]
	[[ $output == *"unlisted core edit: bin/v-change-sys-port2"* ]]
}

@test "unknown base fails clearly" {
	run bash "$guard" 0000000000000000000000000000000000000000
	[ "$status" -eq 2 ]
	[[ $output == *"base commit not found"* ]]
}

@test "missing table fails clearly" {
	git rm -q hostisan/CORE-EDITS.md
	commit "drop table"
	run bash "$guard" "$base"
	[ "$status" -eq 2 ]
	[[ $output == *"CORE-EDITS.md"* ]]
}
