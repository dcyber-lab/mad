package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/team"
)

const teamUsage = `usage:
  mad team ls              the templates here and where each is from, and the
                           teams that run, with their agents' statuses
  mad team start [-t template] [-n name] [-C dir] [-w branch] [--perm auto|allowlist|bypass]
                 [--agent role=agent ...] GOAL...
                           start a team: its roles as agents in a new channel,
                           the lead told the goal; prints the team's name
                           agent: claude:opus, codex, codex:gpt-6-sol, ...,
                           with @effort (claude:opus@high)
                           -w gives all roles one worktree on that branch;
                           without it they work in the project's directory
  mad team show NAME       a running team: roles, agents, briefs
  mad team sleep NAME      put every agent of the team to sleep
  mad team rm NAME         end the team: agents removed, channel archived
  mad team check [FILE…]   check template files (default: every template here)
Templates are JSON files in .mad/teams/ (the project's) and ~/.config/mad/teams/
(yours); "mad team ls" shows the built in one to start from.
`

// teamCmd is `mad team ...`.
func teamCmd(args []string, stdio IO) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	cwd, _ := os.Getwd()
	root, _ := paths.ProjectRoot(cwd)
	switch sub {
	case "ls":
		return teamList(root, stdio)
	case "start":
		fs := flags("team start", stdio.Err)
		var o team.Options
		fs.StringVar(&o.Template, "t", "", "`template` (default: the first of mad team ls)")
		fs.StringVar(&o.Name, "n", "", "`name` of the team and its channel (default: from the goal)")
		fs.StringVar(&o.Project, "C", ".", "project `dir`ectory")
		fs.StringVar(&o.Branch, "w", "", "work in a worktree on this `branch`")
		fs.StringVar(&o.Permission, "perm", madrun.PermAuto, "what claude agents may do unasked: auto, allowlist or bypass")
		o.Agents = map[string]string{}
		fs.Var(agentFlag(o.Agents), "agent", "`role=agent` for a role, e.g. builder=codex@high (repeatable)")
		fs.Usage = func() { fmt.Fprint(stdio.Err, teamUsage) }
		if err := fs.Parse(args); err != nil {
			return errUsage
		}
		o.Goal = strings.Join(fs.Args(), " ")
		o.Kinds = kinds()
		s, err := team.Start(o)
		if err != nil {
			return err
		}
		for _, n := range s.Notes {
			fmt.Fprintln(stdio.Err, "mad:", n)
		}
		fmt.Fprintln(stdio.Out, s.Name)
		return nil
	case "show", "sleep", "rm":
		if len(args) != 1 {
			fmt.Fprint(stdio.Err, teamUsage)
			return errUsage
		}
		switch sub {
		case "show":
			return teamShow(args[0], stdio)
		case "sleep":
			n, err := team.Sleep(args[0])
			fmt.Fprintf(stdio.Out, "%d asleep\n", n)
			return err
		}
		return team.Remove(args[0])
	case "check":
		if len(args) == 0 {
			list, err := team.Templates(root)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "%d templates, all good\n", len(list))
			return nil
		}
		var errs []error
		for _, file := range args {
			t, err := team.ReadFile(paths.Expand(file))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			fmt.Fprintf(stdio.Out, "%s: team %s is good: %d roles\n", file, t.Name, len(t.Roles))
		}
		return errors.Join(errs...)
	}
	fmt.Fprint(stdio.Err, teamUsage)
	return errUsage
}

func teamList(root string, stdio IO) error {
	list, terr := team.Templates(root)
	tw := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TEMPLATE\tROLES\tFROM")
	for _, t := range list {
		var roles []string
		for _, r := range t.Roles {
			s := r.Name + "=" + r.Agent
			if r.Lead {
				s += " (lead)"
			}
			roles = append(roles, s)
		}
		from := t.Source
		if from != "built in" {
			from = paths.Short(from)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", t.Name, strings.Join(roles, " "), from)
	}
	tw.Flush()
	st, err := state.Load()
	if err != nil {
		return err
	}
	running, err := team.Running(st)
	if err != nil {
		return err
	}
	if len(running) > 0 {
		statuses := drive.Statuses(st, kinds())
		fmt.Fprintln(stdio.Out)
		tw = tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "TEAM\tCHANNEL\tAGENTS")
		for _, r := range running {
			var as []string
			for _, x := range r.Roles {
				s := "?"
				if x.Agent != "" {
					s = statuses[x.Agent]
				}
				as = append(as, x.Role+"="+s)
			}
			fmt.Fprintf(tw, "%s\t#%s\t%s\n", r.Name, r.Channel, strings.Join(as, " "))
		}
		tw.Flush()
	}
	return terr
}

func teamShow(name string, stdio IO) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	r, ok, err := team.Find(st, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no team %q (mad team ls)", name)
	}
	statuses := drive.Statuses(st, kinds())
	fmt.Fprintf(stdio.Out, "%s: template %s in #%s, %s, permission %s", r.Name, r.Template, r.Channel, paths.Short(r.Project), r.Permission)
	if r.Branch != "" {
		fmt.Fprintf(stdio.Out, ", worktree %s", r.Branch)
	}
	fmt.Fprintln(stdio.Out)
	for _, x := range r.Roles {
		who := "(not started)"
		if _, a := st.FindAgent(x.Agent); a != nil {
			who = fmt.Sprintf("%s [%s] %s", drive.Label(a), statuses[a.ID], drive.ShortID(a.ID))
		}
		lead := ""
		if x.Lead {
			lead = ", lead"
		}
		fmt.Fprintf(stdio.Out, "\n%s (%s%s): %s\n", x.Role, x.Spec, lead, who)
		if x.Brief != "" {
			fmt.Fprintf(stdio.Out, "  %s\n", strings.ReplaceAll(x.Brief, "\n", "\n  "))
		}
	}
	return nil
}
