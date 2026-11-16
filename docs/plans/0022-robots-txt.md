# Plan: GET /robots.txt asks search engines not to index the instance

Status: open. Spec: `docs/decisions/0022-robots-txt.md` — why robots.txt alone is enough for
now, where the file is embedded, its exact two lines, and the route shape are settled there.
Baseline HEAD is the plan-0021 close-out `0adf64e` (`cmd/sandbin` 199 tests, `cmd/sandbin-client`
107, `internal/version` 6, `internal/frontpage` 13, all gates green). This is a purely additive
route: no existing response changes.

**Goal:** `internal/robots` owns a `//go:embed robots.txt` string (`User-agent: *` /
`Disallow: /`); `ServeHTTP` answers `GET` and `HEAD /robots.txt` with it as
`text/plain; charset=utf-8`, unauthenticated and uncharged, and every other method with the
existing 405 wording.

**Architecture:** new package `internal/robots` (robots.txt + robots.go + robots_test.go);
`cmd/sandbin/handlers.go` gains one exact-match branch after the `/healthz` branch and one
import. `docs.go`, frontpage, skill templates, bin routes untouched. `docs/usages/sandbin-api.md`
gains one sentence in its fixed-routes paragraph.

**Tech stack:** Go stdlib only (`embed`, `net/http`).

## Global Constraints

- Gates: `gofmt -l .` empty, `go vet ./...`, `go build ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`.
- The served bytes of every existing route (landing, docs, skills, healthz, bins, 404/405/301
  shapes) are unchanged: the new branch answers only the exact path `/robots.txt`.
- The robots.txt bytes are exactly `User-agent: *\nDisallow: /\n` — no comments, no BOM, final
  newline.
- Do not touch: `frontBase`, `frontHostRE`, `renderFrontPage`, `docs.go`, skill routes and
  templates, bin routes and semantics, listings, lockout accounting, the CLI layer, client,
  store, `internal/version`, `internal/frontpage`, `scripts/`, `Makefile`, `Containerfile`, every
  operational error wording (the 405 reuses `http.Error(w, "method not allowed", 405)` verbatim).
- No key check, no failure charge, no lockout interaction on `/robots.txt` — the decision record
  says why.
- Never push; never tag the real repo. Tags v0.1.0..v0.3.3 stay where they are.

## Review Focus

- Additivity is the whole review: every existing route serves byte-identical responses before
  and after; the only new behavior is the exact path `/robots.txt`.
- The new branch cannot shadow a bin: "robots.txt" is not an eight-hex hash, and a full
  create/put/get round trip still works with the branch in place.
- The embedded file is the exact two lines with final newline; the package exports it without
  templates or processing.
- GET/HEAD 200 with the right Content-Type and body; other methods 405; no API key needed and no
  lockout charge incurred (a wrong-key budget is untouched by bot GETs).

## Task 1: the package, the branch, the test

**Files:** create `internal/robots/robots.txt`, `internal/robots/robots.go`,
`internal/robots/robots_test.go`; modify `cmd/sandbin/handlers.go` (import + one branch after
the healthz block); modify `docs/usages/sandbin-api.md` (one sentence).

**Interfaces:**
- Produces: package `robots` with `//go:embed robots.txt` → `var Text string`. No functions, no
  template.
- Branch in `ServeHTTP`, directly after the `/healthz` block and its comment style:

  ```go
  // "robots.txt" holds dots, so it can never name a hex bin hash, same reasoning as healthz.
  // It asks crawlers to index nothing on this instance; it needs no key and charges nothing.
  if r.URL.Path == "/robots.txt" {
      if r.Method != http.MethodGet && r.Method != http.MethodHead {
          http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
          return
      }
      w.Header().Set("Content-Type", "text/plain; charset=utf-8")
      io.WriteString(w, robots.Text)
      return
  }
  ```

