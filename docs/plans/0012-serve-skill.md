# Plan: serve the sandbin-preview agent skill

Status: closed. Implemented on `main` (`ba17b36`, `4c7da24`, `4179b73`), reviewed with verdict
**ship** and no blockers. Decisions in `docs/decisions/0012-serve-skill.md`. Sits beside
`docs/plans/0011-client-config.md` (the client the skill documents).

**Goal:** the repo ships `skills/sandbin-preview/SKILL.md`, the server embeds and serves it at
`GET /skills/sandbin-preview/SKILL.md`, and front page + README show a one-snippet install for
OpenCode.

**Architecture:** a module-root `skill.go` embeds the canonical file (go:embed cannot cross
package dirs and rejects symlinks — decided, do not re-litigate); `cmd/sandbin` serves the
embedded string from a static route beside `healthz` and `verify-key`; the front page and README
gain the curl install snippet.

**Tech stack:** Go stdlib (`embed`); no new dependency.

## Global Constraints

- Gates: `gofmt -l .` empty (the CI step itself changes from `-l cmd` to `-l .`), `go vet ./...`,
  `go test -count=1 ./...`, `go test -race -count=1 ./...`.
- Do not touch: client code, `control.go`, `ipcmd.go`, `ipblock.go`, ansi files, `archive.go`,
  `store.go`, deploy/. `Containerfile` gains exactly the two COPY lines from the decisions;
  `.github/workflows/ci.yml` gains exactly the gofmt argument change.
- No bin, key, real host or address appears in SKILL.md or in the front page addition.
- Existing routes keep answering exactly as before.

## Review Focus

- A bin whose subpath is `skills/...` (e.g. `PUT /<hash>/skills/x`) must still store and serve
  normally — the static route is only the exact root path.
- The served bytes must always equal the repo file (a reader editing `skills/.../SKILL.md` and
  rebuilding gets the edit served — the freshness test is the guarantee).
- Method discipline: only GET answers; POST/PUT/PATCH/HEAD/OPTIONS to the skill path 405 with
  nothing charged.
- Copy-paste trust: front page and README snippets must each run verbatim (host adapted only).
- The container image still builds with the new COPY lines and answers the skill route.

## Task 1: the skill file, the embed, the route

**Files:** create `skills/sandbin-preview/SKILL.md`, create `skill.go`, modify
`cmd/sandbin/handlers.go`, `cmd/sandbin/handlers_test.go`, `Containerfile`,
`.github/workflows/ci.yml`

**Interfaces:**
- Produces: root package `sandbin` (module import path `github.com/yookoala/sandbin`) with
  `var PreviewSkillMD string` embedded from `skills/sandbin-preview/SKILL.md`; server route
  `GET /skills/sandbin-preview/SKILL.md` → 200 `text/markdown; charset=utf-8` body = the file.

- [ ] Step 1: write `skills/sandbin-preview/SKILL.md` per the decisions' shape (frontmatter with
  `name` and `description`, the seven body sections, 60-80 lines). Cross-check every command
  against README's client section so the skill and the docs cannot disagree.
- [ ] Step 2: write failing tests in `handlers_test.go`: GET 200 + Content-Type + body
  byte-identical to `os.ReadFile("../../skills/sandbin-preview/SKILL.md")` (freshness) and
  containing the frontmatter `description:`; POST/PUT/PATCH/HEAD/OPTIONS → 405; a 405 or GET on
  the skill path leaves `ip_blocks` unchanged (nothing charged); `GET /skills/other` and
  `/skills/` → 404 `unknown bin`; `PUT /<hash>/skills/x.txt` stores and `GET`s back normally;
  `GET /skills/sandbin-preview/SKILL.md` works with no key.
- [ ] Step 3: run them, confirm they fail.
- [ ] Step 4: write `skill.go` (module root): package `sandbin`, the embed directive, one-line doc
  comment; add the static branch in `ServeHTTP` beside the `verify-key` branch (method switch,
  comment: "skills" is not a hex hash so it cannot name a bin); Containerfile: `COPY skill.go ./`
  and `COPY skills ./skills` before `COPY cmd ./cmd`; ci.yml: `gofmt -l cmd` → `gofmt -l .`.
- [ ] Step 5: `go build ./...` and `go test -count=1 ./cmd/sandbin` green.
- [ ] Step 6: commit `feat: sandbin embeds and serves the sandbin-preview skill`.

## Task 2: front page install snippet

**Files:** modify `cmd/sandbin/handlers.go` (`renderFrontPage`), `cmd/sandbin/handlers_test.go`

- [ ] Step 1: extend `TestFrontPageAtRoot` with the new markers (the install heading, the
  `mkdir -p .opencode/skills/sandbin-preview` line, the `curl -fsSL` line) — write the failing
  assertions first.
- [ ] Step 2: implement: one short paragraph (what the skill teaches the agent: setup, upload,
  folder preview) plus the snippet in a `<pre>` block using `https://this.host`, matching the
  existing example style.
- [ ] Step 3: tests green; commit `feat: the front page shows how to install the agent skill`.

## Task 3: README

**Files:** modify `README.md`

