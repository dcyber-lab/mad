package bus

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// FollowUp is how long an agent may hold a task doing without a word
// before the board calls it stalled.
var FollowUp = 20 * time.Minute

// doneShown is how long a finished task stays on the board, in its column.
const doneShown = 24 * time.Hour

// askShown is how old an unanswered question to the person may be and
// still show.
const askShown = 7 * 24 * time.Hour

// Columns of the board, in the order they are shown.
const (
	ColWaitingYou = "waiting-you"
	ColDoing      = "doing"
	ColBlocked    = "blocked"
	ColReview     = "review"
	ColTodo       = "todo"
	ColDone       = "done"
)

// Columns lists the columns in the order they are shown.
var Columns = []string{ColWaitingYou, ColDoing, ColBlocked, ColReview, ColTodo, ColDone}

// Kinds of card, and why a card waits.
const (
	CardTask   = "task"
	CardAsk    = "ask"
	CardPaused = "paused"
	// CardCheckin is a check-in that could not go out.
	CardCheckin = "checkin"

	WhyPermission = "permission"
	WhyPaused     = "paused"
	WhyAsked      = "asked"
	WhyUnowned    = "unowned"
	WhyLapsed     = "lapsed"
	WhyWaits      = "waits"
	WhyStalled    = "stalled"
	WhyCheckin    = "checkin"
)

// CardMessage is the message an ask card is about.
type CardMessage struct {
	Seq  int    `json:"seq"`
	Text string `json:"text"`
}

// Card is one thing on the board, as it stands now: a task with what is
// derived about it, a question to the person, or a paused channel.
type Card struct {
	Kind string `json:"kind"` // task, ask or paused
	// Task is the task of a task card.
	Task   *Task  `json:"task,omitempty"`
	Title  string `json:"title"`
	Column string `json:"column"`
	// Why says what puts the card in its column when the stored status does
	// not: permission, paused, asked, unowned, lapsed, waits or stalled.
	Why        string        `json:"why,omitempty"`
	Owner      string        `json:"owner,omitempty"` // an agent id, or "you"
	OwnerLabel string        `json:"owner_label,omitempty"`
	Channel    string        `json:"channel,omitempty"`
	Age        time.Duration `json:"age"` // in its state
	Message    *CardMessage  `json:"message,omitempty"`
	Note       string        `json:"note,omitempty"`
	// Waits lists the tasks a card that waits on others still waits for.
	Waits []string `json:"waits,omitempty"`
	// Ref is the timer of a check-in card.
	Ref string `json:"ref,omitempty"`
}

// boardData is what Board reads from the bus, once.
type boardData struct {
	st    *state.State
	chans chanList
	msgs  map[string][]Message
}

func (d *boardData) messages(ch string) []Message {
	if m, ok := d.msgs[ch]; ok {
		return m
	}
	m, _ := readMsgs(ch)
	d.msgs[ch] = m
	return m
}

// youAfter tells whether the person posted in ch after seq.
func (d *boardData) youAfter(ch string, seq int) bool {
	for _, m := range d.messages(ch) {
		if m.Seq > seq && m.From.ID == You {
			return true
		}
	}
	return false
}

// isYourDM tells whether ch is a DM of the person.
func isYourDM(ch string) bool {
	if !isDM(ch) {
		return false
	}
	for _, p := range strings.Split(ch, ".")[1:] {
		if p == You {
			return true
		}
	}
	return false
}

// pendingAsk is the latest message by agent id, in ch, that asks the person
// (an @you, or any in their DM) and no message of theirs followed, since
// the time since.
func (d *boardData) pendingAsk(ch, id string, since time.Time) *Message {
	if ch == "" {
		return nil
	}
	var found *Message
	for _, m := range d.messages(ch) {
		if m.From.ID == id && m.Kind == "text" && !m.At.Before(since) && (isYourDM(ch) || youRe.MatchString(m.Text)) {
			m := m
			found = &m
		}
	}
	if found == nil || d.youAfter(ch, found.Seq) {
		return nil
	}
	return found
}

// lastBy is when agent id last posted in any channel.
func (d *boardData) lastBy(id string) time.Time {
	var at time.Time
	for _, c := range d.chans.Channels {
		for _, m := range d.messages(c.ID) {
			if m.From.ID == id && m.At.After(at) {
				at = m.At
			}
		}
	}
	return at
}

