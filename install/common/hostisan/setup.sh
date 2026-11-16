#!/usr/bin/env bash
# Usage: setup.sh [--upgrade]
#
# Turns the Hostisan panel on. The hestia package's postinst runs it with
# --upgrade on every upgrade (before the upgrade restarts Hestia's services);
# the installer runs it without, once the hestiaweb user exists. Idempotent.
#
#   1. Writes /usr/local/hestia/conf/hostisan.env from the template next to
#      this script, only when it is missing: an existing file (an admin who
#      turned HOSTISAN_FILE_RESTORE off) is never overwritten.
#   2. Removes the old Node web terminal (remove-node-terminal.sh), and
#      installs /etc/sudoers.d/hestiaweb (sudoers.sh), so a box upgraded from
#      upstream Hestia gets `!log_allowed` too (O6).
#   3. Hides /proc (hidepid.sh): adds hestiaweb to the hostisan-proc group.
#   4. Reloads systemd, enables and (re)starts hostisan, then restarts hestia
#      (nginx and php-fpm) and hestia-web-terminal if they run, through
#      restart-unit.sh (deferred until dpkg is done when this runs in the web
#      terminal, and until the update is done inside one, so each restarts
#      once per update). Every process running as hestiaweb starts after
#      step 3, so it has the new group.
#   5. Installs /etc/needrestart/conf.d/hostisan.conf (needrestart.conf next
#      to this script) when it is missing or differs, so needrestart, which
#      runs right after dpkg on Ubuntu, leaves the deferred restarts of step 4
#      to restart-unit.sh (rc6 P1).
#   6. With --upgrade, when phpMyAdmin SSO is on (PHPMYADMIN_KEY set) and out
#      of date: its hestia-sso.php differs from this build's (the key lines
#      aside), its key is not hestia.conf's PHPMYADMIN_KEY, its host name or
#      port are not `hostname -f` and BACKEND_PORT, or its phpmyadmin-sso
#      access key is gone. Re-installs it, so phpMyAdmin gets the HMAC
#      hestia-sso.php and the SSO gets a new key and access key (Plan 6 B2f,
#      SSO4). An up-to-date one is left alone, keys and all; so is one that
#      could not be added back (template, phpMyAdmin or its config.inc.php
#      missing).
#   7. Renews the release key: when the box's Hostisan APT keyring
#      (/usr/share/keyrings/hestia-keyring.gpg, written by the installer)
#      holds the pinned Hostisan release key and no other, and the keyring
#      shipped next to this script holds that same key with a later expiry
#      (both read with signing-key.sh), the shipped keyring replaces it
#      atomically and one line says so: it is copied once into a temp file
#      next to the box's keyring, that copy is checked and renamed over it,
#      and an EXIT trap removes it when the run stops early.
#      An expiry that is neither a number nor "never" leaves the keyring
#      alone, with one warning.
#      A box with a keyring but no gpg (the hestia deb depends on gnupg)
#      gets a warning: the key is neither checked nor renewed.
#      So a key whose expiry the owner extended reaches every server with the
#      next release (SIGNING-KEY.md). Any other keyring is never written: a
#      test box's throwaway key (--apt-key-unpinned) is never replaced.
#      Then reports which key the box's APT source trusts: the release key,
#      or another one, which it names, or says the release key has expired
#      or been revoked. Another key
#      is not a failure and raises no panel notification: setup.sh only runs
#      once apt has installed this hestia, so that key signed it (a test
#      repository the admin chose) or the deb was installed by hand; a box
#      whose key does not sign its repository gets no upgrade, so never
#      reaches this step, and apt reports that itself.
#
# A failed step is reported and the others still run; the exit status is 1
# when any step failed.
set -uo pipefail

hestia=/usr/local/hestia
here=$hestia/install/common/hostisan
envfile=$hestia/conf/hostisan.env

upgrade=no
if [ "${1:-}" = "--upgrade" ]; then
	upgrade=yes
fi

if [ "$(id -u)" != 0 ]; then
	echo "setup.sh: must run as root" >&2
	exit 1
fi

failed=0
warn() {
	echo "[ ! ] Hostisan: $*" >&2
	failed=1
}

# 1. hostisan.env
if [ ! -e "$envfile" ] && [ ! -L "$envfile" ]; then
	if install -m 0640 "$here/hostisan.env" "$envfile"; then
		echo "[ * ] Hostisan: wrote $envfile"
	else
		warn "could not write $envfile"
	fi
fi

# 2. the Node terminal, and hestiaweb's sudoers file (before hostisan starts)
"$here/remove-node-terminal.sh" || warn "could not remove the Node web terminal"
"$here/sudoers.sh" || warn "sudoers.sh failed: /etc/sudoers.d/hestiaweb is not updated"

# 3-4. /proc and the services
if ! getent passwd hestiaweb > /dev/null; then
	warn "no hestiaweb user: the panel is not started"
