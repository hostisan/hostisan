# Hostisan

Hostisan is a web hosting control panel for Ubuntu servers. It is a fork of [Hestia Control Panel](https://www.hestiacp.com/) 1.10.5: the same command line core (the `v-*` scripts), with a new panel written in Go and a React interface.

From one place you manage web domains, mail accounts, DNS zones, databases, backups and the firewall, from the panel or the command line.

## Features and Services

- Apache2 and NGINX with PHP-FPM
- Multiple PHP versions (5.6 - 8.5, 8.5 as default)
- DNS Server (Bind) with clustering capabilities
- POP/IMAP/SMTP mail services with Anti-Virus, Anti-Spam, and Webmail (ClamAV, SpamAssassin, Sieve, Roundcube)
- MariaDB/MySQL and/or PostgreSQL databases
- Let's Encrypt SSL support with wildcard certificates
- Firewall with brute-force attack detection and IP lists (iptables, fail2ban, and ipset).
- A web terminal, and one-click installs of WordPress, Laravel, Nextcloud and other apps

## Supported operating systems

Hostisan v1 supports **Ubuntu 24.04 LTS** only. The installer refuses any other system.

The code Hostisan inherited for Debian 12 and 13 and Ubuntu 22.04 and 26.04 LTS is still in the tree, but it is not tested. To try it anyway, on a test server, add `--force` to the installer; it says the system is untested and goes on. `--force` also turns the installer's refusals into warnings (too little disk, a busy port, other panels' packages already installed, which it then leaves in place) and skips the network configuration check, so never use it on a server you care about. It never excuses a bad option such as an invalid hostname.

Give `-f` or `--force` as its own argument: the installer does not see it inside a group of short options such as `-yf`.

Only 64-bit (amd64) servers are supported. On a VPS, use one based on KVM or LXC: OpenVZ 7 and older can have trouble with DNS and the firewall.

## Installing Hostisan

Install Hostisan on a freshly installed operating system, logged in as root. It assumes you know the basics of running a Linux server.

There is no public Hostisan package repository yet, so you install from a copy of this source tree. The installer downloads nothing from Hostisan or HestiaCP: it takes the Hostisan packages either from a folder of debs or from an APT repository you point it at.

### Step 1: Build the packages

On a machine with Docker (your workstation, not the server), build the four debs from a checkout:

```bash
cd hostisan    # your checkout of the Hostisan source
make debs
```

`make debs` builds `hestia`, `hestia-nginx`, `hestia-php` and `hostisan-web-terminal` for Ubuntu 24.04 in a clean `ubuntu:24.04` container, from a `git archive` of `HEAD`: uncommitted changes are not built. It takes about 20 minutes and writes to `build/debs/` (gitignored):

- the four debs;
- `hostisan-install.tar.gz`, the `install/` folder the server needs;
- `VERSION`, the build stamp the installer shows, `SHA256SUMS` and `build.log`.

It fails, and replaces nothing in `build/debs/`, when any of the four debs is missing or a compile failed (the build script itself can end with exit 0 after a failed compile, so each deb is checked for its compiled binary, and `hestia` for the built web UI).

Options, all optional:

| Variable | Default | Meaning |
|---|---|---|
| `HOSTISAN_BUILD` | `105` | The build number in the deb version (`1:1.10.5-105+ubuntu24.04`), 105 or more. Use a higher number for every rebuild meant for a server that already has Hostisan, or `dpkg` treats the new debs as the same version. |
| `HOSTISAN_VERSION` | `git describe` | What `hostisan version` and the installer banner show, such as `v1.0.0`. |
| `REF` | `HEAD` | The commit or tag to build. |
| `DEBS_OUT` | `build/debs` | The output folder. |
| `DOCKER` | `docker` | The Docker command, with any arguments, such as `sudo docker`. |

For example: `make debs HOSTISAN_BUILD=120 HOSTISAN_VERSION=v1.0.1`.

Without Docker, build on an Ubuntu 24.04 machine with `bash src/hst_autocompile.sh --all --noinstall --keepbuild '~localsrc'`, which writes the debs to `/tmp/hestiacp-src/deb`. It installs a compiler, Go and Node.js, so don't run it on the server, and check that all four debs are there afterwards.

### Step 2: Run the installer

Copy the debs and the install tarball to the server, then run the installer as root:

```bash
scp build/debs/*.deb build/debs/hostisan-install.tar.gz root@SERVER:/root/
ssh root@SERVER
mkdir -p /root/hostisan-debs && mv /root/*.deb /root/hostisan-debs/
tar -xzf /root/hostisan-install.tar.gz -C /root
bash /root/install/hst-install.sh --with-debs /root/hostisan-debs --hostname panel.example.com --email you@example.com
```

If the debs are in a signed Hostisan APT repository instead, give its address:

```bash
bash install/hst-install.sh --apt-repo https://apt.example.com
```

The repository must be signed by the Hostisan release key, which ships in the source tree (`install/common/hostisan/hostisan-archive-keyring.gpg`). The installer prints its fingerprint before it trusts it: `9C25 AACF 8D64 4931 9AD5  2FE2 D5DD 6BC6 4A10 C174`. A test repository signed by another key needs `--apt-key FILE --apt-key-unpinned`, and the installer warns loudly about it.

