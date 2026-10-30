package desktop

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// What interrupt acts through; tests replace them.
var (
	hostInterrupt = func(id string) error { return host.Do(id, host.Command{Type: "interrupt"}) }
	paneInterrupt = attention.InterruptPane
	livePane      = func(id string) (string, bool) {
		panes, err := tmux.ListPanes()
		if err != nil {
			return "", false
		}
		p, ok := tmux.FindPane(panes, id)
		return p.ID, ok && !p.Dead
	}
)

// interruptGap is how long after an interrupt another to the same agent
// is refused: a second Esc to claude's TUI, idle by then, opens its
// rewind menu.
const interruptGap = 2 * time.Second

var errNotWorking = errors.New("it is not working: there is nothing to interrupt")

// interrupts are when each agent was last interrupted.
type interrupts struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// claim tells whether agent id may be interrupted at now, and if so notes it.
func (in *interrupts) claim(id string, now time.Time) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	if at, ok := in.last[id]; ok && now.Sub(at) < interruptGap {
		return false
	}
	if in.last == nil {
		in.last = map[string]time.Time{}
	}
	in.last[id] = now
	return true
}

// interrupt answers POST /api/interrupt?id=: stop what agent id does now,
// as Esc does in its TUI, from the window or the phone. In window mode its
// host interrupts the turn; run as its TUI, its pane gets Esc while the
// screen says that interrupts it.
//
//	204: done
//	404: no such agent
//	409: it is not working, or was interrupted a moment ago
func (s *Server) interrupt(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var mode string
	found := false
	for _, p := range s.poll.Snapshot().Projects {
		for _, a := range p.Agents {
			if a.ID == id {
				mode, found = a.Mode, true
			}
		}
	}
	if !found {
		fail(w, http.StatusNotFound, errNotFound)
		return
	}
	if !s.interrupts.claim(id, time.Now()) {
		fail(w, http.StatusConflict, errors.New("it was interrupted a moment ago"))
		return
	}
	var err error
	if mode == state.ModeWindow {
		err = hostInterrupt(id)
	} else if pane, ok := livePane(id); ok {
		err = paneInterrupt(pane)
	} else {
		err = errNotWorking
	}
	switch {
	case err == nil:
		changed()
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, attention.ErrGone), errors.Is(err, errNotWorking):
		fail(w, http.StatusConflict, errNotWorking)
	default:
		fail(w, http.StatusBadRequest, err)
	}
}
