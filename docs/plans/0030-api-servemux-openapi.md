# /api on ServeMux + a generated OpenAPI 3.1 document — Implementation Plan

Status: implemented and closed 2026-11-17. Nine commits, `e0035e6` (plan+decision) through
`a369410` (docs polish): Tasks 1–5 at `0079c92`+`e33713f`, `da207bb`+`6ac4f36`, `9a5bb78`,
`bf95bc8`, `a425620`, with the preflight plan correction `db01f7e` and the final fix wave
`ffa4084`+`492407b`+`1ec5c9a`+`a369410`. Spec: `docs/decisions/0030-api-servemux-openapi.md`
(including its execution addendum). Review outcome: every task passed an independent
task-scoped review — Task 1 needed one fix round (literal wildcard-key charging flip, ruling
R3) and Task 2 one (the jq post-generation filter, ruling R4), both re-reviewed clean; Tasks
3 and 4 passed first time. The whole-branch review (`3574887..a425620`) found no Critical
issues and six Important ones (the Makefile could clobber its tracked output; the published
document omitted the `Location`/`Retry-After` headers, `required` lists,
`additionalProperties: false`, and the pinned error sentences, and carried Go-internal prose);
the single fix wave closed all eleven findings, its scoped re-review verdict was all ADDRESSED
with no new breakage. Controller rulings R1–R5 all honored; no test deleted or re-pointed
(the route-table pin gained exactly one entry per ruling R5, and the pairing pin per the
final review); final gates green at `a425620`..`a369410` with `cmd/sandbin` 253,
`cmd/sandbin-client` 111, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6.
Carried Minors parked with rulings in the SDD ledger (comment overstates `-exclude-package`
inertness on v0.5.11, jq corrections 11–13 auto-create intermediates, rewritten PATCH/key
prose dropped the body-before-lookup order and the successful-check charges-nothing detail,
docs cosmetics).

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hand-rolled `apiRoutes` switch with a standard `http.ServeMux` (Go 1.22
method patterns) fronted by an authoritative route table, express the key check as a registered
`apiKey` wrapper, generate an OpenAPI 3.1 document for the `/api` JSON surface with
`ehabterra/apispec`, commit it as `cmd/sandbin/openapi.json`, and serve it at `GET
/openapi.json` — with kin-openapi contract pins so the document and the live dispatch can never
drift silently.

**Architecture:** Task 1 moves only `/api` dispatch: one registration helper builds the mux AND a
`path → allowed methods` table; `apiRoutes` becomes classify-then-delegate (unknown → plain 404,
known+wrong-method → plain 405, known+right-method → the mux serves). The `/{hash}` content
surface, static routes, and the front page are untouched. Task 2 installs and configures apispec
(securityMappings + overrides) and commits its generated document. Task 3 embeds the document,
registers `GET /openapi.json` through the same helper, and adds the kin-openapi validation,
cross-check, golden-byte, and body-schema tests. Task 4 writes the hosted docs section. Task 5
records the verification.

**Tech stack:** Go stdlib (`net/http` ServeMux method patterns, `sync.Once`, `go:embed`);
external generator `github.com/ehabterra/apispec` (CLI, installed by `go install`, never a module
dependency); `github.com/getkin/kin-openapi` as the plan's only new Go dependency, imported
exclusively by `_test` files.

**Spec:** `docs/decisions/0030-api-servemux-openapi.md` — executors read both files.

## Global Constraints

