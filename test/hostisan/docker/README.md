# core-edits.bats in Docker

`make core-edits-docker` (or `test/hostisan/docker/run.sh [bats args]`) builds a Debian 13 image, makes a minimal fake Hestia from this checkout (`fake-hestia.sh`: repo `bin/`, `func/`, `install/deb` under `/usr/local/hestia`, one user `admin`) and runs `test/hostisan/core-edits.bats` as root. It is not a real install; on a real Hestia box run `bats test/hostisan/core-edits.bats` as root instead.
