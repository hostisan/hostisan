# Hostisan Development Handoff

Last updated: 2026-11-16

## Project Overview

Hostisan is a HestiaCP fork with:
- Modern Go API backend (`hostisan/`)
- React/TypeScript UI (`ui/`)
- Rebranded from "Hestia" to "Hostisan" for public-facing elements
- Internal paths still use `/usr/local/hestia/` for compatibility

## Current State

### Completed Work

1. **Package Renaming** (build 105)
   - Packages renamed from `hestia-*` to `hostisan-*`
   - Files: `src/deb/hostisan/`, all control files, service files
   - Build scripts updated: `tools/build-debs.sh`, `src/hst_autocompile.sh`
   - Installers updated: `install/hst-install-ubuntu.sh`, `install/hst-install-debian.sh`
   - Ready packages in `dist/`:
     - `hostisan_1.10.5-105+ubuntu24.04_amd64.deb`
     - `hostisan-nginx_1.30.5-105+ubuntu24.04_amd64.deb`
     - `hostisan-php_8.5.11-105+ubuntu24.04_amd64.deb`
     - `hostisan-web-terminal_2.0.0-105+ubuntu24.04_amd64.deb`

2. **Command Aliases**
   - `v-*-hostisan-*` commands work alongside `v-*-hestia-*`
   - 9 alias scripts in `bin/` (e.g., `v-update-sys-hostisan-all`)
   - Implementation: exec wrapper calling the hestia equivalent

3. **Bug Fixes**
   - Let's Encrypt empty `user.key` fix (`bin/v-add-letsencrypt-user`)
   - File Manager "Exit to Control Panel" flash fix (`install/deb/filemanager/filegator/configuration.php`)

4. **Website** (`/home/this/dev/code/hostisan-site/`)
   - Astro + Starlight docs site
   - 28 pages with UI-first documentation
   - Diátaxis structure: tutorials, how-to, reference, explanation
   - Run with: `cd /home/this/dev/code/hostisan-site && npm run dev -- --port 5032`
   - NOT deployed yet

### Remaining Tasks

1. **Deploy Website**
   - Target: hostisan.com via Cloudflare Pages
   - Site is ready, just needs deployment

2. **Package Hosting**
   - See `docs/PACKAGE-HOSTING.md` for detailed instructions
   - Options: GitHub Releases (simpler) or APT repository (better UX)
   - Need to update installer to point to new repo

3. **Mail Relay**
   - Waiting on provider account
   - Once available, configure in installer

### Test Server

- **URL**: panel.logfriend.com
- **IP**: 147.124.209.120
- Fresh install with all fixes applied
- Has build 122 (old hestia-* naming) — needs reinstall for hostisan-* packages

## Architecture Notes

### Internal vs External Naming

| Context | Name Used |
|---------|-----------|
| Package names | `hostisan`, `hostisan-nginx`, etc. |
| apt install | `apt install hostisan hostisan-nginx hostisan-php` |
| File paths | `/usr/local/hestia/` (unchanged for compatibility) |
| Variables | `$HESTIA`, `$HESTIA_INSTALL_DIR` (unchanged) |
| CLI commands | Both `v-*-hostisan-*` and `v-*-hestia-*` work |
| Service names | `hostisan.service`, `hostisan-web-terminal.service` |

### Key Files

| File | Purpose |
|------|---------|
| `src/deb/hostisan/control` | Main package metadata |
| `tools/build-debs.sh` | Builds .deb packages in Docker |
| `src/hst_autocompile.sh` | Compiles nginx/php from source |
| `install/hst-install-ubuntu.sh` | Ubuntu installer |
| `Makefile` | Build targets (`make debs`, `make build`, etc.) |

### Building Packages

```bash
# Build all packages (build number from control file)
make debs

# Build with specific number
make debs BUILD=106

# Output goes to build/debs/, copy to dist/
cp build/debs/hostisan*.deb dist/
```

### Running the Website Locally

```bash
cd /home/this/dev/code/hostisan-site
npm install  # if needed
npm run dev -- --port 5032
# Open http://localhost:5032
```

## Manual Cleanup Required

The `.superpowers/` directory (511MB) contains root-owned files from old build sessions. Remove with:

```bash
sudo rm -rf /home/this/dev/code/hostidea/.superpowers/
```

## Git Status

All changes committed to `main` branch. Recent commits:
- `feat: Rename packages from hestia-* to hostisan-*`
- `fix: Update build script for hostisan-* package names`
- `fix: Update hst_autocompile.sh for hostisan directory rename`
- `feat(cli): Add hostisan command aliases`
- `fix(filemanager): Exit to Control Panel goes directly to dashboard`
- `fix(letsencrypt): Ensure user key is properly generated`

## Next Agent Instructions

1. If continuing package hosting setup, follow `docs/PACKAGE-HOSTING.md`
2. If deploying website, push `/home/this/dev/code/hostisan-site/` to GitHub and connect to Cloudflare Pages
3. If testing on server, reinstall with new hostisan-* packages
4. Run `sudo rm -rf .superpowers/` to free 511MB before moving folder
