package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// State is the persistent project → agent tree. Runtime facts (is the pane
// alive, is it on stage) come from tmux, not from here.
type State struct {
	Projects []*Project `json:"projects"`
	// Ignored projects are not re-added by auto-sync after being removed.
	Ignored []string `json:"ignored,omitempty"`
}

type Project struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Collapsed bool     `json:"collapsed,omitempty"`
	Agents    []*Agent `json:"agents"`
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
	Fork      bool      `json:"fork,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func loadState() (*State, error) {
	data, err := os.ReadFile(stateFile())
	if errors.Is(err, fs.ErrNotExist) {
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", stateFile(), err)
	}
	return &s, nil
}

func (s *State) save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(stateFile(), data)
}

func stateModTime() time.Time {
	fi, err := os.Stat(stateFile())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func (s *State) findProject(path string) *Project {
	for _, p := range s.Projects {
		if p.Path == path {
			return p
		}
	}
	return nil
}

// addProject registers path (already resolved to its root); it reports
// whether the project is new.
func (s *State) addProject(path string) (*Project, bool) {
	if p := s.findProject(path); p != nil {
		return p, false
	}
	s.unignore(path)
	p := &Project{Name: filepath.Base(path), Path: path, Agents: []*Agent{}}
	s.Projects = append(s.Projects, p)
	return p, true
}

func (s *State) removeProject(p *Project) {
	for i, q := range s.Projects {
		if q == p {
			s.Projects = append(s.Projects[:i], s.Projects[i+1:]...)
			break
		}
	}
	if !s.isIgnored(p.Path) {
		s.Ignored = append(s.Ignored, p.Path)
	}
}

func (s *State) isIgnored(path string) bool {
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

func (p *Project) dirFor(a *Agent) string {
	if a.Dir != "" {
		return a.Dir
	}
	return p.Path
}

func (s *State) findAgent(id string) (*Project, *Agent) {
	for _, p := range s.Projects {
		for _, a := range p.Agents {
			if a.ID == id {
				return p, a
			}
		}
	}
	return nil, nil
}

func (s *State) removeAgent(id string) {
	for _, p := range s.Projects {
		for i, a := range p.Agents {
			if a.ID == id {
				p.Agents = append(p.Agents[:i], p.Agents[i+1:]...)
				return
			}
		}
	}
}

// orderedAgents is the global numbering used by `mad switch N` and the
// sidebar's 1-9 labels.
func (s *State) orderedAgents() []*Agent {
	var out []*Agent
	for _, p := range s.Projects {
		out = append(out, p.Agents...)
	}
	return out
}

// displayName disambiguates agents of the same kind within a project:
// claude, claude#2, ...
func (p *Project) displayName(a *Agent) string {
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

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
