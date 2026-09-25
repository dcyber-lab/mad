package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AgentKind describes how to launch one kind of coding agent. Command
// templates are shell strings with these placeholders:
//
//	{id}              the agent's UUID
//	{sid}             the agent's current session id (falls back to {id})
//	{claude_settings} path to mad's claude settings (status hooks)
//	{codex_notify}    `-c notify=[...]` wiring codex's notify to mad
//
// Kinds can be overridden or added via ~/.config/mad/agents.json, a JSON
// array of AgentKind.
type AgentKind struct {
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

var builtinKinds = []AgentKind{
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

func loadKinds() []AgentKind {
	kinds := append([]AgentKind(nil), builtinKinds...)
	if data, err := os.ReadFile(agentsConfigPath()); err == nil {
		var extra []AgentKind
		if json.Unmarshal(data, &extra) == nil {
			for _, k := range extra {
				replaced := false
				for i := range kinds {
					if kinds[i].Name == k.Name {
						kinds[i], replaced = k, true
					}
				}
				if !replaced {
					kinds = append(kinds, k)
				}
			}
		}
	}
	for i := range kinds {
		for _, w := range kinds[i].Waiting {
			if re, err := regexp.Compile(w); err == nil {
				kinds[i].waitingRe = append(kinds[i].waitingRe, re)
			}
		}
	}
	return kinds
}

func kindByName(kinds []AgentKind, name string) AgentKind {
	for _, k := range kinds {
		if k.Name == name {
			return k
		}
	}
	return AgentKind{Name: name, Start: name}
}

// command builds the shell command for a, resuming its previous session
// when resume is set and one can be found.
func (k AgentKind) command(a *Agent, resume bool) string {
	sid := a.SessionID
	tpl := k.Start
	if resume {
		switch {
		case k.Name == "claude":
			// --resume fails hard on a session that never got a message.
			if sid == "" {
				sid = a.ID
			}
			if claudeSessionExists(sid) {
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
	if a.Fork && tpl == k.Resume && k.Name == "claude" {
		tpl += " --fork-session"
	}
	notify := `-c ` + shellQuote(`notify=["`+selfPath()+`","hook","codex"]`)
	return strings.NewReplacer(
		"{id}", a.ID,
		"{sid}", sid,
		"{claude_settings}", shellQuote(claudeSettingsPath()),
		"{codex_notify}", notify,
	).Replace(tpl)
}

func claudeSessionExists(sid string) bool {
	matches, _ := filepath.Glob(filepath.Join(homeDir(), ".claude", "projects", "*", sid+".jsonl"))
	return len(matches) > 0
}

func (k AgentKind) screenWaiting(screen string) bool {
	for _, re := range k.waitingRe {
		if re.MatchString(screen) {
			return true
		}
	}
	return false
}
