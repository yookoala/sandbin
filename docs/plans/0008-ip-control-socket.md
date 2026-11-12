# Plan: `ip` verbs through a unix control socket

Status: implemented on `main` (`1934f6e`, `7410722`, `3476383`), reviewed as "ship with
fixes", review fix in `1c8fe4f`, this commit records the rest. Decisions in
`docs/decisions/0008-ip-control-socket.md`. Supersedes the direct-db row of
`docs/decisions/0006-serve-and-ip.md`; the verbs, wording, exit codes and canonicalisation
settled by 0006/0007 stay exactly as they are.

## What it is

`serve` gains a second listener: a unix socket that exposes the block table as a small JSON API.
`ip list`, `ip show`, `ip unblock` and `ip clear` stop opening the sqlite file and drive that API
instead. Everything an operator sees at the CLI — table shape, `--json` array, `no block for
<ip>` / `unblocked <ip>` / `cleared N blocks`, exit codes, canonical spellings — must come out
identical to `5241a03`.

```
sandbin serve --db sandbin.db            # also creates and listens on sandbin.db.sock (0600)
sandbin ip list --db sandbin.db          # GET /ip over sandbin.db.sock
```

## API on the control socket

A dedicated `http.ServeMux`, separate from the bin server, on `net.Listen("unix", path)`:

| Request | Answer |
|---|---|
| `GET /ip` | `200` json array, exactly the four fields `list --json` prints today, `[]` when empty |
| `GET /ip/{ip}` | `200` one row object; `404` `{"error":"no block for <ip>"}` when the key is absent |
| `DELETE /ip/{ip}` | `200` `{"unblocked":"<ip>"}`; `404` the same error object on a miss |
| `DELETE /ip` | `200` `{"cleared":N}` |
| anything else | `404` / `405` — no bin, file or healthz routes on this mux |

The client canonicalises before dialing (`canonicalIP` stays the CLI's gate, so operator-facing
messages keep their exact wording); the server treats the path value as an opaque key in the
parameterised query. REMAINING stays computed client-side at display time, so the server never
needs a clock in the protocol.

## Path, permissions, startup

- Socket path default: `--db` value plus `.sock`. `serve` and each `ip` verb take `--socket` to
  override; `serve` also falls back to `SANDBIN_SOCKET`, like its other settings.
- `serve` binds with `os.Chmod(path, 0600)` after listen. Connecting the socket is now the whole
  authority for management; the `ip` verbs no longer need any access to the db file, and the db
  file no longer grants management power.
- Startup: live answer on the path → refuse (`another sandbin serve is already listening on <path>`,
  exit 1). Socket file nobody answers on → unlink and bind. Non-socket at the path (regular file
  or symlink) → refuse naming it, leave the file alone.
- `ip` verbs when nothing is listening: one readable error naming the socket path, exit 1, and
  zero bytes of the db file touched — including when the `--db` file does not exist (the verbs no
  longer create it; `openIPStore` and its warning are deleted).
- Client is one `http.Client{Timeout: 10 * time.Second}` dialed through the socket.

## Log line

`sandbin listening on <addr>` stays byte-identical; serve gains `sandbin control socket on <path>`.

## Milestones (three atomic commits)

1. `feat: serve listens on a unix control socket` — `control.go` (mux + four handlers + listen +
   startup guards), serve wiring, its `--socket`/`SANDBIN_SOCKET`, server-side tests.
2. `feat: sandbin ip drives the running server through the control socket` — the client side of
   `ipcmd.go`, `--socket` on each verb, `openIPStore` deleted, usage texts, test harness rewrite.
3. `docs: managing blocked addresses goes through the control socket` — README section rewrite
   including the container run form.

## Implementer TODO

- [ ] `cmd/sandbin/control.go` (new): `controlMux(store) *http.ServeMux` with the four endpoints
      above; `listenControl(socketPath string) (net.Listener, error)` implementing the startup
      rules (live listener → refuse; stale socket → replace; non-socket file → refuse) and the
      0600 chmod. Handlers call the existing `ListBlocks`/`GetBlock`/`UnblockIP`/`ClearBlocks`
      unchanged; store errors become `500 {"error":...}`. `store.go`, `handlers.go`, `ipblock.go`
      are not touched at all.
