package rounds

import (
	"errors"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// SleepAgent puts an agent to sleep (deck.SleepAgent); replaced in tests.
var SleepAgent = deck.SleepAgent

// Slept ends a round of putting idle agents to sleep.
type Slept struct{ Err error }

func (Slept) result() {}

// CanSleep: the kind can reopen the agent's session, so ending its process
// loses nothing but what the process held. A kind that names its sessions
// itself (no {id} in start) needs the agent's to be known: reopening "the
// latest" could take another agent's.
func CanSleep(kinds []agent.Kind, a *state.Agent) bool {
	k := agent.ByName(kinds, a.Kind)
	return k.Resume != "" && (a.SessionID != "" || strings.Contains(k.Start, "{id}"))
}

// Sleepers are the agents that have sat idle off stage for after. One
// whose diff is on stage counts as on stage: closing the diff brings it
// back.
func Sleepers(now time.Time, after time.Duration, f Facts) []string {
	var ids []string
	for _, a := range f.State.OrderedAgents() {
		ag, ok := f.Agents[a.ID]
		// A working run comes back to its agents; it would only wake them.
		if !ok || ag.Status != status.Idle || ag.Seen || !CanSleep(f.Kinds, a) || a.Run != "" && f.RunActive != nil && f.RunActive(a.Run) {
			continue
		}
		if now.Sub(ag.QuietSince) >= after {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// sleep puts the agents that have been idle long enough to sleep, one at
// a time. One that is busy is left for the next check.
func (r *Rounds) sleep(now time.Time, cfg Config, facts func() Facts) *Job {
	r.lastSleepCheck = now
	ids := Sleepers(now, cfg.SleepAfter, facts())
	if len(ids) == 0 {
		return nil
	}
	r.sleeping = true
	return &Job{Stirs: true, Run: func() Result {
		for _, id := range ids {
			var busy *deck.BusyError
			if _, err := SleepAgent(id, true); err != nil && !errors.As(err, &busy) {
				return Slept{Err: err}
			}
		}
		return Slept{}
	}}
}
