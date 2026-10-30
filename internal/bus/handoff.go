package bus

import (
	"crypto/rand"
	"encoding/hex"
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
	"github.com/dcyber-lab/mad/internal/status"
)

// Limits of a chain of handoffs.
const (
	// MaxDepth is how many handoffs deep a chain may go.
	MaxDepth = 4
	// MaxSpawns is how many agents a chain, or one agent, may start for
	// handoffs without a word from a person.
	MaxSpawns = 2
	// MaxOpenSpawned is how many agents started for handoffs may have
	// their handoff open at once, deck-wide.
	MaxOpenSpawned = 4
)

// lapseSlack is how long after a handoff was delivered a turn must end to
// count as the one that took it up, not one that was ending as it came.
const lapseSlack = 5 * time.Second

// lapseWords is how much of the receiver's last reply a lapse posts.
const lapseWords = 600

// readTurn is status.ReadTurn, replaceable in tests.
var readTurn = status.ReadTurn

// Handoff is a piece of work one agent gave another. It is open until the
// receiver finishes it (done or declined) or its turn ends without that
// (lapsed).
type Handoff struct {
	ID      string   `json:"id"`
	From    string   `json:"from"` // agent ids
	To      string   `json:"to"`
	Task    string   `json:"task"`
	Context string   `json:"context,omitempty"`
	Files   []string `json:"files,omitempty"`
	// Channel is where it was posted: a channel, or the DM of the two.
	Channel string `json:"channel"`
	// Seq is its message's seq in Channel.
	Seq int `json:"seq,omitempty"`
	// Parent is the handoff its sender was working on when it gave this
	// one, or "".
	Parent string `json:"parent,omitempty"`
	// Spawned is set when the receiver was started for it.
	Spawned bool `json:"spawned,omitempty"`
	// Status is open, done, declined or lapsed.
	Status      string    `json:"status"`
	Result      string    `json:"result,omitempty"`
	At          time.Time `json:"at"`
	DeliveredAt time.Time `json:"delivered_at,omitzero"`
	DoneAt      time.Time `json:"done_at,omitzero"`
}

func handoffFile() string { return filepath.Join(dir(), "handoffs.jsonl") }

