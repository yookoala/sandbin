# Plan: dark theme for the landing and docs pages, and pretty branded 404 pages

Status: open. Baseline HEAD is the logo commit `9ba9f7c` (`docs: add logo for dark theme`).
Baseline gates green; top-level test counts: `cmd/sandbin` 271, `cmd/sandbin-client` 132,
`internal/frontpage` 17, `internal/robots` 2, `internal/version` 6.

Two independent features, two atomic commits in this order: Task 1 (dark theme) first, Task 2
(pretty 404 pages) second — Task 2's pages are built with `docShell`, which Task 1 makes
themed.

## Task 1 — dark theme for the landing page and the docs pages

**Goal:** the landing page (`/`) and every hosted docs page (hub, member pages, logo routes)
support a dark theme: a theme choice persisted in `localStorage` (key `sandbin-theme`),
falling back to the OS `prefers-color-scheme`, a toggle button in the shared icon nav, and
the dark-theme wordmark (`docs/assets/sandbin-logo-dark.svg`, already committed) shown where
the light one is hidden.

**Architecture:**
- `skill.go` gains one embed: `LogoDarkSVG` for `docs/assets/sandbin-logo-dark.svg`.
- `cmd/sandbin/docs.go`:
  - `brandCSS` moves every hardcoded color to a CSS variable defined on `:root` with today's
    light value, plus one `:root[data-theme="dark"]` block overriding the variables for dark.
    Light mode therefore renders exactly as today; dark is additive.
  - New `themeScript` const (head script, runs before paint, defines the toggle).
  - New `themeToggleButton` const (the nav button, sun/moon).
  - `docShell` writes the theme script into `<head>` after the `<style>`.
  - `logoBlock(class)` emits the light logo div and a dark twin carrying the extra class
    `logo-dark`.
  - `docHead()` appends the toggle button after the GitHub door.
  - `serveDocs` serves `sandbin.LogoDarkSVG` at `/docs/assets/sandbin-logo-dark.svg`
    (`docsDarkLogoPath` const, `image/svg+xml`), beside the existing logo route.
- `internal/frontpage`: `Data` gains `LogoDark` and `ThemeJS` (same posture as `Logo`/
  `BrandCSS` — cmd/sandbin hands the page its values); `front.html` carries the dark hero
  logo, the theme script in `<head>`, and the toggle button in its nav.
- `cmd/sandbin/handlers.go`: `renderFrontPage` passes `LogoDark: sandbin.LogoDarkSVG` and
  `ThemeJS: themeScript`.

**CSS variables** (light value = the value the rule carries today, byte-for-byte):

```
light:  --sandbin-purple:#4E1680; --sandbin-amber:#EA9B25; --sandbin-canvas:#F8F7F4;
         --sb-fg:#222; --sb-li:#333; --sb-quote:#444; --sb-soft:#555; --sb-muted:#666;
         --sb-faint:#8b8171; --sb-pick:#b3a894; --sb-border:#e3ddd3; --sb-line:#ded7cb;
         --sb-hl:#F6EDDD; --sb-code:#efeadf; --sb-surface:#fff; --sb-ok:#2e7d32
dark:   --sandbin-purple:#E2C2FF; --sandbin-canvas:#111111; --sb-fg:#E5E1D8; --sb-li:#D8D3CA;
         --sb-quote:#B0A99E; --sb-soft:#A09A90; --sb-muted:#A09A90; --sb-faint:#7E7668;
         --sb-pick:#57503F; --sb-border:#3B372F; --sb-line:#4A443A; --sb-hl:#2E2410;
         --sb-code:#2A2721; --sb-surface:#1B1A17; --sb-ok:#7BC47F
```

(`--sandbin-amber` is identical in both themes — the dark logo keeps `#EA9B25`.)

