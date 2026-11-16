#!/bin/bash
# os-support.sh: sourced, not run, by install/hst-install.sh and the OS
# installers (hst-install-{debian,ubuntu}.sh) of the same source tree.
#
# Hostisan v1 supports Ubuntu 24.04 LTS only. The installers keep upstream's
# code for Debian 12 and 13 and Ubuntu 22.04 and 26.04, untested; --force
# (-f) installs on those anyway, for testing, and on nothing else.

# hostisan_os_supported ID RELEASE: 0 on a system Hostisan supports. ID is
# os-release's ID, RELEASE the installers' $release (24.04, or 13 on Debian).
hostisan_os_supported() {
	[ "$1" = ubuntu ] && [ "$2" = 24.04 ]
}

# hostisan_os_known ID RELEASE: 0 on a system the installers have code for
# (Debian 12, 13; Ubuntu 22.04, 24.04, 26.04), supported or not.
hostisan_os_known() {
	case "$1 $2" in
		"debian 12" | "debian 13" | "ubuntu 22.04" | "ubuntu 24.04" | "ubuntu 26.04") return 0 ;;
	esac
	return 1
}

# hostisan_os_check ID RELEASE FORCE: 0 on a supported system. Elsewhere it
# says Hostisan v1 supports only Ubuntu 24.04 and returns 1; with FORCE set
# (--force) on a system the installers have code for (hostisan_os_known) it
# says the system is untested and returns 0. On any other system it returns
# 1, FORCE or not: the installers would not know what to install there.
hostisan_os_check() {
	local name
	case $1 in
		ubuntu) name="Ubuntu $2" ;;
		debian) name="Debian $2" ;;
		*) name="$1 $2" ;;
	esac
	if hostisan_os_supported "$1" "$2"; then
		return 0
	fi
	if [ -n "${3:-}" ] && ! hostisan_os_known "$1" "$2"; then
		echo "Error: $name is not supported: Hostisan v1 supports only Ubuntu 24.04 LTS,"
		echo "and the installers have no code for $name, so --force cannot install there."
		return 1
	fi
	if [ -n "${3:-}" ]; then
		echo "[ ! ] $name is not supported: Hostisan v1 supports only Ubuntu 24.04 LTS."
		echo "[ ! ] Installing anyway (--force), for testing only: this system is untested."
		return 0
	fi
	echo "Error: Hostisan v1 supports only Ubuntu 24.04 LTS; this server runs $name."
	echo "Debian 12 and 13 and Ubuntu 22.04 and 26.04 are not tested yet."
	echo "To install anyway, for testing only, add --force (it also skips the"
	echo "installer's other safety checks)."
	return 1
}

# ensure_utf8_locale: make C.UTF-8 the system locale when the current one is
# not UTF-8 (upstream's, from install/hst-install.sh). The Ubuntu installer
# calls it once its preflight has passed and the install is confirmed, so a
# refused install changes nothing; the wrapper calls it for the Debian one.
ensure_utf8_locale() {
	local locale_file="/etc/default/locale"

	if locale | grep -qi 'utf-8'; then
		return
	fi

	echo "[ * ] Enabling UTF-8 locale support via C.UTF-8"
	if ! locale-gen C.UTF-8; then
		echo "[ ! ] Failed to generate C.UTF-8 locale. Leaving existing locale untouched."
		return
	fi

	if ! update-locale LANG=C.UTF-8; then
		echo "[ ! ] Failed to update LANG in $locale_file. Leaving existing locale untouched."
		return
	fi

	export LANG=C.UTF-8
}
