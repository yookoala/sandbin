# Decision 0022: the server asks search engines not to index it via robots.txt

Decided at plan open, HEAD `0adf64e`.

## Why

No Sandbin instance should have its contents indexed by search engines: bins hold throwaway
preview files, often shared links the operator never meant to publish. The whole instance —
landing, hosted docs, bin listings and every served file — should read "do not crawl" to
compliant crawlers.

## Decisions

1. **robots.txt only, for now.** `GET /robots.txt` serves a fixed embedded file that disallows
   everything. The stronger `X-Robots-Tag: noindex` response-header layer (which also covers
   non-HTML files and URLs discovered through links) is deliberately deferred; if a search
   engine is found indexing instance content, that header is the planned follow-up.
2. **A new internal package holds the file.** `internal/robots`: `robots.txt` embedded with
   `//go:embed` into an exported `var Text string`. No template — nothing in the file is
   dynamic, unlike the frontpage/skill templates — so there is no parse step and no Data.
3. **The content is exactly two lines:**

   ```
   User-agent: *
   Disallow: /
   ```

   with a final newline. No comments, no rules per bot, no `Crawl-delay`, no `Sitemap` line
   (the instance has no sitemap and a `Sitemap:` line would invite the crawling we are asking for).
4. **The route follows the healthz shape:** an exact-match branch in `ServeHTTP`, placed right
   after the `/healthz` branch. `"robots.txt"` contains a dot, so it can never name an eight-hex
   bin hash, same reasoning as the healthz/verify-key/skills/docs branches. `GET` and `HEAD`
   answer `200` `text/plain; charset=utf-8` with the embedded bytes, unauthenticated and
   uncharged (crawlers have no API key, and charging them would let anyone burn a visitor's
   key budget with bot traffic); every other method answers the existing
   `405 method not allowed`. Nothing is injected into or derived from uploaded content.
5. **Docs:** one sentence in `docs/usages/sandbin-api.md`'s fixed-routes paragraph, matching how
   verify-key and the skill routes are described there.

## Skipped on purpose

- `X-Robots-Tag` on every response (the deferred follow-up, decision 1).
- `<meta name="robots">` injection into served HTML — impossible without breaking byte identity.
- Any change to the landing page or docs pages themselves; the plan touches no HTML.
- Rate-limiting or blocking known crawler addresses; robots.txt is an ask, and enforcement is a
  different problem.
