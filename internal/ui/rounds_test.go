package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/rounds"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// A tick has the rounds made with what the sidebar knows of the deck: a
// message queued for an idle agent goes to it, the row counts what is
// left, and the sidebar looks at the panes again.
func TestTickHasTheRoundsMade(t *testing.T) {
	m, st := setup(t, "/code/a")
	m.cfg.SleepAfter = 0 // deliveries only: nobody is put to sleep
	st.Projects[0].Agents = []*state.Agent{{ID: "free", Kind: "claude"}, {ID: "busy", Kind: "claude"}}
	m.rebuildRows()
	m.trackers["free"] = &status.Tracker{Status: status.Idle}
	m.trackers["busy"] = &status.Tracker{Status: status.Running}
	now := time.Now()
	for _, text := range []string{"first", "second"} {
		if _, err := queue.Push("free", text, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := queue.Push("busy", "later", now); err != nil {
		t.Fatal(err)
	}
	var sent []string
	oldF, oldOn, oldAt := bus.FollowUp, bus.NudgesOn, bus.PersonAt
	t.Cleanup(func() { bus.FollowUp, bus.NudgesOn, bus.PersonAt = oldF, oldOn, oldAt })
	rounds.Send = func(_ *state.State, a *state.Agent, text string, _ []agent.Kind) error {
		sent = append(sent, a.ID+": "+text)
		return nil
	}

	epoch := m.epoch
	cmds := m.roundCmds(now)
	if len(cmds) == 0 {
		t.Fatal("a tick with a message queued for an idle agent made no round")
	}
	if m.epoch != epoch+1 {
		t.Errorf("epoch %d → %d: a delivery wakes an agent, so a poll under way is stale", epoch, m.epoch)
	}
	if !m.rounds.Dispatching() {
		t.Error("not dispatching: a rebuilt sidebar would leave in the middle of a send")
	}
	if !strings.Contains(m.View(), "⇢2") || !strings.Contains(m.View(), "⇢1") {
		t.Errorf("the rows do not count what waits:\n%s", m.View())
	}
	polled := false
	for _, cmd := range cmds {
		msg, ok := cmd().(roundMsg)
		if !ok {
			t.Fatalf("a round's job ended with %T", msg)
		}
		m.polling = false
		if _, next := m.Update(msg); next != nil {
			polled = true
		}
	}
	if len(sent) != 1 || sent[0] != "free: first" {
		t.Errorf("sent = %q", sent)
	}
	if !polled {
		t.Error("no poll after a message went in")
	}
	if m.rounds.Dispatching() {
		t.Error("still dispatching after the job ended")
	}
	if !strings.Contains(m.View(), "⇢1") || strings.Contains(m.View(), "⇢2") {
		t.Errorf("the row still counts the message that went:\n%s", m.View())
	}
	// Half a second on, nothing is due: no round, and the epoch stays.
	epoch = m.epoch
	if cmds := m.roundCmds(now.Add(500 * time.Millisecond)); len(cmds) != 0 || m.epoch != epoch {
		t.Errorf("half a second later: %d jobs, epoch %d → %d", len(cmds), epoch, m.epoch)
	}
}

// How a job of the rounds ended is the sidebar's to show: the day's budget
// in its footer, a failure as a flash, and a poll when panes changed.
func TestRoundOutcomes(t *testing.T) {
	m, _ := setup(t, "/code/a")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30}) // room for the footer's line
	// A bus item held by the budget: the footer says the agents wait.
	if _, cmd := m.Update(roundMsg{rounds.Dispatched{ID: "a", Wait: time.Minute, Spent: true}}); cmd != nil || !m.spend.Closed {
		t.Errorf("a held item: closed=%v, polled=%v", m.spend.Closed, cmd != nil)
	}
	// The follow-ups say how the budget stands.
	m.Update(roundMsg{rounds.Followed{Spend: bus.SpendState{Turns: 150, BudgetTurns: 150, Closed: true}}})
	if m.spend.Turns != 150 || !strings.Contains(m.View(), "budget 150 turns spent · agents wait") {
		t.Errorf("spend = %+v, footer:\n%s", m.spend, m.View())
	}
	m.Update(roundMsg{rounds.Followed{}})
	if m.spend.Closed || strings.Contains(m.View(), "agents wait") {
		t.Errorf("the budget opened again, the footer did not: %+v", m.spend)
	}
	// A sleep round that failed says why, and the panes are looked at.
	m.polling = false
	_, cmd := m.Update(roundMsg{rounds.Slept{Err: errors.New("no such pane")}})
	if cmd == nil || !m.flashErr || !strings.Contains(m.flash, "no such pane") {
		t.Errorf("a failed sleep round: polled=%v flash=%q err=%v", cmd != nil, m.flash, m.flashErr)
	}
	// A lapse check ends without a word.
	m.flash = ""
	if _, cmd := m.Update(roundMsg{rounds.Lapsed{}}); cmd != nil || m.flash != "" {
		t.Errorf("a lapse check: polled=%v flash=%q", cmd != nil, m.flash)
	}
}
