# The HTTP API

Sandbin's HTTP surface is one server and a handful of routes. Bins are created, inspected,
edited, listed and removed through the JSON API under `/api/`, and their pages and file bytes
live under paths built from the bin's hash. This page documents the API, grouped by path first
— each path is a section — and by method under each path, each with its purpose, the headers
and bodies it reads, what it answers, and an example; where one method serves several usages,
those split into subheadings. The examples are rendered for **this instance**: every address
below is this server's own address, so the requests can be copied and run as they stand. The
non-API routes — the hosted docs under `/docs` and the `robots.txt` file — are answered by the
same server and belong to running it: see [the serve page](sandbin-serve.md).

## Conventions

**Key-checked requests.** `POST /api/bins`, `GET /api/bins`, `PATCH /api/bins/{hash}`,
`DELETE /api/bins/{hash}`, `POST /api/key`, and file `PUT` and `DELETE` take the API key in
`X-API-Key` — the API's only custom header. A wrong key answers `403 invalid API key` and
charges a lockout failure for the address (see [IP Address Blocking](sandbin-ip-blocking.md)).
`GET` never needs a key and never charges — except the listing `GET /api/bins`, which is a
key-checked read. In the examples the key is always written `$API_KEY`: export it once before
running them, with the key this instance was configured with — `export API_KEY=your-key`.

**JSON in, JSON out.** Every `/api/` request that carries data carries it as one JSON object,
and every success under `/api/` answers a JSON object with snake_case fields; refusals stay
short plain text (`invalid API key`, `unknown bin`, `bin password required`). A JSON body is
capped at 64 KiB, and malformed JSON is a `400`. The bin object names the bin's `bin` hash,
its base path `url`, its `title` and `description`, whether it `has_password`, and its
`created_at` (RFC 3339); a bin password is never in any answer.

**Protected bins.** A bin created with a `password` in its create body requires that password
as Basic Auth — user name the bin's hash, e.g. `curl -u "abc1234:sesame"` — on every `GET`,
`PUT` and `DELETE` under it and on the `GET`, `PATCH` and `DELETE` of its `/api/bins/{hash}`
route. A missing or wrong password is a `401` that charges nothing. No bin is ever protected
by an empty password: an empty `password`, given or not, leaves the bin open.

**Bin paths.** The create answer's `url` field already ends in `/`, so append the file name
directly to it (`{{ .Instance.URL }}/abc1234/notes.md`) and never double the slash: a `//`
path is a `400`.

**Status codes.** `200` and `201` answer success (`201` when something new was created);
`204` answers a success with nothing to report; `301` is the bin-base redirect; `400` a
rejected path or metadata value; `401` a missing or wrong bin password; `403` a bad API key;
`404` an unknown bin or path; `405` a method the route does not answer; `413` an oversized
body; `429` an address blocked for bad API keys. `HEAD` is a `405` everywhere except
`/api/health` and `/robots.txt`; to read a `GET`'s headers instead, use `curl -s -D - -o
/dev/null <url>`.

The examples below chain: first create a public bin — its hash comes out as `abc1234` — and a
protected one — `def5678`; every later example works against those, apart from the bin
`DELETE`, which makes its own throwaway bin.

## `/` — the instance root

The root answers `GET` alone: the branded landing page, which is not part of this API
([the serve page](sandbin-serve.md) covers it). Creating a bin lives at `POST /api/bins`
below, and every method the root does not answer — `POST` included — is a `405`.

## `/api/bins` — create and list bins

The collection answers `POST` and `GET`, both key-checked; every other method against it —
`HEAD` and `DELETE` included — answers `405`, and the trailing-slash spelling `/api/bins/` is
the plain `404`.

### `POST` — create a bin

Creates a new bin from a JSON body naming any or all of `title`, `description` and `password`,
and answers `201` with the new bin's JSON object plus a `Location` naming it. Key-checked: a
wrong key is `403` and a lockout failure.

| Field | Purpose |
| --- | --- |
| `title` | optional one-line title stored with the bin (at most 200 bytes, no tab, newline or control character) |
| `description` | optional description (at most 2000 bytes, same character rule) |
| `password` | optional; protects the bin — every read and write under it then needs this password as Basic Auth (at most 72 bytes) |

An absent field, a `null` and an `""` all make the empty value, so a request with no body at
all is a plain bin with nothing to say.

```sh
curl -s \
  -X POST \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/api/bins
