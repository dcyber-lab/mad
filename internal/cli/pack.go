package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/dcyber-lab/mad/internal/pack"
	"github.com/dcyber-lab/mad/internal/paths"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/team"
)

const packUsage = `usage:
  mad pack ls               the packs here: what each has, and where it is from
  mad pack show NAME        a pack's agents, flows, templates and skills
  mad pack check [DIR…]     check packs (default: every pack here): the flows
                            and templates read, the skills they name are there
  mad pack eject [-p] NAME  copy a pack to ~/.config/mad/packs/ (-p: to this
                            project's .mad/packs/) to change it: the copy
                            takes the place of the one it was copied from
A pack is a way of working, kept as files apart from mad itself: pack.json,
flows/*.json, teams/*.json and skills/<name>/SKILL.md. One in the project's
.mad/packs/ or in ~/.config/mad/packs/ takes the place of a built-in one of
its name. A run or a team keeps the pack as it was when it started.
`

// packCmd is `mad pack ...`.
func packCmd(args []string, stdio IO) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	cwd, _ := os.Getwd()
	root, isGit := paths.ProjectRoot(cwd)
	if !isGit {
		root = ""
	}
	switch sub {
	case "ls":
		list, lerr := pack.List(root)
		tw := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PACK\tVERSION\tFLOWS\tTEAMS\tSKILLS\tFROM")
		for _, p := range list {
			flows, _ := p.Files("flows")
			teams, _ := p.Files("teams")
			from := paths.Short(p.Source)
			if p.Replaces != "" {
				from += " (in place of " + p.Replaces + ")"
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\n", p.Name, p.Version, len(flows), len(teams), len(p.Skills()), from)
		}
		tw.Flush()
		return lerr
	case "show":
		if len(args) != 1 {
			break
		}
		list, _ := pack.List(root)
		p, ok := pack.Find(list, args[0])
		if !ok {
			return fmt.Errorf("no pack %q (mad pack ls)", args[0])
		}
		return packShow(p, stdio)
	case "check":
		var checked []pack.Pack
		var errs []error
		if len(args) == 0 {
			list, err := pack.List(root)
			checked, errs = list, append(errs, err)
		}
		for _, dir := range args {
			p, err := pack.Read(paths.Expand(dir))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			checked = append(checked, p)
		}
		for _, p := range checked {
			flows, teams, err := checkPack(p)
			if err != nil {
				errs = append(errs, fmt.Errorf("pack %s (%s):\n%w", p.Name, paths.Short(p.Source), err))
				continue
			}
			fmt.Fprintf(stdio.Out, "pack %s is good: %d flows, %d templates, %d skills\n", p, flows, teams, len(p.Skills()))
		}
		return errors.Join(errs...)
	case "eject":
		fs := flags("pack eject", stdio.Err)
		project := fs.Bool("p", false, "to this project's .mad/packs/")
		fs.Usage = func() { fmt.Fprint(stdio.Err, packUsage) }
		// -p before the name or after it.
		sort.SliceStable(args, func(i, j int) bool { return strings.HasPrefix(args[i], "-") && !strings.HasPrefix(args[j], "-") })
		if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
			fmt.Fprint(stdio.Err, packUsage)
			return errUsage
		}
		list, _ := pack.List(root)
		p, ok := pack.Find(list, fs.Arg(0))
		if !ok {
			return fmt.Errorf("no pack %q (mad pack ls)", fs.Arg(0))
		}
		dst := filepath.Join(pack.UserDir(), p.Name)
		if *project {
			if root == "" {
				return errors.New("not in a git repository: -p copies to a project's .mad/packs/")
			}
			dst = filepath.Join(pack.ProjectDir(root), p.Name)
		}
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s is there already: change it, or remove it to copy anew", paths.Short(dst))
		}
		if err := p.CopyTo(dst); err != nil {
			return err
		}
		fmt.Fprintf(stdio.Out, "pack %s copied to %s: it takes the place of the one from %s.\nChange its files, then `mad pack check`; runs and teams started from now on follow it.\n", p, paths.Short(dst), paths.Short(p.Source))
		return nil
	}
	fmt.Fprint(stdio.Err, packUsage)
	return errUsage
}

