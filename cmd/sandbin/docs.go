package main

import (
	"html"
	"net/http"
	"strings"
	"unicode"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/yookoala/sandbin"
)

// The usage docs the server hosts under /docs come from the module-root embeds in skill.go
// and are rendered once, here, at package init: the same posture as the skill templates —
// a doc that cannot be parsed stops the server at startup rather than one request.

// hostedPage is one rendered docs page: its <title> text and the complete HTML of the page.
type hostedPage struct {
	title string
	html  string
}

// docSet is one of the two doc sets the hub presents: its heading, the summary page, and the
// pages it covers.
type docSet struct {
	title   string
	summary string
	pages   []string
}

// docSets is the hub's fixed layout, and the authority on which embedded files get hosted.
var docSets = []docSet{
	{
		title:   "sandbin: the server",
		summary: "sandbin.html",
		pages:   []string{"sandbin-serve.html", "sandbin-api.html", "sandbin-lockout.html", "sandbin-podman.html", "sandbin-ip.html"},
	},
	{
		title:   "sandbin-client: the client",
		summary: "sandbin-client.html",
		pages:   []string{"sandbin-client-upload.html", "sandbin-client-config.html", "sandbin-client-skill.html"},
	},
}

const (
	// docsLogoPath is the logo route, matching its path in the repo.
	docsLogoPath = "/docs/assets/sandbin-logo.svg"
	// docsSourceBase is where a hosted page's markdown lives on GitHub.
	docsSourceBase = "https://github.com/yookoala/sandbin/blob/main/docs/usages/"
	// docsTreeURL is the docs tree link the hub and the landing page name.
	docsTreeURL = "https://github.com/yookoala/sandbin/tree/main/docs/usages"
)

// docExtensions are the markdown extensions the usage docs need: the GFM tables the two
// summary pages carry, fenced code, strikethrough, and the strict "# " heading rule. Autolink
// stays off on purpose: the docs name localhost addresses in prose and in code, and turning
// those into links would have the hosted pages point at a machine that is not this one.
const docExtensions = parser.Tables | parser.FencedCode | parser.Strikethrough | parser.NoIntraEmphasis |
	parser.SpaceHeadings | parser.DefinitionLists | parser.BackslashLineBreak

// docsPages maps each hosted name ("sandbin.html") to its rendered page. Every name is looked
// up whole, so no request path is ever joined onto anything.
var docsPages = buildDocsPages()

// docsHub is the /docs page: a fixed short page in the same shell, linking the two doc sets.
var docsHub = buildDocsHub()

// buildDocsPages renders every embedded usage file once, and refuses to start when the
// embedded tree and the hub's layout disagree.
func buildDocsPages() map[string]hostedPage {
	entries, err := sandbin.UsageDocs.ReadDir("docs/usages")
	if err != nil {
		panic("read the embedded usage docs: " + err.Error())
	}
	pages := make(map[string]hostedPage, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		page := strings.TrimSuffix(name, ".md") + ".html"
		raw, err := sandbin.UsageDocs.ReadFile("docs/usages/" + name)
		if err != nil {
			panic("read the embedded usage doc " + name + ": " + err.Error())
		}
		title, body := renderUsageDoc(raw)
		pages[page] = hostedPage{title: title, html: docShell(title, docsSourceBase+name, body)}
	}
	for _, set := range docSets {
		for _, page := range append([]string{set.summary}, set.pages...) {
			if _, ok := pages[page]; !ok {
				panic("the hub links " + page + ", which docs/usages does not carry")
			}
		}
	}
	for page := range pages {
		known := false
		for _, set := range docSets {
			for _, want := range append([]string{set.summary}, set.pages...) {
				if page == want {
					known = true
				}
			}
		}
		if !known {
			panic("docs/usages carries " + page + ", which the hub does not link")
		}
	}
	return pages
}

