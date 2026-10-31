// Package bus is where agents talk to each other and to you: channels with
// members, messages kept in files, and a digest that wakes an agent when it
// is mentioned. There is no daemon: whichever process writes (`mad bus`,
// the desktop, an agent's tool) appends under a lock and puts one pointer
// in the agent's queue; the sidebar composes the digest when it sends that
// item (see Compose and Delivered).
//
// Everything is under the state directory's bus/: channels.json, a
// <channel>.jsonl of messages per channel, cursors/<agent>.json for what
// each agent has been told and has read, and a lock all changes take.
package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
)

// You is the person, a member of every channel; it is never woken.
const You = "you"

// system is who the lines mad itself writes are from.
const system = "mad"

// From says who wrote a message. The label is kept as it was at the time.
type From struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
}

// Channel is a place to talk. A DM is the channel dm.<idA>.<idB>.
type Channel struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic,omitempty"`
	Members   []string  `json:"members"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Paused    bool      `json:"paused,omitempty"`
	Archived  bool      `json:"archived,omitempty"`
	// Lead is the agent that answers for the channel's work: the one who
	// hears of a stall when its owner and sender do not act, and whom
	// unowned tasks of the channel are shown with. An agent that makes a
	// channel is its lead until SetLead says otherwise.
	Lead string `json:"lead,omitempty"`
}

// HandoffRef points from a message to the handoff it opens or closes.
type HandoffRef struct {
	ID     string `json:"id"`
	To     string `json:"to"`
	Status string `json:"status"`
	Parent string `json:"parent,omitempty"`
}

// Message is one line of a channel.
type Message struct {
	Seq  int       `json:"seq"`
	At   time.Time `json:"at"`
	From From      `json:"from"`
	// Kind is text, handoff, done or system.
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Mentions are agent ids, or "*" for @all.
	Mentions []string    `json:"mentions,omitempty"`
	ReplyTo  int         `json:"reply_to,omitempty"`
	Handoff  *HandoffRef `json:"handoff,omitempty"`
	// Images are the pictures it carries (images.go).
	Images []Image `json:"images,omitempty"`
}

// PostResult tells what a post did.
type PostResult struct {
	Seq  int      `json:"seq"`
	Woke []string `json:"woke"` // labels
	// Warning says what the poster should know: the channel is near its
	// limit, or paused.
	Warning string `json:"warning,omitempty"`
}

// Limits and clock, replaceable in tests.
var (
	// warnAt and pauseAt: agent posts since the last one from a person
	// that warn, and that pause the channel.
	warnAt, pauseAt = 20, 30
	// pingPong: posts alternating strictly between two agents that pause.
	pingPong = 8
	// maxLines is how long a channel's file may grow; then the last
	// keepLines stay.
	maxLines, keepLines = 2000, 500
	now                 = time.Now
)

func dir() string { return filepath.Join(paths.StateDir(), "bus") }

func chanFile() string { return filepath.Join(dir(), "channels.json") }

func msgFile(ch string) string { return filepath.Join(dir(), ch+".jsonl") }

func cursorFile(agentID string) string {
	return filepath.Join(dir(), "cursors", agentID+".json")
}

// locked runs fn holding the bus lock.
func locked(fn func() error) error {
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(dir(), ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return fn()
}

// writeAtomic replaces a file by renaming a new one over it.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type chanList struct {
	Channels []Channel `json:"channels"`
}

func loadChannels() (chanList, error) {
	var cl chanList
	data, err := os.ReadFile(chanFile())
	if errors.Is(err, os.ErrNotExist) {
		return cl, nil
	}
	if err != nil {
		return cl, err
	}
	return cl, json.Unmarshal(data, &cl)
}

func saveChannels(cl chanList) error {
	data, err := json.MarshalIndent(cl, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(chanFile(), data)
}

func (cl *chanList) find(id string) *Channel {
	for i := range cl.Channels {
		if cl.Channels[i].ID == id {
			return &cl.Channels[i]
		}
	}
	return nil
}

// readMsgs reads a channel's messages; a line that does not parse is
// skipped.
func readMsgs(ch string) ([]Message, error) {
	data, err := os.ReadFile(msgFile(ch))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Message
	for _, line := range strings.Split(string(data), "\n") {
		var m Message
		if line != "" && json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out, nil
}

func lastSeq(msgs []Message) int {
	if len(msgs) == 0 {
		return 0
	}
	return msgs[len(msgs)-1].Seq
}

// addMsg gives m the next seq and appends it to the channel's file and to
// msgs. A file past maxLines is cut to its last keepLines, with a line
// saying so.
func addMsg(ch string, msgs *[]Message, m Message) (Message, error) {
	m.Seq = lastSeq(*msgs) + 1
	if m.At.IsZero() {
		m.At = now()
	}
	*msgs = append(*msgs, m)
	if len(*msgs) > maxLines {
		cut := len(*msgs) - keepLines
		held := undelivered(ch, (*msgs)[:cut])
		kept := append(held, (*msgs)[cut:]...)
		text := fmt.Sprintf("Older messages were dropped; the last %d are kept.", keepLines)
		if len(held) > 0 {
			text = fmt.Sprintf("Older messages were dropped; the last %d are kept, and %d older ones not yet delivered to the agents they wake.", keepLines, len(held))
		}
		kept = append(kept, Message{
			Seq: m.Seq + 1, At: m.At, From: From{ID: system, Label: system, Kind: "system"}, Kind: "system",
			Text: text,
		})
		var b strings.Builder
		for _, k := range kept {
			line, err := json.Marshal(k)
			if err != nil {
				return m, err
			}
			b.Write(line)
			b.WriteByte('\n')
		}
		*msgs = kept
		return m, writeAtomic(msgFile(ch), []byte(b.String()))
	}
	line, err := json.Marshal(m)
	if err != nil {
		return m, err
	}
	f, err := os.OpenFile(msgFile(ch), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return m, err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return m, err
}

// maxHeld is the most old messages a compaction keeps for agents that have
// not been told of them.
const maxHeld = 200

// undelivered picks, of the messages dropped from channel ch, those that
// would wake a member that has not been delivered them yet (at most
// maxHeld, the newest). The bus lock is held by the caller.
func undelivered(ch string, dropped []Message) []Message {
	cl, err := loadChannels()
	if err != nil {
		return nil
	}
	c := cl.find(ch)
	if c == nil {
		return nil
	}
	var out []Message
	for _, m := range dropped {
		for _, id := range c.Members {
			if m.Seq > cursorOf(id, ch).Delivered && wakesAgent(*c, m, id) {
				out = append(out, m)
				break
			}
		}
	}
	return out[max(0, len(out)-maxHeld):]
}

// Slug makes a channel id of a name: lower case letters and digits (of
// any script: a channel may be called 支付重构) joined by single dashes.
// Everything else, a slash or an emoji included, is a separator, so an id
// is a safe file name.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	return b.String()
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func isDM(ch string) bool { return strings.HasPrefix(ch, "dm.") }

// DMChannel is the DM channel between a and b (agent ids, or You).
func DMChannel(a, b string) string { return dmID(a, b) }

// dmID is the channel between two ids.
func dmID(a, b string) string {
	ids := []string{a, b}
	sort.Strings(ids)
	return "dm." + ids[0] + "." + ids[1]
}

// DMSides are the two sides of DM channel ch, read from its id: forgetting
// an agent empties the channel's members, but never the id.
func DMSides(ch string) []string {
	if !isDM(ch) {
		return nil
	}
	return strings.Split(strings.TrimPrefix(ch, "dm."), ".")
}

// poked tells the sidebar, if one runs, that something is waiting.
func poked() { _ = poke.Send(poke.Poll) }

// Channels lists the channels not archived, by id.
func Channels() ([]Channel, error) {
	cl, err := loadChannels()
	if err != nil {
		return nil, err
	}
	var out []Channel
	for _, c := range cl.Channels {
		if !c.Archived {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Find returns channel id.
func Find(id string) (Channel, bool, error) {
	cl, err := loadChannels()
	if err != nil {
		return Channel{}, false, err
	}
	if c := cl.find(id); c != nil {
		return *c, true, nil
	}
	return Channel{}, false, nil
}

// Create makes a channel named name, with members (agent ids) and its
// creator among them.
func Create(name, topic string, members []string, by From) (Channel, error) {
	id := Slug(name)
	if id == "" {
		return Channel{}, fmt.Errorf("%q is no name for a channel", name)
	}
	var out Channel
	err := locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		if cl.find(id) != nil {
			return fmt.Errorf("channel #%s exists", id)
		}
		c := Channel{ID: id, Topic: topic, Members: []string{}, CreatedBy: by.ID, CreatedAt: now()}
		if by.ID != You && by.ID != "" && by.ID != system {
			c.Lead = by.ID
		}
		for _, m := range append(append([]string(nil), members...), by.ID) {
			if m != "" && m != You && !has(c.Members, m) {
				c.Members = append(c.Members, m)
			}
		}
		cl.Channels = append(cl.Channels, c)
		out = c
		return saveChannels(cl)
	})
	if err == nil {
		poked()
	}
	return out, err
}

// addMembers adds ids to channel c, whose messages are msgs: what is
// already in it is not news to them.
func addMembers(c *Channel, msgs []Message, ids ...string) error {
	for _, id := range ids {
		if id == "" || id == You || has(c.Members, id) {
			continue
		}
		c.Members = append(c.Members, id)
		if last := lastSeq(msgs); last > 0 {
			if err := setCursor(id, c.ID, func(cu *cursor) { cu.Delivered, cu.Read = last, last }); err != nil {
				return err
			}
		}
	}
	return nil
}

// change runs fn on channel id under the lock and saves it.
func change(id string, fn func(*Channel, []Message) error) error {
	return locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		c := cl.find(id)
		if c == nil || c.Archived {
			return fmt.Errorf("no channel #%s", id)
		}
		msgs, err := readMsgs(id)
		if err != nil {
			return err
		}
		if err := fn(c, msgs); err != nil {
			return err
		}
		return saveChannels(cl)
	})
}

// SetLead makes agent agentID the lead of channel, adding it to the members
// if it is not one. The agent must exist and not be in a run; "" clears the
// lead. A DM has none.
func SetLead(channel, agentID string) error {
	if isDM(channel) {
		return errors.New("a DM has no lead")
	}
	if agentID != "" {
		st, err := state.Load()
		if err != nil {
			return err
		}
		a := findIn(st, agentID)
		if a == nil {
			return fmt.Errorf("no agent %q", agentID)
		}
		if a.Run != "" {
			return fmt.Errorf("%s is in a run and cannot lead a channel", drive.Label(a))
		}
	}
	err := change(channel, func(c *Channel, msgs []Message) error {
		if agentID != "" {
			if err := addMembers(c, msgs, agentID); err != nil {
				return err
			}
		}
		c.Lead = agentID
		return nil
	})
	if err == nil {
		poked()
	}
	return err
}

// Archive closes channel: it is no longer listed, and nothing is posted
// in it. Its messages stay.
func Archive(channel string) error {
	if isDM(channel) {
		return errors.New("a DM is not archived")
	}
	err := change(channel, func(c *Channel, _ []Message) error {
		c.Archived = true
		return nil
	})
	if err == nil {
		poked()
	}
	return err
}

// Join makes member (an agent id) a member of channel.
func Join(channel, member string) error { return Invite(channel, []string{member}) }

// Invite makes members (agent ids) members of channel.
func Invite(channel string, members []string) error {
	if isDM(channel) {
		return errors.New("a DM has its two sides and no others; make a channel to talk with more")
	}
	return change(channel, func(c *Channel, msgs []Message) error {
		return addMembers(c, msgs, members...)
	})
}

// Leave takes member out of channel, and its cursor with it.
func Leave(channel, member string) error {
	if isDM(channel) {
		return errors.New("a DM cannot be left: it is the conversation of its two sides")
	}
	return change(channel, func(c *Channel, _ []Message) error {
		var kept []string
		for _, m := range c.Members {
			if m != member {
				kept = append(kept, m)
			}
		}
		c.Members = kept
		if c.Lead == member {
			c.Lead = ""
		}
		return setCursor(member, channel, nil)
	})
}

// PostSystem adds a line from mad to channel. It wakes nobody.
func PostSystem(channel, text string) error {
	err := change(channel, func(c *Channel, msgs []Message) error {
		_, err := addMsg(channel, &msgs, systemMsg(text))
		return err
	})
	if err == nil {
		poked()
	}
	return err
}

func systemMsg(text string) Message {
	return Message{From: From{ID: system, Label: system, Kind: "system"}, Kind: "system", Text: text}
}

// Read returns up to limit messages of channel: the ones after seq since,
// or, with since <= 0, the last ones. A limit of 0 or less is 30, and 200
// is the most. It also returns the channel's last seq.
func Read(channel string, since, limit int) ([]Message, int, error) {
	if limit <= 0 {
		limit = 30
	}
	limit = min(limit, 200)
	var out []Message
	var last int
	err := locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		if cl.find(channel) == nil {
			return fmt.Errorf("no channel #%s", channel)
		}
		msgs, err := readMsgs(channel)
		if err != nil {
			return err
		}
		last = lastSeq(msgs)
		if since <= 0 {
			out = msgs[max(0, len(msgs)-limit):]
			return nil
		}
		for _, m := range msgs {
			if m.Seq > since && len(out) < limit {
				out = append(out, m)
			}
		}
		return nil
	})
	return out, last, err
}

// Resume lets a paused channel go on, with a line saying so, and wakes the
// agents that have things waiting in it.
func Resume(channel string) error {
	var members []string
	err := change(channel, func(c *Channel, msgs []Message) error {
		if !c.Paused {
			return nil
		}
		c.Paused = false
		m := systemMsg("Resumed by a person.")
		m.From = From{ID: You, Label: You, Kind: "system"} // a person's word ends the count of quiet posts
		members = append(members, c.Members...)
		_, err := addMsg(channel, &msgs, m)
		return err
	})
	if err != nil {
		return err
	}
	for _, id := range members {
		if wakes(id) {
			_ = Notify(id)
		}
	}
	poked()
	return nil
}

// Forget drops an agent's memberships and cursors, not its messages.
func Forget(agentID string) {
	_ = locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		for i := range cl.Channels {
			c := &cl.Channels[i]
			var kept []string
			for _, m := range c.Members {
				if m != agentID {
					kept = append(kept, m)
				}
			}
			c.Members = kept
			if c.Lead == agentID {
				c.Lead = ""
			}
		}
		_ = os.Remove(cursorFile(agentID))
		_ = os.Remove(wakeFile(agentID))
		return saveChannels(cl)
	})
	_ = dropCheckins(agentID)
}
