# Upload previews with sandbin-client

`sandbin-client` is [the API](sandbin-api.md) shaped for an agent that just wrote a prototype:
one `created` or `updated` line per file on stdout (or one `extracted <n> files into <url>`
line when the server unpacked an upload), and the bin base URL last for the human to open.
Every invocation names its verb: `upload` sends files or piped stdin, `delete` takes paths back
out of a bin, `describe` prints the full inspection of one bin, and `list` prints every bin
the server holds; `init` settles the directory's connection settings and is covered on
[Building and Config](sandbin-client-init.md).
A bare `sandbin-client`, `--help` and `help` print this verb list on stdout and exit 0;
`help <verb>` and `<verb> --help` do the same for one verb's flags; a mistyped verb answers
`No help topic for 'x'` on stderr and exits 1 without making any request. The binary itself is
[Building and Config](sandbin-client-init.md) too; this page assumes you have it and a settled
project.

Three settings feed every verb: `SANDBIN_URL`, `SANDBIN_API_KEY` and `SANDBIN_BIN_PASSWORD`.
`--url` and `--api-key` are flags on every verb and override their variables, and the
`.sandbin.yaml` in the working directory carries what this project chose to store: the url,
and a key only where a person or an `init` prompt chose one, because the environment may
update a field the file already has but never creates one (see [Building and
Config](sandbin-client-init.md) for the file and the full precedence). A url
missing from flag, environment and file alike is a startup error that makes no request at all:
`no server URL: pass --url, set SANDBIN_URL or run sandbin-client init`. A key missing from
flag, environment and file alike is a startup error too, and its message names `--api-key`,
`SANDBIN_API_KEY` and `sandbin-client init` — `describe` is the one verb that asks for no key.
`SANDBIN_BIN_PASSWORD`
has no flag on purpose, so a bin password never lands on a command line: when it is set it goes
out as the `password` of the create body and as Basic Auth (user name = bin hash) on every
upload.
Names follow the same rules wherever a verb takes one: a name that starts with `-` goes after
`--` (`sandbin-client upload -- -draft.html`), and every name is checked locally before
anything is sent — an absolute path, a `..` segment or a file over 1 MiB fails locally instead
of costing a request. A misused verb reports its misuse followed by its own usage.

## upload

