#!/bin/bash
# Builds a minimal fake Hestia inside the container: the repository (mounted at
# /src) provides bin/, func/ and install/deb under /usr/local/hestia, with one
# Hestia user "admin". Enough for core-edits.bats; not a real install.
set -e
H=/usr/local/hestia
mkdir -p /etc/hestiacp "$H/conf" "$H/log" "$H/data/users/admin" "$H/data/queue" \
	"$H/data/keys" "$H/data/access-keys" "$H/install" "$H/php/bin"
ln -s /src/bin "$H/bin"
ln -s /src/func "$H/func"
ln -s /src/install/deb "$H/install/deb"
ln -s /usr/bin/php "$H/php/bin/php"
echo "HESTIA='$H'" >/etc/hestiacp/hestia.conf
cat >"$H/conf/hestia.conf" <<C
ROOT_USER='admin'
BACKUP_SYSTEM='restic'
C
# Hestia users carry an e-mail address in GECOS (v-list-users list keys on '@').
useradd -m -s /bin/bash -c a@example.test admin
mkdir -p /home/admin/tmp /home/admin/backup
chown admin: /home/admin/tmp /home/admin/backup
cat >"$H/data/users/admin/user.conf" <<C
NAME='admin'
PACKAGE='default'
SUSPENDED='no'
CONTACT='a@example.test'
TIME='00:00:00'
DATE='2026-01-01'
C
touch "$H/data/users/admin/history.log"
