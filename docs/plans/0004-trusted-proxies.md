# Plan: trust a configured list of proxy addresses

Status: draft. Extends `docs/plans/0003-api-key-lockout.md`; the identity row in
`docs/decisions/0003-api-key-lockout.md` is amended by `docs/decisions/0004-trusted-proxies.md`.

## What changes

Today `clientIP` believes `X-Forwarded-For` only when the socket host is loopback, private or
link-local. That is right for a container behind a sidecar proxy and wrong for an origin behind a
public proxy such as Cloudflare, where the socket address is a public CDN address and the real
client appears only in the header.

Add a configured list of proxy addresses. Loopback and private networks stay trusted with no
configuration; the list only adds to it.

## Configuration

| Flag | Environment fallback | Value |
|---|---|---|
| `--trusted-proxy` | `$SANDBIN_TRUSTED_PROXIES` | comma-separated IPs and CIDRs: `203.0.113.7,173.245.48.0/20,2400:cb00::/32` |
| `--trusted-proxy-file` | `$SANDBIN_TRUSTED_PROXIES_FILE` | path to a file, one IP or CIDR per line; blank lines and `#` comments ignored |

Both are merged, file first. A bad entry, or a file that cannot be read, makes the server refuse
to start with the offending value in the message, the same way a missing API key does. No entry
means no extra trust, and behaviour is exactly what plan 0003 shipped.

## Code shape

`ipblock.go` grows, still with no new dependency and no new package.

```go
// proxyList holds the addresses whose X-Forwarded-For is believed.
type proxyList []netip.Prefix

// loadTrustedProxies merges the comma separated list with the file list.
func loadTrustedProxies(csv, path string) (proxyList, error)

// shortcut: a linear scan per key-checked request; build a prefix tree if a list grows
// past a few thousand entries.
func (l proxyList) contains(ip netip.Addr) bool

// clientIP charges a request to the client address, which is the header's rightmost entry
// when the socket itself belongs to a proxy we trust.
func clientIP(r *http.Request, proxies proxyList) string

func isLocalAddr(host string) bool // unchanged
```

`server` gains one field, `trustedProxies proxyList`, set from `main.go`:

```go
proxies, err := loadTrustedProxies(*trustedProxy, *trustedProxyFile)
if err != nil {
	log.Fatalf("sandbin: %v", err)
}
srv := &server{store: store, apiKey: []byte(*apiKey), trustedProxies: proxies}
```

`netip.ParseAddr` for entries, `.WithZone("")` on the socket address so `fe80::1%eth0` matches a
`fe80::/64` entry. A bare IP becomes a `/32` or `/128` prefix. Header entries are used as they
are: only the address we charge, never a value that is fed to SQL.

Cloudflare needs no special case: its proxy appends the real client as the rightmost
`X-Forwarded-For` entry, which is the entry already trusted. `CF-Connecting-IP` is not read.

## Milestones (one atomic commit each)

4. `feat: trust configured proxy addresses` — `proxyList`, `loadTrustedProxies`, `contains`,
   `clientIP` signature change, `server` field, `main.go` flags.
5. `docs: document trusted proxies` — README section plus a quadlet example line.

## Implementer TODO

- [ ] `ipblock.go`: `proxyList`, `loadTrustedProxies`, `contains`, `clientIP(r, proxies)`;
      `isLocalAddr` unchanged and still checked first.
- [ ] `main.go`: `--trusted-proxy` (default `$SANDBIN_TRUSTED_PROXIES`) and `--trusted-proxy-file`
      (default `$SANDBIN_TRUSTED_PROXIES_FILE`), refuse to start on a bad value.
- [ ] `handlers.go`: pass `s.trustedProxies` to `clientIP` in `createBin` and `putFile`. Nothing
      else about the lockout changes.
- [ ] Tests for parsing: bare IPv4 and IPv6, CIDR with a `/0`, an IPv6 with a zone on the socket,
      a file with `#` comments, blank lines, CRLF and trailing spaces, a missing file, a garbage
      entry, an empty CSV, and a CSV and file that overlap.
- [ ] Tests for charging: a public socket inside a trusted `/20` is charged to the header's
      rightmost entry; the same header from a public socket outside the list is charged to the
      socket; a private socket is charged from the header with no configuration at all; a
      configured `/32` matches exactly one address; a list that contains `0.0.0.0/0` makes every
      socket header-trusted and must be recorded in a test as the deliberate footgun it is.
- [ ] Tests must keep the lockout semantics asserted end to end: one wrong key from a header-named
      address blocks that address, not the proxy, and the proxy itself keeps working.
- [ ] `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -l cmd/sandbin`.
- [ ] Live smoke: run the binary on `127.0.0.1:18093` with `--trusted-proxy 198.51.100.0/24` and a
      temp db; from a request whose `RemoteAddr` is a public address in that range plus
      `X-Forwarded-For: 203.0.113.9`, show the first wrong key gives 403, the next gives 429, that
      the blocked row in sqlite names `203.0.113.9`, and that the same address range without the
      configured flag gives a 403 charged to the socket address instead. Clean up after.

## Reviewer TODO

- [ ] Startup failure: an unparseable entry, a directory given as the file, an unreadable file, and
      a file with 5000 lines must each behave sanely; report the exact messages.
- [ ] Charging rules against a live server on `127.0.0.1:18094` with curl and hand-written headers:
      a header from an untrusted public socket must not move the penalty; a trusted public socket
      must; rotating headers from an untrusted socket must not produce fresh identities.
- [ ] Confirm nothing new reaches SQL: the value written into `ip_blocks.ip` is always the charged
      address, and a 10 KB header, a header of commas, and a header containing quotes or `--`
      cannot create a bad row or a second statement.
- [ ] Confirm `0.0.0.0/0` and `::/0` are treated as explicit operator choices, not defaults, and
      that README warns about them.
- [ ] Cost: 2000-entry list against the request path, measure wrong-key latency with and without
      the list; say whether the linear scan is fine at that size.
- [ ] Confirm loopback and private trust did not regress: the container flow from the README (the
      container-side socket is private) still charges the header with no configuration.
- [ ] Confirm lockout behaviour from plan 0003 is unchanged: 30/60/90 escalation, 24 hour cap, GET
      stays open, wrong bin passwords are still not charged, restart keeps blocks.
- [ ] `go test -count=1 ./...`, `go test -race ./...` yourself; name any new branch with no test.

## Skipped on purpose

`CF-Connecting-IP` support, fetching Cloudflare ranges automatically, a `--no-local-proxy` switch
to disable implicit local trust, per-proxy differentiation of trust level, Unix socket support, and
validating that a configured proxy range is actually yours.
