# Plan: the landing page becomes the harness picker; the docs header joins its icon style

Status: open. Spec: `docs/decisions/0023-landing-harness-picker.md` — the copy set, the
`Harness{Label, Cmd, Path}` table, the JSON data island, the verbatim picker behavior, the docs
icon nav and shared `brandCSS` rules are settled there. Baseline HEAD is the plan-0022 close-out
`23041d0` (`cmd/sandbin` 200 tests, `cmd/sandbin-client` 107, `internal/version` 6,
`internal/frontpage` 13, `internal/robots` 2, all gates green).

Design source of truth: the approved mockup, preserved at `/tmp/opencode/plan0023/` —
`landing-final.html` (whole landing) and `docs-hub-final.html` / `docs-member-final.html` (the
docs header). If `/tmp` was wiped, rebuild: serve `23041d0` on a loopback port and curl the
pages with `Host: sandbin.example.org` + `X-Forwarded-Proto: https`, then re-apply the decision
record — do not re-derive the design from scratch.

**Goal:** the real `GET /` renders the approved picker page (combobox sentence, command input
with inner copy icon and Copied flash, "Skill Install Path:" line, new copy set) from
`internal/frontpage`, and the hosted docs' header becomes the same borderless icon nav through
`docHead` + `brandCSS`.

**Architecture:** front.html is rewritten around static picker markup + a JSON data island
(`{{.HarnessJSON}}`); `frontpage.Render` gains `HarnessJSON`/`DefaultCmd`/`DefaultPath`/
`DefaultLabel` assembly (still `text/template`, `Base` still the only address-derived value);
`renderFrontPage` keeps its shape and just supplies the richer Data; `docs.go` rewrites `docHead`'s
nav to three icon links and appends the icon rules to `brandCSS`; usage docs prose follows.

**Tech stack:** Go stdlib (`embed`, `text/template`, `encoding/json`, `strings`); page JS is
plain ES5-style script as in the mockup.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Never push; never tag. Tags v0.1.0..v0.3.3 stay where they are.
- Do not touch: bin routes and semantics, listings, `robots.txt` branch, skill routes and
  templates, `SKILL.md`/`install.sh`, lockout accounting, `frontBase`/`frontHostRE` (byte-
  identical), the CLI layer, client, store, `internal/version`, `internal/robots`, `scripts/`,
  `Makefile`, `Containerfile`.
- Intentional diffs against the `23041d0` baseline, and only these: the landing page body; the
  header nav + shared branding CSS on every `/docs` page; the prose of `docs/usages/sandbin-api.md`
  (its landing sentence and the robots.txt sentence from 0022 must survive) and
  `docs/usages/sandbin-client-skill.md` (tab wording). Everything else — bins, files, listings,
  301/404/405 shapes, healthz, robots.txt, `install.sh`, `SKILL.md`, the other docs bodies — is
  byte-identical.
- The picker's behavior contract is the decision record's item 4, in full: pinned current above
  generic above separator; matches float above the pinned pair while typing; alphabetical below
  the separator; separator whenever anything is below it; first item highlighted; blur commits
  the top; unmatched falls back to generic on top; swap fade on change; select-all on focus;
  inner copy icon; retriggerable pointer-transparent Copied flash; clipboard with `execCommand`
  fallback.
- Commands stay the forms `install.sh` already accepts; every path stays `$HOME/`-anchored (no
  tilde); `{{.Base}}` stays the only dynamic token in the harness data.

## Review Focus

- Fidelity to the mockup: the real served landing (Host forced to `sandbin.example.org`,
  `X-Forwarded-Proto: https`) matches `/tmp/opencode/plan0023/landing-final.html` in behavior —
  run the approved picker scenario matrix against the real page in a real browser — and the docs
  pages match `docs-member-final.html`'s header shape with root-relative hrefs.
- The landing's install commands are the same eight `install.sh` already accepts (fresh install
  proofs per the 0021 battery convention: generic + at least three `--dest` rows end in a real
  skill folder on disk).
- Nothing outside the intentional-diff list moved: baseline-vs-HEAD battery elsewhere is zero
  diffs.
