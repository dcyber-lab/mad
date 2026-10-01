package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dcyber-lab/mad/internal/paths"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
)

const runUsage = `usage:
  mad run new [dir]        the form for a new run, on stage (o in the sidebar)
  mad run start [-f flow] [-b branch] [-C dir] [--gate] [--perm auto|allowlist|bypass]
                [--agent role=agent ...] TASK...
                           agent: claude:opus, codex, codex:gpt-6-sol, ...,
                           with @effort to set how hard it thinks
                           (claude:opus@high, codex@xhigh)
                           start a run without the form; prints its id
  mad run ls               list runs
  mad run flow ...         list, show and check flows ("mad run flow" for more)
  mad run continue RUN     go on where the run waits for you (c)
  mad run cancel RUN       stop the run; its branch and files stay (x)
  mad run exec RUN         the runner (runs in its own pane)
`

// runCmd is `mad run ...`.
func runCmd(args []string, stdio IO) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "new":
		dir := "."
		if len(args) > 0 {
			dir = args[0]
		}
		return madrun.NewForm(dir)
	case "start":
		fs := flags("run start", stdio.Err)
		var o madrun.Options
		fs.StringVar(&o.Flow, "f", "", "`flow` (default: the first of mad run flow ls)")
		fs.StringVar(&o.Branch, "b", "", "`branch` (default: named after the task)")
		fs.StringVar(&o.Project, "C", ".", "project `dir`ectory")
		fs.BoolVar(&o.Gate, "gate", false, "stop after the design for you")
		fs.StringVar(&o.Permission, "perm", madrun.PermAuto, "what claude agents may do unasked: auto, allowlist or bypass")
		o.Agents = map[string]string{}
		fs.Var(agentFlag(o.Agents), "agent", "`role=agent` for a role, e.g. builder=codex@high or reviewer=claude:opus (repeatable)")
		fs.Usage = func() { fmt.Fprint(stdio.Err, runUsage) }
		if err := fs.Parse(args); err != nil {
			return errUsage
		}
		o.Task = strings.Join(fs.Args(), " ")
		r, err := madrun.Create(o)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdio.Out, r.ID)
		return nil
	case "ls":
		return runList(stdio)
	case "flow":
		return flowCmd(args, stdio)
	case "continue", "cancel", "exec":
		if len(args) != 1 {
			fmt.Fprint(stdio.Err, runUsage)
			return errUsage
		}
		st, err := state.Load()
		if err != nil {
			return err
		}
		r := findRun(st, args[0])
		if r == nil {
			return fmt.Errorf("no run %q (mad run ls)", args[0])
		}
		if sub == "exec" {
			return madrun.Exec(r.ID, os.Stdout)
		}
		return madrun.Command(r, sub)
	}
	fmt.Fprint(stdio.Err, runUsage)
	return errUsage
}

// agentFlag collects --agent role=agent.
type agentFlag map[string]string

func (f agentFlag) String() string { return "" }

func (f agentFlag) Set(s string) error {
	role, agent, ok := strings.Cut(s, "=")
	if !ok || role == "" || agent == "" {
		return fmt.Errorf("want role=agent, e.g. builder=codex")
	}
	f[role] = agent
	return nil
}

// findRun finds a run by id or name.
func findRun(st *state.State, ref string) *state.Run {
	for _, p := range st.Projects {
		for _, r := range p.Runs {
			if r.ID == ref || r.Name == ref {
				return r
			}
		}
	}
	return nil
}

func runList(stdio IO) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tSTEP\tPROJECT")
	for _, p := range st.Projects {
		for _, r := range p.Runs {
			stat, step := "?", ""
			if f, err := madrun.Load(madrun.Dir(r)); err == nil {
				stat = f.Progress.Status
				if fl := f.Flow(); f.Progress.Step < len(fl.Steps) {
					step = fl.Steps[f.Progress.Step].Name
				}
				if f.Progress.Waiting != "" {
					step += " · " + f.Progress.Waiting
				}
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Name, stat, step, p.Name)
		}
	}
	return tw.Flush()
}

const flowUsage = `usage:
  mad run flow ls             the flows a run here can follow, and where each is from
  mad run flow show NAME      a flow as JSON: a start for one of your own
  mad run flow check [FILE…]  check flow files (default: every flow here)
  mad run flow agents         the agents a role can be played by, with their efforts
Flows are JSON files in .mad/flows/ (the project's) and ~/.config/mad/flows/
(yours); "mad skill install" adds a skill that writes them with you.
`

// flowCmd is `mad run flow ...`.
func flowCmd(args []string, stdio IO) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	cwd, _ := os.Getwd()
	root, _ := paths.ProjectRoot(cwd)
	switch sub {
	case "ls":
		flows, err := madrun.Flows(root)
		tw := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tSTEPS\tROLES\tFROM")
		for _, f := range flows {
			var steps, roles []string
			for _, s := range f.Steps {
				steps = append(steps, s.Name)
			}
			for _, r := range f.Roles {
				roles = append(roles, r.Name+"="+r.Agent)
			}
			from := f.Source
			if from != "built in" {
				from = paths.Short(from)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Name, strings.Join(steps, " → "), strings.Join(roles, " "), from)
		}
		tw.Flush()
		return err
	case "show":
		if len(args) != 1 {
			fmt.Fprint(stdio.Err, flowUsage)
			return errUsage
		}
		f, ok := madrun.FlowByName(root, args[0])
		if !ok {
			return fmt.Errorf("no flow %q (mad run flow ls)", args[0])
		}
		data, err := madrun.Marshal(f)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdio.Out, string(data))
		return nil
	case "check":
		var errs []error
		if len(args) == 0 {
			flows, err := madrun.Flows(root)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%d flows, all good\n", len(flows))
			return nil
		}
		for _, file := range args {
			f, err := madrun.ReadFile(paths.Expand(file))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			fmt.Fprintf(stdio.Out, "%s: flow %s is good: %d roles, %d steps\n", file, f.Name, len(f.Roles), len(f.Steps))
		}
		return errors.Join(errs...)
	case "agents":
		tw := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "AGENT\tEFFORTS (add as @effort)")
		for _, c := range madrun.Choices() {
			levels := madrun.Efforts(c)[1:]
			name := c.String()
			if c.Kind == "codex" && c.Model == "" {
				name += " (the model its config sets)"
			}
			fmt.Fprintf(tw, "%s\t%s\n", name, strings.Join(levels, " "))
		}
		return tw.Flush()
	}
	fmt.Fprint(stdio.Err, flowUsage)
	return errUsage
}
