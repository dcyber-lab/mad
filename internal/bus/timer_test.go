package bus

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

var (
	// base is the clock of the bus tests (setNow's zero), a Friday.
	base = time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC)
	// ny has a change of daylight time on 2026-11-01.
	ny = mustZone("America/New_York")
)

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func TestParseSchedule(t *testing.T) {
	ok := func(in string, check func(Schedule) bool) {
		t.Helper()
		sc, err := ParseSchedule(in, base)
		if err != nil || !check(sc) {
			t.Errorf("ParseSchedule(%q) = %+v, %v", in, sc, err)
		}
	}
	day := func(wd ...time.Weekday) (d [7]bool) {
		for _, w := range wd {
			d[w] = true
		}
		return d
	}
	ok("in 20m", func(s Schedule) bool { return s.Once && s.At.Equal(base.Add(20*time.Minute)) })
	ok("in 2h", func(s Schedule) bool { return s.Once && s.At.Equal(base.Add(2*time.Hour)) })
	ok("IN 1h30m", func(s Schedule) bool { return s.Once && s.At.Equal(base.Add(90*time.Minute)) })
	ok("at 15:00", func(s Schedule) bool { return s.Once && s.At.Equal(time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)) })
	// A time that has passed today is tomorrow's.
	ok("at 09:00", func(s Schedule) bool { return s.Once && s.At.Equal(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)) })
	ok("at 14:02", func(s Schedule) bool { return s.Once && s.At.Day() == 31 })
	ok("at 2026-11-02 09:00", func(s Schedule) bool { return s.Once && s.At.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)) })
	ok("daily 09:00", func(s Schedule) bool { return !s.Once && s.Clock == 540 && s.Days == day(0, 1, 2, 3, 4, 5, 6) })
	ok("weekdays 09:00", func(s Schedule) bool { return s.Clock == 540 && s.Days == day(1, 2, 3, 4, 5) })
	ok("weekends 10:00", func(s Schedule) bool { return s.Clock == 600 && s.Days == day(0, 6) })
	ok("mon,wed,fri 09:00", func(s Schedule) bool { return s.Days == day(1, 3, 5) })
	ok("Mon 9:30", func(s Schedule) bool { return s.Days == day(1) && s.Clock == 570 })
	ok("every 30m", func(s Schedule) bool { return s.Clock < 0 && s.Every == 30*time.Minute && s.From < 0 })
	ok("every 2h 08:00-18:00", func(s Schedule) bool { return s.Every == 2*time.Hour && s.From == 480 && s.To == 1080 })
	ok("every 1h weekdays", func(s Schedule) bool { return s.Every == time.Hour && s.Days == day(1, 2, 3, 4, 5) })
	ok("every 1h 09:00-17:00 mon", func(s Schedule) bool { return s.Days == day(1) && s.From == 540 })

	for _, bad := range []string{"", "in", "in 4m", "in 25h", "in soon", "at", "at 25:00", "at tomorrow", "at 2020-01-01 09:00",
		"daily", "daily 24:00", "daily 9", "funday 09:00", "weekdays 09:00 extra", "every", "every 30s", "every 0m",
		"every 30m 18:00-08:00", "every 30m nowhere", "mo,we 09:00"} {
		if sc, err := ParseSchedule(bad, base); err == nil {
			t.Errorf("ParseSchedule(%q) = %+v, want an error", bad, sc)
		}
	}
}

