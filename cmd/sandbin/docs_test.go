package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/yookoala/sandbin"
	"github.com/yookoala/sandbin/internal/version"
)

// wantDocPages are the nine hosted pages, named after the embedded usage files.
var wantDocPages = []string{
	"sandbin.html",
	"sandbin-serve.html",
	"sandbin-podman.html",
	"sandbin-api.html",
	"sandbin-ip-blocking.html",
	"sandbin-client.html",
	"sandbin-client-init.html",
	"sandbin-client-upload.html",
	"sandbin-client-skill.html",
}

// wantDocTitles is each page's title, taken from the file's H1.
var wantDocTitles = map[string]string{
	"sandbin.html":               "sandbin — usage documentation",
	"sandbin-serve.html":         "Running the server",
	"sandbin-api.html":           "The HTTP API",
	"sandbin-ip-blocking.html":   "IP Address Blocking",
	"sandbin-podman.html":        "Running with podman",
	"sandbin-client.html":        "sandbin-client — usage documentation",
	"sandbin-client-init.html":   "Building and Config",
	"sandbin-client-upload.html": "Upload previews with sandbin-client",
	"sandbin-client-skill.html":  "Using this instance with Agent skill",
}

// hrefRE picks every href out of a served page.
var hrefRE = regexp.MustCompile(`href="([^"]*)"`)

// TestDocsHubServesBothPaths checks that GET /docs and GET /docs/ answer the same hub page
// and that the hub links every hosted page.
func TestDocsHubServesBothPaths(t *testing.T) {
	h := newTestServer(t, "test-key")

	var body string
	for _, p := range []string{"/docs", "/docs/"} {
		rec := get(t, h, p)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want %d; body %q", p, rec.Code, http.StatusOK, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("GET %s Content-Type = %q, want %q", p, ct, "text/html; charset=utf-8")
		}
		b := rec.Body.String()
		if !strings.Contains(b, "<title>") {
			t.Errorf("GET %s carries no <title>:\n%s", p, b)
		}
		for _, name := range wantDocPages {
			if !strings.Contains(b, "href=\"/docs/"+name+"\"") {
				t.Errorf("GET %s hub does not link %q in:\n%s", p, name, b)
			}
		}
		body = b
	}
	// The hub is fixed markup, so no template token of any kind may survive into it.
	if strings.Contains(body, "{{") {
		t.Errorf("the hub page carries a template token:\n%s", body)
	}
	// The hub lists exactly the merged set: one link per hosted page, nothing more.
	if n := strings.Count(body, "href=\"/docs/"); n != len(wantDocPages) {
		t.Errorf("the hub links %q %d times, want %d in:\n%s", "href=\"/docs/", n, len(wantDocPages), body)
	}
	// and in doc-set order: each summary first, then the pages it covers — so the client set
	// reads Building and Config, then the upload page, then the Agent skill page.
	var listed []string
	for _, m := range regexp.MustCompile(`href="/docs/([^"]+)"`).FindAllStringSubmatch(body, -1) {
		listed = append(listed, m[1])
	}
	if strings.Join(listed, " ") != strings.Join(wantDocPages, " ") {
		t.Errorf("the hub lists %v in that order, want %v in:\n%s", listed, wantDocPages, body)
	}
	for _, gone := range []string{"sandbin-lockout.html", "sandbin-ip.html"} {
		if strings.Contains(body, gone) {
			t.Errorf("the hub still links the removed page %q:\n%s", gone, body)
		}
	}
}

// TestDocsPagesServeEveryUsageDoc checks each of the nine pages renders from the embedded
// markdown: 200, the H1 as its title, and the shell around it.
func TestDocsPagesServeEveryUsageDoc(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range wantDocPages {
		t.Run(name, func(t *testing.T) {
			rec := get(t, h, "/docs/"+name)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /docs/%s = %d, want %d; body %q", name, rec.Code, http.StatusOK, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
			}
			body := rec.Body.String()
			title := wantDocTitles[name]
			if !strings.Contains(body, "<title>"+title+"</title>") {
				t.Errorf("page carries no <title>%s</title> in:\n%s", title, body)
			}
			// The body carries the H1 itself, and the shell carries the fixed GitHub door.
			if !strings.Contains(body, "<h1 id=\""+githubSlugForTest(title)+"\">"+title+"</h1>") {
				t.Errorf("page carries no slug-id'd <h1>%s</h1> in:\n%s", title, body)
			}
			for _, want := range []string{
				"<!doctype html>",
				"<meta charset=\"utf-8\">",
				"<style>",
				"#4E1680",
				"#EA9B25",
				"#F8F7F4",
				"'Poetsen One', sans-serif",
				"href=\"/\"",
				"href=\"/docs\"",
				// The nav GitHub door: one fixed repo URL opened in a new tab.
				"href=\"https://github.com/yookoala/sandbin\" target=\"_blank\" rel=\"noopener\"",
				// The icon nav the landing and the docs share: three borderless icons with
				// root-relative hrefs and their aria-labels, styled by the shared brandCSS.
				"<nav class=\"icon-nav\">",
				"class=\"icon-link\"",
				"aria-label=\"Sandbin home\"",
				"aria-label=\"Documentation\"",
				"aria-label=\"Source on GitHub\"",
				".page-head nav.icon-nav{",
				".icon-link{",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("page shell missing %q in:\n%s", want, body)
				}
			}
		})
	}
}

