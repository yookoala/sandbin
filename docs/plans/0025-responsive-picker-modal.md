# Plan: a responsive picker modal, a warm swap tint, and the landing copy retitle

Status: implemented and closed. Spec: `docs/decisions/0025-responsive-picker-modal.md` — its nine items settle
the media query, modal box model, scrim, swap-background fade, path-line retitle, mobile "I
use" hint + placeholder, the closing paragraph, and the tagline retitle. Baseline HEAD is
`348b102` (`cmd/sandbin` 202 tests, `cmd/sandbin-client` 107, `internal/version` 6,
`internal/frontpage` 15, `internal/robots` 2, all gates green).

Design source of truth: the approved mockup, preserved at `/tmp/opencode/plan0025/
landing-modal-approved.html` (a HEAD render of the landing with exactly the decided changes;
preview copy at `https://preview.yookoala.dev/951c2bcb/landing-modal.html`). Port the changes
as targeted edits onto `internal/frontpage/front.html`'s token template — do NOT paste the
mockup wholesale: the mockup carries rendered values (`$HOME/...` paths, `sandbin.example.org`
commands, `dev (348b102+)`) where the template carries `{{.DefaultPath}}`, `{{.DefaultCmd}}`,
`{{.Version}}` etc.

**Goal:** the served landing renders the approved mockup: the picker opens as a near-fullscreen
modal under a 30rem media query (scrim, "I use" hint, placeholder), every harness swap flashes
a warm tint that fades over 1.4s, and the copy set is retitled ("Skill will be installed to:",
the closing `sandbin-preview` paragraph, the new tagline).

**Architecture:** all changes are landing-local — front.html's markup, `<style>` block, and
picker script; no Go-logic changes. Usage-doc prose follows the path-line retitle.

**Tech stack:** Go stdlib templates as before; plain CSS (one media query, one keyframe) and
the existing ES5-style script with one-line scrim toggles.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Never push; never tag; tags v0.1.0..v0.4.1 stay where they are.
- Do not touch: bin routes, listings, `robots.txt` branch, skill routes/templates, `SKILL.md`,
  `install.sh`, lockout, `frontBase`/`frontHostRE`, `frontpage.go` (no Data/Render change
  needed), `renderFrontPage`, `docs.go`/`brandCSS` (all new CSS stays in front.html's local
  style), the picker ordering contract (decision 0023 item 4), the CLI layer, client, store,
  `internal/version`, `internal/robots`, `scripts/`, `Makefile`, `Containerfile`.
- Intentional diffs against `348b102`, and only these: `internal/frontpage/front.html`,
  `internal/frontpage/frontpage_test.go`, `cmd/sandbin/handlers_test.go` (copy-set re-pins),
  `docs/usages/sandbin-api.md`, `docs/usages/sandbin-serve.md`, `docs/usages/
  sandbin-client-skill.md` (path-line retitle wording only), and the plan/decision docs.
  Docs pages, bins, healthz, robots.txt, 301/404/405 shapes, install.sh, logo — byte-identical.
- The behavior contract from decision 0023 item 4 stays intact at every viewport; the new
  scrim-click commit is only the existing `focusout`-commit path reached by a click on the
  scrim (scrim is not focusable), and the decision-0025 items 1-4 modal box model exactly as
  the mockup carries it.

## Task 1: responsive modal (front.html + test pins)

Verbatim from the mockup: `.pick-scrim{display:none}`, `.pick-hint{display:none}`,
`.pick-pop input::placeholder{color:#8b8171;font-weight:400}` and the full
`@media (max-width:30rem){...}` block; the `<span class="pick-scrim" id="pick-scrim" hidden>`
inside `#pick`; `<span class="pick-hint" aria-hidden="true">I use</span>` as the first child of
`#pick-pop`; `placeholder="input harness name"` on `#pick-input`; `pick-scrim` hidden-toggles
in `sandbinOpenPick`/`sandbinClosePick` only. Pins: presence of the media-query rule, scrim
and hint markup, placeholder attribute, and both scrim toggles in the two functions.

## Task 2: swap tint (front.html + test pin)

