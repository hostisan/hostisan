#!/usr/bin/env bash
# install-run.sh IMAGE DEB_DIR
# install-run.sh IMAGE --apt-repo REPO_DIR KEY_FILE
#
# A Docker install smoke (Plan 6, B8).
#
# Starts a systemd container from IMAGE (ubuntu:24.04, debian:trixie), gives
# it an empty "admin" group (and, on images without it, the
# "%admin ALL=(ALL) ALL" sudo rule of Ubuntu and of the VPS image), runs
# install/hst-install.sh from this tree with --with-debs (the debs in DEB_DIR,
# built for that distro) and a minimal stack; or, in the second form, from the
# signed repository REPO_DIR (tools/apt-repo/publish.sh) with its public key
# KEY_FILE (a test key, so --apt-key-unpinned) and the web terminal on; then
# test/hostisan/install-smoke.sh
# and test/hostisan/core-edits.bats.
# Needs docker with privileged containers; downloads the distro packages.
# The container (SMOKE_CONTAINER, default hostisan-smoke) is kept for
# inspection when KEEP=1.
set -euo pipefail

usage="usage: install-run.sh IMAGE DEB_DIR | IMAGE --apt-repo REPO_DIR KEY_FILE"
image=${1:?$usage}
if [ "${2:-}" = --apt-repo ]; then
	src=$(cd "${3:?$usage}" && pwd)
	key=$(cd "$(dirname "${4:?$usage}")" && pwd)/$(basename "$4")
	mounts=(-v "$src:/repo:ro" -v "$key:/repo-key:ro")
	source_args=(--apt-repo file:/repo --apt-key /repo-key --apt-key-unpinned --webterminal yes)
else
	src=$(cd "${2:?$usage}" && pwd)
	mounts=(-v "$src:/debs:ro")
	source_args=(--with-debs /debs --webterminal no)
fi
repo=$(cd "$(dirname "$0")/../../.." && pwd)
name=${SMOKE_CONTAINER:-hostisan-smoke}
tag=hostisan-smoke:$(tr ':/' '--' <<< "$image")

docker build -q -t "$tag" - << EOF > /dev/null
FROM $image
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends systemd systemd-sysv \
	dbus lsb-release ca-certificates curl wget sudo procps iproute2 jq && rm -rf /var/lib/apt/lists/*
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF

docker rm -f "$name" > /dev/null 2>&1 || true
docker run -d --name "$name" --hostname host.hostisan.test --privileged --cgroupns=host \
	-v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock \
	-v "$repo:/src:ro" "${mounts[@]}" "$tag" > /dev/null
cleanup() {
	if [ "${KEEP:-0}" != 1 ]; then
		docker rm -f "$name" > /dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

for _ in $(seq 30); do
	state=$(docker exec "$name" systemctl is-system-running 2> /dev/null || true)
	case "$state" in running | degraded) break ;; esac
	sleep 1
done

docker exec "$name" bash -c '
	set -e
	getent group admin > /dev/null || groupadd -r admin
	grep -q "^%admin" /etc/sudoers || printf "%%admin ALL=(ALL) ALL\n" >> /etc/sudoers
	cp -r /src/install /root/hostisan-install'

docker exec -e TERM=dumb "$name" bash /root/hostisan-install/hst-install.sh \
	"${source_args[@]}" --interactive no --force \
	--hostname host.hostisan.test --email admin@hostisan.test --username admin --password 'Sm0ke-test-pass' \
	--apache no --phpfpm yes --multiphp no --vsftpd no --proftpd no --named no \
	--mysql no --mysql8 no --postgresql no --exim no --dovecot no --sieve no \
	--clamav no --spamassassin no --iptables no --fail2ban no --quota no \
	--resourcelimit no --api yes

docker exec "$name" /src/test/hostisan/install-smoke.sh admin 'Sm0ke-test-pass'
docker exec "$name" bash -c 'cd /tmp && /src/test/test_helper/bats-core/bin/bats /src/test/hostisan/core-edits.bats'
