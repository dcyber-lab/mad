package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// Run statuses.
const (
	Starting  = "starting"
	Running   = "running"
	Waiting   = "waiting" // for you: Progress.Waiting says why
	Done      = "done"
	Cancelled = "cancelled"
	Failed    = "failed"
)

// Active: the run still has work to do, and a runner to do it.
func Active(status string) bool {
	return status == Starting || status == Running || status == Waiting
}

// RelDir is a run's directory inside its worktree.
func RelDir(r *state.Run) string { return filepath.Join(".mad", "runs", r.Name) }

// Dir is where run r keeps its files.
func Dir(r *state.Run) string { return filepath.Join(r.Dir, RelDir(r)) }

// Spec is what a run was asked to do.
type Spec struct {
	Flow string `json:"flow"`
	// Definition is the flow as it was when the run started, which the
	// run follows whatever becomes of its file.
	Definition *Flow  `json:"definition,omitempty"`
	Task       string `json:"task"`
	Base       string `json:"base"` // the commit it started from
	// Gate stops after the steps that offer one (the design) for you.
	Gate bool `json:"gate,omitempty"`
	// MaxRounds bounds a review that says none of its own.
	MaxRounds int `json:"max_rounds"`
	// Budget is what its claude agents may cost, in USD; past it the run
	// waits for you.
	Budget float64 `json:"budget"`
	// Compact: a claude role whose context is past this many tokens is
	// compacted before its next step.
	Compact int64 `json:"compact"`
	// Permission is what its claude agents may do unasked (Permissions).
	Permission string `json:"permission,omitempty"`
	// Agents are the agents chosen for the flow's roles, role name →
	// "kind" or "kind:model"; a role not named keeps the flow's.
	Agents map[string]string `json:"agents,omitempty"`
}

// Entry is one step taken.
type Entry struct {
	Step   string    `json:"step"`
	Role   string    `json:"role"`
	Round  int       `json:"round"`
	Status string    `json:"status"` // running, done, approve, changes
	Sent   bool      `json:"sent,omitempty"`
	Result string    `json:"result,omitempty"` // what it came to, one line
	Reply  string    `json:"reply,omitempty"`  // the file with the whole reply
	Start  time.Time `json:"start"`
	End    time.Time `json:"end,omitzero"`
	Cost   float64   `json:"cost,omitempty"`   // USD (claude)
	Tokens int64     `json:"tokens,omitempty"` // tokens (codex)
}

// Progress is how far a run got.
type Progress struct {
	Status  string `json:"status"`
	Step    int    `json:"step"`  // index into the flow's steps
	Round   int    `json:"round"` // of review, from 1
	Waiting string `json:"waiting,omitempty"`
	// Review is what the last review found, for the next fix.
	Review  string           `json:"review,omitempty"`
	Entries []Entry          `json:"entries"`
	Cost    float64          `json:"cost"`
	Tokens  int64            `json:"tokens"`
	Context map[string]int64 `json:"context,omitempty"` // role → tokens in its context
	Started time.Time        `json:"started,omitzero"`
	Ended   time.Time        `json:"ended,omitzero"`
}

// File is run.json.
type File struct {
	Spec     Spec     `json:"spec"`
	Progress Progress `json:"progress"`
}

// Flow is the flow the run follows, its roles played by the agents
// chosen for them. A run from before runs kept their flow follows the
// built-in one of its name.
func (f *File) Flow() Flow {
	var fl Flow
	if f.Spec.Definition != nil {
		fl = f.Spec.Definition.resolved()
	} else {
		fl, _ = FlowByName("", f.Spec.Flow)
	}
	return fl.WithAgents(f.Spec.Agents)
}

func filePath(dir string) string { return filepath.Join(dir, "run.json") }

// Load reads a run's run.json.
func Load(dir string) (*File, error) {
	data, err := os.ReadFile(filePath(dir))
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %v", filePath(dir), err)
	}
	return &f, nil
}

func (f *File) Save(dir string) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(filePath(dir), data)
}

// Commands from the sidebar to a runner.
const (
	Continue = "continue"
	Cancel   = "cancel"
)

func cmdPath(dir string) string { return filepath.Join(dir, "cmd") }

// Command leaves cmd for run r's runner, which takes it within a second.
func Command(r *state.Run, cmd string) error {
	return paths.WriteFileAtomic(cmdPath(Dir(r)), []byte(cmd))
}

// takeCommand returns the command left for the runner, and removes it.
func takeCommand(dir string) string {
	data, err := os.ReadFile(cmdPath(dir))
	if err != nil {
		return ""
	}
	os.Remove(cmdPath(dir))
	return strings.TrimSpace(string(data))
}

func logPath(dir string) string { return filepath.Join(dir, "log") }

// appendLog adds a line to the run's log.
func appendLog(dir, line string, now time.Time) {
	f, err := os.OpenFile(logPath(dir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", now.Format("15:04:05"), line)
}

// logTail is the last n lines of the run's log.
func logTail(dir string, n int) []string {
	data, err := os.ReadFile(logPath(dir))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
