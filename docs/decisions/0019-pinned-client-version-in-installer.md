# Decisions: the installer pins the client to the server's own release tag

`install.sh` currently runs `go install .../sandbin-client@latest`. The server knows its own
release tag — `scripts/release.sh` embeds it in `internal/version/release.txt` at release time.
The installer will instead install the matching tagged client, and fall back to `@latest` only
when the embedded tag is not a plain semantic version.

| Point | Decision |
|---|---|
| Source of the pin | The embedded release tag only: `internal/version` gains an exported `EmbeddedTag() string` returning `strings.TrimSpace(releaseTag)` — empty on a pre-release or proxy-less tree. The pin does NOT read `String()`: an injected stamp or a `dev (hash)` VCS string is a build state, not a publishable tag, so it must not become an install pin. |
| Semver test | A tag is pinnable when it matches `^v[0-9]+\.[0-9]+\.[0-9]+$` — `vMAJOR.MINOR.PATCH`, `v` required, no prerelease or build suffix (release.sh always writes exactly this shape). Pinnable → the installer runs `go install github.com/yookoala/sandbin/cmd/sandbin-client@vX.Y.Z`, the same tag the serving binary was cut from. Anything else (empty, `v1.2`, `nightly-...`, prerelease) → `@latest`, exactly today's behavior. |
| Wiring | `skills/sandbin-preview/install.sh` replaces the literal `@latest` with `@{{.ClientVersion}}`; `skillData` gains a `ClientVersion string` field. `cmd/sandbin` computes it once (a small `installPin()` in handlers.go: `version.SemverTag(version.EmbeddedTag())` when non-empty, else `latest`) — deterministic per binary, like every other startup constant. The semver decision itself lives in `internal/version.SemverTag`, once. The install.sh comment block gains one sentence explaining the pin and the fallback. |
| Not pinned | `SKILL.md`'s first-use build line stays `@latest`: that path runs from whatever machine and moment the skill was copied to, where the server's tag need not be the newest or reachable; the skill is the resilience path, the installer is the version-matched path. The landing page's one-liner is unchanged — it pipes install.sh, so it inherits the pin. |
| Version honesty downstream | A pinned install yields a client whose own `--version` is the tag (`release.txt` travels in the module zip — the 0017 design), so installer, server and client agree on one version string end to end. An `@latest` install is unchanged behavior. |
| Release flow | No change to `scripts/release.sh`: it already writes the plain `vX.Y.Z` tag into `release.txt`, which is exactly the pinnable shape. Every future release automatically pins its own client. |
| Tests | New `internal/version` test for `EmbeddedTag` trimming and the semver shape. The `@latest` pins in `handlers_test.go` (the install.sh body battery) become computed expectations: `@` + the same `EmbeddedTag()` semver decision the server makes, keeping the "no written-down version in tests" rule of 0017. New render tests prove both branches: embedded tag semver → pinned; tag absent or non-semver → `@latest`. The served-body byte-identity batteries stay, re-pinned to the new template. |

## Skipped on purpose

Pinning SKILL.md's first-use build, prerelease/build-suffix tags, resolving `@latest` to a
concrete tag at render time (needs network), pinning the client by anything other than the
embedded tag (VCS stamping or injected ldflags values), a client change to request the
matching version from the server, docs pages under `/docs` describing the pin beyond the
usage-page wording touch, and any change to `SKILL.md`'s content.
