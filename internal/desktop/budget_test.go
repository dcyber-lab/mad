package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// A spent budget is a card waiting for the person, with the day in the
// snapshot; POST /api/budget raises it or opens it, and the card goes.
func TestBudgetCard(t *testing.T) {
	dir := isolate(t)
	const a1 = "aaaa1111-0000-0000-0000-000000000001"
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: t.TempDir(), Agents: []*state.Agent{{ID: a1, Kind: "claude", Name: "alice"}}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config", "mad")
	_ = os.MkdirAll(cfg, 0o755)
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"bus":{"budget":{"usd":10,"turns":2}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldSend := notify.Send
	t.Cleanup(func() { notify.Send = oldSend })
	notify.Send = func(notify.Config, notify.Event) error { return nil }
	s := New(manageKinds)
	snap := func() Snapshot {
		t.Helper()
		s.poll.Poll(time.Now())
		var out Snapshot
		_ = json.Unmarshal(call(t, s, "GET", "/api/deck", "").Body.Bytes(), &out)
		return out
	}
	budgetCard := func(sn Snapshot) *bus.Card {
		for i, c := range sn.Board {
			if c.Kind == "budget" {
				return &sn.Board[i]
			}
		}
		return nil
	}

	if sn := snap(); sn.Spend.Closed || budgetCard(sn) != nil || sn.Spend.BudgetTurns != 2 {
		t.Fatalf("fresh day: %+v %+v", sn.Spend, sn.Board)
	}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if err := bus.SpendSent(a1, 0, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := status.WriteTurn(a1, &status.Turn{Reply: "ok"}, now); err != nil {
			t.Fatal(err)
		}
		if err := bus.SpendSettle(now, map[string]float64{a1: float64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	sn := snap()
	c := budgetCard(sn)
	if !sn.Spend.Closed || sn.Spend.Turns != 2 || c == nil || c.Column != "waiting-you" || c.Why != "budget" {
		t.Fatalf("spent: %+v card=%+v", sn.Spend, c)
	}

	if w := call(t, s, "POST", "/api/budget", `{"op":"raise","usd":5}`); w.Code != 200 {
		t.Fatalf("raise: %d %s", w.Code, w.Body)
	}
	if sn := snap(); sn.Spend.Closed || budgetCard(sn) != nil || sn.Spend.BudgetTurns != 3 {
		t.Fatalf("after raising: %+v", sn.Spend)
	}
	if w := call(t, s, "POST", "/api/budget", `{"op":"raise","usd":0}`); w.Code != 400 {
		t.Errorf("raise by 0: %d", w.Code)
	}
	if w := call(t, s, "POST", "/api/budget", `{"op":"nope"}`); w.Code != 400 {
		t.Errorf("bad op: %d", w.Code)
	}
	if w := call(t, s, "POST", "/api/budget", `{"op":"open"}`); w.Code != 200 {
		t.Fatalf("open: %d %s", w.Code, w.Body)
	}
	if sn := snap(); !sn.Spend.Opened || sn.Spend.Closed {
		t.Fatalf("after open: %+v", sn.Spend)
	}
}
