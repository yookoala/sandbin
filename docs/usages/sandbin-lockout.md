# API key lockout

`POST /`, `PUT /{hash}/{path}`, `DELETE /{hash}/{path}` and `POST /verify-key` are the
key-checked requests.
`POST /verify-key` is the pure key check `sandbin-client init` runs before saving a key: it
answers `200 key ok` or `403 invalid API key`, charges every failure exactly like `POST /`
does, creates nothing, and answers every method but `POST` with `405 method not allowed`, so no
`GET` can probe a key for free. A wrong or missing `X-API-Key`
blocks that address from all of them for 30 minutes; every further failure adds 30 minutes, capped at
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
