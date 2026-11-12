# Plan: bin title and description

Status: draft. Decisions in `docs/decisions/0009-bin-metadata.md`. Extends
`docs/plans/0001-basic-implementation.md` (bins) and sits beside `docs/plans/0008-ip-control-socket.md`.

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
  key guard with the same charging as `putFile`, `GetBin` 404, `binAuthorized` 401 (uncharged),
  length/control validation 400, then `UpdateBinMeta`; the answer is `200`
  `{"title":...,"description":...}` json of the resulting row. A PATCH body is ignored.
- Length caps: title 200 bytes, description 2000 bytes (UTF-8 bytes); control characters
  (< 0x20 or 0x7f) refused with `400` — belt and braces over what the transport already frames.
- `getFile` at the bin base sets the two response headers when the stored value is non-empty,
  before either the raw-index or the listing branch; sub-path GETs carry no metadata headers.
  `renderListing` puts the title in `<title>`/`<h1>` (falling back to today's `/<hash>/` when
  empty) and a non-empty description in an escaped `<p>`; with no metadata the output stays
  byte-identical to today.
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
