// Package robots holds the file the server answers "/robots.txt" with: the two lines that tell a
// compliant crawler to index nothing on the instance. The bytes are embedded verbatim, so what is
// served is exactly what robots.txt says, and the package has no logic around them: nothing in the
// file varies per request, per bin or per address, so unlike the frontpage and skill templates
// there is no parse step and no Data — one //go:embed and the string it fills.
package robots

import (
	_ "embed"
)

// Text is robots.txt as the compiler stored it, final newline included: "User-agent: *" naming
// every crawler, "Disallow: /" naming the whole instance, and nothing else — no comments, no rules
// per bot, no Crawl-delay, and no Sitemap line, since the instance has no sitemap and naming one
// would invite the crawling the file is asking for.
//
//go:embed robots.txt
var Text string
