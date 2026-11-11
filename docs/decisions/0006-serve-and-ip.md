# Decisions: subcommand shape and IP management

Date: 2026-11-11. Confirmed with the user before `docs/plans/0006-serve-and-ip-management.md`.

| Topic | Decision | Why |
|---|---|---|
| Server moves under `sandbin serve` | Yes, and a bare `sandbin` with no subcommand prints usage and exits 1 | Being explicit about which command talks to the database beats a hidden default; the container, README and quadlet invocations are all updated in the same plan. |
| Management verbs | `sandbin ip list`, `sandbin ip unblock <ip>`, `sandbin ip clear` | Covers the operator's real needs: see who is blocked, lift one, reset all. No manual `ip block`. |
| How management reaches the table | Directly on the `--db` sqlite file | Filesystem permission is already the authority over every bin and every block; an admin HTTP API would add a whole auth design to avoid a problem that file ownership already solves. `busy_timeout(5000)` in `openStore` is what makes a management run safe next to a live server. |
| API key for ip verbs | Not needed | They never touch the network. Requiring the key would only mean copying the server's secret into a local command. |
| `ip list` output | Plain text table, with `--json` as an option | Readable to an operator, parseable to an agent when asked. |
| `unblock` of an address with no block | Exit 1, "no block for <ip>" | The common reason for that is a typo; silence would hide it. |
| `unblock` semantics | Deletes the row, so the failure count is forgotten too | The penalty walks up from the stored count; keeping a 24-hour-failure count behind a lifted block would re-arm the next failure instantly. |
| IP matching | The argument is parsed and canonicalised (unmapped, zone-free) before matching the stored key | Blocks are stored as canonical addresses since `88a0f44`; the operator's `::ffff:1.2.3.4` or `fe80::1%eth0` must hit the same key. |
