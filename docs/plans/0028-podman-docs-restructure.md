# Plan: podman page restructure — published image first, source build second, serve-pair ordering

Status: open — implemented at commit `618d472`, awaiting review. Spec:
`docs/decisions/0028-podman-docs-restructure.md` — its seven items settle
the retitle and its propagation, the serve→podman→api→ip-blocking order, the new
"Running the Published Image" section with its `ghcr.io/yookoala/sandbin` facts, the
"Building & Running from Source" section with its four h3 subsections, the one-option-per-line
command style, the refreshed summary-table row, and the test re-pins. Baseline HEAD is `4c8e5ab`
(`cmd/sandbin` 213 tests, `cmd/sandbin-client` 107, `internal/frontpage` 17, `internal/robots` 2,
`internal/version` 6, all gates green). The implementation commit `618d472` carries
`docs/usages/sandbin-podman.md` and `docs/usages/sandbin.md` byte-for-byte from the two preserved
mockups, the api page's one pointer-line retitle, the `docSets` server-set swap, the
`wantDocPages` order and podman title re-pins, and one new structure test
`TestDocsPodmanPagePutsPublishedImageFirst`; its counts are `cmd/sandbin` 214,
`cmd/sandbin-client` 107, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6, with
`gofmt -l .` empty and `go vet ./...`, `go build ./...`, `go test -count=1 ./...` and
`go test -race -count=1 ./...` all green.

Design source of truth: the approved mockups, preserved at `docs/plans/0028-podman-docs-restructure/`
— `podman-restructured.md` and `server-overview-restructured.md` are the exact final texts of
the two rewritten pages (both went through the real renderer; preview copies at
`https://preview.yookoala.dev/951c2bcb/`), and `docs.go.podmanset.patched` shows the `docSets`
swap. One small content retarget is NOT in the preserved drafts' own files and must be applied
to the live page: `docs/usages/sandbin-api.md`'s health-checks pointer (line ~352) becomes
`see [Running with podman](sandbin-podman.md)` — a title-only edit on that one line, nothing
else in that file.

**Goal:** the hosted server docs become the approved mockups — `sandbin-podman.md` ("Running
with podman") first rewritten as the published-image section plus the source-build section with
its four subsections and option-per-line commands, `sandbin.md` with the podman row retitled,
redescribed and moved second in the table, the hub and `docSets` in serve→podman→api→ip-blocking
order, the api pointer retitled, and every test gate green with no removed pins.

**Architecture:** one content commit. The docs pipeline (plan 0026) is untouched — these pages
already ride it; only the embedded markdown tree, the `docSets` server pages literal, and the
test pins change. A content commit and its test re-pins land together because the pins name the
pages and their order.

**Tech stack:** existing gomarkdown + template pipeline; no code paths change beyond the
`docSets` pages literal.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Never push; never tag; tags v0.1.0..v0.4.3 stay where they are.
- Do not touch: `cmd/sandbin/docs.go` beyond the one `docSets` server-set `pages` literal, the
  docs pipeline code (template pass, copy script, shells), bin routes and their rendering,
  landing/frontpage, skill routes/templates, the `robots.txt` handler, lockout/store/CLI
  behavior, `cmd/sandbin-client` code, `internal/*` packages, `scripts/`, `Makefile`,
  `Containerfile`, `deploy/`, other `docs/usages/` pages except the single api pointer line,
  and the plan/decision records under `docs/plans/0028-podman-docs-restructure/`.
- Test pins: baseline `cmd/sandbin` 213 — pins may be re-pointed at the new title and order,
  none deleted; the new structure test adds pins. `cmd/sandbin` count may grow, never shrink.
  The hosted page count stays nine, so the "nine" comments stay true.
- The two rewritten pages must be byte-identical to their preserved mockups.
- The ghcr facts in the new section must match `.github/workflows/release.yml` (registry name,
  tag-from-tag / `latest` from main) — they were verified against it during mockup drafting.

## Implementer TODO

1. Land the content, byte-for-byte from the preserved drafts:
   a. `docs/usages/sandbin-podman.md` ← `podman-restructured.md` (replaces the current page);
   b. `docs/usages/sandbin.md` ← `server-overview-restructured.md`;
   c. retarget `docs/usages/sandbin-api.md`'s health-checks pointer title only (the sole edit
      to that file);
   d. `cmd/sandbin/docs.go` server set becomes
      `[]string{"sandbin-serve.html", "sandbin-podman.html", "sandbin-api.html", "sandbin-ip-blocking.html"}`
      (the file must then equal `docs.go.podmanset.patched`).
2. Re-pin `cmd/sandbin/docs_test.go`: `wantDocPages` moves `"sandbin-podman.html"` to right
   after `"sandbin-serve.html"`; `wantDocTitles["sandbin-podman.html"]` becomes
   `"Running with podman"`. The hub-order and title assertions then follow. Re-point any
   summary-table or cross-link wording pin that quotes the old podman row or old pointer title
   (re-point truthfully, never delete, never weaken).
