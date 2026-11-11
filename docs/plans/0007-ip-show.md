# Plan: `sandbin ip show <ip>`

Status: draft. Decisions in `docs/decisions/0007-ip-show.md`. Extends
`docs/plans/0006-serve-and-ip-management.md`, which already delivered `ip list`, `ip unblock`
and `ip clear`.

## What it is

One verb added to the `ip` family:

```
sandbin ip show [--db f] <ip>     one address's row in the block table
```

It answers "what is the state of this address right now?" without printing the whole table,
and without the write access `unblock` needs: `show` only needs read.

## Behaviour details

- The argument goes through the same `canonicalIP` as `unblock`: `::ffff:203.0.113.7`,
  `fe80::1%eth0` and `2001:0DB8::1` reach the stored key; a non-address is refused with the
  same wording. The same `--db` flag and the same missing-file warning apply.
- Row found: the same text table as `list` — header plus one row, `REMAINING` computed at run
  time, `expired` when past — on stdout, exit 0.
- Row not found: `no block for <ip>` on stderr, exit 1, the exact wording and exit `unblock`
  uses for the same situation.
- `show` takes no `--json` (`list --json` is the machine-readable route), and exactly one
  address after its flags, same refusal wording as `unblock` for zero or two arguments.
- Store gains one method beside the others:

```go
func (s *Store) GetBlock(ip string) (IPBlock, bool, error) // SELECT by primary key
```

- The table rendering in `ipList` gets factored into one helper that `list` and `show` share;
  `runIP`'s two "pass list, unblock <ip> or clear" messages, the `ipUsage` block and the
  top-level `usage` in `main.go` all name `show`.

## Milestone (one commit)

`feat: sandbin ip show names one address's block` — the verb, `GetBlock`, the shared text
renderer, usage strings, README line, tests.

## Implementer TODO

- [ ] `cmd/sandbin/store.go`: `GetBlock` with the signature above (`ErrNoRows` → `false`).
- [ ] `cmd/sandbin/ipcmd.go`: `ipShow` mirroring `ipUnblock`'s flag/argument handling; add the
      `show` case to `runIP`; update `ipUsage` and both "pass list, …" messages to include
      `show`; factor `list`'s tabwriter table into a helper both verbs call.
- [ ] `cmd/sandbin/main.go`: `usage` gains `sandbin ip show [--db f] <ip>`.
- [ ] Tests in `cmd/sandbin/ipcmd_test.go`: `show` of a seeded live row prints the header and
      exactly that row; `show` of an expired row shows `expired` and its true count; `show` of
      an absent address exits 1 with `no block for` naming the canonical spelling; `show`
      canonicalises `::ffff:` and `%eth0` spellings to a seeded key; `show` refuses a
      non-address, no address, and two addresses with `unblock`'s wordings; `show --json` is a
      flag refusal; `show` leaves the row untouched (a later `list` still shows it); the
      `usage`/`ipUsage` strings name `show` in every place that lists verbs.
- [ ] README: the `Managing blocked addresses` command block gains `./sandbin ip show 203.0.113.7`
      with a one-line comment, and the paragraph around it says `show` needs only read access.
- [ ] `gofmt -l cmd` empty, `go vet ./...` clean, `go test -count=1 ./...` and
      `go test -race -count=1 ./...` green.
- [ ] Live proof, cleaned up afterwards under `/tmp/opencode`: server on `127.0.0.1:18210`;
      charge a block through `X-Forwarded-For` under loopback trust; `ip show` the canonical
      key while the server runs; `ip show ::ffff:<key>`; `ip show` of a free address and its
      exit code; `ip show` with no `--db` in a directory holding `sandbin.db`. Kill the server,
      delete everything, leave `/tmp/opencode` empty.

## Reviewer TODO

- [ ] Read the diff, `docs/decisions/0007-ip-show.md`, and plan 0006's outcome section.
- [ ] CLI matrix with `echo $?`: `show` with no argument, two arguments, `--json`, a
      non-address, flags after the address, `show --help`, and `show` as an unknown-verb
      suggestion test (`sandbin ips how` should still refuse cleanly). Confirm every message
      matches `unblock`'s where the situation is the same.
- [ ] Exit-code semantics end to end against a live server: charge an identity, `show` it
      (exit 0), `show` a free identity (exit 1 `no block for`), and confirm the difference
      README/plan promise is what an agent gets. Confirm `show` against a read-only db file
      works where `unblock` fails (uid 1000, 0644 db) — this is the verb's stated point.
- [ ] Confirm from source and live `ip list` afterwards that `show` writes nothing: no row is
      created, updated or removed by a `show` run, including against a missing `--db` file
      (it creates the file — check the warning still appears).
- [ ] Canonicalisation parity: the same spellings hit or miss for `show` and `unblock`
      side by side against one stored key.
- [ ] Consistency: every place that lists the verbs (`main.go` usage, `ipUsage`, the two
      `runIP` refusal messages, README's command block) names `show`; grep for stale
      "pass list, unblock <ip> or clear".
- [ ] Run `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -l cmd`;
      name untested new paths. Confirm `list`'s existing tests all still pass unmodified in
      behaviour after the renderer factoring.
- [ ] Repo shape: only `store.go`, `ipcmd.go`, `main.go`, `ipcmd_test.go`, `store_test.go`
      (if `GetBlock` gets a store-level test), `README.md`; `handlers.go`, `ipblock.go`, the
      lockout rules, `Containerfile`, `deploy/` and the client untouched; no new dependency.

## Skipped on purpose

`show --json`, multiple addresses in one run, CIDR or wildcard matching, showing failure
history beyond the stored row, and any change to what `list`, `unblock` or `clear` do.
