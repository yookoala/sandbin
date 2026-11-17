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

## The final API surface

| Route | Auth | Behavior |
|---|---|---|
| `GET /` | none | front page (HTML) — unchanged |
| `POST /api/bins` | key | create bin from JSON `{title?, description?, password?}` → `201` + bin JSON + `Location` |
| `GET /api/bins` | key | paged, newest-first bin list (owner's full view) |
| `GET /api/bins/{hash}` | none / Basic if protected | bin metadata + file listing JSON |
| `PATCH /api/bins/{hash}` | key + Basic if protected | partial JSON: present sets, `null`/`""` clears, absent leaves → `200` + bin JSON |
| `DELETE /api/bins/{hash}` | key + Basic if protected | remove bin and all its files → `204` |
| `POST /api/key` | key | key check, charges like a create attempt → `204` / `403` |
| `GET /api/health` | none | `{"status":"ok"}` liveness probe |
| `GET /{hash}/`, `GET /{hash}/{path}` | Basic if protected | bin HTML listing and file bytes — unchanged |
| `PUT /{hash}/{path}` | key + Basic if protected | file create/replace (`?raw=1` skips archive extraction) → `200`/`201` + file JSON, `Location` on create |
| `DELETE /{hash}/{path}` | key + Basic if protected | file/folder removal → `200` + `{"deleted":[...]}` |
| `robots.txt`, `skills/`, `docs/` | none | unchanged |

Custom headers after the change: `X-API-Key` only.

## What the conversation settled

These are the partner's decisions:

1. **Create moves to `POST /api/bins`** (plural) with `X-API-Key` and a JSON body carrying
   `title`, `description`, and `password`; `POST /` no longer creates bins and becomes
   method-not-allowed (`GET /` keeps answering the front page).
2. **Updating title, description, and password is `PATCH /api/bins/{hash}` with partial JSON**:
   a field present sets it, a field explicitly `null` clears it, a field absent leaves it
   unchanged. This deliberately adds password rotation and clearing, which today's
   header-PATCH cannot do at all. Settled after the builder check: for `password`, `null` and
   the empty string `""` mean the same thing — clear the password (at create: leave the bin
   open, matching the old empty-`X-Bin-Password` behavior). No bin can ever be protected by an
   empty password.
3. **`GET /api/bins/{hash}` inspects a bin as JSON**: its metadata plus its file listing
   (path, size, updated_at), built from data the store already keeps.
4. **`GET /api/bins` lists the instance's bins as JSON, requires `X-API-Key`, and is paged.**
5. **The client gains two subcommand behaviors**: `describe` (which today reads metadata off
   response headers and therefore must move anyway) becomes the full single-bin inspection
   backed by the JSON endpoint, and a new `list` verb pages through the instance's bins.
6. **The whole API surface becomes RESTful-compatible** (settled in a second round):
   - `POST /verify-key` becomes **`POST /api/key`**, keeping the exact charging behavior (a
     wrong key charges like a create attempt; the check stays `POST` because "GET never
     charges" is the lockout-recovery doctrine and a charging GET would break it). Success
     slims from `200 "key ok"` to `204 No Content`.
   - `GET /healthz` becomes **`GET /api/health`**, answering `{"status":"ok"}`. It stays
     unauthenticated, GET/HEAD-only, and never charges — same doctrine, REST-flavored name
     and body. `Containerfile`'s `HEALTHCHECK` line and `deploy/sandbin.container`'s
     `HealthCmd` move to it in the same cut, along with the docs' probe examples.
   - The archive-extraction default stays (sniffing an archive and extracting it is genuinely
     useful for scripts and agents), but its opt-out moves from the `X-Extract` header to a
     visible **`?raw=1` query parameter**. After this change `X-API-Key` is the only custom
     header left in the entire API.
   - File `PUT` answers **JSON**: `201` (created) gains a `Location: /{hash}/{path}` header and
     both `200`/`201` carry the file entry object (`path`, `size`, `updated_at` — data the
     store already has). Archive `PUT` answers a JSON result object (created/replaced counts
     instead of today's text lines).
   - **`DELETE /api/bins/{hash}` deletes a bin and all its files** (`X-API-Key`, plus Basic
     when protected), answering `204`. This completes CRUD on `/api/bins` and closes the
     orphaned-empty-bin gap — today no endpoint or store code can remove a bin at all.
     File/folder `DELETE /{hash}/{path}` keeps its current shape, including its existing JSON
     `{"deleted":[...]}` answer.

## How it gets made

These are design calls, mine unless the partner objects:

- **Routing is collision-safe for free.** Bin names are 8-hex-character hashes; `api` contains
  non-hex letters and is the wrong length, so no `/api/...` path can ever name a bin — the same
  holds for `/api/key` and `/api/health` against any inner segment. The server routes
  `/api/...` ahead of the bin subrouter, exactly how it already reserves `robots.txt`,
  `skills`, and `docs`; `healthz` and `verify-key` retire as part of the move. Unknown
  `/api/...` paths answer the same plain `404` shape as other unknown paths.
- **All `X-Bin-*` headers die, and `X-Extract` moves to a query parameter.** `X-Bin-Password`
  currently has exactly one use (setting the password at creation, `createBin`); that job moves
  to the JSON body. Access to password-protected bins already uses HTTP Basic auth
  (`binAuthorized` on GET/PUT/DELETE/PATCH, 401 with `WWW-Authenticate: Basic realm="sandbin"`,
  72-byte bcrypt guard) and that transport stays untouched. After this change the only custom
  header left in the API is `X-API-Key` — and it stays a custom header because the
  `Authorization` slot is already occupied by bin Basic auth, which a protected-bin write must
  carry alongside the instance key.
- **Auth shape per endpoint.** `POST /api/bins`, `PATCH /api/bins/{hash}`, and `GET /api/bins`
  require `X-API-Key` and run the existing guard verbatim — including charging the IP's failure
  counter and honoring active blocks, exactly like today's key-gated routes (a wrong key on
  `GET /api/bins` charges, just like `POST /verify-key`). `GET /api/bins/{hash}` is public for
  open bins and Basic-gated for protected bins via the existing `binAuthorized`, whose failures
  keep charging nothing, as they do today. Settled after the builder check: the inspect
  endpoint lists files for **every** open bin, including bins whose root stores an
  `index.html` cover — today that cover hides the HTML directory listing, but the design
  treats the paths as not-secret for open bins. And `GET /api/bins` is the **owner's full
  view**: the API key holder sees complete metadata for every bin on the instance, protected
  ones included, with no filtering or field suppression — the key holder is the instance owner.
  `PATCH` keeps requiring API key **and** current bin password for a protected bin: rotating or
  clearing a password requires presenting the current one.
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
- **Out of scope and unchanged:** the 1 MiB PUT body cap and the archive sniffing *behavior*
  (its response shape and opt-out spelling change per settled item 6, but what extraction does
  stays), `DELETE /{hash}/{path}` file/folder semantics (its JSON answer already fits), the
  `robots.txt`, `skills`, and `docs` routes, the 301 bin-base redirect, the `404.html`
  fallback, the front page, and the whole IP-blocking machinery.

## Consequences that must ship in the same cut

- `sandbin-client`: `describe` re-pointed at the inspect endpoint and settled to print the
  **full inspection** — title, description, hash, created time, whether the bin has a password,
  and the file listing — as human-readable text lines; the new `list` verb pages through the
  instance's bins, also as human-readable text lines (not raw JSON); client creation switched
  from the three headers to the JSON body; `init`'s key check calls `POST /api/key`; uploads
  and deletes follow the new JSON response shapes and the `?raw=1` spelling. The
  `SANDBIN_BIN_PASSWORD` env var keeps its exact current role (it feeds Basic auth for
  protected bins, and the create-time password as well today — where the create password comes
  from now is the builder's to settle so the UX stays: same env var is the safe answer). A
  client-side whole-bin delete is **not** in this cut — bin removal is API-only for now; a
  client verb can come later if the partner wants one. That's my call.
- Deploy assets: `Containerfile`'s `HEALTHCHECK` line and `deploy/sandbin.container`'s
  `HealthCmd` (and the podman page's `--health-cmd` examples, which quote the unit) move from
  `/healthz` to `/api/health`.
- The embedded `SKILL.md` and installer templates: anything telling an agent to create bins,
  set metadata, or extract archives must show the new JSON endpoints and `?raw=1`.
- Hosted usage docs: `sandbin-api.md` (swagger-style) rewritten for the new surface; the client
  pages' examples retargeted, including `init`'s `POST /verify-key` mention; the serve and
  podman pages' health-probe examples follow `/api/health`; anywhere the docs name `X-Bin-*`
  headers, `X-Extract`, `POST /` creation, `/verify-key`, or `/healthz` must follow. The
  landing page and the front page itself stay put.
- Tests: creation/patch header tests are re-pointed at JSON, not silently dropped; the
  `verify-key`, `healthz`, `X-Extract`, and text-response PUT tests are re-pointed at
  `POST /api/key`, `GET /api/health`, `?raw=1`, and the JSON answers, one-for-one; new
  endpoints, paging, and bin deletion get pins; the plain-404 and lockout conventions keep
  passing.

## Goals, non-goals, anti-goals

- Goals: third-party scripts can create, fix, inspect, and list bins with nothing but JSON and
  two well-understood auth mechanisms; zero custom bin headers.
- Non-goals: per-bin ownership or multi-key auth; renaming bins; pagination on inspect;
  returning file contents through the inspect endpoint; OAuth or bearer tokens; Accept-header
  content negotiation — HTML stays at the bin paths and JSON lives under `/api/`, separated by
  path, not by request headers.
- Anti-goals: any compatibility shim, deprecated-but-working header path, or dual route; a
  listing that leaks file contents or passwords; a paged listing whose results shuffle mid-walk.

## Minor calls adopted from the builder check

The builder check proposed these and the design adopts them; veto any that read wrong:
`POST /api/bins` answers `201` with the full bin JSON object (which is why the bin object names
its base path — the client reads the hash from it); an empty or missing JSON body on create
means a plain bin with no metadata, mirroring today's header-less `POST /`; `/api/bins/` and
`/api/bins/{hash}/` with a trailing slash answer the unknown-path plain 404; `HEAD` on `/api`
routes is 405; `PATCH` with `{}` answers 200 with the unchanged bin; and the HTML bin listing
page keeps rendering title and description, read from the database rather than the retired
response headers.

## Versioning

This is a breaking API change; when the next release is cut it should carry a minor bump
(`v0.5.0`), not a patch. Cutting it is a separate decision the partner owns.
