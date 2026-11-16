#!/usr/bin/env bash
# ======================================================== #
# Hostisan Quick Bootstrap Installer (Ubuntu 24.04 LTS)
# https://github.com/hostisan/hostisan
# ======================================================== #
set -euo pipefail

# Check for root
if [ "$(id -u)" != "0" ]; then
	echo "Error: this script must be executed as root." >&2
	exit 1
fi

# Supported OS check
if [ ! -f /etc/os-release ]; then
	echo "Error: unable to determine OS (/etc/os-release missing)." >&2
	exit 1
fi

# shellcheck source=/dev/null
. /etc/os-release
if [ "${ID:-}" != "ubuntu" ] || [ "${VERSION_ID:-}" != "24.04" ]; then
	echo "Error: Hostisan v1 currently supports Ubuntu 24.04 LTS only." >&2
	echo "Detected: ${NAME:-Unknown} ${VERSION_ID:-Unknown}" >&2
	exit 1
fi

GH_REPO="hostisan/hostisan"
RELEASE_TAG="v1.10.5-105"
BASE_URL="https://github.com/${GH_REPO}/releases/download/${RELEASE_TAG}"
WORK_DIR="/root/hostisan-src"
DEBS_DIR="/root/hostisan-debs"

echo "=========================================================="
echo "           Hostisan Installation Bootstrap                "
echo "        https://github.com/hostisan/hostisan              "
echo "=========================================================="
echo

mkdir -p "$WORK_DIR" "$DEBS_DIR"

# 1. Download release checksums
echo "[ 1/4 ] Fetching release checksums..."
curl -fsSL "${BASE_URL}/SHA256SUMS" -o "${DEBS_DIR}/SHA256SUMS"

# 2. Download installer tree
echo "[ 2/4 ] Fetching installer tree..."
curl -fsSL "${BASE_URL}/hostisan-install.tar.gz" -o "${WORK_DIR}/hostisan-install.tar.gz"
tar -xzf "${WORK_DIR}/hostisan-install.tar.gz" -C "${WORK_DIR}"

# 3. Download deb packages
echo "[ 3/4 ] Fetching Hostisan packages..."
while read -r _hash filename; do
	case "$filename" in
		*.deb)
			echo "        Downloading $filename..."
			curl -fsSL "${BASE_URL}/${filename}" -o "${DEBS_DIR}/${filename}"
			;;
	esac
done < "${DEBS_DIR}/SHA256SUMS"

# 4. Verify checksums
echo "[ 4/4 ] Verifying package checksums..."
(cd "${DEBS_DIR}" && sha256sum --check --ignore-missing SHA256SUMS)
echo "        Checksum verification succeeded."
echo

# 5. Execute installer
echo "=========================================================="
echo "  Starting Hostisan Installer...                          "
echo "=========================================================="
exec bash "${WORK_DIR}/install/hst-install.sh" --with-debs "${DEBS_DIR}" "$@"
