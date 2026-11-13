# Plan: moving both command lines to urfave/cli

Status: planned. Decisions in `docs/decisions/0015-urfave-cli.md` — they are the spec; this plan
implements them. Baseline HEAD is the plan-0014 close-out commit (`cmd/sandbin` 179 top-level
tests + 19 subtests, `cmd/sandbin-client` 99 + 28 subtests, all gates green). Sits on plan 0014
(the guard whose wordings must survive) and 0011 (the config ladder plumbing replaces).

**Goal:** both binaries parse their command lines with `github.com/urfave/cli/v2`; the client
gains explicit verbs `upload / init / delete / describe`, replacing the default-upload shape and
the mode-flags `--describe`, `--delete`, `--delete-folder`. No special effort for `-help`-style
single-dash forms; only what urfave naturally supports.

**Architecture:** `cmd/sandbin/main.go` replaces `dispatch` + `flag.FlagSet` parsing with a
per-call urfave app (verbs `serve`, `ip` with subcommands `list|show|unblock|clear`);
`cmd/sandbin/ipcmd.go` keeps its handler bodies behind urfave command definitions;
`cmd/sandbin-client/main.go` replaces `run`'s FlagSet with a per-call urfave app whose four
verbs attach shared auth flags (`--url`, `--api-key`) plus per-verb flags; the ladder
(`firstEnv`/`flagGiven`) maps onto urfave `EnvVars` + file fall-through. `client.go`, `config.go`,
`store.go`, all HTTP and store behavior are untouched.

**Tech stack:** Go stdlib + urfave/cli/v2 (new direct dependency, transitives via `go get`;
no vendoring).

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Do not touch: HTTP handlers (`handlers.go` incl. the 301 redirect, DELETE, 404.html fallback),
  `store.go`, `control.go`, `ipblock.go`, `archive.go` (both packages), `client.go`,
  `config.go`, `ansi` rendering, `skill.go`, `skills/sandbin-preview/install.sh`, `Containerfile`
  (a plain `go build`, unchanged), `.github/workflows/ci.yml`, deploy/.
- Operational error contract: every operational failure keeps its exact wording and the
  `sandbin-client:` / control-socket error shapes on stderr, exit 1 — proven by re-running the
  0013/0014 pinned wordings (upload refusals, delete refusals, init guard refusal, cancel
  wording, wizard prompts, lockout warning, server-rejected-key wording).
- HTTP byte-identity: every server response (healthz, verify-key, 301 battery, PUT/GET/DELETE,
  fallback, listing, archive extraction, front page, skill routes) stays byte-identical to the
  baseline binary; only CLI plumbing changes.
- The env names stay identical (`SANDBIN_API_KEY`, `SANDBIN_URL`, `SANDBIN_BIN_PASSWORD`,
  `SANDBIN_SOCKET`, `SANDBIN_TRUSTED_PROXIES`, `SANDBIN_TRUSTED_PROXIES_FILE`, and any others
  the code actually reads — inventory them in Task 1) and keep the flag > env > file ladder.

## Review Focus

- The wording contract: `sandbin-client: <message>` operational errors are byte-identical to
  baseline (double-check SilenceErrors/SilenceUsage wiring prints nothing extra — no stray
  urfave `Error:` line, no duplicated usage on operational errors, exactly one error line).
- Usage-vs-operational separation: flag misuse gets urfave usage rendering + exit 1; operational
  errors get the old wording + exit 1; bare invocation gets help on stdout + exit 0; `--help`,
  `-h`, `help <cmd>` work on both binaries.
- The ladder survives: flag > env > file for `--url`/`--api-key`/`--bin`/serve settings on every
  verb; env beats file (plan 0011 semantics) after the urfave rewrite; a value given ONLY by env
  does NOT count as guard consent (0014 semantics survive: env-only re-init still refuses).
- Delete verb equivalence: positional paths behave exactly like old `--delete` (refusal of a
  trailing slash naming `--folder`), repeatable `--folder` like `--delete-folder`, ordering
  (files then folders), metadata-first combination, one-request-per-delete, standalone-ness
  (no file/stdin/archive inputs on the delete verb).
