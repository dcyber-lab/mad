package ui

import (
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// The sidebar raises follow-ups every minute with its own statuses, and the
// follow-up time and the on/off switch come from config.json's "bus".
func TestFollowCmd(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{{ID: "one", Kind: "claude"}, {ID: "two", Kind: "claude"}}
	m.rebuildRows()

	oldF, oldOn, oldAt := bus.FollowUp, bus.NudgesOn, bus.PersonAt
	oldFU, oldRep, oldW := checkFollowUps, checkReport, deckWatched
	t.Cleanup(func() {
		bus.FollowUp, bus.NudgesOn, bus.PersonAt = oldF, oldOn, oldAt
		checkFollowUps, checkReport, deckWatched = oldFU, oldRep, oldW
	})
	watched := false
	deckWatched = func() bool { return watched }
	bus.PersonAt = time.Time{}
	var gotStatuses map[string]string
	var gotAt time.Time
	var clock string
	var reported bool
	checkFollowUps = func(at time.Time, s map[string]string) error { gotAt, gotStatuses = at, s; return nil }
	checkReport = func(_ *state.State, _ map[string]string, _ time.Time, c string) error {
		reported, clock = true, c
		return nil
	}

	// Before the first poll the statuses are unknown: nothing runs.
	if cmd := m.followCmd(time.Now()); cmd != nil || m.following {
		t.Fatal("followed before any status was known")
	}

	m.trackers["one"] = &status.Tracker{Status: status.Idle}
	m.trackers["two"] = &status.Tracker{Status: status.Running}
	m.cfg.Bus.FollowUp, m.cfg.Bus.Report = 45*time.Minute, "08:30"
	at := time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC)
	cmd := m.followCmd(at)
	if cmd == nil || !m.following || !m.lastFollow.Equal(at) {
		t.Fatalf("no follow-up round started: cmd=%v following=%v", cmd != nil, m.following)
	}
	if _, ok := cmd().(followedMsg); !ok {
		t.Fatal("round did not end with followedMsg")
	}
	if !gotAt.Equal(at) || gotStatuses["one"] != status.Idle || gotStatuses["two"] != status.Running || !reported || clock != "08:30" {
		t.Errorf("follow-ups: at=%v statuses=%v reported=%v clock=%q", gotAt, gotStatuses, reported, clock)
	}
	if bus.FollowUp != 45*time.Minute || !bus.NudgesOn {
		t.Errorf("bus.FollowUp=%v NudgesOn=%v, want 45m on", bus.FollowUp, bus.NudgesOn)
	}
	if !bus.PersonAt.IsZero() {
		t.Errorf("the person seen with nobody at the deck: %v", bus.PersonAt)
	}
	// Someone at the deck: they are here, as of this round.
	watched = true
	m.Update(followedMsg{})
	m.followCmd(at)()
	if !bus.PersonAt.Equal(at) {
		t.Errorf("PersonAt=%v with the deck watched, want %v", bus.PersonAt, at)
	}
	m.Update(followedMsg{})
	if m.following {
		t.Error("still following after the round ended")
	}

	// "follow_up": "0" turns the nudges off and leaves the board's time.
	m.cfg.Bus.FollowUp = 0
	m.followCmd(at)
	if bus.NudgesOn || bus.FollowUp != 45*time.Minute {
		t.Errorf("off: NudgesOn=%v FollowUp=%v", bus.NudgesOn, bus.FollowUp)
	}
}

// A tick starts a round when the last was a minute ago, and not before.
func TestTickStartsFollowUps(t *testing.T) {
	m, _ := setup(t, "/code/a")
	oldFU, oldW := checkFollowUps, deckWatched
	t.Cleanup(func() { checkFollowUps, deckWatched = oldFU, oldW })
	deckWatched = func() bool { return false }
	m.lastFollow = time.Now()
	m.Update(tickMsg(time.Now()))
	if m.following {
		t.Error("followed again within a minute")
	}
	m.lastFollow = time.Now().Add(-followEvery)
	m.Update(tickMsg(time.Now()))
	if !m.following {
		t.Error("tick a minute after the last round did not start one")
	}
}