- Gates at every task end: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test
  -count=1 ./...`, `go test -race -count=1 ./...`.
- Never push; never tag.
- Test counts may grow, never shrink: `cmd/sandbin` ≥ 242, `cmd/sandbin-client` ≥ 111. No test is
  deleted.
- **Task 1 touches no test file at all.** Every existing test drives the full server through
  `newTestServer` (no test calls an `api*` handler directly — verified at plan time), so the
  refactor's acceptance criterion is the whole existing suite green with every test file
  byte-identical to its state at `3574887`. This is the hard invariant of the plan: no pinned
  route-shape assertion is re-pointed or weakened anywhere in the plan either.
- Charging semantics are untouched everywhere: unknown `/api` spellings 404 **uncharged** even
  with a wrong key present; a wrong key on a known path charges; a protected-bin 401 charges
  nothing; `GET /api/health` and `GET /openapi.json` never charge and answer even while the
  address is blocked.
- The pinned surface shapes of plan 0029 stay letter-for-letter: `unknown bin` 404, `method not
  allowed` 405, HEAD on `/api/bins`, `/api/key`, and `/api/bins/{hash}` is 405, only
  `/api/health` (GET|HEAD) answers HEAD, `apiBinPathRE` names the one per-bin spelling.
- OpenAPI document facts, pinned: version 3.1; covers exactly the `/api` JSON surface +
  `/openapi.json`; bin-content paths (`/{hash}/…`) are out of document scope; `apiKey` scheme is
  `{type: apiKey, in: header, name: X-API-Key}`; the five key-checked operations
  (`POST /api/key`, `POST /api/bins`, `GET /api/bins`, `PATCH /api/bins/{hash}`,
  `DELETE /api/bins/{hash}`) carry it; `GET /api/health` and `GET /openapi.json` carry no
  security; `GET /api/bins/{hash}` carries optional security `[{ }, {binBasic: [ ]}]` with
  `binBasic: {type: http, scheme: basic}`.
- `kin-openapi` is imported only by `_test` files; apispec is a tool on the dev machine (`go
  install github.com/ehabterra/apispec/cmd/apispec@latest`), not a vendored artifact.
- `cmd/sandbin/openapi.json` is committed and regenerable: `make openapi` reproduces it
  byte-for-byte (the spike must prove regeneration is deterministic before the golden test is
  trusted).
- Hosted docs keep the house style (one request per fenced block, option-per-line continuations,
  `{{ .Instance.URL }}` tokens in examples, `http://localhost:8080` never on the API page).
- Do not touch: `cmd/sandbin-client`, `internal/*`, the docs pipeline machinery, `scripts/`,
  `Containerfile`, `deploy/`, the `/{hash}` content dispatch in `handlers.go` (except the one
  `ServeHTTP` condition line Task 3 adds), `skills/`, `docs/decisions/`.

## Review Focus

- **Task 1 parity is everything.** Reviewer must diff behavior, not just code: unknown spellings
  (non-hex, trailing-slash) stay uncharged 404s even with a wrong key; the `requireKey` order
  inside key-checked handlers (key → body → lookup → bin gate) survives the wrapper move; HEAD
  reaches a handler only where the table lists it. The existing suite passing with untouched test
  files is necessary but the reviewer must also hunt for behavior the suite does NOT pin (e.g. a
  request the old switch answered 405 that the new table answers 404).
- **The apispec spike can bounce Task 1.** apispec reads mux registrations statically; whether
  it follows the `apiRegister` indirection is unknown. The spike settles it, and the fallback
  (literal `mux.HandleFunc` calls with the table derived from the same pattern constants) is
  pinned in Task 2.
- **apispec wart #412:** a middleware header read can also be emitted as an ordinary header
  parameter next to the security scheme. The config or overrides must remove the duplicate
  `X-API-Key` parameter.
- **Golden-byte honesty:** the served `/openapi.json` must equal the committed file byte for
  byte; regeneration determinism and `make openapi` reproducibility are tested, not assumed.
- **Dep discipline:** `go vet` plus a grep pin must show `kin-openapi` imported only from
  `_test` files.

---

### Task 1: /api dispatch on http.ServeMux with an authoritative route table

**Files:**
- Modify: `cmd/sandbin/api.go` (dispatch rewrite, wrapper, handler signatures)
- Modify: `cmd/sandbin/handlers.go` (three new `server` fields only)
- Test: no file changes (invariant); new tests in `cmd/sandbin/api_test.go` may only be appended

