// Package sandbin carries the module-root files the cmd programs embed.
package sandbin

import _ "embed"

// PreviewSkillMD is the agent skill template served at /skills/sandbin-preview/SKILL.md;
// sandbin renders its {{.Base}} with the address the request arrived at, so every
// served copy names the exact server the visitor downloaded it from.
//
//go:embed skills/sandbin-preview/SKILL.md
var PreviewSkillMD string

// InstallScript is the one-liner installer template served at
// /skills/sandbin-preview/install.sh; sandbin renders its {{.Base}} with the address
// the request arrived at before writing it out.
//
//go:embed skills/sandbin-preview/install.sh
var InstallScript string
