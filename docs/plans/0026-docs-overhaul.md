# Plan: swagger-style API docs, a restructured doc set, and a templated copy-button docs pipeline

Status: implemented and closed. Commits `4d0883d` (pipeline: per-request docs templating with
init-time parse/sample-execute, copy buttons) and `84a7887` (content: swagger API page, serve
page restructure, lockout+ip merged into IP Address Blocking, summary page owns the non-API
routes), plus `4520b54` (review follow-up: page-count comments refreshed to nine, and the podman
page now explains its brace escape). Counts `cmd/sandbin` 210
(grew from 202, no test removed), `cmd/sandbin-client` 107, `internal/frontpage` 17,
`internal/robots` 2, `internal/version` 6, all gates green at each commit and at the close.
Review verdict: accept, no FAILs; its three WARNs — the podman escape note, the stale "ten"
comments, and the API page's out-of-scope route *mentions* (framed as pointers to the owning
pages, matching decision 1, so kept) — landed or were dismissed as recorded. The implementation's
two judgment calls, both reviewed as sound: raw `{{...}}` strings quoted in the podman and
client-skill pages had to gain the `{{`{{`}}` escape the moment docs themselves take the
template pass (they joined the pipeline commit), and the plan's claim that current page texts
carry no tokens was wrong for exactly those two files. Spec:
`docs/decisions/0026-docs-overhaul.md` — its seven items settle the page
structure, example style, per-request templating, copy buttons, the serve page's scope, the
lockout+ip merge, and the summary page's new section. Baseline HEAD is `1f5469a`
(`cmd/sandbin` 202 tests, `cmd/sandbin-client` 107, `internal/frontpage` 17, `internal/robots`
2, `internal/version` 6, all gates green).

Design source of truth: the approved mockups, preserved at `docs/plans/0026-docs-overhaul/` —
`api-swagger.md`, `serve-restructured.md`, `ip-blocking-restructured.md`,
`overview-restructured.md` are the exact final texts of the four usage pages (they went
through the real renderer; preview copies at `https://preview.yookoala.dev/951c2bcb/`), and
`docs.go.mockup` carries the pipeline patch (template pass + copy CSS + `docCopyJS`) exactly as
browser-verified. Two corrections apply when porting `docs.go.mockup` to the real
`docs.go`: the doc set in it lists the merged page (land it together with the content commit),
and the template pass must parse-and-sample-execute at init (decision item 3) instead of the
mockup's per-request parse.

**Goal:** the hosted docs become the approved mockups — a swagger-style API page, a
download/build/serve page, one IP Address Blocking page replacing lockout+ip, a summary page
owning the non-API routes — rendered per request with the instance's own address wherever a
`{{ .Instance.URL }}` token appears, every docs code block copyable, and every test gate green
with no removed pins.

**Architecture:** two atomic commits. (A) pipeline-only in `cmd/sandbin/docs.go`:
`text/template` pass over stored pages at request time with data parsed/sample-executed at
init, plus the copy-button CSS and script — the current page texts ride the new pipeline
untouched (they carry no tokens). (B) content: replace `docs/usages/sandbin-api.md`,
`sandbin-serve.md`, `sandbin.md`; add `sandbin-ip-blocking.md`; delete `sandbin-lockout.md`
and `sandbin-ip.md`; retarget `sandbin-client.md`'s lockout link; update `docSets`; re-pin the
tests the content moves. `expandDoc`/`writeDocsPage`/`serveDocs` keep their route shapes: GET
and HEAD answer, everything else the plain 404/405 shapes, the logo route untouched.

**Tech stack:** Go stdlib `text/template` over the already-rendered page strings; the existing
gomarkdown pipeline; plain CSS and the existing ES5-style script pattern for the copy buttons;
no new dependencies.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Never push; never tag; tags v0.1.0..v0.4.2 stay where they are.
- Do not touch: bin routes and their rendering (bins never template), landing/frontpage and its
  copy button, skill routes/templates, the `robots.txt` handler branch, `frontBase`/`frontHostRE`
  (read-only use), lockout/store/CLI behavior (docs-only restructure), `cmd/sandbin-client`
  code, `internal/*` packages, `scripts/`, `Makefile`, `Containerfile`.
- Test pins: baseline counts 202/107/17/2/6 — pins may be re-pointed at the new page names and
  wording, none deleted; new behavior adds pins. `cmd/sandbin` count may grow, never shrink.
- Commit split stays as planned: A = pipeline (`cmd/sandbin/docs.go` + its tests), B = content
  (`docs/usages/*` + `docSets` + test re-pins). Each commit passes the gates on its own.

## Implementer TODO

1. Port the pipeline onto `cmd/sandbin/docs.go` from `docs.go.mockup`, keeping the mockup's
   `docCopyJS` and CSS byte-faithful (including the first-line alignment placement loop):
   a. `docInstance`/`docData` types; `expandDoc` executes a stored template with
      `{Instance: {URL: s.frontBase(r)}}`.
   b. `hostedPage` grows a parsed `*template.Template` (keep the raw html as the log-and-serve
      fallback); `buildDocsPages` parses each page and executes it once with sample data,
      panicking on either failure; `buildDocsHub` returns a parsed template the same way.
   c. `serveDocs` answers hub and pages through the per-request execution; the 404/405/logo
      shapes stay byte-identical.
   d. `brandCSS` gains the `.codeblock`/`.block-copy` rules; `docShell` emits `docCopyJS`
      before `</body>` (docs shell only — listings and ANSI keep no copy buttons).
2. Add pipeline pins to `cmd/sandbin/docs_test.go`:
   a. a docs page served with a chosen `Host`/`X-Forwarded-Proto` shows that address where a
      fixture page carries `{{ .Instance.URL }}`, and no token survives;
   b. every bundled page in `docSets` executes cleanly with fresh sample data (walks the real
      embedded tree);
   c. a bin file holding `{{ .Instance.URL }}` through `PUT` then `GET` is byte-identical
      (bins never template);
   d. docs pages carry the copy script and CSS; the landing page does not gain them.
3. Land the content commit from the preserved drafts, exactly as approved:
   a. replace `sandbin-api.md` (from `api-swagger.md`), `sandbin-serve.md` (from
      `serve-restructured.md`), `sandbin.md` (from `overview-restructured.md`);
   b. add `sandbin-ip-blocking.md` (from `ip-blocking-restructured.md`), delete
      `sandbin-lockout.md` and `sandbin-ip.md`, and swap the `docSets` entry to
      `sandbin-ip-blocking.html`;
   c. retarget `sandbin-client.md`'s "API key lockout" link to the new page;
   d. grep `docs/usages/` for any remaining link to a deleted page — zero may survive.
4. Re-pin the moved tests in `cmd/sandbin/docs_test.go` (page list, titles, the lockout-page GET
   becomes the IP Address Blocking page, hub listing, trusted-proxies href, the robots-sentence
   home moves to `sandbin.html`); touch `handlers_test.go` only where a docs-body pin references
   the old pages. Landing/frontpage tests stay byte-identical.
5. Run the gates; record the new per-package counts in the plan record's Status line.

## Reviewer TODO (independent of the implementer's reading)

1. Read `docs/decisions/0026-docs-overhaul.md` items 1–7 and verify each against the committed
   files, not the diff narrative: the api page's heading hierarchy (path `##` → method `###` →
   usage `####`, exactly the sections the mockup carries), `$API_KEY` count matches the
   key-checked examples, zero `# ->` output comments inside any fenced block, zero backslash +
   trailing space, every long curl one option per line.
2. Pipeline: verify init-time parse + sample-execution (panic paths reachable by reading the
   code), per-request `frontBase` reuse (no second host-trust logic), fallback logs, and that
   the 404/405/logo route shapes are byte-identical to baseline; confirm nothing outside
   `serveDocs` templates anything, and that bin routes, landing, skill and robots handlers are
   untouched (`git diff` against `1f5469a` for those files must be empty).
3. Copy buttons: compare `docCopyJS` against `docs.go.mockup` — clipboard pattern identical to
   the landing's (Clipboard API → execCommand fallback), the placement math (first-line center)
   intact, and only `docs.go`'s shell carries the script.
4. Doc set: built pages answer for the new set, old `/docs/sandbin-lockout.html` and
   `/docs/sandbin-ip.html` answer the plain 404 shape, the hub lists exactly the merged set,
   the summary table matches `overview-restructured.md`, and the merged IP Address Blocking
   page includes the corrected key-checked list with `PATCH` (spot-check `patchBin` charges).
5. Cross-links: grep every `docs/usages/*.md` and the rendered pages for
   `sandbin-lockout.md`/`sandbin-ip.md` references — none; grep for accidental GitHub-blob
   rewrites on links that should be hosted pages.
6. Tests: run all five gates yourself; verify no baseline test was deleted (compare test
   inventories at `1f5469a` vs HEAD), new pins actually assert the new behavior (not weakened
   shadows), and the counts recorded in the plan Status match `go test -v` reality.