**Interfaces:**
- Consumes: `requireKey`, `binAuthorized`, the seven `api*` handlers, `apiBinPathRE`, every
  pinned behavior of `apiRoutes` as written today (`api.go:30-70`).
- Produces:
  - `server` gains: `apiOnce sync.Once`, `apiMux *http.ServeMux`, `apiMethods map[string][]string`
    (path → allowed methods). They build on the first `/api` request inside `apiRoutes`'s
    `apiOnce.Do` — construction must stay lazy because every `server` in the repo (main and the
    three test literals) is a composite literal that cannot run an initializer.
  - `func (s *server) newAPIRoutes() (*http.ServeMux, map[string][]string)` — builds both.
    Registrations, in this order:
    - `POST /api/key` → `s.apiKey(s.apiKeyCheck)`
    - `GET /api/health` and `HEAD /api/health` → `s.apiHealth` (both spellings registered so the
      table derives HEAD from registration, not from a mux quirk)
    - `POST /api/bins` → `s.apiKey(s.apiCreateBin)`
    - `GET /api/bins` → `s.apiKey(s.apiListBins)`
    - `GET /api/bins/{hash}` → `s.apiInspectBin` (unwrapped — inspect takes no API key)
    - `PATCH /api/bins/{hash}` → `s.apiKey(s.apiPatchBin)`
    - `DELETE /api/bins/{hash}` → `s.apiKey(s.apiDeleteBin)`
  - `func apiRegister(mux *http.ServeMux, methods map[string][]string, pattern string, h http.Handler)`
    — one helper doing `mux.Handle(pattern, h)` AND appending the pattern's method to
    `methods[path]`; the table is always exactly what the mux was given.
  - `func (s *server) apiKey(next http.Handler) http.Handler` — `http.HandlerFunc` calling
    `s.requireKey(w, r)` and returning on false, else `next.ServeHTTP(w, r)`. The five
    key-checked handlers drop their internal `s.requireKey` calls (the wrapper runs it first, so
    each handler's pinned order — key before body, before lookup, before `binAuthorized` — is
    preserved by construction). `apiInspectBin`, `apiHealth`, `apiKeyCheck` (whose body is now
    just the 204) keep no key call.
  - Lookup `func apiLookupMethodSet(methods map[string][]string, path string) []string`: literal
    table hit; else if `apiBinPathRE` matches the concrete path, the table entry registered as
    `/api/bins/{hash}`; else nil. This hex gate is what keeps `{hash}` from broadening matching:
    `/api/bins/xyz` and `/api/bins/deadbeef/` stay nil → plain 404.
  - `apiRoutes` becomes exactly the decision doc's three-step classification: nil set → `http.Error
    ("unknown bin", 404)`; method not in set → `http.Error ("method not allowed", 405)`; else
    `s.apiMux.ServeHTTP(w, r)` (the mux cannot miss here, and its built-in 404/405/301 stay
    unreachable).
  - `apiInspectBin`, `apiPatchBin`, `apiDeleteBin` lose the `hash string` parameter and read
    `r.PathValue("hash")` (the hex gate guarantees it is an eight-hex hash). No other handler
    signature changes; `writeJSON`, `decodeBinRequest`, DTOs, cursor helpers untouched.
  - `apiRoutes`'s and `ServeHTTP`'s doc comments updated to describe the mux dispatch (the
    non-shadowing reasoning stays).

- [ ] **Step 1: Append new tests first** in `api_test.go`: `TestAPIRouteTableIsPublishedSurface`
  (call `s.newAPIRoutes()` on a `newTestServer` and pin the table exactly: `/api/key` → [POST],
  `/api/health` → [GET HEAD], `/api/bins` → [GET POST], `/api/bins/{hash}` → [GET PATCH DELETE];
  no other keys). These tests fail to compile until the interfaces exist. **Do not edit any
  existing test.**
- [ ] **Step 2: Run, verify failure** (`go test ./cmd/sandbin -run TestAPIRouteTable` fails to
  compile).
- [ ] **Step 3: Implement** the interfaces above; delete the old switch.
- [ ] **Step 4: Run tests:** `go test -count=1 ./cmd/sandbin` fully green **with every pre-existing
  test file unchanged** — verify with `git diff --stat` that `handlers_test.go` and the pre-Task
  portion of `api_test.go` show no modifications (appends to `api_test.go` are the only test-file
  diff allowed). Any pinned-shape failure means the classification or hex gate is wrong, not that
  the test should move. Count ≥ 243.
- [ ] **Step 5: Gates** then commit `refactor: /api dispatch runs on ServeMux behind a route table`.

---

### Task 2: apispec spike, config, and the generated openapi.json

**Files:**
- Create: the apispec config file at the location/format apispec expects (repo root or
  `cmd/sandbin/` — the spike decides and documents it in a header comment)
- Create: `cmd/sandbin/openapi.json` (generated, committed)
- Modify: `Makefile` (new `openapi` target + `.PHONY` entry)

**Interfaces:**
- Consumes: Task 1's literal registrations and the `s.apiKey` wrapper shape.
- Produces:
  - Tool: `go install github.com/ehabterra/apispec/cmd/apispec@latest` (already Go 1.26+); the
    Makefile header comment names the install line. Not vendored, not a module dep.
  - `make openapi` regenerates `cmd/sandbin/openapi.json` deterministically (byte-stable across
    two consecutive runs — the spike verifies).
  - Config carries `securityMappings` mapping the `apiKey` wrapper (its exact function reference
    per apispec's config format) to `apiKey: {type: apiKey, in: header, name: X-API-Key}`, and
    `overrides` for everything the scan gets wrong: plain-text `text/plain` error bodies with the
    pinned strings, summaries/descriptions, the optional-Basic `security` on
    `GET /api/bins/{hash}`, removal of any duplicate `X-API-Key` header parameter (wart #412),
    and exclusion of the control-socket `/ip…` mux routes (`control.go` is in the same package —
    the document must not describe them).
  - Acceptance checklist for the generated document (reviewer verifies against it):
    1. `openapi` version is 3.1.x.
    2. `paths` contains exactly `/api/key`, `/api/health`, `/api/bins`, `/api/bins/{hash}` with
       method sets matching Task 1's table — exactly these four at Task 2 time; the
       `/openapi.json` path joins the document in Task 3, when the endpoint exists.
    3. The five key-checked operations carry `security: [{apiKey: []}]`; `GET /api/health` none;
       `GET /api/bins/{hash}` the optional `[{ }, {binBasic: [ ]}]`.
    4. `components.securitySchemes` holds `apiKey` (header `X-API-Key`) and `binBasic` (http
       basic).
    5. Request bodies are `application/json`; success responses `application/json` with the 0029
       field spellings as schema properties; error responses `text/plain`.
    6. No `/ip` paths, no stray header parameters duplicating `X-API-Key`.
  - **Spike gate (this task's first step):** run apispec over `cmd/sandbin` as-is. If it does not
    see routes registered through `apiRegister`'s indirection, switch `newAPIRoutes` to literal
    `mux.HandleFunc("GET /api/bins", …)` calls with the table built by parsing the same pattern
    literals (one `const` per pattern so mux and table share one source), and re-run Task 1's
    suite. If apispec cannot reach the checklist after one fix wave, STOP and report to the
    controller; the fallback (switch to `antst/go-apispec`) is a controller decision recorded as
    an addendum in `docs/decisions/0030-api-servemux-openapi.md` — implementers do not switch
    generators on their own.

- [ ] **Step 1: Install** apispec and confirm `apispec --help` runs; read its docs (net/http
  guide, config format) before configuring.
- [ ] **Step 2: Spike run** over `cmd/sandbin` with no config; evaluate against the checklist;
  record findings (what was inferred, what was wrong, what overrides are needed) in the task
  report. Verify route visibility through the `apiRegister` indirection; apply the pinned
  fallback if needed (and if so, re-run the full suite green before continuing).
- [ ] **Step 3: Write the config** (`securityMappings`, `overrides`, scope exclusions per the
  checklist) and the `openapi` Makefile target; generate; iterate until the checklist passes.
- [ ] **Step 4: Verify determinism:** run `make openapi` twice; `git diff` between runs empty;
  `gofmt -l .` untouched (JSON is not Go-formatted but `go build ./...` must still pass — the
  JSON file is inert to Go until Task 3 embeds it).
- [ ] **Step 5: Gates** (test counts unchanged) then commit `feat: apispec generates the OpenAPI 3.1 document`.

---

### Task 3: Serve GET /openapi.json and the kin-openapi contract pins

**Files:**
- Create: `cmd/sandbin/openapi.go` (embed + serve handler)
- Modify: `cmd/sandbin/api.go` (one registration line + doc comment), `cmd/sandbin/handlers.go`
  (one `ServeHTTP` condition line + its comment)
- Create: `cmd/sandbin/openapi_test.go`
- Modify: `go.mod`/`go.sum` (kin-openapi)

**Interfaces:**
- Consumes: Task 1's `apiRegister`/table/mux, Task 2's committed `cmd/sandbin/openapi.json`,
  `newTestServer`.
- Produces:
  - `cmd/sandbin/openapi.go`: `//go:embed openapi.json` → `var openapiDoc []byte`;
    `func (s *server) serveOpenAPI(w http.ResponseWriter, r *http.Request)` setting
    `Content-Type: application/json` and writing `openapiDoc` verbatim (the file already carries
    its trailing newline; add none).
  - `newAPIRoutes` gains: `apiRegister(..., "GET /openapi.json", http.HandlerFunc(s.serveOpenAPI))`.
  - `ServeHTTP`'s `/api` condition becomes `r.URL.Path == "/openapi.json" || r.URL.Path == "/api"
    || strings.HasPrefix(r.URL.Path, "/api/")` with a comment extension: `openapi.json` holds
    dots, so it can never name a hex bin hash, same reasoning as `robots.txt`. The endpoint is
    key-free, never charges, answers while the address is blocked; HEAD and every other method
    are the plain 405 (the table lists GET only); `/openapi.json/` is the plain 404 `unknown bin`
    (nil table entry).
  - `kin-openapi` enters `go.mod` via `go get github.com/getkin/kin-openapi@latest`, imported
    only by `openapi_test.go`.
  - Tests (all in `openapi_test.go`):
    - `TestOpenAPIDocValidatesAsOpenAPI31`: `openapi3.Loader` on the file, `Validate()` clean,
      version 3.1.x.
    - `TestOpenAPISpecMatchesRouteTable` two-way: every spec path×method exists in
      `s.newAPIRoutes()`'s table (with `{hash}` spelled as the table spells it), every table
      entry exists in the spec, and both sets are equal — nothing extra, nothing missing.
    - `TestOpenAPISecurityMatchesWrapper`: the five key-checked operations carry exactly the
      `apiKey` requirement; `GET /api/health` and `GET /openapi.json` carry none;
      `GET /api/bins/{hash}` carries the optional Basic pair; schemes match the pinned spellings.
    - `TestOpenAPIServedBytesMatchTheCommittedFile`: `GET /openapi.json` → 200,
      `Content-Type: application/json`, body byte-identical to `os.ReadFile("openapi.json")`.
    - `TestOpenAPIServeShapes`: HEAD → 405 `method not allowed`; POST → 405;
      `GET /openapi.json/` → 404 `unknown bin`; `GET /openapix.json` → 404.
    - `TestOpenAPINeverCharges`: no key 200; wrong key still 200 and `RecordFailure` never
      fired (repeat until a write with the same wrong key 403s, proving the doc route charged
      nothing); a blocked address still gets 200.
    - `TestOpenAPIBodySchemasMatchLiveAnswers`: live bodies from `newTestServer` — create 201,
      inspect 200, list 200, single-file PUT 201, archive PUT 201 — each validated against the
      spec's 200/201 response schema via kin-openapi's schema validation.
    - `TestKinOpenapiStaysTestOnly`: walk `cmd/sandbin/*.go` non-test files and fail if any
      imports `getkin/kin-openapi`.

- [ ] **Step 1: Write failing tests** (they fail to compile until `openapi.go` and the dep exist).
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** embed + handler + registration + ServeHTTP condition; `go get` the dep.
- [ ] **Step 4: Regenerate the document:** run `make openapi` — the new `apiRegister` line is
  static source apispec can scan, so the regenerated `openapi.json` now carries
  `GET /openapi.json` (no security). If apispec describes the endpoint's body unhelpfully, pin
  it with a minimal override: `application/json`, an opaque object schema. The cross-check test
  only passes after this regeneration.
- [ ] **Step 5: Run tests:** green; `cmd/sandbin` count ≥ 243 + the eight new tests.
- [ ] **Step 6: Gates** then commit `feat: serve the OpenAPI document at GET /openapi.json with contract pins`.

---

### Task 4: Hosted docs — the /openapi.json section

**Files:**
- Modify: `docs/usages/sandbin-api.md` (new h2 section)
- Modify: `docs/usages/sandbin-serve.md` only if its conventions list names the API surface (the
  implementer checks whether a one-line mention belongs; optional)
- Test: `cmd/sandbin/docs_test.go` (extend pins; no existing assertion re-pointed)

**Interfaces:**
- Consumes: the shipped `GET /openapi.json` behavior from Task 3; the docs pipeline (unchanged
  machinery; still exactly nine hosted pages).
- Produces: in `sandbin-api.md`, a new h2 `` `/openapi.json` — the machine-readable API document ``
  placed after the `/api/health` h2 and before the `/{hash}/` h2s (the JSON-API sections stay
  grouped ahead of the content-surface sections). Its pinned content points, each present in
  prose:
  - `GET` answers the document; it needs no API key and charges nothing;
  - the body is the OpenAPI 3.1 description of the `/api` surface, including this endpoint;
  - bin-content paths (`/{hash}/…`) are not in the document;
  - a self-builder can regenerate it with `make openapi`.
  - One fenced request, house style: `curl -s {{ .Instance.URL }}/openapi.json`, and a short
    fenced ```text mock truncated with `…` — never the whole document, and never a claim that
    reads `GET` is never charged (the standing negative pin bans that phrasing).
- New doc pins in `docs_test.go`: `TestDocsOpenAPIDocSection` — the new h2 slug (via the existing
  `githubSlugForTest` idiom), the no-key sentence, the `make openapi` name, and the
  not-in-the-document sentence for bin-content paths; extend `TestDocsAPIPageIsSwaggerStyle`'s
  h2-order pin to include the new section in its new position. The nine-pages comments stay
  true.

- [ ] **Step 1: Extend the doc tests first; verify they fail.**
- [ ] **Step 2: Write the section** in house style.
- [ ] **Step 3: Run tests:** green; `cmd/sandbin` count grows; docs machinery untouched.
- [ ] **Step 4: Preview check** — scratch-serve loop (scratch build `-buildvcs=false`, serve
  `127.0.0.1:18307`, curl with `Host: preview.yookoala.dev` + `X-Forwarded-Proto: https`): fetch
  `/docs/sandbin-api.html`, confirm the new section renders, zero stray `{{`, no
  `http://localhost:8080` on the page; also fetch the real `GET /openapi.json` from the scratch
  server and confirm it serves the committed bytes; kill only the scratch process (scan
  `/proc/*/comm`), delete scratch files.
- [ ] **Step 5: Gates** then commit `docs: the hosted API page describes GET /openapi.json`.

---

### Task 5: Final verification record

- [ ] **Step 1: All five gates** at HEAD.
- [ ] **Step 2: Per-package counts** (`go test -count=1 <pkg> -v | grep -c '^--- PASS'`) recorded
  in this file's tail: `cmd/sandbin` (≥ 251), `cmd/sandbin-client` 111, `internal/frontpage` 17,
  `internal/robots` 2, `internal/version` 6. No package shrank.
- [ ] **Step 3: Scratch end-to-end** on `127.0.0.1:18307` (curl only): `GET /openapi.json` 200
  application/json (save to `/tmp/opencode/` and validate with `go run
  github.com/getkin/kin-openapi/cmd/validate@latest`); spot-check pinned shapes survived the
  refactor — `HEAD /api/bins` 405, `GET /api/bins/xyz` 404, `GET /api/bins/deadbeef/` 404,
  wrong key on `POST /api/bins` 403 then 429 with Retry-After while the same wrong key on
  `GET /api/bins/xyz` stays 404-uncharged, `GET /api/health` 200 while blocked; `make openapi`
  reproduces the committed file (`git diff` empty). Kill scratch, clean up.
- [ ] **Step 4: Record the outcome in this file's tail as the closing commit** `docs: record plan
  0030 final verification`, mirroring the established plan records.

## Final verification (recorded 2026-11-17)

- Gates at HEAD `bf95bc8`: `gofmt -l .` empty; `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...` all green (and green again after the `make openapi` run below).
- Per-package test counts (top-level PASS): `cmd/sandbin` 253 (baseline 242), `cmd/sandbin-client` 111 (baseline 111), `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6. No package shrank.
- Scratch end-to-end (`127.0.0.1:18307`, scratch build with `-buildvcs=false`, real store in a temp dir, curl only, custom headers limited to `X-API-Key`): `GET /openapi.json` 200 `application/json` with the served bytes identical to the committed `cmd/sandbin/openapi.json` (19824 bytes, `openapi: 3.1.1`, paths `/api/key` post · `/api/health` get+head · `/api/bins` get+post · `/api/bins/{hash}` get+patch+delete · `/openapi.json` get — exactly the table) → pinned shapes survived the refactor: `HEAD /api/bins` 405 `method not allowed`, `GET /api/bins/xyz` 404 `unknown bin`, `GET /api/bins/deadbeef/` 404, literal `GET` and `POST /api/bins/{hash}` 404, `HEAD` and `POST /openapi.json` 405 `method not allowed`, `GET /openapi.json/` and `GET /openapix.json` 404 `unknown bin`, `HEAD /api/health` 200, `DELETE /api/bins` 405 → charging walk: wrong key on `POST /api/bins` 403 `invalid API key`, then 429 `too many bad API keys` with `Retry-After: 1799`, then 429 with `Retry-After: 3599` (the penalty walks up), the `ip show` row reading `127.0.0.1 4 failures` while blocked; during the block the same wrong key on `GET /api/bins/xyz` and `GET /api/bins/deadbeef/` stayed the plain 404 (uncharged, spelling unchanged) and `GET /api/health` and `GET /openapi.json` both stayed 200 → cleared with `sandbin ip unblock 127.0.0.1` → happy path: `POST /api/key` 204, create 201 with `Location: /api/bins/9f8508b1` and the bin object, key-free inspect 200 with `files`, list 200 with that one bin, delete 204, inspect then 404 `unknown bin`, final list `{"bins":[]}`. Scratch server killed by exact pid found through a `/proc/*/comm` scan; scratch binary, store and files removed.
- The served document validated with kin-openapi's own validator at the pinned v0.153.0 — `go run github.com/getkin/kin-openapi/cmd/validate <file>` from the repo (go.mod's version) and the `@latest` form both exit 0 with no output, and `go list -m github.com/getkin/kin-openapi@latest` resolves to the same v0.153.0; a deliberately broken copy of the document exits 1 with a real undefined-security-scheme error, so the clean exit is a validation and not a no-op. `go.mod`/`go.sum` untouched.
- `make openapi` (apispec v0.5.11 through `cmd/sandbin/openapi-fix.jq`) reproduced the committed document byte for byte: same sha256 `ae89a5c88553332aa147c3ebbf88c8beef8a5529fb63dcb5902d66495f20116c` before and after, `git diff` empty, the target's `openapi.raw.tmp.json` removed by the target itself, nothing to restore.
