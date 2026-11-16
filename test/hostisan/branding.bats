#!/usr/bin/env bats
# Tests for tools/check-branding.sh (Plan 6 B3d): no user-visible "Hestia" in
# the core text the panel's brand map never sees.

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	check=$repo/tools/check-branding.sh
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/branding.XXXXXX")
	mkdir -p "$work/func" "$work/bin"
}

teardown() {
	rm -rf "$work"
}

@test "branding clean" {
	run "$check" "$repo"
	echo "$output"
	[ "$status" -eq 0 ]
	echo 'echo "Thank you for using Hestia Control Panel"' >"$work/func/upgrade.sh"
	run "$check" "$work"
	[ "$status" -eq 1 ]
	[[ "$output" == *"func/upgrade.sh:1:"*"Hestia Control Panel"* ]]
}

@test "branding check finds each form" {
	printf '%s\n' 'echo "Hestia"' 'echo "HestiaCP"' 'echo "see https://hestiacp.com/docs"' >"$work/bin/v-x"
	run "$check" "$work"
	[ "$status" -eq 1 ]
	[ "$(grep -c '^bin/v-x:' <<<"$output")" -eq 3 ]
}

@test "branding check skips comments" {
	printf '%s\n' '#!/bin/bash' '# info: Hestia Control Panel helper' '	# Hestia keeps this' '// Hestia notes' >"$work/bin/v-x"
	run "$check" "$work"
	[ "$status" -eq 0 ]
}

@test "branding check allows technical identifiers" {
	printf '%s\n' 'source $HESTIA/func/main.sh' 'systemctl restart hestia-web-terminal' \
		'header("Hestia-Exit-Code: $code");' 'echo "?hestia_token=$token"' >"$work/bin/v-x"
	echo 'echo "# Hestia SFTP Chroot" >> $config' >"$work/bin/v-add-sys-sftp-jail"
	run "$check" "$work"
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "branding check flags a second hit on an allowed line" {
	echo 'echo "# Hestia SFTP Chroot added by Hestia Control Panel" >> $config' >"$work/bin/v-add-sys-sftp-jail"
	run "$check" "$work"
	[ "$status" -eq 1 ]
}

@test "branding check needs a root" {
	run "$check"
	[ "$status" -eq 2 ]
	run "$check" "$work/missing"
	[ "$status" -eq 2 ]
}
