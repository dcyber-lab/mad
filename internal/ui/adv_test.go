package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// A team started on a branch (mad team start --branch) runs in a worktree.
func TestAdvTeamWorktreeBadge(t *testing.T) {
	m, st := setup(t, "/p")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	p := st.Projects[0]
	solo := &state.Agent{ID: "solo", Kind: "shell", Name: "solo", Dir: "/p/.worktrees/feat-x"}
	lead := &state.Agent{ID: "lead", Kind: "shell", Name: "lead", Role: "lead", Team: "ship", Dir: "/p/.worktrees/feat-x"}
	p.Agents = []*state.Agent{solo, lead}
	m.st.Compact = true
	m.rebuildRows()
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "solo") || strings.Contains(l, "lead") || strings.Contains(l, "team") {
			t.Logf("%q", l)
		}
	}
	for _, r := range m.rows {
		if r.agent == lead {
			left, _ := m.rowSegs(r)
			var s string
			for _, x := range left {
				s += x.s
			}
			if !strings.Contains(s, "feat-x") {
				t.Errorf("team member row %q does not name its checkout feat-x", s)
			}
		}
	}
}

// The inbox/Working tie-break is "the agent's place in the deck's order".
func TestAdvWorkingOrder(t *testing.T) {
	m, st := setup(t, "/p")
	p := st.Projects[0]
	lead := &state.Agent{ID: "lead", Kind: "claude", Name: "lead", Team: "ship"}
	solo := &state.Agent{ID: "solo", Kind: "claude", Name: "solo"}
	rev := &state.Agent{ID: "rev", Kind: "claude", Name: "rev", Team: "ship"}
	p.Agents = []*state.Agent{lead, solo, rev}
	for _, id := range []string{"lead", "solo", "rev"} {
		m.trackers[id] = &status.Tracker{ID: id, Status: status.Running} // first look: Since unknown
	}
	m.rebuildRows()
	var drawn []string
	for _, a := range st.OrderedAgents() {
		drawn = append(drawn, a.ID)
	}
	m.look(time.Now())
	t.Logf("drawn (1-9) order: %v", drawn)
	t.Logf("Working order:     %v", m.view.Working)
	if strings.Join(drawn, ",") != strings.Join(m.view.Working, ",") {
		t.Errorf("Working %v is not in the deck's order %v", m.view.Working, drawn)
	}
}

// A team member that joins later shifts the numbers of agents after the team.
func TestAdvNumberShift(t *testing.T) {
	p := &state.Project{Path: "/p"}
	own := &state.Agent{ID: "own"}
	lead := &state.Agent{ID: "lead", Team: "ship"}
	solo := &state.Agent{ID: "solo"}
	p.Agents = []*state.Agent{own, lead, solo}
	num := func(id string) int {
		for i, a := range p.TopAgents() {
			if a.ID == id {
				return i + 1
			}
		}
		return 0
	}
	before := num("solo")
	p.Agents = append(p.Agents, &state.Agent{ID: "rev", Team: "ship"})
	after := num("solo")
	t.Logf("solo is %d before rev (team ship) is appended, %d after", before, after)
	if before != after {
		t.Errorf("appending a team member renumbered solo: %d -> %d", before, after)
	}
}
