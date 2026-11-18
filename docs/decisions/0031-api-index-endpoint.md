# The bare /api answers a JSON index of the published API — Decision

Decided 2026-11-18 with the partner, after the format was refined in conversation
(2026-11-17/18). This decision deliberately changes a pinned surface: today the
bare `GET /api` is the plain 404 `unknown bin`; from here it answers the index.
That is a partner ruling, not an executor discretion, and the affected pins move
one-for-one.

## Why

Plan 0030 published the `/api` surface on a ServeMux behind an authoritative
route table and gave it a machine-readable document at `GET /openapi.json`. The
bare `/api` was still a dead 404. A visitor who finds it deserves a live map:
every published endpoint, what it does, and the document's URL — all derived
from the same table the dispatch itself runs on, so the map can never disagree
with the server.

## The index

`GET /api` (and `HEAD /api`, which answers 200 with no body) returns
`Content-Type: application/json`:

```json
{
  "version": "v0.4.4",
  "endpoints": [
    {
      "url": "http://127.0.0.1:8080/api/key",
      "methods": [
        {"method": "POST", "description": "Checks an API key without touching a bin: a good key answers 204, a wrong key costs the requesting address a lockout step."}
      ]
    },
    {
      "url": "http://127.0.0.1:8080/api/health",
      "methods": [
        {"method": "GET",  "description": "Unauthenticated liveness probe; answers 200 even while an address is blocked."},
        {"method": "HEAD", "description": "The same probe with no body."}
      ]
    },
    {
      "url": "http://127.0.0.1:8080/api/bins",
      "methods": [
        {"method": "GET",  "description": "Lists bins with cursor paging; a wrong key costs the address a lockout step."},
        {"method": "POST", "description": "Creates a bin from a JSON body; 201 answers with the bin object and a Location header."}
      ]
    },
    {
      "url": "http://127.0.0.1:8080/api/bins/{hash}",
      "methods": [
        {"method": "GET",   "description": "Inspects one bin; a password-protected bin needs the bin password."},
        {"method": "PATCH", "description": "Updates title, description, or password; a wrong key costs the address a lockout step."},
        {"method": "DELETE","description": "Deletes the bin; the request takes no body."}
      ]
    }
  ],
  "openapi": {
    "url": "http://127.0.0.1:8080/openapi.json",
    "description": "The OpenAPI 3.1 document describing this whole surface."
  }
}
```

- **`url` is the identity, and it is a full URL.** No relative `path` twin (partner
  ruling): the URL as the visitor can fetch it is the permanent thing. The
  per-bin entry's URL is a template — the literal `{hash}` placeholder stays, the
  same spelling the OpenAPI document uses.
- **Descriptions are per method.** One sentence per published pattern, pinned
  verbatim by tests.
- **`openapi` is a sibling key** pointing at the document — not one row in the
  array. A hosted-docs pointer was considered and dropped (partner ruling).
- **`version`** is the stamped `internal/version.Value`.
- **The index does not list itself.** The index IS `/api`; listing its own entry
  would add nothing. The route-table anti-drift test treats this as the single
  sanctioned exception.

## Dispatch

- Two new published patterns: `"GET /api"` and `"HEAD /api"`, registered
  unwrapped — no `withAPIKey`, like health and the document.
- The classification is untouched: `POST`/`PUT`/`PATCH`/`DELETE` on `/api` is the
  plain 405 `method not allowed` (path named, method not in the set); `/api/`
  and every other spelling stay the plain 404 `unknown bin`, uncharged whatever
  the key. The hex gate and the brace guard stand; the mux's built-in 404/405/301
  stay unreachable (the `/api/` → `/api` 301 the mux would love to send is cut
  off by the classification, exactly like every other trailing-slash spelling).
- Charging: the index charges nothing and answers even while the requesting
  address is blocked — the health/document family.

## The full URLs come from the request

