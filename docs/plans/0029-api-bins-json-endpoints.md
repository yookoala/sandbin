# JSON bin API under /api/bins — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace sandbin's header-driven write surface with a RESTful-compatibility JSON API under `/api/bins` (create, inspect, patch with password rotation, paged list, bin delete), move `verify-key`/`healthz` to `POST /api/key`/`GET /api/health`, answer JSON from file PUT, retire every `X-Bin-*` header and `X-Extract`, and carry the client, skill, deploy probes, and hosted docs in the same cut.

**Architecture:** A new `cmd/sandbin/api.go` owns the whole `/api` surface; `handlers.go` keeps bin content serving and loses the retired routes, header handling, and plain-text create/patch handlers. `store.go` grows `ListBins`, `DeleteBin`, `GetFileMeta`, and a password-aware `UpdateBinMeta`. `cmd/sandbin-client` re-points every call and gains a `list` verb. Docs and skill updates land last, re-pointing hosted-page test pins one-for-one.

**Tech stack:** Existing Go stdlib (`net/http`, `database/sql`, `encoding/json`, `golang.org/x/crypto/bcrypt`), sqlite via the current store, urfave/cli in the client. No new dependencies.

**Spec:** `docs/decisions/0029-api-bins-json-endpoints.md` — the plan argues from it; executors read both. Baseline HEAD `55cf4e5` (`cmd/sandbin` 214 tests, `cmd/sandbin-client` 107, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6, all gates green).

## Global Constraints

- Gates at every task end: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`.
- Never push; never tag. A `v0.5.0` release is the partner's call after the plan completes.
- Test counts may grow, never shrink: `cmd/sandbin` ≥ 214, `cmd/sandbin-client` ≥ 107. Tests of retired shapes are re-pointed one-for-one to the new spellings, never deleted outright.
- Error bodies stay plain text via `http.Error` with the established strings (`invalid API key`, `unknown bin`, `bin password required`, `too many bad API keys`, `method not allowed`, `unknown path`, `delete takes no body`); JSON appears only in success bodies.
- JSON field spellings, pinned for every task: bin object `{"bin","url","title","description","has_password","created_at"}` (`url` is the base path `/{hash}/`; `title`/`description` always present, possibly `""`; the password itself is never in any body); inspect adds `"files":[{"path","size","updated_at"}]`; list answers `{"bins":[…bin objects…],"next_cursor":"…"}` with `next_cursor` omitted on the last page; single-file PUT answers `{"path","size","updated_at"}`; archive PUT answers `{"created":N,"replaced":M}`; health answers `{"status":"ok"}`.
- Request rules, pinned: JSON request bodies cap at 64 KiB (`http.MaxBytesReader`); `json.NewDecoder` + `DisallowUnknownFields()` on every JSON request; a decode failure whose error mentions `unknown field` answers `400 unrecognized field in the request` (a misspelled name must fail loudly, never be ignored); any other decode failure answers `400 could not read the request`; the top-level value must be a JSON object.
- Tri-state semantics, pinned: request fields are tracked as `json.RawMessage` — absent (nil RawMessage) leaves on PATCH / defaults empty on create; explicit `null` or `""` is "given as empty", meaning clear on PATCH (password → open, title/description → empty) and empty on create. Track with `fieldValue`: `nil → ("", false)`; `null` or `""` → `("", true)`; a JSON string → its value.
- Paging, pinned: newest-first on `created_at DESC, hash DESC`; `limit` default 30, invalid → `400 invalid limit`, above 100 clamps to 100; opaque `cursor` (URL-safe base64 of `created_at + "\x1f" + hash`), malformed → `400 invalid cursor`.
- No compatibility fallbacks: `/healthz`, `/verify-key`, `X-Bin-Title`, `X-Bin-Description`, `X-Bin-Password`, `X-Extract`, `X-Extracted-Count`, and `POST /` bin creation retire in the tasks that replace them; old paths then answer the established plain 404/405.
- Hosted docs keep the established house style (one request per fenced block, option-per-line continuations, `{{ .Instance.URL }}` tokens in examples, `http://localhost:8080` only as a built-in-default fact).
- Do not touch: the docs pipeline machinery, the IP-blocking mechanism itself, landing/frontpage markup, `robots`/`skills`/`docs` route mechanics, the 301 bin-base redirect, the `404.html` fallback, the 1 MiB PUT cap, `internal/*` packages, `scripts/`, `Makefile`, `docs/decisions/`.