- The JSON island carries commands with quotes intact (`--dest "$HOME/…"` round-trips through
  `JSON.parse`), the initial `value` attribute uses `&quot;`, and no raw `{{` token survives
  rendering; hostile hosts still land on the `this.host` placeholder.

## Task 1: the frontpage package (TDD)

**Files:** rewrite `internal/frontpage/front.html`; modify `internal/frontpage/frontpage.go`
(`Harness`, `DefaultHarnesses`, `Data`, `Render`); rewrite the pins in
`internal/frontpage/frontpage_test.go`.

**Interfaces:**
- `Harness{Label, Cmd, Path string}`; `DefaultHarnesses()` per decision item 2 (generic first,
  label `a generic harness`, no `--dest`, Paths per the record).
- `Data{Base, Logo, BrandCSS string; Harnesses []Harness}` unchanged in shape; `Render` computes
  and exposes to the template, on a copy: `HarnessJSON` (JSON array of
  `{label, cmd, path}` with baseToken filled), `DefaultLabel`, `DefaultCmd` (first row, base
  filled, `"` → `&quot;`), `DefaultPath` (first row Path).
- front.html: mockup structure — icon nav (book `/docs`, GitHub icon link; logo unchanged as
  home link), new title/h1/learn-more/h2/paragraphs, `pick` combobox markup with empty
  `pick-list`, `<div class="cmd">` input (`value="{{.DefaultCmd}}"`) + inner copy button +
  flash span, `cmd-target` seeded from `{{.DefaultPath}}`,
  `<script id="harness-data" type="application/json">{{.HarnessJSON}}</script>`, then the picker
  script ported verbatim from the mockup with `SANDBIN_HARNESSES = JSON.parse(...)`. Landing CSS
  = mockup landing CSS minus the icon rules (they move to brandCSS in Task 3 — until Task 3
  lands the landing may carry them locally; Task 3 must remove the local copy and the pages stay
  identical).
- Tests first: package pins for the JSON island (quotes survive, base filled, eight rows),
  the copy set (`This is a Sandbin instance`, `Tell Your Agent to use This`, the three lead
  sentences, `Skill Install Path:`), default attributes, no surviving `{{`, determinism, hostile
  `Base` passthrough. Handlers pins are Task 2.

## Task 2: handlers wiring + landing test pins

**Files:** `cmd/sandbin/handlers.go` (only if `renderFrontPage` needs the new Data fields —
`DefaultHarnesses()` already supplies rows), update pins in `cmd/sandbin/handlers_test.go`
(`TestFrontPageAtRoot`, `TestFrontPageEchoesTheVisitorsAddress` — the visitor address must now
appear in the JSON island, not a tab block).

**Interfaces:** `renderFrontPage` unchanged except supplying `Harnesses: frontpage.DefaultHarnesses()`
as today; the Execute-failure path unchanged. No new logic.

## Task 3: the docs header

**Files:** `cmd/sandbin/docs.go` (`docHead`, `brandCSS`), `cmd/sandbin/docs_test.go` pins;
remove the landing-local icon CSS added in Task 1.

**Interfaces:**
- `docHead` nav becomes `<nav class="icon-nav">` with home icon (`href="/"`, aria-label
  "Sandbin home"), book icon (`href="/docs"`, "Documentation"), GitHub icon (the page's own
  `sourceURL`, "Source on GitHub") — glyphs exactly as in `docs-member-final.html`.
- `brandCSS` gains the icon rules: `.page-head nav.icon-nav{margin-left:auto;display:flex;
  gap:0.55rem;align-items:center}`, `.page-head nav.icon-nav a{margin-right:0}`, `.icon-link{…}`,
  hover/focus-visible, `.icon-link svg{…}` — matching the mockup's appended block.
- Docs pages keep root-relative hrefs everywhere.

## Task 4: usage docs prose

**Files:** `docs/usages/sandbin-api.md` (landing description sentence only — the 0022 robots.txt
sentence survives), `docs/usages/sandbin-client-skill.md` (tab-row wording → combobox picker,
`Skill Install Path:` line, copy flash). Voice per each page; keep cross-links intact.

## Commits (atomic)

1. `feat: the landing page is a harness picker with an input-based command widget` (Tasks 1–2).
2. `feat: the hosted docs header joins the landing's icon style` (Task 3).
3. `docs: usage pages describe the harness picker landing` (Task 4).

