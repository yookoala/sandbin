# Plan: client doc set restructure — Building and Config page, verb-grouped upload page, sectioned skill page

Status: open. Spec: `docs/decisions/0027-client-docs-restructure.md` — its seven items settle
the merged setup page, its three-group structure, the verb-grouped upload page, the client
example style, the skill page's new title/sections with template-coverage removal, the serve
page tag refresh, and cross-link retitles. Baseline HEAD is `5e12292` (`cmd/sandbin` 210 tests,
`cmd/sandbin-client` 107, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6,
all gates green).

Design source of truth: the approved mockups, preserved at `docs/plans/0027-client-docs-restructure/`
— `client-init-restructured.md`, `client-upload-restructured.md`, `client-skill-restructured.md`,
`client-overview-restructured.md` are the exact final texts of the four client pages (all went
through the real renderer; preview copies at `https://preview.yookoala.dev/951c2bcb/`),
`serve-v043.md` is the refreshed serve page (five `v0.4.2` references become `v0.4.3`, nothing
else changes), and `docs.go.clientset.patched` shows the `docSets` swap. Two small content
retargets are NOT in the preserved drafts' own files and must be applied to the live pages:
`sandbin-api.md`'s `POST /verify-key` pointer becomes
`[Building and Config](sandbin-client-init.md)` (it spans a line break in the current text),
and `sandbin.md`'s "Beyond the API" pointer to the skill page takes the new title (also
currently spanning a line break).

**Goal:** the hosted client docs become the approved mockups — `sandbin-client-init.md`
("Building and Config") first in the client set with the two-ways-getting-the-client structure
and the merged `init` coverage, `sandbin-client-upload.md` grouped by verb with its title and
file kept, `sandbin-client-skill.md` retitled and segmented with all template machinery gone,
the client summary and hub in the new order, the serve page naming v0.4.3, `sandbin-client-config.md`
deleted with zero surviving references, and every test gate green with no removed pins.

**Architecture:** one content commit. The docs pipeline (plan 0026) is untouched — these pages
already ride it; only the embedded markdown tree, the `docSets` client pages list, and the test
pins change. A content commit and its test re-pins land together because the pins name the
pages.

**Tech stack:** existing gomarkdown + template pipeline; no code paths change beyond the
`docSets` literal.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Never push; never tag; tags v0.1.0..v0.4.3 stay where they are.
- Do not touch: `cmd/sandbin/docs.go` beyond the one `docSets` pages literal, the docs pipeline
  code (template pass, copy script, shells), bin routes and their rendering, landing/frontpage
  and its copy button, skill routes/templates (`skills/`, `skill.go` embed literal — the file
  count stays nine so no comment changes there), the `robots.txt` handler, `frontBase`/`frontHostRE`,
  lockout/store/CLI behavior, `cmd/sandbin-client` code, `internal/*` packages, `scripts/`,
  `Makefile`, `Containerfile`, and the plan/decision records under `docs/plans/0027-client-docs-restructure/`.
- Test pins: baseline counts 210/107/17/2/6 — pins may be re-pointed at the new page names,
  titles and wording, none deleted; new structure adds pins. `cmd/sandbin` count may grow,
  never shrink. The hosted page count stays nine (config→init swap nets zero), so the "nine"
  comments in `docs_test.go` comments stay true.
- The four rewritten pages must be byte-identical to their preserved mockups; the serve page
  byte-identical to `serve-v043.md`.

## Implementer TODO

1. Land the content, byte-for-byte from the preserved drafts:
   a. add `docs/usages/sandbin-client-init.md` (from `client-init-restructured.md`) and
      `docs/usages/sandbin-client-skill.md` ← `client-skill-restructured.md`;
   b. replace `docs/usages/sandbin-client.md` ← `client-overview-restructured.md`,
      `docs/usages/sandbin-client-upload.md` ← `client-upload-restructured.md`,
      `docs/usages/sandbin-serve.md` ← `serve-v043.md`;
   c. delete `docs/usages/sandbin-client-config.md`;
   d. retarget `docs/usages/sandbin-api.md`'s verify-key pointer and `docs/usages/sandbin.md`'s
      skill pointer to the new titles/pages (the only edits to those two files);
   e. `cmd/sandbin/docs.go` client set becomes
      `[]string{"sandbin-client-init.html", "sandbin-client-upload.html", "sandbin-client-skill.html"}`.
2. Grep the whole repo for `sandbin-client-config` and `Give the skill to your agent`: only the
   historical records under `docs/plans/` and `docs/decisions/` may still contain them.
3. Re-pin `cmd/sandbin/docs_test.go`: the hosted-name list and title map swap
   `sandbin-client-config.html`/`Point sandbin-client at a server` for
   `sandbin-client-init.html`/`Building and Config` and the skill page's new title; the hub
   expectation gains the new order (init before upload before skill); re-pin any body-wording
   assertion that quotes old upload/config/skill page text truthfully (one-for-one where old
   sentences existed).
