package bus

import (
	"slices"
	"strings"
	"time"
)

// The person reads every channel: there is no membership to count by.

// Yours is how a channel stands for the person: its last message, and
// how many of the agents' they have not read (MarkRead(You, ...)).
type Yours struct {
	Last   int `json:"last"`
	Unread int `json:"unread"`
	// Mention: an unread message in it asks the person: an @you, or any
	// in their DM.
	Mention bool `json:"mention,omitempty"`
	// Listed: it is in the short list the window's sidebar and the deck's
	// bus block show: something unread, or paused; never a DM between
	// two agents, which is theirs.
	Listed bool `json:"listed,omitempty"`
	// LastLine is the last message as one line of a list: "who: its first
	// line", at most lastRunes long, "who: [图片]" for pictures alone; mad's
	// own system lines and the hidden kinds are passed over. Empty for a
	// channel with nothing visible said.
	LastLine string `json:"last_line,omitempty"`
	// LastAt is when that message was said.
	LastAt time.Time `json:"last_at,omitzero"`
}

// HiddenKinds are the kinds of message that are agents' bookkeeping among themselves: a handoff, its result
// (done, declined), a check-in (timer), a nudge. No view of a channel shows them and the person's counts
// (last line, unread, the red dot) skip them; the board and each agent's own conversation still have them.
// The page's QUIET_KINDS (web/bus.js) is the same list, and native views take this one.
var HiddenKinds = []string{"handoff", "done", "declined", "timer", "nudge"}

// Quiet says whether m is bookkeeping the person is not shown: one of HiddenKinds, or a lapsed notice (a
// system line about a handoff). Whatever @mentions the person is never quiet, of any kind, and mad's own
// pause and resume notes are not a handoff's, so they are not either.
func Quiet(m Message) bool {
	if mentionsYou(m.Text) {
		return false
	}
	if m.Kind == "system" {
		return m.Handoff != nil
	}
	return slices.Contains(HiddenKinds, m.Kind)
}

// lastRunes is how much of a message's first line the list keeps.
const lastRunes = 120

// lastLine is m as a line of a list.
func lastLine(m Message) string {
	text, _, _ := strings.Cut(strings.TrimSpace(m.Text), "\n")
	text = strings.TrimSpace(text)
	if text == "" && len(m.Images) > 0 {
		text = "[图片]"
	}
	if r := []rune(text); len(r) > lastRunes {
		text = string(r[:lastRunes-1]) + "…"
	}
	return m.From.Label + ": " + text
}

// YourChannels says how each channel stands for the person, by id. It
// takes no lock: channels.json and the cursors are replaced whole by a
// rename, and a channel's file is appended to or replaced so (readMsgs), so
// a look never sees one half written, and agents posting don't wait on it.
func YourChannels() (map[string]Yours, error) {
	out := map[string]Yours{}
	cl, err := loadChannels()
	if err != nil {
		return out, err
	}
	cs, err := loadCursors(You)
	if err != nil {
		return out, err
	}
	for _, c := range cl.Channels {
		if c.Archived {
			continue
		}
		msgs, err := readMsgs(c.ID)
		if err != nil {
			return out, err
		}
		var y Yours
		last := -1 // the last visible message
		for i, m := range msgs {
			y.Last = max(y.Last, m.Seq)
			if m.Kind == "system" || Quiet(m) {
				// the hidden kinds count for nothing here: a channel whose last messages are all bookkeeping keeps
				// its last visible line (LastLine stays empty if it has none)
				continue
			}
			if strings.TrimSpace(m.Text) != "" || len(m.Images) > 0 {
				last = i
			}
			if m.Seq > cs[c.ID].Read && m.From.ID != You {
				y.Unread++
				y.Mention = y.Mention || isYourDM(c.ID) || mentionsYou(m.Text)
			}
		}
		if last >= 0 {
			y.LastLine, y.LastAt = lastLine(msgs[last]), msgs[last].At
		}
		y.Listed = (y.Unread > 0 || c.Paused) && (!isDM(c.ID) || isYourDM(c.ID))
		out[c.ID] = y
	}
	return out, nil
}