func TestScheduleNext(t *testing.T) {
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, ny) }
	next := func(spec string, after time.Time) time.Time {
		t.Helper()
		sc, err := ParseSchedule(spec, after)
		if err != nil {
			t.Fatal(err)
		}
		return sc.Next(after)
	}
	check := func(spec string, after, want time.Time) {
		t.Helper()
		if got := next(spec, after); !got.Equal(want) {
			t.Errorf("%q after %v = %v, want %v", spec, after, got, want)
		}
	}
	fri := at(2026, 10, 30, 10, 0) // a Friday
	// Weekdays skip the weekend; weekends skip the week.
	check("weekdays 09:00", fri, at(2026, 11, 2, 9, 0))
	check("weekdays 09:00", at(2026, 10, 30, 8, 0), at(2026, 10, 30, 9, 0))
	check("weekends 10:00", fri, at(2026, 10, 31, 10, 0))
	check("mon,wed,fri 09:00", fri, at(2026, 11, 2, 9, 0))
	check("mon 09:00", at(2026, 11, 2, 9, 0), at(2026, 11, 9, 9, 0)) // strictly after
	// Daylight time ended on 2026-11-01: 09:00 stays 09:00 on the wall.
	sat := at(2026, 10, 31, 10, 0)
	got := next("daily 09:00", sat)
	if !got.Equal(at(2026, 11, 1, 9, 0)) || got.Sub(sat) != 24*time.Hour { // 14:00Z both days
		t.Errorf("across the change: %v after %v is %v later", got, sat, got.Sub(sat))
	}
	// And in spring (2027-03-14): 09:00 EDT is 23h after 09:00 EST.
	d1 := at(2027, 3, 13, 9, 0)
	d2 := next("daily 09:00", d1)
	if !d2.Equal(at(2027, 3, 14, 9, 0)) || d2.Sub(d1) != 23*time.Hour {
		t.Errorf("spring: %v after %v (%v)", d2, d1, d2.Sub(d1))
	}
	// Intervals: from now, or inside the window, on the day's slots.
	check("every 30m", fri, at(2026, 10, 30, 10, 30))
	check("every 2h 08:00-18:00", at(2026, 10, 30, 7, 0), at(2026, 10, 30, 8, 0))
	check("every 2h 08:00-18:00", at(2026, 10, 30, 8, 0), at(2026, 10, 30, 10, 0))
	check("every 2h 08:00-18:00", at(2026, 10, 30, 17, 0), at(2026, 10, 30, 18, 0))
	check("every 2h 08:00-18:00", at(2026, 10, 30, 18, 0), at(2026, 10, 31, 8, 0))
	check("every 2h 08:00-18:00 weekdays", at(2026, 10, 30, 18, 0), at(2026, 11, 2, 8, 0))
	check("every 1h weekdays", at(2026, 10, 30, 23, 30), at(2026, 11, 2, 0, 0))
	// A once is its time, once.
	sc, _ := ParseSchedule("in 20m", fri)
	if !sc.Next(fri).Equal(fri.Add(20*time.Minute)) || !sc.Next(fri.Add(time.Hour)).IsZero() {
		t.Error("a once fires once")
	}
}

// timerPosts are the timer posts in channel ch.
func timerPosts(t *testing.T, ch string) []Message {
	t.Helper()
	msgs, _ := readMsgs(ch)
	var out []Message
	for _, m := range msgs {
		if m.Kind == "timer" {
			out = append(out, m)
		}
	}
	return out
}

func tick(t *testing.T, d time.Duration, statuses map[string]string) {
	t.Helper()
	setNow(d)
	if err := CheckTimers(now(), statuses); err != nil {
		t.Fatal(err)
	}
}

