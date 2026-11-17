# 0030 — /api on ServeMux, and a generated OpenAPI 3.1 document

Status: agreed with the partner 2026-11-17. Binding design for plan 0030.

## What and why

`/api` dispatch (the switch in `api.go:apiRoutes`) becomes a standard `http.ServeMux`
(Go 1.22 method patterns; repo is on Go 1.26), and the instance publishes a generated
OpenAPI 3.1 document at `GET /openapi.json`. The generator is `ehabterra/apispec`
(static analysis of the mux registrations — it only sees routes a declarative mux has,
which is why the refactor comes first). The bin-content surface, static routes
(`robots.txt`, `skills/`, `docs`), and the front page stay exactly as they are.

## Empirics this design rests on (probed on this Go 1.26.10 toolchain)

- A `GET` method pattern also matches HEAD: `HEAD /x` with only `GET /x` registered
  answers 200. The pinned `HEAD → 405` rules therefore cannot be left to the mux.
- The mux's own misses are not the pinned ones: built-in 405 writes
  `Method Not Allowed\n` plus an `Allow` header; built-in 404 writes `404 page not found\n`.
- A request for a known path with a trailing slash (`/api/bins/deadbeef/`) matches
  nothing and produces no 301.
- `ServeMux.Handler(r)` (Go 1.26) reports the matching pattern and returns an empty
  pattern for misses.

## The dispatch rule (binding)

Registration goes through one helper that does two things at once: it registers the
method pattern on the mux AND records the method into an authoritative route table
(`path → allowed methods`, the per-bin spelling keyed by the existing `apiBinPathRE`
hex gate). Dispatch classifies before delegating:

1. Path not in the table → the plain `404 unknown bin`, exactly today (including
   non-hex segments and trailing-slash spellings under `/api/bins/`, and these stay
   uncharged — a wrong key must not charge a spelling the dispatch never names).
2. Path in the table + method in its set → delegate to the mux (the mux picks the
   handler; only here).
3. Path in the table + method not in its set → the plain `405 method not allowed`.

Consequences: the mux's built-in 404/405 are never reached; HEAD is only ever
delegated where the table lists it (`/api/health`); hex-gating moves into the
per-bin registration so `{hash}` wildcards cannot broaden matching beyond 8-hex
spellings.

## Key-checked surface as a wrapper

Key-checked endpoints register through `s.apiKey(next)` — a thin middleware calling
today's `requireKey` unchanged. No endpoint changes its charging, status, or body.
The wrapper shape is what `apispec` follows to attach security requirements; the
decision pins its `securityMappings` config to map this wrapper to

```yaml
components:
  securitySchemes:
    apiKey: { type: apiKey, in: header, name: X-API-Key }
```

`apiInspectBin`'s Basic gate (`binAuthorized`) stays handler-internal (it applies
only to protected bins); its `security` entry in the document is set by an override
as optional Basic (`security: [{}, {binBasic: []}]` with
`binBasic: { type: http, scheme: basic }`).

## The published document

- Source of truth: `apispec` run over the module (installed by `go install
  github.com/ehabterra/apispec/cmd/apispec@latest`, driven by a Makefile target),
  with checked-in `securityMappings` + `overrides` config so plain-text error bodies,
  summaries, and the optional-Basic entry are exact.
- Artifact: generated `cmd/sandbin/openapi.json`, committed, embedded with
  `go:embed`, served by `GET /openapi.json` — unauthenticated, key-free, GET only
  (HEAD is the plain 405), `Content-Type: application/json`, byte-for-byte the
  committed file. It never charges and answers even while an address is blocked.
- OpenAPI version: 3.1. The document covers the `/api` JSON surface + `/openapi.json`
  itself; bin-content paths are explicitly out of document scope (arbitrary-depth
  paths are not describable in conformant OpenAPI).

## Honesty pins (test-only dependency: kin-openapi)

- `kin-openapi` enters `go.mod` imported only by `_test` files: it validates the
  committed document (`openapi3.Loader` + `Validate` on 3.1) and powers the
  cross-check: every method×path in the spec's `paths` matches the authoritative
  route table and vice versa, so doc and dispatch can never drift silently.
- A golden test asserts the served `/openapi.json` bytes equal the committed file.
- Representative success bodies (create, inspect, list, PUT entry, archive counts)
  are schema-validated against the spec's schemas.

## Invariants

- No pinned route-shape test is re-pointed or weakened: the refactor's acceptance
  criterion is the existing suite green with zero assertion edits (docs pins move
  only where the new endpoint adds pages/sections).
- Test counts grow, never shrink; the five gates stay green at every commit.
- Charging semantics are untouched everywhere; charging rules documented in 0029
  docs stay true.
