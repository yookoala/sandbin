# Upload previews with sandbin-client

`cmd/sandbin-client` is this API shaped for an agent that just wrote a prototype: one `created`
or `updated` line per file on stdout (or one `extracted <n> files into <url>` line when the
server unpacked an upload), and the bin base URL last for the human to open. Every invocation
names its verb: `upload` sends files or piped stdin, `init` settles the directory's connection
settings (see [Point sandbin-client at a server](sandbin-client-config.md)), `delete` takes paths back out of a bin, and
`describe` prints what a bin already says about itself. A bare `sandbin-client`, `--help` and
`help` print this verb list on stdout and exit 0, and `help <verb>` and `<verb> --help` do the
same for one verb's flags; a mistyped verb answers `No help topic for 'x'` on stderr and exits
1 without making any request.

```sh
go build -o sandbin-client ./cmd/sandbin-client
```

`SANDBIN_URL` (default `http://localhost:8080`), `SANDBIN_API_KEY` and
`SANDBIN_BIN_PASSWORD` are the settings; `--url` and `--api-key` are flags on every verb and
override their variables, and a `.sandbin.yaml` in the working directory can carry the url and
key for a whole project (see [Point sandbin-client at a server](sandbin-client-config.md)). A key missing from flag,
environment and file alike is a startup error that makes no request at all, and its message
names `--api-key`, `SANDBIN_API_KEY` and `sandbin-client init` — `describe` is the one verb
that asks for no key. `SANDBIN_BIN_PASSWORD` has no flag on purpose, so a bin password never
lands on a command line: when it is set it
goes out as `X-Bin-Password` on the create and as Basic Auth (user name = bin hash) on every
upload. On `upload`, `--bin <hash>` uploads into a bin you already have, `--new` forces a fresh
one, and `--name <path>` names piped-in stdin; with nothing piped in it is an error, not an
empty upload. `--archive <folder>` uploads a whole folder as one server-unpacked archive
(below), and `--no-extract` sends the `X-Extract` opt-out header with every upload of the run.
A name that starts with `-` goes after `--` (`sandbin-client upload -- -draft.html`), and a
misused verb reports its misuse followed by its own usage.

```sh
export SANDBIN_URL=http://localhost:8080 SANDBIN_API_KEY=your-key
sandbin-client upload prototype/index.html prototype/app.js prototype/style.css
# created http://localhost:8080/abc1234/prototype/index.html
# created http://localhost:8080/abc1234/prototype/app.js
# created http://localhost:8080/abc1234/prototype/style.css
# http://localhost:8080/abc1234/          <- hand this to a human
make 2>&1 | sandbin-client upload --name build.ansi
# created http://localhost:8080/abc1234/build.ansi
```

A whole folder goes up as one archive: `upload --archive <folder>` writes a tar.gz of the folder
to a temp file, uploads it at the bin root named `<basename>.tar.gz`, and lets the server unpack
it. The entries carry the folder's path relative to the current directory verbatim, so
`./hello/world.html` lands as `/<hash>/hello/world.html` — and `upload --archive projects/app`
still uploads `app.tar.gz` while its entries read `projects/app/...`, so the bin's paths come out
exactly folder-relative, never `projects/projects/`.