// packShow prints what pack p has.
func packShow(p pack.Pack, stdio IO) error {
	fmt.Fprintf(stdio.Out, "%s: %s\nfrom %s", p, p.Description, paths.Short(p.Source))
	if p.Replaces != "" {
		fmt.Fprintf(stdio.Out, ", in place of %s", p.Replaces)
	}
	fmt.Fprintln(stdio.Out)
	if names := p.AgentNames(); len(names) > 0 {
		fmt.Fprintln(stdio.Out, "\nagents:")
		for _, n := range names {
			fmt.Fprintf(stdio.Out, "  $%s = %s\n", n, p.Agents[n])
		}
	}
	tw := tabwriter.NewWriter(stdio.Out, 0, 0, 2, ' ', 0)
	var errs []error
	if files, _ := p.Files("flows"); len(files) > 0 {
		fmt.Fprintln(tw, "\nflows:")
		for _, file := range files {
			f, err := madrun.ParseIn(file.Data, p)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p.Label(file.Name), err))
				continue
			}
			var steps []string
			for _, s := range f.Steps {
				steps = append(steps, s.Name)
			}
			fmt.Fprintf(tw, "  %s\t%s\n", f.Name, strings.Join(steps, " → "))
		}
	}
	if files, _ := p.Files("teams"); len(files) > 0 {
		fmt.Fprintln(tw, "\nteams:")
		for _, file := range files {
			t, err := team.ParseIn(file.Data, p)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p.Label(file.Name), err))
				continue
			}
			var roles []string
			for _, r := range t.Roles {
				s := r.Name
				if r.Lead {
					s += " (lead)"
				}
				roles = append(roles, s)
			}
			fmt.Fprintf(tw, "  %s\t%s\n", t.Name, strings.Join(roles, ", "))
		}
	}
	tw.Flush()
	if skills := p.Skills(); len(skills) > 0 {
		fmt.Fprintf(stdio.Out, "\nskills (a role names one as %s:NAME):\n  %s\n", p.Name, strings.Join(skills, ", "))
	}
	return errors.Join(errs...)
}

// checkPack says everything that is wrong with pack p, or nil, and how
// many flows and templates it has: files that don't read or check, a role
// that works by a skill of the pack that is not there, an agent the pack
// names that none can play, a skill that points at a file that is not
// there.
func checkPack(p pack.Pack) (flows, teams int, err error) {
	var errs []error
	skill := func(what, role, ref string) {
		if in, name, ok := strings.Cut(ref, ":"); ok && in == p.Name && !p.HasSkill(name) {
			errs = append(errs, fmt.Errorf("%s, role %s: the pack has no skill %s (skills/%s/SKILL.md)", what, role, name, name))
		}
	}
	for _, name := range p.AgentNames() {
		c := madrun.ParseChoice(p.Agents[name])
		switch {
		case c.Kind != "claude" && c.Kind != "codex":
			errs = append(errs, fmt.Errorf("agents: $%s is %q: a role is played by claude or codex", name, p.Agents[name]))
		case c.Effort != "" && !containsStr(madrun.Efforts(madrun.Choice{Kind: c.Kind, Model: c.Model}), c.Effort):
			errs = append(errs, fmt.Errorf("agents: $%s is %q: effort %q is not one of %s's", name, p.Agents[name], c.Effort, c.Kind))
		}
	}
	files, ferr := p.Files("flows")
	errs = append(errs, ferr)
	for _, file := range files {
		f, err := madrun.ParseIn(file.Data, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", file.Name, err))
			continue
		}
		flows++
		for _, r := range f.Roles {
			skill(file.Name, r.Name, r.Skill)
		}
	}
	files, ferr = p.Files("teams")
	errs = append(errs, ferr)
	for _, file := range files {
		t, err := team.ParseIn(file.Data, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", file.Name, err))
			continue
		}
		teams++
		for _, r := range t.Roles {
			skill(file.Name, r.Name, r.Skill)
		}
	}
	errs = append(errs, p.CheckLinks())
	return flows, teams, errors.Join(errs...)
}
