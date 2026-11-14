# Plan: stamping the version string from the tag and commit

Status: planned. Spec: `docs/decisions/0017-version-stamping.md` — precedence, formats and
limits are settled there. Baseline HEAD is the logo-hash-fix close-out (`cmd/sandbin` 183
top-level tests + 19 subtests, `cmd/sandbin-client` 106 + 28, all gates green). Sits on plan
0015 (the urfave wiring whose `--version` behavior must survive).

**Goal:** `internal/version.String()` resolves the version string — injected value > Go's
VCS stamping > embedded release tag > module version > `dev` — and the Makefile stamps the
three user formats (`v0.3.0 (hash)`, `v0.3.0+ (hash)`, `dev (hash+)`) via `git describe` +
`-ldflags -X`. `scripts/release.sh` records a tag into an embedded file and tags the commit;
it never pushes.

**Architecture:** new `internal/version` package (`Value` ldflags target, `go:embed
release.txt`, pure resolver + `String()`); both mains set `app.Version = version.String()`.
New `scripts/version.sh` (formats), `Makefile` (`build` default → `dist/`, `test`,
`container`, `release TAG=`, `version`), `scripts/release.sh`. Containerfile gains
`COPY internal ./internal` + `ARG SANDBIN_VERSION` into its build `-ldflags`. No new module
dependency.

