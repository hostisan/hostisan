#!/bin/bash

# ======================================================== #
#
# Hostisan Installation Routine (a HestiaCP fork)
# Automatic OS detection wrapper
# https://github.com/hostisan/hostisan
#
# Supported operating system: Ubuntu 24.04 LTS only (Hostisan v1).
# The code for Debian 12, 13 and Ubuntu 22.04, 26.04 LTS stays, untested:
# --force installs on those anyway, for testing.
#
# ======================================================== #

here=$(cd "$(dirname "$0")" && pwd)

# --help needs neither root nor a supported system. Hostisan v1 installs on
# Ubuntu only, so the Ubuntu installer's help is the help; it names this script.
for arg in "$@"; do
	case $arg in
		-h | --help)
			HOSTISAN_INSTALL_NAME=$0 exec bash "$here/hst-install-ubuntu.sh" --help
			;;
	esac
done

# Am I root?
if [ "x$(id -u)" != 'x0' ]; then
	echo 'Error: this script can only be executed by root'
	exit 1
fi

# An existing admin user, and the other checks on the server and the options,
# are left to the OS installer's preflight, which reports them all together.
# An existing admin group is left to it too: it takes over an empty, unused
# one and disables any sudo rule the panel admin would inherit.

# Detect OS
if [ -e "/etc/os-release" ] && [ ! -e "/etc/redhat-release" ]; then
	type=$(grep "^ID=" /etc/os-release | cut -f 2 -d '=')
	if [ "$type" = "ubuntu" ]; then
		# Check if lsb_release is installed
		if [ -e '/usr/bin/lsb_release' ]; then
			release="$(lsb_release -s -r)"
			VERSION='ubuntu'
		else
			echo "lsb_release is currently not installed, please install it:"
			echo "apt-get update && apt-get install lsb-release"
			exit 1
		fi
	elif [ "$type" = "debian" ]; then
		release=$(cat /etc/debian_version | grep -o "[0-9]\{1,2\}" | head -n1)
		VERSION='debian'
	else
		type="NoSupport"
	fi
else
	type="NoSupport"
fi

no_support_message() {
	echo "****************************************************"
	echo "Your operating system (OS) is not supported by"
	echo "Hostisan. Hostisan v1 supports only:"
	echo "****************************************************"
	echo "  Ubuntu 24.04 LTS"
	echo ""
	exit 1
}

if [ "$type" = "NoSupport" ]; then
	no_support_message
fi

run_os_installer() {
	# The OS installer next to this script (install/ of the Hostisan source
	# tree). Nothing is downloaded: there is no public Hostisan source yet
	# (TODO(O7)).
	local here
	here=$(cd "$(dirname "$0")" && pwd)
	if [ ! -f "$here/hst-install-$type.sh" ]; then
		echo "Error: $here/hst-install-$type.sh not found."
		echo "Run install/hst-install.sh from a copy of the Hostisan source tree."
		exit 1
	fi
	HOSTISAN_INSTALL_NAME=$0 bash "$here/hst-install-$type.sh" "$@"
	exit
}

# Hostisan v1 supports Ubuntu 24.04 only (common/hostisan/os-support.sh).
# The releases the OS installers still have code for install with --force.
# The system is checked before anything on it changes, so a refused system
# prints only the refusal. The Ubuntu installer sets the UTF-8 locale itself,
# once its preflight has passed; for the Debian one it is set here.
if [ ! -r "$here/common/hostisan/os-support.sh" ]; then
	echo "Error: $here/common/hostisan/os-support.sh not found."
	echo "Run install/hst-install.sh from a copy of the Hostisan source tree."
	exit 1
fi
# shellcheck source=install/common/hostisan/os-support.sh
source "$here/common/hostisan/os-support.sh"
force=
for arg in "$@"; do
	case $arg in
		-f | --force) force=yes ;;
	esac
done
if [[ ! "$release" =~ ^(12|13|22.04|24.04|26.04)$ ]]; then
	no_support_message
fi
hostisan_os_check "$type" "$release" "$force" || exit 1
[ "$type" = ubuntu ] || ensure_utf8_locale
run_os_installer "$@"

exit