3. Add one new structure test (patterned on `TestDocsClientInitPageCarriesTheMergedSetup` and
   using the `docHeadingIDs` helper) pinning the podman page: its `<title>Running with podman`,
   the exact ordered h2 id set `running-the-published-image building--running-from-source`, the
   exact ordered h3 id set `download-the-source running health-check running-as-systemd-service`,
   and that the served body carries the literal single-brace line
   `podman inspect --format '{{.State.Health.Status}}' sandbin` (the mockup's doubled-brace
   escape must render to exactly one brace pair) plus a sentence naming the escape ("doubled
   braces").
4. Run the gates; record the new per-package counts in the plan record's Status line (second
   commit allowed: "docs: record plan 0028 implementation counts"; do NOT close the plan — the
   reviewer goes first).
5. Sanity-serve per the mockup loop on `127.0.0.1:18307` (scratch build `-buildvcs=false`,
   temp db + key, `Host: preview.yookoala.dev` + `X-Forwarded-Proto: https`): the hub lists
   serve→podman→api→ip-blocking; the podman page's `<title>`/h1 read "Running with podman"; its
   ghcr pull/run block renders with the option-per-line backslash continuations intact and
   exactly one `{{` pair (the health-status literal, not a stray token); the summary table shows
   the podman row second with its new description; the api page's pointer reads "Running with
   podman"; zero stray `{{` on all nine pages. Kill ONLY your scratch process (match the scratch
   binary name) and remove all scratch files.

Commit message shape: `docs: podman page restructured — published image first, source build second`.

## Reviewer TODO (independent of the implementer's reading)

1. Read `docs/decisions/0028-podman-docs-restructure.md` items 1–7 and verify each against the
   committed files, not the diff narrative. `cmp` the two rewritten pages against the preserved
   mockups (byte-identical); `git diff 4c8e5ab..HEAD` for `sandbin-api.md` shows exactly the one
   pointer-line title change; `diff cmd/sandbin/docs.go
   docs/plans/0028-podman-docs-restructure/docs.go.podmanset.patched` is empty.
2. Fact-survival audit against the superseded page (`git show 4c8e5ab:docs/usages/sandbin-podman.md`):
   every fact must land in the new page — the exact-tag rule with its pull-otherwise warning,
   `make container` + `scripts/version.sh` + what the container prints, the by-hand build, the
   empty-stamp fallback (`release.txt` tag else `dev`), the OCI HEALTHCHECK warning with the
   `--format docker` alternative, the inspect command, the doubled-brace escape AND its plan-0026
   explanation paragraph ("In a plain terminal you type podman's own braces…"), the quadlet
   script's six steps, the `sed`-not-optional warning with its restart-loop reasoning and the
   system-install alternative, the loopback-only port rationale with its edit hint, and the
   `podman inspect` `Config.Env` warning. Enumerate them; a missing one is a FAIL.
3. Ghcr facts audit against `.github/workflows/release.yml`: the registry path
   `ghcr.io/yookoala/sandbin`, tag-push→tag image and main→`:latest` semantics, and the page's
   claim that the image needs "no source and no Go" — all true against the workflow and
   `Containerfile`; also verify the quadlet `Image=` retarget note is consistent with
   `deploy/sandbin.container`.
4. Command style audit of every fenced block on the new page: one option per line except
   decided shared groups, no trailing comments inside blocks, multi-command blocks separated by
   empty lines with a preceding comment, and the env-file/health-trio run command matches the
   user-approved shape from the decision conversation.
5. Run your own scratch server on `127.0.0.1:18307`: hub order, podman title, the single
   rendered `{{.State.Health.Status}}` pair (and that `grep -c '{{'` on the served podman page
   is exactly 1), zero stray tokens on the other eight pages, summary table row second with the
   new description, api pointer retitled, and `GET /docs/sandbin-serve.html` still links
   "the podman page" to `sandbin-podman.html` unchanged.
6. Tests: run all five gates yourself; compare the `cmd/sandbin` test-function inventory at
   4c8e5ab vs HEAD — none deleted, no assertion weakened (read the `docs_test.go` diff fully);
   the new structure test asserts exact ordered id sets (not shadows) and the brace-literal
   line; recorded counts match `go test -count=1 <pkg> -v | grep -c '^--- PASS'` reality;
   `git diff 4c8e5ab..HEAD --name-only` touches nothing outside `docs/usages/`,
   `cmd/sandbin/docs.go`, `cmd/sandbin/docs_test.go` and the plan/decision records.
7. Pipeline untouched: `git diff 4c8e5ab..HEAD -- cmd/sandbin/docs.go` shows only the server-set
   `pages` line; copy buttons still on docs pages and still absent from the landing page;
   bin-served bytes still never template (PUT a file containing `{{ .Instance.URL }}`, GET it
   back byte-identical).