Mapping of today's literals: body bg `#F8F7F4`→`var(--sandbin-canvas)`; body color `#222`→
`var(--sb-fg)`; footer border `#e3ddd3`→`var(--sb-border)`, footer color `#666`→
`var(--sb-muted)`; headings/links `#4E1680`→`var(--sandbin-purple)`; `pre` bg `#fff`→
`var(--sb-surface)`, border `#e3ddd3`→`var(--sb-border)`; `code` bg `#efeadf`→
`var(--sb-code)`; table borders `#ded7cb`→`var(--sb-line)`; `th` bg `#4E1680`→
`var(--sandbin-purple)`, color `#fff`→`var(--sb-surface)`; blockquote `#444`→
`var(--sb-quote)`; `.page-head` border `#4E1680`→`var(--sandbin-purple)`; `.tagline` `#555`→
`var(--sb-soft)`; `.copy`/`.icon-link:hover`/`.block-copy` bg `#fff`→`var(--sb-surface)`;
`.block-copy` border `#ded7cb`→`var(--sb-line)`; `.block-copy.copied` `#2e7d32`→
`var(--sb-ok)`.

Add to `brandCSS`:

```
.logo-dark{display:none}
:root[data-theme="dark"] .logo-dark{display:block}
:root[data-theme="dark"] .wordmark:not(.logo-dark){display:none}
.theme-sun{display:none}
:root[data-theme="dark"] .theme-moon{display:none}
:root[data-theme="dark"] .theme-sun{display:block}
```

Mapping in `front.html`'s own `<style>` block (same variable names): `.pick-face` bg `#fff`→
`var(--sb-surface)`, border `#b3a894`→`var(--sb-pick)`; `.pick-pop input` bg `#fff`→
`var(--sb-surface)`; `.pick-pop ul` bg `#fff`→`var(--sb-surface)`, border `#b3a894`→
`var(--sb-pick)`; `.pick-pop li` color `#333`→`var(--sb-li)`; `.pick-pop li.hl` bg `#F6EDDD`
→`var(--sb-hl)`; `.pick-pop input::placeholder` `#8b8171`→`var(--sb-faint)`; the 30rem
`.pick-pop:not([hidden])` panel bg `#fff`→`var(--sb-surface)`, border `#b3a894`→
`var(--sb-pick)`; the 30rem `.pick-hint` color `#222`→`var(--sb-fg)`; `.cmd-input` color
`#222`→`var(--sb-fg)`, bg `#fff`→`var(--sb-surface)`, border `#e3ddd3`→`var(--sb-border)`;
`.cmd-copy` bg `#fff`→`var(--sb-surface)`; keyframes
`@keyframes sandbin-swapbg{0%{background-color:#f7e7cb}100%{background-color:#fff}}` keeps
the `0%` tint and becomes `100%{background-color:var(--sb-surface)}`.

**themeScript** (exact text; `docs.go` const `themeScript`, passed to the landing as
`Data.ThemeJS`):

```js
(function () {
  var t = null;
  try { t = localStorage.getItem("sandbin-theme"); } catch (e) {}
  if (t !== "dark" && t !== "light") {
    t = (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
  }
  document.documentElement.setAttribute("data-theme", t);
})();
function sandbinToggleTheme() {
  var next = document.documentElement.getAttribute("data-theme") === "dark" ? "light" : "dark";
  document.documentElement.setAttribute("data-theme", next);
  try { localStorage.setItem("sandbin-theme", next); } catch (e) {}
}
```

**themeToggleButton** (exact markup; `docHead()` appends it in the nav after the GitHub
door; `front.html` carries the same line in its nav):

```html
<button type="button" class="icon-link theme-toggle" title="toggle theme" aria-label="Toggle dark theme" onclick="sandbinToggleTheme()"><svg class="theme-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg><svg class="theme-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg></button>
```

**Logo twins:** `logoBlock` becomes
`"<div class=\"" + class + "\">" + sandbin.LogoSVG + "</div>" +
"<div class=\"" + class + " logo-dark\">" + sandbin.LogoDarkSVG + "</div>"`.
The landing hero becomes
`<a class="logo-link" href="/" aria-label="Sandbin home"><div class="wordmark wordmark-hero">{{.Logo}}</div><div class="wordmark wordmark-hero logo-dark">{{.LogoDark}}</div></a>`.
Both inlined logo `<style>` blocks stay valid side by side: the light logo styles
`.brand-purple`/`.brand-amber`, the dark one `.brand-purple-bright`/`.brand-amber` — the
only shared class name (`.brand-amber`) carries the same `#EA9B25` fill in both files.

**Interfaces:**
- `var LogoDarkSVG string` in package `sandbin` (module root).
- `Data{..., Logo string, LogoDark string, BrandCSS string, ThemeJS string, ...}` in
  `internal/frontpage`; `Render` fills `{{.LogoDark}}` and `{{.ThemeJS}}` like the others.
