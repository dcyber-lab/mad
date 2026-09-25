// Package status works out what each agent is doing: it records what
// agents report through `mad hook` and combines that with how the agent's
// screen changes.
package status

import (
	"encoding/json"
	"hash/fnv"
	"io"
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

// ParseClaude maps a Claude Code hook event (JSON on stdin) to a status;
// nil means the event doesn't change it.
func ParseClaude(r io.Reader) *Hook {
	var ev struct {
		Event            string `json:"hook_event_name"`
		SessionID        string `json:"session_id"`
		ToolName         string `json:"tool_name"`
		Message          string `json:"message"`
		NotificationType string `json:"notification_type"`
	}
	data, _ := io.ReadAll(r)
	if json.Unmarshal(data, &ev) != nil {
		return nil
	}
	h := &Hook{Event: ev.Event, SessionID: ev.SessionID}
	switch ev.Event {
	case "SessionStart", "Stop":
		h.State = Idle
	case "UserPromptSubmit", "PostToolUse":
		h.State = Running
	case "PreToolUse":
		h.State = Running
		if ev.ToolName == "AskUserQuestion" || ev.ToolName == "ExitPlanMode" {
			h.State = Waiting
		}
	case "Notification":
		msg := strings.ToLower(ev.Message)
		switch {
		case ev.NotificationType == "permission_prompt", strings.Contains(msg, "permission"):
			h.State, h.Message = Waiting, ev.Message
		case ev.NotificationType == "idle_prompt", strings.Contains(msg, "waiting for your input"):
			h.State = Idle
		default:
			return nil
		}
	default:
		return nil
	}
	return h
}

// ParseCodex maps codex's `notify` payload (its last argv) to a status.
func ParseCodex(payload string) *Hook {
	var ev struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread-id"`
	}
	if json.Unmarshal([]byte(payload), &ev) != nil || ev.Type != "agent-turn-complete" {
		return nil
	}
	return &Hook{State: Idle, Event: ev.Type, SessionID: ev.ThreadID}
}

// Tracker follows one agent across polls.
type Tracker struct {
	hash       uint64
	seen       bool
	lastChange time.Time

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
	return s
}

func (t *Tracker) compute(k agent.Kind, pane tmux.Pane, hasPane bool, hook *Hook, screen string, now time.Time) string {
	if !hasPane {
		return Stopped
	}
	if pane.Dead {
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
