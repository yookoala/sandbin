package frontpage

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// sampleLogo and sampleCSS stand in for the two constants cmd/sandbin hands the template:
// the embedded wordmark markup and the branding style the docs shell shares. The tests only
// need strings that could plausibly be them: what they pin is where the values land.
const (
	sampleLogo = "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 56 86\"><style>.b{fill:#4E1680}</style><path class=\"b\" d=\"M0 0h56v86H0z\"/></svg>"
	sampleCSS  = ":root{--sandbin-purple:#4E1680}\n.page-head{display:flex}"
)

// sampleData is one ordinary render: a https base with a port, as frontBase answers behind a
// TLS socket or a trusted proxy, and the harness list the handler hands the page.
func sampleData() Data {
	return Data{
		Base:      "https://preview.example.org:8443",
		Logo:      sampleLogo,
		BrandCSS:  sampleCSS,
		Harnesses: DefaultHarnesses(),
	}
}

// wantHarnessCmds are the eight decisions-table rows with the sample base filled in: the page
// must serve exactly these one-liners, in this order, and each is a form install.sh accepts.
var wantHarnessCmds = []string{
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.config/opencode/skills\"",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.claude/skills\"",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.codex/skills\"",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.cursor/skills\"",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.copilot/skills\"",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.gemini/skills\"",
	"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.codeium/windsurf/skills\"",
}

// wantHarnessLabels and wantHarnessPaths are the same eight rows' other two columns: the label
// on the combobox face and the folder each command installs into (the folder named on the
// command plus its sandbin-preview subfolder).
var (
	wantHarnessLabels = []string{"a generic harness", "OpenCode", "Claude Code", "Codex", "Cursor", "Copilot", "Gemini CLI", "Windsurf"}
	wantHarnessPaths  = []string{
		"$HOME/.agents/skills/sandbin-preview",
		"$HOME/.config/opencode/skills/sandbin-preview",
		"$HOME/.claude/skills/sandbin-preview",
		"$HOME/.codex/skills/sandbin-preview",
		"$HOME/.cursor/skills/sandbin-preview",
		"$HOME/.copilot/skills/sandbin-preview",
		"$HOME/.gemini/skills/sandbin-preview",
		"$HOME/.codeium/windsurf/skills/sandbin-preview",
	}
)

// islandOpen and islandClose bracket the JSON data island in front.html; the island tests
// below read exactly what sits between them.
const (
	islandOpen  = "<script id=\"harness-data\" type=\"application/json\">"
	islandClose = "</script>"
)

// harnessIsland extracts the data island's raw text from a rendered page and fails the test
// when the island is not opened exactly once.
func harnessIsland(t *testing.T, body string) string {
	t.Helper()
	if n := strings.Count(body, islandOpen); n != 1 {
		t.Fatalf("%d harness data islands, want 1 in:\n%s", n, body)
	}
	start := strings.Index(body, islandOpen) + len(islandOpen)
	end := strings.Index(body[start:], islandClose)
	if end < 0 {
		t.Fatalf("the harness data island is never closed in:\n%s", body)
	}
	return body[start : start+end]
}

// parseIsland JSON-parses the island the way the page's own script does and returns its rows:
// a round trip that must preserve every quote the commands carry.
func parseIsland(t *testing.T, body string) []struct {
	Label string `json:"label"`
	Cmd   string `json:"cmd"`
	Path  string `json:"path"`
} {
	t.Helper()
	var rows []struct {
		Label string `json:"label"`
		Cmd   string `json:"cmd"`
		Path  string `json:"path"`
	}
	raw := harnessIsland(t, body)
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("the data island does not parse as JSON (%v):\n%s", err, raw)
	}
	return rows
}

// The template is parsed at package init: a front.html that does not parse stops the server
// at startup, which is what template.Must is for.
func TestPageTemplateParsesAtInit(t *testing.T) {
	if pageTemplate == nil {
		t.Fatal("pageTemplate is nil: front.html was not parsed at init")
	}
	if pageTemplate.Lookup("front") == nil {
		t.Fatal(`pageTemplate has no template named "front"`)
	}
}

