package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	cases := []struct {
		out  string
		want Info
	}{
		{"## main...origin/main [ahead 2, behind 1]\n M a.go\n?? b.go\n", Info{"main", 2, 2}},
		{"## main...origin/main [ahead 1]\n", Info{"main", 0, 1}},
		{"## feat/x\nA  new\n", Info{"feat/x", 1, 0}},
		{"## HEAD (no branch)\n", Info{"", 0, 0}},
		{"## No commits yet on main\n?? x\n", Info{"main", 1, 0}},
		{"", Info{}},
	}
	for _, c := range cases {
		if got := parseStatus(c.out); got != c.want {
			t.Errorf("parseStatus(%q) = %+v, want %+v", c.out, got, c.want)
		}
	}
}

func TestWorktreeDir(t *testing.T) {
	if got := WorktreeDir("/r", "feat/x"); got != "/r/.claude/worktrees/feat-x" {
		t.Error(got)
	}
	if !IsWorktree("/r", "/r/.claude/worktrees/feat-x") || IsWorktree("/r", "/r") || IsWorktree("/r", "/r/.claude/worktrees") || IsWorktree("/r", "/elsewhere") {
		t.Error("IsWorktree")
	}
}

func TestValidBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if err := ValidBranch("feat/x-1"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "a b", "-x", "a..b", "x/"} {
		if ValidBranch(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// repo makes a git repository with one commit.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644)
	git("add", "a")
	git("commit", "-q", "-m", "one")
	return dir
}

func TestDefaultBranch(t *testing.T) {
	r := repo(t)
	if got := DefaultBranch(r); got != "main" {
		t.Errorf("no origin: %q", got)
	}
	// With an origin, its HEAD decides.
	exec.Command("git", "-C", r, "remote", "add", "origin", r).Run()
	exec.Command("git", "-C", r, "branch", "trunk").Run()
	exec.Command("git", "-C", r, "fetch", "-q", "origin").Run()
	exec.Command("git", "-C", r, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk").Run()
	if got := DefaultBranch(r); got != "trunk" {
		t.Errorf("origin HEAD: %q", got)
	}
	if got := DefaultBranch(t.TempDir()); got != "" {
		t.Errorf("no repo: %q", got)
	}
}

func TestStatusAndWorktrees(t *testing.T) {
	r := repo(t)
	if info, ok := Status(r); !ok || info != (Info{Branch: "main"}) {
		t.Fatalf("clean repo: %+v %v", info, ok)
	}
	if _, ok := Status(t.TempDir()); ok {
		t.Error("a plain directory reads as a repo")
	}
	os.WriteFile(filepath.Join(r, "b"), []byte("b"), 0o644)
	if info, _ := Status(r); info.Dirty != 1 {
		t.Errorf("untracked file not counted: %+v", info)
	}

	dir := WorktreeDir(r, "feat/x")
	if err := AddWorktree(r, "feat/x", dir); err != nil {
		t.Fatal(err)
	}
	if info, ok := Status(dir); !ok || info.Branch != "feat/x" || info.Dirty != 0 {
		t.Errorf("worktree: %+v %v", info, ok)
	}
	// The worktrees directory is excluded, so the main checkout stays as
	// dirty as before.
	if info, _ := Status(r); info.Dirty != 1 {
		t.Errorf("main after worktree add: %+v", info)
	}
	if ex, _ := os.ReadFile(filepath.Join(r, ".git", "info", "exclude")); !strings.Contains(string(ex), ".claude/worktrees/") {
		t.Error("exclude not written")
	}
	// Adding again is a no-op; a second worktree for the same branch is
	// what git refuses, so an existing dir must be reused.
	if err := AddWorktree(r, "feat/x", dir); err != nil {
		t.Errorf("re-add: %v", err)
	}
	// An existing branch is checked out rather than recreated.
	other := WorktreeDir(r, "other")
	exec.Command("git", "-C", r, "branch", "feat/y").Run()
	if err := AddWorktree(r, "feat/y", other); err != nil {
		t.Fatal(err)
	}
	if info, _ := Status(other); info.Branch != "feat/y" {
		t.Errorf("existing branch: %+v", info)
	}

	os.WriteFile(filepath.Join(dir, "c"), []byte("c"), 0o644)
	if err := RemoveWorktree(r, dir); err == nil {
		t.Error("dirty worktree removed")
	} else if !strings.Contains(err.Error(), "feat-x has uncommitted changes") {
		t.Errorf("error: %v", err)
	}
	os.Remove(filepath.Join(dir, "c"))
	if err := RemoveWorktree(r, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("worktree dir still there")
	}
	if exec.Command("git", "-C", r, "rev-parse", "--verify", "refs/heads/feat/x").Run() != nil {
		t.Error("branch should survive the worktree")
	}
}
