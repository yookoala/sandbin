# Plan: block addresses that keep sending bad API keys

Status: implemented on `main` (`f88d02e..88a0f44`), reviewed, three review blockers fixed.
Decisions in `docs/decisions/0003-api-key-lockout.md`.

## Rule being implemented

An address that sends a wrong or missing `X-API-Key` on a key-checked request
(`POST /` and `PUT /{hash}/{path}`) is blocked from those two operations. The block starts at
30 minutes and grows by 30 minutes for every further failure, capped at 24 hours per block.
Bans are stored in the same sqlite file, so a restart does not clear them. Blocked addresses
still get normal `GET` service: the penalty covers key-checked writes only.

`GET` of a bin, `GET /{hash}/` listing, path errors, oversized bodies and wrong bin passwords
are **not** failures under this rule. Wrong bin password in particular is out of scope: the
request is only about the API key.

## Client identity

`X-Forwarded-For` is honoured only when the connection itself arrives from loopback or a
private address, which is what a local proxy or a docker bridge looks like. Otherwise the
header is ignored and the socket address is used. This is what stops the trivial bypass where
an attacker sends a different fake header per request.

```go
// clientIP is the address a failure is charged to. The header is only believed when the
// connection came from loopback or a private network, i.e. a local proxy put it there.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isLocalAddr(host) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		entries := strings.Split(xff, ",")
		// The rightmost entry is the one a proxy appends; anything left of it is client fiction.
		if last := strings.TrimSpace(entries[len(entries)-1]); last != "" {
			return last
		}
	}
	return host
}
```

`isLocalAddr` uses `net.ParseIP` plus `IsLoopback() || IsPrivate() || IsLinkLocalUnicast()`.
Empty or unparsable host falls back to the raw `RemoteAddr` string.

## Storage

One new table in `openStore`, next to `bins` and `files`:

```sql
CREATE TABLE IF NOT EXISTS ip_blocks (
  ip TEXT PRIMARY KEY,
  failures INTEGER NOT NULL,
  blocked_until TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

New methods on `Store`, each taking `now time.Time` as an argument so tests control the clock
and there is no config knob for it:

- `IsBlocked(ip string, now time.Time) (time.Time, bool)` — one indexed `SELECT blocked_until`,
  blocked when `blocked_until.After(now)`.
- `RecordFailure(ip string, now time.Time) (blockedUntil time.Time, err error)` — read the
  current `failures`, `failures+1`,
  `blocked_until = now + min(30min * failures, 24h)`, `updated_at = now`, upsert. Also
  `DELETE FROM ip_blocks WHERE updated_at < now - 30 days` in the same call so the table cannot
  grow without bound.
- Timestamps stored the way `store.go` already stores them, compared with `time.Parse` /
  `Format(time.RFC3339Nano)`.

New file `cmd/sandbin/ipblock.go` holds the two constants and `clientIP` / `isLocalAddr`:

```go
const (
	blockStep      = 30 * time.Minute // each failure adds this much
	blockMax       = 24 * time.Hour   // a single block never gets longer than this
	blockIdlePrune = 30 * 24 * time.Hour
)

