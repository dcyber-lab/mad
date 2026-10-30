package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// drain takes the bus pointer out of agent id's queue, as the dispatcher does.
func drain(t *testing.T, id string) {
	t.Helper()
	if _, _, err := queue.Take(id); err != nil {
		t.Fatal(err)
	}
}

func TestLapseSparedForChildQuestionAndPending(t *testing.T) {
	setup(t)
	turns := map[string]*status.Turn{}
	fakeTurn(t, turns)
	t0 := now()
	late := &status.Turn{Reply: "waiting", At: t0.Add(time.Minute)}

	// A child handoff that is open.
	h := mustHand(t, arch, idBuild, "build the thing", "")
	deliver(t, idBuild)
	drain(t, idBuild)
	child := mustHand(t, build, idCodex, "write its tests", "")
	if child.Parent != h.ID {
		t.Fatalf("parent = %q", child.Parent)
	}
	turns[idBuild] = late
	_ = CheckLapses(t0.Add(2 * time.Minute))
	if hs, _ := Handoffs(); hs[0].Status != "open" {
		t.Fatalf("lapsed with an open child: %+v", hs[0])
	}

	// A question to the sender.
	setup(t)
	fakeTurn(t, turns)
	mustHand(t, arch, idBuild, "build the thing", "")
	deliver(t, idBuild)
	drain(t, idBuild)
	if _, _, err := DM(build, idArch, "which of the two designs do you mean?"); err != nil {
		t.Fatal(err)
	}
	_ = CheckLapses(t0.Add(2 * time.Minute))
	if hs, _ := Handoffs(); hs[0].Status != "open" {
		t.Fatalf("lapsed after a question: %+v", hs[0])
	}

	// More bus news waiting for the receiver.
	setup(t)
	fakeTurn(t, turns)
	mustHand(t, arch, idBuild, "build the thing", "")
	deliver(t, idBuild) // its pointer is still queued
	_ = CheckLapses(t0.Add(2 * time.Minute))
	if hs, _ := Handoffs(); hs[0].Status != "open" {
		t.Fatalf("lapsed with a pointer pending: %+v", hs[0])
	}
}

func TestDoneAcceptsLapsed(t *testing.T) {
	setup(t)
	turns := map[string]*status.Turn{}
	fakeTurn(t, turns)
	h := mustHand(t, arch, idBuild, "build the thing", "")
	deliver(t, idBuild)
	drain(t, idBuild)
	turns[idBuild] = &status.Turn{Reply: "hm", At: now().Add(time.Minute)}
	_ = CheckLapses(now().Add(2 * time.Minute))
	if hs, _ := Handoffs(); hs[0].Status != "lapsed" {
		t.Fatalf("not lapsed: %+v", hs[0])
	}
	drain(t, idArch)
	got, _, err := Done(build, h.ID, "it was done after all", "done")
	if err != nil || got.Status != "done" {
		t.Fatalf("Done = %+v %v", got, err)
	}
	if pendingBus(t, idArch) != 1 {
		t.Error("sender not woken by the late result")
	}
	if _, _, err := Done(build, h.ID, "again", "done"); err == nil {
		t.Error("finished twice")
	}
}

func TestSpawnCountsRootSenderAndDeckWide(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch, idBuild, idCodex)
	for i, to := range []string{idBuild, idCodex, idBuild} {
		h, _, err := HandOff(arch, to, "task number "+string(rune('a'+i)), "", nil, "dev", "")
		if err != nil {
			t.Fatal(err)
		}
		_ = MarkSpawned(h.ID)
	}
	if _, n, err := ChainState(idArch, "", "dev"); err != nil || n != 3 {
		t.Errorf("a root sender's spawns = %d %v", n, err)
	}
	if n, err := OpenSpawned(); err != nil || n != 3 {
		t.Errorf("OpenSpawned = %d %v", n, err)
	}
	// The person's word starts the sender's count again, not the deck's.
	now = func() time.Time { return time.Date(2026, 10, 30, 14, 9, 0, 0, time.UTC) }
	mustPost(t, "dev", me, "carry on, this is fine")
	if _, n, _ := ChainState(idArch, "", "dev"); n != 0 {
		t.Errorf("spawns after the person spoke = %d", n)
	}
	if n, _ := OpenSpawned(); n != 3 {
		t.Errorf("OpenSpawned after = %d", n)
	}
	for _, bad := range []string{"../x", "a/b", ".."} {
		if _, _, err := ChainState(idArch, "", bad); err == nil {
			t.Errorf("channel %q accepted", bad)
		}
	}
}

