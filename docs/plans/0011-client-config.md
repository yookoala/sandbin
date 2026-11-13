# Plan: client config file, init wizard, key-check endpoint

Status: closed. Implemented on `main` (`eb1cef6`, `826444e`, `edc9ca3`, `1a10cdb`), reviewed with
verdict **ship** and no blockers. Decisions in `docs/decisions/0011-client-config.md`. Extends
the client state handling from earlier plans and sits beside `docs/plans/0010-archive-upload.md`.

**Goal:** `sandbin-client` reads its server URL and API key from a per-project `.sandbin.yaml`
created by an interactive `sandbin-client init`, verified against a new `POST /verify-key`
server endpoint.

**Architecture:** the server gains one key-checked route beside `/healthz` and `POST /` with the
identical block/charge guard. The client replaces `state.go` (JSON) with `config.go` (YAML via
`gopkg.in/yaml.v3`), threads the file under flag and env in resolution, and adds an `init` verb
that prompts, verifies, writes the file and gitignores it.

**Tech stack:** Go stdlib plus `gopkg.in/yaml.v3` (the only new dependency, a deliberate
decision — do not hand-parse YAML).

## Global Constraints

- Gates: `gofmt -l cmd` empty, `go vet ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`.
- Do not touch: `control.go`, `ipcmd.go`, `ipblock.go`, ansi files, `archive.go`, `store.go`,
  `Containerfile`, `.github/`, deploy/. `go.mod`/`go.sum` gain only `gopkg.in/yaml.v3`.
- Existing client behaviour (flags, stdin, archive, metadata, result lines) stays byte-identical
  when no `.sandbin.yaml` exists.
- Refusals are worded so docs, code and tests agree; failures go to stderr, never stdout.
- The key is never printed by the client, in any output, success or failure.

## Review Focus

- A hand-edited or corrupt `.sandbin.yaml` must fail loudly naming the file — silently falling
  through would make a second bin for one prototype.
- A file holding a `bin` made for server A must not be reused when the run resolves to server B.
- `GET /verify-key` must stay 405: the endpoint must not become an uncharged key oracle.
- A wrong key through `/verify-key` charges like `POST /` does; a blocked address stays blocked.
- `.sandbin.yaml` is 0600 and `.gitignore`-guarded; an upload run must not persist a key the
  user only passed through env.

## Task 1: `POST /verify-key` on the server

