# Decisions: stamping the version string from the tag and commit hash

The binaries report a git-aware version through `--version`: exact on a tagged commit
(`v0.3.0 (1abcdef)`), tagged-plus when a few commits past a tag (`v0.3.0+ (2abcdef)`),
and `dev (3abcdef+)` with uncommitted changes. The user asked whether `go install` can
still carry this; it partly cannot, so the design is a hybrid whose full-string builds run
where git is available and whose fallbacks report only what each build honestly knows.

| Point | Decision |
|---|---|
| Source precedence | `internal/version.String()` picks the first known: (1) a `-ldflags -X github.com/yookoala/sandbin/internal/version.Value` injection — verbatim, the Makefile, the Containerfile and any manual override impose this; (2) Go's own VCS stamping (`vcs.revision`, `vcs.modified` from `debug.ReadBuildInfo()`, present in any Go ≥1.18 build from a git checkout) → `dev (hash)` / `dev (hash+)`; (3) the tag embedded by the release script in `internal/version/release.txt` via `go:embed` — travels inside the module zip → the tag string alone; (4) the module version from the build info (backstop for 3); (5) plain `dev`. |
| Why a hybrid | Tag distance is only knowable to a build step that can run `git describe`; a finished binary or module zip carries no tags. The Makefile is that step and produces the user's three formats exactly. |
| Release script | `scripts/release.sh vX.Y.Z`, run from a clean tree: validates the tag format, refuses an existing tag, writes the tag into `internal/version/release.txt`, commits `chore: release vX.Y.Z`, then `git tag -a` on that commit. It never pushes — pushing and publishing stay human. The embedded file holds the tag only, no commit hash (a commit cannot embed its own hash; the tag points at the release commit, so the Makefile resolves hashes later). |
| Makefile | The source-build entry point (user's choice over a bare script). Targets: `build` (default) → `dist/sandbin` + `dist/sandbin-client` stamped from `scripts/version.sh` via `-ldflags`; `test` (the five gates); `container` (`podman build --build-arg SANDBIN_VERSION=...`); `release` (`make release TAG=vX.Y.Z`); `version` (print the computed string). `VERSION ?=` allows an override. |
| scripts/version.sh | The git-side formats: exact tag match on HEAD → `v0.3.0 (hash)`; `git describe --tags --match 'v*'` with a nonzero offset → strip the `-N-g<hash>` tail → `v0.3.0+ (hash)`; uncommitted tracked changes → `dev (hash+)`; no reachable `v*` tag (fresh/shallow clone) → `dev (hash)` (user's choice over a bare hash or plain `dev`). Dirty means tracked changes only (`git diff HEAD --quiet`) — untracked files do not count, matching Go's own `vcs.modified` rule. Hashes are 7 hex chars (`git rev-parse --short=7`). |
| go install limitation (recorded) | `go install github.com/yookoala/sandbin/...@vX.Y.Z` downloads a proxy zip: no `.git`, no VCS stamping, and no way to pass `-ldflags` into an `@version` build. Such a binary reports the embedded tag only — `v0.3.0`, format 1 minus the hash, which is honest: the installed code is exactly the tagged commit. No clean workaround puts a commit hash into it. `go install ./cmd/...` from a checkout behaves like `go build` and gets `dev (hash)[+]`. |
| Unstamped repo builds | A plain `go build` inside a clone reports `dev (hash)` / `dev (hash+)` — the binary cannot know the tag; `make build` is the blessed stamped path (README Quick start uses it). `go build -buildvcs=false` or building outside a repo falls to plain `dev`. |
| Container | The Containerfile gains `COPY internal ./internal` and `ARG SANDBIN_VERSION=""` passed into `-ldflags` (with inner quotes so values containing spaces survive cmd/go's quoted split of the ldflags string). `make container` passes the computed version; the podman usage page documents `--build-arg`. |
| Display | Unchanged: urfave `app.Version = version.String()`, so `--version` still prints one line `sandbin version <string>` / `sandbin-client version <string>` and bare help, unknown-verb and misuse behavior is untouched. CI keeps its plain `go build ./...` — the test gates need no stamping. |
| Test seams | `internal/version` holds a pure resolver (inputs: injected value, embedded tag, module version, revision, modified) with a table test for every format and precedence; the two `--version` CLI pins are updated to assert against the resolver output rather than the hardcoded `dev`. |
| .gitignore | Add `/dist/`. |

## Skipped on purpose

CI release automation and publish pipelines, embedding the commit hash at release time
(circular), goreleaser-style tooling, pseudo-version tricks to smuggle hashes through
`@latest`, version display beyond `--version` (front page, startup logs), and pushing —
and the agent never runs the release script against this repository: it exists for the human
to run when they choose to release.
