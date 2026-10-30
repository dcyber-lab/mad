package desktop

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/team"
)

// Teams in the window (package team): the templates a new team starts
// from, starting one, and putting one to sleep or ending it.

// BusTeam is the team a channel belongs to, as the channel's header
// draws it: its roles with their agents, whose live status the window
// has from the agents.
type BusTeam struct {
	Name     string        `json:"name"`
	Template string        `json:"template"`
	Roles    []BusTeamRole `json:"roles"`
}

type BusTeamRole struct {
	Role  string `json:"role"`
	Agent string `json:"agent,omitempty"` // the agent's id
	Lead  bool   `json:"lead,omitempty"`
}

// busTeams maps channel id → its team.
func busTeams(st *state.State) map[string]*BusTeam {
	out := map[string]*BusTeam{}
	list, _ := team.Running(st)
	for _, r := range list {
		bt := &BusTeam{Name: r.Name, Template: r.Template, Roles: []BusTeamRole{}}
		for _, x := range r.Roles {
			bt.Roles = append(bt.Roles, BusTeamRole{Role: x.Role, Agent: x.Agent, Lead: x.Lead})
		}
		out[r.Channel] = bt
	}
	return out
}

type teamRole struct {
	Name     string `json:"name"`
	Agent    string `json:"agent"` // claude:opus, codex@high
	Lead     bool   `json:"lead,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Brief    string `json:"brief,omitempty"` // its first line
}

type teamTemplate struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Source      string     `json:"source"`
	Roles       []teamRole `json:"roles"`
}

type teamChoice struct {
	Agent   string   `json:"agent"` // claude:opus, codex, codex:gpt-6-sol
	Efforts []string `json:"efforts"`
}

// teamTemplates answers GET /api/team/templates?project=PATH: the
// templates a team there can follow, the agents a role can be played by
// (the run form's), the permission levels and the limits.
func (s *Server) teamTemplates(w http.ResponseWriter, r *http.Request) {
	root, _ := paths.ProjectRoot(paths.Expand(r.URL.Query().Get("project")))
	list, err := team.Templates(root)
	out := struct {
		Templates   []teamTemplate `json:"templates"`
		Choices     []teamChoice   `json:"choices"`
		Permissions []string       `json:"permissions"`
		MaxTeams    int            `json:"max_teams"`
		Error       string         `json:"error,omitempty"`
	}{Templates: []teamTemplate{}, Choices: []teamChoice{}, Permissions: run.Permissions, MaxTeams: team.MaxTeams}
	if err != nil {
		out.Error = err.Error()
	}
	for _, t := range list {
		tt := teamTemplate{Name: t.Name, Description: t.Description, Source: t.Source, Roles: []teamRole{}}
		if t.Source != "built in" {
			tt.Source = paths.Short(t.Source)
		}
		for _, x := range t.Roles {
			first, _, _ := strings.Cut(strings.TrimSpace(string(x.Brief)), "\n")
			tt.Roles = append(tt.Roles, teamRole{Name: x.Name, Agent: x.Agent, Lead: x.Lead, ReadOnly: x.ReadOnly, Brief: first})
		}
		out.Templates = append(out.Templates, tt)
	}
	for _, c := range run.Choices() {
		out.Choices = append(out.Choices, teamChoice{Agent: c.String(), Efforts: run.Efforts(c)[1:]})
	}
	reply(w, out)
}

// teamDo answers POST /api/team {op, ...}: start a team (template,
// project, name, goal, permission, worktree, agents), or sleep or rm one
// (team). Starting takes as long as its agents need to come up.
func (s *Server) teamDo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op         string
		Team       string
		Template   string
		Project    string
		Name, Goal string
		Permission string
		Worktree   string
		Agents     map[string]string
	}
	if !decode(w, r, &req) {
		return
	}
	switch req.Op {
	case "start":
		if _, ok := s.projectPath(req.Project); !ok {
			fail(w, http.StatusBadRequest, errors.New("no such project"))
			return
		}
		got, err := team.Start(team.Options{Project: req.Project, Template: req.Template, Name: req.Name, Goal: req.Goal,
			Permission: req.Permission, Branch: req.Worktree, Agents: req.Agents, Kinds: s.kinds})
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		changed()
		reply(w, map[string]any{"name": got.Name, "channel": got.Channel, "notes": got.Notes})
	case "sleep":
		n, err := team.Sleep(req.Team)
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		changed()
		reply(w, map[string]any{"slept": n})
	case "rm":
		if err := team.Remove(req.Team); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		changed()
		reply(w, map[string]any{"ok": true})
	default:
		fail(w, http.StatusBadRequest, errors.New("op is start, sleep or rm"))
	}
}

// projectPath finds the project at path among the deck's.
func (s *Server) projectPath(path string) (*state.Project, bool) {
	st, err := state.Load()
	if err != nil {
		return nil, false
	}
	p := st.FindProject(path)
	return p, p != nil
}