// loadHandoffs reads the handoffs, oldest first; a line that does not
// parse is skipped.
func loadHandoffs() ([]Handoff, error) {
	data, err := os.ReadFile(handoffFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Handoff
	for _, line := range strings.Split(string(data), "\n") {
		var h Handoff
		if line != "" && json.Unmarshal([]byte(line), &h) == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

// saveHandoffs rewrites the file. The bus lock is held by the caller.
func saveHandoffs(hs []Handoff) error {
	var b strings.Builder
	for _, h := range hs {
		line, err := json.Marshal(h)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return writeAtomic(handoffFile(), []byte(b.String()))
}

// changeHandoff runs fn on handoff id under the lock and saves the file.
func changeHandoff(id string, fn func(*Handoff) error) (Handoff, error) {
	var out Handoff
	err := locked(func() error {
		hs, err := loadHandoffs()
		if err != nil {
			return err
		}
		for i := range hs {
			if hs[i].ID == id {
				if err := fn(&hs[i]); err != nil {
					return err
				}
				out = hs[i]
				return saveHandoffs(hs)
			}
		}
		return fmt.Errorf("no handoff %s", id)
	})
	return out, err
}

func findHandoff(hs []Handoff, id string) (Handoff, bool) {
	for _, h := range hs {
		if h.ID == id {
			return h, true
		}
	}
	return Handoff{}, false
}

// chainIDs is the ids from the start of h's chain to h.
func chainIDs(hs []Handoff, h Handoff) []string {
	ids := []string{h.ID}
	for len(ids) <= 2*MaxDepth+1 && h.Parent != "" {
		p, ok := findHandoff(hs, h.Parent)
		ids = append(ids, h.Parent)
		if !ok {
			break
		}
		h = p
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	return ids
}

// Handoffs lists all handoffs, oldest first.
func Handoffs() ([]Handoff, error) { return loadHandoffs() }

// Open lists the handoffs agent id received and has not finished.
func Open(agentID string) ([]Handoff, error) {
	hs, err := loadHandoffs()
	var out []Handoff
	for _, h := range hs {
		if h.To == agentID && h.Status == "open" {
			out = append(out, h)
		}
	}
	return out, err
}

// parentFor is the handoff a new one from agent id continues: the one given,
// else the latest open one id received.
func parentFor(hs []Handoff, id, parent string) (string, error) {
	if parent != "" {
		if _, ok := findHandoff(hs, parent); !ok {
			return "", fmt.Errorf("no handoff %s", parent)
		}
		return parent, nil
	}
	best := ""
	var at time.Time
	for _, h := range hs {
		if h.To == id && h.Status == "open" && !h.At.Before(at) {
			best, at = h.ID, h.At
		}
	}
	return best, nil
}

// validChannel tells whether id can be a channel's id: a slug, or a DM's
// id, never a path.
func validChannel(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
}

// lastYou is when a person last posted in channel, or, with "", in any
// channel.
func lastYou(channel string) (time.Time, error) {
	ids := []string{channel}
	if channel == "" {
		cl, err := loadChannels()
		if err != nil {
			return time.Time{}, err
		}
		ids = nil
		for _, c := range cl.Channels {
			ids = append(ids, c.ID)
		}
	} else if !validChannel(channel) {
		return time.Time{}, fmt.Errorf("%q is no channel", channel)
	}
	var since time.Time
	for _, id := range ids {
		msgs, err := readMsgs(id)
		if err != nil {
			return time.Time{}, err
		}
		for _, m := range msgs {
			if m.From.ID == You && m.At.After(since) {
				since = m.At
			}
		}
	}
	return since, nil
}

// ChainState tells, for a handoff agent from would give with parent (""
// for the one it is working on, if any) in channel (any channel, when ""):
// the depth of the chain it would join, and how many agents have been
// started for handoffs since the last word from a person there: the most
// of those started by the chain, and those started by from itself, so an
// agent with no chain is counted too.
func ChainState(from, parent, channel string) (depth, spawns int, err error) {
	since, err := lastYou(channel)
	if err != nil {
		return 0, 0, err
	}
	hs, err := loadHandoffs()
	if err != nil {
		return 0, 0, err
	}
	if parent, err = parentFor(hs, from, parent); err != nil {
		return 0, 0, err
	}
	own := 0
	for _, h := range hs {
		if h.From == from && h.Spawned && h.At.After(since) {
			own++
		}
	}
	h, ok := findHandoff(hs, parent)
	for n := 0; ok && n < 2*MaxDepth+1; n++ {
		depth++
		if h.Spawned && h.At.After(since) {
			spawns++
		}
		h, ok = findHandoff(hs, h.Parent)
	}
	return depth, max(spawns, own), nil
}

// OpenSpawned counts the agents started for handoffs that are still open.
func OpenSpawned() (int, error) {
	hs, err := loadHandoffs()
	n := 0
	for _, h := range hs {
		if h.Spawned && h.Status == "open" {
			n++
		}
	}
	return n, err
}

// MarkSpawned notes that the receiver of handoff id was started for it.
func MarkSpawned(id string) error {
	_, err := changeHandoff(id, func(h *Handoff) error { h.Spawned = true; return nil })
	return err
}

func newHandoffID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "h-" + hex.EncodeToString(b)
}

// HandOff gives the work task to agent to, with context and files it will
// need, and wakes it. It is posted in channel, or in the DM of the two when
// channel is "". The parent is the handoff it continues; when "", it is the
// open one from agent from received, if any. Refused: a chain more than
// MaxDepth deep, a handoff to oneself or to an agent in a run.
func HandOff(from From, to, task, context string, files []string, channel, parent string) (Handoff, PostResult, error) {
	return HandOffTask(from, to, task, context, files, channel, parent, "")
}

// HandOffTask is HandOff for the task taskID on the board: the handoff
// attaches to it and its owner becomes the receiver. With "" the handoff
// makes a task of its own.
func HandOffTask(from From, to, task, context string, files []string, channel, parent, taskID string) (Handoff, PostResult, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return Handoff{}, PostResult{}, errors.New("no task given")
	}
	if to == "" || to == You {
		return Handoff{}, PostResult{}, errors.New("a handoff goes to an agent")
	}
	if to == from.ID {
		return Handoff{}, PostResult{}, errors.New("a handoff to yourself reaches no one")
	}
	st, err := state.Load()
	if err != nil {
		return Handoff{}, PostResult{}, err
	}
	a := findIn(st, to)
	if a == nil {
		return Handoff{}, PostResult{}, fmt.Errorf("no agent %q", to)
	}
	if a.Run != "" {
		return Handoff{}, PostResult{}, fmt.Errorf("%s is in a run; use mad run note", drive.Label(a))
	}
	dm := channel == ""
	if dm {
		channel = dmID(from.ID, to)
	}
	h := Handoff{ID: newHandoffID(), From: from.ID, To: to, Task: task, Context: strings.TrimSpace(context),
		Files: files, Channel: channel, Status: "open", At: now()}
	var prev *Task
	err = locked(func() error {
		hs, err := loadHandoffs()
		if err != nil {
			return err
		}
		if h.Parent, err = parentFor(hs, from.ID, parent); err != nil {
			return err
		}
		if h.Parent != "" {
			if p, _ := findHandoff(hs, h.Parent); len(chainIDs(hs, p)) >= MaxDepth {
				return fmt.Errorf("a chain of handoffs is at most %d deep: %s; finish the work yourself, or bus_done it", MaxDepth, strings.Join(chainIDs(hs, p), " → "))
			}
		}
		ts, err := loadTasks()
		if err != nil {
			return err
		}
		var next []Task
		if next, prev, err = attachTask(ts, h, taskID, projectOf(st, from.ID)); err != nil {
			return err
		}
		if err := saveHandoffs(append(hs, h)); err != nil {
			return err
		}
		return saveTasks(next)
	})
	if err != nil {
		return Handoff{}, PostResult{}, err
	}
	sp := postSpec{channel: channel, from: from, text: task, kind: "handoff", mentions: []string{to},
		ref: &HandoffRef{ID: h.ID, To: to, Status: "open", Parent: h.Parent}}
	if dm {
		sp.dm, sp.wake = []string{from.ID, to}, to
	}
	// The seq is recorded under the lock of the post, so a digest that
	// carries the message finds it on the handoff.
	sp.stored = func(seq int) error {
		hs, err := loadHandoffs()
		if err != nil {
			return err
		}
		for i := range hs {
			if hs[i].ID == h.ID {
				hs[i].Seq = seq
			}
		}
		return saveHandoffs(hs)
	}
	res, err := post(sp)
	if err != nil {
		_ = dropHandoff(h.ID)
		undoTask(h.ID, prev)
		return Handoff{}, PostResult{}, err
	}
	h.Seq = res.Seq
	return h, res, nil
}

// dropHandoff removes a handoff whose post failed.
func dropHandoff(id string) error {
	return locked(func() error {
		hs, err := loadHandoffs()
		if err != nil {
			return err
		}
		var kept []Handoff
		for _, h := range hs {
			if h.ID != id {
				kept = append(kept, h)
			}
		}
		return saveHandoffs(kept)
	})
}

// Done finishes handoff id as its receiver, from, with result; status is
// done, or declined. A lapsed handoff may still be finished: the receiver
// was only between turns. It posts the result in the handoff's channel and wakes
// the agent that gave the work.
func Done(from From, id, result, status string) (Handoff, PostResult, error) {
	if status == "" {
		status = "done"
	}
	if status != "done" && status != "declined" {
		return Handoff{}, PostResult{}, fmt.Errorf("status is done or declined, not %q", status)
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return Handoff{}, PostResult{}, errors.New("no result given: say what you did, or why you decline")
	}
	prev := ""
	h, err := changeHandoff(id, func(h *Handoff) error {
		switch {
		case h.To != from.ID:
			return fmt.Errorf("handoff %s was not given to you", id)
		case h.Status != "open" && h.Status != "lapsed":
			return fmt.Errorf("handoff %s is already %s", id, h.Status)
		}
		prev = h.Status
		h.Status, h.Result, h.DoneAt = status, result, now()
		return nil
	})
	if err != nil {
		return Handoff{}, PostResult{}, err
	}
	res, err := post(resultSpec(h, from, result, "done"))
	if err != nil {
		_, _ = changeHandoff(id, func(h *Handoff) error { h.Status, h.Result, h.DoneAt = prev, "", time.Time{}; return nil })
		return Handoff{}, PostResult{}, err
	}
	finishTask(h)
	return h, res, nil
}

// resultSpec is the post that tells h's sender how it ended: kind done
// from the receiver, or system from mad for a lapse.
func resultSpec(h Handoff, from From, text, kind string) postSpec {
	sp := postSpec{channel: h.Channel, from: from, text: text, kind: kind, mentions: []string{h.From},
		ref: &HandoffRef{ID: h.ID, To: h.To, Status: h.Status, Parent: h.Parent}}
	if isDM(h.Channel) {
		sp.dm, sp.wake = []string{h.From, h.To}, h.From
	}
	return sp
}

// asked tells whether the receiver of h has said something to its sender
// since h was delivered: a post in their DM, or one in h's channel that
// mentions the sender. Then its turn ended to wait for an answer.
func asked(h Handoff) bool {
	for _, ch := range []string{dmID(h.To, h.From), h.Channel} {
		msgs, _ := readMsgs(ch)
		for _, m := range msgs {
			if m.From.ID != h.To || m.At.Before(h.DeliveredAt) || m.Kind != "text" {
				continue
			}
			if isDM(ch) || has(m.Mentions, h.From) {
				return true
			}
		}
	}
	return false
}

// CheckLapses marks lapsed the open handoffs whose receiver's turn ended
// after it was delivered (lapseSlack later, so a turn ending as it came
// does not count) with no bus_done. Not lapsed: a handoff whose receiver
// has handed on a part of it that is open, has more bus news waiting for
// it, or has asked its sender something. A line with the receiver's last words
// goes in the handoff's channel and wakes the agent that gave the work.
func CheckLapses(at time.Time) error {
	hs, err := loadHandoffs()
	if err != nil {
		return err
	}
	var st *state.State
	var errs []error
	for _, h := range hs {
		if h.Status != "open" || h.DeliveredAt.IsZero() {
			continue
		}
		t := readTurn(h.To)
		if t == nil || !t.At.After(h.DeliveredAt.Add(lapseSlack)) {
			continue
		}
		if hasOpenChild(hs, h.ID) || hasBusItem(h.To) || asked(h) {
			continue
		}
		if st == nil {
			if st, err = state.Load(); err != nil {
				return err
			}
		}
		label := drive.ShortID(h.To)
		if a := findIn(st, h.To); a != nil {
			label = drive.Label(a)
		}
		text := fmt.Sprintf("%s lapsed: %s ended its turn without bus_done.", h.ID, label)
		if r := strings.TrimSpace(t.Reply); r != "" {
			if rs := []rune(r); len(rs) > lapseWords {
				r = string(rs[:lapseWords]) + "…"
			}
			text += " Its last words: " + r
		}
		lapsed, err := changeHandoff(h.ID, func(x *Handoff) error {
			if x.Status != "open" {
				return errors.New("finished meanwhile")
			}
			x.Status, x.DoneAt = "lapsed", at
			return nil
		})
		if err != nil {
			continue
		}
		if _, err := post(resultSpec(lapsed, From{ID: system, Label: system, Kind: "system"}, text, "system")); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// hasOpenChild tells whether an open handoff continues handoff id.
func hasOpenChild(hs []Handoff, id string) bool {
	for _, h := range hs {
		if h.Parent == id && h.Status == "open" {
			return true
		}
	}
	return false
}

// pendingBus tells whether agent id has a bus item waiting in its queue.
func hasBusItem(agentID string) bool {
	q, _ := queue.Load(agentID)
	for _, it := range q.Items {
		if it.Bus {
			return true
		}
	}
	return false
}

// Tree arranges handoffs by parent: each with its depth, a chain's
// handoffs after the one they continue, chains oldest first.
func Tree(hs []Handoff) (list []Handoff, depth []int) {
	byID := map[string]bool{}
	for _, h := range hs {
		byID[h.ID] = true
	}
	sorted := append([]Handoff(nil), hs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	kids := map[string][]Handoff{}
	var roots []Handoff
	for _, h := range sorted {
		if h.Parent != "" && byID[h.Parent] {
			kids[h.Parent] = append(kids[h.Parent], h)
		} else {
			roots = append(roots, h)
		}
	}
	var walk func(h Handoff, d int)
	walk = func(h Handoff, d int) {
		list, depth = append(list, h), append(depth, d)
		for _, k := range kids[h.ID] {
			walk(k, d+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return list, depth
}
