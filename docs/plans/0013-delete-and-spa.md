# Plan: deleting files and folders, and the 404.html fallback

Status: planned. Decisions in `docs/decisions/0013-delete-and-spa.md` — they are the spec; this
plan implements them. Baseline HEAD `2b09bfa` (`cmd/sandbin` 166 top-level tests + 2 subtests,
`cmd/sandbin-client` 79 + 13 subtests, all gates green).

**Goal:** bins answer unmatched paths with their root `404.html` (GitHub Pages style, status
stays 404), and files/folders can be deleted — a new `DELETE` verb on the server, `--delete` /
`--delete-folder` flags on the client.

**Architecture:** `binRoutes` gains a DELETE dispatch to a new `deletePath` handler in
`cmd/sandbin/handlers.go` whose guard order mirrors `putFile`; `store.go` gains `DeleteFile` and
`DeleteFolder` (folder = `ListFiles` + `HasPrefix` + one transaction — no SQL LIKE tricks);
`getFile`'s not-found branch serves the root `404.html` when the bin holds one; the client gains
two repeatable `flag.Var` delete flags with the standalone-run refusals.

**Tech stack:** Go stdlib only; no new dependency; no schema change, no migration.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Do not touch: `control.go`, `ipcmd.go`, `ipblock.go`, `archive.go`, ansi files, `Containerfile`,
  `.github/workflows/ci.yml`, `skill.go`, `skills/` files (except Task 5's SKILL.md edit),
  deploy/. `go.mod`/`go.sum` unchanged.
- Existing routes and responses keep answering exactly as before, including every 405/404/400
  method discipline; the trailing-slash 400 rule stays for every method except the DELETE
  handler's own folder marker.
- DELETE is key-checked with the exact charging semantics of `POST /`/`PUT`/`POST /verify-key`
  (blocked 429 + charge, wrong key 403 + `RecordFailure`, protected-bin password 401 uncharged).
- No real host, key or bin appears in the front page addition or SKILL.md addition.

## Review Focus

- Charging is byte-honest: a wrong-key DELETE charges exactly one failure to the right address
  (XFF through the trusted loopback socket), a 404-after-good-key charges nothing, a wrong bin
  password charges nothing.
- The fallback never shadows anything: real files win, the bin base (index.html-or-listing) is
  untouched, `GET /<hash>/404.html` exactly is a 200 file serve, only the ROOT 404.html counts,
  status stays 404, and only GET falls back.
- `DELETE hello/` never eats `helloworld/x` or `hello` (a file of that exact name); folder delete
  is all-or-nothing; an emptied bin survives with its listing and metadata.
- The client's delete run is standalone and loud about every disallowed combination.
- The README/SKILL.md delete and fallback examples run verbatim (host adapted only).

## Task 1: store delete primitives

**Files:** modify `cmd/sandbin/store.go`, `cmd/sandbin/store_test.go`

**Interfaces:**
- Produces: `func (s *store) DeleteFile(hash, path string) (bool, error)` (true when a row was
  removed) and `func (s *store) DeleteFolder(hash, folder string) ([]string, error)` (returns the
  deleted bin-relative paths sorted, empty slice when nothing matched; one transaction).

- [ ] Step 1: write failing store tests: exact delete (row gone, `GET`-equivalent lookup fails,
  other files untouched), folder delete recursion (`hello/`, `hello/deep/x` both go; `helloworld/x`
  and a file named exactly `hello` survive `DeleteFolder(hello)`), empty match returns empty
  without touching anything, delete does not disturb bin rows or metadata, a second delete of the
  same path returns false/empty.
- [ ] Step 2: run them, confirm they fail.
- [ ] Step 3: implement per the decisions (folder: `ListFiles`, keep `strings.HasPrefix(path,
  folder+"/")`, delete all inside one `*sql.Tx`, return the sorted list). Match the file's existing
  error-handling and prepared-statement style.
- [ ] Step 4: store tests green; commit `feat: the store can delete files and folders from a bin`.

## Task 2: the DELETE route

**Files:** modify `cmd/sandbin/handlers.go`, `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Consumes: Task 1's store methods.
- Produces: `DELETE /{hash}/{path}` → 200 `application/json` body `{"deleted":["<path>"]}`;
  `DELETE /{hash}/{folder}/` → 200 `{"deleted":[…all removed paths, sorted]}`; `404 unknown path`
  for a missing file or empty folder match; `405` on the bin base; guard order identical to
  `putFile`.

- [ ] Step 1: write failing handler tests: file delete round trip (PUT, DELETE, GET 404, listing
  updated); folder delete round trip incl. the `helloworld/x` boundary and a file named `hello`
  surviving `DELETE /{hash}/hello/`; DELETE without slash on a FOLDER name (`DELETE /{hash}/hello`)
  is 404, never recursive; unknown path/unknown bin 404; bin base `DELETE /{hash}/` 405; root
  `DELETE /` 405; DELETE on `/healthz`, `/verify-key`, `/skills/sandbin-preview/SKILL.md` keeps
  each route's existing refusal; charging battery on the block table (via the store directly in
  tests, same style as existing charging tests): wrong key DELETE → 403 + one failure recorded;
  blocked → 429 + another failure + Retry-After; good key then unknown path → 404 with no failure
  row; protected bin no password → 401 with no failure row, wrong password → 401 uncharged,
  right password via Basic Auth (user name = hash) → 200; `X-Extract`-style irrelevance: DELETE
  with a body is refused or ignored consistently with how PUT-with-no-body siblings behave —
  choose "reject non-empty body with 400 delete takes no body" and test it.
- [ ] Step 2: run them, confirm they fail.
- [ ] Step 3: implement: `binRoutes` gains `case http.MethodDelete: s.deletePath(...)`;
  `deletePath` trims one trailing slash from `sub` (that trimmed form is the folder marker),
  runs `cleanPath` on the remainder (existing 400 refusals stand), then mirrors `putFile`'s
  guard order (block check + charge, key check + charge, GetBin 404, `binAuthorized`), then calls
  `DeleteFile` or `DeleteFolder` and answers the JSON body (`encoding/json`, same style as the
  PATCH answer) or 404 `unknown path`; empty-body check first.
- [ ] Step 4: `go test -count=1 ./cmd/sandbin` green; commit `feat: DELETE removes files and folders from a bin`.

## Task 3: the 404.html fallback

**Files:** modify `cmd/sandbin/handlers.go` (`getFile`), `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Produces: unmatched cleaned non-empty subpath + root `404.html` present → status 404, body =
  the file's bytes, `Content-Type: text/html; charset=utf-8`; absent → today's plain 404.

- [ ] Step 1: write failing tests: PUT `hello.txt` 404 (plain text `unknown path`) vs PUT
  `404.html` then GET `missing` → 404 status with body byte-equal to the stored 404.html and
  the html content type; `GET /{hash}/404.html` exactly → 200; a real file `app/route.js` wins
  over the fallback; nested `hello/404.html` present but no root one → plain 404; bin base with
  no index.html but a 404.html still answers the LISTING (base untouched); protected bin: no
  password → 401 before any fallback bytes; `PUT`/`PATCH` to an unmatched path never fall back;
  the fallback body for a `.ansi`-styled 404.html — no ANSI rendering (name is .html, served raw).
- [ ] Step 2: confirm they fail.
- [ ] Step 3: implement in `getFile`'s `!ok` branch (the `sub != ""` one): look up `GetFile(hash,
  "404.html")`; serve its bytes with 404 + html type when found, else the existing 404 answer.
- [ ] Step 4: tests green; commit `feat: a bin with 404.html answers unmatched paths with it`.

## Task 4: client delete flags

**Files:** modify `cmd/sandbin-client/main.go` (and `config.go` if bin resolution needs a hook),
`cmd/sandbin-client/*_test.go`

**Interfaces:**
- Consumes: Task 2's DELETE API.
- Produces: `--delete PATH` and `--delete-folder NAME`, both repeatable in flag order; a
  delete run prints `deleted <url>` per path (folder: one line per deleted file) after any
  metadata answer, makes exactly one request per delete, and refuses every upload shape.

- [ ] Step 1: write failing client tests (same fake-server style as the existing client tests):
  delete one file prints `deleted <base>notes.md`; `--delete a --delete b` order preserved;
  `--delete-folder hello` sends `DELETE hello/` and prints a line per path from the JSON body;
  404 from server → client error naming the path, nonzero exit, no retry (server counts requests);
  refusals each name the conflicting flag: `--delete` + file argument, `--delete` + `--name` stdin,
  `--delete` + `--archive`, `--delete` + `--new`, `--delete` + `--describe`, `--delete hello/`
  (trailing slash → message points at `--delete-folder`), local name checks reused (absolute,
  `..`, control chars refused before any request); with bin password set, DELETE carries Basic
  Auth (user name = hash); `--bin` and the `.sandbin.yaml` bin are honored; metadata flags
  combine (PATCH answer lines first, then `deleted` lines).
- [ ] Step 2: confirm they fail.
- [ ] Step 3: implement: a `stringListflag` (flag.Value) collecting repeats in order; in `run`,
  after flag parse: if any delete flag present → standalone-run validation (refuse file args,
  `--name` stdin use, `--archive`, `--new`, `--describe`), validate names with the existing local
  checks (`--delete` rejects trailing `/`), resolve the bin exactly like an upload run (file /
  `--bin`, no creation — a run with no usable bin is the existing "no bin" error), apply metadata
  flags first if present, then issue the DELETEs (`Authorization` Basic when bin password set,
  same construction as uploads), parse the JSON `deleted` array, print `deleted <url>` per path;
  on non-2xx print the server's message as existing upload failures do and exit 1. Update
  `--help` usage text (it prints on stderr) to name both flags.
- [ ] Step 4: client tests green; commit `feat: sandbin-client deletes files and folders`.

## Task 5: front page, README, SKILL.md

**Files:** modify `cmd/sandbin/handlers.go` (`renderFrontPage`), `cmd/sandbin/handlers_test.go`,
`README.md`, `skills/sandbin-preview/SKILL.md`

- [ ] Step 1: extend `TestFrontPageAtRoot` with a marker for the new clause (e.g. the exact
  sentence you add) — failing first.
- [ ] Step 2: front page: extend the existing bin paragraph with one clause: unmatched paths
  answer the bin's `404.html` when it holds one, and `DELETE` removes files and folders.
- [ ] Step 3: README: a DELETE paragraph in the API section (both forms, slash rule, JSON body,
  bin survives, 404s) + the fallback paragraph (GitHub Pages shape, status 404, GET only, base
  untouched); add DELETE to the key-checked verb list in "API key lockout"; a delete subsection
  in the client section with runnable examples (single file, folder, refusal of mixing uploads).
- [ ] Step 4: SKILL.md: one sentence in the intro about the fallback, and a short "Deleting files
  and folders" section (`--delete`, `--delete-folder`, one line per file, folder deletes
  everything under it, uploads and deletes never mix). Keep the file under ~95 lines.
- [ ] Step 5: run every touched example verbatim against a live server (host adapted only);
  commit `docs: deleting files and folders, and the 404.html fallback`.

## Verification (implementer, after Task 5)

- `gofmt -l .` empty; `go vet ./...`; `go build ./...`; `go test -count=1 ./...`;
  `go test -race -count=1 ./...`; report test counts per package before/after.
- Live server proofs on ports 18256-18258: the full DELETE battery through curl (`-X DELETE`,
  JSON bodies captured; wrong key from an `X-Forwarded-For` fake IP charged — `sandbin ip list`
  before/after; blocked 429; protected bin 401/200; `DELETE /{hash}/` 405; boundary cases), the
  fallback battery (404 status + byte-equal body, exact `404.html` 200, file wins, base listing
  untouched), a full client round trip in a scratch project (init, upload, folder, delete file +
  folder, every refusal wording, bin reuse preserved), and proof that an emptied bin still
  lists and keeps its metadata.
- Cleanup: kill every server via /proc scan (no ps/pgrep/pkill; Z leftovers ignored),
  `/tmp/opencode` empty at the end, binaries only under `/tmp/opencode`, repo tree clean apart
  from `?? .opencode/`.

## Reviewer TODO

1. Gates yourself (all five, incl. `-race`); test counts per package vs the baseline above.
2. Scope: `git diff 2b09bfa..HEAD --name-only` names only files the tasks list; `go.mod`/`go.sum`
   untouched; commit count/messages match the plan's five.
3. DELETE battery live (ports 18259+): every decision-table row, with charging proven through
   `sandbin ip list` before/after using XFF fake IPs (loopback is a trusted socket), request
   counts proven by counting server-side effects (a 404-after-good-key leaves no row), protected
   bins both directions, `DELETE hello/` vs `helloworld/` vs file-named-`hello`, emptied bin
   survives listing+metadata+PATCH, non-empty DELETE body refused with no side effect, trailing
   slash still 400/405-shaped for every OTHER method everywhere it used to be.
4. Fallback battery live: byte-identity (cmp served vs stored), status 404, exact-request 200,
   file-wins matrix, root-only rule (nested 404.html not used), base untouched (listing AND
   index.html precedence), protected bin 401-first, non-GET methods unaffected, plain 404 stands
   for bins without 404.html (byte-identical old body).
5. Client: run every documented example verbatim (host adapted) incl. all refusal messages
   word-for-word vs README/skill text, no-retry proof (server sees exactly one request per
   delete — count via a bin the client cannot have touched otherwise or via ip failure counts),
   password-protected delete, `.sandbin.yaml` reuse (bin unchanged after deletes, `--describe`
   still reads it), delete + metadata ordering proven by output order and server state.
6. Front page + README + SKILL.md: markers vs served page; SKILL.md under ~95 lines, its new
   commands run verbatim, no new real host/key/bin in it (grep); front-page diff vs the 2b09bfa
   binary shows only the new clause.
7. Byte-identity against `2b09bfa` binaries for everything this plan must not touch: healthz,
   verify-key codes, PUT/GET/PATCH round trips, listing HTML, archive extraction, ip verb family
   outputs, skill/install route bodies (except the front page's new clause and getFile's new
   branch — diff and confirm the delta is only that).
8. Container spot proof: `podman build --network=host` from HEAD, run with fresh volume
   `sandbin-0013-rev` on `-p 127.0.0.1:18262:8080`, one full delete + fallback round trip
   through the port, then remove container/volume/own images (keep the three base images and the
   pre-existing dangling a4493fb07dee); `podman ps -a` and `podman volume ls` empty after.
9. Cleanup confirmation: servers killed via /proc scan, `/tmp/opencode` empty, `git status
   --short` shows only `?? .opencode/`.

## Skipped on purpose

Bin deletion (any shape), implicit recursive delete (no slash → no folder), 200-status fallback,
fallback for non-GET, `404.ansi` rendering, nested 404.html as shell, per-path permissions,
undo/history, batch or bulk delete endpoints, and client-side folder enumeration (the server
owns the prefix listing).
