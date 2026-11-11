package rounds

import (
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// Watched tells whether someone is at the deck (tmux.Watched); replaced in
// tests.
var Watched = tmux.Watched

// Replaceable in tests.
var (
	checkLapses    = bus.CheckLapses
	checkFollowUps = bus.CheckFollowUps
	checkReport    = bus.CheckReport
	checkTimers    = bus.CheckTimers
	settleSpend    = bus.SpendSettle
)

// Lapsed ends one check of the handoffs for lapses.
type Lapsed struct{}

func (Lapsed) result() {}

// lapse marks the handoffs lapsed whose receiver's turn ended without
// bus_done (the bus wakes the agent that gave the work). Errors are ignored.
func (r *Rounds) lapse(now time.Time) *Job {
	r.lapsing, r.lastLapse = true, now
	return &Job{Run: func() Result {
		_ = checkLapses(now)
		return Lapsed{}
	}}
}

// Followed ends one round of follow-ups, with how the day's budget for
// mad's own work stands.
type Followed struct{ Spend bus.SpendState }

func (Followed) result() {}

// follow raises work that stands without a word (nudges, and the
// notifications for the person) and sends the progress report when it is
// due. The agents' statuses are the keeper's own. The follow-up time and
// whether to nudge at all come from config.json's "bus" object.
func (r *Rounds) follow(now time.Time, cfg Config, facts func() Facts) *Job {
	f := facts()
	if len(f.Agents) == 0 && len(f.State.OrderedAgents()) > 0 {
		return nil // no poll yet: statuses unknown, and a guess would look like news
	}
	r.following, r.lastFollow = true, now
	bus.NudgesOn = cfg.FollowUp > 0
	if bus.NudgesOn {
		bus.FollowUp = cfg.FollowUp
	}
	statuses := map[string]string{}
	for id, a := range f.Agents {
		if a.Status != "" {
			statuses[id] = a.Status
		}
	}
	st, clock, costs, viewed := f.State.Clone(), cfg.Report, f.Costs, f.Viewed
	return &Job{Run: func() Result {
		if viewed || Watched() {
			bus.PersonAt = now // here: nudging is not wasted on an empty room
		}
		_ = settleSpend(now, costs) // before the nudges: they ask whether the day's budget is spent
		_ = checkFollowUps(now, statuses)
		_ = checkTimers(now, statuses) // check-ins and routines: orders, not guesses
		_ = checkReport(st, statuses, now, clock)
		return Followed{Spend: bus.SpendNow(now)}
	}}
}
