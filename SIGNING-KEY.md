# The Hostisan signing key

Every Hostisan server installs its updates as root. Before it does, apt checks that the update list is signed by the key the server trusts, and refuses anything else. That key proves an update really comes from you: even if the update website or the domain is taken over, servers won't install files you didn't sign.

This guide covers the key, keeping it safe, and signing releases with it. The wider release process is in [RELEASING.md](RELEASING.md).

## The key

The Hostisan release key exists:

| | |
|---|---|
| Fingerprint | `9C25AACF8D6449319AD52FE2D5DD6BC64A10C174` |
| Grouped | `9C25 AACF 8D64 4931 9AD5  2FE2 D5DD 6BC6 4A10 C174` |
| Type | ed25519, sign only |
| User ID | `Hostisan Release <release@hostisan.com>` |
| Expires | 2029-11-04 |

Its public half is in this repository:

| File | What it is |
|---|---|
| `install/common/hostisan/hostisan-archive-keyring.gpg` | The public key in the binary form servers use (`signed-by=`) |
| `install/common/hostisan/hostisan-archive-keyring.asc` | The same key, armored, for checking by hand |
| `install/common/hostisan/hostisan-archive-keyring.fpr` | The pinned fingerprint. It is written down only here |
| `install/common/hostisan/signing-key.sh` | Reads the pin: `pinned`, `fpr FILE`, `check FILE` (the pinned key alone, not expired or revoked), `expires FILE` (when the pinned key in FILE expires) |

The private key, its revocation certificate and its passphrase live on the owner's offline copy. They never go into this repository or onto a server. The revocation certificate never goes into CI either.

Whether `private.asc` and its passphrase go into CI is the owner's choice:

