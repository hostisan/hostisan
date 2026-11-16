#!/usr/bin/env bash
# build-debs.sh (make debs): build the four Hostisan debs (hestia,
# hostisan-nginx, hostisan-php, hostisan-web-terminal) in a clean ubuntu:24.04
# container, from a git archive of REF (HEAD). Uncommitted changes are not
# built. Next to the debs it writes VERSION (the build stamp), the install/
# tree as hostisan-install.tar.gz (stamped the same way), SHA256SUMS and
# build.log.
#
# src/hst_autocompile.sh can exit 0 after a failed compile, so the debs are
# checked inside the container (--verify: each package's compiled binary is
# there and not empty, and the version carries this build number), and the
# build fails when any of the four is missing or did not pass.
#
# Exit 0 = built, 1 = refused or failed, 2 = usage error.
set -euo pipefail

usage() {
	cat <<- 'EOF'
		Usage: tools/build-debs.sh [--out DIR] [--ref REF] [--image IMAGE]
		       tools/build-debs.sh --verify DIR BUILD

		Builds hostisan, hostisan-nginx, hostisan-php and hostisan-web-terminal in a
		clean Docker container from a git archive of REF, and puts them in DIR.

		  --out DIR      output directory (default: build/debs in the repo)
		  --ref REF      the commit or tag to build (default: HEAD)
		  --image IMAGE  the build image (default: ubuntu:24.04)
		  --verify DIR BUILD
		                 check the four debs in DIR: each is there once, holds
		                 its compiled binary and has build number BUILD, and
		                 hestia holds the built web UI

		Environment:
		  HOSTISAN_BUILD    the build number in the deb version, 105 or more
		                    (default 105). Use a higher one for every rebuild
		                    meant for a server that has older debs.
		  HOSTISAN_VERSION  what `hostisan version` prints (default: git
		                    describe of REF)
		  DOCKER            the docker command, with any arguments (default:
		                    docker; "sudo docker" works)
	EOF
}

die() {
	echo "build-debs: $*" >&2
	exit 1
}

packages=(hostisan hostisan-nginx hostisan-php hostisan-web-terminal)
min_build=105

# payload PACKAGE: the compiled file PACKAGE must hold (none for the terminal,
# a stub package).
payload() {
	case $1 in
		hostisan) echo usr/local/hestia/hostisan/hostisan ;;
		hostisan-nginx) echo usr/local/hestia/nginx/sbin/hestia-nginx ;;
		hostisan-php) echo usr/local/hestia/php/sbin/hestia-php ;;
	esac
}

# find_deb DIR PACKAGE: print the one PACKAGE_*.deb in DIR; say what is wrong
# and return 1 when there is none or more than one.
find_deb() {
	local f
	f=$(compgen -G "$1/$2_*.deb" || true)
	if [ -z "$f" ]; then
		echo "missing: $2_*.deb"
		return 1
	fi
	if [ "$(wc -l <<< "$f")" -ne 1 ]; then
		echo "more than one $2_*.deb in $1"
		return 1
	fi
	printf '%s\n' "$f"
}

# deb_size DEB PATH: the size of the regular file PATH in DEB (nothing when
# it is not there).
deb_size() {
	dpkg-deb -c "$1" | awk -v p="./$2" '$NF == p && $1 ~ /^-/ { print $3 }'
}

# web_ui_problems DEB: what is missing from the hostisan deb's web UI. The panel
# binary embeds the React UI (ui/, built by make ui-build into the go:embed
# dir hostisan/internal/webui/dist), whose index.html names a Vite asset
# (src="/assets/index-HASH.js"); without that build the binary serves only
# a "not built" page. The old pages' CSS themes come from npm run build.
web_ui_problems() {
	local deb=$1 n css=usr/local/hestia/web/css/themes/default.min.css size
	# grep -c reads all of it (grep -q would stop early, and pipefail would
	# count the broken pipe as a failure)
	n=$(dpkg-deb --fsys-tarfile "$deb" | tar -xO "./$(payload hostisan)" 2> /dev/null \
		| grep -acE 'src="/assets/index-[A-Za-z0-9_-]+\.js"' || true)
	if [ "${n:-0}" -eq 0 ]; then
		echo "hostisan: the hostisan binary holds no built web UI (no /assets/index-*.js in its index.html): the UI build failed"
	fi
	size=$(deb_size "$deb" "$css")
	if [ -z "$size" ] || [ "$size" -eq 0 ]; then
		echo "hostisan: $css is missing or empty in $(basename "$deb"): npm run build failed"
	fi
}

