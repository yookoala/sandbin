# Plan: fixed GitHub door in a new tab, viewport-filling pages with a footer, a path box on the landing

Status: implemented and closed. Spec: `docs/decisions/0024-page-footer-path-box.md` — the three changes and their
limits are settled there. Baseline HEAD is the plan-0023 close-out `a8f94e3` (`cmd/sandbin` 200
tests, `cmd/sandbin-client` 107, `internal/version` 6, `internal/frontpage` 14, `internal/robots` 2,
all gates green).

**Goal:** every top-menu GitHub icon points at `https://github.com/yookoala/sandbin` with
`target="_blank" rel="noopener"`; landing and docs pages fill the viewport height and end with
"Powered by Sandbin | version: <version.String()>"; the landing's `Skill Install Path:` value
sits in its own command-box-style readonly input.

**Architecture:** `docHead`/`docShell` shed the `sourceURL` parameter and gain the fixed GitHub
href and a `<main>` wrapper + footer built from `version.String()`; the shared layout and footer
rules join `brandCSS`; front.html wraps its content in `<main>`, gains the footer from a new
`Data.Version`, and swaps the inline path span for a readonly input. `renderFrontPage` supplies
`Version: version.String()`.

**Tech stack:** Go stdlib only; same `text/template` + static CSS/JS posture as 0023.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Never push; never tag; never touch the existing tags (v0.4.0 belongs to the user).
- Do not touch: bin routes and semantics, listings, `robots.txt` branch, skill routes and
  templates, `SKILL.md`/`install.sh`, lockout, `frontBase`/`frontHostRE`, the picker ordering
  contract (decision 0023 item 4 — pinned order, separator, blur/Enter/arrows/Escape, copy
  flash all stay byte-identical in behavior), the body-link rewriter in docs.go (it keeps using
  `docsSourceBase`), the CLI layer, client, store, `internal/version`, `internal/robots`,
  `scripts/`, `Makefile`, `Containerfile`.
- Intentional diffs against `a8f94e3`, and only these: front.html, frontpage.go,
  frontpage_test.go, handlers.go (`renderFrontPage` gains `Version`), handlers_test.go pins,
  docs.go, docs_test.go, and the plan/decision docs. Every other route — bins round trip,
  listings, 301/404/405 shapes, healthz, robots.txt, skill routes, install.sh, logo — is
  byte-identical. Docs pages and the landing themselves change exactly per the decision record.
- The preserved mockup at `/tmp/opencode/plan0023/` stays untouched; the shipped pages now
  legitimately differ from it in exactly the three decided ways.

## Task 1: the docs shell (docs.go + docs_test.go, TDD)

- `docHead()` (no parameter): GitHub icon `href="https://github.com/yookoala/sandbin"
  target="_blank" rel="noopener"` — same glyph, title, aria-label. `docShell(title, bodyHTML)`
  (sourceURL dropped) wraps the body: `docHead` + `<main>` + bodyHTML + `</main>` +
  `<footer class="site-footer">Powered by Sandbin | version: ` + version.String() + `</footer>`
  before `</body>`.
- `brandCSS` gains: `body` gets `display:flex;flex-direction:column;min-height:100vh` (existing
  rules kept — max-width 44rem, margin auto, padding), `main{flex:1 0 auto}`, and
  `.site-footer{margin-top:auto;padding-top:1.2rem;border-top:1px solid #e3ddd3;color:#666;
  font-size:0.85rem}`.
- Pins: every docs page (hub + 9 members) carries the fixed GitHub href with target/rel, no
  `docsSourceBase` URL in any nav; every page carries the footer with the version line and the
  `<main>` wrapper; the body-link rewriter tests stay green (the `docsSourceBase` body links
  are unchanged).

## Task 2: the landing (front.html + frontpage.go + tests)

- `frontpage.Data` gains `Version string`; `pageData` unchanged otherwise; front.html wraps
  content in `<main>` and appends `<footer class="site-footer">Powered by Sandbin | version:
  {{.Version}}</footer>`; the nav GitHub icon gets `target="_blank" rel="noopener"` (href
  already fixed).