// TestDocsNavCarriesTheFixedGitHubDoor checks the nav of every docs page (hub and members):
// the GitHub icon links exactly the repo root with target/rel, and no nav carries a
// per-page blob/main/docs/usages source link. The body-link rewriter is untouched by this:
// only the nav changed.
func TestDocsNavCarriesTheFixedGitHubDoor(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, p := range append([]string{"/docs"}, docPageHrefs()...) {
		body := get(t, h, p).Body.String()
		start := strings.Index(body, "<header class=\"page-head\">")
		end := strings.Index(body, "</header>")
		if start < 0 || end < 0 || end < start {
			t.Fatalf("GET %s carries no <header>…</header> block:\n%s", p, body[:min(len(body), 400)])
		}
		nav := body[start:end]
		const door = "<a class=\"icon-link\" href=\"https://github.com/yookoala/sandbin\" target=\"_blank\" rel=\"noopener\" title=\"source on GitHub\""
		if n := strings.Count(nav, door); n != 1 {
			t.Errorf("GET %s nav carries the fixed GitHub door %d times, want 1 in:\n%s", p, n, nav)
		}
		if strings.Contains(nav, "blob/main/docs/usages") {
			t.Errorf("GET %s nav still links a per-page source in:\n%s", p, nav)
		}
	}
}

// TestDocsPagesCarryTheWordmarkInline checks the logo is the embedded SVG markup itself in
// every hosted page, never an <img> reference to it.
func TestDocsPagesCarryTheWordmarkInline(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range append([]string{"/docs"}, docPageHrefs()...) {
		rec := get(t, h, name)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", name, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<svg xmlns=\"http://www.w3.org/2000/svg\"") {
			t.Errorf("GET %s carries no inline <svg> wordmark:\n%s", name, body[:min(len(body), 400)])
		}
		if !strings.Contains(body, sandbin.LogoSVG) {
			t.Errorf("GET %s does not carry the embedded logo markup verbatim:\n%s", name, body[:min(len(body), 400)])
		}
		if strings.Contains(body, "<img") {
			t.Errorf("GET %s references an <img> logo:\n%s", name, body)
		}
	}
}

// TestDocsPagesFillTheViewportAndEndInFooter checks the shell layout every docs page carries:
// the body is a flex column at least as tall as the viewport, the rendered body sits inside a
// <main> that grows, and the page ends in the powered-by footer naming this build's version
// before </body>.
func TestDocsPagesFillTheViewportAndEndInFooter(t *testing.T) {
	h := newTestServer(t, "test-key")
	// The shell ends with the footer, then the docs' copy-button script, then the closing
	// tags — the script landed inside <body> with the pipeline; the version value belongs to
	// the build.
	footer := "</main>\n<footer class=\"site-footer\">Powered by Sandbin | version: " +
		version.String() + "</footer>\n"
	for _, p := range append([]string{"/docs"}, docPageHrefs()...) {
		body := get(t, h, p).Body.String()
		for _, want := range []string{
			// The shared layout rules: body as a flex column whose whole box is at least as
			// tall as the viewport (box-sizing keeps the padding inside that 100vh so the
			// footer's bottom edge stays on-screen), main taking the slack, the footer pushed
			// to the bottom edge.
			"display:flex;flex-direction:column;box-sizing:border-box;min-height:100vh",
			"main{flex:1 0 auto}",
			".site-footer{margin-top:auto;padding-top:1.2rem;border-top:1px solid #e3ddd3;color:#666;font-size:0.85rem;text-align:center}",
			"</header>\n<main>\n",
			footer,
			"</script>\n</body>\n</html>\n",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s misses %q in:\n%s", p, want, body)
			}
		}
		if n := strings.Count(body, "<main>"); n != 1 {
			t.Errorf("GET %s opens <main> %d times, want 1", p, n)
		}
		if n := strings.Count(body, "<footer class=\"site-footer\">"); n != 1 {
			t.Errorf("GET %s carries the footer %d times, want 1", p, n)
		}
	}
}

// TestDocsHeadingSlugIDs checks every heading carries a github-style slug id, including the
// anchor the cross-page links aim at.
func TestDocsHeadingSlugIDs(t *testing.T) {
	h := newTestServer(t, "test-key")

	rec := get(t, h, "/docs/sandbin-ip-blocking.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/sandbin-ip-blocking.html = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "id=\"trusted-proxies\"") {
		t.Errorf("IP Address Blocking page has no id=\"trusted-proxies\" for its Trusted proxies heading:\n%s", rec.Body.String())
	}

	headingRE := regexp.MustCompile(`<h([1-6])>([^<]*)</h[1-6]>`)
	for _, name := range wantDocPages {
		body := get(t, h, "/docs/"+name).Body.String()
		for _, open := range regexp.MustCompile(`<h([1-6])([^>]*)>`).FindAllStringSubmatch(body, -1) {
			if !strings.Contains(open[2], "id=\"") {
				t.Errorf("%s: heading %q has no id attribute", name, open[0])
			}
		}
		// A slug is the heading's text lowercased, spaces dashed, punctuation dropped.
		for _, m := range headingRE.FindAllStringSubmatch(body, -1) {
			want := githubSlugForTest(m[2])
			open := regexp.MustCompile(`<h[1-6] id="([^"]*)">` + regexp.QuoteMeta(m[2]) + `</h[1-6]>`)
			got := open.FindStringSubmatch(body)
			if got == nil {
				t.Errorf("%s: heading %q is not rendered with a slug id (want %q)", name, m[2], want)
				continue
			}
			if got[1] != want {
				t.Errorf("%s: heading %q has id %q, want slug %q", name, m[2], got[1], want)
			}
		}
	}
}

// TestDocsSummaryTablesRender checks the GFM tables of the two summary pages come out as
// real tables, and the fenced code blocks as pre/code.
func TestDocsSummaryTablesRender(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range []string{"sandbin.html", "sandbin-client.html"} {
		body := get(t, h, "/docs/"+name).Body.String()
		if !strings.Contains(body, "<table") {
			t.Errorf("%s renders no <table>:\n%s", name, body)
		}
		if !strings.Contains(body, "<td>") {
			t.Errorf("%s renders no table cells:\n%s", name, body)
		}
	}
	body := get(t, h, "/docs/sandbin-api.html").Body.String()
	if !strings.Contains(body, "<pre><code") {
		t.Errorf("the API page renders no fenced code block:\n%s", body)
	}
}

