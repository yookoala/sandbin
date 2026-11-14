# Decisions: splitting the usage documentation into docs/usages

The 672-line README carried every usage detail. The usage sections now live in `docs/usages/`
as two sets (server, client), each with a summary page whose content table links the set's
other pages; the README keeps its intro, a very brief example, and links to the two summaries.
Requested by the user; executed as one atomic docs commit.

| Point | Decision |
|---|---|
| Layout | Flat files with set prefixes under `docs/usages/` (user's choice over per-set subdirectories). Server set: `sandbin.md` (summary) + `sandbin-serve.md`, `sandbin-api.md`, `sandbin-lockout.md`, `sandbin-podman.md`, `sandbin-ip.md`. Client set: `sandbin-client.md` (summary) + `sandbin-client-upload.md`, `sandbin-client-config.md`, `sandbin-client-skill.md`. |
| Section mapping | `## Usage` split into serve (build/run, verbs, settings/env, startup refusals) and api (front page, verify-key, skill routes, bins, redirect, passwords, metadata, DELETE + fallback, status codes, archives); `## Upload previews with sandbin-client` → `sandbin-client-upload.md`; `## Point sandbin-client at a server` → `sandbin-client-config.md`; its `### Give the skill to your agent` subsection becomes its own page `sandbin-client-skill.md`; `## API key lockout` (incl. `### Trusted proxies`, one heading level up) → `sandbin-lockout.md`; `## Run it with podman` → `sandbin-podman.md`; `## Managing blocked addresses` → `sandbin-ip.md`. |
| Lockout ownership | The server set owns the lockout page (it is a server behavior); both summaries point at it because every client run is governed by it (user's choice over duplicating the text). |
| Content preservation | Every body line of the original sections moved byte-for-byte — a generator sliced exact line ranges with a coverage assertion (lines 23-669 mapped exactly once; the seven old section-heading lines themselves changed — six
`##` headings plus the `### Give the skill to your agent` heading replaced by the eight page `#`
titles, and `### Trusted proxies` re-leveled to `##` inside its page). The only text edits are eight cross-reference fixes: `see <section>` mentions became relative links (`sandbin-ip.md` x2 — one unwrap of a wrapped line, `sandbin-lockout.md#trusted-proxies`, `sandbin-client-config.md` x4 incl. one `(below)` that no longer points within the same file, `sandbin-client-skill.md` x1). `(below)`/`see below` mentions whose referent stayed inside the same file were left alone. |
| README after | Intro paragraphs byte-identical; new `## Quick example` (build/run server, curl create + PUT + GET, client upload — all commands already documented and live-proven elsewhere); `## Documentation` naming the two summaries and the skill-install one-liner; License section kept. 53 lines. |
| Summaries | Each summary page: one short blurb + a content-table linking every page of its set with a one-line description, and a cross-link to the other set (the server summary also flags the lockout page for client readers). The content table is the authoritative index of the set. |
| Verification | Generator coverage assertion; a link/anchor checker over all 11 markdown files (every relative link resolves, `#trusted-proxies` anchor exists, every page is linked from its summary, both summaries from README); README intro byte-identity check. Docs-only commit: gates re-run unchanged (no Go file touched). |

## Skipped on purpose

Subdirectory-per-set layout, duplicating the lockout text into the client set, rewriting any
prose beyond the eight cross-reference fixes, moving the plan/decision records under `docs/usages`,
splitting the client upload page further (its verbs stay one page), and touching the served
front page or SKILL.md (they are rendered products, not the README's audience).

## Post-split revision (same day)

At the user's request the README's `## Quick example` became a `## Quick start` guide split
by machine. The server steps (build, `serve`, curl sanity check) stay on the server machine.
The client steps moved to the client machine and set up with the served one-liner
`curl -fsSL http://your-server:8080/skills/sandbin-preview/install.sh | bash` in the project
directory instead of `go build -o sandbin-client`, with the explicit note that the client
machine needs Go too (the installer builds the binary there with `go install`). The upload
example therefore invokes the installed binary at
`$HOME/.agents/skills/sandbin-preview/sandbin-client` and relies on the `.sandbin.yaml` the
installer's single API-key question wrote in that directory. The Documentation section's
closing sentence no longer repeats the installer one-liner; it now only mentions fetching
`GET /skills/sandbin-preview/SKILL.md`. No usage-page body changed: the coverage and
byte-identity claims above are unaffected.

## Review outcome (same day)

Independent re-verification (own checkers, binaries built from `8dbb1f0` and HEAD side by side):
**ship with fixes**, both docs-side.

- Stale pointers outside `docs/` — fixed in `b95c8ac`: `deploy/sandbin.env.example` still said
  `see README, Trusted proxies` and the `Containerfile` comment still said `the README run
  command`; both now name `docs/usages/sandbin-lockout.md#trusted-proxies` and
  `docs/usages/sandbin-podman.md` respectively.
- Claims re-verified: 476 old body lines mapped verbatim to exactly one member page; the eight
  cross-reference edits (2 `sandbin-ip.md`, 1 anchored `sandbin-lockout.md#trusted-proxies`, 4
  `sandbin-client-config.md` incl. the `(below)` rewrite, 1 `sandbin-client-skill.md`); zero
  broken file/anchor links across README + 10 `docs/usages` files; summaries covering every page
  and cross-linking; intro and License byte-identical across `432dbab` → `2052b99` → HEAD.
- Installer-path dependency (recorded, not a doc defect): the Quick start's client setup and the
  served skill both run `go install github.com/yookoala/sandbin/cmd/sandbin-client@latest`, and
  the only published tag (v0.1.2) predates the verb CLI — that installed binary treats `upload`
  as a filename (proved live; the same command with a binary built from this tree works). A
  tagged release carrying the verbs is therefore a precondition for pushing/publishing this tree;
  the release bump stays a human decision.
