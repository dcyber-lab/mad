package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// The model is driven by messages only; commands it returns (tmux work) are
// never run, so these tests need no tmux.

func setup(t *testing.T, projects ...string) (*model, *state.State) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	st := &state.State{}
	for _, p := range projects {
		st.AddProject(p)
	}
	old := usableDir
	usableDir = func(dir string) bool { fi, err := os.Stat(dir); return err == nil && fi.IsDir() }
	t.Cleanup(func() { usableDir = old })
	m := newModel(st, agent.Builtin())
	m.Update(tea.WindowSizeMsg{Width: 32, Height: 30})
	return m, st
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

func rowNames(m *model) []string {
	var out []string
	for _, r := range m.rows {
		switch {
		case r.agent != nil:
			out = append(out, "  "+r.proj.DisplayName(r.agent))
		case r.ext != nil:
			out = append(out, "  ext:"+r.ext.Kind)
		case r.desktop > 0:
			out = append(out, "  desktop")
		default:
			out = append(out, r.proj.Name)
		}
	}
	return out
}

func assertRows(t *testing.T, m *model, want ...string) {
	t.Helper()
	if got := strings.Join(rowNames(m), "|"); got != strings.Join(want, "|") {
		t.Errorf("rows = %q\nwant   %q", got, strings.Join(want, "|"))
	}
}

func TestNavigationOnEmptyListDoesNotPanic(t *testing.T) {
	// Regression: moving on an empty list left the cursor at -1 and the
	// next rebuild indexed rows[-1].
	m, _ := setup(t)
	press(m, "j", "k", "G", "g")
	dir := t.TempDir()
	m.addPicked(dir)
	assertRows(t, m, filepath.Base(dir))
	if m.cursor != 0 {
		t.Errorf("cursor = %d", m.cursor)
	}
}

func TestNavigationAndBurstKeys(t *testing.T) {
	m, st := setup(t, "/p/one", "/p/two", "/p/three")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude"}, {ID: "a2", Kind: "claude"}}
	m.rebuildRows()
	assertRows(t, m, "one", "  claude", "  claude#2", "two", "three")

	press(m, "j", "j")
	if m.cursor != 2 {
		t.Errorf("after j j cursor = %d", m.cursor)
	}
	// Fast typing arrives as one multi-rune key.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kk")})
	if m.cursor != 0 {
		t.Errorf("after burst kk cursor = %d", m.cursor)
	}
	press(m, "G")
	if m.cursor != 4 {
		t.Errorf("G → %d", m.cursor)
	}
	press(m, "j")
	if m.cursor != 4 {
		t.Errorf("j at bottom → %d", m.cursor)
	}
}

func TestCollapseKeepsCursorOnProject(t *testing.T) {
	m, st := setup(t, "/p/one", "/p/two")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "codex"}}
	m.rebuildRows()
	press(m, "enter") // collapse "one"
	assertRows(t, m, "one", "two")
	if !st.Projects[0].Collapsed || m.cursor != 0 {
		t.Errorf("collapsed=%v cursor=%d", st.Projects[0].Collapsed, m.cursor)
	}
	if saved, _ := state.Load(); !saved.Projects[0].Collapsed {
		t.Error("collapse not saved")
	}
	press(m, "enter")
	assertRows(t, m, "one", "  codex", "two")
}

func TestKillAgentAndRemoveProject(t *testing.T) {
	m, st := setup(t, "/p/one", "/p/two")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude"}}
	m.rebuildRows()

	press(m, "j", "x")
	if m.mode != modeConfirm || !strings.Contains(m.confirmMsg, "kill claude") {
		t.Fatalf("mode=%v msg=%q", m.mode, m.confirmMsg)
	}
	press(m, "n") // declined
	if len(st.Projects[0].Agents) != 1 {
		t.Fatal("declined kill removed the agent")
	}
	press(m, "x", "y")
	if len(st.Projects[0].Agents) != 0 {
		t.Error("agent not removed")
	}

	press(m, "g", "x", "y")
	assertRows(t, m, "two")
	if !st.IsIgnored("/p/one") {
		t.Error("removed project should be ignored by auto-sync")
	}
}