- `themeScript`, `themeToggleButton`, `docsDarkLogoPath` unexported consts in `cmd/sandbin`.

**Files:** `skill.go`; `cmd/sandbin/docs.go`; `cmd/sandbin/handlers.go` (the one
`renderFrontPage` call site); `cmd/sandbin/handlers_test.go` (front page wants);
`cmd/sandbin/docs_test.go`; `internal/frontpage/front.html`;
`internal/frontpage/frontpage.go`; `internal/frontpage/frontpage_test.go`.

**Steps (TDD):**
1. frontpage tests first: `sampleData()` gains `LogoDark: sampleLogoDark` and
   `ThemeJS: sampleThemeJS` (new consts: `sampleLogoDark` a small distinct SVG like
   `sampleLogo` with `.d{fill:#E2C2FF}`; `sampleThemeJS` a short string naming
   `sandbinToggleTheme` and `data-theme` — no `{{` in either). New
   `TestRenderCarriesTheDarkTheme`: the render carries
   `"<script>\n" + sampleThemeJS + "</script>"` before `</head>`, the
   `theme-toggle` button in the `<header>…</header>` block, and the dark twin
   `<div class="wordmark wordmark-hero logo-dark">` once inside the header. Update
   `TestRenderPlacesTheLogoInItsHeroWrapper` (light wrapper still exactly once, dark twin
   exactly once, both inside `<header>`), `TestRenderCarriesTheSwapTint` (new keyframe
   line), and `TestRenderCarriesTheResponsiveModal` (placeholder `var(--sb-faint)`,
   pick-hint `var(--sb-fg)`). Run: red.
2. Implement: `skill.go` embed; `frontpage.go` `Data` fields + doc comment; `front.html`
   (dark hero twin, theme script after the second `</style>` and before `</head>`, toggle
   button after the GitHub anchor, the variable swap in its `<style>`). Green.
3. docs side: `docs_test.go` first — extend `TestDocsLogoRouteServesEmbeddedBytes` to also
   GET `/docs/assets/sandbin-logo-dark.svg` (200, `image/svg+xml`, body byte-equal to the
   repo file, `sandbin.LogoDarkSVG` byte-equal to the repo file); extend
   `TestDocsPagesCarryTheWordmarkInline` to require `sandbin.LogoDarkSVG` verbatim on every
   page; new `TestDocsPagesCarryTheDarkTheme` over hub + member pages: the theme script text
   (`function sandbinToggleTheme() {`) sits before `</head>`, the nav carries the
   `theme-toggle` button, the CSS carries `:root[data-theme="dark"]`, and the served pages
   still carry `sandbin.LogoSVG` verbatim; update
   `TestDocsPagesFillTheViewportAndEndInFooter`'s pinned footer rule to the variable form
   `.site-footer{margin-top:auto;padding-top:1.2rem;border-top:1px solid var(--sb-border);color:var(--sb-muted);font-size:0.85rem;text-align:center}`;
   add `/docs/assets/sandbin-logo-dark.svg` to `TestDocsMethodDiscipline`'s live list. Run: red.
4. Implement `docs.go`: variable-ized `brandCSS` with the dark block (exact text above),
   `themeScript`, `themeToggleButton`, `logoBlock` twins, `docHead` toggle, `docShell`
   script, `docsDarkLogoPath` + route. `handlers.go`: the two new `Data` fields at the
   `renderFrontPage` call site. `handlers_test.go` `TestFrontPageAtRoot` wants gain
   `class="icon-link theme-toggle"`, `function sandbinToggleTheme() {`, `logo-dark`. Green.
5. Gates + commit `feat: dark theme for the landing and docs pages`.

## Task 2 — pretty branded 404 pages

**Goal:** for routes outside `/api/`, a `GET` that finds nothing answers a 404 in the same
branded shell as `/docs/` (theme included), with the message named by cause:

| path shape | first segment | bin exists | page message |
|---|---|---|---|
| `/{seg}` or `/{seg}/{rest}` | eight lowercase hex (`^[0-9a-f]{8}$`) | no | `bin not found` |
| `/{hash}/{sub}` | (any — but only reached through an existing bin here) | yes, file missing | `bin content not found` |
| anything else | not an eight-hex hash | — | `page not found` |

