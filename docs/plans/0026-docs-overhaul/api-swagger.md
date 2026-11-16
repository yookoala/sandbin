# The HTTP API

Sandbin's HTTP surface is one server and a handful of routes. This page documents the API,
grouped by path first — each path is a section — and by method under each path, each with its
purpose, the headers and bodies it reads, what it answers, and an example; where one method
serves several usages, those split into subheadings. The examples are rendered for **this
instance**: every address below is this server's own address, so the requests can be copied
and run as they stand. The non-API routes — the landing page `GET /`, the hosted docs under
`/docs`, and the `robots.txt` file — are answered by the same server and belong to running
it: see [the serve page](sandbin-serve.md).

## Conventions

**Key-checked writes.** `POST /`, `PUT`, `PATCH` and `DELETE` take the API key in
`X-API-Key`. A wrong key answers `403 invalid API key` and charges a lockout failure for the
address (see [IP Address Blocking](sandbin-ip-blocking.md)). `GET` never needs a key and never
charges. In the examples the key is always written `$API_KEY`: export it once before running
them, with the key this instance was configured with — `export API_KEY=your-key`.

**Protected bins.** A bin created with `X-Bin-Password` requires that password as Basic
Auth — user name the bin's hash, e.g. `curl -u "abc1234:sesame"` — on every `GET`, `PUT` and
`DELETE` under it. A missing or wrong password is a `401` that charges nothing.

**Bin paths.** The create answer already ends in `/`, so append the file name directly to
it (`{{ .Instance.URL }}/abc1234/notes.md`) and never double the slash: a `//` path is a
`400`.

**Status codes.** `200` and `201` answer success (`201` when something new was created);
`301` is the bin-base redirect; `400` a rejected path or metadata value; `401` a missing or
wrong bin password; `403` a bad API key; `404` an unknown bin or path; `405` a method the
route does not answer; `413` an oversized body; `429` an address blocked for bad API keys.
`HEAD` is a `405` everywhere except `/healthz` and `/robots.txt`; to read a `GET`'s headers
instead, use `curl -s -D - -o /dev/null <url>`.

The examples below chain: first create a public bin `/abc1234/` and a protected bin
`/def5678/`, then each later example works against those.

## `/` — the instance root

The root route is where bins are born. `POST /` creates one; `GET /` answers the branded
landing page, which is not part of this API ([the serve page](sandbin-serve.md) covers it);
every other method answers `405`.

### `POST` — create a bin

Creates a new bin and answers `201` with its path as `text/plain`. Key-checked.

| Header | Purpose |
| --- | --- |
| `X-API-Key` | required; a wrong key is `403` and a lockout failure |
| `X-Bin-Password` | optional; protects the bin — every read and write under it then needs this password as Basic Auth |
| `X-Bin-Title` | optional one-line title stored with the bin (at most 200 bytes, no tab, newline or control character) |
| `X-Bin-Description` | optional description (at most 2000 bytes, same character rule) |

```sh
curl -s \
  -X POST \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/
```

It answers `201` with the new bin's path, e.g. `/abc1234/`, as `text/plain`.

```sh
curl -s \
  -X POST \
  -H "X-API-Key: $API_KEY" \
  -H "X-Bin-Password: sesame" \
  -H "X-Bin-Title: Button grid" \
  -H "X-Bin-Description: A grid of pressable buttons" \
  {{ .Instance.URL }}/
```

This one answers `201` with `/def5678/`: a protected bin with its metadata stored.

An over-long or control-character-bearing title or description is a `400` and creates
nothing.

## `/{hash}/` — the bin base

The bin base answers `GET` and `PATCH`. An unknown hash is `404`; every other method against
the base without a path — `PUT`, `DELETE` included — answers `405`.

### `GET` — the cover page or listing

The bin base serves its `index.html` byte for byte when the bin holds one; otherwise it
answers a directory listing page. Needs no key on a public bin; a protected bin needs its
Basic Auth.

```sh
curl {{ .Instance.URL }}/abc1234/
```

It answers `200` with the public bin's `index.html` or, lacking one, its directory listing.

Every `GET` on the bin base also carries the stored metadata as `X-Bin-Title` and
`X-Bin-Description` response headers. Metadata travels on headers only: a raw `index.html`
cover is untouched by it, and only a directory listing shows it, as a `<p class="bin-meta">`
line above the file list. A public bin's metadata is public: anyone who can read the bin can
read what it says it holds.

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

### `PATCH` — edit the title and description