- Path box: replace `<p class="cmd-target">Skill Install Path: <span class="path" id="cmd-path">…`
  with `<p class="cmd-target">Skill Install Path:</p>` plus
  `<div class="cmd"><input class="cmd-input path-input" id="cmd-path" type="text" readonly
  spellcheck="false" autocomplete="off" aria-label="Skill install path" value="{{.DefaultPath}}"></div>`;
  `.path-input{padding-right:0.9em}` (no copy-icon gutter) and the swap-animation selector moves
  from `.cmd-target.swap` to the box (`#cmd-path.swap` or `.path-input.swap`); the picker script's
  path write switches from `textContent` to `value`.
- `renderFrontPage` supplies `Version: version.String()`.
- Pins: frontpage tests — re-pin the old `Skill Install Path: <span…` pin to the new box markup,
  pin the footer line and the fixed GitHub icon with target/rel, pin `{{.Version}}` lands, pin no
  surviving `{{`; handlers tests — the landing still echoes the visitor base in the JSON island,
  and now contains the footer prefix `Powered by Sandbin | version:` (pin the prefix, not the
  build-dependent version value).

## Commits (atomic)

1. `feat: the nav GitHub icon always opens the repository in a new tab` (Task 1's link change +
   landing icon attr).
2. `feat: hosted pages fill the viewport and end with a powered-by footer` (Task 1 shell/footer
   + brandCSS + Task 2 landing footer + Version wiring).
3. `feat: the landing names the install path in a box of its own` (Task 2 path box).

## Verification (implementer)

- Five gates; test-name counts vs baseline (200/107/6/14/2) via `comm` — zero removals;
  additions allowed.
- Live battery vs `git archive a8f94e3` (baseline 127.0.0.1:18306 / HEAD 18307, Host
  `sandbin.example.org` + `X-Forwarded-Proto: https`): diffs only in the landing and docs pages,
  and within them only the three decided changes; everything else byte-identical (bins round
  trip, listing + 301, skill routes, install.sh, healthz, robots.txt, 404/405 shapes, logo).
- Browser check (agent-browser) on HEAD's served pages: landing footer pinned at the viewport
  bottom with short content (viewport taller than content); the GitHub icon opens a new tab at
  exactly `https://github.com/yookoala/sandbin` on landing and on a docs member page; docs member
  page footer present; path box: on a harness change the box shows that row's path with the swap
  animation, select-all still works on the command box, and the picker ordering matrix from 0023
  still passes (spot-run: open-no-typing order, `co`, blur commit); console clean.
- Hostile-host spot check unchanged (placeholder base still lands in the island).
- Cleanup: servers dead (Z counts dead), ports 18306-18307 free, scratch removed,
  `git status --short` only `?? .opencode/`, tags unchanged, nothing pushed.

## Reviewer TODO

1. Gates; `comm` counts vs `a8f94e3`; `git diff a8f94e3..HEAD --name-only` limited to the
   intentional set.
2. Nav audit: grep every served page (landing, hub, 9 members) for the GitHub icon — href is
   the fixed repo URL with `target="_blank" rel="noopener"`; no docs nav carries a
   `blob/main/docs/usages` URL; the body-link rewriter still emits `docsSourceBase` links in
   page bodies (unchanged behavior).
3. Layout audit in a real browser: on landing + hub + one member, with a tall viewport the
   footer sits at the bottom edge (measure footer bounding-box vs viewport); content fills
   the column; footer text is `Powered by Sandbin | version: X` where X equals the binary's
   own `--version` output.
4. Path box: browser-verify the box updates with harness changes (swap class fires), holds the
   exact `Skill Install Path:` value from the island, is readonly, and the 0023 picker matrix
   spot-checks still pass (no-typing order, `co`, zz→generic, blur commits, Escape unchanged).
