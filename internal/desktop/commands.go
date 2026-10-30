package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/state"
)

// The message box offers the agent's slash commands as you type / at its
// start: its kind's, in its directory (discover.Commands); a new chat
// offers its kind's models (discover.Models). Finding them starts the
// agent headless for a few seconds, so each kind and directory is asked
// once at a time, and again only once the answer is old; the old one is
// given meanwhile.

const (
	commandsFresh = 5 * time.Minute
	commandsRetry = 30 * time.Second // after asking failed
)

// findCommands and findModels are discover's, replaced in tests.
var (
	findCommands = discover.Commands
	findModels   = discover.Models
)

// answer is what agents of a kind said in a directory, once they have.
type answer[T any] struct {
	ready chan struct{} // closed once the rest is set
	list  []T
	err   error
	at    time.Time
}

func (c *answer[T]) done() bool {
	select {
	case <-c.ready:
		return true
	default:
		return false
	}
}

func (c *answer[T]) stale(now time.Time) bool {
	ttl := commandsFresh
	if c.err != nil {
		ttl = commandsRetry
	}
	return now.Sub(c.at) > ttl
}

// answers keeps the answers to one question, by kind and directory.
type answers[T any] struct {
	mu    sync.Mutex
	find  func(ctx context.Context, kind, dir string) ([]T, error)
	lists map[string]*answer[T]
}

func newAnswers[T any](find func(ctx context.Context, kind, dir string) ([]T, error)) *answers[T] {
	return &answers[T]{find: find, lists: map[string]*answer[T]{}}
}

// get returns kind's answer in dir, once found, or nil when ctx ends
// first.
func (cc *answers[T]) get(ctx context.Context, kind, dir string) []T {
	key := kind + "\x00" + dir
	cc.mu.Lock()
	c := cc.lists[key]
	if c == nil || c.done() && c.stale(time.Now()) {
		fresh := &answer[T]{ready: make(chan struct{})}
		cc.lists[key] = fresh
		go func(old *answer[T]) {
			fresh.list, fresh.err = cc.find(context.Background(), kind, dir)
			if fresh.err != nil && old != nil && old.err == nil {
				fresh.list = old.list // what the agent said last time
			}
			fresh.at = time.Now()
			close(fresh.ready)
		}(c)
		if c == nil {
			c = fresh
		}
	}
	cc.mu.Unlock()
	select {
	case <-c.ready:
		return c.list
	case <-ctx.Done():
		return nil
	}
}

func (s *Server) commands(w http.ResponseWriter, r *http.Request) {
	st, err := state.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, a := st.FindAgent(r.URL.Query().Get("id"))
	if a == nil {
		http.Error(w, "no such agent", http.StatusNotFound)
		return
	}
	list := s.cmds.get(r.Context(), a.Kind, p.Dir(a))
	if list == nil {
		list = []discover.Command{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

// models answers GET /api/models?kind=KIND&project=PATH: the models a new
// chat of the kind can start on there.
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	st, err := state.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	dir, _ := os.UserHomeDir()
	if p := st.FindProject(q.Get("project")); p != nil {
		dir = p.Path
	}
	list := s.modelList.get(r.Context(), q.Get("kind"), dir)
	if list == nil {
		list = []discover.Model{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}
