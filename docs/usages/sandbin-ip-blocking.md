# IP Address Blocking

A Sandbin server blocks addresses that present bad API keys, and hands the operator the tools
to inspect and lift those blocks. This page covers the whole machinery: why and how a block is
charged, which address a request charges, the proxy settings that decide that, and the
commands that manage the block table.

## How the block works

`POST /api/bins`, `GET /api/bins`, `PATCH /api/bins/{hash}`, `DELETE /api/bins/{hash}`,
`PUT /{hash}/{path}`, `DELETE /{hash}/{path}` and `POST /api/key` are the key-checked
requests. A wrong or missing `X-API-Key` blocks that address from all of them for 30 minutes;
every further failure adds 30 minutes, capped at 24 hours per block, and the failure count
survives a restart. While blocked, a key-checked request answers `429` with a `Retry-After`
(the remaining wait in seconds) and the JSON error envelope body `too many bad API keys`, and
counts as another failure, so hammering only lengthens
the wait. Key-less bin `GET`s are never charged and never blocked: a blocked address can still
read public bins. Wrong bin passwords (`401`) are not failures here.

`POST /api/key` is the pure key check `sandbin-client init` runs before saving a key: it
answers `200` with `{"status":"ok"}` for a working key or the `403 invalid API key` envelope,
charges every failure exactly like
a failed bin create does, creates nothing, and answers every method but `POST` with the `405
envelope method not allowed`, so no `GET` can probe a key for free.

## Which address is charged

`X-Forwarded-For` is believed only when the connection itself arrives from a proxy the server
trusts, and only its rightmost entry is charged. Loopback, private and link-local addresses
are trusted with no configuration, so a reverse proxy on the same machine or network is
honoured exactly as before; `--trusted-proxy` only adds to that list (see [Trusted
proxies](#trusted-proxies)). Anyone reaching the port over a route the server does not trust
is charged by their own socket address and cannot frame a third party, but a client whose
connection arrives through a trusted proxy can name an arbitrary address in that header and
get that address blocked.

That freedom cuts both ways on every trusted socket, loopback included: a client allowed to
name itself can also send a different valid address per attempt and have each failure charged
to a fresh identity, so it never accumulates a penalty of its own. The defence is
configuration, not code: trust only the sockets and ranges you really front with a proxy,
because the default loopback, private and link-local trust is what makes the trick available
wherever clients share a network.

## Trusted proxies

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

## The control socket

`serve` listens on a second address besides its port: a unix control socket that exposes the
block table. The socket is what controls the lockout now, not the sqlite file. `serve` creates
it beside the db it opens — `sandbin.db` comes with `sandbin.db.sock`, `/data/sandbin.db` with
`/data/sandbin.db.sock` — and chmods it `0600` after binding, so it is the server's own user
who may manage blocks; relax that mode only on purpose. `--socket <path>` names another socket
for either side (the socket on a tmpfs while the db stays on disk), and `serve` also falls back
to `SANDBIN_SOCKET` the way it falls back for its other settings.

`serve` refuses to start when the socket path is already answering (`another sandbin serve is
already listening on sandbin.db.sock`), replaces the stale socket a killed server left behind,
and refuses a path that holds a regular file or a symlink, leaving that file exactly where it
is. The two listeners share no routes: `/ip` on the public port is an unknown bin, and the
socket has no bin, file or `/api/health` route.

## The `ip` commands

The `ip` family drives the control socket: `list`, `show`, `unblock` and `clear`. A live
`serve` is required: there is no offline mode, and no verb opens, stats or creates the `--db`
file. Nothing answering the socket is one readable error naming it (`no sandbin serve
listening on sandbin.db.sock`, exit 1) and nothing changes. No verb sends an API key, and
connecting to the socket grants block management and nothing else — narrower than what it used
to be, because `ip unblock` once needed write access to the whole db file, which also allowed
editing bins and files with plain SQL. Each verb takes `--db` (default `sandbin.db`) and asks
the serve whose socket sits next to that file; `./sandbin ip help` prints this family's usage:

```sh
./sandbin ip list                    # every row of the block table
./sandbin ip list --json             # the same rows as a json array
./sandbin ip show 203.0.113.7        # one address's row in the block table
./sandbin ip unblock 203.0.113.7     # delete one address's block
./sandbin ip clear                   # delete every block
```

Flags go before the address, for `show` and `unblock` alike:
`./sandbin ip unblock --db /data/sandbin.db 203.0.113.7`.

`list` prints `IP`, `FAILURES`, `BLOCKED UNTIL`, `REMAINING` and `LAST FAILURE`. `REMAINING`
is worked out while the command runs and reads `expired` once `blocked_until` is past; expired
rows are listed on purpose, because their failure count still shapes the next penalty. An empty
table prints `no blocked addresses` and exits 0. `--json` prints the same rows as an array of
`ip`, `failures`, `blocked_until` and `updated_at` (`[]` when there is no row), and it belongs
to `list` alone: `show`, `unblock` and `clear` refuse it.

`show` prints one address's row in that same table and nothing else: the header, then the row,
with `REMAINING` worked out the same way. Its argument is spelled the way `unblock` spells it
(see below), and an address with no row is the same error `unblock` gives for it (`no block for
203.0.113.7`, exit 1). Reading a row and lifting one now need the same thing — the right to
connect to the socket — so there is no read-only half of the family any more.

`unblock` deletes the whole row, so the failure count goes with it: the next wrong key from
that address starts again at 30 minutes instead of resuming the escalation it had reached. Its
argument is spelled the way the server spells an address it charges — the keys the server
stores never carry a zone — so `::ffff:203.0.113.7`, `fe80::1%eth0` and `2001:0DB8::1` all
reach the stored key. A key that itself carries a zone can only come from a hand-inserted row,
and `ip clear` or plain SQL is how to remove one. An address with no block is an error naming
it (`no block for 203.0.113.7`, exit 1), which is how a typo shows up.
`clear` deletes every row and prints `cleared N blocks`; `N=0` is fine.

## The socket's HTTP shape

The socket speaks four requests and nothing else: `GET /ip` (every row), `GET /ip/{ip}` (one
row), `DELETE /ip/{ip}` and `DELETE /ip`, answering `404` with `{"error":"no block for <ip>"}`
for a key the table does not have and `{"unblocked":"<ip>"}` or `{"cleared":N}` when a delete
did something; an unknown path is `404`, a wrong method is `405`, and `HEAD` answers like
`GET`. `curl --unix-socket` drives it the same way:

```sh
curl --unix-socket sandbin.db.sock http://localhost/ip
curl -X DELETE --unix-socket sandbin.db.sock http://localhost/ip/203.0.113.7
```

The running server is the only writer of the block table, so a management run cannot contend
with it for the sqlite lock; `RecordFailure` keeps its bounded retry for the writers that
remain — an old binary or a stray sqlite client can still open the db file — and a charge that
outlives every retry is lost but logged (`record auth failure`), never silently dropped.

## In the container

The socket rides the same `sandbin-data` volume as `/data/sandbin.db`, so a second container of
the same image is the management route, and it needs the volume rather than the key. The
image's working directory is `/`, so name that db, or work in the volume and the default
`sandbin.db` resolves to it:

```sh
podman run --rm -v sandbin-data:/data localhost/sandbin ip list --db /data/sandbin.db
podman run --rm -w /data -v sandbin-data:/data localhost/sandbin ip list
```
