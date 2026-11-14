// Package version resolves the one-line version string both commands print at --version.
// Sources are tried in the order docs/decisions/0017-version-stamping.md settles: an injected
// value, Go's own VCS stamping, the tag scripts/release.sh embedded, the module version, then
// plain dev.
package version

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

// Value is the -ldflags -X target: the Makefile, the Containerfile and any manual build stamp
// a whole version string into it, and that string then wins over everything this package can
// work out by itself.
var Value string

// releaseTag is what scripts/release.sh wrote into release.txt at release time: a tag alone,
// no commit hash. It travels inside the module zip, which is how a proxy-installed binary can
// name its version at all.
//
//go:embed release.txt
var releaseTag string

// shortHashLen is how many characters of a revision the dev formats carry, matching the
// git rev-parse --short=7 the Makefile uses.
const shortHashLen = 7

// String reports the version string of the running binary.
func String() string {
	var modVersion, revision string
	var haveVcs, modified bool
	if info, ok := debug.ReadBuildInfo(); ok && info != nil {
		modVersion = info.Main.Version
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
				haveVcs = true
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	return format(Value, strings.TrimSpace(releaseTag), modVersion, revision, haveVcs, modified)
}

// format is the whole decision table as a pure function, so every precedence rule and every
// git-state string is testable without building anything: the first source that knows something
// decides.
func format(value, embeddedTag, modVersion, revision string, haveVcs, modified bool) string {
	// (1) Injection is verbatim: a build step that could run git describe already spelled the
	// whole string, so nothing here reinterprets it.
	if value != "" {
		return value
	}

	// (2) Go stamps vcs.revision and vcs.modified into any build made from a git checkout, so
	// a repo build knows its commit even when nobody stamped it. It cannot know the tag, so it
	// says dev rather than claiming a release it has not seen.
	if haveVcs && revision != "" {
		short := revision
		if len(short) > shortHashLen {
			short = short[:shortHashLen]
		}
		if modified {
			return "dev (" + short + "+)"
		}
		return "dev (" + short + ")"
	}

	// (3) A module zip has no .git and no way to take -ldflags, so the embedded tag is all it
	// honestly knows: the tag alone, which is exact because the zip is the tagged commit.
	if embeddedTag != "" {
		return embeddedTag
	}

	// (4) The build info's module version is the backstop for an empty release.txt. A main
	// package built from a checkout reports "(devel)", which is not a version to report.
	if modVersion != "" && modVersion != "(devel)" {
		return modVersion
	}

	// (5) Nothing known.
	return "dev"
}
