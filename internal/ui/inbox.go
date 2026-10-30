package ui

import (
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/deck"
)

// The deck's inbox is the window's: the same attention.Desk.Look over the
// same facts (its trackers, the agents' reports, the bus as last looked
// at), so the block's count and rows, and where d goes, agree with the
// window and the phone.

// look builds the view from what the sidebar holds now. It reads no
// file: the UI goroutine calls it after each poll and each look at the bus.
func (m *model) look(now time.Time) {
	var agents []attention.Agent
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			ag := attention.AgentOf(p, a, m.trackers[a.ID], m.panes[a.ID].ID, m.reports[a.ID], m.transcripts[a.ID])
			ag.Screen = m.screens[a.ID]
			agents = append(agents, ag)
		}
	}
	v := m.desk.Look(attention.Facts{Agents: agents, Cards: m.cards, Skills: m.skills, Spend: m.spend, Now: now})
	same := reflect.DeepEqual(v.Inbox, m.view.Inbox)
	m.view = v
	if !same {
		m.rebuildRows()
	}
}

// jumpNext opens the next item of the inbox after the one d last opened,
// wrapping around: the order the window's Alt-N and the phone's Next walk.
func (m *model) jumpNext() tea.Cmd {
	it, ok := m.view.Inbox.After(m.nextKey)
	if !ok {
		m.setFlash("nothing waiting or done")
		return nil
	}
	m.nextKey = it.Key
	return m.openItem(it)
}

// openItem goes where item it is settled: its agent, its channel's chat,
// the board, or the pending skills.
func (m *model) openItem(it attention.Item) tea.Cmd {
	switch o := it.Open; {
	case o.Agent != "":
		_, a := m.st.FindAgent(o.Agent)
		if a == nil {
			return nil
		}
		m.selectAgent(a.ID)
		return m.openCmd(a)
	case o.Channel != "":
		return m.showBusView("chat:"+o.Channel, chatCommand(o.Channel))
	case o.Page == attention.PageSkills:
		return m.showBusView("skills", deck.HoldCommand(deck.SelfCommand("skill pending")))
	}
	return m.showBusView("board", deck.SelfCommand("board -w"))
}
