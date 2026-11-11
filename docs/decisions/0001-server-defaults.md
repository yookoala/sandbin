# Decisions: server defaults

Date: 2026-11-11. Confirmed with the user before implementation.

| Topic | Decision | Why |
|---|---|---|
| SQLite driver | `modernc.org/sqlite` | Pure Go, no cgo, so one static binary cross-compiles for sandboxes. |
| API key | `--api-key` flag, falling back to `$SANDBIN_API_KEY`; clients send `X-API-Key` | Keeps `Authorization` free for per-bin Basic Auth. |
| Bin password | Optional `X-Bin-Password` at POST, stored as bcrypt hash, required for every GET and PUT under that bin | No mutable password state, so no extra endpoints. |
| ANSI output | Convert to HTML (inline-styled `<span>`s in a `<pre>`) on GET of `.ansi` | Previews must be readable in a browser. |
| Limits | 1 MiB per file, no expiry | One constant plus a 413; TTL would need a cleanup path nobody asked for. |
