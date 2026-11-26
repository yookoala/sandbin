# Env-only client settings — Implementation Plan

## Status

Status: implemented and closed 2026-11-26. Eight commits, `26ebb70` (decision+plan) through `1088e89`
(the docs half of the fix wave): Tasks 1–4 at `70c4998` ("feat(client): a run with no server url
refuses and names the fix"), `c4b1218` ("feat(client): init stops storing settings the environment
supplied"), `7bd3adf` ("docs: the client works from the environment and says so when it cannot") and
`92f17cd` ("docs: record plan 0032 verification and amend 0005, 0011 and 0014"), then the single fix
wave `a741630` ("fix(client): an init url prompt that answers nothing refuses, not guesses"),
`10d2c43` ("test(client): pin that a stored api_key follows the key init verified") and `1088e89`
("docs: the pages say what an env-supplied key leaves the file holding"). Spec:
`docs/decisions/0032-client-env-only-settings.md` (including its execution addendum). Review outcome:
every task passed an independent task-scoped review **first time**, zero Critical and zero Important
each; the whole-branch review (`26ebb70..92f17cd`) verdict "Ready to merge: With fixes" — zero
Critical, three Important (`init`'s url prompt guessing `http://localhost:8080` from a stdin that
answered nothing; the skill page printing the `init` fence above the env-first sentence; this plan's
Architecture paragraph saying "two `clientConfig` fields" where four shipped) and six Minors. The fix
wave closed all eight actionable findings and its scoped re-review (`92f17cd..1088e89`) verdict was
all eight ADDRESSED, with no new Critical or Important breakage and no out-of-scope observation.
Controller rulings P1–P4 and review rulings F1–F12 all honored: F1–F6 fixed in the wave, F7 rejected,
F8–F11 deferred, F12 out of scope, each deferral in one clause under Carried findings below. Final
gates green at `1088e89` under `env -u SANDBIN_API_KEY make test` — `gofmt -l .` empty, `go vet
./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...` — with
`cmd/sandbin-client` **121**, `cmd/sandbin` 260, `internal/frontpage` 17, `internal/robots` 2,
`internal/version` 6: no test deleted, three renamed or replaced, ten added (111 → 115 at Task 1 →
119 at Task 2 → 121 at the fix wave).

Task 4's gates green — `env -u SANDBIN_API_KEY make test` ran twice in this task, once after the
amendments and once after this Status was written, both exit 0 (the per-package times below are
from the first of the two; the re-run was identical apart from timing). The gates are run under
`env -u SANDBIN_API_KEY` because the shell exports a 66-char quote-wrapped `SANDBIN_API_KEY`
which otherwise feeds `serve`'s default and breaks `cmd/sandbin`'s `TestServeFlagParsing`, the
noise Task 1 Step 6 notes: `gofmt -l .` empty, `go vet ./...`,
`go build ./...`, `go test -count=1 ./...` and `go test -race -count=1 ./...` all ok —
`cmd/sandbin` 3.237s / 37.483s, `cmd/sandbin-client` 0.382s / 5.015s, `internal/frontpage`
0.003s / 1.024s, `internal/robots` 0.002s / 1.015s, `internal/version` 0.003s / 1.017s.
Top-level counts at `92f17cd` (`go test -list '.+' | grep -c '^Test'`): `cmd/sandbin-client` **119**
(111 → 115 at Task 1 → 119 at Task 2), `cmd/sandbin` 260, `internal/frontpage` 17,
`internal/robots` 2, `internal/version` 6 — the last four never moved; no test was deleted.

Assertions that moved, exactly the task-listed ones, each verified against HEAD: Task 1's
`TestURLPrecedenceFlagEnvFileDefault` → `TestURLPrecedenceFlagEnvFileThenRefusal` with only its
last block rewritten (verb refusal verbatim, `defaultURL` absent from stderr, config file absent,
request counts 4/2 kept); Task 2's `TestInitStoresTheEnvKey` → `TestInitEnvKeyIsUsedButNotStored`
(assert `cfg.APIKey == ""` and no raw `api_key:` line), `TestInitNonTerminalUrlTakesDefault`
replaced by `TestInitNonTerminalWithoutAnyURLRefuses`, one assertion flipped each in
`TestInitPartialConfigIsNotInitialized`/`url only` (the `cfg.APIKey` half) and
`TestInitReinitOnFreshDirectory` (`cfg.URL != url1 || cfg.APIKey != "test-key"` →
`cfg != clientConfig{}`). New tests: Task 1's four in `main_test.go`
(`TestNoServerURLRefusesNamingInit`, `TestEveryVerbRefusesWithoutAServerURL`,
`TestSettingsRefusalDoesNotConsumeStdin`, `TestEmptyFlagURLIsNoURLAtAll`); Task 2's three
`TestInitEnvOnly*` in `init_test.go` plus the replacement `TestInitNonTerminalWithoutAnyURLRefuses`;
and the controller-ruled `TestInitEmptyFlagsAreNoChoice` — the spec's "the environment alone adds
nothing" governs the brief's looser `c.IsSet` pseudo-code. Fix wave, both in `init_test.go`:
`TestInitDevNullStdinRefusesInsteadOfChoosingTheDefault` and `TestInitEnvKeyReplacesTheStoredKey`.

### The live walk (Task 4)

`make build` produced `dist/sandbin` and `dist/sandbin-client`. The walk ran in
`/tmp/opencode/sandbin-0032-walk` (scratch project directories `proj1` and `proj2`, one
`index.html` each) against a live `serve` on `127.0.0.1:18080` with `--db` inside the scratch
directory, so the db and its control socket stayed in the scratch too. Walk key `walk-0032-key`.
This shell also exports `SANDBIN_URL=https://preview.yookoala.dev` and the 66-char quoted
`SANDBIN_API_KEY`; every command below names its own environment explicitly, so no ambient value
decided any step's output. Server start:

```text
$ env -u SANDBIN_URL SANDBIN_API_KEY=walk-0032-key dist/sandbin serve --addr 127.0.0.1:18080 --db /tmp/opencode/sandbin-0032-walk/server.db
2026-11-27 02:13:27 sandbin control socket on /tmp/opencode/sandbin-0032-walk/server.db.sock
2026-11-27 02:13:27 sandbin listening on 127.0.0.1:18080
```

1. env-only upload in `proj1` — a `created` line with the bin base last, and the file holds only
   `sandbin_url` and `bin`: no `api_key`, no `protected` (this bin has no password), and `ls -a`
   proves no `.gitignore` (only `init` writes one):

```text
$ env SANDBIN_URL=http://127.0.0.1:18080 SANDBIN_API_KEY=walk-0032-key dist/sandbin-client upload index.html
created http://127.0.0.1:18080/c5c1d52e/index.html
http://127.0.0.1:18080/c5c1d52e/
$ cat .sandbin.yaml
sandbin_url: http://127.0.0.1:18080
bin: c5c1d52e
$ ls -a
.	..	.sandbin.yaml	index.html
```

2. the same command again — `updated`, same hash; `grep -c` over `GET /api/bins` proves one bin at
   the server:

```text
updated http://127.0.0.1:18080/c5c1d52e/index.html
http://127.0.0.1:18080/c5c1d52e/
$ curl -s -H "X-API-Key: walk-0032-key" http://127.0.0.1:18080/api/bins
{"bins":[{"bin":"c5c1d52e","url":"/c5c1d52e/","title":"","description":"","has_password":false,"created_at":"2026-11-26T18:13:32Z"}]}
$ curl -s -H "X-API-Key: walk-0032-key" http://127.0.0.1:18080/api/bins | grep -c '"bin"'
1
```

3. same directory, url variable cleared — a refusal naming `init`; the file byte-identical
   (md5 `fda79964fde18d8603e7c20300eeb31c` before and after) and the server still lists one bin:

```text
$ env -u SANDBIN_URL -u SANDBIN_API_KEY dist/sandbin-client upload index.html; echo "exit=$?"
sandbin-client: no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init
exit=1
```

   Two faithful deviations, recorded as seen. The file pins `sandbin_url`, so the url resolves
   from it and the refusal printed is the **key** half — the spec's one-refusal-at-a-time rule
   working as written — which means the plan's step 4 vs step 5 "the same refusal" is really "the
   same family of refusals naming `init`", not the same line. And `-u SANDBIN_API_KEY` had to be
   added to the brief's `env -u SANDBIN_URL`: left in place, this shell's ambient quoted 66-char
   key would have answered the step with a 403 request instead of a refusal — exactly the
   ambient-value trap Task 1 Step 6 warns about.

4. fresh `proj2` with neither variable — the url refusal verbatim, and nothing written:

```text
$ env -u SANDBIN_URL -u SANDBIN_API_KEY dist/sandbin-client upload index.html; echo "exit=$?"
sandbin-client: no server URL: pass --url, set SANDBIN_URL or run sandbin-client init
exit=1
$ ls -a
.	..	index.html
```

5. env-only `init` in `proj2`, then the identical run again with no `--reinit` — `initialized`, an
   empty mapping, and the guard never fires:

```text
$ env SANDBIN_URL=http://127.0.0.1:18080 SANDBIN_API_KEY=walk-0032-key dist/sandbin-client init; echo "exit=$?"
initialized http://127.0.0.1:18080
exit=0
$ cat .sandbin.yaml
{}
$ md5sum .sandbin.yaml && ls -a
8a80554c91d9fca8acb82f023de02f11  .sandbin.yaml
.	..	.gitignore	.sandbin.yaml	index.html
$ # identical re-run, no --reinit: same line, no warning, file byte-identical
$ env SANDBIN_URL=http://127.0.0.1:18080 SANDBIN_API_KEY=walk-0032-key dist/sandbin-client init; echo "exit=$?"
initialized http://127.0.0.1:18080
exit=0
$ md5sum .sandbin.yaml
8a80554c91d9fca8acb82f023de02f11  .sandbin.yaml
```

   `init` also created a `.gitignore` holding exactly `.sandbin.yaml` — 0014's existing behavior,
   untouched by this plan.

6. wrong key in `proj1` (where the file supplies the url) — the client relays the server's own 403
   wording; the file is byte-identical afterwards, and once the lockout the bad attempt triggers is
   cleared (next block), `GET /api/bins` still lists exactly one bin, so the rejected upload wrote
   nothing at the server:

```text
$ env -u SANDBIN_URL SANDBIN_API_KEY=wrong dist/sandbin-client upload index.html; echo "exit=$?"
sandbin-client: 403 invalid API key
exit=1
```

   The plan's "`server rejected this API key`-family wording *from the server*": the upload path
   relays the server's body (`403 invalid API key`); `server rejected this API key` is the client's
   own verify-key wording, which `init` uses. The family holds; the exact line differs by path.

7. the lockout the bad key charged, seen while proving step 6 — an attempt from a blocked address
   is itself a failure even with the correct key, and the block walks up (`handlers.go:201-214`;
   the 0003/0005 rule behind never-retry):

```text
$ curl -s -H "X-API-Key: walk-0032-key" http://127.0.0.1:18080/api/bins
too many bad API keys
$ dist/sandbin ip list --db /tmp/opencode/sandbin-0032-walk/server.db
IP         FAILURES  BLOCKED UNTIL         REMAINING  LAST FAILURE
127.0.0.1  3         2026-11-26T19:44:27Z  1h30m0s    2026-11-26T18:14:27Z
$ dist/sandbin ip unblock --db /tmp/opencode/sandbin-0032-walk/server.db 127.0.0.1
unblocked 127.0.0.1
$ curl -s -H "X-API-Key: walk-0032-key" http://127.0.0.1:18080/api/bins | grep -c '"bin"'
1
```

The `FAILURES 3` row counts three charged attempts and only two are transcribed here: the step-6
wrong key, and the blocked listing above. The third is a second `GET /api/bins` attempt the walk
made with the correct key while the address was still blocked, answered 429 like the first — the
walk record in `.superpowers/sdd/0032-client-env-only-settings/task-4-report.md` logs two such
blocked attempts before the unblock (1 wrong key + 2 attempts while blocked = 3).

Then the server was sent SIGTERM and `127.0.0.1:18080` answered connection-refused; nothing was
created outside `/tmp/opencode/sandbin-0032-walk`.

Carried findings — the six task-review Minors with their whole-branch rulings, then the review's own
deferrals; none blocks the merge:

1. No test pins "bare Enter at a *reachable* `init` url prompt stores `sandbin_url:
   http://localhost:8080`" — `defaultURL` is a constant (`cmd/sandbin-client/main.go:25`), so the
   wizard tests cannot reach a successful write. **Deferred (ruling F8)**: unreachable without a
   listener on `:8080`; the case a person actually hits is the EOF one `a741630` pins.
2. `cmd/sandbin-client/main.go` has the env-fallback pair twice (`readSettings` ~187-192, `runInit`
   ~732-740), mirroring the pre-existing ladder duplication. **Rejected (ruling F7)**: no shared
   helper — `init` needs prompts and provenance the verbs do not, and a helper is the abstraction
   this plan declined.
3. `docs/usages/sandbin-client-upload.md:18` says the file "carries the url and key for a whole
   project" — in an env-only project it carries the url and no key. **Closed at `1088e89`**: the page
   now says the file carries what this project chose to store.
4. `skills/sandbin-preview/SKILL.md` compresses the env rule, losing the "the environment may update
   a field the file already has" nuance that `docs/usages/sandbin-client-init.md`'s Settings
   precedence section states. **Deferred (ruling F9)**: the usage page carries the full rule and the
   skill page points at it.
5. `docs/usages/sandbin-client-init.md` — a colon promising an `--api-key` refusal was followed by a
   fence showing the url refusal (correct order, momentarily mismatched prose). **Closed at
   `1088e89`**: the fence now sits under the url clause it shows and the key refusal is its own
   sentence.
6. Cosmetic: `runInit`'s `flagGiven, keyFlagged := …` line packs two compound expressions into one
   ~110-char line. **Stands**: cosmetic only, and the fix wave left it alone.

Parked by the whole-branch review, each in one clause:

7. This Status walk's remaining evidence unevenness — `exit=$?` missing on two blocks, the one-line
   `.gitignore` stated in prose instead of shown by `cat -A`, and a tab-normalized `ip list` row.
   **Deferred (ruling F10)**: record style, not record integrity.
8. `docs/decisions/0011-client-config.md:11,16` keep their superseded-but-live rows beside the
   amendment at :24. **Deferred (ruling F11)**: 0032 mandates one sentence plus a link, and this repo
   keeps superseded decision text on purpose.
9. Out of scope (ruling F12, every one pre-existing at `26ebb70`): the `v0.4.3` strings in
   `docs/usages/sandbin-client-init.md` pinned by `cmd/sandbin/docs_test.go:534-538`; `delete`
   validating paths before reading settings; 0011's `POST /verify-key` drift; and the absence of a
   changelog entry for this user-visible change.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `sandbin-client` work from `SANDBIN_URL` and `SANDBIN_API_KEY` alone — refusing loudly and naming `init` when no server url comes from anywhere, and never writing a setting into `.sandbin.yaml` that the environment supplied.

**Architecture:** Two code tasks, both inside `cmd/sandbin-client`. Task 1 takes the environment away from the flag parser (`EnvVars` off the two settings flags) and puts one refusal in `readSettings`, which every verb already reads settings through, so the four verbs inherit it. Task 2 changes only what `init` writes, plus `omitempty` on all four `clientConfig` fields; `resolveBin`'s write keeps today's behavior because a bin must be stored with the url it belongs to. Task 3 rewrites the instructions agents actually read. Task 4 walks the flow against a real server and records the outcome.

**Tech stack:** Go 1.26.10+ (`go.mod`), `github.com/urfave/cli/v2` v2.27.7 (flags, no `EnvVars` after Task 1), `gopkg.in/yaml.v3`, stdlib `os`/`errors`. No new dependency. The client's own fake-server harness (`newFake`, `newFakeAt`, `runClient`, `ttyReader`, `initConfig`, `readConfigFile`, `gitignoreBytes`, `noInitFiles` in `cmd/sandbin-client/*_test.go`) is the test surface.

**Spec:** `docs/decisions/0032-client-env-only-settings.md` — executors read both files.

## Global Constraints

- Gates at every task end: `make test`, which is `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`.
- Never push; never tag. Commit per task with the house prefixes (`feat(client):`, `test(client):`, `docs:`).
- Test counts may grow, never shrink: `cmd/sandbin-client` ≥ 111, `cmd/sandbin` 260, `internal/frontpage` 17, `internal/robots` 2, `internal/version` 6 (`go test -list '.+' | grep -c '^Test'`). No test is deleted; renames keep the count.
- Only the assertions named in a task's "assertions that move" list may move. Any other pre-existing test that turns red means the implementation is wrong — fix the code, not the test, and report it rather than bending the assertion.
- Exact refusal wording, verbatim in code and tests: `no server URL: pass --url, set SANDBIN_URL or run sandbin-client init` for the four verbs; `no server URL: pass --url or set SANDBIN_URL` for `init`. The existing `no API key: pass --api-key, set SANDBIN_API_KEY or run sandbin-client init` and init's `…or answer the init prompt` variant keep their wording unchanged.
- Unchanged behavior that must survive: `describe` needs no API key (`main.go:517-518`); `SANDBIN_BIN_PASSWORD` stays env-only (`main.go:285`); the re-init guard still fires only on a file holding both a `sandbin_url` and an `api_key` (`main.go:701-719`); `init` still verifies the key with `POST /api/key` before writing anything; a bin's hash never travels to another server; the protected-bin warning line and the 401 `set SANDBIN_BIN_PASSWORD` hint line keep their wording.
- `defaultURL` (`main.go:25`) stays in the source as `init`'s prompt display and as what a bare Enter at that prompt accepts. It is no longer a fallback for any other path.
- An empty value never becomes a stored setting: `--url ""`, `SANDBIN_URL=""`, or an empty prompt answer contributes nothing and cannot produce an empty `sandbin_url:` or `api_key:` line.
- Do not touch: `cmd/sandbin` sources, `internal/*`, the installer (`install.sh`'s `init --url "$BASE" </dev/tty` flow is pinned by `cmd/sandbin/handlers_test.go:478` and the landing-page tests), `Containerfile`, `deploy/`, `scripts/`, `Makefile`, `cmd/sandbin-client/config.go`'s `readConfig`/`writeConfig`/`ensureGitignore` machinery beyond the two struct tags.

## Review Focus

Failure modes the spec implies and a person will actually hit; each is pinned by a named test in the task that owns the code.

1. **A fresh CI directory with neither variable.** Expected: one line naming `--url`, `SANDBIN_URL` and `init`, zero requests, and no `.sandbin.yaml` written — a refusal must not leave half a setup behind. Pinned in Task 1.
2. **A variable that is set but empty** (`--url ""`, `SANDBIN_URL=""`, or a bare Enter at a non-terminal). Expected: treated as nothing given, never as "the empty string is my server", and never stored as an empty field. Pinned in Task 1 (verbs) and Task 2 (`init`).
3. **A project whose file pins a url, run with `SANDBIN_URL` pointing elsewhere.** Expected: the new server gets its own fresh bin, the pinned server sees zero requests, and the file is rewritten to the url in use. This is `0031`-style honesty about a stored bin and it is the behavior Task 2's write rule could easily break. Pinned in Task 2.
4. **`init --reinit` with both variables exported over a file that already pins both.** Expected: byte-identical `.sandbin.yaml` and `.gitignore` — the environment updating fields it already matches must not empty them. This is the row of the spec's rule 3 an implementer is most likely to get wrong. Pinned in Task 2.
5. **A run whose stdin is `/dev/null`, a pipe, or absent.** Expected: treated as no terminal — `init` refuses rather than hanging on a prompt, no verb asks anything at all, and a piped `upload --name build.ansi` stream is never consumed by settings handling. Pinned in Task 1.

---

### Task 1: A url must come from somewhere, and every verb says so

**Files:**
- Modify: `cmd/sandbin-client/main.go:148-199` (`settingFlags`, `settings`, `readSettings`)
- Modify: `cmd/sandbin-client/main_test.go` (new refusal tests; the default rung of `TestURLPrecedenceFlagEnvFileDefault` at 1369-1417)

**Interfaces:**
- Consumes: `readConfig(dir string) (clientConfig, bool, error)` unchanged; `runUpload`/`runDelete`/`runDescribe`/`runList` all calling `readSettings(c, dir)` before their key guard.
- Produces:
  - `settingFlags()` — two `*cli.StringFlag`s named `url` and `api-key` with **no `EnvVars`**, and `Usage` text that names the variable: `sandbin server base URL ($SANDBIN_URL)` and `pre-shared API key to send as X-API-Key ($SANDBIN_API_KEY)`.
  - `readSettings(c *cli.Context, dir string) (settings, error)` — same signature, same `settings` struct (no new fields). Ladder: `c.String("url")`, then `os.Getenv("SANDBIN_URL")`, then `cfg.URL` when `cfgFound`; if all are empty, return the error `no server URL: pass --url, set SANDBIN_URL or run sandbin-client init` and no `settings`. The key half is `c.String("api-key")`, then `os.Getenv("SANDBIN_API_KEY")`, then `cfg.APIKey`; still no refusal here — the key guards stay in the verbs.
  - Provenance for later tasks is `c.IsSet("url")` / `c.IsSet("api-key")`, now trustworthy because the parser no longer folds the environment into them. Task 2 consumes this; nothing in Task 1 needs it.
- New tests in `main_test.go` (exact names):
  - `TestNoServerURLRefusesNamingInit` — `newFake`, `t.Setenv("SANDBIN_URL", "")`, no config file, `upload index.html`: error non-nil, stderr contains the verb wording verbatim, `f.requestsSeen() == 0`, and `os.Stat(configFileName)` is `os.ErrNotExist`.
  - `TestEveryVerbRefusesWithoutAServerURL` — the same fresh-directory setup through `delete index.html`, `describe` and `list`, each asserting the same wording and `f.requestsSeen() == 0`; `describe` proves it reaches the url refusal while still needing no key (`t.Setenv("SANDBIN_API_KEY", "")`).
  - `TestSettingsRefusalDoesNotConsumeStdin` — `runClient(t, strings.NewReader("piped body\n"), "upload", "--name", "build.ansi")` with `SANDBIN_URL` cleared: the refusal line only, zero requests, and no file; the piped reader is left untouched (`isTerminal`/`stdinCarriesUpload` already make a terminal and a pipe exclusive, `main.go:1145-1162`).
  - `TestEmptyFlagURLIsNoURLAtAll` — `upload --url "" index.html` with `SANDBIN_URL` cleared and no file: the refusal, zero requests, nothing written.
- Assertions that move: `TestURLPrecedenceFlagEnvFileDefault` (`main_test.go:1369-1417`) becomes `TestURLPrecedenceFlagEnvFileThenRefusal`. Its file > env > flag rungs stay as written. Only the last block (1402-1416) changes: instead of expecting stderr to name `defaultURL`, it asserts the verb refusal wording verbatim, `f1.requestsSeen() == 4` and `f2.requestsSeen() == 2` unchanged, `defaultURL` **absent** from stderr, and the config file absent.

- [ ] **Step 1: Write the four failing tests** in `cmd/sandbin-client/main_test.go`, named exactly as above, with the verbatim refusal string as a literal.
- [ ] **Step 2: Run them to confirm they fail** — `go test ./cmd/sandbin-client -run 'TestNoServerURL|TestEveryVerbRefuses|TestSettingsRefusalDoesNotConsumeStdin|TestEmptyFlagURL' -v`. Expected: each fails today by dialing `http://127.0.0.1:8080` or `http://localhost:8080` and reporting a connection error.
- [ ] **Step 3: Implement in `main.go`** — drop `EnvVars` from both flags in `settingFlags` and extend their `Usage`; in `readSettings` add the `os.Getenv` rung after `c.String(...)` and before the file, and replace the `if s.baseURL == "" { s.baseURL = defaultURL }` block with the refusal error. No new fields on `settings`; the key half keeps returning empty for a missing key.
- [ ] **Step 4: Move the one ruled assertion block** in `TestURLPrecedenceFlagEnvFileDefault` and rename the test to `TestURLPrecedenceFlagEnvFileThenRefusal`, per the list above.
- [ ] **Step 5: Run the package and confirm the new tests pass and nothing else moved** — `go test -count=1 ./cmd/sandbin-client`. Expected: PASS, ≥ 115 top-level tests, and no other test renamed or deleted.
- [ ] **Step 6: Run the gates** — `make test`. Expected: green across all five gates. If `cmd/sandbin` fails on `TestServeFlagParsing`, first check `SANDBIN_API_KEY` is not exported in the shell (`docs/decisions/0032` notes this environmental noise) before suspecting the change.
- [ ] **Step 7: Commit** — `git add cmd/sandbin-client/main.go cmd/sandbin-client/main_test.go && git commit -m "feat(client): a run with no server url refuses and names the fix"`.

---

### Task 2: What `init` stores, and the file that omits what it does not have

**Files:**
- Modify: `cmd/sandbin-client/config.go:22-27` (`clientConfig` tags)
- Modify: `cmd/sandbin-client/main.go:674-772` (`runInit`)
- Modify: `cmd/sandbin-client/init_test.go` (flips listed below, plus new tests)
- Modify: `cmd/sandbin-client/main_test.go:1608-1636` only if `TestConfigFileIsPrivateAndHoldsNoBinPassword` proves otherwise — it is listed as stay-green.

**Interfaces:**
- Consumes: Task 1's `c.IsSet("url")` / `c.IsSet("api-key")` as the flag-vs-environment answer; `readConfig`/`writeConfig`/`ensureGitignore` unchanged; `cl.verifyKey()` unchanged.
- Produces:
  - `clientConfig` with `omitempty` on all four tags: `URL` (`sandbin_url,omitempty`), `APIKey` (`api_key,omitempty`), `Bin` (`bin,omitempty`), `Protected` (`protected,omitempty`) — verified on this tree: today `yaml.Marshal(clientConfig{})` answers `sandbin_url: ""\napi_key: ""\nbin: ""\nprotected: false\n`, and no test or reader treats a `bin: ""` or `protected: false` line as data (every `protected:` literal in the client tests is a `writeWorkDirFile` input, and the `bin:` literals at `main_test.go:1938`, `2029`, `2145`, `2334`, `2408` are `describe` output, not the config file). An all-empty config marshals to `{}`, which `readConfig` reads back as a found file holding no settings — `TestConfigFileWithoutABinGetsAFreshBin` (`main_test.go:1320-1343`) keeps proving that shape is legal, not corrupt.
  - `runInit` keeps its ladder, its prompt wording (`sandbin_url [http://localhost:8080]: `), its terminal-Enter-takes-the-default behavior, its guard and its verify-before-write order. What changes is the value it writes, by the spec's rule 3 table, decided per field:
    - persist the url as `base` when the flag was given (`c.IsSet("url")`), or the answer was typed at the prompt, or `cfgFound && cfg.URL != ""`; otherwise persist nothing.
    - persist the key as `key` when `c.IsSet("api-key")`, or the answer was typed, or `cfgFound && cfg.APIKey != ""`; otherwise persist nothing.
    - an empty resolved value is never persisted, whatever the source;
    - `Bin` and `Protected` keep today's rule verbatim: kept when `cfgFound && cfg.URL == base`, dropped otherwise (`main.go:758-763`).
  - `runInit`'s own refusal, reached only when no terminal supplied an answer: `no server URL: pass --url or set SANDBIN_URL`. It sits where today's `base = defaultURL` fallback for a non-terminal run sits, before `verifyKey` and before any write, so nothing is written on refusal.
- New tests in `init_test.go` (exact names):
  - `TestInitEnvOnlyStoresNeitherSetting` — `newFake`, file absent, both variables exported: `init` answers `initialized <url>`, the file exists and is `{}`, `initConfig(t)` reports an empty `clientConfig`, `gitignoreBytes(t)` is `.sandbin.yaml\n`, and `f.requestsSeen() == 1`.
  - `TestInitEnvOnlyIsNotInitializedSoReRunningIsHarmless` — the above, then `init` again with no `--reinit`: the guard never fires, nothing is warned or asked, the second run is a second verify request, and the file is unchanged.
  - `TestInitEnvOnlyUploadStillPairsBinWithItsURL` — env-only `init` (file `{}`), then `upload index.html`: the new file holds `sandbin_url` equal to `os.Getenv("SANDBIN_URL")` and a `bin`, with **no `api_key:` line and no `protected:` line** (this bin has no password), proven on the raw bytes from `readConfigFile` and not only through `initConfig`.
  - `TestInitNonTerminalWithoutAnyURLRefuses` — replaces `TestInitNonTerminalUrlTakesDefault` (`init_test.go:227-242`): `init --api-key test-key` with both variables cleared refuses with `no server URL: pass --url or set SANDBIN_URL`, makes zero requests, and `noInitFiles(t)` still holds.
- Assertions that move, exactly these four:
  - `TestInitStoresTheEnvKey` (`init_test.go:321-336`): the file already names `sandbin_url`, so `cfg.URL != url1` stays a failure; `cfg.APIKey != ""` becomes the failure — rename to `TestInitEnvKeyIsUsedButNotStored` and assert `cfg.APIKey == ""` and that the file's raw bytes contain no `api_key:` line.
  - `TestInitPartialConfigIsNotInitialized` / `url only` (`init_test.go:668-687`): same flip on the `cfg.APIKey` assertion only; `stdout` and no-guard wording assertions unchanged.
  - `TestInitReinitOnFreshDirectory` (`init_test.go:605-629`): `cfg.URL != url1 || cfg.APIKey != "test-key"` becomes `cfg != clientConfig{}`; `initialized <url>` on stdout, empty stderr, the `.gitignore` line and `f.requestsSeen() == 1` all stay.
  - `TestInitNonTerminalUrlTakesDefault` (`init_test.go:227-242`): replaced by the new `TestInitNonTerminalWithoutAnyURLRefuses`.
- Tests that must stay green unchanged, because they are the Review Focus gates: `TestInitReRunIsIdempotent` (`init_test.go:97-122`), `TestInitEnvUrlOverridesTheFileUrl` (`init_test.go:342-362`), `TestInitReinitKeepsBinAndProtected` (`634-662`), `TestInitReinitSkipsTheTerminalQuestion` (`582-601`), `TestInitGuardAsksAtTerminal` (`533-578`), `TestInitWizardEnterKeepsDefault` (`281-300`), `TestInitWizardPromptsAndWrites` (`246-279`), `TestInitWizardEmptyKeyRefuses` (`302-319`), the `key only` and terminal subtests of `TestInitPartialConfigIsNotInitialized` (`688-724`), and `TestConfigFileIsPrivateAndHoldsNoBinPassword` (`main_test.go:1608-1636`).

- [ ] **Step 1: Write the four new tests** named above, plus the four assertion moves, in `cmd/sandbin-client/init_test.go`.
- [ ] **Step 2: Run them to confirm they fail** — `go test ./cmd/sandbin-client -run 'TestInitEnvOnly|TestInitEnvKeyIsUsedButNotStored|TestInitPartialConfigIsNotInitialized|TestInitReinitOnFreshDirectory|TestInitNonTerminalWithoutAnyURLRefuses' -v`. Expected: FAIL on the stored-setting assertions (today `init` writes both) and on the new refusal wording.
- [ ] **Step 3: Add `omitempty` to all four `clientConfig` tags** in `cmd/sandbin-client/config.go` and change nothing else in that file.
- [ ] **Step 4: Implement the `runInit` write rule and its refusal.** One line per field on where the answer comes from: `flagGiven := c.IsSet("url")` and `keyFlagged := c.IsSet("api-key")` before the ladder, `typedURL` / `typedKey` set when a prompt answer is taken, and the persist decision as the four-row table above. Keep `defaultURL` for the terminal-Enter case and keep every refusal before `verifyKey` and before `writeConfig`.
- [ ] **Step 5: Run the package** — `go test -count=1 ./cmd/sandbin-client`. Expected: PASS, and every stay-green test listed above still passing. A stay-green test turning red means the write rule was implemented as "env never writes, even to a field the file has" — the wrong row of the table.
- [ ] **Step 6: Run the gates** — `make test`. Expected: green.
- [ ] **Step 7: Commit** — `git add cmd/sandbin-client/config.go cmd/sandbin-client/main.go cmd/sandbin-client/init_test.go && git commit -m "feat(client): init stops storing settings the environment supplied"`.

---

### Task 3: The instructions agents and humans read

**Files:**
- Modify: `skills/sandbin-preview/SKILL.md:23-32` (the "First-time setup in this directory" section)
- Modify: `docs/usages/sandbin-client-init.md` (`Without a terminal`, `The .sandbin.yaml file`, `Settings precedence`)
- Modify: `docs/usages/sandbin-client-upload.md:16-18` (settings paragraph) and `42-48` (bin finding)
- Modify: `docs/usages/sandbin-client.md:12-13` (the summary rows that describe what the pages cover)
- Modify: `README.md` (the client setup paragraph, if it still presents `init` as the first step)

**Interfaces:**
- Consumes: Task 1's and Task 2's shipped wordings — every refusal quoted in these pages is copied verbatim from the code, not paraphrased.
- Produces: prose only. `install.sh` and the landing-page templates stay byte-identical; `docs/usages/*.md` keep the house style already in them (`{{ .Instance.URL }}` tokens for the live instance, fenced commands with their output in ```text blocks, no invented flags).
- Content each page must say:
  - `SKILL.md`: with `SANDBIN_URL` and `SANDBIN_API_KEY` in the environment, upload directly and `init` is unnecessary; `init` is how a human's own machine saves a key. A refusal is answered by doing what it names. The sentence that promises `init` saves `sandbin_url`, `api_key`, `bin` and `protected` becomes what it now saves, including "a key from the environment is used and never written".
  - `sandbin-client-init.md`: no url from flag, environment or file is a refusal, in both wordings; the prompt's bracketed default survives and a bare Enter at a terminal is choosing it; the file in an env-only project is an empty mapping, then `sandbin_url` and `bin` after the first upload (`protected` appears only for a bin with a password), with no `api_key` line unless a human chose the key.
  - `sandbin-client-upload.md`: drop "default `http://localhost:8080`" from the settings paragraph and add the refusal line; the bin-finding paragraph keeps its promise and states the reason — the file names the server each stored bin belongs to.
- [ ] **Step 1: Rewrite the four pages and the README paragraph.** No new section headings beyond what the pages already have; keep each page's existing table and fence shapes.
- [ ] **Step 2: Check every quoted refusal against the code** — `grep -rn 'no server URL' cmd/sandbin-client docs skills README.md` shows the same string in code and in the pages, and `grep -rn 'localhost:8080' docs/usages/sandbin-client*.md skills` returns only `init`'s prompt text.
- [ ] **Step 3: Run the whole suite** — `go test -count=1 ./...`. Expected: PASS, including `internal/frontpage` (17) and the `cmd/sandbin` docs and installer tests, which proves the docs pipeline and `install.sh` still agree with the pages.
- [ ] **Step 4: Run the gates** — `make test`. Expected: green.
- [ ] **Step 5: Commit** — `git add skills docs README.md && git commit -m "docs: the client works from the environment and says so when it cannot"`.

---

### Task 4: Prove the flow end to end and record it

**Files:**
- Create: `docs/decisions/0032-client-env-only-settings.md` if Task 1 has not already landed it (it exists at plan time; amend it in place if anything here differs)
- Modify: `docs/decisions/0005-client-defaults.md`, `docs/decisions/0011-client-config.md`, `docs/decisions/0014-reinit-guard-and-installer-init.md` (one amendment line each, pointing at 0032)
- Modify: `docs/plans/0032-client-env-only-settings.md` (the Status line this task writes)

**Interfaces:**
- Consumes: everything Tasks 1-3 shipped; `make build` for `dist/sandbin` and `dist/sandbin-client`.
- Produces: a scratch walk, run in a throwaway directory under `/tmp/opencode` against a real `serve` on a free port, whose exact outputs are transcribed into this plan's Status section. Nothing in the repository other than the docs named above.
- The walk, in order, each step recorded with its real output:
  1. `SANDBIN_API_KEY=<key> ./dist/sandbin serve --addr 127.0.0.1:18080` and a scratch project directory with one `index.html`.
  2. `SANDBIN_URL=http://127.0.0.1:18080 SANDBIN_API_KEY=<key> sandbin-client upload index.html` → a `created` line, then the bin base; `cat .sandbin.yaml` shows `sandbin_url` and `bin` only — no `api_key`, no `protected` (this bin has no password); `ls` shows no `.gitignore` (only `init` writes one).
  3. the same command again → `updated`, same hash, `grep -c` proves one bin at the server.
  4. `env -u SANDBIN_URL sandbin-client upload index.html` in the same directory → the refusal naming `init`, nothing new at the server, file unchanged.
  5. a second scratch directory with neither variable exported → the same refusal, `ls -a` proves no `.sandbin.yaml`.
  6. `SANDBIN_URL=… SANDBIN_API_KEY=… sandbin-client init` in that second directory → `initialized <url>` and `cat .sandbin.yaml` is `{}`; run it again with no `--reinit` → same line, no warning, file unchanged.
  7. wrong key: `SANDBIN_API_KEY=wrong sandbin-client upload index.html` → `server rejected this API key`-family wording from the server, no file written.
- [ ] **Step 1: Build** — `make build`.
- [ ] **Step 2: Run the walk** in `/tmp/opencode/sandbin-0032-walk`, recording each command with its output.
- [ ] **Step 3: Add the three amendment lines** to `docs/decisions/0005`, `0011` and `0014`, each one sentence plus a link to `0032`, worded as in the decision doc's "Amendments" section.
- [ ] **Step 4: Write the Status section** at the top of this plan: commit list per task, the transcribed walk outputs, the final gate results with the test counts (`cmd/sandbin-client`, `cmd/sandbin`, `internal/*`), every assertion that moved, and any carried finding.
- [ ] **Step 5: Run the gates one final time** — `make test`. Expected: green with the counts at or above the Global Constraints floor.
- [ ] **Step 6: Commit** — `git add docs && git commit -m "docs: record plan 0032 verification and amend 0005, 0011 and 0014"`.

---

## Self-review note

Spec coverage: the decision doc's eight rows land on Task 1 (url ladder, no-verb-prompts, one-refusal-at-a-time), Task 2 (env never creates a field, bin paired with its url, `omitempty`, guard untouched, `EnvVars` off the flags), Task 3 (what changes for a person) and Task 4 (amendments, verification). Review Focus lines 1, 2 and 5 are pinned in Task 1, 3 and 4 in Task 2. Test counts, gate commands, refusal strings and the stay-green list are copied from the code and this repo's existing plan style rather than invented.

Builder calls recorded here, both mine rather than the partner's: `omitempty` on all four `clientConfig` tags rather than the two the partner named, because `bin: ""` and `protected: false` are the same noise they objected to and `{}` was verified as the all-empty marshaling on this tree; and no new fields on `settings`, since only `init` needs provenance and it can ask `c.IsSet` directly once Task 1 removes `EnvVars`.
