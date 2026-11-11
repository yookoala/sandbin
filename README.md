# Review Bin

A simple PUT/GET server for CLI agents in sandbox to submit HTML / ANSI mock up previews
or simple prototyping.

With a fixed pre-shared API key, a client may use POST to create a bin (for a collection
of preview pages) with a randomly assigned hash path. Then a client may PUT markdown /
HTML / ANSI text to subpaths of the bin.

Client may specify additional password protections (Basic Auth) to the bin it created.

The base path of the bin, if not specified with an index.html, will automatically has
directory listing for the items in it.

All contents are stored in a single sqlite file.

## Usage

Build and run:

```sh
go build -o sandbin ./cmd/sandbin
SANDBIN_API_KEY=your-key ./sandbin
```

`--api-key` (default `$SANDBIN_API_KEY`), `--db` (default `sandbin.db`) and `--addr`
(default `:8080`) are the only settings; the server exits when no API key is configured.

Create a bin, then PUT files under it:

```sh
BIN=$(curl -s -X POST -H "X-API-Key: your-key" http://localhost:8080/)   # -> /abc1234/
curl -X PUT -H "X-API-Key: your-key" -d '<h1>Hello</h1>' "http://localhost:8080$BIN/index.html"
curl -X PUT -H "X-API-Key: your-key" -d '# Notes' "http://localhost:8080$BIN/notes.md"
curl -X PUT -H "X-API-Key: your-key" --data-binary "$(make 2>&1)" "http://localhost:8080$BIN/build.ansi"
```

Read them back. The bin base path serves `index.html` when there is one, otherwise a
directory listing; a `.ansi` file comes back as rendered HTML:

```sh
curl http://localhost:8080$BIN/
curl http://localhost:8080$BIN/notes.md
curl http://localhost:8080$BIN/build.ansi
```

Give a bin its own password and send it as Basic Auth (user name = bin hash) on every
GET and PUT below it:

```sh
PBIN=$(curl -s -X POST -H "X-API-Key: your-key" -H "X-Bin-Password: sesame" http://localhost:8080/)
curl -u "${PBIN//\//}:sesame" http://localhost:8080$PBIN/notes.md
```

PUT bodies are capped at 1 MiB. `403` is a bad API key, `401` a missing bin password,
`404` an unknown bin or path, `405` an unsupported method, `400` a rejected path,
`413` an oversized body.