```sh
sandbin-client upload --archive hello
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
with file arguments and `--name` stdin in one `upload` run — the archive uploads last — and
piped stdin with no `--name` is refused in an `--archive` run exactly as it is in any other run,
rather than quietly dropped. With `--no-extract` the built archive itself is what lands in the
bin, stored raw under its own name.

Hand-built archives need no client machinery: the server unpacks them from an ordinary upload.

```sh
tar czf - hello | sandbin-client upload --name site/bundle.tar.gz   # entries land under site/
sandbin-client upload site/bundle.zip                               # zip too, from any zip tool
```

A broken archive — a zip-slip path, a symlink entry, an encrypted entry, a duplicate path, no
files, corrupt bytes, or a breached size cap — comes back as the `400` or `413` the API spells
out, the whole archive refused and the bin left exactly as it was.

`--title <text>` and `--description <text>` say what the bin holds. They go out as
`X-Bin-Title` and `X-Bin-Description`: on the `POST` when the run creates the bin, and on a
`PATCH /abc1234/` when it already exists. A run with metadata flags and no files or stdin is a
run of its own — it describes the bin and uploads nothing — so notes can follow the uploads or
come before them, and giving a flag an empty value clears that field instead of leaving it
alone. `describe` uploads nothing at all and prints what the bin already says about itself, read
from the bin base; it needs no API key, because a public bin's metadata is public, and a
protected bin is read with `SANDBIN_BIN_PASSWORD` as the bin password. A title is at most 200
bytes and a description at most 2000, neither may span lines or carry control characters, and
`sandbin-client` refuses one that is too long locally rather than spending a request on it.
`describe` takes no arguments at all — file arguments are refused, and `--name` and `--archive`
are not its flags — so it can only ever read.

```sh
sandbin-client upload --title 'Button grid' --description 'A grid of pressable buttons'
# updated /abc1234/
# http://localhost:8080/abc1234/
sandbin-client describe
# title: Button grid
# description: A grid of pressable buttons
```

The two fields show up wherever the bin is read: as `X-Bin-Title` and `X-Bin-Description` on any
`GET` of the bin base, and as a line above the file list on the directory listing the bin base
serves when the bin has no `index.html`. A bin with no metadata listed is listed exactly as it
was before metadata existed.

`delete` takes files and folders back out of the bin: the file arguments name files, and a
repeatable `--folder <folder>` names folders. Flags may be written before or after the file
arguments — `delete build.ansi --folder drafts` and `delete --folder drafts build.ansi` are the
same run — and a delete run uploads nothing: `--name`, `--archive`, `--new` and `--no-extract`
are not flags of `delete`, so a run written with them is a usage error that makes no request,
and a delete run reads no stdin even when a pipe is attached. The names follow the upload
name rules: a trailing slash on a file argument is a refusal pointing at `--folder`, a trailing
slash on `--folder` is a refusal saying the client sends that slash itself, a name starting with
`-` goes after `--`, and a name that would escape the bin is refused the way an upload name is.
The names are checked against the bin by the server: a path the bin does not hold is a `404`
naming it, and a folder the bin does not hold is a `404` too. Deletions are sent in the order
written, files before folders, and one failing name stops the run — the deletions already sent
stay done. Every delete is exactly one `DELETE` request, so a refused run costs nothing. The run
prints one line per deleted file and ends with the bin URL:

```sh
sandbin-client delete build.ansi --folder drafts
# deleted http://localhost:8080/abc1234/build.ansi
# deleted http://localhost:8080/abc1234/drafts/one.md
# deleted http://localhost:8080/abc1234/drafts/two.md
# http://localhost:8080/abc1234/
```

A protected bin is deleted from with `SANDBIN_BIN_PASSWORD` the way it is uploaded to, and a
delete run finds its bin in `.sandbin.yaml` or `--bin` exactly like an upload run — with neither
it is a refusal naming both ways, never a fresh bin.

The bin is remembered in `.sandbin.yaml` in the working directory. `--bin` beats that file, a
readable file whose `sandbin_url` matches the url in use comes next, and otherwise the client
creates a bin and writes the file: mode `0600`, renamed in from a temp file in the same
directory, and an upload run never newly persists a key that came from flag or environment —
the `api_key` line is `sandbin-client init`'s work (see [Point sandbin-client at a server](sandbin-client-config.md)). The file's `bin` and `protected`
belong to its `sandbin_url`: change the url in use and the file simply stops matching, the
client makes a new bin for the new url, and the old bin is orphaned — its files stay readable at
the old URL, but nothing in this directory points at them any more. A corrupt or unreadable
`.sandbin.yaml` is an error naming the file, never a silent second bin for the same prototype.
Names are checked before anything
is sent, so an absolute path, a `..` segment or a file over 1 MiB fails locally instead of
costing a request.

**Do not retry a failing `sandbin-client` from a script.** It makes exactly one request per
operation and never retries, because a wrong or missing API key blocks the client's address
from `POST`, `PUT` and `DELETE` for 30 minutes, every further failure adds 30 minutes (to a 24 hour
cap per block), and an attempt while already blocked counts as another failure too. A loop
that reruns the client after a `403` only digs that wait deeper; read the `429` and its
`Retry-After`, fix the key, and wait out the stated seconds. `GET` is never charged or
blocked, so a blocked address can still open what it already published.

**A prototype needs `index.html` at the bin root** for the bin base URL to show the page: the
base path serves `index.html` only when that exact path exists in the bin, and otherwise
answers with the directory listing. `sandbin-client upload prototype/index.html` therefore gives
a listing at `http://localhost:8080/abc1234/` and the page itself at
`http://localhost:8080/abc1234/prototype/index.html`.
