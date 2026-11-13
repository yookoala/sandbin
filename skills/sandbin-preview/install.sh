#!/usr/bin/env bash
# sandbin-preview skill installer. sandbin renders this text template, filling
# {{.Base}} with the address this script was served from, so SKILL.md is fetched from
# the same server that handed you this script. When go is on PATH the installer also
# builds the client binary into the skill folder; without go (or if the install
# fails) the skill builds it on first use instead. Run it as a file or piped:
#   curl -fsSL https://your.sandbin/skills/sandbin-preview/install.sh | bash
#   curl -fsSL https://your.sandbin/skills/sandbin-preview/install.sh | bash -s -- --dest "$HOME/.claude/skills"
set -euo pipefail

BASE="{{.Base}}"
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

if command -v go >/dev/null 2>&1; then
  if GOBIN="$DEST/sandbin-preview" go install github.com/yookoala/sandbin/cmd/sandbin-client@latest; then
    echo "installed $DEST/sandbin-preview/sandbin-client (go install)"
  else
    echo "sandbin-installer: go install failed; the skill will build the client on first use" >&2
  fi
else
  echo "sandbin-installer: no go on PATH; the skill will build the client on first use" >&2
fi