func TestDMRefusesOutsiders(t *testing.T) {
	setup(t)
	ch := DMChannel(idArch, idBuild)
	if _, _, err := DM(arch, idBuild, "a private word with you"); err != nil {
		t.Fatal(err)
	}
	if _, err := Post(ch, cdx, "I listen in and speak up", 0); err == nil {
		t.Error("an outsider posted in a DM")
	}
	if _, err := Post(ch, build, "my own answer to you", 0); err != nil {
		t.Errorf("a member could not post: %v", err)
	}
	if _, err := Post(DMChannel(You, idBuild), arch, "into the person's DM", 0); err == nil {
		t.Error("an agent posted in the person's DM with another")
	}
	if _, _, err := DM(me, idBuild, "the person speaks to builder"); err != nil {
		t.Errorf("the person in their own DM: %v", err)
	}
}

func pause(t *testing.T, ch string) {
	t.Helper()
	if err := change(ch, func(c *Channel, _ []Message) error { c.Paused = true; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestRelayNotHeldByPause(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch, idBuild)
	pause(t, "dev")
	h, res, err := HandOff(arch, idBuild, "build it while paused", "", nil, "dev", "")
	if err != nil || len(res.Woke) != 1 || res.Warning != "" {
		t.Fatalf("handoff in a paused channel = %+v %v", res, err)
	}
	if text := deliver(t, idBuild); !strings.Contains(text, "handoff "+h.ID) {
		t.Errorf("digest lacks the handoff:\n%s", text)
	}
	drain(t, idBuild)
	_, dres, err := Done(build, h.ID, "all built", "done")
	if err != nil || len(dres.Woke) != 1 || dres.Woke[0] != "architect" {
		t.Fatalf("done in a paused channel = %+v %v", dres, err)
	}
	if text := deliver(t, idArch); !strings.Contains(text, "all built") {
		t.Errorf("digest lacks the result:\n%s", text)
	}
	// An ordinary mention in it still waits.
	if r := mustPost(t, "dev", arch, "@builder and what about the docs?"); len(r.Woke) != 0 {
		t.Errorf("a paused channel woke %v", r.Woke)
	}
}

func TestRelayNotCounted(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch, idBuild)
	pingPong = 3
	t.Cleanup(func() { pingPong = 8 })
	mustPost(t, "dev", arch, "@builder first question for you")
	mustPost(t, "dev", build, "@architect first answer to you")
	_, res, err := HandOff(arch, idBuild, "a handoff in the exchange", "", nil, "dev", "")
	if err != nil || len(res.Woke) != 1 {
		t.Fatalf("handoff = %+v %v", res, err)
	}
	if c, _, _ := Find("dev"); c.Paused {
		t.Error("paused by a handoff counted as ping-pong")
	}
}

func TestWaitFor(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch, idBuild)
	t0 := now()
	if w := WaitFor(idBuild, t0); w != 0 {
		t.Errorf("never woken: wait %v", w)
	}
	mustPost(t, "dev", arch, "@builder the first thing to look at")
	deliver(t, idBuild)
	mustPost(t, "dev", arch, "@builder a second thing to look at")
	if w := WaitFor(idBuild, t0.Add(10*time.Second)); w != 50*time.Second {
		t.Errorf("wait = %v", w)
	}
	if w := WaitFor(idBuild, t0.Add(61*time.Second)); w != 0 {
		t.Errorf("wait after the minute = %v", w)
	}
	// A handoff goes through.
	mustHand(t, arch, idBuild, "do this one now", "")
	if w := WaitFor(idBuild, t0.Add(10*time.Second)); w != 0 {
		t.Errorf("a handoff waited %v", w)
	}
}