// TestDocsInternalLinksAllResolve crawls the hub and every page and resolves every href that
// points into the server: zero 404s, and every #fragment lands on a real id.
func TestDocsInternalLinksAllResolve(t *testing.T) {
	h := newTestServer(t, "test-key")
	seen := map[string]bool{}
	crawl := []string{"/docs", "/docs/"}
	for _, name := range wantDocPages {
		crawl = append(crawl, "/docs/"+name)
	}
	links := 0
	for _, pagePath := range crawl {
		rec := get(t, h, pagePath)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", pagePath, rec.Code)
		}
		for _, href := range hrefRE.FindAllStringSubmatch(rec.Body.String(), -1) {
			target, frag, ok := resolveDocHref(pagePath, href[1])
			if !ok {
				// An outside link must still be a github.com link: the pages carry no other
				// external asset.
				if !strings.HasPrefix(href[1], "https://github.com/yookoala/sandbin") {
					t.Errorf("%s links outside the repo: %q", pagePath, href[1])
				}
				continue
			}
			links++
			sub := get(t, h, target)
			if sub.Code != http.StatusOK {
				t.Errorf("%s links %q → GET %s = %d, want 200", pagePath, href[1], target, sub.Code)
				continue
			}
			if frag != "" {
				if !strings.Contains(sub.Body.String(), "id=\""+frag+"\"") {
					t.Errorf("%s links %q but %s has no id=%q", pagePath, href[1], target, frag)
				}
			}
			seen[href[1]] = true
		}
	}
	if links == 0 {
		t.Fatal("the crawl resolved no in-server links at all")
	}
	// Every hosted page must be reachable from the hub or a summary page.
	for _, name := range wantDocPages {
		if !seen["/docs/"+name] && !seen[name] {
			t.Errorf("%s is not linked from the hub or any page", name)
		}
	}
	t.Logf("resolved %d distinct in-server docs links", len(seen))
}

// TestDocsLinksAreRewrittenToHTML checks the .md → .html rewrite in the rendered pages, the
// #fragment forms included.
func TestDocsLinksAreRewrittenToHTML(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range wantDocPages {
		body := get(t, h, "/docs/"+name).Body.String()
		for _, href := range hrefRE.FindAllStringSubmatch(body, -1) {
			if _, _, inServer := resolveDocHref("/docs/"+name, href[1]); inServer && strings.Contains(href[1], ".md") {
				t.Errorf("%s still links a .md path: %q", name, href[1])
			}
		}
	}

	server := get(t, h, "/docs/sandbin.html").Body.String()
	for _, want := range []string{
		"href=\"sandbin-client.html\"",
		"href=\"sandbin-ip-blocking.html#trusted-proxies\"",
		"href=\"sandbin-serve.html\"",
		"href=\"sandbin-api.html\"",
		// The server summary's Beyond the API pointer carries the skill page's new title.
		"<a href=\"sandbin-client-skill.html\">Using this instance\nwith Agent skill</a>",
	} {
		if !strings.Contains(server, want) {
			t.Errorf("the server summary does not carry %q in:\n%s", want, server)
		}
	}

	// The client summary's table rows are the new set, in the new order, with the new titles.
	client := get(t, h, "/docs/sandbin-client.html").Body.String()
	rows := []string{
		"<a href=\"sandbin-client-init.html\">Building and Config</a>",
		"<a href=\"sandbin-client-upload.html\">Upload previews with sandbin-client</a>",
		"<a href=\"sandbin-client-skill.html\">Using this instance with Agent skill</a>",
	}
	at := -1
	for _, want := range rows {
		i := strings.Index(client, want)
		if i < 0 {
			t.Errorf("the client summary does not carry %q in:\n%s", want, client)
			continue
		}
		if i < at {
			t.Errorf("the client summary lists %q out of order in:\n%s", want, client)
		}
		at = i
	}
}

