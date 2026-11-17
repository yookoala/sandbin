# Decision 0028: a published-image first podman page, ordered right after the serve page

Decided at HEAD `4c8e5ab` (plan 0027 closed, tag `v0.4.3` pushed), previewed at
`https://preview.yookoala.dev/951c2bcb/` (`podman-restructured.html/.md`,
`server-overview-restructured.html/.md`). The approved mockup sources are preserved next to the
plan at `docs/plans/0028-podman-docs-restructure/` (`podman-restructured.md`,
`server-overview-restructured.md` and a `docs.go.podmanset.patched` showing the doc-set change),
and both drafts were verified against the real docs renderer before approval.

## Why

The podman page opened mid-story: it assumed a source checkout and a local image build, while the
image every release actually publishes to the GitHub container registry —
`.github/workflows/release.yml` builds and pushes `ghcr.io/yookoala/sandbin` — was documented
nowhere in the repo. A reader with podman and no Go had no documented path to a running instance.
The page also sat last in the server doc set, far from the "Running the server" page it parallels,
under a title ("Run it with podman") that did not match that page's name, and its commands were
written as long wrapped lines with trailing comments.

## Decisions

1. **The page is retitled "Running with podman"** — parallel to "Running the server" — and the
   retitle propagates: the `docSets` server pages list, the hub order, the server summary table
   row, and `sandbin-api.md`'s health-checks pointer all name the new title. `sandbin-serve.md`'s
   "the podman page" link text stays as-is (it matches no title, so nothing to re-pin).
2. **The server doc set order becomes serve, podman, api, ip-blocking**: "Running with podman"
   is listed right after "Running the server" in `cmd/sandbin/docs.go` `docSets`, in the hub, and
   in the summary table, so the two ways of running an instance read as a pair.
3. **First section "Running the Published Image"** documents the registry path directly, with
   facts taken from `.github/workflows/release.yml`: every release publishes the repository's
   `Containerfile` as `ghcr.io/yookoala/sandbin`; each release tag becomes an image tag of the
   same name (`:v0.4.3`); the working `main` branch publishes `:latest`; no source and no Go are
   needed. Its example is `podman pull` plus the full `podman run` (named volume, `--env-file`,
   health probe trio), the env file is cross-referenced to the `SANDBIN_API_KEY` setting
   *Running the server* describes, pulling a named tag pins the container to a release, and the
   quadlet unit's `Image=` line can be retargeted at the registry image to run the published
   image as a service.
4. **Second section "Building & Running from Source"** carries the current page's content,
   sub-segmented into four `###` subsections: **Download the Source** (`git clone` of
   `https://github.com/yookoala/sandbin`, `cd sandbin`, and the hermetic two-stage build note —
   `golang:1.26-alpine` builds, `alpine:3.22` runs, so the host needs nothing but podman, unlike
   the binary source build), **Running** (the exact `localhost/sandbin:latest` tag rule, `make
   container` and the hand `--build-arg SANDBIN_VERSION` build, the empty-stamp fallback to
   `release.txt` or `dev`, and the ad-hoc run expressed as the published image's run with the
   other name), **Health Check** (the OCI `HEALTHCHECK` warning, the podman-native probe, the
   `podman inspect --format` command with its doubled-brace escape **and plan 0026's explanation
   paragraph of that escape** — both must survive the rewrite), and **Running as Systemd
   Service** (the quadlet script, the non-optional `sed` warning about `EnvironmentFile=`
   resolution, loopback-only port rationale, `podman inspect` env visibility).
5. **Command style for this page.** Multi-line commands split one option per line (small
   related groups such as `--health-interval=30s --health-start-period=5s --health-retries=3`
   may share a line); trailing end-of-line comments move to a comment line above the command;
   multi-command script blocks separate commands by empty lines with a comment before each
   saying what it does. The change applies to every fenced block on the page.
6. **The summary table's podman row description is refreshed** to the new coverage: "pulling and
   running the published `ghcr.io/yookoala/sandbin` image, building the image from a source
   checkout, ad-hoc run with volume/env file/health probe, the quadlet systemd service".
7. **Test re-pins only.** The hosted page count stays nine; `wantDocPages` order and the
   `sandbin-podman.html` title entry are re-pointed, and one new structure test pins the podman
   page's exact ordered h2 set (`running-the-published-image`,
   `building--running-from-source`) and h3 set (`download-the-source`, `running`,
   `health-check`, `running-as-systemd-service`), matching the slugs the real renderer produced.