- [ ] `cmd/sandbin/main.go`: `serveConfig` gains `socketPath` (flag `--socket`, env
      `SANDBIN_SOCKET`, default `dbPath+".sock"`); `runServe` starts the control listener before
      the TCP one, after the existing guards, with the new log line; the refuse-to-start guards
      keep their exact wording and order. Top-level `usage` gains a line for `--socket` only where
      a line already lists flags.
- [ ] `cmd/sandbin/ipcmd.go`: each verb resolves its socket the same way (`--socket` or
      `--db`+`.sock`), dials with the 10 s client, maps API answers onto today's outputs: `GET
      /ip` → table or `--json` (print the server's array; `list --json` must still emit exactly
      the four-field shape and `[]`); `GET /ip/{ip}` 404 → `no block for <ip>` exit 1; `DELETE`
      miss → same; `DELETE /ip` → `cleared N blocks`. `canonicalIP` stays the front gate, so the
      zero-arg / two-arg / non-address refusals are word-for-word unchanged and happen before any
      dial. Delete `openIPStore` and the missing-file warning. `ipUsage` says the socket is found
      next to `--db` and `--socket` overrides.
- [ ] Tests: new `control_test.go` — drive the mux over a real unix socket with the dialing
      client: list empty/live, show hit/miss, unblock hit/miss (row really gone, failure count
      gone), clear count, method mismatch → 405, unknown route → 404, 0600 mode proven with
      `os.Stat`, startup guards proven (live listener, stale file replaced, regular file refused
      and still present). Rewrite the `ipcmd_test.go` harness to start a control server on a
      socket in `t.TempDir()` over a seeded store, then run `runIP` against it; keep every existing
      assertion's expected message and exit unchanged, including `TestIPShowCorruptRow` (seed via
      the store, expect the CLI read-failure wording the client derives from the server's 500
      error — adapt the prefix, keep it naming the address). Add: verb with no server at the path
      (readable error naming the socket, exit 1, and no `--db` file created — assert with
      `os.Stat`); `--socket` override hit and wrong. `dispatch`-level tests keep passing.
- [ ] `README.md` (milestone 3): rewrite `## Managing blocked addresses` — the socket is the
      control, 0600 by default, a live `serve` is required, the path derivation, the container
      form `podman run --rm -v sandbin-data:/data localhost/sandbin ip list` (the `--db /data/
      sandbin.db` form still works since the socket rides the same volume). The legacy-db caveat
      added in `76b9486` disappears with direct db access.
- [ ] `gofmt -l cmd` empty, `go vet ./...` clean, `go test -count=1 ./...` and
      `go test -race -count=1 ./...` green.
- [ ] Live proof under `/tmp/opencode` (binaries there, ports ≥ 18213, clean up everything
      afterwards): server on `127.0.0.1:18213`; charge `203.0.113.44` via `X-Forwarded-For` on the
      trusted loopback socket; `ip list`, `ip show 203.0.113.44`, `ip show ::ffff:203.0.113.44`,
      a miss with its exit, `ip unblock`, a re-charge restarting at 30 minutes, `ip clear`; raw
      `curl --unix-socket` against each endpoint (including a wrong method and a bogus route);
      `curl http://127.0.0.1:18213/ip` proving the control API is absent from TCP; the three
      startup-guard runs; an `ip` run with no server listening.

## Reviewer TODO

- [ ] Read the diff, `docs/decisions/0008-ip-control-socket.md`, and plan 0006/0007 outcomes.
- [ ] Output-identity sweep against the pre-migration binary (`5241a03`): seed one db by hand,
      run the old binary direct and the new one through the live server against identical db
      contents, and diff stdout/stderr/exit for every case: list empty/live/`--json`, show hit/
      expired/miss/`::ffff:`/`%eth0`, unblock hit/miss/non-address/zero/two args/`--json`,
      clear, `ip help`, `ip bogus`, flags-after-address. The only allowed differences: the new
      offline behaviour, `--socket`, and the deletion of the missing-file warning.
