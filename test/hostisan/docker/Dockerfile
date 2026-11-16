# Throwaway Debian 13 image for test/hostisan/core-edits.bats (make core-edits-docker).
FROM debian:trixie
RUN apt-get update \
	&& DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		bats php-cli restic openssl jq at idn2 lsb-release ca-certificates procps strace \
	&& rm -rf /var/lib/apt/lists/*