Same rules as the front page's `{{ .Instance.URL }}`: host from `r.Host`, or the
rightmost `X-Forwarded-Host` entry when the socket is trusted (loopback or a
configured trusted proxy); scheme `https` only for a TLS socket or a trusted
`X-Forwarded-Proto: https`; any Host failing `frontHostRE` falls back to the
`https://this.host` placeholder. `frontBase(r)` is reused unchanged — nothing is
hardcoded; a direct local hit renders `http://127.0.0.1:8080/…` and a proxied
public hit renders `https://your.domain/…`.

## Generation and anti-drift

- A descriptions map keyed by the exact pattern strings (`"POST /api/key"` →
  its description) must cover every table pattern exactly, except the two index
  patterns themselves.
- The index is built from the route table: a fixed path enumeration in the order
  the format above publishes them (`/api/key`, `/api/health`, `/api/bins`,
  `/api/bins/{hash}`), each path's methods in the table's canonical order, with
  the `openapi` sibling taken from the table's `/openapi.json` entry.
- Pins: the enumeration equals the table's key set (minus `/api`) one-for-one;
  the descriptions map equals the table's patterns one-for-one; the golden body
  is pinned in named request contexts (direct host, trusted
  `X-Forwarded-Host`/`X-Forwarded-Proto`, and the `frontHostRE` fallback). A
  route added to the table without joining both lists fails the suite.

## Owner-approved pin moves

- `TestAPIUnknownShapes` drops `"/api"` from its unknown-spelling loop — the only
  test asserting the old bare-`/api` 404. The other spellings in the loop stand.
- `TestAPIRouteTableIsPublishedSurface` gains `"/api": [GET HEAD]` (additive).
- Every other 404/405/charging pin of plans 0029–0030 stands untouched. Nothing
  is deleted; the moves are one-for-one and trace to this decision.

## OpenAPI document

`make openapi` regenerates the document, which now describes `GET /api` and
`HEAD /api`: both carry `security: []`, contract-level summaries and
operationIds (`apiIndex`, `headAPIIndex`), and `GET /api`'s 200 carries a
component schema describing the index body — the honesty pins then validate the
live index body against the document itself. The cross-check equality test now
pins six table keys — four endpoint paths, `/api` itself, and `/openapi.json`.

## Declined for now

A hosted Swagger-UI/API-testing console was considered and declined (partner
ruling 2026-11-18): the document covers only the JSON subset of the surface; a
vendored multi-megabyte JS bundle conflicts with the self-contained culture; and
a browser "Try it" panel charges wrong-key lockout steps against its visitor. If
a tester is ever wanted, the repo-shaped form is a hand-rolled page over the
six JSON endpoints, as its own decision.

## Addendum — executed corrections (recorded 2026-11-18, during plan 0031)

What execution found out about the design above, kept here because the plan and reviews
argued from it:

- `apispec` v0.5.11 reached the index DTOs after all: joining `apiIndex` to the
  `writeJSON` `responsePattern` config made the generator emit the four components
  (`apiIndexDoc`, `apiIndexEndpoint`, `apiIndexMethod`, `apiIndexLink`) with required lists
  that match the un-`omitempty` json tags exactly, so `GET /api`'s 200 `$ref`s a generated
  schema. Only `additionalProperties: false` still needs the jq filter — that config family
  is verified inert on v0.5.11 (the RawMessage precedent).
- A HEAD operation registered through a handler that writes bodies gets apispec's body
  schema misattributed onto its 200; filter correction 16 strips it from `HEAD /api`, and a
  pin in `TestOpenAPIBodySchemasMatchLiveAnswers` now fails if the committed document ever
  gives that 200 a content map again. The same pass gave `HEAD /api` its own summary
  ("Index of this API — headers only.") because the generator copies the GET's.
- `httptest.NewRequest`'s default socket is `192.0.2.1:1234` (TEST-NET, untrusted by
  `frontBase`'s rule); tests exercising forwarded-host contexts must pin a loopback
  `RemoteAddr`, as `handlers_test.go` already does (ruling P3).