// Every action in front.html must be filled by its Data field or by what Render computes from
// it: a surviving "{{" means a token the executor never saw, and a field that never reaches
// the page is a rendering bug.
func TestRenderFillsEveryField(t *testing.T) {
	d := sampleData()
	var buf bytes.Buffer
	if err := Render(&buf, d); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	// No action survives. A }} may legitimately appear — the page's own CSS spells its
	// keyframe blocks with adjacent braces — but every }} must belong to a closed pair, and
	// the only way an action reaches the page in the first place is a {{ the executor missed.
	if strings.Contains(body, "{{") {
		t.Errorf("Render left {{ in the page:\n%s", body)
	}

	for _, want := range []string{
		// BrandCSS lands inside the <style> element, which keeps its own newline before the
		// closing tag exactly as the page has always had it.
		"<style>\n" + sampleCSS + "\n</style>",
		// Base lands inside the install commands, whole: the default row's line is the one the
		// page has always carried.
		"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash",
		// The header closes and the static body resumes: the page has always carried the
		// header's own trailing newline on top of the newline that ended its line, which is
		// one blank line between the two.
		"</header>\n\n<h1 id=\"this-is-a-sandbin-instance\">This is a Sandbin instance</h1>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Render missing %q in:\n%s", want, body)
		}
	}
}

// The logo is the content of the hero-sized wrapper: the exact div the landing page has always
// carried, and nothing else wraps it.
func TestRenderPlacesTheLogoInItsHeroWrapper(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	want := "<div class=\"wordmark wordmark-hero\">" + sampleLogo + "</div>"
	if !strings.Contains(body, want) {
		t.Errorf("Render missing %q in:\n%s", want, body)
	}
	// The wrapper is opened once and its closing tag belongs to it: the logo is not left to
	// wander into the nav or the body.
	if n := strings.Count(body, "<div class=\"wordmark wordmark-hero\">"); n != 1 {
		t.Errorf("hero wrapper opened %d times, want 1 in:\n%s", n, body)
	}
	open := strings.Index(body, "<div class=\"wordmark wordmark-hero\">")
	header := strings.Index(body, "</header>")
	if open < 0 || header < 0 || open > header {
		t.Errorf("the logo wrapper (offset %d) is not inside the <header> block (ends at %d)", open, header)
	}
	if end := strings.Index(body, "</div>"); end < 0 || end > header {
		t.Errorf("the logo wrapper's </div> (offset %d) is not inside the <header> block (ends at %d)", end, header)
	}
}

// The base arrives in every harness command as one unbroken string: the placeholder frontBase
// falls back to for an odd Host is rendered like any other base. Eight commands ride the data
// island and the command widget's initial value names the default one once more.
func TestRenderCarriesTheInstallCommandLine(t *testing.T) {
	for _, base := range []string{
		"http://127.0.0.1:8080",
		"https://example.com",
		"https://this.host",
	} {
		var buf bytes.Buffer
		d := sampleData()
		d.Base = base
		if err := Render(&buf, d); err != nil {
			t.Fatalf("Render(base %q): %v", base, err)
		}
		want := "curl -fsSL " + base + "/skills/sandbin-preview/install.sh | bash"
		got := buf.String()
		if !strings.Contains(got, want) {
			t.Errorf("base %q missing install line %q in:\n%s", base, want, got)
		}
		if n := strings.Count(got, "curl -fsSL "); n != len(wantHarnessCmds)+1 {
			t.Errorf("base %q: %d install commands, want %d in:\n%s", base, n, len(wantHarnessCmds)+1, got)
		}
	}
}

