# Decisions: hosted docs under /docs and a redesigned landing page

The server embeds its usage documentation and logo and serves both under `/docs`; the
landing page shrinks to logo, tagline, GitHub links, the one-line skill installer (with a
copy button), and links into the hosted docs. User-driven; layout and engine choices are the
user's.

| Point | Decision |
|---|---|
| What is hosted | The ten `docs/usages/*.md` files and `docs/assets/sandbin-logo.svg`, embedded at build time via the module-root package (`skill.go` gains `//go:embed docs/usages/*.md` into an `embed.FS` and `//go:embed docs/assets/sandbin-logo.svg`). The server never reads `docs/` from disk at runtime; the Containerfile gains `COPY docs ./docs` in the same commit that adds the embed, so the image keeps building. |
| URL layout (user's choice: HTML only) | `GET /docs` and `/docs/` → a hub page presenting the two doc sets with links. `GET /docs/<name>.html` → the rendered page for each of the ten usage files, names matching the files (`sandbin.html`, `sandbin-client.html`, `sandbin-serve.html`, `sandbin-api.html`, `sandbin-lockout.html`, `sandbin-podman.html`, `sandbin-ip.html`, `sandbin-client-upload.html`, `sandbin-client-config.html`, `sandbin-client-skill.html`). No raw `.md` route. The logo is served at `/docs/assets/sandbin-logo.svg` (`image/svg+xml`), matching its repo path. Unknown `/docs/...` paths are plain 404s in the existing shape; non-GET methods answer 405 as elsewhere; "docs" is not an eight-hex bin hash, so routing order (docs before binRoutes) removes any collision. |
| Markdown engine (user's choice) | `github.com/gomarkdown/markdown` as a new direct dependency — pure Go, renders the GFM tables the summary pages use. Parse + render each embedded file once at startup (same posture as the skill templates: a broken doc fails fast at boot) and cache the output. |
| Anchors | Headings carry github-style slug ids (lowercase, spaces to dashes, punctuation dropped), added after rendering if the engine does not emit them, so cross-page links like `sandbin-lockout.html#trusted-proxies` resolve. |
| Link rewriting | Before rendering, relative links to known doc files rewrite `.md` → `.html` (including `#fragment` forms); http(s) links pass through; any relative link to something not hosted rewrites to its `https://github.com/yookoala/sandbin/blob/main/...` source. Each hosted page gets a branded shell: logo, title linking back to `/`, a link to `/docs`, and a GitHub source link for that page. |
| Landing page (user's choice) | Logo + name + the existing one-line tagline; links to GitHub; the one-line installer as the only command shown, templated through `frontBase` exactly as today (plus the `--dest` sentence); links to the two doc summaries and the hub. The bin-semantics paragraph, the curl trio, and the privacy paragraph are removed — they live in the hosted API docs. `frontBase`/`frontHostRE` security behavior is untouched. |
| Copy button (user's request) | Small inline vanilla JS on the landing page only: a copy button on the installer block copies its exact text; `navigator.clipboard` where available (secure contexts), otherwise the select-textarea + `document.execCommand("copy")` fallback — measured feedback text ("copied"), no external script or font is referenced. With JavaScript disabled the page is fully usable and the command is plain selectable text. Hosted docs pages stay JS-free. |
| Style | One shared branding style used by the landing page and the docs shell: the logo's palette (purple `#4E1680`, amber `#EA9B25`, canvas `#F8F7F4`), system fonts, a max-width column, styled code blocks; inline `<style>` only, no external assets. Bin listings, bin pages and the 404 fallback keep their current look. |
| Routing security | Doc pages are chosen by exact name lookup into the startup-rendered map — never a filesystem join, so no path traversal; the logo is embedded bytes only. Pages contain no per-bin data and no key echo, like today's front page. |
| Tests | Front-page pins in `handlers_test.go` are rewritten against the new page. New `/docs` battery: hub and all ten pages, every internal docs link resolves (crawl, zero 404s), the `#trusted-proxies` anchor exists, logo bytes byte-equal to the repo file, content types, method handling, unknown-path 404 shape, no unrendered template tokens. Byte-identity batteries for SKILL.md, install.sh, listings, 404 fallback and the rest of the HTTP surface stay. |

## Skipped on purpose

Serving raw `.md` under `/docs`, mirroring the `docs/usages` tree in URLs, docs search,
syntax highlighting, dark theme, hosting `docs/plans`/`docs/decisions`, cache/ETag headers,
version display on the landing page, JS on the docs pages, client output changes, and links
from hosted docs back into the installer flow.
