# Plan: the landing page gains per-harness install command tabs

Status: planned. Spec: `docs/decisions/0021-harness-install-tabs.md` — the harness table, tab
mechanics, copy-button posture, data shape, removed content and doc updates are settled there.
Baseline HEAD is the plan-0020 close-out `2b1c29a` (`cmd/sandbin` 199+29, `cmd/sandbin-client`
107+28, `internal/version` 6+35, `internal/frontpage` 6, all gates green). Unlike 0020 this is a
content change: the landing page's bytes intentionally change, as do the two usage docs that
describe it; everything else must stay byte-identical.

**Goal:** the landing's install block becomes a harness selector — a tab row (Agents default,
OpenCode, Claude Code, Codex, Cursor, Copilot, Gemini CLI, Windsurf) whose visible one-liner is
that harness's install command (`--dest "$HOME/<native dir>"`), with the existing copy button
posture; the `--dest` paragraph is replaced by the tabs, a default-tab sentence and one
project-scope note.

**Architecture:** `internal/frontpage` gains `Harness{Label, Cmd}` and `DefaultHarnesses()`;
`front.html` generates the tab row and command blocks with one `{{range}}` and carries static tab
CSS plus the extended tab-aware copy script; `frontpage.Data` gains `Harnesses []Harness`;
`cmd/sandbin/handlers.go`'s `renderFrontPage` only adds `Harnesses: frontpage.DefaultHarnesses()`
to the Data it already builds. `docs.go`, `frontBase`, `frontHostRE` untouched. Two usage docs
updated; hosted docs pages for exactly those two change.

**Tech stack:** Go stdlib (`embed`, `text/template`), vanilla inline HTML/CSS/JS.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Every tab command is a command the existing installer already accepts: `install.sh | bash` or
  `install.sh | bash -s -- --dest "$HOME/..."` — always `$HOME/`, never `~/` (bash does not
  expand a tilde in the `DEST=` assignment). The eight tab rows are exactly the decisions table.
- Byte-identity of everything that is not the landing page or the two edited docs pages: full
  HTTP battery old-vs-new (bins, redirect, fallback, listing, skill routes incl. the 0019 pin
  line, healthz, docs hub and the eight unedited pages, error shapes). The landing and the two
  hosted doc pages are the only intentional diffs; quote them explicitly.
- The copy button keeps its posture: `class="copy"`, `navigator.clipboard` primary,
  `document.execCommand("copy")` fallback, `copied` feedback; it copies the currently visible
  command block. No `<script src=`, no external assets or fonts, `<noscript>`-free page.
- Do not touch: `install.sh`, `SKILL.md` (byte-identical), `brandCSS`, `docs.go` shell/rendering
  code, `frontBase`, `frontHostRE` (byte-identical), bin routes and semantics, listings, 404
  fallback, 301, CLI layer, `client.go`/`config.go`/store, `internal/version`, `scripts/`,
  `Makefile`, `Containerfile`, every operational error wording.
- The tagline, GitHub links, hosted-docs paragraph and the header/logo stay verbatim.
- `text/template` only; the harness commands are page content — `{{.Base}}` remains the page's
  only dynamic value, guarded by `frontHostRE` exactly as today.
- Never push; never tag the real repo. Tags v0.1.0..v0.3.3 stay where they are.

## Review Focus

