package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/state"
)

// The message box offers the agent's slash commands as you type / at its
// start: its kind's, in its directory (discover.Commands). Finding them
// starts the agent headless for a few seconds, so each kind and directory
// is asked once at a time, and again only once the answer is old; the
// old one is given meanwhile.

const (
	commandsFresh = 5 * time.Minute
	commandsRetry = 30 * time.Second // after asking failed
)

// findCommands is discover.Commands, replaced in tests.
var findCommands = discover.Commands

type commandList struct {
	ready chan struct{} // closed once the rest is set
	list  []discover.Command
	err   error
	at    time.Time
}

func (c *commandList) done() bool {
	select {
	case <-c.ready:
		return true
	default:
		return false
	}
}

func (c *commandList) stale(now time.Time) bool {
	ttl := commandsFresh
	if c.err != nil {
		ttl = commandsRetry
	}
	return now.Sub(c.at) > ttl
}

type commandCache struct {
	mu    sync.Mutex
	lists map[string]*commandList // by kind and directory
}

func newCommandCache() *commandCache {
	return &commandCache{lists: map[string]*commandList{}}
}

// get returns kind's commands in dir, once found, or nil when ctx ends
// first.
func (cc *commandCache) get(ctx context.Context, kind, dir string) []discover.Command {
	key := kind + "\x00" + dir
	cc.mu.Lock()
	c := cc.lists[key]
	if c == nil || c.done() && c.stale(time.Now()) {
		fresh := &commandList{ready: make(chan struct{})}
		cc.lists[key] = fresh
		go func(old *commandList) {
			fresh.list, fresh.err = findCommands(context.Background(), kind, dir)
			if fresh.err != nil && old != nil && old.err == nil {
				fresh.list = old.list // what the agent said last time, not its TUI's own alone
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
