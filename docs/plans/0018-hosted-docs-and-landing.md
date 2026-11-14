# Plan: hosted docs under /docs and a redesigned landing page

Status: planned. Spec: `docs/decisions/0018-hosted-docs-and-landing.md` — layout, engine,
landing content and the copy button are settled there. Baseline HEAD is the plan-0017 close-out
`a9e251c` (`cmd/sandbin` 183 + 19, `cmd/sandbin-client` 107 + 28, `internal/version` 4 + 23,
all gates green). Sits on 0016 (the doc tree being hosted) and 0015 (the HTTP surface whose
byte-identity must survive).

**Goal:** the server embeds `docs/usages/*.md` + `docs/assets/sandbin-logo.svg`, renders the
docs to HTML once at startup with gomarkdown, and serves `/docs` (hub), `/docs/<name>.html`
(ten pages) and `/docs/assets/sandbin-logo.svg`; the landing page becomes logo + tagline +
GitHub links + the one-line installer (with a copy button) + doc-set links.

**Architecture:** `skill.go` gains the two embeds; new `cmd/sandbin/docs.go` builds a
startup-rendered page map (exact-name lookup, `.md`→`.html` link rewrite, github-style heading
slug ids, shared branded shell + hub); `ServeHTTP` routes `/docs*` before binRoutes;
`renderFrontPage` is rewritten with the new body (frontBase templating untouched). No client
change, no new route touching bins.

**Tech stack:** Go stdlib + `github.com/gomarkdown/markdown` (new direct dep) + inline
vanilla JS for the one copy button.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Do not touch: bin routes and their semantics, listings, 404 fallback, verify-key, archives,
  DELETE, PATCH metadata, ip family, control socket, `client.go`/`config.go`/store, `SKILL.md`
  and `install.sh` (byte-identical, served bodies included), every operational error wording,
  the CLI layer, `internal/version`.
- `frontBase`/`frontHostRE` behavior stays byte-for-byte; only the page text around it changes.
- Landing page and docs pages reference no external assets except github.com links; the only
  JS is the landing copy button, inline, with the execCommand fallback path (plain-HTTP
  deployments).
- Docs pages come from exact-name lookups into a startup-built map — no path joins, no
  traversal surface; embedded bytes only.
- Never push; never tag the real repo.

## Review Focus

- The byte-identity of everything that is not the landing page: full HTTP battery old-vs-new
  (bins, redirect, fallback, skill routes, listing, error shapes), with the landing page the
  only intentional HTML change.
- The docs crawl: every link in the hub and all ten pages resolves (zero 404s in-server,
  correct `.html` rewrites, `#trusted-proxies` anchor present); no unrendered markdown or
  template tokens leak; logo bytes byte-equal to `docs/assets/sandbin-logo.svg`.
- The landing install command is byte-identical to the served installer flow: the exact text
  the copy button holds works piped through `curl | bash` on a live server.
- Routing: `/docs` before binRoutes cannot shadow bins (hashes are eight-hex) nor change POST
  / creation; method handling matrix.

## Task 1: embed, render and serve the docs

**Files:** modify `skill.go`, `go.mod`, `go.sum`, `cmd/sandbin/handlers.go` (routing only),
`Containerfile` (COPY docs in this same commit — the image build needs the embedded tree);
create `cmd/sandbin/docs.go`, `cmd/sandbin/docs_test.go`

**Interfaces:**
- Produces: `sandbin.UsageDocs embed.FS` (`//go:embed docs/usages/*.md`) and
  `sandbin.LogoSVG string` (`//go:embed docs/assets/sandbin-logo.svg`); `cmd/sandbin/docs.go`:
  startup-built `map[string]hostedPage` (name → {title, rendered html}) built by parsing each
  embedded file (fail-fast at init like the skill templates), a `rewriteDocLinks` pass
  (`](<known>.md[#frag])` → `.html`, other relative `.md` → GitHub blob URL), github-style
  heading id attributes on every rendered heading, `docShell(title, sourceURL, bodyHTML)`
  branded wrapper, `docsHub` page, and the logo handler.
- Routes (GET/HEAD): `/docs` + `/docs/` → hub; `/docs/<name>.html` → page for exactly the ten
  usage names; `/docs/assets/sandbin-logo.svg` → `image/svg+xml` embedded bytes; anything else
  under `/docs/` → the existing plain 404 shape; non-GET methods → the existing 405 shape.

- [ ] Step 1: write failing tests in docs_test.go: all ten pages 200 with their H1-derived
  title; hub 200 linking every page; rewritten links (crawl every href — all resolve);
  `sandbin-lockout.html` contains `id="trusted-proxies"`; logo 200 `image/svg+xml` byte-equal
  to the repo file; unknown `/docs/nope.html` → 404 shape; POST → 405; `GET /docs` and
  `GET /docs/` both serve the hub; no `{{` token in any response; `go build` compiles with the
  embed (docs tree present) — and confirm they fail.
- [ ] Step 2: `go get github.com/gomarkdown/markdown`; implement docs.go; wire ServeHTTP;
  add `COPY docs ./docs` to the Containerfile build stage.
- [ ] Step 3: green + gates; commit `feat: server hosts the usage docs and logo under /docs`.

## Task 2: the landing page

**Files:** modify `cmd/sandbin/handlers.go` (renderFrontPage), `cmd/sandbin/handlers_test.go`
(front-page pins)

