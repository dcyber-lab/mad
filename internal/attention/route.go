package attention

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// A route is how an item's acts reach what they act on. The route, not
// the kind, decides the acts: a window-mode agent's permission offers
// Allow and Deny (hostRoute), a TUI-run agent's the options its screen
// shows (keysRoute), or Open when it shows none it can read (openRoute).
// One type both offers the acts and
// carries them out, so the two cannot drift apart.
type route interface {
	// acts are what the person can do to it, in the order shown.
	acts() []Act
	// do carries a out. a is one of acts() (Desk.Act checked); f are the
	// facts the item was built from.
	do(a Answer, f *Facts) error
}

// What the routes act through; tests replace them.
var (
	hostDo     = host.Do
	busReply   = bus.Reply
	approve    = skill.Approve
	discard    = skill.Discard
	spendRaise = bus.SpendRaise
	spendLift  = bus.SpendOpenToday
	fireTimer  = bus.FireTimer
	dropTimer  = bus.RemoveTimer
	resume     = bus.Resume
	markSeen   = tmux.MarkSeen
	capture    = tmux.Capture
	sendKey    = tmux.SendKey
)

// hostRoute answers a request of a window-mode agent through its host,
// with the command the agent's page sends over /api/host.
type hostRoute struct {
	agent string
	ask   status.Ask
}

func (r hostRoute) acts() []Act {
	switch r.ask.Kind {
	case status.AskPlan:
		return []Act{
			{Verb: VerbAllow, Label: "Approve plan", Row: true, Primary: true},
			{Verb: VerbDeny, Label: "Keep planning", Input: true, Row: true},
		}
	case status.AskQuestion:
		if !r.ask.OneTap {
			return nil // several questions, or several picks: its page has the whole form
		}
		var acts []Act
		for _, o := range r.ask.Options {
			acts = append(acts, Act{Verb: VerbAnswer, Label: o, Option: o})
		}
		return append(acts, Act{Verb: VerbAnswer, Label: "Reply", Input: true, Row: true, Primary: true})
	}
	acts := []Act{{Verb: VerbAllow, Label: "Allow", Row: true, Primary: true}}
	if r.ask.Always {
		acts = append(acts, Act{Verb: VerbAlways, Label: "Always allow"})
	}
	return append(acts, Act{Verb: VerbDeny, Label: "Deny", Row: true}, Act{Verb: VerbDeny, Label: "Tell it instead…", Input: true})
}

func (r hostRoute) do(a Answer, _ *Facts) error {
	ans := host.Answer{ID: r.ask.ID, Instance: r.ask.Host}
	switch a.Verb {
	case VerbAllow:
		ans.Allow = true
	case VerbAlways:
		ans.Allow, ans.Always = true, true
	case VerbDeny:
		ans.Message = a.Text
	case VerbAnswer:
		text := a.Option
		if text == "" {
			text = a.Text
		}
		ans.Allow, ans.Answers = true, map[string]string{r.ask.Question: text}
	}
	err := hostDo(r.agent, host.Command{Type: "answer", Answer: &ans})
	if err != nil && strings.Contains(err.Error(), "answered already") { // host.staleAnswer
		return ErrGone
	}
	return err
}

// openRoute offers Open only: the waits of TUI-run agents whose screen
// shows no prompt keys can answer, and tasks.
type openRoute struct{ label string }

func (r openRoute) acts() []Act {
	return []Act{{Verb: VerbOpen, Label: r.label, Row: true, Primary: true}}
}
func (r openRoute) do(Answer, *Facts) error { return ErrNotOffered }

// keysRoute answers a TUI-run agent's prompt as the person would in its
// pane: the key of the option they chose, or Esc. It offers the options
// its screen showed (p), and sends only while the pane still shows that
// prompt.
type keysRoute struct {
	pane string
	p    prompt
}

// rowLabel is the longest option label a sidebar row shows.
const rowLabel = 12

func (r keysRoute) acts() []Act {
	var acts []Act
	rows := 0
	for _, o := range r.p.Options {
		a := Act{Verb: VerbAnswer, Label: o.Label, Option: o.Label, Primary: o.N == r.p.Cursor}
		if a.Primary || rows < 2 && utf8.RuneCountInString(o.Label) <= rowLabel {
			a.Row = true
			rows++
		}
		acts = append(acts, a)
	}
	return append(acts, Act{Verb: VerbDeny, Label: "Cancel (Esc)"})
}

// keySettle is how long a TUI is given to take a digit before its screen
// is read again.
var keySettle = 500 * time.Millisecond

