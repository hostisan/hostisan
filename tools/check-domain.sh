#!/usr/bin/env bash
# check-domain.sh ROOT: fail when Hostisan's product domain is written anywhere
# under ROOT but install/common/hostisan/product.env, the one place it is set.
#
# The domain is HOSTISAN_DOMAIN in ROOT's product.env, so the check follows a
# change of domain. Matched as a whole word, ignoring case, in every text file
# (comments too: a stale comment is as wrong as stale code), both as written
# and with its dots escaped for a regex (example\.com for example.com).
# Not scanned:
#   - product.env itself;
#   - Markdown docs (*.md): text, updated by hand (RELEASING.md);
#   - .git, node_modules, the bats submodules (test/test_helper), build output
#     (dist, ui/dist, and build/ at the top: make debs, whose log and debs
#     carry the domain as built) and agent folders (.claude, .superpowers).
# Each hit is reported as PATH:LINE: TEXT. Exit 0 when nothing is reported,
# 1 when something is, 2 on a usage error.
set -euo pipefail

root=${1:-}
if [ -z "$root" ] || [ ! -d "$root" ]; then
	echo "usage: check-domain.sh ROOT (an existing directory)" >&2
	exit 2
fi
cd "$root"
env_file=install/common/hostisan/product.env
if [ ! -f "$env_file" ]; then
	echo "check-domain.sh: $root/$env_file not found" >&2
	exit 2
fi
domain=$(env -i bash -c 'source "$1" && printf "%s" "${HOSTISAN_DOMAIN:-}"' _ "$env_file")
if [ -z "$domain" ]; then
	echo "check-domain.sh: no HOSTISAN_DOMAIN in $env_file" >&2
	exit 2
fi

# grep exits 1 when nothing matches: that is the clean case. Both patterns are
# fixed strings: the second is the domain as a regex would spell it.
hits=$(grep -rIniwF \
	--exclude-dir=.git --exclude-dir=node_modules --exclude-dir=.claude \
	--exclude-dir=.superpowers --exclude-dir=dist --exclude-dir=test_helper \
	--exclude='*.md' -e "$domain" -e "${domain//./\\.}" . | grep -v -e "^\./$env_file:" -e '^\./build/' || true)
if [ -n "$hits" ]; then
	sed 's|^\./||' <<< "$hits"
	echo "check-domain.sh: '$domain' is written outside $env_file; read it from there" >&2
	exit 1
fi
