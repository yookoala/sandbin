// Package frontpage renders the landing page the server answers at "/" with on GET. The whole
// page is one embedded text/template, parsed once at package init: the wordmark, the copy, the
// icon nav, the harness combobox and the command widget with its copy icon and Copied flash are
// static markup in front.html, and the values that vary — the branding style the docs shell
// shares, the embedded wordmark markup, the version the footer names, and the harness table —
// arrive as Data.
//
// The harness commands are page content, so they live here as data rather than as markup:
// DefaultHarnesses is the table, and each command names the same {{.Base}} token the page
// itself uses. Rendering a page fills that token in every command with the page's own base
// (text/template does not re-parse a string it prints) and then hands the filled table to the
// page as one JSON data island, which the picker script reads with JSON.parse; the island is
// assembled with encoding/json, whose escaping is safe inside a <script> element and keeps the
// quotes of the --dest "$HOME/…" commands intact. Only the table's first row is also baked
// into the markup — the command widget's initial value, the combobox face's label and the
// install-path box — so the no-JS page still shows the default row.
//
// The package holds no request logic: which base is safe to echo is decided in cmd/sandbin
// (frontBase and its frontHostRE guard) and handed over as a finished string. Nothing here
// escapes or reinterprets what it is given, which is why the engine is text/template rather
// than html/template: every value is either a constant this repo owns or a host already
// validated whole, and the one structured value — the data island — is escaped by
// json.Marshal on the way.
package frontpage

import (
	_ "embed"
	"encoding/json"
	"io"
	"strings"
	"text/template"
)

// frontHTML is the landing page's markup: {{.BrandCSS}} inside the <style>, {{.Logo}} and
// {{.LogoDark}} inside their hero-sized wrappers in the header, {{.ThemeJS}} inside the
// <head>'s theme script, {{.DefaultLabel}}, {{.DefaultCmd}} and {{.DefaultPath}} in the
// combobox face, the command widget's initial value and the install-path box,
// {{.Version}} in the powered-by footer, and {{.HarnessJSON}} inside the JSON data island the
// picker script parses.
//
//go:embed front.html
var frontHTML string

// Parsed once at init, like the SKILL.md and install.sh templates: a front.html that does not
// parse stops the server at startup instead of serving one broken page.
var pageTemplate = template.Must(template.New("front").Parse(frontHTML))

// baseToken is the page's one dynamic value spelled as a template token, and the token every
// harness command names in place of the address it will be served from.
const baseToken = "{{.Base}}"

// installCmd is the one-liner every harness row shares up to its --dest: the same pipe the
// landing has always carried, fetched from the server that serves the page and piped into
// bash.
const installCmd = "curl -fsSL " + baseToken + "/skills/sandbin-preview/install.sh | bash"

// Harness is one row of the landing's harness table: the label on the combobox, the command
// the widget shows for it, and the folder that command installs the skill into. The command
// names baseToken for the server's own address, which Render fills, so a Harness says what
// the picker offers and where it lands.
type Harness struct {
	Label string
	Cmd   string
	Path  string
}

// DefaultHarnesses is the landing page's harness table: the generic row first — the shared
// $HOME/.agents/skills folder, with no --dest at all — then each harness's own global skills
// folder as a quoted $HOME path because bash does not expand a tilde in an assignment or a
// quoted argument. Every command is a form the served install.sh already accepts, so the page
// promises nothing the installer does not do. A fresh slice per call: a caller may edit what
// it is handed without touching the next request's page.
func DefaultHarnesses() []Harness {
	dest := func(dir string) string {
		return installCmd + " -s -- --dest \"$HOME/" + dir + "/skills\""
	}
	return []Harness{
		{Label: "a generic harness", Cmd: installCmd, Path: "$HOME/.agents/skills/sandbin-preview"},
		{Label: "OpenCode", Cmd: dest(".config/opencode"), Path: "$HOME/.config/opencode/skills/sandbin-preview"},
		{Label: "Claude Code", Cmd: dest(".claude"), Path: "$HOME/.claude/skills/sandbin-preview"},
		{Label: "Codex", Cmd: dest(".codex"), Path: "$HOME/.codex/skills/sandbin-preview"},
		{Label: "Cursor", Cmd: dest(".cursor"), Path: "$HOME/.cursor/skills/sandbin-preview"},
		{Label: "Copilot", Cmd: dest(".copilot"), Path: "$HOME/.copilot/skills/sandbin-preview"},
		{Label: "Gemini CLI", Cmd: dest(".gemini"), Path: "$HOME/.gemini/skills/sandbin-preview"},
		{Label: "Windsurf", Cmd: dest(".codeium/windsurf"), Path: "$HOME/.codeium/windsurf/skills/sandbin-preview"},
	}
}

// Data is what the page is assembled from. Base is the example prefix implied by the request
// ("https://host" or the "https://this.host" placeholder for an odd Host); Logo the embedded
// light wordmark markup; LogoDark the embedded dark-theme wordmark markup; BrandCSS the
// branding style the docs shell also uses; ThemeJS the theme script the page runs before
// paint, which persists the visitor's choice and defines the toggle its nav button calls;
// Version the version string the footer names, which cmd/sandbin supplies from
// internal/version; Harnesses the harness table, which cmd/sandbin supplies as
// DefaultHarnesses() and a test may replace with its own rows.
type Data struct {
	Base      string
	Logo      string
	LogoDark  string
	BrandCSS  string
	ThemeJS   string
	Version   string
	Harnesses []Harness
}

// islandRow is one data-island row: the same three facts as a Harness under the lowercase key
// names the picker script reads.
type islandRow struct {
	Label string `json:"label"`
	Cmd   string `json:"cmd"`
	Path  string `json:"path"`
}

// pageData is the Data Render hands the template: the caller's fields as they are, plus the
// four values Render computes from them — the JSON data island and the first row's label,
// command and path as the static markup sees them.
type pageData struct {
	Data
	HarnessJSON  string
	DefaultLabel string
	DefaultCmd   string
	DefaultPath  string
}

// Render executes the page straight into w. The bytes it writes are exactly front.html with
// its actions replaced: every harness command's baseToken filled with Base on the way (the
// copy of the slice keeps the caller's rows untouched), the filled table assembled into the
// data island with encoding/json — whose Marshal escapes < and > by default, so no command
// can close the script element it lands in — and the first row's label, command (its "
// spelled as &quot; for the value attribute) and path baked into the widget markup.
func Render(w io.Writer, d Data) error {
	if len(d.Harnesses) > 0 {
		filled := make([]Harness, len(d.Harnesses))
		for i, h := range d.Harnesses {
			h.Cmd = strings.ReplaceAll(h.Cmd, baseToken, d.Base)
			filled[i] = h
		}
		d.Harnesses = filled
	}
	rows := make([]islandRow, 0, len(d.Harnesses))
	for _, h := range d.Harnesses {
		rows = append(rows, islandRow{Label: h.Label, Cmd: h.Cmd, Path: h.Path})
	}
	island, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	p := pageData{Data: d, HarnessJSON: string(island)}
	if len(d.Harnesses) > 0 {
		first := d.Harnesses[0]
		p.DefaultLabel = first.Label
		p.DefaultCmd = strings.ReplaceAll(first.Cmd, "\"", "&quot;")
		p.DefaultPath = first.Path
	}
	return pageTemplate.Execute(w, p)
}
