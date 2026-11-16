# Decision 0027: a merged client setup page, a verb-grouped upload page, and a sectioned skill page

Decided across the mockup review rounds at HEAD `5e12292` (v0.4.3), previewed at
`https://preview.yookoala.dev/951c2bcb/` (`client-init-restructured.html/.md`,
`client-upload-restructured.html/.md`, `client-skill-restructured.html/.md`,
`client-overview-restructured.html/.md`, `serve-v043-refresh.html/.md`). The approved mockup
sources are preserved next to the plan at `docs/plans/0027-client-docs-restructure/` (four
markdown drafts, a refreshed serve page, and a `docs.go.clientset.patched` showing the doc-set
change).

## Why

The client doc set split one setup story across three pages in a confusing order:
`sandbin-client-upload.md` opened with a bare build line, `sandbin-client-config.md` owned
`init` but sat after the upload page in the index, and the building facts a reader needs before
either lived on the upload page. The upload page itself mixed verbs, settings, bin mechanics and
warnings in prose order rather than by subcommand. The skill page opened with three sentences
of template machinery the reader does not need to install the skill, and its URLs said
`localhost` on pages that now render per request. The serve page still named `v0.4.2` in five
places after `v0.4.3` was released.

## Decisions

1. **`sandbin-client-config.md` and the client-building material merge into one page,
   "Building and Config" (`sandbin-client-init.md`)**, listed first in the client doc set —
   before "Upload previews with sandbin-client" in the hub and in the client summary table.
   `sandbin-client-config.md` is deleted; every link into it retargets, `sandbin-api.md`'s
   `POST /verify-key` pointer included.
2. **The setup page has three `##` groups.** "Installing with `go install`" is its own section —
   it needs no source code — and carries the release-tag facts (releases are tags on `main`,
   name a version in the command names a release) and the zip-carried tag / tag-alone
   `--version` facts, plus the pointer that the skill installer is this way packaged. "Building
   from Source" holds the Go 1.26.10-or-newer requirement and the `###` subsections "Download
   from GitHub" and "Build from a checkout", closing with "Check the binary" (the `make build`
   tag-plus-commit answer and the `go install` tag-alone answer). "init" keeps the wizard first,
   then `###` subsections "Without a terminal", "Re-initializing a project", "The `.sandbin.yaml`
   file", and "Settings precedence". All facts from the old config page survive the merge.
3. **The upload page keeps its file name and its title** ("Upload previews with sandbin-client"
   — the URL and the hub entry do not move) **and is rewritten grouped by subcommand at `##`
   and use case at `###`**: `## upload` (subsection "Finding or creating the bin", "Upload
   files", "Pipe stdin", "Upload a whole folder as one archive", "Upload a hand-built archive",
   "Give the bin a title and description"), `## delete`, `## describe`, and `## Rules that hold
   for every run` with `###` subsections for the no-retry rule and the `index.html` rule. Every
   fact on the old page survives. Page mechanics split by ownership: what `.sandbin.yaml` is,
   its shape and who writes what live on the setup page; the bin-finding order (`--bin` >
   matching file > create), orphaned bins and the corrupt-file error stay on the upload page as
   run behavior.
4. **Example style for the client pages.** Every fenced example is a pure runnable command (an
   `export` line paired with the command it feeds counts as one run); the expected result is
   stated in its own paragraph; mock outputs appear in a separate ```` ```text ```` block, never
   as `# ->` comments. Client examples carry `{{ .Instance.URL }}` so hosted client docs name
   their own instance; `http://localhost:8080` stays literal only where it is the built-in
   default as a fact (the settings default, the wizard's bracketed shown default, the
   "falls back to" sentence, the precedence chain). One example consequence: the config page's
   `init --reinit --url http://other.example` server-switch example cannot be tokenized — a
   template names only its own instance — so the re-init example becomes a key-rotation run
   against the instance's url.
5. **The skill page is retitled "Using this instance with Agent skill"** and segmented into two
   `##` sections: "General Install" (the one-line installer against `GET
   /skills/sandbin-preview/install.sh`, the default skills folder and OpenCode pickup, the
   pinned client build described functionally, the `init` the installer runs including the guard
   question and the no-terminal printed command) and "Install for Specific AI Harness" (`-d` /
   `--dest` through `bash -s --`, the single-project and Claude examples, `--help` /
   `unknown argument`, then the landing page's harness picker that presents exactly those
   commands). **All SKILL.md template-generation coverage is removed** — the per-request
   rendering explanation, the git copy's raw `{{.Base}}` token note and the
   install-from-serve-not-from-checkout warning, and the installer's served-fill internals.
   This is a deliberate coverage removal: those facts lose their documented home. A consequence:
   the skill page no longer needs any `{{`{{`}}` brace escape.
6. **The serve page's concrete release references refresh** from `v0.4.2` to `v0.4.3` (five
   places), keeping the usage docs honest about the newest tag the docs themselves now name.
7. **Title changes propagate through cross-links.** The retitle of the skill page lands in the
   client summary table and in the server summary's "Beyond the API" pointer; the init page's
   pointer to it uses the new title too. Test pins for the page list, titles, and any re-pinned
   body wording follow the content; pins are re-pointed, never deleted.
