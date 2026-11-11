package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/discover"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
)

// The hook reads a run agent's role from the run's file: a push is
// refused to any role, an edit in the worktree to one that edits nothing.
func TestRunHookRefuses(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	root := filepath.Join(home, "proj")
	r := &state.Run{ID: "r1", Name: "x", Dir: filepath.Join(root, ".claude", "worktrees", "run-x")}
	if err := os.MkdirAll(madrun.Dir(r), 0o755); err != nil {
		t.Fatal(err)
	}
	fl, err := madrun.Parse([]byte(`{"name":"f","roles":[{"name":"builder","agent":"claude"},{"name":"prober","agent":"claude","no_edits":true}],
		"steps":[{"name":"s","role":"builder","prompt":"p"},{"name":"r","role":"prober","prompt":"q","review":true,"back":"s"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := (&madrun.File{Spec: madrun.Spec{Flow: "f", Definition: &fl}}).Save(madrun.Dir(r)); err != nil {
		t.Fatal(err)
	}
	if err := state.Update(func(st *state.State) error {
		p, _ := st.AddProject(root)
		p.Runs = append(p.Runs, r)
		p.Agents = append(p.Agents, &state.Agent{ID: "b", Kind: "claude", Name: "builder", Run: "r1", Role: "builder"},
			&state.Agent{ID: "p", Kind: "claude", Name: "prober", Run: "r1", Role: "prober"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	inTree := filepath.Join(r.Dir, "a.go")
	for _, c := range []struct {
		id   string
		tool discover.Tool
		want string
	}{
		{"b", discover.Tool{Name: "Write", Path: inTree}, ""},
		{"p", discover.Tool{Name: "Write", Path: inTree}, "the prober edits no file"},
		{"p", discover.Tool{Name: "Write", Path: filepath.Join(madrun.Dir(r), "notes.md")}, ""},
		{"b", discover.Tool{Name: "Bash", Command: "git push"}, "do not push"},
		{"b", discover.Tool{Name: "Bash", Command: "go test ./..."}, ""},
	} {
		got := outsideRun(c.id, &c.tool)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s %s%s: %q, want %q", c.id, c.tool.Path, c.tool.Command, got, c.want)
		}
	}
}
