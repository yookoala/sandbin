# Plan: the landing page moves to an embedded template in internal/frontpage

Status: implemented and closed. Spec: `docs/decisions/0020-frontpage-internal-template.md` — the new package,
its Data shape, what stays in `cmd/sandbin`, and the engine choice are settled there. Baseline
HEAD is the plan-0019 close-out `0aff529` (`cmd/sandbin` 199 + 29, `cmd/sandbin-client` 107 + 28,
`internal/version` 6 + 35, all gates green). This is a rendering-only refactor of the 0018 landing
page: served bytes must not change.

**Goal:** `internal/frontpage` owns the landing page as `//go:embed front.html` parsed once with
`text/template` (`Data{Base, Logo, BrandCSS}`); `renderFrontPage` stops concatenating and
replacing and just executes the template.

**Architecture:** new package `internal/frontpage` (front.html + frontpage.go + frontpage_test.go);
`cmd/sandbin/handlers.go`'s `renderFrontPage` loses the `pageTemplate` const, the `NewReplacer`
and the header concat, keeps `frontBase`/`frontHostRE` and the `brandCSS` (from `docs.go`) and
`sandbin.LogoSVG` injections as data, and mirrors the skill routes' Execute-failure pattern (log
+ 500 `render failed`). `docs.go` untouched.

**Tech stack:** Go stdlib only (`embed`, `text/template`).

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- The served landing page is byte-identical to the baseline across the whole Host/XFF/TLS matrix
  — every existing front-page pin in `handlers_test.go` stays untouched and keeps passing.
- Do not touch: `frontBase`, `frontHostRE` (byte-identical), `docs.go` (`brandCSS`, `logoBlock`,
  `docHead`, `docShell`, the /docs battery), bin routes and semantics, listings, 404 fallback,
  skill routes and templates, SKILL.md/install.sh, the CLI layer, `client.go`/`config.go`/store,
  `internal/version`, `scripts/`, `Makefile`, `Containerfile`, every operational error wording
  (the failure path reuses the existing `render failed` 500 wording verbatim).
- `text/template` (not `html/template`), parsed once at init via `template.Must`; no new
  escaping anywhere; the only dynamic value is `Data.Base`, already guarded by `frontHostRE`.
- front.html reproduces the current markup byte-for-byte, including the header block and the
  `<div class="wordmark wordmark-hero">{{.Logo}}</div>` wrapper matching
  `logoBlock("wordmark wordmark-hero")`.
- Never push; never tag the real repo. Tags v0.1.0..v0.3.3 stay where they are.

## Review Focus

- Byte identity is the whole review: baseline binaries vs HEAD binaries, landing page identical
  across ordinary hosts, odd hosts (placeholder), X-Forwarded-Host/Proto combinations — zero
  diffs; and every other route trivially identical since nothing else is touched.
- The new package's tests genuinely exercise the template (no surviving `{{` token, logo inside
  the hero wrapper, base inside the install command, determinism), and no front-page pin in
  `handlers_test.go` was edited.
- `frontBase`/`frontHostRE` byte-identical; the Execute-failure path is the existing pattern,
  unreachable under the battery.

## Task 1: the package and the wiring

**Files:** create `internal/frontpage/front.html`, `internal/frontpage/frontpage.go`,
`internal/frontpage/frontpage_test.go`; modify `cmd/sandbin/handlers.go` (`renderFrontPage` only)

**Interfaces:**
- Produces: `frontpage.Data struct{ Base, Logo, BrandCSS string }`; package-level
  `var pageTemplate = template.Must(template.New("front").Parse(frontHTML))` over
  `//go:embed front.html`; `func Render(w io.Writer, d Data) error`.
- front.html: the current page body verbatim, with `<!--brand-css-->` → `{{.BrandCSS}}`,
  `<!--brand-head-->` → the literal header block (`<header class="page-head">` … with
  `<div class="wordmark wordmark-hero">{{.Logo}}</div>` … `</header>`) reproduced from
  `renderFrontPage`'s current concat byte-for-byte, and `https://this.host` in the install
  command → `{{.Base}}`. No other token appears in the file.
- `renderFrontPage`: build `frontpage.Data{Base: s.frontBase(r), Logo: sandbin.LogoSVG,
  BrandCSS: brandCSS}`; `var buf bytes.Buffer`; `if err := frontpage.Render(&buf, d)` → same
  handling as the skill routes (`log.Printf("render %s: %v", ...)` + `http.Error(w, "render
  failed", 500)`); else set `Content-Type: text/html; charset=utf-8` and write the buffer.
  Delete the const template, the NewReplacer and the head concat. `frontBase`, `frontHostRE`
  and everything else in handlers.go untouched.
- Tests first (TDD): the frontpage battery fails before the package exists, then passes;
  handlers_test.go edits: NONE expected (its pins must pass against the new rendering unchanged).

- [ ] Step 1: write internal/frontpage/frontpage_test.go (init parse; Render fills Base/Logo/
  BrandCSS with zero `{{` left; hero wrapper exact; install command line exact; two Renders
  byte-equal) — confirm they fail (no package).
- [ ] Step 2: front.html (extracted from the current const, markers swapped per the Interfaces)
  + frontpage.go; rewire renderFrontPage; green — the untouched handlers_test front pins passing
  unchanged is the byte-identity proof.
- [ ] Step 3: gates + commit `refactor: the landing page renders from an embedded template in internal/frontpage`.

## Verification (implementer, after Task 1)

- All five gates; counts per package before/after with `comm` on test-name lists (baseline
  above; additions only in the new package; zero removals anywhere); `go test ./...` now lists
  `github.com/yookoala/sandbin/internal/frontpage`.
