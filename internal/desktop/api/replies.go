package api

import "time"

// The fields of a type that replaced a map are in the order of their keys,
// as the map wrote them, so the bytes on the wire did not change.

// AgentAdded answers POST /api/agents: the new agent's id, and why its
// first message did not go when it did not.
type AgentAdded struct {
	Error string `json:"error,omitempty"`
	ID    string `json:"id"`
}

// ProjectAdded answers POST /api/projects: the project the directory
// belongs to, and whether it was new to the deck.
type ProjectAdded struct {
	Added bool   `json:"added"`
	Name  string `json:"name"`
	Path  string `json:"path"`
}

// Suggestion is a directory to add as a project (GET /api/projects/suggest).
type Suggestion struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Short string `json:"short"`
	Used  string `json:"used"`
	Added bool   `json:"added"`
}

// Session is a past session of a project to go on with (GET /api/history).
type Session struct {
	Cwd     string    `json:"cwd"`
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Origin  string    `json:"origin"` // cli | desktop
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
}

// FileEntry is a file or directory that matches what is typed (GET
// /api/files).
type FileEntry struct {
	Path string `json:"path"` // from the directory, "/"-separated
	Dir  bool   `json:"dir,omitempty"`
}

// DiffFailed answers GET /api/diff when the directory has no diff to give.
type DiffFailed struct {
	Dir   string `json:"dir"`
	Error string `json:"error"`
}

// FinishAction is an entry of an agent's finish menu.
type FinishAction struct {
	Name string `json:"name"`
	// Kind is a built-in action's kind, for the page to name it in its
	// own words: push, pr, rebase, merge; empty for the user's own.
	Kind string `json:"kind,omitempty"`
}

// FinishMenu answers GET /api/finish: the checkout's branch, the one it
// would go into, and the actions offered.
type FinishMenu struct {
	Actions []FinishAction `json:"actions"`
	Base    string         `json:"base"`
	Branch  string         `json:"branch"`
}

// FinishFailed answers /api/finish when the checkout can't be read.
type FinishFailed struct {
	Actions []FinishAction `json:"actions"` // always empty
	Error   string         `json:"error"`
}

// FinishResult answers POST /api/finish: how the action went, the end of
// what it printed, and its error when it failed.
type FinishResult struct {
	Error  string `json:"error,omitempty"`
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

// PushState answers GET /api/push: mad's key, and whether the browser
// asking is subscribed.
type PushState struct {
	Key string `json:"key"`
	On  bool   `json:"on"`
}

// PushOn answers POST and DELETE /api/push: whether the browser is
// subscribed now.
type PushOn struct {
	On bool `json:"on"`
}

// PushSent answers POST /api/push/test.
type PushSent struct {
	Sent bool `json:"sent"`
}

// Paired answers POST /api/pair: the device's id and the Mac's name.
type Paired struct {
	ID      string `json:"id"`
	Machine string `json:"machine"`
}

// PhoneDevice is a paired phone (GET /api/phone/devices).
type PhoneDevice struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

// TeamStarted answers POST /api/team {op: start}: the team, its channel,
// and what needs the person from its start.
type TeamStarted struct {
	Channel string   `json:"channel"`
	Name    string   `json:"name"`
	Notes   []string `json:"notes"`
}

// TeamSlept answers POST /api/team {op: sleep}: how many agents went to
// sleep.
type TeamSlept struct {
	Slept int `json:"slept"`
}

// TeamTemplates answers GET /api/team/templates: the templates a team can
// follow, the agents a role can be played by, the permission levels and
// the limit on teams; Error says why the templates could not be read.
type TeamTemplates struct {
	Templates   []TeamTemplate `json:"templates"`
	Choices     []TeamChoice   `json:"choices"`
	Permissions []string       `json:"permissions"`
	MaxTeams    int            `json:"max_teams"`
	Error       string         `json:"error,omitempty"`
}

type TeamTemplate struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Source      string     `json:"source"`
	Roles       []TeamRole `json:"roles"`
}

type TeamRole struct {
	Name     string `json:"name"`
	Agent    string `json:"agent"` // claude:opus, codex@high
	Lead     bool   `json:"lead,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Brief    string `json:"brief,omitempty"` // its first line
	Skill    string `json:"skill,omitempty"`
}

type TeamChoice struct {
	Agent   string   `json:"agent"` // claude:opus, codex, codex:gpt-6-sol
	Efforts []string `json:"efforts"`
}

// Hello answers GET /api/hello?nonce=: an HMAC-SHA256 of "mad-hello:"
// and the nonce, keyed by the master token, in hex. A client that holds
// the token checks it before sending the token anywhere, so a process
// squatting on mad's port never sees it.
type Hello struct {
	Proof string `json:"proof"`
}

// WindowCode answers POST /api/window/code: a code a new window trades
// once, within a minute, for the token (POST /api/window/token), so the
// token itself is never on a browser's command line.
type WindowCode struct {
	Code string `json:"code"`
}

// WindowToken answers POST /api/window/token: the token the code was for.
type WindowToken struct {
	Token string `json:"token"`
}