`upload` takes file arguments, piped stdin, or both in one run. `--bin <hash>` uploads into a
bin you already have and `--new` forces a fresh one; `--name <path>` names piped-in stdin, and
with nothing piped in that is an error, not an empty upload; `--archive <folder>` uploads a
whole folder as one server-unpacked archive (below); and `--no-extract` uploads every file of
the run with `?raw=1`, so archive-shaped bodies are stored raw. `--json` prints the run as one
JSON action report ([rules below](#rules-that-hold-for-every-run)) — on `upload`, write it
before the file arguments.

### Finding or creating the bin

An upload run that is not told otherwise finds its bin first: `--bin` beats the file, a
readable `.sandbin.yaml` whose `sandbin_url` matches the url in use comes next, and otherwise
the run creates a bin and writes the file — the file's shape and writing rules are on
[Building and Config](sandbin-client-init.md). A corrupt or unreadable `.sandbin.yaml` is an
error naming the file, never a silent second bin for the same prototype. The file names the
server each stored bin belongs to, and the rule follows from that: the file's `bin` and
`protected` belong to its `sandbin_url` — change the url in use and the file simply stops
matching, the client makes a new bin for the new url, and the old bin is orphaned — its files
stay readable at the old URL, but nothing in this directory points at them any more. A
`delete` run finds its bin exactly the same way, except that with neither `--bin` nor a
matching file it is a refusal naming both ways, never a fresh bin.

### Upload files

```sh
export SANDBIN_URL={{ .Instance.URL }} SANDBIN_API_KEY=your-key
sandbin-client upload prototype/index.html prototype/app.js prototype/style.css
```

In a project directory that is initialized but has not uploaded yet — a `.sandbin.yaml` with a
url and key but no `bin` line — this creates the project's bin, uploads the three files under
their own paths, and records the new bin in the file. Every file answers one `created` line
naming where it landed, and the run ends with the bin base URL — the line to hand to a human:

```text
created {{ .Instance.URL }}/abc1234/prototype/index.html
created {{ .Instance.URL }}/abc1234/prototype/app.js
created {{ .Instance.URL }}/abc1234/prototype/style.css
{{ .Instance.URL }}/abc1234/
```

A later run into the same project prints `updated` for paths the bin already held, and `--bin`
sends a run to a named bin regardless of the file.

### Pipe stdin

Command output goes straight from a pipe into the bin, named by `--name`:

```sh
make 2>&1 | sandbin-client upload --name build.ansi
```

The stdin lands at the bin root as `build.ansi`, answered by one `created` line:

```text
created {{ .Instance.URL }}/abc1234/build.ansi
```

### Upload a whole folder as one archive

`upload --archive <folder>` writes a tar.gz of the folder to a temp file, uploads it at the bin
root named `<basename>.tar.gz`, and lets the server unpack it:

```sh
sandbin-client upload --archive hello
```

The run answers the line naming the upload, followed by the bin base URL; every path that
landed is in the bin's listing:

```text
extracted 2 files into {{ .Instance.URL }}/abc1234/hello.tar.gz
{{ .Instance.URL }}/abc1234/
```

The entries carry the folder's path relative to the current directory verbatim, so
`./hello/world.html` lands as `/<hash>/hello/world.html` — and `upload --archive projects/app`
still uploads `app.tar.gz` while its entries read `projects/app/...`, so the bin's paths come
out exactly folder-relative, never `projects/projects/`.

The folder is checked before any request, the way upload names are checked: `.` and absolute
folder arguments are refused (`--archive names a folder` — the base path of a bin must name a
folder), as are a missing path, a plain file instead of a folder, a symlink or other
non-regular entry inside it (`<path> is not a regular file`), a file over 1 MiB
(`<path> is over 1 MiB`), more than 1000 files (`folder has over 1000 files`), more than 32 MiB
of total content (`folder unpacks over 32 MiB`), entry names past the server's path limits, a
folder holding no files at all (`folder contains no files`), and a built archive over 1 MiB
(`archive is over 1 MiB`). The temp file is removed on every exit path.

`--archive` combines with file arguments and `--name` stdin in one `upload` run — the archive
uploads last — and piped stdin with no `--name` is refused in an `--archive` run exactly as it
is in any other run, rather than quietly dropped. With `--no-extract` the built archive itself
is what lands in the bin, stored raw under its own name.

### Upload a hand-built archive

Hand-built archives need no client machinery: the server unpacks them from an ordinary upload.

```sh
tar czf - hello | sandbin-client upload --name site/bundle.tar.gz
```

The tar arrives as `site/bundle.tar.gz` and its entries land under `site/`. A zip works the
same way, from any zip tool:

```sh
sandbin-client upload site/bundle.zip
```

A broken archive — a zip-slip path, a symlink entry, an encrypted entry, a duplicate path, no
files, corrupt bytes, or a breached size cap — comes back as the `400` or `413` the API spells
out, the whole archive refused and the bin left exactly as it was.

### Give the bin a title and description

`--title <text>` and `--description <text>` say what the bin holds. They go out in the JSON
body: on the `POST /api/bins` when the run creates the bin, and on a `PATCH /api/bins/abc1234`
when it already exists. A run with metadata flags and no files or stdin is a run of its own —
it describes the bin and uploads nothing — so notes can follow the uploads or come before them:

```sh
sandbin-client upload --title 'Button grid' --description 'A grid of pressable buttons'
```

```text
updated /abc1234/
{{ .Instance.URL }}/abc1234/
```

Giving a flag an empty value clears that field instead of leaving it alone. A title is at most
200 bytes and a description at most 2000, neither may span lines or carry control characters,
and `sandbin-client` refuses one that is too long locally rather than spending a request on it.
The two fields show up wherever the bin is read: in the JSON answers of the `/api/bins` routes,
and as a line above the file list on the directory listing the bin base serves when the bin has
no `index.html`. A bin with no metadata listed is listed exactly as it was before metadata
existed.

## delete

`delete` takes files and folders back out of the bin: the file arguments name files, and a
repeatable `--folder <folder>` names folders. Flags may be written before or after the file
arguments — `delete build.ansi --folder drafts` and `delete --folder drafts build.ansi` are the
same run. A delete run uploads nothing: `--name`, `--archive`, `--new` and `--no-extract` are
not flags of `delete`, so a run written with them is a usage error that makes no request, and a
delete run reads no stdin even when a pipe is attached. The names follow the upload name rules,
with two of their own: a trailing slash on a file argument is a refusal pointing at `--folder`,
and a trailing slash on `--folder` is a refusal saying the client sends that slash itself.
`--json` on a delete run reports one `delete <path>` action per file the server accepted, and
the array stops at the first refusal.

The names are checked against the bin by the server: a path the bin does not hold is refused
with a `404` `path not found` naming it, and a folder the bin does not hold is a `404` too. Deletions are sent in the order
written, files before folders, and one failing name stops the run — the deletions already sent
stay done. Every delete is exactly one `DELETE` request, so a refused run costs nothing. The
run prints one line per deleted file and ends with the bin URL:

```sh
sandbin-client delete build.ansi --folder drafts
```

```text
deleted {{ .Instance.URL }}/abc1234/build.ansi
deleted {{ .Instance.URL }}/abc1234/drafts/one.md
deleted {{ .Instance.URL }}/abc1234/drafts/two.md
{{ .Instance.URL }}/abc1234/
```

A protected bin is deleted from with `SANDBIN_BIN_PASSWORD` the way it is uploaded to, and the
bin itself is found the way [upload finds it](#finding-or-creating-the-bin).

## describe

`describe` uploads nothing at all and prints the full inspection of the bin this directory
points at, read from the inspect endpoint. It needs no API key, because a public bin's
inspection is public, and a protected bin is read with `SANDBIN_BIN_PASSWORD` as the bin
password:

```sh
sandbin-client describe
```

```text
bin: abc1234
title: Button grid
description: A grid of pressable buttons
password: no
created: 2026-11-17T09:14:02Z
files:
  index.html  13 bytes  2026-11-17T09:15:00Z
  notes.md  7 bytes  2026-11-17T09:15:01Z
```

The lines are the hash, the title, the description, whether a password guards the bin, the
created time, and one indented line per stored file — a bin with nothing stored answers
`files: none`. `describe` takes no arguments at all — file arguments are refused, and `--name`
and `--archive` are not its flags — so it can only ever read. `--json` on a describe run reports
the same inspection as a single `describe the bin` action carrying the inspect answer.

## list

`list` prints every bin the instance holds, newest first. The server gates the listing behind
the API key, so this run needs the key like any write and refuses before making a request
without one:

```sh
sandbin-client list
```

```text
  abc1234  2026-11-17T09:14:02Z  Button grid
  9c0ffee0  2026-11-16T18:41:30Z
```

One line per bin: hash, created time, and title — a bin with no title prints without the title
column — and an instance with nothing to list answers `no bins`. The run walks the server's
pages on its own; no page size or cursor is ever named on the command line. `--json` on a list
run reports one `list bins` action per listing page read.

## Rules that hold for every run

`upload`, `delete`, `describe` and `list` take a `--json` flag; `init` has no `--json` (its
output is a setup transcript for a person). With it, the run prints one JSON action report on
stdout instead of its human lines: `status` (`ok` or `error`), `bin` and `url` whenever the
run worked on a bin, `actions` — the requests the client actually sent to the server, in
request order, each `{"description": "<fixed sentence>", "result": <that request's raw server
body unchanged>}` — and `message`, only when `status` is `error`. The `description` sentences
are fixed: `create the bin`, `store <path>`, `set the bin title and description`,
`delete <path>`, `describe the bin` and `list bins`. A failed run's array stops at the failed
step and that step's `result` is the refusal envelope the server answered — the object still
goes to stdout and the process exits non-zero. Nothing failure-related lands on stderr under
`--json`: no `sandbin-client:` line, no `set SANDBIN_BIN_PASSWORD` hint — stdout carries the
whole failure; advisories that are not failures, like the protected-bin warning, still print
on stderr out of stdout's way. On `upload`, write the flag before the file paths:
`sandbin-client upload --json file.md`.

```sh
sandbin-client upload --json notes.md
```

```text
{"status":"ok","bin":"abc1234","url":"{{ .Instance.URL }}/abc1234/","actions":[{"description":"create the bin","result":{"status":"ok","bin":"abc1234","url":"/abc1234/","title":"","description":"","has_password":false,"created_at":"2026-11-17T09:14:02Z"}},{"description":"store notes.md","result":{"status":"ok","path":"notes.md","size":7,"updated_at":"2026-11-17T09:15:01Z"}}]}
```

### Do not retry a failing run from a script

**Do not retry a failing `sandbin-client` from a script.** It makes exactly one request per
operation and never retries, because a wrong or missing API key blocks the client's address
from every key-checked request for 30 minutes, every further failure adds 30 minutes (to a 24
hour cap per block), and an attempt while already blocked counts as another failure too. A loop
that reruns the client after a `403` only digs that wait deeper; read the `429` and its
`Retry-After`, fix the key, and wait out the stated seconds. Bin `GET`s are never charged or
blocked, so a blocked address can still open what it already published (see [IP Address
Blocking](sandbin-ip-blocking.md)).

### A prototype needs index.html at the bin root

**A prototype needs `index.html` at the bin root** for the bin base URL to show the page: the
base path serves `index.html` only when that exact path exists in the bin, and otherwise
answers with the directory listing. `sandbin-client upload prototype/index.html` therefore
gives a listing at `{{ .Instance.URL }}/abc1234/` and the page itself at
`{{ .Instance.URL }}/abc1234/prototype/index.html`.
