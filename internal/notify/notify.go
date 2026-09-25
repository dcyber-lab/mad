// Package notify tells the user that an agent finished or needs them: a
// desktop notification, or a command of their own from config.json.
package notify

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Event kinds.
const (
	Done    = "done"    // was running, now idle
	Waiting = "waiting" // needs input: permission prompt, question
)

type Event struct {
	Kind    string
	Project string
	Agent   string // display name, e.g. claude#2
	Message string // the agent's own words, when its hook gave any
}

func (e Event) Title() string { return "mad · " + e.Project }

func (e Event) Body() string {
	switch {
	case e.Kind == Waiting && e.Message != "":
		return e.Agent + ": " + e.Message
	case e.Kind == Waiting:
		return e.Agent + " needs you"
	default:
		return e.Agent + " finished"
	}
}

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

// Default notifies about both events with the desktop notification.
func Default() Config { return Config{On: []string{Done, Waiting}} }

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

// Send delivers e: the configured command, else the platform's desktop
// notification (osascript on macOS, notify-send on Linux; nothing where
// neither exists). Replaceable in tests.
var Send = func(c Config, e Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if cmd := command(ctx, c, e, runtime.GOOS, exec.LookPath); cmd != nil {
		return cmd.Run()
	}
	return nil
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
