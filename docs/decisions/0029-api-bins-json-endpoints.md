# Design 0029: a JSON bin API under /api/bins, replacing the header-driven surface

## Intent and why

`sandbin`'s write API is built on custom headers: bins are created with `POST /` carrying
`X-Bin-Title`, `X-Bin-Description`, and `X-Bin-Password`, updated with `PATCH /{hash}/` carrying
the first two, and there is no way to list what an instance holds or to read a bin's metadata
and file listing as data. That shape is fine for the author's own `sandbin-client` but awkward
for third-party users and scripts. This design replaces the write surface with a conventional
JSON resource API under `/api/bins`, adds inspection and listing, and carries both CLI and docs
along in the same cut.

There has been no public release with users yet, so **breaking change is explicitly allowed and
no fallback, shim, or dual path is wanted anywhere**. The old surface is deleted, not deprecated.

## What the conversation settled

These are the partner's decisions:

1. **Create moves to `POST /api/bins`** (plural) with `X-API-Key` and a JSON body carrying
   `title`, `description`, and `password`; `POST /` no longer creates bins and becomes
   method-not-allowed (`GET /` keeps answering the front page).
2. **Updating title, description, and password is `PATCH /api/bins/{hash}` with partial JSON**:
   a field present sets it, a field explicitly `null` clears it, a field absent leaves it
   unchanged. This deliberately adds password rotation and clearing, which today's
   header-PATCH cannot do at all.
3. **`GET /api/bins/{hash}` inspects a bin as JSON**: its metadata plus its file listing
   (path, size, updated_at), built from data the store already keeps.
4. **`GET /api/bins` lists the instance's bins as JSON, requires `X-API-Key`, and is paged.**
5. **The client gains two subcommand behaviors**: `describe` (which today reads metadata off
   response headers and therefore must move anyway) becomes the full single-bin inspection
   backed by the JSON endpoint, and a new `list` verb pages through the instance's bins.

## How it gets made

These are design calls, mine unless the partner objects:

- **Routing is collision-safe for free.** Bin names are 8-hex-character hashes; `api` contains
  non-hex letters and is the wrong length, so no `/api/...` path can ever name a bin. The
  server routes `/api/...` ahead of the bin subrouter, exactly how it already reserves
  `healthz`, `robots.txt`, `verify-key`, `skills`, and `docs`. Unknown `/api/...` paths answer
  the same plain `404` shape as other unknown paths.
- **All `X-Bin-*` headers die.** `X-Bin-Password` currently has exactly one use (setting the
  password at creation, `createBin`); that job moves to the JSON body. Access to
  password-protected bins already uses HTTP Basic auth (`binAuthorized` on GET/PUT/DELETE/PATCH,
  401 with `WWW-Authenticate: Basic realm="sandbin"`, 72-byte bcrypt guard) and that transport
  stays untouched. After this change the only special headers left in the API are `X-API-Key`
  and `X-Extract`.
- **Auth shape per endpoint.** `POST /api/bins`, `PATCH /api/bins/{hash}`, and `GET /api/bins`
  require `X-API-Key` and run the existing guard verbatim — including charging the IP's failure
  counter and honoring active blocks, exactly like today's key-gated routes (a wrong key on
  `GET /api/bins` charges, just like `POST /verify-key`). `GET /api/bins/{hash}` is public for
  open bins — reading a bin's listing is already public through its HTML page — and
  Basic-gated for protected bins via the existing `binAuthorized`, whose failures keep charging
  nothing, as they do today. `PATCH` keeps requiring API key **and** current bin password for a
  protected bin: rotating or clearing a password requires presenting the current one.
- **Errors stay plain text.** Every error the API answers — including the new endpoints — stays
  the established short plain-text body (`invalid API key`, `unknown bin`, `bin password
  required`, …). JSON bodies are for success responses; the existing plain-404 test conventions
  keep holding.
- **Listing is keyset-paged, newest first.** Sorted on `bins.created_at` with the hash as a
  tie-break so pages stay stable while bins are created during pagination; a `limit` parameter
  with a default and a cap, and a cursor parameter plus a next-cursor field in the response.
  The exact cursor encoding, default, and cap are left to the builder. Listing summaries carry
  per-bin metadata (never file contents); exactly which summary fields beyond metadata (file
  count, total bytes) is left to the builder.
- **JSON shape.** Responses are `application/json` objects using snake_case fields. A bin object
  names its hash, its base path, its title, description, a boolean `has_password`, and its
  `created_at` (RFC 3339, the format the store already keeps). The password itself is never
  returned, only `has_password`. The inspect object adds the `files` array (`path`, `size`,
  `updated_at` — what `ListFiles` already returns). Exact field spellings are the builder's to
  pin, then documented.
- **Limits.** JSON request bodies get a small `MaxBytesReader` cap (comfortably above the title
  and description byte limits already enforced); exact value left to the builder. Validation
  reuses the existing title/description limits and bcrypt password rules; malformed JSON is a
  plain-text 400.
- **`POST /verify-key`, `GET /healthz`, `/robots.txt`, `/skills/...`, `/docs/...`, file PUT
  semantics (1 MiB cap, archive extraction, `X-Extract`), `DELETE`, the 301 bin-base redirect,
  the `404.html` fallback, and the whole IP-blocking machinery are out of scope and unchanged.**

## Consequences that must ship in the same cut

- `sandbin-client`: `describe` re-pointed at the inspect endpoint; new `list` verb (its output
  is human-readable text lines, matching the CLI's current style, not raw JSON — flag if
  wrong); client creation switched from the three headers to the JSON body; the
  `SANDBIN_BIN_PASSWORD` env var keeps its exact current role (it feeds Basic auth for
  protected bins, and the create-time password as well today — where the create password comes
  from now is the builder's to settle so the UX stays: same env var is the safe answer).
- The embedded `SKILL.md` and installer templates: anything telling an agent to create bins or
  set metadata must show the new JSON endpoints.
- Hosted usage docs: `sandbin-api.md` (swagger-style) rewritten for the new surface; the client
  pages' examples retargeted; anywhere the docs name `X-Bin-*` headers or `POST /` creation must
  follow. `sandbin-serve.md`, the landing page, and the front page itself stay put.
- Tests: creation/patch header tests are re-pointed at JSON, not silently dropped; new
  endpoints and the paging behavior get pins; the plain-404 and lockout conventions keep passing.

## Goals, non-goals, anti-goals

- Goals: third-party scripts can create, fix, inspect, and list bins with nothing but JSON and
  two well-understood auth mechanisms; zero custom bin headers.
- Non-goals: per-bin ownership or multi-key auth; renaming bins; pagination on inspect;
  returning file contents through the inspect endpoint; OAuth or bearer tokens.
- Anti-goals: any compatibility shim, deprecated-but-working header path, or dual route; a
  listing that leaks file contents or passwords; a paged listing whose results shuffle mid-walk.

## Versioning

This is a breaking API change; when the next release is cut it should carry a minor bump
(`v0.5.0`), not a patch. Cutting it is a separate decision the partner owns.
