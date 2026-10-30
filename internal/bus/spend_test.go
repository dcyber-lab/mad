package bus

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// spendSetup is followSetup with the budget fixed: usd dollars and turns
// turns a day.
func spendSetup(t *testing.T, usd float64, turns int) map[string]*status.Turn {
	t.Helper()
	ts, _ := followSetup(t)
	old := budget
	budget = func() (float64, int) { return usd, turns }
	t.Cleanup(func() { budget = old })
	return ts
}

// spendTurn is one turn mad started for id (cost before: before), which
// ends a minute later at a cost of after, and is settled.
func spendTurn(t *testing.T, turns map[string]*status.Turn, id string, before, after float64) {
	t.Helper()
	sent := now()
	if err := SpendSent(id, before, sent); err != nil {
		t.Fatal(err)
	}
	turns[id] = &status.Turn{Reply: "done", At: sent.Add(time.Minute)}
	setNow(time.Duration(sent.Sub(time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC))) + 2*time.Minute)
	if err := SpendSettle(now(), map[string]float64{id: after}); err != nil {
		t.Fatal(err)
	}
}

func TestSpendSettleAttributesDelta(t *testing.T) {
	turns := spendSetup(t, 10, 100)
	if err := SpendSent(idBuild, 12.40, now()); err != nil {
		t.Fatal(err)
	}
	if err := SpendSent(idCodex, 0, now()); err != nil {
		t.Fatal(err)
	}
	// Nobody's turn has ended: nothing counted yet.
	if err := SpendSettle(now(), map[string]float64{idBuild: 13}); err != nil {
		t.Fatal(err)
	}
	if s := SpendNow(now()); s.USD != 0 || s.Turns != 0 {
		t.Fatalf("counted before the turns ended: %+v", s)
	}
	// An older turn ending does not close an entry sent after it.
	turns[idBuild] = &status.Turn{At: now().Add(-time.Minute)}
	_ = SpendSettle(now(), map[string]float64{idBuild: 13})
	if s := SpendNow(now()); s.Turns != 0 {
		t.Fatalf("an old turn settled a new entry: %+v", s)
	}

	turns[idBuild] = &status.Turn{At: now().Add(time.Minute)}
	turns[idCodex] = &status.Turn{At: now().Add(time.Minute)}
	setNow(3 * time.Minute)
	// claude's cost goes by its delta; codex has no cost, only a turn.
	if err := SpendSettle(now(), map[string]float64{idBuild: 14.71}); err != nil {
		t.Fatal(err)
	}
	s := SpendNow(now())
	if s.Turns != 2 || s.USD < 2.309 || s.USD > 2.311 {
		t.Fatalf("spend = %+v, want 2 turns and $2.31", s)
	}
	// And only once.
	_ = SpendSettle(now(), map[string]float64{idBuild: 20})
	if s2 := SpendNow(now()); s2.Turns != 2 || s2.USD != s.USD {
		t.Fatalf("settled twice: %+v", s2)
	}
	// A cost that went down (a new session) is no negative spend.
	_ = SpendSent(idBuild, 20, now())
	turns[idBuild] = &status.Turn{At: now().Add(time.Minute)}
	setNow(5 * time.Minute)
	_ = SpendSettle(now(), map[string]float64{idBuild: 1})
	if s3 := SpendNow(now()); s3.Turns != 3 || s3.USD != s.USD {
		t.Fatalf("after a cost reset: %+v", s3)
	}
}

func TestSpendPersonsTurnsNotCounted(t *testing.T) {
	spendSetup(t, 10, 100)
	mustCreate(t, "api", idBuild, idArch)
	drain(t, idBuild)

	// mad's own: an agent's mention.
	mustPost(t, "api", arch, "@builder what is the status of the endpoint work")
	_, _, yours, ok, err := ComposeCounted(idBuild, now())
	if err != nil || !ok || yours {
		t.Fatalf("an agent's digest: yours=%v ok=%v err=%v", yours, ok, err)
	}
	if PersonWaits(idBuild) {
		t.Error("PersonWaits for an agent's mention")
	}
	// The person's message makes the digest theirs, whoever else is in it.
	mustPost(t, "api", me, "@builder please look at the tests too")
	_, _, yours, ok, err = ComposeCounted(idBuild, now())
	if err != nil || !ok || !yours {
		t.Fatalf("a digest with the person's message: yours=%v ok=%v err=%v", yours, ok, err)
	}
	if !PersonWaits(idBuild) {
		t.Error("PersonWaits missed the person's message")
	}
}

