// Package skills holds the agent skills mad comes with; `mad skill
// install` puts them where claude finds them.
package skills

import "embed"

// FS has a directory per skill, with its SKILL.md.
//
//go:embed mad-flow
var FS embed.FS

// Names are the skills in FS.
var Names = []string{"mad-flow"}
