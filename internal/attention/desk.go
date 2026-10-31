package attention

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// Errors of Desk.Act, for a surface to tell apart.
var (
	// ErrGone: no such item is open: answered already (here, on another
	// surface, in the agent's TUI) or no longer waiting.
	ErrGone = errors.New("that was answered already")
	// ErrChanged: no such item is open, and its agent asks something else
	// now (another prompt, another request): an act meant for the old one
	// is never sent to the new.
	ErrChanged = errors.New("it asks something else now")
	// ErrNotOffered: the item does not offer that act (or that option).
	ErrNotOffered = errors.New("that cannot be done to it")
)

// settle is how long an item acted on stays hidden while what it acted on
// catches up (a host flips to running in ms, a channel's post in a poll).
// After it, an item still there shows again: the act did not take.
const settle = 30 * time.Second

// Desk is the inbox as one process serves it: the last View built, and
// the items the person acted on, hidden until they settle. It is the
// only way to act on an item.
//
// The window's poller and its HTTP handlers share one; the deck's sidebar
// has one; `mad board` makes one for a single look.
type Desk struct {
	mu    sync.Mutex
	facts Facts
	view  View
	acted map[Key]time.Time // when acted on
	// gone are the acted-on items their route found answered already: they
	// stay hidden for as long as they are listed, not for a settle.
	gone  map[Key]bool
	first map[Key]time.Time // when first seen, for items with no time of their own
	now   func() time.Time
}

func NewDesk() *Desk {
	return &Desk{acted: map[Key]time.Time{}, gone: map[Key]bool{}, first: map[Key]time.Time{}, now: time.Now}
}

// Look builds the View from f, keeps it for Act, and returns it as the
// surfaces should draw it: without the items acted on in the last settle.
func (d *Desk) Look(f Facts) View {
	v := build(f)
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	present := map[Key]bool{}
	kept := make([]Item, 0, len(v.Inbox.Items))
	needs := 0
	for i, it := range v.Inbox.Items {
		present[it.Key] = true
		if it.undated {
			if at, ok := d.first[it.Key]; ok {
				it.Since = at
			} else {
				d.first[it.Key] = it.Since
			}
		}
		if at, ok := d.acted[it.Key]; ok && (now.Sub(at) < settle || d.gone[it.Key]) {
			continue
		}
		delete(d.acted, it.Key)
		kept = append(kept, it)
		if i < v.Inbox.Needs {
			needs++
		}
	}
	v.Inbox = Inbox{Items: kept, Needs: needs}
	// A first-seen time made the needs out of order; sort them again.
	v.Inbox.Items = append(order(v.Inbox.Items[:needs]), v.Inbox.Items[needs:]...)
	for k := range d.acted {
		if !present[k] {
			delete(d.acted, k) // settled
			delete(d.gone, k)
		}
	}
	for k := range d.first {
		if !present[k] {
			delete(d.first, k)
		}
	}
	d.facts, d.view = f, v
	return v
}

// View is the last View Look built, as it returned it.
func (d *Desk) View() View {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.view
}

// Act carries out a on the item with key k, once: a second act on the
// same key (a double tap, the phone and the window both) is ErrGone; an
// act on an item gone while its agent waits on something else is
// ErrChanged. a must be one of the item's acts, with one of its options:
// the surface is not trusted to offer only what the item does. VerbOpen
// is not sent.
//
// The lock is not held while the route works (a host can take seconds).
// If the route fails, the item shows again at the next look.
func (d *Desk) Act(k Key, a Answer) error {
	d.mu.Lock()
	it, ok := d.view.Inbox.Find(k)
	if _, acted := d.acted[k]; !ok || acted {
		err := ErrGone
		if !ok && d.asks(keyAgent(k)) {
			err = ErrChanged
		}
		d.mu.Unlock()
		return err
	}
	if !offered(it, a) {
		d.mu.Unlock()
		return ErrNotOffered
	}
	d.acted[k] = d.now()
	f := d.facts
	d.mu.Unlock()

	err := it.route.do(a, &f)
	if err != nil {
		d.mu.Lock()
		if errors.Is(err, ErrGone) || errors.Is(err, ErrChanged) {
			d.gone[k] = true // answered elsewhere: it must not come back
		} else {
			delete(d.acted, k)
		}
		d.mu.Unlock()
	}
	return err
}

// asks tells whether agent waits on the person now: a prompt or a
// request of its is an item, or a prompt is on its screen that its hook
// does not tell of yet (claude's permission hook comes seconds after it).
// d.mu is held.
func (d *Desk) asks(agent string) bool {
	if agent == "" {
		return false
	}
	for _, it := range d.view.Inbox.NeedsYou() {
		if keyAgent(it.Key) == agent {
			return true
		}
	}
	for _, a := range d.facts.Agents {
		if a.ID == agent && a.Screen != "" {
			_, ok := parsePrompt(a.Screen)
			return ok || status.ShowsPrompt(a.Screen)
		}
	}
	return false
}

// offered tells whether a is one of it's acts: the same verb, and the
// same option when the act carries one; an Input act takes any words.
func offered(it Item, a Answer) bool {
	if a.Verb == VerbOpen {
		return false
	}
	for _, act := range it.Acts {
		switch {
		case act.Verb != a.Verb:
		case act.Input:
			if a.Option == "" && strings.TrimSpace(a.Text) != "" {
				return true
			}
		case act.Option != "":
			if a.Option == act.Option {
				return true
			}
		case a.Option == "" && a.Text == "":
			return true
		}
	}
	return false
}
