// Package state is the persistent project → agent tree, stored as JSON in
// the state directory. Runtime facts (is a pane alive, which agent is on
// stage) come from tmux, not from here.
package state

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

type State struct {
	Projects []*Project `json:"projects"`
	// Compact hides the line under each agent that says what it is on.
	Compact bool `json:"compact,omitempty"`
	// Ignored projects are not re-added by auto-sync after being removed.
	Ignored []string `json:"ignored,omitempty"`
}

type Project struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Collapsed bool     `json:"collapsed,omitempty"`
	Agents    []*Agent `json:"agents"`
	// Runs are tasks handed through agents in roles (package run); the
	// agents in them are among Agents, marked with the run's id.
	Runs []*Run `json:"runs,omitempty"`
}

// Run is a task its agents work on in turn, each in a role: one designs,
// one implements, one reviews. What it is and how far it got is in its
// own directory (package run); here is what the sidebar lists.
type Run struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Dir       string    `json:"dir"` // the worktree its agents work in
	Branch    string    `json:"branch"`
	Collapsed bool      `json:"collapsed,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Agent struct {
	ID   string `json:"id"` // UUID; also passed as --session-id where supported
	Kind string `json:"kind"`
	// SessionID is the agent's current conversation id when it differs from
	// ID (claude after /clear, codex thread ids). Used for resume.
	SessionID string `json:"session_id,omitempty"`
	// Dir overrides the project path as working directory (e.g. an adopted
	// session that ran in a worktree).
	Dir string `json:"dir,omitempty"`
	// Fork resumes SessionID as a copy (the original is open elsewhere,
	// e.g. in the desktop app). Cleared once the copy reports its own id.
	Fork bool `json:"fork,omitempty"`
	// Name is what the user called the agent; shown under it instead of
	// the conversation's own title.
	Name string `json:"name,omitempty"`
	// Args are added to the kind's command, on resume too: the model,
	// permission flags (`mad spawn`).
	Args []string `json:"args,omitempty"`
	// Run is the id of the run the agent works in, Role its part there.
	Run       string    `json:"run,omitempty"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Load reads the state file; a missing file is an empty state, a
// malformed one an error naming the line.
func Load() (*State, error) {
	var s State
	if err := paths.ReadJSON(paths.StateFile(), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *State) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(paths.StateFile(), data)
}

