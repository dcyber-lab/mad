package ui

import (
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
)

// An agent sleeps only if waking it reopens its own session: claude's is
// its id, agy's and codex's must have been learnt.
func TestCanSleep(t *testing.T) {
	m := newModel(&state.State{}, agent.Builtin())
	cases := []struct {
		a    state.Agent
		want bool
	}{
		{state.Agent{ID: "a", Kind: "claude"}, true},
		{state.Agent{ID: "b", Kind: "agy"}, false},
		{state.Agent{ID: "c", Kind: "agy", SessionID: "ec33ebf9"}, true},
		{state.Agent{ID: "d", Kind: "codex"}, false},
		{state.Agent{ID: "e", Kind: "codex", SessionID: "019a"}, true},
		{state.Agent{ID: "f", Kind: "pi"}, false},
		{state.Agent{ID: "g", Kind: "shell"}, false},
	}
	for _, c := range cases {
		if got := m.canSleep(&c.a); got != c.want {
			t.Errorf("%s %+v: canSleep = %v", c.a.Kind, c.a, got)
		}
	}
}