- The eight rendered commands match the decisions table byte-for-byte, quote-safe (`"$HOME/...")`,
  and each is an installer-accepted form — prove one or two actually install against a live server
  with a scratch HOME.
- Copy button follows the selected tab (including on plain HTTP where the clipboard API rejects
  and the execCommand fallback must re-read the now-visible block), proven in a real browser.
- The intentional-diff list is exactly landing + the two edited docs pages; everything else is
  byte-identical vs baseline binaries.
- The test edits are only in `TestFrontPageAtRoot`'s pin lists and the `internal/frontpage`
  battery; `TestFrontPageEchoesTheVisitorsAddress` is provably untouched.

## Task 1: the selector in the template and the data

**Files:** modify `internal/frontpage/front.html`, `internal/frontpage/frontpage.go`,
`internal/frontpage/frontpage_test.go`, `cmd/sandbin/handlers.go` (`renderFrontPage` data literal
+ doc comment only), `cmd/sandbin/handlers_test.go` (`TestFrontPageAtRoot` pins only)

**Interfaces:**
- `frontpage.Harness struct{ Label, Cmd string }`; `func DefaultHarnesses() []Harness` returning
  a fresh slice per call with the eight decisions-table rows (Labels: `Agents`, `OpenCode`,
  `Claude Code`, `Codex`, `Cursor`, `Copilot`, `Gemini CLI`, `Windsurf`; first Cmd has no
  `--dest`, the rest end with `--dest "$HOME/<dir>"` per the table). `Data` gains
  `Harnesses []Harness`; `Render` unchanged otherwise.
- front.html install section: `<h2 id="give-the-skill-to-your-agent">` and the intro `<p>` stay;
  then `<div class="tabs">` of `<button type="button" class="tab" aria-pressed="true|false">`
  generated by `{{range $i, $h := .Harnesses}}`; a `<div class="cmd">` holding one
  `<pre class="cmd-body" id="cmd-{{$i}}">` per harness — first visible, rest carry `hidden` —
  each pre containing exactly `{{$h.Cmd}}` with `{{.Base}}` filled in the command text (the Cmd
  strings in the table already name `{{.Base}}`), and the single copy button beside the pre. Then
  the default-tab sentence (no `--dest` installs into `$HOME/.agents/skills`, the harnesses that
  also read it) and the project-scope note (`--dest .agents/skills` installs into the current
  project). Static `<style>` block after `{{.BrandCSS}}` for `.tabs`, `.tab`, `.cmd-body`,
  `.hidden{display:none}`.
- JS: `sandbinSelectTab(button)` toggles `aria-pressed` and un-hides exactly the matching block;
  `sandbinCopyInstall(button)` reads `textContent` of `document.querySelector(".cmd-body:not(.hidden)")`
  (visible block looked up at copy time) and keeps today's clipboard posture, retrying the
  fallback against the same visible block if `writeText` rejects. Existing function names and
  `copied`/`copy` words stay.
- `renderFrontPage`: adds `Harnesses: frontpage.DefaultHarnesses()` to the existing Data literal;
  everything else (error path, Content-Type, write) untouched.
- Tests first (TDD): extend the frontpage battery and edit `TestFrontPageAtRoot` pins BEFORE the
  template change — they fail against the old page, then pass. `TestFrontPageEchoesTheVisitorsAddress`
  and every other test stay untouched. New frontpage tests: the eight default rows render (labels
  present, exact command strings, quote-safe `$HOME`, first row without `--dest`); exactly one
  command block is not hidden by default and it is the Agents one; the project note and
  default-tab sentence present; rendering with a custom two-row slice produces exactly two tab
  buttons/blocks (data-driven `range`); zero `{{` survives with the defaults; existing six tests
  keep passing.

- [ ] Step 1: write the new/edited pins (frontpage battery + `TestFrontPageAtRoot`); confirm they
  fail against the current page (`go test ./internal/frontpage ./cmd/sandbin -run 'Front|Render|Harness|Tab'` style).
- [ ] Step 2: front.html tabs/CSS/JS + `Harness`/`DefaultHarnesses()` + Data field + handler
  data line; green.
- [ ] Step 3: gates + commit `feat: the landing page offers per-harness install command tabs`.

## Task 2: the usage docs follow

**Files:** modify `docs/usages/sandbin-serve.md` (the landing description sentence),
`docs/usages/sandbin-client-skill.md` (the landing sentence; the installer `--dest` description
stays)

**Interfaces:** both describe the landing as carrying a harness selector whose tabs show each
harness's install one-liner with a copy button, the no-`--dest` default going to
`$HOME/.agents/skills`, and a repo folder install via `--dest .agents/skills`. Keep each page's
existing tone and length discipline (a couple of sentences, one command example).

- [ ] Step 1: edit the two pages.
- [ ] Step 2: gates (the docs battery in cmd/sandbin must still pass — it pins rendered pages; if
  a pin quotes the edited sentences, update exactly that pin and say so) + commit
  `docs: usage pages describe the landing page's harness tabs`.

## Verification (implementer, after Tasks 1-2)

- All five gates; counts per package before/after with `comm` on test-name lists (baseline above;
  additions only in `internal/frontpage`; zero removals anywhere).
- Side-by-side vs baseline binaries (`git archive 2b1c29a` under /tmp/opencode): full battery —
  landing under ordinary hosts, odd hosts (placeholder), XFF/XFP combos; quote the landing diff as
  intentional (old size vs new size); the docs hub + the eight UNEDITED pages + logo route +
  SKILL.md + install.sh + healthz + bins/301/listing/DELETE + 404 fallback + 405 shapes all
  byte-identical (zero diffs); the two edited docs pages differ only where edited (quote).
- Install proof: serve HEAD's install flow into a scratch HOME and run (a) the default command and
  (b) at least two tab commands verbatim as rendered (e.g. Claude Code and Windsurf, with scratch
  `$HOME`); each lands `SKILL.md` under the named dir and reports the pinned client or the
  build-later line; also `bash -s -- --dest .agents/skills` into a scratch repo dir.
- `git diff 2b1c29a..HEAD -- cmd/sandbin/handlers_test.go` shows only `TestFrontPageAtRoot` pin
  edits; `TestFrontPageEchoesTheVisitorsAddress` region byte-identical (quote); `install.sh`,
  `SKILL.md`, `docs.go`, `frontBase`/`frontHostRE` absent from the diff or byte-identical.
- Cleanup: kill servers via /proc scan (`*sandbi` prefix), ports 18303-18305 clear by connect
  attempts, `/tmp/opencode` EMPTY, repo clean apart from `?? .opencode/`, tags unchanged.

## Reviewer TODO

1. Gates yourself; counts vs baseline (`git archive 2b1c29a`) with comm — additions only in the
   new package, zero removals; the handlers_test.go diff is only `TestFrontPageAtRoot` pins, and
   the echo test region is byte-identical (quote both).
2. Scope: `git diff 2b1c29a..HEAD --name-only` = the five Task-1 files + two usage docs + plan
   docs only; `install.sh`/`SKILL.md` byte-identical (quote cmp); `docs.go` untouched; the
   handlers.go diff is the Data line + comment only; `frontBase`/`frontHostRE` cmp-identical.
3. Rendered table: fetch the landing from your HEAD server and extract all eight commands; diff
   against the decisions table (expect byte-equal modulo the echoed base); every non-default
   command is quote-safe `"$HOME/..."`; run two of them verbatim against a live HEAD server with a
   scratch HOME and confirm the skill lands where the tab says.
4. Battery vs baseline binaries YOUR OWN run (HEAD 18305, baseline 18304): landing intentional diff
   (quote sizes; confirm tagline/header/GitHub/docs paragraphs survive verbatim, the removed
   paragraph is gone, zero `{{` survives, no `<script src=`/`<img` introduced); everything else
   byte-identical — full surface battery incl. docs hub + eight unedited pages + the two edited
   pages quoting only their edits + skill routes with the pin line + bins/301/listing/DELETE +
   error shapes; bad-key 403→429 walk LAST in a separate run, identical both sides.
5. Browser proof (real Chrome, e.g. the agent-browser skill): on the landing, click through all
   eight tabs — visible command changes per tab, exactly one block visible, `aria-pressed`
   follows; copy on the DEFAULT tab and on a NON-default tab (plain-HTTP page: clipboard API
   rejects → execCommand fallback must copy the now-visible command — verify clipboard contents
   where the harness allows reading it, else the `copied` feedback + textarea path) ; commands
   selectable without JS (disable JS or fetch raw); no console errors; narrow-viewport tab row
   does not break.
6. Mutation proofs in scratch copies under /tmp/opencode/rev0021: mutate a `DefaultHarnesses()`
   row and front.html (drop a tab's `hidden`, swap a `$HOME` for `~`, delete the project note) and
   confirm specific named tests fail, restoring between mutations; confirm the frontpage battery
   proves data-drivenness (custom slice).
7. Container proof: `podman build --network=host --build-arg SANDBIN_VERSION=$(scripts/version.sh)`
   from HEAD, run on -p 127.0.0.1:18306:8080 (volume + 0600 env-file convention): landing served
   with all eight tabs' commands byte-equal to the tree server's for the same Host string;
   install.sh pin line present; version stamp via `--entrypoint --version`; full cleanup per the
   keep-list (golang:1.26-alpine, alpine:3.22, alpine:latest; prune only your own layers).
8. Cleanup confirmation: /proc scan clean (`*sandbi`, skip Z, kill by number), ports 18304-18306
   free by connect attempts, `/tmp/opencode` empty, `git status --short` only `?? .opencode/`,
   tags unchanged, HEAD unchanged.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: `go mod` changes (none needed),
`Data` refactors beyond the one field, docs pages for harness specifics beyond the two edits, and
any installer flag changes.
