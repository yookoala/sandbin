# JSON API responses — Implementation Plan

## Status

Status: implemented and closed 2026-11-27. Twelve commits, `085bccf` (decision+plan) through
this closing `docs:` commit: Task 1 `4454cd5` ("feat: API refusals answer the JSON error
envelope"), Task 2 `d3f47df` ("feat: request-shape and internal refusals answer the JSON
envelope too") with its follow-up `1b7406a` ("docs(spec): the generated document's prose
describes the dispatcher's 405 refusals too"), Task 3 `42bff58` ("feat: every JSON success
says ok, and the empty 204s become 200 bodies"), Task 4 `5961ff3` ("feat: file PUT and
DELETE refusals answer the JSON envelope"), Task 5 `5b2069f` ("feat(client): --json prints
the run as a JSON action report") and its fix wave `4f72c26` ("test(client): pin that --json
survives a non-JSON refusal body"), and Task 6 `107b98b` ("docs: the API and client pages
carry the JSON envelope and --json") with its fix wave `7dcd6bf` ("docs: the skill quotes the
live path not found refusal and folders count as one action") plus `d885a3d` ("test(client):
the fake answers delete refusals the way the live server does"). Spec:
`docs/decisions/0033-json-api-responses.md` (including its execution addendum). Review
outcome: every task passed an independent task-scoped review. Tasks 1–4 were approved first
time, zero Critical and zero Important each (Task 1's reviewer additionally verified the
15-tests-moved-beyond-the-named-list claim as sound — every move forced by an exact-body pin
on a switched call site, sentences verbatim, two pins ended stronger than before). Task 5's
first pass was "Needs work" — one Important: the non-JSON-body branch of `note()` had no pin
and a broken wrap would print a `--json` run's stdout empty; the fix wave landed exactly the
test-only pin asked for (`4f72c26`, micro-RED independently reproduced) and the scoped
re-review approved with zero new findings. Task 6's first pass was "Needs work" — an
environment revert had un-landed one SKILL.md edit while the client test-fake still modeled
the retired plain-404 delete answer, keeping tests green on a stale model; the fix wave
(`7dcd6bf` + `d885a3d`) landed the skill sentence, the folder-action wording, and a fake that
answers delete refusals as the live server does, and the scoped re-review approved with zero
new findings. Controller rulings R1–R5 and the per-task adjudications recorded in
`.superpowers/sdd/0033-json-api-responses/progress.md` all honored. Final gates
`env -u SANDBIN_API_KEY make test` green against the tree carrying this Status — `gofmt -l .`
empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...` and `go test -race -count=1
./...` all ok (`cmd/sandbin` 3.610s / 41.825s, `cmd/sandbin-client` 0.512s / 5.824s,
`internal/frontpage` 0.006s / 1.023s, `internal/robots` 0.004s / 1.014s, `internal/version`
0.004s / 1.016s) — with `cmd/sandbin` **271**, `cmd/sandbin-client` **132**, `internal/frontpage`
17, `internal/robots` 2, `internal/version` 6: no test deleted, five renamed (each keeping
its slot), 22 added — 260/121 at `085bccf` → 264/123 at Task 1 → 265/123 at Task 2 → 266/123
at Task 3 → 270/123 at Task 4 → 270/131 at Task 5 and 270/132 at its fix wave → 271/132 at
Task 6; the three `internal/*` counts never moved.

Assertions that moved, exactly the task-listed families, each verified by its task's reviewer
against the tree and listed with full names in `task-*-report.md`: **Task 1** — one rename
(`TestAPILiteralBraceSpellingIsPlainFourOhFour` → `TestAPILiteralBraceSpellingIsAPINotFound`)
plus body-text-only moves in 20 tests (the RED list in the Task 1 report: from
`TestAPIIndexShapes` through `TestOpenAPIServeShapes`), every sentence verbatim inside the
exact envelope, statuses and charging assertions untouched. **Task 2** — 17 assertions across
four tests (`TestAPICreateBinRefusals` 7 cases + the oversize block, `TestAPICreateBinBodyShapes`
loop want, `TestAPIPatchJSON` 7 cases, `TestAPIPatchShapes` 2 assertions). **Task 3** — the
four 204 pins renamed (`TestAPIKeyCheck` → `TestAPIKeyCheckAnswersOKBody`, `TestAPIDeleteBin`
→ `TestAPIDeleteBinAnswersOKBody`, `TestAPIKeyAnswersNoContent` → `TestAPIKeyAnswersOKBody`,
`TestClientVerifyKey204` → `TestVerifyKeyAcceptsOKBody`) and the golden bodies extended with
`"status":"ok"` as first key (index body, create pin, 7 PATCH rows, empty-list body, 7 archive
goldens, the `putEntry` 3→4-key pin, the `deletedBody` helper) plus `openapi_test.go`'s
`apiIndexDoc` pins. **Task 4** — 34 pins one-for-one: 12 blocks in `handlers_test.go`
(`TestPutUnknownBin` … `TestFallbackNeverShadowsAnything`) and 22 refusal comparisons in
`archive_test.go`; only the two decision-named renames (`unknown bin` → `bin not found` on
the write halves, `unknown path` → `path not found` at `deletePath`), everything else verbatim
inside the envelope. **Task 5** — no pre-existing client assertion moved: 123 pass
byte-unchanged, and the reviewer's 20-scenario base-vs-head default-output comparison found
identical stdout, stderr and exit codes everywhere; the fake gained `putFailBody` /
`putFailAfter` knobs. **Task 6** — `TestDocsNoRetiredSpellings` gained a plain `204` scan
(controller ruling over the brief's `204 No Content` wording), and the fix wave moved one pin
(`TestDeleteUnknownPathNamesThePathAndMakesOneRequest`) together with the fake's answer.
Every stay-green gate stayed green unchanged: `TestOldRoutesRetired`,
`TestDocsUnknownPathsArePlainNotFound`, the `/openapi.json/` and `/openapix.json` near-miss
404 pins, and the plain-side `getFile`/`binRoutes` bodies.

### The live walk (Task 7)

`make build` produced `dist/sandbin` and `dist/sandbin-client`. The walk ran in
`/tmp/opencode/sandbin-0033-walk` against `serve --addr 127.0.0.1:18084 --db sandbin.db`
started inside that directory (key `walk-0033-key`; every command names its own environment —
this shell exports `SANDBIN_URL` at an old production server plus a broken 66-char
`SANDBIN_API_KEY`, so no ambient value decided any step). Port 18084 rather than 18080
because four stray `sandbin serve` processes from earlier sessions' scratch work hold
18080–18083 (found via `/proc/*/cmdline`; left running). Full byte-honest transcripts — every
`curl` with its `-i` headers, every client run with its exit code and stderr bytes — are in
`.superpowers/sdd/0033-json-api-responses/task-7-report.md`. Highlights, all as documented:

- `GET /api/bananas` (keyless, uncharged) and `GET /api/bins/` → 404
  `{"status":"error","code":404,"message":"API endpoint not found"}`;
  `GET /api/bins/deadbeef` → 404 `bin not found`.
- Wrong key on `POST /api/bins` → 403 `{"status":"error","code":403,"message":"invalid API
  key"}`; the next wrong-key attempt answered 429 `{"status":"error","code":429,"message":
  "too many bad API keys"}` with `Retry-After: 1799`, then 3599, 5399, 7199 — one charged
  failure blocks the address and every attempt while blocked walks the block up 30 minutes
  (`ip list` read `FAILURES 5`, `2h30m0s` remaining; `ip unblock` restored service).
- `PUT` into a password bin without Basic Auth → 401 envelope `bin password required` beside
  `Www-Authenticate: Basic realm="sandbin"`.
- `POST /api/key` with the good key → 200 `{"status":"ok"}`; `DELETE /api/bins/{hash}` → 200
  `{"status":"ok"}`, and the follow-up `GET` reads 404 `bin not found`.
- `GET /api/bins` → `{"status":"ok","bins":[{…}]}` — top-level `status` first, rows
  status-free; `PUT /{hash}/` → 400 `empty path` envelope; `DELETE` of a missing path → 404
  `path not found` envelope; `POST /openapi.json` → 405 envelope, while `/openapix.json` and
  `/openapi.json/` still answer plain `unknown bin` and `HEAD /api` answers 200 headers-only
  (`Content-Length: 1301`, no body on the wire).
- The plain half held at the same paths: `GET /{hash}/gone.txt` plain `unknown path`, `GET`
  under an unknown hash plain `unknown bin`, a protected bin's GET file plain 401 (still with
  `Www-Authenticate`), and `PATCH /{hash}/` plain `method not allowed`.
- Client: `upload --json file.md` printed the full object (create + store actions, `bin` and
  `url` named, exit 0); `delete --json file.md gone.txt` stopped the array at the failed step
  (`result` the 404 envelope), `message":"path not found"`, exit 1, **zero bytes on stderr**;
  the same into a protected bin without `SANDBIN_BIN_PASSWORD` reported the 401 envelope as
  the step's `result` while the warning line stayed on stderr; `list --json` carried no
  `bin`/`url` keys; a default-mode `delete gone.txt` printed exactly
  `sandbin-client: delete "gone.txt": 404 path not found`; `init --help` offers no `--json`
  and `init --json` answers `Incorrect Usage: flag provided but not defined: -json`.
- The walk contradicted no documented claim. Two behaviors are live-only observations: the
  429 ramp above (tests pin the header's presence, not its growth) and `PATCH /{hash}`
  without its trailing slash answering plain 404 `unknown bin` — 0013's added-after-shipping
  rule (redirect is GET-only), which the pages quote only in the slash form.

Then the server was sent SIGTERM and `127.0.0.1:18084` answers connection-refused; nothing
was created outside the scratch directory.

Carried findings — every one adjudicated in the task reviews or this walk; none blocks the
merge:

1. The fixed action sentence `verify the API key` exists in code and `verifyKey` records it,
   but no verb run emits it: `verifyKey` is called only by `init`; the four verbs attach the
   key per request. Plan-vs-actual drift from the brief's example action lists, ruled away in
   Task 5 (actions are the requests actually made); Task 6 documents only actions that occur.
2. `verifyKey` accepts a bare `200` without parsing `{"status":"ok"}` — adjudicated
   spec-true (its only 200-expected endpoint; a bogus 200 is caught by the next call's 403),
   kept unhardened.
3. The no-leading-slash bin 404 in `ServeHTTP`'s classification stays plain even for
   `PUT`/`DELETE` — reachable only in theory (absolute-form request URIs); compliant as
   scoped, and this walk confirms it is not an inconsistency to flag.
4. A bin this run created is unnamed in a `--json` report when the `.sandbin.yaml` write
   fails: `bin`/`url` are absent while `actions[0].result` still carries the hash — cosmetic.
5. `json.Marshal` HTML-escapes `<`, `>`, `&` inside `result` values — semantically identical
   JSON, a display detail.
6. The dispatcher's 404 pair and 405s are described in the generated document's
   `info.description` prose, not as operations: kin-openapi v0.153.0 models no path-level
   responses and the operations must equal the route table (`TestOpenAPISpecMatchesRouteTable`
   rules out a wildcard path). Recorded honestly in 0030's amendment and 0033's addendum.
7. `HEAD` keeps answering 200 headers-only — the one remaining success with no body, a
   transport rule rather than a choice, pinned by correction 16.
8. The 204→200 trade-off, restated: `POST /api/key` and `DELETE /api/bins/{hash}` gave up the
   empty `204` so every JSON success carries a parseable body.
9. `writeAPIError` discards marshal/write errors (these DTOs cannot fail to marshal; a failed
   write means the visitor is already gone) — noted under ruling R5(2), unchanged.
10. With `--json`, a transport failure's `message` is the client's own wrapped Go sentence
    (`… connect: connection refused`) and a never-answered step adds no action — the report
    distinguishes "the server refused step N" from "step N never got an answer".
11. A non-JSON refusal body rides in `result` as a JSON string (`"result":"invalid API key"`)
    so the report always parses — pinned by `TestJSONActionRecordsNonJSONBodyAsPlainString`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every refusal on a JSON-answering route answers `{"status":"error","code":<http status>,"message":"…"}`, every JSON success body carries `"status":"ok"` at its top level, and `sandbin-client` keeps its relayed line while gaining a machine-readable `--json` action report.

**Architecture:** One `writeAPIError` helper in `cmd/sandbin/api.go` replaces the `http.Error` calls the API dispatcher answers, so the envelope's `code` can never disagree with the transport status. The shared `requireKey` switches wholesale (every route it guards already answers JSON on success); `binAuthorized` gains an `envelope` flag so the JSON pairs answer 401 in the envelope while the plain `GET` file side keeps today's text. Success DTOs gain a `Status` field at the top level only — `binJSON` and `fileJSON` carry it `omitempty` and set it only at their top-level call sites. `make openapi` regeneration lands in the same task as each live success change, because `TestOpenAPIBodySchemasMatchLiveAnswers` holds live bodies to the committed document's closed schemas. The client's `newAPIError` prefers the envelope's `message` and falls back to the trimmed body, so every relayed line a person sees is unchanged.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`), `urfave/cli` v2.27.7 for the client, `apispec` v0.5.11 + `jq` for the generated `openapi.json`, `kin-openapi` as a test-only contract checker.

**Spec:** [docs/decisions/0033-json-api-responses.md](../decisions/0033-json-api-responses.md)

## Global Constraints

- Gates at every task end: `env -u SANDBIN_API_KEY make test`, which is `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`. This shell exports `SANDBIN_API_KEY` as a 66-character quote-wrapped value that breaks `cmd/sandbin`'s `TestServeFlagParsing` on an untouched tree — that is why the gate runs under `env -u`; new tests name their own environment explicitly.
- Never push; never tag. Commit per task with the house prefixes (`feat:`, `test:`, `docs:`; `feat(client):`, `test(client):` for client code).
- Test counts may grow, never shrink: `cmd/sandbin` 260, `cmd/sandbin-client` 121, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6 (`go test -list '.+' | grep -c '^Test'`). No test is deleted; renames keep the count.
- Only the assertions named in a task's "assertions that move" list may move. Any other pre-existing test that turns red means the implementation is wrong — fix the code, not the test, and report it.
- Exact envelope, verbatim in code, tests and docs: `{"status":"error","code":<http status>,"message":"<sentence>"}` in that field order, `Content-Type: application/json`, trailing newline. `code` always equals the response's HTTP status.
- The only new sentences: `API endpoint not found` (a path the API table does not name), `bin not found` (an unresolved `{hash}` on a JSON pair), `path not found` (where `deletePath` today answers `unknown path`). Every other refusal sentence goes into the envelope unchanged.
- Success bodies: `"status":"ok"` is the first key of each top-level JSON success. Nested bin rows and nested file rows carry no `status`. `POST /api/key` and `DELETE /api/bins/{hash}` answer `200` + `{"status":"ok"}` (the 204s are retired); `HEAD` keeps answering `200` headers-only.
- Do not touch: `install.sh`, the landing-page templates, `Containerfile`, `deploy/`, `scripts/`, `Makefile`, `internal/*`, the `ip` control socket's `{"error":…}` contract, and every plain-text refusal outside the API dispatcher and the `PUT`/`DELETE` file pairs — the front page, file `GET` refusals, `/robots.txt`, skill/installer/docs routes, the plain unknown-path 404 `unknown bin`, the `binRoutes` default-method 405, and `PATCH /{hash}`.

## Review Focus

The failure modes the spec implies but no single task's happy-path test exercises. Each line gets its pin in the named task.

1. **Envelope honesty** (Tasks 1, 2, 4): a refusal body whose `code` disagrees with the transport status, a refusal written without `Content-Type: application/json`, or a 429 that loses its `Retry-After` header / a 401 that loses `WWW-Authenticate` when wrapped. Expected: `code` is always the status just set, both headers survive the envelope.
2. **Relay fallback** (Task 1): a refusal body that is *not* an envelope — today's plain `method not allowed` / `unknown bin` from untouched routes, a proxy's HTML error page, an empty body. Expected: the client prints the trimmed first body line exactly as today, never raw JSON, never a blank message.
3. **Protected-bin 401 on JSON pairs** (Tasks 1, 4): the envelope must not disturb what the 401 means — `bin password required` wording, `WWW-Authenticate`, and the client's `set SANDBIN_BIN_PASSWORD` hint all stay.
4. **The 204→200 migration** (Task 3): `sandbin-client`'s key check must accept `200` + `{"status":"ok"}` as the good key and still answer the 403 refusal sentence for a wrong key; a server-internal 500 refusal stays a refusal.
5. **`--json` mid-run failure** (Task 5): an upload whose third file is refused must still print one complete JSON object on stdout (array stopped at the failed step, its `result` printed), exit non-zero, print no `sandbin-client:` error line, and still print the protected-bin warning on stderr.

## File Structure

- `cmd/sandbin/api.go` — `apiErrorBody` DTO + `writeAPIError` helper; the dispatcher's 404/405; every `http.Error` site inside the API handlers; the success DTOs' `Status` fields; the two ex-204 sites.
- `cmd/sandbin/handlers.go` — `requireKey` wholesale envelope; `binAuthorized(w, r, bin, envelope bool)`; `putFile`/`deletePath` refusal families; `ServeHTTP`'s bin-path classification refusals in `PUT`/`DELETE` mode only; `fileJSON`/`archiveAnswer`/`deleteBody` `Status` fields.
- `cmd/sandbin/api_index.go` — `apiIndexDoc.Status`.
- `cmd/sandbin/apispec.yaml`, `cmd/sandbin/openapi-fix.jq`, `cmd/sandbin/openapi.json` — the generated contract, regenerated whenever a documented answer changes.
- `cmd/sandbin/api_test.go`, `api_index_test.go`, `handlers_test.go`, `openapi_test.go`, `docs_test.go` — pins that move, pins that are new.
- `cmd/sandbin-client/client.go` — `newAPIError` envelope parsing; the `--json` report types; `verifyKey` 200.
- `cmd/sandbin-client/main.go` + `main_test.go`, `init_test.go` — the `--json` flag on the four verbs, output routing, pins.
- `docs/usages/sandbin-api.md`, `sandbin.md`, `sandbin-client.md` + member pages, `skills/sandbin-preview/SKILL.md` — Task 6 only.

---

### Task 1: The API dispatcher answers the error envelope

**Files:**
- Modify: `cmd/sandbin/api.go` (new DTO + helper near `writeJSON` at :204-215; dispatcher refusals :44, :48; every `http.Error` site inside `apiHealth`/`apiCreateBin`/`apiListBins`/`apiInspectBin`/`apiPatchBin`/`apiDeleteBin`; `binAuthorized` call sites :518, :591, :669)
- Modify: `cmd/sandbin/handlers.go` (`requireKey` :205-225; `binAuthorized` :534-554 signature and body; its `getFile` call site :246)
- Modify: `cmd/sandbin/apispec.yaml` (`responsePatterns`), `cmd/sandbin/openapi-fix.jq`, `cmd/sandbin/openapi.json` (regenerated)
- Test: `cmd/sandbin/api_test.go`, `cmd/sandbin/openapi_test.go`, `cmd/sandbin/handlers_test.go`
- Test: `cmd/sandbin-client/client.go` (`newAPIError` :37-49), `cmd/sandbin-client/main_test.go`

**Interfaces:**
- Consumes: the route table (`apiPattern*` consts, `apiLookupMethodSet`) and `withAPIKey` unchanged.
- Produces (later tasks use exactly these):
  - `type apiErrorBody struct { Status string `json:"status"`; Code int `json:"code"`; Message string `json:"message"` }` and `func (s *server) writeAPIError(w http.ResponseWriter, status int, message string)` in `api.go` — sets `Content-Type: application/json`, writes `status`, marshals `apiErrorBody{"error", status, message}` plus trailing newline. Every refusal in scope calls only this.
  - `func (s *server) binAuthorized(w http.ResponseWriter, r *http.Request, bin Bin, envelope bool) bool` — `true` answers 401 through `writeAPIError` (after setting `WWW-Authenticate`), `false` keeps today's `http.Error`.
  - Client `newAPIError`: when the body unmarshals to an object with `status == "error"` and a string `message`, the `apiError.message` is that message; otherwise the trimmed first body line, as today. `apiError.status` stays the transport status; the 429 `retry after` relay format is unchanged.

**Steps:**

- [ ] **Step 1: Write the failing envelope pins** in `api_test.go`:
  - `TestAPIErrorEnvelopeShape`: `GET /api/health` on a closed store (reuse the existing 500 mechanism if one exists; otherwise `POST /api/bins` with a 400-provoking body) — assert `Content-Type: application/json`, exact body `{"status":"error","code":<the status just answered>,"message":"<the existing sentence>"}\n` in that field order.
  - `TestAPIEndpointNotFoundPair`: `GET /api/bananas` and `GET /api/bins/` answer 404 `{"status":"error","code":404,"message":"API endpoint not found"}`; `GET /api/bins/deadbeef` (no such bin) answers 404 `{"status":"error","code":404,"message":"bin not found"}`.
  - `TestKeyRefusalIsEnvelope`: wrong key on `POST /api/bins` answers 403 envelope `invalid API key` and still charges; the lockout refusal 429 keeps `Retry-After` next to the envelope.
  - `TestProtectedBinAPI401IsEnvelope`: `PATCH` a password bin without Basic Auth answers 401 envelope `bin password required` **and** keeps `WWW-Authenticate: Basic realm="sandbin"`.

- [ ] **Step 2: Run to verify they fail** — `go test ./cmd/sandbin -run 'TestAPIErrorEnvelopeShape|TestAPIEndpointNotFoundPair|TestKeyRefusalIsEnvelope|TestProtectedBinAPI401IsEnvelope' -v`. Expected: FAIL, bodies are today's `unknown bin\n` / `invalid API key\n` plain text.

- [ ] **Step 3: Implement.** Add `apiErrorBody` + `writeAPIError` per the Interfaces block. In `apiRoutes` (`api.go:44`) the table miss becomes `s.writeAPIError(w, http.StatusNotFound, "API endpoint not found")` and (`:48`) the 405 `s.writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")`. Replace every `http.Error` in `apiHealth`, `apiCreateBin`, `apiListBins`, `apiInspectBin`, `apiPatchBin`, `apiDeleteBin` with `s.writeAPIError` carrying the same status and sentence; the `unknown bin` 404s at `:511/:515/:582/:586/:626/:635/:641/:662/:666/:681` take the sentence `bin not found`. Switch `requireKey` (:213, :221) to the envelope (its three call sites are `withAPIKey`, `putFile`, `deletePath` — all JSON pairs). Change `binAuthorized`'s signature per Interfaces; `true` at `api.go:518/:591/:669`, `false` at `handlers.go:246` (the `PUT`/`DELETE` call sites `:335`/`:474` stay `false` until Task 4).

- [ ] **Step 4: Move the refusal-shape assertions** (assertions that move — body text only, statuses and charging unchanged): `TestAPIUnknownShapes` (404 bodies → the two new sentences in envelopes; rename the 405 sub-assertions to the envelope body); `TestAPILiteralBraceSpellingIsPlainFourOhFour` → `TestAPILiteralBraceSpellingIsAPINotFound` (envelope `API endpoint not found`, charging assertions unchanged); the 403/401/429 body assertions in `TestAPIRouteTableIsPublishedSurface`, `TestAPIDeleteBinProtected`, `TestAPIInspectBin` and `handlers_test.go`'s key-refusal blocks. `TestOldRoutesRetired` and `TestDocsUnknownPathsArePlainNotFound` must stay green **unchanged** — `/healthz` and `/verify-key` fall through to the plain bin-path 404.

- [ ] **Step 5: Update the generated contract.** `apispec.yaml`: add one `responsePatterns` entry mirroring the `^Error$` entry with `callRegex: ^writeAPIError$`, `statusArgIndex: 1`, no `typeFromArg`, same `callerRecvTypePatterns`, `functionNameRegex` extended to the functions listed above (keep `requireKey|binAuthorized` in it; move `apiRoutes` there too). `openapi-fix.jq`: replace correction 11 (which rewrites error bodies to `text/plain` strings) so every documented non-2xx response carries `application/json` with the named component `apiErrorBody` — properties `status`, `code`, `message`, all required, `additionalProperties: false`. Run `make openapi`; commit the regenerated `cmd/sandbin/openapi.json` with the task.

- [ ] **Step 6: Move the served-document pins.** `TestOpenAPIServeShapes`: the `HEAD`/`POST /openapi.json` 405 pins expect the envelope body `{"status":"error","code":405,"message":"method not allowed"}\n`; the `/openapi.json/` and `/openapix.json` 404 pins stay `unknown bin\n` unchanged (they never reach the dispatcher).

- [ ] **Step 7: Client relay, TDD.** In `cmd/sandbin-client`: new `main_test.go` tests `TestRelayReadsEnvelopeMessage` (a fake server answering 403 envelope → the relayed line is `403 invalid API key`, the `sandbin-client: 403 invalid API key` output byte-identical to today) and `TestRelayFallsBackToPlainBody` (plain `method not allowed\n`, an HTML page, and an empty body → today's trimmed-line behavior, empty body answering the status text). Implement in `newAPIError` per Interfaces. Existing relay assertions in `main_test.go` (403, 429 `retry after`) must stay green unchanged.

- [ ] **Step 8: Run the covering tests, then the gates.** `go test ./cmd/sandbin ./cmd/sandbin-client -count=1`, then `env -u SANDBIN_API_KEY make test`. Expected: all green; counts `cmd/sandbin` ≥ 260 (renames keep counts), `cmd/sandbin-client` ≥ 121.

- [ ] **Step 9: Commit.**

```bash
git add cmd/sandbin cmd/sandbin-client
git commit -m "feat: API refusals answer the JSON error envelope"
```

---

### Task 2: Request-shape and internal refusals join the envelope

**Files:**
- Modify: `cmd/sandbin/api.go` (the remaining `http.Error` sites: `decodeBinRequest`'s and the 400 family at :269-298, :345-356, :446, :459, :562-575 and the 500 family at :365-390, :468-483, :525-538, :611-647, :676)
- Modify: `cmd/sandbin/openapi.json` (regenerated, if the documented error set shifts)
- Test: `cmd/sandbin/api_test.go`

**Interfaces:**
- Consumes: Task 1's `writeAPIError`.
- Produces: nothing new — this task completes the API-dispatcher surface, so "every refusal `apiRoutes` answers" is true from here on.

**Steps:**

- [ ] **Step 1: Write the failing pin.** `TestAPIRequestShapeRefusalsAreEnvelopes` in `api_test.go`: a malformed JSON body on `POST /api/bins` (400 `could not read the request`), an unrecognized field (400 `unrecognized field in the request`), `limit=0` on `GET /api/bins` (400 `invalid limit`), a bad cursor (400 `invalid cursor`), a 300-byte title (400 `title over 200 bytes`) — each asserts the exact envelope with the existing sentence and `code` equal to the status.

- [ ] **Step 2: Run to verify it fails** — `go test ./cmd/sandbin -run TestAPIRequestShapeRefusalsAreEnvelopes -v`. Expected: FAIL, plain bodies.

- [ ] **Step 3: Implement.** Replace every remaining `http.Error` call inside `api.go`'s API handlers (and `decodeBinRequest`, which answers `could not read the request`) with `writeAPIError`, same status and sentence. The 500 family (`could not create bin`, `could not answer the bin`, `could not list bins`, `could not list bin`, `could not set the bin password`, `could not update bin`, `could not delete bin`, `could not answer health`, `could not answer the delete`-style siblings) joins the same way.

- [ ] **Step 4: Move the refusal assertions** (assertions that move): the body-shape blocks in `TestAPICreateBinRefusals`, `TestAPICreateBinBodyShapes`, `TestAPIPatchShapes`, `TestAPIDeleteBinShapes`, `TestAPIListBinsLimits`, `TestAPIListBinsCursorIntegrity`, `TestAPIPatchJSON` — each quoted sentence stays verbatim inside the envelope. No status changes.

- [ ] **Step 5: Run the covering tests, re-run `make openapi` if the document's error entries shifted** (the Task 1 pattern already covers these functions; the regenerated document should differ only where statuses appear), then the gates.

- [ ] **Step 6: Commit.**

```bash
git add cmd/sandbin
git commit -m "feat: request-shape and internal refusals answer the JSON envelope too"
```

---

### Task 3: Every JSON success says `ok`; the empty 204s become 200 bodies

**Files:**
- Modify: `cmd/sandbin/api.go` (`okBody` DTO; `apiKeyCheck` :186-188 and `apiDeleteBin` :684; `binJSON`, `binListJSON`, `binDetailJSON` `Status` fields; the top-level `writeJSON` sites)
- Modify: `cmd/sandbin/handlers.go` (`fileJSON`, `archiveAnswer`, `deleteBody` `Status` fields; `putFile` :388 and archive :429 top-level answers; `deletePath`'s hand-rolled JSON answer at :508-516 gains the field)
- Modify: `cmd/sandbin/api_index.go` (`apiIndexDoc.Status` first field)
- Modify: `cmd/sandbin/apispec.yaml` (the `writeJSON` `responsePatterns` entry's `functionNameRegex` gains `apiKeyCheck|apiDeleteBin`; drop the `WriteHeader` entry, which existed only for those two), `cmd/sandbin/openapi.json` (regenerated)
- Test: `cmd/sandbin/api_test.go`, `api_index_test.go` (or the golden pins wherever they live), `openapi_test.go`; `cmd/sandbin-client/client.go` (`verifyKey` :370), `cmd/sandbin-client` tests pinning the key check

**Interfaces:**
- Consumes: Task 1's `writeJSON` unchanged.
- Produces: `type okBody struct { Status string `json:"status"` }` in `api.go`; `Status string `json:"status"` as the first field of `binListJSON`, `binDetailJSON`, `archiveAnswer`, `deleteBody`, `apiIndexDoc` (no `omitempty`), and `Status string `json:"status,omitempty"` as the first field of `binJSON` and `fileJSON` — set to `"ok"` only at their top-level call sites (`apiCreateBin`, `apiPatchBin`, `putFile`), left empty in list/detail rows so it is omitted there.

**Steps:**

- [ ] **Step 1: Write the failing tests.**
  - `TestAPIKeyCheckAnswersOKBody` (moves `TestAPIKeyCheck`'s 204 expectation): a good key `POST /api/key` answers 200, `Content-Type: application/json`, exact body `{"status":"ok"}\n`.
  - `TestAPIDeleteBinAnswersOKBody`: a `DELETE /api/bins/{hash}` answers 200 + `{"status":"ok"}\n`; the follow-up `GET` still answers `bin not found`.
  - Extend the golden success bodies: the `GET /api` index body and `TestAPICreateBin`'s pinned bin object gain `"status":"ok"` as the first key; `GET /api/bins` rows carry no `status` inside each row while the top level has it first — pin both.
  - `TestNestedRowsCarryNoStatus`: each entry of `GET /api/bins` and each entry of the `GET /api/bins/{hash}` file list has no `status` key (unmarshal into `map[string]any`, assert absence).

- [ ] **Step 2: Run to verify they fail** — `go test ./cmd/sandbin -run 'TestAPIKeyCheck|TestAPIDeleteBinAnswersOKBody|TestNestedRowsCarryNoStatus' -v`. Expected: FAIL (204s, missing `status`).

- [ ] **Step 3: Implement** per the Interfaces block; `apiKeyCheck` and `apiDeleteBin` answer through `writeJSON(w, http.StatusOK, okBody{})`.

- [ ] **Step 4: Client key check, TDD.** Move the client test that pins `verifyKey` against a 204 to `TestVerifyKeyAcceptsOKBody`: fake server answering 200 + `{"status":"ok"}` → `verifyKey` succeeds; a wrong key still answers the 403 sentence. `case http.StatusNoContent:` becomes `case http.StatusOK:` in `client.go:370`.

- [ ] **Step 5: Regenerate the contract.** `apispec.yaml`: add `apiKeyCheck|apiDeleteBin` to the `writeJSON` entry's `functionNameRegex`, remove the `WriteHeader` entry. `make openapi`. In `openapi_test.go`: `TestOpenAPIBodySchemasMatchLiveAnswers` gains two validations — the live `POST /api/key` 200 and `DELETE /api/bins/{hash}` 200 validate against the `okBody` component — and the closed-component property pins (`apiIndexDoc`) gain `status`. The `HEAD /api` 200 stays content-map-less (correction 16).

- [ ] **Step 6: Run the covering tests, then the gates.** `go test ./cmd/sandbin ./cmd/sandbin-client -count=1`, then `env -u SANDBIN_API_KEY make test`. Expected: green including `TestOpenAPIBodySchemasMatchLiveAnswers` against the regenerated document.

- [ ] **Step 7: Commit.**

```bash
git add cmd/sandbin cmd/sandbin-client
git commit -m "feat: every JSON success says ok, and the empty 204s become 200 bodies"
```

---

### Task 4: The file `PUT`/`DELETE` pairs answer the envelope

**Files:**
- Modify: `cmd/sandbin/handlers.go` (`ServeHTTP` classification sites :155, :159 in `PUT`/`DELETE` mode; `putFile` :317-431 refusals; `deletePath` :447-511 refusals; `binAuthorized` call sites :335, :474 flip to `true`)
- Test: `cmd/sandbin/handlers.go` untouched on the `GET` side — `getFile` and `binRoutes`' default-method 405 stay byte-identical; `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Consumes: Task 1's `writeAPIError` and `binAuthorized(..., envelope bool)`.
- Produces: nothing later tasks consume; this closes the "outside `/api`" half of the spec.

**Steps:**

- [ ] **Step 1: Write the failing tests.**
  - `TestFileRouteRefusalsAreEnvelopes`: `PUT /{hash}/` (400 `empty path`), `DELETE /{hash}/` (405 `method not allowed`), `PUT` with a `.`-path (400, existing `cleanPath` sentence), an over-1-MiB body (413 `body over 1 MiB`), `PUT` a zip with a bad archive (the existing 400/413 sentences with their dynamic status), `DELETE` a path nothing exists under (404 `path not found`), and `PUT` to an unknown hash (404 `bin not found`) — each asserts the envelope with `code` equal to the status.
  - `TestFilePutPassword401IsEnvelope`: `PUT` a protected bin without the password answers the 401 envelope **with** `WWW-Authenticate` (Review Focus 3).
  - `TestFileGetRefusalsStayPlain`: `GET` the same refused shapes — unknown bin (plain `unknown bin`), unknown path (plain `unknown path`), protected bin (plain 401) — assert today's bodies unchanged (the boundary must not leak into `GET`).
  - `TestDotPathRefusalFollowsMethod`: `PUT /{hash}/../x` answers the 400 envelope while `GET /{hash}/../x` answers today's plain sentence — the classification refusals at `handlers.go:155`/`:159` are per method.

- [ ] **Step 2: Run to verify they fail** — `go test ./cmd/sandbin -run 'TestFileRouteRefusals|TestFilePutPassword401|TestFileGetRefusalsStayPlain|TestDotPathRefusalFollowsMethod' -v`. Expected: FAIL on the envelope assertions, the `GET` pins already pass.

- [ ] **Step 3: Implement.** `putFile` and `deletePath` write every refusal through `writeAPIError`, same sentences (the `unknown bin` pairs at `:329/:332/:468/:471` take `bin not found`; `deletePath`'s `:504` takes `path not found`); their `binAuthorized` calls pass `true`. In `ServeHTTP`, when `r.Method` is `PUT` or `DELETE`, the classification refusals (`:155` unknown bin → `bin not found`; `:159` dot-path 400) go through `writeAPIError`; every other method keeps `http.Error` verbatim. `getFile`, `binRoutes`' default 405, and the front-page/robots/root refusals are untouched.

- [ ] **Step 4: Move the refusal assertions** (assertions that move): the `PUT`/`DELETE` refusal blocks in `handlers_test.go` (403 key refusals already moved in Task 1; now the path-shape, size, archive, `delete takes no body`, 401 and 404 blocks). Every sentence verbatim inside the envelope. `internal/frontpage` and `TestDocsUnknownPathsArePlainNotFound` stay green unchanged.

- [ ] **Step 5: Run the covering tests, then the gates** — no `apispec`/`openapi.json` change: file routes are not documented operations.

- [ ] **Step 6: Commit.**

```bash
git add cmd/sandbin
git commit -m "feat: file PUT and DELETE refusals answer the JSON envelope"
```

---

### Task 5: `sandbin-client --json` prints the run as an action report

**Files:**
- Modify: `cmd/sandbin-client/main.go` (a `--json` bool flag on `upload`, `delete`, `describe`, `list` — not `init`; output routing per verb)
- Modify: `cmd/sandbin-client/client.go` (the report types; the action log threaded through `upload`'s steps)
- Test: `cmd/sandbin-client/main_test.go`, `init_test.go` untouched

**Interfaces:**
- Consumes: Task 1's relay (unchanged human output), Tasks 3-4's JSON bodies.
- Produces:

```go
type clientAction struct {
    Description string          `json:"description"`
    Result      json.RawMessage `json:"result"`
}
type clientReport struct {
    Status  string         `json:"status"`
    Bin     string         `json:"bin,omitempty"`
    URL     string         `json:"url,omitempty"`
    Actions []clientAction `json:"actions"`
    Message string         `json:"message,omitempty"`
}
```

  One JSON object printed to stdout with a trailing newline. `Actions` holds every server request the run made, in order; an action is a request, so reading `.sandbin.yaml` or reusing a stored bin is not one. Fixed `Description` sentences (documented in Task 6, pinned in tests): `verify the API key`, `create the bin`, `store <path>`, `set the bin title and description`, `delete <path>`, `describe the bin`, `list bins`. On failure: `Status:"error"`, `Message` is the last response's envelope `message` — or the client's own refusal sentence (no url, no key, connection refused, corrupt config) with `Actions` empty — the array stops at the failed step and that step's `Result` (the server's envelope) is printed anyway. `Bin`/`URL` appear whenever the run resolved a bin (list omits both). With `--json`: no `sandbin-client:` error line and no `set SANDBIN_BIN_PASSWORD` hint on stderr, but advisories (the protected-bin warning) still print there. Exit code is the verb's usual one.

**Steps:**

- [ ] **Step 1: Write the failing tests** in `main_test.go` (reuse the existing fake-server harness): `TestUploadJSONActions` (a create + key check + two file stores + metadata patch → actions in order with the fixed sentences, `Status:"ok"`, `Bin`/`URL` present, each `Result` the server's raw body); `TestUploadJSONReusesBinWithoutCreate` (a stored bin → no `create the bin` action, `Bin`/`URL` still named); `TestUploadJSONStopsAtFailedStep` (third file refused → `Status:"error"`, `Message` the refused envelope's message, three actions with the failed step's `Result` the refusal envelope, exit non-zero, stdout the only failure output, the protected-bin warning still on stderr — Review Focus 5); `TestListJSONActions` / `TestDescribeJSONObject` (single request, `list bins` / `describe the bin`); `TestDeleteJSONActions` (`verify the API key` + `delete <path>`); `TestJSONRefusalWithoutServerIsClientSentence` (no url → `Status:"error"`, client's sentence, empty `Actions`).

- [ ] **Step 2: Run to verify they fail** — `go test ./cmd/sandbin-client -run 'JSON' -v`. Expected: FAIL, the flag is unknown.

- [ ] **Step 3: Implement** the flag and report per the Interfaces block. `upload`'s threading: `verifyKey`, then bin create-or-resolve (a created bin is a `create the bin` action; a resolved one is not), then each file `PUT`, then the metadata `PATCH` only when the run sets title/description. Non-`--json` output for every verb stays byte-identical.

- [ ] **Step 4: Run the covering tests, then the gates** — `env -u SANDBIN_API_KEY make test`; `cmd/sandbin-client` count grows from 121.

- [ ] **Step 5: Commit.**

```bash
git add cmd/sandbin-client
git commit -m "feat(client): --json prints the run as a JSON action report"
```

---

### Task 6: The published pages carry the envelope, the `ok` bodies and `--json`

**Files:**
- Modify: `docs/usages/sandbin-api.md` (every refusal fence, the success bodies, the retired 204s, the 404 pair, the 405 spellings), `docs/usages/sandbin.md` (summary rows that describe answer shapes), `docs/usages/sandbin-client.md` and `docs/usages/sandbin-client-upload.md` (the only client member pages are `init` and `upload`; `delete`/`describe`/`list` live on the verb-grouped upload page and the summary page — document `--json` where those verbs are documented), `docs/usages/sandbin-client-skill.md` and `skills/sandbin-preview/SKILL.md` (one `--json` mention where the verbs are described)
- Test: `cmd/sandbin/docs_test.go` (pins that move, plus a new one below)

**Steps:**

- [ ] **Step 1: Update the API page first** — copy sentences and bodies from the code, not from memory: refusal fences become the envelope JSON with the same sentences; add the `API endpoint not found` / `bin not found` / `path not found` spellings with their triggers; success fences show `"status":"ok"` first; the key check and bin delete now answer 200 + `{"status":"ok"}` (say so in prose, keeping the deliberate 204 trade-off visible); a wrong method on `/api` routes answers the 405 envelope.

- [ ] **Step 2: Update the client pages** — document `--json` on the four verbs with its object shape and the fixed `description` sentences from Task 5's Interfaces (verbatim), the stdout-on-failure + exit-code rule, and that warnings stay on stderr. `init` says plainly it has no `--json`.

- [ ] **Step 3: Move the docs pins.** `TestDocsNoRetiredSpellings` gains `204 No Content` as a retired spelling for the two retired routes (keep `GET /api/health`'s and any control-socket wording out of its reach). Add `TestDocsEnvelopeSectionNamesBoth404s`: the API page renders both new 404 sentences and both retired-200 sentences. `TestDocsUnknownPathsArePlainNotFound` stays green unchanged. If `internal/frontpage` or another docs test turns red on page wording, fix the prose until it agrees — do not bend the test.

- [ ] **Step 4: Run the gates** — `env -u SANDBIN_API_KEY make test`; counts unchanged.

- [ ] **Step 5: Commit.**

```bash
git add docs skills cmd/sandbin/docs_test.go
git commit -m "docs: the API and client pages carry the JSON envelope and --json"
```

---

### Task 7: Live verification, decision amendments, closing record

**Files:**
- Modify: `docs/plans/0033-json-api-responses.md` (its Status section)
- Modify: `docs/decisions/0029-api-bins-json-endpoints.md`, `0030-api-servemux-openapi.md`, `0031-api-index-endpoint.md`, `0013-delete-and-spa.md`, `0010-archive-upload.md`, `0012-serve-skill.md` — one amendment sentence plus a link to 0033 in each, as the decision doc's "Amends" list promises
- Scratch: `/tmp/opencode/sandbin-0033-walk`

**Steps:**

- [ ] **Step 1: Walk a real server** (`make build`; `serve --addr 127.0.0.1:18080 --db` inside the scratch dir; every command names its own environment explicitly). Record each command's real output: `curl` the `API endpoint not found` 404, the `bin not found` 404, wrong-key 403 envelope, lockout 429 envelope with its `Retry-After` header (`-i`), protected `PUT` 401 envelope with `WWW-Authenticate`, `POST /api/key` 200 body, bin `DELETE` 200 body, `GET /api/bins` top-level `status` with rowless entries, a file `PUT` refusal envelope, and a near-miss path (`/openapix.json`) still answering plain `unknown bin`.
- [ ] **Step 2: Walk `--json`** with `dist/sandbin-client`: a successful `upload --json` (transcribe the full object), a mid-run refusal (quota-size or password bin) showing the stopped array and the `403`-style exit, a `list --json`, and one default-output run proving the relayed line is unchanged.
- [ ] **Step 3: Amend the six decision docs** — one sentence each naming what 0033 changes about that decision (e.g. 0029's "refusals stay plain text" and 204 spellings; 0013's `unknown path` wording on `DELETE`) plus a link to `0033-json-api-responses.md`. Change no other line.
- [ ] **Step 4: Write the Status section** at the top of this plan (commit list, walk outputs, gate results with the counts, every assertion that moved, findings carried), then the final gates.
- [ ] **Step 5: Commit.**

```bash
git add docs
git commit -m "docs: record plan 0033 verification and amend 0029, 0030, 0031, 0013, 0010 and 0012"
```

---

## Self-review notes

- Spec coverage: envelope + code-as-status (T1, T2), 403 stays 403 (T1), sentences unchanged + 404 pair (T1, T4), `status:"ok"` top-level only (T3), 204→200 (T3), HEAD headers-only preserved (T3 step 5 pin), boundary incl. `/openapi.json` 405s and near-miss 404s (T1, T4), OpenAPI follows (T1, T2, T3), client relay + fallback (T1), `--json` object incl. `bin`/`url`, stopped array, message-from-last-response, stdout/exit rules, stderr advisories, `init` excluded (T5), docs follow (T6), amendments to the six decisions (T7).
- One spec row is implemented as a *pattern*, not a table: `writeAPIError` is the only refusal writer on JSON pairs; the plain side keeps `http.Error`. Reviewers should grep for stray `http.Error` inside `api.go` (must be none) and inside `putFile`/`deletePath` (must be none) after Task 4.
- Test-count floors are the current live counts (260/121/17/2/6 verified at plan-writing time on `d1a641c`).
