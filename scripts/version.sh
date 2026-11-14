#!/bin/sh
# version.sh prints the version string for the git state of this checkout, the way
# docs/decisions/0017-version-stamping.md settles it:
#
#   exact tag on HEAD        -> v0.3.0 (1abcdef)
#   a few commits past a tag -> v0.3.0+ (2abcdef)
#   uncommitted tracked work -> dev (3abcdef+)
#   no reachable v* tag      -> dev (4abcdef)
#
# Run it from the repository root. It writes nothing, prints exactly one line, and exits 0.

short=$(git rev-parse --short=7 HEAD)

# Dirty counts tracked changes only; untracked files are not part of a commit and do not count.
if ! git diff HEAD --quiet; then
	printf 'dev (%s+)\n' "$short"
	exit 0
fi

if tag=$(git describe --tags --exact-match --match 'v*' 2>/dev/null); then
	printf '%s (%s)\n' "$tag" "$short"
	exit 0
fi

if describe=$(git describe --tags --match 'v*' 2>/dev/null); then
	# describe appends -<commits>-g<hash> to the nearest tag. Only that tail is stripped, so a
	# prerelease tag keeps its own text: v1.0.0-beta.1-3-g1abcdef becomes v1.0.0-beta.1+.
	tag=$(printf '%s' "$describe" | sed -E 's/-[0-9]+-g[0-9a-f]+$//')
	printf '%s+ (%s)\n' "$tag" "$short"
	exit 0
fi

# A fresh or shallow clone has no tag to name, but it does know its commit.
printf 'dev (%s)\n' "$short"
