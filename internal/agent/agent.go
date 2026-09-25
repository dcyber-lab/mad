// Package agent describes the kinds of coding agents mad can run and how
// to launch or resume each of them.
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
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
//	{claude_settings} path to mad's claude settings (status hooks)
//	{codex_notify}    `-c notify=[...]` wiring codex's notify to mad
//
// Kinds can be overridden or added via ~/.config/mad/agents.json, a JSON
// array of Kind.
type Kind struct {
	Name string `json:"name"`
	// Start launches a fresh session.
	Start string `json:"start"`
	// Resume reopens session {sid}; used when a session id is known.
	Resume string `json:"resume,omitempty"`
	// ResumeLatest reopens the most recent session when no id is known.
	ResumeLatest string `json:"resume_latest,omitempty"`
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
		Start:   "claude --settings {claude_settings} --session-id {id}",
		Resume:  "claude --settings {claude_settings} --resume {sid}",
		Waiting: []string{`Do you want to`, `❯ 1\. Yes`, `trust this folder`, `Enter to confirm`},
		Hooks:   true,
	},
	{
		Name:         "codex",
		Start:        "codex {codex_notify}",
		Resume:       "codex resume {codex_notify} {sid}",
		ResumeLatest: "codex resume {codex_notify} --last",
		Waiting:      []string{`Would you like to`, `Allow command`, `Approve`},
	},
	{
		// pi creates the session on first use, so start == resume.
		Name:  "pi",
		Start: "pi --session-id {id}",
	},
	{
		Name:  "shell",
		Start: `exec "${SHELL:-/bin/zsh}" -l`,
	},
}

// Builtin returns the kinds mad knows without any configuration.
func Builtin() []Kind {
	return compile(append([]Kind(nil), builtin...))
}

// Load returns the built-in kinds merged with agents.json: an entry with a
// built-in name replaces it, others are appended. A missing or invalid file
// leaves the built-ins as they are.
func Load() []Kind {
	kinds := append([]Kind(nil), builtin...)
	if data, err := os.ReadFile(paths.AgentsConfig()); err == nil {
		var extra []Kind
		if json.Unmarshal(data, &extra) == nil {
			for _, k := range extra {
				replaced := false
				for i := range kinds {
					if kinds[i].Name == k.Name {
						kinds[i], replaced = k, true
					}
				}
				if !replaced && k.Name != "" {
					kinds = append(kinds, k)
				}
			}
		}
	}
	return compile(kinds)
}

func compile(kinds []Kind) []Kind {
	for i := range kinds {
		kinds[i].waitingRe = nil
		for _, w := range kinds[i].Waiting {
			if re, err := regexp.Compile(w); err == nil {
				kinds[i].waitingRe = append(kinds[i].waitingRe, re)
			}
		}
	}
	return kinds
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

// Command builds the shell command for a, resuming its previous session
// when resume is set and one can be found.
func (k Kind) Command(a *state.Agent, resume bool) string {
	sid := a.SessionID
	tpl := k.Start
	if resume {
		switch {
		case k.Name == "claude":
			// --resume fails hard on a session that never got a message.
			if sid == "" {
				sid = a.ID
			}
			if ClaudeSessionExists(sid) {
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
	if a.Fork && k.Name == "claude" && tpl == k.Resume {
		tpl += " --fork-session"
	}
	notify := ""
	if !CodexHasOwnNotify() {
		notify = `-c ` + paths.ShellQuote(`notify=["`+paths.Self()+`","hook","codex"]`)
	}
	return strings.NewReplacer(
		"{id}", a.ID,
		"{sid}", sid,
		"{claude_settings}", paths.ShellQuote(paths.ClaudeSettings()),
		"{codex_notify}", notify,
	).Replace(tpl)
}

// notifyKey matches a `notify = ...` line in codex's config.toml, at the top
// level or in a profile.
var notifyKey = regexp.MustCompile(`^\s*notify\s*=`)

// CodexHasOwnNotify reports whether the user set codex's notify command
// themselves. codex takes a single notify command, so mad's -c notify=...
// would replace theirs; mad leaves it alone then, and codex resumes fall
// back to the most recent session because mad never learns the thread id.
func CodexHasOwnNotify() bool {
	data, err := os.ReadFile(filepath.Join(paths.CodexHome(), "config.toml"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if notifyKey.MatchString(line) {
			return true
		}
	}
	return false
}

// ClaudeSessionExists reports whether claude has a transcript for sid.
func ClaudeSessionExists(sid string) bool {
	matches, _ := filepath.Glob(filepath.Join(paths.Home(), ".claude", "projects", "*", sid+".jsonl"))
	return len(matches) > 0
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
