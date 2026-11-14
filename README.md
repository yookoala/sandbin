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

## Quick example

```sh
go build -o sandbin ./cmd/sandbin
SANDBIN_API_KEY=your-key ./sandbin serve

BIN=$(curl -s -X POST -H "X-API-Key: your-key" http://localhost:8080/)   # -> /abc1234/
curl -X PUT -H "X-API-Key: your-key" -d '<h1>Hello</h1>' "http://localhost:8080${BIN}index.html"
curl http://localhost:8080${BIN}                                         # -> the page

go build -o sandbin-client ./cmd/sandbin-client
export SANDBIN_URL=http://localhost:8080 SANDBIN_API_KEY=your-key
sandbin-client upload prototype/index.html                               # -> created + bin URL
```

## Documentation

Usage docs come in two sets, each with its own summary and content table:

- **[docs/usages/sandbin.md](docs/usages/sandbin.md)** — the server: running it, the HTTP API,
  the API key lockout, running it with podman, managing blocked addresses.
- **[docs/usages/sandbin-client.md](docs/usages/sandbin-client.md)** — the client: uploading and
  deleting previews, pointing it at a server, and the agent skill + one-line installer.

An agent learns the client from the served skill itself: fetch
`GET /skills/sandbin-preview/SKILL.md` from a running server, or install the skill with
`curl -fsSL http://localhost:8080/skills/sandbin-preview/install.sh | bash`.

# License

This software is licensed under the MIT License. You can find a copy of the license [here](LICENSE.md).
