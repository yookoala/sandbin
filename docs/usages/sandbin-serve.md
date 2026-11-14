# Running the server

Build and run:

```sh
make                                    # -> dist/sandbin and dist/sandbin-client, stamped from git
SANDBIN_API_KEY=your-key ./dist/sandbin serve
```

`make` is the source-build entry point, and its other targets are the rest of the workflow:
`make test` runs the five gates (`gofmt`, `go vet`, `go build`, `go test`, `go test -race`),
`make container` builds the podman image carrying the same version string, `make release
TAG=v0.3.0` records a release tag (it commits and tags, and never pushes), and `make version`
prints the string `make build` would stamp. `VERSION='anything' make build` overrides the stamp.

## What `--version` reports

Both binaries print one line, `sandbin version <string>` or `sandbin-client version <string>`,
and `internal/version` picks the first source that knows something:

1. a value injected at build time with `-ldflags -X github.com/yookoala/sandbin/internal/version.Value`
   — what `make build`, the Containerfile and any manual `VERSION=` override do, used verbatim;
2. Go's own stamping of the git checkout it was built from, which knows the commit and whether
   anything differs from HEAD — tracked edits *and* new untracked files (measured: a clean HEAD
   with only an untracked scratch folder stamps `dev (3abcdef+)`) — but cannot know a tag:
   `dev (3abcdef)` or `dev (3abcdef+)`;
3. the tag `scripts/release.sh` embedded in `internal/version/release.txt`, which travels inside
   the module zip: the tag alone;
4. the module version the build info carries, the backstop for an empty `release.txt`;
5. plain `dev`.

So the stamped `make build` gives the three git-aware formats (`scripts/version.sh` computes
them: exact tag `v0.3.0 (1abcdef)`, tag plus offset `v0.3.0+ (2abcdef)`, dirty `dev
(3abcdef+)`, no reachable `v*` tag `dev (4abcdef)`), while `go build ./cmd/sandbin` in the same
checkout reports `dev (hash)` — an honest answer, since a build that never ran `git describe`
has no idea a tag exists. `go build -buildvcs=false`, or a build made outside a repository,
knows neither and falls to the embedded tag, or to plain `dev` where `release.txt` is still
empty. `go install github.com/yookoala/sandbin/cmd/sandbin@v0.3.0` prints the tag alone: a
proxy-downloaded module has no `.git` and `@version` builds take no `-ldflags`, so no
clean route puts a commit hash into it. Building from a checkout with `go install ./cmd/...`
behaves like `go build` and gets the `dev (hash)` form.

Every invocation names its verb: `serve` runs the server, `ip list`, `ip show <ip>`, `ip unblock
<ip>` and `ip clear` inspect and lift the API key blocks through that server's control socket
(see [Managing blocked addresses](sandbin-ip.md)), and `help` prints the usage. A bare `sandbin`, `sandbin help`
and `--help` print that usage on stdout and exit 0; an unknown verb answers
`No help topic for 'x'` on stderr and exits 1.

`--api-key` (default `$SANDBIN_API_KEY`), `--db` (default `sandbin.db`), `--addr` (default
`:8080`) and `--socket` (default `$SANDBIN_SOCKET`, otherwise the `--db` path plus `.sock`; see [Managing blocked addresses](sandbin-ip.md)) are `serve`'s core settings; `--trusted-proxy` and
`--trusted-proxy-file` (defaults `$SANDBIN_TRUSTED_PROXIES` and `$SANDBIN_TRUSTED_PROXIES_FILE`,
see [Trusted proxies](sandbin-lockout.md#trusted-proxies)) optionally name the proxies the server believes. The server exits when no API
key is configured, a trusted proxy value cannot be read, or another serve already answers on its
control socket.

`GET /` answers the branded landing page: the wordmark, the tagline, the GitHub links, the
one-line skill installer with its copy button, and the links into the hosted docs. The hosted
docs live under `/docs`: `GET /docs` and `GET /docs/` answer a hub page listing the two doc sets
and every page each set covers, `GET /docs/<name>.html` answers the page rendered from the
`docs/usages/<name>.md` of the same name — the markdown is embedded in the binary at build time,
so nothing under `/docs` is read from disk — and `GET /docs/assets/sandbin-logo.svg` answers the
logo as `image/svg+xml`. None of them need a key, every other method on them answers `405`, and
an unknown name under `/docs` is the plain `404 unknown bin`.
