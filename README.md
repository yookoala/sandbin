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
SANDBIN_API_KEY=your-key ./sandbin serve
```

Every invocation names its verb: `serve` runs the server, `ip list`, `ip unblock <ip>` and
`ip clear` inspect and lift the API key blocks (see Managing blocked addresses), and `help`
prints the usage. A bare `sandbin` or an unknown verb prints that usage on stderr and exits 1.

`--api-key` (default `$SANDBIN_API_KEY`), `--db` (default `sandbin.db`) and `--addr`
(default `:8080`) are `serve`'s core settings; `--trusted-proxy` and `--trusted-proxy-file`
(defaults `$SANDBIN_TRUSTED_PROXIES` and `$SANDBIN_TRUSTED_PROXIES_FILE`, see Trusted
proxies) optionally name the proxies the server believes. The server exits when no API key
is configured or a trusted proxy value cannot be read.

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
`413` an oversized body, `429` an address blocked for bad API keys. A `HEAD` answers `405`:
read the headers of a `GET` with `curl -s -D - -o /dev/null <url>`.

## Upload previews with sandbin-client

`cmd/sandbin-client` is this API shaped for an agent that just wrote a prototype: one
command, one `created` or `updated` line per file on stdout, and the bin base URL last for
the human to open.

```sh
go build -o sandbin-client ./cmd/sandbin-client
```

`SANDBIN_URL` (default `http://localhost:8080`), `SANDBIN_API_KEY` and
`SANDBIN_BIN_PASSWORD` are the settings; `--url` and `--api-key` override their variables,
and a missing key is a startup error that makes no request at all. `SANDBIN_BIN_PASSWORD`
has no flag on purpose, so a bin password never lands on a command line: when it is set it
goes out as `X-Bin-Password` on the create and as Basic Auth (user name = bin hash) on every
upload. `--bin <hash>` uploads into a bin you already have, `--new` forces a fresh one, and
`--name <path>` names piped-in stdin; with nothing piped in it is an error, not an empty
upload. A name that starts with `-` goes after `--` (`sandbin-client -- -draft.html`), and
`--help` prints the flag list on stderr.

```sh
export SANDBIN_URL=http://localhost:8080 SANDBIN_API_KEY=your-key
sandbin-client prototype/index.html prototype/app.js prototype/style.css
# created http://localhost:8080/abc1234/prototype/index.html
# created http://localhost:8080/abc1234/prototype/app.js
# created http://localhost:8080/abc1234/prototype/style.css
# http://localhost:8080/abc1234/          <- hand this to a human
make 2>&1 | sandbin-client --name build.ansi
# updated http://localhost:8080/abc1234/build.ansi
```

The bin is remembered in `.sandbin.json` in the working directory:

```json
{ "url": "http://localhost:8080", "bin": "abc1234", "protected": false }
```

`--bin` beats that file, a readable file whose `url` matches the url in use comes next, and
otherwise the client creates a bin and writes it: mode `0600`, renamed in from a temp file in
the same directory, never holding the API key or the bin password, and gitignored. A corrupt
or unreadable `.sandbin.json` is an error naming the file, never a silent second bin for the
same prototype. Change the url in use and the file simply stops matching: the client makes a
new bin for the new url, and the old bin is orphaned — its files stay readable at the old
URL, but nothing in this directory points at them any more. Names are checked before anything
is sent, so an absolute path, a `..` segment or a file over 1 MiB fails locally instead of
costing a request.

**Do not retry a failing `sandbin-client` from a script.** It makes exactly one request per
operation and never retries, because a wrong or missing API key blocks the client's address
from `POST` and `PUT` for 30 minutes, every further failure adds 30 minutes (to a 24 hour
cap per block), and an attempt while already blocked counts as another failure too. A loop
that reruns the client after a `403` only digs that wait deeper; read the `429` and its
`Retry-After`, fix the key, and wait out the stated seconds. `GET` is never charged or
blocked, so a blocked address can still open what it already published.

**A prototype needs `index.html` at the bin root** for the bin base URL to show the page: the
base path serves `index.html` only when that exact path exists in the bin, and otherwise
answers with the directory listing. `sandbin-client prototype/index.html` therefore gives a
listing at `http://localhost:8080/abc1234/` and the page itself at
`http://localhost:8080/abc1234/prototype/index.html`.

## API key lockout

`POST /` and `PUT /{hash}/{path}` are the key-checked requests. A wrong or missing `X-API-Key`
blocks that address from both for 30 minutes; every further failure adds 30 minutes, capped at
24 hours per block, and the failure count survives a restart. While blocked, a key-checked
request answers `429` with a `Retry-After` (the remaining wait in seconds) and counts as another
failure, so hammering only lengthens the wait. `GET` is never charged and never blocked: a
blocked address can still read public bins. Wrong bin passwords (`401`) are not failures here.

`X-Forwarded-For` is believed only when the connection itself arrives from a proxy the server
trusts, and only its rightmost entry is charged. Loopback, private and link-local addresses are
trusted with no configuration, so a reverse proxy on the same machine or network is honoured
exactly as before; `--trusted-proxy` only adds to that list (see Trusted proxies). Anyone
reaching the port over a route the server does not trust is charged by their own socket address
and cannot frame a third party, but a client whose connection arrives through a trusted proxy
can name an arbitrary address in that header and get that address blocked.

