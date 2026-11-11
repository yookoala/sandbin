# Decisions: `sandbin ip show`

Date: 2026-11-12. For `docs/plans/0007-ip-show.md`. (`sandbin ip list` already exists from
plan 0006; this plan adds the single-address verb only.)

| Topic | Decision | Why |
|---|---|---|
| Missing row | `no block for <ip>` on stderr, exit 1 — the same wording and code `unblock` uses | Every `ip` verb answers the same question shape: the row it was asked about. A script can tell "no such row" from a bad argument by exit wording, and consistency beats a special-case exit 0 that only `show` has. |
| Argument handling | Identical to `unblock`: canonicalised like `clientIP`, non-addresses refused | Two verbs naming one address must not spell that address differently. |
| Output | The same text table as `list`, one row | No new shape to learn or keep stable; the row is already self-describing (`REMAINING`, `expired`). |
| `--json` | Not on `show` | `list --json` is already the machine-readable route, and an agent can filter one row from it. A single-object json shape for one verb is one more format to hold still for no gain. |
