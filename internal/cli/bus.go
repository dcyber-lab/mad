package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
)

const busUsage = `usage:
  mad bus ls                          the channels: members, last message, paused
  mad bus create NAME [-t TOPIC] [AGENT...]
                                      make a channel
  mad bus post CHANNEL [TEXT...|-]    say something; @agent wakes it, @all everyone
  mad bus read CHANNEL [-n N]         the last N messages (default 30)
  mad bus log CHANNEL                 all of them
  mad bus dm AGENT [TEXT...|-]        a private message that wakes the agent
  mad bus resume CHANNEL              let a paused channel go on

Inside an agent (MAD_AGENT_ID set) these speak as that agent, else as you.
`

// busAuthor is who `mad bus` speaks as: the agent whose shell it runs in,
// else you.
func busAuthor() (bus.From, error) {
	id := os.Getenv("MAD_AGENT_ID")
	if id == "" {
		return bus.From{ID: bus.You, Label: bus.You}, nil
	}
	st, err := state.Load()
	if err != nil {
		return bus.From{}, err
	}
	_, a := st.FindAgent(id)
	if a == nil {
		return bus.From{}, fmt.Errorf("MAD_AGENT_ID %s is no agent", id)
	}
	return bus.From{ID: a.ID, Label: drive.Label(a), Kind: a.Kind}, nil
}

// busText is the words after a command's arguments, or stdin.
func busText(words []string, stdio IO) (string, error) {
	if len(words) == 0 || len(words) == 1 && words[0] == "-" {
		data, err := io.ReadAll(stdio.In)
		return string(data), err
	}
	return strings.Join(words, " "), nil
}

func busCmd(args []string, stdio IO) error {
	if len(args) == 0 {
		fmt.Fprint(stdio.Err, busUsage)
		return errUsage
	}
	sub, args := args[0], args[1:]
	if sub == "ls" {
		return busList(stdio.Out)
	}
	if len(args) == 0 {
		fmt.Fprint(stdio.Err, busUsage)
		return errUsage
	}
	target, args := strings.TrimPrefix(args[0], "#"), args[1:]
	switch sub {
	case "create":
		return busCreate(target, args, stdio)
	case "post":
		text, err := busText(args, stdio)
		if err != nil {
			return err
		}
		from, err := busAuthor()
		if err != nil {
			return err
		}
		res, err := bus.Post(target, from, text, 0)
		if err != nil {
			return err
		}
		busPosted(stdio, target, res)
		return nil
	case "dm":
		_, a, err := find(target)
		if err != nil {
			return err
		}
		text, err := busText(args, stdio)
		if err != nil {
			return err
		}
		from, err := busAuthor()
		if err != nil {
			return err
		}
		ch, res, err := bus.DM(from, a.ID, text)
		if err != nil {
			return err
		}
		busPosted(stdio, ch, res)
		return nil
	case "read", "log":
		// A DM: @agent (with you), or @agent @agent.
		if strings.HasPrefix(target, "@") {
			var refs []string
			for len(args) > 0 && strings.HasPrefix(args[0], "@") {
				refs, args = append(refs, args[0]), args[1:]
			}
			ch, err := busDM(target, refs)
			if err != nil {
				return err
			}
			target = ch
		}
		n := 0
		if sub == "read" {
			for i := 0; i < len(args); i++ {
				if args[i] != "-n" || i+1 >= len(args) {
					fmt.Fprint(stdio.Err, busUsage)
					return errUsage
				}
				v, err := strconv.Atoi(args[i+1])
				if err != nil || v < 1 {
					return fmt.Errorf("-n wants a number, not %q", args[i+1])
				}
				n = v
				i++
			}
		}
		return busRead(target, n, sub == "log", stdio)
	case "resume":
		return bus.Resume(target)
	}
	fmt.Fprint(stdio.Err, busUsage)
	return errUsage
}

func busCreate(name string, args []string, stdio IO) error {
	var topic string
	var members []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-t" {
			if i+1 >= len(args) {
				fmt.Fprint(stdio.Err, busUsage)
				return errUsage
			}
			topic = args[i+1]
			i++
			continue
		}
		_, a, err := find(args[i])
		if err != nil {
			return err
		}
		members = append(members, a.ID)
	}
	from, err := busAuthor()
	if err != nil {
		return err
	}
	c, err := bus.Create(name, topic, members, from)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdio.Out, c.ID)
	return nil
}

func busPosted(stdio IO, channel string, res bus.PostResult) {
	line := fmt.Sprintf("#%d in %s", res.Seq, channel)
	if len(res.Woke) > 0 {
		line += "; woke " + strings.Join(res.Woke, ", ")
	}
	fmt.Fprintln(stdio.Out, line)
	if res.Warning != "" {
		fmt.Fprintln(stdio.Err, "mad: "+res.Warning)
	}
}

// busRead prints a channel's messages: the last n (30 by default), or all.
func busRead(channel string, n int, all bool, stdio IO) error {
	st, _ := state.Load()
	var msgs []bus.Message
	var last int
	var err error
	if all {
		for since := 0; ; {
			var part []bus.Message
			if part, last, err = bus.Read(channel, since, 200); err != nil {
				return err
			}
			if len(part) == 0 {
				break
			}
			msgs = append(msgs, part...)
			since = part[len(part)-1].Seq
			if since >= last {
				break
			}
		}
	} else if msgs, last, err = bus.Read(channel, 0, n); err != nil {
		return err
	}
	for _, m := range msgs {
		reply := ""
		if m.ReplyTo > 0 {
			reply = fmt.Sprintf(" (reply to #%d)", m.ReplyTo)
		}
		fmt.Fprintf(stdio.Out, "#%d %s %s%s: %s\n", m.Seq, m.At.Local().Format("15:04"), m.From.Label, reply, m.Text)
	}
	if id := os.Getenv("MAD_AGENT_ID"); id != "" && st != nil {
		_ = bus.MarkRead(id, channel, last)
	}
	return nil
}

// busDM is the DM channel ref names (@agent) and others name: with you
// when it is one agent.
func busDM(ref string, others []string) (string, error) {
	var ids []string
	for _, r := range append([]string{ref}, others...) {
		r = strings.TrimPrefix(r, "@")
		if r == bus.You {
			ids = append(ids, bus.You)
			continue
		}
		_, a, err := find(r)
		if err != nil {
			return "", err
		}
		ids = append(ids, a.ID)
	}
	switch len(ids) {
	case 1:
		return bus.DMChannel(ids[0], bus.You), nil
	case 2:
		return bus.DMChannel(ids[0], ids[1]), nil
	}
	return "", errors.New("a DM is between two: @agent, or @agent @agent")
}

func busList(out io.Writer) error {
	chans, err := bus.Channels()
	if err != nil {
		return err
	}
	st, _ := state.Load()
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CHANNEL\tMEMBERS\tLAST\tSTATE")
	for _, c := range chans {
		var names []string
		for _, id := range c.Members {
			if _, a := st.FindAgent(id); a != nil {
				names = append(names, drive.Label(a))
			} else {
				names = append(names, drive.ShortID(id))
			}
		}
		_, last, _ := bus.Read(c.ID, 0, 1)
		mark := ""
		if c.Paused {
			mark = "paused"
		}
		name := c.ID
		if strings.HasPrefix(name, "dm.") {
			// As read takes it: mad bus read @a @b.
			name = "@" + strings.Join(names, " @")
		}
		fmt.Fprintf(tw, "%s\t%s\t#%d\t%s\n", name, strings.Join(names, ", "), last, mark)
	}
	return tw.Flush()
}