`@keyframes sandbin-swapbg{0%{background-color:#f7e7cb}100%{background-color:#fff}}` and the
animation selector extended to `animation:sandbin-swap 0.28s ease-out,sandbin-swapbg 1.4s
ease-out`. Pin: the keyframe name and the extended selector present.

## Task 3: copy retitle (front.html + re-pins + prose)

Tagline paragraph becomes "A small preview service for AI agents to show you HTML+JS+CSS
interactive preview / presentation or ANSI design mock up."; "Skill Install Path:" becomes
"Skill will be installed to:" (the `#cmd-path` box and its aria-label stay); after the path
box: "Then tell your agent to use the skill `<code>sandbin-preview</code>` to show you HTML /
ANSI preview here." Re-pin `TestRenderCarriesTheApprovedCopySet` and the `TestFrontPageAtRoot`
copy-set list (old tagline string and old path-line string are decided removals — the ONLY
pin-content edits allowed; test names unchanged). Prose: `sandbin-api.md`, `sandbin-serve.md`,
`sandbin-client-skill.md` — replace only the quoted "Skill Install Path:" wording (and any
quoted old tagline if present); keep cross-links and everything else intact.

## Commits (atomic)

1. `feat: the harness picker opens as a modal on small screens` (Task 1).
2. `feat: a harness swap tints the swapped widgets before settling` (Task 2).
3. `feat: the landing copy pitches the preview promise and the closing step` (Task 3 landing).
4. `docs: usage pages follow the landing path-line retitle` (Task 3 prose).

## Verification (implementer)

- Five gates; test-name counts vs baseline 202/107/6/15/2 via `comm` — zero removals (content
  re-pins are fine); additions allowed.
- Mockup fidelity: serve HEAD with Host `sandbin.example.org` + `X-Forwarded-Proto: https`,
  diff the served landing against the preserved mockup — after normalizing only the template
  tokens (island, default values, version stamp) the diff must be empty.
- Browser matrix (agent-browser), on the served landing, both viewports: desktop 1280×900 —
  picker anchored dropdown, scrim display none, hint none, 0023 ordering matrix spot-run
  (no-typing order, `co`, Escape), swap: early background sample warm (`rgb(247,231,203)`),
  white after 1.6s; phone 390×844 — `position:fixed` panel top 51/bottom 51/side 12, full-width
  input with `I use` hint above it and the muted placeholder, list fills the rest, scrim click
  commits the highlighted row and closes (picker still functional afterward), row click picks
  and closes the scrim, separator inert, swap tint fires in modal too; console clean.
- Copy proofs: served page contains the three new strings and none of the two old ones; usage
  docs contain no stale "Skill Install Path:".
- Battery vs `git archive 348b102` (18306 baseline / 18307 HEAD, matched Host+XFP): landing
  diff = the decided changes only; every docs page and every other route byte-identical; JSON
  island unchanged. Hostile-host spot check unchanged (placeholder island).
- Cleanup: servers dead (Z dead), ports 18306-18307 free, scratch removed (keep
  `/tmp/opencode/plan0025/`), `git status --short` only `?? .opencode/`, tags untouched,
  nothing pushed.

## Reviewer TODO

1. Gates; `comm` counts vs `348b102`; `git diff 348b102..HEAD --name-only` limited to the
   intentional set.
2. Mockup fidelity yourself: served-HEAD landing vs the preserved mockup (token-normalized
   diff empty or justified line-by-line), plus re-run the full two-viewport browser matrix
   (ordering contract, scrim commit, Escape, hint/placeholder, swap tint sampling in BOTH
   modes) and quote each result.
3. Contract audit: the 0023 item-4 matrix at desktop width AND in modal mode (pinned pair,
   separator, matches float, blur-equivalent commit via scrim, highlighted-row commit); the
   focusout handler is unchanged (scrim commit works because the scrim is not focusable).
4. Copy: no "Skill Install Path:" or old-tagline string remains on the served landing or in
   docs/usages; the new closing paragraph matches the mockup verbatim; the `#cmd-path` input
   keeps `readonly` and its aria-label.
