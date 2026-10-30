package desktop

import (
	"net/http"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/tmux"
)

// looks keeps which agents a window looks at: the one its page shows
// while the window has the focus, as the page says every few seconds
// (/api/seen). What finished there was seen: no notification, and it is
// not put to sleep. The agent's pane is marked too, for the deck.
type looks struct {
	mu sync.Mutex
	at map[string]time.Time // agent id → when last said
}

func newLooks() *looks {
	return &looks{at: map[string]time.Time{}}
}

// Focused reports whether a window looks at agent id now.
func (l *looks) Focused(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Since(l.at[id]) < tmux.SeenFor
}

func (l *looks) saw(id string, now time.Time) {
	l.mu.Lock()
	l.at[id] = now
	l.mu.Unlock()
}

func (s *Server) seen(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	now := time.Now()
	s.looks.saw(id, now)
	if panes, err := tmux.ListPanes(); err == nil {
		if p, ok := tmux.FindPane(panes, id); ok {
			_ = tmux.MarkSeen(p.ID, now)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
