package main

import (
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Agent status as shown in the sidebar.
const (
	statusRunning = "running"
	statusWaiting = "waiting" // needs the user: permission prompt, question
	statusIdle    = "idle"
	statusExited  = "exited"  // process ended, pane kept (remain-on-exit)
	statusStopped = "stopped" // no pane, e.g. after a reboot
)

// HookStatus is what an agent last reported through `mad hook`, stored in
// statusDir()/<agent id>.json.
type HookStatus struct {
	State     string    `json:"state"`
	Event     string    `json:"event"`
	SessionID string    `json:"session_id,omitempty"`
	Message   string    `json:"message,omitempty"`
	At        time.Time `json:"at"`
}

func hookStatusPath(id string) string { return filepath.Join(statusDir(), id+".json") }

func readHookStatus(id string) *HookStatus {
	data, err := os.ReadFile(hookStatusPath(id))
	if err != nil {
		return nil
	}
	var hs HookStatus
	if json.Unmarshal(data, &hs) != nil {
		return nil
	}
	return &hs
}

// cmdHook never fails: a broken hook must not disturb the agent.
func cmdHook(args []string) {
	id := os.Getenv("MAD_AGENT_ID")
	if id == "" || len(args) == 0 {
		return
	}
	var hs *HookStatus
	switch args[0] {
	case "claude":
		hs = claudeHook(os.Stdin)
	case "codex":
		if len(args) > 1 {
			hs = codexHook(args[len(args)-1])
		}
	}
	if hs == nil {
		return
	}
	if hs.SessionID == "" {
		if prev := readHookStatus(id); prev != nil {
			hs.SessionID = prev.SessionID
		}
	}
	hs.At = time.Now()
	data, _ := json.Marshal(hs)
	_ = writeFileAtomic(hookStatusPath(id), data)
}

func claudeHook(r io.Reader) *HookStatus {
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
	hs := &HookStatus{Event: ev.Event, SessionID: ev.SessionID}
	switch ev.Event {
	case "SessionStart", "Stop":
		hs.State = statusIdle
	case "UserPromptSubmit", "PostToolUse":
		hs.State = statusRunning
	case "PreToolUse":
		hs.State = statusRunning
		if ev.ToolName == "AskUserQuestion" || ev.ToolName == "ExitPlanMode" {
			hs.State = statusWaiting
		}
	case "Notification":
		msg := strings.ToLower(ev.Message)
		switch {
		case ev.NotificationType == "permission_prompt", strings.Contains(msg, "permission"):
			hs.State, hs.Message = statusWaiting, ev.Message
		case ev.NotificationType == "idle_prompt", strings.Contains(msg, "waiting for your input"):
			hs.State = statusIdle
		default:
			return nil
		}
	default:
		return nil
	}
	return hs
}

// codexHook handles codex's `notify` program call, whose last argument is
// a JSON payload.
func codexHook(payload string) *HookStatus {
	var ev struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread-id"`
	}
	if json.Unmarshal([]byte(payload), &ev) != nil || ev.Type != "agent-turn-complete" {
		return nil
	}
	return &HookStatus{State: statusIdle, Event: ev.Type, SessionID: ev.ThreadID}
}

const placeholderText = `

   mad — multi-agent deck

   sidebar keys
     ↑/↓ j/k   move            enter   open agent
     n         new agent       a       add project
     r         restart/resume  x       kill agent / remove project
     1-9       open agent N    q       detach (agents keep running)
     < / >     narrower / wider sidebar (or drag the border)

   anywhere  (Option must act as Alt in Ghostty)
     Alt-s     sidebar ⇄ agent
     Alt-j/k   next / prev agent
     Alt-1..9  open agent N
     Ctrl-] then s / n / p / 1-9 / d   same, without Alt
`

// runPlaceholder fills the stage when no agent is shown.
func runPlaceholder() {
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	os.Stdout.WriteString("\x1b[2J\x1b[H\x1b[?25l\x1b[38;5;245m" + placeholderText + "\x1b[0m")
	for {
		time.Sleep(time.Hour)
	}
}
