#!/usr/bin/env bats
# Hostisan's product domain (install/common/hostisan/product.env): the one place
# the domain, the site URL and the contact addresses are written. Checks the
# file's format, its bash loader (product.sh), the guard (tools/check-domain.sh),
# and that a new domain reaches every consumer and the old one none.

bats_require_minimum_version 1.5.0

setup() {
	repo=$(cd "$BATS_TEST_DIRNAME/../.." && pwd)
	env_file=$repo/install/common/hostisan/product.env
	loader=$repo/install/common/hostisan/product.sh
	check=$repo/tools/check-domain.sh
	work=$(mktemp -d "${BATS_TMPDIR:-/tmp}/product.XXXXXX")
	# shellcheck source=install/common/hostisan/product.env
	source "$env_file"
	old=$HOSTISAN_DOMAIN
}

teardown() {
	rm -rf "$work"
}

# product_copy DIR DOMAIN: product.sh and a product.env for DOMAIN in DIR.
product_copy() {
	mkdir -p "$1"
	cp "$loader" "$1/product.sh"
	sed "s/^HOSTISAN_DOMAIN=.*/HOSTISAN_DOMAIN=$2/" "$env_file" >"$1/product.env"
}

# make_values ENV: the values Make reads from product file ENV, as KEY=value.
make_values() {
	env -i PATH="$PATH" make -s --no-print-directory -f - PRODUCT_ENV="$1" <<-'EOF'
		include $(PRODUCT_ENV)
		all: ; @$(foreach v,$(sort $(filter HOSTISAN_%,$(.VARIABLES))),echo '$(v)=$($(v))';)
	EOF
}

# bash_values ENV: the values bash reads from ENV, as KEY=value.
bash_values() {
	env -i bash -c 'source "$1"; for v in $(compgen -v HOSTISAN_ | sort); do echo "$v=${!v}"; done' _ "$1"
}

# ---------------------------------------------------------------- the file

@test "product.env is plain KEY=value lines" {
	local bad
	# the value characters are the ones src/hst_autocompile.sh's
	# hostisan_render_control accepts: keep the two rules the same
	bad=$(grep -vE '^(#.*)?$' "$env_file" |
		grep -vE '^HOSTISAN_[A-Z_]+=([A-Za-z0-9.@:/_-]|\$\{HOSTISAN_[A-Z_]+\})+$' || true)
	[ -z "$bad" ]
	[ -n "$HOSTISAN_DOMAIN" ]
	[ "$HOSTISAN_URL" = "https://$HOSTISAN_DOMAIN" ]
	[ "$HOSTISAN_DOCS_URL" = "https://$HOSTISAN_DOMAIN/docs" ]
	[ "$HOSTISAN_INFO_EMAIL" = "info@$HOSTISAN_DOMAIN" ]
	[ "$HOSTISAN_SECURITY_EMAIL" = "security@$HOSTISAN_DOMAIN" ]
	# the domain is written once; everything else is built on it
	[ "$(grep -ciF "$HOSTISAN_DOMAIN" "$env_file")" -eq 1 ]
}

@test "bash and make read product.env the same" {
	command -v make >/dev/null || skip "needs make"
	run make_values "$env_file"
	[ "$status" -eq 0 ]
	[ "$output" = "$(bash_values "$env_file")" ]
	[[ "$output" == *"HOSTISAN_URL=https://$old"* ]]
}

@test "product.sh loads product.env next to it" {
	product_copy "$work/p" example.test
	run env -i bash -c 'source "$1"; echo "$HOSTISAN_DOMAIN $HOSTISAN_URL $HOSTISAN_DOCS_URL $HOSTISAN_INFO_EMAIL $HOSTISAN_SECURITY_EMAIL"' _ "$work/p/product.sh"
	[ "$status" -eq 0 ]
	[ "$output" = "example.test https://example.test https://example.test/docs info@example.test security@example.test" ]
	# from another directory too
	run env -i bash -c 'cd / && source "$1" && echo "$HOSTISAN_URL"' _ "$work/p/product.sh"
	[ "$output" = "https://example.test" ]
}

