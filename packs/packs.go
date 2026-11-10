// Package packs holds the packs mad comes with: ways of working kept as
// files (flows, team templates, the skills their roles work by), apart
// from the code that runs them. Package pack reads them; one in
// ~/.config/mad/packs/ or a project's .mad/packs/ of the same name takes
// a built-in one's place, so a pack changes without mad changing.
package packs

import "embed"

// FS has a directory per pack.
//
//go:embed mad pstack
var FS embed.FS

// Names are the packs in FS, in the order their flows and templates are
// listed, a pack's by name: the first flow and the first template of the
// first pack are the defaults.
var Names = []string{"mad", "pstack"}
