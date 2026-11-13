# Plan: the init re-initialization guard, and the installer running init

Status: closed. Implemented on `main` (`7a52ca1`, `ed8afb6`, `8dbb1f0`), reviewed with verdict
**ship** and no blockers. Decisions in `docs/decisions/0014-reinit-guard-and-installer-init.md`.
Sits on `docs/plans/0011-client-config.md` (the init wizard this guards) and plan 0012 (the
installer).

**Goal:** re-running `sandbin-client init` in a project whose `.sandbin.yaml` already holds both
`sandbin_url` and `api_key` is an explicit act — a `[y/N]` question at a terminal, a refusal
pointing at `--reinit` elsewhere — and the installer one-liner then runs `init` against the
serving URL, at a terminal asking only for the API key.

**Architecture:** guard logic goes inside `runInit` (`cmd/sandbin-client/main.go`) between
`readConfig` and the value ladder, reusing the existing `isTerminal` and the wizard's
`bufio.Reader`; `skills/sandbin-preview/install.sh` (a rendered template) gains an init step
after the go-install block, gated on `[ -t 1 ]`, a working `/dev/tty` and `init`'s stdin
redirected from `/dev/tty`; `cmd/sandbin` changes only in the front-page paragraph and the
install/front-page test markers.

**Tech stack:** Go stdlib; no new dependency; no server behavior change beyond front-page text.

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- Do not touch: `cmd/sandbin/store.go`, `control.go`, `ipcmd.go`, `ipblock.go`, `archive.go`,
  the DELETE/GET handlers and the 301 redirect from `9467d78`, `skill.go`, `SKILL.md` template
  rendering, deploy/, `Containerfile`, `.github/workflows/ci.yml`. `go.mod`/`go.sum` unchanged.
- Every non-init flow of the client (uploads, deletes, `--describe`, `--new`) answers exactly as
  before. The url/key wizard prompts keep their terminal-only reading and flag > env > file
  ladder; only the new guard question is added.
- The guard fires before any prompt, any `POST /verify-key` and any write; a refused re-init
  sends zero requests and writes zero files.
- No real host, key or bin in README/front-page additions; SKILL.md keeps its `{{.Base}}`
  template tokens.

## Review Focus

- Consent is consent: `--url`/`--api-key` values alone never satisfy the guard; only a `y` at a
  terminal or `--reinit` does; partial configs (url-only or key-only files) never trigger it.
- A refused re-init charges nothing and requests nothing (block table and server proof), and a
  cancelled question (n / Enter / EOF) exits 0 leaving every byte untouched.
- The installer's `/dev/tty` redirect means a piped `curl | bash` cannot let init eat the rest
  of the script; the gate `[ -t 1 ]` + openable `/dev/tty` means agents never hang on a prompt;
  a failed init never fails the installer.
- At a terminal in a fresh directory the installer-init asks exactly one question (api_key); the
  url never gets asked because `--url` carries `{{.Base}}`.
- README/SKILL/front-page wordings run verbatim (host adapted only).

## Task 1: the re-initialization guard

**Files:** modify `cmd/sandbin-client/main.go`, `cmd/sandbin-client/init_test.go`

**Interfaces:**
- Produces: in `runInit`, a guard between `readConfig` and the ladder; `--reinit` bool flag in
  init's FlagSet (help text names it); the verbatim refusal/warning/cancel/question wordings
  from the decision table (question on stdout, warning and refusal on stderr with the usual
  `sandbin-client:` error prefix on the refusal only).

- [ ] Step 1: write failing tests (fake-server style, `ttyReader` for terminal cases):
  initialized directory + non-terminal re-run → exit error, stderr word-for-word naming the url,
  `--reinit` and deleting the file; config file byte-identical and `.gitignore` untouched; same
  refusal when `--url`/`--api-key` are supplied (and with a WRONG `--api-key` the fake server
  saw zero verify-key requests — the guard precedes verification); initialized + `--reinit` →
  success, `initialized <url>` line, file byte-identical (update
  `TestInitReRunIsIdempotent` to pass `--reinit`), `bin`/`protected` preserved; `--reinit` on a
  fresh directory → ordinary init; url-only and key-only files → ordinary init, no question;
  terminal + warning wording + `y` answer → wizard proceeds to `initialized <url>`; terminal +
  `n` answer → exit nil, stdout cancel wording, file untouched, zero requests; terminal + bare
  Enter → same as `n`; question prompt wording pinned (`re-initialize? [y/N]: `).
- [ ] Step 2: run them, confirm they fail.
- [ ] Step 3: implement the guard exactly per the decision table; keep `--help` usage text
  updated for `--reinit`.
