# Decisions: API key lockout

Date: 2026-11-11. Confirmed with the user before the plan in
`docs/plans/0003-api-key-lockout.md`.

| Topic | Decision | Why |
|---|---|---|
| Trigger | The first wrong or missing `X-API-Key` blocks; each further failure adds 30 minutes | The user's wording. No free attempts, at the cost of a 30 minute penalty for one typo. |
| Cap | A single block never exceeds 24 hours, but the failure count is not reset | Bounds the penalty and keeps escalation meaningful without a growing penalty forever. |
| Scope | Only `POST /` and `PUT /{hash}/{path}`; `GET` stays open, wrong bin passwords are not failures | Matches "incorrect API key", and keeps public previews readable. |
| Storage | New `ip_blocks` table in the existing sqlite file | Blocks survive a restart, and everything stays in the one file the README promises. |
| Client identity | `X-Forwarded-For` is believed only when the socket address is loopback or private, using its rightmost entry; otherwise the socket address | Trusting the header always lets an attacker rotate fake headers to stay unbanned or name someone else. Behind a real proxy the rightmost entry is the trustworthy one. |
| Block lookup failure | `IsBlocked` reads a missing, unparsable or failed lookup as "not blocked", and a failing `RecordFailure` still answers `403` | A damaged block table must degrade to the pre-lockout behaviour, never take the whole service down. |
