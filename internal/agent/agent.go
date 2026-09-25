// Package agent describes the kinds of coding agents mad can run and how
// to launch or resume each of them.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// Kind describes how to launch one kind of coding agent. Command templates
// are shell strings with these placeholders:
//
//	{id}              the agent's UUID
//	{sid}             the agent's current session id (falls back to {id})
//
// and those agents' wiring fills (see Launch.Vars): {claude_settings},
// the path to mad's claude settings (status hooks), and {codex_notify},
// `-c notify=[...]` pointing codex's notify at mad.
//
// Kinds can be overridden or added via ~/.config/mad/agents.json, a JSON
// array of Kind.
type Kind struct {
	Name string `json:"name"`
	// Icon stands for the kind in the sidebar: a glyph or two, as the
	// agent draws itself (✻, >_). Kinds without one show the first letter
	// of their name. Color is the icon's, an xterm-256 number or #rrggbb;
	// without one it is drawn like other text.
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	// Start launches a fresh session.
	Start string `json:"start"`
	// Resume reopens session {sid}; used when a session id is known.
	Resume string `json:"resume,omitempty"`
	// ResumeLatest reopens the most recent session when no id is known.
	ResumeLatest string `json:"resume_latest,omitempty"`
	// Fork is appended to Resume to continue a session in a copy of its
	// own, for one still open outside the deck. Kinds without it only
	// take over sessions nothing else has open.
	Fork string `json:"fork,omitempty"`
	// Waiting are regexes matched against the bottom of the screen to
	// detect "needs your input" when no hook says so.
	Waiting []string `json:"waiting,omitempty"`
	// Hooks means the agent reports running/idle/waiting through `mad hook`.
	Hooks bool `json:"hooks,omitempty"`

	waitingRe []*regexp.Regexp
}

var builtin = []Kind{
	{
		Name:    "claude",
		Icon:    "✻",
		Color:   "173", // claude's orange
		Start:   "claude --settings {claude_settings} --session-id {id}",
		Resume:  "claude --settings {claude_settings} --resume {sid}",
		Fork:    "--fork-session",
		Waiting: []string{`Do you want to`, `❯ 1\. Yes`, `trust this folder`, `Enter to confirm`},
		Hooks:   true,
	},
	{
		Name:         "codex",
		Icon:         ">_",
		Color:        "252",
		Start:        "codex {codex_notify}",
		Resume:       "codex resume {codex_notify} {sid}",
		ResumeLatest: "codex resume {codex_notify} --last",
		Waiting:      []string{`Would you like to`, `Allow command`, `Approve`},
	},
	{
		// pi creates the session on first use, so start == resume.
		Name:  "pi",
		Icon:  "π",
		Color: "111",
		Start: "pi --session-id {id}",
	},
	{
		Name:  "shell",
		Icon:  "$",
		Color: "114",
		Start: `exec "${SHELL:-/bin/zsh}" -l`,
	},
}

// Builtin returns the kinds mad knows without any configuration.
func Builtin() []Kind {
	kinds, _ := compile(clone(builtin))
	return kinds
}

// clone copies kinds deep enough to decode into: a slice left shared
// would be overwritten in place.
func clone(kinds []Kind) []Kind {
	out := append([]Kind(nil), kinds...)
	for i := range out {
		out[i].Waiting = append([]string(nil), out[i].Waiting...)
	}
	return out
}

// Load returns the built-in kinds merged with agents.json. An entry with a
// built-in name changes the fields it sets and keeps the others ("fork":
// "" drops one); others are appended. What can't be used is left out and
// reported in the error, the rest still loads. A file that isn't a JSON
// array at all gives nil and the error, for the caller to keep what it
// had (or take Builtin).
func Load() ([]Kind, error) {
	kinds := clone(builtin)
	var entries []json.RawMessage
	if err := paths.ReadJSON(paths.AgentsConfig(), &entries); err != nil {
		return nil, err
	}
	var errs []error
	for n, raw := range entries {
		var named struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &named) != nil || named.Name == "" {
			errs = append(errs, fmt.Errorf("agents.json: entry %d has no name", n+1))
			continue
		}
		i := 0
		for i < len(kinds) && kinds[i].Name != named.Name {
			i++
		}
		k := Kind{}
		if i < len(kinds) {
			k = clone(kinds[i : i+1])[0] // a bad entry must not half-change it
		}
		if err := json.Unmarshal(raw, &k); err != nil {
			errs = append(errs, fmt.Errorf("agents.json: %s: %v", named.Name, strings.TrimPrefix(err.Error(), "json: ")))
			continue
		}
		if i < len(kinds) {
			kinds[i] = k
		} else {
			kinds = append(kinds, k)
		}
	}
	kinds, err := compile(kinds)
	return kinds, errors.Join(append(errs, err)...)
}

// compile readies the waiting patterns; one that doesn't compile is
// skipped and reported.
func compile(kinds []Kind) ([]Kind, error) {
	var errs []error
	for i := range kinds {
		kinds[i].waitingRe = nil
		for _, w := range kinds[i].Waiting {
			re, err := regexp.Compile(w)
			if err != nil {
				errs = append(errs, fmt.Errorf("agents.json: %s: waiting %q: %v", kinds[i].Name, w, err))
				continue
			}
			kinds[i].waitingRe = append(kinds[i].waitingRe, re)
		}
	}
	return kinds, errors.Join(errs...)
}

// ByName finds a kind; unknown names run as a command of that name.
func ByName(kinds []Kind, name string) Kind {
	for _, k := range kinds {
		if k.Name == name {
			return k
		}
	}
	return Kind{Name: name, Start: name}
}

// Glyph is the kind's icon, or the first letter of its name.
func (k Kind) Glyph() string {
	if k.Icon != "" {
		return k.Icon
	}
	for _, r := range k.Name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// Launch is what building a command needs to know from outside the kind.
type Launch struct {
	// Exists tells whether a session was ever written, for kinds whose
	// sessions mad can look up; nil otherwise. It matters to kinds mad
	// names the session of ({id} in Start): the session is the agent's
	// id until it reports another, and one that never got a message
	// can't be resumed (claude's --resume fails).
	Exists func(sid string) bool
	// Vars are the placeholders agents' wiring fills ({claude_settings},
	// {codex_notify}) and their shell text. Any kind may use any of them.
	Vars map[string]string
}

// Command builds the shell command for a, resuming its previous session
// when resume is set and one can be found.
func (k Kind) Command(a *state.Agent, resume bool, l Launch) string {
	sid := a.SessionID
	tpl := k.Start
	if resume {
		switch {
		case l.Exists != nil && strings.Contains(k.Start, "{id}"):
			if sid == "" {
				sid = a.ID
			}
			if k.Resume != "" && l.Exists(sid) {
				tpl = k.Resume
			}
		case sid != "" && k.Resume != "":
			tpl = k.Resume
		case k.ResumeLatest != "":
			tpl = k.ResumeLatest
		}
	}
	if sid == "" {
		sid = a.ID
	}
	if a.Fork && k.Fork != "" && k.Resume != "" && tpl == k.Resume {
		tpl += " " + k.Fork
	}
	pairs := []string{"{id}", a.ID, "{sid}", sid}
	for name, text := range l.Vars {
		pairs = append(pairs, name, text)
	}
	return strings.NewReplacer(pairs...).Replace(tpl)
}

// ScreenWaiting reports whether screen text shows the agent asking for
// input, per the kind's Waiting patterns.
func (k Kind) ScreenWaiting(screen string) bool {
	for _, re := range k.waitingRe {
		if re.MatchString(screen) {
			return true
		}
	}
	return false
}
