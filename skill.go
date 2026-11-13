// Package sandbin carries the module-root files the cmd programs embed.
package sandbin

import _ "embed"

// PreviewSkillMD is the agent skill file served at /skills/sandbin-preview/SKILL.md.
//
//go:embed skills/sandbin-preview/SKILL.md
var PreviewSkillMD string

// InstallScript is the one-liner installer template served at
// /skills/sandbin-preview/install.sh; sandbin replaces __SANDBIN_BASE__ with the
// address the request arrived at before writing it out.
//
//go:embed skills/sandbin-preview/install.sh
var InstallScript string
