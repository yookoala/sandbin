# The HTTP API

`GET /` answers a small branded front page that says what the service is; its example commands
echo the address your request arrived at (the `Host`, or `X-Forwarded-Host` and
`X-Forwarded-Proto: https` from a proxy on a trusted address — the same trust rule as blocked
addresses use), falling back to a `this.host` placeholder for anything odd. Creating a bin stays
the key-checked `POST /`, and every other method on `/` still answers `405 method not allowed`.
A `POST /verify-key` answers `key ok` or `403 invalid API key` for the key it is given, charges
a failure exactly like `POST /` does, and creates nothing; `sandbin-client init` uses it to
check a key before saving it (see [Point sandbin-client at a server](sandbin-client-config.md)). `GET /skills/sandbin-preview/SKILL.md`
answers the agent skill rendered with the address the request arrived at as `200` `text/markdown`, and `GET /skills/sandbin-preview/install.sh`
answers the one-line installer as `200` `text/plain` with its download address filled in to the
one the request arrived at; both need no key and charge nothing, and every other method on those
paths answers `405` (see [Give the skill to your agent](sandbin-client-skill.md)).

Create a bin, then PUT files under it. `$BIN` already ends in `/`, so write `${BIN}name` and
not `$BIN/name`, which asks for a `//` path the server rejects:

```sh
BIN=$(curl -s -X POST -H "X-API-Key: your-key" http://localhost:8080/)   # -> /abc1234/
curl -X PUT -H "X-API-Key: your-key" -d '<h1>Hello</h1>' "http://localhost:8080${BIN}index.html"
curl -X PUT -H "X-API-Key: your-key" -d '# Notes' "http://localhost:8080${BIN}notes.md"
curl -X PUT -H "X-API-Key: your-key" --data-binary "$(make 2>&1)" "http://localhost:8080${BIN}build.ansi"
```

Read them back. The bin base path serves `index.html` when there is one, otherwise a
directory listing; a `.ansi` file comes back as rendered HTML:

```sh
curl http://localhost:8080${BIN}
curl http://localhost:8080${BIN}notes.md
curl http://localhost:8080${BIN}build.ansi
```

A `GET` of a bin without its trailing slash (`http://localhost:8080/abc1234`) is redirected
with a 301 to the slashed path, so relative links inside the bin resolve; unknown hashes keep
answering 404.

Give a bin its own password and send it as Basic Auth (user name = bin hash) on every
GET, PUT and DELETE below it:

```sh
PBIN=$(curl -s -X POST -H "X-API-Key: your-key" -H "X-Bin-Password: sesame" http://localhost:8080/)
curl -u "${PBIN//\//}:sesame" http://localhost:8080${PBIN}notes.md
```

Say what a bin holds. `X-Bin-Title` and `X-Bin-Description` on the create store a one-line
title and description with the bin, `PATCH /{hash}/` edits them afterwards, and every `GET` on
the bin base answers with both as response headers:

```sh
BIN=$(curl -s -X POST -H "X-API-Key: your-key" -H "X-Bin-Title: Button grid" \
  -H "X-Bin-Description: A grid of pressable buttons" http://localhost:8080/)   # -> /abc1234/
curl -s -X PATCH -H "X-API-Key: your-key" -H "X-Bin-Description: A grid, keyboard, and pointer" \
  "http://localhost:8080${BIN}"                                                 # -> {"title":"Button grid","description":"A grid, keyboard, and pointer"}
curl -s -D - -o /dev/null "http://localhost:8080${BIN}"                        # -> … X-Bin-Title / X-Bin-Description
```

`PATCH` merges: a header that is present replaces that field, a header that is absent leaves it
as it was, and a header present with an empty value clears it, so one request can set, keep or
drop either field. In curl the empty header is written `-H "X-Bin-Title;"` — `-H "X-Bin-Title:"`
strips the header from the request instead of sending it empty. A title is at most 200 bytes and
a description at most 2000, and neither may
contain a tab, newline or other control character; an over-long or control-character-bearing
value is a `400` and leaves the bin as it was. `PATCH` is key-checked, so a wrong API key costs
a lockout failure, while a wrong bin password on a protected bin is a `401` that charges
nothing. Metadata travels on headers, which keeps the stored bytes untouched: the bin base still
serves a raw `index.html` byte for byte (metadata visible in the response headers, not in the
document), and only the directory listing page carries it, as a `<p class="bin-meta">` line
above the file list. A public bin's metadata is public: anyone who can read the bin can read
what it says it holds.