// TestDocsIPBlockingPageMergesBothMachineries pins the merged page's content: it carries the
// corrected key-checked list with PATCH, the escalation and 429 answers, the address-detection
// rule with its Trusted proxies section, and the control socket machinery both old pages held.
func TestDocsIPBlockingPageMergesBothMachineries(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-ip-blocking.html").Body.String()
	for _, want := range []string{
		"<title>IP Address Blocking</title>",
		"id=\"trusted-proxies\"",
		"<code>PATCH /api/bins/{hash}</code>",
		"<code>POST /api/key</code>",
		"30 minutes",
		"<code>Retry-After</code>",
		"<code>--trusted-proxy</code>",
		"<code>--trusted-proxy-file</code>",
		"no blocked addresses",
		"the control socket",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the IP Address Blocking page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
}

// TestDocsAPIPageIsSwaggerStyle pins the swagger shape of the JSON-API page: the Conventions
// opener with the one-time $API_KEY export, the decided h2 route sections in the decided
// order, the method h3 subsections under them, the JSON-body and ?raw=1 spellings, and none
// of the old localhost placeholders.
func TestDocsAPIPageIsSwaggerStyle(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-api.html").Body.String()
	for _, want := range []string{
		"<h2 id=\"conventions\">Conventions</h2>",
		"<code>export API_KEY=your-key</code>",
		"id=\"" + githubSlugForTest("/api — the API index") + "\"",
		"id=\"" + githubSlugForTest("/api/bins — create and list bins") + "\"",
		"id=\"" + githubSlugForTest("POST — create a bin") + "\"",
		"id=\"" + githubSlugForTest("GET — list bins") + "\"",
		"id=\"" + githubSlugForTest("/api/bins/{hash} — inspect, update, delete") + "\"",
		"id=\"" + githubSlugForTest("GET — inspect the bin") + "\"",
		"id=\"" + githubSlugForTest("PATCH — edit title, description, and password") + "\"",
		"id=\"" + githubSlugForTest("DELETE — remove the bin") + "\"",
		"id=\"" + githubSlugForTest("/api/key — check the API key") + "\"",
		"id=\"" + githubSlugForTest("/api/health — the liveness probe") + "\"",
		"id=\"" + githubSlugForTest("/openapi.json — the machine-readable API document") + "\"",
		"id=\"" + githubSlugForTest("/ — the instance root") + "\"",
		"id=\"" + githubSlugForTest("/{hash}/ — the bin base") + "\"",
		"id=\"" + githubSlugForTest("/{hash}/{path} — a file in a bin") + "\"",
		"id=\"" + githubSlugForTest("Archive uploads unpack themselves") + "\"",
		"id=\"" + githubSlugForTest("/{hash}/{folder}/ — a folder in a bin") + "\"",
		"X-API-Key: $API_KEY",
		"JSON body naming any or all of <code>title</code>, <code>description</code> and <code>password</code>",
		"<code>?raw=1</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the API page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
	// exactly the decided h2 sections, in the decided order.
	h2texts := []string{
		"Conventions",
		"/ — the instance root",
		"/api — the API index",
		"/api/bins — create and list bins",
		"/api/bins/{hash} — inspect, update, delete",
		"/api/key — check the API key",
		"/api/health — the liveness probe",
		"/openapi.json — the machine-readable API document",
		"/{hash}/ — the bin base",
		"/{hash}/{path} — a file in a bin",
		"/{hash}/{folder}/ — a folder in a bin",
	}
	h2ids := make([]string, 0, len(h2texts))
	for _, text := range h2texts {
		h2ids = append(h2ids, githubSlugForTest(text))
	}
	if got, want := strings.Join(docHeadingIDs(body, "2"), " "), strings.Join(h2ids, " "); got != want {
		t.Errorf("the API page's h2 sections read %q, want %q in:\n%s", got, want, body)
	}
	if strings.Contains(body, "http://localhost:8080") {
		t.Error("the API page still carries a localhost placeholder address")
	}
}

// TestDocsOpenAPIDocSection pins the /openapi.json section's content points on the API page:
// the new h2 itself (its placement is pinned by the swagger-style h2 order), the sentence that
// this route needs no key and charges nothing, the make openapi name a self-builder regenerates
// with, and the sentence that keeps bin-content paths out of the document.
func TestDocsOpenAPIDocSection(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-api.html").Body.String()
	for _, want := range []string{
		"id=\"" + githubSlugForTest("/openapi.json — the machine-readable API document") + "\"",
		"It needs no API key and charges nothing",
		"<code>make openapi</code>",
		"are not in the document",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the API page's /openapi.json section misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
}

// TestDocsAPIIndexSection pins the /api index section's content points on the API page: the new
// h2 itself (its placement is pinned by the swagger-style h2 order), the sentence that the index
// needs no key and charges nothing, the sentence that every entry's url is built from the
// request's own host and scheme, and the sentence pointing at the machine-readable document.
func TestDocsAPIIndexSection(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-api.html").Body.String()
	for _, want := range []string{
		"id=\"" + githubSlugForTest("/api — the API index") + "\"",
		"needs no API key, charges nothing, and answers even while the requesting address is blocked",
		"is a full URL built from the request's own host and scheme",
		"<code>openapi</code> sibling pointing at the machine-readable <code>/openapi.json</code> document",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the API page's /api index section misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
}

// TestDocsNoRetiredSpellings pins the no-shim rule on the hosted pages: the retired
// header-driven surface appears on no page — no X-Bin-* or X-Extract header, no /verify-key
// or /healthz route, no `POST /`-creates-a-bin text, and no blanket `GET` is never charged
// claim that the key-checked listing disproves.
func TestDocsNoRetiredSpellings(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range wantDocPages {
		body := get(t, h, "/docs/"+name).Body.String()
		for _, gone := range []string{"X-Bin-", "X-Extract", "/verify-key", "/healthz", "<code>POST /</code>", "<code>GET</code> is never charged"} {
			if strings.Contains(body, gone) {
				t.Errorf("%s still carries the retired spelling %q in:\n%s", name, gone, body[:min(len(body), 400)])
			}
		}
	}
}

// TestDocsServePageCarriesTheCoreSettings pins the rebuilt serve page: the new download and
// build sections, the key precedence sentence, the control socket's default path, and the
// --version section gone from it.
func TestDocsServePageCarriesTheCoreSettings(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-serve.html").Body.String()
	for _, want := range []string{
		"<h2 id=\"download-the-source\">Download the source</h2>",
		"<h2 id=\"build-the-server\">Build the server</h2>",
		"id=\"" + githubSlugForTest("The serve subcommand") + "\"",
		"go install github.com/yookoala/sandbin/cmd/sandbin@v0.4.3",
		"go install github.com/yookoala/sandbin/cmd/sandbin-client@v0.4.3",
		"<code>v0.4.3</code> is the newest",
		"<code>git checkout v0.4.3</code>",
		"<code>make release TAG=v0.4.3</code>",
		"The flag wins over the variable, and with neither set the server refuses to start.",
		"sandbin.db.sock",
		"curl http://127.0.0.1:8080/api/health",
		"<code>GET /api/health</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the serve page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
	if strings.Contains(body, "v0.4.2") {
		t.Error("the serve page still names the superseded v0.4.2 release")
	}
	if strings.Contains(body, "id=\"what---version-reports\"") {
		t.Error("the serve page still carries the What `--version` reports section")
	}
}

// TestDocsPodmanPagePutsPublishedImageFirst pins the restructured podman page: its new title,
// exactly the two decided H2 sections in the decided order — the published ghcr image first, the
// source build second — the four H3 subsections under the source build, and the health-status
// inspect command rendered down from the markdown's doubled-brace escape to podman's own single
// brace pair, with the sentence that explains that escape still naming it.
func TestDocsPodmanPagePutsPublishedImageFirst(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-podman.html").Body.String()
	for _, want := range []string{
		"<title>Running with podman</title>",
		"<h1 id=\"running-with-podman\">Running with podman</h1>",
		"ghcr.io/yookoala/sandbin:latest",
		// The probe targets the JSON health route on the built-in default address; the
		// renderer quotes the shell quotes.
		"--health-cmd &quot;wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1&quot;",
		// The inspect command as the template pass leaves it: one brace pair, not the escape.
		"podman inspect --format '{{.State.Health.Status}}' sandbin",
		"doubled braces",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the podman page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
	// exactly the two decided H2 sections, in the decided order.
	if got, want := strings.Join(docHeadingIDs(body, "2"), " "), "running-the-published-image building--running-from-source"; got != want {
		t.Errorf("the podman page's h2 sections read %q, want %q in:\n%s", got, want, body)
	}
	// the four decided H3 subsections, all under the source-build section, in order.
	if got, want := strings.Join(docHeadingIDs(body, "3"), " "), "download-the-source running health-check running-as-systemd-service"; got != want {
		t.Errorf("the podman page's h3 sections read %q, want %q in:\n%s", got, want, body)
	}
	// the doubled-brace escape itself never reaches the reader.
	if strings.Contains(body, "{{`{{`}}") {
		t.Errorf("the podman page serves the doubled-brace escape instead of rendering it:\n%s", body)
	}
}

// TestDocsSummaryPageOwnsTheRobotsSentence pins the doc set's division: the robots behaviour
// sentence has its documented home on the server summary page, not on the API page, and the
// summary table links the merged page where the two old rows stood.
func TestDocsSummaryPageOwnsTheRobotsSentence(t *testing.T) {
	h := newTestServer(t, "test-key")
	summary := get(t, h, "/docs/sandbin.html").Body.String()
	for _, want := range []string{
		"<h2 id=\"beyond-the-api\">Beyond the API</h2>",
		"a two-line file asking",
		"href=\"sandbin-ip-blocking.html#trusted-proxies\"",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the server summary misses %q in:\n%s", want, summary[:min(len(summary), 400)])
		}
	}
	api := get(t, h, "/docs/sandbin-api.html").Body.String()
	if strings.Contains(api, "a two-line file asking") {
		t.Error("the API page still carries the robots behaviour sentence")
	}
}

// TestDocsClientInitPageCarriesTheMergedSetup pins the merged setup page's shape: the three H2
// groups the decision settled, in order, each carrying its decided H3 subsections.
func TestDocsClientInitPageCarriesTheMergedSetup(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-client-init.html").Body.String()
	for _, want := range []string{
		"<title>Building and Config</title>",
		"<h2 id=\"installing-with-go-install\">Installing with <code>go install</code></h2>",
		"<h2 id=\"building-from-source\">Building from Source</h2>",
		"<h2 id=\"init\">init</h2>",
		"<h3 id=\"download-from-github\">Download from GitHub</h3>",
		"<h3 id=\"build-from-a-checkout\">Build from a checkout</h3>",
		"<h3 id=\"check-the-binary\">Check the binary</h3>",
		"<h3 id=\"without-a-terminal\">Without a terminal</h3>",
		"<h3 id=\"re-initializing-a-project\">Re-initializing a project</h3>",
		"<h3 id=\"the-sandbinyaml-file\">The <code>.sandbin.yaml</code> file</h3>",
		"<h3 id=\"settings-precedence\">Settings precedence</h3>",
		// the two-ways structure, and the pages the setup hands the reader on to.
		"go install github.com/yookoala/sandbin/cmd/sandbin-client@v0.4.3",
		"href=\"sandbin-client-upload.html\"",
		"href=\"sandbin-client-skill.html\"",
		// the key check init runs is the JSON API's key route.
		"<code>POST /api/key</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Building and Config page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
	// exactly the three decided H2 groups, in the decided order.
	if got, want := strings.Join(docHeadingIDs(body, "2"), " "), "installing-with-go-install building-from-source init"; got != want {
		t.Errorf("the Building and Config page's h2 sections read %q, want %q in:\n%s", got, want, body)
	}
}

// TestDocsClientUploadPageGroupsByVerb pins the upload page's grouping: one H2 per verb plus the
// shared-rules section, in order, with the decided H3 use cases under the verbs.
func TestDocsClientUploadPageGroupsByVerb(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-client-upload.html").Body.String()
	for _, want := range []string{
		"<h2 id=\"upload\">upload</h2>",
		"<h2 id=\"delete\">delete</h2>",
		"<h2 id=\"describe\">describe</h2>",
		"<h2 id=\"list\">list</h2>",
		"<h2 id=\"rules-that-hold-for-every-run\">Rules that hold for every run</h2>",
		// The JSON-era inspection describe prints, and the list verb's lines.
		"bin: abc1234",
		"password: no",
		"files: none",
		"sandbin-client list",
		"no bins",
		"<h3 id=\"finding-or-creating-the-bin\">Finding or creating the bin</h3>",
		"<h3 id=\"upload-files\">Upload files</h3>",
		"<h3 id=\"pipe-stdin\">Pipe stdin</h3>",
		"<h3 id=\"upload-a-whole-folder-as-one-archive\">Upload a whole folder as one archive</h3>",
		"<h3 id=\"upload-a-hand-built-archive\">Upload a hand-built archive</h3>",
		"<h3 id=\"give-the-bin-a-title-and-description\">Give the bin a title and description</h3>",
		"<h3 id=\"do-not-retry-a-failing-run-from-a-script\">Do not retry a failing run from a script</h3>",
		"<h3 id=\"a-prototype-needs-indexhtml-at-the-bin-root\">A prototype needs index.html at the bin root</h3>",
		// the setup facts live on the merged page; the run behavior stays here.
		"href=\"sandbin-client-init.html\"",
		"href=\"#finding-or-creating-the-bin\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the upload page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
	if got, want := strings.Join(docHeadingIDs(body, "2"), " "), "upload delete describe list rules-that-hold-for-every-run"; got != want {
		t.Errorf("the upload page's h2 sections read %q, want %q in:\n%s", got, want, body)
	}
}

// TestDocsClientSkillPageIsSectionedWithoutTemplateProse pins the skill page's two H2 sections
// and its new title, and decision 5's removals: no template-generation prose, and no brace
// escape anywhere in its served bytes — every token that page's markdown carries names this
// instance, so nothing brace-shaped survives the render.
func TestDocsClientSkillPageIsSectionedWithoutTemplateProse(t *testing.T) {
	h := newTestServer(t, "test-key")
	body := get(t, h, "/docs/sandbin-client-skill.html").Body.String()
	for _, want := range []string{
		"<title>Using this instance with Agent skill</title>",
		"<h1 id=\"using-this-instance-with-agent-skill\">Using this instance with Agent skill</h1>",
		"<h2 id=\"general-install\">General Install</h2>",
		"<h2 id=\"install-for-specific-ai-harness\">Install for Specific AI Harness</h2>",
		"<code>GET /skills/sandbin-preview/install.sh</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the skill page misses %q in:\n%s", want, body[:min(len(body), 400)])
		}
	}
	if got, want := strings.Join(docHeadingIDs(body, "2"), " "), "general-install install-for-specific-ai-harness"; got != want {
		t.Errorf("the skill page's h2 sections read %q, want %q in:\n%s", got, want, body)
	}
	for _, gone := range []string{"{{", "text/template", "{{.Base}}"} {
		if strings.Contains(body, gone) {
			t.Errorf("the skill page still carries %q in:\n%s", gone, body)
		}
	}
}

// TestDocsLogoRouteServesEmbeddedBytes checks the logo route answers the repo's own bytes
// with the SVG content type.
func TestDocsLogoRouteServesEmbeddedBytes(t *testing.T) {
	h := newTestServer(t, "test-key")
	rec := get(t, h, "/docs/assets/sandbin-logo.svg")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/assets/sandbin-logo.svg = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "assets", "sandbin-logo.svg"))
	if err != nil {
		t.Fatalf("read the repo logo: %v", err)
	}
	if rec.Body.String() != string(raw) {
		t.Errorf("served logo is not byte-equal to docs/assets/sandbin-logo.svg")
	}
	if sandbin.LogoSVG != string(raw) {
		t.Error("sandbin.LogoSVG is not the repo logo")
	}
}

// TestDocsUnknownPathsArePlainNotFound checks unknown /docs paths — including traversal
// shapes — answer the plain 404 shape, and no path joins anything.
func TestDocsUnknownPathsArePlainNotFound(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, p := range []string{
		"/docs/nope.html", "/docs/SANDBIN.html", "/docs/sandbin.md", "/docs/assets/nope.svg",
		"/docs/../etc/passwd", "/docs/..%2f..%2fetc/passwd", "/docs//sandbin.html",
		"/docs/sandbin/", "/docs/sandbin.html/x", "/docs/assets", "/docs/assets/",
		// The two pages the merged IP Address Blocking page replaced answer the plain 404 shape.
		"/docs/sandbin-lockout.html", "/docs/sandbin-ip.html",
		// The page the merged Building and Config page replaced answers it the same way.
		"/docs/sandbin-client-config.html",
	} {
		rec := get(t, h, p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d; body %q", p, rec.Code, http.StatusNotFound, rec.Body.String())
		}
		if got := rec.Body.String(); got != "unknown bin\n" {
			t.Errorf("GET %s body = %q, want the plain 404 shape %q", p, got, "unknown bin\n")
		}
	}
}

// TestDocsMethodDiscipline answers GET and HEAD and refuses every other method on the docs
// routes.
func TestDocsMethodDiscipline(t *testing.T) {
	h := newTestServer(t, "test-key")
	live := []string{"/docs", "/docs/", "/docs/sandbin.html", "/docs/assets/sandbin-logo.svg"}
	for _, p := range live {
		req := httptest.NewRequest(http.MethodHead, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("HEAD %s = %d, want %d", p, rec.Code, http.StatusOK)
		}
	}
	// An unknown name is refused the same way by HEAD as by GET: the method guard answers
	// before the lookup, the lookup then says the plain 404.
	if rec := get(t, h, "/docs/nope.html"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /docs/nope.html = %d, want %d", rec.Code, http.StatusNotFound)
	}
	req := httptest.NewRequest(http.MethodHead, "/docs/nope.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("HEAD /docs/nope.html = %d, want %d", rec.Code, http.StatusNotFound)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		for _, p := range append(live, "/docs/nope.html") {
			req := httptest.NewRequest(method, p, nil)
			req.Header.Set("X-API-Key", "test-key")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want %d; body %q", method, p, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
			}
		}
	}
}

// TestDocsPagesCarryNoStrayTemplateTokens counts the {{ tokens a served page holds against
// the literal tokens its markdown source asked for: the pipeline expands everything else, so
// the only {{ a page may serve are the ones its source escaped as {{`{{`}}. The shell adds
// none of its own.
func TestDocsPagesCarryNoStrayTemplateTokens(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range wantDocPages {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "usages", strings.TrimSuffix(name, ".html")+".md"))
		if err != nil {
			t.Fatalf("read the source of %s: %v", name, err)
		}
		body := get(t, h, "/docs/"+name).Body.String()
		want, page := strings.Count(string(raw), "{{`{{`}}"), strings.Count(body, "{{")
		if want != page {
			t.Errorf("%s carries %d {{ tokens, its markdown source escaped %d:\n%s", name, page, want, body)
		}
		if strings.Contains(body, "<no value>") {
			t.Errorf("%s serves a <no value> where a template field was missing:\n%s", name, body)
		}
	}
	for _, p := range []string{"/docs", "/docs/"} {
		if body := get(t, h, p).Body.String(); strings.Contains(body, "{{") {
			t.Errorf("GET %s carries a template token:\n%s", p, body)
		}
	}
}

