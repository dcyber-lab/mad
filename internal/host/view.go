package host

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/live"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
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
	redraw := paced(ctx, h.redraw, drawEvery)
	for {
		select {
		case <-ctx.Done():
			return
		case <-redraw:
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

// drawEvery is how often the pane is drawn at most: a stream asks for a
// redraw with each piece, a few bytes, and a draw is the whole pane.
const drawEvery = 100 * time.Millisecond

// paced passes on what comes from in, once per every at most. What comes
// sooner waits its turn, so the last is always passed on.
func paced(ctx context.Context, in <-chan struct{}, every time.Duration) <-chan struct{} {
	out := make(chan struct{}, 1)
	go func() {
		var last time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-in:
			}
			if wait := every - time.Since(last); wait > 0 {
				t := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					t.Stop()
					return
				case <-t.C:
				}
			}
			last = time.Now()
			select {
			case out <- struct{}{}:
			default: // one is waiting already
			}
		}
	}()
	return out
}

// render is the pane's screen: who, what it is doing, the end of what it
// wrote, what it asks, and the key.
func render(s State, who, note string, w, ht int) string {
	const pad = "  "
	width := max(w-2*len(pad), 10)
	dim, bold, warn, reset := "\x1b[2m", "\x1b[1m", "\x1b[33m", "\x1b[0m"

	look := status.LookOf(s.Status)
	what := strings.ToLower(look.Word)
	if look.Tone == status.ToneNeeds {
		what = warn + what + reset
	}
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
	room := max(ht-len(head)-len(foot), 0)
	var body []string
	if s.Message != nil {
		for _, l := range messageEnd(s.Message.Blocks, width, room) {
			body = append(body, pad+l)
		}
	}
	lines := append(append(head, body...), make([]string, room-len(body))...)
	lines = append(lines, foot...)
	return "\x1b[H\x1b[2J" + strings.Join(lines, "\r\n")
}

// messageEnd is the end of what the text blocks say, wrapped to width:
// its last n lines. Only the end is wrapped, from the last line up: a
// long message is drawn again for each piece of it.
func messageEnd(blocks []live.Block, width, n int) []string {
	if n <= 0 {
		return nil
	}
	text := lastLines(blocks, n)
	var out []string
	for i := len(text) - 1; i >= 0 && len(out) < n; i-- {
		out = append(strings.Split(ansi.Wrap(text[i], width, ""), "\n"), out...)
	}
	return out[max(len(out)-n, 0):]
}

// lastLines is the end of what the text blocks say, n lines of it at most
// (as written, before they are wrapped): the blocks a line apart, with
// the space around them all trimmed.
func lastLines(blocks []live.Block, n int) []string {
	var texts []string
	for _, b := range blocks {
		if b.Kind == "text" {
			texts = append(texts, b.Text)
		}
	}
	for len(texts) > 0 && strings.TrimSpace(texts[len(texts)-1]) == "" {
		texts = texts[:len(texts)-1]
	}
	for len(texts) > 0 && strings.TrimSpace(texts[0]) == "" {
		texts = texts[1:]
	}
	if len(texts) == 0 {
		return []string{""}
	}
	texts[0] = strings.TrimLeftFunc(texts[0], unicode.IsSpace)
	texts[len(texts)-1] = strings.TrimRightFunc(texts[len(texts)-1], unicode.IsSpace)
	var out []string
	for i := len(texts) - 1; i >= 0 && len(out) < n; i-- {
		for t := texts[i]; len(out) < n; {
			j := strings.LastIndexByte(t, '\n')
			out = append(out, t[j+1:])
			if j < 0 {
				break
			}
			t = t[:j]
		}
	}
	slices.Reverse(out)
	return out
}

func modelNote(m string) string {
	if m == "" {
		return ""
	}
	return "  ·  " + m
}
