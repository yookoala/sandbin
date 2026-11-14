# sandbin-client — usage documentation

`sandbin-client` is the preview client for an agent that just wrote a prototype: one `created`
or `updated` line per file, the bin URL last. It speaks the API documented in
[sandbin.md](sandbin.md), so that set — in particular the [API key lockout](sandbin-lockout.md)
rules — applies to every run.

| Page | What it covers |
|---|---|
| [Upload previews with sandbin-client](sandbin-client-upload.md) | the `upload`/`delete`/`describe` verbs, stdin and `--archive` folder uploads, hand-built archives, `--title`/`--description` metadata, deleting files and folders, `.sandbin.yaml` bin reuse, the no-retry rule |
| [Point sandbin-client at a server](sandbin-client-config.md) | the `init` wizard, the key check against the server, the re-initialization guard and `--reinit`, settings precedence flag > env > file |
| [Give the skill to your agent](sandbin-client-skill.md) | the served `SKILL.md`, the one-line installer and what it does (including its single API-key question) |
