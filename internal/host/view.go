package host

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// view draws the host in its pane, for the terminal deck: what the agent
// writes and asks, plainly, as the window has it all. t takes the agent
// back to its TUI there, once it is not busy.
func view(ctx context.Context, h *Host, a *state.Agent, kinds []agent.Kind) {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) {
		return
	}
	if old, err := term.MakeRaw(in); err == nil {
		defer term.Restore(in, old)
	}
	fmt.Print("\x1b[?25l") // no cursor
	keys := make(chan byte, 16)
	go func() {
		buf := make([]byte, 16)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			for _, b := range buf[:n] {
				keys <- b
			}
		}
	}()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	icon := agent.ByName(kinds, a.Kind).Icon
	note := ""
	draw := func() {
		w, ht, err := term.GetSize(out)
		if err != nil {
			w, ht = 80, 24
		}
		fmt.Print(render(h.State(), icon+" "+a.Kind, note, w, ht))
	}
	draw()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.redraw:
			draw()
		case <-winch:
			draw()
		case k := <-keys:
			if k != 't' && k != 'T' {
				continue
			}
			if s := h.State().Status; s == Running || s == Waiting {
				note = "It is busy: let it finish, or stop it in the window, then press t."
				draw()
				continue
			}
			note = "Going back to its TUI…"
			draw()
			if err := deck.SetMode(a.ID, "", kinds); err != nil {
				note = err.Error()
				draw()
			}
		}
	}
}

// render is the pane's screen: who, what it is doing, the end of what it
// wrote, what it asks, and the key.
func render(s State, who, note string, w, ht int) string {
	const pad = "  "
	width := max(w-2*len(pad), 10)
	dim, bold, warn, reset := "\x1b[2m", "\x1b[1m", "\x1b[33m", "\x1b[0m"

	what := map[string]string{
		Starting: "starting", Idle: "idle", Running: "working", Waiting: warn + "needs you" + reset, Exited: "ended",
	}[s.Status]
	head := []string{
		pad + bold + who + reset + dim + "  ·  in the mad window" + reset,
		pad + what + dim + modelNote(s.Model) + reset,
		"",
	}
	var foot []string
	for _, r := range s.Requests {
		ask := r.Title
		if ask == "" {
			switch r.Kind {
			case "question":
				ask = "It has a question"
			case "plan":
				ask = "It has a plan to approve"
			default:
				ask = strings.TrimSpace("May it use " + r.Tool + "? " + r.Label)
			}
		}
		foot = append(foot, pad+warn+textutil.Truncate(ask, width)+reset)
	}
	if len(s.Requests) > 0 {
		foot = append(foot, pad+dim+"Answer in the mad window."+reset)
	}
	if s.Error != "" {
		foot = append(foot, pad+warn+textutil.Truncate(s.Error, width)+reset)
	}
	if note != "" {
		foot = append(foot, pad+note)
	}
	foot = append(foot, "", pad+dim+"t  back to its TUI here"+reset)

	// The end of the message, as many lines as fit between.
	room := ht - len(head) - len(foot)
	var body []string
	if s.Message != nil {
		var text strings.Builder
		for _, b := range s.Message.Blocks {
			if b.Kind == "text" {
				text.WriteString(b.Text + "\n")
			}
		}
		for _, l := range strings.Split(strings.TrimSpace(text.String()), "\n") {
			for _, part := range strings.Split(ansi.Wrap(l, width, ""), "\n") {
				body = append(body, pad+part)
			}
		}
	}
	if room < 0 {
		room = 0
	}
	if len(body) > room {
		body = body[len(body)-room:]
	}
	lines := append(append(head, body...), make([]string, max(room-len(body), 0))...)
	lines = append(lines, foot...)
	return "\x1b[H\x1b[2J" + strings.Join(lines, "\r\n")
}

func modelNote(m string) string {
	if m == "" {
		return ""
	}
	return "  ·  " + m
}
