# Plan: bin title and description

Status: implemented on `main` (`fe8116d`, `49f6c31`, `190a4e7`), reviewed as "ship with fixes";
the review's blocker was this plan's own listing wording, amended below and in
`docs/decisions/0009-bin-metadata.md`. Decisions in `docs/decisions/0009-bin-metadata.md`.
Extends `docs/plans/0001-basic-implementation.md` (bins) and sits beside
`docs/plans/0008-ip-control-socket.md`.

## What it is

Every bin gains a title and a description — single-line text an agent sets at creation and can
change later, so a bin can say what its files are for.

```
POST  /                     X-Bin-Title / X-Bin-Description optional, set at creation
PATCH /<hash>/              same headers; present replaces, absent leaves, empty clears
GET   /<hash>/              both headers in the answer when non-empty; listing shows them
sandbin-client --title t --description d [...]   set on create or on the resolved bin
sandbin-client --describe                          print the current title and description
```

## Server behaviour

- `bins` gains `title`/`description` (`TEXT NOT NULL DEFAULT ''`); `openStore` adds either column
  to a pre-existing table via `ALTER TABLE` after checking `pragma table_info(bins)`.
- `Bin` carries `Title` and `Description`; `CreateBin(passwordHash, title, description)`;
  `UpdateBinMeta(hash, title, description sql.NullString) (bool, error)` is one atomic
  `UPDATE bins SET title=COALESCE(?,title), description=COALESCE(?,description) WHERE hash=?`
  reporting whether the bin exists.
- `binRoutes` gains `case http.MethodPatch` → bin base only (`sub != ""` is 405): block guard,
  key guard with the same charging as `putFile`, length/control validation `400`, `GetBin` 404,
  `binAuthorized` 401 (uncharged), then `UpdateBinMeta`; the answer is `200`
  `{"title":...,"description":...}` json of the resulting row. A PATCH body is ignored.
- Length caps: title 200 bytes, description 2000 bytes (UTF-8 bytes); control characters
  (< 0x20 or 0x7f) refused with `400` — belt and braces over what the transport already frames.
- `getFile` at the bin base sets the two response headers when the stored value is non-empty,
  before either the raw-index or the listing branch; sub-path GETs carry no metadata headers.
  `renderListing` keeps `/<hash>/` in `<title>`/`<h1>` — the browser tab stays the bin path, the
  identifier everything else addresses bins by — and when metadata exists appends one escaped
  block above the file list, `<p class="bin-meta"><strong>title</strong><br>description</p>` with
  a one-line `<style>` that appears only together with it; with no metadata the output stays
  byte-identical to today. (Amended after review; this line originally asked for the title inside
  `<title>`/`<h1>` — see the Outcome.)
- Nothing about the control socket, the ip verbs, the lockout rules or the file routes changes.

## Client behaviour

- New flags `--title`, `--description` (validation mirrors the server's caps with client-side
  wording, before any request, like `cleanUploadName` does for names).
- A bin being created (auto, `--new`) gets the metadata in the creation `POST` headers — no
  second request. An existing bin (`--bin` or the state file) with metadata flags gets one `PATCH`
  right after `resolveBin`, before uploads; stdout gains the line `updated /<hash>/` for it.
- A run with no upload targets is currently refused; it becomes allowed when at least one
  metadata flag is given (metadata-only run), and stays refused otherwise.