// ModTime of the state file, to notice writes by other processes.
func ModTime() time.Time {
	fi, err := os.Stat(paths.StateFile())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func (s *State) FindProject(path string) *Project {
	for _, p := range s.Projects {
		if p.Path == path {
			return p
		}
	}
	return nil
}

// AddProject registers path (already resolved to its root) and reports
// whether it is new. Adding by hand also lifts an earlier removal.
func (s *State) AddProject(path string) (*Project, bool) {
	if p := s.FindProject(path); p != nil {
		return p, false
	}
	s.unignore(path)
	p := &Project{Name: filepath.Base(path), Path: path, Agents: []*Agent{}}
	s.Projects = append(s.Projects, p)
	return p, true
}

// RemoveProject drops p and remembers it so auto-sync won't bring it back.
func (s *State) RemoveProject(p *Project) {
	for i, q := range s.Projects {
		if q == p {
			s.Projects = append(s.Projects[:i], s.Projects[i+1:]...)
			break
		}
	}
	if !s.IsIgnored(p.Path) {
		s.Ignored = append(s.Ignored, p.Path)
	}
}

func (s *State) IsIgnored(path string) bool {
	for _, x := range s.Ignored {
		if x == path {
			return true
		}
	}
	return false
}

func (s *State) unignore(path string) {
	for i, x := range s.Ignored {
		if x == path {
			s.Ignored = append(s.Ignored[:i], s.Ignored[i+1:]...)
			return
		}
	}
}

func (s *State) FindAgent(id string) (*Project, *Agent) {
	for _, p := range s.Projects {
		for _, a := range p.Agents {
			if a.ID == id {
				return p, a
			}
		}
	}
	return nil, nil
}

// FindRun finds a run by id.
func (s *State) FindRun(id string) (*Project, *Run) {
	for _, p := range s.Projects {
		for _, r := range p.Runs {
			if r.ID == id {
				return p, r
			}
		}
	}
	return nil, nil
}

// RemoveRun drops run id; its agents stay.
func (s *State) RemoveRun(id string) {
	for _, p := range s.Projects {
		for i, r := range p.Runs {
			if r.ID == id {
				p.Runs = append(p.Runs[:i], p.Runs[i+1:]...)
				return
			}
		}
	}
}

// RunAgents are the agents of run id in p, in their order there.
func (p *Project) RunAgents(id string) []*Agent {
	var out []*Agent
	for _, a := range p.Agents {
		if a.Run == id {
			out = append(out, a)
		}
	}
	return out
}

func (s *State) RemoveAgent(id string) {
	for _, p := range s.Projects {
		for i, a := range p.Agents {
			if a.ID == id {
				p.Agents = append(p.Agents[:i], p.Agents[i+1:]...)
				return
			}
		}
	}
}

// OrderedAgents are all agents in sidebar order, which `mad switch
// next|prev` steps through: in each project the agents of its runs, run
// by run, then its own.
func (s *State) OrderedAgents() []*Agent {
	var out []*Agent
	for _, p := range s.Projects {
		for _, r := range p.Runs {
			out = append(out, p.RunAgents(r.ID)...)
		}
		out = append(out, p.TopAgents()...)
	}
	return out
}

// InRun reports whether a plays a role in one of p's runs.
func (p *Project) InRun(a *Agent) bool {
	if a.Run == "" {
		return false
	}
	for _, r := range p.Runs {
		if r.ID == a.Run {
			return true
		}
	}
	return false
}

// TopAgents are p's agents in none of its runs.
func (p *Project) TopAgents() []*Agent {
	var out []*Agent
	for _, a := range p.Agents {
		if !p.InRun(a) {
			out = append(out, a)
		}
	}
	return out
}

// Group is the agents a is numbered among, as the sidebar shows them
// and 1-9 open them: its run's, or p's own.
func (p *Project) Group(a *Agent) []*Agent {
	if p.InRun(a) {
		return p.RunAgents(a.Run)
	}
	return p.TopAgents()
}

// OpenSessions are the session ids the agents hold: the current one, and
// the agent's own id, which a claude agent's first session is started with.
func (s *State) OpenSessions() map[string]bool {
	ids := map[string]bool{}
	for _, p := range s.Projects {
		for _, a := range p.Agents {
			ids[a.ID] = true
			if a.SessionID != "" {
				ids[a.SessionID] = true
			}
		}
	}
	return ids
}

// Clone deep-copies the tree, so background work never races the UI.
func (s *State) Clone() *State {
	cp := &State{Ignored: append([]string(nil), s.Ignored...)}
	for _, p := range s.Projects {
		pc := *p
		pc.Runs = nil
		for _, r := range p.Runs {
			rc := *r
			pc.Runs = append(pc.Runs, &rc)
		}
		pc.Agents = make([]*Agent, 0, len(p.Agents))
		for _, a := range p.Agents {
			ac := *a
			ac.Args = append([]string(nil), a.Args...)
			pc.Agents = append(pc.Agents, &ac)
		}
		cp.Projects = append(cp.Projects, &pc)
	}
	return cp
}

// Dir is where a's process runs.
func (p *Project) Dir(a *Agent) string {
	if a.Dir != "" {
		return a.Dir
	}
	return p.Path
}

// DisplayName disambiguates agents of the same kind within a project:
// claude, claude#2, ...
func (p *Project) DisplayName(a *Agent) string {
	n, idx := 0, 0
	for _, b := range p.Agents {
		if b.Kind == a.Kind {
			n++
			if b == a {
				idx = n
			}
		}
	}
	if idx <= 1 {
		return a.Kind
	}
	return fmt.Sprintf("%s#%d", a.Kind, idx)
}

// NewUUID returns a random (version 4) UUID.
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
