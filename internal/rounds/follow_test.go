package rounds

import (
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// Follow-ups are raised with the keeper's statuses, and the follow-up time
// and the on/off switch come from config.json's "bus".
func TestFollow(t *testing.T) {
	r, st, set, facts := newDeck(t)
	st.Projects[0].Agents = []*state.Agent{{ID: "one", Kind: "claude"}, {ID: "two", Kind: "claude"}}

	oldF, oldOn, oldAt := bus.FollowUp, bus.NudgesOn, bus.PersonAt
	oldFU, oldRep, oldW, oldTm := checkFollowUps, checkReport, Watched, checkTimers
	t.Cleanup(func() {
		bus.FollowUp, bus.NudgesOn, bus.PersonAt = oldF, oldOn, oldAt
		checkFollowUps, checkReport, Watched, checkTimers = oldFU, oldRep, oldW, oldTm
	})
	watched := false
	Watched = func() bool { return watched }
	bus.PersonAt = time.Time{}
	var gotStatuses map[string]string
	var gotAt time.Time
	var clock string
	var reported bool
	var timersAt time.Time
	checkTimers = func(at time.Time, _ map[string]string) error { timersAt = at; return nil }
	checkFollowUps = func(at time.Time, s map[string]string) error { gotAt, gotStatuses = at, s; return nil }
	checkReport = func(_ *state.State, _ map[string]string, _ time.Time, c string) error {
		reported, clock = true, c
		return nil
	}

	// Before the first poll the statuses are unknown: nothing runs.
	var cfg Config
	if job := r.follow(time.Now(), cfg, facts); job != nil || r.following {
		t.Fatal("followed before any status was known")
	}

	set("one", status.Idle)
	set("two", status.Running)
	cfg.FollowUp, cfg.Report = 45*time.Minute, "08:30"
	at := time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC)
	job := r.follow(at, cfg, facts)
	if job == nil || !r.following || !r.lastFollow.Equal(at) {
		t.Fatalf("no follow-up round started: job=%v following=%v", job != nil, r.following)
	}
	if _, ok := job.Run().(Followed); !ok {
		t.Fatal("round did not end with Followed")
	}
	if !gotAt.Equal(at) || gotStatuses["one"] != status.Idle || gotStatuses["two"] != status.Running || !reported || clock != "08:30" {
		t.Errorf("follow-ups: at=%v statuses=%v reported=%v clock=%q", gotAt, gotStatuses, reported, clock)
	}
	if !timersAt.Equal(at) {
		t.Errorf("timers checked at %v, want %v", timersAt, at)
	}
	if bus.FollowUp != 45*time.Minute || !bus.NudgesOn {
		t.Errorf("bus.FollowUp=%v NudgesOn=%v, want 45m on", bus.FollowUp, bus.NudgesOn)
	}
	if !bus.PersonAt.IsZero() {
		t.Errorf("the person seen with nobody at the deck: %v", bus.PersonAt)
	}
	// Someone at the deck: they are here, as of this round.
	watched = true
	r.Done(Followed{}, at)
	r.follow(at, cfg, facts).Run()
	if !bus.PersonAt.Equal(at) {
		t.Errorf("PersonAt=%v with the deck watched, want %v", bus.PersonAt, at)
	}
	// The round's end says how the day's budget stands.
	if out := r.Done(Followed{Spend: bus.SpendState{Turns: 3}}, at); out.Spend == nil || out.Spend.Turns != 3 {
		t.Errorf("the budget was not passed on: %+v", out)
	}
	if r.following {
		t.Error("still following after the round ended")
	}
	// A window showing an agent is the person being there too.
	watched, bus.PersonAt = false, time.Time{}
	shown := func() Facts { f := facts(); f.Viewed = true; return f }
	r.follow(at, cfg, shown).Run()
	r.Done(Followed{}, at)
	if !bus.PersonAt.Equal(at) {
		t.Errorf("PersonAt=%v with an agent in a window, want %v", bus.PersonAt, at)
	}

	// "follow_up": "0" turns the nudges off and leaves the board's time.
	cfg.FollowUp = 0
	r.follow(at, cfg, facts)
	if bus.NudgesOn || bus.FollowUp != 45*time.Minute {
		t.Errorf("off: NudgesOn=%v FollowUp=%v", bus.NudgesOn, bus.FollowUp)
	}
}

// A tick starts a round when the last was a minute ago, and not before.
func TestDueStartsFollowUps(t *testing.T) {
	r, _, _, facts := newDeck(t)
	oldFU := checkFollowUps
	t.Cleanup(func() { checkFollowUps = oldFU })
	now := time.Now()
	r.lastFollow = now
	r.Due(now, Config{}, facts)
	if r.following {
		t.Error("followed again within a minute")
	}
	r.lastFollow = now.Add(-followEvery)
	r.Due(now, Config{}, facts)
	if !r.following {
		t.Error("tick a minute after the last round did not start one")
	}
}
