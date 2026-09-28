// Package workspace starts work from a project's templates: a worktree on
// a base resolved to a commit, setup steps run in order, then one agent.
// Templates live in the project (.mad/workspaces.json); approving one is
// the user's, kept in mad's state directory and asked again whenever the
// template changes.
//
// A run is recorded step by step, so a failure says what was done (the
// worktree exists), what failed (a step, its exit code and log) and what
// was not (the agent never started). Nothing is cleaned up behind you.
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
)

type Step struct {
	ID             string   `json:"id"`
	Argv           []string `json:"argv"`
	Cwd            string   `json:"cwd,omitempty"` // relative to the worktree
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	Trigger        string   `json:"trigger,omitempty"` // checks: "manual" only, for now
}

func (s Step) String() string { return strings.Join(s.Argv, " ") }

// TimeoutLabel is the step's time limit as shown.
func (s Step) TimeoutLabel() string { return s.timeout().String() }

func (s Step) timeout() time.Duration {
	if s.TimeoutSeconds <= 0 {
		return 10 * time.Minute
	}
	return time.Duration(s.TimeoutSeconds) * time.Second
}

type Template struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace struct {
		Mode        string `json:"mode"` // "new-worktree"
		DefaultBase string `json:"default_base,omitempty"`
	} `json:"workspace"`
	Setup  []Step `json:"setup"`
	Launch struct {
		Agent string `json:"agent"`
	} `json:"launch"`
	Checks []Step `json:"checks,omitempty"`
}

// Fingerprint changes whenever anything the template would run changes.
func (t Template) Fingerprint() string {
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

type file struct {
	Version   int        `json:"version"`
	Templates []Template `json:"templates"`
}

// ConfigFile is where a project keeps its templates.
func ConfigFile(repo string) string { return filepath.Join(repo, ".mad", "workspaces.json") }

// Load reads repo's templates; none is not an error.
func Load(repo string) ([]Template, error) {
	var f file
	if err := paths.ReadJSON(ConfigFile(repo), &f); err != nil {
		return nil, err
	}
	var out []Template
	for _, t := range f.Templates {
		if t.ID == "" || len(t.Setup) > 0 && len(t.Setup[0].Argv) == 0 {
			continue
		}
		if t.Name == "" {
			t.Name = t.ID
		}
		out = append(out, t)
	}
	return out, nil
}

// ---- plan ----

// Plan is exactly what a run will do, fixed when you confirm it.
type Plan struct {
	Repo        string   `json:"repo"`
	Template    Template `json:"template"`
	Fingerprint string   `json:"fingerprint"`
	Base        string   `json:"base"`     // as typed
	BaseSHA     string   `json:"base_sha"` // what it resolved to
	Branch      string   `json:"branch"`
	BranchNew   bool     `json:"branch_new"` // false: an existing branch is checked out, Base unused
	Dir         string   `json:"dir"`
	Agent       string   `json:"agent"`
	RepoDirty   int      `json:"repo_dirty"` // uncommitted files in the main checkout: not carried over
	Approved    string   `json:"-"`          // "" never, "same", "changed"
}

// MakePlan resolves base and names the worktree for branch.
func MakePlan(repo string, t Template, base, branch, agentKind, dir string) (Plan, error) {
	p := Plan{Repo: repo, Template: t, Fingerprint: t.Fingerprint(), Base: base, Branch: branch, Dir: dir, Agent: agentKind}
	if p.Base == "" {
		p.Base = "HEAD"
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", p.Base+"^{commit}").Output()
	if err != nil {
		return p, fmt.Errorf("base %q is not a commit here", p.Base)
	}
	p.BaseSHA = strings.TrimSpace(string(out))
	p.BranchNew = exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() != nil
	if out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if l != "" && !strings.Contains(l, ".claude/worktrees") {
				p.RepoDirty++
			}
		}
	}
	p.Approved = approval(repo, t)
	return p, nil
}

// ShortSHA is the base commit as shown.
func (p Plan) ShortSHA() string {
	if len(p.BaseSHA) > 7 {
		return p.BaseSHA[:7]
	}
	return p.BaseSHA
}

// ---- approvals: the user's, never the repository's ----

func approvalsFile() string { return filepath.Join(paths.StateDir(), "template-approvals.json") }

func approvalKey(repo string, t Template) string { return repo + "#" + t.ID }

// approval is "" for a template never approved, "changed" when it
// changed since, "same" otherwise.
func approval(repo string, t Template) string {
	m := map[string]string{}
	_ = paths.ReadJSON(approvalsFile(), &m)
	fp, ok := m[approvalKey(repo, t)]
	switch {
	case !ok:
		return ""
	case fp != t.Fingerprint():
		return "changed"
	}
	return "same"
}

// Approve records that the user confirmed t as it is now.
func Approve(repo string, t Template) error {
	m := map[string]string{}
	_ = paths.ReadJSON(approvalsFile(), &m)
	m[approvalKey(repo, t)] = t.Fingerprint()
	data, _ := json.MarshalIndent(m, "", "  ")
	return paths.WriteFileAtomic(approvalsFile(), data)
}

// ---- runs ----

