package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/rounds"
)

// The sidebar keeps the deck: on each tick it has the rounds made that are
// due (package rounds: queued messages and bus digests sent, lapsed
// handoffs marked, work followed up, idle agents put to sleep), with what
// its own poll found, and takes in how each job ended.

// roundMsg ends a job of the rounds.
type roundMsg struct{ res rounds.Result }

// roundCmds are the jobs of the rounds due at now, each to run off the UI
// goroutine.
func (m *model) roundCmds(now time.Time) []tea.Cmd {
	cfg := rounds.Config{SleepAfter: m.cfg.SleepAfter, FollowUp: m.cfg.Bus.FollowUp, Report: m.cfg.Bus.Report}
	var cmds []tea.Cmd
	for _, job := range m.rounds.Due(now, cfg, m.roundFacts) {
		if job.Stirs {
			m.epoch++ // a woken or sleeping agent changes the panes
		}
		run := job.Run
		cmds = append(cmds, func() tea.Msg { return roundMsg{run()} })
	}
	return cmds
}

// roundFacts are the deck as the sidebar has it, for a round: the agents'
// statuses are its trackers'.
func (m *model) roundFacts() rounds.Facts {
	f := rounds.Facts{State: m.st, Kinds: m.kinds, Agents: make(map[string]rounds.Agent, len(m.trackers)), RunActive: m.runActive, Costs: m.costs()}
	for id, tr := range m.trackers {
		if tr != nil {
			f.Agents[id] = rounds.Agent{Status: tr.Status, QuietSince: tr.QuietSince(), Seen: m.seen(id)}
		}
	}
	for _, p := range m.panes {
		f.Viewed = f.Viewed || p.Viewed // in a desktop window
	}
	return f
}

// applyRound takes in how a job of the rounds ended.
func (m *model) applyRound(res rounds.Result) tea.Cmd {
	now := time.Now()
	out := m.rounds.Done(res, now)
	if out.Spent {
		m.spend.Closed = true
	}
	if out.Spend != nil {
		m.spend = *out.Spend
		m.look(now)
	}
	if out.Err != nil {
		m.setFlashErr(out.Err.Error())
	}
	if out.Poll {
		return m.pollNow()
	}
	return nil
}

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

// spendFooter is the footer's line once the day's budget is spent.
func spendFooter(s bus.SpendState) string {
	if s.BudgetUSD > 0 && s.USD >= s.BudgetUSD {
		return fmt.Sprintf("budget $%.0f spent · agents wait", s.USD)
	}
	return fmt.Sprintf("budget %d turns spent · agents wait", s.Turns)
}
