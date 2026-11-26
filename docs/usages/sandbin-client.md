# sandbin-client — usage documentation

`sandbin-client` is the preview client for an agent that just wrote a prototype: one `created`
or `updated` line per file, the bin URL last. It speaks the API documented in
[sandbin.md](sandbin.md), so that set — in particular the [IP Address Blocking](sandbin-ip-blocking.md)
rules — applies to every run. A running server hosts this whole set under `/docs`: the hub at
`/docs/`, this summary at `/docs/sandbin-client.html`, and each page below at its own
`/docs/<name>.html`.

| Page | What it covers |
|---|---|
| [Building and Config](sandbin-client-init.md) | installing the client with `go install`, building it from source (clone from GitHub, `make build` versus a plain `go build`), the `init` wizard and its key check, the re-initialization guard and `--reinit`, `.sandbin.yaml`, settings precedence flag > env > file > refusal |
| [Upload previews with sandbin-client](sandbin-client-upload.md) | the `upload`/`delete`/`describe`/`list` verbs one section each, the settings every verb reads and the refusals that name their fix, bin finding and `.sandbin.yaml` reuse, stdin and `--archive` folder uploads, hand-built archives, `--title`/`--description` metadata, deleting files and folders, the no-retry rule, `index.html` at the bin root |
| [Using this instance with Agent skill](sandbin-client-skill.md) | the general install one-liner into the skills folder, its pinned client build and the `init` it runs, and the harness-specific install through `--dest` and the landing-page picker |
