# Releasing Hostisan

Hostisan ships as the `hostisan`, `hostisan-web-terminal`, `hostisan-nginx` and `hostisan-php` debs, built for Ubuntu 24.04 (amd64): Hostisan v1 supports Ubuntu 24.04 LTS only. Servers install them from a signed static APT repo. The build and the installers keep the code for Debian 12 and 13 and Ubuntu 22.04 and 26.04, untested; the release builds none of them for now.

## How a release works

- `.github/workflows/hostisan.yml` runs on every pull request and push: Go and UI tests, bats and shellcheck (`make tools-test`), the branding check, the core-edits check (full history), `core-edits.bats` in Docker, and a package build with `package.bats` run on the debs.
- `.github/workflows/hostisan-release.yml` runs on a `v*` tag. It builds all four debs in an `ubuntu:24.04` container (the `build` matrix lists only Ubuntu 24.04; the other systems are commented out there, one line each, ready for when they are supported) and attaches them to a GitHub release. When publishing is turned on, it also adds them to the APT repo on the `gh-pages` branch, signs it, and checks it with `apt-get` on Ubuntu 24.04.
- Every package is versioned `1:<upstream version>-<build>+<distro>`, for example `1:1.10.5-201+ubuntu24.04`. The epoch `1:` keeps any upstream Hestia release from replacing a Hostisan package. The build number is 200 plus the workflow run number, so every release gets new file names.
- `tools/apt-repo/publish.sh` builds the repo. It refuses debs without the epoch, a `hostisan` without a 2.x `hostisan-web-terminal` for the same distro, and a file name that is already published with other contents. It also refuses any key but the Hostisan release key, whose fingerprint is pinned in `install/common/hostisan/hostisan-archive-keyring.fpr`. `HOSTISAN_APT_KEY_UNPINNED=1` turns that off for test repositories only, and only together with `HOSTISAN_APT_KEY_FPR` set to the test key's fingerprint. The release workflow clears it in the publish step.

Repo layout: `dists/<codename>/{InRelease,Release,Release.gpg}`, `dists/<codename>/main/binary-amd64/Packages{,.gz}`, `pool/main/<codename>/*.deb`, and the public key as `hostisan-archive-keyring.gpg` (binary) and `.asc`. The codename is `noble` (Ubuntu 24.04); `publish.sh` also knows `bookworm`, `trixie`, `jammy` and `resolute` for when those builds come back.

## One-time setup (owner)

1. **The signing key exists.** It is the Hostisan release key:

   - fingerprint `9C25AACF8D6449319AD52FE2D5DD6BC64A10C174` (`9C25 AACF 8D64 4931 9AD5  2FE2 D5DD 6BC6 4A10 C174`);
   - ed25519, uid `Hostisan Release <release@hostisan.com>`, expires 2029-11-04.

   Its public half is in the tree: `install/common/hostisan/hostisan-archive-keyring.gpg` (binary, as servers use it) and `hostisan-archive-keyring.asc` (armored). The fingerprint is written down once, in `install/common/hostisan/hostisan-archive-keyring.fpr`; the installers, `setup.sh` and `publish.sh` read it through `install/common/hostisan/signing-key.sh`. The private key exists only on the owner's offline copy, with a passphrase, and never enters this repository. [SIGNING-KEY.md](SIGNING-KEY.md) covers keeping it, renewing it and replacing it. `tools/apt-repo/keygen.sh` stays for making throwaway test keys.

2. **Create the GitHub repository** and push the code (`main`, then tags). Check that `94c1e760f` is in the pushed history, because the core-edits check needs it.

3. **Add the secrets** (Settings → Secrets and variables → Actions → Secrets):
   - `HOSTISAN_APT_KEY`: the whole contents of `private.asc` from the owner's offline copy.
   - `HOSTISAN_APT_KEY_PASSPHRASE`: its passphrase.

   For extra safety, put the secrets in an environment named `apt-repo` with required reviewers. The publish job uses that environment.

4. **Add the variables** (same page → Variables):
   - `HOSTISAN_APT_KEY_FPR`: `9C25AACF8D6449319AD52FE2D5DD6BC64A10C174`. The publish step stops when it is unset, and `publish.sh` refuses any other value, and any other key even when the variable is unset. The `check-repo` jobs don't use it: they check the published keyring with `install/common/hostisan/signing-key.sh check` against the pin in the tree, which refuses any other key, a second key beside it, and an expired or revoked key.
   - `HOSTISAN_APT_URL`: the URL servers use, without a trailing slash, such as `https://<owner>.github.io/<repo>` or `https://apt.<domain>` (the domain in `install/common/hostisan/product.env`).
   - `HOSTISAN_APT_CNAME`: only for a custom domain, such as `apt.<domain>`.
   - `HOSTISAN_APT_PUBLISH`: `true`. Until it is set, tags only build the debs and attach them to the GitHub release.

5. **Allow the workflow to push `gh-pages`**: Settings → Actions → General → Workflow permissions → "Read and write permissions". Branch protection must not block the `gh-pages` branch.