- [ ] CLI matrix with `echo $?` for the new failure modes: no server anywhere; socket path held
      by a regular file; `--socket` pointing at nothing; server died leaving a stale socket (then
      the next `serve` must replace it cleanly); a second `serve` against a live one (refused,
      naming the socket); `--db` file absent with no server (the verb must create no file at all —
      assert `ls` after).
- [ ] Authority model proven, both directions, using `setpriv --reuid=65534`: (a) the 0600
      socket refuses uid 65534 — readable error, exit 1 — for all four verbs (the plan 0007 split
      of "show needs read, unblock needs write" is gone: connectivity is the gate); (b) db access
      alone grants nothing, and the socket grants nothing but management: make the db 0600 root-
      owned (unreadable by uid 65534) and chmod the socket 0666 — uid 65534 then manages blocks
      fine, proving the verbs never open the db; and the converse: db 0666 (uid 65534 could read
      and rewrite it) with the socket back at 0600 — every verb fails.
- [ ] Control endpoints cannot leak: `curl http://127.0.0.1:<port>/ip` and `DELETE` on TCP →
      404/405 from the bin server with no effect; `curl --unix-socket` for a bin path, `PUT` to a
      bin, `GET /healthz` → 404/405, no bin reachable through the control mux. Confirm from source
      the two muxes share no routes.
- [ ] Lockout semantics unchanged through the new path: full 403→429 escalation with growing
      `Retry-After`; block visible in `ip list` immediately after the first charge; `unblock`
      through the socket restarts the next penalty at 30 minutes; expired row still walks to
      failures 2; wrong bin passwords still charge nothing; `GET` of a public bin stays open for a
      blocked identity. Under a concurrent hammer: zero dropped charges, zero `SQLITE_BUSY` lines.
- [ ] `--json` discipline: `ip list --json` output parses with `python3 -m json.tool`, is exactly
      the four fields, `[]` when empty, and equals the raw `curl --unix-socket GET /ip` body.