# verify DIR BUILD: the four debs in DIR are complete builds of BUILD, and the
# hestia deb holds the built web UI.
verify() {
	local dir=$1 build=$2 pkg deb want size version problems bad=0
	for pkg in "${packages[@]}"; do
		if ! deb=$(find_deb "$dir" "$pkg"); then
			echo "$deb" >&2
			bad=1
			continue
		fi
		version=$(dpkg-deb -f "$deb" Version)
		if [[ ! $version =~ ^1:[^-]+-${build}\+ ]]; then
			echo "$pkg: version $version is not build $build (want 1:…-${build}+…)" >&2
			bad=1
		fi
		want=$(payload "$pkg")
		[ -n "$want" ] || continue
		size=$(deb_size "$deb" "$want")
		if [ -z "$size" ] || [ "$size" -eq 0 ]; then
			echo "$pkg: $want is missing or empty in $(basename "$deb"): its compile failed" >&2
			bad=1
		elif [ "$pkg" = hostisan ]; then
			problems=$(web_ui_problems "$deb")
			if [ -n "$problems" ]; then
				printf '%s\n' "$problems" >&2
				bad=1
			fi
		fi
	done
	return "$bad"
}

out=
ref=HEAD
image=ubuntu:24.04
while [ "$#" -gt 0 ]; do
	case $1 in
		-h | --help)
			usage
			exit 0
			;;
		--verify)
			[ "$#" -eq 3 ] || {
				usage >&2
				exit 2
			}
			if verify "$2" "$3"; then
				echo "build-debs: the four debs in $2 are complete builds of $3"
				exit 0
			fi
			exit 1
			;;
		--out | --ref | --image)
			[ "$#" -ge 2 ] && [ -n "$2" ] || {
				usage >&2
				exit 2
			}
			case $1 in
				--out) out=$2 ;;
				--ref) ref=$2 ;;
				--image) image=$2 ;;
			esac
			shift 2
			;;
		*)
			usage >&2
			exit 2
			;;
	esac
done

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
out=${out:-$root/build/debs}
# DOCKER may carry arguments ("sudo docker"): split into words
read -r -a docker <<< "${DOCKER:-docker}"
[ "${#docker[@]}" -gt 0 ] || docker=(docker)

build=${HOSTISAN_BUILD:-$min_build}
if [[ ! $build =~ ^[0-9]+$ ]] || [ "$build" -lt "$min_build" ]; then
	die "HOSTISAN_BUILD is '$build'; it must be a number, $min_build or more (hostisan-web-terminal needs hostisan >= 1:1.10.5-$min_build)"
fi
commit=$(git -C "$root" rev-parse -q --verify "$ref^{commit}") || die "unknown ref: $ref"
version=${HOSTISAN_VERSION:-$(git -C "$root" describe --always "$commit")}
# it ends up in a make variable, the panel binary and the installer banner
if [[ ! $version =~ ^[A-Za-z0-9][A-Za-z0-9._+~-]*$ ]]; then
	die "HOSTISAN_VERSION '$version' may hold only A-Z a-z 0-9 . _ + ~ -"
fi
stamp="$version (build $build)"

if ! command -v "${docker[0]}" > /dev/null 2>&1 || ! "${docker[@]}" info > /dev/null 2>&1; then
	die "make debs needs Docker (a running daemon and the docker command). Install Docker, or build on an Ubuntu 24.04 machine as README.md shows."
fi
if [ "$ref" = HEAD ] && [ -n "$(git -C "$root" status --porcelain --untracked-files=no)" ]; then
	echo "build-debs: note: uncommitted changes are not built (the build uses a git archive of HEAD)"
fi