// TestDocsRoutesDoNotShadowBins keeps bins reachable with the docs routes in place.
func TestDocsRoutesDoNotShadowBins(t *testing.T) {
	h := newTestServer(t, "test-key")

	base := createTestBin(t, h, "test-key")
	if rec := put(t, h, base+"page.html", "test-key", "<h1>Hello</h1>"); rec.Code != http.StatusCreated {
		t.Fatalf("PUT = %d; body %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, h, base+"page.html"); rec.Code != http.StatusOK || rec.Body.String() != "<h1>Hello</h1>" {
		t.Errorf("GET a bin file after the docs routes = %d, body %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, h, "/docs"); rec.Code != http.StatusOK {
		t.Errorf("GET /docs with a bin in place = %d", rec.Code)
	}
	// "docs" is no eight-hex hash, so it cannot name the bin it is checked before.
	if rec := get(t, h, "/docs/"); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), base) {
		t.Errorf("GET /docs/ is not the hub with a bin in place:\n%s", rec.Body.String())
	}
	if base2 := createTestBin(t, h, "test-key"); base2 == "" {
		t.Error("POST /api/bins stopped creating bins")
	}
}

// fixtureDocPage runs one fixture markdown through the exact pipeline the bundled docs take:
// rendered, shelled, and parsed plus sample-executed by parseDocTemplate.
func fixtureDocPage(t *testing.T, markdown string) hostedPage {
	t.Helper()
	title, body := renderUsageDoc([]byte(markdown))
	pageHTML := docShell(title, body)
	return hostedPage{title: title, html: pageHTML, tmpl: parseDocTemplate("usage doc fixture ("+t.Name()+")", pageHTML)}
}

