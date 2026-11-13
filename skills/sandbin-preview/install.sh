#!/usr/bin/env bash
# sandbin-preview skill installer. sandbin replaces __SANDBIN_BASE__ below with the
# address this script was served from, so SKILL.md is fetched from the same server
# that handed you this script. Run it as a file or piped:
#   curl -fsSL https://your.sandbin/skills/sandbin-preview/install.sh | bash
#   curl -fsSL https://your.sandbin/skills/sandbin-preview/install.sh | bash -s -- --dest "$HOME/.claude/skills"
set -euo pipefail

BASE="__SANDBIN_BASE__"
DEST="$HOME/.agents/skills"

while [ $# -gt 0 ]; do
  case "$1" in
    -d|--dest) DEST="${2:?--dest needs a value}"; shift 2 ;;
    -h|--help) echo "usage: install.sh [-d|--dest SKILLS_DIR]  (default \$HOME/.agents/skills)"; exit 0 ;;
    *) echo "sandbin-installer: unknown argument $1" >&2; exit 1 ;;
  esac
done

mkdir -p "$DEST/sandbin-preview"
curl -fsSL "$BASE/skills/sandbin-preview/SKILL.md" -o "$DEST/sandbin-preview/SKILL.md"
echo "installed $DEST/sandbin-preview/SKILL.md (from $BASE)"