else
	"$here/hidepid.sh" || warn "hidepid.sh failed: /proc is not hidden"
	systemctl daemon-reload || warn "systemctl daemon-reload failed"
	systemctl enable hostisan.service > /dev/null 2>&1 || warn "could not enable hostisan"
	# Run from the web terminal, restart-unit.sh defers these restarts until
	# dpkg is done: the terminal's sessions go through hostisan and
	# hestia-nginx, so an in-line restart hung up the shell running the
	# upgrade. Inside an update it defers them to the update's end (rc7).
	# The old panel keeps serving until then.
	"$here/restart-unit.sh" hostisan.service restart || warn "hostisan did not start (journalctl -u hostisan)"
	"$here/restart-unit.sh" hestia.service try-restart || warn "could not restart hestia"
	# the terminal package is optional (--webterminal no)
	if systemctl cat hestia-web-terminal.service > /dev/null 2>&1; then
		"$here/restart-terminal.sh" try-restart || warn "could not restart hestia-web-terminal"
	fi
fi

# 5. needrestart: written even when needrestart is not installed yet, so it
# applies once it is. The postinst runs before apt's Post-Invoke hook, so an
# upgrade that brings this file is already covered.
nrconf=/etc/needrestart/conf.d/hostisan.conf
if ! cmp -s "$here/needrestart.conf" "$nrconf"; then
	install -D -m 0644 "$here/needrestart.conf" "$nrconf" ||
		warn "could not write $nrconf: needrestart may restart the panel in line"
fi

# 6. phpMyAdmin SSO: re-installed only when it is out of date (sso_outdated),
# so an upgrade that finds it current keeps the keys. Re-installing deletes it
# first; that is not started when the add could not work, so SSO is never left
# off by a step that was bound to fail.
pma_install=/usr/share/phpmyadmin
pma_config=/etc/phpmyadmin
sso_template=$hestia/install/deb/phpmyadmin/hestia-sso.php
sso_file=$pma_install/hestia-sso.php
# sso_body FILE: FILE without the key lines v-add-sys-pma-sso fills in.
sso_body() {
	grep -Ev '^define\("(PHPMYADMIN_KEY|API_HOST_NAME|API_HESTIA_PORT|API_KEY)", ' "$1"
}
# sso_value NAME: what the installed hestia-sso.php defines NAME as.
sso_value() {
	sed -n "s/^define(\"$1\", \"\(.*\)\");\$/\1/p" "$sso_file" | head -n1
}
# conf_value NAME: NAME's value in hestia.conf.
conf_value() {
	sed -n "s/^$1='\(.*\)'\$/\1/p" "$hestia/conf/hestia.conf" | tail -n1
}
# sso_outdated: prints why the SSO needs installing again, nothing when it is
# current: hestia-sso.php is this build's (key lines aside), its key is
# hestia.conf's PHPMYADMIN_KEY, it points at this host and the panel's port,
# and its access key (v-add-access-key ... 'phpmyadmin-sso') still exists.
sso_outdated() {
	local api_key id
	if [ ! -f "$sso_file" ] || [ ! -f "$sso_template" ] ||
		! cmp -s <(sso_body "$sso_template") <(sso_body "$sso_file"); then
		echo "hestia-sso.php is not this build's"
	elif [ "$(sso_value PHPMYADMIN_KEY)" != "$(conf_value PHPMYADMIN_KEY)" ]; then
		echo "its key is not hestia.conf's PHPMYADMIN_KEY"
	elif [ "$(sso_value API_HOST_NAME)" != "$(hostname -f 2> /dev/null)" ]; then
		echo "the host name changed"
	elif [ "$(sso_value API_HESTIA_PORT)" != "$(conf_value BACKEND_PORT)" ]; then
		echo "the panel port changed"
	else
		api_key=$(sso_value API_KEY)
		id=${api_key%%:*}
		if [[ ! "$api_key" =~ ^[A-Za-z0-9]+:. ]] || [ ! -f "$hestia/data/access-keys/$id" ] ||
			! grep -qx "PERMISSIONS='phpmyadmin-sso'" "$hestia/data/access-keys/$id" ||
			! grep -qxF "SECRET_ACCESS_KEY='${api_key#*:}'" "$hestia/data/access-keys/$id"; then
			echo "its phpmyadmin-sso access key is missing"
		fi
	fi
}
if [ "$upgrade" = yes ] && grep -Eq "^PHPMYADMIN_KEY='[^']+'" "$hestia/conf/hestia.conf" 2> /dev/null; then
	sso_stale=$(sso_outdated)
	if [ -z "$sso_stale" ]; then
		: # up to date
	elif [ ! -f "$sso_template" ] || [ ! -d "$pma_install" ] || [ ! -f "$pma_config/config.inc.php" ]; then
		warn "phpMyAdmin SSO is out of date ($sso_stale) but could not be installed again (missing $sso_template, $pma_install or $pma_config/config.inc.php): left as it is"
	else
		echo "[ * ] Hostisan: refreshing phpMyAdmin SSO ($sso_stale)"
		if "$hestia/bin/v-delete-sys-pma-sso" quiet; then
			"$hestia/bin/v-add-sys-pma-sso" quiet || warn "phpMyAdmin SSO could not be installed again (v-add-sys-pma-sso): it is off; turn it on with v-add-sys-pma-sso"
		else
			warn "phpMyAdmin SSO could not be removed (v-delete-sys-pma-sso)"
		fi
	fi