// TestBundledDocsNameTheRequestAddress serves a fixture page that carries a
// {{ .Instance.URL }} token: under a chosen Host named by a request the server trusts
// (loopback socket + X-Forwarded-Proto: https) the page names that address and no token
// survives the execution; under a plain Host it renders that host with the scheme frontBase
// implies.
func TestBundledDocsNameTheRequestAddress(t *testing.T) {
	s := newTestServer(t, "test-key")
	fixture := "# Token fixture\n\n```sh\ncurl {{ .Instance.URL }}/x\n```\n"
	docsPages["fixture.html"] = fixtureDocPage(t, fixture)
	t.Cleanup(func() { delete(docsPages, "fixture.html") })

	req := httptest.NewRequest(http.MethodGet, "/docs/fixture.html", nil)
	req.Host = "preview.yookoala.dev"
	req.RemoteAddr = "127.0.0.1:4321"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/fixture.html = %d; body %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "curl https://preview.yookoala.dev/x") {
		t.Errorf("the fixture page does not name the addressed host+proto:\n%s", body)
	}
	if strings.Contains(body, "{{") || strings.Contains(body, "sandbin.example") {
		t.Errorf("the fixture page serves a token leftover or the init-time sample data:\n%s", body)
	}

	// A plain Host under the default untrusted socket: the Host httptest.NewRequest names,
	// with the http scheme frontBase falls to.
	if rec = get(t, s, "/docs/fixture.html"); rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/fixture.html plain = %d; body %q", rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	if !strings.Contains(body, "curl http://example.com/x") {
		t.Errorf("the fixture page does not render the plain Host:\n%s", body)
	}
	if strings.Contains(body, "{{") {
		t.Errorf("the fixture page serves a token leftover:\n%s", body)
	}
}