- Tests first (TDD): `internal/robots/robots_test.go` pins `robots.Text` to the exact bytes
  (including final newline) and that it starts `User-agent: *`; `cmd/sandbin/handlers_test.go`
  gains `TestRobotsTxt` modeled on `TestHealthz`: GET 200 + body exact +
  `Content-Type: text/plain; charset=utf-8`; HEAD 200; POST 405; and the shadow proof — a full
  create/put/get round trip after the branch exists. Confirm they fail before the package exists.

- [ ] Step 1: write both test files; run — fail (no `internal/robots`, no route).
- [ ] Step 2: `internal/robots/robots.txt` + `robots.go` + the handlers branch; green.
- [ ] Step 3: the `docs/usages/sandbin-api.md` sentence: after the skills-routes sentence in the
  fixed-routes paragraph, something like "A `GET /robots.txt` answers a two-line file that asks
  search engines not to crawl anything on the instance; it needs no key, charges nothing, and
  every other method on that path answers `405`." Match the page's voice.
- [ ] Step 4: gates + commit `feat: GET /robots.txt asks search engines not to index the instance`
  (code + tests + the docs sentence in one atomic commit is fine — the sentence is part of the
  same change).

## Verification (implementer, after Task 1)

- All five gates; test-name counts vs baseline (199/107/6/13): additions only — the new package's
  tests plus `TestRobotsTxt` in `cmd/sandbin`; zero removals (`comm` on test-name lists).
- Live-server proof: `sandbin serve` on 127.0.0.1:18307; `curl -i` shows GET 200
  `text/plain; charset=utf-8` body `cmp`-identical to `internal/robots/robots.txt`, HEAD 200,
  POST 405, and a robots.txt GET arriving with a wrong key neither blocked nor charged (the key
  budget behind `X-API-Key` stays intact — prove with a subsequent correct-key PUT).
- Additivity proof: landing, one docs page, install.sh and healthz responses `cmp`-identical
  against a `git archive 0adf64e` baseline server on another port.
- Cleanup: kill servers via /proc scan (`*sandbi` prefix), port 18307 clear, `/tmp/opencode`
  scratch removed, repo clean apart from `?? .opencode/`, tags unchanged.

## Reviewer TODO

1. Gates yourself; counts vs baseline via `comm` on test-name lists — additions only
   (`internal/robots` + `TestRobotsTxt`), zero removals; no existing test body changed
   (`git diff 0adf64e..HEAD -- cmd/sandbin/handlers_test.go` shows only the added test).
2. Scope: `git diff 0adf64e..HEAD --name-only` = the three new files + handlers.go +
   sandbin-api.md only; everything else absent. The handlers.go diff is exactly two hunks
   (import + branch); the branch sits after the healthz block; the 405 wording is verbatim.
3. Byte-identity: robots.txt body served by a live HEAD-tree server `cmp`-identical to the
   embedded file; the additivity battery re-run yourself (landing, docs hub, a docs member page,
   install.sh, healthz, one bin round trip, 404 and 405 shapes) against baseline binaries — zero
   diffs expected. Quote the diff (empty).
4. Shadow proof: with the branch present, create+PUT+GET a bin still round-trips; also GET
   `/robotx.txt` and `/<8hex>/robots.txt` keep the plain 404 bin fallback (the branch is exact
   path only).
5. No-charge proof: a wrong-key `GET /robots.txt` leaves the lockout budget untouched (follow it
   with a correct-key operation that must not be blocked or escalated).
6. Container proof: `podman build --network=host` from HEAD, run on -p 127.0.0.1:18308:8080 with
   the standard volume convention, `curl` robots.txt inside the container byte-equal to the tree
   server's, healthz unchanged; full cleanup per the keep-list (golang:1.26-alpine, alpine:3.22,
   alpine:latest; do not prune beyond what you created).
7. Cleanup confirmation: /proc scan clean of live sandbin, ports clear, `/tmp/opencode` scratch
   gone, `git status --short` only `?? .opencode/`, `git tag -l` unchanged.

## Skipped on purpose

Everything in the decisions record's Skipped section (notably the `X-Robots-Tag` header layer,
deferred as the follow-up if search engines index content anyway), plus: robots.txt variations
per user-agent, caching headers for /robots.txt, and any mention on the landing page itself.
