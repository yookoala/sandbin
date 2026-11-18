# The /api index endpoint — Implementation Plan

Status: planned 2026-11-18. Spec: `docs/decisions/0031-api-index-endpoint.md` — the binding
design, including the partner-approved index format, the owner-approved pin moves, and what
was declined. Baseline HEAD `ed04371` (`cmd/sandbin` 253 tests, `cmd/sandbin-client` 111,
`internal/frontpage` 17, `internal/robots` 2, `internal/version` 6, all five gates green).

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the bare `GET /api` (and `HEAD /api`) answer a JSON index of the published API
— version, every route-table endpoint with per-method descriptions and full request-context
URLs, and the `/openapi.json` pointer — built from the route table itself, regenerated into the
OpenAPI document, and described on the hosted API page.

**Architecture:** Task 1 is one vertical slice: two new pattern consts, an `apiIndex` handler in
a new `api_index.go` (typed DTOs + a descriptions map keyed by the exact pattern strings),
registration unwrapped in `newAPIRoutes`, the owner-approved one-for-one pin moves in
`api_test.go`, and `make openapi` regeneration + `openapi-fix.jq` corrections so the existing
contract tests (cross-check equality, golden bytes) stay green in the same commit. Task 2
promotes the document's placeholder `GET /api` response schema into a real component schema and
adds the honesty pin that validates the live index body against the committed document. Task 3
writes the hosted docs section. Task 4 records the verification.

**Tech stack:** Go stdlib (`net/http` method patterns, `encoding/json`); the existing
`frontBase` request-context helper; `internal/version.Value`; the plan-0030 pipeline
(apispec v0.5.11 → temp → `openapi-fix.jq` → committed `openapi.json`); kin-openapi stays
test-only.

**Spec:** `docs/decisions/0031-api-index-endpoint.md` — executors read both files.

## Global Constraints

- Gates at every task end: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test
  -count=1 ./...`, `go test -race -count=1 ./...`.
- Never push; never tag.
- Test counts may grow, never shrink: `cmd/sandbin` ≥ 253, `cmd/sandbin-client` 111,
  `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6. No test is deleted.
- **Exactly two pre-existing assertions move, both ruled by the decision doc**: the
  `"/api"` entry drops out of `TestAPIUnknownShapes`' unknown-spelling loop, and
  `TestAPIRouteTableIsPublishedSurface` gains `"/api": [GET HEAD]`. Any other pre-existing
  assertion that turns red is a sign the implementation is wrong — fix the code, not the test.
  (Extensions that new surface *forces* on the contract tests — e.g. the security test naming
  the new unwrapped operations — are additive and allowed; deletions are not.)
- Charging semantics everywhere else untouched: unknown spellings 404 uncharged; a wrong key on
  a key-checked path charges; the index charges nothing, needs no key, and answers while the
  address is blocked.
- Pinned shapes that must survive: `GET /api/` and every unlisted spelling stay the plain 404
  `unknown bin`; `POST`/`PUT`/`PATCH`/`DELETE` on `/api` are the plain 405 `method not allowed`;
  everything plan 0030 pinned about `/api/bins…`, `/api/key`, `/api/health`, `/openapi.json`
  stands.
- Full URLs are `frontBase(r) + path` — never hardcoded. `httptest.NewRequest` defaults
  `RemoteAddr` to `192.0.2.1:1234` (TEST-NET, untrusted), so trusted-`X-Forwarded-Host`/
  `X-Forwarded-Proto` contexts must pin `RemoteAddr` to a loopback socket — the idiom already
  used in `handlers_test.go`'s front-base tables (ruling P3). Remember `httptest.NewRecorder`
  keeps HEAD bodies (only the real server strips them): pin HEAD status and headers in unit
  tests; the empty-body fact is proven in the Task 4 scratch walk with curl.
- `cmd/sandbin/openapi.json` stays regenerable: `make openapi` reproduces it byte-for-byte
  after every filter change.