@test "product.sh without product.env sets nothing and fails" {
	mkdir -p "$work/p"
	cp "$loader" "$work/p/product.sh"
	run env -i bash -c 'source "$1"; echo "rc=$? ${HOSTISAN_DOMAIN-unset} ${HOSTISAN_URL-unset} ${HOSTISAN_INFO_EMAIL-unset}"' _ "$work/p/product.sh"
	[ "$status" -eq 0 ]
	[ "$output" = "rc=1 unset unset unset" ]
	# no stand-in that looks like a domain (a URL path is not one)
	run ! grep -qE '^[^#]*HOSTISAN_[A-Z_]+=' "$loader"
}

@test "hostisan_product falls back without product.sh or product.env" {
	local want='|||root@localhost.localdomain|root@localhost.localdomain'
	# shellcheck disable=SC2016 # expanded by the inner bash
	local show='HESTIA=$1; source "$2/func/main.sh"; hostisan_product; echo "$HOSTISAN_DOMAIN|$HOSTISAN_URL|$HOSTISAN_DOCS_URL|$HOSTISAN_INFO_EMAIL|$HOSTISAN_SECURITY_EMAIL"'
	# no product.sh (main.sh reads conf/hestia.conf)
	mkdir -p "$work/h/conf"
	: >"$work/h/conf/hestia.conf"
	run env -i PATH="$PATH" bash -c "$show" _ "$work/h" "$repo"
	[ "$status" -eq 0 ]
	[ "$output" = "$want" ]
	# product.sh without product.env
	mkdir -p "$work/h/install/common/hostisan"
	cp "$loader" "$work/h/install/common/hostisan/product.sh"
	run env -i PATH="$PATH" bash -c "$show" _ "$work/h" "$repo"
	[ "$status" -eq 0 ]
	[ "$output" = "$want" ]
	# both: the values from product.env
	product_copy "$work/h/install/common/hostisan" example.test
	run env -i PATH="$PATH" bash -c "$show" _ "$work/h" "$repo"
	[ "$output" = "example.test|https://example.test|https://example.test/docs|info@example.test|security@example.test" ]
	# the fallback address passes Hestia's e-mail check (v-add-user)
	[[ root@localhost.localdomain =~ ^[A-Za-z0-9._%+-]+@[[:alnum:].-]+\.[A-Za-z]{2,63}$ ]]
}

# ---------------------------------------------------------------- bash consumers

# fake_hestia DIR DOMAIN: a /usr/local/hestia under DIR with func/, an empty
# conf/hestia.conf and the product files for DOMAIN, as the hestia deb lays
# them out.
fake_hestia() {
	mkdir -p "$1/func" "$1/conf"
	: >"$1/conf/hestia.conf"
	cp "$repo/func/main.sh" "$repo/func/upgrade.sh" "$repo/func/syshealth.sh" "$1/func/"
	product_copy "$1/install/common/hostisan" "$2"
}

@test "the upgrade banner shows the site from product.env" {
	fake_hestia "$work/h" example.test
	run env -i PATH="$PATH" bash -c 'HESTIA=$1; source "$HESTIA/func/main.sh"; source "$HESTIA/func/upgrade.sh"; upgrade_complete_message' _ "$work/h"
	[ "$status" -eq 0 ]
	[[ "$output" == *"Web:      https://example.test/ "* ]]
	run ! grep -qiF "$old" <<<"$output"
	# without product.env, no Web line (and no error)
	rm "$work/h/install/common/hostisan/product.env"
	run env -i PATH="$PATH" bash -c 'HESTIA=$1; source "$HESTIA/func/main.sh"; source "$HESTIA/func/upgrade.sh"; upgrade_complete_message 2>&1' _ "$work/h"
	[ "$status" -eq 0 ]
	[[ "$output" == *"GitHub:   https://github.com/hostisan/hostisan/"* ]]
	[[ "$output" != *"Web:"* ]]
	[[ "$output" != *"product"* ]]
}