func TestPollTracksStageStatusAndSessions(t *testing.T) {
	m, st := setup(t, "/p/one")
	a := &state.Agent{ID: "a1", Kind: "claude", SessionID: "old", Fork: true}
	b := &state.Agent{ID: "a2", Kind: "codex"}
	st.Projects[0].Agents = []*state.Agent{a, b}
	m.rebuildRows()

	now := time.Now()
	m.applyPoll(pollMsg{
		panes: []tmux.Pane{
			{ID: "%1", MadID: tmux.IDSidebar, Session: tmux.MainSession, Index: 0, Active: true},
			{ID: "%2", MadID: "a2", Session: tmux.MainSession, Index: 1},
			{ID: "%3", MadID: "a1", Session: tmux.PoolSession, Index: -1},
		},
		screens: map[string]string{"a1": "x", "a2": "y"},
		hooks:   map[string]*status.Hook{"a1": {State: status.Waiting, SessionID: "forked", At: now}},
	}, now)

	if m.stageID != "a2" || !m.focused {
		t.Errorf("stage=%q focused=%v", m.stageID, m.focused)
	}
	if r, _ := m.current(); r.agent != b {
		t.Error("cursor should follow the agent put on stage")
	}
	if got := m.trackers["a1"].Status; got != status.Waiting {
		t.Errorf("a1 status = %s", got)
	}
	// The fork reported its own session id: remembered, no longer a fork.
	if a.SessionID != "forked" || a.Fork {
		t.Errorf("agent after hook = %+v", a)
	}
	if saved, _ := state.Load(); saved.Projects[0].Agents[0].SessionID != "forked" {
		t.Error("session id not saved")
	}

	// A pane that disappeared reads as stopped.
	m.applyPoll(pollMsg{}, now.Add(time.Second))
	if got := m.trackers["a1"].Status; got != status.Stopped {
		t.Errorf("no pane → %s", got)
	}
}

func TestExternalsAutoSyncAndRows(t *testing.T) {
	m, st := setup(t, "/p/one")
	proj := t.TempDir()
	ignored := t.TempDir()
	st.Ignored = []string{ignored}

	m.Update(externalsMsg{
		{PID: 1, Kind: "codex", TTY: "ttys001", Root: "/p/one"},
		{PID: 2, Kind: "claude", Root: proj, Desktop: true},
		{PID: 3, Kind: "claude", Root: proj, Desktop: true},
		{PID: 4, Kind: "claude", Root: ignored},
		{PID: 5, Kind: "claude", Root: "/does/not/exist"},
	})
	assertRows(t, m, "one", "  ext:codex", filepath.Base(proj), "  desktop")
	if len(m.rows) != 4 {
		t.FailNow()
	}
	if r := m.rows[3]; r.desktop != 2 {
		t.Errorf("desktop count = %d", r.desktop)
	}
	if !strings.Contains(m.flash, filepath.Base(proj)) {
		t.Errorf("flash = %q", m.flash)
	}

	// External rows can't be killed; enter asks before taking over.
	press(m, "j", "x")
	if m.mode != modeNormal {
		t.Error("x on an external row should do nothing")
	}
	press(m, "enter")
	if m.mode != modeConfirm || !strings.Contains(m.confirmMsg, "take over codex from ttys001") {
		t.Errorf("confirm = %q", m.confirmMsg)
	}
	press(m, "y")
	if len(st.Projects[0].Agents) != 1 || st.Projects[0].Agents[0].Kind != "codex" {
		t.Errorf("adopted agent = %+v", st.Projects[0].Agents)
	}
	assertRows(t, m, "one", "  codex", filepath.Base(proj), "  desktop")
}

func TestPicker(t *testing.T) {
	m, st := setup(t, "/code/added")
	old := scanHistory
	scanHistory = func() []discover.Candidate { return nil }
	defer func() { scanHistory = old }()

	now := time.Now()
	m.externals = []discover.External{{Kind: "claude", Root: "/code/open-now", Desktop: true}}
	press(m, "a")
	if m.mode != modeAddProject {
		t.Fatal("picker not open")
	}
	m.Update(historyMsg{
		{Path: "/code/cdn-api", LastUsed: now.Add(-time.Hour), Sources: []string{"codex"}},
		{Path: "/code/cdn-de", LastUsed: now.Add(-24 * time.Hour)},
		{Path: "/code/ndre-bot", LastUsed: now.Add(-time.Minute)},
		{Path: "/code/added", LastUsed: now},
	})
	pickPaths := func() string {
		var out []string
		for _, it := range m.pk.items {
			out = append(out, filepath.Base(it.path))
		}
		return strings.Join(out, ",")
	}
	// Running first, then most recent; already-added projects are hidden.
	if got := pickPaths(); got != "open-now,ndre-bot,cdn-api,cdn-de" {
		t.Errorf("items = %s", got)
	}

	for _, r := range "cdn" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := pickPaths(); got != "cdn-api,cdn-de" {
		t.Errorf("filtered = %s", got)
	}
	if v := m.View(); !strings.Contains(v, "/code/cdn-api · codex") {
		t.Errorf("selection path/sources missing from view:\n%s", v)
	}
	press(m, "esc")
	if m.mode != modeNormal || len(st.Projects) != 1 {
		t.Error("esc should cancel without adding")
	}
}

func TestPickerPathModeAndAdd(t *testing.T) {
	m, st := setup(t)
	base := t.TempDir()
	for _, d := range []string{"alpha", "beta"} {
		_ = os.MkdirAll(filepath.Join(base, d), 0o755)
	}
	press(m, "a")
	m.input.SetValue(base + "/al")
	m.refreshPicker()
	if len(m.pk.items) != 1 || !m.pk.items[0].isDir {
		t.Fatalf("completion = %+v", m.pk.items)
	}
	press(m, "enter")
	if len(st.Projects) != 1 || st.Projects[0].Name != "alpha" {
		t.Errorf("projects = %+v", st.Projects)
	}

	// A non-directory is refused with a message.
	press(m, "a")
	m.addPicked(filepath.Join(base, "missing"))
	if !strings.Contains(m.flash, "not a directory") || len(st.Projects) != 1 {
		t.Errorf("flash=%q projects=%d", m.flash, len(st.Projects))
	}
}

