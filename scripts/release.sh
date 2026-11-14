#!/bin/sh
# release.sh records one release tag in this repository: it writes the tag into the file
# internal/version embeds, commits that change, and annotates the commit with the tag.
#
# It never touches the network. The two commands it prints at the end are what a human runs to
# publish the release; this script does not run them, and contains no push, fetch or upload of
# its own.
#
# Refusals (missing or malformed TAG, an existing tag, uncommitted tracked changes) exit 1
# before anything is written, so a refused release leaves no trace.

tag=$1

if [ -z "$tag" ]; then
	echo "release: usage: scripts/release.sh vX.Y.Z" >&2
	exit 1
fi

# A tag is vMAJOR.MINOR.PATCH, optionally followed by a dotted prerelease tail.
if ! printf '%s' "$tag" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'; then
	echo "release: $tag is not a tag: use vMAJOR.MINOR.PATCH, optionally with a -prerelease tail" >&2
	exit 1
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	echo "release: not inside a git repository" >&2
	exit 1
fi

# The embedded file lives at the root, so the script runs there.
if [ -n "$(git rev-parse --show-prefix)" ]; then
	echo "release: run scripts/release.sh from the repository root" >&2
	exit 1
fi

# The tag has to point at recorded work, not at a half-finished tree.
if ! git diff HEAD --quiet; then
	echo "release: the tree has uncommitted tracked changes; commit or revert them first" >&2
	exit 1
fi

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	echo "release: tag $tag already exists" >&2
	exit 1
fi

printf '%s\n' "$tag" >internal/version/release.txt
git add internal/version/release.txt
git commit -m "chore: release $tag"
git tag -a "$tag" -m "$tag"

echo "release: embedded $tag in internal/version/release.txt"
echo "release: committed and annotated $tag at $(git rev-parse --short=7 HEAD)"
echo "release: nothing was pushed; to publish it yourself run:"
echo "  git push"
echo "  git push origin $tag"
