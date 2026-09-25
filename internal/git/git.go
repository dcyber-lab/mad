// Package git is what mad needs from git: the state of a checkout for the
// sidebar (branch, uncommitted files), and worktrees for agents that
// should work on their own branch.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Info summarizes a checkout for display.
type Info struct {
	Branch string // "" when detached with no name; "HEAD" is never shown
	Dirty  int    // changed or untracked files
	Ahead  int    // commits not on the upstream
}

// Status reads dir's checkout; ok is false outside a git work tree.
// --no-optional-locks keeps a background read from touching the index.
func Status(dir string) (info Info, ok bool) {
	out, err := exec.Command("git", "--no-optional-locks", "-C", dir, "status", "--porcelain=v1", "-b").Output()
	if err != nil {
		return Info{}, false
	}
	return parseStatus(string(out)), true
}

func parseStatus(out string) Info {
	var info Info
	for i, line := range strings.Split(out, "\n") {
		if i == 0 {
			info.Branch, info.Ahead = parseBranchLine(line)
			continue
		}
		if line != "" {
			info.Dirty++
		}
	}
	return info
}

// parseBranchLine reads the "## ..." header of porcelain v1:
//
//	## main...origin/main [ahead 2, behind 1]
//	## feature
//	## HEAD (no branch)
//	## No commits yet on main
func parseBranchLine(line string) (branch string, ahead int) {
	s, ok := strings.CutPrefix(line, "## ")
	if !ok {
		return "", 0
	}
	if rest, ok := strings.CutPrefix(s, "No commits yet on "); ok {
		return rest, 0
	}
	if rest, ok := strings.CutPrefix(s, "Initial commit on "); ok {
		return rest, 0
	}
	if strings.HasPrefix(s, "HEAD ") {
		return "", 0
	}
	if i := strings.Index(s, " ["); i >= 0 {
		if j := strings.Index(s[i:], "ahead "); j >= 0 {
			n := s[i+j+len("ahead "):]
			if k := strings.IndexAny(n, ",]"); k >= 0 {
				n = n[:k]
			}
			ahead, _ = strconv.Atoi(n)
		}
		s = s[:i]
	}
	branch, _, _ = strings.Cut(s, "...")
	return branch, ahead
}

// worktreesSub is where mad keeps a project's worktrees. It is the
// directory Claude Code uses for its own, so mad's discovery already maps
// sessions in it back to the main repository.
const worktreesSub = ".claude/worktrees"

// WorktreeDir is where the worktree for branch lives under repo.
func WorktreeDir(repo, branch string) string {
	return filepath.Join(repo, worktreesSub, strings.ReplaceAll(branch, "/", "-"))
}

// IsWorktree reports whether dir is one of repo's mad-style worktrees.
func IsWorktree(repo, dir string) bool {
	rel, err := filepath.Rel(filepath.Join(repo, worktreesSub), dir)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// ValidBranch checks a user-typed branch name with git's own rules.
func ValidBranch(name string) error {
	if name == "" {
		return errors.New("empty branch name")
	}
	if err := exec.Command("git", "check-ref-format", "--branch", name).Run(); err != nil {
		return fmt.Errorf("invalid branch name %q", name)
	}
	return nil
}

// AddWorktree checks branch out in dir, creating the branch from repo's
// HEAD when it doesn't exist yet. An existing worktree at dir is reused.
func AddWorktree(repo, branch, dir string) error {
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return nil
		}
		return fmt.Errorf("%s exists and is not a worktree", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	excludeWorktrees(repo)
	args := []string{"-C", repo, "worktree", "add"}
	if exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil {
		args = append(args, dir, branch)
	} else {
		args = append(args, "-b", branch, dir)
	}
	return run(args...)
}

// RemoveWorktree unregisters and deletes dir. The branch stays. A
// worktree with uncommitted changes is refused, as by git.
func RemoveWorktree(repo, dir string) error {
	err := run("-C", repo, "worktree", "remove", dir)
	if err != nil && strings.Contains(err.Error(), "modified or untracked") {
		return fmt.Errorf("%s has uncommitted changes; commit or discard them first", filepath.Base(dir))
	}
	return err
}

// excludeWorktrees keeps the worktrees directory out of git status without
// touching the project's .gitignore: it goes in .git/info/exclude unless
// something ignores it already.
func excludeWorktrees(repo string) {
	if exec.Command("git", "-C", repo, "check-ignore", "-q", worktreesSub).Run() == nil {
		return
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return
	}
	exclude := filepath.Join(strings.TrimSpace(string(out)), "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(exclude, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n# added by mad\n%s/\n", worktreesSub)
}

func run(args ...string) error {
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:] // git's last line is the reason
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git %s: %s", args[2], msg)
	}
	return nil
}
