// Package attention is the inbox: what needs you, kept apart from what
// each agent is doing. A status is a fact about an agent right now; an
// item stays open until you deal with it, whether or not a notification
// went out, and survives a sidebar restart.
//
// The sidebar is the only writer. Items are raised from the status it
// observes (hooks, screens) and from mad's own work (workspace setup).
package attention

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// Kind is why an item needs you.
type Kind string

const (
	NeedsInput  Kind = "needs_input"  // a permission prompt or a question
	TurnEnded   Kind = "turn_ended"   // a turn ended; its result is unread (not "done")
	SetupFailed Kind = "setup_failed" // a workspace template step failed
	Exited      Kind = "exited"       // the agent's process ended on its own
)

// rank orders kinds in the inbox: blocked work first.
var rank = map[Kind]int{NeedsInput: 0, SetupFailed: 1, Exited: 2, TurnEnded: 3}

// State is whether an item still needs handling.
type State string

const (
	Open       State = "open"
	Resolved   State = "resolved"   // dealt with: answered, marked by you
	Dismissed  State = "dismissed"  // no longer relevant: the agent was removed
	Superseded State = "superseded" // a later turn took over from this result
)

// Source is what an item rests on, so a guess never passes for a fact.
const (
	FromHook   = "hook"   // the agent's own lifecycle event
	FromScreen = "screen" // inferred from the screen going quiet or showing a prompt
	FromMad    = "mad"    // mad's own work, e.g. a setup step's exit code
)

type Item struct {
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	State   State  `json:"state"`
	AgentID string `json:"agent_id,omitempty"`
	RunID   string `json:"run_id,omitempty"` // workspace run, for setup failures
	Project string `json:"project"`          // project path
	// Where is how the item is labeled when it has no agent to look up:
	// "branch" of the workspace being set up.
	Where   string `json:"where,omitempty"`
	Message string `json:"message,omitempty"`
	Source  string `json:"source"`
	// SourceRef identifies the event behind the item (a hook report's
	// time), so the same event read again after a restart is not a new item.
	SourceRef    string    `json:"source_ref,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	SeenAt       time.Time `json:"seen_at,omitzero"`
	SnoozedUntil time.Time `json:"snoozed_until,omitzero"`
	ClosedAt     time.Time `json:"closed_at,omitzero"`
	Resolution   string    `json:"resolution,omitempty"` // "answered", "marked by you", "continued"…
}

// Seen reports whether the item was looked at since it last changed.
func (it *Item) Seen() bool { return !it.SeenAt.IsZero() && !it.SeenAt.Before(it.UpdatedAt) }

// Inbox is every item, open and closed; closed ones are kept a while so
// events read again are recognized.
type Inbox struct {
	Items []*Item `json:"items"`
}

// keepClosed bounds the history of closed items.
const keepClosed = 200

func File() string { return filepath.Join(paths.StateDir(), "inbox.json") }

// Load reads the inbox; a missing file is an empty inbox.
func Load() (*Inbox, error) {
	var b Inbox
	if err := paths.ReadJSON(File(), &b); err != nil {
		return &Inbox{}, err
	}
	return &b, nil
}

func (b *Inbox) Save() error {
	b.prune()
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(File(), data)
}

func (b *Inbox) prune() {
	closed := 0
	for i := len(b.Items) - 1; i >= 0; i-- {
		if b.Items[i].State != Open {
			if closed++; closed > keepClosed {
				b.Items = append(b.Items[:i], b.Items[i+1:]...)
			}
		}
	}
}

// Find returns the item with id, or nil.
func (b *Inbox) Find(id string) *Item {
	for _, it := range b.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// openFor is agent's open item of kind, or nil: one open item per agent
// and kind, the current episode.
func (b *Inbox) openFor(agentID string, kind Kind) *Item {
	for _, it := range b.Items {
		if it.State == Open && it.AgentID == agentID && it.Kind == kind {
			return it
		}
	}
	return nil
}

// Known reports whether an item was already raised for the event ref of
// agent, open or not.
func (b *Inbox) Known(agentID string, kind Kind, ref string) bool {
	if ref == "" {
		return false
	}
	for _, it := range b.Items {
		if it.AgentID == agentID && it.Kind == kind && it.SourceRef == ref {
			return true
		}
	}
	return false
}

// Raise opens an item, or updates the open one of the same agent and
// kind: the same wait reported twice is one item. It reports whether the
// inbox changed.
func (b *Inbox) Raise(n Item, now time.Time) (*Item, bool) {
	if n.AgentID != "" {
		if it := b.openFor(n.AgentID, n.Kind); it != nil {
			if it.Message == n.Message && it.SourceRef == n.SourceRef && it.Source == n.Source {
				return it, false
			}
			if n.Message != "" {
				it.Message = n.Message
			}
			it.Source, it.SourceRef, it.UpdatedAt = n.Source, n.SourceRef, now
			return it, true
		}
	}
	it := n
	it.ID, it.State, it.CreatedAt, it.UpdatedAt = state.NewUUID(), Open, now, now
	b.Items = append(b.Items, &it)
	return &it, true
}

// Settle closes agent's open items of the given kinds and reports
// whether any were.
func (b *Inbox) Settle(agentID string, kinds []Kind, to State, why string, now time.Time) bool {
	changed := false
	for _, it := range b.Items {
		if it.State != Open || it.AgentID != agentID {
			continue
		}
		for _, k := range kinds {
			if it.Kind == k {
				it.State, it.Resolution, it.ClosedAt = to, why, now
				changed = true
			}
		}
	}
	return changed
}

// SettleAll closes every open item of agent (it was removed).
func (b *Inbox) SettleAll(agentID string, now time.Time) bool {
	return b.Settle(agentID, []Kind{NeedsInput, TurnEnded, Exited, SetupFailed}, Dismissed, "agent removed", now)
}

// MarkSeen notes that agent's open items were looked at; they stay open.
func (b *Inbox) MarkSeen(agentID string, now time.Time) bool {
	changed := false
	for _, it := range b.Items {
		if it.State == Open && it.AgentID == agentID && !it.Seen() {
			it.SeenAt, changed = now, true
		}
	}
	return changed
}

func (b *Inbox) Resolve(id, why string, now time.Time) {
	if it := b.Find(id); it != nil && it.State == Open {
		it.State, it.Resolution, it.ClosedAt = Resolved, why, now
	}
}

func (b *Inbox) Snooze(id string, until time.Time) {
	if it := b.Find(id); it != nil {
		it.SnoozedUntil = until
	}
}

// Pending is what the inbox shows: open items not snoozed, blocked work
// first, then the longest waiting.
func (b *Inbox) Pending(now time.Time) []*Item {
	var out []*Item
	for _, it := range b.Items {
		if it.State == Open && !now.Before(it.SnoozedUntil) {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank[out[i].Kind], rank[out[j].Kind]; ri != rj {
			return ri < rj
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Count is how many items the inbox shows, per project path when by is set.
func (b *Inbox) Count(now time.Time) (total int, by map[string]int) {
	by = map[string]int{}
	for _, it := range b.Pending(now) {
		total++
		by[it.Project]++
	}
	return total, by
}
