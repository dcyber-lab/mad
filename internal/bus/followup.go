package bus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// Follow-ups: work that stands without a word is raised, step by step, to
// the agent that owns it, to whoever gave it (or the channel's lead), and
// last to the person. See CheckFollowUps.
//
// Nudges are posts of kind "nudge" from mad (the same author as a lapse
// line: From{ID: "mad", Label: "mad", Kind: "system"}) that mention their
// target. They wake it like a relay does, but they count toward a
// channel's quiet budget, wait for the agent's minute (WaitFor), and are
// not posted in a paused channel.

// Tunables, replaceable in tests. FollowUp (board.go) is how long work may
// stand before the first rung.
var (
	// NudgesOn is false when config.json says bus.follow_up is "0": the
	// board still derives stalled cards, but no agent is nudged.
	NudgesOn = true
	// MaxIdle: with no message by a person anywhere for this long, nobody
	// is nudged at all.
	MaxIdle = 8 * time.Hour

	// maxNudges is the most nudges one record ever gets.
	maxNudges = 2
	// deckNudges per deckWindow is the most nudges mad sends, deck-wide.
	deckNudges = 6
	deckWindow = 10 * time.Minute

	senderWait  = 15 * time.Minute // a lapsed handoff's sender may act this long
	permWait    = 30 * time.Minute // a permission wait this long is reminded
	askWait     = 20 * time.Minute // an agent's question this old, unanswered, is nudged
	askShownFor = 24 * time.Hour   // ...and not older than this
	personEvery = 2 * time.Hour    // the person hears of an unchanged stall this often
	personMin   = 10 * time.Minute // ...and of a changed one no sooner than this
	keepRecords = 7 * 24 * time.Hour

	// readHook is status.ReadHook, replaceable in tests.
	readHook = status.ReadHook
)

// tellPerson notifies the person with a notification of kind (followup or
// report) as the deck's notifications do; replaced in tests.
var tellPerson = func(kind, message string) {
	cfg, _ := deck.LoadConfig()
	if !cfg.Notify.Wants(kind) {
		return
	}
	_ = notify.Send(cfg.Notify, notify.Event{Kind: kind, Project: "board", Message: message})
}

// madFrom is who nudges are from.
var madFrom = From{ID: system, Label: system, Kind: "system"}

// nudgeStore is bus/nudges.json: the nudges of records that have no place
// of their own for them (handoffs, asks), the times of the latest nudges
// deck-wide, and the notifications sent to the person.
type nudgeStore struct {
	Records map[string][]Nudge `json:"records,omitempty"`
	Log     []time.Time        `json:"log,omitempty"`
	// Sent is when each notification was sent, by key; Stalled is the set
	// the last "followup" notification was about.
	Sent    map[string]time.Time `json:"sent,omitempty"`
	Stalled string               `json:"stalled,omitempty"`
}

func nudgeFile() string { return filepath.Join(dir(), "nudges.json") }

func loadNudges() nudgeStore {
	var ns nudgeStore
	if data, err := os.ReadFile(nudgeFile()); err == nil {
		_ = json.Unmarshal(data, &ns)
	}
	return ns
}

// changeNudges runs fn on the store under the bus lock and saves it.
func changeNudges(fn func(*nudgeStore) error) error {
	return locked(func() error {
		ns := loadNudges()
		if err := fn(&ns); err != nil {
			return err
		}
		data, err := json.Marshal(ns)
		if err != nil {
			return err
		}
		if old, err := os.ReadFile(nudgeFile()); err == nil && string(old) == string(data) {
			return nil
		}
		return writeAtomic(nudgeFile(), data)
	})
}

// prune drops what is too old to matter at time at.
func (ns *nudgeStore) prune(at time.Time) {
	var log []time.Time
	for _, x := range ns.Log {
		if at.Sub(x) < deckWindow {
			log = append(log, x)
		}
	}
	ns.Log = log
	for k, l := range ns.Records {
		if len(l) == 0 || at.Sub(l[len(l)-1].At) > keepRecords {
			delete(ns.Records, k)
		}
	}
	for k, x := range ns.Sent {
		if at.Sub(x) > keepRecords && k != "followup" {
			delete(ns.Sent, k)
		}
	}
}

func hasWhy(l []Nudge, why ...string) bool {
	for _, x := range l {
		for _, w := range why {
			if x.Why == w {
				return true
			}
		}
	}
	return false
}

