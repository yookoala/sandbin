---
name: Sandbin preview
description: Upload files or whole folders to the sandbin server at {{.Base}} so a human can open preview URLs; use when asked to publish or preview HTML pages, ANSI output or prototype files.
---

# Sandbin preview

`"$SKILL/sandbin-client"` always names its verb — `upload` sends files and folders to the sandbin
server at {{.Base}}, `delete` takes them back out again, `describe` reads the bin's metadata and
`init` settles this directory's settings — and prints preview URLs. `$SKILL` is this skill's directory — the folder holding this SKILL.md, which OpenCode names when it loads the skill; start every shell command for this skill by setting `SKILL=<that directory>`. Replace `your-key` with the real API key; `/<bin>/` is a created bin.
A bin holding a `404.html` answers every unmatched path under the bin with that file, so a
one-page app can serve its own routes.

## The client binary lives in this skill's directory

`sandbin-client` belongs next to this file — the installer put it there when Go was available. If it is missing, build it there (needs Go; an older Go fetches the module's toolchain itself):

```sh
GOBIN="$SKILL" go install github.com/yookoala/sandbin/cmd/sandbin-client@latest
```

Always invoke `"$SKILL/sandbin-client"` by that full path — never a bare `sandbin-client` found on PATH.

## First-time setup in this directory

```sh
"$SKILL/sandbin-client" init --url {{.Base}} --api-key your-key
# initialized {{.Base}}
```

Get `your-key` from the human; `--api-key` may be left out at an interactive terminal, where `init` prompts instead, but a non-terminal session must pass it or set `SANDBIN_API_KEY` — otherwise the run stops with `no API key: pass --api-key, set SANDBIN_API_KEY or answer the init prompt` and writes nothing.

`init` checks the key against the server first: a wrong key answers `server rejected this API key` and writes nothing. It saves `sandbin_url`, `api_key`, `bin` and `protected` in a mode-`0600` `.sandbin.yaml` the `.gitignore` covers. Resolution order is flag > environment > file: `--url` > `SANDBIN_URL` > `sandbin_url`, `--api-key` > `SANDBIN_API_KEY` > `api_key`. An already initialized directory (the file holds both `sandbin_url` and `api_key`) refuses a plain `init` with `.sandbin.yaml is already initialized for <url>: pass --reinit to overwrite it, or delete the file to start over` — pass `--reinit` when the overwrite is intended, or delete `.sandbin.yaml` to start over; at an interactive terminal `init` asks `re-initialize? [y/N]: ` instead. A stdin redirected from `/dev/null` still reads as a terminal but answers that question with nothing: the run cancels as `no` and leaves the file untouched, so a script that means to re-initialize must pass `--reinit` deliberately.

## Uploading files

```sh
"$SKILL/sandbin-client" upload page.html style.css
# created {{.Base}}/<bin>/page.html
# created {{.Base}}/<bin>/style.css
# {{.Base}}/<bin>/
```

New paths answer `created`, replaced ones `updated`, and the bin base URL comes last — hand that line to a human. Later runs here reuse the bin in `.sandbin.yaml`; `--new` forces a fresh one, `--bin <hash>` names one.

## Uploading a whole folder

```sh
"$SKILL/sandbin-client" upload --archive hello
# extracted 2 files into {{.Base}}/<bin>/hello.tar.gz
# {{.Base}}/<bin>/
```

The folder uploads as one tar.gz the server unpacks: `hello/index.html` lands as `/<bin>/hello/index.html`, an ordinary listed bin file from then on; the `extracted` line names the upload, not the landed files. Hand the human `{{.Base}}/<bin>/` (the bin's listing) or a file such as `{{.Base}}/<bin>/hello/index.html`; the bin base serves a page only when an `index.html` sits at its root.

## Piping stdin, keeping an archive raw

```sh
make 2>&1 | "$SKILL/sandbin-client" upload --name build.ansi
# created {{.Base}}/<bin>/build.ansi
"$SKILL/sandbin-client" upload --no-extract --archive hello
# created {{.Base}}/<bin>/hello.tar.gz
```

Piped stdin needs `--name` to say what it uploads; a `.ansi` file comes back rendered as HTML. `--no-extract` stores archive-shaped bodies as one file instead of unpacking them.

## Deleting files and folders

```sh
"$SKILL/sandbin-client" delete build.ansi --folder drafts
# deleted {{.Base}}/<bin>/build.ansi
# deleted {{.Base}}/<bin>/drafts/one.md
# deleted {{.Base}}/<bin>/drafts/two.md
# {{.Base}}/<bin>/
```

File arguments name files and a repeatable `--folder` names a folder and takes everything under it; flags may be written before or after the file arguments. A delete run reuses this directory's bin and never makes one, and it never uploads: `--name`, `--archive`, `--new` and `--no-extract` are not delete flags, so a run written with them is a usage error that makes no request. A path the bin does not hold is refused with `delete "missing.txt": 404 unknown path`.

## Saying what the bin holds

```sh
"$SKILL/sandbin-client" upload --title 'Button grid' --description 'A grid of pressable buttons'
# updated /<bin>/
# {{.Base}}/<bin>/
"$SKILL/sandbin-client" describe
# title: Button grid
# description: A grid of pressable buttons
```

An `upload` with only the metadata flags uploads nothing. `describe` only reads and needs no API key.

## Caps and what a refusal looks like

- A file over 1 MiB is refused locally before any request: `sandbin-client: big.html: body over 1 MiB`; piped stdin over 1 MiB is `stdin over 1 MiB`.
- `--archive` checks the folder locally first: `hello/big.bin is over 1 MiB` (per entry), `folder has over 1000 files`, `folder unpacks over 32 MiB`, `archive is over 1 MiB`, `folder contains no files`.
- Hand-built archive uploads obey the server's caps: `413 extracted file over 1 MiB`, `413 archive has over 1000 files`, `413 archive unpacks over 32 MiB`, `413 body over 1 MiB`; each refusal leaves the bin exactly as it was.
- A wrong API key blocks this address from writes for 30 minutes, every further failure adds 30 more, and an attempt while already blocked counts as one too. Never rerun a failing `sandbin-client` in a loop; `GET` is never charged.