- `--describe`: resolves the bin (`--bin` or this directory's state — `--new` refused), makes one
  `GET`, needs no API key (the key guard applies only to runs that write), and prints exactly
  `title: <t>` and `description: <d>`; refused together with file arguments, `--name` or the
  metadata flags. A protected bin still needs `SANDBIN_BIN_PASSWORD` and the existing warning.

## Milestones (three atomic commits)

1. `feat: bins carry a title and a description` — store schema/migration, methods, `POST`/`PATCH`
   /`GET` handlers, listing rendering, store and handler tests.
2. `feat: sandbin-client records what a bin holds` — the three flags, metadata-only runs,
   `--describe`, client tests.
3. `docs: bins can say what they hold` — README.

## Implementer TODO

- [ ] `cmd/sandbin/store.go`: schema columns; `openStore` migration (pragma check + ALTER, with a
      test opening a db built with the old schema); `Bin.Title/Description`; `CreateBin` gains the
      two parameters; `UpdateBinMeta` as above; `GetBin` selects the new columns.
- [ ] `cmd/sandbin/handlers.go`: header constants; `createBin` validates and stores; `binRoutes`
      PATCH case implementing the order above; validation helpers shared by POST and PATCH with
      wording `title over 200 bytes`, `description over 2000 bytes`, `title may not contain control
      characters` (description likewise); `getFile` base-path headers; `renderListing` title/
      description rendering, escaped, byte-identical when both empty.
- [ ] Tests in `cmd/sandbin/`: creation with/without headers; creation with an over-long title or
      description → 400 and no bin created; PATCH replace/keep/clear/both, unknown bin → 404,
      wrong key → 403 and the address charged (escalation visible), blocked → 429, wrong bin
      password → 401 uncharged, over-long → 400 with nothing changed; GET base carries headers
      when non-empty and not when empty, on both the listing and the raw-index branches; sub-path
      GET carries neither header; listing html with title+description escaped and correct, and
      byte-identical to the old renderer for a bin with no metadata; the old-db migration test;
      PATCH on a sub path → 405.
- [ ] `cmd/sandbin-client/main.go`: `--title`, `--description`, `--describe` with the rules above;
      `cleanMetadata` mirroring the server limits; `createBin` passes the headers; `patchMeta` and
      `describeBin` on `client`; metadata-only runs; `--describe` needs no key; flag combinations
      refused with clear wording. Keep every existing behaviour (key guard for write runs, stdin
      ordering, held stdout lines) intact.
- [ ] Client tests: metadata-only run against a live test server sets and re-reads; create carries
      headers (server sees them, no PATCH request made); existing bin gets exactly one PATCH before
      uploads and the `updated /<hash>/` line; over-long or control-character flag values fail
      with zero requests; `--describe` on an open bin works with no API key set and prints the two
      lines; `--describe` refused with `--new`, uploads or metadata flags; `--describe` on the
      state bin; protected bin `--describe` honours `SANDBIN_BIN_PASSWORD`.
- [ ] README (milestone 3): client examples (create with metadata, update on an existing bin,
      `--describe` without a key); the `PATCH` line and header semantics in the API description;
      the 200/2000-byte limits; metadata visible on the listing page and in headers; a stored
      index.html is still raw and its metadata is header-only.
- [ ] `gofmt -l cmd` empty, `go vet ./...` clean, `go test -count=1 ./...` and
      `go test -race -count=1 ./...` green.
- [ ] Live proof under `/tmp/opencode` (binaries there only; server on 127.0.0.1:18220; clean up
      everything after): create with `curl -H X-Bin-Title: -H X-Bin-Description:` and read back via
      GET headers; PATCH keep/replace/clear semantics with `curl -X DELETE`-style raw checks using
      `-X PATCH`; over-long title refused 400, no bin drift; wrong-key PATCH charges (list shows
      the row, escalation walks); client create/`--describe`/update round trip against the live
      server; listing html contains the escaped title and description; an old-schema db (created
      with the `3476383` binary or a raw SQL CREATE) opens with the new binary, gets the columns,
      and its old bin reads with empty metadata; migration idempotence (opening twice).
- [ ] Container spot check: build (`--network=host`), run on the CMD, `podman run --rm <image>`-
      style client run is not shipped in the image — instead prove through the published port that
      a bin created with metadata is readable and patchable; quadlet untouched; clean up.

## Reviewer TODO

- [ ] Read the diff, `docs/decisions/0009-bin-metadata.md`, and plan 0001's/0008's context.
- [ ] Migration safety is the headline: open a db written by the old binary with the new one and
      vice versa (old binary against a migrated db: create/read/put/get still work with defaults);
      open the migrated db repeatedly (idempotence); a half-migrated table (only `title` added)
      gets `description` on the next open. No data loss in any direction.
- [ ] PATCH semantics table: present/absent/empty for each header independently (four combinations
      per field), against a live server, reading back after each; PATCH on a sub path → 405; PATCH
      body ignored; response json is the resulting row.
- [ ] Charging rules unchanged and extended: wrong-key POST/PUT/PATCH all charge and escalate;
      blocked attempt on PATCH charges; wrong bin password on PATCH charges nothing (row count in
      `ip list` proves it); GET/--describe charge nothing; the control socket and ip verbs behave
      exactly as plan 0008 left them.
- [ ] Byte-identity sweep for the untouched paths: listing html for a metadata-less bin, GET/PUT/
      POST responses, and the `ip` verbs all match the `3476383` binary outputs (seed identical
      dbs) — the feature must be invisible when unused.
- [ ] Injection discipline: title/description containing `<script>`, quotes, and unicode render
      escaped in the listing; response headers hold them without splitting (values with weird
      bytes: tab is < 0x20 → refused; test what actually gets stored); a title shaped like
      `x" onload="y` cannot break the listing markup (parse the produced html with python3).
- [ ] Limits enforced in three places with consistent numbers: client refuses before a request,
      POST refuses, PATCH refuses — same caps, 400 wording matches the README; boundary values
      (exactly 200/2000 bytes, multi-byte UTF-8 counted in bytes) accepted.
- [ ] `--describe` authority: works keyless on an open bin; a protected bin 401s without the
      password and works with `SANDBIN_BIN_PASSWORD`; makes exactly one request (server log line
      count); refused with `--new`/uploads/metadata flags.
- [ ] Raw index.html shortcut intact: a bin with an index.html and metadata serves the file
      unmodified with the metadata headers present; the listing branch untouched.
- [ ] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
      `gofmt -l cmd`; count tests; name untested new arms.
- [ ] Repo shape: only `store.go`, `handlers.go`, their tests, `main.go` of the client and its
      tests, README; control.go, ipcmd.go, ipblock.go, ansi files, Containerfile, deploy/
      untouched; no new dependency.

## Skipped on purpose

Multi-line descriptions, a json bin API, per-file metadata, listing/searching metadata across all
bins, metadata on the control socket, HEAD support on bin paths (still 405), storing metadata in
the client state file, and changing anything about creation auth or bin passwords.

## Outcome

Tests went 145 to 170 top-level (123 + 2 subtests in `cmd/sandbin`, 47 in `cmd/sandbin-client`);
`gofmt`, `go vet`, `go test -count=1` and `go test -race` green at HEAD, verified again by the
review, which also confirmed zero rewritten pre-existing assertions (only `CreateBin` arity call
sites and a fakeServer harness extension).

The review verdict was **ship with fixes**, with the one blocker in the docs, not the code: this
plan and the decisions' Visibility row said the listing puts the title in `<title>`/`<h1>`, while
the shipped `renderListing` keeps `/<hash>/` there and renders metadata as the escaped
`<p class="bin-meta">` block the README describes. The review judged — and this plan now agrees —
that the docs should follow the code: the browser tab keeps the bin path, the stable identifier
everything addresses bins by, the page body still carries title and description, and the code,
tests and README already assert that shape consistently. Both docs were amended (this commit).
The implementer had justified the deviation with a quote about "a small CSS rule in `layout.go`"
— there is no `layout.go` in the repo, before or after this plan, and the decisions file contains
no such sentence: the citation was unfounded, and the rationale recorded here is the review's, not
the implementer's. Other implementer-supplied citations from this plan were spot-checked and are
unreliable except where they reproduce against the code.

Verified live by the review against a 18223 server and the `3476383`-era binary side by side:
migration both directions on real dbs (old writes → new opens: columns added, six GET shapes
byte-identical, zero metadata headers, PATCH works; new migrates → old binary POSTs, PUTs, lists
and renders ANSI unchanged with the metadata rows intact; half-migrated table completes on the
next open; four reopens leave db md5 and rows unchanged); all nine PATCH present/absent/empty
combinations with the answer equal to the stored row every time, PATCH body ignored (even over
the PUT cap), 405 on sub paths and root, 404 unknown bin; charging correct throughout (wrong-key
PATCH `403→429` with `Retry-After` 1799 then 3599, blocked POST charged, wrong bin password 401
uncharged, GET and `--describe` charge nothing); the full byte-identity sweep of untouched paths
including the whole ip verb family and the control socket, and the flagless client reproducing the
old client word for word; injection discipline (a listing parsed with `html.parser` after
`<script>alert(1)</script>` and `x" onload="y` metadata holds 0 script elements and 0 onload
attributes; unicode round-trips in headers and listing); limits enforced in all three places with
byte-exact boundaries (100×é accepted, 200×é refused; 666×漢 accepted, 667 refused) and client
refusals making zero requests against a dead port; `--describe` keyless on open bins, one request
through a counting proxy, zero side effects, protected-bin 401 with the key-hint line, all
refusals verbatim, `--title ''` clearing via flag-given semantics; every README example run
verbatim; container build, metadata create/patch/persist-through-restart on a fresh volume, and
the quadlet unit generated unchanged.

Corrections that came out of the review: this plan's PATCH guard-order wording now matches the
code (validation `400` before `GetBin` 404 — an unknown bin with an over-long title answers 400,
harmless and charging-neutral either way), and the README shows the curl empty-header syntax
`-H "X-Bin-Title;"`, because `-H "X-Bin-Title:"` silently strips the header — the clearing feature
was unusable via curl without that line.

Accepted as they are: a control character in a header can also be bounced by Go's own transport
with a bare `400 Bad Request` (DEL) before reaching our wording (a tab reaches it) — right
outcome, inconsistent text; the metadata stdout line `updated /<hash>/` is relative while upload
lines are absolute (plan-internal inconsistency, tidied wording deferred); the client's local
refusals mirror the server's wording exactly, which is deliberate; and a batch of defensive error
arms stays untested by Go (`patchBin`'s store-failure and marshal arms, the concurrent-PATCH
`!updated` 404 — not deterministically triggerable, code inspection only, the `store.go`
ALTER-failure arm, client transport-error paths) — proven by hand or accepted, as in earlier
plans. Left unproven here: quadlet unit execution (dryrun only) and a genuinely untrusted public
socket, both standing sandbox limits.