Before it installs or changes anything, the installer checks the server and the options and lists every problem at once:

- **Refused** (the install stops): a hostname that is not a fully qualified name of three or more parts (`panel.example.com`, not `example.com`, which the panel would take from your websites and mail), an invalid email, an existing user of the admin's name, a `--password` with spaces or one starting with `/tmp/`, under 5 GB of free disk, a port it needs (80, 443, 25 with mail, the panel port) already in use, or other web, mail or database servers already installed when `--interactive no` is set.
- **Warned about**: under 2 GB of RAM (ClamAV is off by default under 3 GB anyway), under 20 GB of free disk, a hostname that does not point at this server yet, and `--password` on the command line, which other users can see in `ps`.

`ufw` is on every stock Ubuntu image, and Hostisan manages its own iptables firewall: the installer removes ufw without asking and says so in one line, just before it sets up that firewall near the end, so ufw keeps guarding the server until then. With `--iptables no` ufw is not touched.

Leave out `--password`: the installer then makes a strong password, shows it once at the end and keeps it in `/root/hostisan-admin-password.txt` (readable by root only; delete it once the password is stored elsewhere). The installer hands the password to `v-add-user` in a root-only file under `/tmp`, deleted straight after, never on a command line. With `--interactive no` the admin is called `admin` unless you give `--username`, and `--hostname` and `--email` are needed.

The package phase takes 10 to 15 minutes and prints a progress line every minute. The full log is `/root/hst_install_backups/hst_install-<date>.log`.

At the end the installer prints the panel address (and the IP address to use until DNS is ready), the admin login, the log path and the next steps: the DNS record, `v-add-letsencrypt-host`, the panel port in your firewall, and how updates arrive. It mails the same summary, without the password, to `--email`. Then reboot.

### Custom installation

Flags choose what gets installed, for example `--apache no` or `--webterminal yes`. To see them all:

```bash
bash install/hst-install.sh -h
```

## Upgrading

On a server installed with `--apt-repo`, updates come from that repository. Automatic updates are on by default and can be changed under **Server Settings > Updates**. To update by hand:

```bash
v-update-sys-hestia-all
```

A server installed with `--with-debs` alone has no package source. Build new debs as in step 1, with a higher `HOSTISAN_BUILD` than the installed one (`make debs HOSTISAN_BUILD=120`), copy them to the server and install them together:

```bash
dpkg -i hostisan_*.deb hostisan-nginx_*.deb hostisan-php_*.deb hostisan-web-terminal_*.deb
```

Leave out `hostisan-web-terminal` if the web terminal is not installed.

## Issues

There is no public issue tracker yet: report bugs by email to the info address of the product domain (`HOSTISAN_INFO_EMAIL` in `install/common/hostisan/product.env`). Say what you did, what happened, and what you have already tried. Problems with third-party applications (such as WordPress itself) are out of scope.

## Contributions

[RELEASING.md](RELEASING.md) explains how releases are built and signed, and [hostisan/CORE-EDITS.md](hostisan/CORE-EDITS.md) lists every change made to the Hestia core.

## Credits

Hostisan is built on [Hestia Control Panel](https://www.hestiacp.com/) by the HestiaCP team, which is itself based on the [VestaCP](https://vestacp.com/) project. The command line core and the server templates are their work.

To support the upstream Hestia project, see [hestiacp.com](https://www.hestiacp.com/) or donate:

[![paypal](https://www.paypalobjects.com/en_US/i/btn/btn_donateCC_LG.gif)](https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=ST87LQH2CHGLA)<br /><br />
Bitcoin : bc1q48jt5wg5jaj8g9zy7c3j03cv57j2m2u5anlutu<br>
Ethereum : 0xfF3Dd2c889bd0Ff73d8085B84A314FC7c88e5D51<br>
Binance: bnb1l4ywvw5ejfmsgjdcx8jn5lxj7zsun8ktfu7rh8<br>
Smart Chain: 0xfF3Dd2c889bd0Ff73d8085B84A314FC7c88e5D51<br>

Hestia's project infrastructure is partly provided by [infrahost](https://infrahost.ch/en/).

## Copyright

"Hestia Control Panel", "HestiaCP", and the Hestia logo are original copyright of hestiacp.com and the following restrictions apply:

**You are allowed to:**

- use the names "Hestia Control Panel", "HestiaCP", or the Hestia logo in any context directly related to the application or the project. This includes the application itself, local communities and news or blog posts.

**You are not allowed to:**

- sell or redistribute the application under the name "Hestia Control Panel", "HestiaCP", or similar derivatives, including the use of the Hestia logo in any brand or marketing materials related to revenue generating activities,
- use the names "Hestia Control Panel", "HestiaCP", or the Hestia logo in any context that is not related to the project,
- alter the name "Hestia Control Panel", "HestiaCP", or the Hestia logo in any way.

## License

Hostisan is licensed under [GPL v3](LICENSE), as Hestia Control Panel is ([GPL v3](https://github.com/hestiacp/hestiacp/blob/release/LICENSE)), which is based on the [VestaCP](https://vestacp.com/) project.<br>
