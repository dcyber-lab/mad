package cli

import (
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

// A DM whose agent was removed still names its sides in `mad bus ls`: the
// channel's id keeps them when its members don't.
func TestBusLsNamesADMOfARemovedAgent(t *testing.T) {
	isolate(t)
	const a = "aaaa1111-0000-0000-0000-000000000001"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: a, Kind: "claude", Name: "architect"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.DM(bus.From{ID: a, Label: "architect"}, bus.You, "a word for you"); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := run(t, "", "bus", "ls"); !strings.Contains(out, "@architect") {
		t.Fatalf("bus ls = %q", out)
	}

	p.Agents = nil
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	bus.Forget(a)
	_, out, _ := run(t, "", "bus", "ls")
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "dm.") || strings.HasPrefix(strings.TrimSpace(line), "@ ") || strings.HasPrefix(line, "@  ") {
			t.Errorf("a nameless DM row: %q", line)
		}
	}
	if !strings.Contains(out, "@aaaa1111") {
		t.Errorf("bus ls does not name the DM by its id: %q", out)
	}
}

// bus ls names a DM from the sides in its id, whatever its members lost: a
// DM between two keeps both when one is forgotten, and a DM with you names
// the agent alone.
func TestBusLsDMSidesFromID(t *testing.T) {
	isolate(t)
	const a = "aaaa1111-0000-0000-0000-000000000001"
	const b = "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: a, Kind: "claude", Name: "architect"}, {ID: b, Kind: "claude", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.DM(bus.From{ID: a, Label: "architect"}, b, "a word for builder"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.DM(bus.From{ID: a, Label: "architect"}, bus.You, "a word for you"); err != nil {
		t.Fatal(err)
	}
	p.Agents = p.Agents[1:]
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	bus.Forget(a)
	_, out, _ := run(t, "", "bus", "ls")
	if strings.Contains(out, "you") {
		t.Errorf("a DM with you names you:\n%s", out)
	}
	if !strings.Contains(out, "@aaaa1111 @builder") && !strings.Contains(out, "@builder @aaaa1111") {
		t.Errorf("the DM between two lost a side:\n%s", out)
	}
	if !strings.Contains(out, "@aaaa1111  ") {
		t.Errorf("the DM with you is not named for the agent alone:\n%s", out)
	}
}
