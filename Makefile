# The source-build entry point: build stamps both binaries with the version string
# scripts/version.sh computes from git, test runs the five gates, container builds the image
# with the same string, release records a tag. docs/decisions/0017-version-stamping.md explains
# what each stamped string means.

VERSION ?= $(shell scripts/version.sh)

# cmd/go quoted-splits the -ldflags value, so the inner quotes are what keep a version string
# containing spaces and parentheses in one piece.
LDFLAGS = '-s -w -X "github.com/yookoala/sandbin/internal/version.Value=$(VERSION)"'

.PHONY: build test container openapi release version

build:
	mkdir -p dist
	go build -trimpath -ldflags $(LDFLAGS) -o dist/sandbin ./cmd/sandbin
	go build -trimpath -ldflags $(LDFLAGS) -o dist/sandbin-client ./cmd/sandbin-client

test:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "needs gofmt:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go build ./...
	go test -count=1 ./...
	go test -race -count=1 ./...

container:
	podman build --network=host -t localhost/sandbin:latest --build-arg "SANDBIN_VERSION=$(VERSION)" -f Containerfile .

# openapi regenerates cmd/sandbin/openapi.json from cmd/sandbin/apispec.yaml. The generator is a
# dev-machine tool, never a module dependency, pinned to the version the config's patterns match:
#   go install github.com/ehabterra/apispec/cmd/apispec@v0.5.11
# The target opens by asserting that version, because the config's patterns and openapi-fix.jq's
# corrections are written against v0.5.11 exactly: another generator version regenerates a
# different document, so it must fail here instead of quietly rewriting it.
# apispec's generated JSON goes through cmd/sandbin/openapi-fix.jq (ruling R4): checked-in corrections the
# generator verifiably cannot express itself — the header there explains each one. jq is a
# dev-machine prerequisite of this target, like apispec.
# Keeping the tracked document safe from a failed run: jq writes openapi.tmp.json and only a
# successful filter run mv's it over cmd/sandbin/openapi.json. A jq compile error truncating the
# tracked file straight through a redirect would destroy the committed document (and its embedded
# copy) with nothing to restore it from but git. Both temps are gitignored scratch of this target.
# The working client-package exclusion is the -exclude-package CLI flag; apispec v0.5.11 ignores a
# yaml exclude: block at the repo root, so the config carries none.
# docs/decisions/0030-api-servemux-openapi.md explains what the document pins.
openapi:
	@apispec -version 2>&1 | grep -q '^apispec version: v0\.5\.11$$' || { \
	  echo "openapi: the pinned generator is apispec v0.5.11 — install it with:" >&2; \
	  echo "  go install github.com/ehabterra/apispec/cmd/apispec@v0.5.11" >&2; \
	  echo "this machine reports:" >&2; apispec -version 2>&1 >&2; exit 1; }
	apispec -dir cmd/sandbin -c cmd/sandbin/apispec.yaml -o openapi.raw.tmp.json -exclude-package sandbin-client
	jq -f cmd/sandbin/openapi-fix.jq openapi.raw.tmp.json > openapi.tmp.json
	mv openapi.tmp.json cmd/sandbin/openapi.json
	@rm openapi.raw.tmp.json

# make release TAG=v0.3.0 records the tag; it commits, tags and stops. Pushing stays human.
release:
	@if [ -z "$(TAG)" ]; then echo "release: TAG=vX.Y.Z is required" >&2; exit 1; fi
	scripts/release.sh "$(TAG)"

version:
	@echo "$(VERSION)"
