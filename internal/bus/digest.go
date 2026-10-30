package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
)

// notifyText is what the queue shows for a bus item.
const notifyText = "[mad bus] messages waiting"

// cursor is how far an agent has been told (delivered) and has read.
type cursor struct {
	Delivered int `json:"delivered"`
	Read      int `json:"read"`
}

func loadCursors(agentID string) (map[string]cursor, error) {
	out := map[string]cursor{}
	data, err := os.ReadFile(cursorFile(agentID))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(data, &out)
}

// setCursor changes an agent's cursor for a channel under the lock held by
// the caller; a nil fn drops it.
func setCursor(agentID, channel string, fn func(*cursor)) error {
	cs, err := loadCursors(agentID)
	if err != nil {
		return err
	}
	if fn == nil {
		if _, ok := cs[channel]; !ok {
			return nil
		}
		delete(cs, channel)
	} else {
		cu := cs[channel]
		fn(&cu)
		cs[channel] = cu
	}
	data, err := json.Marshal(cs)
	if err != nil {
		return err
	}
	return writeAtomic(cursorFile(agentID), data)
}

// Notify puts one bus item in the agent's queue, unless one is waiting.
func Notify(agentID string) error {
	_, err := queue.PushBus(agentID, notifyText, now())
	return err
}

// entry is one message of a digest, in its channel.
type entry struct {
	ch Channel
	m  Message
}

// also counts the new messages of a channel that were not for the agent.
type also struct {
	ch string
	n  int
}

// wakesAgent tells whether message m of channel c is one that wakes agent
// id: it mentions it or all, or it is the other side of a DM.
func wakesAgent(c Channel, m Message, id string) bool {
	if m.Kind == "system" || m.From.ID == id {
		return false
	}
	return isDM(c.ID) || has(m.Mentions, id) || has(m.Mentions, "*")
}

// gather finds what is new for an agent: the messages that wake it past its
// delivered cursors, a count of the rest, and how far each channel goes.
// The channels paused are left for when they are resumed. The bus lock is
// held by the caller.
func gather(id string) (list []entry, rest []also, upto map[string]int, err error) {
	cl, err := loadChannels()
	if err != nil {
		return nil, nil, nil, err
	}
	cs, err := loadCursors(id)
	if err != nil {
		return nil, nil, nil, err
	}
	upto = map[string]int{}
	for _, c := range cl.Channels {
		if c.Archived || c.Paused || !has(c.Members, id) {
			continue
		}
		msgs, err := readMsgs(c.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		cu := cs[c.ID]
		n := 0
		for _, m := range msgs {
			switch {
			case m.Seq <= cu.Delivered:
			case wakesAgent(c, m, id):
				list = append(list, entry{c, m})
			case m.Seq > cu.Read && m.Kind != "system" && m.From.ID != id:
				n++
			}
		}
		if n > 0 {
			rest = append(rest, also{c.ID, n})
		}
		if last := lastSeq(msgs); last > cu.Delivered {
			upto[c.ID] = last
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].m.At.Before(list[j].m.At) })
	sort.Slice(rest, func(i, j int) bool { return rest[i].ch < rest[j].ch })
	return list, rest, upto, nil
}

// wakes tells whether agent id has anything that would wake it.
func wakes(id string) bool {
	var list []entry
	_ = locked(func() error {
		var err error
		list, _, _, err = gather(id)
		return err
	})
	return len(list) > 0
}

// Compose writes the digest of what wakes agent id, past what it was told
// (see Delivered); upto says how far to move the cursors once it is sent.
// ok is false when there is nothing to say.
func Compose(agentID string, at time.Time) (text string, upto map[string]int, ok bool, err error) {
	st, err := state.Load()
	if err != nil {
		return "", nil, false, err
	}
	var list []entry
	var rest []also
	err = locked(func() error {
		var err error
		list, rest, upto, err = gather(agentID)
		return err
	})
	if err != nil || len(list) == 0 {
		return "", nil, false, err
	}

	label := func(id string) string {
		if a := findIn(st, id); a != nil {
			return drive.Label(a)
		}
		if id == You {
			return You
		}
		return drive.ShortID(id)
	}
	var b strings.Builder
	n := len(list)
	plural := "s"
	if n == 1 {
		plural = ""
	}
	fmt.Fprintf(&b, "[mad bus] %d message%s for you (%s). Act on what asks you something; reply with\n", n, plural, label(agentID))
	b.WriteString("bus_post(channel, text) in the channel or bus_dm(agent, text) privately, @mentioning\n")
	b.WriteString("who should read it. Say nothing on the bus if nothing is asked of you.\n")
	for _, e := range list {
		name := "#" + e.ch.ID
		if isDM(e.ch.ID) {
			name = "DM with " + e.m.From.Label
		}
		fmt.Fprintf(&b, "\n%s · %s · #%d · %s", name, e.m.From.Label, e.m.Seq, e.m.At.In(at.Location()).Format("15:04"))
		if e.m.ReplyTo > 0 {
			fmt.Fprintf(&b, " (reply to #%d)", e.m.ReplyTo)
		}
		b.WriteString("\n" + e.m.Text + "\n")
	}
	if len(rest) > 0 {
		var parts []string
		for _, r := range rest {
			name := "#" + r.ch
			if isDM(r.ch) {
				name = "DM"
				if c, found, _ := Find(r.ch); found {
					for _, m := range c.Members {
						if m != agentID {
							name = "DM with " + label(m)
						}
					}
				}
			}
			s := "s"
			if r.n == 1 {
				s = ""
			}
			parts = append(parts, fmt.Sprintf("%s %d message%s", name, r.n, s))
		}
		fmt.Fprintf(&b, "\nAlso new, not addressed to you: %s (bus_read to see them).\n", strings.Join(parts, ", "))
	}
	return b.String(), upto, true, nil
}

// Delivered moves agent id's cursors to upto, the digest having been sent.
func Delivered(agentID string, upto map[string]int) error {
	if len(upto) == 0 {
		return nil
	}
	return locked(func() error {
		for ch, seq := range upto {
			err := setCursor(agentID, ch, func(cu *cursor) { cu.Delivered = max(cu.Delivered, seq) })
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkRead records that agent id has read channel up to seq.
func MarkRead(agentID, channel string, seq int) error {
	return locked(func() error {
		return setCursor(agentID, channel, func(cu *cursor) { cu.Read = max(cu.Read, seq) })
	})
}

// Unread counts, for each channel agent id is in, the messages of others
// after what it has read.
func Unread(agentID string) (map[string]int, error) {
	out := map[string]int{}
	err := locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		cs, err := loadCursors(agentID)
		if err != nil {
			return err
		}
		for _, c := range cl.Channels {
			if c.Archived || !has(c.Members, agentID) {
				continue
			}
			msgs, err := readMsgs(c.ID)
			if err != nil {
				return err
			}
			n := 0
			for _, m := range msgs {
				if m.Seq > cs[c.ID].Read && m.Kind != "system" && m.From.ID != agentID {
					n++
				}
			}
			out[c.ID] = n
		}
		return nil
	})
	return out, err
}