func TestSpendGateClosesHoldsAndTellsOnce(t *testing.T) {
	turns := spendSetup(t, 100, 2)
	mustCreate(t, "api", idBuild, idArch)
	drain(t, idBuild)
	drain(t, idArch)

	spendTurn(t, turns, idBuild, 0, 1)
	if !SpendOK(now()) {
		t.Fatal("closed after one of two turns")
	}
	if len(personTold) != 0 {
		t.Fatalf("told early: %v", personTold)
	}
	spendTurn(t, turns, idArch, 0, 1.5)
	if SpendOK(now()) {
		t.Fatal("open at the turn bound")
	}
	if len(personTold) != 1 || !strings.HasPrefix(personTold[0], "budget: mad spent $2.50 on its own today (2 turns)") {
		t.Fatalf("told = %v", personTold)
	}
	// Once a day: more settling says nothing.
	_ = SpendSettle(now(), nil)
	_ = SpendSettle(now(), nil)
	if len(personTold) != 1 {
		t.Fatalf("told again: %v", personTold)
	}

	// An agent's post is stored and wakes no one, with a warning.
	res := mustPost(t, "api", arch, "@builder can you take the endpoint work now")
	if len(res.Woke) != 0 || !strings.Contains(res.Warning, "budget for agent-to-agent work is spent") {
		t.Fatalf("closed post: %+v", res)
	}
	if n := pendingBus(t, idBuild); n != 0 {
		t.Errorf("%d bus items queued for builder", n)
	}
	// A handoff is refused, nothing made.
	if _, _, err := HandOff(arch, idBuild, "implement the endpoint", "", nil, "api", ""); !errors.Is(err, ErrSpent) {
		t.Fatalf("handoff err = %v", err)
	}
	if hs, _ := Handoffs(); len(hs) != 0 {
		t.Errorf("handoffs = %+v", hs)
	}
	// A DM to the person still reaches them, with no warning of the gate.
	_, res, err := DM(arch, You, "the endpoint work is done, please review it")
	if err != nil || res.Warning != "" {
		t.Fatalf("dm to the person: %+v %v", res, err)
	}
	// The person's own posts go on and wake.
	res = mustPost(t, "api", me, "@builder carry on with the tests")
	if len(res.Woke) != 1 || res.Warning != "" {
		t.Fatalf("the person's post: %+v", res)
	}
	if n := pendingBus(t, idBuild); n != 1 {
		t.Errorf("%d bus items queued for builder after the person's post", n)
	}
}

func TestSpendGateHoldsNudges(t *testing.T) {
	turns := spendSetup(t, 100, 1)
	if err := SetLead("dev", idArch); err != nil {
		t.Fatal(err)
	}
	doingTask(t, "add the 404 case")
	drain(t, idBuild)
	spendTurn(t, turns, idArch, 0, 0)
	if SpendOK(now()) {
		t.Fatal("still open")
	}
	check(t, 25*time.Minute, idleBoth)
	if n := nudgesIn(t, "dev"); len(n) != 0 {
		t.Fatalf("nudged with the gate closed: %+v", n)
	}
	// A nudge posted anyway is refused too.
	if _, err := post(postSpec{channel: "dev", from: madFrom, kind: "nudge", text: "[mad] follow-up: still there", mentions: []string{idBuild}}); !errors.Is(err, errNudgeSpent) {
		t.Fatalf("direct nudge err = %v", err)
	}
	// Opened for the day, the next check nudges.
	if err := SpendOpenToday(now()); err != nil {
		t.Fatal(err)
	}
	check(t, 26*time.Minute, idleBoth)
	if n := nudgesIn(t, "dev"); len(n) != 1 {
		t.Fatalf("after opening: %+v", n)
	}
}

func TestSpendRaiseOpenAndMidnight(t *testing.T) {
	turns := spendSetup(t, 4, 40)
	mustCreate(t, "api", idBuild, idArch)
	spendTurn(t, turns, idBuild, 0, 4.5)
	if SpendOK(now()) {
		t.Fatal("open over the dollar bound")
	}
	if len(personTold) != 1 {
		t.Fatalf("told = %v", personTold)
	}

	// Raised by $1: the gate opens (a share of the turns too).
	if err := SpendRaise(1, now()); err != nil {
		t.Fatal(err)
	}
	s := SpendNow(now())
	if s.Closed || s.BudgetUSD != 5 || s.BudgetTurns != 50 {
		t.Fatalf("after +1: %+v", s)
	}
	// Spent again: the person is told again, having just acted.
	spendTurn(t, turns, idArch, 0, 1)
	if SpendOK(now()) || len(personTold) != 2 {
		t.Fatalf("after spending the raise: ok=%v told=%v", SpendOK(now()), personTold)
	}
	// Opened for today: no bound.
	if err := SpendOpenToday(now()); err != nil {
		t.Fatal(err)
	}
	if !SpendOK(now()) {
		t.Fatal("closed after open")
	}
	if SpendRaise(0, now()) == nil {
		t.Error("raised by nothing")
	}

	// Local midnight: a new day, nothing spent, nothing raised, not opened.
	setNow(10 * time.Hour)
	s = SpendNow(now())
	if s.Day != "2026-10-31" || s.USD != 0 || s.Turns != 0 || s.Closed || s.Opened || s.BudgetUSD != 4 {
		t.Fatalf("next day: %+v", s)
	}
	// And the same bound applies there, told once more.
	spendTurn(t, turns, idBuild, 0, 4)
	if SpendOK(now()) || len(personTold) != 3 {
		t.Fatalf("next day: ok=%v told=%v", SpendOK(now()), personTold)
	}
}

func TestSpendBoundsOff(t *testing.T) {
	turns := spendSetup(t, 0, 2) // dollars off, turns still gate
	spendTurn(t, turns, idBuild, 0, 500)
	if !SpendOK(now()) {
		t.Fatal("closed by dollars with the dollar bound off")
	}
	spendTurn(t, turns, idArch, 0, 0)
	if SpendOK(now()) {
		t.Fatal("open over the turn bound")
	}

	turns = spendSetup(t, 0, 0) // both off
	spendTurn(t, turns, idBuild, 0, 500)
	spendTurn(t, turns, idArch, 0, 500)
	if !SpendOK(now()) {
		t.Fatal("closed with no bounds")
	}
}

func TestSpendStaleEntrySettles(t *testing.T) {
	spendSetup(t, 100, 100)
	if err := SpendSent(idBuild, 1, now()); err != nil {
		t.Fatal(err)
	}
	setNow(spendStale + time.Minute)
	if err := SpendSettle(now(), map[string]float64{idBuild: 3}); err != nil {
		t.Fatal(err)
	}
	if s := SpendNow(now()); s.Turns != 1 || s.USD != 2 {
		t.Fatalf("stale entry: %+v", s)
	}
	if l := loadSpend(now()); len(l.Open) != 0 {
		t.Errorf("open = %+v", l.Open)
	}
}