// shortcut: an address idle for 30 days forgets its failure count; raise the window if
// long-lived deployments need escalation to survive that long.
```

## Handler wiring

In `createBin` and `putFile`, before the API key comparison:

```go
ip := clientIP(r)
if until, blocked := s.store.IsBlocked(ip, time.Now()); blocked {
	w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(until).Seconds())))
	http.Error(w, "too many bad API keys", http.StatusTooManyRequests)
	return
}
```

and inside the existing "invalid API key" branch, before writing the 403:

```go
if _, err := s.store.RecordFailure(ip, time.Now()); err != nil {
	log.Printf("record auth failure for %s: %v", ip, err) // never fail the request over this
}
http.Error(w, "invalid API key", http.StatusForbidden)
```

An attempt that arrives while already blocked returns 429 **and** records a failure, so
hammering while blocked walks the penalty up to the cap. Missing header counts as a failure,
same as a wrong one.

New response: `429` with `Retry-After: <seconds>`, plain text body `too many bad API keys`.

## Milestones (one atomic commit each)

1. `feat: store address blocks in sqlite` — table, `IsBlocked`, `RecordFailure`, pruning, store tests.
2. `feat: block addresses after a bad API key` — `ipblock.go`, wiring in `createBin` and `putFile`, handler tests.
3. `docs: document the API key lockout` — README section.

## Implementer TODO

- [ ] `store.go`: add the `ip_blocks` table to the schema and the three methods. `IsBlocked`
      returns `(time.Time, bool)`; `RecordFailure` does the read, the `failures+1` upsert and the
      30-day prune in one place.
- [ ] `cmd/sandbin/ipblock.go`: the three duration constants, `clientIP`, `isLocalAddr`.
- [ ] `handlers.go`: block check first in `createBin` and `putFile`, `RecordFailure` inside both
      invalid-key branches, `Retry-After` header, `429`.
- [ ] `store_test.go`: first failure blocks 30 minutes, second 60, third 90; 48th failure stays
      at 24 hours; a block ends exactly at `blocked_until`; `RecordFailure` accumulates on the
      same `now`; rows older than 30 days are pruned and rows inside the window are not.
- [ ] `handlers_test.go`, using `httptest` and setting `req.RemoteAddr` explicitly:
      - wrong key on `POST /` gives 403, the next request from the same address gives 429 with a
        positive `Retry-After`;
      - no `X-API-Key` header at all also triggers the block;
      - a blocked address can still `GET` a public bin and its listing (200), and still gets 429
        on `PUT`;
      - `PUT` under a bin escalates the same way as `POST /`;
      - a second `store` opened on the same db file (a restart) still refuses the blocked address;
      - a request from a public socket address (`RemoteAddr: "8.8.8.8:1234"`) with a forged
        `X-Forwarded-For` is charged to `8.8.8.8`, not to the header;
      - the same request from `127.0.0.1` with `X-Forwarded-For: 203.0.113.7, 198.51.100.4` is
        charged to `198.51.100.4` (rightmost), and a later request from `127.0.0.1` **without**
        the header is still allowed: it is a different address;
      - a successful request from a blocked-but-unrelated address is unaffected.
- [ ] `ipblock_test.go` for `clientIP`/`isLocalAddr` edge cases: missing port, IPv6
        (`[::1]:1234`), an empty header, a header of only commas, a link-local address.
- [ ] `gofmt -l cmd/sandbin` empty, `go vet ./...` clean, `go build ./...`,
      `go test -count=1 ./...` and `go test -race ./...` green.
- [ ] Live smoke test with the built binary on `127.0.0.1:18091` and a temp db in `/tmp/opencode`:
      wrong key once -> 403, again -> 429 with `Retry-After` near 1800, `sqlite3`? no: read the
      row with a tiny Go program or `go test`, and confirm a public `GET` still works and the
      container/user flow from the README still works. Clean everything up.

## Reviewer TODO

- [ ] Read `README.md`, `docs/plans/0002-podman-container.md` outcome, `handlers.go`, `store.go`,
      `ipblock.go` before judging.
- [ ] Escalation arithmetic against a live server on `127.0.0.1:18092` with a temp db: confirm the
      first block is 30 minutes, the second 60 (inspect `blocked_until` in the db file after each
      failure), and that `Retry-After` matches the remaining time within a second or two.
- [ ] Hammer test: 30 rapid wrong-key `POST /` requests in a loop; confirm the block extends
      rather than being ignored, that the response stays a plain 429, and that the sqlite file
      does not grow per request (one row per address).
- [ ] Confirm a blocked address can still read public bins and that `GET` requests never charge a
      failure, including `GET` with a wrong key.
- [ ] Confirm a wrong **bin password** does not charge the API key block (out of scope by
      decision) and report whether that gap is worth stating in the README.
- [ ] Identity: prove with `--path-as-is` and hand-built headers that a direct request from a
      non-local address cannot dodge the block with a forged `X-Forwarded-For`, and that rotating
      fake headers cannot produce fresh identities. Also confirm a request whose `RemoteAddr` has
      no port, or an IPv6 form, is charged to something sane rather than the empty string.
- [ ] Bypass hunt: can a client get an unbounded number of failures without ever being blocked
      (many addresses, one failure each)? Is the block keyed on something a client controls
      entirely? Is `blocked_until` ever set in the past, or 0 seconds, letting a block be
      permanently inert? Does an `X-Forwarded-For` of `""`, `","`, `" "` or a 10 KB header create
      a bad primary key or a huge row?
- [ ] Denial of service by spoofing: state plainly, from the chosen rule, how far an attacker who
      can reach the port directly can push a *third-party* address's block, and whether the README
      warns about it.
- [ ] Storage: no SQL concatenation, the prune cannot delete a live block, `IsBlocked` uses the
      primary key (indexed), no read of `content` blobs in the block path, one row per address
      under 1000 distinct failing addresses.
- [ ] Failure of the failure recorder: if `RecordFailure` returns an error (locked db, disk full),
      the request must still get its 403 and must not become a 500.
- [ ] Concurrency: 50 parallel wrong-key requests from one address under `-race`; failures must not
      lose counts badly enough to matter, and no error may leak to the client.
- [ ] Tests: run `go test -count=1 ./...`, `go test -race ./...` yourself; check the tests assert
      status codes, `Retry-After`, and the escalation durations rather than only "not 403"; say
      plainly which new code paths have no test.
- [ ] Repo shape: no new dependency, no new flags, no config file, changes confined to
      `cmd/sandbin/` and `README.md`/`docs/`.

## Skipped on purpose

Blocking on wrong bin passwords, blocking `GET`, per-bin rather than per-address blocks, rate
limiting of *successful* requests, an unblock command or admin API, a `--trusted-proxy` flag,
CIDR-range bans, distributed bans across replicas, and any attempt to hide the penalty from the
client (`Retry-After` is deliberately honest).

## Outcome

Implemented in `f88d02e`, `e845b46`, `2f69e4b` and reviewed. The reviewer confirmed 30/60/90
minute escalation against a live server, the 24 hour cap, `Retry-After` accuracy, restart
persistence, `GET` staying open, wrong bin passwords not being charged, and that a non-local
socket cannot dodge a block by rotating headers. Three defects were found and fixed:

- `6ab72b9`: `RecordFailure` read and wrote as two statements, so 50 parallel failures from one
  address stored 34 (12 under `-race`), and 34 of 200 hammer requests failed with `SQLITE_BUSY`,
  which also hit ordinary bin and file writes. The store now pools exactly one connection and the
  read, upsert and prune run in one transaction. Re-measured live: 250 parallel wrong-key
  requests stored exactly 250 failures and the log held no store errors.
- `3fd3729`: `blockStep * failures` overflows int64 nanoseconds past about 5.1 million failures
  and puts `blocked_until` in the past, disarming the block for exactly the hammerers whose
  counter grew most. From 48 failures the penalty is charged as `blockMax` without multiplying.
  Re-measured live: a seeded `failures=6000000` row produced `Retry-After: 86391`.
- `88a0f44`: the header's rightmost entry was charged verbatim, so a 200 KB header became a
  200000-byte primary key and grew the database 425984 bytes in one request, and keys like
  `fe80::1%eth0` or `203.0.113.7:8080` never matched a real client. The charged value is now a
  parsed, canonical, unmapped, zone-free address, or the socket host when the entry is not one.

Known and accepted: the 30 day prune is an unindexed scan of `ip_blocks` on each failure, fine at
current table sizes; `Retry-After` can be `0` in the last second of a block; `main.go`'s
refuse-to-start guards are proven from the shell but have no `go test` coverage; and an attacker
who can reach the port with one failure each from many identities is never blocked, which is the
cost of blocking per identity rather than by rate. `b96c943` records the residual bypass: on any
trusted socket, including loopback, a client may rotate valid header addresses and keep each
failure charged to a fresh identity.
