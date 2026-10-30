package ui

import (
	"testing"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

// A DM of yours with a removed agent keeps a name: its sides are read from
// the id, as its members are gone.
func TestDMNameOfARemovedAgent(t *testing.T) {
	const a = "aaaa1111-0000-0000-0000-000000000001"
	c := bus.Channel{ID: bus.DMChannel(a, bus.You)} // no members left
	if got := dmName(&state.State{}, c); got != "aaaa1111" {
		t.Errorf("dmName = %q", got)
	}
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: a, Kind: "claude", Name: "architect"}}
	if got := dmName(st, c); got != "architect" {
		t.Errorf("dmName = %q", got)
	}
}
