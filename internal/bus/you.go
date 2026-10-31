package bus

import "strings"

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
	// own system lines are passed over. Empty for a channel with nothing
	// said.
	LastLine string `json:"last_line,omitempty"`
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

// YourChannels says how each channel stands for the person, by id.
func YourChannels() (map[string]Yours, error) {
	out := map[string]Yours{}
	err := locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		cs, err := loadCursors(You)
		if err != nil {
			return err
		}
		for _, c := range cl.Channels {
			if c.Archived {
				continue
			}
			msgs, err := readMsgs(c.ID)
			if err != nil {
				return err
			}
			var y Yours
			for _, m := range msgs {
				y.Last = max(y.Last, m.Seq)
				if m.Kind != "system" && (strings.TrimSpace(m.Text) != "" || len(m.Images) > 0) {
					y.LastLine = lastLine(m)
				}
				if m.Seq > cs[c.ID].Read && m.Kind != "system" && m.From.ID != You {
					y.Unread++
					y.Mention = y.Mention || isYourDM(c.ID) || youRe.MatchString(m.Text)
				}
			}
			y.Listed = (y.Unread > 0 || c.Paused) && (!isDM(c.ID) || isYourDM(c.ID))
			out[c.ID] = y
		}
		return nil
	})
	return out, err
}