**Tech stack:** Go stdlib (`embed`, `runtime/debug`), POSIX shell, Make, existing urfave wiring.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...` — now covering `./internal/version` too.
- Do not touch: HTTP handlers, `store.go`, `control.go`, `ipblock.go`, archives, ansi,
  `skill.go`, `skills/sandbin-preview/*` (byte-identical), `.github/workflows/ci.yml`,
  deploy/, the urfave wiring beyond `app.Version`, every operational error wording.
- Version output stays exactly one line (`sandbin version <string>`, exit 0) on both
  binaries; bare-invocation, unknown-verb and misuse behavior unchanged.
- `-ldflags` must survive values containing spaces and parentheses: quote as
  `-ldflags '-X "github.com/yookoala/sandbin/internal/version.Value=v0.3.0 (1abcdef)"'`
  (cmd/go quoted-splits the ldflags string) — proven live in Task 3.
- Never push; never create or move a tag in the real repo (scratch clones under
  `/tmp/opencode` only); never run `scripts/release.sh` against this repository.
- Dirty = tracked changes only (`git diff HEAD --quiet`); short hashes = 7 hex;
  `git describe --tags --match 'v*'`.

## Review Focus

- Exact format equality against real git states: exact-tag, few-commits-after-tag, dirty,
  no-reachable-tag, shallow clone — via `make build` in scratch clones of this repo.
- Precedence: injection beats everything; repo builds without injection fall to
  `dev (hash)[+]` even when `release.txt` is nonempty (a stamp-less build cannot know the
  tag); a zip build reports the embedded tag only.
- `release.sh` safety: clean-tree requirement, format + duplicate-tag refusal, annotated
  tag created on the release commit, embedded file content byte-equal to the tag, no push
  logic anywhere.
- Quoting through two layers: Makefile recipe and Containerfile RUN both must deliver a
  spaced value verbatim into the binary.
- `go install` simulation must use a real file:// GOPROXY built from a tagged scratch
  clone's `git archive`, not a hand-crafted fake.

## Task 1: the version package

**Files:** create `internal/version/version.go`, `internal/version/version_test.go`,
`internal/version/release.txt` (empty)

**Interfaces:**
- Produces: `var Value string` (ldflags target, empty default); `//go:embed release.txt`
  content; an internal pure resolver `format(value, embeddedTag, modVersion, revision
  string, haveVcs, modified bool) string` implementing the decision-table precedence and
  the four git-state strings; `String() string` feeding it from `debug.ReadBuildInfo()`
  (Settings keys `vcs.revision`, `vcs.modified`; `Main.Version`, with `(devel)`/empty
  treated as unknown).

- [ ] Step 1: write the failing table test: exact-tag injection verbatim; injected beats
  everything; VCS dirty → `dev (abcdef1+)`; VCS clean → `dev (abcdef1)`; no VCS + tag →
  the tag; no VCS + mod version only → that version; nothing known → `dev`; empty/garbage
  inputs stay safe. Plus a test asserting `String()` on the test binary matches
  `^dev \([0-9a-f]{7}\+?\)$` or a known-state equivalent.
- [ ] Step 2: confirm they fail.
- [ ] Step 3: implement the package.
- [ ] Step 4: `go test -count=1 ./internal/version` green; commit `feat: version package resolves tag, commit hash and dirty state`.

## Task 2: wire both commands

**Files:** modify `cmd/sandbin/main.go`, `cmd/sandbin/main_test.go`,
`cmd/sandbin-client/main.go`, `cmd/sandbin-client/main_test.go`

**Interfaces:**
- Consumes: `version.String()`. Produces: `app.Version = version.String()` replacing
  `"dev"` on both apps; `--version` pins rewritten to assert the resolver's actual output
  (exact match against `version.String()` computed in the test) at exit 0, single line.

- [ ] Step 1: update the two `--version` tests to the new expected line; confirm they fail.
- [ ] Step 2: wire the package; everything else about the urfave apps untouched.
- [ ] Step 3: both packages green; commit `feat: both commands print the stamped version`.

## Task 3: the build entry points

**Files:** create `Makefile`, `scripts/version.sh`, `scripts/release.sh`; modify
`Containerfile` (COPY internal + ARG), `.gitignore` (add `/dist/`)

**Interfaces:**
- `scripts/version.sh`: prints the git-state string per the decision table (exact-tag →
  `v0.3.0 (hash)`; describe offset → `v0.3.0+ (hash)`; tracked-dirty → `dev (hash+)`; no
  reachable v-tag → `dev (hash)`); run from the repo root; no writes.
- `Makefile`: `VERSION ?= $(shell scripts/version.sh)`; `build` (default) compiles both
  binaries to `dist/` with `-trimpath -ldflags '-s -w -X "github.com/yookoala/sandbin/internal/version.Value=$(VERSION)"'`;
  `test` runs the five gates; `container` runs `podman build --network=host
  --build-arg SANDBIN_VERSION=$(VERSION) -f Containerfile .`; `release` runs
  `scripts/release.sh $(TAG)`; `version` prints `$(VERSION)`.
- `scripts/release.sh TAG`: refuses a dirty tracked tree, a bad format
  (`v[0-9]+\.[0-9]+\.[0-9]+` with optional `-dotted` prerelease), or an existing tag;
  writes `TAG\n` into `internal/version/release.txt`, commits `chore: release TAG`, runs
  `git tag -a TAG -m TAG`; prints what it did and the follow-up push commands; contains no
  `git push` / network call.
- Containerfile: `COPY internal ./internal` before the build step;
  `ARG SANDBIN_VERSION=` and ldflags `-s -w -X \"github.com/yookoala/sandbin/internal/version.Value=${SANDBIN_VERSION}\"`
  (inner quotes for spaced values; empty ARG keeps today's behavior).

- [ ] Step 1: write the live checks as a script or step-by-step in /tmp/opencode scratch: `make version` against the real repo prints the exact clean-after-tag string `v0.2.0+ (d89f760-or-current)` (the repo has reachable tags; HEAD sits several commits after v0.2.0 — recompute the hash at run time); `make build && ./dist/sandbin --version` and `./dist/sandbin-client --version` print that same string; a manual `VERSION='v9.9.9 (abcdef1)' make build` override prints it verbatim. Confirm they fail against today's `dev`.
- [ ] Step 2: confirm they fail.
- [ ] Step 3: implement the Makefile, scripts, Containerfile, .gitignore.
- [ ] Step 4: checks pass; all gates green; commit `feat: Makefile and release script stamp the build version`.

## Task 4: documentation

**Files:** modify `README.md` (Quick start), `docs/usages/sandbin-serve.md` (build + version
semantics), `docs/usages/sandbin-podman.md` (stamped container build),
`docs/usages/sandbin-client-skill.md` (installed-client version note),
`docs/usages/sandbin-client-upload.md` only if a command line changes (it should not)

- [ ] Step 1: README Quick start step 1 switches to `make` + `./dist/sandbin serve`; a
  short version-semantics sentence (three formats + `go install` reports the tag only).
- [ ] Step 2: `sandbin-serve.md`: building section rewritten for `make` / `make build`,
  `make test`, `make release TAG=`, the precedence list, and the `go install` limitation;
  `sandbin-podman.md`: the build command with `--build-arg SANDBIN_VERSION="$(scripts/version.sh)"`
  or `make container`; `sandbin-client-skill.md`: one sentence — an `@latest`-installed
  client prints its tag alone (`v0.1.2` style), by-design.
- [ ] Step 3: run every touched example verbatim against a live server / scratch clones;
  commit `docs: document the version stamping and the make build entry point`.

## Verification (implementer, after Task 4)

- All five gates; test counts per package before/after (baseline above; new
  `internal/version` counts reported).
- Scratch-clone battery under /tmp/opencode (a real `git clone` of this repo, its own tags
  via `scripts/release.sh v9.9.9` INSIDE THE CLONE only): exact-tag build → `v9.9.9 (hash)`;
  two commits later → `v9.9.9+ (hash)`; dirty → `dev (hash+)`; `git clone --no-tags
  --depth 1` → `dev (hash)`; `VERSION=` override verbatim; `-buildvcs=false` → `dev`.
- Zip-install battery: publish the tagged clone into a file:// GOPROXY (`@v/v9.9.9.info`,
  `.mod`, `.zip` from `git archive`), then `GOPROXY=file://... GONOSUMDB=* GOFLAGS=-mod=mod
  go install github.com/yookoala/sandbin/cmd/sandbin-client@v9.9.9` with GOBIN into
  scratch → prints `sandbin-client version v9.9.9`.
- Side-by-side vs the pre-change binaries: every other CLI output byte-identical; only the
  `--version` line changes.
- Cleanup: kill servers via /proc scan (pid via basename, Z ignored), `/tmp/opencode`
  EMPTY at the end, repo clean apart from `?? .opencode/`, no new tag in the real repo
  (`git tag -l` unchanged).

## Reviewer TODO

1. Gates yourself (all five incl. `-race`) across all three packages; counts vs baseline;
   `comm` on old-vs-new test-name lists — zero removals.
2. Scope: `git diff <plan-open>..HEAD --name-only` names only Task-listed files;
   `git tag -l` identical to baseline; `go.mod`/`go.sum` unchanged; `skills/`, handlers,
   store, control, ipblock, archives, ansi, CI workflow, deploy/ byte-identical.
3. Formats battery yourself in fresh scratch clones (do not trust the implementer's):
   exact-tag, +after-tag, dirty, no-tags, shallow — each `make build` output asserted
   against the exact expected string computed independently from `git rev-parse`/describe.
4. `release.sh` battery in a scratch clone: happy path (release commit message, embedded
   file content, annotated tag exists, `cat tag` shows it); each refusal (dirty, bad
   format `v9.9`, duplicate) with exit 1 and zero side effects; grep the script for
   `push`/network. Then verify the tagged clone's plain `go build ./cmd/sandbin` (no make)
   still reports `dev (hash)` — stamp-less builds must not claim the tag.
5. Precedence battery: injection beats embedded tag beats VCS beats mod version (use
   `-ldflags` directly, the file:// GOPROXY zip install, `-buildvcs=false`, and
   `VERSION=` overrides); the GOPROXY install must come from a `git archive`-built zip,
   not a hand-made one, and report the tag only.
6. Container proof: `make container` (or explicit `podman build --network=host --build-arg
   SANDBIN_VERSION='v9.9.9 (abcdef1)'`) from a scratch clone carrying tag v9.9.9, run with
   the standard volume/ports convention, container's `sandbin --version` prints the exact
   string; empty ARG keeps `dev`-fallback behavior; full podman cleanup per the standing
   keep-list.
7. Regression sweep vs the pre-change binaries: full 0015 CLI battery (bare 0, unknown verb
   1, misuse 1, refusals single-line wordings, one upload/delete round trip) byte-identical
   except the `--version` line; docs examples from README + the three touched usage pages
   run verbatim.
8. Cleanup confirmation: /proc scan clean of live sandbin, `/tmp/opencode` empty, `git
   status --short` only `?? .opencode/`, `git tag -l` unchanged.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: version display on the front
page or startup log, `--version` JSON output, CI-side stamping of artifacts, commit-hash
embedding at release time, and running the release script against this repository.
