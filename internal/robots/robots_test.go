package robots

import (
	"strings"
	"testing"
)

// Text is the whole file the server answers /robots.txt with: the two lines
// docs/decisions/0022-robots-txt.md settles, final newline included and nothing else.
func TestTextIsTheExactTwoLines(t *testing.T) {
	const want = "User-agent: *\nDisallow: /\n"
	if Text != want {
		t.Errorf("Text = %q, want %q", Text, want)
	}
	if !strings.HasPrefix(Text, "User-agent: *") {
		t.Errorf("Text = %q does not start with the User-agent line a crawler reads first", Text)
	}
}

// The bytes are served verbatim, so the shapes that would silently change what a crawler reads
// must be absent: a BOM, CRLF line endings, a template token, a comment line, a blank line, and
// a missing or doubled final newline.
func TestTextIsPlainStaticBytes(t *testing.T) {
	if strings.HasPrefix(Text, "\ufeff") {
		t.Error("Text starts with a byte order mark")
	}
	if strings.Contains(Text, "\r") {
		t.Error("Text contains a carriage return: the file must use plain LF")
	}
	if strings.Contains(Text, "{{") {
		t.Error("Text contains a template token: the file is served verbatim")
	}
	lines := strings.Split(strings.TrimSuffix(Text, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("Text holds %d lines, want 2: %q", len(lines), Text)
	}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			t.Errorf("Text holds the blank or commented line %q", line)
		}
	}
}
