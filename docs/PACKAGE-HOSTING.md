# Package Hosting Setup

Instructions for hosting Hostisan .deb packages so users can install and update via apt.

## Current State

- Build 122 packages are in `/home/this/dev/code/hostidea/dist/`
- Packages: `hostisan_1.0.0-122_amd64.deb`, `hostisan-nginx_1.0.0-122_amd64.deb`, `hostisan-php_1.0.0-122_amd64.deb`
- The installer currently points to `apt.hestiacp.com` — needs to change to Hostisan's repo

## Option A: GitHub Releases (Simpler)

Use GitHub Releases to host packages. Users add a script that downloads the latest release.

### Setup Steps

1. Create GitHub repo `hostisan/hostisan` (or use existing)
2. Create a release for each version (e.g., `v1.0.0-122`)
3. Upload all .deb files to the release
4. Update installer to download from GitHub:

```bash
# In installer, replace apt install with:
RELEASE_URL="https://github.com/hostisan/hostisan/releases/latest/download"
curl -LO "$RELEASE_URL/hostisan_${VERSION}_amd64.deb"
dpkg -i hostisan_*.deb
apt-get install -f  # fix dependencies
```

### Pros/Cons
- Pro: No server to maintain, free, reliable CDN
- Con: No `apt update && apt upgrade` workflow, manual download each time

## Option B: APT Repository (Better UX)

Host a proper apt repository at `apt.hostisan.com`. Users get standard apt workflow.

### Infrastructure Needed

- A server or static hosting (Cloudflare Pages, S3, GitHub Pages)
- GPG key for signing packages
- `reprepro` or `aptly` to manage the repository

### Setup Steps

#### 1. Generate GPG key

```bash
gpg --full-generate-key
# Choose RSA, 4096 bits, no expiration
# Name: Hostisan Package Signing
# Email: packages@hostisan.com

# Export public key
gpg --armor --export packages@hostisan.com > hostisan.gpg
```

#### 2. Set up repository structure

```bash
mkdir -p apt-repo/pool/main apt-repo/dists/noble/main/binary-amd64

# Copy packages
cp dist/*.deb apt-repo/pool/main/

# Generate Packages file
cd apt-repo
dpkg-scanpackages pool/main > dists/noble/main/binary-amd64/Packages
gzip -k dists/noble/main/binary-amd64/Packages

# Create Release file
cd dists/noble
cat > Release <<EOF
Origin: Hostisan
Label: Hostisan
Suite: noble
Codename: noble
Architectures: amd64
Components: main
Description: Hostisan package repository
EOF

# Add checksums
apt-ftparchive release . >> Release

# Sign Release
gpg --default-key packages@hostisan.com -abs -o Release.gpg Release
gpg --default-key packages@hostisan.com --clearsign -o InRelease Release
```

#### 3. Host the repository

**Option B1: Cloudflare Pages**
```bash
# Push apt-repo/ to a GitHub repo
# Connect to Cloudflare Pages
# Set custom domain: apt.hostisan.com
```

**Option B2: GitHub Pages**
```bash
# Create repo hostisan/apt
# Push apt-repo/ contents
# Enable Pages, set custom domain apt.hostisan.com
```

**Option B3: Dedicated server**
```bash
# Nginx config
server {
    listen 80;
    server_name apt.hostisan.com;
    root /var/www/apt-repo;
    autoindex on;
}
```

#### 4. Update installer

In `install/hst-install.sh`, change:

```bash
# Old
echo "deb https://apt.hestiacp.com/ $CODENAME main" > /etc/apt/sources.list.d/hestia.list
curl -s https://apt.hestiacp.com/deb_signing.key | apt-key add -

# New  
echo "deb https://apt.hostisan.com/ $CODENAME main" > /etc/apt/sources.list.d/hostisan.list
curl -fsSL https://apt.hostisan.com/hostisan.gpg | gpg --dearmor -o /usr/share/keyrings/hostisan.gpg
echo "deb [signed-by=/usr/share/keyrings/hostisan.gpg] https://apt.hostisan.com/ $CODENAME main" > /etc/apt/sources.list.d/hostisan.list
```

#### 5. Automate releases

Create a script or GitHub Action:

```bash
#!/bin/bash
# release.sh - Run after building new packages

VERSION=$1
GPG_KEY="packages@hostisan.com"

# Copy new packages
cp dist/*.deb apt-repo/pool/main/

# Regenerate metadata
cd apt-repo
dpkg-scanpackages pool/main > dists/noble/main/binary-amd64/Packages
gzip -k -f dists/noble/main/binary-amd64/Packages

cd dists/noble
apt-ftparchive release . > Release.new
mv Release.new Release
gpg --default-key $GPG_KEY -abs -o Release.gpg Release
gpg --default-key $GPG_KEY --clearsign -o InRelease Release

# Push to hosting
cd ../..
git add .
git commit -m "Release $VERSION"
git push
```

## Files to Update

When package hosting is live, update these:

1. `install/hst-install.sh` — apt source URL and GPG key
2. `bin/v-update-sys-hostisan-all` — update check URL if different
3. Website docs — installation instructions

## Testing

```bash
# On a fresh Ubuntu 24.04 server
curl -fsSL https://apt.hostisan.com/hostisan.gpg | sudo gpg --dearmor -o /usr/share/keyrings/hostisan.gpg
echo "deb [signed-by=/usr/share/keyrings/hostisan.gpg] https://apt.hostisan.com/ noble main" | sudo tee /etc/apt/sources.list.d/hostisan.list
sudo apt update
apt-cache policy hostisan  # Should show available version
```

## Recommendation

Start with **GitHub Releases** for simplicity. Move to APT repo later when user base grows and `apt upgrade` UX matters.

For GitHub Releases:
1. Create release v1.0.0-122
2. Upload the 3 .deb files from dist/
3. Update installer to curl from GitHub
4. Test on fresh server
