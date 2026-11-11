// Package api is the JSON the desktop server (package desktop) answers
// with: the shapes the page, the native apps and the fixture server read.
// It holds types only, and imports only the standard library, so anything
// can take it in to check a body against the server's own types.
//
// The deck snapshot itself (desktop.Snapshot and the Project, Team and
// Agent it nests) is not here: it nests types of packages bus, attention,
// status and queue, which carry behaviour this package must not take in.
package api

// Version is the API's version, sent as VersionHeader on every /api/
// answer. It goes up only on a breaking change: a field removed, renamed
// or retyped, or a meaning changed. A field added is not one, and leaves
// it as it is, since every client ignores what it does not know.
const Version = 1

// VersionHeader carries Version.
const VersionHeader = "X-Mad-API"

// OK is the answer of an act that has nothing more to say.
type OK struct {
	OK bool `json:"ok"`
}

// WindowState says whether a window is open (GET /api/window/state).
type WindowState struct {
	Open bool `json:"open"`
}
