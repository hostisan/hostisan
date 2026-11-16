# Hostisan build and test entry points. Run from the repo root.
export GOTOOLCHAIN := local

VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)
TYGO := github.com/gzuidhof/tygo@v0.2.21

# Hostisan's domain and the addresses built on it: the one place they are set.
# The panel binary gets the ones its UI links to (-X); PRODUCT_ENV=FILE builds
# with another copy. Make includes the file that bash sources: the test
# "bash and make read product.env the same" (test/hostisan/product.bats) keeps
# the two readings in agreement, and "product.env is plain KEY=value lines"
# keeps the file to the syntax both share.
PRODUCT_ENV ?= install/common/hostisan/product.env
include $(PRODUCT_ENV)
PRODUCT_LDFLAGS := -X main.productURL=$(HOSTISAN_URL) -X main.securityEmail=$(HOSTISAN_SECURITY_EMAIL)

.PHONY: test go-test ui-test tools-test core-edits-check core-edits-docker ui-install ui-build types build go-build dev debs \
	i18n-extract i18n-check i18n-merge i18n-stats

test: go-test ui-test tools-test

go-test:
	cd hostisan && go test -race -timeout 30m ./...

ui-install:
	cd ui && npm ci

ui-test:
	cd ui && npx vitest run

tools-test:
	test/test_helper/bats-core/bin/bats test/hostisan/
	shellcheck -S error tools/*.sh tools/apt-repo/*.sh
	tools/check-domain.sh .

# Hestia core files (bin func install src web) may differ from the fork point
# 94c1e760f only where hostisan/CORE-EDITS.md allows it. Needs full git history.
core-edits-check:
	tools/check-core-edits.sh

# Runs test/hostisan/core-edits.bats as root on a fake Hestia in Docker (needs docker).
core-edits-docker:
	test/hostisan/docker/run.sh

ui-build:
	cd ui && npm run build

# Regenerates ui/src/api/types.gen.ts from the Go wire structs (see hostisan/tygo.yaml).
types:
	cd hostisan && go run $(TYGO) generate

# Hostisan's message catalogue (hostisan/locale, see CONTRIBUTING.md "Translating
# Hostisan"). i18n-extract regenerates hostisan.pot from the code: it is generated,
# like types.gen.ts, so regenerate it rather than merging it by hand. i18n-check
# fails when it is out of date; i18n-merge updates every language's hostisan.po to
# it; i18n-stats prints each language's coverage.
i18n-extract:
	cd hostisan && go run ./cmd/i18n extract -repo ..

i18n-check:
	cd hostisan && go run ./cmd/i18n extract -repo .. -check

i18n-merge:
	cd hostisan && go run ./cmd/i18n merge -repo .. -all

i18n-stats:
	cd hostisan && go run ./cmd/i18n stats -repo ..

# One binary with the UI embedded.
build: ui-build
	$(MAKE) go-build

# The binary alone, built reproducibly: no cgo (a static binary), no local paths
# or VCS stamp, no build id. The same source, VERSION and GOARCH (taken from the
# environment, like GOOS) give the same bytes. OUT may be relative to the repo root.
OUT := dist/hostisan
go-build:
	cd hostisan && CGO_ENABLED=0 go build -trimpath -buildvcs=false \
		-ldflags "-s -w -buildid= -X main.version=$(VERSION) $(PRODUCT_LDFLAGS)" -o $(abspath $(OUT)) ./cmd/hostisan

# The four debs (hostisan, hostisan-nginx, hostisan-php, hostisan-web-terminal) for
# Ubuntu 24.04, built in a clean ubuntu:24.04 container from a git archive of
# HEAD (or REF=tag), with the install/ tarball, into DEBS_OUT (gitignored).
# Needs Docker; about 20 minutes. HOSTISAN_BUILD (default 105) and
# HOSTISAN_VERSION (default git describe) override the stamp.
DEBS_OUT ?= build/debs
debs:
	$(if $(HOSTISAN_BUILD),HOSTISAN_BUILD=$(HOSTISAN_BUILD)) $(if $(HOSTISAN_VERSION),HOSTISAN_VERSION=$(HOSTISAN_VERSION)) \
		tools/build-debs.sh --out $(DEBS_OUT)$(if $(REF), --ref $(REF))

# Development without a Hestia server: Go replays the dev fixtures on :8090 and the
# Vite dev server (http://localhost:5173) proxies /api to it. Any password logs in.
# The tools section works too: a --dev terminal (your own shell) on a socket in a
# private dir under /tmp (short: socket paths max out at 108 bytes), the file-manager handoff writing its PHP sessions there, and
# per-file restore turned on. Legacy /api/: hash=devapikey0123456789, or access key
# Zx9yWv8uTs7rQp6oNm5l with secret key aaaa… (40 a's). The /tmp dir is removed on exit.
dev:
	d=$$(mktemp -d /tmp/hostisan-dev.XXXXXX) && mkdir -m 700 $$d/sessions || exit 1; \
	pids=; trap 'kill $$pids 2>/dev/null; rm -rf "$$d"' EXIT; trap 'exit 130' INT TERM; \
	(cd hostisan && go run ./cmd/hostisan terminal --dev --listen unix:$$d/terminal.sock) & pids=$$!; \
	(cd hostisan && HOSTISAN_FILE_RESTORE=on go run -ldflags "$(PRODUCT_LDFLAGS)" ./cmd/hostisan serve --fixtures testdata/fixtures/dev \
		--locale-dir ../web/locale --dev --terminal-socket $$d/terminal.sock --php-session-dir $$d/sessions) & pids="$$pids $$!"; \
	cd ui && npm run dev
