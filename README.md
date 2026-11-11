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

Create a bin, then PUT files under it. `$BIN` already ends in `/`, so write `${BIN}name` and
not `$BIN/name`, which asks for a `//` path the server rejects:

```sh
BIN=$(curl -s -X POST -H "X-API-Key: your-key" http://localhost:8080/)   # -> /abc1234/
curl -X PUT -H "X-API-Key: your-key" -d '<h1>Hello</h1>' "http://localhost:8080${BIN}index.html"
curl -X PUT -H "X-API-Key: your-key" -d '# Notes' "http://localhost:8080${BIN}notes.md"
curl -X PUT -H "X-API-Key: your-key" --data-binary "$(make 2>&1)" "http://localhost:8080${BIN}build.ansi"
```

Read them back. The bin base path serves `index.html` when there is one, otherwise a
directory listing; a `.ansi` file comes back as rendered HTML:

```sh
curl http://localhost:8080${BIN}
curl http://localhost:8080${BIN}notes.md
curl http://localhost:8080${BIN}build.ansi
```

Give a bin its own password and send it as Basic Auth (user name = bin hash) on every
GET and PUT below it:

```sh
PBIN=$(curl -s -X POST -H "X-API-Key: your-key" -H "X-Bin-Password: sesame" http://localhost:8080/)
curl -u "${PBIN//\//}:sesame" http://localhost:8080${PBIN}notes.md
```

PUT bodies are capped at 1 MiB. `403` is a bad API key, `401` a missing bin password,
`404` an unknown bin or path, `405` an unsupported method, `400` a rejected path,
`413` an oversized body, `429` an address blocked for bad API keys.

## API key lockout

`POST /` and `PUT /{hash}/{path}` are the key-checked requests. A wrong or missing `X-API-Key`
blocks that address from both for 30 minutes; every further failure adds 30 minutes, capped at
24 hours per block, and the failure count survives a restart. While blocked, a key-checked
request answers `429` with a `Retry-After` (the remaining wait in seconds) and counts as another
failure, so hammering only lengthens the wait. `GET` is never charged and never blocked: a
blocked address can still read public bins. Wrong bin passwords (`401`) are not failures here.

`X-Forwarded-For` is believed only when the connection itself arrives from loopback, a private
or a link-local address, so a reverse proxy must run on the same machine or network to be
honoured, and only its rightmost entry is charged. Anyone reaching the port over a public route
is charged by their own socket address and cannot frame a third party, but a client connected
from loopback or the local network can name an arbitrary address in that header and get that
address blocked.

## Run it with podman

Build the image (hermetic two stages: `golang:1.26-alpine` builds, `alpine:3.22` runs; the
host needs nothing but podman). Build it under exactly this tag, because the quadlet unit
looks for it and podman tries to pull a registry image otherwise:

```sh
podman build -t localhost/sandbin:latest .
```

Podman warns `HEALTHCHECK is not supported for OCI image format` on every build: its default
image format drops that line. The probes below are the podman-native form, and podman runs
them. Add `--format docker` to the build instead if you want the image to carry it.

Ad-hoc run, with a named volume holding the sqlite file, the key from a `0600` env file, and
the health probe:

```sh
podman run -d --name sandbin -p 127.0.0.1:8080:8080 -v sandbin-data:/data \
  --env-file ~/.config/sandbin/sandbin.env \
  --health-cmd "wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1" \
  --health-interval=30s --health-start-period=5s --health-retries=3 \
  localhost/sandbin:latest
podman inspect --format '{{.State.Health.Status}}' sandbin   # probed every 30s, healthy on a healthy server
```

As a systemd user service through podman quadlet:

```sh
mkdir -p ~/.config/containers/systemd ~/.config/sandbin
cp deploy/sandbin.container ~/.config/containers/systemd/
install -m 600 deploy/sandbin.env.example ~/.config/sandbin/sandbin.env   # then edit replace-me
sed -i "s#^EnvironmentFile=.*#EnvironmentFile=$HOME/.config/sandbin/sandbin.env#" ~/.config/containers/systemd/sandbin.container
systemctl --user daemon-reload
systemctl --user start sandbin
```

The `sed` is not optional. `deploy/sandbin.container` points `EnvironmentFile=` at
`/etc/sandbin/sandbin.env`, meant for a system install, and quadlet resolves a relative or
`~`-prefixed path against the unit's own directory, so an unedited `~/.config/...` value
restart-loops. For a system install, keep the path as shipped and create `/etc/sandbin/sandbin.env`
with mode 0600 instead.

The published port is loopback-only (`PublishPort=127.0.0.1:8080:8080`) because this is a
preview tool; edit the unit to `8080:8080` to expose it on the network. The API key never
appears in the container command line, but it is visible in `podman inspect` output
(`Config.Env`), so treat the env file like a password.

