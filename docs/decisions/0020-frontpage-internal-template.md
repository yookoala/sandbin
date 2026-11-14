# Decisions: the landing page moves to an embedded template in an internal package

`renderFrontPage` today carries the whole page as a Go const string and assembles it with
`strings.NewReplacer` (three token swaps) plus a string-concatenated header. The page moves to an
embedded `.html` template file in a new `internal/frontpage` package, rendered with
`text/template` — the same posture the SKILL.md/install.sh templates already use. Rendering-only
refactor: the served bytes must not change. User-driven request.

| Point | Decision |
|---|---|
| New package | `internal/frontpage` (package `frontpage`): `front.html` (the whole landing page) embedded via `//go:embed`, parsed once at package init with `template.Must` (a broken template stops the server at startup, like `skillTemplate`/`installTemplate` and the docs pages), and `Render(w io.Writer, d Data)` executing it straight into the writer. `Data{Base, Logo, BrandCSS string}`. |
| What the template holds | The full page markup, header included: the `<header class="page-head">…</header>` block that today is concatenated in `renderFrontPage` becomes literal markup in `front.html` with `{{.Logo}}` where `logoBlock("wordmark wordmark-hero")`'s wrapper content lands — `<div class="wordmark wordmark-hero">{{.Logo}}</div>`, reproduced byte-for-byte. The three replaced tokens become template actions: `{{.BrandCSS}}` in the `<style>`, `{{.Base}}` in the install command (the `https://this.host` literal leaves the template entirely — frontBase already returns the placeholder as a value for odd hosts). |
| What stays in `cmd/sandbin` | All security logic untouched: `frontBase`, `frontHostRE`, the host/XFF guard behavior. `brandCSS` stays a `docs.go` constant — it is shared with the docs shell — and arrives as data; `logoBlock`/`docHead`/`docShell` stay where the docs need them. `renderFrontPage` shrinks to: build `frontpage.Data{Base: s.frontBase(r), Logo: sandbin.LogoSVG, BrandCSS: brandCSS}`, set the content type, execute; its failure path mirrors the skill routes' existing pattern (log + 500 `render failed`) rather than inventing new wording. |
| Engine | `text/template`, not `html/template`: same posture as install.sh/SKILL.md (the only dynamic value, Base, is already validated whole by `frontHostRE`; Logo and BrandCSS are constants this repo owns), and `html/template`'s contextual CSS/JS escaping would sit between the branding CSS and the page for zero added safety. No new escaping anywhere. |
| Byte identity | The refactor changes only how the page is built. The served landing page must be byte-identical old-vs-new across the whole host/XFF/TLS matrix — the existing front-page pins in `handlers_test.go` stay untouched and passing unchanged is the refactor's proof. |
| Tests | New `internal/frontpage` battery: template parses at init; `Render` fills every action (no `{{` token survives), carries the logo inside the hero wrapper, and the base into the install command; output deterministic across calls. `cmd/sandbin` front pins unchanged (byte identity), `frontBase` guard battery unchanged. |

## Skipped on purpose

Moving `brandCSS`/the docs shell or the logo embed into the new package (they are shared and
stay put), extracting other HTML surfaces (listings, 404 fallback, bin pages) into the package,
`html/template`, any page content or style change, template blocks/partials sharing between the
landing page and the docs shell, and handler-level caching of the rendered page.
