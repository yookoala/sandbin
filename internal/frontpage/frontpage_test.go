package frontpage

import (
	"bytes"
	"strconv"
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

// Every action in front.html must be filled by its Data field: a surviving "{{" means a token
// the executor never saw, and a field that never reaches the page is a rendering bug.
func TestRenderFillsEveryField(t *testing.T) {
	d := sampleData()
	var buf bytes.Buffer
	if err := Render(&buf, d); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	for _, gone := range []string{"{{", "}}"} {
		if strings.Contains(body, gone) {
			t.Errorf("Render left %q in the page:\n%s", gone, body)
		}
	}

	for _, want := range []string{
		// BrandCSS lands inside the <style> element, which keeps its own newline before the
		// closing tag exactly as the page has always had it.
		"<style>\n" + sampleCSS + "\n</style>",
		// Base lands inside the install commands, whole: the default tab's line is the one the
		// page has always carried.
		"curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash",
		// The header closes and the static body resumes: the page has always carried the
		// header's own trailing newline on top of the newline that ended its line, which is
		// one blank line between the two.
		"</header>\n\n<h1 id=\"sandbin\">Sandbin</h1>",
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
		t.Errorf("hero wrapper opened %d times, want 1", n)
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
// falls back to for an odd Host is rendered like any other base, and the tab row names the
// command once per harness and nowhere else.
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
		if n := strings.Count(got, "curl -fsSL "); n != len(wantHarnessCmds) {
			t.Errorf("base %q: %d install commands, want %d", base, n, len(wantHarnessCmds))
		}
	}
}

// DefaultHarnesses is the decisions table: eight rows in order, every command a form the
// existing installer accepts, the shared-folder row alone without --dest, and every other
// destination a quoted $HOME path rather than a tilde bash would not expand.
func TestDefaultHarnessesAreTheDecisionsTable(t *testing.T) {
	hs := DefaultHarnesses()
	wantLabels := []string{"Agents", "OpenCode", "Claude Code", "Codex", "Cursor", "Copilot", "Gemini CLI", "Windsurf"}
	if len(hs) != len(wantLabels) {
		t.Fatalf("DefaultHarnesses returned %d rows, want %d", len(hs), len(wantLabels))
	}
	for i, h := range hs {
		if h.Label != wantLabels[i] {
			t.Errorf("row %d label = %q, want %q", i, h.Label, wantLabels[i])
		}
		// Each stored command names the page's own token for the server address, which Render
		// fills with the address the request arrived at.
		wantCmd := strings.Replace(wantHarnessCmds[i], "https://preview.example.org:8443", baseToken, 1)
		if h.Cmd != wantCmd {
			t.Errorf("row %d (%s) Cmd = %q, want %q", i, h.Label, h.Cmd, wantCmd)
		}
		if i == 0 {
			if strings.Contains(h.Cmd, "--dest") {
				t.Errorf("the default row %q names --dest: %q", h.Label, h.Cmd)
			}
			continue
		}
		if !strings.HasPrefix(h.Cmd, "curl -fsSL "+baseToken+"/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/") {
			t.Errorf("row %d (%s) is not the installer's --dest form: %q", i, h.Label, h.Cmd)
		}
		if !strings.HasSuffix(h.Cmd, "/skills\"") {
			t.Errorf("row %d (%s) destination is not a quoted skills folder: %q", i, h.Label, h.Cmd)
		}
	}
	for _, h := range hs {
		if strings.Contains(h.Cmd, "~/") {
			t.Errorf("row %q names a tilde bash would not expand: %q", h.Label, h.Cmd)
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
	if second[0].Label != "Agents" {
		t.Errorf("a second DefaultHarnesses names row 0 %q, want %q", second[0].Label, "Agents")
	}
	for _, h := range second {
		if h.Label == "Edited" || h.Label == "Extra" {
			t.Errorf("an edit of one call reached the next call: %+v", second)
		}
	}
}

// The eight tabs render as eight commands with the request's own address filled in, each one
// the whole text of its command block and nothing else in it.
func TestRenderCarriesEveryHarnessCommand(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	for i, want := range wantHarnessCmds {
		block := "<pre class=\"cmd-body" + hiddenSuffix(i) + "\" id=\"cmd-" + strconv.Itoa(i) + "\">" + want + "</pre>"
		if !strings.Contains(body, block) {
			t.Errorf("tab %d missing command block %q in:\n%s", i, block, body)
		}
	}
	if n := strings.Count(body, "<pre class=\"cmd-body"); n != len(wantHarnessCmds) {
		t.Errorf("%d command blocks, want %d in:\n%s", n, len(wantHarnessCmds), body)
	}
	if n := strings.Count(body, "curl -fsSL "); n != len(wantHarnessCmds) {
		t.Errorf("%d install commands, want %d in:\n%s", n, len(wantHarnessCmds), body)
	}
	// One copy button for the whole area, and it is still the same posture.
	if n := strings.Count(body, "class=\"copy\""); n != 1 {
		t.Errorf("%d copy buttons, want 1 in:\n%s", n, body)
	}
}

// The page opens on the shared-folder tab: exactly one command block is visible, it is the
// Agents one, and exactly one tab button is pressed.
func TestRenderShowsOnlyTheFirstTabCommand(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	visible := "<pre class=\"cmd-body\" id=\"cmd-0\">" + wantHarnessCmds[0] + "</pre>"
	if !strings.Contains(body, visible) {
		t.Errorf("the visible command block is not the Agents one (%q) in:\n%s", visible, body)
	}
	if n := strings.Count(body, "<pre class=\"cmd-body\""); n != 1 {
		t.Errorf("%d unhidden command blocks, want 1 in:\n%s", n, body)
	}
	if n := strings.Count(body, "<pre class=\"cmd-body hidden\""); n != len(wantHarnessCmds)-1 {
		t.Errorf("%d hidden command blocks, want %d in:\n%s", n, len(wantHarnessCmds)-1, body)
	}
	if n := strings.Count(body, "aria-pressed=\"true\""); n != 1 {
		t.Errorf("%d pressed tabs, want 1 in:\n%s", n, body)
	}
	if n := strings.Count(body, "aria-pressed=\"false\""); n != len(wantHarnessCmds)-1 {
		t.Errorf("%d unpressed tabs, want %d in:\n%s", n, len(wantHarnessCmds)-1, body)
	}
	if strings.Contains(visible, "--dest") {
		t.Errorf("the default command carries --dest: %q", visible)
	}
}

// The two sentences that replaced the old --dest paragraph: the default tab names the shared
// folder, and the project note names the repo-folder form of the same pipe.
func TestRenderCarriesTheDefaultTabSentenceAndProjectNote(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	for _, want := range []string{
		"The first tab is the shared folder: with no <code>--dest</code> the installer writes to",
		"<code>$HOME/.agents/skills/sandbin-preview</code>",
		"also read <code>.agents/skills</code>",
		"Claude Code reads <code>.claude/skills</code>",
		"the current project instead",
		"<code>| bash -s -- --dest .agents/skills</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	// The paragraph the tabs replaced is gone, and the tabs only switch which command is
	// visible: every command stays plain selectable text.
	for _, gone := range []string{
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

// The tab row and the command blocks both come from the slice the handler hands over: a
// different list renders a different page, so the markup cannot drift from the table.
func TestRenderTabsFollowTheHarnessSlice(t *testing.T) {
	var buf bytes.Buffer
	d := sampleData()
	d.Harnesses = []Harness{
		{Label: "Agents", Cmd: "curl -fsSL " + baseToken + "/skills/sandbin-preview/install.sh | bash"},
		{Label: "Odd One", Cmd: "curl -fsSL " + baseToken + "/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.odd/skills\""},
	}
	if err := Render(&buf, d); err != nil {
		t.Fatalf("Render(two harnesses): %v", err)
	}
	body := buf.String()

	if n := strings.Count(body, "class=\"tab\""); n != 2 {
		t.Errorf("%d tab buttons, want 2 in:\n%s", n, body)
	}
	if n := strings.Count(body, "<pre class=\"cmd-body"); n != 2 {
		t.Errorf("%d command blocks, want 2 in:\n%s", n, body)
	}
	for _, want := range []string{
		">Agents</button>",
		">Odd One</button>",
		"<pre class=\"cmd-body hidden\" id=\"cmd-1\">curl -fsSL https://preview.example.org:8443/skills/sandbin-preview/install.sh | bash -s -- --dest \"$HOME/.odd/skills\"</pre>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	// What a slice cannot bring: the rows of the default table are gone and no token survives.
	// The sentences front.html carries itself stay, because they are the page's own prose and
	// not derived from the slice.
	for _, gone := range []string{"Gemini CLI", "Windsurf", "{{", "}}"} {
		if strings.Contains(body, gone) {
			t.Errorf("the two-row render still carries %q in:\n%s", gone, body)
		}
	}
}

// With the default list the page is finished markup: no template token of any kind survives,
// and no command names a path bash would hand over literally.
func TestRenderWithDefaultsLeavesNoTokensOrTildes(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleData()); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := buf.String()

	for _, gone := range []string{"{{", "}}", "~/", "baseToken"} {
		if strings.Contains(body, gone) {
			t.Errorf("the rendered page carries %q in:\n%s", gone, body)
		}
	}
	if n := strings.Count(body, "\"$HOME/"); n != len(wantHarnessCmds)-1 {
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
	for _, gone := range []string{"{{", "}}", "<nil>"} {
		if strings.Contains(body, gone) {
			t.Errorf("empty render left %q in the page:\n%s", gone, body)
		}
	}
	// The static page is still the static page: its markup and script are untouched by the
	// fields being empty, and an empty harness list simply renders no tabs.
	for _, want := range []string{
		"<!doctype html>",
		"<html lang=\"en\">",
		"<title>Sandbin</title>",
		"<h1 id=\"sandbin\">Sandbin</h1>",
		"<header class=\"page-head\">",
		"<nav><a href=\"/docs\">docs</a>",
		"function sandbinCopyInstall(button) {",
		"function sandbinSelectTab(button) {",
		"</body>\n</html>\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty render missing %q in:\n%s", want, body)
		}
	}
	if n := strings.Count(body, "<pre class=\"cmd-body"); n != 0 {
		t.Errorf("an empty harness list rendered %d command blocks, want 0:\n%s", n, body)
	}
	// An empty BrandCSS leaves the template's own newline between the tags.
	if n := strings.Count(body, "<style>\n\n</style>"); n != 1 {
		t.Errorf("empty render has %d empty <style> pairs, want 1:\n%s", n, body)
	}
}

// hiddenSuffix spells out what the template composes for a block of the given index, so the
// pins above are plain strings rather than a second template.
func hiddenSuffix(i int) string {
	if i == 0 {
		return ""
	}
	return " hidden"
}
