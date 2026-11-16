package main

import (
	"html"
	"log"
	"net/http"
	"strings"
	"text/template"
	"unicode"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/yookoala/sandbin"
	"github.com/yookoala/sandbin/internal/version"
)

// The usage docs the server hosts under /docs come from the module-root embeds in skill.go
// and are rendered once, here, at package init: the same posture as the skill templates —
// a doc that cannot be parsed stops the server at startup rather than one request. Each
// stored page HTML is also parsed as a template at init and executed once with sample data,
// then executed per request with this instance's own front address (see expandDoc). A
// markdown page that means to show a literal token escapes it as {{`{{`}} — the pipeline
// emits exactly those escapes and expands everything else.

// hostedPage is one rendered docs page: its <title> text, the complete HTML of the page, and
// that HTML parsed once at init as a template. The raw html stands behind the template as the
// fallback an execute failure logs and serves.
type hostedPage struct {
	title string
	html  string
	tmpl  *template.Template
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
	// docsSourceBase is where a hosted page's markdown lives on GitHub: the body-link
	// rewriter sends a relative .md link with no hosted page there, and only there — the
	// nav's GitHub door is one fixed repo URL on every page.
	docsSourceBase = "https://github.com/yookoala/sandbin/blob/main/docs/usages/"
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
		pageHTML := docShell(title, body)
		pages[page] = hostedPage{title: title, html: pageHTML, tmpl: parseDocTemplate("usage doc "+name, pageHTML)}
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
func buildDocsHub() hostedPage {
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
	pageHTML := docShell("Sandbin usage docs", b.String())
	return hostedPage{title: "Sandbin usage docs", html: pageHTML, tmpl: parseDocTemplate("docs hub", pageHTML)}
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
// page, and the two must not reach each other. The body rule makes every hosted page a flex
// column at least as tall as the viewport, main takes the slack, and .site-footer is the
// powered-by line both page families end with. The icon rules at the end style the icon nav
// every docs page and the landing carry; the plain .page-head nav a margin rule stays for
// any text nav, overridden for the icon nav.
const brandCSS = `:root{--sandbin-purple:#4E1680;--sandbin-amber:#EA9B25;--sandbin-canvas:#F8F7F4}
body{max-width:44rem;margin:0 auto;padding:1.25rem 1rem 3rem;background:#F8F7F4;color:#222;
font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;line-height:1.55;
display:flex;flex-direction:column;box-sizing:border-box;min-height:100vh}
main{flex:1 0 auto}
.site-footer{margin-top:auto;padding-top:1.2rem;border-top:1px solid #e3ddd3;color:#666;font-size:0.85rem;text-align:center}
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

.page-head nav.icon-nav{margin-left:auto;display:flex;gap:0.55rem;align-items:center}
.page-head nav.icon-nav a{margin-right:0}
.icon-link{display:inline-flex;align-items:center;justify-content:center;width:2.1rem;height:2.1rem;
border:none;border-radius:8px;color:var(--sandbin-purple);text-decoration:none}
.icon-link:hover,.icon-link:focus-visible{color:var(--sandbin-amber);background:#fff}
.icon-link svg{width:1.35rem;height:1.35rem;display:block}

.codeblock{position:relative}
.codeblock pre{margin:1em 0}
.block-copy{position:absolute;right:0.45em;top:calc(1em + 0.3em);border:1px solid #ded7cb;background:#fff;
color:var(--sandbin-purple);border-radius:4px;padding:0.25em;line-height:0;cursor:pointer}
.block-copy:hover,.block-copy:focus-visible{color:var(--sandbin-amber)}
.block-copy svg{width:1.05rem;height:1.05rem;display:block}
.block-copy.copied{color:#2e7d32;border-color:#2e7d32}
`

// logoBlock is the embedded wordmark markup, inlined verbatim inside a sized wrapper: the
// SVG's own width/height=100% and viewBox scale to that wrapper, and its <style> keeps the
// web font its @import names.
func logoBlock(class string) string {
	return "<div class=\"" + class + "\">" + sandbin.LogoSVG + "</div>"
}

// docCopyJS wraps every code block under <main> with a copy button, the landing page's
// pattern: Clipboard API first, an execCommand textarea fallback, and a brief green check on
// the button when it worked. The button's center aligns with the center of the code's first
// line: exact vertical center in a single-line block, top-right in a multi-line one.
const docCopyJS = `<script>
(function () {
  var COPY_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="13" height="13" x="9" y="9" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
  var CHECK_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>';
  function flashCopied(button, restore) {
    button.classList.add("copied");
    button.innerHTML = CHECK_ICON;
    setTimeout(function () {
      button.classList.remove("copied");
      button.innerHTML = restore;
    }, 1400);
  }
  function copyText(text, button, restore) {
    function done() { flashCopied(button, restore); }
    function fallback() {
      var area = document.createElement("textarea");
      area.value = text;
      document.body.appendChild(area);
      area.select();
      document.execCommand("copy");
      document.body.removeChild(area);
      done();
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, fallback);
    } else {
      fallback();
    }
  }
  document.querySelectorAll("main pre").forEach(function (pre) {
    var wrap = document.createElement("div");
    wrap.className = "codeblock";
    pre.parentNode.insertBefore(wrap, pre);
    wrap.appendChild(pre);
    var icon = COPY_ICON;
    var button = document.createElement("button");
    button.type = "button";
    button.className = "block-copy";
    button.title = "copy";
    button.setAttribute("aria-label", "Copy the code block");
    button.innerHTML = icon;
    button.addEventListener("click", function () { copyText(pre.textContent, button, icon); });
    wrap.appendChild(button);
    var cs = getComputedStyle(pre);
    var codeEl = pre.querySelector("code") || pre;
    var lh = parseFloat(getComputedStyle(codeEl).lineHeight);
    if (!lh) { lh = parseFloat(getComputedStyle(codeEl).fontSize) * 1.5; }
    var top = pre.getBoundingClientRect().top - wrap.getBoundingClientRect().top +
      parseFloat(cs.paddingTop) + lh / 2 - button.offsetHeight / 2;
    button.style.top = top + "px";
  });
})();
</script>
`

// docShell wraps one rendered body in the branded page: the logo, the icon nav — home, docs,
// and the fixed GitHub door — then the body inside a <main> that fills the viewport, then the
// powered-by footer naming this build's version.
func docShell(title, bodyHTML string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	b.WriteString("<style>\n" + brandCSS + "</style>\n")
	b.WriteString("</head>\n<body>\n")
	b.WriteString(docHead())
	b.WriteString("<main>\n")
	b.WriteString(bodyHTML)
	b.WriteString("</main>\n")
	b.WriteString("<footer class=\"site-footer\">Powered by Sandbin | version: " + version.String() + "</footer>\n")
	b.WriteString(docCopyJS)
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// The three icon glyphs of the shared icon nav, exactly as the approved mockup carries them:
// a house, an open book, and the GitHub mark.
const (
	docIconHome = "<svg viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\" aria-hidden=\"true\"><path d=\"m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z\"/><polyline points=\"9 22 9 12 15 12 15 22\"/></svg>"
	docIconBook = "<svg viewBox=\"0 0 24 24\" fill=\"none\" stroke=\"currentColor\" stroke-width=\"2\" stroke-linecap=\"round\" stroke-linejoin=\"round\" aria-hidden=\"true\"><path d=\"M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z\"/><path d=\"M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z\"/></svg>"
	// docIconGitHub is the GitHub mark shared by every docs page; the link around it is the
	// fixed repo door that opens in a new tab.
	docIconGitHub = "<svg viewBox=\"0 0 24 24\" fill=\"currentColor\" aria-hidden=\"true\"><path d=\"M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.615-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12\"/></svg>"
)

// docHead is the header the docs shell and the landing page share: the inline wordmark
// linking home, then the borderless icon nav — home, docs, and the GitHub door to
// https://github.com/yookoala/sandbin opened in a new tab — with the first two hrefs
// root-relative.
func docHead() string {
	var b strings.Builder
	b.WriteString("<header class=\"page-head\">\n")
	b.WriteString("<a class=\"logo-link\" href=\"/\" aria-label=\"Sandbin home\">" + logoBlock("wordmark") + "</a>\n")
	b.WriteString("<nav class=\"icon-nav\">\n")
	b.WriteString("<a class=\"icon-link\" href=\"/\" title=\"home\" aria-label=\"Sandbin home\">" + docIconHome + "</a>\n")
	b.WriteString("<a class=\"icon-link\" href=\"/docs\" title=\"docs\" aria-label=\"Documentation\">" + docIconBook + "</a>\n")
	b.WriteString("<a class=\"icon-link\" href=\"https://github.com/yookoala/sandbin\" target=\"_blank\" rel=\"noopener\" title=\"source on GitHub\" aria-label=\"Source on GitHub\">" + docIconGitHub + "</a>\n")
	b.WriteString("</nav>\n")
	b.WriteString("</header>\n")
	return b.String()
}

// docInstance and docData are what bundled-doc templates may read. Instance names this
// instance itself for the request at hand: URL is its own front address, exactly as frontBase
// answers it for the landing page and the skill routes. Later fields join the same shape as
// docs need them.
type docInstance struct {
	URL string
}

type docData struct {
	Instance docInstance
}

// sampleDocData is the stand-in instance the init-time execution of every stored template
// runs with, so a page that cannot execute against the doc data is a startup panic, not a
// runtime surprise.
func sampleDocData() docData {
	return docData{Instance: docInstance{URL: "https://sandbin.example"}}
}

// parseDocTemplate parses one stored page HTML once and executes it once with sample data.
// Either failure panics at init, the way the embedded-tree mismatches do: the panic says
// which doc is at fault and why.
func parseDocTemplate(subject, pageHTML string) *template.Template {
	tmpl, err := template.New(subject).Parse(pageHTML)
	if err != nil {
		panic("the embedded " + subject + " is not a valid template: " + err.Error())
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, sampleDocData()); err != nil {
		panic("the embedded " + subject + " does not execute with sample instance data: " + err.Error())
	}
	return tmpl
}

// expandDoc executes a stored page's template with this request's data, so bundled docs can
// name the address the request arrived at. Only bundled docs take this pass; markdown hosted
// in bins is never templated. An execute failure — unreachable for the embedded tree, whose
// pages init already executed — logs and serves the page's raw html.
func (s *server) expandDoc(page hostedPage, r *http.Request) string {
	var buf strings.Builder
	data := docData{Instance: docInstance{URL: s.frontBase(r)}}
	if err := page.tmpl.Execute(&buf, data); err != nil {
		log.Printf("render docs page %s: %v", r.URL.Path, err)
		return page.html
	}
	return buf.String()
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
		writeDocsPage(w, s.expandDoc(docsHub, r))
		return
	case docsLogoPath:
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(sandbin.LogoSVG))
		return
	}
	if page, ok := docsPages[strings.TrimPrefix(r.URL.Path, "/docs/")]; ok {
		writeDocsPage(w, s.expandDoc(page, r))
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