- **Sign offline** ([Sign a release](#sign-a-release)): the key only leaves the offline copy for the signing. Each release needs the owner and that copy at hand.
- **Sign in CI** (GitHub Pages, [RELEASING.md](RELEASING.md) steps 3 and 4): `private.asc` and the passphrase are stored as GitHub secrets, so a tag publishes on its own. The trade-off is that the key then also lives at GitHub: anyone who can change the release workflow, or read the `apt-repo` environment's secrets, can sign with it. Required reviewers on that environment narrow this; they don't remove it. If the key may have leaked from there, follow [If the key is lost or stolen](#if-the-key-is-lost-or-stolen).

The test servers use a throwaway key that lives in the project's notes. It is not protected and it expires, so it must never sign a real release. `publish.sh` refuses it unless `HOSTISAN_APT_KEY_UNPINNED=1` is set together with `HOSTISAN_APT_KEY_FPR` naming it (see [Sign a release](#sign-a-release)).

## How servers trust it

- **New installs.** `hst-install.sh --apt-repo URL` installs `hostisan-archive-keyring.gpg` from the source tree as `/usr/share/keyrings/hestia-keyring.gpg` and prints its fingerprint. A key given with `--apt-key FILE` must be the same pinned key, alone; any other key is refused, and so is the pinned key once it has expired or been revoked. The file is read once into a private copy, and that copy is what is checked and installed. `--apt-key-unpinned` lets a test repository's key through, with a loud warning. No key is ever downloaded.
- **Existing servers.** On every upgrade `setup.sh` renews the release key when it has been extended: if the server's keyring holds the pinned key and nothing else, and the keyring shipped in the new `hestia` package holds that same key with a later expiry, the shipped keyring replaces the server's in one step (a temp file renamed over it), and the upgrade log gets one line saying so. Both files are read with `signing-key.sh`. Any other keyring is never written: a test server keeps its throwaway key, and a keyring holding a second key, or a later or equal expiry, stays as it is. Then `setup.sh` reports whether the keyring holds the release key, or names the other key it holds. No panel notification is raised for another key, because `setup.sh` only runs once apt has installed an update signed by the key the server trusts, so that key is a test repository's the admin chose.
- **Releases.** `tools/apt-repo/publish.sh` signs only with the pinned key.

The fingerprint also goes on the project website (`HOSTISAN_URL` in `install/common/hostisan/product.env`) and in the release notes, so admins can check it.

## Keep it safe

Keep the owner's offline copy in two separate encrypted places, and the passphrase in your password manager.

- If you lose the key, you can't sign updates any more, and every server has to be moved to a new key by hand.
- If someone gets `private.asc` and the passphrase, they can push updates to every server. Use `revoke.asc` and a new key at once (see [If the key is lost or stolen](#if-the-key-is-lost-or-stolen)).

Check a backup opens before you rely on it:

```bash
gpg --show-keys --with-fingerprint <key-folder>/public.asc
```

The fingerprint it prints must be `9C25 AACF 8D64 4931 9AD5  2FE2 D5DD 6BC6 4A10 C174`.

## Sign a release

Each release, sign the update list with:

```bash
read -s -p "Key passphrase: " HOSTISAN_APT_KEY_PASSPHRASE; echo
export HOSTISAN_APT_KEY_PASSPHRASE
tools/apt-repo/publish.sh --key-file <key-folder>/private.asc <debs-folder> <repo-folder>
unset HOSTISAN_APT_KEY_PASSPHRASE
```

- `<key-folder>` is the owner's offline copy, mounted for the signing only.
- `<debs-folder>` holds the new `hestia*` packages; `<repo-folder>` is the published repo, kept between releases.
- `publish.sh` refuses any key but the pinned one, so the test key can't sign by mistake. `HOSTISAN_APT_KEY_FPR`, if set, must be the pinned fingerprint too. Only `HOSTISAN_APT_KEY_UNPINNED=1` turns the pin off, for test repositories, and then `HOSTISAN_APT_KEY_FPR` must be set to the test key's fingerprint, so a stray `UNPINNED=1` alone signs nothing.
- The key file must hold one secret key; a file with several is refused.
- The key is imported into a throwaway GnuPG home, never your own keyring.

Then copy `<repo-folder>` to wherever servers download updates from. If you publish through GitHub Pages instead, the release workflow signs for you: follow steps 3 and 4 of [RELEASING.md](RELEASING.md) to store `private.asc`, the passphrase and the fingerprint as GitHub secrets and variables.

Check the result before servers see it:

```bash
gpgv --keyring install/common/hostisan/hostisan-archive-keyring.gpg \
  <repo-folder>/dists/noble/Release.gpg <repo-folder>/dists/noble/Release
```

It must say "Good signature" and show the release key.

## Extend the key before it expires

Do this a few months before 2029-11-04. The key itself doesn't change, so its fingerprint and the pin stay the same.

```bash
fpr=$(install/common/hostisan/signing-key.sh pinned)
gpg --homedir <key-folder>/gnupg --quick-set-expire "$fpr" 3y
gpg --homedir <key-folder>/gnupg --armor --export-secret-keys > <key-folder>/private.asc
gpg --homedir <key-folder>/gnupg --armor --export > <key-folder>/public.asc
gpg --homedir <key-folder>/gnupg --export > <key-folder>/hostisan-archive-keyring.gpg
```

Then:

1. Refresh both backups.
2. Update the GitHub secret if you use one.
3. Copy the new `public.asc` and `hostisan-archive-keyring.gpg` (public files only) over `install/common/hostisan/hostisan-archive-keyring.asc` and `.gpg`, and change the expiry date in the table at the top of this file. The `.fpr` file stays as it is.
4. Release, before the old expiry date. New installs get the renewed key from the source tree. Existing servers pick it up on their next upgrade: `setup.sh` sees the same fingerprint with a later expiry and replaces their keyring (see [How servers trust it](#how-servers-trust-it)). Nothing else is needed. The release must be signed while the servers' old copy is still valid, or apt refuses it and the upgrade that carries the renewed key never arrives.

## If the key is lost or stolen

Servers only accept updates signed by the key they already trust, so moving them to a new key always starts from the old one.

**Stolen** (someone else may have `private.asc` and the passphrase):
1. Create a new key with `tools/apt-repo/keygen.sh <new-key-folder>` (its default uid is built from the domain in `install/common/hostisan/product.env`) (with `HOSTISAN_APT_KEY_PASSPHRASE` set), on a trusted machine, never a server or CI.
2. Publish `revoke.asc` and announce it on the project website, with the new fingerprint.
3. Put the new public key and fingerprint in `install/common/hostisan/`, then sign one last release with the old key whose only job is to install the new public key and remove the old one. Every later release uses the new key.
4. Tell admins to check the fingerprint on their servers against the one on the project website. A server could have received a fake update while the old key was in the wrong hands.

**Lost** (no copy of `private.asc` survives):
1. Create a new key as above, and put its public half and fingerprint in `install/common/hostisan/`.
2. Servers can't be moved automatically, because nothing can sign with the old key any more. Publish the new fingerprint and the exact commands admins run to install the new public key by hand.

This is why the two backups matter.
