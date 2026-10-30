package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

func mustHand(t *testing.T, from From, to, task string, parent string) Handoff {
	t.Helper()
	h, _, err := HandOff(from, to, task, "", nil, "", parent)
	if err != nil {
		t.Fatalf("handoff %q: %v", task, err)
	}
	return h
}

// deliver composes agent id's digest and marks it sent.
func deliver(t *testing.T, id string) string {
	t.Helper()
	text, upto, ok, err := Compose(id, now())
	if err != nil || !ok {
		t.Fatalf("Compose(%s) = %v %v", id, ok, err)
	}
	if err := Delivered(id, upto); err != nil {
		t.Fatal(err)
	}
	return text
}

func TestHandoffWakesAndRendersCard(t *testing.T) {
	setup(t)
	h, res, err := HandOff(arch, idBuild, "add the 404 case", "design.md section 3 says so", []string{"api/handler.go", "api/handler_test.go"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h.ID, "h-") || len(h.ID) != 10 || h.Status != "open" || h.Channel != DMChannel(idArch, idBuild) || res.Seq != 1 || h.Seq != 1 {
		t.Fatalf("handoff = %+v %+v", h, res)
	}
	if len(res.Woke) != 1 || res.Woke[0] != "builder" || pendingBus(t, idBuild) != 1 || pendingBus(t, idArch) != 0 {
		t.Errorf("woke %v, queue wrong", res.Woke)
	}
	text, _, ok, _ := Compose(idBuild, now())
	if !ok {
		t.Fatal("nothing to compose")
	}
	for _, want := range []string{
		"DM with architect · architect · #1 · 14:02 — handoff " + h.ID + "\n",
		"Task: add the 404 case\n",
		"Context: design.md section 3 says so\n",
		"Files: api/handler.go, api/handler_test.go\n",
		"Finish it with bus_done(\"" + h.ID + "\", result) when done, or decline it with status \"declined\".",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("digest lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Chain:") {
		t.Errorf("a chain without a parent:\n%s", text)
	}

	// In a channel the handoff brings the receiver in and wakes it.
	mustCreate(t, "dev", idArch)
	h2, res2, err := HandOff(arch, idCodex, "write the changelog", "", nil, "dev", "")
	if err != nil || len(res2.Woke) != 1 || h2.Channel != "dev" {
		t.Fatalf("channel handoff = %+v %+v %v", h2, res2, err)
	}
	c, _, _ := Find("dev")
	if !has(c.Members, idCodex) || pendingBus(t, idCodex) != 1 {
		t.Errorf("receiver not in #dev, or not woken: %v", c.Members)
	}
	if text, _, _, _ := Compose(idCodex, now()); !strings.Contains(text, "#dev · architect · #1 · 14:02 — handoff "+h2.ID) {
		t.Errorf("channel card:\n%s", text)
	}
}

func TestHandoffRefusals(t *testing.T) {
	setup(t)
	for _, c := range []struct{ to, task string }{
		{idArch, "do it yourself"},  // oneself
		{idRun, "work in the run"},  // an agent in a run
		{"nobody", "no such agent"}, // unknown
		{idBuild, "  "},             // no task
		{You, "to the person"},      // not an agent
	} {
		if _, _, err := HandOff(arch, c.to, c.task, "", nil, "", ""); err == nil {
			t.Errorf("handed off %+v", c)
		}
	}
	if hs, _ := Handoffs(); len(hs) != 0 {
		t.Errorf("records of refused handoffs: %+v", hs)
	}
	if _, _, err := HandOff(arch, idBuild, "x task here", "", nil, "nowhere", ""); err == nil {
		t.Error("handed off in a channel that does not exist")
	}
	if hs, _ := Handoffs(); len(hs) != 0 {
		t.Errorf("record of a handoff whose post failed: %+v", hs)
	}
}

func TestDone(t *testing.T) {
	setup(t)
	h := mustHand(t, arch, idBuild, "add the 404 case", "")
	deliver(t, idBuild)

	if _, _, err := Done(arch, h.ID, "i did it myself", "done"); err == nil {
		t.Error("the sender finished its own handoff")
	}
	if _, _, err := Done(cdx, h.ID, "not mine to finish", "done"); err == nil {
		t.Error("a third agent finished it")
	}
	if _, _, err := Done(build, "h-00000000", "no such thing", "done"); err == nil {
		t.Error("finished a handoff that does not exist")
	}
	if _, _, err := Done(build, h.ID, "fine", "great"); err == nil {
		t.Error("a status of great")
	}
	got, res, err := Done(build, h.ID, "404 returned for an unknown id; tests pass", "done")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "done" || got.Result == "" || got.DoneAt.IsZero() || len(res.Woke) != 1 || res.Woke[0] != "architect" || pendingBus(t, idArch) != 1 {
		t.Errorf("Done = %+v %+v", got, res)
	}
	if _, _, err := Done(build, h.ID, "again", "done"); err == nil {
		t.Error("finished it twice")
	}
	text := deliver(t, idArch)
	if want := "— " + h.ID + " done\n404 returned for an unknown id; tests pass"; !strings.Contains(text, want) {
		t.Errorf("result card lacks %q:\n%s", want, text)
	}

	// A decline.
	h2 := mustHand(t, arch, idBuild, "port it to windows", "")
	if got, _, err := Done(build, h2.ID, "needs a design first", "declined"); err != nil || got.Status != "declined" {
		t.Fatalf("decline = %+v %v", got, err)
	}
	if text := deliver(t, idArch); !strings.Contains(text, "— "+h2.ID+" declined\nneeds a design first") {
		t.Errorf("decline card:\n%s", text)
	}
	if open, _ := Open(idBuild); len(open) != 0 {
		t.Errorf("open for builder: %+v", open)
	}
}

func TestHandoffChain(t *testing.T) {
	setup(t)
	h1 := mustHand(t, arch, idBuild, "design the api", "")
	if h1.Parent != "" {
		t.Errorf("parent of the first = %q", h1.Parent)
	}
	h2 := mustHand(t, build, idCodex, "write the handler", "") // builder has h1 open: its parent
	if h2.Parent != h1.ID {
		t.Errorf("parent of h2 = %q, want %s", h2.Parent, h1.ID)
	}
	h3 := mustHand(t, cdx, idArch, "review the handler", "")
	h4 := mustHand(t, arch, idBuild, "apply the review", "")
	if h3.Parent != h2.ID || h4.Parent != h3.ID {
		t.Errorf("parents: %q %q", h3.Parent, h4.Parent)
	}
	text := deliver(t, idBuild)
	if want := "Chain: " + strings.Join([]string{h1.ID, h2.ID, h3.ID, h4.ID}, " → ") + "\n"; !strings.Contains(text, want) {
		t.Errorf("digest lacks %q:\n%s", want, text)
	}
	// The fifth is too deep.
	if _, _, err := HandOff(build, idCodex, "one more hop", "", nil, "", ""); err == nil || !strings.Contains(err.Error(), "at most 4 deep") {
		t.Errorf("fifth handoff: %v", err)
	}
	if d, _, err := ChainState(idBuild, "", ""); err != nil || d != 4 {
		t.Errorf("ChainState depth = %d %v", d, err)
	}
	// An explicit parent starts a branch; an unknown one is refused.
	if h, _, err := HandOff(arch, idCodex, "a side job", "", nil, "", h1.ID); err != nil || h.Parent != h1.ID {
		t.Errorf("explicit parent: %+v %v", h, err)
	}
	if _, _, err := HandOff(arch, idCodex, "a side job again", "", nil, "", "h-ffffffff"); err == nil {
		t.Error("parent that does not exist")
	}
	// A finished handoff is no parent.
	if _, _, err := Done(build, h4.ID, "applied", "done"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Done(build, h1.ID, "designed", "done"); err != nil {
		t.Fatal(err)
	}
	if h := mustHand(t, build, idCodex, "something fresh", ""); h.Parent != "" {
		t.Errorf("parent after finishing: %q", h.Parent)
	}
}

func TestChainSpawns(t *testing.T) {
	setup(t)
	mustCreate(t, "dev", idArch, idBuild, idCodex)
	h1, _, err := HandOff(arch, idBuild, "first of the chain", "", nil, "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = MarkSpawned(h1.ID)
	h2, _, _ := HandOff(build, idCodex, "second of the chain", "", nil, "dev", "")
	_ = MarkSpawned(h2.ID)
	if d, n, err := ChainState(idCodex, "", "dev"); err != nil || d != 2 || n != 2 {
		t.Errorf("ChainState = %d %d %v", d, n, err)
	}
	// A word from the person starts the count again.
	now = func() time.Time { return time.Date(2026, 10, 30, 14, 9, 0, 0, time.UTC) }
	mustPost(t, "dev", me, "carry on, this is fine")
	if _, n, _ := ChainState(idCodex, "", "dev"); n != 0 {
		t.Errorf("spawns after the person spoke = %d", n)
	}
}

func TestDeliveredStampsHandoff(t *testing.T) {
	setup(t)
	h := mustHand(t, arch, idBuild, "add the 404 case", "")
	if hs, _ := Handoffs(); len(hs) != 1 || !hs[0].DeliveredAt.IsZero() {
		t.Fatalf("before delivery: %+v", hs)
	}
	text, upto, _, _ := Compose(idBuild, now())
	if hs, _ := Handoffs(); !hs[0].DeliveredAt.IsZero() {
		t.Error("stamped by Compose")
	}
	// Another agent's delivery does not stamp it.
	_ = Delivered(idCodex, upto)
	if hs, _ := Handoffs(); !hs[0].DeliveredAt.IsZero() {
		t.Error("stamped by someone else's delivery")
	}
	if err := Delivered(idBuild, upto); err != nil {
		t.Fatal(err)
	}
	hs, _ := Handoffs()
	if hs[0].ID != h.ID || !hs[0].DeliveredAt.Equal(now()) || !strings.Contains(text, h.ID) {
		t.Errorf("after delivery: %+v", hs)
	}
}

func fakeTurn(t *testing.T, turns map[string]*status.Turn) {
	t.Helper()
	old := readTurn
	readTurn = func(id string) *status.Turn { return turns[id] }
	t.Cleanup(func() { readTurn = old })
}

func TestCheckLapses(t *testing.T) {
	setup(t)
	delivered := now()
	turns := map[string]*status.Turn{}
	fakeTurn(t, turns)
	h := mustHand(t, arch, idBuild, "add the 404 case", "")

	// Not delivered yet: a turn that ended is nothing to it.
	turns[idBuild] = &status.Turn{Reply: "early", At: delivered.Add(time.Minute)}
	if err := CheckLapses(delivered.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if hs, _ := Handoffs(); hs[0].Status != "open" {
		t.Fatalf("lapsed before delivery: %+v", hs[0])
	}
	if pendingBus(t, idArch) != 0 {
		t.Error("sender woken for nothing")
	}

	deliver(t, idBuild)
	// The turn that ended before the delivery, or as it came, is not it.
	for _, at := range []time.Time{delivered.Add(-time.Hour), delivered.Add(2 * time.Second)} {
		turns[idBuild] = &status.Turn{Reply: "old", At: at}
		if err := CheckLapses(delivered.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if hs, _ := Handoffs(); hs[0].Status != "open" {
			t.Fatalf("lapsed by a turn at %v", at)
		}
	}

	// A later turn with no bus_done is a lapse.
	reply := "I looked at it. " + strings.Repeat("long ", 200)
	turns[idBuild] = &status.Turn{Reply: reply, At: delivered.Add(3 * time.Minute)}
	if err := CheckLapses(delivered.Add(4 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	hs, _ := Handoffs()
	if hs[0].Status != "lapsed" || hs[0].DoneAt.IsZero() {
		t.Fatalf("after the turn: %+v", hs[0])
	}
	if pendingBus(t, idArch) != 1 {
		t.Error("sender not woken")
	}
	text := deliver(t, idArch)
	want := h.ID + " lapsed: builder ended its turn without bus_done. Its last words: I looked at it. "
	if !strings.Contains(text, want) {
		t.Errorf("lapse line lacks %q:\n%s", want, text)
	}
	if strings.Contains(text, reply) || !strings.Contains(text, "…") {
		t.Errorf("last words not cut to %d characters:\n%s", lapseWords, text)
	}
	// It is not lapsed twice, and cannot be finished any more.
	if err := CheckLapses(delivered.Add(5 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if msgs, _, _ := Read(h.Channel, 0, 50); len(msgs) != 2 {
		t.Errorf("lapsed twice: %d messages", len(msgs))
	}
	if _, _, err := Done(build, h.ID, "too late", "done"); err == nil {
		t.Error("finished a lapsed handoff")
	}
}

func TestTree(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	hs := []Handoff{
		{ID: "h-1", At: t0},
		{ID: "h-3", At: t0.Add(2 * time.Minute), Parent: "h-1"},
		{ID: "h-2", At: t0.Add(time.Minute)},
		{ID: "h-4", At: t0.Add(3 * time.Minute), Parent: "h-3"},
		{ID: "h-5", At: t0.Add(4 * time.Minute), Parent: "h-gone"},
	}
	list, depth := Tree(hs)
	var got []string
	for i, h := range list {
		got = append(got, strings.Repeat(">", depth[i])+h.ID)
	}
	if want := "h-1 >h-3 >>h-4 h-2 h-5"; strings.Join(got, " ") != want {
		t.Errorf("Tree = %v, want %s", got, want)
	}
}
