package deck

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestSwitchIndex(t *testing.T) {
	cases := []struct {
		arg     string
		cur, n  int
		want    int
		wantOK  bool
		comment string
	}{
		{"next", -1, 3, 0, true, "nothing on stage → first"},
		{"next", 2, 3, 0, true, "wraps around"},
		{"prev", -1, 3, 2, true, "nothing on stage → last"},
		{"prev", 0, 3, 2, true, "wraps around"},
		{"prev", 2, 3, 1, true, ""},
		{"2", 0, 3, 1, true, "1-based"},
		{"4", 0, 3, 0, false, "out of range"},
		{"0", 0, 3, 0, false, "out of range"},
		{"x", 0, 3, 0, false, "not a number"},
		{"next", -1, 0, 0, false, "no agents"},
	}
	for _, c := range cases {
		got, ok := SwitchIndex(c.arg, c.cur, c.n)
		if got != c.want || ok != c.wantOK {
			t.Errorf("SwitchIndex(%q, %d, %d) = %d, %v; want %d, %v (%s)",
				c.arg, c.cur, c.n, got, ok, c.want, c.wantOK, c.comment)
		}
	}
}

func TestSidebarWidth(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := SidebarWidth(); got != DefaultSidebarWidth {
		t.Errorf("default = %d", got)
	}
	for in, want := range map[int]int{42: 42, 3: MinSidebarWidth, 500: MaxSidebarWidth} {
		if err := SaveSidebarWidth(in); err != nil {
			t.Fatal(err)
		}
		if got := SidebarWidth(); got != want {
			t.Errorf("save %d → %d, want %d", in, got, want)
		}
	}
	_ = os.WriteFile(paths.SidebarWidthFile(), []byte("wide"), 0o644)
	if got := SidebarWidth(); got != DefaultSidebarWidth {
		t.Errorf("garbage file → %d", got)
	}
}

func TestClaudeSettings(t *testing.T) {
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(ClaudeSettings(), &s); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop"} {
		entries := s.Hooks[ev]
		if len(entries) != 1 || len(entries[0].Hooks) != 1 {
			t.Fatalf("%s: %+v", ev, entries)
		}
		h := entries[0].Hooks[0]
		if h.Type != "command" || !strings.HasSuffix(h.Command, " hook claude") {
			t.Errorf("%s hook = %+v", ev, h)
		}
	}
	if s.Hooks["PreToolUse"][0].Matcher != "*" {
		t.Error("tool hooks must match every tool")
	}
}

func TestTmuxConfigBindings(t *testing.T) {
	conf := TmuxConfig()
	for _, want := range []string{
		"set -g remain-on-exit on",
		"set -g mouse on",
		"bind -n M-s ",
		"bind -n M-1 run-shell -b ",
		"bind -n M-j run-shell -b ",
		"bind -n M-n run-shell -b ",
		"bind -n M-v run-shell -b ",
		"bind -r < resize-pane -t main:0.0 -L 2",
		"set-hook -g client-resized ",
		"set-hook -g pane-died ",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("config lacks %q", want)
		}
	}
}

func TestTmuxQuote(t *testing.T) {
	if got := tmuxQuote(`'/a b/mad' fit "$x"`); got != `"'/a b/mad' fit \"\$x\""` {
		t.Errorf("got %s", got)
	}
}

// ---- integration: a real tmux server on a throwaway socket ----