func (r keysRoute) do(a Answer, _ *Facts) error {
	now, ok := parsePrompt(capture(r.pane))
	if !ok || now.ID != r.p.ID {
		return ErrGone // answered in the pane, or it asks something else now
	}
	if a.Verb == VerbDeny {
		return press(r.pane, "Escape")
	}
	o, ok := now.option(a.Option)
	if !ok {
		return ErrNotOffered
	}
	if err := press(r.pane, strconv.Itoa(o.N)); err != nil {
		return err
	}
	// A digit picks the option in claude's and codex's prompts; where it
	// only moves the cursor (codex's trust screen), Enter picks it.
	time.Sleep(keySettle)
	if after, ok := parsePrompt(capture(r.pane)); ok && after.ID == now.ID && after.Cursor == o.N {
		return press(r.pane, "Enter")
	}
	return nil
}

// press sends key to pane, if it is one mad sends: a digit that picks an
// option, Enter, Escape. Nothing typed, nothing to another target.
func press(pane, key string) error {
	if !strings.HasPrefix(pane, "%") {
		return fmt.Errorf("%q is not a pane", pane)
	}
	switch {
	case key == "Enter", key == "Escape":
	case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
	default:
		return fmt.Errorf("%q is not a key mad sends", key)
	}
	return sendKey(pane, key)
}

// busRoute answers an ask, in its channel or DM.
type busRoute struct {
	channel string
	seq     int
}

// theirs: a DM between two agents, which the person may read but not
// post in (bus.Reply refuses).
func (r busRoute) theirs() bool {
	if !strings.HasPrefix(r.channel, "dm.") {
		return false
	}
	for _, s := range bus.DMSides(r.channel) {
		if s == bus.You {
			return false
		}
	}
	return true
}

func (r busRoute) acts() []Act {
	open := "Open #" + r.channel
	if strings.HasPrefix(r.channel, "dm.") {
		open = "Open DM"
	}
	if r.theirs() {
		return []Act{{Verb: VerbOpen, Label: open, Row: true, Primary: true}}
	}
	return []Act{{Verb: VerbReply, Label: "Reply", Input: true, Row: true, Primary: true}, {Verb: VerbOpen, Label: open}}
}

func (r busRoute) do(a Answer, _ *Facts) error {
	if r.theirs() {
		return ErrNotOffered
	}
	_, err := busReply(r.channel, a.Text, r.seq)
	return err
}

// skillRoute approves or discards a pending skill.
type skillRoute struct{ slug string }

func (r skillRoute) acts() []Act {
	return []Act{{Verb: VerbApprove, Label: "Approve", Row: true, Primary: true}, {Verb: VerbDiscard, Label: "Discard"}, {Verb: VerbOpen, Label: "Read it"}}
}

func (r skillRoute) do(a Answer, _ *Facts) error {
	if a.Verb == VerbDiscard {
		return discard(r.slug)
	}
	return approve(r.slug, nil)
}

// budgetRaise is the dollars Raise adds.
const budgetRaise = 10

// budgetRoute raises the day's budget or lifts it.
type budgetRoute struct{}

func (budgetRoute) acts() []Act {
	n := strconv.Itoa(budgetRaise)
	return []Act{
		{Verb: VerbRaise, Label: "Raise by $" + n, Option: n, Row: true, Primary: true},
		{Verb: VerbLift, Label: "Lift for today"},
		{Verb: VerbOpen, Label: "Open board"},
	}
}

func (budgetRoute) do(a Answer, _ *Facts) error {
	if a.Verb == VerbLift {
		return spendLift(time.Now())
	}
	usd, err := strconv.ParseFloat(a.Option, 64)
	if err != nil {
		return err
	}
	return spendRaise(usd, time.Now())
}

// timerRoute sends a held check-in now, or drops it.
type timerRoute struct{ timer, label string }

func (r timerRoute) acts() []Act {
	return []Act{{Verb: VerbSend, Label: "Send now", Row: true, Primary: true}, {Verb: VerbDrop, Label: "Drop"}, {Verb: VerbOpen, Label: "Open " + r.label}}
}

func (r timerRoute) do(a Answer, f *Facts) error {
	if a.Verb == VerbDrop {
		return dropTimer(r.timer, bus.You)
	}
	return fireTimer(r.timer, time.Now(), f.Statuses())
}

// channelRoute resumes a paused channel.
type channelRoute struct{ channel string }

func (r channelRoute) acts() []Act {
	return []Act{{Verb: VerbResume, Label: "Resume", Row: true, Primary: true}, {Verb: VerbOpen, Label: "Open #" + r.channel}}
}

func (r channelRoute) do(Answer, *Facts) error { return resume(r.channel) }

// seenRoute marks a finished agent as looked at: the pane's seen mark,
// which every tracker reads (tmux.Seen), so it clears everywhere, as
// looking at it does.
type seenRoute struct{ label, pane string }

func (r seenRoute) acts() []Act {
	acts := []Act{{Verb: VerbOpen, Label: "Open " + r.label, Row: true, Primary: true}}
	if r.pane != "" {
		acts = append(acts, Act{Verb: VerbSeen, Label: "Mark seen"})
	}
	return acts
}

func (r seenRoute) do(Answer, *Facts) error {
	if r.pane == "" {
		return errors.New("it has no pane to mark")
	}
	return markSeen(r.pane, time.Now())
}
