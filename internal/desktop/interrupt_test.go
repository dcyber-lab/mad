package desktop

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/state"
)

// An interrupt goes the way the agent runs: its host's in window mode, Esc
// to its pane as its TUI, saying whether its tracker has it working; once
// in a short while.
func TestInterruptRoutesPerMode(t *testing.T) {
	isolate(t)
	s := New(manageKinds)
	s.poll.mu.Lock()
	s.poll.snap = Snapshot{Projects: []Project{{Agents: []Agent{
		{ID: "win", Mode: state.ModeWindow, Status: "running"}, {ID: "tui", Status: "running"}, {ID: "done", Status: "idle"}, {ID: "dead"},
	}}}}
	s.poll.mu.Unlock()
	var hosts, panes []string
	oldHost, oldPane, oldLive := hostInterrupt, paneInterrupt, livePane
	t.Cleanup(func() { hostInterrupt, paneInterrupt, livePane = oldHost, oldPane, oldLive })
	hostInterrupt = func(id string) error { hosts = append(hosts, id); return nil }
	paneInterrupt = func(pane string, working bool) error {
		panes = append(panes, fmt.Sprint(pane, " ", working))
		if pane == "%done" {
			return attention.ErrGone // not working, its screen silent
		}
		return nil
	}
	livePane = func(id string) (string, bool) { return "%" + id, id != "dead" }

	for _, c := range []struct {
		id   string
		code int
	}{
		{"win", http.StatusNoContent},
		{"tui", http.StatusNoContent},
		{"tui", http.StatusConflict}, // a moment ago
		{"done", http.StatusConflict},
		{"dead", http.StatusConflict},
		{"nobody", http.StatusNotFound},
	} {
		if w := call(t, s, "POST", "/api/interrupt?id="+c.id, ""); w.Code != c.code {
			t.Errorf("%s: %d %s, want %d", c.id, w.Code, w.Body.String(), c.code)
		}
	}
	if !reflect.DeepEqual(hosts, []string{"win"}) || !reflect.DeepEqual(panes, []string{"%tui true", "%done false"}) {
		t.Errorf("hosts %q panes %q", hosts, panes)
	}
}
