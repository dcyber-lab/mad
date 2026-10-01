package deck

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/notify"
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

func TestSwitchTarget(t *testing.T) {
	st := &state.State{}
	st.AddProject("/code/a")
	st.AddProject("/code/b")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1"}, {ID: "a2"}}
	st.Projects[1].Agents = []*state.Agent{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}}
	cases := []struct{ arg, stage, want string }{
		{"2", "b1", "b2"}, // N counts within the project on stage
		{"3", "b1", "b3"},
		{"3", "a1", ""},          // project a has two agents
		{"2", "", "a2"},          // nothing on stage: the first project
		{"2", tmux.IDTask, "a2"}, // nor an agent
		{"next", "a2", "b1"},     // next and prev cross projects
		{"prev", "b1", "a2"},
	}
	for _, c := range cases {
		got := ""
		if a, ok := switchTarget(st, c.arg, c.stage); ok {
			got = a.ID
		}
		if got != c.want {
			t.Errorf("switch %s with %q on stage → %q, want %q", c.arg, c.stage, got, c.want)
		}
	}
}

// Numbers count within the run on stage, its panel included.
func TestSwitchTargetInRun(t *testing.T) {
	st := &state.State{}
	p, _ := st.AddProject("/p")
	top, r1, r2 := &state.Agent{ID: "top"}, &state.Agent{ID: "r1", Run: "run"}, &state.Agent{ID: "r2", Run: "run"}
	p.Agents = []*state.Agent{top, r1, r2}
	p.Runs = []*state.Run{{ID: "run", Name: "x"}}
	for _, c := range []struct{ arg, stage, want string }{
		{"2", "r1", "r2"},
		{"1", "top", "top"},
		{"2", "top", ""}, // the project's own: only one
		{"2", RunnerID("run"), "r2"},
		{"next", "top", "r1"}, // wraps, in sidebar order: the run first
	} {
		a, ok := switchTarget(st, c.arg, c.stage)
		got := ""
		if ok {
			got = a.ID
		}
		if got != c.want {
			t.Errorf("switch %s with %s on stage = %q, want %q", c.arg, c.stage, got, c.want)
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

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if c, err := LoadConfig(); err != nil || !reflect.DeepEqual(c, DefaultConfig()) || !c.Quota || c.SleepAfter != time.Hour {
		t.Errorf("no file: %+v %v", c, err)
	}
	write := func(body string) {
		os.MkdirAll(filepath.Join(dir, "mad"), 0o755)
		os.WriteFile(filepath.Join(dir, "mad", "config.json"), []byte(body), 0o644)
	}
	write(`{"notify": {"on": []}, "diff": {"command": "x {dir}"},
		"finish": [{"name": "x", "command": "y"}], "quota": false, "sleep": {"after": "90m"}}`)
	c, err := LoadConfig()
	if err != nil || c.Notify.Wants(notify.Done) || c.Diff.Command != "x {dir}" ||
		len(c.Finish) != 1 || c.Finish[0].Name != "x" || c.Quota || c.SleepAfter != 90*time.Minute {
		t.Errorf("full file: %+v %v", c, err)
	}
	for _, bad := range []string{`"soon"`, `"-1h"`, `"60"`} {
		write(`{"sleep": {"after": ` + bad + `}}`)
		if c, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "sleep.after") || c.SleepAfter != time.Hour {
			t.Errorf("sleep.after %s: %+v %v", bad, c, err)
		}
	}
	write(`{"sleep": {"after": "0"}}`)
	if c, err := LoadConfig(); err != nil || c.SleepAfter != 0 {
		t.Errorf("sleep.after 0: %+v %v", c, err)
	}
	// A typo is reported with its line, not silently read as defaults.
	write("{\n  \"quota\": false,\n}")
	if c, err := LoadConfig(); err == nil || !strings.HasPrefix(err.Error(), "config.json:3:") || !c.Quota {
		t.Errorf("broken file: %+v %v", c, err)
	}
}

func TestTmuxConfigBindings(t *testing.T) {
	conf := TmuxConfig()
	for _, want := range []string{
		"set -g remain-on-exit on",
		"set -g mouse on",
		"set -sq extended-keys on",
		`set -asq terminal-features ",xterm*:extkeys"`,
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
	// Shift+Enter: tmux 3.2 and later tell it from Enter when asked to.
	if out, err := tmux.Out("show-options", "-s", "extended-keys"); err != nil || !strings.Contains(out, "on") {
		t.Errorf("extended-keys = %q, %v", out, err)
	}
	if out, err := tmux.Out("show-options", "-s", "terminal-features"); err != nil || !strings.Contains(out, "xterm*:extkeys") {
		t.Errorf("terminal-features = %q, %v", out, err)
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

// Claude resumes only a session its provider finds a transcript of; a
// fresh agent that never got a message starts over under its own id.
func TestResumeAsksProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	p := &state.Project{Path: "/work/app"}
	a := &state.Agent{ID: "id-1", Kind: "claude"}
	kinds := agent.Builtin()

	got := command(p, a, true, kinds)
	if !strings.HasSuffix(got, "--session-id id-1") {
		t.Errorf("never written = %q", got)
	}
	if !strings.Contains(got, "--settings '"+filepath.Join(home, ".config", "mad", "claude-settings.json")+"'") {
		t.Errorf("{claude_settings} not filled: %q", got)
	}
	file := filepath.Join(home, ".claude", "projects", "-work-app", "id-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := command(p, a, true, kinds); !strings.HasSuffix(got, "--resume id-1") {
		t.Errorf("written = %q", got)
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

func TestViews(t *testing.T) {
	useDeck(t)
	if err := tmux.Run("resize-window", "-t", tmux.MainSession+":0", "-x", "220", "-y", "50"); err != nil {
		t.Fatal(err)
	}
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	for i := 1; i <= 5; i++ {
		p.Agents = append(p.Agents, &state.Agent{ID: fmt.Sprintf("agent-%d", i), Kind: "fake"})
	}
	ids := func() []string {
		var out []string
		for _, v := range tmux.Views(panes(t)) {
			out = append(out, v.MadID)
		}
		return out
	}
	// Onto an empty stage, a view takes the placeholder's place.
	if err := OpenAgentView(st, "agent-1", fakeKind, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"agent-2", "agent-3", "agent-4"} {
		if err := OpenAgentView(st, id, fakeKind, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(ids(), " "); got != "agent-1 agent-2 agent-3 agent-4" {
		t.Fatalf("views = %s", got)
	}
	// Two rows of two, right of the sidebar at its width.
	var sb tmux.Pane
	for _, pn := range panes(t) {
		if pn.Session == tmux.MainSession && pn.Index == 0 {
			sb = pn
		}
	}
	if sb.Width != DefaultSidebarWidth {
		t.Errorf("sidebar width = %d", sb.Width)
	}
	for _, v := range tmux.Views(panes(t)) {
		if v.Height < 23 || v.Height > 25 || v.Width < 90 {
			t.Errorf("view %s is %dx%d", v.MadID, v.Width, v.Height)
		}
	}
	if err := OpenAgentView(st, "agent-5", fakeKind, true); !errors.Is(err, ErrFullStage) {
		t.Errorf("a fifth view: %v", err)
	}
	// The one in use is the focused one; opening another puts it there.
	a2, _ := tmux.FindPane(panes(t), "agent-2")
	tmux.Run("select-pane", "-t", a2.ID)
	if err := OpenAgent(st, "agent-5", fakeKind); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids(), " "); got != "agent-1 agent-5 agent-3 agent-4" {
		t.Errorf("views after open = %s", got)
	}
	// Opening one on stage only focuses it.
	if err := OpenAgent(st, "agent-3", fakeKind); err != nil {
		t.Fatal(err)
	}
	if s, _ := tmux.Stage(panes(t)); s.MadID != "agent-3" || len(ids()) != 4 {
		t.Errorf("stage = %s, views %v", s.MadID, ids())
	}
	// Closing views sends them back to the pool, still running; the last
	// gives way to the placeholder.
	for _, id := range []string{"agent-5", "agent-3", "agent-4"} {
		if err := CloseView(id); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(ids(), " "); got != "agent-1" {
		t.Errorf("views after closing = %s", got)
	}
	if v := tmux.Views(panes(t)); len(v) == 1 && v[0].Width != 220-DefaultSidebarWidth-1 {
		t.Errorf("a lone view is %d wide", v[0].Width)
	}
	if a3, _ := tmux.FindPane(panes(t), "agent-3"); a3.Session != tmux.PoolSession || a3.Dead {
		t.Errorf("closed view = %+v", a3)
	}
	if err := CloseView("agent-1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids(), " "); got != tmux.IDPlaceholder {
		t.Errorf("views after closing all = %s", got)
	}
	// Killing an agent in one of several views closes that view.
	OpenAgentView(st, "agent-1", fakeKind, true)
	OpenAgentView(st, "agent-2", fakeKind, true)
	if err := KillAgent("agent-1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids(), " "); got != "agent-2" {
		t.Errorf("views after kill = %s", got)
	}
	// A run's views take the whole stage, and give it back.
	if err := ShowViews(st, []string{"agent-3", "agent-4", "agent-5"}, fakeKind, true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids(), " "); got != "agent-3 agent-4 agent-5" {
		t.Errorf("views shown = %s", got)
	}
	if s, _ := tmux.Stage(panes(t)); s.MadID != "agent-3" {
		t.Errorf("focus after ShowViews = %s", s.MadID)
	}
	if err := ShowViews(st, []string{"agent-3"}, fakeKind, true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids(), " "); got != "agent-3" {
		t.Errorf("views back to one = %s", got)
	}
}

func TestStageLayout(t *testing.T) {
	// Checksum and shape as tmux itself writes them.
	if got := stageLayout(200, 50, 30, []int{0, 1}); got != "fab5,200x50,0,0{30x50,0,0,0,169x50,31,0,1}" {
		t.Errorf("one view: %s", got)
	}
	for _, c := range []struct {
		w, views int
		want     string
	}{
		{120, 2, "[89x19,31,0,2,89x20,31,20,3]"}, // too narrow for two abreast
		{200, 2, "{84x50,31,0,2,84x50,116,0,3}"},
		{200, 3, "[169x24,31,0{84x24,31,0,2,84x24,116,0,3},169x25,31,25,4]"}, // the odd one out spans its row
		{200, 4, "[169x24,31,0{84x24,31,0,2,84x24,116,0,3},169x25,31,25{84x25,31,25,4,84x25,116,25,5}]"},
		{260, 3, "{75x50,31,0,2,75x50,107,0,3,77x50,183,0,4}"}, // wide enough for three abreast
	} {
		ids := []int{1}
		for i := range c.views {
			ids = append(ids, i+2)
		}
		h := 50
		if c.w == 120 {
			h = 40
		}
		if got := stageLayout(c.w, h, 30, ids); !strings.Contains(got, c.want) {
			t.Errorf("%d views in %d columns: %s, want %s", c.views, c.w, got, c.want)
		}
	}
}
