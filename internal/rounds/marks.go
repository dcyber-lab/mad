package rounds

import (
	"sync"
	"time"
)

// Marks says which agents' next turn is mad's own: set by the dispatcher
// (in a job, on another goroutine) as it delivers, taken by the poll that
// sees the turn start.
type Marks struct {
	mu sync.Mutex
	at map[string]time.Time
}

// markTTL: a mark whose turn never showed up is forgotten.
const markTTL = 10 * time.Minute

// Mark notes at now that id's next turn is one mad starts.
func (k *Marks) Mark(id string, now time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.at == nil {
		k.at = map[string]time.Time{}
	}
	k.at[id] = now
}

// Clear forgets id's mark: its next turn is the person's.
func (k *Marks) Clear(id string) {
	k.mu.Lock()
	delete(k.at, id)
	k.mu.Unlock()
}

// Take reports whether id's turn starting at now is mad's, and clears the mark.
func (k *Marks) Take(id string, now time.Time) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	at, ok := k.at[id]
	delete(k.at, id)
	return ok && now.Sub(at) < markTTL
}
