---
name: Sandbin preview
description: Upload files or whole folders to a sandbin server so a human can open preview URLs; use when asked to publish or preview HTML pages, ANSI output or prototype files.
---

# Sandbin preview

`sandbin-client` uploads files to a sandbin server and prints preview URLs. Replace
`https://sandbin.example` and `your-key` with the real server URL and key; `/<bin>/` is the created bin.

## When to use this skill

You wrote HTML pages, ANSI output or prototype files and a human needs to open them in a browser.

## First-time setup in this directory

```sh
sandbin-client init --url https://sandbin.example --api-key your-key
# initialized https://sandbin.example
```

`init` checks the key against the server first: a wrong key answers `server rejected this API key` and
writes nothing. It saves `sandbin_url`, `api_key`, `bin` and `protected` in a mode-`0600` `.sandbin.yaml`
the `.gitignore` covers. Resolution order is flag > environment > file: `--url` > `SANDBIN_URL` > `sandbin_url`,
`--api-key` > `SANDBIN_API_KEY` > `api_key`.

## Uploading files

```sh
sandbin-client page.html style.css
# created https://sandbin.example/<bin>/page.html
# created https://sandbin.example/<bin>/style.css
# https://sandbin.example/<bin>/
```

New paths answer `created`, replaced ones `updated`, and the bin base URL comes last — hand that line to a
human. Later runs here reuse the bin in `.sandbin.yaml`; `--new` forces a fresh one, `--bin <hash>` names one.

## Uploading a whole folder

```sh
sandbin-client --archive hello
# extracted 2 files into https://sandbin.example/<bin>/hello.tar.gz
# https://sandbin.example/<bin>/
```

The folder uploads as one tar.gz the server unpacks: `hello/index.html` lands as `/<bin>/hello/index.html`, an
ordinary listed bin file from then on. The `extracted` line names the upload, not the landed files. Hand the human
`https://sandbin.example/<bin>/` (the bin's listing) or a file such as `https://sandbin.example/<bin>/hello/index.html`; the bin base serves a page only when an `index.html` sits at its root.

## Piping stdin, keeping an archive raw

```sh
make 2>&1 | sandbin-client --name build.ansi
# created https://sandbin.example/<bin>/build.ansi
sandbin-client --no-extract --archive hello
# created https://sandbin.example/<bin>/hello.tar.gz
```

Piped stdin needs `--name` to say what it uploads; a `.ansi` file comes back rendered as HTML. `--no-extract` stores archive-shaped bodies as one file instead of unpacking them.

## Saying what the bin holds

```sh
sandbin-client --title 'Button grid' --description 'A grid of pressable buttons'
# updated /<bin>/
# https://sandbin.example/<bin>/
sandbin-client --describe
# title: Button grid
# description: A grid of pressable buttons
```

A run with only the metadata flags uploads nothing. `--describe` only reads and needs no API key.

## Caps and what a refusal looks like

- A file over 1 MiB is refused locally before any request: `sandbin-client: big.html: body over 1 MiB`; piped stdin over 1 MiB is `stdin over 1 MiB`.
- `--archive` checks the folder locally first: `hello/big.bin is over 1 MiB` (per entry), `folder has over 1000 files`, `folder unpacks over 32 MiB`, `archive is over 1 MiB`, `folder contains no files`.
- Hand-built archive uploads obey the server's caps: `413 extracted file over 1 MiB`, `413 archive has over 1000 files`, `413 archive unpacks over 32 MiB`, `413 body over 1 MiB`; each refusal leaves the bin exactly as it was.
- A wrong API key blocks this address from writes for 30 minutes, every further failure adds 30 more, and an attempt while already blocked counts as one too. Never rerun a failing `sandbin-client` in a loop; `GET` is never charged.