A path is *possible to be a bin* iff its first segment is exactly eight lowercase hex
characters — the store's `newBinHash` writes four random bytes as `hex.EncodeToString`,
so lowercase is the only spelling. The bin's own `404.html` fallback still wins over the
pretty page (SPA behavior is untouched). PUT/DELETE keep the JSON envelope
(`bin not found`); HEAD, POST, PATCH, OPTIONS keep today's plain 404s/405s; the 301
trailing-slash redirect on existing bins is untouched; `.`/`..` segments keep their 400s.

**Architecture:**
- `docs.go` gains the three pages, built at init like `docsHub` through `docShell`
  (so they carry brandCSS, the theme script, the toggle, the logo twins, the footer):

  ```go
  func notFoundPage(message string) hostedPage {
      body := "<h1>404 Not Found</h1>\n" +
          "<p class=\"tagline\">" + message + "</p>\n" +
          "<p><a href=\"/\">Back to the home page</a></p>\n"
      pageHTML := docShell("404 Not Found", body)
      return hostedPage{title: "404 Not Found", html: pageHTML, tmpl: parseDocTemplate("404 page", pageHTML)}
  }

  var (
      nfBinNotFound        = notFoundPage("bin not found")
      nfBinContentNotFound = notFoundPage("bin content not found")
      nfPageNotFound       = notFoundPage("page not found")
  )
  ```

- `handlers.go` gains:

  ```go
  var binHashRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

  func notFoundPageForSegment(seg string) hostedPage {
      if binHashRE.MatchString(seg) { return nfBinNotFound }
      return nfPageNotFound
  }

  func (s *server) writeNotFoundPage(w http.ResponseWriter, r *http.Request, page hostedPage) {
      w.Header().Set("Content-Type", "text/html; charset=utf-8")
      w.WriteHeader(http.StatusNotFound)
      io.WriteString(w, s.expandDoc(page, r))
  }
  ```

- Four answer sites change, all GET-only:
  1. `ServeHTTP`'s `!ok` branch (single segment, no trailing slash): the
     `PUT/DELETE → envelope` line stays; the `else` becomes
     `else if r.Method == http.MethodGet { s.writeNotFoundPage(w, r, notFoundPageForSegment(strings.TrimPrefix(r.URL.Path, "/"))) } else { http.Error(w, "unknown bin", http.StatusNotFound) }`.
     Update the comment above (GET now gets the pretty page; the 301 redirect for existing
     bins stays first).
  2. `getFile` bin lookup: both the `err` and `!ok` branches become
     `s.writeNotFoundPage(w, r, notFoundPageForSegment(hash))`.
  3. `getFile` file-missing tail (after the `404.html` fallback check):
     `s.writeNotFoundPage(w, r, nfBinContentNotFound)`.
  4. `serveDocs` tail: `s.writeNotFoundPage(w, r, nfPageNotFound)` (the method guard above
     it already restricts to GET/HEAD; HEAD answers the same 404 with no body).

