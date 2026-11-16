# Decision 0023: the landing page becomes the harness picker page; the docs shell joins its icon header

Decided at plan open, HEAD `23041d0`, after the preview-arc mockup was approved
(`https://preview.yookoala.dev/951c2bcb/`, reference copies at `/tmp/opencode/plan0023/`).

## Why

The tab row shipped by plan 0021 reads as crowded, the page repeats the wordmark as its `h1`,
and the hosted docs' header does not match the landing. The approved mockup replaces all three:
a select-style combobox sentence, a command input with an inner copy icon and a flash overlay,
and one shared icon-header language across landing and docs.

## Decisions

1. **Landing copy is exactly the approved mockup.** `<title>` and `h1` become "This is a Sandbin
   instance" (`h1 id="this-is-a-sandbin-instance"`); a second tagline line "Learn more about
   Sandbin on GitHub." (repo URL); the section heading "Tell Your Agent to use This"
   (`id="tell-your-agent-to-use-this"`); the intro paragraph "Tell me what harness you are
   using:"; after the picker line, "Run this command in your terminal:". The "The hosted docs"
   section and the GitHub-links paragraph stay deleted. Page links: logo(`/`), docs icon
   (`/docs`), GitHub icon, inline GitHub — the landing's nav is two borderless icons (book,
   GitHub); the logo is the home link.
2. **The harness table gains `Path`.** `Harness{Label, Cmd, Path}`; `DefaultHarnesses()` starts
   with `{Label: "a generic harness", Cmd: installCmd}` (no `--dest`) and each row's `Path` names
   the folder the command installs into plus `sandbin-preview` — `$HOME/.agents/skills/sandbin-preview`
   for generic, `$HOME/<dir>/skills/sandbin-preview` per harness. Commands are unchanged forms
   `install.sh` already accepts. The `Agents` label is gone.
3. **The picker is static markup plus a JSON data island; `text/template` stays.** front.html
   carries the combobox, the command widget, and the flash as markup; the table reaches the page
   through `<script id="harness-data" type="application/json">{{.HarnessJSON}}</script>` built by
   `encoding/json` in `Render` (its escaping is JSON-safe for a script element and sidesteps the
   `"` inside `--dest "$HOME/…"` commands). The engine is still `text/template`; the only extra
   escaping is Render's `strings.ReplaceAll(cmd, "\"", "&quot;")` for the server-rendered initial
   input value, so the no-JS markup shows the default row. `Data` gains `HarnessJSON`,
   `DefaultCmd` (escaped), `DefaultPath`, `DefaultLabel`; `Base` stays the only address-derived
   value, guarded by `frontBase`/`frontHostRE` as before.
4. **Picker behavior is a verbatim port of the approved mockup JS:** select-style face; click
   reveals the type box (input exists only while open) and a listbox that always lists all eight;
   typing floats matches to the top; the current selection and `a generic harness` are always
   above the separator (selection first, generic last in the group); everything below the
   separator is case-insensitively alphabetical; the separator shows whenever anything is below
   it — including the no-typing state (`[current, generic?, ---, alphabetical rest]`) — and is
   absent only when the group covers the whole table; the first item is highlighted while typing;
   Enter/click selects the highlighted row, arrows move the highlight (the separator is inert),
   Escape closes unchanged, blur commits the highlighted row (the group's top until arrow keys
   move it; a review pass tightened this wording); unmatched text falls back to generic on top;
   the command/path/label update with the 0.28s swap fade; the copy button is an icon inside the
   full-width readonly input's right end, focus/click on the input selects all, and copying
   flashes the non-interactive "Copied" overlay (`pointer-events:none`, hold then 2.2s fade,
   retriggerable). Clipboard API first, `execCommand` fallback — the shipped posture.
   Post-review amendment (`6dfa2bb`): `#cmd-input` carries `aria-label="Install command"` and
   the type box toggles its own `aria-expanded` with the popup, clearing the only
   accessibility regression the approved mockup carried (axe critical unlabeled input); the
   preserved mockup stays the pre-amendment reference.
5. **The docs shell adopts the same header.** `docHead`'s text nav becomes three borderless
   icons: home (house glyph → `/`, aria-label "Sandbin home"), book (`/docs`, "Documentation"),
   and GitHub keeping each page's own `sourceURL`. The icon CSS (`.page-head nav.icon-nav`,
   `.icon-link`, hover/focus states, svg sizing) moves into `brandCSS` in `docs.go`, shared by
   docs pages and the landing through the existing `{{.BrandCSS}}` injection; the docs' existing
   `.page-head nav a` margin rule is kept, overridden for the icon nav. Root-relative hrefs
   (`/`, `/docs`) everywhere — the mockup's `index.html`/`docs-hub.html` rewrites were
   bin-preview-only.
6. **Usage docs describe the new page.** `sandbin-api.md`'s landing sentence and
   `sandbin-client-skill.md`'s tab-row wording are rewritten for the picker, install-path line,
   and copy-flash; no other usage page changes.

## Skipped on purpose

- Client or installer changes: every mocked command form is already accepted by `install.sh`.
- `robots.txt`/`X-Robots-Tag` (plan 0022 territory), the `docShell` aside, bin routes, listings,
  lockout, CLI, client, store.
- Server-rendering the listbox rows: the list is a runtime construct from the JSON island
  (ordering is interactive); only the initial widget values and the data island are rendered.
- Fitting the picker into native `<select>`/`<datalist>`: rejected in the arc — native widgets
  cannot show the separator, the pinned group, or the type-box-on-click behavior.
