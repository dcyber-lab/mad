package attention

import (
	"fmt"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// Activity is an agent's activity line: what it does now, or its state
// and for how long. Which part applies in which state is decided here,
// by leaving the rest empty; a surface draws whatever is set, in field
// order, and needs no rule of its own:
//
//	working   Bash · go test ./... · 6m · 2/5
//	needs you Bash · go test ./... · 4m
//	finished  Finished 3m ago · Tests pass, PR #42
//	quiet     Idle 12m
//
// The state's word comes from the Look table, not from here.
type Activity struct {
	// Since is when its tracked status began (Tracker.Since).
	Since time.Time `json:"since,omitzero"`
	// Tool and Target: its current call, only while running or waiting.
	// The call stays in the transcript after the turn; here it ends with
	// the turn.
	Tool   string `json:"tool,omitempty"`
	Target string `json:"target,omitempty"`
	// Todo: its todo list, only while running or waiting and some is left.
	Todo *discover.Todo `json:"todo,omitempty"`
	// Reply: the first line of its last reply, only while shown Finished.
	Reply string `json:"reply,omitempty"`
}

// replyRunes is how much of a reply's first line the activity line keeps.
const replyRunes = 120

// activityOf is a's line.
func activityOf(a Agent) Activity {
	act := Activity{Since: a.Since}
	switch a.Status {
	case status.Running, status.Waiting:
		act.Tool, act.Target = splitCall(a.Info.Tool)
		if t := a.Info.Todo; t != nil && t.Done < t.Total {
			act.Todo = t
		}
	}
	if a.Shown == status.Finished {
		act.Reply = firstLine(replyOf(a), replyRunes)
	}
	return act
}

func activities(agents []Agent) map[string]Activity {
	out := make(map[string]Activity, len(agents))
	for _, a := range agents {
		out[a.ID] = activityOf(a)
	}
	return out
}

// splitCall parts mad's own label of a tool call ("Bash · go test ./...",
// discover's toolLabel) into the tool and what it acts on.
// TODO: have the providers give the two apart (discover.Event), and drop
// this second reading of their label.
func splitCall(label string) (tool, target string) {
	tool, target, _ = strings.Cut(label, " · ")
	return tool, target
}

// Parts is the line as text parts, for the deck, which styles them by
// position: the tool, its target, then the rest. word is the shown
// state's Look word; now is the clock ages are taken at.
func (a Activity) Parts(word string, now time.Time) []string {
	age := textutil.AgeAt(a.Since, now)
	if a.Since.IsZero() {
		age = ""
	}
	if a.Tool != "" {
		parts := []string{a.Tool}
		if a.Target != "" {
			parts = append(parts, a.Target)
		}
		if age != "" {
			parts = append(parts, age)
		}
		if a.Todo != nil {
			parts = append(parts, fmt.Sprintf("%d/%d", a.Todo.Done, a.Todo.Total))
		}
		return parts
	}
	head := word
	if age != "" {
		head += " " + age
		if word == status.LookOf(status.Finished).Word {
			head += " ago"
		}
	}
	parts := []string{head}
	if a.Todo != nil {
		parts = append(parts, fmt.Sprintf("%d/%d", a.Todo.Done, a.Todo.Total))
	}
	if a.Reply != "" {
		parts = append(parts, a.Reply)
	}
	return parts
}

// firstLine is s's first line, cut to n runes.
func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// replyOf is a's last reply, if it is of the turn that ended into this
// finished state: the turn file keeps the last turn that reported, and
// an agent finished by its screen alone may not have reported this one.
func replyOf(a Agent) string {
	t := a.Report.Turn
	if t == nil || t.At.Before(a.Since) {
		return ""
	}
	return t.Reply
}
