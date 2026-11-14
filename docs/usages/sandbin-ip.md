# Managing blocked addresses

`serve` listens on a second address besides its port: a unix control socket that exposes the
block table, and the `ip` commands drive it. The socket is what controls the lockout now, not the
sqlite file. `serve` creates it beside the db it opens — `sandbin.db` comes with
`sandbin.db.sock`, `/data/sandbin.db` with `/data/sandbin.db.sock` — and chmods it `0600` after
binding, so it is the server's own user who may manage blocks; relax that mode only on purpose.
`--socket <path>` names another socket for either side (the socket on a tmpfs while the db stays
on disk), and `serve` also falls back to `SANDBIN_SOCKET` the way it falls back for its other
settings.

A live `serve` is required: there is no offline mode, and no verb opens, stats or creates the
`--db` file. Nothing answering the socket is one readable error naming it (`no sandbin serve
listening on sandbin.db.sock`, exit 1) and nothing changes. No verb sends an API key, and
connecting to the socket grants block management and nothing else — narrower than what it used to
be, because `ip unblock` once needed write access to the whole db file, which also allowed
editing bins and files with plain SQL. Each verb takes `--db` (default `sandbin.db`) and asks the
serve whose socket sits next to that file; `./sandbin ip help` prints this family's usage:

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
with `REMAINING` worked out the same way. Its argument is spelled the way `unblock` spells it (see
below), and an address with no row is the same error `unblock` gives for it (`no block for
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

The socket speaks four requests and nothing else: `GET /ip` (every row), `GET /ip/{ip}` (one
row), `DELETE /ip/{ip}` and `DELETE /ip`, answering `404` with `{"error":"no block for <ip>"}`
for a key the table does not have and `{"unblocked":"<ip>"}` or `{"cleared":N}` when a delete
did something; an unknown path is `404`, a wrong method is `405`, and `HEAD` answers like `GET`.
`curl --unix-socket` drives it the same way:

```sh
curl --unix-socket sandbin.db.sock http://localhost/ip
curl -X DELETE --unix-socket sandbin.db.sock http://localhost/ip/203.0.113.7
```

The two listeners share no routes: `/ip` on the public port is an unknown bin, and the socket has
no bin, file or healthz route. The running server is now the only writer of the block table, so a
management run cannot contend with it for the sqlite lock; `RecordFailure` keeps its bounded
retry for the writers that remain — an old binary or a stray sqlite client can still open the db
file — and a charge that outlives every retry is lost but logged (`record auth failure`), never
silently dropped.
`serve` refuses to start when the socket path is already answering (`another sandbin serve is
already listening on sandbin.db.sock`), replaces the stale socket a killed server left behind, and
refuses a path that holds a regular file or a symlink, leaving that file exactly where it is.

In the container the socket rides the same `sandbin-data` volume as `/data/sandbin.db`, so a
second container of the same image is the management route, and it needs the volume rather than
the key. The image's working directory is `/`, so name that db, or work in the volume and the
default `sandbin.db` resolves to it:

```sh
podman run --rm -v sandbin-data:/data localhost/sandbin ip list --db /data/sandbin.db
podman run --rm -w /data -v sandbin-data:/data localhost/sandbin ip list
```
