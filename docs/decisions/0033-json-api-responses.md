# Every JSON-answering route answers JSON, errors included — Decision

Decided 2026-11-26 with the partner. Plan:
[0033-json-api-responses](../plans/0033-json-api-responses.md). Amends
[0029-api-bins-json-endpoints](0029-api-bins-json-endpoints.md),
[0030-api-servemux-openapi](0030-api-servemux-openapi.md),
[0031-api-index-endpoint](0031-api-index-endpoint.md),
[0013-delete-and-spa](0013-delete-and-spa.md), [0010-archive-upload](0010-archive-upload.md) and
[0012-serve-skill](0012-serve-skill.md).

## Why

Success under `sandbin`'s API answers JSON; every refusal answers a plain sentence from
`http.Error`. A caller that parses the success body has to parse something else for the failure —
different content type, different shape, and no field that says which status the transport used.
The partner's word for the goal: **consistency is required for the caller to properly handle the
output of API calls.**

The server already has one JSON body that carries its own status: `GET /api/health` answers
`{"status":"ok"}`. This decision generalizes that shape across the API — successes say `ok`,
refusals say `error` with the status code and a message.

## The decisions

| Topic | Decision | Why |
|---|---|---|
| The error envelope | Every refusal on a JSON-answering route answers `Content-Type: application/json` with exactly `{"status":"error","code":<http status>,"message":"<sentence>"}`, in that field order. | One shape a caller parses once. `status` says which half of the contract it is, `code` repeats the transport so a body alone is self-explanatory, `message` is the human sentence. |
| `code` is the HTTP status, always | The `code` field equals the response's HTTP status. No code is invented or moved for the envelope's sake. | The partner's ruling. A JSON `code` that disagrees with the transport status is a second inconsistency on top of the one being removed. |
| 403 stays the API-key answer | A wrong or missing API key keeps answering `403` with `invalid API key` inside the envelope, keeps charging a lockout step, and `401` keeps meaning `bin password required` with its `WWW-Authenticate` header. | The partner's ruling after being shown the mismatch: their first example said `401` for the key, but `401` is already spoken for by the bin password, and `sandbin-client` reads that code to decide its `set SANDBIN_BIN_PASSWORD` hint. |
| Messages stay as they are | The sentences already in the code go into the envelope unchanged: `invalid API key`, `too many bad API keys`, `bin password required`, `method not allowed`, `could not read the request`, `unrecognized field in the request`, `invalid limit`, `invalid cursor`, `title over 200 bytes`, `bin password too long`, `body over 1 MiB`, `delete takes no body`, and the internal-failure sentences. | The partner's ruling: "use the same phrase is ideal unless there is a new need to new message(s)". These are already short and human, and roughly fifty test assertions, the OpenAPI examples and the API docs page quote them. |
| The 404 pair is the new need | Two 404 messages, because a 404 under `/api` happens for two different reasons that today share one sentence. A path that is not an endpoint (`/api/bananas`, `/api/bins/`) answers `API endpoint not found`. An endpoint whose resource id does not resolve answers `<resource> not found` — `bin not found` for `/api/bins/{hash}` and for the unknown bin on the JSON file routes, and `path not found` where today's `deletePath` answers `unknown path` for a path nothing exists under. `GET` sides keep today's spellings, per the method-plus-route rule below. The rule is a pattern: name the resource the id was meant to select. | The partner's ruling. `unknown bin` for `/api/bananas` describes a bin that is not there when the real problem is the URL, and a caller that retried or created a bin on the strength of that sentence was guessing. |
| Success bodies say `ok` | Every JSON success body gains `"status":"ok"` as its first key — at the top level of the response only: the `GET /api` index, the bin object, the bin list, the bin detail, the file answer, the archive answer and the delete answer. A nested row is data, not a response: bins listed inside `GET /api/bins` and files listed inside a bin's file list carry no `status` of their own. `GET /api/health` already reads that way and does not change. | The partner's ask, plus the partner's ruling on the builder check's finding that the same struct serves twice — `binJSON` is both a success answer and a list row, `fileJSON` both a `PUT` answer and a detail row. `status` answers "did this call work", a fact about the response, not about each record it contains. |
| No empty successes | `POST /api/key` and `DELETE /api/bins/{hash}` answer `200` with `{"status":"ok"}` instead of `204 No Content`. | The partner's ruling: always return a JSON body. A `204` must not carry a body, so a body means `200`; the "DELETE answers 204" convention is traded away deliberately, for one consistent shape, and this row is where that trade is recorded. |
| HEAD carries no body | `HEAD` keeps answering `200` with headers and no body, as `0031` decided for `HEAD /api`. | A `HEAD` body is not a body. This is the one remaining "success with no body", and it is a transport rule rather than a choice. Mine, not the partner's. |
| Which routes carry this | The boundary is the dispatcher, decided per method-plus-route. The API dispatcher handles `/api`, `/api/*` and the exact `/openapi.json`: every refusal it answers carries the envelope, the 404s and 405s included — and that covers `HEAD`/`POST /openapi.json`, whose 405 comes from the same table dispatch as the `/api` ones (the partner's ruling, reversing the first draft, which had left `POST /openapi.json` plain). Outside it: only the pairs that already answer JSON on success — `PUT /{hash}/{path}` (file and archive) and `DELETE /{hash}/{path}`. On those pairs every refusal produced while serving that method gets the envelope with its existing sentence — `empty path`, the path-shape 400s, the archive 400/413s, `delete takes no body`, the 405 at `DELETE /{hash}/` — and the unknown-bin 404 there reads `bin not found`, while the plain `GET` side of the same paths keeps `unknown bin` (mine, resolving the builder check's question: the rule follows the method-plus-route, not the helper that writes the refusal). Everything else is untouched: a file download, the front page, `/robots.txt`, the skill and installer files, the docs pages, the plain-text unknown-path 404 the docs pin, near-miss paths that never reach the API dispatcher (`/openapi.json/`, `/openapix.json` — they fall through to the bin path and answer today's plain `unknown bin`), a wrong method on a route that never answered JSON (`PATCH /{hash}`), and the `ip` family on the control socket with its own `{"error":…}` shape. | The partner's rule: extend to JSON responses outside `/api`, but leave a method-plus-route that does not already answer JSON alone. A half-JSON surface on the file routes is exactly the inconsistency being removed; a browser-facing 404 page is not a caller's failure; and a path that never reaches the API surface has no JSON half to be consistent with. |
| The OpenAPI document follows | `cmd/sandbin/openapi.json` is regenerated so every documented error carries the envelope as an `application/json` schema with `status`, `code` and `message` required, every success schema gains `status` (top-level bodies only, matching the nested-row ruling), and the `405` responses the API dispatcher now answers in JSON — `/api` and `/openapi.json` alike — are described in the document as well. | The partner's ask: "make sure the documentation and openapi.json follow the changes, if needed." The document is generated from the route table and the pinned correction script, so a stale one would be a published contract that lies. |
| The client keeps its line | `sandbin-client` keeps printing `sandbin-client: 403 invalid API key` exactly as it does now, by taking `message` out of the envelope. When the body is not an envelope — a plain-text 405 or 404 from a route that never answered JSON, a proxy's error page — it falls back to the trimmed first line of the body, as today. | The partner's ruling. The relayed line is what people and agent skills already read, and it is pinned by client tests; the envelope is for programs that asked for it. |
| `--json` on the client | `--json` is offered to `upload`, `delete`, `describe` and `list`. It prints one JSON object on stdout: `status` (`ok` or `error`), `bin` (the bin's hash) and `url` (its preview address) whenever the verb worked on a bin, `actions` (the steps the client actually performed, each `{"description": "<fixed sentence>", "result": <that step's raw server JSON>}`), and `message` only when the status is `error`. The array stops at the failed step, and that step's `result` is printed anyway. On failure the object still goes to stdout and the process exits with the client's usual error code. | The partner's ruling, refined three times: an array of actions with `description` and `result`; wrapped in an object carrying its own `status`, `actions` and an optional `message` copied from the last response "for the ease of use"; and the bin and its address named at top level, because when an existing bin is reused the step results are per-file answers that name neither — a caller must not have to already know the hash or dig it out of the steps. A caller gets one parse path whether the run worked or not. |
| When nothing answered | If no server answered at all — connection refused, no server URL, no API key, a corrupt `.sandbin.yaml` — the client object carries `status: "error"`, `message` set to the client's own refusal sentence, and `actions` as an empty array: an action is a request the client sent to the server, so internal steps that make none (reading `.sandbin.yaml`, reusing a stored bin) are not actions, and the bin and address the client did learn still appear in the top-level `bin`/`url`. No `result: null` placeholders. | Mine, not the partner's. `message` is defined as the last response's message, and "no response" needs a stated shape rather than a missing key; keeping actions request-only is what makes a failed run's array readable as "what the server was actually asked". |
| `--json` errors are stdout-only; advisories keep stderr | With `--json`, a failure prints nothing on stderr: no `sandbin-client:` line, and no `set SANDBIN_BIN_PASSWORD` hint — stdout carries the whole failure. Advisories that are not failures keep printing on stderr, out of stdout's way: the protected-bin warning (`sandbin-client: warning: bin … has a password and SANDBIN_BIN_PASSWORD is not set`) and any later one a human needs. | The partner's ruling, sharpening my earlier "silent on stderr", which was only about failures: stdout stays pure machine-readable JSON either way, and an advisory must not vanish just because the output became machine-readable. The hint's own page keeps telling a human what a `401` means. |
| `init` has no `--json` | `init` does not take `--json`. Its output is a setup transcript for a person, and its value is the prompts and the guard question. | Mine, not the partner's. The four verbs that carry work are the machine's side of the client. |

## What this does not change

- No new status codes, and no code moved: 400, 401, 403, 404, 405, 413, 429 and 500 mean what they
  mean today, and the lockout charge, the `Retry-After` header and `WWW-Authenticate` stay as
  headers where they are headers today. No `details` field, no `retry_after` in the body.
- No HTML or file-serving response becomes JSON. A browser asking for a page gets what it gets today.
- The `ip` family over the control socket is a separate contract (`{"error": …}` over a unix socket,
  no HTTP status) and stays.
- The client's human-facing success output is unchanged unless `--json` is given.
- Roughly fifty refusal-wording assertions in `cmd/sandbin/api_test.go`, one golden index body, the
  client's relayed-line assertions and the `openapi.json` examples move because the *shape* around
  the sentence changed, not because a sentence changed. `docs_test.go`'s pin that unknown paths
  outside `/api` are a plain not-found stays green unchanged, as do the two near-miss pins
  `TestOpenAPIServeShapes` holds for `/openapi.json/` and `/openapix.json` — those paths never
  reach the API dispatcher; only that test's two `/openapi.json` 405 pins move to the envelope.

## Builder check

A fresh engineer planning from the first draft asked five questions (docs/decisions are not
changed by a check; recorded here so later readers know which rulings came from it). Four went to
the partner and are now the rulings on the `Success bodies say ok`, `--json on the client`,
`Which routes carry this` and `--json errors are stdout-only` rows. The fifth — which file-route
refusals join the envelope and whether those routes' unknown-bin 404 renames — was resolved here
and is recorded on the `Which routes carry this` row, marked as the assistant's call. One premise
in that question was checked against the code and corrected: `/openapi.json/` and `/openapix.json`
do not dispatch from the API table, so they stay plain.

## Left to the builder

- One error-writing helper for the JSON-answering routes, replacing the `http.Error` calls at those
  sites, with the plain-text `http.Error` sites outside them untouched — including the shared
  `requireKey`, `binAuthorized` and lockout helpers, which serve both halves and so have to answer
  one way or the other per call site.
- How the client threads its action log through `upload`'s steps (create-or-find, each file, the
  metadata patch) and how each step's `description` sentence is chosen, keeping the sentences fixed
  and documented.
- How `apispec.yaml` and `openapi-fix.jq` describe the envelope and the new `405`s so the generated
  document is exact and its validation tests keep proving it matches the live answers.
- Which tests move one-for-one and which new tests pin the envelope, the `404` pair, the `200`
  instead of `204`, and each `--json` shape.

## Addendum — executed corrections (recorded 2026-11-27, during plan 0033)

What execution found out about the design above, kept here because the plan and reviews
argued from it:

- **The 405 documentation went by prose, because the toolchain cannot express it otherwise.**
  The row "The OpenAPI document follows" was first ruled to add path-level
  `responses: {"405": apiErrorBody}` entries to each documented API path. That shape is a
  dead end on this toolchain: path-level `responses` are an OpenAPI 3.2 Path Item field, the
  pinned `kin-openapi v0.153.0` `PathItem` struct has no `Responses` field at all, and
  `TestOpenAPIDocValidatesAsOpenAPI31` rejects the entries as `extra sibling fields:
  [responses]` whatever the document's version string says — scratch-verified at 3.1.1 and
  3.2.0. An operation is no better: `TestOpenAPISpecMatchesRouteTable` holds the document's
  operations to equal the route table exactly, so a wildcard path for the unnamed-404 surface
  would fail it. Ruling: the dispatcher's 404 pair and its 405s are described in the
  document's `info.description` prose (commit `1b7406a`), the live 405 envelope bytes stay
  pinned by `TestOpenAPIServeShapes`, `TestAPIUnknownShapes` and `TestAPIDeleteBinShapes`, and
  the planned jq-405 correction plus `TestOpenAPIDocumentsDispatcher405s` are cancelled. The
  row remains honestly true as "described in the document", and 0030 carries the amendment.
- **A bare-200 `verifyKey` acceptance stands.** The client's key check moved from `case
  http.StatusNoContent:` to `case http.StatusOK:` without parsing `{"status":"ok"}` from the
  body. That was reviewed and kept: `POST /api/key` is the only endpoint `verifyKey` calls and
  it answers 200 only for a good key, and a middlebox answering a bogus 200 is caught by the
  next call's 403, not silently trusted. Hardening was optional and was not taken.
- **The relay needed one fix the server never exercises today.** An envelope whose `message`
  is empty made `newAPIError`'s `envelopeMessage` relay the raw JSON body. Fixed inside Task 5
  (ruling R5(1)): `envelopeMessage` returns `(msg, isEnvelope)`, and an envelope with an empty
  message relays the transport's status sentence instead. Pinned by
  `TestRelayEmptyEnvelopeMessageUsesStatus`; `verifyKey` untouched.
- **`verify the API key` is a documented action no verb run emits.** The plan's `--json`
  threading put a `verify the API key` action in upload and delete reports, but only `init`
  calls `verifyKey` — the four verbs attach the key per request and make no key-check request.
  Under this decision's ruling that an action is a request the client actually sent, inventing
  the action would put a lie in the array; the ruling stands, the sentence remains in code
  (`verifyKey` records it), and the drift is recorded in the plan's closing record rather than
  papered over.
- **Two fix waves, recorded one line each.** Task 5: the non-JSON-body branch of the action
  recorder had no pin — a broken wrap would have made a `--json` run print an empty stdout —
  closed by `4f72c26` (test-only). Task 6: a working-tree revert un-landed one SKILL.md edit
  while the client test-fake still answered delete refusals with the retired plain 404, closed
  by `7dcd6bf` (the live `404 path not found` quote and folder-action wording) and `d885a3d`
  (the fake answers the envelope; the pin moved with it).