func (d *boardData) label(id string) string {
	switch id {
	case "":
		return ""
	case You:
		return You
	}
	if a := findIn(d.st, id); a != nil {
		return drive.Label(a)
	}
	return drive.ShortID(id)
}

// Board derives the cards of the board at time at. statuses says how each
// agent stands (drive.Statuses). A card is made for each task not finished
// (and for those finished in the last day), for each question to the
// person no one has answered (an @you in a channel, a message in their DM)
// that no task of the asker already carries, and for each channel that is
// paused. Apart from reading the bus's files it is pure.
//
// A task's column is derived, first match first: waiting-you when its owner
// waits on a permission (permission), its channel is paused (paused), its
// owner asked the person something since the task changed (asked), or it
// has no owner an agent can be (unowned); blocked when a task it waits on
// is not done (waits) or its handoff lapsed (lapsed); blocked as stalled
// when it is doing, its owner idle, asleep or stopped, and nothing from
// the owner for FollowUp; doing when it is written doing or its handoff is
// open and delivered; else its stored status.
func Board(st *state.State, statuses map[string]string, at time.Time) ([]Card, error) {
	tasks, err := loadTasks()
	if err != nil {
		return nil, err
	}
	hs, err := loadHandoffs()
	if err != nil {
		return nil, err
	}
	cl, err := loadChannels()
	if err != nil {
		return nil, err
	}
	d := &boardData{st: st, chans: cl, msgs: map[string][]Message{}}
	byID := map[string]Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	paused := map[string]bool{}
	for _, c := range cl.Channels {
		if c.Paused && !c.Archived {
			paused[c.ID] = true
		}
	}

	var cards []Card
	carried := map[string]bool{} // "channel#seq" of asks a task shows
	for i := range tasks {
		t := tasks[i]
		if !t.open() && (t.Status == TaskDropped || at.Sub(t.UpdatedAt) > doneShown) {
			continue
		}
		c := Card{Kind: CardTask, Task: &t, Title: t.Title, Owner: t.Owner, OwnerLabel: d.label(t.Owner),
			Channel: t.Channel, Age: max(0, at.Sub(t.UpdatedAt)), Note: t.Note}
		if !t.open() {
			c.Column = ColDone
			cards = append(cards, c)
			continue
		}
		owner := findIn(st, t.Owner)
		var ask *Message
		if owner != nil {
			for _, ch := range []string{t.Channel, DMChannel(t.Owner, You)} {
				if m := d.pendingAsk(ch, t.Owner, t.UpdatedAt); m != nil {
					ask = m
					carried[ch+"#"+strconv.Itoa(m.Seq)] = true
					if c.Message == nil {
						c.Message = &CardMessage{m.Seq, m.Text}
						if ch != t.Channel {
							c.Channel = ch
						}
					}
				}
			}
		}
		var h *Handoff
		if x, ok := findHandoff(hs, t.Handoff); ok {
			h = &x
		}
		waits := awaited(t, byID)
		switch {
		case owner != nil && statuses[t.Owner] == status.Waiting:
			c.Column, c.Why = ColWaitingYou, WhyPermission
		case t.Channel != "" && paused[t.Channel]:
			c.Column, c.Why = ColWaitingYou, WhyPaused
		case ask != nil:
			c.Column, c.Why = ColWaitingYou, WhyAsked
		case t.Owner == You:
			c.Column = ColWaitingYou
		case owner == nil:
			c.Column, c.Why = ColWaitingYou, WhyUnowned
		case h != nil && h.Status == "lapsed":
			c.Column, c.Why = ColBlocked, WhyLapsed
		case len(waits) > 0:
			c.Column, c.Why, c.Waits = ColBlocked, WhyWaits, waits
		case t.Status == TaskDoing && stalled(d, t, h, statuses[t.Owner], at):
			c.Column, c.Why = ColBlocked, WhyStalled
		case t.Status == TaskDoing || h != nil && h.Status == "open" && !h.DeliveredAt.IsZero():
			c.Column = ColDoing
		case t.Status == TaskReview:
			c.Column = ColReview
		default:
			c.Column = ColTodo
		}
		if c.Why != WhyAsked {
			c.Message = nil
		}
		// An unowned task is the lead's to place: say who that is.
		if c.Why == WhyUnowned && t.Channel != "" {
			if ch := cl.find(t.Channel); ch != nil && ch.Lead != "" {
				lead := "lead: " + d.label(ch.Lead)
				if c.Note != "" {
					lead = c.Note + " · " + lead
				}
				c.Note = lead
			}
		}
		cards = append(cards, c)
	}

	// Questions no task carries: by channel, then author.
	for _, ch := range cl.Channels {
		if ch.Archived {
			continue
		}
		var authors []string
		for _, m := range d.messages(ch.ID) {
			if m.Kind == "text" && m.From.ID != You && m.From.ID != system && !has(authors, m.From.ID) {
				authors = append(authors, m.From.ID)
			}
		}
		for _, id := range authors {
			m := d.pendingAsk(ch.ID, id, at.Add(-askShown))
			if m == nil || carried[ch.ID+"#"+strconv.Itoa(m.Seq)] {
				continue
			}
			label := m.From.Label
			if a := findIn(st, id); a != nil {
				label = drive.Label(a)
			}
			title := firstLine(m.Text, titleRunes)
			cards = append(cards, Card{Kind: CardAsk, Title: title, Column: ColWaitingYou, Why: WhyAsked,
				Owner: id, OwnerLabel: label, Channel: ch.ID, Age: max(0, at.Sub(m.At)),
				Message: &CardMessage{m.Seq, afterTitle(m.Text, title)}})
		}
		if paused[ch.ID] {
			var last time.Time
			for _, m := range d.messages(ch.ID) {
				last = m.At
			}
			cards = append(cards, Card{Kind: CardPaused, Title: "#" + ch.ID + " is paused", Column: ColWaitingYou, Why: WhyPaused,
				Channel: ch.ID, Age: max(0, at.Sub(last))})
		}
	}

	// Check-ins that could not go out: the agent is stopped, or it was
	// held so long that it was dropped.
	timers, _ := loadTimers()
	for _, t := range timers {
		if t.Kind != TimerOnce || t.Held != "stopped" && t.Held != "expired" {
			continue
		}
		who := d.label(t.By)
		cards = append(cards, Card{Kind: CardCheckin, Title: who + "'s check-in: " + firstLine(t.Note, titleRunes), Column: ColWaitingYou,
			Why: WhyCheckin, Owner: t.By, OwnerLabel: who, Channel: t.Channel, Age: max(0, at.Sub(t.NextAt)), Note: t.Why, Ref: t.ID})
	}

	rank := map[string]int{}
	for i, c := range Columns {
		rank[c] = i
	}
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if rank[a.Column] != rank[b.Column] {
			return rank[a.Column] < rank[b.Column]
		}
		return a.Age > b.Age
	})
	return cards, nil
}