// Run states, in order; a run ends active, failed or cancelled.
const (
	Planned   = "planned"
	Creating  = "creating_workspace"
	Preparing = "preparing"
	Ready     = "ready"
	Launching = "launching"
	Active    = "active"
	Failed    = "failed"
	Cancelled = "cancelled"
)

type StepResult struct {
	ID    string    `json:"id"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end,omitzero"`
	Exit  int       `json:"exit"`
	Log   string    `json:"log"`
	Err   string    `json:"err,omitempty"`
}

func (r StepResult) OK() bool { return !r.End.IsZero() && r.Exit == 0 && r.Err == "" }

type Run struct {
	ID        string       `json:"id"`
	Plan      Plan         `json:"plan"`
	State     string       `json:"state"`
	Worktree  bool         `json:"worktree_created"`
	Steps     []StepResult `json:"steps"`
	Error     string       `json:"error,omitempty"`
	AgentID   string       `json:"agent_id,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func runsDir() string { return filepath.Join(paths.StateDir(), "runs") }

func runFile(id string) string { return filepath.Join(runsDir(), id+".json") }

// LogFile is where step id of run keeps its output.
func LogFile(runID, step string) string { return filepath.Join(runsDir(), runID, step+".log") }

func NewRun(id string, p Plan, now time.Time) *Run {
	return &Run{ID: id, Plan: p, State: Planned, CreatedAt: now, UpdatedAt: now}
}

func LoadRun(id string) (*Run, error) {
	var r Run
	if err := paths.ReadJSON(runFile(id), &r); err != nil {
		return nil, err
	}
	if r.ID == "" {
		return nil, fmt.Errorf("no run %s", id)
	}
	return &r, nil
}

func (r *Run) Save() error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(runFile(r.ID), data)
}

// NextStep is the index of the first setup step not yet done, or
// len(setup) when all are.
func (r *Run) NextStep() int {
	for i := range r.Plan.Template.Setup {
		if i >= len(r.Steps) || !r.Steps[i].OK() {
			return i
		}
	}
	return len(r.Plan.Template.Setup)
}

// FailedStep is the result of the step that failed, if any.
func (r *Run) FailedStep() (StepResult, bool) {
	for _, s := range r.Steps {
		if !s.End.IsZero() && !s.OK() {
			return s, true
		}
	}
	return StepResult{}, false
}

// Summary says what is done and what is not, for a failed run.
func (r *Run) Summary() string {
	var parts []string
	if r.Worktree {
		parts = append(parts, "worktree created")
	} else {
		parts = append(parts, "no worktree")
	}
	if s, ok := r.FailedStep(); ok {
		parts = append(parts, fmt.Sprintf("step %s failed (exit %d)", s.ID, s.Exit))
	} else if r.Error != "" {
		parts = append(parts, r.Error)
	}
	parts = append(parts, "agent not started")
	return strings.Join(parts, " · ")
}

// ErrChanged: the template on disk is no longer the one confirmed.
var ErrChanged = errors.New("the template changed since you confirmed it; review it again")

// Recheck refuses to go on when the template was edited after the plan.
func (p Plan) Recheck() error {
	ts, err := Load(p.Repo)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if t.ID == p.Template.ID {
			if t.Fingerprint() != p.Fingerprint {
				return ErrChanged
			}
			return nil
		}
	}
	return ErrChanged
}

// CreateWorktree checks the branch out in the plan's directory: a new
// branch starts at the resolved base commit, never at whatever the base
// name points to by now. An existing worktree there is reused.
func CreateWorktree(p Plan) error {
	base := ""
	if p.BranchNew {
		base = p.BaseSHA
	}
	return git.AddWorktreeFrom(p.Repo, p.Branch, p.Dir, base)
}

// RunStep runs setup step i of the plan in the worktree, its output in
// the step's log.
func RunStep(runID string, p Plan, i int, now func() time.Time) StepResult {
	s := p.Template.Setup[i]
	res := StepResult{ID: s.ID, Start: now(), Log: LogFile(runID, s.ID)}
	if err := os.MkdirAll(filepath.Dir(res.Log), 0o755); err != nil {
		res.Err, res.Exit, res.End = err.Error(), -1, now()
		return res
	}
	log, err := os.Create(res.Log)
	if err != nil {
		res.Err, res.Exit, res.End = err.Error(), -1, now()
		return res
	}
	defer log.Close()
	fmt.Fprintf(log, "$ %s\n  (in %s)\n\n", s, filepath.Join(p.Dir, s.Cwd))
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Argv[0], s.Argv[1:]...)
	cmd.Dir = filepath.Join(p.Dir, s.Cwd)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.Env = append(os.Environ(), "MAD_WORKSPACE="+p.Dir, "MAD_BRANCH="+p.Branch, "MAD_BASE_SHA="+p.BaseSHA)
	err = cmd.Run()
	res.End = now()
	var exit *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.Exit, res.Err = -1, fmt.Sprintf("timed out after %s", s.timeout())
	case errors.As(err, &exit):
		res.Exit = exit.ExitCode()
	case err != nil:
		res.Exit, res.Err = -1, err.Error()
	}
	fmt.Fprintf(log, "\n[mad] exit %d %s\n", res.Exit, res.Err)
	return res
}