## Review Focus

- **Misspelled JSON fields** (`"passwprd"`, `"desc"`) must answer 400, not silently create or leave a bin unprotected — a script's typo should fail loudly. Pinned in Tasks 3 and 4.
- **Password boundaries through JSON**: 73+ bytes → `400 bin password too long` (the existing bcrypt guard's text); `""` and `null` clear on PATCH and default open on create. Pinned in Tasks 3 and 4.
- **Cursor integrity**: a malformed cursor is 400; bins created between two pages must not shift or repeat entries mid-walk. Pinned in Task 5.
- **Gate and charge parity**: every write (old and new) plus `GET /api/bins` charges the IP lockout on a wrong API key; protected-bin Basic misses 401 and charge nothing; blocked IPs still read files and `GET /api/health`. Pinned in Tasks 2–6.
- **Surface-shape rules**: unknown `/api/...` spellings and trailing-slash variants answer the plain `404 unknown bin`; `HEAD` on `/api` routes is `405` except `/api/health` (GET/HEAD allowed); `POST /` and `PATCH /{hash}/` (the retired spellings) 405. Pinned in Tasks 2 and 4.
- Controller ruling on `/api` wording (settled with the partner after Task 2 shipped): `/api` and unknown `/api` spellings stay plain `404 unknown bin` — industry practice for a sub-namespace root is 404 (Stripe-style majority); endpoint discovery is served by the hosted docs pages, not a root index. No change; the plan's pin stands.

---

### Task 1: Store groundwork

**Files:**
- Modify: `cmd/sandbin/store.go`
- Test: `cmd/sandbin/store_test.go`

**Interfaces:**
- Consumes: existing `Bin`, `FileEntry`, `GetBin`, `ListFiles`.
- Produces:
  - `type Bin struct { Hash, PasswordHash, Title, Description, CreatedAt string }` (`GetBin` now selects `created_at`; every existing consumer keeps compiling — `PasswordHash` stays out of every JSON body).
  - `func (s *Store) UpdateBinMeta(hash string, title, description, passwordHash sql.NullString) (bool, error)` — each `COALESCE`'d; a Valid `passwordHash` sets it (empty string = open), a NULL argument leaves it.
  - `func (s *Store) DeleteBin(hash string) (bool, error)` — one transaction removing the `files` rows then the `bins` row; reports whether the bin existed.
  - `type BinCursor struct { CreatedAt, Hash string }` and `func (s *Store) ListBins(limit int, cutoff *BinCursor) (bins []Bin, hasMore bool, err error)` — `ORDER BY created_at DESC, hash DESC`, `LIMIT limit+1` probe; a nil cutoff starts at the newest.
  - `func (s *Store) GetFileMeta(binHash, path string) (FileEntry, bool, error)`.

- [ ] **Step 1: Write failing tests** in `store_test.go`: `TestStoreBinCreatedAtRoundTrip` (CreateBin → GetBin carries a non-empty RFC 3339 `CreatedAt`); `TestStoreUpdateBinMetaPasswordStates` (set password → PasswordHash changes; pass `sql.NullString{Valid:true, String:""}` → cleared to `""`; pass NULL → unchanged; title/description keep today's NULL-leaves semantics); `TestStoreDeleteBinCascade` (bin with two files → `DeleteBin` true, `ListFiles` empty, `GetBin` false, second delete reports false); `TestStoreListBinsKeyset` (seed 5 bins; `ListBins(3, nil)` → newest 3, `hasMore` true; cutoff at the 3rd row's `(CreatedAt,Hash)` → next 2, `hasMore` false; equal-`created_at` rows order by hash desc and the cursor cut is strictly after the cutoff row).
- [ ] **Step 2: Run, verify failure:** `go test ./cmd/sandbin -run 'TestStore(BinCreatedAt|UpdateBinMetaPassword|DeleteBin|ListBinsKeyset)' -v` fails to compile / fails assertions.
- [ ] **Step 3: Implement** the five interface additions in `store.go`. `ListBins` SQL: `WHERE (? IS NULL OR created_at < ? OR (created_at = ? AND hash < ?)) ORDER BY created_at DESC, hash DESC LIMIT ?` with the cutoff args NULL when `cutoff == nil`.
- [ ] **Step 4: Run tests:** `go test -count=1 ./cmd/sandbin` all green (existing store tests keep passing — `GetBin` consumers only gain a field).
- [ ] **Step 5: Gates** (all five) then commit `feat: store groundwork for the JSON bin API (list, delete, password updates)`.

---

### Task 2: /api routing skeleton, POST /api/key, GET /api/health

**Files:**
- Create: `cmd/sandbin/api.go`
- Modify: `cmd/sandbin/handlers.go` (ServeHTTP: add the `/api` branch ahead of the bin subrouter; delete the `/healthz` and `/verify-key` branches; delete `verifyKey`; extract the repeated block-keyed guard shared by `createBin`/`putFile`/`deletePath`/`patchBin` into `func (s *server) requireKey(w http.ResponseWriter, r *http.Request) bool` — same behavior: block check with `Retry-After`, `RecordFailure` + 403 on key mismatch)
- Test: `cmd/sandbin/api_test.go` (new); re-point existing healthz/verify-key tests in `cmd/sandbin/handlers_test.go`
- Modify: `Containerfile` (`HEALTHCHECK` line → `http://127.0.0.1:8080/api/health`), `deploy/sandbin.container` (`HealthCmd=` same URL)

**Interfaces:**
- Consumes: `requireKey` (the exact guard the retired `verifyKey` ran), `clientIP`, `Store.IsBlocked/RecordFailure`.
- Produces (all in `api.go`, used by Tasks 3–7):
  - `func (s *server) apiRoutes(w http.ResponseWriter, r *http.Request)` — ServeHTTP calls it when the path is `/api` or starts `/api/`. Dispatch: `POST /api/key` → `apiKeyCheck`; `GET|HEAD /api/health` → `apiHealth`; every other current-method/path combination answers `404 unknown bin` (unknown path) or `405 method not allowed` (known path, wrong method — e.g. `DELETE /api/bins`, `GET /api/key`).
  - `func (s *server) requireKey(w http.ResponseWriter, r *http.Request) bool` (in `handlers.go`).
  - `func writeJSON(w http.ResponseWriter, status int, v any) error` — `Content-Type: application/json`, body + trailing newline.
  - `apiKeyCheck` answers 204 (empty body) on a good key; `apiHealth` answers 200 `{"status":"ok"}` unauthenticated, GET/HEAD only, never charges, and answers even while the IP is blocked.

- [ ] **Step 1: Write failing tests** in `api_test.go` using the existing `newTestServer` helper: `TestAPIKeyCheck` (good key → 204 with empty body; wrong key → 403 `invalid API key` AND the failure is recorded — reuse the exact lockout-walk assertions from the retired verify-key tests; blocked-IP refusal with `Retry-After` carried over); `TestAPIHealth` (200 `{"status":"ok"}`, works with no key, HEAD allowed, `POST` → 405, a blocked IP still gets 200); `TestAPIUnknownShapes` (`/api`, `/api/`, `/api/bins/`, `/api/bins/{hash}/`, `/api/keyextra` → 404 body `unknown bin`; `GET /api/key`, `DELETE /api/bins` → 405; `HEAD /api/bins` → 405); `TestOldRoutesRetired` (`GET /healthz` → 404 `unknown bin`; `POST /verify-key` → 404). Re-point in `handlers_test.go`: every assertion the old healthz/verify-key tests pinned (charging, blocking, method discipline) moves one-for-one onto `/api/health` and `/api/key` equivalents; delete nothing.
- [ ] **Step 2: Run, verify failure** (`api_test.go` fails to compile).
- [ ] **Step 3: Implement** `apiRoutes`, `apiKeyCheck`, `apiHealth`, `writeJSON` in `api.go`; wire ServeHTTP; delete the `/healthz` + `/verify-key` branches and `verifyKey`; extract `requireKey` and use it in the existing `createBin`/`putFile`/`deletePath`/`patchBin` (no behavior change); update the two probe lines in `Containerfile` and `deploy/sandbin.container`.
- [ ] **Step 4: Run tests:** `go test -count=1 ./cmd/sandbin` green; `cmd/sandbin` count ≥ 214.
- [ ] **Step 5: Gates** then commit `feat: /api routing skeleton with POST /api/key and GET /api/health`.

---

### Task 3: POST /api/bins — JSON create

**Files:**
- Modify: `cmd/sandbin/api.go`, `cmd/sandbin/handlers.go` (delete `createBin`; `POST /` now falls to the 405 default)
- Test: `cmd/sandbin/api_test.go`; re-point existing create tests in `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Consumes: `requireKey`, `writeJSON`, `Store.CreateBin`, existing `checkBinText`, `maxTitleBytes`, `maxDescriptionBytes`.
- Produces:
  - `type binRequest struct { Title, Description, Password json.RawMessage }` decoded with `json.NewDecoder(r.Body)` + `DisallowUnknownFields()` behind `http.MaxBytesReader(w, r.Body, 64<<10)`.
  - `func fieldValue(raw json.RawMessage) (value string, given bool)` — absent (nil) → `("", false)`; `null` or `""` → `("", true)` (present-as-empty); a JSON string → its value. Create treats `!given` and given-empty alike (empty); PATCH tells them apart (leave vs clear).
  - `type binJSON struct { Bin, URL, Title, Description string; HasPassword bool; CreatedAt string }` with the pinned json tags; `func binToJSON(b Bin) binJSON` (`URL = "/"+b.Hash+"/"`, `HasPassword = b.PasswordHash != ""`).
  - Create handler `apiCreateBin`: `requireKey` → decode → validate title/description via `checkBinText` → password rule (`len > 72` → `400 bin password too long`, else bcrypt-hash as today) → `CreateBin` → `201` + `Location: /api/bins/{hash}` + bin JSON.

- [ ] **Step 1: Write failing tests:** `TestAPICreateBin` (no body / `{}` → 201, open bin, JSON carries `bin`, `url`, empty title/description, `has_password:false`, RFC 3339 `created_at`, `Location` header); `TestAPICreateBinMetadata` (`{"title":"T","description":"D"}` → stored and echoed; `{"title":null}`, `{"title":""}` → empty title); `TestAPICreateBinPassword` (`{"password":"sesame"}` → `has_password:true`, stored hash is bcrypt, plain password nowhere in the body; `{"password":""}` and `{"password":null}` → open; `{"password":"<73 bytes>"}` → 400 `bin password too long`); `TestAPICreateBinRefusals` (`{"passwprd":"x"}` → 400 `unrecognized field in the request`; `not json` → 400; `"[1]"` → 400; body over 64 KiB → 400; long-title 400 keeps today's `checkBinText` messages; wrong key charges exactly like the retired create tests asserted; blocked IP → 429 + `Retry-After`). Re-point: every old `POST /` create test's assertion lands on `POST /api/bins` one-for-one; add `TestRootPostIsGone` (`POST /` → 405 `method not allowed`).
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** the request/response types, `fieldValue`, `binToJSON`, `apiCreateBin`; dispatch it from `apiRoutes`; delete `createBin` and the `X-Bin-Title`/`X-Bin-Description`/`X-Bin-Password` constants (the client stop happens in Task 8).
- [ ] **Step 4: Run tests:** green; count ≥ prior.
- [ ] **Step 5: Gates** then commit `feat: POST /api/bins creates bins from a JSON body`.

---

### Task 4: GET and PATCH /api/bins/{hash} — inspect and JSON patch

**Files:**
- Modify: `cmd/sandbin/api.go`, `cmd/sandbin/handlers.go` (delete `patchBin`; remove the `X-Bin-Title`/`X-Bin-Description` response headers from `getFile`)
- Test: `cmd/sandbin/api_test.go`; re-point existing PATCH tests in `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Consumes: `binRequest`/`fieldValue`/`binToJSON` (Task 3), `binAuthorized`, `Store.GetBin/ListFiles/UpdateBinMeta` (Task 1 signature).
- Produces:
  - `apiInspectBin`: no API key; if protected, `binAuthorized` gates it (401 `bin password required`, charges nothing); answers bin JSON + `"files":[{path,size,updated_at}]` from `ListFiles` — including for bins whose root stores an `index.html` cover.
  - `apiPatchBin`: `requireKey` → decode `binRequest` → validate → bin lookup (404 `unknown bin`) → `binAuthorized` if protected → for each field: `given` value sets it (password: empty-as-given clears to `""`, non-empty is bcrypt-hashed; `!given` leaves) → `UpdateBinMeta(hash, title, description, password)` → re-read → `200` + full bin JSON. `{}` is a 200 no-op.
  - `type binDetailJSON` = `binJSON` + `Files []fileJSON `json:"files"``; `type fileJSON struct { Path string `json:"path"`; Size int64 `json:"size"`; UpdatedAt string `json:"updated_at"` }`.

- [ ] **Step 1: Write failing tests:** `TestAPIInspectBin` (open bin, no key → 200 with `files` array sorted by path incl. a covered `index.html` bin; protected bin → 401 without Basic, 200 with `Basic {hash}:{password}`, wrong Basic → 401 and **no** failure recorded); `TestAPIPatchJSON` (title/description set-and-echo; `{"title":null}` clears title; `{"title":""}` clears title; absent leaves; `{}` → 200 unchanged row; malformed JSON → 400); `TestAPIPatchPassword` (rotation: protected bin + current Basic + `{"password":"new"}` → 200, and a following GET with the new password 200s while the old 401s; clear: `{"password":null}` or `{"password":""}` → 200 `has_password:false` and the bin is now open; 73 bytes → 400 `bin password too long`; rotation attempted with no/wrong Basic → 401, row unchanged); `TestAPIPatchShapes` (`PATCH /{hash}/` old spelling → 405; unknown bin → 404; unknown field → 400). Re-point: each old header-PATCH test lands one-for-one here. Add `TestBinPageNoMetadataHeaders`: `GET /{hash}/` body still shows title/description (from the DB) but carries no `X-Bin-Title`/`X-Bin-Description` response headers.
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** `apiInspectBin`, `apiPatchBin`, the detail types; dispatch; delete `patchBin` and the metadata response headers in `getFile`.
- [ ] **Step 4: Run tests:** green.
- [ ] **Step 5: Gates** then commit `feat: bin inspection and JSON PATCH with password rotation`.

---

### Task 5: GET /api/bins — key-gated paged listing

**Files:**
- Modify: `cmd/sandbin/api.go`
- Test: `cmd/sandbin/api_test.go`

**Interfaces:**
- Consumes: `requireKey`, `binToJSON`, `Store.ListBins`, `BinCursor`.
- Produces: `apiListBins` — `requireKey` (a wrong key charges, exactly like the other writes); query parse: `limit` (default 30; non-integer → 400 `invalid limit`; clamp to [1,100]); `cursor` (URL-safe base64 of `created_at + "\x1f" + hash`; malformed base64 or missing separator → 400 `invalid cursor`); answer `{"bins":[binJSON…]}`, `next_cursor` = encoding of the last returned row, omitted when `hasMore` is false.

- [ ] **Step 1: Write failing tests:** `TestAPIListBins` (seed bins; page 1 default size → newest-first JSON objects, `next_cursor` present; following it returns the rest with `next_cursor` omitted on the last page; `GET /api/bins` with wrong key → 403 + charged; blocked IP → 429); `TestAPIListBinsLimits` (`limit=2` pages of 2; `limit=0` → 400 `invalid limit`; `limit=500` → clamps, page ≤ 100; `limit=abc` → 400); `TestAPIListBinsCursorIntegrity` (`cursor=nonsense` → 400 `invalid cursor`; walk page 1, create a newer bin, walk page 2 via cursor → no repeats and no gaps — keyset stability); empty instance → `{"bins":[]}` with no `next_cursor`.
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** `apiListBins` + cursor encode/decode helpers (`encodeCursor(createdAT, hash string) string`, `decodeCursor(string) (BinCursor, error)`).
- [ ] **Step 4: Run tests:** green.
- [ ] **Step 5: Gates** then commit `feat: paged key-gated bin listing at GET /api/bins`.

---

### Task 6: DELETE /api/bins/{hash} — remove a bin

**Files:**
- Modify: `cmd/sandbin/api.go`
- Test: `cmd/sandbin/api_test.go`

**Interfaces:**
- Consumes: `requireKey`, `binAuthorized`, `Store.GetBin`, `Store.DeleteBin` (Task 1).
- Produces: `apiDeleteBin` — `requireKey` → bin lookup (404 `unknown bin`) → `binAuthorized` (protected bins need current Basic; 401 charges nothing) → `DeleteBin` → `204` empty body.

- [ ] **Step 1: Write failing tests:** `TestAPIDeleteBin` (open bin: key, no Basic → 204; afterwards `GET /{hash}/`, `GET /{hash}/{path}`, and `GET /api/bins/{hash}` all 404; the orphaned-empty-bin case — a bin with no files — deletes too); `TestAPIDeleteBinProtected` (no/wrong Basic → 401, bin survives; correct Basic → 204; 401 charged nothing); `TestAPIDeleteBinShapes` (`DELETE /api/bins/{hash}/` → 404; unknown hash → 404; no key → 403 charged).
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** `apiDeleteBin`; dispatch.
- [ ] **Step 4: Run tests:** green.
- [ ] **Step 5: Gates** then commit `feat: DELETE /api/bins/{hash} removes a bin and its files`.

---

### Task 7: JSON PUT answers, Location headers, ?raw=1

**Files:**
- Modify: `cmd/sandbin/handlers.go` (`putFile`, `putArchive`), `cmd/sandbin/api.go` (move `fileJSON` use — already defined)
- Test: `cmd/sandbin/handlers_test.go` re-points

**Interfaces:**
- Consumes: `Store.PutFile/PutFiles/GetFileMeta`, `fileJSON`, `writeJSON`, `detectArchive`.
- Produces: single-file `putFile` success answers `201` (new) with `Location: /{hash}/{sub}` or `200` (replaced), body `{"path","size","updated_at"}` read back via `GetFileMeta`; archive path answers `200`/`201` (`201` iff `created ≥ 1`) body `{"created":N,"replaced":M}` and **no `X-Extracted-Count` header**; the extraction opt-out is presence of the `raw` query key (`_, ok := r.URL.Query()["raw"]` — bare `?raw`, `?raw=`, `?raw=1`, any value counts) replacing the `X-Extract` header check; `maxBodyBytes` cap unchanged. (Controller correction from Task 7 review: the earlier `Get("raw") != ""` spelling contradicted the pinned `?raw=` presence tests; presence is the binding rule.)

- [ ] **Step 1: Write failing tests (re-point one-for-one + add):** every existing test asserting the text body `/{hash}/{sub}` now asserts the JSON entry with the same status rule and, on 201, the `Location` header; every `X-Extract` test re-points to `?raw=1` / `?raw=` (presence semantics) with identical keep-raw assertions, including the archive-magic-still-detects-without-raw case; archive tests assert `{"created","replaced"}` counts (mixed new+replaced entries → `201` with exact counts) and assert absence of the `X-Extracted-Count` header; add `TestPutRawQueryShapes` (`?raw=1` on a plain file is a no-op; `?raw=1` on an archive stores it raw under its own name).
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** the response changes and the query opt-out; delete the `extractHeader`/`extractedCountHeader` constants from `handlers.go`.
- [ ] **Step 4: Run tests:** green.
- [ ] **Step 5: Gates** then commit `feat: file uploads answer JSON; ?raw=1 replaces X-Extract`.

---

### Task 8: Client — JSON calls, full describe, list verb

**Files:**
- Modify: `cmd/sandbin-client/client.go`, `cmd/sandbin-client/main.go`
- Test: `cmd/sandbin-client/main_test.go` (re-point; new tests), `cmd/sandbin-client/init_test.go` (re-point the verify call)

**Interfaces:**
- Consumes: the server surface from Tasks 2–7 (tests run against a real `cmd/sandbin` server via the existing in-test server fakes, matching how current client tests drive requests).
- Produces:
  - `createBin(title, description string) (string, error)` — signature unchanged; POSTs `/api/bins` with a JSON body containing only the fields worth sending (`title`/`description` when non-empty, `password` from `c.password` when non-empty), parses the bin JSON, returns its `bin` hash (delete `binHashFromPath`).
  - `type binInfo struct { Hash, Title, Description, CreatedAt string; HasPassword bool; Files []fileInfo }`; `type fileInfo struct { Path string; Size int64; UpdatedAt string }`; `inspectBin(bin string) (binInfo, error)` replaces `describeBin` (GET `/api/bins/{bin}`, Basic when `c.password` set, no API key).
  - `listBins() ([]binInfo, error)` — GETs `/api/bins` walking `next_cursor` to the end; no API-key handling beyond what `do` already sends.
  - `verifyKey()` re-points to `POST /api/key`; success is 204 (adjust the "is this key ok?" check to accept 204; error mapping otherwise unchanged).
  - Uploads: `--no-extract` now appends `?raw=1` to each PUT URL instead of setting `X-Extract`; archive counts come from the `{"created","replaced"}` body instead of `X-Extracted-Count`; status-code rules for `created`/`updated` output lines unchanged.
  - `describe` output (full inspection, exact lines pinned): `bin: {hash}`, `title: …`, `description: …`, `password: yes|no`, `created: {created_at}`, then `files:` with `  {path}  {size} bytes  {updated_at}` lines or `files: none`.
  - New verb `list`: no arguments; prints one line per bin `  {hash}  {created_at}  {title}` (title omitted when empty), or `no bins` on stdout when the instance is empty; walks all pages.
  - Delete retired header constants from `client.go`; `SANDBIN_BIN_PASSWORD` keeps feeding both the create-time JSON password and Basic auth, as today.

- [ ] **Step 1: Write failing tests** (extend the existing fake-server test harness): `TestClientCreateJSON` (create sends `POST /api/bins`, JSON body with the expected fields, no `X-Bin-*` header set); `TestClientDescribeFull` (against a server with a titled, password-less bin holding two files: printed stdout matches the pinned lines exactly, including `password: no` and both file lines); `TestClientList` (empty instance → `no bins`; three bins → three lines newest-first, cursor walk transparent); `TestClientVerifyKey204`; re-point `--no-extract` tests to expect `?raw=1` on the wire and archive counts from the JSON body. Re-point every existing test asserting the old wire shapes (`POST /` creation body/headers, describe's metadata-header read, `X-Extract` presence, `X-Extracted-Count` read) one-for-one.
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Implement** client.go + main.go changes; register the `list` command next to `describe`; update `describe`'s usage text ("print the full inspection of the directory's bin") and the top-level usage line.
- [ ] **Step 4: Run tests:** `go test -count=1 ./cmd/sandbin-client` green, count ≥ 107.
- [ ] **Step 5: Gates** then commit `feat: sandbin-client speaks the JSON API and lists bins`.

---

### Task 9: Skill text and hosted docs

**Files:**
- Modify: `skills/sandbin-preview/SKILL.md`
- Modify: `docs/usages/sandbin-api.md` (rewrite its route sections for the new surface), `docs/usages/sandbin-serve.md`, `docs/usages/sandbin-podman.md`, `docs/usages/sandbin-client.md`, `docs/usages/sandbin-client-init.md`, `docs/usages/sandbin-client-upload.md` (retarget retired-route mentions; describe's new output; `list` verb; `/api/health` probe examples)
- Test: `cmd/sandbin/docs_test.go` (re-pin)

**Interfaces:**
- Consumes: the shipped server + client behavior from Tasks 2–8; the existing docs pipeline (unchanged machinery).
- Produces: hosted docs that describe exactly the shipped API. Pinned structure for `sandbin-api.md`'s h2 set, in order: `Conventions`; `` `/` — the instance root `` (GET only; POST now 405); `` `/api/bins` — create and list bins `` (h3 verbs `POST — create a bin`, `GET — list bins`); `` `/api/bins/{hash}` — inspect, update, delete `` (h3 verbs GET/PATCH/DELETE); `` `/api/key` — check the API key ``; `` `/api/health` — the liveness probe ``; `` `/{hash}/` — the bin base ``; `` `/{hash}/{path}` — a file in a bin `` (its archive section names `?raw=1`); `` `/{hash}/{folder}/` — a folder in a bin ``.

- [ ] **Step 1: Re-pin failing doc tests first:** update `TestDocsAPIPageIsSwaggerStyle` to the new h2 set (pin each via `githubSlugForTest` on the heading text as it does today), the `POST — create a bin` slug, a JSON-body sentence, the `?raw=1` name, and its existing no-localhost rule; add `TestDocsNoRetiredSpellings` over all nine hosted pages: no `X-Bin-`, no `X-Extract`, no `/verify-key`, no `/healthz`, no `POST /`-creates text; extend the serve/podman pins so probe examples read `http://127.0.0.1:8080/api/health`; extend the client-page pins so the upload page shows the JSON-era `describe` output lines and a `list` example, and the init page names `POST /api/key`.
- [ ] **Step 2: Run, verify failure.**
- [ ] **Step 3: Rewrite the docs** to the pinned structure, following the house style (one request per block; option-per-line continuations; `{{ .Instance.URL }}` tokens; mock outputs in their own ```text blocks). `SKILL.md`: the `describe` line grows to name the full inspection output; add a `list` usage line; `--no-extract` flag text stays (its spelling is unchanged at the CLI).
- [ ] **Step 4: Run tests:** `go test -count=1 ./cmd/sandbin` green; `cmd/sandbin` count ≥ 214; the nine "nine pages" comments stay true.
- [ ] **Step 5: Preview check** — scratch-serve per the mockup loop (scratch build, `Host: preview.yookoala.dev` + `X-Forwarded-Proto: https`): fetch `/docs/sandbin-api.html`, one client page, and `/docs/sandbin-podman.html`; confirm zero stray `{{`, the JSON examples render, `?raw=1` and `/api/health` appear; kill only the scratch process and delete scratch files.
- [ ] **Step 6: Gates** then commit `docs: hosted docs and skill describe the JSON bin API`.

---

## Final verification (after Task 9)

- Run all five gates at HEAD; record per-package counts (`go test -count=1 <pkg> -v | grep -c '^--- PASS'` for the five packages) in this file's tail as a closing commit, mirroring the established plan records.
- Scratch end-to-end on `127.0.0.1:18307`: create → upload (plain + archive + `?raw=1`) → inspect → patch (rotate then clear the password) → list (two pages) → delete bin, all with `curl` only, no custom header except `X-API-Key` and Basic; then the same walk through `sandbin-client`. Kill scratch, clean up.

## Final verification (recorded 2026-11-17)

- Gates at HEAD `74db0fb`: `gofmt -l .` empty; `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...` all green.
- Per-package test counts (top-level PASS): `cmd/sandbin` 242 (baseline 214), `cmd/sandbin-client` 111 (baseline 107), `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6. No package shrank.
- Scratch end-to-end (`127.0.0.1:18307`, curl only, custom headers limited to `X-API-Key` + Basic): JSON create 201+Location+bin object → file PUT 201+JSON entry → replace 200 → archive PUT 201 `{"created":2,"replaced":0}` → `?raw=1` keeps the zip raw under its own name → inspect lists every file incl. the archive → PATCH re-patches description → rotate password (old 401, new 200) → `{"password":null}` clears (`has_password:false`, bin open) → `POST /api/key` 204 → `GET /api/health` `{"status":"ok"}` → retired spellings 405 (`POST /`) / 404 (`/healthz`, `/verify-key`) → paged list → create+delete bins (204, then 404s) → final list `{"bins":[]}`.
- The walk's first attempt omitted `X-API-Key` on three PUTs and demonstrated the lockout live: 403 charged, then 429 with Retry-After on subsequent key-checked requests.
- Same lifecycle through `sandbin-client`: init 204-checked, `--archive` folder upload reports `extracted 2 files` from the JSON body, `describe` prints the full inspection lines, `list` prints newest-first rows (title column omitted when empty), `--no-extract` stores `archive.zip` raw (`created`). Scratch server killed via `/proc/*/comm` scan; scratch files removed.
