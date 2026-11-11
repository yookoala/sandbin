# Plan: `sandbin serve` and a command to inspect and lift blocks

Status: draft. Decisions in `docs/decisions/0006-serve-and-ip.md`. Extends
`docs/plans/0003-api-key-lockout.md`.

## What it is

The server becomes `sandbin serve`, and a new `sandbin ip` family lets whoever owns the
database file see and undo the lockout blocks:

```
sandbin serve [--api-key k] [--db f] [--addr a] [--trusted-proxy l] [--trusted-proxy-file f]
sandbin ip list [--json]
sandbin ip unblock <ip>
sandbin ip clear
sandbin help | --help
```

A bare `sandbin` prints the usage on stderr and exits 1: every invocation names its verb.

`ip` verbs open the `--db` file directly (default `sandbin.db`, same flag as `serve`). They
make no network request and need no API key: filesystem permission is the authority. They can
run while the server is up — `openStore`'s `busy_timeout(5000)` queues the management write
behind the server's current transaction instead of failing — and they work inside a container
through `podman run localhost/sandbin ip list --db /data/sandbin.db`.

## Behaviour details

- **Dispatch.** `main` picks the verb from the first argument and hands the rest to that verb's
  `flag.FlagSet`. The five server flags keep their names, env fallbacks and refuse-to-start
  guards under `serve`, unchanged. Unknown verbs and unknown flags print that verb's usage and
  exit 1. `help` and `--help` print the usage on stdout and exit 0.
- **`ip list`.** Every row of `ip_blocks` in a column-aligned text table: `IP`, `FAILURES`,
  `BLOCKED UNTIL`, `REMAINING`, `LAST FAILURE`. `REMAINING` is computed at run time and reads
  `expired` when `blocked_until` is past — expired rows are listed on purpose because their
  failure count still shapes the next penalty. Empty table prints `no blocked addresses` and
  exits 0. `--json` prints the same rows as an array of
  `{"ip","failures","blocked_until","updated_at"}` (`[]` when empty); it is a flag of `list`
  only.
- **`ip unblock <ip>`.** The argument must parse as an IP address; it is canonicalised the way
  `clientIP` canonicalises a charged address (parsed, unmapped, zone-free), so `::ffff:1.2.3.4`
  and `fe80::1%eth0` reach the stored keys. It deletes exactly that row. No row: `no block for
  <ip>` on stderr, exit 1. A non-address argument is refused with wording naming it. Deleting
  the row forgets the failure count too (decision table).
- **`ip clear`.** Deletes every row and prints `cleared N blocks`; `N=0` is fine and exits 0.
- **Store additions** in `store.go`, beside the existing block methods:

```go
type IPBlock struct {
	IP, BlockedUntil, UpdatedAt string
	Failures                    int
}

func (s *Store) ListBlocks() ([]IPBlock, error)        // ORDER BY ip
func (s *Store) UnblockIP(ip string) (bool, error)     // DELETE; reports whether a row went
func (s *Store) ClearBlocks() (int64, error)           // DELETE all; reports the count
```

Nothing in `IsBlocked`, `RecordFailure` or the handlers changes.

## Container coupling (this is what breaks first if forgotten)

- `Containerfile` `CMD ["--db", "/data/sandbin.db"]` becomes `CMD ["serve", "--db",
  "/data/sandbin.db"]` in the same commit that makes a bare `sandbin` an error, or the image
  stops starting. `ENTRYPOINT` and the quadlet unit stay as they are: the unit relies on the
  image CMD.
- README's `./sandbin` run command, its podman `podman run` line and any other invocation
  example gain `serve`.

## Layout

```
cmd/sandbin/
  main.go      verb dispatch + serve subcommand (the server body itself moves to runServe unchanged)
  ipcmd.go     runIP: list, unblock, clear, text and json rendering
  store.go     the three new methods only
  ipcmd_test.go
  store_test.go grows the block-method tests
Containerfile   CMD gains serve
```

## Milestones (one atomic commit each)

1. `feat: sandbin serve runs the server as a subcommand` — dispatch, `help`, no-verb refusal,
   all existing guards preserved, and the `Containerfile` CMD in the same commit.
2. `feat: sandbin ip inspects and lifts blocks` — store methods, `ipcmd.go`, tests.
3. `docs: managing blocked addresses` — README run commands gain `serve` and a new section for
   the `ip` verbs.

## Implementer TODO

- [ ] `cmd/sandbin/main.go`: build the dispatch around `flag.FlagSet` per verb. Keep every
      existing server flag name, env fallback, refusal message and log line exactly as they are,
      just under `serve`. Bare `sandbin`, an unknown verb and an unknown flag each print usage on
      stderr and exit 1; `help` prints on stdout and exits 0.
- [ ] `cmd/sandbin/store.go`: `ListBlocks`, `UnblockIP`, `ClearBlocks` with the signatures above.
- [ ] `cmd/sandbin/ipcmd.go`: `runIP(args []string, stdout, stderr io.Writer) error` for a test
      seam; the text table, `--json`, the canonicalising `unblock`, the counting `clear`. Reject
      `sandbin ip` with no verb and unknown verbs with usage.
