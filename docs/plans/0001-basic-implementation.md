# Plan: basic sandbin implementation

Status: draft. Decisions recorded in `docs/decisions/0001-server-defaults.md`.

## What we build

One binary, `cmd/sandbin`, single `package main`, one sqlite file. No config file, no
web UI beyond the generated directory listing.

## Layout

```
cmd/sandbin/
  main.go      flags, open db, mux, ListenAndServe
  store.go     sqlite access: CreateBin, GetBin, PutFile, GetFile, ListFiles
  handlers.go  POST /, GET/PUT /{hash}/{path...}, auth, directory listing
  ansi.go      ANSI -> HTML
  store_test.go
  handlers_test.go
  ansi_test.go
```

One package on purpose: the whole server is ~400 lines, splitting into `internal/` buys
nothing yet.

## Schema

```sql
CREATE TABLE IF NOT EXISTS bins (
  hash TEXT PRIMARY KEY,
  password_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS files (
  bin_hash TEXT NOT NULL,
  path TEXT NOT NULL,
  content BLOB NOT NULL,
  size INTEGER NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (bin_hash, path)
);
```

`password_hash` empty string means "no password". Created with `CREATE TABLE IF NOT EXISTS`
on every start; no migration machinery.

## API

| Method | Path | Auth | Result |
|---|---|---|---|
| POST | `/` | `X-API-Key` | `201` + `text/plain` body `/abc1234/`; bin hash is 8 hex chars from `crypto/rand`, retried on collision |
| POST | `/` | plus `X-Bin-Password` | same, but bin requires Basic Auth (user = bin hash) |
| GET | `/{hash}/` | bin password if set | `index.html` if present, else `<ul>` directory listing |
| GET | `/{hash}/{path}` | bin password if set | stored bytes, `Content-Type` from extension |
| PUT | `/{hash}/{path}` | `X-API-Key` + bin password if set | `201` new / `200` replaced; body becomes the file |

Errors: `403` bad API key, `401` + `WWW-Authenticate: Basic` for bin password, `404` unknown
bin or path, `405` other methods, `413` body over 1 MiB, `400` bad path. All plain text bodies.

Content type by extension: `.html` -> `text/html; charset=utf-8`, `.ansi` -> rendered HTML,
everything else (`text/plain` for `.txt`, `.md`) -> `text/plain; charset=utf-8` unless bytes
contain NUL, then `application/octet-stream`.

## Rules the code must enforce

- Path handling: reject absolute paths, `..`, empty segments, backslashes; total length <= 256;
  segment count <= 16. `path.Clean` before the SQL lookup, never after.
- All SQL through `?` placeholders.
- API key and bin password compared with `subtle.ConstantTimeCompare`.
- Bin passwords stored as bcrypt hashes (`golang.org/x/crypto/bcrypt`), never plaintext.
- `http.MaxBytesReader` at 1 MiB per PUT.
- HTML escaping for directory listing and ANSI output.

## Milestones (one atomic commit each)

1. `feat: server skeleton with sqlite store and bin creation` — main.go, store.go, POST, store_test.go.
2. `feat: PUT and GET for bin files` — content types, handlers_test.go happy path.
3. `feat: directory listing` — GET base path when no `index.html`.
4. `feat: per-bin basic auth` — POST password, 401 on GET/PUT.
5. `feat: ANSI to HTML rendering` — ansi.go, ansi_test.go.
6. `docs: usage with curl examples` — README section.

## Implementer TODO

- [ ] `go get modernc.org/sqlite golang.org/x/crypto/bcrypt` (the only two deps).
- [ ] `store.go`: open db, create tables, `CreateBin`, `GetBin`, `PutFile`, `GetFile`, `ListFiles`
      (`SELECT path, size, updated_at FROM files WHERE bin_hash=? ORDER BY path`).
- [ ] `main.go`: flags `--api-key` (default `$SANDBIN_API_KEY`, required), `--db` (default
      `sandbin.db`), `--addr` (default `:8080`); build mux; `http.ListenAndServe`.
- [ ] `handlers.go`: `createBin`, `binRoutes` (mux subrouter per `/{hash}/`), path validation,
      API key check, Basic Auth check, PUT via `MaxBytesReader`, GET with content-type sniffing,
      `renderListing`.
- [ ] `ansi.go`: `ansiToHTML(string) string` — escape HTML, split on lines, map SGR codes
      0/1/2/3/4/7/22/23/24/27, 30-37, 39, 90-97, 40-47, 49, 38;5;n and 48;5;n to inline `style`
      on `<span>`s inside `<pre class="ansi">`.
- [ ] `store_test.go`: temp db in `t.TempDir()`; create bin, put/get file, listing, collision-free
      hash length.
- [ ] `handlers_test.go` with `httptest`: POST -> PUT -> GET round trip, directory listing,
      `403` wrong API key, `401` missing bin password, `404` unknown bin/path, `413` oversize,
      `400` path traversal (`..`).
- [ ] `ansi_test.go`: bold + fg color + reset sequences produce expected `<span>` markup.
- [ ] `gofmt -l .`, `go vet ./...`, `go test ./...` clean.
- [ ] Manual smoke: `curl -X POST -H "X-API-Key: k" localhost:8080/` then PUT a `.html`, a
      `.md`, an `.ansi` file and check the listing in a browser.

## Reviewer TODO

- [ ] Read `README.md` and this plan; confirm every README promise (POST/PUT/GET, hash path,
      optional Basic Auth, directory listing, single sqlite file) has code and a test.
- [ ] Path traversal: try `..`, `%2e%2e`, `//`, trailing slash, over-long paths, deep nesting.
- [ ] Auth: constant-time compare used; wrong key gives 403 not 401; 401 carries
      `WWW-Authenticate: Basic`; API key never logged.
- [ ] SQL: no string concatenation of user input anywhere.
- [ ] Output safety: listing and ANSI output escape `<`, `>`, `&`; a stored `index.html` is
      served raw and flagged in review as intentional.
- [ ] Limits: 1 MiB enforced with `MaxBytesReader`, 413 body correct, no unbounded read of body.
- [ ] Concurrency: sqlite writes under parallel PUTs do not error out (`busy_timeout` set).
- [ ] Tests: `go test ./...` passes; run `go test -race ./...` once; check tests assert status
      codes and bodies, not just "no error".
- [ ] Repo shape: `cmd/sandbin` as AGENTS.md requires, plan/decision docs present.

## Skipped on purpose

DELETE of a bin or file, TTL/expiry, bin rename, markdown rendering (served as plain text),
256-color beyond `38;5;n`/`48;5;n` and truecolor, TLS (put a reverse proxy in front), rate
limiting, pagination, static-file or multipart PUT, JSON API bodies. Each is one clear feature
later if a real user needs it.