**Interfaces:**
- Produces: the new front page: logo (`<img src="/docs/assets/sandbin-logo.svg">`), name, the
  existing tagline sentence, GitHub links (repo at minimum), one install block whose command
  is exactly `curl -fsSL <frontBase>/skills/sandbin-preview/install.sh | bash` with a copy
  button (inline script: navigator.clipboard, textarea+execCommand fallback, "copied"
  feedback; `<noscript>`-free — text is selectable regardless), the `--dest` sentence, and
  links to `/docs`, `/docs/sandbin.html`, `/docs/sandbin-client.html`. Removed: the
  bin-semantics paragraph, the curl trio, the privacy paragraph. frontBase replacement
  unchanged.

- [ ] Step 1: rewrite the front-page pins in handlers_test.go against the new page (content
  presence/absence assertions: tagline present, install command present, doc links present,
  removed paragraphs absent; frontBase templating still applied; host-guard battery still
  passes unchanged). Confirm they fail.
- [ ] Step 2: rewrite renderFrontPage; green; commit `feat: landing page shows the logo, tagline, installer and docs links`.

## Task 3: usage-doc wording for the hosted pages

**Files:** modify `README.md`, `docs/usages/sandbin.md`, `docs/usages/sandbin-client.md`,
`docs/usages/sandbin-serve.md`, `docs/usages/sandbin-client-skill.md`

- [ ] Step 1: serve page: the front-page description sentence updated for the new shape and a
  short `/docs` routes paragraph; the two summaries note they are hosted at `/docs/...` on a
  running server; README: one sentence that a running server hosts these pages under `/docs`
  (GitHub links stay); skill page: one sentence that the landing page carries the installer
  one-liner. Keep every existing body claim byte-identical except these additions.
- [ ] Step 2: run the touched examples verbatim; commit `docs: document the hosted docs pages`.

## Verification (implementer, after Task 3)

- All five gates; test counts per package before/after (baseline above) with `comm` on
  test-name lists; front-pin rewrites justified.
- Side-by-side vs baseline binaries (`git archive a9e251c` build under /tmp/opencode): full
  HTTP battery byte-identical everywhere except the landing page (list each route); landing
  diff is exactly the intended content; SKILL.md + install.sh served bodies byte-equal;
  piped installer install + upload round trip through the new server still works.
- Docs crawl yourself: fetch hub + 10 pages, extract every href, resolve all (zero 404s),
  anchors exist; logo byte-equal; method matrix (GET/HEAD 200, POST 405, unknown 404).
- Copy button: fetch the landing page, extract the installer command text from the block, run
  it verbatim piped against a live server (non-tty shape), and confirm the page references no
  external scripts/fonts (grep every src=/href= across landing + hosted pages: only self,
  /docs paths and github.com).
- Cleanup: kill servers via /proc scan (pid via basename, Z ignored), ports 18286-18288
  clear, `/tmp/opencode` EMPTY at the end, repo clean apart from `?? .opencode/`, tags
  unchanged.

## Reviewer TODO

1. Gates yourself; counts vs baseline; `comm` old-vs-new test names — removals only with
   justification (front-pin rewrites are the expected kind).
2. Scope: `git diff <plan-open>..HEAD --name-only` names only Task-listed files; go.sum
   additions are gomarkdown + its transitives only (`go mod why`, `go mod verify`); SKILL.md,
   install.sh, client tree, `internal/version` byte-identical.
3. Full HTTP byte-identity battery vs baseline binaries (bins, 301, fallback, listing,
   archives, metadata, DELETE, verify-key, 405/404/403/413 shapes, skill routes) with the
   landing page the single intentional diff — quote the diff summary.
4. Docs battery yourself: hub + ten pages; crawl every href and anchor (zero unresolved);
   verify the eight 0016 cross-reference links landed as `.html` forms; logo bytes == repo
   file; content types; `/docs` vs `/docs/`; method matrix; no `{{` tokens; embedded-render
   sanity: tables render as tables, code fences render, heading ids follow the slug rule
   including `Trusted proxies` → `trusted-proxies`.
5. Routing safety: POST / still creates; a GET to an existing bin's base still serves;
   `/docs` cannot be reached as a bin; `/docs/..%2f`-style traversal attempts 404; HEAD
   bodies empty.
6. Landing battery: the install command text extracted from the page piped through
   `curl | bash` installs (reuse the scratch --dest convention); copy-button markup present
   with both clipboard paths (navigator.clipboard + execCommand fallback) and no external
   script src; removed content really gone; tagline + GitHub links present; frontBase guard
   battery re-run (odd Host → placeholder).
7. Container proof: build from HEAD (`--network=host`, COPY docs works, gomarkdown resolves
   in-build), serve on the standard volume/-p 127.0.0.1:18289:8080 convention, fetch `/docs`,
   one rendered page and the logo, `--version` stamp unaffected; full podman cleanup per the
   standing keep-list.
8. Docs battery: touched sentences in README + the four usage pages accurate against measured
   behavior; the 0016 coverage claim stays true for the touched pages (byte-identical bodies
   apart from the documented additions).
9. Cleanup confirmation: /proc scan clean of live sandbin, ports clear, `/tmp/opencode`
   empty, `git status --short` only `?? .opencode/`, `git tag -l` unchanged.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: dark mode, favicon wiring, cache
headers, docs pages for plans/decisions, and any change to bin-page HTML.
