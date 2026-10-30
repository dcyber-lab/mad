// Package attention is what waits on the person, and how each agent
// stands, worked out once for every surface: the window's snapshot (its
// sidebar, the phone, home, the board's "Waiting for you"), the deck's
// sidebar and `mad board`. Every surface draws the same Items in the same
// order, so their counts cannot disagree.
//
// It is pure apart from carrying out an answer (Desk.Act): callers pass
// what their poll already read (Facts), and a Desk builds the View from
// it. Nothing here reads a file on a poll.
//
// An Item is drawn without knowing its kind: it says its own title,
// context, line, acts and where it opens. A new kind is added here and
// nowhere else.
package attention

import "time"

// Kind is what an item is. The page uses it only as a CSS class.
type Kind string

const (
	// An agent is blocked on the person.
	KindPermission Kind = "permission" // may it use a tool (or carry out a plan)
	KindQuestion   Kind = "question"   // it asked, maybe with options
	// Something else waits on the person.
	KindAsk     Kind = "ask"     // an @you or a message in their DM, unanswered
	KindSkill   Kind = "skill"   // a skill an agent saved, to approve
	KindBudget  Kind = "budget"  // the day's budget for mad's own work: near or spent
	KindCheckin Kind = "checkin" // a check-in that could not go out
	KindPaused  Kind = "paused"  // a channel is paused: its agents' messages wait
	KindTask    Kind = "task"    // a task is the person's, or has no owner to place it
	// An agent finished while no one looked. Not counted as needing you.
	KindFinished Kind = "finished"
)

// Key names an item for as long as it stands: the same thing has the
// same key on every poll and in every process, a new one (a second
// permission, the next turn finishing) a new key. Only keys.go makes one.
type Key string

// Item is one thing in the inbox.
type Item struct {
	Key  Key  `json:"key"`
	Kind Kind `json:"kind"`
	// Agent is the agent it is about, if any: a surface draws its avatar
	// and name from its own entry for it, so the live dot stays right.
	Agent string `json:"agent,omitempty"`
	// Icon leads an item no agent does: "skill", "budget", "clock",
	// "pause", "task".
	Icon string `json:"icon,omitempty"`
	// Title, Context and Text are the item in words, short to long:
	// "kim" "Bash" "go test ./..."; "gus" "in #plan" "send the export's
	// location"; "/csv-export" "skill from gus" "When parsing a CSV...".
	Title   string `json:"title"`
	Context string `json:"context,omitempty"`
	Text    string `json:"text,omitempty"`
	// Mono: Text is a command or a path, drawn as code.
	Mono bool `json:"mono,omitempty"`
	// Where is the directory a permission runs in.
	Where string `json:"where,omitempty"`
	// Body is what a card shows under Text: the ask past its first line,
	// a skill's description, a check-in's note, the agent's own line.
	Body string `json:"body,omitempty"`
	// Part is how much of the budget is spent (1 = all), for its meter.
	Part *float64 `json:"part,omitempty"`
	// Tasks are the board's tasks this item holds up.
	Tasks []TaskRef `json:"tasks,omitempty"`
	// Since is when it began to wait. Surfaces tick its age themselves.
	Since time.Time `json:"since"`
	// Blocks: an agent is stopped until the person acts. Blocking items
	// come first.
	Blocks bool `json:"blocks,omitempty"`
	// Acts are what the person can do to it, in the order shown.
	Acts []Act `json:"acts"`
	// Open is where it is settled at length: Next and a click go there.
	Open Open `json:"open"`

	// route carries the acts out; only Desk.Act reaches it.
	route route
	// undated: Since is the first look's time, kept by the Desk.
	undated bool
}

// TaskRef names a task on the board.
type TaskRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Open is where an item is settled: one of the three is set.
type Open struct {
	Agent   string `json:"agent,omitempty"`
	Channel string `json:"channel,omitempty"`
	Page    string `json:"page,omitempty"` // PageSkills or PageBoard
}

const (
	PageSkills = "skills"
	PageBoard  = "board"
)

// Verb is what an act does.
type Verb string

const (
	VerbOpen    Verb = "open"    // go to Open; the surface does it, nothing is sent
	VerbAllow   Verb = "allow"   // a permission once, or a plan
	VerbAlways  Verb = "always"  // a permission from now on
	VerbDeny    Verb = "deny"    // a permission or plan; Text says why, or what instead
	VerbAnswer  Verb = "answer"  // a question: Option, or Text of one's own
	VerbReply   Verb = "reply"   // an ask: Text, in its channel or DM
	VerbApprove Verb = "approve" // a skill
	VerbDiscard Verb = "discard" // a skill
	VerbRaise   Verb = "raise"   // the budget, by Option dollars
	VerbLift    Verb = "lift"    // the budget, for today
	VerbResume  Verb = "resume"  // a paused channel
	VerbSend    Verb = "send"    // a held check-in, now
	VerbDrop    Verb = "drop"    // a held check-in
	VerbSeen    Verb = "seen"    // a finished agent, as looked at
)

// Act is one thing the person can do to an item, labeled in the deck's
// words.
type Act struct {
	Verb  Verb   `json:"verb"`
	Label string `json:"label"`
	// Option is the value the act sends: an option's label for an answer,
	// the dollars of a raise.
	Option string `json:"option,omitempty"`
	// Input: the act takes words first (a reply, an answer of one's own,
	// why not).
	Input bool `json:"input,omitempty"`
	// Row: shown on a sidebar row, not only on cards. Primary: the one
	// Enter does.
	Row     bool `json:"row,omitempty"`
	Primary bool `json:"primary,omitempty"`
}

// Inbox is every item, in the order Next walks them: what needs the
// person (blocking first, then oldest first), then what finished unseen
// (newest first).
type Inbox struct {
	Items []Item `json:"items"`
	// Needs counts the items that need the person, Items[:Needs]: the one
	// "N need you" every surface shows.
	Needs int `json:"needs"`
}

// NeedsYou is Items[:Needs].
func (in Inbox) NeedsYou() []Item { return in.Items[:in.Needs] }

// Finished is Items[Needs:].
func (in Inbox) Finished() []Item { return in.Items[in.Needs:] }

// Find is the item with key k.
func (in Inbox) Find(k Key) (Item, bool) {
	for _, it := range in.Items {
		if it.Key == k {
			return it, true
		}
	}
	return Item{}, false
}

// After is the item Next goes to from k: the one after it, wrapping; the
// first when k is "" or no longer there. ok is false for an empty inbox.
func (in Inbox) After(k Key) (it Item, ok bool) {
	if len(in.Items) == 0 {
		return Item{}, false
	}
	for i, x := range in.Items {
		if x.Key == k {
			return in.Items[(i+1)%len(in.Items)], true
		}
	}
	return in.Items[0], true
}

// Answer is the person's act on an item, as a surface sends it.
type Answer struct {
	Verb   Verb   `json:"verb"`
	Option string `json:"option,omitempty"`
	Text   string `json:"text,omitempty"`
}
