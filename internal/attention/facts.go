package attention

import (
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// Agent is what the inbox knows of one agent, as one poll has it. Make it
// with AgentOf: the window, the deck and `mad board` each have trackers
// of their own, and AgentOf turns what they pass into one shape with one
// set of rules.
type Agent struct {
	ID      string
	Label   string // drive.Label: its name on the bus
	Project string // the project's name
	Dir     string // where it runs
	PaneID  string // its tmux pane, "" without one: Mark seen marks it
	// Status is its tracked (raw) status; Shown what is drawn, a run's
	// role never Finished (its run says when it is done).
	Status string
	Shown  string
	Since  time.Time // Tracker.Since
	Report status.Report
	Info   transcript.Info
}

// AgentOf is agent a of project p as tracker tr saw it on this poll, with
// its report and its transcript as last read. tr may be nil (not seen
// yet): a stopped agent.
func AgentOf(p *state.Project, a *state.Agent, tr *status.Tracker, paneID string, rep status.Report, info transcript.Info) Agent {
	raw, done, since := status.Stopped, false, time.Time{}
	if tr != nil && tr.Status != "" {
		raw, done, since = tr.Status, tr.Done(), tr.Since()
	}
	if p.InRun(a) {
		done = false
	}
	return Agent{ID: a.ID, Label: drive.Label(a), Project: p.Name, Dir: p.Dir(a), PaneID: paneID,
		Status: raw, Shown: status.Shown(raw, done), Since: since, Report: rep, Info: info}
}

// Facts is everything a View is built from: what the caller's poll read.
type Facts struct {
	// Agents are every agent of the deck, in the deck's order.
	Agents []Agent
	// Cards are bus.Board's as of the caller's last look at the bus,
	// which may be older than Agents: the inbox takes agents' waits from
	// Agents, never from a card, so a stale card cannot make or hide one.
	Cards []bus.Card
	// Skills are the skills waiting for approval (skill.PendingList).
	Skills []skill.Pending
	// Spend is the day's budget for mad's own work.
	Spend bus.SpendState
	Now   time.Time
}

// Statuses are the agents' raw statuses by id, as bus.Board and
// bus.FireTimer take them.
func (f Facts) Statuses() map[string]string { return Statuses(f.Agents) }

// Statuses are agents' raw statuses by id.
func Statuses(agents []Agent) map[string]string {
	out := make(map[string]string, len(agents))
	for _, a := range agents {
		out[a.ID] = a.Status
	}
	return out
}

// View is what a poll draws: the inbox, every agent's activity line, the
// working agents in order, and the board without what the inbox took.
type View struct {
	Inbox Inbox
	// Activity is each agent's activity line, by id.
	Activity map[string]Activity
	// Working are the ids of the agents shown working, longest in that
	// state first, so a stall floats up.
	Working []string
	// Board is Cards without the waiting-you column: the inbox is that
	// column. Every waiting-you card became an item or was folded into
	// one.
	Board []bus.Card
}