5. Battery: baseline-vs-HEAD zero diffs outside the landing/docs pages; inside them the diff is
   only the three decided changes — quote the docs-page diff and confirm it is nav link + main/
   footer wrapper + brandCSS block.
6. Mutation probes in a scratch clone (never the real repo): drop `target="_blank"` from
   docHead → a docs test fails; drop the footer from docShell → a test fails; put a per-page
   source URL back into the nav → a test fails; change `.path-input` id or the picker's
   `value=` write in the shipped JS and confirm what the Go tests do and do not catch — a Go
   test must at least pin the shipped markup shape (`id="cmd-path"`, readonly, `value=`).
7. Cleanup confirmation: /proc (Z dead), ports clear, scratch gone, `git status --short` only
   `?? .opencode/`, tags unchanged, nothing pushed.

## Skipped on purpose

Everything in the decision record's Skipped section, plus: dark-mode footer colors, any per-page
"edit this doc" link (the nav door is the only GitHub exit), and X-Robots-Tag (0022's deferred
item stays deferred).

## Outcome

Implemented in `4445c22` (nav door), `46fabb7` (viewport footer) and `94aa7a5` (path box) on
plan-open `bb20f9c`; reviewer verdict APPROVE — no findings, nits only.

- Counts: 202/107/6/15/2 vs baseline 200/107/6/14/2 — zero removals; the additions are
  `TestDocsNavCarriesTheFixedGitHubDoor`, `TestDocsPagesFillTheViewportAndEndInFooter`,
  `TestRenderCarriesThePoweredByFooter`.
- Nav (reviewer-verified on the landing, hub and all ten member pages): the door is the literal
  `<a class="icon-link" href="https://github.com/yookoala/sandbin" target="_blank"
  rel="noopener">` on every page, zero blob/tree URLs in any nav; real clicks open new tabs at
  the repo root. Body cross-link rewriting untouched; the landing's tagline GitHub link stays
  same-tab. The dead `docsTreeURL` const was deleted (its only use was the hub's nav
  sourceURL); `docsSourceBase` stays for the body rewriter.
- Layout (reviewer-measured): with border-box body + `main{flex:1 0 auto}` + footer
  `margin-top:auto`, the footer's bottom edge sits exactly at `innerHeight − 48px` (the body's
  3rem bottom padding) on short and long pages alike, `scrollHeight == innerHeight` for short
  pages — no phantom scroll. Footer text equals the served binary's own `--version` on landing
  and docs alike; test pins take the prefix or fixture versions, never a build-dependent value.
- Path box (reviewer-verified): one readonly `#cmd-path` input styled like the command box
  (monospace, amber left border, 0.9em right padding, swap animation), updated via `value` and
  byte-equal to the island row's path; the 0023 picker matrix re-ran intact; the `.value` write
  itself is JS-only — Go pins cover the markup shape (`id="cmd-path"`, `readonly`, `value=`),
  the browser catches the write.
- Battery: baseline-vs-HEAD byte-identical on healthz, robots.txt, install.sh, SKILL.md, logo,
  404/405 bodies, bin round trip, 301 shape, listing bodies; every docs page diff is exactly
  2 removed/8 added lines (brandCSS block, nav-door swap, main/footer wrapper); the landing's
  JSON island byte-identical (1553 B). Hostile `Host: bad.host!x` → placeholder island, host
  echoed nowhere; `</script>`-bearing Host is refused by Go's parser with 400 (pre-existing).
- Mutation battery (scratch clone): dropping target/rel, dropping the footer, restoring a
  per-page nav source, dropping readonly, and deleting the `.site-footer` rule each fail named
  tests; only the JS `.textContent` mutation sails the Go suite, covered by the browser checks.
- Environment notes: an archive-built binary prints the embedded `v0.3.3` while a tree build
  prints `dev (hash+)` — `internal/version`'s defined precedence, not a 0024 artifact;
  `release.txt` lagging the v0.4.0 tag is pre-existing and out of scope.
