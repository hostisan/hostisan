#!/bin/bash
# Runs test/hostisan/core-edits.bats as root on a fake Hestia in Docker.
# Usage: test/hostisan/docker/run.sh [bats args...] (default: the whole file).
set -e
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
image=hostisan-core-edits
docker build -q -t "$image" "$here" >/dev/null
[ "$#" -gt 0 ] || set -- test/hostisan/core-edits.bats
# SYS_PTRACE: the argv check traces the scripts with strace.
exec docker run --rm --cap-add SYS_PTRACE -v "$repo:/src:ro" "$image" \
	bash -c '/src/test/hostisan/docker/fake-hestia.sh && cd /src && bats "$@"' _ "$@"