func mustCheckin(t *testing.T, id, spec, ch, note string) Timer {
	t.Helper()
	tm, err := AddCheckin(id, spec, ch, note, now())
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func mustRoutine(t *testing.T, sp RoutineSpec) Timer {
	t.Helper()
	tm, err := AddRoutine(sp, now())
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func timerByID(t *testing.T, id string) (Timer, bool) {
	t.Helper()
	ts, _ := Timers()
	for _, x := range ts {
		if x.ID == id {
			return x, true
		}
	}
	return Timer{}, false
}

func TestCheckinFiresOnceAndWakes(t *testing.T) {
	followSetup(t)
	dm := DMChannel(idBuild, You)
	tm := mustCheckin(t, idBuild, "in 20m", "", "check CI on PR 42")
	if tm.Kind != TimerOnce || tm.Channel != dm || !tm.NextAt.Equal(base.Add(20*time.Minute)) {
		t.Fatalf("checkin = %+v", tm)
	}
	tick(t, 19*time.Minute, idleBoth)
	if len(timerPosts(t, dm)) != 0 {
		t.Fatal("fired early")
	}
	tick(t, 20*time.Minute, idleBoth)
	tick(t, 20*time.Minute, idleBoth) // the same minute again
	tick(t, 21*time.Minute, idleBoth)
	ps := timerPosts(t, dm)
	if len(ps) != 1 {
		t.Fatalf("posts = %+v", ps)
	}
	m := ps[0]
	if m.From.ID != "mad" || len(m.Mentions) != 1 || m.Mentions[0] != idBuild ||
		m.Text != "[mad] check-in for @builder (booked 14:02, in 20m): check CI on PR 42" {
		t.Errorf("post = %+v", m)
	}
	if pendingBus(t, idBuild) != 1 {
		t.Error("the agent was not woken")
	}
	if len(told) != 0 {
		t.Errorf("a check-in told the person: %q", told)
	}
	if _, ok := timerByID(t, tm.ID); ok {
		t.Error("a fired check-in stays")
	}
	if data, _ := os.ReadFile(timerArchive()); !strings.Contains(string(data), tm.ID) {
		t.Error("a fired check-in is not archived")
	}
}

func TestRoutinePostsAndRepeats(t *testing.T) {
	followSetup(t)
	r := mustRoutine(t, RoutineSpec{Every: "Every  15m", Channel: "#dev", Text: "@builder how is the 404 case?"})
	if r.Every != "every 15m" || len(r.Mentions) != 1 || r.Mentions[0] != idBuild {
		t.Fatalf("routine = %+v", r)
	}
	tick(t, 14*time.Minute, idleBoth)
	tick(t, 15*time.Minute, idleBoth)
	tick(t, 15*time.Minute, idleBoth)
	ps := timerPosts(t, "dev")
	if len(ps) != 1 || ps[0].Text != "@builder how is the 404 case?" || ps[0].From.ID != "mad" {
		t.Fatalf("posts = %+v", ps)
	}
	if pendingBus(t, idBuild) != 1 || pendingBus(t, idArch) != 0 {
		t.Errorf("woke build=%d arch=%d", pendingBus(t, idBuild), pendingBus(t, idArch))
	}
	got, _ := timerByID(t, r.ID)
	if got.Fired != 1 || !got.LastFired.Equal(now()) || !got.NextAt.Equal(now().Add(15*time.Minute)) {
		t.Errorf("after firing: %+v", got)
	}
	tick(t, 30*time.Minute, idleBoth)
	if len(timerPosts(t, "dev")) != 2 {
		t.Error("did not repeat")
	}
	// Paused: nothing; resumed: from its next slot, not a backlog.
	if err := PauseTimer(r.ID, true, now()); err != nil {
		t.Fatal(err)
	}
	tick(t, 2*time.Hour, idleBoth)
	if len(timerPosts(t, "dev")) != 2 {
		t.Error("a paused routine fired")
	}
	if err := PauseTimer(r.ID, false, now()); err != nil {
		t.Fatal(err)
	}
	tick(t, 2*time.Hour+14*time.Minute, idleBoth)
	tick(t, 2*time.Hour+15*time.Minute, idleBoth)
	if n := len(timerPosts(t, "dev")); n != 3 {
		t.Errorf("posts after resume = %d, want 3", n)
	}
}

func TestReportRoutineWakesNoOne(t *testing.T) {
	followSetup(t)
	doingTask(t, "add the 404 case")
	drain(t, idBuild)
	mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Report: true})
	tick(t, 15*time.Minute, idleBoth)
	msgs, _ := readMsgs("dev")
	last := msgs[len(msgs)-1]
	if last.Kind != "system" || !strings.Contains(last.Text, "dev: 1 doing") {
		t.Errorf("last = %+v", last)
	}
	if pendingBus(t, idBuild) != 0 || len(timerPosts(t, "dev")) != 0 {
		t.Error("a report woke someone")
	}
}

func TestTimersAfterDowntime(t *testing.T) {
	followSetup(t)
	dm := DMChannel(idBuild, You)
	mustCheckin(t, idBuild, "in 20m", "", "check CI")
	r := mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@architect standup"})
	// The deck was down for three hours.
	tick(t, 3*time.Hour, idleBoth)
	ps := timerPosts(t, dm)
	if len(ps) != 1 || !strings.Contains(ps[0].Text, "late by 2h40m") {
		t.Fatalf("check-in posts = %+v", ps)
	}
	// The routine is skipped once, with a miss; the next slot is ahead.
	got, _ := timerByID(t, r.ID)
	if len(timerPosts(t, "dev")) != 0 || got.Missed != 1 || got.Fired != 0 || !got.NextAt.Equal(now().Add(15*time.Minute)) || !strings.Contains(got.Why, "missed") {
		t.Errorf("routine = %+v, posts %d", got, len(timerPosts(t, "dev")))
	}
	tick(t, 3*time.Hour+15*time.Minute, idleBoth)
	if got, _ = timerByID(t, r.ID); got.Fired != 1 || got.Missed != 1 || len(timerPosts(t, "dev")) != 1 {
		t.Errorf("routine on time = %+v", got)
	}
	// Less than an hour late still fires.
	tick(t, 3*time.Hour+15*time.Minute+50*time.Minute, idleBoth)
	if got, _ = timerByID(t, r.ID); got.Fired != 2 || got.Missed != 1 {
		t.Errorf("routine 35m late = %+v", got)
	}
}

