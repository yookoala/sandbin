# Decisions: client config file, init wizard, key-check endpoint

Plan: `docs/plans/0011-client-config.md`. These points were argued and settled; do not
revisit them during implementation.

| Point | Decision |
|---|---|
| Format | YAML, parsed with `gopkg.in/yaml.v3` — the third direct dependency, added on purpose: the user chose a maintained library over a hand-rolled YAML-subset parser. No hand parsing of the file anywhere. |
| File | `.sandbin.yaml` in the working directory (workspace approach: sandboxed agents need no extra mounts). Keys: `sandbin_url`, `api_key`, `bin`, `protected`. Mode 0600, written by the same temp-file+rename pattern `writeState` uses. |
| Old file | `.sandbin.json` is gone without migration or fallback — the client is an internal tool, breaking change accepted. |
| Precedence | `--url` flag > `SANDBIN_URL` > file `sandbin_url` > `http://localhost:8080`. `--api-key` flag > `SANDBIN_API_KEY` > file `api_key`. Env still beats the file so CI and agents keep working without touching projects. `flagGiven` (already in the client) tells "flag passed" from "flag defaulted". |
| api_key persistence | Only `init` writes a key into the file, at the user's explicit request. An ordinary upload run writes url/bin/protected and keeps whatever `api_key` the file already holds — it never newly persists a key that came from flag or env. The bin password stays env-only, as before. |
| Bin reuse | Same rule as the JSON state had: the file's `bin` is reused only when the file's `sandbin_url` equals the base URL this run resolved to. A bin made for another server is never carried to a new one. `--new` replaces bin/protected (and the url it belongs to) and leaves `api_key` untouched. |
| readConfig errors | A file that exists but is unreadable or unparsable is an error naming it — same philosophy as `readState`: falling through silently would make a second bin for one prototype. A missing file is `found = false`, not an error. |
| init dispatch | `sandbin-client init [flags]`: the verb is the first argument; init has its own FlagSet with only `--url` and `--api-key`. Idempotent: re-running re-verifies and rewrites. |
| init values | flag > env > existing file value. Missing value on a terminal → interactive prompt showing the current default, Enter keeps it. Missing on a non-terminal → `sandbin_url` falls back to `http://localhost:8080`, a missing `api_key` refuses with wording that names `--api-key`, `SANDBIN_API_KEY` and `init`. |
| init verification | Before writing anything: `POST /verify-key` with the candidate key. 200 continues; 403 → "server rejected this API key"; 429 → the address is currently blocked; transport failure → name the URL. Nothing is written on failure — the client never saves a broken setup. |
| init writes | On success: write `.sandbin.yaml` (keeping the file's bin/protected only when its url equals the url being saved), 0600, and ensure `.gitignore` in the same directory contains a `.sandbin.yaml` line — create the file if absent, append if missing, leave it alone if present. stdout confirms the url; the key is never echoed. |
| Prompt echo | The wizard reads the key with the terminal's normal echo (`shortcut: visible echo for the key prompt; upgrade with golang.org/x/term ReadPassword if agents ever need masking`). The alternative was an extra dependency for one line. |
| Key-check endpoint | `POST /verify-key` and nothing else — 405 for every other method, so a GET cannot probe a key without charging. The guard is the same shape as `createBin`'s: blocked → 429 `too many bad API keys` + RecordFailure; wrong or missing key → 403 `invalid API key` + RecordFailure; match → 200 `key ok`. "verify-key" holds a dash, so it can never name a hex bin hash, same reasoning as `healthz`. No bin is created, no side effect. |
| Brute force | The endpoint charges wrong attempts exactly like `POST /` does, so the existing lockout is the bound. A friendlier key-check policy (lighter charging for this endpoint) was discussed and is explicitly out of scope. |
| Key guard wording | The client's "no API key" refusal now also names the file and `init`: `no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init`. |
