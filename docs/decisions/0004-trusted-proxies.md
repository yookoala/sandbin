# Decisions: trusted proxies

Date: 2026-11-11. Amends the client identity row of `docs/decisions/0003-api-key-lockout.md`.
Confirmed with the user before `docs/plans/0004-trusted-proxies.md`.

| Topic | Decision | Why |
|---|---|---|
| Configuration | `--trusted-proxy` / `$SANDBIN_TRUSTED_PROXIES` for a comma-separated list of IPs and CIDRs, and separately `--trusted-proxy-file` / `$SANDBIN_TRUSTED_PROXIES_FILE` for a file with one entry per line | The user asked for both. The file exists because Cloudflare publishes hundreds of CIDRs. |
| Implicit local trust | Kept: loopback, private and link-local sockets remain believed with no configuration; the list only adds | The container and quadlet defaults documented so far depend on it. |
| Public proxies | Trusting a public range such as Cloudflare means any connection whose source address is in that range gets header authority | The honest cost of supporting a public proxy, stated in the README. |
| Header entry | Still the rightmost `X-Forwarded-For` entry | A well-behaved proxy, Cloudflare included, appends the real client there. `CF-Connecting-IP` is not read. |