```

It answers `201` with the new bin's object — here hash `abc1234` — and `Location:
/api/bins/abc1234`:

```text
{"bin":"abc1234","url":"/abc1234/","title":"","description":"","has_password":false,"created_at":"2026-11-17T09:14:02Z"}
```

```sh
curl -s \
  -X POST \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"Button grid","description":"A grid of pressable buttons","password":"sesame"}' \
  {{ .Instance.URL }}/api/bins
```

This one answers `201` with the object of a protected bin — here hash `def5678`:

```text
{"bin":"def5678","url":"/def5678/","title":"Button grid","description":"A grid of pressable buttons","has_password":true,"created_at":"2026-11-17T09:14:03Z"}
```

An over-long or control-character-bearing title or description, or a password over 72 bytes,
is a `400` and creates nothing.

### `GET` — list bins

Lists the instance's bins newest-first as JSON. Key-checked — a wrong key charges exactly like
a write — because the listing is the owner's view: every bin on the instance, protected ones
included, is listed with its full metadata in plain view.

| Parameter | Purpose |
| --- | --- |
| `limit` | page size: default 30, capped at 100; a non-integer or a limit below `1` is `400 invalid limit` |
| `cursor` | the `next_cursor` a previous page answered; a malformed one is `400 invalid cursor` |

```sh
curl -s \
  -H "X-API-Key: $API_KEY" \
  "{{ .Instance.URL }}/api/bins?limit=1"
```

It answers `200` with the page of bin objects and, because an older bin follows, the
`next_cursor` naming the walk's position after the page's last one:

```text
{"bins":[{"bin":"def5678","url":"/def5678/","title":"Button grid","description":"A grid of pressable buttons","has_password":true,"created_at":"2026-11-17T09:14:03Z"}],"next_cursor":"MjAyNi0xMS0xN1QwOToxNDowM1ofZGVmNTY3OA"}
```

Pass that value back as `?cursor=` to read the next page:

```sh
curl -s \
  -H "X-API-Key: $API_KEY" \
  "{{ .Instance.URL }}/api/bins?limit=1&cursor=MjAyNi0xMS0xN1QwOToxNDowM1ofZGVmNTY3OA"
```

```text
{"bins":[{"bin":"abc1234","url":"/abc1234/","title":"","description":"","has_password":false,"created_at":"2026-11-17T09:14:02Z"}]}
```

An answer with no `next_cursor` is the end of the walk. A cursor is the unpadded URL-safe
base64 of the last row's `created_at` and hash, so pages stay stable while bins are created
mid-walk.

## `/api/bins/{hash}` — inspect, update, delete

The per-bin route answers `GET`, `PATCH` and `DELETE`. An unknown hash is the plain `404`, the
trailing-slash spelling `/api/bins/abc1234/` is the plain `404` whatever the method, and `HEAD`
is a `405`.

### `GET` — inspect the bin

Answers the bin's object plus its full file listing — each file's `path`, `size` and
`updated_at`, sorted by path — as JSON. Needs no key on a public bin, because an open bin's
words and paths are public; a protected bin needs its Basic Auth, and that `401` charges
nothing. The listing covers every open bin, even one whose base is covered by a stored
`index.html`: the HTML listing hides the file list, the inspect answer never does.

```sh
curl {{ .Instance.URL }}/api/bins/abc1234
```

It answers `200` with the bin object and its `files` array — empty for the freshly created
bin; run it again after the uploads below and it names each landed file:

```text
{"bin":"abc1234","url":"/abc1234/","title":"","description":"","has_password":false,"created_at":"2026-11-17T09:14:02Z","files":[]}
```

```sh
curl -u "def5678:sesame" {{ .Instance.URL }}/api/bins/def5678
```

It answers `200` for the protected bin with its password given; without it the answer is
`401 bin password required`.

### `PATCH` — edit title, description, and password

Edits the stored metadata and the bin's password, and answers `200` with the bin object as it
now reads. Key-checked: a wrong API key costs a lockout failure; on a protected bin a wrong
bin password is a `401` that charges nothing.

`PATCH` merges. A field present in the body replaces that field — a field given as `null` or
`""` clears it — and a field absent from the body leaves it as it was, so one request can set,
keep or drop any of the three. `{}` answers `200` with the unchanged bin. The same length and
character rules as at create apply; a rejected value is a `400` and leaves the bin exactly as
it was. A protected bin carries its current password as Basic Auth on every change — rotating
or clearing its password requires presenting the current one.

```sh
curl -s \
  -X PATCH \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -u "def5678:sesame" \
  -d '{"description":"A grid, keyboard, and pointer"}' \
  {{ .Instance.URL }}/api/bins/def5678
