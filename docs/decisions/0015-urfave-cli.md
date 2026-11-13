# Decisions: moving both command lines to urfave/cli

The hand-rolled verb/flag plumbing in `cmd/sandbin` and `cmd/sandbin-client` is replaced with
`github.com/urfave/cli/v2`, and the client gains explicit verbs. Chosen by the user; the
flag-form rule is theirs too: no effort to keep Go-style single-dash long flags (`-help`) —
whatever urfave (which parses with the stdlib flag package under the hood) accepts naturally is
kept, nothing extra is added. Plan in `docs/plans/0015-urfave-cli.md`.

| Point | Decision |
|---|---|
| Dependency | `github.com/urfave/cli/v2` — the stable major (v3 is still beta), added to both binaries with one `go get`; `go.mod`/`go.sum` gain urfave and its transitive help-rendering helpers. This is the fourth direct dependency, accepted on purpose. No vendoring. |
| Server shape | `sandbin serve`, `sandbin ip list|show|unblock|clear` keep their verb trees; urfave adds the natural `help` command (`sandbin help`, `sandbin help serve`, `sandbin help ip`) and `--help`/`-h` on every command. Flag names are unchanged (`--addr`, `--db`, `--socket`, `--api-key`, `--trusted-proxy`, `--trusted-proxy-file`, ip's `--json` etc.) and keep their environment fallbacks via urfave's `EnvVars` — the same variable names, including the container's keyless-start behavior. |
| Client verbs | Explicit verbs, per the user's choice: `sandbin-client upload FILE...`, `init`, `delete PATH... [--folder NAME]...`, `describe`. The old default-upload (`sandbin-client notes.md`) and the mode-flags `--describe`, `--delete`, `--delete-folder` are gone — breaking change accepted (precedent: `.sandbin.json` removal; this is an internal tool, docs + SKILL.md + installer updated in the same plan). Delete's file paths become positional args; folders stay a repeatable flag. Upload keeps `--name`, `--archive`, `--no-extract`, `--new`, `--title`, `--description`, stdin-with-`--name`. Delete keeps applying `--title`/`--description` metadata first when given, exactly as the old combined run did. `describe` keeps its no-API-key property. |
| Flag placement | `--url` and `--api-key` exist on every client verb (a shared flag list attached per verb); `--bin` on upload/delete/describe; `--reinit` on init only. The resolution ladder is unchanged (flag > env > file > default): urfave `EnvVars` fills env when the flag is absent, and an empty result falls through to the file exactly as `firstEnv`/`flagGiven` did. `SANDBIN_BIN_PASSWORD` stays plain-env (never a flag), as decided in 0011. |
| Behavior contract | The HTTP protocol, config file, wizard prompts, re-init guard (0014), prompts, lockout handling, charging, exit code 1 on operational failures, and every operational error wording stay byte-for-byte — `sandbin-client: <message>` on stderr stays the operational-error shape (urfave's error plumbing wrapped so the wording does not change). What DOES change: usage/help rendering becomes urfave's (help on stdout for `--help`/`-h`/`help`, error + command usage on stderr for misuse, still exit 1), and a bare verb-less invocation now prints help and exits 0 instead of the hand-written refusal (urfave's natural default action) — for both binaries. The unknown-verb error becomes urfave's `unknown command` rendering. |
| Single-dash flags | Nothing extra done for `-help`-style forms; urfave parses flags with the stdlib flag package, so single-dash long forms continue to parse as a natural consequence, unadvertised. Aliases only where urfave itself has them (`-h` for `--help`). |
| Version | `app.Version` = `"dev"` for both; `--version` prints it. Build-time stamping via ldflags is out of scope. |
| Test entry points | `dispatch(args, stdout, stderr)` (server) and `run(args, stdin, stdout, stderr)` (client) stay the test-callable seam: each call builds a fresh urfave app (apps are stateful; per-call construction keeps tests isolated) with `Writer`/`ErrWriter` and `Reader` wired to the test's writers and stdin, then `app.Run(append([]string{progName}, args...))` and maps the returned exit code/error like today. Tests that pinned hand-written usage text are updated to the new rendering; tests pinning operational wordings keep their pins. |
| install.sh | Untouched: its command line (`init --url ... --api-key ...`) is already verb-shaped and stays valid. The `[ -t 1 ]`/`/dev/tty` wiring is framework-independent. |
| Docs ripple | README: every client example verb-shaped (upload/describe/delete with `--folder`), lockout and init section verb mentions, the client section's refusal wordings re-pinned to the new usage rendering where they are usage errors. SKILL.md: every command example verb-shaped, plus the deferred 0014 note that a `/dev/null` stdin answers the guard question with nothing (= "no") so scripts choose `--reinit` deliberately. Front page: unchanged unless it quotes a client command line (it does not). |
| Shell completion | Accepted as inherited: urfave answers `--generate-bash-completion` when a shell env is present. Not advertised, not tested beyond confirming it cannot hijack a normal invocation (a `sandbin-client` run without a detected shell behaves identically; unit tests set no SHELL surprises). |

## Skipped on purpose

urfave/cli v3 (beta), command aliases (`ls` for `list`, a hidden upload alias), a `delete`
folder-flag rename to positional form, keeping bare-file upload as a default action, custom
help templates or colored output, ldflags version stamping, TOML/flagfile config sources,
urfave's `Before`-based auth scaffolding (the client's ladder stays hand-written where it is),
and renaming the server's verbs.