// buildDocsHub writes the hub body: one sentence, then the two doc sets with their pages.
func buildDocsHub() string {
	var b strings.Builder
	b.WriteString("<h1 id=\"" + githubSlug("Sandbin usage docs") + "\">Sandbin usage docs</h1>\n")
	b.WriteString("<p>A running <code>sandbin</code> serves its own usage documentation here: the " +
		"server set and the client set, each with its summary page and the pages that set covers.</p>\n")
	for _, set := range docSets {
		b.WriteString("<h2 id=\"" + githubSlug(set.title) + "\">" + html.EscapeString(set.title) + "</h2>\n")
		b.WriteString("<ul>\n")
		for _, page := range append([]string{set.summary}, set.pages...) {
			label := docsPages[page].title
			if page == set.summary {
				label += " (summary)"
			}
			b.WriteString("<li><a href=\"/docs/" + page + "\">" + html.EscapeString(label) + "</a></li>\n")
		}
		b.WriteString("</ul>\n")
	}
	return docShell("Sandbin usage docs", docsTreeURL, b.String())
}

// renderUsageDoc parses one embedded markdown file and returns its H1 text and its rendered
// body HTML: heading ids added, doc links rewritten, the rest as the engine wrote it.
func renderUsageDoc(raw []byte) (title, body string) {
	p := parser.NewWithExtensions(docExtensions)
	doc := p.Parse(raw)
	title = firstHeadingText(doc)
	if title == "" {
		panic("a usage doc carries no H1 to title its page")
	}
	setHeadingIDs(doc)
	rewriteDocLinks(doc)
	renderer := mdhtml.NewRenderer(mdhtml.RendererOptions{Flags: mdhtml.FlagsNone})
	return title, string(markdown.Render(doc, renderer))
}

// firstHeadingText is the text of the document's first H1, which titles its page.
func firstHeadingText(doc ast.Node) string {
	title := ""
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if heading, ok := node.(*ast.Heading); ok && entering {
			if heading.Level == 1 {
				title = headingText(heading)
				return ast.Terminate
			}
		}
		return ast.GoToNext
	})
	return title
}

// headingText is a heading's plain text: its text and code spans, which is what the anchor
// rule works on.
func headingText(heading *ast.Heading) string {
	var b strings.Builder
	ast.WalkFunc(heading, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		switch node := node.(type) {
		case *ast.Text:
			b.Write(node.Literal)
		case *ast.Code:
			b.Write(node.Literal)
		}
		return ast.GoToNext
	})
	return b.String()
}

// setHeadingIDs gives every heading a github-style slug id, so links like
// sandbin-lockout.html#trusted-proxies land where they say. The engine keeps whatever id it
// was given and only makes repeated ones unique, so this is the slug rule of record.
func setHeadingIDs(doc ast.Node) {
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if heading, ok := node.(*ast.Heading); ok && entering {
			heading.HeadingID = githubSlug(headingText(heading))
		}
		return ast.GoToNext
	})
}

// githubSlug is the anchor rule GitHub uses: lowercased, spaces to dashes, punctuation
// dropped, a dash or an underscore kept. "Trusted proxies" is "trusted-proxies" and
// "What `--version` reports" is "what---version-reports".
func githubSlug(text string) string {
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

// rewriteDocLinks rewrites the links of a parsed doc before it renders: relative links to a
// hosted file keep their name and gain .html (a #fragment rides along), a relative link to
// anything else is sent to its GitHub source, and every absolute link is left alone. The
// pass works on the parsed tree, so text inside code spans and code blocks is never touched.
func rewriteDocLinks(doc ast.Node) {
	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}
		if link, ok := node.(*ast.Link); ok {
			link.Destination = []byte(rewriteDocLink(string(link.Destination)))
		}
		return ast.GoToNext
	})
}

// rewriteDocLink is the link rule for one destination.
func rewriteDocLink(dest string) string {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
		return dest
	}
	name, fragment := dest, ""
	if i := strings.Index(dest, "#"); i >= 0 {
		name, fragment = dest[:i], dest[i:]
	}
	page := strings.TrimSuffix(name, ".md") + ".html"
	for _, set := range docSets {
		if page == set.summary {
			return page + fragment
		}
		for _, hosted := range set.pages {
			if page == hosted {
				return page + fragment
			}
		}
	}
	if strings.HasSuffix(name, ".md") {
		// The usage files link relative to docs/usages, which is where the source lives.
		return docsSourceBase + name + fragment
	}
	return dest
}

