package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/status"
)

const (
	// dispatchEvery: how often the queues are looked at.
	dispatchEvery = time.Second
	// queueRetry: after an agent refused a queued message (it was busy,
	// asking you something, or had a draft of yours), how long until it
	// is offered again.
	queueRetry = 15 * time.Second
	// queueSettle: after a message went in, the agent's status needs a
	// poll to say it works (the typed text alone changes its screen, and
	// a changed screen reads as running for status.ActiveWindow); the
	// next one waits that long.
	queueSettle = status.ActiveWindow + 500*time.Millisecond
)

// sendQueued is drive.Send, replaceable in tests.
var sendQueued = drive.Send

// dispatchedMsg ends one try at sending agent id its next queued message.
type dispatchedMsg struct {
	id  string
	err error
}

// dispatchCmd sends the first message queued for an agent whose turn is
// over: idle, or asleep or stopped (it is resumed for it). An agent that
// is working or waiting for you keeps its queue, and so does a run's:
// its runner gives it its work.
func (m *model) dispatchCmd(now time.Time) tea.Cmd {
	m.lastDispatch = now
	m.queued = queue.Pending()
	if len(m.queued) == 0 {
		return nil
	}
	for _, a := range m.st.OrderedAgents() {
		if m.queued[a.ID] == 0 || a.Run != "" || now.Before(m.queueAt[a.ID]) {
			continue
		}
		tr := m.trackers[a.ID]
		if tr == nil {
			continue
		}
		switch tr.Status {
		case status.Idle, status.Asleep, status.Stopped:
		default:
			continue
		}
		id, st, kinds := a.ID, m.st.Clone(), m.kinds
		m.dispatching = true
		m.epoch++ // a woken agent changes the panes
		return func() tea.Msg {
			it, ok, err := queue.Take(id)
			if err != nil || !ok {
				return dispatchedMsg{id: id, err: err}
			}
			_, ag := st.FindAgent(id)
			if ag == nil {
				return dispatchedMsg{id: id} // removed meanwhile; so is its queue
			}
			if err := sendQueued(st, ag, it.Text, kinds); err != nil {
				_ = queue.PutBack(id, it, err.Error())
				return dispatchedMsg{id: id, err: err}
			}
			_ = queue.Sent(id)
			return dispatchedMsg{id: id}
		}
	}
	return nil
}

func (m *model) applyDispatched(msg dispatchedMsg) tea.Cmd {
	m.dispatching = false
	if msg.err != nil {
		m.queueAt[msg.id] = time.Now().Add(queueRetry)
		return nil
	}
	m.queueAt[msg.id] = time.Now().Add(queueSettle)
	if n := m.queued[msg.id]; n > 0 {
		m.queued[msg.id] = n - 1
	}
	return m.pollNow()
}
