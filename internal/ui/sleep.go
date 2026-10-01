package ui

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// sleepCheckEvery is how often agents are checked against sleep.after.
const sleepCheckEvery = 30 * time.Second

// sleepAgent is replaceable in tests.
var sleepAgent = deck.SleepAgent

// sleptMsg ends a round of putting agents to sleep: the idle check's, or
// z's on the agent named manual.
type sleptMsg struct {
	manual string
	err    error
}

// canSleep: the kind can reopen the agent's session, so ending its process
// loses nothing but what the process held.
func (m *model) canSleep(a *state.Agent) bool {
	return agent.ByName(m.kinds, a.Kind).Resume != ""
}

// sleepers are the agents that have sat idle off stage for sleep.after.
// One whose diff is on stage counts as on stage: closing the diff brings
// it back.
func (m *model) sleepers(now time.Time) []string {
	var ids []string
	for _, a := range m.st.OrderedAgents() {
		tr := m.trackers[a.ID]
		shown := a.ID == m.stageID || m.stageID == tmux.IDTask && a.ID == m.taskFor
		// A working run comes back to its agents; it would only wake them.
		if tr == nil || tr.Status != status.Idle || shown || !m.canSleep(a) || a.Run != "" && m.runActive(a.Run) {
			continue
		}
		if now.Sub(tr.QuietSince()) >= m.cfg.SleepAfter {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// sleepCmd puts the agents that have been idle long enough to sleep, one
// at a time off the UI goroutine. One that is busy is left for the next
// check.
func (m *model) sleepCmd(now time.Time) tea.Cmd {
	m.lastSleepCheck = now
	ids := m.sleepers(now)
	if len(ids) == 0 {
		return nil
	}
	m.sleeping = true
	m.epoch++
	return func() tea.Msg {
		for _, id := range ids {
			var busy *deck.BusyError
			if _, err := sleepAgent(id, true); err != nil && !errors.As(err, &busy) {
				return sleptMsg{err: err}
			}
		}
		return sleptMsg{}
	}
}

// sleepNow is z: put the agent to sleep now, unless it is in the middle
// of something.
func (m *model) sleepNow(p *state.Project, a *state.Agent) tea.Cmd {
	name := p.DisplayName(a)
	st := status.Stopped
	if tr := m.trackers[a.ID]; tr != nil && tr.Status != "" {
		st = tr.Status
	}
	switch {
	case !m.canSleep(a):
		m.setFlash(a.Kind + " can't resume a session: not put to sleep")
		return nil
	case st == status.Running || st == status.Waiting:
		m.setFlash(name + " is " + st)
		return nil
	case st != status.Idle:
		return nil // nothing runs
	}
	m.epoch++
	id := a.ID
	return func() tea.Msg {
		_, err := sleepAgent(id, false)
		return sleptMsg{manual: name, err: err}
	}
}

func (m *model) applySlept(msg sleptMsg) tea.Cmd {
	if msg.manual == "" {
		m.sleeping = false
	}
	var busy *deck.BusyError
	switch {
	case errors.As(msg.err, &busy):
		m.setFlash(fmt.Sprintf("%s: %s runs under it, left awake", msg.manual, busy.Shell))
	case msg.err != nil:
		m.setFlash(msg.err.Error())
	}
	return m.pollNow()
}