// brandCSS is the one branding style the docs shell and the landing page share. The names
// here never borrow the logo's own brand-* class names: its <style> is inlined on the same
// page, and the two must not reach each other.
const brandCSS = `:root{--sandbin-purple:#4E1680;--sandbin-amber:#EA9B25;--sandbin-canvas:#F8F7F4}
body{max-width:44rem;margin:0 auto;padding:1.25rem 1rem 3rem;background:#F8F7F4;color:#222;
font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;line-height:1.55}
h1,h2,h3,h4,h5,h6{font-family: 'Poetsen One', sans-serif;color:#4E1680;line-height:1.2}
h1{font-size:1.9rem;margin:0 0 0.4em}
h2{font-size:1.35rem;margin:1.6em 0 0.4em}
h3{font-size:1.1rem;margin:1.4em 0 0.3em}
a{color:#4E1680}
p,ul,ol{overflow-wrap:break-word}
pre{background:#fff;border:1px solid #e3ddd3;border-left:4px solid #EA9B25;border-radius:4px;
padding:0.6em 0.8em;overflow-x:auto}
code{background:#efeadf;border-radius:3px;padding:0.1em 0.35em;
font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:0.95em}
pre code{background:none;padding:0;border-radius:0;font-size:1em}
table{border-collapse:collapse;width:100%;margin:1em 0}
th,td{border:1px solid #ded7cb;padding:0.45em 0.6em;text-align:left;vertical-align:top}
th{background:#4E1680;color:#fff}
blockquote{border-left:4px solid #EA9B25;margin:1em 0;padding:0.2em 0 0.2em 0.9em;color:#444}
.page-head{display:flex;align-items:flex-end;gap:1rem;border-bottom:2px solid #4E1680;
padding-bottom:0.8rem;margin-bottom:1.4rem;flex-wrap:wrap}
.wordmark{width:56px;height:86px;flex:none}
.wordmark-hero{width:120px;height:183px}
.wordmark svg,.wordmark-hero svg{display:block}
.logo-link{text-decoration:none}
.page-head nav{font-size:0.95rem}
.page-head nav a{margin-right:0.7rem}
.tagline{color:#555;margin:0 0 1.5em}
.cmd{display:flex;align-items:center;gap:0.7rem}
.cmd pre{flex:1 1 auto;margin:0}
.copy{flex:none;border:1px solid #4E1680;background:#fff;color:#4E1680;border-radius:4px;
padding:0.4em 0.8em;font:inherit;cursor:pointer}
`

// logoBlock is the embedded wordmark markup, inlined verbatim inside a sized wrapper: the
// SVG's own width/height=100% and viewBox scale to that wrapper, and its <style> keeps the
// web font its @import names.
func logoBlock(class string) string {
	return "<div class=\"" + class + "\">" + sandbin.LogoSVG + "</div>"
}

// docShell wraps one rendered body in the branded page: the logo, links back to / and /docs,
// a link to the page's own GitHub source, then the body.
func docShell(title, sourceURL, bodyHTML string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	b.WriteString("<style>\n" + brandCSS + "</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString(docHead(sourceURL))
	b.WriteString(bodyHTML)
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// docHead is the header the docs shell and the landing page share: the inline wordmark
// linking home, then the links back to / and /docs and to a GitHub source.
func docHead(sourceURL string) string {
	var b strings.Builder
	b.WriteString("<header class=\"page-head\">\n")
	b.WriteString("<a class=\"logo-link\" href=\"/\" aria-label=\"Sandbin home\">" + logoBlock("wordmark") + "</a>\n")
	b.WriteString("<nav><a href=\"/\">Sandbin</a><a href=\"/docs\">docs</a>")
	b.WriteString("<a href=\"" + sourceURL + "\">source on GitHub</a></nav>\n")
	b.WriteString("</header>\n")
	return b.String()
}

// serveDocs answers GET and HEAD on the docs routes: the hub, a hosted page named whole, and
// the embedded logo. Everything else under /docs is the plain 404 shape, and every other
// method is the plain 405 shape.
func (s *server) serveDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case "/docs", "/docs/":
		writeDocsPage(w, docsHub)
		return
	case docsLogoPath:
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(sandbin.LogoSVG))
		return
	}
	if page, ok := docsPages[strings.TrimPrefix(r.URL.Path, "/docs/")]; ok {
		writeDocsPage(w, page.html)
		return
	}
	http.Error(w, "unknown bin", http.StatusNotFound)
}

// writeDocsPage answers one rendered page: the body is already the complete HTML, cached at
// startup, so answering is a single write.
func writeDocsPage(w http.ResponseWriter, pageHTML string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(pageHTML))
}
