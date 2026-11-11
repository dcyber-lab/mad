package rounds

import (
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/status"
)

// Send types a message into an agent (drive.Send); replaced in tests.
var Send = drive.Send

// Dispatched ends one try at sending agent ID its next queued message.
type Dispatched struct {
	ID  string
	Err error
	// Wait, when set, is how long the bus item must still wait for the
	// agent's minute; nothing was sent and it is not a failure.
	Wait time.Duration
	// Spent: the bus item was held by the day's budget.
	Spent bool
}

func (Dispatched) result() {}

// dispatch sends the first message queued for an agent whose turn is
// over: idle, or asleep or stopped (it is resumed for it). An agent that
// is working or waiting for the person keeps its queue, and so does a
// run's: its runner gives it its work.
func (r *Rounds) dispatch(now time.Time, facts func() Facts) *Job {
	r.lastDispatch = now
	r.queued = queue.Pending()
	if len(r.queued) == 0 {
		return nil
	}
	f := facts()
	for _, a := range f.State.OrderedAgents() {
		if r.queued[a.ID] == 0 || a.Run != "" || now.Before(r.queueAt[a.ID]) {
			continue
		}
		switch f.Agents[a.ID].Status { // "" for one the poll has not seen
		case status.Idle, status.Asleep, status.Stopped:
		default:
			continue
		}
		id, st, kinds, costs := a.ID, f.State.Clone(), f.Kinds, f.Costs
		r.dispatching = true
		return &Job{Stirs: true, Run: func() Result { // a woken agent changes the panes
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
					return Dispatched{ID: id, Wait: wait, Spent: spent}
				}
			} else {
				it, ok, err = queue.Take(id)
			}
			if err != nil || !ok {
				return Dispatched{ID: id, Err: err}
			}
			_, ag := st.FindAgent(id)
			if ag == nil {
				return Dispatched{ID: id} // removed meanwhile; so is its queue
			}
			text := it.Text
			var upto map[string]int
			yours := true // the person's own message; only mad's is counted
			if it.Bus {   // a pointer: say what waits on the bus now
				t, u, y, ok, err := bus.ComposeCounted(id, time.Now())
				if err != nil {
					_ = queue.PutBack(id, it, err.Error())
					return Dispatched{ID: id, Err: err}
				}
				if !ok { // nothing to say any more
					_ = queue.Sent(id)
					return Dispatched{ID: id}
				}
				text, upto, yours = t, u, y
			}
			// Marked before the send: the agent may be seen running before it returns.
			if it.Bus && !yours {
				r.Mad.Mark(id, time.Now())
			} else {
				r.Mad.Clear(id)
			}
			if err := Send(st, ag, text, kinds); err != nil {
				r.Mad.Clear(id)
				_ = queue.PutBack(id, it, err.Error())
				return Dispatched{ID: id, Err: err}
			}
			_ = queue.Sent(id)
			if it.Bus {
				_ = bus.Delivered(id, upto)
				if !yours {
					_ = bus.SpendSent(id, costs[id], now)
				}
			}
			return Dispatched{ID: id}
		}}
	}
	return nil
}

// dispatched notes when the agent may be offered a message again: after
// the bus's wait, after a refusal, or once its status caught up with what
// was sent.
func (r *Rounds) dispatched(res Dispatched, now time.Time) Outcome {
	r.dispatching = false
	out := Outcome{Spent: res.Spent}
	if res.Wait > 0 {
		r.queueAt[res.ID] = now.Add(res.Wait)
		return out
	}
	if res.Err != nil {
		r.queueAt[res.ID] = now.Add(queueRetry)
		return out
	}
	r.queueAt[res.ID] = now.Add(queueSettle)
	if n := r.queued[res.ID]; n > 0 {
		r.queued[res.ID] = n - 1
	}
	out.Poll = true
	return out
}
