package bus

import (
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// board derives the cards at base+d, with agents standing as statuses say.
func board(t *testing.T, d time.Duration, statuses map[string]string) []Card {
	t.Helper()
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	cards, err := Board(st, statuses, time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC).Add(d))
	if err != nil {
		t.Fatal(err)
	}
	return cards
}

// cardOf is the card of task id.
func cardOf(t *testing.T, cards []Card, id string) Card {
	t.Helper()
	for _, c := range cards {
		if c.Task != nil && c.Task.ID == id {
			return c
		}
	}
	t.Fatalf("no card for %s in %d cards", id, len(cards))
	return Card{}
}

func wantCard(t *testing.T, cards []Card, id, column, why string) Card {
	t.Helper()
	c := cardOf(t, cards, id)
	if c.Column != column || c.Why != why {
		t.Errorf("%s: %s·%s, want %s·%s", id, c.Column, c.Why, column, why)
	}
	return c
}

func setNow(d time.Duration) {
	at := time.Date(2026, 10, 30, 14, 2, 0, 0, time.UTC).Add(d)
	now = func() time.Time { return at }
}

func TestBoardColumns(t *testing.T) {
	setup(t)
	fakeTurn(t, map[string]*status.Turn{})
	todo, _ := SetTask(arch, TaskChange{Title: sp("todo one"), Owner: sp(idBuild)})
	rev, _ := SetTask(arch, TaskChange{Title: sp("review one"), Owner: sp(idBuild), Status: sp("review")})
	doing, _ := SetTask(arch, TaskChange{Title: sp("doing one"), Owner: sp(idBuild), Status: sp("doing")})
	done, _ := SetTask(arch, TaskChange{Title: sp("done one"), Owner: sp(idBuild), Status: sp("done"), Note: sp("shipped")})
	dropped, _ := SetTask(arch, TaskChange{Title: sp("dropped one"), Status: sp("dropped")})
	mine, _ := SetTask(arch, TaskChange{Title: sp("yours"), Owner: sp(You)})
	idle := map[string]string{idBuild: status.Idle, idArch: status.Idle}

	cards := board(t, time.Minute, idle)
	wantCard(t, cards, todo.ID, ColTodo, "")
	wantCard(t, cards, rev.ID, ColReview, "")
	c := wantCard(t, cards, doing.ID, ColDoing, "")
	wantCard(t, cards, done.ID, ColDone, "")
	wantCard(t, cards, mine.ID, ColWaitingYou, "")
	if c.OwnerLabel != "builder" || c.Owner != idBuild || c.Age != time.Minute || c.Kind != CardTask {
		t.Errorf("card = %+v", c)
	}
	for _, c := range cards {
		if c.Task != nil && c.Task.ID == dropped.ID {
			t.Error("dropped task shown")
		}
	}
	// Waiting for you comes first.
	if cards[0].Column != ColWaitingYou || cards[len(cards)-1].Column != ColDone {
		t.Errorf("order: %s ... %s", cards[0].Column, cards[len(cards)-1].Column)
	}
	// Done goes after a day.
	for _, c := range board(t, 25*time.Hour, idle) {
		if c.Task != nil && c.Task.ID == done.ID {
			t.Error("done task shown after a day")
		}
	}
}

func TestBoardPermissionAndUnowned(t *testing.T) {
	setup(t)
	fakeTurn(t, map[string]*status.Turn{})
	a, _ := SetTask(arch, TaskChange{Title: sp("a"), Owner: sp(idBuild), Status: sp("doing")})
	b, _ := SetTask(me, TaskChange{Title: sp("b")}) // the person's, no owner yet
	cards := board(t, time.Minute, map[string]string{idBuild: status.Waiting})
	wantCard(t, cards, a.ID, ColWaitingYou, WhyPermission)
	wantCard(t, cards, b.ID, ColWaitingYou, WhyUnowned)

	// An owner that left the deck.
	gone := "gone-0000"
	ts, _ := Tasks()
	ts[0].Owner = gone
	if err := locked(func() error { return saveTasks(ts) }); err != nil {
		t.Fatal(err)
	}
	c := wantCard(t, board(t, time.Minute, nil), a.ID, ColWaitingYou, WhyUnowned)
	if c.OwnerLabel != "gone-000" {
		t.Errorf("label = %q", c.OwnerLabel)
	}
}