// TestEveryBundledDocExecutesWithFreshData walks the real doc set and executes each stored
// template with fresh sample data: no execute error, the output holds only the literal
// tokens its own markdown escaped as {{`{{`}}, and a page whose markdown names
// {{ .Instance. shows the fresh address where the token stood.
func TestEveryBundledDocExecutesWithFreshData(t *testing.T) {
	data := docData{Instance: docInstance{URL: "https://fresh.example"}}
	one := func(name string, page hostedPage) {
		t.Helper()
		if page.tmpl == nil {
			t.Errorf("%s carries no stored template", name)
			return
		}
		raw, err := sandbin.UsageDocs.ReadFile("docs/usages/" + strings.TrimSuffix(name, ".html") + ".md")
		if err != nil {
			t.Errorf("read the embedded source of %s: %v", name, err)
			return
		}
		var buf strings.Builder
		if err := page.tmpl.Execute(&buf, data); err != nil {
			t.Errorf("%s does not execute with fresh sample data: %v", name, err)
			return
		}
		out := buf.String()
		if left, want := strings.Count(out, "{{"), strings.Count(string(raw), "{{`{{`}}"); left != want {
			t.Errorf("%s leaves %d {{ tokens behind; its markdown escaped %d", name, left, want)
		}
		if strings.Contains(out, "<no value>") {
			t.Errorf("%s renders <no value> where a template field was missing", name)
		}
		if strings.Contains(string(raw), "{{ .Instance.") && !strings.Contains(out, "https://fresh.example") {
			t.Errorf("%s does not render the fresh instance address where its token stood", name)
		}
	}
	for _, set := range docSets {
		for _, page := range append([]string{set.summary}, set.pages...) {
			one(page, docsPages[page])
		}
	}
	var buf strings.Builder
	if err := docsHub.tmpl.Execute(&buf, data); err != nil {
		t.Errorf("the hub does not execute with fresh sample data: %v", err)
	}
	if strings.Contains(buf.String(), "{{") {
		t.Errorf("the hub leaves a token leftover:\n%s", buf.String())
	}
}

