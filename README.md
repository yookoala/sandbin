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

## Run it with podman

Build the image (hermetic two stages: `golang:1.26-alpine` builds, `alpine:3.22` runs; the
host needs nothing but podman):

```sh
podman build -t localhost/sandbin:latest .
```

Ad-hoc run, with a named volume holding the sqlite file and the key from a `0600` env file:

```sh
podman run -d --name sandbin -p 127.0.0.1:8080:8080 -v sandbin-data:/data --env-file ~/.config/sandbin/sandbin.env localhost/sandbin:latest
```

As a systemd user service through podman quadlet:

```sh
mkdir -p ~/.config/containers/systemd ~/.config/sandbin
cp deploy/sandbin.container ~/.config/containers/systemd/
install -m 600 deploy/sandbin.env.example ~/.config/sandbin/sandbin.env   # then edit replace-me
systemctl --user daemon-reload
systemctl --user start sandbin
```

`deploy/sandbin.container` points `EnvironmentFile=` at `/etc/sandbin/sandbin.env` for a
system install; for a rootless install change it to `~/.config/sandbin/sandbin.env`.

The published port is loopback-only (`PublishPort=127.0.0.1:8080:8080`) because this is a
preview tool; edit the unit to `8080:8080` to expose it on the network. The API key never
appears in the container command line, but it is visible in `podman inspect` output
(`Config.Env`), so treat the env file like a password.