func TestBoardPaused(t *testing.T) {
	setup(t)
	fakeTurn(t, map[string]*status.Turn{})
	mustCreate(t, "dev", idArch, idBuild)
	mustCreate(t, "quiet", idArch)
	a, _ := SetTask(arch, TaskChange{Title: sp("a"), Owner: sp(idBuild), Channel: sp("dev")})
	b, _ := SetTask(arch, TaskChange{Title: sp("b"), Owner: sp(idBuild), Channel: sp("dev")})
	for _, ch := range []string{"dev", "quiet"} {
		if err := change(ch, func(c *Channel, _ []Message) error { c.Paused = true; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	cards := board(t, time.Minute, nil)
	wantCard(t, cards, a.ID, ColWaitingYou, WhyPaused)
	wantCard(t, cards, b.ID, ColWaitingYou, WhyPaused)
	paused := map[string]int{}
	for _, c := range cards {
		if c.Kind == CardPaused {
			paused[c.Channel]++
		}
	}
	if len(paused) != 2 || paused["dev"] != 1 || paused["quiet"] != 1 {
		t.Errorf("paused cards = %v, want one for each channel", paused)
	}
	if err := Resume("dev"); err != nil {
		t.Fatal(err)
	}
	wantCard(t, board(t, time.Minute, nil), a.ID, ColTodo, "")
}

func TestBoardAsked(t *testing.T) {
	setup(t)
	fakeTurn(t, map[string]*status.Turn{})
	mustCreate(t, "dev", idArch)
	h, _, err := HandOff(arch, idBuild, "pick the database", "", nil, "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	task := taskOfH(t, h)
	deliver(t, idBuild)
	idle := map[string]string{idBuild: status.Idle}

	// An @you before the task changed, or from someone else, is not its.
	setNow(time.Minute)
	mustPost(t, "dev", arch, "@you fyi, builder takes the database")
	wantCard(t, board(t, 2*time.Minute, idle), task.ID, ColDoing, "")

	setNow(2 * time.Minute)
	r := mustPost(t, "dev", build, "@you postgres or sqlite?")
	cards := board(t, 3*time.Minute, idle)
	c := wantCard(t, cards, task.ID, ColWaitingYou, WhyAsked)
	if c.Message == nil || c.Message.Seq != r.Seq || c.Message.Text != "@you postgres or sqlite?" {
		t.Errorf("message = %+v", c.Message)
	}
	for _, c := range cards { // the task carries it: no card of its own
		if c.Kind == CardAsk && c.Owner == idBuild {
			t.Errorf("ask card besides the task: %+v", c)
		}
	}

	// A reply from the person clears it.
	setNow(4 * time.Minute)
	mustPost(t, "dev", me, "postgres")
	wantCard(t, board(t, 5*time.Minute, idle), task.ID, ColDoing, "")
}

func TestBoardAskCards(t *testing.T) {
	setup(t)
	fakeTurn(t, map[string]*status.Turn{})
	mustCreate(t, "dev", idArch, idBuild)
	mustPost(t, "dev", arch, "@you may I drop the old table?")
	mustPost(t, "dev", build, "tests pass, nothing for you") // no @you
	ch, _, err := DM(build, You, "which branch should I base this on?")
	if err != nil {
		t.Fatal(err)
	}

	var asks []Card
	for _, c := range board(t, time.Minute, nil) {
		if c.Kind == CardAsk {
			asks = append(asks, c)
		}
	}
	if len(asks) != 2 {
		t.Fatalf("asks = %+v", asks)
	}
	byChan := map[string]Card{}
	for _, c := range asks {
		byChan[c.Channel] = c
		if c.Column != ColWaitingYou || c.Why != WhyAsked || c.Task != nil || c.Message == nil {
			t.Errorf("ask = %+v", c)
		}
	}
	if c := byChan["dev"]; c.OwnerLabel != "architect" || c.Message.Text != "@you may I drop the old table?" || c.Message.Seq != 1 {
		t.Errorf("channel ask = %+v", c)
	}
	if c := byChan[ch]; c.OwnerLabel != "builder" || c.Title != "which branch should I base this on?" {
		t.Errorf("DM ask = %+v", c)
	}

	// The person answers the DM, and speaks in the channel: both clear.
	setNow(time.Minute)
	if _, _, err := DM(me, idBuild, "main"); err != nil {
		t.Fatal(err)
	}
	mustPost(t, "dev", me, "yes")
	for _, c := range board(t, 2*time.Minute, nil) {
		if c.Kind == CardAsk {
			t.Errorf("answered, still asked: %+v", c)
		}
	}

	// A new question after the answer is asked again.
	setNow(3 * time.Minute)
	mustPost(t, "dev", arch, "@you and the index?")
	n := 0
	for _, c := range board(t, 4*time.Minute, nil) {
		if c.Kind == CardAsk {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d asks, want 1", n)
	}
}

func TestBoardBlocked(t *testing.T) {
	setup(t)
	turns := map[string]*status.Turn{}
	fakeTurn(t, turns)
	first, _ := SetTask(arch, TaskChange{Title: sp("schema"), Owner: sp(idCodex)})
	second, _ := SetTask(arch, TaskChange{Title: sp("handlers"), Owner: sp(idBuild), WaitsOn: &[]string{first.ID}})
	idle := map[string]string{idBuild: status.Idle, idCodex: status.Idle}
	wantCard(t, board(t, time.Minute, idle), second.ID, ColBlocked, WhyWaits)
	if _, err := SetTask(arch, TaskChange{ID: first.ID, Status: sp("done")}); err != nil {
		t.Fatal(err)
	}
	wantCard(t, board(t, time.Minute, idle), second.ID, ColTodo, "")

	// A lapsed handoff blocks its task, which stays doing as stored.
	h := mustHand(t, arch, idBuild, "check the index", "")
	deliver(t, idBuild)
	_, _, _ = queue.Take(idBuild) // the dispatcher took the pointer
	turns[idBuild] = &status.Turn{At: now().Add(time.Minute)}
	if err := CheckLapses(now().Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	task := taskOfH(t, h)
	if task.Status != TaskDoing {
		t.Fatalf("task = %+v", task)
	}
	wantCard(t, board(t, 3*time.Minute, idle), task.ID, ColBlocked, WhyLapsed)
}

func TestBoardStalled(t *testing.T) {
	setup(t)
	turns := map[string]*status.Turn{}
	fakeTurn(t, turns)
	h := mustHand(t, arch, idBuild, "rewrite the parser", "")
	deliver(t, idBuild)
	task := taskOfH(t, h)
	idle := map[string]string{idBuild: status.Idle}

	wantCard(t, board(t, 19*time.Minute, idle), task.ID, ColDoing, "")
	wantCard(t, board(t, 21*time.Minute, idle), task.ID, ColBlocked, WhyStalled)
	// Running is not stalled, however long.
	wantCard(t, board(t, 3*time.Hour, map[string]string{idBuild: status.Running}), task.ID, ColDoing, "")
	for _, s := range []string{status.Asleep, status.Stopped} {
		wantCard(t, board(t, 21*time.Minute, map[string]string{idBuild: s}), task.ID, ColBlocked, WhyStalled)
	}

	// A turn that ended starts the count again.
	turns[idBuild] = &status.Turn{At: now().Add(15 * time.Minute)}
	wantCard(t, board(t, 21*time.Minute, idle), task.ID, ColDoing, "")
	wantCard(t, board(t, 36*time.Minute, idle), task.ID, ColBlocked, WhyStalled)

	// So does a word on the bus.
	delete(turns, idBuild)
	setNow(25 * time.Minute)
	mustCreate(t, "dev", idBuild)
	mustPost(t, "dev", build, "@architect the parser is half done, lexer first")
	wantCard(t, board(t, 30*time.Minute, idle), task.ID, ColDoing, "")

	// A task written doing by its owner stalls the same way; todo never does.
	written, _ := SetTask(build, TaskChange{Title: sp("own work"), Owner: sp(idBuild), Status: sp("doing")})
	todo, _ := SetTask(build, TaskChange{Title: sp("later"), Owner: sp(idBuild)})
	cards := board(t, 2*time.Hour, idle)
	wantCard(t, cards, written.ID, ColBlocked, WhyStalled)
	wantCard(t, cards, todo.ID, ColTodo, "")

	// FollowUp 0 turns it off.
	old := FollowUp
	FollowUp = 0
	t.Cleanup(func() { FollowUp = old })
	wantCard(t, board(t, 2*time.Hour, idle), written.ID, ColDoing, "")
}