4. Add new structure pins to `cmd/sandbin/docs_test.go` (patterned on
   `TestDocsAPIPageIsSwaggerStyle`):
   a. the init page carries `## Installing with `go install``, `## Building from Source`,
      `## init` at h2 and the decided h3 subsections, and the old config page's
      `/docs/sandbin-client-config.html` answers the plain 404 shape;
   b. the upload page carries exactly h2 `upload`/`delete`/`describe` plus
      `rules-that-hold-for-every-run`;
   c. the skill page carries exactly h2 `general-install`/`install-for-specific-ai-harness`, its
      new `<title>`, and contains no `{{` brace-escape sequence and no template-machinery words
      (`text/template`, `{{.Base}}`) in its served body.
5. Run the gates; record the new per-package counts in the plan record's Status line.
6. Sanity-serve a scratch binary on `127.0.0.1:18307` (temp db + key): with
   `Host: preview.yookoala.dev` + `X-Forwarded-Proto: https`, the init/upload/skill pages render
   the host address wherever a token sits and no `{{` survives; `/docs/sandbin-client-config.html`
   404s; the hub and both summary pages show the new titles/order; the api page's verify-key
   link points at `sandbin-client-init.html`. Kill the scratch and remove all scratch files.

Commit message shape: `docs: merged client setup page, verb-grouped upload page, sectioned skill page`.

## Reviewer TODO (independent of the implementer's reading)

1. Read `docs/decisions/0027-client-docs-restructure.md` items 1–7 and verify each against the
   committed files, not the diff narrative. `cmp` the four client pages against the preserved
   mockups and the serve page against `serve-v043.md` (byte-identical); verify the only edits to
   `sandbin-api.md` and `sandbin.md` are the two pointer retargets (`git diff` those files).
2. Fact-survival audit against the two superseded pages (`git show 5e12292:docs/usages/sandbin-client-config.md`
   and `.../sandbin-client-upload.md`): every fact must land in the new pages except the
   decision-approved removals. Config facts to find on the init page: Enter-keeps-shown-default;
   `POST /verify-key` check with both refusal texts and writes-nothing; prompt-echoes-typed-key /
   client-never-prints-key; no-terminal flags/env fallback, url fallback, missing-key refusal
   naming; the re-init guard (terminal warning text, `re-initialize? [y/N]`, `y`/`yes` semantics,
   bare-Enter exits 0 untouched, no-terminal refusal text, flags/env-not-consent, `--reinit`
   consent + no-op, re-verify-and-rewrite, keeps `bin`/`protected`, byte-identical unchanged
   inputs); the file section (yaml shape, `0600`, temp-rename, `.gitignore` three cases,
   init-only `api_key`, upload-run writes url/bin/protected and keeps the key, bin password
   env-only, JSON-state-file farewell); precedence chain plus the CI-override rationale.
   Upload-page facts: every old sentence's content survives under its new verb section (spot-check
   the long ones: all ten folder-check refusals, the delete name-rule refusals and 404/order/one-DELETE
   semantics, the metadata limits and visibility rules, the `SANDBIN_BIN_PASSWORD` wire semantics).
3. Skill page: verify decision 5's removals actually happened (no template-machinery prose, no
   brace escape, `{{.Base}}` absent from the file) and that its functional facts all survive
   (installer route, default folder + OpenCode pickup, pinned build end-to-end version identity,
   `@latest` fallback, no-Go exit-0 with first-use build, `init --url` run with its single
   question, guard-question behavior, failed-init warning text still exiting 0, no-terminal
   printed command with the tokenized url, `-d`/`--dest`, `bash -s --`, Claude example,
   `--help`/`unknown argument`, the landing picker description matching `internal/frontpage/front.html`'s
   current labels "Tell Your Agent to use This" and "Skill will be installed to:").
4. Token discipline: list every literal `http://localhost:8080` left in the three rewritten
   client pages and confirm each is a built-in-default fact per decision 4, not an example URL;
   run a scratch server and confirm token render under a chosen Host/X-Forwarded-Proto and zero
   leftovers on every client page.
5. Route behavior: `/docs/sandbin-client-config.html` and its 404 shape; hub lists nine pages in
   the new order with both new titles; `/docs/sandbin-client.html` table and `sandbin.md`'s
   Beyond-the-API pointer carry the new titles; api verify-key pointer retargeted; grep the
   rendered pages for `sandbin-client-config` and the old skill title (zero).
6. Tests: run all five gates yourself; compare test function inventories at `5e12292` vs HEAD —
   none deleted, no assertion weakened (read the `docs_test.go` diff fully); new structure pins
   assert the decided headings (not weakened shadows); recorded counts match
   `go test -count=1 <pkg> -v | grep -c '^--- PASS'` reality; `git diff 5e12292..HEAD --name-only`
   touches nothing outside `docs/usages/`, `cmd/sandbin/docs.go`, `cmd/sandbin/docs_test.go` and
   the plan/decision records.
7. Copy-button + pipeline untouched: `git diff 5e12292..HEAD -- cmd/sandbin/docs.go` shows only
   the `docSets` literal; the landing page still has no copy script; bin-served bytes still
   never template.
