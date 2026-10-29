package desktop

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
)

// The window lines messages up the way `mad queue` does; the deck types
// them in. Each answers with the agent's queue as it is now.

// maxMessage bounds a queued message: a prompt, not a file.
const maxMessage = 256 << 10

func (s *Server) queueAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := knownAgent(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMessage))
	if err != nil {
		http.Error(w, "message too long", http.StatusRequestEntityTooLarge)
		return
	}
	if _, err := queue.Push(id, string(body), time.Now()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.queueReply(w, id)
}

func (s *Server) queueRemove(w http.ResponseWriter, r *http.Request) {
	id, ok := knownAgent(w, r)
	if !ok {
		return
	}
	var err error
	if item := r.URL.Query().Get("item"); item != "" {
		err = queue.Remove(id, item)
	} else {
		err = queue.Clear(id)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.queueReply(w, id)
}

func (s *Server) queuePause(w http.ResponseWriter, r *http.Request) {
	id, ok := knownAgent(w, r)
	if !ok {
		return
	}
	if err := queue.SetPaused(id, r.URL.Query().Get("paused") == "1"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.queueReply(w, id)
}

func (s *Server) queueReply(w http.ResponseWriter, id string) {
	q, err := queue.Load(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(q)
}

// knownAgent is the request's agent id, refused unless the deck has it: the
// id names a file.
func knownAgent(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.URL.Query().Get("id")
	st, err := state.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	if _, a := st.FindAgent(id); a == nil || id == "" {
		http.Error(w, "no such agent", http.StatusNotFound)
		return "", false
	}
	return id, true
}