- [ ] Step 1: add an "Give the skill to your agent" subsection after the client section: what the
  skill is, the install snippet with `http://localhost:8080`, the one-line global-install variant
  (`~/.config/opencode/skills`), and the `/skills/sandbin-preview/SKILL.md` row in the API
  section (GET only, `text/markdown`, not charged).
- [ ] Step 2: run every touched example verbatim against a live server (host adapted only), and
  validate the frontmatter parses as YAML with python3.
- [ ] Step 3: commit `docs: installing the sandbin-preview skill for your agent`.

## Reviewer TODO

1. Run all four gates yourself (`gofmt -l .` included); test counts per package before/after.
2. Freshness: edit-proof — read the repo file and the served body, byte-identical (cmp); rebuild
   the binary from HEAD and confirm the route; confirm no second copy of the skill text exists
   anywhere (grep for a distinctive SKILL.md line).
3. Route battery on a live server (port 18249+): exact GET 200 (curl -D shows content-type);
   all six other methods 405; nothing charged (ip list row counts before/after, including a
   POST with a wrong key to the skill path — the 405 must fire before any auth); `GET
   /skills/other` 404; `PUT /<hash>/skills/x` normal store+serve+listing; bin named `skills`
   impossible (POST / then PUT under the hash — never "skills").
4. Front page and README: run the snippets verbatim into a scratch `.opencode/skills/...` layout;
   then actually load the installed skill with OpenCode's own discovery if feasible in this
   sandbox (e.g. `opencode` CLI listing the skill) — otherwise validate structure against
   opencode.ai/docs/skills rules (directory form, frontmatter keys, description present) with a
   python3 YAML parse and record what was proven.
5. SKILL.md content truth: run EVERY command the skill shows against a live 18249 server + the
   client (init non-interactive, file upload, --archive round trip, stdin --name, --no-extract,
   title/description, an over-cap refusal to confirm the quoted wording); flag any command the
   client rejects or any wording mismatch between skill text and actual output. The skill must
   name no real host, key or bin.
6. Containerfile: `podman build --network=host` from HEAD, run (fresh volume `sandbin-0012`,
   `-p 127.0.0.1:18250:8080`), curl the skill route through the port, cmp against the repo file;
   `podman rm -f`, volume rm, rmi (keep base images); quadlet dryrun unchanged.
7. Untouched paths byte-identity against the `14dcd06` binaries: front page (apart from the new
   block — diff the rest), healthz, verify-key, bin round trips, listing, ip verbs, plain client
   run stdout word-for-word.
8. ci.yml diff is exactly the gofmt argument; Containerfile diff exactly the two COPY lines;
   `git diff 14dcd06..HEAD --name-only` names only the files the tasks list; `go.mod`/`go.sum`
   untouched; `go build ./...` (CI's step) proves the root package builds in CI's shape.
9. Cleanup: kill servers via /proc scan (no ps/pgrep/pkill; Z leftovers ignored), /tmp/opencode
   empty, no scratch .opencode/skills left in repo root, git status only `?? .opencode/`.

## Skipped on purpose

Serving other files under `skills/`, an HTTP catalog (`index.json`) endpoint for OpenCode's
catalog source, per-user skill config, auth on the skill route, HTML rendering of the markdown,
`GET /skill` aliases, versioning the skill content, install scripts beyond the two-line snippet.

## Outcome

Tests went 157 + 2 to 161 + 2 in `cmd/sandbin`; the client package untouched at 79 (+13
subtests); `gofmt -l .`, `go vet`, `go build ./...`, `go test -count=1` and `go test -race`
green at HEAD, run by both implementer and reviewer.

The review proved live: the served skill is byte-identical to the repo file from both the
HEAD binary and the container; all six non-GET methods 405 with the 405 firing **before** auth
(a wrong-key POST left no ip row, while a wrong-key `POST /` from the same run charged as
always); `/skills/other`, the trailing-slash and case variants all fall through to `unknown
bin`; `PUT /<hash>/skills/x.txt` stores, serves and lists normally. Every command shown inside
SKILL.md ran verbatim against a live server — init, upload, bin reuse, `--archive` round trip
with listing diff, stdin with the ANSI-rendering claim confirmed, `--no-extract`, metadata —
and every refusal the skill quotes matched the client's real stderr word-for-word, with the
server's four 413 archive caps leaving the bin byte-unchanged. Both install snippets ran into
scratch `.opencode/skills/` layouts, the fetched file's frontmatter parses with exactly `name`
+ `description`, and the layout matches opencode.ai/docs/skills directory-form rules. The
front-page diff against the `14dcd06` binary is exactly the five new install-block lines;
everything else — healthz, verify-key, bin round trips, listing, PATCH, the whole ip verb
family, plain client stdout — came back identical. Container rebuild with the two new COPY
lines works and persists through restart; quadlet dryrun unchanged.

Accepted as they are: SKILL.md quotes some local refusals without the `sandbin-client: ` stderr
prefix (verbatim substrings of the real lines); trailing-slash probes on the skill path answer
the router's long-standing `400` rather than a 404; and one true fact ("names are checked
locally before any request") was dropped from the file during the 80-line trim rather than
reworded — no sentence in the skill contradicts the code. Left unproven: discovery by a live
OpenCode model session (the CLI here exposes no skills-discovery endpoint, so the structure was
validated against the docs instead).