fi

# 7. The APT key: renewed when this build ships the same key with a later
# expiry (key_renewal), then reported.
keyring=/usr/share/keyrings/hestia-keyring.gpg
shipped=$here/hostisan-archive-keyring.gpg
keyring_tmp=
# is_expiry X: X is what signing-key.sh expires prints: seconds or "never".
is_expiry() {
	[ "$1" = never ] || [[ $1 =~ ^[0-9]+$ ]]
}
# later A B: expiry A is after expiry B (both checked with is_expiry).
later() {
	if [ "$1" = never ]; then
		[ "$2" != never ]
	else
		[ "$2" != never ] && [ "$1" -gt "$2" ]
	fi
}
# key_renewal: replaces the box's keyring with the shipped one when the box's
# is a plain file holding the pinned key and no other, not revoked, and the
# shipped one passes signing-key.sh check (the pinned key and no other, not
# expired or revoked) and expires later. The shipped keyring is copied once,
# into a temp file next to the box's (removed on exit, even when killed):
# that copy is the one checked and the one renamed over it, so apt never reads
# half a keyring nor one that was not checked. Says so in one line; says
# nothing when there is nothing to renew.
key_renewal() {
	local old new until
	[ -f "$keyring" ] && [ ! -L "$keyring" ] && [ -f "$shipped" ] || return 0
	! cmp -s "$shipped" "$keyring" || return 0
	old=$("$here/signing-key.sh" expires "$keyring" 2> /dev/null) || return 0
	if ! is_expiry "$old"; then
		warn "release key expiry '$old' in $keyring is not a date: $keyring is left as it is"
		return 0
	fi
	if ! keyring_tmp=$(mktemp "$(dirname "$keyring")/.hestia-keyring.XXXXXX"); then
		keyring_tmp=
		warn "could not renew the Hostisan release key in $keyring (no temp file)"
		return 0
	fi
	trap 'rm -f "$keyring_tmp"' EXIT
	if ! cp "$shipped" "$keyring_tmp"; then
		warn "could not renew the Hostisan release key in $keyring (could not copy $shipped)"
		return 0
	fi
	"$here/signing-key.sh" check "$keyring_tmp" > /dev/null 2>&1 || return 0
	new=$("$here/signing-key.sh" expires "$keyring_tmp" 2> /dev/null) || return 0
	if ! is_expiry "$new"; then
		warn "release key expiry '$new' in $shipped is not a date: $keyring is left as it is"
		return 0
	fi
	later "$new" "$old" || return 0
	if chmod 0644 "$keyring_tmp" && mv -f "$keyring_tmp" "$keyring"; then
		keyring_tmp=
		[ "$new" = never ] && until="does not expire" || until="expires $(date -u -d "@$new" +%F)"
		echo "[ * ] Hostisan: APT key $("$here/signing-key.sh" pinned) renewed in $keyring (now $until)"
	else
		warn "could not renew the Hostisan release key in $keyring"
	fi
}
if [ -f "$keyring" ] && ! command -v gpg > /dev/null; then
	warn "no gpg (package gnupg): the Hostisan release key in $keyring cannot be checked or updated"
elif [ -f "$keyring" ]; then
	key_renewal
	if [ -n "$keyring_tmp" ]; then
		rm -f "$keyring_tmp"
		keyring_tmp=
	fi
	pinned=$("$here/signing-key.sh" pinned 2> /dev/null) || pinned=
	if [ -n "$pinned" ] && "$here/signing-key.sh" check "$keyring" > /dev/null 2>&1; then
		echo "[ * ] Hostisan: APT key $pinned (the Hostisan release key)"
	elif [ -n "$pinned" ]; then
		trusted=$("$here/signing-key.sh" fpr "$keyring" 2> /dev/null | paste -sd' ') || trusted=
		if [ "$trusted" = "$pinned" ]; then
			# the release key, expired or revoked: say so, still read only
			why=$("$here/signing-key.sh" check "$keyring" 2> /dev/null) || true
			echo "[ ! ] Hostisan: APT key: ${why:-$keyring holds the Hostisan release key $pinned, which cannot be used}; left as it is (see SIGNING-KEY.md)"
		else
			echo "[ * ] Hostisan: APT key ${trusted:-(none found)} in $keyring is not the Hostisan release key $pinned; left as it is (a test repository's key)"
		fi
	fi
fi

exit "$failed"
