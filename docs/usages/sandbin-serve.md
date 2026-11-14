# Running the server

Build and run:

```sh
go build -o sandbin ./cmd/sandbin
SANDBIN_API_KEY=your-key ./sandbin serve
```

Every invocation names its verb: `serve` runs the server, `ip list`, `ip show <ip>`, `ip unblock
<ip>` and `ip clear` inspect and lift the API key blocks through that server's control socket
(see [Managing blocked addresses](sandbin-ip.md)), and `help` prints the usage. A bare `sandbin`, `sandbin help`
and `--help` print that usage on stdout and exit 0; an unknown verb answers
`No help topic for 'x'` on stderr and exits 1.

`--api-key` (default `$SANDBIN_API_KEY`), `--db` (default `sandbin.db`), `--addr` (default
`:8080`) and `--socket` (default `$SANDBIN_SOCKET`, otherwise the `--db` path plus `.sock`; see [Managing blocked addresses](sandbin-ip.md)) are `serve`'s core settings; `--trusted-proxy` and
`--trusted-proxy-file` (defaults `$SANDBIN_TRUSTED_PROXIES` and `$SANDBIN_TRUSTED_PROXIES_FILE`,
see [Trusted proxies](sandbin-lockout.md#trusted-proxies)) optionally name the proxies the server believes. The server exits when no API
key is configured, a trusted proxy value cannot be read, or another serve already answers on its
control socket.