- `docs/usages/sandbin-api.md`: reword the stale plain-text 404 sentences to the new
  behavior (lines that say plain `unknown bin` / `unknown path` to a `GET`): the
  plain-refusals paragraph (around line 46-49, including the
  `/openapi.json/` and `/openapix.json` clause), the `DELETE` section's "from then on"
  sentence (~320), the `/openapi.json` section's trailing-slash clause (~371), the bin-base
  section (~411), the file route's unknown-path sentence (~463) and the `404.html`
  paragraph's "a bin without one" clause (~479-482). New wording, in the page's voice: a
  `GET` that finds no bin or no file answers a branded HTML `404 Not Found` page —
  `bin not found` when the first path segment is an eight-hex hash that names no bin,
  `bin content not found` for a path inside an existing bin the bin does not hold (after
  the bin's own `404.html` still wins), `page not found` for a path that cannot name a bin;
  `PUT`/`DELETE` keep the envelope, and the other methods keep their plain refusals.

**Files:** `cmd/sandbin/docs.go`; `cmd/sandbin/handlers.go`; `docs/usages/sandbin-api.md`;
`cmd/sandbin/handlers_test.go`; `cmd/sandbin/api_test.go`; `cmd/sandbin/docs_test.go`.

**Steps (TDD):**
1. Tests first:
   - New `TestPrettyNotFoundPages` in `handlers_test.go`:
     - `GET /deadbeef` → 404 page saying `bin not found`; `GET /deadbeef/notes.md` (no such
       bin) → `bin not found`;
     - `GET /deadbee` (7), `/deadbeef1` (9), `/DEADBEEF` (upper), `/deadbeeg` (`g` not hex)
       → all `page not found`;
     - a created bin: `GET {hash}/missing.md` → `bin content not found`;
     - a bin holding `404.html` answers its own file (byte-equal) for an unmatched path —
       the fallback still beats the pretty page;
     - every page carries `<!doctype html>`, `<title>404 Not Found</title>`,
       `<h1>404 Not Found</h1>`, the theme script (`sandbinToggleTheme`), and the
       powered-by footer.
   - Update `TestGetUnknownBin` (body now the `bin not found` page),
     `TestOtherSkillPathsAreUnknownBins` (`/skills/other`, `/skills/` → `page not found`),
     `TestBinBaseWantsItsTrailingSlash` (`GET /deadbeef` → `bin not found` page; the other
     methods' 404s stay as pinned), `api_test.go` `TestOldRoutesRetired` (`GET /healthz` →
     `page not found` page; `POST /verify-key` stays plain `unknown bin\n`),
     `api_test.go` `TestAPIDeleteBinAnswersOKBody` (the three GET faces → `bin not found`
     page), `docs_test.go` `TestDocsUnknownPathsArePlainNotFound` (rename to match the new
     shape; the paths now answer the `page not found` HTML page), `docs_test.go` new
     `TestNotFoundPagesCarryTheDocsShellAndTheme` over the three `nf*` pages (title, h1,
     the message line, theme script, toggle button, footer, no `{{`).
   Run: red.
2. Implement: the `docs.go` pages, the `handlers.go` helper + the four answer sites (and
   the comment rewording), the `sandbin-api.md` sentences. Green.
3. Gates + commit `feat: pretty branded 404 pages for the bin and page routes`.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...` (the `make test` set).
- Unchanged on purpose: every `/api/` response (including the envelope 404s and the
  405/400 shapes), PUT/DELETE/POST/PATCH/HEAD/OPTIONS answers outside `/api/` (plain 404s,
  405s, 400s stay plain), the 301 bin-base redirect, the `404.html` fallback, bin listings
  and stored user content (a bin's pages are the author's, untouched), `frontBase`/
  `frontHostRE`, lockout accounting, the CLI layer, the client, the store,
  `internal/version`, `internal/robots`, `scripts/`, `Makefile`, `Containerfile`,
  `openapi.json`/`apispec.yaml` (the `/api` surface is unchanged), every non-theme color in
  light mode (the light values are today's literals moved verbatim into `:root`).
- No new dependencies. No new routes outside the two named ones.
- Never push; never tag.

## Review Focus

- Light-mode equivalence: the `:root` light values equal today's literals one-for-one; a
  light browser sees the same colors as before (only extra DOM: the hidden dark logo, the
  toggle button, the head script).
- Dark-mode mechanics: `data-theme` on `<html>`; the stored choice wins, then
  `prefers-color-scheme`; the toggle flips both and persists; the dark wordmark swaps
  (`.logo-dark` display rules), and both logo `<style>` blocks coexist without cross-talk.
- 404 matrix: every row of the table above, including the near-miss hashes (7/9/upper/
  non-hex), the fallback-beats-pretty ordering, and that PUT/DELETE/other methods keep
  their old answers byte-for-byte.
- Additivity: hub/member docs pages, landing, and the API surface still serve; the new
  pages carry no `{{` token and no per-instance value.

## Implementer TODO

- [x] Task 1 Step 1: frontpage theme tests (red)
- [x] Task 1 Step 2: `skill.go` + `internal/frontpage` implementation (green)
- [x] Task 1 Step 3: docs theme tests (red)
- [x] Task 1 Step 4: `docs.go`/`handlers.go` theme implementation + front page wants (green)
- [x] Task 1 Step 5: gates + commit `feat: dark theme for the landing and docs pages`
- [x] Task 2 Step 1: pretty-404 tests incl. updates (red)
- [x] Task 2 Step 2: `docs.go` pages + `handlers.go` sites + `sandbin-api.md` (green)
- [x] Task 2 Step 3: gates + commit `feat: pretty branded 404 pages for the bin and page routes`

## Verification (implementer, after Task 2)

- All five gates; top-level test counts vs baseline (271/132/17/2/6): additions plus the
  named updates only — prove with `comm` on sorted test-name lists and
  `git log --oneline` showing exactly two feature commits on the baseline.
- Light-mode proof: extract the served landing + one docs page's `:root{...}` line from a
  live tree server; every light value equals the corresponding pre-change literal
  (`#4E1680`, `#EA9B25`, `#F8F7F4`, `#222`, `#333`, `#444`, `#555`, `#666`, `#8b8171`,
  `#b3a894`, `#e3ddd3`, `#ded7cb`, `#F6EDDD`, `#efeadf`, `#fff`, `#2e7d32`).
- Live-server proof (`sandbin serve` on a scratch port, then cleanup): `GET /` and
  `GET /docs` carry the theme script before `</head>`, the toggle button, both logos;
  `GET /docs/assets/sandbin-logo-dark.svg` is `cmp`-equal to the repo file;
  `GET /deadbeef` → 404 HTML `bin not found`; `GET /nope` → 404 HTML `page not found`;
  create a bin, `GET {hash}/missing.md` → 404 HTML `bin content not found`;
  `PUT /deadbeef/x` with the key → the JSON envelope `bin not found` (unchanged);
  `HEAD /deadbeef` → 404; `PATCH /` → 405 unchanged.
- Cleanup: kill scratch servers via /proc scan, ports clear, `/tmp/opencode` scratch gone,
  repo clean apart from `?? .opencode/`, tags unchanged.

## Reviewer TODO

1. Gates yourself. Counts vs baseline via `comm` on sorted test-name lists: additions plus
   only the named updates; no removals; `git diff 9ba9f7c..HEAD --name-only` is exactly
   `skill.go`, `cmd/sandbin/docs.go`, `cmd/sandbin/handlers.go`,
   `cmd/sandbin/handlers_test.go`, `cmd/sandbin/api_test.go`, `cmd/sandbin/docs_test.go`,
   `internal/frontpage/front.html`, `internal/frontpage/frontpage.go`,
   `internal/frontpage/frontpage_test.go`, `docs/usages/sandbin-api.md`, and the plan doc —
   nothing else.
2. Two atomic commits on the baseline; each commit's gates green
   (`git stash`/`git show` or check out each commit and run the gates).
3. Light-mode equivalence: the `:root` light values equal the pre-change literals one-for-
   one (diff the old `brandCSS`/`front.html` literals against the new variable
   definitions); no light value changed; the only new DOM is the hidden dark logo, the
   toggle button, and the head script (plus the new dark-logo route).
4. Dark mechanics: read the served pages — the script sets `data-theme` on `<html>` before
   paint, the toggle persists `sandbin-theme` and flips, the `.logo-dark` rules swap the
   wordmark, `--sandbin-purple` goes `#E2C2FF` in dark (matching the dark logo's
   calibration comment), amber stays `#EA9B25`.
5. 404 matrix live or via the pinned tests: every near-miss (7/9/upper/non-hex),
   the fallback-beats-pretty ordering, PUT/DELETE envelope byte-identical, other methods'
   plain 404s/405s byte-identical, the 301 redirect intact, `/docs/...` unknown →
   `page not found`, `GET /healthz` → `page not found`.
6. `sandbin-api.md`: every sentence the old plain `unknown bin`/`unknown path` wording
   carried is reworded to the new behavior and the hosted `/docs/sandbin-api.html`
   renders the rewording with no template breakage; the envelope sentences are untouched.
7. Cleanup confirmation: /proc scan clean, ports clear, `/tmp/opencode` scratch gone,
   `git status --short` only `?? .opencode/`, `git tag -l` unchanged.

## Skipped on purpose

- Themed bin listings and stored user content (bin pages are the author's; the user asked
  for landing + docs + the 404 pages only).
- A per-instance theme setting or a URL-driven theme — the choice is the visitor's
  localStorage + OS preference.
- HEAD/POST/PATCH/OPTIONS pretty pages (those are machine answers; the pages are for
  browsing GETs).
- Serving the light logo at a second route or moving the logo routes — the dark logo gets
  its route only because the light one has one.

## Outcome

Implemented on baseline `9ba9f7c`: the theme in `25c5797`, the 404 pages in `8162c3c`, the
plan doc in `0969dc4`. Reviewer verdict: APPROVE WITH FINDINGS — no blockers; every
finding is a nit (F1–F4 below) and none changes behavior.

- Gates: all five gates green at each commit (verified in independent worktrees) and at
  HEAD.
- Counts: `cmd/sandbin` 271 → 274, `internal/frontpage` 17 → 18, the rest unchanged
  (132/2/6). No test removals beyond this plan's named rename
  `TestDocsUnknownPathsArePlainNotFound` → `TestDocsUnknownPathsAnswerTheNotFoundPage`;
  additions are `TestPrettyNotFoundPages`, `TestNotFoundPagesCarryTheDocsShellAndTheme`,
  `TestDocsPagesCarryTheDarkTheme` (cmd/sandbin) and `TestRenderCarriesTheDarkTheme`
  (frontpage).
- Scope: name-only diff is the plan's files plus `cmd/sandbin/openapi_test.go` (deviation
  1); nothing else.
- Light-mode equivalence (reviewer-verified byte-for-byte): the new `:root` light values
  equal the pre-change literals one-for-one and no rule keeps a changed light value. The
  only new light DOM is the hidden dark-logo twin, the toggle button, the head script,
  and the dark-logo route.
- Dark mechanics: the head script sets `data-theme` on `<html>` before paint
  (localStorage `sandbin-theme` first, then `prefers-color-scheme`); the toggle flips and
  persists; the `.logo-dark`/`.theme-sun`/`.theme-moon` display rules are the plan's six
  lines; dark `--sandbin-purple` is `#E2C2FF` per the dark logo's own calibration comment
  and amber stays `#EA9B25` in both themes.
- 404 matrix (reviewer-verified live): missing bin and its subpaths → `bin not found`;
  every near-miss first segment (7/9 chars, uppercase, non-hex) → `page not found`;
  a path inside an existing bin → `bin content not found`; a bin's own `404.html` still
  wins byte-for-byte; `/docs/…` unknown, `/healthz`, `/openapi.json/`, `/openapix.json`,
  `/skills/other` → `page not found`; PUT/DELETE keep the JSON envelope, the other
  methods keep their plain 404s/405s/400s, and the 301 bin-base redirect is intact.
- `docs/usages/sandbin-api.md`: all six stale plain-404 sentences reworded, no stale
  sentence remains, the envelope sentences untouched, and the hosted
  `/docs/sandbin-api.html` renders the rewording with no template breakage.
- Deviations (all ruled FORCED): the `openapi_test.go` update (plan file-list omission —
  `TestOpenAPIServeShapes` pinned the two near-miss spellings the behavior change
  necessarily changes); the test-side `binHashRE` redeclaration removed from
  `api_test.go` (the production declaration in `handlers.go` shares the package); three
  unnamed test updates the GET-404 change forces
  (`TestNotFoundServesTheBinsFallback`, `TestFallbackNeverShadowsAnything`,
  `TestFileGetRefusalsStayPlain` — their 401/405/400 pins stay byte-for-byte); and the
  gofmt expansion of the compact `notFoundPageForSegment` if and one compact test struct.
- Findings (all nits, no behavior change):
  - F1: the Global Constraints' "HEAD answers outside /api/ stay plain" and Task 2 site 4's
    "HEAD answers the same 404 with no body" conflict; the implementation follows the
    specific spec — `HEAD /docs/…` unknown now carries the pretty page's headers
    (`text/html`, full Content-Length) with no body, as spec'd.
  - F2: the implementer's deviation list omitted one more forced, in-scope test update:
    `TestDocsCarryCopyButtonsAndTheLandingDoesNot`'s `.block-copy.copied` pin to the
    variable form (forced by the plan's own `#2e7d32`→`var(--sb-ok)` mapping).
  - F3 (pre-existing, not from this plan): the `cmd/sandbin-client` archive tests assert
    the shared `os.TempDir()` is free of `sandbin-archive-*`, so two concurrent
    `make test` runs on one host can cross-contaminate.
  - F4: this plan's step lists name most but not all forced test updates; the file-level
    lists are exact.
- Cleanup: no scratch sandbin processes, scratch ports refuse connections, `/tmp/opencode`
  holds the implementer's two proof dirs (`0034-baseline`, `0034-current`) with the
  per-package sorted test-name lists, `git status` clean, tags unchanged (ending
  `v0.7.0`).
