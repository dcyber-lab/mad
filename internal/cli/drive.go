package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// errUsage is reported with the command's usage, as exit code 2.
var errUsage = errors.New("usage")

// flags is a command's flag set; its errors go to errOut.
func flags(name string, errOut io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	return fs
}

// kinds are the agent kinds, agents.json included when it loads.
func kinds() []agent.Kind {
	k, _ := agent.Load()
	if k == nil {
		return agent.Builtin()
	}
	return k
}

// spawn starts an agent off stage and prints its id.
func spawn(args []string, stdio IO) error {
	fs := flags("spawn", stdio.Err)
	var s drive.Spec
	fs.StringVar(&s.Kind, "k", "claude", "agent `kind`")
	fs.StringVar(&s.Name, "n", "", "`name` to send to it by")
	fs.StringVar(&s.Model, "m", "", "`model`, passed as --model")
	fs.StringVar(&s.Dir, "C", ".", "`dir`ectory to run in")
	fs.StringVar(&s.Branch, "w", "", "run in a worktree on `branch`")
	fs.Usage = func() {
		fmt.Fprintln(stdio.Err, "usage: mad spawn [flags] [-- agent flags...]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	s.Args = fs.Args()
	a, err := drive.Spawn(s, kinds())
	if a != nil {
		// It exists even when it came up asking something (err says what).
		fmt.Fprintln(stdio.Out, a.ID)
	}
	return err
}

// find resolves an agent named on the command line.
func find(ref string) (*state.State, *state.Agent, error) {
	st, err := state.Load()
	if err != nil {
		return nil, nil, err
	}
	cwd, _ := os.Getwd()
	_, a, err := drive.Find(st, ref, cwd)
	return st, a, err
}

// send types a message into an agent, and with -w prints its reply.
func send(args []string, stdio IO) error {
	fs := flags("send", stdio.Err)
	file := fs.String("f", "", "send the contents of `file`")
	wait := fs.Bool("w", false, "wait for the reply and print it")
	timeout := fs.Duration("t", 0, "with -w, give up after `duration` (exit 124)")
	fs.Usage = func() {
		fmt.Fprintln(stdio.Err, "usage: mad send [flags] AGENT [TEXT...|-]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return errUsage
	}
	var text string
	switch rest := fs.Args()[1:]; {
	case *file != "":
		data, err := os.ReadFile(paths.Expand(*file))
		if err != nil {
			return err
		}
		text = string(data)
	case len(rest) == 0 || len(rest) == 1 && rest[0] == "-":
		data, err := io.ReadAll(stdio.In)
		if err != nil {
			return err
		}
		text = string(data)
	default:
		text = strings.Join(rest, " ")
	}
	st, a, err := find(fs.Arg(0))
	if err != nil {
		return err
	}
	ks := kinds()
	if err := drive.Send(st, a, text, ks); err != nil {
		return err
	}
	if !*wait {
		return nil
	}
	return printTurn(a, ks, *timeout, stdio)
}

// wait prints an agent's reply once its turn ends.
func wait(args []string, stdio IO) error {
	fs := flags("wait", stdio.Err)
	timeout := fs.Duration("t", 0, "give up after `duration` (exit 124)")
	fs.Usage = func() {
		fmt.Fprintln(stdio.Err, "usage: mad wait [-t duration] AGENT")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	_, a, err := find(fs.Arg(0))
	if err != nil {
		return err
	}
	return printTurn(a, kinds(), *timeout, stdio)
}

func printTurn(a *state.Agent, ks []agent.Kind, timeout time.Duration, stdio IO) error {
	note := func(asking bool, q string) {
		if asking {
			fmt.Fprintf(stdio.Err, "mad: %s is waiting for you%s\n", drive.Label(a), paren(q))
		}
	}
	turn, err := drive.Wait(a, ks, drive.WaitOptions{Timeout: timeout, Note: note})
	if err != nil {
		return err
	}
	if turn != nil && turn.Reply != "" {
		fmt.Fprintln(stdio.Out, strings.TrimRight(turn.Reply, "\n"))
	}
	return nil
}

func paren(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}

// ls lists the deck's agents and what they are doing.
func ls(out io.Writer) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	stats := drive.Statuses(st, kinds())
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tKIND\tSTATUS\tDIR")
	for _, p := range st.Projects {
		for _, a := range p.Agents {
			name := a.Name
			if name == "" {
				name = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", drive.ShortID(a.ID), name, a.Kind, stats[a.ID], paths.Short(p.Dir(a)))
		}
	}
	return tw.Flush()
}
