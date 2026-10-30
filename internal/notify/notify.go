// Package notify tells the user that an agent finished or needs them: a
// desktop notification, or a command of their own from config.json.
package notify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/push"
	"github.com/dcyber-lab/mad/internal/status"
)

// Event kinds.
const (
	Done    = "done"    // was running, now idle
	Waiting = "waiting" // needs input: permission prompt, question
	Bus     = "bus"     // a message on the bus for the person (package bus)
	// FollowUp: work on the board has stalled, or an agent has waited for
	// the person's permission a long while (package bus).
	FollowUp = "followup"
	// Report: the board's progress report, daily and when something new
	// waits for the person (package bus).
	Report = "report"
	// Budget: mad's own work for the day reached its bound and agents wait
	// for the person (package bus).
	Budget = "budget"
)

type Event struct {
	Kind    string
	Project string
	Agent   string // display name, e.g. claude#2
	AgentID string // the agent's id, for the phone to open it
	Message string // the agent's own words, when its hook gave any
	Channel string // a bus message's channel, for the phone to open it
}

func (e Event) Title() string { return "mad · " + e.Project }

func (e Event) Body() string {
	switch {
	case e.Kind == Bus:
		return e.Agent + ": " + shorten(e.Message, 200)
	case e.Kind == FollowUp || e.Kind == Report || e.Kind == Budget:
		return shorten(e.Message, 300)
	case e.Kind == Waiting && e.Message != "":
		return e.Agent + ": " + e.Message
	case e.Kind == Waiting:
		return e.Agent + " " + word(status.Waiting)
	case e.Kind == Done && e.Message != "":
		return e.Agent + ": " + e.Message
	default:
		return e.Agent + " " + word(status.Finished)
	}
}

// word is a state's word as the window and the deck show it, in running
// text.
func word(state string) string { return strings.ToLower(status.LookOf(state).Word) }

// Config is the "notify" object of ~/.config/mad/config.json:
//
//	{"notify": {"on": ["done", "waiting"], "command": "..."}}
//
// "on" lists the events to notify about ([] turns notifications off).
// "command", when set, runs through sh instead of the desktop
// notification, with MAD_EVENT, MAD_PROJECT, MAD_AGENT, MAD_TITLE and
// MAD_MESSAGE in its environment.
type Config struct {
	On      []string `json:"on"`
	Command string   `json:"command,omitempty"`
}

// Default notifies about every event with the desktop notification.
func Default() Config { return Config{On: []string{Done, Waiting, Bus, FollowUp, Report, Budget}} }

// shorten cuts s to n runes at most, on one line.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// FromFile completes the "notify" object as config.json gave it: without
// one it is Default, without an "on" list it notifies about both events.
func FromFile(c *Config) Config {
	if c == nil {
		return Default()
	}
	out := *c
	if out.On == nil {
		out.On = Default().On
	}
	return out
}

func (c Config) Wants(kind string) bool {
	for _, k := range c.On {
		if k == kind {
			return true
		}
	}
	return false
}

const timeout = 10 * time.Second

// Tag names what a newer notification replaces: the agent's (or the bus
// channel's) older one, whatever its kind, so a "needs you" that was
// answered gives way to the "finished" that follows.
func (e Event) Tag() string {
	if e.AgentID == "" && e.Channel == "" {
		return e.Project
	}
	return e.AgentID + "/" + e.Channel
}

// Send delivers e: the configured command, else the platform's desktop
// notification (terminal-notifier or else osascript on macOS, notify-send
// on Linux; nothing where neither exists); and to the phones subscribed,
// if any (package push). Replaceable in tests.
var Send = func(c Config, e Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	pushed := make(chan error, 1)
	go func() {
		pushed <- push.Send(ctx, push.Message{Title: e.Title(), Body: e.Body(), Agent: e.AgentID, Channel: e.Channel, Tag: e.Tag()}, "")
	}()
	var err error
	if cmd := command(ctx, c, e, runtime.GOOS, exec.LookPath); cmd != nil {
		err = cmd.Run()
	}
	return errors.Join(err, <-pushed)
}

// command builds the command for e, or nil when there is no way to notify.
// Text travels as arguments or environment, never inside a shell string.
func command(ctx context.Context, c Config, e Event, goos string, lookPath func(string) (string, error)) *exec.Cmd {
	if c.Command != "" {
		cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
		cmd.Env = append(os.Environ(),
			"MAD_EVENT="+e.Kind, "MAD_PROJECT="+e.Project, "MAD_AGENT="+e.Agent,
			"MAD_TITLE="+e.Title(), "MAD_MESSAGE="+e.Body())
		return cmd
	}
	switch goos {
	case "darwin":
		// osascript's notifications belong to Script Editor, so clicking one
		// opens it. terminal-notifier's bring back the terminal instead.
		if p, err := lookPath("terminal-notifier"); err == nil {
			args := []string{"-title", e.Title(), "-message", e.Body(), "-sound", "Glass", "-group", "mad." + e.Tag()}
			if id := os.Getenv("__CFBundleIdentifier"); id != "" {
				args = append(args, "-activate", id)
			}
			return exec.CommandContext(ctx, p, args...)
		}
		return exec.CommandContext(ctx, "osascript",
			"-e", "on run argv",
			"-e", `display notification (item 2 of argv) with title (item 1 of argv) sound name "Glass"`,
			"-e", "end run",
			e.Title(), e.Body())
	case "linux":
		if p, err := lookPath("notify-send"); err == nil {
			return exec.CommandContext(ctx, p, "--app-name=mad", e.Title(), e.Body())
		}
	}
	return nil
}
