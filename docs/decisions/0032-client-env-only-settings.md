# Env-only client settings — Decision

Decided 2026-11-26 with the partner, over a long exchange that narrowed the ask twice. Plan:
[0032-client-env-only-settings](../plans/0032-client-env-only-settings.md). Amends
[0005-client-defaults](0005-client-defaults.md), [0011-client-config](0011-client-config.md) and
[0014-reinit-guard-and-installer-init](0014-reinit-guard-and-installer-init.md).

## Why

Agents run `sandbin-client init` before every upload out of habit — `skills/sandbin-preview/SKILL.md`
tells them to — and in a directory whose `.sandbin.yaml` already pins a url and a key that habit
meets the re-initialization guard and turns into a refusal the agent then works around with
`--reinit`. When the environment already carries `SANDBIN_URL` and `SANDBIN_API_KEY`, nothing in
that directory needs initializing at all. The partner's word for the goal: **reduce the need for
unnecessary repeated `init`**. Keeping the server address out of the file is *not* the goal, and this
decision deliberately keeps writing it.

## The decisions

| Topic | Decision | Why |
|---|---|---|
| Missing url | A verb run with no `--url`, no `SANDBIN_URL` and no `sandbin_url` in the file refuses before any request: `no server URL: pass --url, set SANDBIN_URL or run sandbin-client init`. `init`, which *is* the fix, says `no server URL: pass --url or set SANDBIN_URL`. The built-in `http://localhost:8080` stops being a fallback value and survives only as the text in `init`'s prompt default. | A client that guesses a server uploads previews to a localhost nobody can open and records the guess for later runs to undo. The partner's ruling: refuse and point at the fix. |
| No verb prompts | `upload`, `delete`, `describe` and `list` print the instruction and stop, at a terminal or not. Only `init` asks questions, and it keeps `sandbin_url [http://localhost:8080]: ` for a human at a terminal — a bare Enter there is a human choosing that address. | Chosen by the partner from the alternative of asking inline. `isTerminal` and `stdinCarriesUpload` (`main.go:1145-1162`) already make a terminal run and a piped upload mutually exclusive, so inline prompting was possible; it was still declined. |
| The environment never creates a field | `init` stores what it just verified, **except** that a value sourced only from the environment is not written into a field the file does not already have. A flag or a typed answer replaces a field; a field the file already has follows the verified value; the environment alone adds nothing. | Storing a key the environment already carries duplicates CI's config in a `0600` file in the project. Keeping a stored field truthful means the file's url and key always describe the setup the run just checked — no pairing one server's key with another's url. |
| A bin is stored with its url | An upload run that creates a bin writes `sandbin_url` beside the hash, from whichever source supplied it, and never writes `api_key`. | The partner's own correction. A hash with no server is a hash that a later `SANDBIN_URL` change strands: the client would reuse it against the new server and take a 404. This keeps today's promise that a bin made for one server never travels to another, and keeps `main.go:761`, `848` and `874` untouched. |
| The file omits what it does not have | `omitempty` goes on all four `clientConfig` fields, so the file carries only what this project actually has: an `init` fed only by the environment writes `{}`, an upload in the same project writes `sandbin_url`, `bin` and — when the bin has a password — `protected`, and never an `api_key`. A missing `protected` reads back as `false` and a missing `bin` as "no bin yet", so nothing changes in what the client *does*. An empty mapping is a legal file: `readConfig` reads it back as "no settings stored". | The partner's ask stated plainly: do not write the keys at all rather than writing `api_key: ""`. `bin: ""` and `protected: false` are the same kind of noise, and no test or reader treats those lines as data. |
| "Initialized" is unchanged | The guard still fires only on a file holding both a `sandbin_url` and an `api_key`. An env-only file holds neither, so `init` in a CI job is idempotent and `--reinit` keeps meaning "overwrite a file that pinned a url and a key". | With nothing running `init` proactively the guard almost never fires, so `0014`'s code stands and only its wording needs a line. |
| Settings flags stop telling the flag parser about the environment | `EnvVars` comes off the two settings flags; the ladder reads `os.Getenv("SANDBIN_URL")` and `os.Getenv("SANDBIN_API_KEY")` itself, the way `SANDBIN_BIN_PASSWORD` is already read; each flag's `Usage` names its variable so `--help` stays honest. | `urfave/cli`'s `Context.IsSet` cannot separate a command-line flag from an env-sourced value (`context.go:58-95`; `flag_string.go`'s `Apply` sets `HasBeenSet` from the environment), so "was this chosen by a human" is unanswerable while the parser owns the environment. A shell that exports the variables while `install.sh` passes them as flags is ordinary usage, not a test artifact. |
| Refusals are one at a time | A run missing both a url and a key reports the url first. | Reading settings already happens before the key guard in every verb (`main.go:273` then `280-282`; `466`/`470`; `623`/`630`). Fix one, the run then names the next. |

## What changes for a person using the client

| Situation | Today | From here |
|---|---|---|
| Both variables exported, no `.sandbin.yaml` | `upload` works and writes `sandbin_url`, `api_key: ""`, `bin`, `protected` | works, and writes `sandbin_url`, `bin`, `protected` — no `api_key` line at all |
| `init` with both variables exported | writes both into the file, so the next plain `init` meets the guard | writes neither, so `init` is re-runnable and the guard never fires |
| Neither variable, no file | dials `http://localhost:8080` and reports the connection failure | refuses in one line naming `--url`, `SANDBIN_URL` and `init`, having made no request |
| `init` with no url in flag, environment or file, no terminal | quietly verifies against `http://localhost:8080` and fails there | refuses: `no server URL: pass --url or set SANDBIN_URL` |
| `init` fed only by the environment, over a file that pins a url | stores the env key next to that url | uses the key, stores neither, keeps the url the file already had |
| `init --reinit` over a pinned file with `SANDBIN_URL` pointing elsewhere | re-points the file's url, drops the stale bin | unchanged |

## Amendments to the earlier decisions

- **0005-client-defaults**: the client-side url default survives only as `init`'s prompt text. Bin
  selection through the working directory's file, `SANDBIN_BIN_PASSWORD` staying env-only, and
  never-retry all stand.
- **0011-client-config**: precedence is now `--url` > `SANDBIN_URL` > `sandbin_url` > refusal, and
  `--api-key` > `SANDBIN_API_KEY` > `api_key` > refusal for the key-needing verbs (`describe` still
  needs no key). "Env still beats the file" stands; the new half is that the environment may update
  a field the file has but never create one.
- **0014-reinit-guard-and-installer-init**: the guard's code and its "the file, not the environment,
  is the marker" rule stand. Add that an `init` fed only by the environment produces a file that
  holds neither field and so is not initialized, which is intended, not a hole.
