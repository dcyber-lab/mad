package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

const (
	// dispatchEvery: how often the queues are looked at.
	dispatchEvery = time.Second
	// lapseEvery: how often handoffs are checked for a receiver that
	// ended its turn without finishing.
	lapseEvery = 10 * time.Second
	// followEvery: how often work that stands without a word is raised
	// (bus.CheckFollowUps) and the progress report considered.
	followEvery = time.Minute
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

// lapsedMsg ends one check of the handoffs for lapses.
type lapsedMsg struct{}

// lapseCmd marks the handoffs lapsed whose receiver's turn ended without
// bus_done (the bus wakes the agent that gave the work). Errors are ignored.
func (m *model) lapseCmd(now time.Time) tea.Cmd {
	m.lapsing, m.lastLapse = true, now
	return func() tea.Msg {
		_ = bus.CheckLapses(now)
		return lapsedMsg{}
	}
}

// followedMsg ends one round of follow-ups, with how the day's budget
// for mad's own work stands.
type followedMsg struct{ spend bus.SpendState }

// costs are the agents' costs so far, as the sidebar has them (claude's;
// the others cost nothing here and only count turns).
func (m *model) costs() map[string]float64 {
	out := map[string]float64{}
	for id, info := range m.transcripts {
		if c := info.Tokens.Cost; c > 0 {
			out[id] = c
		}
	}
	return out
}

// Replaceable in tests.
var (
	checkFollowUps = bus.CheckFollowUps
	deckWatched    = tmux.Watched
	checkReport    = bus.CheckReport
	checkTimers    = bus.CheckTimers
	settleSpend    = bus.SpendSettle
)

// followCmd raises work that stands without a word (nudges, and the
// notifications for the person) and sends the progress report when it is
// due. The agents' statuses are the sidebar's own. The follow-up time and
// whether to nudge at all come from config.json's "bus" object.
func (m *model) followCmd(now time.Time) tea.Cmd {
	if len(m.trackers) == 0 && len(m.st.OrderedAgents()) > 0 {
		return nil // no poll yet: statuses unknown, and a guess would look like news
	}
	m.following, m.lastFollow = true, now
	bus.NudgesOn = m.cfg.Bus.FollowUp > 0
	if bus.NudgesOn {
		bus.FollowUp = m.cfg.Bus.FollowUp
	}
	statuses := map[string]string{}
	for id, tr := range m.trackers {
		if tr != nil && tr.Status != "" {
			statuses[id] = tr.Status
		}
	}
	viewed := false // in a desktop window
	for _, p := range m.panes {
		viewed = viewed || p.Viewed
	}
	st, clock, costs := m.st.Clone(), m.cfg.Bus.Report, m.costs()
	return func() tea.Msg {
		if viewed || deckWatched() {
			bus.PersonAt = now // here: nudging is not wasted on an empty room
		}
		_ = settleSpend(now, costs) // before the nudges: they ask whether the day's budget is spent
		_ = checkFollowUps(now, statuses)
		_ = checkTimers(now, statuses) // check-ins and routines: orders, not guesses
		_ = checkReport(st, statuses, now, clock)
		return followedMsg{spend: bus.SpendNow(now)}
	}
}

// spendFooter is the footer's line once the day's budget is spent.
func spendFooter(s bus.SpendState) string {
	if s.BudgetUSD > 0 && s.USD >= s.BudgetUSD {
		return fmt.Sprintf("budget $%.0f spent · agents wait", s.USD)
	}
	return fmt.Sprintf("budget %d turns spent · agents wait", s.Turns)
}

// dispatchedMsg ends one try at sending agent id its next queued message.
type dispatchedMsg struct {
	id  string
	err error
	// wait, when set, is how long the bus item must still wait for the
	// agent's minute; nothing was sent and it is not a failure.
	wait time.Duration
	// spent: the bus item was held by the day's budget (the footer says so).
	spent bool
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
		id, st, kinds, costs := a.ID, m.st.Clone(), m.kinds, m.costs()
		m.dispatching = true
		m.epoch++ // a woken agent changes the panes
		return func() tea.Msg {
			// The bus wakes an agent once a minute at most (a handoff
			// and its result excepted): until then its bus item stays
			// and the other queued messages go first.
			// With the day's budget for mad's own work spent, a bus item
			// waits too, unless a person's message is in it.
			var it queue.Item
			var ok bool
			var err error
			wait := bus.WaitFor(id, now)
			spent := !bus.SpendOK(now) && !bus.PersonWaits(id)
			if spent && wait == 0 {
				wait = bus.SpendWait
			}
			if wait > 0 || spent {
				if it, ok, err = queue.TakeWhere(id, func(x queue.Item) bool { return !x.Bus }); err == nil && !ok {
					return dispatchedMsg{id: id, wait: wait, spent: spent}
				}
			} else {
				it, ok, err = queue.Take(id)
			}
			if err != nil || !ok {
				return dispatchedMsg{id: id, err: err}
			}
			_, ag := st.FindAgent(id)
			if ag == nil {
				return dispatchedMsg{id: id} // removed meanwhile; so is its queue
			}
			text := it.Text
			var upto map[string]int
			yours := true // the person's own message; only mad's is counted
			if it.Bus {   // a pointer: say what waits on the bus now
				t, u, y, ok, err := bus.ComposeCounted(id, time.Now())
				if err != nil {
					_ = queue.PutBack(id, it, err.Error())
					return dispatchedMsg{id: id, err: err}
				}
				if !ok { // nothing to say any more
					_ = queue.Sent(id)
					return dispatchedMsg{id: id}
				}
				text, upto, yours = t, u, y
			}
			if err := sendQueued(st, ag, text, kinds); err != nil {
				_ = queue.PutBack(id, it, err.Error())
				return dispatchedMsg{id: id, err: err}
			}
			_ = queue.Sent(id)
			if it.Bus {
				_ = bus.Delivered(id, upto)
				if !yours {
					_ = bus.SpendSent(id, costs[id], now)
				}
			}
			if it.Bus && !yours {
				m.madNext.mark(id, time.Now())
			} else {
				m.madNext.clear(id)
			}
			return dispatchedMsg{id: id}
		}
	}
	return nil
}

func (m *model) applyDispatched(msg dispatchedMsg) tea.Cmd {
	m.dispatching = false
	if msg.spent {
		m.spend.Closed = true
	}
	if msg.wait > 0 {
		m.queueAt[msg.id] = time.Now().Add(msg.wait)
		return nil
	}
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