5. Battery: baseline-vs-HEAD — every docs page and non-landing route byte-identical (no
   brandCSS change); landing diff is only the decided markup/CSS/script lines; island
   byte-identical; 0022/0024 outcomes survive.
6. Mutation battery in a scratch clone (never the real repo): delete the media-query block →
   a named test fails; drop a scrim toggle from `sandbinClosePick` → named test fails; change
   the retitle back → copy-set test fails; JS-only mutations (remove scrim click preventDefault
   none — instead: change the swapbg duration to 0.1s, or delete `placeholder` handling CSS):
   state plainly which mutations the Go suite cannot catch and confirm the browser matrix
   covers them.
7. Cleanup confirmation: /proc scan (Z dead), ports free, scratch tidy, `git status --short`
   only `?? .opencode/`, tags unchanged, nothing pushed.

## Skipped on purpose

Everything in the decision record's Skipped section, plus: remembering the last harness across
visits, modal focus trapping, and any release/tag action (releasing the new copy is a separate
human call).

## Outcome

Implemented in `7fd95d7` (modal), `e5e5f57` (swap tint), `42b6c36` (copy retitle) and `a400501`
(usage prose) on plan-open `55b6c8c`; reviewer verdict APPROVE WITH FINDINGS — no blockers,
nits only.

- Counts: 202/107/6/17/2 vs baseline 202/107/6/15/2 — zero removals; the additions are
  `TestRenderCarriesTheResponsiveModal` and `TestRenderCarriesTheSwapTint`. Touched files:
  exactly the intentional set; `frontpage.go`, `handlers.go` and `docs.go`/`brandCSS`
  byte-unchanged. `sandbin-client-skill.md` carried no stale wording, so it needed no edit.
- Mockup fidelity (reviewer-verified): the served landing (Host `sandbin.example.org`, XFP
  https) equals the approved mockup except the footer's build-dependent version stamp — a
  one-line diff, exactly as claimed.
- Browser matrix (both parties, both viewports): desktop keeps the anchored dropdown with
  scrim/hint hidden and the 0023 ordering matrix intact; at 390×844 the modal measures exactly
  as decided (fixed panel 51/51/12 insets, 345px input, "I use" hint above, muted placeholder
  `rgb(139,129,113)`, list fills the rest, zero horizontal overflow); scrim click commits the
  highlighted row through the untouched focusout path (scrim probed non-focusable), separator
  stays inert, row picks close the scrim, Escape unchanged; swap tint samples warm at t≈0,
  mid-fade `rgb(252,247,238)` at ~700ms (the reviewer's addition, pinning the 1.4s), white
  after 1.6s — in both modes. Console clean.
- Battery vs `348b102` (both binaries `-buildvcs=false`, stamp-matched): 9 of 11 docs pages
  byte-identical; `sandbin-api.html` and `sandbin-serve.html` differ by exactly the retitle
  line each; landing diff 24/5 lines, all decided; JSON island byte-identical; healthz,
  robots.txt, install.sh, SKILL.md, logo, 404/405 bodies, bin round trip byte-identical;
  hostile probes unchanged.
- Mutation battery (scratch clone): deleting the media block, a scrim toggle, the placeholder
  attribute, the swapbg selector string, or reverting the tagline each fails a named Go test —
  the new pins string-pin the CSS/markup/JS blocks. What the suite cannot catch is mutation of
  unpinned event-handler JS (probed live: removing the separator's mousedown preventDefault
  sails the suite but flips the browser separator-inert check) — the browser matrix covers that
  class.
- Disclosure corrections recorded: the implementer expected the swapbg-duration mutation to
  escape the Go suite; it does not (the pin quotes `1.4s`) — the claim was conservative, and
  the plan's "what Go cannot catch" line should have named event-handler mutations like the
  separator probe. The reviewer also added the missing mid-fade browser sample.
- Environment note: ~22 zombie `sandbin-*` entries from earlier sessions linger in the process
  table (all `Z`, holding nothing); they match the plan's "Z dead" convention but a future
  session could reap them.
