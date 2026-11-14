# Decisions: the landing page picks a harness and shows its install command

The landing's install block is today one plain one-liner plus a paragraph about the default
folder and the `--dest` form. It becomes a harness selector: a tab per harness, each showing that
harness's own one-liner with the copy button. User-driven request, with the harness table taken
from the Agent Skills installation guide the user supplied. This is a landing-content change, not
a refactor: the landing page's served bytes intentionally change; every other route stays
untouched.

## The selector

| Tab | Install command shown |
|---|---|
| Agents (default, first) | `curl -fsSL {{.Base}}/skills/sandbin-preview/install.sh \| bash` (no `--dest`) |
| OpenCode | `… \| bash -s -- --dest "$HOME/.config/opencode/skills"` |
| Claude Code | `… \| bash -s -- --dest "$HOME/.claude/skills"` |
| Codex | `… \| bash -s -- --dest "$HOME/.codex/skills"` |
| Cursor | `… \| bash -s -- --dest "$HOME/.cursor/skills"` |
| Copilot | `… \| bash -s -- --dest "$HOME/.copilot/skills"` |
| Gemini CLI | `… \| bash -s -- --dest "$HOME/.gemini/skills"` |
| Windsurf | `… \| bash -s -- --dest "$HOME/.codeium/windsurf/skills"` |

Every non-default command uses `$HOME/...`, never `~/...`: the installer assigns `DEST` from the
flag value, and bash does not expand a tilde in an assignment or quoted argument. Each tab uses
the harness's native global folder; the shared `~/.agents/skills` route is the default tab's
business, and one sentence under the tabs says the seven harnesses all also read `.agents/skills`
(except Claude Code, which reads `.claude/skills`).

| Point | Decision |
|---|---|
| Tab mechanics | Vanilla inline JS in front.html, same posture as the existing copy script: a row of `<button class="tab">`s toggling `aria-pressed` and showing exactly one `<pre class="cmd-body">` by removing a `hidden` class. No JS framework, no external script, `<noscript>`-free — every command is plain selectable text, the tabs only switch which one is visible. |
| Copy button | One copy button beside the command area, kept as `class="copy"` with the same clipboard posture as today (`navigator.clipboard.writeText` with the `document.execCommand("copy")` fallback, `copied` feedback). It reads `textContent` from the currently visible command block and, when the clipboard API reports failure, retries the fallback against the same visible block at call time (so it works on the tab the user just switched to, even on a plain-HTTP page where the API path always rejects). |
| Harness data | The `Harness{Label, Cmd}` list moves to `internal/frontpage` as `DefaultHarnesses()`, returning a fresh slice per call; `Data` gains a `Harnesses []Harness` field. `cmd/sandbin` keeps passing only finished values (`Base`, `Logo`, `BrandCSS`, `DefaultHarnesses()`). front.html generates buttons and command blocks with one `{{range}}`, so the tab list and the markup cannot drift apart and the battery can render a different slice to prove the page is data-driven. |
| CSS | Tab styles live in front.html itself (a second static `<style>` after `{{.BrandCSS}}`): `.tabs`, `.tab` (aria-pressed highlight in the brand colors), `.cmd-body` and `.hidden{display:none}`. `brandCSS` in `docs.go` stays byte-identical — it is shared with the docs shell and the tabs are landing-only. |
| Removed content | The paragraph "That installs into `$HOME/.agents/skills/sandbin-preview` and, at a terminal, initializes the current directory … `--dest .opencode/skills`" is deleted; its facts move into the tabs, the default-tab sentence, and the per-project note. The tagline, GitHub links and hosted-docs paragraph stay verbatim. |
| Project-scope note | One short paragraph under the tabs: passing a repo folder instead — `… \| bash -s -- --dest .agents/skills` — installs into the current project, which `.agents/skills` reading harnesses pick up; the usage docs carry the details. |
| Test pins | `handlers_test.go`'s front pins change because the page intentionally changes: `TestFrontPageAtRoot` keeps the plain-curl, copy-button and clipboard pins, drops the deleted paragraph's sentence, replaces the bare `--dest` pin with pins on several harness paths, and gains pins for the harness labels. `TestFrontPageEchoesTheVisitorsAddress` is untouched: every command carries `{{.Base}}`, so its per-host echo and placeholder pins still hold. The `internal/frontpage` battery gains the harness-table pins, the hidden-until-selected behavior, the project note, and a custom-slice render proving the range is data-driven. |
| Usage docs | `docs/usages/sandbin-serve.md`'s landing description and `docs/usages/sandbin-client-skill.md`'s landing sentence are updated to describe the tabs (the installer's `--dest` description there stays). Because the docs pages are embedded at build time, the hosted HTML of exactly those two pages changes too. |

## Implementation mechanics (recorded at review)

- `Render` fills the literal `{{.Base}}` token inside each harness `Cmd` **before** executing the
  page, on a copy of the slice: `text/template` does not re-parse strings it prints, so the token
  would otherwise land in the page verbatim. Proven non-mutating — a caller's slice keeps its
  tokens after `Render`, and `DefaultHarnesses()` rebuilds from literals per call.
- Command blocks carry one composed attribute, `class="cmd-body{{if ne $i 0}} hidden{{end}}"` —
  the plan's sketch of a second `class` would be invalid HTML.
- Two existing frontpage tests adapted to the page's new content (`sampleData()` supplies the
  harnesses; the install-command-line test counts the eight commands): strengthened, never
  weakened; no other existing test anywhere was edited.

## Skipped on purpose

Changing `install.sh` or `SKILL.md` (byte-identical: the commands shown are forms the existing
installer already accepts), server-side harness detection (User-Agent sniffing), per-harness docs
pages, a global/project scope toggle inside each tab (user chose the single project note),
symlink tips from the installation guide, npx/CLI installer mentions on the landing, changes to
`brandCSS` or the docs shell, tab state persistence (localStorage or URL hashes), and any
behavior change to non-landing routes.