@test "the installers stop without product.env" {
	local f
	for f in debian ubuntu; do
		rm -rf "$work/src"
		mkdir -p "$work/src/common/hostisan"
		cp "$repo/install/hst-install-$f.sh" "$work/src/"
		cp "$loader" "$work/src/common/hostisan/"
		run bash "$work/src/hst-install-$f.sh"
		echo "$output"
		[ "$status" -eq 1 ]
		[[ "$output" == *"Error: $work/src/common/hostisan/product.env not found"* ]]
		# product.sh missing too
		rm "$work/src/common/hostisan/product.sh"
		run bash "$work/src/hst-install-$f.sh"
		[ "$status" -eq 1 ]
		[[ "$output" == *"Error: $work/src/common/hostisan/product.sh not found"* ]]
	done
}

# installer_fn FILE NAME: function NAME of installer FILE.
installer_fn() {
	awk -v name="$2" '$0 ~ "^" name "\\(\\) \\{" { on = 1 } on { print } on && /^}/ { exit }' "$1"
}

@test "the installers' welcome shows the domain from product.env" {
	local f
	product_copy "$work/src/common/hostisan" example.test
	for f in "$repo"/install/hst-install-{debian,ubuntu}.sh; do
		installer_fn "$f" install_welcome_message >"$work/welcome.sh"
		[ -s "$work/welcome.sh" ]
		run env -i bash -c 'source "$1/common/hostisan/product.sh"; source "$2"; HESTIA_INSTALL_VER=1.10.5 install_welcome_message' _ "$work/src" "$work/welcome.sh"
		[ "$status" -eq 0 ]
		[[ "$output" == *"example.test"* ]]
		run ! grep -qiF "$old" <<<"$output"
	done
}

@test "bash consumers read product.sh" {
	local f
	# the installers, from the source tree they run from (before $HESTIA exists)
	for f in "$repo"/install/hst-install-{debian,ubuntu}.sh; do
		grep -qF 'source "$HOSTISAN_SRC/common/hostisan/product.sh"' "$f"
		grep -qF 'https://github.com/hostisan/hostisan' "$repo/install/hst-install.sh"
		# the Ubuntu installer's summary is in install-ui.sh (installer-ux)
		grep -qF '$HOSTISAN_URL' "$f" "$repo/install/common/hostisan/install-ui.sh"
		grep -qF '$HOSTISAN_DOCS_URL' "$f"
	done
	# the Debian installer's usage example still writes an address at the
	# domain; the Ubuntu one's example has no address (installer-ux)
	grep -qF '@$HOSTISAN_DOMAIN' "$repo/install/hst-install-debian.sh"
	# bin/ and func/, from the installed copy, through main.sh's hostisan_product
	# (which survives a missing product.sh)
	grep -qF 'source "$HESTIA/install/common/hostisan/product.sh" 2> /dev/null || true' "$repo/func/main.sh"
	for f in bin/v-backup-user bin/v-restore-user-full-restic; do
		grep -qx $'\thostisan_product' "$repo/$f"
		run ! grep -qF 'hostisan/product.sh' "$repo/$f"
	done
	grep -qx $'\thostisan_product' "$repo/func/upgrade.sh"
	run ! grep -qF 'hostisan/product.sh' "$repo/func/upgrade.sh"
	grep -qF '$HOSTISAN_DOCS_URL' "$repo/bin/v-backup-user"
	grep -qF '"$HOSTISAN_INFO_EMAIL"' "$repo/bin/v-restore-user-full-restic"
}

# ---------------------------------------------------------------- the panel binary

@test "the panel binary takes the product values from product.env" {
	command -v go >/dev/null || skip "needs go"
	product_copy "$work/p" example.test
	run make -s -C "$repo" go-build PRODUCT_ENV="$work/p/product.env" OUT="$work/hostisan"
	echo "$output"
	[ "$status" -eq 0 ]
	grep -aqF 'https://example.test' "$work/hostisan"
	grep -aqF 'security@example.test' "$work/hostisan"
	run ! grep -aqiF "$old" "$work/hostisan"
	# the default build reads the tree's product.env
	run make -s -C "$repo" -n go-build
	[[ "$output" == *"main.productURL=$HOSTISAN_URL "* ]]
	[[ "$output" == *"main.securityEmail=$HOSTISAN_SECURITY_EMAIL"* ]]
	# and so does make dev's panel
	run make -s -C "$repo" -n dev
	[[ "$output" == *"main.productURL=$HOSTISAN_URL "* ]]
}

