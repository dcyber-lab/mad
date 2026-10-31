package ui

import (
	"fmt"
	"strings"
	"testing"

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
	// the order they were started in: the team's agents are apart
	p.Agents = []*state.Agent{own, lead, solo, rev}
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

	// No team head without its agents.
	p.Agents = []*state.Agent{own, solo}
	m.rebuildRows()
	if strings.Contains(ansi.Strip(m.View()), "team ship") {
		t.Error("a team head with nobody under it")
	}
}
