package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// cursorOf is agent id's cursor for a channel; zero when unreadable.
func cursorOf(agentID, channel string) cursor {
	cs, _ := loadCursors(agentID)
	return cs[channel]
}

// wakeEvery is the least time between two digests for one agent.
const wakeEvery = time.Minute

func wakeFile(agentID string) string {
	return filepath.Join(dir(), "cursors", agentID+".wake")
}

// lastWake is when agent id was last delivered a digest; zero if never.
func lastWake(agentID string) time.Time {
	data, err := os.ReadFile(wakeFile(agentID))
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	return t
}

// WaitFor is how much longer a digest for agent id should wait for its
// minute: an agent is woken by the bus at most once a minute, and what
// arrives meanwhile is in the next digest. It is 0 when the last digest
// was a minute ago or more, and when what waits carries a handoff or its
// result: a relay is not held back.
func WaitFor(agentID string, at time.Time) time.Duration {
	last := lastWake(agentID)
	if last.IsZero() || at.Sub(last) >= wakeEvery {
		return 0
	}
	var list []entry
	_ = locked(func() error {
		var err error
		list, _, _, err = gather(agentID)
		return err
	})
	for _, e := range list {
		if e.m.Handoff != nil {
			return 0
		}
	}
	return wakeEvery - at.Sub(last)
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
	if m.Kind == "system" && m.Handoff == nil || m.From.ID == id {
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
		if c.Archived || !has(c.Members, id) {
			continue
		}
		msgs, err := readMsgs(c.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		cu := cs[c.ID]
		// A paused channel is left for when it is resumed, except up to
		// the last handoff message for the agent: a relay is not held.
		cut := lastSeq(msgs)
		if c.Paused {
			cut = 0
			for _, m := range msgs {
				if m.Seq > cu.Delivered && m.Handoff != nil && wakesAgent(c, m, id) {
					cut = m.Seq
				}
			}
			if cut == 0 {
				continue
			}
		}
		n := 0
		for _, m := range msgs {
			if m.Seq > cut {
				break
			}
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
		if cut > cu.Delivered {
			upto[c.ID] = cut
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
	text, upto, _, ok, err = ComposeCounted(agentID, at)
	return
}

// ComposeCounted is Compose that also says whether the digest carries a
// message from the person: then the turn is theirs, and the spend gate
// neither holds nor counts it.
func ComposeCounted(agentID string, at time.Time) (text string, upto map[string]int, yours, ok bool, err error) {
	st, err := state.Load()
	if err != nil {
		return "", nil, false, false, err
	}
	var list []entry
	var rest []also
	err = locked(func() error {
		var err error
		list, rest, upto, err = gather(agentID)
		return err
	})
	if err != nil || len(list) == 0 {
		return "", nil, false, false, err
	}
	yours = fromPerson(list)

	hs, _ := Handoffs()
	ts, _ := Tasks()
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
	fmt.Fprintf(&b, "Your name on the bus is %s (the deck's name for this session). Who wrote reads only the bus: an answer written here reaches no one.\n", label(agentID))
	for _, e := range list {
		name := "#" + e.ch.ID
		if isDM(e.ch.ID) {
			name = "DM with " + e.m.From.Label
		}
		fmt.Fprintf(&b, "\n%s · %s · #%d · %s", name, e.m.From.Label, e.m.Seq, e.m.At.In(at.Location()).Format("15:04"))
		if e.m.ReplyTo > 0 {
			fmt.Fprintf(&b, " (reply to #%d)", e.m.ReplyTo)
		}
		switch ref := e.m.Handoff; {
		case ref != nil && e.m.Kind == "handoff":
			if h, found := findHandoff(hs, ref.ID); found && h.Status != "open" {
				fmt.Fprintf(&b, " — handoff %s, already %s\nTask: %s\nNothing to do for it: do not bus_done it.\n", ref.ID, h.Status, e.m.Text)
				break
			}
			fmt.Fprintf(&b, " — handoff %s\n", ref.ID)
			writeCard(&b, hs, ts, ref.ID, e.m.Text)
		case ref != nil && e.m.Kind == "done":
			fmt.Fprintf(&b, " — %s %s\n%s\n", ref.ID, ref.Status, e.m.Text)
		default:
			b.WriteString("\n" + e.m.Text + "\n")
			writeImages(&b, e.m)
		}
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
	return b.String(), upto, yours, true, nil
}

// writeImages names a message's pictures by their files: an agent opens
// one with its file-read tool.
func writeImages(b *strings.Builder, m Message) {
	if len(m.Images) == 0 {
		return
	}
	b.WriteString("Pictures (files: open them with your file-read tool):\n")
	for _, im := range m.Images {
		if p := im.Path(); p != "" {
			fmt.Fprintf(b, "- %s\n", p)
		} else {
			fmt.Fprintf(b, "- (a picture that is gone: %s)\n", im.ID[:8])
		}
	}
}

// writeCard writes the body of a handoff's card: its task (text), context,
// files and chain, and how to finish it.
func writeCard(b *strings.Builder, hs []Handoff, ts []Task, id, text string) {
	fmt.Fprintf(b, "Task: %s\n", text)
	if h, found := findHandoff(hs, id); found {
		if h.Context != "" {
			fmt.Fprintf(b, "Context: %s\n", h.Context)
		}
		if len(h.Files) > 0 {
			fmt.Fprintf(b, "Files: %s\n", strings.Join(h.Files, ", "))
		}
		if h.Parent != "" {
			fmt.Fprintf(b, "Chain: %s\n", strings.Join(chainIDs(hs, h), " → "))
		}
	}
	if t := taskOf(ts, id); t != nil {
		fmt.Fprintf(b, "Task: %s (board)\n", t.ID)
	}
	fmt.Fprintf(b, "Finish it with bus_done(%q, result) when done, or decline it with status \"declined\".\n", id)
}

// Delivered moves agent id's cursors to upto, the digest having been sent,
// and stamps the open handoffs for id that the digest carried.
func Delivered(agentID string, upto map[string]int) error {
	if len(upto) == 0 {
		return nil
	}
	return locked(func() error {
		_ = writeAtomic(wakeFile(agentID), []byte(now().Format(time.RFC3339Nano)))
		for ch, seq := range upto {
			err := setCursor(agentID, ch, func(cu *cursor) { cu.Delivered = max(cu.Delivered, seq) })
			if err != nil {
				return err
			}
		}
		hs, err := loadHandoffs()
		if err != nil {
			return err
		}
		changed := false
		for i := range hs {
			h := &hs[i]
			if h.To == agentID && h.Status == "open" && h.DeliveredAt.IsZero() && h.Seq > 0 && h.Seq <= upto[h.Channel] {
				h.DeliveredAt = now()
				changed = true
			}
		}
		if !changed {
			return nil
		}
		if err := saveHandoffs(hs); err != nil {
			return err
		}
		ts, err := loadTasks()
		if err != nil {
			return err
		}
		moved := false
		for _, h := range hs {
			if t := taskOf(ts, h.ID); t != nil && h.To == agentID && !h.DeliveredAt.IsZero() && t.Status == TaskTodo {
				t.Status, t.UpdatedAt = TaskDoing, h.DeliveredAt
				moved = true
			}
		}
		if moved {
			return saveTasks(ts)
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