// reserve records nudge n on record rec (a task when onTask, else a key of
// the store) and in the deck-wide log, unless a cap forbids it: the record
// has maxNudges, or one for the same reason, or the deck sent deckNudges in
// deckWindow.
func reserve(rec string, onTask bool, n Nudge) bool {
	ok := false
	_ = changeNudges(func(ns *nudgeStore) error {
		ns.prune(n.At)
		if len(ns.Log) >= deckNudges {
			return nil
		}
		var ts []Task
		var hist []Nudge
		if onTask {
			var err error
			if ts, err = loadTasks(); err != nil {
				return err
			}
			t := findTask(ts, rec)
			if t == nil {
				return nil
			}
			hist = t.Nudges
		} else {
			hist = ns.Records[rec]
		}
		if len(hist) >= maxNudges || hasWhy(hist, n.Why) {
			return nil
		}
		hist = append(append([]Nudge(nil), hist...), n)
		if onTask {
			findTask(ts, rec).Nudges = hist
			if err := saveTasks(ts); err != nil {
				return err
			}
		} else {
			if ns.Records == nil {
				ns.Records = map[string][]Nudge{}
			}
			ns.Records[rec] = hist
		}
		ns.Log = append(ns.Log, n.At)
		ok = true
		return nil
	})
	return ok
}

// release takes back a reservation whose post did not go out.
func release(rec string, onTask bool, n Nudge) {
	_ = changeNudges(func(ns *nudgeStore) error {
		drop := func(l []Nudge) []Nudge {
			var out []Nudge
			for _, x := range l {
				if x.Why != n.Why || !x.At.Equal(n.At) {
					out = append(out, x)
				}
			}
			return out
		}
		if onTask {
			ts, err := loadTasks()
			if err != nil {
				return err
			}
			if t := findTask(ts, rec); t != nil {
				t.Nudges = drop(t.Nudges)
				if err := saveTasks(ts); err != nil {
					return err
				}
			}
		} else if l := drop(ns.Records[rec]); len(l) > 0 {
			ns.Records[rec] = l
		} else {
			delete(ns.Records, rec)
		}
		for i, x := range ns.Log {
			if x.Equal(n.At) {
				ns.Log = append(ns.Log[:i], ns.Log[i+1:]...)
				break
			}
		}
		return nil
	})
}

// nudge is one reminder to send.
type nudge struct {
	rec     string // the record it is kept on: a task id, or a key of the store
	onTask  bool
	channel string
	to      string // an agent id
	why     string // owner, sender, lead or ask: each at most once per record
	text    string
}

// send reserves and posts n; it reports whether it went out.
func (n nudge) send(at time.Time) bool {
	rec := Nudge{At: at, To: n.to, Why: n.why}
	if !reserve(n.rec, n.onTask, rec) {
		return false
	}
	sp := postSpec{channel: n.channel, from: madFrom, kind: "nudge", text: n.text, mentions: []string{n.to}}
	if parts := strings.Split(n.channel, "."); isDM(n.channel) && len(parts) == 3 {
		sp.dm, sp.wake = parts[1:], n.to
	}
	if _, err := post(sp); err != nil {
		release(n.rec, n.onTask, rec)
		return false
	}
	return true
}

// canNudge tells whether id is an agent mad may wake: it exists and is not
// in a run.
func canNudge(st *state.State, id string) bool {
	if id == "" || id == You || id == system {
		return false
	}
	a := findIn(st, id)
	return a != nil && a.Run == ""
}

// mins writes d as "20 min".
func mins(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 120 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h", m/60)
}

// dur writes d as 45m, 1h or 1h20m.
func dur(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh%dm", m/60, m%60)
}

// stall is something the person is told is stalled.
type stall struct{ id, text string }

