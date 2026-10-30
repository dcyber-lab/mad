package deck

import (
	"os"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// A claude that runs as a shell sleeping: its TUI as far as tmux knows.
var tuiKind = []agent.Kind{{Name: "claude", Start: `sh -c 'echo "tui $MAD_AGENT_ID"; sleep 600'`}}

func runsHost(t *testing.T, id string) bool {
	t.Helper()
	p, ok := tmux.FindPane(panes(t), id)
	return ok && RunsHost(p, id)
}

func modeOf(t *testing.T, id string) string {
	t.Helper()
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, a := st.FindAgent(id)
	return a.Mode
}

func TestWindowMode(t *testing.T) {
	useDeck(t)
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	a := &state.Agent{ID: "agent-1", Kind: "claude", Mode: state.ModeWindow}
	p.Agents = []*state.Agent{a}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if !Windowed(a) || Windowed(&state.Agent{Kind: "shell", Mode: state.ModeWindow}) {
		t.Fatal("Windowed")
	}

	// Started in window mode: mad host runs in its pane.
	if err := StartAgent(p, a, true, tuiKind); err != nil {
		t.Fatal(err)
	}
	if !runsHost(t, a.ID) {
		t.Fatal("window mode does not run mad host")
	}

	// Back to its TUI, and to the window again: restarted each time.
	if err := SetMode(a.ID, "", tuiKind); err != nil {
		t.Fatal(err)
	}
	if runsHost(t, a.ID) || modeOf(t, a.ID) != "" {
		t.Fatal("SetMode did not take it back to its TUI")
	}
	if err := SetMode(a.ID, state.ModeWindow, tuiKind); err != nil {
		t.Fatal(err)
	}
	if !runsHost(t, a.ID) || modeOf(t, a.ID) != state.ModeWindow {
		t.Fatal("SetMode did not put it in window mode")
	}

	// The terminal deck opening it takes it back, unless it is busy.
	if err := status.WriteHook(a.ID, &status.Hook{State: status.Running}, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ = state.Load()
	if err := OpenAgent(st, a.ID, tuiKind); err != nil {
		t.Fatal(err)
	}
	if !runsHost(t, a.ID) {
		t.Error("taken back while it works")
	}
	if err := status.WriteHook(a.ID, &status.Hook{State: status.Idle}, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ = state.Load()
	if err := OpenAgent(st, a.ID, tuiKind); err != nil {
		t.Fatal(err)
	}
	if runsHost(t, a.ID) || modeOf(t, a.ID) != "" {
		t.Error("the deck opening it did not take it back to its TUI")
	}
	if got := stageID(t); got != a.ID {
		t.Errorf("stage = %q", got)
	}
}
