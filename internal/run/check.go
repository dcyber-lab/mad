package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A step is checked when its reply comes: against what it must leave
// behind (its Expect: files and commits the first time it runs, an
// unchanged worktree every time), and for a role that edits nothing,
// against the worktree it was given. Both read the worktree's state when
// the step began, kept in its Entry.

// worktreeState is the commit a worktree is on and the tree its files
// would make if all were added: ignored files, the run's own directory
// and checkouts nested in it (a subagent's worktree) left out. Its index
// is not touched: the files go into a copy. "" when git can't tell.
func worktreeState(dir string) (head, tree string) {
	head, err := gitOut(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", ""
	}
	index, err := gitOut(dir, "rev-parse", "--git-path", "index")
	if err != nil {
		return head, ""
	}
	if !filepath.IsAbs(index) {
		index = filepath.Join(dir, index)
	}
	tmp, err := os.CreateTemp("", "mad-index-*")
	if err != nil {
		return head, ""
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if data, err := os.ReadFile(index); err == nil {
		_ = os.WriteFile(tmp.Name(), data, 0o644)
	}
	// git lists an untracked checkout as its directory, "sub/", and does
	// not go into it.
	add := []string{"add", "-A", "--", "."}
	if out, err := gitOut(dir, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, p := range strings.Split(out, "\n") {
			if strings.HasSuffix(p, "/") {
				add = append(add, ":(exclude)"+p)
			}
		}
	}
	env := append(os.Environ(), "GIT_INDEX_FILE="+tmp.Name())
	cmd := exec.Command("git", append([]string{"-C", dir}, add...)...)
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return head, ""
	}
	cmd = exec.Command("git", "-C", dir, "write-tree")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return head, ""
	}
	return head, strings.TrimSpace(string(out))
}

// unmet says what step s, in round, left undone of what it must, from
// the worktree's state when it began (e).
func (x *runner) unmet(s Step, round int, e Entry) []string {
	ex := s.Expect
	if ex == nil || e.Head == "" {
		return nil
	}
	var out []string
	if ex.Unchanged {
		if what := changedBy(x.run.Dir, e); what != "" {
			out = append(out, fmt.Sprintf("the worktree is not as this step found it (%s): undo that; what the step makes goes in %s/", what, RelDir(x.run)))
		}
	}
	if round > 1 {
		return out
	}
	for _, f := range ex.Files {
		name := render(f, map[string]string{"run": RelDir(x.run)})
		if fi, err := os.Stat(filepath.Join(x.run.Dir, name)); err != nil || fi.IsDir() || fi.Size() == 0 {
			out = append(out, name+" is missing or empty")
		}
	}
	commits := newCommits(x.run.Dir, e.Head)
	if ex.Commit && len(commits) == 0 {
		out = append(out, "there is no commit from this step")
	}
	if ex.TestsFirst && (len(commits) > 0 || !ex.Commit) {
		if why := testsFirst(x.run.Dir, commits); why != "" {
			out = append(out, why)
		}
	}
	return out
}

// newCommits are the commits on dir's HEAD since base, oldest first.
func newCommits(dir, base string) []string {
	out, err := gitOut(dir, "rev-list", "--reverse", base+"..HEAD")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// testsFirst says how commits fail to put a test ahead of the code it
// tests, or "": one that changes tests alone must come before the first
// that changes anything else.
func testsFirst(dir string, commits []string) string {
	sawTest := false
	for _, c := range commits {
		files, err := gitOut(dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", c)
		if err != nil {
			return ""
		}
		var code []string
		for _, f := range strings.Split(files, "\n") {
			if f != "" && !isTest(f) {
				code = append(code, f)
			}
		}
		if len(code) == 0 {
			sawTest = sawTest || files != ""
			continue
		}
		if sawTest {
			return ""
		}
		subject, _ := gitOut(dir, "log", "-1", "--format=%h %s", c)
		return fmt.Sprintf("the first commit that changes code (%s: %s) has no commit of the failing test alone before it", subject, strings.Join(first(code, 3), ", "))
	}
	if !sawTest {
		return "no commit adds a test"
	}
	return ""
}

func first(list []string, n int) []string {
	if len(list) > n {
		return append(list[:n:n], "…")
	}
	return list
}

// isTest reports whether path is a test, or what only tests use, by the
// names most languages give them.
func isTest(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, d := range parts[:len(parts)-1] {
		switch d {
		case "test", "tests", "__tests__", "spec", "testdata", "Tests", "UITests":
			return true
		}
	}
	base := parts[len(parts)-1]
	name := strings.TrimSuffix(base, filepath.Ext(base))
	switch {
	case strings.HasSuffix(base, "_test.go"),
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, "_test.py"), base == "conftest.py",
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasSuffix(base, "_spec.rb"), strings.HasSuffix(base, "_test.rb"),
		strings.HasSuffix(name, "Test") || strings.HasSuffix(name, "Tests"):
		return true
	}
	return false
}

// changedBy says how the worktree differs from its state when a step
// began, for a role that edits nothing: new commits, or the files that
// differ. "" when it is as it was, or the state was not taken.
func changedBy(dir string, e Entry) string {
	if e.Head == "" || e.Tree == "" {
		return ""
	}
	head, tree := worktreeState(dir)
	if head == "" || tree == "" || head == e.Head && tree == e.Tree {
		return ""
	}
	if head != e.Head {
		log, _ := gitOut(dir, "log", "--format=%h %s", e.Head+".."+head)
		if log == "" {
			return "the worktree is on another commit (" + head[:min(7, len(head))] + ")"
		}
		return "new commits: " + strings.Join(first(strings.Split(log, "\n"), 3), "; ")
	}
	files, _ := gitOut(dir, "diff-tree", "--name-only", "-r", e.Tree, tree)
	if files == "" {
		return "files in the worktree changed"
	}
	return "changed files: " + strings.Join(first(strings.Split(files, "\n"), 5), ", ")
}