// TestBinBytesCarryTemplateTokensVerbatim keeps the template pass inside the docs: a bin file
// whose body carries template-shaped text travels PUT to GET byte-identical.
func TestBinBytesCarryTemplateTokensVerbatim(t *testing.T) {
	h := newTestServer(t, "test-key")
	base := createTestBin(t, h, "test-key")
	body := "value: {{ .Instance.URL }} and {{.State.Health.Status}}\n"
	if rec := put(t, h, base+"tokens.md", "test-key", body); rec.Code != http.StatusCreated {
		t.Fatalf("PUT = %d; body %q", rec.Code, rec.Body.String())
	}
	rec := get(t, h, base+"tokens.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d; body %q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != body {
		t.Errorf("the bin round trip changed the bytes:\ngot  %q\nwant %q", rec.Body.String(), body)
	}
}

// TestDocsCarryCopyButtonsAndTheLandingDoesNot pins the copy machinery's reach: every docs
// page (the hub and each member) carries the docCopyJS script inside its <body> and its CSS
// in the shell's <style>, while the landing page's own <body> region gains neither — its
// copy button stays the landing's own.
func TestDocsCarryCopyButtonsAndTheLandingDoesNot(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, p := range append([]string{"/docs"}, docPageHrefs()...) {
		body := get(t, h, p).Body.String()
		for _, want := range []string{
			"document.querySelectorAll(\"main pre\")",
			"wrap.className = \"codeblock\"",
			"button.className = \"block-copy\"",
			"navigator.clipboard.writeText",
			"document.execCommand(\"copy\")",
			".codeblock{position:relative}",
			".block-copy{position:absolute",
			".block-copy.copied{color:#2e7d32;border-color:#2e7d32}",
			"</script>\n</body>\n</html>\n",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s misses %q in:\n%s", p, want, body[:min(len(body), 400)])
			}
		}
	}
	front := get(t, h, "/").Body.String()
	start, end := strings.Index(front, "<body>"), strings.Index(front, "</body>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("the landing page carries no <body>…</body> region:\n%s", front[:min(len(front), 400)])
	}
	region := front[start:end]
	for _, gone := range []string{"block-copy", "codeblock"} {
		if strings.Contains(region, gone) {
			t.Errorf("the landing page body gained %q in:\n%s", gone, region[:min(len(region), 400)])
		}
	}
}

// TestRenderUsageDocRewritesUnhostedRelativeLinks feeds the renderer a document no usage
// file is: links to pages that are not hosted — the merged-away sandbin-lockout.md included —
// become their GitHub source, absolute links pass through, and code stays verbatim.
func TestRenderUsageDocRewritesUnhostedRelativeLinks(t *testing.T) {
	_, body := renderUsageDoc([]byte("# Linked\n\n" +
		"[lockout](sandbin-lockout.md#trusted-proxies) [summary](sandbin.md) [license](LICENSE.md) " +
		"[plain](notes.md#top) [out](https://example.com/x) [anchor](#linked)\n\n" +
		"`keep [this](other.md)` and `curl -fsSL https://host/skills/sandbin-preview/install.sh`\n"))

	for _, want := range []string{
		"href=\"https://github.com/yookoala/sandbin/blob/main/docs/usages/sandbin-lockout.md#trusted-proxies\"",
		"href=\"sandbin.html\"",
		"href=\"https://github.com/yookoala/sandbin/blob/main/docs/usages/LICENSE.md\"",
		"href=\"https://github.com/yookoala/sandbin/blob/main/docs/usages/notes.md#top\"",
		"href=\"https://example.com/x\"",
		"href=\"#linked\"",
		"id=\"linked\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered document missing %q in:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "<code>keep [this](other.md)</code>") {
		t.Errorf("the rewrite changed text inside inline code:\n%s", body)
	}
	if strings.Contains(body, "usages/other.md") {
		t.Error("the rewrite turned inline code into a link")
	}
}

// resolveDocHref maps one href of a served docs page to a path of this server. ok is false
// for links that point outside it.
func resolveDocHref(pagePath, href string) (target, frag string, ok bool) {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") {
		return "", "", false
	}
	frag = ""
	if i := strings.Index(href, "#"); i >= 0 {
		href, frag = href[:i], href[i+1:]
	}
	switch {
	case strings.HasPrefix(href, "/"):
		return href, frag, true
	case href == "":
		return pagePath, frag, true
	default:
		return "/docs/" + href, frag, true
	}
}

// docPageHrefs are the /docs paths of the nine hosted pages.
func docPageHrefs() []string {
	out := make([]string, 0, len(wantDocPages))
	for _, name := range wantDocPages {
		out = append(out, "/docs/"+name)
	}
	return out
}

// docHeadingIDs lists the id of every heading of one level in a served page, in document order.
func docHeadingIDs(body string, level string) []string {
	var ids []string
	for _, m := range regexp.MustCompile(`<h`+level+` id="([^"]*)">`).FindAllStringSubmatch(body, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// githubSlugForTest is the anchor rule the hosted pages must follow: lowercase, spaces to
// dashes, punctuation dropped.
func githubSlugForTest(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