Edits the stored metadata and answers `200` `application/json` with the row as it now reads.
Key-checked: a wrong API key costs a lockout failure; on a protected bin a wrong bin
password is a `401` that charges nothing.

`PATCH` merges. A header that is present replaces that field; a header that is absent leaves
it as it was; a header present with an empty value clears it — so one request can set, keep
or drop either field. In curl the empty header is written `-H "X-Bin-Title;"`: `-H
"X-Bin-Title:"` strips the header from the request instead of sending it empty. The same
length and character rules as at create apply; a rejected value is a `400` and leaves the bin
exactly as it was.

```sh
curl -s \
  -X PATCH \
  -H "X-API-Key: $API_KEY" \
  -H "X-Bin-Description: A grid, keyboard, and pointer" \
  {{ .Instance.URL }}/def5678/
```

It answers `200` with the row as it now reads:
`{"title":"Button grid","description":"A grid, keyboard, and pointer"}`.

The metadata rides on every bin-base answer as response headers:

```sh
curl -s -D - -o /dev/null {{ .Instance.URL }}/def5678/
```

It prints the answer's headers, which carry the metadata: `200`, `X-Bin-Title: Button grid`
and `X-Bin-Description: A grid, keyboard, and pointer`.

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
below.

#### Plain uploads

Stores the body under the path and answers `201` (`200` when the path already existed) with
the landed path as `text/plain`.

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  -d '<h1>Hello</h1>' \
  {{ .Instance.URL }}/abc1234/index.html
```

It answers `201` with the landed path, `/abc1234/index.html`. From then on the bin base
serves that file as its cover page.

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  -d '# Notes' \
  {{ .Instance.URL }}/abc1234/notes.md
```

It answers `201` with `/abc1234/notes.md`. Repeating the same upload answers `200` with the
same path.

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  --data-binary "$(make 2>&1)" \
  {{ .Instance.URL }}/abc1234/build.ansi
```

It answers `201` with `/abc1234/build.ansi`.

#### Archive uploads unpack themselves

An upload whose cleaned path ends in `.zip`, `.tar`, `.tar.gz` or `.tgz` (either case) **and**
whose body starts with the matching archive magic is unpacked into the bin instead of stored
as one file. The format is taken from the body's magic rather than the extension, so a `.tgz`
holding a zip is unzipped, while an archive-looking name holding no archive magic is a
`400 not a zip, tar or tar.gz archive`. An archive body under an ordinary name stays just a
file. To keep an archive-named body raw regardless, send an `X-Extract` header on the `PUT` —
any value, even empty; presence is what the server reads — and the bytes are stored and
served unchanged. In curl the empty header is written `-H "X-Extract;"`; `-H "X-Extract:"`
strips the header from the request instead of sending it empty, so the server extracts.

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
deletes anything the archive did not mention. It answers `201` when at least one path was
new and `200` when every path was a replacement, with an `X-Extracted-Count: <n>` header and
one `/<hash>/<path>` line per extracted file as its body.

```sh
tar czf /tmp/bundle.tgz index.html css/app.css
```

```sh
curl -X PUT \
  -H "X-API-Key: $API_KEY" \
  --data-binary @/tmp/bundle.tgz \
  {{ .Instance.URL }}/abc1234/site/bundle.tgz
```

It answers `201` with `X-Extracted-Count: 2` and the body
`/abc1234/site/index.html` and `/abc1234/site/css/app.css`, one line per extracted file.

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

## `/verify-key`

The key-check route; it answers `POST` alone and every other method `405`.

### `POST` — check a key without creating anything

Answers `key ok` for an API key that works, or `403 invalid API key` for one that does not.
A failure is charged exactly like `POST /` does — same lockout accounting — and nothing is
created. `sandbin-client init` uses it to check a key before saving it (see [Point
sandbin-client at a server](sandbin-client-config.md)).

```sh
curl -s \
  -X POST \
  -H "X-API-Key: $API_KEY" \
  {{ .Instance.URL }}/verify-key
```

It answers `key ok` as `text/plain` for a key that works; a wrong key is `403 invalid API
key` and one lockout failure.

## `/healthz`

The liveness route answers `GET` and `HEAD` and every other method `405`.

### `GET` and `HEAD` — the liveness answer

Answers `200` `text/plain` with `ok`. Unauthenticated on purpose for container health
checks (see [Run it with podman](sandbin-podman.md)); it says nothing about bins.

```sh
curl {{ .Instance.URL }}/healthz
```

It answers `ok`.
