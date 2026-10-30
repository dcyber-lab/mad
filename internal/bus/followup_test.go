package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// personTold is what tellPerson was given: "kind: message".
var personTold []string

// followSetup is setup with the follow-up hooks replaced: no notification
// is sent, no turn is read from disk, and the person spoke in #dev at the
// start. It returns the turns and the permission hooks to fake.
func followSetup(t *testing.T) (turns map[string]*status.Turn, hooks map[string]*status.Hook) {
	t.Helper()
	setup(t)
	turns, hooks = map[string]*status.Turn{}, map[string]*status.Hook{}
	fakeTurn(t, turns)
	personTold = nil
	oldTell, oldHook, oldF, oldOn, oldIdle := tellPerson, readHook, FollowUp, NudgesOn, MaxIdle
	tellPerson = func(kind, msg string) { personTold = append(personTold, kind+": "+msg) }
	readHook = func(id string) *status.Hook { return hooks[id] }
	FollowUp, NudgesOn, MaxIdle = 20*time.Minute, true, 8*time.Hour
	t.Cleanup(func() { tellPerson, readHook, FollowUp, NudgesOn, MaxIdle = oldTell, oldHook, oldF, oldOn, oldIdle })
	mustCreate(t, "dev", idArch, idBuild)
	mustPost(t, "dev", me, "morning, please start on the api")
	return turns, hooks
}

var idleBoth = map[string]string{idArch: status.Idle, idBuild: status.Idle, idCodex: status.Idle}

