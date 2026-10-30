package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// mad bus budget shows the day, +N and open reopen it (the person's, not an
// agent's), and with it spent an agent's bus_post warns and bus_handoff is
// refused.
func TestBusBudget(t *testing.T) {
	dir := isolate(t)
	const a, b = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: a, Kind: "claude", Name: "architect"}, {ID: b, Kind: "claude", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config", "mad")
	_ = os.MkdirAll(cfg, 0o755)
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"bus":{"budget":{"usd":4,"turns":1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldSend := notify.Send
	t.Cleanup(func() { notify.Send = oldSend })
	notify.Send = func(notify.Config, notify.Event) error { return nil }

	if code, out, _ := run(t, "", "bus", "budget"); code != 0 || !strings.Contains(out, "$0.00 of $4 · 0 of 1 turns; open") {
		t.Fatalf("budget = %d %q", code, out)
	}
	// One turn of mad's own, ended: the day is spent.
	now := time.Now()
	if err := bus.SpendSent(b, 1, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := status.WriteTurn(b, &status.Turn{Reply: "ok"}, now); err != nil {
		t.Fatal(err)
	}
	if err := bus.SpendSettle(now, map[string]float64{b: 1.5}); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "", "bus", "budget"); code != 0 || !strings.Contains(out, "$0.50 of $4 · 1 of 1 turns; spent: agents wait for you") {
		t.Fatalf("budget = %d %q", code, out)
	}

	// What an agent is told.
	if _, err := bus.Create("api", "", []string{a, b}, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		t.Fatal(err)
	}
	res, err := bus.Post("api", bus.From{ID: a, Label: "architect"}, "@builder take the handler work please", 0)
	if err != nil || len(res.Woke) != 0 || !strings.Contains(res.Warning, "mad bus budget") {
		t.Fatalf("post = %+v %v", res, err)
	}
	if _, err := mcpHandoff(a, "builder", "write the handler", "", nil, "api", ""); err == nil || !strings.Contains(err.Error(), "do not hand it off") {
		t.Fatalf("handoff err = %v", err)
	}
	// It cannot raise its own budget.
	t.Setenv("MAD_AGENT_ID", a)
	if code, _, errs := run(t, "", "bus", "budget", "open"); code == 0 || !strings.Contains(errs, "the person's to change") {
		t.Fatalf("an agent opened the budget: %d %q", code, errs)
	}
	t.Setenv("MAD_AGENT_ID", "")

	// The person raises it: $1 of $4 is a quarter of the turns, rounded up.
	if code, out, errs := run(t, "", "bus", "budget", "+1"); code != 0 || !strings.Contains(out, "of $5 · 1 of 2 turns; open") {
		t.Fatalf("+1 = %d %q %q", code, out, errs)
	}
	if res, err := bus.Post("api", bus.From{ID: a, Label: "architect"}, "@builder take the handler work please", 0); err != nil || len(res.Woke) != 1 || res.Warning != "" {
		t.Fatalf("post after raising = %+v %v", res, err)
	}
	if code, _, _ := run(t, "", "bus", "budget", "five"); code == 0 {
		t.Error("accepted a bad amount")
	}
	if code, out, _ := run(t, "", "bus", "budget", "open"); code != 0 || !strings.Contains(out, "lifted for today") {
		t.Fatalf("open = %d %q", code, out)
	}
}