```

It answers `200` with the bin object as it now reads:

```text
{"bin":"def5678","url":"/def5678/","title":"Button grid","description":"A grid, keyboard, and pointer","has_password":true,"created_at":"2026-11-17T09:14:03Z"}
```

Rotating the password is the same route with a `password` field — the current one in Basic
Auth, the new one in the body:

```sh
curl -s \
  -X PATCH \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -u "def5678:sesame" \
  -d '{"password":"mustard"}' \
  {{ .Instance.URL }}/api/bins/def5678
```

It answers `200` with the same object — still `has_password: true`, opened by `mustard` from
then on. Clearing the password is `"password":null` (or `""`), and the answered object reads
`has_password: false`.

### `DELETE` — remove the bin

Removes a bin and every file in it. Key-checked — plus the current bin password as Basic Auth
on a protected bin — carries no body, and answers `204` with an empty body. An unknown hash is
`404`. The example makes its own throwaway bin so the ones above stay readable:

```sh
curl -s \
  -X POST \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"Throwaway"}' \
  {{ .Instance.URL }}/api/bins
```

It answers `201` with a new bin — here hash `9c0ffee0`:

```text
{"bin":"9c0ffee0","url":"/9c0ffee0/","title":"Throwaway","description":"","has_password":false,"created_at":"2026-11-17T09:14:40Z"}
```

```sh
curl -X DELETE \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/api/bins/9c0ffee0
```

It answers `204`: the bin and its files are gone, and every path under
`{{ .Instance.URL }}/9c0ffee0/` is a `404` from then on. A protected bin takes the same
`DELETE` with its current password — for `def5678`, add `-u "def5678:sesame"`. Removing files
one by one stays on the file route below.

## `/api/key` — check the API key

The key-check route answers `POST` alone and every other method `405`. Key-checked: a working
key answers `204` with an empty body; a wrong one answers `403 invalid API key` and is charged
exactly like a failed `POST /api/bins` — same lockout accounting — and nothing is created. The
check stays a `POST` because `GET` never charges: a key check on `GET` would let anyone probe
keys for free. `sandbin-client init` uses it to check a key before saving it (see [Building
and Config](sandbin-client-init.md)).

```sh
curl -s \
  -X POST \
  -o /dev/null \
  -w "%{http_code}\n" \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/api/key
```

It prints `204` for a key that works; a wrong key prints `403` and costs one lockout failure.

## `/api/health` — the liveness probe

The liveness route answers `GET` and `HEAD` and every other method `405`.

Answers `200` `application/json` with `{"status":"ok"}`. Unauthenticated on purpose for
container health checks (see [Running with podman](sandbin-podman.md)); it charges nothing and
answers even while its address is blocked, because it needs no key. It says nothing
about bins.

```sh
curl {{ .Instance.URL }}/api/health
```

It answers `{"status":"ok"}`.

## `/openapi.json` — the machine-readable API document

The contract route answers `GET` alone; `HEAD` and every other method — `POST` included —
are the plain `405`, and the trailing-slash spelling `/openapi.json/` is the plain `404`.

Answers `200` `application/json` with the OpenAPI 3.1 document: the machine-readable
description of the `/api` JSON surface — `/api/bins`, `/api/bins/{hash}`, `/api/key`,
`/api/health`, and this route itself. It needs no API key and charges nothing, so it
answers even while its address is blocked. The document is generated from the server's own
route table and committed with the source; a self-builder regenerates it with `make openapi`.

Bin-content paths — every path under `/{hash}/` — are not in the document: a path of
arbitrary depth cannot be described in conformant OpenAPI, so the sections below document
that surface by hand.

```sh
curl -s {{ .Instance.URL }}/openapi.json
```

It answers the whole document — here its opening and the paths it names, the rest elided:

```text
{
  "openapi": "3.1.1",
  "info": {
    "title": "sandbin HTTP API",
    "version": "1.0.0"
  },
  "paths": {
    "/api/bins": { … },
    "/api/bins/{hash}": { … },
    "/api/key": { … },
    "/api/health": { … },
    "/openapi.json": { … }
  }
}
```

## `/{hash}/` — the bin base

The bin base answers `GET`. An unknown hash is `404`; every other method against the base
without a path — `PUT`, `DELETE` and `PATCH` included — answers `405`; the bin's metadata is
edited at `PATCH /api/bins/{hash}` above.

### `GET` — the cover page or listing

The bin base serves its `index.html` byte for byte when the bin holds one; otherwise it
answers a directory listing page. Needs no key on a public bin; a protected bin needs its
Basic Auth.

```sh
curl {{ .Instance.URL }}/abc1234/
```

It answers `200` with the public bin's `index.html` or, lacking one, its directory listing.

The bin's stored title and description show on that listing — a `<p class="bin-meta">` line
above the file list — and in the JSON inspect answer above. Metadata reaches a reader through
the listing body and the JSON only: a raw `index.html` cover is untouched by it. A public
bin's metadata is public: anyone who can read the bin can read what it says it holds.

A `GET` of the bin base without its trailing slash is a `301` redirect to the slashed path,
so relative links inside the bin resolve:

```sh
curl -s \
  -o /dev/null \
  -w "%{http_code} %{redirect_url}\n" \
  {{ .Instance.URL }}/abc1234