- Side-by-side vs baseline binaries (`git archive 0aff529` under /tmp/opencode): full battery —
  landing under plain Host, example.com Host, X-Forwarded-Host (trusted/untrusted),
  X-Forwarded-Proto https, hostile hosts (`bad host<!<script>`, path-bearing, empty) — landing
  body byte-identical on every case; plus the rest of the surface (bins, docs pages, skill
  routes, healthz, 405/404 shapes) identical. Quote: zero diffs expected.
- Diff `frontBase` + `frontHostRE` region old-vs-new: byte-identical.
- Cleanup: kill servers via /proc scan (`*sandbi` basename prefix — comm truncates 16-char
  names), ports 18303-18305 clear, `/tmp/opencode` EMPTY, repo clean apart from `?? .opencode/`,
  tags unchanged.

## Reviewer TODO

1. Gates yourself; counts vs baseline; comm — additions only, in the new package; zero
   handlers_test.go changes (`git diff 0aff529..HEAD -- cmd/sandbin/handlers_test.go` empty).
2. Scope: `git diff 0aff529..HEAD --name-only` = the three new files + handlers.go only;
   `docs.go`, skill templates, client, version, scripts, Containerfile untouched; the
   `frontBase`/`frontHostRE` region of handlers.go byte-identical; the 500 failure path reuses
   the skill routes' exact existing wording.
3. Byte-identity battery vs baseline binaries: the landing page across the whole host matrix
   (ordinary, odd-host placeholder cases re-run from the frontBase battery's host values,
   XFF/XFP combos) AND the full rest-of-surface battery — zero diffs expected anywhere. Quote
   the diff (empty).
4. Template hygiene: front.html vs the old const pageTemplate — after swapping the three markers
   back (`{{.BrandCSS}}`→`<!--brand-css-->` etc.) the files are textually the old page; rendered
   output carries no `{{` token; `{{.Logo}}` lands inside the exact hero wrapper; text/template
   confirmed (grep imports).
5. New-package tests: run them yourself; confirm they fail meaningfully if front.html is mutated
   in a scratch copy (e.g. drop a token) — proof they actually bind the rendering.
6. Container proof: `podman build --network=host` from HEAD (the embed compiles inside the
   image), run on -p 127.0.0.1:18306:8080 with the standard volume convention, landing byte-equal
   to the tree server's, then full cleanup per the keep-list (golang:1.26-alpine, alpine:3.22,
   alpine:latest; do not prune layers beyond the ones you created).
7. Cleanup confirmation: /proc scan clean of live sandbin, ports clear, `/tmp/opencode` empty,
   `git status --short` only `?? .opencode/`, `git tag -l` unchanged.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: `go mod`/dependency changes (none
needed), caching the rendered page per host, and any behavior change to the docs shell that would
share the template.

## Outcome

Implemented in one commit `d523216` on plan-open `d5ec4d5`. Reviewer verdict: APPROVE WITH
FIXES — one follow-up-grade finding (wire framing, recorded in the decisions record), all
functional claims verified.

- Counts: `cmd/sandbin` 199+29, `cmd/sandbin-client` 107+28, `internal/version` 6+35 — all
  unchanged; new `internal/frontpage`: 6 tests; zero test-name removals.
  `git diff 0aff529..HEAD -- cmd/sandbin/handlers_test.go` is empty: the unchanged front-page
  pins passing against the new rendering is the byte-identity proof.
- Scope: handlers.go diff is exactly two hunks (import + `renderFrontPage`); `frontHostRE` and
  the `frontBase` region `cmp`-identical (271 / 1201 bytes); docs.go, skill templates, client,
  version, scripts, Containerfile absent. Failure path is the skill routes' pattern verbatim.
- Reviewer's own battery (HEAD vs `0aff529` binaries, 20 raw-socket Host cases + 36-surface
  battery + a separate 52-probe bad-key walk): headers and dechunked bodies diff-clean
  everywhere — zero content diffs including the hostile-Host 400s, placeholder echoes,
  multi-entry XFF, XFP https combos, docs pages, skill routes with the 0019 pin, bins, 301s,
  405/404 shapes and the lockout walk.
- Template hygiene: `front.html` holds exactly three actions (`{{.BrandCSS}}`, `{{.Logo}}` in
  the exact hero wrapper, `{{.Base}}` in the install command); swapped back they reproduce the
  old pageTemplate const byte-for-byte plus the documented blank line after `</header>`
  (2240 == 2240; the implementer's initial "2239 == 2239" pairing was imprecise — corrected in
  the decisions record). Mutation proofs in scratch copies made named frontpage tests fail and
  only those.
- Review finding (recorded, not reverted): `w.Write(buf.Bytes())` vs the old `io.WriteString`
  changes only Go's chunked-transport framing of the landing body (one chunk vs 2 KiB chunks);
  status line, headers and decoded body are identical, `Content-Length` is absent both ways, and
  no client or test can observe framing. The decisions record carries the accepted deviation and
  the honest phrasing "headers and dechunked bodies diff-clean".
- Container proof: image built from HEAD stamps `v0.3.3+ (d523216)`, serves the landing
  wire-identically to the tree server (18225 == 18225 raw bytes), healthz and the pinned
  install.sh; full podman cleanup per the keep-list.
- Limits accepted: untrusted-socket XFF and the TLS-socket branch are pinned by unchanged test
  rows (every live socket in the sandbox is loopback-trusted; no TLS listener); the frontBase
  region those rows bind is byte-identical.
