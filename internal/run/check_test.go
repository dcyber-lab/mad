package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/state"
)

func TestPublishes(t *testing.T) {
	for line, want := range map[string]string{
		"git push":                         "push",
		"git push -u origin HEAD":          "push",
		"git -C /x/y push":                 "push",
		"cd x && git push origin b":        "push",
		"FOO=1 git push":                   "push",
		"env GIT_TRACE=1 git push":         "push",
		"/usr/bin/git push":                "push",
		"bash -lc 'git push'":              "push",
		`sh -c "cd x; git push"`:           "push",
		"echo $(git push)":                 "push",
		"gh pr create --fill":              "open or merge pull requests",
		"gh pr merge 12 --squash":          "open or merge pull requests",
		"git status":                       "",
		"git log --format=push":            "",
		"grep -rn 'git push' .":            "",
		"echo git push":                    "",
		"git commit -m 'git push later'":   "",
		"git pushx":                        "",
		"gh pr view 3":                     "",
		"go test ./... | tee /tmp/out.txt": "",
		"bash --norc -c 'go test ./...'":   "",
	} {
		if got := publishes(line); got != want {
			t.Errorf("publishes(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestRefused(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(root, ".claude", "worktrees", "run-x")
	runDir := filepath.Join(dir, ".mad", "runs", "x")
	sub := filepath.Join(dir, ".claude", "worktrees", "agent-1")
	for _, d := range []string{runDir, sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prober := Role{Name: "prober", Label: "prober", NoEdits: true}
	builder := Role{Name: "builder", Label: "builder"}
	cases := []struct {
		role    Role
		tool    discover.Tool
		refused bool
	}{
		{prober, discover.Tool{Name: "Write", Path: filepath.Join(dir, "a.go")}, true},
		{prober, discover.Tool{Name: "Edit", Path: filepath.Join(dir, "pkg", "b.go")}, true},
		{prober, discover.Tool{Name: "Write", Path: filepath.Join(runDir, "report.md")}, false},
		{prober, discover.Tool{Name: "Write", Path: filepath.Join(os.TempDir(), "repro.go")}, false},
		{prober, discover.Tool{Name: "Write", Path: filepath.Join(sub, "a.go")}, false}, // a subagent's worktree
		{prober, discover.Tool{Name: "Bash", Command: "go test ./..."}, false},
		{prober, discover.Tool{Name: "Edit", Path: filepath.Join(root, "a.go")}, true}, // out of the worktree, as for any role
		{builder, discover.Tool{Name: "Write", Path: filepath.Join(dir, "a.go")}, false},
		{builder, discover.Tool{Name: "Bash", Command: "git push origin HEAD"}, true},
		{builder, discover.Tool{Name: "Bash", Command: "gh pr create --fill"}, true},
		{builder, discover.Tool{Name: "Bash", Command: "git commit -am fix"}, false},
	}
	for _, c := range cases {
		why := Refused(dir, root, runDir, c.role, &c.tool)
		if (why != "") != c.refused {
			t.Errorf("%s %s %s%s: %q", c.role.Name, c.tool.Name, c.tool.Path, c.tool.Command, why)
		}
	}
	if why := Refused(dir, root, runDir, prober, &discover.Tool{Name: "Write", Path: filepath.Join(dir, "a.go")}); !strings.Contains(why, "the prober edits no file in the run's worktree") {
		t.Errorf("why = %q", why)
	}
}

// gitRepo is a repository with one commit, its runs excluded as mad does.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	write(t, filepath.Join(repo, "a.go"), "package a\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	excludeRuns(repo)
	return repo
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, repo, msg string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		write(t, filepath.Join(repo, name), body)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

// A role that edits nothing is caught changing the worktree, not writing
// in the run's directory or a checkout of its own nested in it.
func TestChangedBy(t *testing.T) {
	repo := gitRepo(t)
	write(t, filepath.Join(repo, "wip.go"), "package a // uncommitted, the builder's\n")
	var e Entry
	e.Head, e.Tree = worktreeState(repo)
	if e.Head == "" || e.Tree == "" {
		t.Fatal("no state")
	}
	if what := changedBy(repo, e); what != "" {
		t.Errorf("unchanged: %q", what)
	}
	write(t, filepath.Join(repo, ".mad", "runs", "x", "notes.md"), "a finding\n")
	nested := filepath.Join(repo, ".claude", "worktrees", "agent-1")
	if out, err := exec.Command("git", "init", "-q", nested).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	write(t, filepath.Join(nested, "repro.go"), "package main\n")
	if what := changedBy(repo, e); what != "" {
		t.Errorf("the run's directory or a nested checkout counted: %q", what)
	}
	if index, _ := gitOut(repo, "diff", "--cached", "--name-only"); index != "" {
		t.Errorf("the index was touched: %q", index)
	}
	write(t, filepath.Join(repo, "wip.go"), "package a // changed by the prober\n")
	if what := changedBy(repo, e); what != "changed files: wip.go" {
		t.Errorf("an uncommitted file changed again: %q", what)
	}
	write(t, filepath.Join(repo, "wip.go"), "package a // uncommitted, the builder's\n")
	if err := os.RemoveAll(filepath.Join(repo, ".claude")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "repro_test.go"), "package a\n")
	if what := changedBy(repo, e); what != "changed files: repro_test.go" {
		t.Errorf("a new file: %q", what)
	}
	commit(t, repo, "probe", nil)
	if what := changedBy(repo, e); !strings.Contains(what, "new commits:") || !strings.Contains(what, "probe") {
		t.Errorf("a commit: %q", what)
	}
}

func TestUnmet(t *testing.T) {
	repo := gitRepo(t)
	x := &runner{run: &state.Run{Name: "x", Dir: repo}}
	var e Entry
	e.Head, e.Tree = worktreeState(repo)
	s := Step{Name: "fix", Expect: &Expect{Files: []string{"{{run}}/bug.md"}, Commit: true, TestsFirst: true}}

	got := x.unmet(s, 1, e)
	if strings.Join(got, "; ") != ".mad/runs/x/bug.md is missing or empty; there is no commit from this step" {
		t.Errorf("nothing done: %q", got)
	}
	if got := x.unmet(s, 2, e); got != nil {
		t.Errorf("a later round is not checked: %q", got)
	}
	write(t, filepath.Join(repo, ".mad", "runs", "x", "bug.md"), "root cause\n")
	commit(t, repo, "fix it", map[string]string{"a.go": "package a // fixed\n", "a_test.go": "package a\n"})
	got = x.unmet(s, 1, e)
	if len(got) != 1 || !strings.Contains(got[0], "fix it: a.go) has no commit of the failing test alone before it") {
		t.Errorf("the test with the fix: %q", got)
	}

	repo = gitRepo(t)
	x.run.Dir = repo
	e.Head, e.Tree = worktreeState(repo)
	write(t, filepath.Join(repo, ".mad", "runs", "x", "bug.md"), "root cause\n")
	commit(t, repo, "failing test", map[string]string{"a_test.go": "package a\n", "testdata/in.txt": "x\n"})
	commit(t, repo, "fix", map[string]string{"a.go": "package a // fixed\n"})
	if got := x.unmet(s, 1, e); got != nil {
		t.Errorf("the test first, then the fix: %q", got)
	}
	if got := x.unmet(Step{Expect: &Expect{TestsFirst: true}}, 1, Entry{Head: "HEAD", Tree: "x"}); len(got) != 1 || got[0] != "no commit adds a test" {
		t.Errorf("no commits: %q", got)
	}
}

func TestIsTest(t *testing.T) {
	for path, want := range map[string]bool{
		"internal/run/check_test.go": true, "internal/run/check.go": false,
		"tests/test_api.py": true, "app/test_api.py": true, "app/api_test.py": true, "app/api.py": false,
		"src/Button.test.tsx": true, "src/Button.spec.ts": true, "src/__tests__/a.js": true, "src/Button.tsx": false,
		"spec/models/user_spec.rb": true, "app/models/user.rb": false,
		"src/test/java/a/FooTest.java": true, "src/main/java/a/Foo.java": false,
		"ios/MadKitTests/ChannelTests.swift": true, "ios/MadKit/Channel.swift": false,
		"internal/run/testdata/flow.json": true, "contest/main.go": false,
	} {
		if got := isTest(path); got != want {
			t.Errorf("isTest(%q) = %v", path, got)
		}
	}
}

func TestCheckNoEditsAndExpect(t *testing.T) {
	for body, want := range map[string]string{
		`{"name":"x","roles":[{"name":"a","agent":"claude","read_only":true,"no_edits":true}],"steps":[{"name":"s","role":"a","prompt":"p","review":true,"back":"s"}]}`: "read_only or no_edits, not both",
		`{"name":"x","roles":[{"name":"a","agent":"claude","no_edits":true}],"steps":[{"name":"s","role":"a","prompt":"p","expect":{"commit":true}}]}`:                  "makes no commits, so the step can't expect one",
		`{"name":"x","roles":[{"name":"a","agent":"claude"}],"steps":[{"name":"s","role":"a","prompt":"p","expect":{"files":["{{task}}.md"]}}]}`:                        "expect.files: unknown placeholder {{task}}",
	} {
		if _, err := Parse([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
	f, err := Parse([]byte(`{"name":"x","roles":[{"name":"a","agent":"claude"},{"name":"p","agent":"codex","no_edits":true}],
		"steps":[{"name":"s","role":"a","prompt":"p","expect":{"files":["{{run}}/out.md"],"commit":true}},{"name":"r","role":"p","prompt":"q","review":true,"back":"s"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := f.Role("p"); strings.Join(r.Args(PermAuto), " ") != "-s read-only -a never -c check_for_update_on_startup=false" || r.Permits(PermAuto) != "read only (codex sandbox)" {
		t.Errorf("a codex that edits nothing: %v, %q", r.Args(PermAuto), r.Permits(PermAuto))
	}
}
