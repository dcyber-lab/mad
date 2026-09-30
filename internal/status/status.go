// Package status works out what each agent is doing: it records what
// agents report through `mad hook` and combines that with how the agent's
// screen changes.
package status

import (
	"encoding/json"
	"hash/fnv"
	"os"
	"path/filepath"
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
)

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

func RemoveHook(id string) { os.Remove(HookPath(id)) }

// Tracker follows one agent across polls.
type Tracker struct {
	hash       uint64
	seen       bool
	lastChange time.Time
	quiet      time.Time

	Status string
	// Attention: the agent finished or got blocked while not on stage.
	Attention bool
}

// Observe derives the agent's status from its pane, screen text and last
// hook report, and updates the attention flag.
func (t *Tracker) Observe(k agent.Kind, pane tmux.Pane, hasPane bool, hook *Hook, screen string, onStage bool, now time.Time) string {
	s := t.compute(k, pane, hasPane, hook, screen, now)
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

func (t *Tracker) compute(k agent.Kind, pane tmux.Pane, hasPane bool, hook *Hook, screen string, now time.Time) string {
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
			return Waiting
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