```

It prints `301` and the redirect target `{{ .Instance.URL }}/abc1234/`.

A protected bin's base takes its password as Basic Auth, user name the hash:

```sh
curl -u "def5678:sesame" {{ .Instance.URL }}/def5678/
```

It answers `200` with the protected bin's cover or listing.

## `/{hash}/{path}` — a file in a bin

Under the bin base, each stored file lives at its own path, answered by `GET`, `PUT` and
`DELETE`.

### `GET` — read a file

Serves the stored bytes: `text/plain` for text shapes, rendered `text/html` for a `.ansi`
file, and `text/html` for the bin's own `404.html` treatment described below. An unknown
path is `404 unknown path`. Needs no key on a public bin.

```sh
curl {{ .Instance.URL }}/abc1234/notes.md
```

It answers `200` `text/plain` with the stored bytes.

```sh
curl {{ .Instance.URL }}/abc1234/build.ansi
```

It answers `200` `text/html` with the ANSI rendered.

A bin whose files include a `404.html` answers every unmatched path under it with that file
and a `404` status, which lets a single-page app serve its own error page from the bin; a
bin without one answers the plain `404 unknown path`.

### `PUT` — upload a file

Key-checked; a protected bin also needs its Basic Auth. Every `PUT` body is capped at 1 MiB —
over it is `413`. A rejected path (an escaping or malformed name) is `400`. An upload lands
as a plain file, except one named as an archive, which unpacks instead: two usages, split
below. Both answer JSON.

#### Plain uploads

Stores the body under the path and answers `201` (`200` when the path already existed) with
the file's JSON object — its `path`, `size` and `updated_at` — plus a `Location` naming the
path when the file was new.

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  -d '<h1>Hello</h1>' \
  {{ .Instance.URL }}/abc1234/index.html
```

It answers `201` with `Location: /abc1234/index.html` and the file's object:

```text
{"path":"index.html","size":13,"updated_at":"2026-11-17T09:15:00Z"}
```

From then on the bin base serves that file as its cover page.

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  -d '# Notes' \
  {{ .Instance.URL }}/abc1234/notes.md
```

It answers `201` with the object of `/abc1234/notes.md`. Repeating the same upload answers
`200` with the object of the replaced file and no `Location`.

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  --data-binary "$(make 2>&1)" \
  {{ .Instance.URL }}/abc1234/build.ansi
```

It answers `201` with the file's object.

#### Archive uploads unpack themselves

An upload whose cleaned path ends in `.zip`, `.tar`, `.tar.gz` or `.tgz` (either case) **and**
whose body starts with the matching archive magic is unpacked into the bin instead of stored
as one file. The format is taken from the body's magic rather than the extension, so a `.tgz`
holding a zip is unzipped, while an archive-looking name holding no archive magic is a
`400 not a zip, tar or tar.gz archive`. An archive body under an ordinary name stays just a
file. To keep an archive-named body raw regardless, append `?raw=1` to the upload URL — the
parameter's presence is what the server reads, so a bare `?raw`, an empty `?raw=` or any
value opts out the same way — and the bytes are stored and served unchanged.