// DefaultHarnesses is the decisions table: eight rows in order, the generic row first with no
// --dest at all, every other destination a quoted $HOME path rather than a tilde bash would
// not expand, and every row's Path naming the folder its command installs into.
func TestDefaultHarnessesAreTheDecisionsTable(t *testing.T) {
	hs := DefaultHarnesses()
	if len(hs) != len(wantHarnessLabels) {
		t.Fatalf("DefaultHarnesses returned %d rows, want %d", len(hs), len(wantHarnessLabels))
	}
	for i, h := range hs {
		if h.Label != wantHarnessLabels[i] {
			t.Errorf("row %d label = %q, want %q", i, h.Label, wantHarnessLabels[i])
		}
		// Each stored command names the page's own token for the server address, which Render
		// fills with the address the request arrived at.
		wantCmd := strings.Replace(wantHarnessCmds[i], "https://preview.example.org:8443", baseToken, 1)
		if h.Cmd != wantCmd {
			t.Errorf("row %d (%s) Cmd = %q, want %q", i, h.Label, h.Cmd, wantCmd)
		}
		if h.Path != wantHarnessPaths[i] {
			t.Errorf("row %d (%s) Path = %q, want %q", i, h.Label, h.Path, wantHarnessPaths[i])
		}
		if i == 0 {
			if strings.Contains(h.Cmd, "--dest") {
				t.Errorf("the generic row %q names --dest: %q", h.Label, h.Cmd)
			}
			continue
		}
		if !strings.HasPrefix(h.Cmd, "curl -fsSL "+baseToken+"/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/") {
			t.Errorf("row %d (%s) is not the installer's --dest form: %q", i, h.Label, h.Cmd)
		}
		if !strings.HasSuffix(h.Cmd, "/skills\"") {
			t.Errorf("row %d (%s) destination is not a quoted skills folder: %q", i, h.Label, h.Cmd)
		}
		// The Path is the command's own destination plus the sandbin-preview subfolder.
		dest := h.Cmd[strings.Index(h.Cmd, "--dest \"")+len("--dest \"") : strings.LastIndex(h.Cmd, "\"")]
		if h.Path != dest+"/sandbin-preview" {
			t.Errorf("row %d (%s) Path %q is not its command's folder %q plus sandbin-preview", i, h.Label, h.Path, dest)
		}
	}
	for _, h := range hs {
		if strings.Contains(h.Cmd, "~/") {
			t.Errorf("row %q names a tilde bash would not expand: %q", h.Label, h.Cmd)
		}
		if strings.HasPrefix(h.Cmd, "\"") || strings.Contains(h.Path, "~/") {
			t.Errorf("row %q carries an unanchored path: %q / %q", h.Label, h.Cmd, h.Path)
		}
	}
}

// The list is handed out fresh: a caller that appends to or edits its rows cannot change what
// the next request's page shows.
func TestDefaultHarnessesReturnAFreshSlice(t *testing.T) {
	first := DefaultHarnesses()
	first[0].Label = "Edited"
	first = append(first, Harness{Label: "Extra", Cmd: "extra"})
	second := DefaultHarnesses()
	if len(second) != 8 {
		t.Fatalf("a second DefaultHarnesses has %d rows, want 8", len(second))
	}
	if second[0].Label != "a generic harness" {
		t.Errorf("a second DefaultHarnesses names row 0 %q, want %q", second[0].Label, "a generic harness")
	}
	for _, h := range second {
		if h.Label == "Edited" || h.Label == "Extra" {
			t.Errorf("an edit of one call reached the next call: %+v", second)
		}
	}
}

// The eight harness rows reach the page as the JSON data island: a JSON.parse of the island
// yields the eight labels, base-filled commands and install paths in order, and the quoted
// --dest "$HOME/…" commands round-trip through it whole.
func TestRenderCarriesEveryHarnessCommand(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	rows := parseIsland(t, body)
	if len(rows) != len(wantHarnessCmds) {
		t.Fatalf("the data island carries %d rows, want %d", len(rows), len(wantHarnessCmds))
	}
	for i, row := range rows {
		if row.Label != wantHarnessLabels[i] {
			t.Errorf("island row %d label = %q, want %q", i, row.Label, wantHarnessLabels[i])
		}
		if row.Cmd != wantHarnessCmds[i] {
			t.Errorf("island row %d cmd = %q, want %q", i, row.Cmd, wantHarnessCmds[i])
		}
		if row.Path != wantHarnessPaths[i] {
			t.Errorf("island row %d path = %q, want %q", i, row.Path, wantHarnessPaths[i])
		}
	}
	if n := strings.Count(body, "curl -fsSL "); n != len(wantHarnessCmds)+1 {
		t.Errorf("%d install commands, want %d in:\n%s", n, len(wantHarnessCmds)+1, body)
	}
	// The widget's own parts: one readonly input, one inner copy icon button, one flash span.
	for _, want := range []string{"class=\"cmd-input\"", "class=\"cmd-copy\"", "class=\"cmd-flash\""} {
		if n := strings.Count(body, want); n != 1 {
			t.Errorf("%d copies of %q, want 1 in:\n%s", n, want, body)
		}
	}
}

// The page opens on the generic row without any scripting: the command widget's initial value
// is that row's command, the face names the row, and the path line names its folder.
func TestRenderShowsOnlyTheFirstTabCommand(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	visible := "value=\"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash\""
	if !strings.Contains(body, visible) {
		t.Errorf("the command widget's initial value is not the generic row's command (%q) in:\n%s", visible, body)
	}
	if n := strings.Count(body, "value=\""); n != 1 {
		t.Errorf("%d prefilled values, want 1 in:\n%s", n, body)
	}
	if !strings.Contains(body, "<span id=\"pick-label\">a generic harness</span>") {
		t.Errorf("the combobox face does not open on the generic row in:\n%s", body)
	}
	if !strings.Contains(body, "<span class=\"path\" id=\"cmd-path\">"+wantHarnessPaths[0]+"</span>") {
		t.Errorf("the path line does not open on %q in:\n%s", wantHarnessPaths[0], body)
	}
	if strings.Contains(visible, "--dest") {
		t.Errorf("the default command carries --dest: %q", visible)
	}
}

