package version

import (
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

// The resolver's whole job is picking the first known source and shaping the string for the
// git state a build was made in. Every row below is a state a build can actually be in,
// spelled the way docs/decisions/0017-version-stamping.md spells it.
func TestFormatPrecedenceAndFormats(t *testing.T) {
	cases := []struct {
		name        string
		value       string
		embeddedTag string
		modVersion  string
		revision    string
		haveVcs     bool
		modified    bool
		want        string
	}{
		// (1) an injected value is used verbatim and beats every other source.
		{
			name:  "injected exact-tag value",
			value: "v0.3.0 (1abcdef)",
			want:  "v0.3.0 (1abcdef)",
		},
		{
			name:        "injected beats vcs, the embedded tag and the module version",
			value:       "v9.9.9+ (2abcdef)",
			embeddedTag: "v0.3.0",
			modVersion:  "v1.0.0",
			revision:    "3456789012abcdef",
			haveVcs:     true,
			modified:    true,
			want:        "v9.9.9+ (2abcdef)",
		},
		{
			name:  "injected text is not reinterpreted",
			value: "custom (x)",
			want:  "custom (x)",
		},
		{
			name:  "an injected prerelease stays verbatim",
			value: "v1.0.0-beta.1 (1abcdef)",
			want:  "v1.0.0-beta.1 (1abcdef)",
		},
		{
			name:  "an injected dev format stays verbatim",
			value: "dev (3abcdef+)",
			want:  "dev (3abcdef+)",
		},

		// (2) Go's own VCS stamping: a repo build with no injection reports dev plus the
		// short revision, and a + on top when tracked files are dirty.
		{
			name:     "vcs clean",
			revision: "abcdef1234567890",
			haveVcs:  true,
			want:     "dev (abcdef1)",
		},
		{
			name:     "vcs dirty",
			revision: "abcdef1234567890",
			haveVcs:  true,
			modified: true,
			want:     "dev (abcdef1+)",
		},
		{
			name:     "a full 40-hex revision is cut to seven",
			revision: "0123456789abcdef0123456789abcdef01234567",
			haveVcs:  true,
			want:     "dev (0123456)",
		},
		{
			name:        "vcs outranks the embedded tag",
			embeddedTag: "v0.3.0",
			modVersion:  "v9.9.9",
			revision:    "abcdef1234567890",
			haveVcs:     true,
			want:        "dev (abcdef1)",
		},
		{
			name:        "a dirty tree outranks the embedded tag",
			embeddedTag: "v0.3.0",
			revision:    "abcdef1234567890",
			haveVcs:     true,
			modified:    true,
			want:        "dev (abcdef1+)",
		},

		// (3) the tag the release script embedded: a proxy-downloaded build knows nothing else.
		{
			name:        "embedded tag alone",
			embeddedTag: "v0.3.0",
			want:        "v0.3.0",
		},
		{
			name:        "embedded prerelease tag verbatim",
			embeddedTag: "v1.0.0-beta.2",
			want:        "v1.0.0-beta.2",
		},
		{
			name:        "the embedded tag outranks the module version",
			embeddedTag: "v0.3.0",
			modVersion:  "v9.9.9",
			want:        "v0.3.0",
		},

		// (4) the module version from the build info, the backstop for an empty embedded file.
		{
			name:       "module version alone",
			modVersion: "v9.9.9",
			want:       "v9.9.9",
		},

		// (5) nothing known.
		{
			name: "nothing known",
			want: "dev",
		},

		// Degenerate inputs stay safe: "(devel)" and an empty revision are not versions to
		// report, and no row may produce an empty pair of parentheses.
		{
			name:       "the devel module version is unknown",
			modVersion: "(devel)",
			want:       "dev",
		},
		{
			name:        "an empty revision with a tag falls to the tag",
			embeddedTag: "v0.3.0",
			haveVcs:     true,
			modified:    true,
			want:        "v0.3.0",
		},
		{
			name:       "an empty revision with a module version falls to it",
			modVersion: "v9.9.9",
			haveVcs:    true,
			modified:   true,
			want:       "v9.9.9",
		},
		{
			name:     "an empty revision alone stays plain dev",
			haveVcs:  true,
			modified: true,
			want:     "dev",
		},
		{
			name:     "a revision shorter than seven is kept whole",
			revision: "abc12",
			haveVcs:  true,
			modified: true,
			want:     "dev (abc12+)",
		},
		{
			name:     "a revision that is not a hash is reported as given",
			revision: "not-a-hash-at-all",
			haveVcs:  true,
			want:     "dev (not-a-h)",
		},
		{
			name:     "an empty injected value falls through",
			revision: "abcdef1234567890",
			haveVcs:  true,
			want:     "dev (abcdef1)",
		},
		{
			name:        "an injected value with spaces and parentheses survives",
			value:       "v0.3.0+ (2abcdef)",
			embeddedTag: "v0.3.0",
			want:        "v0.3.0+ (2abcdef)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := format(tc.value, tc.embeddedTag, tc.modVersion, tc.revision, tc.haveVcs, tc.modified)
			if got != tc.want {
				t.Errorf("format(%q, %q, %q, %q, %v, %v) = %q, want %q",
					tc.value, tc.embeddedTag, tc.modVersion, tc.revision, tc.haveVcs, tc.modified, got, tc.want)
			}
			if strings.TrimSpace(got) == "" {
				t.Errorf("format(%q, %q, %q, %q, %v, %v) = %q, want a non-empty string",
					tc.value, tc.embeddedTag, tc.modVersion, tc.revision, tc.haveVcs, tc.modified, got)
			}
		})
	}
}

