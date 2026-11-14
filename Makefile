# The source-build entry point: build stamps both binaries with the version string
# scripts/version.sh computes from git, test runs the five gates, container builds the image
# with the same string, release records a tag. docs/decisions/0017-version-stamping.md explains
# what each stamped string means.

VERSION ?= $(shell scripts/version.sh)

# cmd/go quoted-splits the -ldflags value, so the inner quotes are what keep a version string
# containing spaces and parentheses in one piece.
LDFLAGS = '-s -w -X "github.com/yookoala/sandbin/internal/version.Value=$(VERSION)"'

.PHONY: build test container release version

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

# make release TAG=v0.3.0 records the tag; it commits, tags and stops. Pushing stays human.
release:
	@if [ -z "$(TAG)" ]; then echo "release: TAG=vX.Y.Z is required" >&2; exit 1; fi
	scripts/release.sh "$(TAG)"

version:
	@echo "$(VERSION)"
