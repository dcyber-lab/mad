package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/buschat"
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
  mad bus chat CHANNEL                read and write in a channel (or @agent for a DM), live
  mad bus dm AGENT [TEXT...|-]        a private message that wakes the agent
  mad bus resume CHANNEL              let a paused channel go on
  mad bus lead CHANNEL AGENT          make AGENT the channel's lead (who hears when work stalls)
  mad bus report [CHANNEL]            post the board's progress report as a line from mad
                                      (in CHANNEL, or in each channel with open work)
  mad bus handoffs                    the work agents gave each other, as chains
  mad bus budget [+N|open]            today's budget for what mad starts by itself;
                                      +N adds N dollars for today, open lifts it

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
	switch sub {
	case "ls":
		return busList(stdio.Out)
	case "handoffs":
		return busHandoffs(stdio.Out)
	case "budget":
		return busBudget(args, stdio)
	case "report":
		if len(args) > 1 {
			fmt.Fprint(stdio.Err, busUsage)
			return errUsage
		}
		ch := ""
		if len(args) == 1 {
			ch = strings.TrimPrefix(args[0], "#")
		}
		return busReport(ch, stdio)
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
	case "chat":
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
		if len(args) > 0 {
			fmt.Fprint(stdio.Err, busUsage)
			return errUsage
		}
		return buschat.Run(target)
	case "resume":
		return bus.Resume(target)
	case "lead":
		if len(args) != 1 {
			fmt.Fprint(stdio.Err, busUsage)
			return errUsage
		}
		_, a, err := find(args[0])
		if err != nil {
			return err
		}
		return bus.SetLead(target, a.ID)
	}
	fmt.Fprint(stdio.Err, busUsage)
	return errUsage
}

// busBudget shows the day's budget for what mad starts by itself, and with
// +N or open (the person's, never an agent's) raises or lifts it.
func busBudget(args []string, stdio IO) error {
	at := time.Now()
	if len(args) > 1 {
		fmt.Fprint(stdio.Err, busUsage)
		return errUsage
	}
	if len(args) == 1 {
		if os.Getenv("MAD_AGENT_ID") != "" {
			return errors.New("the budget is the person's to change, not an agent's: ask them with @you")
		}
		if args[0] == "open" {
			if err := bus.SpendOpenToday(at); err != nil {
				return err
			}
		} else {
			usd, err := strconv.ParseFloat(strings.TrimPrefix(args[0], "+"), 64)
			if err != nil || usd <= 0 || !strings.HasPrefix(args[0], "+") {
				fmt.Fprint(stdio.Err, busUsage)
				return errUsage
			}
			if err := bus.SpendRaise(usd, at); err != nil {
				return err
			}
		}
	}
	s := bus.SpendNow(at)
	how := "open"
	switch {
	case s.Closed:
		how = "spent: agents wait for you (mad bus budget +5, or open)"
	case s.Opened:
		how = "lifted for today"
	}
	fmt.Fprintf(stdio.Out, "%s: mad's own work today %s; %s\n", s.Day, s.Line(), how)
	return nil
}

// busReport posts the board's report as a line from mad, which wakes no
// one: the channel's part in channel, else each channel's part in its own
// channel. The whole report is printed.
func busReport(channel string, stdio IO) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	statuses, at := drive.Statuses(st, kinds()), time.Now()
	if channel != "" {
		line := bus.ReportChannel(st, statuses, at, channel)
		if line == "" {
			fmt.Fprintf(stdio.Out, "Nothing open in #%s.\n", channel)
			return nil
		}
		fmt.Fprintln(stdio.Out, line)
		return bus.PostSystem(channel, line)
	}
	all := bus.Report(st, statuses, at)
	if all == "" {
		fmt.Fprintln(stdio.Out, "Nothing open.")
		return nil
	}
	fmt.Fprintln(stdio.Out, all)
	lines := bus.ReportLines(st, statuses, at)
	var chans []string
	for ch := range lines {
		chans = append(chans, ch)
	}
	sort.Strings(chans)
	for _, ch := range chans {
		if err := bus.PostSystem(ch, lines[ch]); err != nil {
			return err
		}
	}
	return nil
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

// busHandoffs prints the handoffs as a tree by parent, newest chains last:
// "h-xxxx from → to [status] task".
func busHandoffs(out io.Writer) error {
	hs, err := bus.Handoffs()
	if err != nil {
		return err
	}
	st, _ := state.Load()
	name := func(id string) string {
		if _, a := st.FindAgent(id); a != nil {
			return drive.Label(a)
		}
		if id == bus.You {
			return id
		}
		return drive.ShortID(id)
	}
	list, depth := bus.Tree(hs)
	for i, h := range list {
		task, _, _ := strings.Cut(strings.TrimSpace(h.Task), "\n")
		fmt.Fprintf(out, "%s%s %s → %s [%s] %s\n", strings.Repeat("  ", depth[i]), h.ID, name(h.From), name(h.To), h.Status, task)
	}
	return nil
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
		members := c.Members
		if len(members) == 0 { // a DM whose agent was forgotten
			members = bus.DMSides(c.ID)
		}
		for _, id := range members {
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