# ---------------------------------------------------------------- the guard

@test "check-domain: the tree is clean" {
	run "$check" "$repo"
	echo "$output"
	[ "$status" -eq 0 ]
}

@test "check-domain finds the domain outside product.env and the docs" {
	mkdir -p "$work/t/install/common/hostisan" "$work/t/bin" "$work/t/docs"
	cp "$env_file" "$work/t/install/common/hostisan/product.env"
	printf '%s\n' 'echo "see https://'"$old"'/docs"' >"$work/t/bin/v-x"
	printf '%s\n' '# mail info@'"${old^^}" >"$work/t/bin/v-y"
	echo "Read https://$old" >"$work/t/README.md"
	echo "Read https://$old" >"$work/t/docs/guide.md"
	echo "not the domain: my$old and ${old}munity" >"$work/t/bin/v-z"
	run "$check" "$work/t"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"bin/v-x:1:"* ]]
	[[ "$output" == *"bin/v-y:1:"* ]]
	[ "$(grep -c "^bin/" <<<"$output")" -eq 2 ]
}

@test "check-domain skips make debs output at the top only" {
	mkdir -p "$work/t/install/common/hostisan" "$work/t/build/debs" "$work/t/bin/build"
	cp "$env_file" "$work/t/install/common/hostisan/product.env"
	echo "-X main.productURL=https://$old" >"$work/t/build/debs/build.log"
	run "$check" "$work/t"
	[ "$status" -eq 0 ]
	echo "https://$old" >"$work/t/bin/build/v-x"
	run "$check" "$work/t"
	[ "$status" -eq 1 ]
	[[ "$output" == *"bin/build/v-x:1:"* ]]
}

@test "check-domain finds the domain written as a regex" {
	mkdir -p "$work/t/install/common/hostisan" "$work/t/bin"
	cp "$env_file" "$work/t/install/common/hostisan/product.env"
	# ${domain//./\\.}: the form grep -E and sed patterns take
	printf '%s\n' "grep -E 'info@${old//./\\.}' mail.log" >"$work/t/bin/v-x"
	run "$check" "$work/t"
	echo "$output"
	[ "$status" -eq 1 ]
	[[ "$output" == *"bin/v-x:1:"* ]]
	# still whole words only
	printf '%s\n' "grep -E 'my${old//./\\.}' mail.log" >"$work/t/bin/v-x"
	run "$check" "$work/t"
	[ "$status" -eq 0 ]
}

@test "check-domain reads the domain from product.env" {
	mkdir -p "$work/t/install/common/hostisan" "$work/t/bin"
	sed "s/^HOSTISAN_DOMAIN=.*/HOSTISAN_DOMAIN=example.test/" "$env_file" >"$work/t/install/common/hostisan/product.env"
	echo "echo https://$old" >"$work/t/bin/v-x"
	run "$check" "$work/t"
	[ "$status" -eq 0 ]
	echo "echo https://example.test" >"$work/t/bin/v-x"
	run "$check" "$work/t"
	[ "$status" -eq 1 ]
	[[ "$output" == *"bin/v-x:1:"* ]]
}

@test "check-domain needs a root with product.env" {
	run "$check"
	[ "$status" -eq 2 ]
	run "$check" "$work/missing"
	[ "$status" -eq 2 ]
	mkdir -p "$work/empty"
	run "$check" "$work/empty"
	[ "$status" -eq 2 ]
}

# ---------------------------------------------------------------- the proof

