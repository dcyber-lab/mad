package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

const queueUsage = `usage:
  mad queue add AGENT [TEXT...|-]   line a message up for when the agent's
                                    turn is over (- or nothing: stdin)
  mad queue ls [AGENT]              what waits, for one agent or all
  mad queue rm AGENT [ITEM]         drop one message, or all of them
  mad queue pause|resume AGENT      hold the agent's messages, or let them go
`

func queueCmd(args []string, stdio IO) error {
	if len(args) == 0 {
		fmt.Fprint(stdio.Err, queueUsage)
		return errUsage
	}
	sub, args := args[0], args[1:]
	agentArg := func(min int) (*state.State, *state.Agent, error) {
		if len(args) < min {
			fmt.Fprint(stdio.Err, queueUsage)
			return nil, nil, errUsage
		}
		return find(args[0])
	}
	switch sub {
	case "add":
		_, a, err := agentArg(1)
		if err != nil {
			return err
		}
		var text string
		if rest := args[1:]; len(rest) == 0 || len(rest) == 1 && rest[0] == "-" {
			data, err := io.ReadAll(stdio.In)
			if err != nil {
				return err
			}
			text = string(data)
		} else {
			text = strings.Join(rest, " ")
		}
		it, err := queue.Push(a.ID, text, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintln(stdio.Out, it.ID)
		if !tmux.HasSession(tmux.MainSession) {
			fmt.Fprintln(stdio.Err, "mad: the deck is not running; the message goes in once it is (mad)")
		}
		return nil
	case "ls":
		var only string
		if len(args) > 0 {
			_, a, err := find(args[0])
			if err != nil {
				return err
			}
			only = a.ID
		}
		return queueList(stdio.Out, only)
	case "rm":
		_, a, err := agentArg(1)
		if err != nil {
			return err
		}
		if len(args) > 1 {
			return queue.Remove(a.ID, args[1])
		}
		return queue.Clear(a.ID)
	case "pause", "resume":
		_, a, err := agentArg(1)
		if err != nil {
			return err
		}
		return queue.SetPaused(a.ID, sub == "pause")
	}
	fmt.Fprint(stdio.Err, queueUsage)
	return errUsage
}

func queueList(out io.Writer, only string) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	all, err := queue.All()
	if err != nil {
		return err
	}
	for _, a := range st.OrderedAgents() {
		q, ok := all[a.ID]
		if !ok || only != "" && a.ID != only {
			continue
		}
		head := drive.Label(a)
		if q.Paused {
			head += " (paused)"
		}
		fmt.Fprintln(out, head)
		if q.Error != "" {
			fmt.Fprintf(out, "  last try: %s\n", q.Error)
		}
		for _, it := range q.Items {
			line, _, _ := strings.Cut(it.Text, "\n")
			fmt.Fprintf(out, "  %s  %s\n", it.ID, textutil.Truncate(line, 70))
		}
	}
	return nil
}
