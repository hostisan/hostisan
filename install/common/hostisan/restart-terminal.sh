#!/usr/bin/env bash
# Usage: restart-terminal.sh restart|try-restart
#        restart-terminal.sh --deferred restart|try-restart
#
# restart-unit.sh for hestia-web-terminal.service: deferred when this runs in
# the web terminal. The terminal postinst calls it by this name, and a
# hostisan-terminal-restart unit scheduled by an older build runs it with
# --deferred.
set -uo pipefail

here=$(dirname -- "$(realpath -- "${BASH_SOURCE[0]}")")
exec "$here/restart-unit.sh" hestia-web-terminal.service "$@"
