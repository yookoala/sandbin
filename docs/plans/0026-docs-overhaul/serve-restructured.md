# Running the server

This page covers the machine side of a Sandbin instance: getting the source, building the
binaries, and running the `serve` subcommand. What the server answers on its port is [the API
page](sandbin-api.md); the blocking machinery behind bad keys is [IP Address
Blocking](sandbin-ip-blocking.md); running under podman is [the podman page](sandbin-podman.md).

## Download the source

The repository is [github.com/yookoala/sandbin](https://github.com/yookoala/sandbin):

```sh
git clone https://github.com/yookoala/sandbin
cd sandbin
```

`main` is the working branch, and every release is a tag on it (`v0.4.2` is the newest), so a
named release of the source is one `git checkout v0.4.2` away. You can also skip the clone
entirely: the Go module proxy serves any tagged release, and `go install` builds it for you:

```sh
go install github.com/yookoala/sandbin/cmd/sandbin@v0.4.2
go install github.com/yookoala/sandbin/cmd/sandbin-client@v0.4.2
```

Those land as `sandbin` and `sandbin-client` under `$GOPATH/bin`, and each reports its tag as
its version string — a proxy-downloaded module zip has no git history to derive more from.

## Build the server

Building from a checkout needs Go 1.26.10 or newer (the `go` line of `go.mod`) and `make`. One
command builds both binaries:

```sh
make
```

That is `make build`: it stamps `dist/sandbin` and `dist/sandbin-client` with the version
string `scripts/version.sh` computes from the checkout. The other targets are the rest of the
workflow: `make test` runs the five gates (`gofmt`, `go vet`, `go build`, `go test`,
`go test -race`), `make container` builds the podman image carrying the same version string,
`make release TAG=v0.4.2` records a release tag (it commits and tags, and never pushes), and
`make version` prints the string `make build` would stamp. `VERSION='anything' make build`
overrides the stamp. Plain `go build ./cmd/sandbin` works too, but its version string reports
`dev (hash)` — an honest answer, since that build never asked git which tag it sits on.

## The `serve` subcommand

`serve` runs the server: it opens the sqlite database named by `--db` (creating it when
missing), binds the HTTP address named by `--addr`, and serves until stopped.

The server needs an API key, and there are two ways to give it one. The flag:

```sh
./dist/sandbin serve --api-key your-key
```

or the environment variable, which is the form the container and quadlet flows use:

```sh
export SANDBIN_API_KEY=your-key
./dist/sandbin serve
```

The flag wins over the variable, and with neither set the server refuses to start. This key is
what every key-checked request — `POST /`, `PUT`, `PATCH`, `DELETE` and `POST /verify-key` —
must present in `X-API-Key` (see [the API page](sandbin-api.md)); a wrong one answers `403`
and charges the address to the [block table](sandbin-ip-blocking.md).

The rest of `serve`'s core settings are flags with environment fallbacks:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--api-key` | `$SANDBIN_API_KEY` | the write key every key-checked request must present |
| `--db` | `sandbin.db` | the sqlite file holding every bin and file |
| `--addr` | `:8080` | the address to listen on |
| `--socket` | `$SANDBIN_SOCKET`, else the `--db` path plus `.sock` | the control socket the `ip` family drives (see [IP Address Blocking](sandbin-ip-blocking.md)) |
| `--trusted-proxy` | `$SANDBIN_TRUSTED_PROXIES` | proxy addresses and CIDRs whose `X-Forwarded-For` is believed (see [Trusted proxies](sandbin-ip-blocking.md#trusted-proxies)) |
| `--trusted-proxy-file` | `$SANDBIN_TRUSTED_PROXIES_FILE` | the same proxy list, one entry per line in a file |

Beyond a missing API key, the server exits when a trusted-proxy value cannot be read or when
another serve already answers on its control socket.

The CLI names its verb: `serve` runs the server and `help` prints the usage. A bare `sandbin`,
`sandbin help` and `--help` print that usage on stdout and exit 0; an unknown verb answers
`No help topic for 'x'` on stderr and exits 1. The `ip` family inspects and lifts API key
blocks and belongs to the blocking machinery: see [IP Address Blocking](sandbin-ip-blocking.md).
