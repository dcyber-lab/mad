package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/skills"
)

const skillUsage = `usage:
  mad skill ls [PROJECT]      the skills mad comes with; with a project (path
                              or name), the skills approved for it
  mad skill install [NAME…]   put them (default: all) in ~/.claude/skills/
                              for claude to use; again to update them
  mad skill pending           skills agents saved, waiting for you
  mad skill approve NAME      install a pending skill for claude, codex and pi
  mad skill discard NAME      drop a pending skill
  mad skill sync              copy the approved skills to the agents again
`

// claudeSkills is where claude looks for a user's skills.
func claudeSkills() string { return filepath.Join(paths.Home(), ".claude", "skills") }

// skillCmd is `mad skill ...`.
func skillCmd(args []string, stdio IO) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "ls":
		if len(args) > 0 {
			return skillLs(args[0], stdio)
		}
		for _, name := range skills.Names {
			fmt.Fprintln(stdio.Out, name)
		}
		return nil
	case "pending":
		list, err := skill.PendingList()
		if err != nil {
			return err
		}
		for _, p := range list {
			fmt.Fprintf(stdio.Out, "%s  (%s, by %s)  %s\n  %s\n", p.Name, p.Scope, p.By, p.Description, paths.Short(p.Path()))
		}
		if len(list) == 0 {
			fmt.Fprintln(stdio.Out, "no skills are pending")
		}
		return nil
	case "approve":
		if len(args) != 1 {
			break
		}
		if err := skill.Approve(args[0], nil); err != nil {
			return err
		}
		fmt.Fprintf(stdio.Out, "approved %s\n", args[0])
		return nil
	case "discard":
		if len(args) != 1 {
			break
		}
		if err := skill.Discard(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(stdio.Out, "discarded %s\n", args[0])
		return nil
	case "sync":
		return skill.Sync()
	case "install":
		names := args
		if len(names) == 0 {
			names = skills.Names
		}
		for _, name := range names {
			if _, err := fs.Stat(skills.FS, name); err != nil {
				return fmt.Errorf("no skill %q (mad skill ls)", name)
			}
			dest := filepath.Join(claudeSkills(), name)
			if err := installSkill(name, dest); err != nil {
				return err
			}
			fmt.Fprintf(stdio.Out, "installed %s in %s\n", name, paths.Short(dest))
		}
		return nil
	}
	fmt.Fprint(stdio.Err, skillUsage)
	return errUsage
}

// skillLs lists the approved skills of a project given by path or name.
func skillLs(ref string, stdio IO) error {
	project := paths.Expand(ref)
	if st, err := state.Load(); err == nil {
		for _, p := range st.Projects {
			if p.Name == ref || p.Path == project {
				project = p.Path
			}
		}
	}
	for _, s := range skill.List(project) {
		fmt.Fprintf(stdio.Out, "%s  (%s)  %s\n", s.Name, s.Scope, s.Description)
	}
	return nil
}

// installSkill copies skill name's files to dest, replacing what an
// earlier install put there.
func installSkill(name, dest string) error {
	return fs.WalkDir(skills.FS, name, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(name, path)
		data, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return paths.WriteFileAtomic(target, data)
	})
}