// doingTask is a task builder is doing in #dev, as of now.
func doingTask(t *testing.T, title string) Task {
	t.Helper()
	tk, err := SetTask(arch, TaskChange{Title: sp(title), Owner: sp(idBuild), Status: sp("doing"), Channel: sp("dev")})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func check(t *testing.T, d time.Duration, statuses map[string]string) {
	t.Helper()
	setNow(d)
	if err := CheckFollowUps(now(), statuses); err != nil {
		t.Fatal(err)
	}
}

// nudgesIn lists the nudge messages in channel ch.
func nudgesIn(t *testing.T, ch string) []Message {
	t.Helper()
	msgs, _ := readMsgs(ch)
	var out []Message
	for _, m := range msgs {
		if m.Kind == "nudge" {
			out = append(out, m)
		}
	}
	return out
}

func TestFollowUpLadder(t *testing.T) {
	followSetup(t)
	if err := SetLead("dev", idArch); err != nil {
		t.Fatal(err)
	}
	tk := doingTask(t, "add the 404 case")
	drain(t, idBuild)
	drain(t, idArch)

	check(t, 19*time.Minute, idleBoth)
	if n := nudgesIn(t, "dev"); len(n) != 0 {
		t.Fatalf("nudged at 19m: %+v", n)
	}
	// 20m: the owner, once.
	check(t, 20*time.Minute, idleBoth)
	n := nudgesIn(t, "dev")
	if len(n) != 1 || n[0].From.ID != "mad" || len(n[0].Mentions) != 1 || n[0].Mentions[0] != idBuild ||
		!strings.Contains(n[0].Text, tk.ID) || !strings.Contains(n[0].Text, "has been yours 20 min with no word") || !strings.Contains(n[0].Text, "@you") {
		t.Fatalf("owner nudge = %+v", n)
	}
	if pendingBus(t, idBuild) != 1 || pendingBus(t, idArch) != 0 {
		t.Errorf("woke build=%d arch=%d", pendingBus(t, idBuild), pendingBus(t, idArch))
	}
	if len(told) != 0 {
		t.Errorf("a nudge's @you told the person: %q", told)
	}
	check(t, 21*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 1 {
		t.Error("owner nudged twice")
	}
	// 40m: the channel's lead (the task has no handoff).
	check(t, 40*time.Minute, idleBoth)
	n = nudgesIn(t, "dev")
	if len(n) != 2 || n[1].Mentions[0] != idArch || !strings.Contains(n[1].Text, "owner builder") {
		t.Fatalf("lead nudge = %+v", n)
	}
	if pendingBus(t, idArch) != 1 {
		t.Error("lead not woken")
	}
	if len(personTold) != 0 {
		t.Errorf("person told early: %q", personTold)
	}
	// 60m: the person, with no post.
	check(t, 60*time.Minute, idleBoth)
	want := "followup: 1 stalled: " + tk.ID + " (builder, 1h)"
	if len(personTold) != 1 || personTold[0] != want {
		t.Fatalf("told = %q, want %q", personTold, want)
	}
	if len(nudgesIn(t, "dev")) != 2 {
		t.Error("a nudge went to the person's rung")
	}
	// Unchanged: not again for 2h; then again. The record's cap holds.
	check(t, 61*time.Minute, idleBoth)
	check(t, 2*time.Hour+59*time.Minute, idleBoth)
	if len(personTold) != 1 || len(nudgesIn(t, "dev")) != 2 {
		t.Errorf("told %q, nudges %d at 2h59m", personTold, len(nudgesIn(t, "dev")))
	}
	check(t, 3*time.Hour+30*time.Minute, idleBoth)
	if len(personTold) != 2 {
		t.Errorf("not told again after 2h: %q", personTold)
	}
	got, _, _ := FindTask(tk.ID)
	if len(got.Nudges) != 2 || got.Nudges[0].Why != "owner" || got.Nudges[1].Why != "lead" || !got.UpdatedAt.Equal(tk.UpdatedAt) {
		t.Errorf("history = %+v (updated %v)", got.Nudges, got.UpdatedAt)
	}
}

func TestFollowUpOwnerNotRunningOrWaiting(t *testing.T) {
	followSetup(t)
	doingTask(t, "add the 404 case")
	for _, s := range []string{status.Running, status.Waiting, ""} {
		check(t, 30*time.Minute, map[string]string{idBuild: s})
		if n := nudgesIn(t, "dev"); len(n) != 0 {
			t.Fatalf("owner %q nudged: %+v", s, n)
		}
	}
	check(t, 31*time.Minute, map[string]string{idBuild: status.Asleep})
	if len(nudgesIn(t, "dev")) != 1 {
		t.Error("an asleep owner is nudged")
	}
}

func TestFollowUpOwnerActivityResetsTimeNotCap(t *testing.T) {
	turns, _ := followSetup(t)
	doingTask(t, "add the 404 case")
	t0 := now()
	turns[idBuild] = &status.Turn{Reply: "working", At: t0.Add(15 * time.Minute)}
	check(t, 20*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 0 {
		t.Fatal("nudged 5m after a turn ended")
	}
	check(t, 35*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 1 {
		t.Fatal("not nudged 20m after the turn")
	}
	// More activity, then silence: the owner's nudge is spent.
	turns[idBuild] = &status.Turn{Reply: "still", At: t0.Add(36 * time.Minute)}
	check(t, 60*time.Minute, idleBoth)
	if n := nudgesIn(t, "dev"); len(n) != 1 {
		t.Errorf("owner nudged again: %d", len(n))
	}
}

func TestFollowUpLapsedSender(t *testing.T) {
	turns, _ := followSetup(t)
	h := mustHand(t, arch, idBuild, "build the thing", "")
	deliver(t, idBuild)
	drain(t, idBuild)
	t0 := now()
	turns[idBuild] = &status.Turn{Reply: "hm", At: t0.Add(time.Minute)}
	setNow(2 * time.Minute)
	if err := CheckLapses(now()); err != nil {
		t.Fatal(err)
	}
	lapsedAt := now()
	drain(t, idArch)
	dm := DMChannel(idArch, idBuild)

	check(t, 2*time.Minute+14*time.Minute, idleBoth)
	if len(nudgesIn(t, dm)) != 0 {
		t.Fatal("sender nudged before 15m")
	}
	check(t, 2*time.Minute+15*time.Minute, idleBoth)
	n := nudgesIn(t, dm)
	if len(n) != 1 || n[0].Mentions[0] != idArch || !strings.Contains(n[0].Text, h.ID+", that lapsed") {
		t.Fatalf("sender nudge = %+v", n)
	}
	if pendingBus(t, idArch) != 1 {
		t.Error("sender not woken in its DM")
	}
	check(t, 2*time.Minute+16*time.Minute, idleBoth)
	if len(nudgesIn(t, dm)) != 1 {
		t.Error("sender nudged twice")
	}
	// The person, three follow-ups after the lapse.
	check(t, 2*time.Minute+60*time.Minute, idleBoth)
	if len(personTold) != 1 || personTold[0] != "followup: 1 stalled: "+h.ID+" lapsed" {
		t.Errorf("told %q", personTold)
	}
	_ = lapsedAt
}

func TestFollowUpLapsedSenderActed(t *testing.T) {
	turns, _ := followSetup(t)
	mustHand(t, arch, idBuild, "build the thing", "")
	deliver(t, idBuild)
	drain(t, idBuild)
	t0 := now()
	turns[idBuild] = &status.Turn{Reply: "hm", At: t0.Add(time.Minute)}
	setNow(2 * time.Minute)
	_ = CheckLapses(now())
	// The sender took it up: its turn ended after the lapse.
	turns[idArch] = &status.Turn{Reply: "on it", At: t0.Add(5 * time.Minute)}
	check(t, 30*time.Minute, idleBoth)
	if n := nudgesIn(t, DMChannel(idArch, idBuild)); len(n) != 0 {
		t.Errorf("nudged a sender that acted: %+v", n)
	}
}

func TestFollowUpPausedChannel(t *testing.T) {
	followSetup(t)
	tk := doingTask(t, "add the 404 case")
	if err := change("dev", func(c *Channel, _ []Message) error { c.Paused = true; return nil }); err != nil {
		t.Fatal(err)
	}
	check(t, 30*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 0 {
		t.Error("nudged in a paused channel")
	}
	// Even asked directly, the post refuses and the reservation is given back.
	ok := nudge{tk.ID, true, "dev", idBuild, "owner", "[mad] follow-up: x"}.send(now())
	got, _, _ := FindTask(tk.ID)
	if ok || len(got.Nudges) != 0 || len(nudgesIn(t, "dev")) != 0 || len(loadNudges().Log) != 0 {
		t.Errorf("paused send: ok=%v history=%+v log=%v", ok, got.Nudges, loadNudges().Log)
	}
}

func TestFollowUpCountsTowardQuiet(t *testing.T) {
	followSetup(t)
	old := pauseAt
	pauseAt = 4
	t.Cleanup(func() { pauseAt = old })
	for i := 0; i < 4; i++ {
		doingTask(t, "task number "+string(rune('a'+i)))
	}
	check(t, 20*time.Minute, idleBoth)
	c, _, _ := Find("dev")
	n := nudgesIn(t, "dev")
	// The person's post ends the count; each nudge is one quiet post, so the
	// 4th reaches pauseAt and pauses the channel.
	if !c.Paused || len(n) != 4 {
		t.Fatalf("paused=%v nudges=%d", c.Paused, len(n))
	}
	msgs, _ := readMsgs("dev")
	last := msgs[len(msgs)-1]
	if last.Kind != "system" || !strings.Contains(last.Text, "Paused: 4 posts") {
		t.Errorf("last = %+v", last)
	}
	if len(told) != 1 || !strings.Contains(told[0], "paused") {
		t.Errorf("person not told of the pause: %q", told)
	}
	// A paused channel gets no more.
	check(t, 45*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 4 {
		t.Error("nudged after the pause")
	}
}

func TestFollowUpDeckCap(t *testing.T) {
	followSetup(t)
	for i := 0; i < 8; i++ {
		doingTask(t, "task number "+string(rune('a'+i)))
	}
	check(t, 20*time.Minute, idleBoth)
	if n := len(nudgesIn(t, "dev")); n != 6 {
		t.Fatalf("nudges = %d, want the deck cap 6", n)
	}
	check(t, 25*time.Minute, idleBoth)
	if n := len(nudgesIn(t, "dev")); n != 6 {
		t.Fatalf("nudges = %d within 10 minutes", n)
	}
	check(t, 31*time.Minute, idleBoth) // the first six aged out
	if n := len(nudgesIn(t, "dev")); n != 8 {
		t.Errorf("nudges = %d, want the other two", n)
	}
}

func TestFollowUpPerRecordCap(t *testing.T) {
	followSetup(t)
	tk := doingTask(t, "add the 404 case")
	for _, why := range []string{"owner", "lead", "sender"} {
		sent := nudge{tk.ID, true, "dev", idBuild, why, "[mad] follow-up: again"}.send(now())
		if want := why != "sender"; sent != want {
			t.Errorf("%s: sent=%v", why, sent)
		}
	}
	if n := len(nudgesIn(t, "dev")); n != 2 {
		t.Errorf("nudges = %d, want 2", n)
	}
	// The same reason is never twice, for records kept in nudges.json too.
	key := "ask:dev#1:" + idBuild
	if !(nudge{key, false, "dev", idBuild, "ask", "[mad] follow-up: a"}).send(now()) ||
		(nudge{key, false, "dev", idBuild, "ask", "[mad] follow-up: b"}).send(now()) {
		t.Error("an ask nudge is once")
	}
}

func TestFollowUpMaxIdle(t *testing.T) {
	followSetup(t)
	MaxIdle = time.Hour
	tk := doingTask(t, "add the 404 case")
	check(t, 90*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 0 {
		t.Error("nudged with the person away")
	}
	// The person is still told: that is the point of the rung.
	if len(personTold) != 1 || !strings.Contains(personTold[0], tk.ID) {
		t.Errorf("told %q", personTold)
	}
	// They come back: the agents are nudged again.
	setNow(100 * time.Minute)
	mustPost(t, "dev", me, "I am back, how is it going")
	check(t, 125*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 1 {
		t.Error("not nudged once the person is back")
	}
}

func TestFollowUpOff(t *testing.T) {
	followSetup(t)
	NudgesOn = false
	doingTask(t, "add the 404 case")
	check(t, 90*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 0 {
		t.Error("nudged with nudges off")
	}
	if len(personTold) != 1 {
		t.Errorf("the board still stalls, and the person is told: %q", personTold)
	}
}

func TestFollowUpPermissionReminder(t *testing.T) {
	_, hooks := followSetup(t)
	hooks[idBuild] = &status.Hook{State: status.Waiting, Message: "Bash go test", At: now()}
	waiting := map[string]string{idArch: status.Idle, idBuild: status.Waiting}
	check(t, 29*time.Minute, waiting)
	if len(personTold) != 0 {
		t.Fatalf("reminded at 29m: %q", personTold)
	}
	check(t, 30*time.Minute, waiting)
	if len(personTold) != 1 || personTold[0] != "followup: builder has waited 30 min for your permission: Bash go test" {
		t.Fatalf("told %q", personTold)
	}
	check(t, 45*time.Minute, waiting)
	check(t, 4*time.Hour, waiting)
	if len(personTold) != 1 {
		t.Errorf("reminded twice: %q", personTold)
	}
	// A new wait is a new reminder.
	hooks[idBuild] = &status.Hook{State: status.Waiting, Message: "Edit x.go", At: now()}
	check(t, 4*time.Hour+31*time.Minute, waiting)
	if len(personTold) != 2 {
		t.Errorf("new wait not reminded: %q", personTold)
	}
}

func TestFollowUpUnansweredAsk(t *testing.T) {
	turns, _ := followSetup(t)
	_ = turns
	mustPost(t, "dev", arch, "@builder which schema should the endpoint use?")
	drain(t, idBuild)
	check(t, 19*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 0 {
		t.Fatal("nudged at 19m")
	}
	check(t, 20*time.Minute, map[string]string{idBuild: status.Running})
	if len(nudgesIn(t, "dev")) != 0 {
		t.Fatal("nudged a running agent")
	}
	check(t, 21*time.Minute, idleBoth)
	n := nudgesIn(t, "dev")
	if len(n) != 1 || n[0].Mentions[0] != idBuild || !strings.Contains(n[0].Text, "architect asked you") || !strings.Contains(n[0].Text, "which schema") {
		t.Fatalf("ask nudge = %+v", n)
	}
	check(t, 45*time.Minute, idleBoth)
	if len(nudgesIn(t, "dev")) != 1 {
		t.Error("ask nudged twice")
	}
	if len(personTold) != 0 {
		t.Errorf("the person is not told of agents' questions: %q", personTold)
	}
}

func TestFollowUpAskAnsweredOrNotAQuestion(t *testing.T) {
	followSetup(t)
	mustPost(t, "dev", arch, "@builder which schema should the endpoint use?")
	setNow(5 * time.Minute)
	mustPost(t, "dev", build, "@architect the v2 schema, see api/schema.go")
	mustPost(t, "dev", arch, "@builder please use the v2 schema then.")
	// A question to the person is not for the agents.
	mustPost(t, "dev", build, "@you shall I merge now?")
	check(t, 60*time.Minute, idleBoth)
	if n := nudgesIn(t, "dev"); len(n) != 0 {
		t.Errorf("nudged: %+v", n)
	}
}

func TestFollowUpAskInDM(t *testing.T) {
	followSetup(t)
	if _, _, err := DM(arch, idBuild, "can you check the failing build?"); err != nil {
		t.Fatal(err)
	}
	check(t, 25*time.Minute, idleBoth)
	dm := DMChannel(idArch, idBuild)
	if n := nudgesIn(t, dm); len(n) != 1 || n[0].Mentions[0] != idBuild || !strings.Contains(n[0].Text, "this DM") {
		t.Fatalf("dm nudge = %+v", n)
	}
	if pendingBus(t, idBuild) != 2 && pendingBus(t, idBuild) != 1 {
		t.Error("not woken")
	}
}

// A nudge waits for the agent's minute like any message: it does not
// bypass WaitFor as a relay does.
func TestFollowUpNudgeWaitsItsMinute(t *testing.T) {
	followSetup(t)
	FollowUp = 10 * time.Second
	mustPost(t, "dev", me, "@builder please start on the api now")
	deliver(t, idBuild)
	drain(t, idBuild)
	doingTask(t, "add the 404 case")
	check(t, 10*time.Second, idleBoth)
	if len(nudgesIn(t, "dev")) != 1 {
		t.Fatal("no nudge")
	}
	if w := WaitFor(idBuild, now()); w != 50*time.Second {
		t.Errorf("WaitFor = %v, want 50s", w)
	}
}

func TestLeadDefaultsAndSetLead(t *testing.T) {
	setup(t)
	c, err := Create("api", "", []string{idBuild}, arch)
	if err != nil || c.Lead != idArch {
		t.Fatalf("creator lead = %+v %v", c, err)
	}
	c2 := mustCreate(t, "ops")
	if c2.Lead != "" {
		t.Errorf("a person's channel has lead %q", c2.Lead)
	}
	// The lead must exist, not be in a run; a non-member is added.
	if err := SetLead("ops", idCodex); err != nil {
		t.Fatal(err)
	}
	got, _, _ := Find("ops")
	if got.Lead != idCodex || !has(got.Members, idCodex) {
		t.Errorf("after SetLead: %+v", got)
	}
	if err := SetLead("ops", "nobody"); err == nil {
		t.Error("lead that does not exist")
	}
	if err := SetLead("ops", idRun); err == nil {
		t.Error("lead in a run")
	}
	if err := SetLead("nope", idArch); err == nil {
		t.Error("lead of a channel that does not exist")
	}
	if err := SetLead(DMChannel(idArch, idBuild), idArch); err == nil {
		t.Error("lead of a DM")
	}
	// Leaving gives it up; so does being forgotten.
	if err := Leave("ops", idCodex); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := Find("ops"); got.Lead != "" {
		t.Errorf("left, still lead %q", got.Lead)
	}
	Forget(idArch)
	if got, _, _ := Find("api"); got.Lead != "" {
		t.Errorf("forgotten, still lead %q", got.Lead)
	}
	if err := SetLead("api", ""); err != nil {
		t.Errorf("clearing: %v", err)
	}
}

// An unowned task shows its channel's lead on the card.
func TestBoardUnownedShowsLead(t *testing.T) {
	setup(t)
	fakeTurn(t, map[string]*status.Turn{})
	if _, err := Create("api", "", nil, arch); err != nil {
		t.Fatal(err)
	}
	tk, _ := SetTask(me, TaskChange{Title: sp("who does this"), Channel: sp("api")})
	c := wantCard(t, board(t, time.Minute, idleBoth), tk.ID, ColWaitingYou, WhyUnowned)
	if c.Note != "lead: architect" {
		t.Errorf("note = %q", c.Note)
	}
}
