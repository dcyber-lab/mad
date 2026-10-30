package bus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
)

// A poller that shows the bus every second or two need not read it every
// time: the bus is files, so their sizes and times say whether anything
// moved, and only then are the messages read again. Source is that cache,
// for the window's poller and the deck's sidebar alike.

// FullEvery is how often a Source reads the bus again whatever its files
// say, for what moves with the clock alone: a task stalls, an ask ages.
const FullEvery = 15 * time.Second

// Signature is the sizes and times of the bus's files and of the
// person's read cursor: what any change to channels, messages, tasks,
// handoffs, timers or the spend moves.
func Signature() string {
	var b strings.Builder
	add := func(path string) {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			fmt.Fprintf(&b, "%s:%d:%d;", filepath.Base(path), fi.Size(), fi.ModTime().UnixNano())
		}
	}
	if es, err := os.ReadDir(dir()); err == nil {
		for _, e := range es {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				add(filepath.Join(dir(), e.Name()))
			}
		}
	}
	add(cursorFile(You))
	return b.String()
}

// Look is the bus as a poller shows it.
type Look struct {
	// Cards are Board's, every column; nil when the agents' statuses were
	// not known yet, or the board could not be read.
	Cards []Card
	// Channels and Yours are Channels and YourChannels.
	Channels []Channel
	Yours    map[string]Yours
	Err      error
}

// Source keeps the bus's Look between looks. It is safe for concurrent
// use, but meant for one poller.
type Source struct {
	mu   sync.Mutex
	sig  string
	at   time.Time
	look Look
}

// Look is the bus as of now: read again only when Signature or the
// agents' statuses moved (Board's columns depend on them), or FullEvery
// passed; else the last look. statuses are the agents' tracked statuses
// by id, nil before the first poll (the cards are left out). read says
// whether this call read the bus.
func (s *Source) Look(st *state.State, statuses map[string]string, now time.Time) (l Look, read bool) {
	sig := Signature() + "|" + statusSig(statuses)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sig == s.sig && now.Sub(s.at) < FullEvery {
		return s.look, false
	}
	if statuses != nil {
		l.Cards, l.Err = Board(st, statuses, now)
	}
	l.Channels, _ = Channels()
	l.Yours, _ = YourChannels()
	s.sig, s.at, s.look = sig, now, l
	return l, true
}

// statusSig is statuses as a string, the same for the same map.
func statusSig(statuses map[string]string) string {
	if statuses == nil {
		return "-"
	}
	ids := make([]string, 0, len(statuses))
	for id := range statuses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id + "=" + statuses[id] + ";")
	}
	return b.String()
}
