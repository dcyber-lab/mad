package run

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
)

// Defaults of a run's limits.
const (
	DefaultMaxRounds = 3
	DefaultBudget    = 10.0    // USD
	DefaultCompact   = 150_000 // tokens
)

// Options say what run to create.
type Options struct {
	Project string // its directory
	Flow    string
	Task    string
	Branch  string // "" names one after the task
	Gate    bool
	// Permission and Agents as in Spec; "" and nil keep the defaults.
	Permission string
	Agents     map[string]string
}

var (
	nonSlug = regexp.MustCompile(`[^a-z0-9]+`)
	urlRe   = regexp.MustCompile(`https?://\S+`)
	// itemRe finds what a link to an issue, merge or pull request names:
	// gitlab's /-/issues/1, github's /pull/2.
	itemRe = regexp.MustCompile(`/(?:-/)?(issues|merge_requests|pull|pulls)/(\d+)`)
)

// BranchFor names a run's branch after its task: run/ and the task's
// first ASCII words, the issue or merge request a link in it points at
// first, or the time when there is nothing to name it by.
func BranchFor(task string, now time.Time) string {
	var words []string
	for _, u := range urlRe.FindAllString(task, -1) {
		if m := itemRe.FindStringSubmatch(u); m != nil {
			kind := map[string]string{"issues": "issue", "merge_requests": "mr", "pull": "pr", "pulls": "pr"}[m[1]]
			words = append(words, kind, m[2])
		}
	}
	text := urlRe.ReplaceAllString(task, " ")
	words = append(words, strings.Fields(nonSlug.ReplaceAllString(strings.ToLower(text), " "))...)
	slug := ""
	for _, w := range words {
		if len(slug)+len(w) > 32 {
			break
		}
		if slug != "" {
			slug += "-"
		}
		slug += w
	}
	if len(slug) < 3 {
		slug = now.Format("0102-1504")
	}
	return "run/" + slug
}

// Create starts a run: a worktree on its branch, its directory there,
// its entry in the sidebar, and its runner, which takes it from there.
func Create(o Options) (*state.Run, error) {
	root, isGit := paths.ProjectRoot(paths.Expand(o.Project))
	if !isGit {
		return nil, fmt.Errorf("%s is not a git repository", paths.Short(root))
	}
	flow, ok := FlowByName(root, o.Flow)
	if !ok {
		return nil, fmt.Errorf("no flow %q (mad run flow ls)", o.Flow)
	}
	task := strings.TrimSpace(o.Task)
	if task == "" {
		return nil, errors.New("the run needs a task")
	}
	if o.Permission == "" {
		o.Permission = PermAuto
	}
	for role, s := range o.Agents {
		if _, ok := flow.Role(role); !ok {
			return nil, fmt.Errorf("flow %s has no role %q", flow.Name, role)
		}
		if k := ParseChoice(s).Kind; k != "claude" && k != "codex" {
			return nil, fmt.Errorf("%s: unknown agent %q", role, s)
		}
	}
	branch := strings.TrimSpace(o.Branch)
	if branch == "" {
		branch = BranchFor(task, time.Now())
	}
	if err := git.ValidBranch(branch); err != nil {
		return nil, err
	}
	dir := git.WorktreeDir(root, branch)
	if err := git.AddWorktree(root, branch, dir); err != nil {
		return nil, err
	}
	base, err := gitOut(dir, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	excludeRuns(root)

	name := strings.ReplaceAll(strings.TrimPrefix(branch, "run/"), "/", "-")
	r := &state.Run{ID: newID(), Name: name, Dir: dir, Branch: branch, CreatedAt: time.Now()}
	rd := Dir(r)
	if _, err := os.Stat(filePath(rd)); err == nil {
		return nil, fmt.Errorf("%s already has a run; pick another branch", branch)
	}
	if err := os.MkdirAll(rd, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(rd, "task.md"), []byte(task+"\n"), 0o644); err != nil {
		return nil, err
	}
	f := &File{
		Spec: Spec{Flow: flow.Name, Definition: &flow, Task: task, Base: base, Gate: o.Gate,
			MaxRounds: DefaultMaxRounds, Budget: DefaultBudget, Compact: DefaultCompact,
			Permission: o.Permission, Agents: o.Agents},
		Progress: Progress{Status: Starting, Round: 1},
	}
	if err := f.Save(rd); err != nil {
		return nil, err
	}
	appendLog(rd, "run created: "+task, time.Now())

	if err := drive.EnsureDeck(root); err != nil {
		return nil, err
	}
	st, err := state.Load()
	if err != nil {
		return nil, err
	}
	p, _ := st.AddProject(root)
	p.Runs = append(p.Runs, r)
	p.Collapsed = false
	if err := st.Save(); err != nil {
		return nil, err
	}
	if err := deck.StartRunner(r.ID); err != nil {
		return nil, err
	}
	_ = poke.Send(poke.Poll)
	return r, nil
}

func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// excludeRuns keeps runs' directories out of git status, through
// .git/info/exclude, which every worktree of the repository shares.
func excludeRuns(root string) {
	const pattern = ".mad/runs/"
	if exec.Command("git", "-C", root, "check-ignore", "-q", ".mad/runs/x").Run() == nil {
		return
	}
	common, err := gitOut(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return
	}
	path := filepath.Join(common, "info", "exclude")
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), pattern) {
		return
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, append(data, pattern+"\n"...), 0o644)
}