- [ ] Step 4: client tests green; commit `feat: init asks before overwriting an initialized project`.

## Task 2: the installer's init step and the front page clause

**Files:** modify `skills/sandbin-preview/install.sh`, `cmd/sandbin/handlers.go` (front page
paragraph only), `cmd/sandbin/handlers_test.go`

**Interfaces:**
- Consumes: Task 1's init behavior (question via stdin=/dev/tty, refusal wording).
- Produces: install.sh init step per the decision table — after the go-install block,
  `CLIENT="$DEST/sandbin-preview/sandbin-client"`, run only when it exists and `[ -t 1 ]` and
  `/dev/tty` opens, `init --url "$BASE" </dev/tty` guarded with `||` + a warning naming the
  exact command and `--reinit`; else a non-terminal hint line; header comment updated. Front
  page gains one clause under the skill-install paragraph naming the single key question.

- [ ] Step 1: extend the install.sh serve test with a marker (e.g. `init --url` appearing in
  the body) and `TestFrontPageAtRoot` with the exact new clause — failing first.
- [ ] Step 2: confirm they fail.
- [ ] Step 3: write the shell step (mind `set -euo pipefail`: the init call carries `||`, the
  `/dev/tty` open probe lives in the if-condition) and the front-page clause.
- [ ] Step 4: `go test -count=1 ./cmd/sandbin` green; commit
  `feat: the installer initializes the directory, asking only for the API key`.

## Task 3: README and SKILL.md

**Files:** modify `README.md`, `skills/sandbin-preview/SKILL.md`

- [ ] Step 1: README init section: guard paragraph with the verbatim refusal, the `[y/N]`
  question behavior, `--reinit`, and preservation of `bin`/`protected` on a confirmed re-init;
  update the "Re-running init re-verifies and rewrites" sentence (it describes the old flow).
- [ ] Step 2: README install section: the installer now initializes the current directory (one
  key question at a terminal; hint line otherwise, word-for-word with the script).
- [ ] Step 3: SKILL.md init section: one guard sentence (`already initialized → refuses; pass
  --reinit or delete .sandbin.yaml`) — keep the file near its current ~96 lines.
- [ ] Step 4: run every touched example verbatim against a live server (host adapted only);
  commit `docs: the re-init guard and the installer's key question`.

## Verification (implementer, after Task 3)

- `gofmt -l .` empty; `go vet ./...`; `go build ./...`; `go test -count=1 ./...`;
  `go test -race -count=1 ./...`; report test counts per package before/after.
- Live battery on ports 18264-18266 with the freshly built binary: fresh init in a scratch
  project; non-terminal re-init refusal (exit code, stderr word-for-word, file byte-identical);
  refusal with a WRONG `--api-key` leaves `sandbin ip list` empty (no request made); `--reinit`
  re-init keeps `bin`/`protected` byte-identically; `--reinit` on a fresh directory; url-only
  file proceeds without asking.
- Terminal battery driven through python3 `os.openpty` (no `script` exists in this sandbox):
  spawn bash/init with the pty slave as stdin/stdout/stderr (`setsid` then `TIOCSCTTY` so
  `/dev/tty` resolves), assert the warning + `re-initialize? [y/N]: ` wording, feed `y` (proceeds
  to the key prompt in the installer flow — confirm exactly ONE key question and no url question)
  and feed `n`/empty (exit 0, cancel wording, files untouched).
- Installer battery: `bash install.sh -d <scratch>` under the pty harness against a live server
  — full flow with one typed key lands `.sandbin.yaml` in the CWD with the right url and a
  working bin afterward; re-run in the initialized directory answers `n` → untouched + exit 0,
  answer `y` → re-initialized; plain non-tty run prints the hint line and exits 0; run with go
  stripped from PATH → no-go warning, no init attempted; `curl -fsSL <url>/install.sh | bash`
  form piped under the pty harness completes without init eating the script (script fully runs).
- Cleanup: kill every server via /proc scan (pid via basename, no ps/pgrep/pkill; Z leftovers
  ignored), `/tmp/opencode` empty at the end, binaries only under `/tmp/opencode`, repo tree
  clean apart from `?? .opencode/`.

## Reviewer TODO

1. Gates yourself (all five, incl. `-race`); test counts per package vs the baseline above.
2. Scope: `git diff 9467d78..HEAD --name-only` names only Task-listed files; `go.mod`/`go.sum`
   untouched; three commits with the plan's messages.
