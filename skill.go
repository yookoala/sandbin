// Package sandbin carries the module-root files the cmd programs embed.
package sandbin

import _ "embed"

// PreviewSkillMD is the agent skill file served at /skills/sandbin-preview/SKILL.md.
//
//go:embed skills/sandbin-preview/SKILL.md
var PreviewSkillMD string
