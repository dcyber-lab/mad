// Package status works out what each agent is doing: it records what
// agents report through `mad hook` and combines that with how the agent's
// screen changes.
package status

import (
	"encoding/json"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// Agent status as shown in the sidebar.
const (
	Running = "running"
	Waiting = "waiting" // needs the user: permission prompt, question
	Idle    = "idle"
	Exited  = "exited"  // process ended, pane kept (remain-on-exit)
	Asleep  = "asleep"  // process ended by mad to free what it held; resumes when opened
	Stopped = "stopped" // no pane, e.g. after a reboot
)

const (
	// ActiveWindow: a screen that changed this recently counts as running.
	ActiveWindow = 2 * time.Second
	// StaleRunning: a hook said running but the screen froze this long, so
	// the turn was interrupted (claude fires no Stop hook on Esc).
	StaleRunning = 10 * time.Second
	// waitingTail is how many bottom screen lines are scanned for prompts.
	waitingTail = 15
	// answerSettle: a wait's prompt off a screen that moved since it
	// asked, this long, is answered. A shorter gap is not: claude's
	// PreToolUse hook comes before its question is drawn, and a look may
	// catch a frame half redrawn.
	answerSettle = ActiveWindow
)

// promptMarks are what a TUI's prompt shows in its last lines, of any
// kind, and its screen at work does not: a footer that offers a way out,
// a choice under a cursor, claude's question. A prompt too long to read
// whole shows them still.
var promptMarks = regexp.MustCompile(`(?im)\besc to cancel\b|\benter to (?:select|confirm)\b|^\s*[❯›>]\s*[1-9]\.\s|\bdo you want to proceed\?`)

// ShowsPrompt tells whether the last lines of screen look like a TUI's
// prompt (promptMarks), whether or not it can be read.
func ShowsPrompt(screen string) bool {
	return promptMarks.MatchString(tailLines(screen, waitingTail))
}

// Hook is what an agent last reported, stored as StatusDir/<agent id>.json.
type Hook struct {
	State     string    `json:"state"`
	Event     string    `json:"event"`
	SessionID string    `json:"session_id,omitempty"`
	Message   string    `json:"message,omitempty"`
	At        time.Time `json:"at"`
}

func HookPath(id string) string { return filepath.Join(paths.StatusDir(), id+".json") }

// ReadHook returns the agent's last report, or nil.
func ReadHook(id string) *Hook {
	data, err := os.ReadFile(HookPath(id))
	if err != nil {
		return nil
	}
	var h Hook
	if json.Unmarshal(data, &h) != nil {
		return nil
	}
	return &h
}

// WriteHook stores h for agent id, keeping the previously known session id
// when h doesn't carry one.
func WriteHook(id string, h *Hook, now time.Time) error {
	if h.SessionID == "" {
		if prev := ReadHook(id); prev != nil {
			h.SessionID = prev.SessionID
		}
	}
	h.At = now
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(HookPath(id), data)
}

// RemoveHook forgets everything the agent reported and was sent.
func RemoveHook(id string) {
	os.Remove(HookPath(id))
	os.Remove(turnPath(id))
	os.Remove(sentPath(id))
	os.Remove(digestPath(id))
	os.Remove(asksPath(id))
	os.Remove(sincePath(id))
}

// Turn is how the agent's last turn ended, stored as StatusDir/<agent
// id>.turn.json apart from the hook report, which the next event
// overwrites.
type Turn struct {
	// Reply is the agent's last message of the turn, as the agent itself
	// reports it when the turn ends.
	Reply     string    `json:"reply"`
	SessionID string    `json:"session_id,omitempty"`
	At        time.Time `json:"at"`
}

func turnPath(id string) string { return filepath.Join(paths.StatusDir(), id+".turn.json") }

// WriteTurn records the end of agent id's turn.
func WriteTurn(id string, t *Turn, now time.Time) error {
	t.At = now
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(turnPath(id), data)
}

// ReadTurn returns how agent id's last turn ended, or nil before its
// first one did.
func ReadTurn(id string) *Turn {
	data, err := os.ReadFile(turnPath(id))
	if err != nil {
		return nil
	}
	var t Turn
	if json.Unmarshal(data, &t) != nil {
		return nil
	}
	return &t
}

func sentPath(id string) string   { return filepath.Join(paths.StatusDir(), id+".sent") }
func digestPath(id string) string { return filepath.Join(paths.StatusDir(), id+".sent-digest") }

// MarkSent notes that mad typed a message into agent id at now, so a
// turn that ended before then is not its answer. digest stands for the
// message, to tell later whether it was the one that went in; it is kept
// apart from the time, which a mad from before reads alone.
func MarkSent(id string, now time.Time, digest string) error {
	if err := paths.WriteFileAtomic(digestPath(id), []byte(digest)); err != nil {
		return err
	}
	return paths.WriteFileAtomic(sentPath(id), []byte(now.Format(time.RFC3339Nano)))
}

// SentDigest is the digest of the message mad last typed into agent id;
// "" if none is known.
func SentDigest(id string) string {
	data, err := os.ReadFile(digestPath(id))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SentAt is when mad last typed a message into agent id; zero if never.
func SentAt(id string) time.Time {
	data, err := os.ReadFile(sentPath(id))
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	return t
}

// Tracker follows one agent across polls.
type Tracker struct {
	// ID is the agent's id: with it, Since is kept in a record every
	// process tracking the agent shares (see sinceRecord). Empty: none.
	ID string

	hash       uint64
	seen       bool
	lastChange time.Time
	quiet      time.Time
	since      time.Time
	// wait is the waiting hook's time last seen, clear since when its
	// prompt has been off the screen (zero while it shows), clearing that
	// it is off for less than answerSettle.
	wait, clear time.Time
	clearing    bool

	Status string
	// Attention: the agent finished or got blocked while not on stage.
	Attention bool
}

// Observe derives the agent's status from its pane, screen text and last
// hook report, and updates the attention flag.
func (t *Tracker) Observe(k agent.Kind, pane tmux.Pane, hasPane bool, hook *Hook, screen string, onStage bool, now time.Time) string {
	s := t.compute(k, pane, hasPane, hook, screen, now)
	if s != t.Status {
		t.since = t.began(s, hook, now)
	}
	switch {
	case onStage:
		t.Attention = false
	case t.Status == Running && (s == Idle || s == Waiting):
		t.Attention = true
	}
	t.Status = s
	if t.quiet.IsZero() || onStage || s == Running || s == Waiting {
		t.quiet = now
	}
	if t.lastChange.After(t.quiet) {
		t.quiet = t.lastChange
	}
	if hook != nil && hook.At.After(t.quiet) {
		t.quiet = hook.At
	}
	return s
}

// sinceOf is when status s began, s being new at now: when the hook says
// so, its time, which every process polling the same file reads alike;
// else now. A running hook is rewritten by every tool call and the host's
// heartbeat, so its time says "recently", not "since".
func sinceOf(s string, prev time.Time, hook *Hook, now time.Time) time.Time {
	if hook != nil && s != Running && hook.State == s && hook.At.After(prev) && !hook.At.After(now) {
		return hook.At
	}
	return now
}

// began is when status s began, s being new to this tracker at now: when
// the hook says so, its time, which every process polling the same file
// reads alike; else the record of the state the first of them saw begin;
// else, if this tracker watched the state change, now. A tracker's first
// look (nothing seen before) cannot tell when the state began, so it
// guesses nothing: the time is zero (unknown), nothing is written (a
// read-only `mad desktop` leaves the deck's state alone) and no surface
// shows an age for it.
func (t *Tracker) began(s string, hook *Hook, now time.Time) time.Time {
	first := t.Status == ""
	at := sinceOf(s, t.since, hook, now)
	hooked := !at.Equal(now)
	if t.ID == "" {
		if first && !hooked {
			return time.Time{}
		}
		return at
	}
	rec, ok := readSince(t.ID)
	same := ok && rec.State == s
	if hooked {
		if !first && (!same || !rec.At.Equal(at)) {
			writeSince(t.ID, sinceRecord{State: s, At: at})
		}
		return at
	}
	if same && !rec.At.After(now) {
		return rec.At
	}
	if first {
		return time.Time{}
	}
	writeSince(t.ID, sinceRecord{State: s, At: now})
	return now
}

// Since is when the agent's tracked status began: its raw status, so one
// that finished at 10:00 and was looked at at 10:05 is idle since 10:00.
func (t *Tracker) Since() time.Time { return t.since }

// QuietSince is when the agent last did or showed anything: ran, waited,
// reported through a hook, changed its screen or was on stage. It is
// never earlier than the first observation, so a restarted sidebar starts
// counting afresh.
func (t *Tracker) QuietSince() time.Time { return t.quiet }

// Done: the agent finished while you were elsewhere and you haven't looked
// since, whether or not it has been put to sleep meanwhile.
func (t *Tracker) Done() bool {
	return t.Attention && (t.Status == Idle || t.Status == Asleep)
}

// Clearing: the agent waits by its hook, but its prompt has been off its
// screen for less than answerSettle: answered, or not drawn yet. Nothing
// it asks is on the screen to answer.
func (t *Tracker) Clearing() bool { return t.clearing }

func (t *Tracker) compute(k agent.Kind, pane tmux.Pane, hasPane bool, hook *Hook, screen string, now time.Time) string {
	t.clearing = false
	if !hasPane {
		return Stopped
	}
	if pane.Dead {
		if pane.Asleep {
			return Asleep
		}
		return Exited
	}
	h := fnv.New64a()
	h.Write([]byte(screen))
	sum := h.Sum64()
	if !t.seen {
		t.seen, t.hash = true, sum
	} else if sum != t.hash {
		t.hash, t.lastChange = sum, now
	}

	if k.Hooks && hook != nil {
		switch hook.State {
		case Waiting:
			// Answered in its pane, or by keys from the window: claude says
			// nothing until the tool it allowed is done (or ever, when it
			// was refused or interrupted), but its screen moves on, with no
			// prompt on it. Its prompt off a screen that moved since it
			// asked, for answerSettle, is answered: it runs while the screen
			// moves, as a running hook does. Nothing is kept of it: a prompt
			// on the screen again is waiting again.
			if !t.wait.Equal(hook.At) {
				t.wait, t.clear = hook.At, time.Time{}
			}
			if !t.lastChange.After(hook.At) || k.ScreenWaiting(tailLines(screen, waitingTail)) || ShowsPrompt(screen) {
				t.clear = time.Time{}
				return Waiting
			}
			if t.clear.IsZero() {
				t.clear = now
			}
			if now.Sub(t.clear) < answerSettle {
				t.clearing = true
				return Waiting
			}
			if now.Sub(t.lastChange) > StaleRunning {
				return Idle
			}
			return Running
		case Running:
			last := hook.At
			if t.lastChange.After(last) {
				last = t.lastChange
			}
			if now.Sub(last) > StaleRunning {
				return Idle
			}
			return Running
		default:
			return Idle
		}
	}
	if now.Sub(t.lastChange) < ActiveWindow {
		return Running
	}
	if k.ScreenWaiting(tailLines(screen, waitingTail)) {
		return Waiting
	}
	return Idle
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n "), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// sinceRecord is <id>.since.json: the state an agent's trackers last saw
// begin, and when. Whichever process sees a state begin first writes it;
// the others, finding their own state in it, take its time.
type sinceRecord struct {
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

func sincePath(id string) string { return filepath.Join(paths.StatusDir(), id+".since.json") }

func readSince(id string) (sinceRecord, bool) {
	var r sinceRecord
	data, err := os.ReadFile(sincePath(id))
	if err != nil || json.Unmarshal(data, &r) != nil || r.State == "" {
		return r, false
	}
	return r, true
}

func writeSince(id string, r sinceRecord) {
	if data, err := json.Marshal(r); err == nil {
		_ = paths.WriteFileAtomic(sincePath(id), data)
	}
}