- [ ] Tests (`ipcmd_test.go`, `store_test.go`), at least: dispatch table — no args, `help`,
      `--help`, unknown verb, `serve` with each existing flag, `serve --bogus`; `ip` with no verb;
      list on an empty table (text and `--json`); list shows seeded rows with `expired` computed
      from `now`; `--json` output parses as JSON with exactly the four fields; unblock of a seeded
      row removes only that row; unblock miss exits 1 naming the ip; unblock canonicalises
      `::ffff:1.2.3.4` to a `1.2.3.4` row and `fe80::1%eth0` to a `fe80::1` row; unblock of a
      non-address is refused; `clear` reports the count and an empty clear reports 0 with exit 0;
      `--json` is rejected as a flag of `unblock`/`clear`; the `--db` flag points every `ip` verb
      at the same file `serve` uses.
- [ ] `Containerfile`: `CMD ["serve", "--db", "/data/sandbin.db"]`.
- [ ] README: `./sandbin serve` in the build/run example and in the podman `podman run` command;
      a new `## Managing blocked addresses` section: the three verbs, direct-database meaning
      ("whoever can read the `--db` file can lift blocks from it; protecting that file is the
      control"), the unblock-forgot-the-count semantics, running it safely next to a live server,
      and the container form `podman run --rm localhost/sandbin ip list --db /data/sandbin.db`.
- [ ] `gofmt -l cmd` empty, `go vet ./...` clean, `go build ./...` for both commands,
      `go test -count=1 ./...` and `go test -race -count=1 ./...` green.
- [ ] Live proof, cleaned up afterwards, under `/tmp/opencode`: server on `127.0.0.1:18170`;
      wrong key → `403`; `sandbin ip list` while the server runs shows the `127.0.0.1` row;
      `ip unblock 127.0.0.1` then a wrong key again gets `403` and a fresh 30-minute row, proving
      the lift and the forgotten count; `ip clear` reports the count; empty `ip list` text and
      `--json`; `--json` piped through a JSON parser; bare `./sandbin` exits 1. Then the container
      proof: `podman build --network=host`, start the quadlet-shaped container, `curl` healthz
      200, `podman run --rm localhost/sandbin ip list --db /data/sandbin.db` on a fresh volume,
      and `/usr/libexec/podman/quadlet -user -dryrun` clean. Kill every server, delete every db,
      image, container and directory, leave `/tmp/opencode` empty and no binary in the repo root.

## Reviewer TODO

- [ ] Read README, `docs/decisions/0006-serve-and-ip.md`, `cmd/sandbin/main.go`, `ipcmd.go`,
      `store.go`, `ipblock.go`, then the diff range.
- [ ] CLI matrix from the shell with `echo $?` after each: no args, `help`, `--help`, unknown
      verb, `ip` with no verb, `ip list --bogus`, `ip unblock` with no argument, `ip unblock a b`
      with two, `ip clear extra`, `serve --bogus`, `serve` with a missing key, `ip list` with no
      `--db` (relative default). Report every message verbatim and whether usage went to stdout
      or stderr.
- [ ] Regression on the server path: confirm all five flags, both env fallbacks, all four
      refuse-to-start guards, the log line and `/healthz` behave exactly as before, with the live
      403→429 escalation re-run once end to end.
- [ ] Concurrency against a live server under load: while `sandbin-client` uploads and a wrong-key
      hammer runs, run `ip list`, `ip unblock` and `ip clear`; the server must keep serving 201s
      with no `SQLITE_BUSY` in its log, and the management command must either succeed or fail
      with a readable error inside the 5 s busy timeout — never corrupt.
- [ ] Unblock canonicalisation against real stored keys: charge a block live through a trusted-proxy
      socket so the stored key is canonical, then unblock it with the sloppy spelling
      (`::ffff:` prefix, a `%eth0` zone, uppercase hex); also confirm `[1.2.3.4]` (brackets) is
      refused as a non-address, and report what happens for a row stored from an untrusted socket,
      whose key is the socket host verbatim.
- [ ] Semantics: after `unblock`, the next wrong key restarts at 30 minutes (count forgotten);
      after `clear`, every address is free but a live open connection of the server caches
      nothing it should not (the handlers re-read `IsBlocked` per request — confirm from source);
      `list` shows expired rows with the `expired` marker and their true counts.
- [ ] JSON discipline: `--json` output parses with `python3 -m json.tool`, is `[]` when empty,
      has exactly the four fields, and is the only flag `list` accepts; `--json` on `unblock` or
      `clear` is a usage error.
- [ ] Container coupling: build with podman (`--network=host`), confirm the container starts on
      the new CMD alone (quadlet unit untouched), healthz answers, `podman run --rm
      localhost/sandbin ip list --db /data/sandbin.db` works on a fresh volume, and the quadlet
      dryrun parses. Confirm `grep -rn 'sandbin --' README.md deploy/ Containerfile` shows no
      invocation left without a verb.
- [ ] Security wording: README must state plainly that `ip` verbs are authorised by file access
      and not by the API key, and that `serve`'s guards did not loosen. Confirm `ip` verbs make
      zero network requests (watch with `ss` or just point `--db` at a copy of the live db file —
      the copy proof also proves they never talk to the server process).
- [ ] Run `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -l cmd`
      yourself; name every new code path with no test, including the `--json` render, the busy
      timeout wait and the `help` verb.
- [ ] Repo shape: `IsBlocked`, `RecordFailure`, the handlers, the trusted-proxy rules, ANSI
      rendering, quadlet unit and the client are untouched; the only store changes are the three
      new methods; no new dependency. Report anything else the diff touched.

## Skipped on purpose

An admin HTTP API, manual `ip block`, a single-ip `ip show`, editing `failures` or
`blocked_until` by hand, remote management, TLS, changing the block parameters from the CLI,
per-verb config files, and shipping anything new in the image beyond the CMD line.