The entries land under the upload name's parent directory: `PUT {{ .Instance.URL }}/abc1234/site/bundle.tar.gz`
with entries `index.html` and `css/app.css` creates `site/index.html` and `site/css/app.css`,
and an archive at the bin root unpacks to the root. The archive name itself is not stored and
no directory is created for it; every extracted file is an ordinary bin file from then on,
listed, served and rendered like any upload, and an `index.html` among the entries becomes
the bin's cover page.

Entry names are checked before anything is stored, on the name the archive itself wrote:

- A `.` or `..` segment anywhere, a name starting with `/`, a control character, a backslash
  (tar keeps them), or an over-long or over-deep name is refused by the same rules an
  ordinary upload name obeys. One leading `./` is stripped, because that is how `tar czf - .`
  names its files, and the entry naming the folder itself is checked and skipped.
- A regular file with no name at all is `400 archive contains an unnamed entry`. A tar
  header whose name field carries bytes past its NUL terminator is `400 archive contains an
  entry name with a NUL byte <name>`, because the archive reader would otherwise store that
  entry under the shortened name it stops at; the walk that reads those name fields reaches
  every header it can count, a size written the base-256 way included.
- Two shapes are judged by what the archive reader reads instead of by the raw bytes: an
  entry following a GNU sparse entry, whose extra blocks the walk cannot count past, and an
  entry whose name arrives in a GNU long name record. There the entry lands under that
  reader's shortened name, judged by the same rules.
- So an entry can never move a file out of the folder the archive lands in: `sub/../up.txt`
  under `site/bundle.zip` is a refusal, not `site/up.txt`, and `deep/../../evil.txt` never
  reaches the bin root. Every refusal answers `400` naming the rule (`path must not escape
  the bin` and friends) and the bin stays exactly as it was.
- Symlink, hardlink, device and fifo entries (`archive contains a symlink <path>`) and
  entries a zip claims to have encrypted (`archive contains an encrypted entry <path>`) are
  refused the same loud way, as are duplicate paths (`archive contains duplicate path
  <path>`), an archive holding no files (`archive contains no files`) and bodies that are
  broken archives (`corrupt zip archive`, `corrupt tar archive`, `corrupt tar.gz archive`).
- A GNU sparse entry — what `tar --sparse` writes — is a regular file with holes and
  extracts as one: the holes fill out to the size the entry claims, and the caps below count
  the bytes it fills out to.

Inside the unchanged 1 MiB body cap, each entry unpacks to at most 1 MiB, one archive holds
at most 1000 entries and unpacks at most 32 MiB in total — checked while streaming, so a
decompression bomb stops at its caps instead of at the server's memory: a breach is `413
extracted file over 1 MiB`, `archive has over 1000 files` or `archive unpacks over 32 MiB`,
and nothing is stored.

A successful extraction stores all entries or none, replaces the paths it names and never
deletes anything the archive did not mention. It answers `201` when at least one path was new
and `200` when every path was a replacement, with a JSON object naming the counts; the
archive name itself was never stored, so there is no `Location`.

```sh
tar czf /tmp/bundle.tgz index.html css/app.css
```

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  --data-binary @/tmp/bundle.tgz \
  {{ .Instance.URL }}/abc1234/site/bundle.tgz
```

It answers `201` with the counts of the two landed files:

```text
{"created":2,"replaced":0}
```

Re-running the same upload answers `200` with `{"created":0,"replaced":2}`.

### `DELETE` — remove a file

Removes one file. Key-checked like `PUT`, carries no body. A path the bin does not hold is
`404`.

```sh
curl -X DELETE \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/abc1234/build.ansi
```

It answers `200` `application/json` listing what was removed — here `{"deleted":["build.ansi"]}`.

## `/{hash}/{folder}/` — a folder in a bin

A path ending in a folder and a slash is the same routes over a whole subtree.

### `DELETE` — remove a folder

The trailing slash marks a folder and takes everything under it. Key-checked, carries no
body, and answers the same JSON as a file `DELETE`, listing every removed path.

```sh
curl -X DELETE \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/abc1234/drafts/
```

It answers `200` with `{"deleted":["drafts/one.md","drafts/two.md"]}`.