// The copy set the mockup approved: the title and h1, the GitHub tagline line, the section
// heading, the picker lead-in, and the command lead-in, plus the install-path line. What the
// redesign deleted stays deleted: the old wordmark h1, the GitHub-links paragraph, the hosted
// docs section, and the tab-era prose.
func TestRenderCarriesTheDefaultTabSentenceAndProjectNote(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	for _, want := range []string{
		"<title>This is a Sandbin instance</title>",
		"<h1 id=\"this-is-a-sandbin-instance\">This is a Sandbin instance</h1>",
		"Learn more about Sandbin on <a href=\"https://github.com/yookoala/sandbin\">GitHub</a>",
		"<h2 id=\"tell-your-agent-to-use-this\">Tell Your Agent to use This</h2>",
		"<p>Tell me what harness you are using:</p>",
		"<div class=\"pick-line\">I use <span class=\"pick\" id=\"pick\">",
		"<p>Run this command in your terminal:</p>",
		"Skill Install Path: <span class=\"path\" id=\"cmd-path\">",
		"aria-label=\"Which harness you use\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	for _, gone := range []string{
		"<h1 id=\"sandbin\">Sandbin</h1>",
		"<h2 id=\"give-the-skill-to-your-agent\">",
		"<h2 id=\"the-hosted-docs\">The hosted docs",
		"the usage docs on GitHub",
		"The first tab is the shared folder",
		"also read <code>.agents/skills</code>",
		"the current project instead",
		"<code>| bash -s -- --dest .agents/skills</code>",
		"initializes the current directory by asking a single question, the API key",
		"hand the installer a destination",
		"<script src=",
		"<noscript",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("the page still carries %q in:\n%s", gone, body)
		}
	}
}