- The client never silently uploads on an unknown verb: `sandbin-client notes.md` must fail as an
  unknown command, never upload anything.

## Task 1: dependency and the server's command line

**Files:** modify `go.mod`, `go.sum`, `cmd/sandbin/main.go`, `cmd/sandbin/ipcmd.go`,
`cmd/sandbin/main_test.go` (new or existing — where dispatch/serve-flag parsing is tested today:
find it first), `cmd/sandbin/ipcmd_test.go` (usage-wording pins only)

**Interfaces:**
- Produces: `main` delegating to a test-callable seam of the same shape as today (args, writers
  in, exit code or error out); urfave app per invocation with `Writer`/`ErrWriter` wired; verbs
  `serve`, `ip list|show|unblock|clear`; serve flags with `EnvVars` fallbacks per the decision
  table; `app.Version = "dev"`. Handler code (`runServe` body, ip handlers) unchanged.

- [ ] Step 1: inventory the current dispatch/ip tests and every flag/env pair; write/adjust
  failing tests: bare `sandbin` → help on stdout + exit 0; `sandbin help` and `sandbin help ip`
  → help + 0; unknown verb → exit 1 with urfave unknown-command rendering (no more hand-written
  `unknown command %q` pin — update the pin); `serve` flag+env matrix (flag beats env, env alone
  works, neither keeps defaults incl. keyless start); `ip list/show/unblock/clear` keep their
  current stdout shapes; `--version` prints dev; a bare `ip` shows ip help.
- [ ] Step 2: confirm they fail.
- [ ] Step 3: `go get github.com/urfave/cli/v2`, rewrite main.go/ipcmd.go parsing; keep every
  handler body byte-identical where possible; wire error printing so operational ip errors keep
  their exact current wording.
- [ ] Step 4: `go test -count=1 ./cmd/sandbin` green; commit `feat: sandbin runs on urfave/cli`.

## Task 2: the client's verbs

**Files:** modify `cmd/sandbin-client/main.go`, `cmd/sandbin-client/main_test.go`,
`cmd/sandbin-client/init_test.go`, `cmd/sandbin-client/archive_test.go` (argument lists)

**Interfaces:**
- Produces: `run(args, stdin, stdout, stderr)` kept as the test seam (per-call urfave app,
  `Reader` = stdin); verbs `upload` (positional FILE..., flags `--name --archive --no-extract
  --new --title --description --bin --url --api-key`), `init` (flags `--url --api-key --reinit`),
  `delete` (positional PATH..., repeatable `--folder`, flags `--bin --url --api-key
  --title --description`), `describe` (flags `--bin --url --api-key` accepted but no key ever
  required); bare invocation → client help on stdout + nil; unknown verb → usage error exit 1;
  `delete` reuses `cleanDeleteTargets` semantics (paths then folders, trailing-slash refusal
  naming `--folder`), metadata-first; every operational error wording from baseline unchanged.

- [ ] Step 1: write failing tests: the four verbs' happy paths; bare-file invocation refused as
  unknown command (with zero requests made); `delete` positional battery ported from the old
  `--delete` tests (incl. refusals: trailing slash pointing at `--folder`, `..`, control chars,
  absolute paths); repeatable `--folder`; metadata-first ordering on delete; old upload tests
  updated by inserting `upload`; `describe` verb replaces `--describe` tests; 0014 guard tests
  keep their wordings under `init`; bare invocation shows help + nil; misuse exits with error.
- [ ] Step 2: confirm they fail.
- [ ] Step 3: rewrite `run` as the urfave app; keep `runInit`, upload/delete/describe handler
  bodies and `client.go` untouched behind the new flag plumbing; remove `--describe`/`--delete`/
  `--delete-folder` flags; wire the ladder via `EnvVars` + empty-value file fall-through.
- [ ] Step 4: client tests green; commit
  `feat: sandbin-client runs on urfave/cli with explicit verbs`.

## Task 3: README and SKILL.md

**Files:** modify `README.md`, `skills/sandbin-preview/SKILL.md`