// awaited lists the tasks t waits on that are not done. A task that is
// gone (archived) is done.
func awaited(t Task, byID map[string]Task) []string {
	var ids []string
	for _, w := range t.WaitsOn {
		if o, ok := byID[w]; ok && o.Status != TaskDone && o.Status != TaskDropped {
			ids = append(ids, w)
		}
	}
	return ids
}

// afterTitle is what of an ask's text its card's title, made by firstLine,
// does not already say: the body must not repeat the title.
func afterTitle(text, title string) string {
	text = strings.TrimSpace(text)
	if stem, cut := strings.CutSuffix(title, "…"); cut {
		return strings.TrimSpace(strings.TrimPrefix(text, stem))
	}
	_, rest, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(rest)
}

// stalled tells whether t, written doing, has an owner that is not at work
// and has not been heard from for FollowUp since the task was last touched
// or its handoff delivered. A turn ending, or a post, counts as heard.
func stalled(d *boardData, t Task, h *Handoff, owner string, at time.Time) bool {
	if FollowUp <= 0 || !atRest(owner) {
		return false
	}
	return at.Sub(lastMoved(d, t, h)) >= FollowUp
}

// atRest tells whether an agent with status s is between turns: idle,
// asleep or stopped. Running is at work, and waiting is for the person.
func atRest(s string) bool {
	return s == status.Idle || s == status.Asleep || s == status.Stopped
}

// lastMoved is when task t, with its handoff h (nil if none), last moved:
// the latest of its last change, its handoff's delivery, the end of its
// owner's last turn and the owner's last post. Files only.
func lastMoved(d *boardData, t Task, h *Handoff) time.Time {
	last := t.UpdatedAt
	if h != nil && h.DeliveredAt.After(last) {
		last = h.DeliveredAt
	}
	if tr := readTurn(t.Owner); tr != nil && tr.At.After(last) {
		last = tr.At
	}
	if m := d.lastBy(t.Owner); m.After(last) {
		last = m
	}
	return last
}
