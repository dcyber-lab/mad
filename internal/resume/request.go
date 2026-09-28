package resume

import (
	"fmt"
	"strings"

	"github.com/dcyber-lab/mad/internal/resume/summarize"
)

const maxReply = 4000 // characters of one reply sent

// Request is what a summarizer plugin gets for b: the turns since the
// baseline, newest first until maxChars is spent, and what mad collected.
func Request(b Brief, maxChars int) summarize.Request {
	r := summarize.Request{Goal: b.Goal}
	if b.Anchor != nil {
		r.Bookmark = b.Anchor.Text
	}
	for _, d := range b.Decisions {
		r.Decisions = append(r.Decisions, summarize.Decision{ID: d.ID, Choice: d.Choice, Scope: d.Scope})
	}
	for _, it := range b.Items {
		f := it.Text
		if it.Tag != "" {
			f += " [" + it.Tag + "]"
		}
		r.Facts = append(r.Facts, f)
	}
	used := 0
	start := len(b.Window)
	for i := len(b.Window) - 1; i >= 0; i-- {
		n := len(b.Window[i].You) + min(len(b.Window[i].Reply), maxReply)
		if used+n > maxChars && start < len(b.Window) {
			break
		}
		used += n
		start = i
	}
	r.Omitted = start
	for i, t := range b.Window[start:] {
		reply := t.Reply
		if r := []rune(reply); len(r) > maxReply {
			reply = string(r[:maxReply]) + "…"
		}
		r.Turns = append(r.Turns, summarize.Turn{Ref: fmt.Sprintf("T%d", i+1), At: t.At, You: strings.TrimSpace(t.You), Reply: reply, Cmds: t.Cmds})
	}
	return r
}