// CheckFollowUps raises work that stands without a word, at time at;
// statuses says how each agent stands (as for Board). The sidebar runs it
// every minute. Everything is measured from files (a task's change, a
// handoff's delivery, a turn's end, a message's time), so it holds across
// restarts. The ladder, t being the time since the work last moved:
//
//  1. t >= FollowUp, the owner idle (never running; waiting is the
//     person's): the owner is nudged, once.
//  2. t >= 2*FollowUp, or a handoff lapsed and its sender did nothing for
//     15 minutes: the sender (else the channel's lead) is nudged, once.
//  3. t >= 3*FollowUp: the person gets a "followup" notification naming
//     what is stalled, at most every 2 hours while that is unchanged. No
//     post.
//  4. An agent waiting on a permission for 30 minutes: the person gets one
//     reminder.
//  5. A question from one agent to another (a text post mentioning it, or
//     in their DM, ending in "?") unanswered in that channel for 20
//     minutes, the other idle: it is nudged, once.
//
// A record gets at most two nudges ever (history is on Task.Nudges, or in
// nudges.json), the deck at most six per ten minutes, none go to a paused
// channel, and none at all while no person has posted for MaxIdle. A
// FollowUp of 0 or NudgesOn false turns off the nudges (1, 2, 5); the
// notifications (3, 4) still go.
func CheckFollowUps(at time.Time, statuses map[string]string) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	cards, err := Board(st, statuses, at)
	if err != nil {
		return err
	}
	hs, err := loadHandoffs()
	if err != nil {
		return err
	}
	cl, err := loadChannels()
	if err != nil {
		return err
	}
	d := &boardData{st: st, chans: cl, msgs: map[string][]Message{}}
	closed := func(ch string) bool { c := cl.find(ch); return c == nil || c.Archived || c.Paused }

	nudging := NudgesOn && FollowUp > 0
	if nudging {
		since, err := lastYou("")
		nudging = err == nil && !since.IsZero() && at.Sub(since) <= MaxIdle
	}
	f := FollowUp

	var stalled []stall
	for _, c := range cards {
		if c.Kind != CardTask || c.Task == nil {
			continue
		}
		t := *c.Task
		var h *Handoff
		if x, ok := findHandoff(hs, t.Handoff); ok {
			h = &x
		}
		ch := t.Channel
		if ch == "" && h != nil {
			ch = h.Channel
		}
		switch c.Why {
		case WhyStalled:
			ta := at.Sub(lastMoved(d, t, h))
			if ta >= 3*f {
				stalled = append(stalled, stall{t.ID, fmt.Sprintf("%s (%s, %s)", t.ID, c.OwnerLabel, dur(ta))})
			}
			if !nudging || ch == "" || closed(ch) {
				continue
			}
			sent := false
			if ta >= f && !hasWhy(t.Nudges, "owner") && canNudge(st, t.Owner) {
				yours := fmt.Sprintf("%s %q has been yours %s with no word. ", t.ID, t.Title, mins(ta))
				if h != nil && h.Status == "open" {
					yours += fmt.Sprintf("Finish it (bus_done %s), decline it, hand it on, or say what blocks you with @you.", h.ID)
				} else {
					yours += "Finish it (task_set status done), hand it on, or say what blocks you with @you."
				}
				sent = nudge{t.ID, true, ch, t.Owner, "owner", "[mad] follow-up: " + yours + " Say nothing if you are on it now."}.send(at)
			}
			if !sent && ta >= 2*f {
				escalate(st, cl, statuses, t, h, ch, c.OwnerLabel, fmt.Sprintf("has had no word for %s", mins(ta)), at)
			}
		case WhyLapsed:
			if h == nil || h.DoneAt.IsZero() {
				continue
			}
			tl := at.Sub(h.DoneAt)
			if tl >= 3*f {
				stalled = append(stalled, stall{h.ID, h.ID + " lapsed"})
			}
			if !nudging || ch == "" || closed(ch) || tl < senderWait || senderActed(d, *h) {
				continue
			}
			escalate(st, cl, statuses, t, h, ch, c.OwnerLabel, fmt.Sprintf("has a handoff, %s, that lapsed %s ago: its owner ended a turn without bus_done", h.ID, mins(tl)), at)
		}
	}
	if nudging {
		unansweredAsks(st, cl, d, statuses, at)
	}
	permissionReminders(st, statuses, at)
	return tellStalled(at, stalled)
}

// escalate nudges the sender of t's handoff, else the lead of its channel,
// about t, whose owner is ownerLabel; what says how it stands.
func escalate(st *state.State, cl chanList, statuses map[string]string, t Task, h *Handoff, ch, ownerLabel, what string, at time.Time) {
	if hasWhy(t.Nudges, "sender", "lead") {
		return
	}
	to, why := "", ""
	if h != nil && h.From != t.Owner && canNudge(st, h.From) && atRest(statuses[h.From]) {
		to, why = h.From, "sender"
	} else if c := cl.find(t.Channel); c != nil && c.Lead != t.Owner && canNudge(st, c.Lead) && atRest(statuses[c.Lead]) {
		to, why = c.Lead, "lead"
	}
	if to == "" {
		return
	}
	text := fmt.Sprintf("[mad] follow-up: %s %q (owner %s) %s. Check on it, take it back, hand it on, or tell @you what is stuck. Say nothing if you are on it now.", t.ID, t.Title, ownerLabel, what)
	nudge{t.ID, true, ch, to, why, text}.send(at)
}

// senderActed tells whether the sender of lapsed handoff h did anything
// since it lapsed: posted, or ended a turn.
func senderActed(d *boardData, h Handoff) bool {
	if d.lastBy(h.From).After(h.DoneAt) {
		return true
	}
	tr := readTurn(h.From)
	return tr != nil && tr.At.After(h.DoneAt)
}

