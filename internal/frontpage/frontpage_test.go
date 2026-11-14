package frontpage

import (
	"bytes"
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
// TLS socket or a trusted proxy.
func sampleData() Data {
	return Data{
		Base:     "https://preview.example.org:8443",
		Logo:     sampleLogo,
		BrandCSS: sampleCSS,
	}
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
		// Base lands inside the one install command line, whole.
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

// The base arrives in the install command as one unbroken string: the placeholder frontBase
// falls back to for an odd Host is rendered like any other base.
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
		if n := strings.Count(got, "curl -fsSL "); n != 1 {
			t.Errorf("base %q: %d install commands, want 1", base, n)
		}
	}
}

// The page is static apart from its three fields: two renders of the same data are the same
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
	// three fields being empty.
	for _, want := range []string{
		"<!doctype html>",
		"<html lang=\"en\">",
		"<title>Sandbin</title>",
		"<h1 id=\"sandbin\">Sandbin</h1>",
		"<header class=\"page-head\">",
		"<nav><a href=\"/docs\">docs</a>",
		"function sandbinCopyInstall(button) {",
		"</body>\n</html>\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("empty render missing %q in:\n%s", want, body)
		}
	}
	// An empty BrandCSS leaves the template's own newline between the tags.
	if n := strings.Count(body, "<style>\n\n</style>"); n != 1 {
		t.Errorf("empty render has %d empty <style> pairs, want 1:\n%s", n, body)
	}
}
