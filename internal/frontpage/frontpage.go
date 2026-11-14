// Package frontpage renders the landing page the server answers at "/" with on GET. The whole
// page is one embedded text/template, parsed once at package init: the wordmark, the tagline,
// the GitHub links, the per-harness install tabs and the links into the hosted docs are static
// markup in front.html, and the four values that vary — the branding style the docs shell
// shares, the embedded wordmark markup, the base URL the install commands name, and the harness
// list the tab row is generated from — arrive as Data.
//
// The harness commands are page content, so they live here as data rather than as markup:
// DefaultHarnesses is the table, and each command names the same {{.Base}} token the page
// itself uses. Rendering a page fills that token in every command with the page's own base
// (text/template does not re-parse a string it prints), which is why the tab row and the
// command blocks can be generated from the slice with one range and cannot drift from it.
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
	"strings"
	"text/template"
)

// frontHTML is the landing page's markup, {{.BrandCSS}} inside the <style>, {{.Logo}} inside
// its hero-sized wrapper in the header, and {{range $i, $h := .Harnesses}} around the tab row
// and the command blocks each harness's one-liner lands in.
//
//go:embed front.html
var frontHTML string

// Parsed once at init, like the SKILL.md and install.sh templates: a front.html that does not
// parse stops the server at startup instead of serving one broken page.
var pageTemplate = template.Must(template.New("front").Parse(frontHTML))

// baseToken is the page's one dynamic value spelled as a template token, and the token every
// harness command names in place of the address it will be served from.
const baseToken = "{{.Base}}"

// installCmd is the one-liner every tab shows up to its --dest: the same pipe the landing has
// always carried, fetched from the server that serves the page and piped into bash.
const installCmd = "curl -fsSL " + baseToken + "/skills/sandbin-preview/install.sh | bash"

// Harness is one tab of the landing's install selector: the label on the button and the
// command its command block shows. The command names baseToken for the server's own address,
// which Render fills, so a Harness says what the tab installs and where.
type Harness struct {
	Label string
	Cmd   string
}

// DefaultHarnesses is the landing page's tab row: the shared `~/.agents/skills` folder first,
// with no --dest at all, then each harness's own global skills folder as a quoted $HOME path
// because bash does not expand a tilde in an assignment or a quoted argument. Every command is
// a form the served install.sh already accepts, so the page promises nothing the installer does
// not do. A fresh slice per call: a caller may edit what it is handed without touching the next
// request's page.
func DefaultHarnesses() []Harness {
	dest := func(dir string) string {
		return installCmd + " -s -- --dest \"$HOME/" + dir + "/skills\""
	}
	return []Harness{
		{Label: "Agents", Cmd: installCmd},
		{Label: "OpenCode", Cmd: dest(".config/opencode")},
		{Label: "Claude Code", Cmd: dest(".claude")},
		{Label: "Codex", Cmd: dest(".codex")},
		{Label: "Cursor", Cmd: dest(".cursor")},
		{Label: "Copilot", Cmd: dest(".copilot")},
		{Label: "Gemini CLI", Cmd: dest(".gemini")},
		{Label: "Windsurf", Cmd: dest(".codeium/windsurf")},
	}
}

// Data is what the page is assembled from. Base is the example prefix implied by the request
// ("https://host" or the "https://this.host" placeholder for an odd Host); Logo the embedded
// wordmark markup; BrandCSS the branding style the docs shell also uses; Harnesses the tab row,
// which cmd/sandbin supplies as DefaultHarnesses() and a test may replace with its own rows.
type Data struct {
	Base      string
	Logo      string
	BrandCSS  string
	Harnesses []Harness
}

// Render executes the page straight into w. The bytes it writes are exactly front.html with its
// actions replaced by the fields, every harness command's baseToken filled with Base on the way
// (the copy of the slice keeps the caller's rows untouched).
func Render(w io.Writer, d Data) error {
	if len(d.Harnesses) > 0 {
		filled := make([]Harness, len(d.Harnesses))
		for i, h := range d.Harnesses {
			h.Cmd = strings.ReplaceAll(h.Cmd, baseToken, d.Base)
			filled[i] = h
		}
		d.Harnesses = filled
	}
	return pageTemplate.Execute(w, d)
}