That freedom cuts both ways on every trusted socket, loopback included: a client allowed to name
itself can also send a different valid address per attempt and have each failure charged to a
fresh identity, so it never accumulates a penalty of its own. The defence is configuration, not
code: trust only the sockets and ranges you really front with a proxy, because the default
loopback, private and link-local trust is what makes the trick available wherever clients share a
network.

### Trusted proxies

An origin behind a public proxy such as Cloudflare sees the proxy's own public address as the
socket and the real client only in `X-Forwarded-For`. `--trusted-proxy` (default
`$SANDBIN_TRUSTED_PROXIES`) takes a comma-separated list of proxy IPs and CIDRs whose header is
believed the way loopback's is:

```sh
./sandbin serve --api-key your-key --trusted-proxy 203.0.113.7,173.245.48.0/20
```

`--trusted-proxy-file` (default `$SANDBIN_TRUSTED_PROXIES_FILE`) reads the same entries one per
line, ignoring blank lines and `#` comments, for lists long enough to deserve a file, such as
the published Cloudflare ranges. The file list and the comma list are merged, file first:

```sh
cat > ~/.config/sandbin/proxies.txt <<'EOF'
# Cloudflare IPv4 ranges (example entries; fetch the current list from Cloudflare)
173.245.48.0/20
103.21.244.0/22
103.22.200.0/22
EOF
./sandbin serve --api-key your-key --trusted-proxy-file ~/.config/sandbin/proxies.txt
```

A bad entry or an unreadable file makes the server refuse to start, naming the offending value,
the way a missing API key does. In the quadlet flow these settings come from the same `0600`
env file as the key, as `SANDBIN_TRUSTED_PROXIES=203.0.113.7,173.245.48.0/20`; use
`SANDBIN_TRUSTED_PROXIES_FILE` there only if the file itself is mounted into the container.

Trusting a public range means any connection whose source address is inside that range gets
header authority: whoever can open a socket from such an address can name whichever address the
penalty lands on. `0.0.0.0/0` (every IPv4 connection) and `::/0` (every IPv6 connection) are
accepted only as a deliberate operator choice and enable that framing for the whole internet;
they are never defaults, and loopback, private and link-local trust needs no configuration.

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
the health probe. It needs no command of its own: the image's CMD is `serve --db
/data/sandbin.db`.

```sh
podman run -d --name sandbin -p 127.0.0.1:8080:8080 -v sandbin-data:/data \
  --env-file ~/.config/sandbin/sandbin.env \
  --health-cmd "wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1" \
  --health-interval=30s --health-start-period=5s --health-retries=3 \
  localhost/sandbin:latest   # runs serve --db /data/sandbin.db
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

## Managing blocked addresses

`ip` commands read the sqlite file itself, so whoever owns that file can see and undo the
lockout blocks. They make no network request and need no API key: whoever can read the `--db`
file can lift blocks from it, and protecting that file is the control. Nothing about `serve`
loosens — it still refuses to start without an API key, and an `ip` run never reaches the
running server. Each verb takes `--db` (default `sandbin.db`, the same file `serve` opens):

```sh
./sandbin ip list                    # every row of the block table
./sandbin ip list --json             # the same rows as a json array
./sandbin ip unblock 203.0.113.7     # delete one address's block
./sandbin ip clear                   # delete every block
```

Flags go before the address: `./sandbin ip unblock --db /data/sandbin.db 203.0.113.7`.

`list` prints `IP`, `FAILURES`, `BLOCKED UNTIL`, `REMAINING` and `LAST FAILURE`. `REMAINING`
is worked out while the command runs and reads `expired` once `blocked_until` is past; expired
rows are listed on purpose, because their failure count still shapes the next penalty. An empty
table prints `no blocked addresses` and exits 0. `--json` prints the same rows as an array of
`ip`, `failures`, `blocked_until` and `updated_at` (`[]` when there is no row), and it belongs
to `list` alone: `unblock` and `clear` refuse it.

`unblock` deletes the whole row, so the failure count goes with it: the next wrong key from
that address starts again at 30 minutes instead of resuming the escalation it had reached. Its
argument is spelled the way the server spells an address it charges, so `::ffff:203.0.113.7`,
`fe80::1%eth0` and `2001:0DB8::1` all reach the key the server stored. An address with no block
is an error naming it (`no block for 203.0.113.7`, exit 1), which is how a typo shows up.
`clear` deletes every row and prints `cleared N blocks`; `N=0` is fine.

They are safe to run next to a live server: `openStore`'s `busy_timeout(5000)` queues the
management write behind whatever transaction the server is in instead of failing with
`SQLITE_BUSY`. In the container the sqlite file lives in the `sandbin-data` volume at
`/data/sandbin.db`, so the same verbs run through the image:

```sh
podman run --rm localhost/sandbin ip list --db /data/sandbin.db
```