6. **Push the first tag** (for example `git tag v1.0.0-rc1 && git push origin v1.0.0-rc1`), then **enable Pages**: Settings → Pages → Deploy from a branch → `gh-pages` / root. For a custom domain, add the DNS CNAME record and turn on "Enforce HTTPS". The release's `check-repo` jobs retry for about 10 minutes while Pages comes up. Re-run them if the first deploy took longer.

7. **Publish the fingerprint** (`9C25 AACF 8D64 4931 9AD5  2FE2 D5DD 6BC6 4A10 C174`) wherever users check it (website, README, release notes). To print it from the tree:

   ```bash
   gpg --show-keys --with-fingerprint install/common/hostisan/hostisan-archive-keyring.asc
   ```

8. **Install servers from the repo.** The installers trust the release key from the tree, so only the address is needed:

   ```bash
   bash install/hst-install.sh --apt-repo "$HOSTISAN_APT_URL"
   ```

   `hostisan_apt_setup` in `install/hst-install-{debian,ubuntu}.sh` then:

   - removes any upstream HestiaCP source (`hestiacp.com` lines and files);
   - prints the fingerprint of the key and installs it as `/usr/share/keyrings/hestia-keyring.gpg`. The key is `install/common/hostisan/hostisan-archive-keyring.gpg`, or the `--apt-key FILE` given (dearmored if needed), which must hold the pinned release key and no other. Nothing is downloaded on first use;
   - writes `/etc/apt/sources.list.d/hestia.list`:

     ```
     deb [arch=amd64 signed-by=/usr/share/keyrings/hestia-keyring.gpg] <HOSTISAN_APT_URL> <codename> main
     ```

     The file has to be `hestia.list`: `v-update-sys-hestia` updates from that source only, so a repository listed under any other name never reaches the server's updates.

   Compare the fingerprint it prints with the one above. With `--with-debs DIR` alone the installer uses the local debs and adds no source.

   A test repository signed by a throwaway key needs `--apt-key FILE --apt-key-unpinned`. The installer then prints a loud warning with both fingerprints and trusts that key alone, so the server refuses updates signed by the release key. `test/hostisan/docker/install-run.sh IMAGE --apt-repo` does this.

   On every upgrade `setup.sh` reports which key `/usr/share/keyrings/hestia-keyring.gpg` holds: the release key, or which other one. It never writes the keyring, so a test server keeps its throwaway key and keeps updating from its test repository. To move such a server to the published repository, copy `install/common/hostisan/hostisan-archive-keyring.gpg` over that keyring and point `/etc/apt/sources.list.d/hestia.list` at `$HOSTISAN_APT_URL` by hand.

   **Still to do once the hosting is chosen (TODO(O7) in the installers):** make the published repository the default, so `hst-install.sh` works without `--apt-repo`. The key is still never fetched over the network. [SIGNING-KEY.md](SIGNING-KEY.md#how-servers-trust-it) covers this from the key's side.

## Each release

1. Make sure CI is green on `main`.
2. Tag and push: `git tag vX.Y.Z && git push origin vX.Y.Z`. A tag with a `-` (such as `v1.0.0-rc1`) is marked as a pre-release on GitHub.
3. Watch the "Hostisan release" workflow. Build, GitHub release, publish and check-repo must all pass.

## Changing the domain

To change the domain, edit `HOSTISAN_DOMAIN` in `install/common/hostisan/product.env` and rebuild. The site, docs and contact addresses in that file are built on it, so one line changes them all.

Everything else reads that file:

- the installers, through `install/common/hostisan/product.sh`, and `bin/` and `func/`, through `hostisan_product` in `func/main.sh` (the hestia deb ships both product files under `/usr/local/hestia/install/common/hostisan/`). An installer stops when either file is missing. On a server without them, `hostisan_product` leaves the links out and uses `root@localhost.localdomain` as the contact;
- the deb control files, whose `@HOSTISAN_URL@` and `@HOSTISAN_INFO_EMAIL@` placeholders `src/hst_autocompile.sh` fills in;
- the panel binary (`make go-build` passes the site and security address with `-ldflags -X`), which hands them to the UI's footer through `/api/v1/bootstrap`;
- the release workflow and `tools/apt-repo/keygen.sh`.

`make tools-test` runs `tools/check-domain.sh .`, which fails when the domain is written anywhere else, and `test/hostisan/product.bats` builds a copy of the tree with another domain to check that it reaches every one of these.

Three things this does not change:

- **Docs.** Markdown files (this one, README.md, SIGNING-KEY.md, CHANGELOG.md and the rest) are text: update them by hand.
- **Servers' update address.** A server keeps the repository URL it was installed with in `/etc/apt/sources.list.d/hestia.list`; a new build does not rewrite it. If the repository moves to the new domain (`HOSTISAN_APT_URL`), point each server's `hestia.list` at it by hand.
- **The release key.** Its uid (`Hostisan Release <release@…>`) was fixed when the key was made. It goes on working under a new domain; a new key would only be needed for a new name.

## Notes

- **First update into this build from rc6 or older:** the update runs the `v-update-sys-hestia-all` already installed, not the new one. Up to rc5 it cannot move to a unit of its own. rc6's can, but not from a `sudo -i` or `su -` shell, and rc6 ships no needrestart drop-in, so on Ubuntu needrestart could restart the panel right after dpkg. So run that first update over SSH (or let the nightly cron do it). From the panel's web terminal, the old loop can still be hung up part-way when the panel restarts; if that happens, run `v-update-sys-hestia-all` a second time from the terminal. Later updates move to a unit of their own. The nginx and php debs defer their hestia restart only from the build that carries round 5 on, so rebuild them rather than republishing the -101 ones.
- **Restarts and the web terminal during an update:** from the build after v1.0.0 (rc7) on, an update (`v-update-sys-hestia-all` or `v-update-sys-hestia`, from the nightly cron, SSH or the web terminal) restarts hostisan, hostisan and hostisan-web-terminal once each, after the last package and once dpkg is idle, not once per package script (rc7 restarted hestia three times and hostisan once, mid-loop). Started in the web terminal, the update prints `… finished, exit status N` and the prompt comes back; a few seconds later those restarts close the terminal once. Press Reconnect: signed-in sessions are kept in `/var/lib/hostisan/sessions.json`, so neither the panel nor the terminal asks for a new login (the new shell is a fresh one; the old one is gone). A raw `apt upgrade` over SSH still restarts in line, as upstream; typed in the web terminal it is deferred until dpkg is done, as before. On a non-default panel port the hestia-nginx postinst no longer restarts hestia in line through `v-change-sys-port`. The first update into this build still runs the installed scripts: from rc7 over SSH or cron the restarts are in line as before (rc7's `v-update-sys-hestia` does not mark the update), while from the web terminal they already wait for the end. The panel that stops then is the old one, which keeps sessions in memory, so everyone signs in once more after that first update.
- **Signing people out:** since sessions survive a panel restart, `systemctl restart hostisan` no longer signs anyone out, and deleting `sessions.json` before a restart doesn't either (the panel writes the file again as it stops). To end sessions, run `v-delete-sys-hostisan-sessions all` as root (everyone) or `v-delete-sys-hostisan-sessions USER` (one user, also a deleted one). With no argument it only prints its usage: from the build after rc10 on, signing everyone out takes the explicit `all`. It stops the panel, edits the file through `hostisan sessions forget` and starts the panel again. Sessions of a deleted, suspended or demoted user end at their next request, and the panel drops a deleted user's saved sessions within about 10 minutes even if their browser never comes back, and so do the sessions of a user whose password changed through Hestia (the panel, `v-change-user-password` over SSH or the legacy API), within 15 seconds. A password set with `passwd` or `chpasswd` alone bypasses Hestia's record of it and is not seen: run `v-delete-sys-hostisan-sessions USER` after it. One user keeps at most 20 sessions; a login past that ends the least recently used one.
- **Upgrade mail:** with `UPGRADE_SEND_EMAIL='true'` (the installer's default) every hestia upgrade queues a notice to the admin address. On a test box that must not send mail, set it to `false` in `hestia.conf` first.
- **Your own server instead of Pages:** on any machine with `apt-utils` and `gnupg`, download the release debs and run `tools/apt-repo/publish.sh --key-file private.asc <debs> <repo-dir>`, keeping `<repo-dir>` between releases. Serve it over HTTPS with any static web server (rsync it there).
- **Size:** the `gh-pages` branch is force-pushed as one commit, so git history doesn't grow, but the pool keeps every release. GitHub Pages sites are limited to 1 GB. Delete old `pool/main/*/` debs from `gh-pages` before a release when it gets close; the next publish rebuilds the indexes.
- **Key expiry:** renewing the key means extending its expiry, exporting the public files into the repo, and releasing. Before the key expires, run `gpg --homedir <dir>/gnupg --quick-set-expire <fingerprint> 3y`, re-export `private.asc` (update the secret if you sign in CI) and the public files, and copy the public `hostisan-archive-keyring.gpg` and `.asc` into `install/common/hostisan/` (the fingerprint, and so the `.fpr` file, stays the same). Then release before the 2029-11-04 expiry. New installs and the published `hostisan-archive-keyring.gpg` get the renewed key, and existing servers pick it up automatically on that upgrade: `setup.sh` replaces a keyring that holds only the pinned key when the shipped one has a later expiry (see [SIGNING-KEY.md](SIGNING-KEY.md#extend-the-key-before-it-expires)).
- **arm64:** the `build-arm64` job is off (`if: false`) until it is approved (O7). Turning it on also needs `binary-arm64` in `ARCHES` in `publish.sh`.
- Local test of the tools: `test/test_helper/bats-core/bin/bats test/hostisan/apt-repo.bats` uses a throwaway key. Its `docker:` test runs `apt-get update` against the repo in `debian:trixie`.