func pauseChan(t *testing.T, ch string) {
	t.Helper()
	if err := change(ch, func(c *Channel, _ []Message) error { c.Paused = true; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestTimersHeldInAPausedChannel(t *testing.T) {
	followSetup(t)
	ci := mustCheckin(t, idBuild, "in 20m", "dev", "rerun the flaky test")
	r := mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@builder ping to keep up"})
	pause(t, "dev")
	tick(t, 20*time.Minute, idleBoth)
	if len(timerPosts(t, "dev")) != 0 {
		t.Fatal("posted in a paused channel")
	}
	got, ok := timerByID(t, ci.ID)
	if !ok || got.Held != "paused" || got.Why != "held: #dev paused" {
		t.Errorf("check-in = %+v", got)
	}
	// The routine counted a miss for the 15m slot, and the 30m slot.
	if rr, _ := timerByID(t, r.ID); rr.Missed != 1 || rr.Fired != 0 {
		t.Errorf("routine = %+v", rr)
	}
	// Held, it is not a card (the channel's own card says it waits).
	for _, c := range board(t, 20*time.Minute, idleBoth) {
		if c.Kind == CardCheckin {
			t.Errorf("a card for a check-in held in a paused channel: %+v", c)
		}
	}
	// A person resumes the channel: it fires on the next minute, late.
	if err := Resume("dev"); err != nil {
		t.Fatal(err)
	}
	tick(t, 25*time.Minute, idleBoth)
	ps := timerPosts(t, "dev")
	if len(ps) < 1 || !strings.Contains(ps[0].Text, "rerun the flaky test") || !strings.Contains(ps[0].Text, "late by 5m") {
		t.Fatalf("after resume: %+v", ps)
	}
	if _, ok := timerByID(t, ci.ID); ok {
		t.Error("the check-in stays after firing")
	}
}

func TestCheckinHeldForTooLongIsDropped(t *testing.T) {
	followSetup(t)
	ci := mustCheckin(t, idBuild, "in 20m", "dev", "rerun the flaky test")
	pause(t, "dev")
	tick(t, 21*time.Minute, idleBoth)
	tick(t, 21*time.Minute+heldFor+time.Minute, idleBoth)
	got, ok := timerByID(t, ci.ID)
	if !ok || got.Held != "expired" || !strings.Contains(got.Why, "dropped") {
		t.Fatalf("check-in = %+v", got)
	}
	c := board(t, 21*time.Minute+heldFor+time.Minute, idleBoth)
	var card Card
	for _, x := range c {
		if x.Kind == CardCheckin {
			card = x
		}
	}
	if card.Column != ColWaitingYou || card.Why != WhyCheckin || card.Ref != ci.ID {
		t.Errorf("card = %+v", card)
	}
	Resume("dev")
	tick(t, 21*time.Minute+heldFor+2*time.Minute, idleBoth)
	if len(timerPosts(t, "dev")) != 0 {
		t.Error("a dropped check-in fired")
	}
	tick(t, 21*time.Minute+heldFor+timerKeep+3*time.Minute, idleBoth)
	if _, ok := timerByID(t, ci.ID); ok {
		t.Error("a dropped check-in is kept for good")
	}
}

func TestTimersHeldByTheSpendGate(t *testing.T) {
	turns := spendSetup(t, 5, 50)
	ci := mustCheckin(t, idBuild, "in 20m", "", "look at the deploy")
	r := mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@builder ping"})
	spendTurn(t, turns, idArch, 0, 6)
	if SpendOK(now()) {
		t.Fatal("the gate is open")
	}
	dm := DMChannel(idBuild, You)
	tick(t, 20*time.Minute, idleBoth)
	got, _ := timerByID(t, ci.ID)
	if len(timerPosts(t, dm))+len(timerPosts(t, "dev")) != 0 || got.Held != "budget" || got.Why != "held: budget" {
		t.Errorf("check-in = %+v", got)
	}
	if rr, _ := timerByID(t, r.ID); rr.Missed < 1 || rr.Fired != 0 {
		t.Errorf("routine = %+v", rr)
	}
	// Opened for the day: the check-in goes out, late.
	if err := SpendOpenToday(now()); err != nil {
		t.Fatal(err)
	}
	tick(t, 22*time.Minute, idleBoth)
	if ps := timerPosts(t, dm); len(ps) != 1 || !strings.Contains(ps[0].Text, "look at the deploy") {
		t.Errorf("after opening: %+v", ps)
	}
}

func TestCheckinForAStoppedAgentIsACard(t *testing.T) {
	followSetup(t)
	dm := DMChannel(idBuild, You)
	ci := mustCheckin(t, idBuild, "in 20m", "", "verify the deploy")
	stopped := map[string]string{idArch: status.Idle, idBuild: status.Stopped}
	tick(t, 20*time.Minute, stopped)
	if len(timerPosts(t, dm)) != 0 {
		t.Fatal("posted to a stopped agent")
	}
	var card Card
	for _, c := range board(t, 20*time.Minute, stopped) {
		if c.Kind == CardCheckin {
			card = c
		}
	}
	if card.Column != ColWaitingYou || card.Why != WhyCheckin || card.Owner != idBuild || !strings.Contains(card.Title, "verify the deploy") || card.Ref != ci.ID {
		t.Fatalf("card = %+v", card)
	}
	// Running again: it fires, and the card goes.
	tick(t, 25*time.Minute, idleBoth)
	if len(timerPosts(t, dm)) != 1 {
		t.Error("did not fire once the agent was back")
	}
	for _, c := range board(t, 25*time.Minute, idleBoth) {
		if c.Kind == CardCheckin {
			t.Errorf("card stays: %+v", c)
		}
	}
	// A routine is posted to a stopped agent anyway.
	mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@builder daily ping"})
	tick(t, 40*time.Minute, stopped)
	if len(timerPosts(t, "dev")) != 1 {
		t.Error("a routine waited for a stopped agent")
	}
}

func TestRemovedAgentTimers(t *testing.T) {
	followSetup(t)
	ci := mustCheckin(t, idBuild, "in 20m", "", "verify the deploy")
	other := mustCheckin(t, idArch, "in 20m", "", "look at the logs")
	r := mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@builder status please"})
	all := mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@all status please"})
	st, _ := state.Load()
	st.RemoveAgent(idBuild)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	tick(t, 15*time.Minute, idleBoth)
	if _, ok := timerByID(t, ci.ID); ok {
		t.Error("a removed agent's check-in stays")
	}
	if _, ok := timerByID(t, other.ID); !ok {
		t.Error("another agent's check-in went")
	}
	got, _ := timerByID(t, r.ID)
	if !got.Paused || !strings.HasPrefix(got.Why, "no agent ") || got.Fired != 0 {
		t.Errorf("routine = %+v", got)
	}
	if n := len(personTold); n != 1 || !strings.HasPrefix(personTold[0], "followup: routine "+r.ID+" paused itself: no agent") {
		t.Errorf("told = %q", personTold)
	}
	if g, _ := timerByID(t, all.ID); g.Fired != 1 {
		t.Errorf("the @all routine = %+v", g)
	}
	tick(t, 30*time.Minute, idleBoth)
	if len(personTold) != 1 {
		t.Errorf("told again: %q", personTold)
	}
	// Forget drops the check-ins at once.
	Forget(idArch)
	if _, ok := timerByID(t, other.ID); ok {
		t.Error("Forget left a check-in")
	}
}

func TestTimerCaps(t *testing.T) {
	followSetup(t)
	a := mustCheckin(t, idBuild, "in 10m", "", "one")
	mustCheckin(t, idBuild, "in 20m", "dev", "two")
	mustCheckin(t, idBuild, "in 30m", "", "three")
	_, err := AddCheckin(idBuild, "in 40m", "", "four", now())
	if err == nil || !strings.Contains(err.Error(), "3 open check-ins") || !strings.Contains(err.Error(), a.ID) {
		t.Errorf("4th check-in: %v", err)
	}
	mustCheckin(t, idArch, "in 40m", "", "the other agent has its own three")
	// Cancelling makes room; an agent cancels only its own.
	if err := RemoveTimer(a.ID, idArch); err == nil {
		t.Error("an agent cancelled another's check-in")
	}
	if err := RemoveTimer(a.ID, idBuild); err != nil {
		t.Fatal(err)
	}
	mustCheckin(t, idBuild, "in 40m", "", "four")
	for _, bad := range []string{"in 4m", "in 25h", "at 2026-11-02 09:00", "daily 09:00", "every 30m", "soon"} {
		if tm, err := AddCheckin(idArch, bad, "", "x", now()); err == nil {
			t.Errorf("check-in %q: %+v", bad, tm)
		}
	}
	for _, c := range []struct{ ch, note string }{{"nowhere", "x"}, {DMChannel(idBuild, You), "x"}, {"", " "}} {
		if tm, err := AddCheckin(idArch, "in 10m", c.ch, c.note, now()); err == nil {
			t.Errorf("check-in %+v: %+v", c, tm)
		}
	}
	if _, err := AddCheckin(idRun, "in 10m", "", "x", now()); err == nil {
		t.Error("an agent in a run booked a check-in")
	}

	// Routines: every >= 15m, at most 20, a channel that exists, text.
	for _, sp := range []RoutineSpec{
		{Every: "every 14m", Channel: "dev", Text: "hello"},
		{Every: "in 20m", Channel: "dev", Text: "hello"},
		{Every: "daily 09:00", Channel: "nowhere", Text: "hello"},
		{Every: "daily 09:00", Channel: "dev"},
		{Every: "daily 09:00", Channel: "dev", Text: "@codex hello"},
	} {
		if tm, err := AddRoutine(sp, now()); err == nil {
			t.Errorf("routine %+v: %+v", sp, tm)
		}
	}
	for i := 0; i < maxRoutines; i++ {
		mustRoutine(t, RoutineSpec{Every: fmt.Sprintf("daily 09:%02d", i), Channel: "dev", Text: "standup"})
	}
	if _, err := AddRoutine(RoutineSpec{Every: "daily 12:00", Channel: "dev", Text: "one too many"}, now()); err == nil || !strings.Contains(err.Error(), "20 routines") {
		t.Errorf("21st routine: %v", err)
	}
}

func TestRoutineRemovePauseAndFire(t *testing.T) {
	followSetup(t)
	r := mustRoutine(t, RoutineSpec{Every: "weekdays 09:00", Channel: "dev", Text: "@all what did you do yesterday?"})
	if !r.NextAt.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("next = %v", r.NextAt)
	}
	// Fire now: posts, and leaves its slot as it was.
	if err := FireTimer(r.ID, now(), idleBoth); err != nil {
		t.Fatal(err)
	}
	ps := timerPosts(t, "dev")
	if len(ps) != 1 || len(ps[0].Mentions) != 1 || ps[0].Mentions[0] != "*" {
		t.Fatalf("posts = %+v", ps)
	}
	if pendingBus(t, idBuild) != 1 || pendingBus(t, idArch) != 1 {
		t.Error("@all woke no one")
	}
	if got, _ := timerByID(t, r.ID); got.Fired != 1 || !got.NextAt.Equal(r.NextAt) {
		t.Errorf("after fire now: %+v", got)
	}
	// In a paused channel it says why it did not.
	pause(t, "dev")
	if err := FireTimer(r.ID, now(), idleBoth); err == nil || !strings.Contains(err.Error(), "held: #dev paused") {
		t.Errorf("fire in a paused channel: %v", err)
	}
	if err := FireTimer("r-nothing", now(), idleBoth); err == nil {
		t.Error("fired a timer that does not exist")
	}
	if err := PauseTimer("r-nothing", true, now()); err == nil {
		t.Error("paused a timer that does not exist")
	}
	if err := RemoveTimer(r.ID, You); err != nil {
		t.Fatal(err)
	}
	if _, ok := timerByID(t, r.ID); ok {
		t.Error("removed routine stays")
	}
}

// A timer is mad's word: the guards that hold a nudge hold it.
func TestTimerPostsAreGuarded(t *testing.T) {
	followSetup(t)
	// Counts toward the quiet budget.
	r := mustRoutine(t, RoutineSpec{Every: "every 15m", Channel: "dev", Text: "@builder tick"})
	for i := 1; i <= 3; i++ {
		tick(t, time.Duration(i)*15*time.Minute, idleBoth)
	}
	_ = r
	msgs, _ := readMsgs("dev")
	quiet := 0
	for i := len(msgs) - 1; i >= 0 && msgs[i].From.ID != You; i-- {
		quiet++
	}
	if quiet != 3 {
		t.Errorf("quiet posts = %d, want 3", quiet)
	}
	// A person's word after it ends the count.
	mustPost(t, "dev", me, "carry on")
	tick(t, 4*15*time.Minute, idleBoth)
	if len(timerPosts(t, "dev")) != 4 {
		t.Errorf("timer posts = %d", len(timerPosts(t, "dev")))
	}
}