- Hosted docs keep the house style (one request per fenced block, `{{ .Instance.URL }}` tokens,
  truncated ```text mocks with `…`, no blanket "GET is never charged" phrasing).
- Do not touch: `cmd/sandbin-client`, `internal/*` sources (importing `internal/version` is
  fine), the docs pipeline machinery, `scripts/`, `Containerfile`, `deploy/`, `skills/`,
  `handlers.go`'s `ServeHTTP` (the existing `/api` condition already routes the bare path),
  `control.go`.

## Review Focus

- **The index is the table.** Reviewer must verify the build reads the route table (not a
  hand-maintained duplicate): a route added to `newAPIRoutes` without a description and without
  joining the path enumeration fails a test, and vice versa — the enumeration ↔ table ↔
  descriptions-triangle pins.
- **No URL lies.** Full URLs come from `frontBase` with its trusted-proxy and `frontHostRE`
  rules intact; nothing echoes an untrusted header; the fallback context is pinned.
- **The pin moves are exactly the ruled two.** Reviewer counts them in the diff; anything else
  that moved in a test file is a finding.
- **`/api/` stays a 404.** The mux would 301 `/api/` → `/api` if classification ever leaked it;
  the classification must still answer the plain 404 before the mux (the brace guard from R3 is
  likewise untouched).
- **Document honesty:** the regenerated document carries the two `/api` operations with
  `security: []`, and Task 2's live-body validation genuinely validates against the committed
  document's schema, not a hand-written mirror.

---

### Task 1: GET|HEAD /api answers the index, and the document learns about it

**Files:**
- Create: `cmd/sandbin/api_index.go` (DTOs, descriptions map, handler)
- Create: `cmd/sandbin/api_index_test.go`
- Modify: `cmd/sandbin/api.go` (two pattern consts + registrations + record-loop entry + doc-comment updates)
- Modify: `cmd/sandbin/api_test.go` (only the two ruled pin moves)
- Modify: `cmd/sandbin/openapi-fix.jq` and regenerate `cmd/sandbin/openapi.json` (via `make openapi`); `apispec.yaml` only if the generator needs a config nudge

**Interfaces:**
- Consumes: `newAPIRoutes`' literal registration idiom (`apiPattern*` consts + `apiRecordMethod`
  loop), `apiLookupMethodSet` unchanged, `frontBase(r)` unchanged, `writeJSON`,
  `internal/version.Value`, `make openapi` + `openapi-fix.jq` pipeline.
- Produces:
  - Two new consts, joining the const block: `apiPatternIndexGet = "GET /api"`,
    `apiPatternIndexHead = "HEAD /api"`. `newAPIRoutes` registers both with
    `http.HandlerFunc(s.apiIndex)`, unwrapped, and both join the `apiRecordMethod` loop.
  - `api_index.go`:
    - DTOs with pinned field order/names: `apiIndexMethod{Method string json:"method";
      Description string json:"description"}`, `apiIndexEndpoint{URL string json:"url";
      Methods []apiIndexMethod json:"methods"}`, `apiIndexLink{URL json:"url"; Description}`,
      `apiIndexDoc{Version json:"version"; Endpoints json:"endpoints"; OpenAPI json:"openapi"}`.
    - `apiIndexDescriptions map[string]string` keyed by the exact pattern strings — every
      published pattern except `apiPatternIndexGet`/`apiPatternIndexHead` — with the decision
      doc's wording verbatim.
    - `apiIndexPathOrder = []string{"/api/key", "/api/health", "/api/bins", "/api/bins/{hash}"}`
      — the fixed enumeration the endpoints are published in.
    - `func (s *server) apiIndex(w http.ResponseWriter, r *http.Request)` —
      `Content-Type: application/json`, version `version.Value`, built from `s.apiMethods`:
      enumerate `apiIndexPathOrder` paths, per path the table's method list (already canonically
      sorted), description looked up by `method + " " + path`, `url` = `s.frontBase(r) + path`;
      the `openapi` sibling from the table's `/openapi.json` entry.
  - `apiRoutes` and `newAPIRoutes` doc comments updated to name the index as a published
    pattern (the "table names the request, then the mux answers" rule is unchanged).
- Tests in `api_index_test.go` (all new; exact names pinned):
  - `TestAPIIndexBodyIsThePublishedSurface` — golden body against a pinned direct-Host context:
    the expected JSON embeds the paths, methods, full URLs and description strings as **literal
    test-side copies of the decision doc's wording**; only `version.Value` interpolates. A
    description rewording must consciously move this test along with the map — a golden assembled
    from the map itself would pin nothing about wording.
  - `TestAPIIndexURLsFollowRequestContext` — trusted loopback socket with
    `X-Forwarded-Host`/`X-Forwarded-Proto: https` renders `https://…`, a Host failing
    `frontHostRE` renders `https://this.host/…`;
  - `TestAPIIndexShapes` — `HEAD /api` 200 (status+headers pinned per the recorder caveat),
    `POST`/`PUT`/`PATCH`/`DELETE /api` 405 `method not allowed`, `GET /api/` 404 `unknown bin`;
  - `TestAPIIndexNeverCharges` — wrong key on `GET /api` 200 with nothing recorded; while the
    address is blocked, `GET /api` still 200 (and the same wrong key on `POST /api/bins` still
    429s, proving the block is real around it);
  - `TestAPIIndexDescriptionsCoverTheTableOneForOne` — the map's keys equal the table's patterns
    minus the two index patterns, exactly;
  - `TestAPIIndexPathOrderEqualsTheTable` — `apiIndexPathOrder` plus `"/api"` plus
    `"/openapi.json"` equals the table's key set exactly, both directions.
- `api_test.go` — the two ruled moves only: drop `"/api"` from `TestAPIUnknownShapes`' loop; add
  `"/api": {GET HEAD}` to `TestAPIRouteTableIsPublishedSurface` (the existing mux-pairing loop
  then covers the new entries automatically).
- Regeneration: after registration, `make openapi`; the cross-check equality test now requires
  the document to carry `/api` (get+head). Add `openapi-fix.jq` corrections: `security: []` on
  both `/api` operations, contract summaries and `operationId`s `apiIndex`/`headAPIIndex`, and
  `GET /api`'s 200 pinned to `application/json` (a placeholder schema is fine here — Task 2
  replaces it; note that in the filter header). Extend `TestOpenAPISecurityMatchesWrapper`
  only as far as the new operations force it (they must appear as no-security).

- [ ] **Step 1: Write the new tests** (`api_index_test.go` — they fail to compile until the
  interfaces exist) **and make the two ruled `api_test.go` moves.**
- [ ] **Step 2: Run, verify failure** (`go test ./cmd/sandbin -run 'TestAPIIndex'` fails to
  compile; `TestAPIRouteTableIsPublishedSurface` goes red against the `/api` entry added ahead
  of the implementation — that is the behavioral RED for the registrations).
- [ ] **Step 3: Implement** `api_index.go` + `api.go` registrations + doc comments.
- [ ] **Step 4: Regenerate:** `make openapi`; iterate `openapi-fix.jq` until
  `TestOpenAPISpecMatchesRouteTable`, the security test, and the golden-bytes test are green.
- [ ] **Step 5: Full suite green**, `cmd/sandbin` count ≥ 259; verify with `git diff` that no
  test file moved beyond the two ruled assertions plus new files.
- [ ] **Step 6: Gates** then commit `feat: the bare /api answers a JSON index of the published API`.

---

### Task 2: The document describes the index body, and the pin validates it

**Files:**
- Modify: `cmd/sandbin/apispec.yaml` and/or `cmd/sandbin/openapi-fix.jq`; regenerate `cmd/sandbin/openapi.json`
- Modify: `cmd/sandbin/openapi_test.go` (additive only)

**Interfaces:**
- Consumes: Task 1's shipped index and its placeholder response schema in the document.
- Produces:
  - The document's `GET /api` 200 references a real component schema (generated from the DTO
    types by apispec if it can — `requiredFromJSONTags` already produces required lists — or
    carried by the jq filter, the `archiveAnswer` precedent) describing: `version` string;
    `endpoints` array of `{url, methods[]}`; `methods` items `{method, description}`; `openapi`
    object `{url, description}` — with required lists that exactly match what the handler
    marshals, and no `additionalProperties` lie.
  - `TestOpenAPIBodySchemasMatchLiveAnswers` gains the index: a live `GET /api` body from
    `newTestServer` validates against that schema via the existing kin-openapi validation
    helper. No existing assertion in the test moves; the case list grows.
  - `make openapi` reproduces everything byte-for-byte (two runs, empty diff).
- [ ] **Step 1: Write the failing validation case first;** verify RED.
- [ ] **Step 2: Implement** the schema (config preferred, filter as the sanctioned fallback per
    decision 0030's addendum); regenerate; iterate until the new case and every existing case
    are green.
- [ ] **Step 3: Verify determinism** (two `make openapi` runs byte-identical; temps gitignored,
    never surviving).
- [ ] **Step 4: Gates** then commit `feat: the OpenAPI document describes the API index body`.

---

### Task 3: Hosted docs — the `/api` index section

**Files:**
- Modify: `docs/usages/sandbin-api.md` (new h2 section)
- Modify: `docs/usages/sandbin.md` only if the API-page index row should name the index (implementer judgment, pinned tests decide)
- Test: `cmd/sandbin/docs_test.go` (extend pins; no existing assertion re-pointed except prose that the new section legitimately reworded)

**Interfaces:**
- Consumes: the shipped Task 1 behavior.
- Produces: a new h2 `` `/api` — the API index `` placed **first among the JSON-API sections**
  (before the `POST /api/bins` section — discovery comes before use) and before the `/{hash}/`
  h2s; `TestDocsAPIPageIsSwaggerStyle`'s h2-order pin extended one-for-one. Pinned content
  points, each present in prose:
  - `GET` (and `HEAD`) answers the index; it needs no API key, charges nothing, and answers
    even while the address is blocked;
  - it lists every published JSON endpoint with a one-line description and a full URL built
    from the request's own host and scheme;
  - it points at the `/openapi.json` document and stamps the server version;
  - `POST /api` is the plain 405 and `/api/` the plain 404 (the old bare-`/api` shape is gone —
    if any existing prose says otherwise, it moves with this section, one-for-one).
  One fenced request `curl -s {{ .Instance.URL }}/api` and a short truncated ```text mock
  (the real body elided with `…`, never the full index). New test `TestDocsAPIIndexSection`
  pins: the h2 slug (githubSlugForTest idiom), the no-key/charges-nothing sentence, the
  full-URL sentence, and the `openapi` pointer sentence. Nine hosted pages stay nine.
- [ ] **Step 1: Extend/add the doc tests first; verify they fail.**
- [ ] **Step 2: Write the section** in house style.
- [ ] **Step 3: Run tests:** green; `cmd/sandbin` count grows; docs machinery untouched.
- [ ] **Step 4: Preview check** — scratch loop (scratch build `-buildvcs=false`, serve
  `127.0.0.1:18307`, curl with `Host: preview.yookoala.dev` + `X-Forwarded-Proto: https`):
  `/docs/sandbin-api.html` renders the new section, zero stray `{{`, no `http://localhost:8080`;
  the scratch server's own `GET /api` renders `https://preview.yookoala.dev/...` URLs under that
  Host; kill only the scratch process (`/proc/*/comm` scan, kill by pid), delete scratch files.
- [ ] **Step 5: Gates** then commit `docs: the hosted API page describes the /api index`.

---

### Task 4: Final verification record

- [ ] **Step 1: All five gates** at HEAD.
- [ ] **Step 2: Per-package counts** (`go test -count=1 <pkg> -v | grep -c '^--- PASS'`)
  recorded in this file's tail: `cmd/sandbin` ≥ 260, `cmd/sandbin-client` 111,
  `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6. Nothing shrank.
- [ ] **Step 3: Scratch end-to-end** on `127.0.0.1:18307` (curl only): `GET /api` 200
  application/json with socket-host URLs; with `Host: preview.yookoala.dev` +
  `X-Forwarded-Proto: https` the same body renders `https://preview.yookoala.dev/…` URLs;
  `HEAD /api` 200 with **empty body** (the real-server HEAD rule); `POST /api` 405
  `method not allowed`; `GET /api/` 404 `unknown bin`; wrong key repeated on `POST /api/bins`
  to 429 `Retry-After`, and while blocked: `GET /api`, `GET /api/health` and `GET /openapi.json`
  all still 200 while the same wrong key on `GET /api/bins/xyz` stays 404 uncharged; the index's
  `openapi` URL actually fetches. Spot-check survived shapes: `HEAD /api/bins` 405,
  `GET /api/bins/deadbeef/` 404, literal-braces 404. `make openapi` reproduces the committed
  file (`git diff` empty). Kill scratch, clean `/tmp/opencode`.
- [ ] **Step 4: Record the outcome in this file's tail as the closing commit** `docs: record
  plan 0031 final verification`, mirroring the 0029/0030 records.

## Final verification (recorded 2026-11-18)

- Gates at HEAD `b0e2484`: `gofmt -l .` empty; `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...` all green (and green again after the `make openapi` run below).
- Per-package test counts (top-level PASS): `cmd/sandbin` 260 (baseline 253), `cmd/sandbin-client` 111, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6. No package shrank.
- Scratch end-to-end (`127.0.0.1:18307`, scratch build with `-buildvcs=false`, real store in a temp dir, `--api-key` set, curl only): `GET /api` 200 `application/json` (1245 bytes) with `version:""` — deliberately unstamped scratch build — and `endpoints` in order `/api/key`, `/api/health`, `/api/bins`, `/api/bins/{hash}` with the literal `{hash}`, each URL `http://127.0.0.1:18307/…` from the socket host, plus the `openapi` sibling → the same request with `Host: preview.yookoala.dev` + `X-Forwarded-Proto: https` renders all five URLs as `https://preview.yookoala.dev/…` → the real-server HEAD rule the recorder unit tests could not pin: `HEAD /api` 200 with headers (`Content-Type: application/json`, `Content-Length: 1245`) and **zero body bytes**, proven with `curl -sI` and a raw-socket read past the header terminator (`curl -X HEAD` hangs reading a body that is correctly never sent — curl's quirk, not the server's) → pinned refusals: `POST /api` 405 `method not allowed`, `GET /api/` 404 `unknown bin` (no 301 leak into the mux) → charging walk: wrong key on `POST /api/bins` 403 `invalid API key`, then 429 `too many bad API keys` with `Retry-After: 1799`; while blocked `GET /api` stayed 200 with a byte-identical index body, `GET /api/health` 200, `GET /openapi.json` 200, and the same wrong key on `GET /api/bins/xyz` stayed the plain 404 (uncharged) → cleared with `sandbin ip unblock 127.0.0.1` → happy path: create 201 with `Location: /api/bins/0f3a5656` and the bin object, key-free inspect 200 with `files`, list 200 with that one bin, delete 204, inspect then 404 `unknown bin`, final list `{"bins":[]}` → survived shapes: `HEAD /api/bins` 405, `GET /api/bins/deadbeef/` 404, literal-braces `GET /api/bins/%7Bhash%7D` 404. Scratch server killed by exact pid through a `/proc/*/comm` scan; scratch binary, store and files removed.
- The index's `openapi` URL fetches for real: `GET /openapi.json` 200 `application/json` with `openapi: 3.1.1`, paths exactly the table (`/api` get+head now among them), the served bytes identical to the committed `cmd/sandbin/openapi.json` (sha256 `2ec536ed7f07fcba954378eadbdc923bd825ddacae66de7a90bad2a603f6eff8`).
- The saved served document validated with kin-openapi's own validator at the pinned v0.153.0 — `go run github.com/getkin/kin-openapi/cmd/validate <file>` from the repo exits 0 with no output; a deliberately broken copy (security requirement on an undefined scheme) exits 1 with a real validation error, so the clean exit is a validation and not a no-op. `go.mod`/`go.sum` untouched.
- `make openapi` (apispec v0.5.11 through `cmd/sandbin/openapi-fix.jq`) reproduced the committed document byte for byte: same sha256 before and after, `git diff` empty, both `openapi.raw.tmp.json` and `openapi.tmp.json` removed by the target itself, nothing to restore.
