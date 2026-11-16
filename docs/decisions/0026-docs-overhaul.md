# Decision 0026: a swagger-style API page, a restructured doc set, and templated, copy-button docs

Decided across the mockup review rounds at HEAD `1f5469a`, previewed at
`https://preview.yookoala.dev/951c2bcb/` (`api-swagger.html/.md`, `serve-restructured.html/.md`,
`ip-blocking-restructured.html/.md`, `overview-restructured.html/.md`). The approved mockup
sources are preserved next to the plan at `docs/plans/0026-docs-overhaul/` (four markdown
drafts and a `docs.go.mockup` showing the pipeline patch exactly as browser-verified).

## Why

`sandbin-api.md` was prose-era: endpoint+method sections flat at one heading level, outputs as
in-code comments, a placeholder `your-key` and `http://localhost:8080` in every example. The
bundled docs could not name their own instance. Code blocks in docs had no copy affordance
even though the landing page's does. The lockout and blocked-address pages split one machinery
across two pages, and the serve page carried sections (`--version` stamping, landing/docs
description) that do not belong to "running the server".

## Decisions

1. **API page structure is swagger-style.** Grouped by path first (`## /`, `## /{hash}/`,
   `## /{hash}/{path}`, `## /{hash}/{folder}/`, `## /verify-key`, `## /healthz`), methods under
   each path at `###`, and a method's distinct usages split at `####` (the `PUT` case: "Plain
   uploads" and "Archive uploads unpack themselves"). A `## Conventions` section opens the
   page. The API page documents bin APIs only: the landing page, hosted docs, `robots.txt` and
   skill routes are not on it — their coverage lives on the pages that own them.
2. **Example style.** The API key is `$API_KEY`, introduced in Conventions with a one-time
   `export API_KEY=your-key`. Expected outputs are never code comments: every example block is
   a pure runnable command, followed by a short paragraph stating what it answers. Long curl
   commands break one option per line with two-space continuation indent; short fetches stay
   single-line. No backslash ever carries a trailing space.
3. **Bundled docs render per request through `text/template`.** The stored page HTML of every
   hosted page (each doc-set page and the hub) is parsed once at build time — a parse failure
   panics at init the way the embedded-tree mismatches do, and each page is also executed once
   with sample data at init so a bad field name is a startup panic, not a runtime surprise —
   and executed per request with `docData{Instance: docInstance{URL: s.frontBase(r)}}`: the
   same trust rule the landing page and skill routes already use. Examples carry
   `{{ .Instance.URL }}`, so hosted docs name their own instance. The mockup token `.Base`
   was renamed to the readable nested `.Instance.URL`; Go naming writes the field `URL`.
   Markdown served from bins never takes this pass: bins store raw bytes and `gomarkdown` is
   used only by `docs.go`. A runtime execute failure logs and serves the unexpanded page —
   unreachable for the embedded tree, which init already executed.
4. **Docs code blocks get copy buttons.** `brandCSS` gains `.codeblock`/`.block-copy` rules and
   `docShell` emits a `docCopyJS` script before `</body>`: it wraps each `main pre`, adds a
   button carrying the landing page's copy-icon glyph, copies with the landing's pattern
   (Clipboard API first, `execCommand` textarea fallback) and flashes a green check for 1.4s.
   The button's center aligns with the center of the code's first line — exact vertical center
   for a single-line block, top-right for a multi-line one — measured from the `pre`'s padding
   and computed line-height. Bins' own rendered pages (listings, ANSI) keep no copy buttons:
   the script ships only in the docs shell.
5. **The serve page is download + build + serve, nothing else.** New sections "Download the
   source" (git clone, tag checkouts, `go install @vX.Y.Z` from the module proxy) and "Build
   the server" (Go 1.26.10-or-newer, `make` targets, `VERSION=` override, why plain `go build`
   says `dev (hash)`); "The `serve` subcommand" spells out the API key setup (flag vs
   `SANDBIN_API_KEY`, precedence, refuses-without-a-key) and a Flag/Default/Meaning table of
   core settings. "What `--version` reports" and the landing/docs paragraph are removed from
   the usage pages (the version design lives in `docs/decisions/0017`).
6. **The lockout and ip pages merge into one page, "IP Address Blocking"
   (`sandbin-ip-blocking.md`)**, covering the security design (key-checked requests — now
   correctly including `PATCH`, which `patchBin` charges exactly like the others — the
   30-minute escalation, `429`/`Retry-After`, GET-exempt rule), address detection
   (`X-Forwarded-For` trust, the self-naming caveat), Trusted proxies, the control socket, the
   `ip` family, the socket's HTTP shape, and the container flow. `sandbin-lockout.md` and
   `sandbin-ip.md` are deleted; the hub's server set becomes serve, api, IP Address Blocking,
   podman. All links into either old page retarget to the new one, `sandbin-client.md`
   included.
7. **The summary page hosts the non-API routes.** `sandbin.md` gains "Beyond the API": the
   landing page (one compact paragraph) and `GET /robots.txt` (the only documented home of the
   robots behavior, which leaves `sandbin-api.md`), and its table carries the merged page's row
   in place of the two old rows.
