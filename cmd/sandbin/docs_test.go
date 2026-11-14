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
)

// wantDocPages are the ten hosted pages, named after the embedded usage files.
var wantDocPages = []string{
	"sandbin.html",
	"sandbin-serve.html",
	"sandbin-api.html",
	"sandbin-lockout.html",
	"sandbin-podman.html",
	"sandbin-ip.html",
	"sandbin-client.html",
	"sandbin-client-upload.html",
	"sandbin-client-config.html",
	"sandbin-client-skill.html",
}

// wantDocTitles is each page's title, taken from the file's H1.
var wantDocTitles = map[string]string{
	"sandbin.html":               "sandbin — usage documentation",
	"sandbin-serve.html":         "Running the server",
	"sandbin-api.html":           "The HTTP API",
	"sandbin-lockout.html":       "API key lockout",
	"sandbin-podman.html":        "Run it with podman",
	"sandbin-ip.html":            "Managing blocked addresses",
	"sandbin-client.html":        "sandbin-client — usage documentation",
	"sandbin-client-upload.html": "Upload previews with sandbin-client",
	"sandbin-client-config.html": "Point sandbin-client at a server",
	"sandbin-client-skill.html":  "Give the skill to your agent",
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
}

// TestDocsPagesServeEveryUsageDoc checks each of the ten pages renders from the embedded
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
			// The body carries the H1 itself, and the shell names the page's GitHub source.
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
				"https://github.com/yookoala/sandbin/blob/main/docs/usages/",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("page shell missing %q in:\n%s", want, body)
				}
			}
		})
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

// TestDocsHeadingSlugIDs checks every heading carries a github-style slug id, including the
// anchor the cross-page links aim at.
func TestDocsHeadingSlugIDs(t *testing.T) {
	h := newTestServer(t, "test-key")

	rec := get(t, h, "/docs/sandbin-lockout.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/sandbin-lockout.html = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "id=\"trusted-proxies\"") {
		t.Errorf("lockout page has no id=\"trusted-proxies\" for its Trusted proxies heading:\n%s", rec.Body.String())
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
		"href=\"sandbin-lockout.html#trusted-proxies\"",
		"href=\"sandbin-serve.html\"",
		"href=\"sandbin-api.html\"",
	} {
		if !strings.Contains(server, want) {
			t.Errorf("the server summary does not carry %q in:\n%s", want, server)
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

// TestDocsPagesCarryNoStrayTemplateTokens counts the {{ tokens a page carries against the
// ones its own markdown source holds: the shell adds none.
func TestDocsPagesCarryNoStrayTemplateTokens(t *testing.T) {
	h := newTestServer(t, "test-key")
	for _, name := range wantDocPages {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "usages", strings.TrimSuffix(name, ".html")+".md"))
		if err != nil {
			t.Fatalf("read the source of %s: %v", name, err)
		}
		body := get(t, h, "/docs/"+name).Body.String()
		source, page := strings.Count(string(raw), "{{"), strings.Count(body, "{{")
		if source != page {
			t.Errorf("%s carries %d {{ tokens, its markdown source holds %d:\n%s", name, page, source, body)
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
		t.Error("POST / stopped creating bins")
	}
}

// TestRenderUsageDocRewritesUnhostedRelativeLinks feeds the renderer a document no usage
// file is: links to pages that are not hosted become their GitHub source, absolute links
// pass through, and code stays verbatim.
func TestRenderUsageDocRewritesUnhostedRelativeLinks(t *testing.T) {
	_, body := renderUsageDoc([]byte("# Linked\n\n" +
		"[lockout](sandbin-lockout.md#trusted-proxies) [summary](sandbin.md) [license](LICENSE.md) " +
		"[plain](notes.md#top) [out](https://example.com/x) [anchor](#linked)\n\n" +
		"`keep [this](other.md)` and `curl -fsSL https://host/skills/sandbin-preview/install.sh`\n"))

	for _, want := range []string{
		"href=\"sandbin-lockout.html#trusted-proxies\"",
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

// docPageHrefs are the /docs paths of the ten hosted pages.
func docPageHrefs() []string {
	out := make([]string, 0, len(wantDocPages))
	for _, name := range wantDocPages {
		out = append(out, "/docs/"+name)
	}
	return out
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
