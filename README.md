# Sandbin

[![CI status](https://github.com/yookoala/sandbin/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/yookoala/sandbin/actions/workflows/ci.yml?query=branch%3Amain)

A simple PUT/GET server for CLI agents in sandbox to submit HTML / ANSI mock up previews
or simple prototyping.

With a fixed pre-shared API key, a client may use POST to create a bin (for a collection
of preview pages) with a randomly assigned hash path. Then a client may PUT markdown /
HTML / ANSI text to subpaths of the bin.

Client may specify additional password protections (Basic Auth) to the bin it created.

The base path of the bin, if not specified with an index.html, will automatically has
directory listing for the items in it.

A bin can also say what it holds: a one-line title and description, set when it is created,
edited afterwards with PATCH, and read back at the bin base.

All contents are stored in a single sqlite file. A db written before metadata existed gains the
columns for it the next time a `serve` opens it, so upgrading needs no manual step.

## Usage

Build and run:

```sh
go build -o sandbin ./cmd/sandbin
SANDBIN_API_KEY=your-key ./sandbin serve
```

Every invocation names its verb: `serve` runs the server, `ip list`, `ip show <ip>`, `ip unblock
<ip>` and `ip clear` inspect and lift the API key blocks through that server's control socket
(see Managing blocked addresses), and `help` prints the usage. A bare `sandbin` or an unknown verb
prints that usage on stderr and exits 1.

`--api-key` (default `$SANDBIN_API_KEY`), `--db` (default `sandbin.db`), `--addr` (default
`:8080`) and `--socket` (default `$SANDBIN_SOCKET`, otherwise the `--db` path plus `.sock`; see
Managing blocked addresses) are `serve`'s core settings; `--trusted-proxy` and
`--trusted-proxy-file` (defaults `$SANDBIN_TRUSTED_PROXIES` and `$SANDBIN_TRUSTED_PROXIES_FILE`,
see Trusted proxies) optionally name the proxies the server believes. The server exits when no API
key is configured, a trusted proxy value cannot be read, or another serve already answers on its
control socket.

`GET /` answers a small branded front page that says what the service is. Creating a bin stays
the key-checked `POST /`, and every other method on `/` still answers `405 method not allowed`.

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

Give a bin its own password and send it as Basic Auth (user name = bin hash) on every
GET and PUT below it:

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

## Upload previews with sandbin-client

`cmd/sandbin-client` is this API shaped for an agent that just wrote a prototype: one
command, one `created` or `updated` line per file on stdout (or one `extracted <n> files into
<url>` line when the server unpacked an upload), and the bin base URL last for
the human to open.

```sh
go build -o sandbin-client ./cmd/sandbin-client
```

`SANDBIN_URL` (default `http://localhost:8080`), `SANDBIN_API_KEY` and
`SANDBIN_BIN_PASSWORD` are the settings; `--url` and `--api-key` override their variables,
and a missing key is a startup error that makes no request at all — `--describe` is the one run
that asks for no key. `SANDBIN_BIN_PASSWORD`
has no flag on purpose, so a bin password never lands on a command line: when it is set it
goes out as `X-Bin-Password` on the create and as Basic Auth (user name = bin hash) on every
upload. `--bin <hash>` uploads into a bin you already have, `--new` forces a fresh one, and
`--name <path>` names piped-in stdin; with nothing piped in it is an error, not an empty
upload. `--archive <folder>` uploads a whole folder as one server-unpacked archive (below), and
`--no-extract` sends the `X-Extract` opt-out header with every upload of the run. A name that
starts with `-` goes after `--` (`sandbin-client -- -draft.html`), and
`--help` prints the flag list on stderr.

```sh
export SANDBIN_URL=http://localhost:8080 SANDBIN_API_KEY=your-key
sandbin-client prototype/index.html prototype/app.js prototype/style.css
# created http://localhost:8080/abc1234/prototype/index.html
# created http://localhost:8080/abc1234/prototype/app.js
# created http://localhost:8080/abc1234/prototype/style.css
# http://localhost:8080/abc1234/          <- hand this to a human
make 2>&1 | sandbin-client --name build.ansi
# updated http://localhost:8080/abc1234/build.ansi
```

A whole folder goes up as one archive: `--archive <folder>` writes a tar.gz of the folder to a
temp file, uploads it at the bin root named `<basename>.tar.gz`, and lets the server unpack it.
The entries carry the folder's path relative to the current directory verbatim, so
`./hello/world.html` lands as `/<hash>/hello/world.html` — and `--archive projects/app` still
uploads `app.tar.gz` while its entries read `projects/app/...`, so the bin's paths come out
exactly folder-relative, never `projects/projects/`.

```sh
sandbin-client --archive hello
# extracted 2 files into http://localhost:8080/abc1234/hello.tar.gz
# http://localhost:8080/abc1234/          <- the folder, as ordinary bin files
```

That line names the upload; every path that landed is in the bin's listing. The folder is
checked before any request, the way upload names are: `.` and absolute folder arguments are
refused (`--archive names a folder` — the base path of a bin must name a folder), as are a
missing path, a plain file instead of a folder, a symlink or other non-regular entry inside it
(`<path> is not a regular file`), a file over 1 MiB (`<path> is over 1 MiB`), more than 1000
files (`folder has over 1000 files`), more than 32 MiB of total content
(`folder unpacks over 32 MiB`), entry names past the server's path limits, a folder holding no
files at all (`folder contains no files`), and a built archive over 1 MiB
(`archive is over 1 MiB`). The temp file is removed on every exit path. `--archive` combines
with file arguments and `--name` stdin in one run — the archive uploads last — and piped stdin
with no `--name` is refused in an `--archive` run exactly as it is in any other run, rather
than quietly dropped. With `--no-extract` the built archive itself is what lands in the bin,
stored raw under its own name.

Hand-built archives need no client machinery: the server unpacks them from an ordinary upload.

```sh
tar czf - hello | sandbin-client --name site/bundle.tar.gz   # entries land under site/
sandbin-client site/bundle.zip                               # zip too, from any zip tool
```

A broken archive — a zip-slip path, a symlink entry, an encrypted entry, a duplicate path, no
files, corrupt bytes, or a breached size cap — comes back as the `400` or `413` the API spells
out, the whole archive refused and the bin left exactly as it was.

`--title <text>` and `--description <text>` say what the bin holds. They go out as
`X-Bin-Title` and `X-Bin-Description`: on the `POST` when the run creates the bin, and on a
`PATCH /abc1234/` when it already exists. A run with metadata flags and no files or stdin is a
run of its own — it describes the bin and uploads nothing — so notes can follow the uploads or
come before them, and giving a flag an empty value clears that field instead of leaving it
alone. `--describe` uploads nothing at all and prints what the bin already says about itself,
read from the bin base; it needs no API key, because a public bin's metadata is public, and a
protected bin is read with `SANDBIN_BIN_PASSWORD` as the bin password. A title is at most 200
bytes and a description at most 2000, neither may span lines or carry control characters, and
`sandbin-client` refuses one that is too long locally rather than spending a request on it.
`--describe` takes no file arguments, no `--name` and no `--archive`, so it can only ever read.

```sh
sandbin-client --title 'Button grid' --description 'A grid of pressable buttons'
# updated /abc1234/
# http://localhost:8080/abc1234/
sandbin-client --describe
# title: Button grid
# description: A grid of pressable buttons
```

The two fields show up wherever the bin is read: as `X-Bin-Title` and `X-Bin-Description` on any
`GET` of the bin base, and as a line above the file list on the directory listing the bin base
serves when the bin has no `index.html`. A bin with no metadata listed is listed exactly as it
was before metadata existed.

The bin is remembered in `.sandbin.json` in the working directory:

```json
{ "url": "http://localhost:8080", "bin": "abc1234", "protected": false }
```

`--bin` beats that file, a readable file whose `url` matches the url in use comes next, and
otherwise the client creates a bin and writes it: mode `0600`, renamed in from a temp file in
the same directory, never holding the API key or the bin password, and gitignored. A corrupt
or unreadable `.sandbin.json` is an error naming the file, never a silent second bin for the
same prototype. Change the url in use and the file simply stops matching: the client makes a
new bin for the new url, and the old bin is orphaned — its files stay readable at the old
URL, but nothing in this directory points at them any more. Names are checked before anything
is sent, so an absolute path, a `..` segment or a file over 1 MiB fails locally instead of
costing a request.

**Do not retry a failing `sandbin-client` from a script.** It makes exactly one request per
operation and never retries, because a wrong or missing API key blocks the client's address
from `POST` and `PUT` for 30 minutes, every further failure adds 30 minutes (to a 24 hour
cap per block), and an attempt while already blocked counts as another failure too. A loop
that reruns the client after a `403` only digs that wait deeper; read the `429` and its
`Retry-After`, fix the key, and wait out the stated seconds. `GET` is never charged or
blocked, so a blocked address can still open what it already published.

**A prototype needs `index.html` at the bin root** for the bin base URL to show the page: the
base path serves `index.html` only when that exact path exists in the bin, and otherwise
answers with the directory listing. `sandbin-client prototype/index.html` therefore gives a
listing at `http://localhost:8080/abc1234/` and the page itself at
`http://localhost:8080/abc1234/prototype/index.html`.

## API key lockout

`POST /` and `PUT /{hash}/{path}` are the key-checked requests. A wrong or missing `X-API-Key`
blocks that address from both for 30 minutes; every further failure adds 30 minutes, capped at
24 hours per block, and the failure count survives a restart. While blocked, a key-checked
request answers `429` with a `Retry-After` (the remaining wait in seconds) and counts as another
failure, so hammering only lengthens the wait. `GET` is never charged and never blocked: a
blocked address can still read public bins. Wrong bin passwords (`401`) are not failures here.

`X-Forwarded-For` is believed only when the connection itself arrives from a proxy the server
trusts, and only its rightmost entry is charged. Loopback, private and link-local addresses are
trusted with no configuration, so a reverse proxy on the same machine or network is honoured
exactly as before; `--trusted-proxy` only adds to that list (see Trusted proxies). Anyone
reaching the port over a route the server does not trust is charged by their own socket address
and cannot frame a third party, but a client whose connection arrives through a trusted proxy
can name an arbitrary address in that header and get that address blocked.

That freedom cuts both ways on every trusted socket, loopback included: a client allowed to name
itself can also send a different valid address per attempt and have each failure charged to a
fresh identity, so it never accumulates a penalty of its own. The defence is configuration, not
code: trust only the sockets and ranges you really front with a proxy, because the default
loopback, private and link-local trust is what makes the trick available wherever clients share a
network.

### Trusted proxies

An origin behind a public proxy such as Cloudflare sees the proxy's own public address as the
socket and the real client only in `X-Forwarded-For`. `--trusted-proxy` (default
`$SANDBIN_TRUSTED_PROXIES`) takes a comma-separated list of proxy IPs and CIDRs whose header is
believed the way loopback's is:

```sh
./sandbin serve --api-key your-key --trusted-proxy 203.0.113.7,173.245.48.0/20
```

`--trusted-proxy-file` (default `$SANDBIN_TRUSTED_PROXIES_FILE`) reads the same entries one per
line, ignoring blank lines and `#` comments, for lists long enough to deserve a file, such as
the published Cloudflare ranges. The file list and the comma list are merged, file first:

```sh
cat > ~/.config/sandbin/proxies.txt <<'EOF'
# Cloudflare IPv4 ranges (example entries; fetch the current list from Cloudflare)
173.245.48.0/20
103.21.244.0/22
103.22.200.0/22
EOF
./sandbin serve --api-key your-key --trusted-proxy-file ~/.config/sandbin/proxies.txt
```

A bad entry or an unreadable file makes the server refuse to start, naming the offending value,
the way a missing API key does. In the quadlet flow these settings come from the same `0600`
env file as the key, as `SANDBIN_TRUSTED_PROXIES=203.0.113.7,173.245.48.0/20`; use
`SANDBIN_TRUSTED_PROXIES_FILE` there only if the file itself is mounted into the container.

Trusting a public range means any connection whose source address is inside that range gets
header authority: whoever can open a socket from such an address can name whichever address the
penalty lands on. `0.0.0.0/0` (every IPv4 connection) and `::/0` (every IPv6 connection) are
accepted only as a deliberate operator choice and enable that framing for the whole internet;
they are never defaults, and loopback, private and link-local trust needs no configuration.

## Run it with podman

Build the image (hermetic two stages: `golang:1.26-alpine` builds, `alpine:3.22` runs; the
host needs nothing but podman). Build it under exactly this tag, because the quadlet unit
looks for it and podman tries to pull a registry image otherwise:

```sh
podman build -t localhost/sandbin:latest .
```

Podman warns `HEALTHCHECK is not supported for OCI image format` on every build: its default
image format drops that line. The probes below are the podman-native form, and podman runs
them. Add `--format docker` to the build instead if you want the image to carry it.

Ad-hoc run, with a named volume holding the sqlite file, the key from a `0600` env file, and
the health probe. It needs no command of its own: the image's CMD is `serve --db
/data/sandbin.db`.

```sh
podman run -d --name sandbin -p 127.0.0.1:8080:8080 -v sandbin-data:/data \
  --env-file ~/.config/sandbin/sandbin.env \
  --health-cmd "wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1" \
  --health-interval=30s --health-start-period=5s --health-retries=3 \
  localhost/sandbin:latest   # runs serve --db /data/sandbin.db
podman inspect --format '{{.State.Health.Status}}' sandbin   # probed every 30s, healthy on a healthy server
```

As a systemd user service through podman quadlet:

```sh
mkdir -p ~/.config/containers/systemd ~/.config/sandbin
cp deploy/sandbin.container ~/.config/containers/systemd/
install -m 600 deploy/sandbin.env.example ~/.config/sandbin/sandbin.env   # then edit replace-me
sed -i "s#^EnvironmentFile=.*#EnvironmentFile=$HOME/.config/sandbin/sandbin.env#" ~/.config/containers/systemd/sandbin.container
systemctl --user daemon-reload
systemctl --user start sandbin
```

The `sed` is not optional. `deploy/sandbin.container` points `EnvironmentFile=` at
`/etc/sandbin/sandbin.env`, meant for a system install, and quadlet resolves a relative or
`~`-prefixed path against the unit's own directory, so an unedited `~/.config/...` value
restart-loops. For a system install, keep the path as shipped and create `/etc/sandbin/sandbin.env`
with mode 0600 instead.

The published port is loopback-only (`PublishPort=127.0.0.1:8080:8080`) because this is a
preview tool; edit the unit to `8080:8080` to expose it on the network. The API key never
appears in the container command line, but it is visible in `podman inspect` output
(`Config.Env`), so treat the env file like a password.

## Managing blocked addresses

`serve` listens on a second address besides its port: a unix control socket that exposes the
block table, and the `ip` commands drive it. The socket is what controls the lockout now, not the
sqlite file. `serve` creates it beside the db it opens — `sandbin.db` comes with
`sandbin.db.sock`, `/data/sandbin.db` with `/data/sandbin.db.sock` — and chmods it `0600` after
binding, so it is the server's own user who may manage blocks; relax that mode only on purpose.
`--socket <path>` names another socket for either side (the socket on a tmpfs while the db stays
on disk), and `serve` also falls back to `SANDBIN_SOCKET` the way it falls back for its other
settings.

A live `serve` is required: there is no offline mode, and no verb opens, stats or creates the
`--db` file. Nothing answering the socket is one readable error naming it (`no sandbin serve
listening on sandbin.db.sock`, exit 1) and nothing changes. No verb sends an API key, and
connecting to the socket grants block management and nothing else — narrower than what it used to
be, because `ip unblock` once needed write access to the whole db file, which also allowed
editing bins and files with plain SQL. Each verb takes `--db` (default `sandbin.db`) and asks the
serve whose socket sits next to that file; `./sandbin ip help` prints this family's usage:

```sh
./sandbin ip list                    # every row of the block table
./sandbin ip list --json             # the same rows as a json array
./sandbin ip show 203.0.113.7        # one address's row in the block table
./sandbin ip unblock 203.0.113.7     # delete one address's block
./sandbin ip clear                   # delete every block
```

Flags go before the address, for `show` and `unblock` alike:
`./sandbin ip unblock --db /data/sandbin.db 203.0.113.7`.

`list` prints `IP`, `FAILURES`, `BLOCKED UNTIL`, `REMAINING` and `LAST FAILURE`. `REMAINING`
is worked out while the command runs and reads `expired` once `blocked_until` is past; expired
rows are listed on purpose, because their failure count still shapes the next penalty. An empty
table prints `no blocked addresses` and exits 0. `--json` prints the same rows as an array of
`ip`, `failures`, `blocked_until` and `updated_at` (`[]` when there is no row), and it belongs
to `list` alone: `show`, `unblock` and `clear` refuse it.

`show` prints one address's row in that same table and nothing else: the header, then the row,
with `REMAINING` worked out the same way. Its argument is spelled the way `unblock` spells it (see
below), and an address with no row is the same error `unblock` gives for it (`no block for
203.0.113.7`, exit 1). Reading a row and lifting one now need the same thing — the right to
connect to the socket — so there is no read-only half of the family any more.

`unblock` deletes the whole row, so the failure count goes with it: the next wrong key from
that address starts again at 30 minutes instead of resuming the escalation it had reached. Its
argument is spelled the way the server spells an address it charges — the keys the server
stores never carry a zone — so `::ffff:203.0.113.7`, `fe80::1%eth0` and `2001:0DB8::1` all
reach the stored key. A key that itself carries a zone can only come from a hand-inserted row,
and `ip clear` or plain SQL is how to remove one. An address with no block is an error naming
it (`no block for 203.0.113.7`, exit 1), which is how a typo shows up.
`clear` deletes every row and prints `cleared N blocks`; `N=0` is fine.

The socket speaks four requests and nothing else: `GET /ip` (every row), `GET /ip/{ip}` (one
row), `DELETE /ip/{ip}` and `DELETE /ip`, answering `404` with `{"error":"no block for <ip>"}`
for a key the table does not have and `{"unblocked":"<ip>"}` or `{"cleared":N}` when a delete
did something; an unknown path is `404`, a wrong method is `405`, and `HEAD` answers like `GET`.
`curl --unix-socket` drives it the same way:

```sh
curl --unix-socket sandbin.db.sock http://localhost/ip
curl -X DELETE --unix-socket sandbin.db.sock http://localhost/ip/203.0.113.7
```

The two listeners share no routes: `/ip` on the public port is an unknown bin, and the socket has
no bin, file or healthz route. The running server is now the only writer of the block table, so a
management run cannot contend with it for the sqlite lock; `RecordFailure` keeps its bounded
retry for the writers that remain — an old binary or a stray sqlite client can still open the db
file — and a charge that outlives every retry is lost but logged (`record auth failure`), never
silently dropped.
`serve` refuses to start when the socket path is already answering (`another sandbin serve is
already listening on sandbin.db.sock`), replaces the stale socket a killed server left behind, and
refuses a path that holds a regular file or a symlink, leaving that file exactly where it is.

In the container the socket rides the same `sandbin-data` volume as `/data/sandbin.db`, so a
second container of the same image is the management route, and it needs the volume rather than
the key. The image's working directory is `/`, so name that db, or work in the volume and the
default `sandbin.db` resolves to it:

```sh
podman run --rm -v sandbin-data:/data localhost/sandbin ip list --db /data/sandbin.db
podman run --rm -w /data -v sandbin-data:/data localhost/sandbin ip list
```


# License

This software is licensed under the MIT License. You can find a copy of the license [here](LICENSE.md).