## Verification (implementer, after all tasks)

- All five gates; per-package test-name counts vs baseline (200/107/6/13/2) with `comm`: zero
  removals; frontpage test count may grow, others unchanged unless a pin moved with the design.
- Install proofs: fresh-container or scratch-HOME installs of the generic command and three
  `--dest` rows (OpenCode, Claude Code, Windsurf) against the HEAD server's `install.sh`; each
  lands the skill folder named on the page's path line.
- Browser matrix against the real served landing (Host `sandbin.example.org`, XFP https so the
  page echoes `https://sandbin.example.org`, mirroring the mockup): run the full scenario matrix
  from the decision record (open no-query order, `co`, `zz`, pinned-current cases, separator
  absence at `c` with Windsurf, blur commits, Enter/arrows/Escape, select-all on focus, copy
  flash retrigger, swap animation) and report each result.
- Additivity battery vs a `git archive 23041d0` server (ports 18306 baseline / 18307 HEAD,
  matched Host headers): landing diff is the redesign; docs pages diff only in nav + shared CSS;
  `sandbin-api.html`/`sandbin-client-skill.html` also diff in body prose; the other docs bodies,
  bins round trip + listing + 301, skill routes, healthz, robots.txt, 404/405 shapes identical.
- Hostile-host spot checks: bad Host values still yield the placeholder base inside the JSON
  island (no raw injection into the script element; JSON escaping must defang `</script>`).
- Cleanup: /proc scan (treat `Z` as dead), ports 18306-18308 clear, `/tmp/opencode` scratch
  removed except `plan0023/` and pre-existing files, repo clean apart from `?? .opencode/`,
  tags unchanged.

## Reviewer TODO

1. Gates; counts via `comm`; `git diff 23041d0..HEAD --name-only` limited to: front.html,
   frontpage.go, frontpage_test.go, handlers.go (if touched), handlers_test.go, docs.go,
   docs_test.go, the two usage md files, plan/decision docs. Anything else is a finding.
2. Mockup fidelity: diff the HEAD-rendered landing against `/tmp/opencode/plan0023/
   landing-final.html` — after swapping `{{…}} tokens back (JSON island → the inline array is
   allowed as the known structural difference), the behavior-bearing markup (nav, copy set,
   pick markup, cmd widget, flash, script functions) must be equivalent; re-run the full browser
   scenario matrix yourself against the real page. Quote each scenario result.
3. Battery: baseline-vs-HEAD diff everywhere outside the intentional list is zero; quote the
   docs-page diffs and confirm they are exactly nav + CSS (+ prose for the two rewritten pages);
   the 0022 robots.txt sentence and route survive.
4. JSON island integrity: commands with `--dest "$HOME/…"` round-trip through the served island
   (`JSON.parse` + copy → clipboard byte-equal to the command shown); a hostile Base cannot
   break out of the script element (try `</script>`-bearing Host headers live); `Content-Type`
   of the landing unchanged (`text/html; charset=utf-8`).
5. Install proofs: run the generic + three `--dest` commands from the served page against the
   HEAD server in a scratch HOME; the installed folders match the page's `Skill Install Path:`
   lines.
6. docs_test/docs battery: confirm docHead's three icons on every docs page, per-page GitHub
   sourceURL preserved, root-relative hrefs, and no other docs body changed (compare rendered
   bodies pre/post nav-strip).
7. Mutation battery in a scratch clone (never the real repo): break the pinned group (drop the
   generic pin), break the alphabetical rest, remove the separator, retitle the flash to the
   button, re-add tilde paths — each must fail a named frontpage or handlers test; report what
   JS-only behavior mutations tests cannot catch and confirm the browser matrix covers those.
8. Container proof: build from HEAD, serve on 127.0.0.1:18308, landing + one docs page +
   install.sh byte-consistent with the tree server; full podman cleanup per the keep-list.
9. Cleanup confirmation: /proc (Z dead), ports clear, `/tmp/opencode` tidy, `git status --short`
   only `?? .opencode/`, tags unchanged, no push.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: localStorage remembering of the
picked harness across visits, keyboard type-to-select on the closed face, dark-mode styling, and
any `X-Robots-Tag` follow-up (0022's deferred item stays deferred).