// useDeck isolates tmux, config and state, and makes the deck's own panes
// run sleep instead of this test binary.
func useDeck(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	t.Setenv("LC_ALL", "C") // non-UTF-8: tmux must still keep the tabs in -F output

	oldSocket, oldSelf := tmux.Socket, SelfCommand
	tmux.Socket = fmt.Sprintf("mad-deck-test-%d", time.Now().UnixNano())
	SelfCommand = func(sub string) string { return "sleep 600 # mad " + sub }
	t.Cleanup(func() {
		_ = tmux.Run("kill-server")
		tmux.Socket, SelfCommand = oldSocket, oldSelf
	})
	if err := WriteConfigs(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLayout(dir, 120, 40); err != nil {
		t.Fatal(err)
	}
}

func panes(t *testing.T) []tmux.Pane {
	t.Helper()
	ps, err := tmux.ListPanes()
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func stageID(t *testing.T) string {
	t.Helper()
	s, ok := tmux.Stage(panes(t))
	if !ok {
		t.Fatal("no stage")
	}
	return s.MadID
}

// fakeKind is an "agent" that prints its id and waits.
var fakeKind = []agent.Kind{{Name: "fake", Start: `sh -c 'echo "agent $MAD_AGENT_ID"; sleep 600'`}}

func TestConfigLoadsInTmux(t *testing.T) {
	useDeck(t)
	if err := tmux.Run("source-file", paths.TmuxConf()); err != nil {
		t.Fatalf("generated config doesn't parse: %v", err)
	}
	out, err := tmux.Out("show-options", "-g", "remain-on-exit")
	if err != nil || !strings.Contains(out, "on") {
		t.Errorf("remain-on-exit = %q, %v", out, err)
	}
}

func TestLayout(t *testing.T) {
	useDeck(t)
	ps := panes(t)
	sb, ok := tmux.FindPane(ps, tmux.IDSidebar)
	if !ok || sb.Session != tmux.MainSession || sb.Index != 0 || sb.Width != DefaultSidebarWidth {
		t.Errorf("sidebar = %+v", sb)
	}
	if got := stageID(t); got != tmux.IDPlaceholder {
		t.Errorf("stage = %q, want placeholder", got)
	}
	if _, ok := tmux.FindPane(ps, tmux.IDKeepalive); !ok {
		t.Error("pool keepalive missing")
	}

	// Re-running on an existing deck keeps the layout (sidebar respawned).
	if err := EnsureLayout("/", 120, 40); err != nil {
		t.Fatal(err)
	}
	if got := len(panes(t)); got != 3 {
		t.Errorf("panes after re-run = %d, want 3", got)
	}
}

func TestAgentLifecycle(t *testing.T) {
	useDeck(t)
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	a := &state.Agent{ID: "agent-1", Kind: "fake"}
	b := &state.Agent{ID: "agent-2", Kind: "fake"}
	p.Agents = []*state.Agent{a, b}

	// Opening a stopped agent starts it and swaps it into the stage.
	if err := OpenAgent(st, a.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != a.ID {
		t.Fatalf("stage = %q, want %s", got, a.ID)
	}
	if ph, _ := tmux.FindPane(panes(t), tmux.IDPlaceholder); ph.Session != tmux.PoolSession {
		t.Errorf("placeholder should move to the pool, is in %q", ph.Session)
	}
	waitFor(t, func() bool {
		pa, _ := tmux.FindPane(panes(t), a.ID)
		return strings.Contains(tmux.Capture(pa.ID), "agent agent-1")
	}, "MAD_AGENT_ID reaches the agent")

	// A second agent takes the stage; the first keeps running in the pool.
	if err := OpenAgent(st, b.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != b.ID {
		t.Errorf("stage = %q, want %s", got, b.ID)
	}
	if pa, _ := tmux.FindPane(panes(t), a.ID); pa.Session != tmux.PoolSession || pa.Dead {
		t.Errorf("agent-1 = %+v", pa)
	}

	// Back to the first one, still alive in the pool.
	if err := OpenAgent(st, a.ID, fakeKind); err != nil {
		t.Fatal(err)
	}

	// Killing the agent on stage puts the placeholder back.
	if err := KillAgent(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != tmux.IDPlaceholder {
		t.Errorf("stage after kill = %q", got)
	}
	if _, ok := tmux.FindPane(panes(t), a.ID); ok {
		t.Error("killed agent's pane still exists")
	}
	if err := KillAgent("never-started"); err != nil {
		t.Errorf("killing an unknown agent: %v", err)
	}
}

func TestDiffCommand(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(string) (string, error) { return "/usr/bin/lazygit", nil }
	if got := DiffCommand(DiffConfig{}, "/p/it's"); got != `lazygit -p '/p/it'\''s'` {
		t.Error(got)
	}
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if got := DiffCommand(DiffConfig{}, "/p"); !strings.HasPrefix(got, "cd '/p' && ") || !strings.Contains(got, "diff HEAD") {
		t.Error(got)
	}
	if got := DiffCommand(DiffConfig{Command: "tig -C {dir} status"}, "/p"); got != "tig -C '/p' status" {
		t.Error(got)
	}

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got := LoadDiffConfig(); got.Command != "" {
		t.Errorf("no config: %+v", got)
	}
	os.MkdirAll(filepath.Join(dir, "mad"), 0o755)
	os.WriteFile(filepath.Join(dir, "mad", "config.json"), []byte(`{"notify":{"on":[]},"diff":{"command":"x {dir}"}}`), 0o644)
	if got := LoadDiffConfig(); got.Command != "x {dir}" {
		t.Errorf("config: %+v", got)
	}
}

func TestFinishActions(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(string) (string, error) { return "/usr/bin/gh", nil }
	names := func(acts []FinishAction) string {
		var n []string
		for _, a := range acts {
			n = append(n, a.Name)
		}
		return strings.Join(n, ", ")
	}

	wt := Checkout{Dir: "/r/.claude/worktrees/f", Repo: "/r", Branch: "feat/x", Base: "main", RepoBranch: "main"}
	acts := FinishActions(nil, wt)
	if got := names(acts); got != "push & open PR, rebase onto main, merge into main, push" {
		t.Error(got)
	}
	if acts[2].Command != "git -C '/r' merge --no-edit 'feat/x'" || acts[1].Command != "git rebase 'main'" {
		t.Errorf("%+v", acts)
	}
	// The main checkout is elsewhere: no merge there.
	wt.RepoBranch = "other"
	if got := names(FinishActions(nil, wt)); got != "push & open PR, rebase onto main, push" {
		t.Error(got)
	}
	// On the base branch itself there is nothing to rebase or merge.
	if got := names(FinishActions(nil, Checkout{Dir: "/r", Repo: "/r", Branch: "main", Base: "main", RepoBranch: "main"})); got != "push & open PR, push" {
		t.Error(got)
	}
	// No gh: no PR.
	lookPath = func(string) (string, error) { return "", os.ErrNotExist }
	if got := names(FinishActions(nil, wt)); got != "rebase onto main, push" {
		t.Error(got)
	}
	// Configured actions replace the list; placeholders are filled in.
	cfg := []FinishAction{{"pr", "gh pr create --head {branch} --base {base}"}}
	if got := FinishActions(cfg, wt); len(got) != 1 || got[0].Command != "gh pr create --head 'feat/x' --base 'main'" {
		t.Errorf("%+v", got)
	}

	if got := HoldCommand("git push"); !strings.HasPrefix(got, "sh -c 'git push; s=$?;") || !strings.Contains(got, "read _") {
		t.Error(got)
	}

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "mad"), 0o755)
	os.WriteFile(filepath.Join(dir, "mad", "config.json"), []byte(`{"finish":[{"name":"x","command":"y"}]}`), 0o644)
	if got := LoadFinishConfig(); len(got) != 1 || got[0].Name != "x" {
		t.Errorf("config: %+v", got)
	}
}

func TestDiffView(t *testing.T) {
	useDeck(t)
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	a := &state.Agent{ID: "agent-1", Kind: "fake"}
	p.Agents = []*state.Agent{a}
	if err := OpenAgent(st, a.ID, fakeKind); err != nil {
		t.Fatal(err)
	}

	// The diff view takes the stage; the agent waits in the pool.
	if err := OpenTask(os.TempDir(), "sleep 600"); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != tmux.IDTask {
		t.Fatalf("stage = %q", got)
	}
	// Opening it again replaces the pane instead of piling up.
	if err := OpenTask(os.TempDir(), "sleep 600"); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, pn := range panes(t) {
		if pn.MadID == tmux.IDTask {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d diff panes", n)
	}

	// Closing puts the agent back and the pane is gone.
	if err := CloseTask(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != a.ID {
		t.Errorf("stage after close = %q", got)
	}
	if _, ok := tmux.FindPane(panes(t), tmux.IDTask); ok {
		t.Error("diff pane survived close")
	}
	if err := CloseTask(a.ID); err != nil {
		t.Errorf("closing when there is none: %v", err)
	}

	// Showing another pane over the diff view kills it too.
	if err := OpenTask(os.TempDir(), "sleep 600"); err != nil {
		t.Fatal(err)
	}
	if err := OpenAgent(st, a.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	if _, ok := tmux.FindPane(panes(t), tmux.IDTask); ok {
		t.Error("diff pane survived a switch")
	}
	// Closing with an unknown agent falls back to the placeholder.
	if err := OpenTask(os.TempDir(), "sleep 600"); err != nil {
		t.Fatal(err)
	}
	if err := CloseTask("gone"); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != tmux.IDPlaceholder {
		t.Errorf("stage = %q", got)
	}
}

func TestDeadAgentRestarts(t *testing.T) {
	useDeck(t)
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	a := &state.Agent{ID: "short-lived", Kind: "fake"}
	p.Agents = []*state.Agent{a}

	exits := []agent.Kind{{Name: "fake", Start: "true"}}
	if err := StartAgent(p, a, false, exits); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		pa, _ := tmux.FindPane(panes(t), a.ID)
		return pa.Dead
	}, "agent exits (pane kept by remain-on-exit)")

	if err := OpenAgent(st, a.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	pa, _ := tmux.FindPane(panes(t), a.ID)
	if pa.Dead || stageID(t) != a.ID {
		t.Errorf("dead agent should be respawned and shown: %+v", pa)
	}
}

func TestEnsureStageRecovers(t *testing.T) {
	useDeck(t)
	stage, _ := tmux.Stage(panes(t))
	if err := tmux.Run("kill-pane", "-t", stage.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := tmux.Stage(panes(t)); ok {
		t.Fatal("stage still there")
	}
	if err := EnsureStage(panes(t)); err != nil {
		t.Fatal(err)
	}
	if got := stageID(t); got != tmux.IDPlaceholder {
		t.Errorf("recovered stage = %q", got)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
