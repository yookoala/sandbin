// Package sandbin carries the module-root files the cmd programs embed.
package sandbin

import "embed"

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

// UsageDocs is the usage documentation tree the server renders under /docs: the ten
// docs/usages/*.md files travel inside the binary, so a running server never reads docs/
// from disk and a container image needs no docs volume.
//
//go:embed docs/usages/*.md
var UsageDocs embed.FS

// LogoSVG is the markup of docs/assets/sandbin-logo.svg. The landing page and the hosted
// docs pages inline it straight into the HTML rather than loading it through an <img>,
// because an image fetched that way cannot load the web font its own <style> imports.
//
//go:embed docs/assets/sandbin-logo.svg
var LogoSVG string
