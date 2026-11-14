# Plan: the installer pins the client to the server's own release tag

Status: planned. Spec: `docs/decisions/0019-pinned-client-version-in-installer.md` — the pin
source, the semver test, the wiring and the not-pinned SKILL.md line are settled there.
Baseline HEAD is the plan-0018 close-out `e28cc99` (`cmd/sandbin` 196 + 29, `cmd/sandbin-client`
107 + 28, `internal/version` 4 + 23, all gates green). Sits on 0017 (the version stamping whose
embedded tag this plan consumes) and 0015 (the served install flow whose byte-identity must
survive every change except this plan's intended one).

**Goal:** `install.sh` installs `sandbin-client@vX.Y.Z` matching the server's own embedded
release tag (`internal/version/release.txt`) when that tag is a plain semantic version
(`^v[0-9]+\.[0-9]+\.[0-9]+$`), and keeps `@latest` otherwise (empty tag, non-semver name).

**Architecture:** `internal/version` gains `EmbeddedTag() string` (trimmed `releaseTag`, empty
pre-release) and `SemverTag(tag string) string` (returns the tag itself when it matches
`^v[0-9]+\.[0-9]+\.[0-9]+$`, empty otherwise — the one decided home of the semver test, so the
table tests can drive both branches with synthetic tags). `cmd/sandbin/handlers.go` computes
`installPin()` once — `SemverTag(EmbeddedTag())` or the literal `latest` — and passes it to the
already-startup-parsed `installTemplate` through
a new `skillData.ClientVersion` field. `skills/sandbin-preview/install.sh` replaces the literal
`@latest` with `@{{.ClientVersion}}` and its header comment gains one explaining sentence. No
client change, no SKILL.md change, no landing-page change.

**Tech stack:** Go stdlib only.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Do not touch: bin routes and semantics, listings, 404 fallback, lockout, archives, DELETE,
  PATCH metadata, ip family, the CLI layer, the `/docs` hosting of 0018, `frontBase`/host guards,
  the landing page, `client.go`/`config.go`/store, `SKILL.md` (template and served body stay
  byte-identical), every operational error wording, the version resolver's precedence order and
  `String()` (only additive `EmbeddedTag()`), `scripts/release.sh` (it already writes the
  pinnable shape), `Makefile`, `Containerfile`.
- The pin reads ONLY the embedded tag — never `Value`, the VCS stamping or `String()` — so an
  injected or dev build never installs a tag that does not exist.
- The semver test is exactly `^v[0-9]+\.[0-9]+\.[0-9]+$`: `v` required, three plain numeric
  segments, no prerelease or build suffix; everything else falls back to `latest`.
- Tests keep the 0017 rule: no written-down version literal in a test expectation — expectations
  derive from `EmbeddedTag()`/the template the same way the resolver's self-test derives from
  `releaseTag`.
- Never push; never tag the real repo. Tags v0.1.0..v0.3.2 stay where they are.

## Review Focus

- The served install.sh is the ONLY intentional byte diff in the whole HTTP surface; every other
  route stays byte-identical to the baseline binaries, SKILL.md first.
- Both branches proven live: on this released tree (release.txt = `v0.3.2`) the served script
  carries `@v0.3.2`, and piping it installs a client that reports `v0.3.2`; on a scratch tree
  whose release.txt is non-semver (e.g. `nightly-2026`) or empty, the served script carries
  `@latest` and piping still installs and round-trips.
- `installPin` cannot be fooled: injected ldflags values and dev formats never appear in the
  served script.

## Task 1: `EmbeddedTag()` in internal/version

**Files:** modify `internal/version/version.go`, `internal/version/version_test.go`

**Interfaces:**
- Produces: `version.EmbeddedTag() string` — `strings.TrimSpace(releaseTag)`, the tag
  `scripts/release.sh` embedded, empty when release.txt is empty. Pure accessor: no precedence
  logic, no use of `Value`. Additive only; `String()` untouched. And
  `version.SemverTag(tag string) string` — the tag verbatim when it matches
  `^v[0-9]+\.[0-9]+\.[0-9]+$`, `""` otherwise; the semver decision lives here once.
- Tests: `EmbeddedTag` equals the trimmed embedded content (derives from `releaseTag` itself);
  `SemverTag` table-driven at both boundaries: accepts `v0.3.2`, `v1.2.3`, `v10.20.30`; rejects
  `v1.2`, `v1.2.3.4`, `1.2.3`, `V1.2.3`, `v1.2.3-rc1`, `v1.2.3+build`, `nightly-2026`, ``,
  ` dev (abc1234)`.

- [ ] Step 1: write the failing tests, confirm they fail.
- [ ] Step 2: implement; green; commit `feat: internal/version exposes its embedded release tag`.

## Task 2: the pin in the installer

**Files:** modify `skills/sandbin-preview/install.sh`, `cmd/sandbin/handlers.go`,
`cmd/sandbin/handlers_test.go`

**Interfaces:**
- `install.sh`: line 30's `.../sandbin-client@latest` becomes `.../sandbin-client@{{.ClientVersion}}`;
  the header comment gains one sentence: the server fills the version with its own release tag
  when that tag is a plain `vMAJOR.MINOR.PATCH`, otherwise `latest`. Nothing else in the script
  changes — the `go install failed` fallback path, `--dest`, the init flow stay byte-identical.
- `handlers.go`: package-level `installPin() string` — `version.SemverTag(version.EmbeddedTag())`
  when non-empty, else the literal `"latest"` — and `skillData` gains `ClientVersion string`;
  `renderInstallScript`
  (whatever path executes `installTemplate`) fills it. `skillTemplate`'s data gains the field
  too but SKILL.md references no new token (its `@latest` line stays literal), so its rendered
  bytes do not change.
- `handlers_test.go`: the install.sh battery pins that hardcode `@latest` become computed
  expectations (`"@" + installPin()`, which by construction equals
  `SemverTag(EmbeddedTag())` or `latest` — no written-down version literal anywhere); add a
  served-body test asserting the install.sh command line carries exactly that value, and a test
  that the rendered SKILL.md body is unaffected by the new field.

- [ ] Step 1: rewrite the failing pins (they must fail against today's literal `@latest`
  template on this released tree), confirm failure.
- [ ] Step 2: edit install.sh + wire handlers.go; green; commit
  `feat: the installer pins sandbin-client to the server's release tag`.

## Task 3: usage-doc wording

**Files:** modify `docs/usages/sandbin-client-skill.md` (and reword only where the installer's
`@latest` is described, if `sandbin-client.md`/`sandbin.md` carry any such sentence)

- [ ] Step 1: update the sentences that describe what the installer installs: the installer now
  pins `@vX.Y.Z` matching the server's own release tag (plain semver tags only), while SKILL.md's
  first-use build and non-semver/empty tags stay `@latest`. Keep every other claim
  byte-identical; run every touched example verbatim against a live server.
- [ ] Step 2: commit `docs: document the installer version pin`.

## Verification (implementer, after Task 3)

- All five gates; test counts per package before/after (baseline above) with `comm` on
  test-name lists; re-pinned assertions justified, no removals.
- Side-by-side vs baseline binaries (`git archive e28cc99` build under /tmp/opencode): full
  HTTP battery byte-identical EXCEPT the served install.sh body (quote the exact diff — one
  line of the go install command + the added comment sentence); SKILL.md served body
  byte-identical; landing page, /docs pages, bins and all error shapes identical.
- Live pin proof on the released tree: served install.sh contains `sandbin-client@v0.3.2`
  literally (release.txt holds v0.3.2); piped with `bash -s -- --dest <scratch>/skills` it
  installs and the installed `sandbin-client --version` prints exactly `v0.3.2`; an upload
  round trip works.
- Live fallback proof: a scratch COPY of the tree with release.txt rewritten to `nightly-2026`
  (and a second run with it emptied) builds, serves, and its served install.sh carries
  `@latest`; piping still installs (client prints a dev/`@latest`-shaped version) and
  round-trips. The copy lives under /tmp/opencode only; the real repo's release.txt is never
  touched.
- installPin cannot be fooled: a server binary built with `-ldflags -X ...version.Value=v9.9.9`
  still serves `@v0.3.2` (the pin reads the embedded tag, not Value); a built-with-Value server's
  `--version` line still prints v9.9.9 (String precedence untouched).
- Cleanup: kill servers via /proc scan (basename pid, Z ignored), ports 18296+ clear,
  `/tmp/opencode` EMPTY at the end, repo clean apart from `?? .opencode/`, tags unchanged.

## Reviewer TODO

1. Gates yourself; counts vs baseline; `comm` old-vs-new test names — removals only the expected
   re-pins.
2. Scope: `git diff e28cc99..HEAD --name-only` names only Task-listed files; SKILL.md template
   and its served body byte-identical; client tree, landing, /docs, Containerfile, Makefile,
   scripts/ untouched; `String()` and the resolver precedence unchanged (diff version.go).
3. Full HTTP byte-identity battery vs baseline binaries with install.sh the single intentional
   diff; quote the install.sh diff (command line + comment sentence).
4. Pin battery: served script carries `@` + `EmbeddedTag()` when semver (measured `@v0.3.2` here);
   scratch-tree `nightly-2026` and empty-tag trees serve `@latest` and piped installs still
   work; injected-Value server still pins the embedded tag and its `--version` still echoes the
   injected value; semver regex tested at both boundaries (`v1.2`, `v1.2.3.4`, `V1.2.3`,
   `v1.2.3-rc1`, `v1.2.3+build`, empty, whitespace-padded).
5. Installed-client proof: piped installer from the live released-tree server yields a client
   reporting exactly `v0.3.2` (the 0017 release.txt-in-zip path), and its upload/describe round
   trip works.
6. Usage-doc sentences accurate against measured served scripts; untouched claims byte-identical.
7. Cleanup confirmation: /proc scan clean of live sandbin, ports clear, `/tmp/opencode` empty,
   `git status --short` only `?? .opencode/`, `git tag -l` unchanged.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: Go-version-dependent `@v` install
workflows beyond plain tags, `GOPROXY` configuration advice, and pinning anything on the landing
page (it pipes install.sh and inherits the pin).
