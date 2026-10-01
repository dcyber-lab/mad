package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/skills"
)

const skillUsage = `usage:
  mad skill ls                the skills mad comes with
  mad skill install [NAME…]   put them (default: all) in ~/.claude/skills/
                              for claude to use; again to update them
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
		for _, name := range skills.Names {
			fmt.Fprintln(stdio.Out, name)
		}
		return nil
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
