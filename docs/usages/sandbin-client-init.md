# Building and Config

`sandbin-client` needs two things before a project can upload: the binary — `cmd/sandbin-client`
in this repository, one Go program — and one `init` run that settles the directory's connection
settings. This page covers both: two ways to get the
binary, then `init` — it collects a url and a key, checks them against the server before
writing anything, and remembers them in `.sandbin.yaml` next to the work. What the client then
does with them is [Working with sandbin-client](sandbin-client-upload.md).

## Installing with `go install`

The short way needs no source code at all: the Go module proxy serves any tagged release, and
`go install` builds it for you:

```sh
go install github.com/yookoala/sandbin/cmd/sandbin-client@v0.4.3
```

That lands as `sandbin-client` under `$GOPATH/bin`. Releases are tags on the repository's
`main` branch and `v0.4.3` is the newest, so naming a version in that command names a release.
A proxy-downloaded module zip carries no git history to derive a version from, so the tag
itself travels inside the zip — this binary's `--version` names the tag alone, `v0.4.3`. A
running server's one-line skill installer is this same way packaged up: it runs the pinned
`go install` for you ([Using this instance with Agent skill](sandbin-client-skill.md)).

## Building from Source

The source way needs Go 1.26.10 or newer (the `go` line of `go.mod`) and the source tree.

### Download from GitHub

The repository is [github.com/yookoala/sandbin](https://github.com/yookoala/sandbin):

```sh
git clone https://github.com/yookoala/sandbin
cd sandbin
```

`main` is the working branch, and every release is a tag on it (`v0.4.3` is the newest), so a
named release of the source is one `git checkout v0.4.3` away.

### Build from a checkout

From the clone, one command builds both binaries:

```sh
make build
```

That stamps `dist/sandbin` and `dist/sandbin-client` with the version string
`scripts/version.sh` computes from the checkout — the tag plus the commit, `v0.4.3 (5e12292)`
for a tagged release. `VERSION='anything' make build` overrides the stamp, and the rest of the
make workflow (`test`, `container`, `release`, `version`) is the same one
[Running the server](sandbin-serve.md) describes. A plain single-command build works too:

```sh
go build -o sandbin-client ./cmd/sandbin-client
```

Its version string reports `dev (hash)` — an honest answer, since that build never asked git
which tag it sits on.

### Check the binary

```sh
sandbin-client --version
```

A `make build` of the v0.4.3 checkout answers:

```text
sandbin-client version v0.4.3 (5e12292)
```

and a `go install ...@v0.4.3` binary answers with the tag alone, `v0.4.3`.

## init

`init` settles one project. On a terminal it prompts for whatever is missing; Enter on the url
prompt keeps the shown default:

```sh
sandbin-client init
```

The wizard asks for the url first, then the key, and answers with the url it settled on. The
prompt shows the built-in default `http://localhost:8080` in brackets — right only when the
server you mean is listening there — so this example types the instance's own address at the
prompt, and the prompt echoes what you type, key included, the way any terminal prompt does;
no client output ever prints the key on its own. The check before writing anything is a
`POST /api/key` with the candidate key: a wrong key answers `server rejected this API key`
and a locked-out address answers `server is blocking this address: too many bad API keys`, and
either way init writes nothing — a project never holds a setup that cannot upload.

```text
sandbin_url [http://localhost:8080]: {{ .Instance.URL }}
api_key: your-key
initialized {{ .Instance.URL }}
```

### Without a terminal

In a script, a CI job or an agent nobody is there to answer, so the same verb reads flags and
variables; a missing url falls back to `http://localhost:8080` and a missing key is a refusal
naming `--api-key`, `SANDBIN_API_KEY` and the prompt:

```sh
SANDBIN_URL={{ .Instance.URL }} SANDBIN_API_KEY=your-key sandbin-client init
```

```text
initialized {{ .Instance.URL }}
```

### Re-initializing a project

Re-running `init` in a project whose `.sandbin.yaml` already holds both a url and a key is an
explicit act. At the terminal it warns and asks:

```sh
sandbin-client init
```

```text
sandbin-client: warning: .sandbin.yaml is already initialized for {{ .Instance.URL }}
re-initialize? [y/N]: n
left the existing initialization for {{ .Instance.URL }} untouched
```

`y` or `yes` continues the wizard as before, and anything else — bare Enter included — prints
that `left ... untouched` line and exits 0 with nothing requested and nothing rewritten.
Without a terminal the run refuses with `sandbin-client: .sandbin.yaml is already initialized
for {{ .Instance.URL }}: pass --reinit to overwrite it, or delete the file to start over`.
Flags and environment are not consent: a fresh `--url` or `--api-key` is exactly the overwrite
the guard asks about. `--reinit` is the consent a script or CI job passes, and on a directory
that is not initialized it is a harmless no-op — here re-consenting the instance's own url with
a fresh key:

```sh
sandbin-client init --reinit --api-key your-key
```

```text
initialized {{ .Instance.URL }}
```

A confirmed re-init re-verifies and rewrites, which is how a key or a server gets changed,
keeps `bin` and `protected` while the url stays the same, and with unchanged inputs the file
comes out byte-identical.

### The `.sandbin.yaml` file

The file `init` writes, in the project's working directory, mode `0600` and renamed in from a
temp file in the same directory:

```yaml
sandbin_url: {{ .Instance.URL }}
api_key: your-key
bin: abc1234
protected: false
```

`init` makes sure the directory's `.gitignore` names `.sandbin.yaml` — creating the file if it
is absent, appending the line if it is missing, and leaving an existing `.gitignore` exactly as
written if the line is already there. `init` is the only run that writes an `api_key`, at your
explicit request; an ordinary upload run writes `sandbin_url`, `bin` and `protected` and keeps
whatever key the file already holds. The bin password stays env-only, as before. The state
file earlier versions of this client kept as JSON is gone without migration or fallback: run
`init` once in a project that has one and delete the old file.

### Settings precedence

Settings resolve flag first, environment second, the file third, the built-in default last:
`--url` > `SANDBIN_URL` > `sandbin_url` > `http://localhost:8080`, and `--api-key` >
`SANDBIN_API_KEY` > `api_key`. The environment beating the file is what lets a CI job or an
agent override a project without editing it.
