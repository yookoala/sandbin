# Decision 0024: the nav GitHub icon always leaves, pages fill the viewport with a footer, and the install path gets a box

Decided at plan open, HEAD `a8f94e3`, from the user's three follow-up requests on the shipped
plan-0023 pages. These change the shipped pages away from the preserved mockup at
`/tmp/opencode/plan0023/` — the mockup stays the pre-0024 reference.

## Decisions

1. **The nav GitHub icon is one fixed door, opened in a new tab.** On the landing and on every
   docs page (hub and members alike) the icon link's href is exactly
   `https://github.com/yookoala/sandbin` — never the per-page markdown source — and it carries
   `target="_blank" rel="noopener"`. `docHead` loses its `sourceURL` parameter and `docShell`
   stops threading it; `docsSourceBase` survives because the body-link rewriter still uses it
   for relative `.md` links that have no hosted page. Everything else in the nav (home, book)
   stays same-tab. The landing's inline tagline "…on GitHub." link also stays same-tab — only
   the top-menu icon opens a new tab.
2. **Every hosted page fills the viewport and ends in a footer.** `brandCSS` makes `body` a
   flex column with `min-height:100vh` and gives `main{flex:1 0 auto}`; `docShell` wraps the
   rendered body in `<main>` and appends
   `<footer class="site-footer">Powered by Sandbin | version: ` + `version.String()` +
   `</footer>`; front.html wraps its content in `<main>` and carries the same footer built from
   a new `frontpage.Data.Version` string that `renderFrontPage` supplies as `version.String()`.
   Footer style lives in `brandCSS` (`.site-footer`: pushed down by `margin-top:auto`, thin top
   rule, muted small type) so both page families share it. The version shown is exactly what
   `sandbin --version` prints.
3. **The landing's path line becomes a box.** "Skill Install Path:" stays its own short
   paragraph; the path itself moves to a full-width readonly `<input id="cmd-path">` on the next
   line, styled like the command box above it (same monospace type, same white field with the
   amber left border) but without the copy icon. Its swap animation rides with it (the class
   that animated the old inline span moves to the box), and the picker script writes
   `value` instead of `textContent`.

## Skipped on purpose

- Body cross-links and the hub's docs-tree link (same-tab as today), the inline tagline GitHub
  link, `robots.txt`, bin/skill routes, install.sh, the picker's ordering contract, and the
  client. No client or installer behavior is involved.