A file is removed with `DELETE /{hash}/{path}`, and a folder with `DELETE /{hash}/{folder}/`
— the trailing slash marks the folder and takes everything under it. The answer is JSON
listing what was removed, like `{"deleted":["drafts/one.md","drafts/two.md"]}`. `DELETE` is
key-checked like `PUT`, carries no body, and answers `405` against a bin without a path and
`404` for a path the bin does not hold. A bin whose files include a `404.html` answers every
unmatched path under it with that file and a `404` status, which lets a single-page app serve
its own error page from the bin; a bin without one answers the plain `404 unknown path`.

```sh
curl -X DELETE -H "X-API-Key: your-key" "http://localhost:8080${BIN}build.ansi"  # -> {"deleted":["build.ansi"]}
curl -X DELETE -H "X-API-Key: your-key" "http://localhost:8080${BIN}drafts/"     # -> {"deleted":["drafts/one.md","drafts/two.md"]}
```

PUT bodies are capped at 1 MiB. `403` is a bad API key, `401` a missing bin password,
`404` an unknown bin or path, `405` an unsupported method, `400` a rejected path or metadata,
`413` an oversized body, `429` an address blocked for bad API keys. A `HEAD` answers `405`:
read the headers of a `GET` with `curl -s -D - -o /dev/null <url>`.

An upload whose cleaned path ends in `.zip`, `.tar`, `.tar.gz` or `.tgz` (either case) and
whose body starts with the matching archive magic is unpacked into the bin instead of stored as
one file: zip, raw tar and gzipped tar, and the format is taken from the body's magic rather
than the extension, so a `.tgz` holding a zip is unzipped while an archive-looking name holding
no archive magic is a `400 not a zip, tar or tar.gz archive`. An archive body under an ordinary
name stays just a file. The entries land under the upload name's parent directory:
`PUT ${BIN}site/bundle.tar.gz` with entries `index.html` and `css/app.css` creates
`site/index.html` and `site/css/app.css`, and an archive at the bin root unpacks to the root.
The archive name itself is not stored and no directory is created for it; every extracted file
is an ordinary bin file from then on, listed, served and rendered like any upload, and an
`index.html` among the entries becomes the bin's cover page.

Every entry is checked before anything is stored, on the name the archive itself wrote: a
`.` or `..` segment anywhere in it, a name starting with `/`, a control character, a
backslash (tar keeps them), an over-long or over-deep name is refused by the same rules an
ordinary upload name obeys. One leading `./` is stripped, because that is how
`tar czf - .` names its files, and the entry naming the folder itself is checked and
skipped; a regular file with no name at all is `400 archive contains an unnamed entry`. A
tar header whose name field carries bytes past its NUL terminator is
`400 archive contains an entry name with a NUL byte <name>`, because the archive reader
would otherwise store that entry under the shortened name it stops at; the walk that reads
those name fields reaches every header it can count, a size written the base-256 way
included. Two shapes are judged by what the archive reader reads instead of by the raw
bytes: an entry following a GNU sparse entry, whose extra blocks the walk cannot count
past, and an entry whose name arrives in a GNU long name record. There the entry lands
under that reader's shortened name, judged by the same rules. So an entry can never move a
file out of the folder the archive lands in: `sub/../up.txt` under
`site/bundle.zip` is a refusal, not `site/up.txt`, and `deep/../../evil.txt` never reaches
the bin root. Every refusal answers `400` naming the rule (`path must not escape the bin`
and friends) and the bin stays exactly as it was. Symlink, hardlink, device and fifo entries
(`archive contains a symlink <path>`) and entries a zip claims to have encrypted
(`archive contains an encrypted entry <path>`) are refused the same loud way, as are duplicate
paths (`archive contains duplicate path <path>`), an archive holding no files
(`archive contains no files`) and bodies that are broken archives (`corrupt zip archive`,
`corrupt tar archive`, `corrupt tar.gz archive`). A GNU sparse entry — what `tar --sparse`
writes — is a regular file with holes and extracts as one: the holes fill out to the size
the entry claims, and the caps just below count the bytes it fills out to. Inside the
unchanged 1 MiB body cap each entry unpacks to at most 1 MiB, one archive holds at most
1000 entries and unpacks at most 32 MiB in total, checked while streaming so a
decompression bomb stops at its caps instead of at the server's memory: a breach is
`413 extracted file over 1 MiB`, `archive has over 1000 files` or `archive unpacks over
32 MiB`, and nothing is stored. A successful extraction stores
all entries or none, replaces the paths it names and never deletes anything the archive did not
mention, and answers `201` when at least one path was new and `200` when every path was a
replacement, with an `X-Extracted-Count: <n>` header and one `/<hash>/<path>` line per
extracted file as its body. To keep an archive-named body raw after all, send an `X-Extract`
header on the `PUT` — any value, even empty; presence is what the server reads — and the bytes
are stored and served unchanged. In curl the empty header is written `-H "X-Extract;"` —
`-H "X-Extract:"` strips the header from the request instead of sending it empty, so the server
extracts.
