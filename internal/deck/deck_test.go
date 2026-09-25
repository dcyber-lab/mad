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
		"bind -n M-n send-keys -t main:0.0 d",
		"bind -r < resize-pane -t main:0.0 -L 2",
		"set-hook -g client-resized ",
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