// TestFormatParenthesesAreBalanced checks what the dev formats share: the hash always sits
// inside one pair of parentheses closed at the end, so a reader can always tell a stamped
// build from a stamp-less one.
func TestFormatParenthesesAreBalanced(t *testing.T) {
	for _, got := range []string{
		format("", "", "", "abcdef1234567890", true, false),
		format("", "", "", "abcdef1234567890", true, true),
		format("", "v0.3.0", "", "", false, false),
		format("", "", "", "", false, false),
	} {
		if strings.Count(got, "(") != strings.Count(got, ")") {
			t.Errorf("format output %q has unbalanced parentheses", got)
		}
		if strings.Contains(got, "(") && !strings.HasSuffix(got, ")") {
			t.Errorf("format output %q should close its parentheses at the end", got)
		}
	}
}

var devFormat = regexp.MustCompile(`^dev \([0-9a-f]{7}\+?\)$`)

// TestStringOnThisBinary reads this test binary's own build info and asserts String() matches
// it: a build from a git checkout reports the dev format carrying this checkout's short
// revision, a build outside version control reports plain dev. The expected hash comes from
// the build info, never from a value written down in this file.
func TestStringOnThisBinary(t *testing.T) {
	got := String()

	// A build can inject Value with -ldflags; then the resolver must echo it verbatim, which is
	// what makes the Makefile's stamped builds and a `go test -ldflags -X ...` run alike.
	if Value != "" {
		if got != Value {
			t.Errorf("String() = %q with Value injected as %q, want the injected value verbatim", got, Value)
		}
		return
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		t.Skip("this test binary carries no build info")
	}

	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}

	if revision == "" {
		if got != "dev" {
			t.Errorf("String() = %q in a build without VCS stamping, want %q", got, "dev")
		}
		return
	}

	want := "dev (" + revision[:7] + ")"
	if modified == "true" {
		want += "+"
	}
	if got != want {
		t.Errorf("String() = %q, want %q for this checkout", got, want)
	}
	if !devFormat.MatchString(got) {
		t.Errorf("String() = %q, want it to match %s", got, devFormat)
	}
}

// TestEmbeddedReleaseFileIsTheRawTagFile guards the release script's contract: whatever
// release.txt holds is reported as it is, with no hash and no decoration added, and go:embed
// really read the same bytes the file on disk has.
func TestEmbeddedReleaseFileIsTheRawTagFile(t *testing.T) {
	onDisk, err := os.ReadFile("release.txt")
	if err != nil {
		t.Fatalf("read release.txt: %v", err)
	}
	if releaseTag != string(onDisk) {
		t.Errorf("embedded release.txt = %q, want the file's bytes %q", releaseTag, string(onDisk))
	}
	if strings.Contains(releaseTag, "(") || strings.Contains(releaseTag, ")") {
		t.Errorf("release.txt = %q, want a tag only, with no hash or parentheses", releaseTag)
	}
	if got := format("", releaseTag, "", "", false, false); releaseTag != "" && got != releaseTag {
		t.Errorf("the embedded tag %q resolved to %q, want the tag itself", releaseTag, got)
	}
	if releaseTag == "" && String() == "" {
		t.Errorf("String() with an empty release.txt must still name a version")
	}
}
