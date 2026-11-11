package ui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/rounds"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// Idle agents are put to sleep by the rounds (package rounds); here is z,
// which puts the agent under the cursor to sleep now.

// sleepAgent is replaceable in tests.
var sleepAgent = deck.SleepAgent

// sleptMsg ends z's putting the agent named manual to sleep.
type sleptMsg struct {
	manual string
	err    error
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
	case !rounds.CanSleep(m.kinds, a):
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
	var busy *deck.BusyError
	switch {
	case errors.As(msg.err, &busy):
		m.setFlash(fmt.Sprintf("%s: %s runs under it, left awake", msg.manual, busy.Command))
	case msg.err != nil:
		m.setFlashErr(msg.err.Error())
	}
	return m.pollNow()
}