@test "a new domain in a copy of the tree reaches every consumer, the old one none" {
	command -v git >/dev/null || skip "needs git"
	local t=$work/tree c out f new
	mkdir -p "$t"
	# the tracked tree, as it is in the working copy
	(cd "$repo" && git ls-files -z --cached --others --exclude-standard |
		xargs -0 -I{} sh -c '[ -f "$1" ] && printf "%s\0" "$1"' _ {}) |
		(cd "$repo" && tar --null -T - -cf -) | tar -C "$t" -xf -
	# a domain written nowhere in the tree (not even here), so the guard can
	# run on the copy
	new=proof-$RANDOM$RANDOM.test
	sed -i "s/^HOSTISAN_DOMAIN=.*/HOSTISAN_DOMAIN=$new/" "$t/install/common/hostisan/product.env"

	# the source: the guard passes, and the old domain is left only in docs
	run "$t/tools/check-domain.sh" "$t"
	echo "$output"
	[ "$status" -eq 0 ]
	run grep -rIliwF --exclude='*.md' --exclude-dir=test_helper -e "$old" "$t"
	echo "$output"
	[ -z "$output" ]

	# bash: product.sh, the upgrade banner and the installers' welcome
	run env -i bash -c 'source "$1"; echo "$HOSTISAN_URL $HOSTISAN_DOCS_URL $HOSTISAN_INFO_EMAIL $HOSTISAN_SECURITY_EMAIL"' _ "$t/install/common/hostisan/product.sh"
	[ "$output" = "https://$new https://$new/docs info@$new security@$new" ]
	mkdir -p "$work/h/func" "$work/h/install/common" "$work/h/conf"
	: >"$work/h/conf/hestia.conf"
	cp "$t/func/main.sh" "$t/func/upgrade.sh" "$t/func/syshealth.sh" "$work/h/func/"
	cp -r "$t/install/common/hostisan" "$work/h/install/common/"
	run env -i PATH="$PATH" bash -c 'HESTIA=$1; source "$HESTIA/func/main.sh"; source "$HESTIA/func/upgrade.sh"; upgrade_complete_message' _ "$work/h"
	[[ "$output" == *"https://$new/"* ]]
	for f in "$t"/install/hst-install-{debian,ubuntu}.sh; do
		installer_fn "$f" install_welcome_message >"$work/welcome.sh"
		run env -i bash -c 'source "$1/install/common/hostisan/product.sh"; source "$2"; HESTIA_INSTALL_VER=1.10.5 install_welcome_message' _ "$t" "$work/welcome.sh"
		[[ "$output" == *"$new"* ]]
		run ! grep -qiF "$old" <<<"$output"
	done

	# packages: each control file as the build writes it
	(
		eval "$(sed -n '/^hostisan_render_control() {/,/^}/p' "$t/src/hst_autocompile.sh")"
		# shellcheck source=/dev/null
		source "$t/install/common/hostisan/product.env"
		for c in "$t"/src/deb/*/control; do
			out=$work/$(basename "$(dirname "$c")").control
			cp "$c" "$out"
			hostisan_render_control "$out"
			grep -qx "Maintainer: Hostisan <info@$new>" "$out"
			grep -qx "Homepage: https://$new" "$out"
			# not "! grep": errexit ignores a negated command, so only the
			# last file's check would count
			if grep -qiF -e "$old" -e '@HOSTISAN_' "$out"; then
				echo "$out: placeholder or old domain left" >&2
				false
			fi
		done
	)

	# the panel binary (and so the UI's footer, through the bootstrap)
	if command -v go >/dev/null; then
		run make -s -C "$t" go-build OUT="$work/hostisan"
		echo "$output"
		[ "$status" -eq 0 ]
		grep -aqF "https://$new" "$work/hostisan"
		grep -aqF "security@$new" "$work/hostisan"
		run ! grep -aqiF "$old" "$work/hostisan"
	fi

	# the release workflow and keygen.sh read the file, with no literal left
	grep -qF 'source install/common/hostisan/product.env' "$t/.github/workflows/hostisan-release.yml"
	grep -qF 'user.email="$HOSTISAN_INFO_EMAIL"' "$t/.github/workflows/hostisan-release.yml"
	grep -qF 'install/common/hostisan/product.env' "$t/tools/apt-repo/keygen.sh"
}
