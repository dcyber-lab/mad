package attention

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// A route is how an item's acts reach what they act on. The route, not
// the kind, decides the acts: a window-mode agent's permission offers
// Allow and Deny (hostRoute), a TUI-run agent's offers Open (openRoute)
// until it can be answered by keys. One type both offers the acts and
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
	ans := host.Answer{ID: r.ask.ID}
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
	if err != nil && strings.Contains(err.Error(), "answered already") {
		return ErrGone
	}
	return err
}

// openRoute offers Open only: the waits of TUI-run agents, and tasks.
// Answering a TUI by keys to its pane slots in as a route of its own,
// chosen where builder.agent picks this one.
type openRoute struct{ label string }

func (r openRoute) acts() []Act {
	return []Act{{Verb: VerbOpen, Label: r.label, Row: true, Primary: true}}
}
func (r openRoute) do(Answer, *Facts) error { return ErrNotOffered }

// busRoute answers an ask, in its channel or DM.
type busRoute struct {
	channel string
	seq     int
}

func (r busRoute) acts() []Act {
	open := "Open #" + r.channel
	if strings.HasPrefix(r.channel, "dm.") {
		open = "Open DM"
	}
	return []Act{{Verb: VerbReply, Label: "Reply", Input: true, Row: true, Primary: true}, {Verb: VerbOpen, Label: open}}
}

func (r busRoute) do(a Answer, _ *Facts) error {
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