func TestPickerMouseRowMapping(t *testing.T) {
	m, st := setup(t)
	dirs := []string{t.TempDir(), t.TempDir()}
	press(m, "a")
	m.Update(historyMsg{{Path: dirs[0], LastUsed: time.Now()}, {Path: dirs[1], LastUsed: time.Now().Add(-time.Hour)}})
	// Rows start right under title, input and rule.
	m.Update(tea.MouseMsg{X: 3, Y: pickerHeader + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if len(st.Projects) != 1 || st.Projects[0].Path != dirs[1] {
		t.Errorf("clicked second row, added %+v", st.Projects)
	}
}

func TestSessionPicker(t *testing.T) {
	m, st := setup(t, "/code/app")
	proj := st.Projects[0]
	m.externals = []discover.External{
		{Kind: "claude", SessionID: "c-live", Desktop: true},
		{Kind: "codex", SessionID: "x-live", TTY: "ttys9"},
	}
	sessions := []discover.Session{
		{Kind: "claude", ID: "c-live", Title: "open in desktop", Origin: "desktop", Updated: time.Now()},
		{Kind: "claude", ID: "c-old", Title: "批量解决仓库 issues", Origin: "cli", Updated: time.Now().Add(-time.Hour)},
	}

	open := func(kind string, list []discover.Session) {
		press(m, "g", "n", map[string]string{"claude": "1", "codex": "2"}[kind])
		if m.mode != modePickSession || m.sp.kind != kind {
			t.Fatalf("mode=%v kind=%q", m.mode, m.sp.kind)
		}
		m.Update(sessionsMsg{root: proj.Path, kind: kind, list: list})
	}

	// Row 0 starts fresh.
	open("claude", sessions)
	press(m, "enter")
	if a := proj.Agents[len(proj.Agents)-1]; a.SessionID != "" || a.Kind != "claude" {
		t.Errorf("new session agent = %+v", a)
	}

	// A session open in the desktop app continues as a fork.
	open("claude", sessions)
	press(m, "down", "enter")
	if a := proj.Agents[len(proj.Agents)-1]; a.SessionID != "c-live" || !a.Fork {
		t.Errorf("live claude session = %+v", a)
	}

	open("claude", sessions)
	press(m, "down", "down", "enter")
	if a := proj.Agents[len(proj.Agents)-1]; a.SessionID != "c-old" || a.Fork {
		t.Errorf("past session = %+v", a)
	}

	// A codex thread still open elsewhere is refused.
	n := len(proj.Agents)
	open("codex", []discover.Session{{Kind: "codex", ID: "x-live", Title: "t", Updated: time.Now()}})
	press(m, "down", "enter")
	if len(proj.Agents) != n || !strings.Contains(m.flash, "close it there first") {
		t.Errorf("agents=%d flash=%q", len(proj.Agents), m.flash)
	}

	// Stale results for another project are ignored.
	open("claude", nil)
	m.Update(sessionsMsg{root: "/elsewhere", kind: "claude", list: sessions})
	if len(m.sp.items) != 0 {
		t.Error("results for another project were shown")
	}
}

func TestViewFitsWidth(t *testing.T) {
	m, st := setup(t, "/code/a-project-with-a-very-long-name", "/code/中文项目名称很长很长很长")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude"}, {ID: "a2", Kind: "a-very-long-agent-kind"}}
	m.rebuildRows()
	m.externals = []discover.External{{PID: 1, Kind: "codex", TTY: "ttys001", Root: st.Projects[1].Path}}
	m.rebuildRows()

	check := func(view, what string) {
		t.Helper()
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > m.width {
				t.Errorf("%s line %d is %d wide (> %d): %q", what, i, w, m.width, line)
			}
		}
	}
	check(m.View(), "sidebar")

	press(m, "a")
	m.Update(historyMsg{{Path: "/code/中文项目名称很长很长很长很长很长", LastUsed: time.Now().Add(-48 * time.Hour)}})
	check(m.View(), "picker")
	press(m, "esc")

	press(m, "g", "n", "1")
	m.Update(sessionsMsg{root: st.Projects[0].Path, kind: "claude", list: []discover.Session{
		{Kind: "claude", ID: "s", Title: strings.Repeat("会话标题", 20), Updated: time.Now()},
	}})
	check(m.View(), "sessions")

	// The narrowest sidebar still never wraps.
	m.Update(tea.WindowSizeMsg{Width: 16, Height: 30})
	check(m.View(), "narrow sessions")
	press(m, "esc")
	check(m.View(), "narrow sidebar")
	press(m, "a")
	check(m.View(), "narrow picker")
}
