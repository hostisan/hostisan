#!/bin/bash
# product.sh: sourced, not run, by the installers and by func/main.sh's
# hostisan_product for Hostisan's product values: HOSTISAN_DOMAIN,
# HOSTISAN_URL, HOSTISAN_DOCS_URL, HOSTISAN_INFO_EMAIL and
# HOSTISAN_SECURITY_EMAIL. It reads product.env next to it
# (install/common/hostisan in the source tree, /usr/local/hestia/install/
# common/hostisan on a server: the hestia deb ships both files).
#
# Without product.env it sets nothing and returns 1: the installers refuse to
# run, and hostisan_product falls back to empty links and a local address. No
# stand-in value here could pass for the domain.

hostisan_product_env="$(CDPATH= cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)/product.env"
if [ ! -r "$hostisan_product_env" ]; then
	unset hostisan_product_env
	return 1
fi
# shellcheck source=install/common/hostisan/product.env
source "$hostisan_product_env"
unset hostisan_product_env