mkdir -p "$out"
out=$(cd "$out" && pwd)
tmp=$(mktemp -d "$out/.build.XXXXXX")
# The container goes too, when the build ends or is stopped (Ctrl-C)
container=hostisan-debs-$$
trap '"${docker[@]}" rm -f "$container" >/dev/null 2>&1; rm -rf "$tmp"' EXIT
mkdir "$tmp/debs"
log=$out/build.log

echo "build-debs: building $stamp from $(git -C "$root" rev-parse --short "$commit") in $image"
echo "build-debs: this takes about 20 minutes; the full log is $log"

# Inside the container (--init: an init as PID 1, so a stop signal reaches the
# build): unpack the archive from stdin, build all four debs, check them, and
# hand them to the caller's user even when something failed.
inner='set -e
trap "chown -R \"$HOST_UID:$HOST_GID\" /out" EXIT
mkdir /src
tar -x -C /src
apt-get -qq update
apt-get -qq install -y git curl wget ca-certificates lsb-release >/dev/null
cd /src
bash src/hst_autocompile.sh --all --noinstall --keepbuild '"'~localsrc'"'
bash tools/build-debs.sh --verify /out "$HOSTISAN_BUILD"
touch /out/.verified'

start=$(date +%s)
rc=0
git -C "$root" archive "$commit" \
	| "${docker[@]}" run -i --rm --init --name "$container" \
		-e DEBIAN_FRONTEND=noninteractive -e TERM=dumb \
		-e HOSTISAN_BUILD="$build" -e HOSTISAN_VERSION="$version" \
		-e BUILD_DIR=/build -e DEB_DIR=/out \
		-e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
		-v "$tmp/debs:/out" "$image" bash -c "$inner" > "$log" 2>&1 || rc=$?
minutes=$((($(date +%s) - start + 59) / 60))

# All four, once each, checked
problems=()
for pkg in "${packages[@]}"; do
	if ! msg=$(find_deb "$tmp/debs" "$pkg"); then
		problems+=("$msg")
	fi
done
if [ "${#problems[@]}" -eq 0 ] && [ ! -e "$tmp/debs/.verified" ]; then
	problems+=("the debs did not pass their checks: a compile failed even though hst_autocompile.sh exited 0")
fi
if [ "$rc" -ne 0 ]; then
	problems+=("the build container exited $rc")
fi
if [ "${#problems[@]}" -gt 0 ]; then
	printf 'build-debs: %s\n' "${problems[@]}" >&2
	echo "build-debs: FAILED after about $minutes min; no file in $out was replaced. The end of $log:" >&2
	tail -n 25 "$log" >&2 || true
	exit 1
fi

# The install/ tree the server needs, with the same stamp as the debs
mkdir "$tmp/tree"
git -C "$root" archive "$commit" install | tar -x -C "$tmp/tree"
printf '%s\n' "$stamp" > "$tmp/tree/install/common/hostisan/VERSION"
tar -czf "$tmp/debs/hostisan-install.tar.gz" -C "$tmp/tree" install
printf '%s\n' "$stamp" > "$tmp/debs/VERSION"

# Replace the previous build's files
rm -f "$out"/hostisan_*.deb "$out"/hostisan-nginx_*.deb "$out"/hostisan-php_*.deb \
	"$out"/hostisan-web-terminal_*.deb "$out"/hostisan-install.tar.gz "$out"/VERSION "$out"/SHA256SUMS
mv "$tmp"/debs/*.deb "$tmp/debs/hostisan-install.tar.gz" "$tmp/debs/VERSION" "$out"/
(cd "$out" && sha256sum ./*.deb hostisan-install.tar.gz | sed 's# \./# #' > SHA256SUMS)

echo "build-debs: built $stamp in about $minutes min:"
(cd "$out" && ls -l ./*.deb ./hostisan-install.tar.gz | awk '{ printf "  %10s  %s\n", $5, substr($NF, 3) }')
cat << EOF
Next, on the server (as root), with SERVER your server's address:
  scp $out/*.deb $out/hostisan-install.tar.gz root@SERVER:/root/
  ssh root@SERVER
  mkdir -p /root/hostisan-debs && mv /root/*.deb /root/hostisan-debs/
  tar -xzf /root/hostisan-install.tar.gz -C /root
  bash /root/install/hst-install.sh --with-debs /root/hostisan-debs --hostname panel.example.com --email you@example.com
EOF
