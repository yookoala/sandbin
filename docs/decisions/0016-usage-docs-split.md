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
| Content preservation | Every body line of the original sections moved byte-for-byte — a generator sliced exact line ranges with a coverage assertion (lines 23-669 mapped exactly once; only the seven old section headings dropped, replaced by each file's `#` title). The only text edits are eight cross-reference fixes: `see <section>` mentions became relative links (`sandbin-ip.md` x2 — one unwrap of a wrapped line, `sandbin-lockout.md#trusted-proxies`, `sandbin-client-config.md` x4 incl. one `(below)` that no longer points within the same file, `sandbin-client-skill.md` x1). `(below)`/`see below` mentions whose referent stayed inside the same file were left alone. |
| README after | Intro paragraphs byte-identical; new `## Quick example` (build/run server, curl create + PUT + GET, client upload — all commands already documented and live-proven elsewhere); `## Documentation` naming the two summaries and the skill-install one-liner; License section kept. 53 lines. |
| Summaries | Each summary page: one short blurb + a content-table linking every page of its set with a one-line description, and a cross-link to the other set (the server summary also flags the lockout page for client readers). The content table is the authoritative index of the set. |
| Verification | Generator coverage assertion; a link/anchor checker over all 11 markdown files (every relative link resolves, `#trusted-proxies` anchor exists, every page is linked from its summary, both summaries from README); README intro byte-identity check. Docs-only commit: gates re-run unchanged (no Go file touched). |

## Skipped on purpose

Subdirectory-per-set layout, duplicating the lockout text into the client set, rewriting any
prose beyond the eight cross-reference fixes, moving the plan/decision records under `docs/usages`,
splitting the client upload page further (its verbs stay one page), and touching the served
front page or SKILL.md (they are rendered products, not the README's audience).
