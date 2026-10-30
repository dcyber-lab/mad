package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// With the day's budget spent, the dispatcher leaves a bus item queued
// (unless a person's message is in its digest) while the person's own
// queued messages go on. What it sends of mad's own is recorded to be
// counted; the person's is not.
func TestDispatchSpendGate(t *testing.T) {
	m, st := setup(t, "/code/a")
	const to, from = "bbbb2222-0000-0000-0000-000000000002", "aaaa1111-0000-0000-0000-000000000001"
	st.Projects[0].Agents = []*state.Agent{
		{ID: from, Kind: "claude", Name: "architect"}, {ID: to, Kind: "claude", Name: "builder"},
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	m.rebuildRows()
	m.trackers[to] = &status.Tracker{Status: status.Idle}
	m.trackers[from] = &status.Tracker{Status: status.Idle}
	you := bus.From{ID: bus.You, Label: bus.You}
	if _, err := bus.Create("api", "", []string{from, to}, you); err != nil {
		t.Fatal(err)
	}
	// The architect's mention queues a pointer for the builder...
	if _, err := bus.Post("api", bus.From{ID: from, Label: "architect"}, "@builder please start on the handler", 0); err != nil {
		t.Fatal(err)
	}
	// ...then the day's budget (1 turn) is spent. The person is told.
	var told []notify.Event
	oldSend := notify.Send
	t.Cleanup(func() { notify.Send = oldSend })
	notify.Send = func(_ notify.Config, e notify.Event) error { told = append(told, e); return nil }
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	if err := os.MkdirAll(filepath.Join(cfgDir, "mad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "mad", "config.json"), []byte(`{"bus":{"budget":{"turns":1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := bus.SpendSent(from, 0, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := status.WriteTurn(from, &status.Turn{Reply: "ok"}, now); err != nil {
		t.Fatal(err)
	}
	if err := bus.SpendSettle(now, nil); err != nil {
		t.Fatal(err)
	}
	if bus.SpendOK(now) {
		t.Fatal("the gate is open")
	}
	if len(told) != 1 || told[0].Kind != notify.Budget || !strings.Contains(told[0].Message, "agents wait for you") {
		t.Fatalf("told = %+v", told)
	}

	var sent []string
	old := sendQueued
	t.Cleanup(func() { sendQueued = old })
	sendQueued = func(_ *state.State, a *state.Agent, text string, _ []agent.Kind) error {
		sent = append(sent, a.ID+": "+text)
		return nil
	}
	run := func(at time.Time) dispatchedMsg {
		t.Helper()
		cmd := m.dispatchCmd(at)
		if cmd == nil {
			t.Fatal("nothing offered")
		}
		msg := cmd().(dispatchedMsg)
		m.applyDispatched(msg)
		return msg
	}

	// The person's queued message goes; the agent's mention is held.
	if _, err := queue.Push(to, "look at the logs", now); err != nil {
		t.Fatal(err)
	}
	if q, _ := queue.Load(to); len(q.Items) != 2 || !q.Items[0].Bus {
		t.Fatalf("queue = %+v", q)
	}
	if msg := run(now); msg.err != nil || len(sent) != 1 || sent[0] != to+": look at the logs" {
		t.Fatalf("the person's message did not go: %+v %q", msg, sent)
	}
	later := now.Add(queueSettle + time.Second)
	msg := run(later)
	if msg.wait != bus.SpendWait || !msg.spent || len(sent) != 1 {
		t.Fatalf("the bus item was not held: %+v sent=%q", msg, sent)
	}
	if q, _ := queue.Load(to); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Fatalf("queue = %+v", q)
	}
	if !m.spend.Closed {
		t.Error("the footer was not told")
	}

	// The person speaks in the channel: that digest is theirs, and goes.
	if _, err := bus.Post("api", you, "@builder and the tests too", 0); err != nil {
		t.Fatal(err)
	}
	if msg := run(later.Add(time.Minute)); msg.spent || len(sent) != 2 || !strings.Contains(sent[1], "and the tests too") {
		t.Fatalf("the person's digest was held: %+v sent=%q", msg, sent)
	}
	// It was not counted: no turn is open for the builder.
	if err := status.WriteTurn(to, &status.Turn{Reply: "ok"}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := bus.SpendSettle(now, nil); err != nil {
		t.Fatal(err)
	}
	if s := bus.SpendNow(now); s.Turns != 1 {
		t.Errorf("turns = %d, want the one spent before", s.Turns)
	}
}

// An open gate counts the turn mad started by itself (a digest with no
// person in it) and settles it with the agent's cost.
func TestDispatchSpendCounted(t *testing.T) {
	m, st := setup(t, "/code/a")
	const to, from = "bbbb2222-0000-0000-0000-000000000002", "aaaa1111-0000-0000-0000-000000000001"
	st.Projects[0].Agents = []*state.Agent{
		{ID: from, Kind: "claude", Name: "architect"}, {ID: to, Kind: "claude", Name: "builder"},
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	m.rebuildRows()
	m.trackers[to] = &status.Tracker{Status: status.Idle}
	you := bus.From{ID: bus.You, Label: bus.You}
	if _, err := bus.Create("api", "", []string{from, to}, you); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Post("api", bus.From{ID: from, Label: "architect"}, "@builder please start on the handler", 0); err != nil {
		t.Fatal(err)
	}
	old := sendQueued
	t.Cleanup(func() { sendQueued = old })
	sendQueued = func(*state.State, *state.Agent, string, []agent.Kind) error { return nil }
	cmd := m.dispatchCmd(time.Now())
	if cmd == nil {
		t.Fatal("nothing offered")
	}
	if msg := cmd().(dispatchedMsg); msg.err != nil || msg.spent {
		t.Fatalf("dispatched: %+v", msg)
	}
	// Its turn ends later: the settle adds it, with no cost for no cost data.
	if err := status.WriteTurn(to, &status.Turn{Reply: "ok"}, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := bus.SpendSettle(time.Now(), map[string]float64{to: 0.5}); err != nil {
		t.Fatal(err)
	}
	if s := bus.SpendNow(time.Now()); s.Turns != 1 || s.USD != 0.5 {
		t.Errorf("spend = %+v", s)
	}
}
