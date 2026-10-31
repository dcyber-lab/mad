package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// A team's agents sit together under a head of the team's name and how they
// stand, inside their project; the number keys count them as drawn.
func TestTeamRows(t *testing.T) {
	m, st := setup(t, "/p")
	p := st.Projects[0]
	own := &state.Agent{ID: "own", Kind: "shell"}
	lead := &state.Agent{ID: "lead", Kind: "claude", Name: "lead", Role: "lead", Team: "ship"}
	solo := &state.Agent{ID: "solo", Kind: "claude"}
	rev := &state.Agent{ID: "rev", Kind: "claude", Name: "review", Role: "review", Team: "ship"}
	// the team's agents are spawned together: contiguous, in storage order
	p.Agents = []*state.Agent{own, lead, rev, solo}
	m.trackers["lead"] = &status.Tracker{Status: status.Running}
	m.trackers["rev"] = &status.Tracker{Status: status.Waiting}
	m.st.Compact = true // one line a row
	m.rebuildRows()

	var shape []string
	for _, r := range m.rows {
		if r.agent != nil {
			shape = append(shape, fmt.Sprintf("%s%d", r.agent.ID, r.num))
		}
	}
	if got := strings.Join(shape, " "); got != "own1 lead2 rev3 solo4" {
		t.Fatalf("rows = %s", got)
	}
	for i, a := range p.TopAgents() { // what 1-9 open is what is drawn
		if r := m.rows[1+i]; r.agent != a {
			t.Errorf("key %d opens %s, row shows %s", i+1, a.ID, r.agent.ID)
		}
	}

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	at := -1
	for i, l := range lines {
		if strings.Contains(l, "team ship") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no team head:\n%s", strings.Join(lines, "\n"))
	}
	head := lines[at]
	if !strings.Contains(head, "◆") && !strings.Contains(head, "?") || !strings.Contains(head, "2") {
		t.Errorf("head %q lacks the roll-up (the one waiting) and the count", head)
	}
	if !strings.Contains(lines[at+1], "lead") || !strings.Contains(lines[at+2], "review") || !strings.Contains(lines[at+3], "claude") {
		t.Errorf("under the head:\n%s", strings.Join(lines[at:at+4], "\n"))
	}
	if !strings.HasPrefix(lines[at+1], "    ") || strings.HasPrefix(lines[at+3], "    ") && strings.Contains(lines[at+3], "solo") {
		t.Errorf("members are indented, the others are not:\n%s", strings.Join(lines[at:at+4], "\n"))
	}

	// The head is no row: a click on it opens nothing, the line under it is lead's.
	y := at - m.headerH()
	if _, ok := m.rowAt(y); ok {
		t.Error("the team head is a row")
	}
	if i, ok := m.rowAt(y + 1); !ok || m.rows[i].agent != lead {
		t.Errorf("under the head: %v %v", i, ok)
	}

	// A member apart from its team starts a head of its own: no agent moves.
	late := &state.Agent{ID: "late", Kind: "claude", Name: "late", Team: "ship"}
	p.Agents = []*state.Agent{own, lead, rev, solo, late}
	m.rebuildRows()
	shape = nil
	for _, r := range m.rows {
		if r.agent != nil {
			shape = append(shape, fmt.Sprintf("%s%d", r.agent.ID, r.num))
		}
	}
	if got := strings.Join(shape, " "); got != "own1 lead2 rev3 solo4 late5" {
		t.Errorf("a late member moved agents: %s", got)
	}

	// No team head without its agents.
	p.Agents = []*state.Agent{own, solo}
	m.rebuildRows()
	if strings.Contains(ansi.Strip(m.View()), "team ship") {
		t.Error("a team head with nobody under it")
	}
}

// Numbers never move: a team member that joins later renumbers nobody.
func TestTeamNumbersStay(t *testing.T) {
	p := &state.Project{Path: "/p"}
	own := &state.Agent{ID: "own"}
	lead := &state.Agent{ID: "lead", Team: "ship"}
	solo := &state.Agent{ID: "solo"}
	p.Agents = []*state.Agent{own, lead, solo}
	before := p.TopAgents()
	p.Agents = append(p.Agents, &state.Agent{ID: "rev", Team: "ship"})
	for i, a := range before {
		if p.TopAgents()[i] != a {
			t.Errorf("number %d moved from %s", i+1, a.ID)
		}
	}
}

// A team started on a branch works in a worktree: the head names it when
// they share it, the row when they do not.
func TestTeamWorktreeBadge(t *testing.T) {
	m, st := setup(t, "/p")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	p := st.Projects[0]
	lead := &state.Agent{ID: "lead", Kind: "shell", Name: "lead", Team: "ship", Dir: "/p/.worktrees/feat-x"}
	rev := &state.Agent{ID: "rev", Kind: "shell", Name: "rev", Team: "ship", Dir: "/p/.worktrees/feat-x"}
	p.Agents = []*state.Agent{lead, rev}
	m.st.Compact = true
	m.rebuildRows()
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "team ship  feat-x") {
		t.Errorf("the head does not name the shared worktree:\n%s", view)
	}
	rev.Dir = "/p/.worktrees/feat-y"
	m.rebuildRows()
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "lead  feat-x") || !strings.Contains(view, "rev  feat-y") {
		t.Errorf("members in different checkouts must name theirs:\n%s", view)
	}
}

// At 24 columns a member keeps its whole name; a head is never the last line.
func TestTeamNarrowAndLastLine(t *testing.T) {
	m, st := setup(t, "/p")
	p := st.Projects[0]
	lead := &state.Agent{ID: "lead", Kind: "shell", Name: "lead", Team: "ship"}
	p.Agents = []*state.Agent{lead, {ID: "rev", Kind: "shell", Name: "reviewer", Team: "ship"}, {ID: "solo", Kind: "shell", Name: "reviewer"}}
	m.st.Compact = true
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 30})
	m.rebuildRows()
	view := ansi.Strip(m.View())
	// a member's name is cut no sooner than a plain row's of the same name
	var cuts []string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "·") && strings.Contains(l, "re") {
			f := strings.Fields(l)
			cuts = append(cuts, f[3])
		}
	}
	if len(cuts) != 2 || cuts[0] != cuts[1] {
		t.Errorf("member and plain name cut differently %q:\n%s", cuts, view)
	}
	for h := 4; h < 12; h++ {
		m.Update(tea.WindowSizeMsg{Width: 40, Height: h})
		m.rebuildRows()
		lines := strings.Split(strings.TrimRight(ansi.Strip(m.sidebarView()), "\n "), "\n")
		for i, l := range lines {
			if strings.Contains(l, "team ship") && (i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == "" || strings.Contains(lines[i+1], "team ")) {
				t.Errorf("height %d: a head with nobody under it:\n%s", h, strings.Join(lines, "\n"))
			}
		}
	}
}
