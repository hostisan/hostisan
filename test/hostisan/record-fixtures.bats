#!/usr/bin/env bats
# Tests for tools/record-fixtures.sh, run against the fake commands in fakebin/.

load '../test_helper/bats-support/load'
load '../test_helper/bats-assert/load'

setup() {
	script="$BATS_TEST_DIRNAME/../../tools/record-fixtures.sh"
	out="$BATS_TEST_TMPDIR/out"
	manifest="$BATS_TEST_TMPDIR/manifest.txt"
	export HESTIA_BIN="$BATS_TEST_DIRNAME/fakebin"
}

record() {
	printf '%s\n' "$@" >"$manifest"
	run "$script" "$manifest" "$out"
}

@test "empty quoted arg is one empty arg" {
	record "v-echo-args a '' b"
	assert_success
	run jq -c '.[0].args' "$out/v-echo-args.json"
	assert_output '["a","","b"]'
	run jq -e '.[0].stdout == "3\na\n\nb\n"' "$out/v-echo-args.json"
	assert_success
}

@test "dash args are not jq options" {
	record "v-echo-args ls -A -- --arg x"
	assert_success
	run jq -c '.[0].args' "$out/v-echo-args.json"
	assert_output '["ls","-A","--","--arg","x"]'
}

@test "quoted space stays one arg" {
	record "v-echo-args 'a b' \"c d\""
	assert_success
	run jq -c '.[0].args' "$out/v-echo-args.json"
	assert_output '["a b","c d"]'
	run jq -e '.[0].stdout == "2\na b\nc d\n"' "$out/v-echo-args.json"
	assert_success
}

@test "stdout kept exactly" {
	bin="$BATS_TEST_TMPDIR/bin"
	mkdir -p "$bin"
	printf '#!/bin/sh\nprintf "x\\n\\n"\n' >"$bin/v-blank-tail"
	printf '#!/bin/sh\nexit 0\n' >"$bin/v-silent"
	chmod +x "$bin"/*
	HESTIA_BIN=$bin record v-blank-tail v-silent
	assert_success
	run jq -e '.[0].stdout == "x\n\n"' "$out/v-blank-tail.json"
	assert_success
	run jq -e '.[0].stdout == ""' "$out/v-silent.json"
	assert_success
}

@test "stderr and exit recorded" {
	record v-fail-stderr "v-echo-args a"
	assert_success
	run jq -c '.[0]' "$out/v-fail-stderr.json"
	assert_output '{"args":[],"stdout":"out\n","stderr":"err\n","exit":3}'
	run jq -e '.[0] | has("stderr") | not' "$out/v-echo-args.json"
	assert_success
}

@test "bad quote aborts" {
	record "v-echo-args 'a"
	assert_failure
	assert_output --partial 'line 1: unmatched quote'
	[[ ! -e $out/v-echo-args.json ]]
}

@test "non-v command skipped" {
	record "ls /"
	assert_success
	assert_output --partial 'skip:'
	run find "$out" -type f
	assert_output ''
}

@test "entries append in manifest order" {
	record "v-echo-args one" "# comment" "" "v-echo-args two"
	assert_success
	run jq -c '[.[].args[0]]' "$out/v-echo-args.json"
	assert_output '["one","two"]'
}