// The island and the initial widget both come from the slice the handler hands over: a
// different list renders a different page, and the command shown server-side escapes its
// quotes as &quot; so the attribute survives.
func TestRenderTabsFollowTheHarnessSlice(t *testing.T) {
	var buf bytes.Buffer
	d := sampleData()
	d.Harnesses = []Harness{
		{Label: "Odd One", Cmd: installCmd + " -s -- --dest \"$HOME/.odd/skills\"", Path: "$HOME/.odd/skills/sandbin-preview"},
		{Label: "Plain Row", Cmd: installCmd, Path: "$HOME/plain/sandbin-preview"},
	}
	if err := Render(&buf, d); err != nil {
		t.Fatalf("Render(two harnesses): %v", err)
	}
	body := buf.String()

	rows := parseIsland(t, body)
	if len(rows) != 2 {
		t.Fatalf("the data island carries %d rows, want 2", len(rows))
	}
	if rows[0].Cmd != "curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.odd/skills\"" {
		t.Errorf("island row 0 cmd = %q, want the quoted --dest form", rows[0].Cmd)
	}
	if rows[1].Label != "Plain Row" {
		t.Errorf("island row 1 label = %q, want %q", rows[1].Label, "Plain Row")
	}
	for _, want := range []string{
		"value=\"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest &quot;$HOME/.odd/skills&quot;\"",
		"<span id=\"pick-label\">Odd One</span>",
		"<span class=\"path\" id=\"cmd-path\">$HOME/.odd/skills/sandbin-preview</span>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	// What a slice cannot bring: the rows of the default table are gone and no action token
	// survives. The sentences front.html carries itself stay, because they are the page's own
	// prose and not derived from the slice.
	for _, gone := range []string{"Gemini CLI", "Windsurf", "{{"} {
		if strings.Contains(body, gone) {
			t.Errorf("the two-row render still carries %q in:\n%s", gone, body)
		}
	}
}

// A hostile Base must stay inside the island as data: json.Marshal escapes < and >, so a
// Base carrying </script> lands as \u003c/script\u003e and can never close the script element
// it is written into. (A base that hostile is never produced by cmd/sandbin's frontBase,
// which falls back to the placeholder — pinned in the handlers tests — but the island keeps
// the escape even for a package caller that hands Render one directly.)
func TestHostileBaseStaysInsideTheJSONIsland(t *testing.T) {
	hostile := "https://evil.example.org</script><script>alert(1)</script>"
	var buf bytes.Buffer
	d := sampleData()
	d.Base = hostile
	if err := Render(&buf, d); err != nil {
		t.Fatalf("Render(hostile base): %v", err)
	}
	body := buf.String()

	island := harnessIsland(t, body)
	// The island's own text carries no raw closer or opener: every < and > of the hostile
	// base lands JSON-escaped inside it.
	if strings.Contains(island, "</script>") || strings.Contains(island, "<script") {
		t.Errorf("the hostile base broke out of the island as raw markup:\n%s", island)
	}
	if !strings.Contains(island, "https://evil.example.org\\u003c/script\\u003e") {
		t.Errorf("the hostile base did not land JSON-escaped in the island:\n%s", island)
	}
	// A JSON.parse sees the hostile base whole, exactly as given — escaped, not filtered.
	rows := parseIsland(t, body)
	if len(rows) != len(wantHarnessCmds) {
		t.Fatalf("the hostile render carries %d island rows, want %d", len(rows), len(wantHarnessCmds))
	}
	if want := "curl -fsSL " + hostile + "/skills/sandbin-preview/install.sh | bash"; rows[0].Cmd != want {
		t.Errorf("island row 0 cmd = %q, want the hostile base filled whole", rows[0].Cmd)
	}
}

// With the default list the page is finished markup: no action token of any kind survives,
// and no command names a path bash would hand over literally.
func TestRenderWithDefaultsLeavesNoTokensOrTildes(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	for _, gone := range []string{"{{", "~/", "baseToken"} {
		if strings.Contains(body, gone) {
			t.Errorf("the rendered page carries %q in:\n%s", gone, body)
		}
	}
	// The island spells each --dest destination as an escaped quote before $HOME, and the
	// default row's command — the only one also baked into the widget's value — has none.
	if n := strings.Count(body, "--dest \\\"$HOME/"); n != len(wantHarnessCmds)-1 {
		t.Errorf("%d quoted $HOME destinations, want %d in:\n%s", n, len(wantHarnessCmds)-1, body)
	}
}

// The page is static apart from its fields: two renders of the same data are the same
// bytes, and the same render into a fresh writer never carries a previous call's state.
func TestRenderIsDeterministic(t *testing.T) {
	var first, second bytes.Buffer
	for _, buf := range []*bytes.Buffer{&first, &second} {
		if err := Render(buf, sampleData()); err != nil {
			t.Fatalf("Render: %v", err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("two Renders of the same data differ:\nfirst:\n%s\nsecond:\n%s", first.String(), second.String())
	}
}

// Empty fields are still a page: the render must not panic and must stay stable across calls.
func TestRenderWithEmptyFields(t *testing.T) {
	var first, second bytes.Buffer
	for _, buf := range []*bytes.Buffer{&first, &second} {
		if err := Render(buf, Data{}); err != nil {
			t.Fatalf("Render(empty Data): %v", err)
		}
	}
	body := first.String()
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("two empty renders differ:\nfirst:\n%s\nsecond:\n%s", body, second.String())
	}
	for _, gone := range []string{"{{", "<nil>"} {
		if strings.Contains(body, gone) {
			t.Errorf("empty render left %q in the page:\n%s", gone, body)
		}
	}
	// The static page is still the static page: its markup and script are untouched by the
	// fields being empty, and an empty harness list simply renders an empty island.
	for _, want := range []string{
		"<!doctype html>",
		"<html lang=\"en\">",
		"<title>This is a Sandbin instance</title>",
		"<h1 id=\"this-is-a-sandbin-instance\">This is a Sandbin instance</h1>",
		"<header class=\"page-head\">",
		"<nav class=\"icon-nav\">",
		"<a class=\"icon-link\" href=\"/docs\"",
		"function sandbinApply(h) {",
		"function sandbinHarnessOrder(q) {",
		"function sandbinCopyInstall(button) {",
		"var sandbinPick = { current: SANDBIN_HARNESSES[0], order: SANDBIN_HARNESSES.slice(), hl: 0 };",
		"</body>\n</html>\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty render missing %q in:\n%s", want, body)
		}
	}
	if island := harnessIsland(t, body); island != "[]" {
		t.Errorf("an empty harness list rendered island %q, want \"[]\"\n", island)
	}
	// An empty BrandCSS leaves the template's own newline between the tags.
	if n := strings.Count(body, "<style>\n\n</style>"); n != 1 {
		t.Errorf("empty render has %d empty <style> pairs, want 1:\n%s", n, body)
	}
}
