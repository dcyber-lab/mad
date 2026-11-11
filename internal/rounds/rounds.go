// Package rounds is the work the deck does by itself, with nobody at the
// keys. Every second it looks for an agent whose turn is over and that has
// a message waiting, and types it in: one the person queued, or what the
// bus has for it. Every ten seconds it marks lapsed the handoffs whose
// receiver stopped short. Every minute it raises work that stands without
// a word, fires the timers that are due, settles the day's spending and
// considers the progress report. Every half minute it puts to sleep the
// agents that have sat idle long enough.
//
// It draws nothing and has no loop of its own. Whoever keeps the deck (the
// sidebar, today) calls Due on each tick, with what its own poll found
// (Facts), runs the jobs it gets off its loop, and gives each result to
// Done, which says what the keeper is to do about it. A keeper with no
// terminal can make the same rounds.
//
// Still the sidebar's own, and not here: working out each agent's status
// and the notifications that follow from it, starting a run's runner
// again, and ending the requests nobody answered.
package rounds

import (
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
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
	// sleepCheckEvery is how often agents are checked against sleep.after.
	sleepCheckEvery = 30 * time.Second
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

// Config is what config.json says of the rounds.
type Config struct {
	// SleepAfter is how long an agent sits idle off stage before it is
	// put to sleep; 0: never.
	SleepAfter time.Duration
	// FollowUp is how long work stands without a word before it is
	// raised; 0 turns the nudges off.
	FollowUp time.Duration
	// Report is the time of day of the progress report ("09:00").
	Report string
}

// Facts are the deck as its keeper's last poll found it. Nothing in them
// is read again here.
type Facts struct {
	// State is the keeper's own tree: read on its goroutine, and copied
	// for a job.
	State *state.State
	Kinds []agent.Kind
	// Agents is how each agent the poll has seen stands, by id. None at
	// all while State has agents: no poll was taken yet.
	Agents map[string]Agent
	// RunActive tells whether a run still works, and so drives its
	// agents itself.
	RunActive func(run string) bool
	// Costs are the agents' costs so far (claude's; the others cost
	// nothing here and only count turns).
	Costs map[string]float64
	// Viewed: a desktop window shows one of the agents, so the person is
	// there.
	Viewed bool
}

// Agent is how one agent stands.
type Agent struct {
	Status string
	// QuietSince is when it last did or showed anything (status.Tracker).
	QuietSince time.Time
	// Seen: it is in front of someone: on stage, in a desktop window, or
	// its diff is on stage.
	Seen bool
}

// A Job is a piece of work a round found due. Run does it and takes as
// long as it takes: it is for another goroutine than the keeper's. What it
// returns goes to Done.
type Job struct {
	// Stirs: the work may start or stop an agent, so a poll under way
	// may list panes that are no longer so.
	Stirs bool
	Run   func() Result
}

// Result is how a job ended: Dispatched, Lapsed, Followed or Slept.
type Result interface{ result() }

// Outcome is what the keeper is to do once a job is done.
type Outcome struct {
	// Poll: an agent was sent to, or put to sleep: look at the panes now.
	Poll bool
	// Spend is how the day's budget for mad's own work stands, when the
	// job learnt it.
	Spend *bus.SpendState
	// Spent: a bus item was held, the day's budget being spent.
	Spent bool
	// Err is a failure to tell the person of.
	Err error
}

// Rounds is where the rounds stand between ticks: when each was last
// made, which is under way, and what the dispatcher keeps of each agent.
// Its methods are for the keeper's one goroutine; a Job's Run is not.
type Rounds struct {
	// Mad says which agents' next turn is mad's own.
	Mad Marks

	sleeping       bool      // idle agents are being put to sleep
	lastSleepCheck time.Time // when they were last looked for

	dispatching  bool                 // a queued message is being sent
	lastDispatch time.Time            // when the queues were last looked at
	queued       map[string]int       // agent id → messages waiting (not paused)
	queueAt      map[string]time.Time // agent id → not offered a message before

	lapsing   bool      // handoffs are being checked for lapses
	lastLapse time.Time // when they were last checked

	following  bool      // follow-ups are being raised
	lastFollow time.Time // when they were last
}

// New is the rounds before the first was made: all are due.
func New() *Rounds {
	return &Rounds{queued: map[string]int{}, queueAt: map[string]time.Time{}}
}

// Due makes the rounds that are due at now and returns the jobs they found,
// none when there is nothing to do. facts is asked for the deck when a
// round needs it, once at most.
func (r *Rounds) Due(now time.Time, cfg Config, facts func() Facts) []Job {
	var got *Facts
	once := func() Facts {
		if got == nil {
			f := facts()
			got = &f
		}
		return *got
	}
	var jobs []Job
	add := func(j *Job) {
		if j != nil {
			jobs = append(jobs, *j)
		}
	}
	if cfg.SleepAfter > 0 && !r.sleeping && now.Sub(r.lastSleepCheck) >= sleepCheckEvery {
		add(r.sleep(now, cfg, once))
	}
	if !r.dispatching && now.Sub(r.lastDispatch) >= dispatchEvery {
		add(r.dispatch(now, once))
	}
	if !r.lapsing && now.Sub(r.lastLapse) >= lapseEvery {
		add(r.lapse(now))
	}
	if !r.following && now.Sub(r.lastFollow) >= followEvery {
		add(r.follow(now, cfg, once))
	}
	return jobs
}

// Done takes in how a job ended, at now.
func (r *Rounds) Done(res Result, now time.Time) Outcome {
	switch res := res.(type) {
	case Dispatched:
		return r.dispatched(res, now)
	case Lapsed:
		r.lapsing = false
	case Followed:
		r.following = false
		return Outcome{Spend: &res.Spend}
	case Slept:
		r.sleeping = false
		return Outcome{Poll: true, Err: res.Err}
	}
	return Outcome{}
}

// Dispatching: a message is being typed into an agent right now. A keeper
// that leaves meanwhile cuts it short, and the message is lost.
func (r *Rounds) Dispatching() bool { return r.dispatching }

// Queued is how many messages wait for agent id, as of the last look at
// the queues (paused ones not counted).
func (r *Rounds) Queued(id string) int { return r.queued[id] }