- [ ] Step 1: README: rewrite every client example verb-shaped (upload incl. stdin `--name` and
  `--archive`, describe, delete positional + `--folder` examples, refusal wordings that are now
  usage errors re-pinned against real output); state the verb set once near the top of the client
  section; keep the init guard section's wordings (unchanged flows).
- [ ] Step 2: SKILL.md: verb-shape every command example; add the deferred note: a redirected
  `/dev/null` stdin answers the guard question with nothing — the run cancels as "no" — so
  scripts must pass `--reinit` deliberately; keep the file near 96 lines.
- [ ] Step 3: run every touched example verbatim against a live server (host adapted only);
  commit `docs: verb-shaped sandbin-client commands`.

## Verification (implementer, after Task 3)

- All five gates; report test counts per package before/after (baseline above).
- CLI battery on ports 18270-18272 with the new binary AND the baseline binary (built from
  `git archive` of the baseline HEAD under /tmp/opencode) side by side: the full 0013 DELETE
  battery via the new `delete` verb vs old `--delete` shape (JSON answers identical), the 0014
  guard battery (`init` refusal/`--reinit`/pty question wordings identical to baseline), upload/
  describe round trips word-for-word vs baseline outputs, env-ladder matrix, exit-code table
  (misuse 1, operational 1, help 0, bare 0), `--version`, `sandbin ip` family incl. `--json`,
  serve via env only (container shape), and the unchanged-HTTP battery proving byte-identity of
  every response.
- Install flow proof: fetch install.sh from the new server, run it piped non-tty (hint) and
  under a python3-pty driver (one key question) — install.sh file itself must stay byte-identical
  to baseline's served body.
- Cleanup: kill servers via /proc scan (pid via basename, Z ignored), `/tmp/opencode` EMPTY at
  the end, binaries only under /tmp/opencode, repo clean apart from `?? .opencode/`.

## Reviewer TODO

1. Gates yourself (all five incl. `-race`); counts vs baseline; no test silently dropped
  (`comm` on test-name lists old vs new).
2. Scope: `git diff <plan-close>..HEAD --name-only` names only Task-listed files + go.mod/go.sum;
  go.sum additions are urfave + transitives only (`go mod verify`, `go mod why github.com/urfave/cli/v2`).
3. Wording contract battery: re-run every pinned operational wording from 0013/0014 old-vs-new,
  diff must be empty (upload refusals, delete refusals, guard refusal/cancel, wizard prompts,
  lockout warning, server errors); check stderr carries exactly one error line (no urfave double
  print).
4. Verb battery old-vs-new: same server state produced by `--delete x --delete-folder d` (old
  binary) and `delete x --folder d` (new binary) → identical JSON + listing; `describe` verb ==
  old `--describe` stdout; unknown-verb upload attempt makes zero requests; bare invocation help
  exit 0 on both binaries; `sandbin notes.md` fails.
5. Ladder battery: flag>env>file matrix per verb; env-only does not count as guard consent;
  `SANDBIN_BIN_PASSWORD` still env-only and sent as Basic Auth by the delete verb; container
  env-only serve still starts keyless when no key anywhere.
6. HTTP byte-identity vs baseline binary (full battery incl. 301, fallback, archives, ip verbs,
  skill/install routes byte-equal).
7. Docs: every README/SKILL.md example and refusal run verbatim (host adapted); SKILL.md line
  count and the new /dev/null sentence accurate against measured behavior.
8. Container proof: `podman build --network=host` from HEAD (urfave deps must download inside
  the build), fresh volume `sandbin-0015-rev` on `-p 127.0.0.1:18275:8080`, serve via env-only
  start, one upload + delete round trip + install.sh fetch byte-equal; full cleanup after
  (keep base images golang:1.26-alpine, alpine:3.22, alpine:latest and pre-existing dangling
  a4493fb07dee); `podman ps -a` and `podman volume ls` empty after.
9. Cleanup confirmation: /proc scan clean of live sandbin, `/tmp/opencode` empty, `git status
  --short` only `?? .opencode/`.

## Skipped on purpose

Everything in the decisions record's Skipped section, plus: shell-completion UX work, help text
customization, `sandbin-client upload` aliases, changing any HTTP verb semantics, and publishing
a new module tag (the release bump stays a human decision).