func TestMentionsUnicodeCodeAndURLs(t *testing.T) {
	setup(t)
	st, _ := state.Load()
	cases := []struct {
		text string
		want []string
	}{
		{"İİİ KKKK @builder please look", []string{idBuild}},
		{"KKKKKKKK @BUILDER", []string{idBuild}},
		{"İ @Architect and @builder", []string{idArch, idBuild}},
		{"see `@builder` here", nil},
		{"```\n@builder\n```\n@architect now", []string{idArch}},
		{"``` unclosed\n@builder", nil},
		{"https://example.com/@builder/x", nil},
		{"mail me@builder.dev", nil},
		{"(@builder)", []string{idBuild}},
	}
	for _, c := range cases {
		got, err := mentionsIn(st, c.text)
		if err != nil || strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("mentionsIn(%q) = %v %v, want %v", c.text, got, err, c.want)
		}
	}
}

func TestDMCannotBeLeftAndHealsOnPost(t *testing.T) {
	setup(t)
	if _, _, err := DM(arch, idBuild, "hello there, builder"); err != nil {
		t.Fatal(err)
	}
	ch := DMChannel(idArch, idBuild)
	if err := Leave(ch, idBuild); err == nil {
		t.Error("left a DM")
	}
	if err := Invite(ch, []string{idCodex}); err == nil {
		t.Error("invited into a DM")
	}
	// A DM whose member went missing is repaired by the next post.
	if err := change(ch, func(c *Channel, _ []Message) error { c.Members = []string{idArch}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, r, err := DM(arch, idBuild, "are you still there?"); err != nil || len(r.Woke) != 1 {
		t.Fatalf("DM = %+v %v", r, err)
	}
	if c, _, _ := Find(ch); !has(c.Members, idBuild) {
		t.Errorf("members = %v", c.Members)
	}
	if text := deliver(t, idBuild); !strings.Contains(text, "are you still there?") {
		t.Errorf("digest:\n%s", text)
	}
}

func TestStaleHandoffCard(t *testing.T) {
	setup(t)
	h := mustHand(t, arch, idBuild, "build the thing", "")
	// It ends before builder is told.
	if _, err := changeHandoff(h.ID, func(x *Handoff) error { x.Status = "lapsed"; return nil }); err != nil {
		t.Fatal(err)
	}
	text := deliver(t, idBuild)
	if !strings.Contains(text, "already lapsed") || strings.Contains(text, "Finish it with bus_done") {
		t.Errorf("stale card:\n%s", text)
	}
}

func TestHandoffSeqSetWithThePost(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch, idBuild)
	mustPost(t, "dev", arch, "@builder some earlier thing here")
	h, res, err := HandOff(arch, idBuild, "build it", "", nil, "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	hs, _ := Handoffs()
	msgs, _, _ := Read("dev", 0, 10)
	last := msgs[len(msgs)-1]
	if hs[0].Seq != res.Seq || h.Seq != res.Seq || last.Seq != res.Seq || last.Handoff == nil || last.Handoff.ID != h.ID {
		t.Errorf("seq %d / %d / message %+v", hs[0].Seq, res.Seq, last)
	}
}

func TestCompactionKeepsUndeliveredMentions(t *testing.T) {
	setup(t)
	maxLines, keepLines = 10, 3
	t.Cleanup(func() { maxLines, keepLines = 2000, 500 })
	mustCreate(t, "dev", idArch, idBuild)
	mustPost(t, "dev", arch, "@builder please read this early note")
	for i := 0; i < 14; i++ {
		mustPost(t, "dev", me, "chatter from the person, number "+string(rune('a'+i)))
	}
	msgs, _, _ := Read("dev", 0, 100)
	found, note := false, false
	for _, m := range msgs {
		found = found || strings.Contains(m.Text, "early note")
		note = note || strings.Contains(m.Text, "not yet delivered")
	}
	if !found || !note {
		t.Errorf("early mention kept=%v note=%v: %d messages", found, note, len(msgs))
	}
	if text := deliver(t, idBuild); !strings.Contains(text, "early note") {
		t.Errorf("digest:\n%s", text)
	}
}
