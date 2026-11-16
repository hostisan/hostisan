# Sandbox for running Hestia core scripts (bin/v-*) as an ordinary user.
# Load with `load core_sandbox` and call sandbox_init in setup().
#
# sandbox_bin NAME copies bin/NAME into the sandbox with its absolute paths
# (/etc/..., /usr/local/hestia) moved under $root, so a script reads and
# writes only the sandbox. System tools the scripts call (apt-get, dpkg,
# systemctl, ...) are stubs in $root/stub that append their argv to
# $root/calls; helper v-* scripts default to stubs that do the same.

sandbox_init() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	root=$(mktemp -d "${BATS_TMPDIR:-/tmp}/core-sandbox.XXXXXX")
	HESTIA_DIR=$root/usr/local/hestia
	mkdir -p "$root/etc/hestiacp" "$root/stub" "$HESTIA_DIR"/{bin,func,conf,log,data}
	echo "HESTIA='$HESTIA_DIR'" >"$root/etc/hestiacp/hestia.conf"
	cat >"$HESTIA_DIR/conf/hestia.conf" <<-'EOF'
		ROOT_USER='admin'
		WEB_SYSTEM='nginx'
	EOF
	sandbox_rewrite "$repo/func/main.sh" "$HESTIA_DIR/func/main.sh"
	: >"$root/calls"
	for tool in apt-get apt-cache dpkg systemctl curl gpg tee; do
		stub "$tool"
	done
	# `which node` finds nothing: the host's own Node must not leak in.
	stub which 1
	# dpkg-query prints STUB_DPKG_STATUS (default: installed).
	cat >"$root/stub/dpkg-query" <<-EOF
		#!/bin/bash
		echo "dpkg-query \$*" >>"$root/calls"
		printf '%s' "\${STUB_DPKG_STATUS-install ok installed}"
	EOF
	# stat -c %U prints STUB_OWNER (default root): sandbox files belong to the
	# test user. Other stat calls run the real stat.
	cat >"$root/stub/stat" <<-EOF
		#!/bin/bash
		if [ "\$1" = -c ] && [ "\$2" = %U ]; then echo "\${STUB_OWNER:-root}"; exit; fi
		exec /usr/bin/stat "\$@"
	EOF
	# install drops -o/-g (the test user cannot chown) after logging them.
	cat >"$root/stub/install" <<-EOF
		#!/bin/bash
		echo "install \$*" >>"$root/calls"
		args=()
		while [ \$# -gt 0 ]; do
			case "\$1" in -o | -g) shift 2 ;; *) args+=("\$1"); shift ;; esac
		done
		exec /usr/bin/install "\${args[@]}"
	EOF
	chmod 755 "$root/stub/dpkg-query" "$root/stub/stat" "$root/stub/install"
	for v in v-log-action v-change-sys-config-value v-restart-service v-start-service v-stop-service; do
		stub_bin "$v"
	done
	PATH="$root/stub:$PATH"
	export PATH
}

sandbox_teardown() {
	[ -n "$root" ] && rm -rf "$root"
}

# sandbox_rewrite SRC DST: copy SRC to DST with absolute paths under $root.
sandbox_rewrite() {
	sed -e "s#/usr/local/hestia#$HESTIA_DIR#g" \
		-e "s#\([\"' =(]\)/etc/#\1$root/etc/#g" \
		-e "s#^/etc/#$root/etc/#" "$1" >"$2"
	chmod 755 "$2"
}

# sandbox_bin NAME: install the real bin/NAME into the sandbox.
sandbox_bin() {
	sandbox_rewrite "$repo/bin/$1" "$HESTIA_DIR/bin/$1"
}

# stub NAME [EXIT]: a PATH tool that logs "NAME args..." and exits EXIT.
# STUB_EXIT_<NAME> (dashes as underscores) overrides EXIT at run time.
stub() {
	_stub_write "$root/stub/$1" "$1" "${2:-0}"
}

# stub_bin NAME [EXIT]: same, as a helper script in $BIN.
stub_bin() {
	_stub_write "$HESTIA_DIR/bin/$1" "$1" "${2:-0}"
}

_stub_write() {
	local var=STUB_EXIT_${2//[-.]/_}
	cat >"$1" <<-EOF
		#!/bin/bash
		echo "$2 \$*" >>"$root/calls"
		exit "\${$var:-$3}"
	EOF
	chmod 755 "$1"
}

# called PATTERN: some logged call matches the extended regex PATTERN.
called() {
	grep -Eq -- "$1" "$root/calls"
}

# not_called PATTERN: no logged call matches (bats ignores a bare "! cmd").
not_called() {
	if called "$1"; then
		echo "unexpected call matching $1:" >&2
		cat "$root/calls" >&2
		return 1
	fi
}
