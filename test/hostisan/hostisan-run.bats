#!/usr/bin/env bats
# bin/v-hostisan-run (Plan 6 C2 S7): the panel's runner calls every v-* command
# through it, so a command that hits the runner's deadline is stopped as root,
# children included. Runs without root: the wrapper finds its bin dir from its
# own path, so a copy in a temp bin dir runs the temp scripts.

# shellcheck disable=SC2016 # literal $ in argv and script bodies on purpose

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	dir=$(mktemp -d "${BATS_TMPDIR:-/tmp}/hostisan-run.XXXXXX")
	bin=$dir/bin
	mkdir "$bin"
	cp "$repo/bin/v-hostisan-run" "$bin/"
	run_cmd=$bin/v-hostisan-run
}

teardown() {
	if [ -f "$dir/pid" ]; then kill -KILL "$(cat "$dir/pid")" 2>/dev/null || true; fi
	rm -rf "$dir"
}

script() { # NAME BODY
	printf '#!/bin/bash\n%s\n' "$2" >"$bin/$1"
	chmod 755 "$bin/$1"
}

alive() { kill -0 "$1" 2>/dev/null; }

@test "hostisan-run: passes argv through unchanged" {
	script v-list-user 'printf "%s|" "$#" "$@"'
	run "$run_cmd" 10 v-list-user 'a b' '' '$(id)' '*' '-x' ';echo pwned'
	[ "$status" -eq 0 ]
	[ "$output" = '6|a b||$(id)|*|-x|;echo pwned|' ]
}

@test "hostisan-run: keeps stdin, stdout, stderr and the exit code" {
	script v-list-user 'read -r line; echo "out:$line"; echo err >&2; exit 3'
	run bash -c 'echo hello | "$1" 10 v-list-user 2>"$2"' _ "$run_cmd" "$dir/err"
	[ "$status" -eq 3 ]
	[ "$output" = out:hello ]
	[ "$(cat "$dir/err")" = err ]
}

@test "hostisan-run: refuses bad seconds" {
	script v-list-user 'echo ran'
	for secs in 0 -1 01 1.5 abc '' 86401 999999 ' 5' '5 '; do
		run "$run_cmd" "$secs" v-list-user
		[ "$status" -ne 0 ] || { echo "accepted seconds [$secs]"; false; }
		[[ $output != *ran* ]]
	done
	run "$run_cmd" 86400 v-list-user
	[ "$status" -eq 0 ]
}

@test "hostisan-run: refuses bad command names" {
	script v-list-user 'echo ran'
	for name in '' list-user v-List-user 'v-list user' v-list_user ../bin/v-list-user \
		"$bin/v-list-user" 'v-list-user/' v-hostisan-run; do
		run "$run_cmd" 5 "$name"
		[ "$status" -ne 0 ] || { echo "accepted command [$name]"; false; }
		[[ $output != *ran* ]]
	done
}

@test "hostisan-run: refuses missing commands, links and directories" {
	script v-real 'echo ran'
	ln -s "$bin/v-real" "$bin/v-link"
	mkdir "$bin/v-dir"
	for name in v-missing v-link v-dir; do
		run "$run_cmd" 5 "$name"
		[ "$status" -ne 0 ] || { echo "accepted [$name]"; false; }
		[[ $output != *ran* ]]
	done
}

@test "hostisan-run: needs seconds and a command" {
	run "$run_cmd"
	[ "$status" -ne 0 ]
	run "$run_cmd" 5
	[ "$status" -ne 0 ]
}

@test "hostisan-run: timeout exits 124 and kills a background grandchild" {
	script v-slow "sleep 300 >/dev/null 2>&1 & echo \$! >'$dir/pid'; wait"
	start=$SECONDS
	run "$run_cmd" 1 v-slow
	[ "$status" -eq 124 ]
	[ $((SECONDS - start)) -le 3 ]
	pid=$(cat "$dir/pid")
	for _ in 1 2 3 4 5 6 7 8 9 10; do alive "$pid" || break; sleep 0.2; done
	if alive "$pid"; then echo "pid $pid still runs"; false; fi
}

@test "hostisan-run: a command ignoring SIGTERM is killed (137)" {
	script v-stubborn "trap '' TERM; sleep 300 >/dev/null 2>&1 & echo \$! >'$dir/pid'; while :; do sleep 0.1; done"
	run timeout 20 "$run_cmd" 1 v-stubborn
	[ "$status" -eq 137 ]
	pid=$(cat "$dir/pid")
	for _ in 1 2 3 4 5 6 7 8 9 10; do alive "$pid" || break; sleep 0.2; done
	if alive "$pid"; then echo "pid $pid still runs"; false; fi
}

@test "hostisan-run: a TERM-ignoring grandchild dies after the script stops on SIGTERM" {
	# The script dies on SIGTERM at once, so timeout exits 124 without its own SIGKILL;
	# the wrapper must still sweep the group.
	script v-leaky "(trap '' TERM; exec sleep 300) >/dev/null 2>&1 & echo \$! >'$dir/pid'; wait"
	start=$SECONDS
	run "$run_cmd" 1 v-leaky
	[ "$status" -eq 124 ]
	[ $((SECONDS - start)) -le 8 ]
	pid=$(cat "$dir/pid")
	for _ in 1 2 3 4 5 6 7 8 9 10; do alive "$pid" || break; sleep 0.2; done
	if alive "$pid"; then echo "pid $pid still runs"; false; fi
}

@test "hostisan-run: a command that exits 124 in time keeps its background child" {
	script v-quick "sleep 300 >/dev/null 2>&1 </dev/null & echo \$! >'$dir/pid'; exit 124"
	run "$run_cmd" 5 v-quick
	[ "$status" -eq 124 ]
	sleep 0.5
	alive "$(cat "$dir/pid")"
}

@test "hostisan-run: SIGTERM to the wrapper stops the command's group" {
	# The panel's backstop: sudo relays its SIGTERM to the wrapper.
	script v-slow "sleep 300 >/dev/null 2>&1 & echo \$! >'$dir/pid'; wait"
	"$run_cmd" 60 v-slow >/dev/null 2>&1 &
	wrapper=$!
	for _ in $(seq 50); do [ -s "$dir/pid" ] && break; sleep 0.1; done
	kill -TERM "$wrapper"
	status=0
	wait "$wrapper" || status=$?
	[ "$status" -eq 143 ]
	pid=$(cat "$dir/pid")
	for _ in 1 2 3 4 5 6 7 8 9 10; do alive "$pid" || break; sleep 0.2; done
	if alive "$pid"; then echo "pid $pid still runs"; false; fi
}

@test "hostisan-run: refuses to run without GNU timeout" {
	script v-list-user 'echo ran'
	sed -i 's|^timeout_bin=/usr/bin/timeout$|timeout_bin=/nonexistent/timeout|' "$run_cmd"
	grep -q '^timeout_bin=/nonexistent/timeout$' "$run_cmd"
	run "$run_cmd" 5 v-list-user
	[ "$status" -ne 0 ]
	[[ $output == *"/nonexistent/timeout"* ]]
	[[ $output != *ran* ]]
}

@test "hostisan-run: a background child survives normal completion" {
	# Scripts that start a daemon on purpose must keep it when they finish in time.
	script v-start "sleep 300 >/dev/null 2>&1 </dev/null & echo \$! >'$dir/pid'"
	run "$run_cmd" 5 v-start
	[ "$status" -eq 0 ]
	sleep 0.5
	alive "$(cat "$dir/pid")"
}