// askTargets are the agents a question m in channel c is put to: the
// other side of a DM, else those it mentions.
func askTargets(c Channel, m Message) []string {
	var out []string
	if isDM(c.ID) {
		for _, p := range strings.Split(c.ID, ".")[1:] {
			if p != m.From.ID {
				out = append(out, p)
			}
		}
		return out
	}
	for _, id := range m.Mentions {
		if id != "*" && id != m.From.ID {
			out = append(out, id)
		}
	}
	return out
}

// unansweredAsks nudges, once, an idle agent that an agent asked a question
// (rung 5) and has not answered in that channel for askWait.
func unansweredAsks(st *state.State, cl chanList, d *boardData, statuses map[string]string, at time.Time) {
	for _, c := range cl.Channels {
		if c.Archived || c.Paused {
			continue
		}
		msgs := d.messages(c.ID)
		for i, m := range msgs {
			age := at.Sub(m.At)
			if m.Kind != "text" || age < askWait || age > askShownFor || m.From.ID == You || m.From.ID == system ||
				findIn(st, m.From.ID) == nil || !strings.HasSuffix(strings.TrimSpace(m.Text), "?") {
				continue
			}
			for _, x := range askTargets(c, m) {
				if !canNudge(st, x) || !atRest(statuses[x]) || answeredBy(msgs[i+1:], x) {
					continue
				}
				asker := m.From.Label
				if a := findIn(st, m.From.ID); a != nil {
					asker = drive.Label(a)
				}
				text := fmt.Sprintf("[mad] follow-up: %s asked you %s ago (#%d): %q. Answer in %s, or say what blocks you. Say nothing if you are on it now.",
					asker, mins(age), m.Seq, firstLine(m.Text, 100), where(c.ID))
				nudge{fmt.Sprintf("ask:%s#%d:%s", c.ID, m.Seq, x), false, c.ID, x, "ask", text}.send(at)
			}
		}
	}
}

func where(ch string) string {
	if isDM(ch) {
		return "this DM"
	}
	return "#" + ch
}

func answeredBy(msgs []Message, id string) bool {
	for _, m := range msgs {
		if m.From.ID == id {
			return true
		}
	}
	return false
}

// permissionReminders notifies the person, once per wait, of an agent that
// has waited permWait for their permission (rung 4).
func permissionReminders(st *state.State, statuses map[string]string, at time.Time) {
	for _, a := range st.OrderedAgents() {
		if statuses[a.ID] != status.Waiting {
			continue
		}
		hk := readHook(a.ID)
		if hk == nil || hk.State != status.Waiting || at.Sub(hk.At) < permWait {
			continue
		}
		key := fmt.Sprintf("perm:%s@%d", a.ID, hk.At.Unix())
		fresh := false
		_ = changeNudges(func(ns *nudgeStore) error {
			if _, ok := ns.Sent[key]; ok {
				return nil
			}
			if ns.Sent == nil {
				ns.Sent = map[string]time.Time{}
			}
			ns.Sent[key], fresh = at, true
			return nil
		})
		if !fresh {
			continue
		}
		msg := fmt.Sprintf("%s has waited %s for your permission", drive.Label(a), mins(at.Sub(hk.At)))
		if m := strings.TrimSpace(hk.Message); m != "" {
			msg += ": " + m
		}
		tellPerson(notify.FollowUp, msg)
	}
}

// tellStalled notifies the person of what is stalled for 3*FollowUp (rung
// 3): when the set changed (no sooner than personMin after the last), or
// every personEvery while unchanged.
func tellStalled(at time.Time, items []stall) error {
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	var ids, texts []string
	for _, it := range items {
		ids = append(ids, it.id)
		texts = append(texts, it.text)
	}
	key := strings.Join(ids, ",")
	msg := ""
	err := changeNudges(func(ns *nudgeStore) error {
		if key == "" {
			ns.Stalled = ""
			return nil
		}
		last := ns.Sent["followup"]
		changed := key != ns.Stalled
		if !(changed && at.Sub(last) >= personMin) && at.Sub(last) < personEvery {
			return nil
		}
		if ns.Sent == nil {
			ns.Sent = map[string]time.Time{}
		}
		ns.Sent["followup"], ns.Stalled = at, key
		msg = fmt.Sprintf("%d stalled: %s", len(items), strings.Join(texts, ", "))
		return nil
	})
	if msg != "" {
		tellPerson(notify.FollowUp, msg)
	}
	return err
}

// lastFollowupNotice is when the person was last sent a "followup"
// notification for a stall.
func lastFollowupNotice() time.Time { return loadNudges().Sent["followup"] }
