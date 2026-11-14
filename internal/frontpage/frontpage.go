// Package frontpage renders the landing page the server answers at "/" with on GET. The whole
// page is one embedded text/template, parsed once at package init: the wordmark, the tagline,
// the GitHub links, the installer one-liner and the links into the hosted docs are static
// markup in front.html, and the three values that vary — the branding style the docs shell
// shares, the embedded wordmark markup, and the base URL of the one command — arrive as Data.
//
// The package holds no request logic: which base is safe to echo is decided in cmd/sandbin
// (frontBase and its frontHostRE guard) and handed over as a finished string. Nothing here
// escapes or reinterprets what it is given, which is why the engine is text/template rather
// than html/template: every value is either a constant this repo owns or a host already
// validated whole, and html/template's contextual escaping would only sit between them and
// the page.
package frontpage

import (
	_ "embed"
	"io"
	"text/template"
)

// frontHTML is the landing page's markup, {{.BrandCSS}} inside the <style>, {{.Logo}} inside
// its hero-sized wrapper in the header, and {{.Base}} in the install command.
//
//go:embed front.html
var frontHTML string

// Parsed once at init, like the SKILL.md and install.sh templates: a front.html that does not
// parse stops the server at startup instead of serving one broken page.
var pageTemplate = template.Must(template.New("front").Parse(frontHTML))

// Data is what the page is assembled from. Base is the example prefix implied by the request
// ("https://host" or the "https://this.host" placeholder for an odd Host); Logo the embedded
// wordmark markup; BrandCSS the branding style the docs shell also uses.
type Data struct {
	Base     string
	Logo     string
	BrandCSS string
}

// Render executes the page straight into w. The bytes it writes are exactly front.html with
// its three actions replaced by the three fields.
func Render(w io.Writer, d Data) error {
	return pageTemplate.Execute(w, d)
}