3. Guard battery live (ports 18267+): every decision row incl. zero-request proof on refusals
   (`sandbin ip list` before/after with a wrong key; a fake-IP `X-Forwarded-For` wrong-key
   ordinary init still charges exactly one — the guard must not change ordinary charging),
   partial-config matrix (no file / url-only / key-only / both), exit codes (refusal 1,
   cancel 0), file byte-identity after every refusal/cancel, `--reinit` preservation of
   `bin`/`protected` and its no-op on fresh directories.
4. Terminal battery via your own python3 openpty driver: warning/question/cancel wordings
   word-for-word vs README, `y`/`n`/bare-Enter/EOF all behave, `yes` uppercase answers, the
   wizard after a `y` still asks only the missing values, and a refused init in the SAME
   directory immediately followed by `init --reinit` succeeds.
5. Installer battery: the five flows from the Verification list, each with verbatim stdout
   comparisons vs README/SKILL.md; prove the `[ -t 1 ]` gate (no tty → no init attempt, no hang,
   exit 0); prove the `/dev/tty` redirect (piped curl form runs start to finish); prove a wrong
   typed key warns and still exits 0 with the skill installed and no `.sandbin.yaml`; prove no-go
   skips init entirely.
6. Byte-identity: everything this plan must not touch answers exactly as at `9467d78` (build
   the old binary from `git archive 9467d78` under /tmp/opencode): the 301 redirect battery,
   DELETE/fallback battery, healthz/verify-key, SKILL.md route body (template unchanged),
   front page apart from the new clause (diff), client upload/delete/describe round trips.
7. Front page + README + SKILL.md: markers vs served page; SKILL.md line count near 96 with
   its new wording runnable; no new real host/key/bin (grep).
8. Container spot proof: `podman build --network=host` from HEAD, fresh volume
   `sandbin-0014-rev` on `-p 127.0.0.1:18269:8080`, fetch install.sh through the port and run
   it piped non-tty (hint path), then one full pty-driven install + init round trip through the
   port; remove container/volume/own images after (keep golang:1.26-alpine, alpine:3.22,
   alpine:latest and the pre-existing dangling a4493fb07dee); `podman ps -a` and `podman volume
   ls` empty after.
9. Cleanup confirmation: /proc scan clean of live sandbin, `/tmp/opencode` empty, `git status
   --short` shows only `?? .opencode/`.

## Skipped on purpose

Piped `y` answers (the user chose `--reinit` instead), consent memory across invocations,
`--force` aliases, guarding partial config files, server-side init state, the installer forcing
`--reinit` or accepting an `--api-key`, masking the key prompt (existing shortcut stands), and
re-running the wizard on refusal.

## Outcome

Tests went 91 + 13 to 99 + 28 in `cmd/sandbin-client` (+8 guard tests; `cmd/sandbin` stayed
179 + 19 with markers extended in place); all five gates green at every task commit and at HEAD,
run by implementer and reviewer, with `comm` on test-name lists proving zero removals.

The review proved live: every refusal wording byte-for-byte (naming the stored url, never the
key) with exit 1 and the config byte-identical after each refusal; refusals send zero requests
(`ip list` empty before and after a wrong-key re-run) while a contrast ordinary wrong-key verify
charges exactly one via an X-Forwarded-For fake IP; the partial-config matrix, `--reinit`
preservation (and correct dropping of `bin`/`protected` on a url change), and its no-op on fresh
directories all matched the decision table. The pty battery pinned the terminal flow — warning,
`re-initialize? [y/N]: `, y/Y/yes/YES proceed with no extra prompts, n/bare-Enter/EOF cancel at
exit 0 leaving every byte untouched, and cancel-then-`--reinit` succeeding. The installer asks
exactly one question (the key; zero url prompts), a piped `curl | bash` completes with an
appended command after the pipeline still executing (init ate only `/dev/tty`), a wrong typed key
warns yet exits 0 with the skill installed and no config written, and the go-less path attempts
no init. Byte-identity against `9467d78` binaries held across a 30-step battery: only the front
page clause, the install.sh step and one SKILL.md line differ. The container served the full
pty install + init + upload round trip through its port.

Deviation verdicts (reviewer): updating `TestInitEnvUrlOverridesTheFileUrl` to pass `--reinit`
keeps every original assertion (mandatory, not a softening); `/dev/null` stdin counts as a
terminal (pre-existing `isTerminal` char-device rule) so it answers the guard question, hits EOF
and cancels at exit 0 — accepted, with a follow-up SKILL.md sentence deferred to the next plan; the HEAD-binary substitution for guard-dependent installer proofs (published
`@latest` v0.1.2 predates the guard) judged honest — a release-bump concern, not a repo defect;
the plan's baseline naming `9467d78` vs HEAD `94c2cf2` confirmed docs-only delta.