**Files:** `cmd/sandbin/handlers.go`, `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Produces: route `POST /verify-key` → 200 `key ok`; 403 `invalid API key`; 429 `too many bad
  API keys`; other methods 405. Handler `func (s *server) verifyKey(w http.ResponseWriter,
  r *http.Request)`.

- [ ] Step 1: write failing tests in `handlers_test.go` — correct key 200 body `key ok`; wrong
  key 403 **and** an `ip_blocks` row (reuse the charging-test pattern from
  `TestWrongKeyBlocksAddress`); missing header 403 and charged; blocked address 429 with
  `Retry-After` and the attempt itself charged; `GET`/`HEAD`/`PUT /verify-key` 405; a `PUT
  /<hash>/verify-key` still stores a normal file (routing does not shadow bins); a key-correct
  `/verify-key` call charges nothing.
- [ ] Step 2: run them, confirm they fail.
- [ ] Step 3: implement. In `ServeHTTP`, after the `healthz` branch and before `splitBinPath`:
  `if r.URL.Path == "/verify-key"` → non-POST is 405, POST calls `verifyKey`. `verifyKey` copies
  `createBin`'s guard verbatim (block check → `RecordFailure` on block → key compare →
  `RecordFailure` on mismatch) and then answers `text/plain; charset=utf-8` body `key ok`. A
  one-line comment states the dash keeps it clear of hex bin hashes.
- [ ] Step 4: `go test -count=1 ./cmd/sandbin` green.
- [ ] Step 5: commit `feat: sandbin answers POST /verify-key with a key check`.

## Task 2: `.sandbin.yaml` replaces `.sandbin.json`

**Files:** create `cmd/sandbin-client/config.go`, delete `cmd/sandbin-client/state.go`, modify
`cmd/sandbin-client/main.go`, `cmd/sandbin-client/main_test.go`

**Interfaces:**
- Consumes: `flagGiven`, `envOr`, `isTerminal` (existing helpers in the client).
- Produces: `configFileName = ".sandbin.yaml"`; `type clientConfig struct { URL string
  `yaml:"sandbin_url"`; APIKey string `yaml:"api_key"`; Bin string `yaml:"bin"`; Protected bool
  `yaml:"protected"` }`; `readConfig(dir string) (clientConfig, bool, error)` (missing file →
  `found=false`; unreadable/unparsable → error naming the path); `writeConfig(dir string, cfg
  clientConfig) error` (yaml.Marshal, temp+rename in dir, 0600 — reuse `writeAndClose`).
- Resolution in `run()`, before the key guard: `baseURL` = flag if `flagGiven` else
  `SANDBIN_URL` if set else file `sandbin_url` else `defaultURL`; `apiKey` = flag if `flagGiven`
  else `SANDBIN_API_KEY` if set else file `api_key`. Key guard wording becomes: `no API key:
  pass --api-key, set SANDBIN_API_KEY or run sandbin-client init`.
- `resolveBin`/`resolveExistingBin` read the config instead of the state; the URL-match rule and
  the write-back rule from the decisions apply (`api_key` preserved, never newly written).

- [ ] Step 1: write failing tests — YAML round trip through the client's own paths; corrupt YAML
  (`not: [valid`) errors naming `.sandbin.yaml` and makes zero requests; precedence matrix for
  url (flag > env > file > default) and key (flag > env > file); the new key-guard wording;
  file `bin` reused only when file url equals resolved base (env override proves non-reuse); an
  upload run that passes `--api-key` with no file key writes a file whose `api_key` stays empty;
  file `bin`/`protected` survive a `--new` run's metadata and vice versa the url change drops
  the stale bin. Move the existing `.sandbin.json` fixtures/tests over rather than duplicating
  them.
- [ ] Step 2: run them, confirm they fail.
- [ ] Step 3: implement `config.go` (struct, readConfig, writeConfig with the existing
  temp+rename+0600 pattern), delete `state.go`, thread resolution through `run()`.
- [ ] Step 4: `go test -count=1 ./cmd/sandbin-client` green; existing upload/archive/metadata/
  stdin tests pass with at most fixture renames (assertions unchanged).
- [ ] Step 5: commit `feat: sandbin-client keeps its setup in .sandbin.yaml`.

## Task 3: `sandbin-client init`

**Files:** `cmd/sandbin-client/main.go`, `cmd/sandbin-client/client.go`, `cmd/sandbin-client/config.go`,
`cmd/sandbin-client/main_test.go`, new `cmd/sandbin-client/init_test.go`

**Interfaces:**
- Consumes: `readConfig`/`writeConfig`, the client's HTTP plumbing.
- Produces: in `run()`, `args[0] == "init"` dispatches to `runInit(args[1:], stdin, stdout,
  stderr)` **before** the normal FlagSet parses; `runInit` parses its own FlagSet with only
  `--url` and `--api-key`. `(*client) verifyKey() error` POSTs `<base>/verify-key` with the key
  header and maps: 200 → nil; 403 → `server rejected this API key`; 429 → `server is blocking
  this address: too many bad API keys`; transport error → `cannot reach <base>: <err>`.
- Wizard: value order flag > env > file. Missing on terminal (`isTerminal(stdin)`) → prompt
  `sandbin_url [shown default]: ` and `api_key: ` through a `bufio.Reader` on stdin, Enter
  accepts the shown default for the url, empty key after prompt stays "missing". Missing on
  non-terminal: url takes `defaultURL`, missing key refuses with
  `no API key: pass --api-key, set SANDBIN_API_KEY or answer the init prompt`.
- After a successful `verifyKey`: save the file (bin/protected kept only when the old file's url
  equals the url being saved), `ensureGitignore(dir)` (create `ensure` semantics per decisions),
  print to stdout `initialized <base>` — the key never appears in any output. Verify failure:
  error out, **nothing written**.

- [ ] Step 1: write failing tests in `init_test.go` using the existing fakeServer harness (give
  it a `/verify-key` handler): happy init writes 0600 file with the right keys and creates
  `.gitignore` containing `.sandbin.yaml`; re-init keeps an existing gitignore byte-identical;
  existing gitignore without the line gets exactly one appended line; 403 from verify → no file,
  no gitignore, refusal wording; 429 wording; dead port → `cannot reach` wording, nothing
  written; pre-existing bin/protected kept on same url and dropped on url change; non-terminal
  missing key refuses; a key that came only from env appears in the written file **only** for
  init (that is init's job) and init's stdout/stderr contain no trace of the key literal.
- [ ] Step 2: run them, confirm they fail.
- [ ] Step 3: implement dispatch, `runInit`, `verifyKey`, `ensureGitignore`. One `shortcut:`
  comment for the visible-echo prompt per the decisions.
- [ ] Step 4: `go test -count=1 ./cmd/sandbin-client` and `-race` green.
- [ ] Step 5: commit `feat: sandbin-client init sets up a project`.

## Task 4: README

**Files:** `README.md`

- [ ] Step 1: add a "Point sandbin-client at a server" section: `sandbin-client init` (wizard
  shape, non-TTY flag/env shape), the `.sandbin.yaml` example block (all four keys), the
  precedence line, the gitignore note, and that only `init` stores a key. Add `POST
  /verify-key` to the API section (guard/charge wording, `key ok`). Note that `.sandbin.json` is
  gone without migration.
- [ ] Step 2: run every example in the section verbatim against a live server (adapt only host
  and key) before committing.
- [ ] Step 3: commit `docs: configuring sandbin-client with init`.

## Reviewer TODO

1. Run all four gates yourself; count tests per package before/after.
2. Verify the guard in `verifyKey` matches `createBin`'s shape line for line (block, charge on
   block, charge on bad key, no charge on success); prove 403 charges and escalates to 429 with
   growing `Retry-After` via `sandbin ip` verbs against a live db; prove GET/HEAD 405 (no key
   oracle) and that `/<hash>/verify-key` still stores files.
3. Config adversarial battery on a live setup: corrupt YAML, empty file, `protected: maybe`
   (bad bool), file readable but not writable scenario for writeConfig (temp+rename failure),
   0600 mode asserted with a loose umask, key never visible in captured stdout/stderr of init.
4. Precedence matrix run for real: flag/env/file/default combinations both for url and key;
   env-over-file bin non-reuse; upload run with env-only key leaves file `api_key` empty.
5. init wizard live proof through a real pty (`script -qc` or equivalent), including Enter-on-
   default and a typed key; if the sandbox cannot fake a terminal, prove the whole wizard at
   unit level and record that honestly.
6. `.gitignore` behaviours (create/append/skip) and repeated inits idempotent (file byte-identical
   on re-init with unchanged inputs).
7. Untouched-path byte-identity: with no `.sandbin.yaml`, client behaviour (stdout lines, state
   absence) matches the pre-plan binary; server front page, healthz, bins, archives unaffected.
8. README: every recipe in the new section run verbatim.
9. `git diff` scope: only the files the tasks name; `go.mod` gains exactly `gopkg.in/yaml.v3`;
   no `.sandbin.json` reference survives anywhere (`grep -rn sandbin.json`).
10. Cleanup: servers killed via `/proc` scan (no ps/pgrep/pkill; Z zombies are leftovers),
    `/tmp/opencode` empty, ports 18240+, repo clean apart from `?? .opencode/`.

## Skipped on purpose

Relaxed charging for `/verify-key` (user deferred it explicitly), global config under
`~/.config`, migration from `.sandbin.json`, hidden echo for the key prompt (`x/term`), key
strength validation client-side (the server is the authority), `init --bin`, multiple config
lookup levels (walk-up to parent dirs), Windows support.

## Outcome

Tests went 153 + 2 / 57 to 157 + 2 in `cmd/sandbin` and 79 (+13 subtests) in
`cmd/sandbin-client`; `gofmt`, `go vet`, `go test -count=1` and `go test -race` green at HEAD,
run by both implementer and reviewer.

The review proved live: `verifyKey`'s guard byte-identical to `createBin`'s (its own `diff`
came back empty) — 403 charges through XFF-faked IPs, escalation walks `Retry-After`, blocked
attempts charge, a correct call charges nothing, `GET /verify-key` with the *correct* key still
405s before any key check (no uncharged oracle), and `/<hash>/verify-key` stores ordinary
files. The config battery ran corrupt YAML (zero requests), `protected: maybe`, config-as-
directory, 0600 under loose umask and over a 0644 file, key literal grepped 0 from init's
captured stdout and stderr, env-only keys never persisted, the full precedence matrix, and the
wizard live through a python3 `forkpty` pty including Enter-on-default. `ensureGitignore` is
whole-line matching: a trailing-space line counts as present (git agrees), a commented one
appends; verify failure and write failure both leave the gitignore untouched. Byte-identity
against the `bb8af64` binaries held across front page, healthz, PUT/GET/ANSI/listing/PATCH, ip
verbs and a plain no-config client run word for word. Test-integrity check: all 9 moved
state-file tests kept their assertion bodies; the one deleted test's semantics were
deliberately overturned by the decisions and its inverse exists.

Accepted as they are: `--api-key ""` counts as a flag given on upload runs (refuses despite a
good file key) but falls through to env/file/prompt in `init` — flagGiven semantics, code
commented, not spelled in the README; an un-writable gitignore (made a directory) aborts init
after the verified config was already written — loud, exit 1; `init` writes `bin: ""` before
the first upload (the README example shows a completed setup); the wizard echoes the typed key
through the terminal (`shortcut:` in the decisions). Left unproven live under the root sandbox:
`writeConfig`/`ensureGitignore` rename-write failures (unit-covered via a missing directory and
a directory-named config) and `verifyKey`'s odd-status arm, which the reviewer proved live
against a 500 server anyway.