- [ ] Canonicalisation parity show/unblock still total (same table as plan 0007's review).
- [ ] Log line: `sandbin listening on <addr>` byte-identical; control line present; guards keep
      their wording order (missing key first).
- [ ] Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`,
      `gofmt -l cmd`; count tests; name every untested new arm.
- [ ] Repo shape: `store.go`, `handlers.go`, `ipblock.go`, `ansi*.go`, `Containerfile`, `deploy/`,
      `cmd/sandbin-client`, `go.mod`/`go.sum` untouched (no new dependency); only `main.go`,
      `ipcmd.go`, new `control.go`, tests, README in the diff.
- [ ] Container: build (`--network=host`), start on the CMD alone, healthz 200, charge through the
      published port, `podman run --rm -v sandbin-data:/data localhost/sandbin ip list` shows the
      row (second container is the new management route), `ip unblock` lifts it, the running
      server's next wrong key gets a fresh 403; quadlet dryrun unchanged. Clean everything up.

## Skipped on purpose

Management with no running server, tokens or TLS on the socket, TCP-remote management, systemd
socket activation, removing the `RecordFailure` busy retry, changing any verb's wording or exit
codes, and a `--json` flag on `show`.

## Outcome

Tests went 128 to 145 top-level tests (110 + 2 subtests in `cmd/sandbin`, 35 client);
`gofmt`, `go vet`, `go test -count=1` and `go test -race` green at every commit (the reviewer
re-ran each commit in its own worktree). Coverage: 84.3% `cmd/sandbin`.

The review verdict was **ship with fixes**. One blocker: the new README claimed a charge that
outlives every retry is "never dropped" — the code logs it and drops it, exactly as `5241a03`
documented; the README now says "lost but logged … never silently dropped" again. `README.md`
also now spells `HEAD` like `GET` on the control API instead of lumping it into "404 or 405",
and `store.go`'s two comments that still described management verbs as direct-db second writers
were corrected (comment-only; the plan's no-touch rule for `store.go` was kept for code).

`1c8fe4f` fixed the review's worst non-blocker: a dial refused by socket permission was reported
as `no sandbin serve listening on <path>`, so the one failure the new authority model creates
anyone-but-the-server's-user hitting a 0600 socket read as its opposite and invited an needless
serve restart. Now `cannot connect to <path>: permission denied`, proven live as uid 65534
against a live root-owned 0600 socket while the same run as root listed the rows normally; the
nothing-listening wording is unchanged. Same commit: `--db` on the `ip` verbs no longer claims in
its help text to be a file the verb reads.

The review verified against a live server (127.0.0.1:18217) and the `5241a03` binary side by
side, 37 cases each diffing stdout, stderr, exit code and the post-run `ip_blocks` contents:
the only differences are `--socket` in flag lists, the rewritten `ipUsage` and serve usage line,
and one `REMAINING` cell of clock drift. The corrupt-row wording came out byte-identical. The
authority model was proven in both directions as uid 65534: db 0600 root-owned (unreadable) plus
socket chmod 0666 — all four verbs manage fine, so they never open the db; db 0666 (uid 65534
demonstrably read and inserted via `sqlite3`) plus socket 0600 — every verb fails. No leak
between the muxes both ways: TCP `GET`/`DELETE /ip` answers `404`/`405` from the bin server with
no effect on the table, and bin paths, `POST /`, `PUT` and `GET /healthz` over the socket all
answer `404`/`405`. Lockout semantics carried over unchanged: the `Retry-After` walk, the row in
`ip list` immediately after the first charge, socket `unblock` restarting at failures 1, an
expired row walking to 2, bin passwords charging nothing, GET open for a blocked identity; a
303-request hammer with all four verbs in flight recorded 303 charges in 101 rows, zero
`record auth failure`, zero busy lines, 3–5 ms verb latency. Startup guards all proven live
(live listener refused naming it, first server unaffected; `kill -9` stale socket replaced by
the next serve; regular file refused and byte-intact; same for symlinks, target left alone).
`ip list --json` is byte-equal to the raw `curl --unix-socket` body, `[]` when empty. All four
refuse-to-start guards, the stray-positional refusal and all env fallbacks (plus the new
`SANDBIN_SOCKET`) match `5241a03` wording byte-for-byte; the TCP log line is identical apart
from the added control-socket line. Container: image CMD alone starts both listeners, healthz
200, a row charged through the published port was listed, shown and lifted by a second container
sharing the volume (`srw------- /data/sandbin.db.sock` in the volume), the next wrong key got a
fresh 403, `podman stop` + `start` replaced the stale socket, and the quadlet unit generates
unchanged.

Accepted as they are: after `podman stop` the socket file stays in the volume until the next
`serve` replaces it, so a management run in between says `no sandbin serve listening` — the
decided "no offline mode"; `GET /ip/` (trailing slash) is `404` by the mux patterns and `HEAD`
answers like `GET`; a blocked identity is refused even with the correct key while its public-bin
GET stays open (pre-existing behaviour the README's wrong-key prose does not mention); the client
trusts the status code alone, so a rogue server on the socket would be echoed — harmless against
the only process that speaks this protocol; a symlink at the socket path gets the blunt "exists
and is not a socket" sentence; Go's flag package accepts `-socket` as it always has for `--db`;
and the pre-existing stdout quirks (`ip list` exiting 1 silently on unwritable stdout) are
unchanged from `5241a03`. `TestControlRefusesASocketItMayNotReach` proves the permission arm
only when the suite runs unprivileged — under root the live-listener arm answers instead; the
permission arm itself was proven live as uid 65534. Left unproven here: a genuinely untrusted
public socket (this sandbox cannot bind one), the podman healthcheck machinery (cannot run here;
the probe command itself passes inside the image), and `runServe`'s guard arms under `go test`
they stay shell-proven only, as before. `README.md:199` shows `podman build` without
`--network=host`, which this sandbox requires; that is a sandbox limit, not a README defect.
