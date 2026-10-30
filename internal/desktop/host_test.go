package desktop

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestModeAPI(t *testing.T) {
	p, a := useDeck(t)
	a.Kind = "claude"
	if err := (&state.State{Projects: []*state.Project{p}}).Save(); err != nil {
		t.Fatal(err)
	}
	kinds := []agent.Kind{{Name: "claude", Start: "sleep 600 # tui", Hooks: true}}
	if err := drive.EnsureDeck(p.Path); err != nil {
		t.Fatal(err)
	}
	if err := deck.StartAgent(p, a, false, kinds); err != nil {
		t.Fatal(err)
	}
	s := New(kinds)
	h := s.Handler()
	post := func(id, mode string) int {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/mode?id="+id+"&mode="+mode+"&token="+s.Token, nil))
		if w.Code != 204 {
			t.Logf("mode %s: %d %s", mode, w.Code, w.Body)
		}
		return w.Code
	}
	runsHost := func() bool {
		panes, _ := tmux.ListPanes()
		pn, ok := tmux.FindPane(panes, a.ID)
		return ok && deck.RunsHost(pn, a.ID)
	}
	snapMode := func() string {
		s.poll.Poll(time.Now())
		return s.poll.Snapshot().Projects[0].Agents[0].Mode
	}

	// Working in its TUI: left alone.
	if err := status.WriteHook(a.ID, &status.Hook{State: status.Running}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.poll.Poll(time.Now())
	if c := post(a.ID, "window"); c != 409 || runsHost() {
		t.Fatalf("busy: %d", c)
	}
	// Idle: restarted in window mode, its host in its pane.
	if err := status.WriteHook(a.ID, &status.Hook{State: status.Idle}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.poll.Poll(time.Now())
	if c := post(a.ID, "window"); c != 204 || !runsHost() {
		t.Fatalf("idle: %d, host %v", c, runsHost())
	}
	if m := snapMode(); m != state.ModeWindow {
		t.Errorf("snapshot mode %q", m)
	}
	if c := post(a.ID, "window"); c != 204 || !runsHost() {
		t.Errorf("again: %d", c)
	}
	// Back to its TUI.
	if c := post(a.ID, "tui"); c != 204 || runsHost() {
		t.Errorf("tui: %d", c)
	}
	if m := snapMode(); m != "" {
		t.Errorf("snapshot mode %q after tui", m)
	}

	if c := post(a.ID, "nonsense"); c != 400 {
		t.Errorf("unknown mode: %d", c)
	}
	if c := post("nobody", "window"); c != 404 {
		t.Errorf("unknown agent: %d", c)
	}
}

// The page says which agent it shows: looked at for a while, here and in
// the deck (its pane marked).
func TestSeenAPI(t *testing.T) {
	p, a := useDeck(t)
	if err := drive.EnsureDeck(p.Path); err != nil {
		t.Fatal(err)
	}
	kinds := []agent.Kind{{Name: "fake", Start: "sleep 600"}}
	if err := deck.StartAgent(p, a, false, kinds); err != nil {
		t.Fatal(err)
	}
	s := New(kinds)
	if s.looks.Focused(a.ID) {
		t.Fatal("looked at before the page said so")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/seen?id="+a.ID+"&token="+s.Token, nil))
	if w.Code != 204 || !s.looks.Focused(a.ID) {
		t.Fatalf("seen: %d, focused %v", w.Code, s.looks.Focused(a.ID))
	}
	panes, _ := tmux.ListPanes()
	if pn, _ := tmux.FindPane(panes, a.ID); !pn.Viewed {
		t.Error("its pane is not marked viewed for the deck")
	}
}
