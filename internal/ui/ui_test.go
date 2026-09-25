package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/poke"
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
	t.Setenv("CODEX_HOME", "")
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

func TestSidebarGapsAndMouse(t *testing.T) {
	m, st := setup(t, "/code/a", "/code/b")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude"}}
	st.Projects[1].Agents = []*state.Agent{{ID: "b1", Kind: "codex"}}
	m.rebuildRows()
	// a, claude, (gap), b, codex
	if got := m.lineOf; len(got) != 4 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("lineOf = %v", got)
	}
	lines := strings.Split(m.View(), "\n")
	if strings.TrimSpace(lines[headerLines+2]) != "" || !strings.Contains(lines[headerLines+3], "b") {
		t.Errorf("no gap before the second project:\n%s", strings.Join(lines[:8], "\n"))
	}

	click := func(y int) {
		m.Update(tea.MouseMsg{X: 3, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}
	click(headerLines + 2) // the gap
	if m.cursor != 0 || st.Projects[1].Collapsed {
		t.Errorf("gap click: cursor=%d collapsed=%v", m.cursor, st.Projects[1].Collapsed)
	}
	click(headerLines + 3) // project b
	if m.cursor != 2 || !st.Projects[1].Collapsed {
		t.Errorf("project click: cursor=%d collapsed=%v", m.cursor, st.Projects[1].Collapsed)
	}
}

func TestSidebarScrollWithGaps(t *testing.T) {
	var projects []string
	for i := 0; i < 12; i++ {
		projects = append(projects, fmt.Sprintf("/code/p%02d", i))
	}
	m, st := setup(t, projects...)
	for i, p := range st.Projects {
		p.Agents = []*state.Agent{{ID: fmt.Sprint("a", i), Kind: "claude"}}
	}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 32, Height: 12})
	press(m, "G")
	if v := m.View(); !strings.Contains(v, "p11") || strings.Count(v, "\n") != 11 {
		t.Errorf("last row not visible or wrong height:\n%s", v)
	}
	press(m, "g")
	if v := m.View(); !strings.Contains(v, "p00") || m.offset != 0 {
		t.Errorf("offset=%d after g:\n%s", m.offset, v)
	}
}

func TestSidebarHeaderCounts(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{{ID: "w", Kind: "claude"}, {ID: "d", Kind: "claude"}, {ID: "i", Kind: "shell"}}
	m.rebuildRows()
	if top := strings.Split(m.View(), "\n")[0]; !strings.Contains(top, "3 agents") {
		t.Errorf("quiet header = %q", top)
	}
	m.trackers["w"] = &status.Tracker{Status: status.Waiting}
	m.trackers["d"] = &status.Tracker{Status: status.Idle, Attention: true}
	m.trackers["i"] = &status.Tracker{Status: status.Idle}
	top := strings.Split(m.View(), "\n")[0]
	if !strings.Contains(top, "?1") || !strings.Contains(top, "●1") || strings.Contains(top, "agents") {
		t.Errorf("header = %q", top)
	}
}

func TestNotifyEvents(t *testing.T) {
	m, st := setup(t, "/code/api")
	m.notifyCfg = notify.Default()
	bg := &state.Agent{ID: "bg", Kind: "claude"}
	onStage := &state.Agent{ID: "st", Kind: "claude"}
	st.Projects[0].Agents = []*state.Agent{bg, onStage}
	m.rebuildRows()

	t0 := time.Now()
	poll := func(sec int, hooks map[string]*status.Hook) []alert {
		now := t0.Add(time.Duration(sec) * time.Second)
		for _, h := range hooks {
			h.At = now
		}
		return m.applyPoll(pollMsg{
			panes: []tmux.Pane{
				{ID: "%1", MadID: "bg", Session: tmux.PoolSession, Index: -1},
				{ID: "%2", MadID: "st", Session: tmux.MainSession, Index: 1},
			},
			screens: map[string]string{},
			hooks:   hooks,
		}, now)
	}
	hook := func(state string) *status.Hook { return &status.Hook{State: state} }
	kinds := func(as []alert) string {
		var out []string
		for _, a := range as {
			s := a.Agent + " " + a.Kind
			if a.onStage {
				s += " (stage)"
			}
			out = append(out, s)
		}
		return strings.Join(out, ",")
	}
	steps := []struct {
		sec    int
		bg, st string
		want   string
	}{
		{0, status.Running, status.Running, ""},                             // first sight: nothing to compare
		{2, status.Idle, status.Running, ""},                                // bg ran 2s: too short
		{3, status.Running, status.Running, ""},                             // bg starts again
		{10, status.Idle, status.Idle, "claude done,claude#2 done (stage)"}, // bg ran 7s, st 10s
		{11, status.Waiting, status.Running, "claude waiting"},
		{12, status.Running, status.Running, ""},
		{13, status.Waiting, status.Running, ""}, // cooldown
	}
	for _, s := range steps {
		got := kinds(poll(s.sec, map[string]*status.Hook{"bg": hook(s.bg), "st": hook(s.st)}))
		if got != s.want {
			t.Errorf("t=%ds: alerts %q, want %q", s.sec, got, s.want)
		}
	}

	// The hook's own words ride along with waiting.
	m.notified = map[string]time.Time{}
	poll(40, map[string]*status.Hook{"bg": hook(status.Running), "st": hook(status.Idle)})
	w := hook(status.Waiting)
	w.Message = "Claude needs your permission to use Bash"
	as := poll(41, map[string]*status.Hook{"bg": w, "st": hook(status.Idle)})
	if len(as) != 1 || as[0].Project != "api" || as[0].Message != w.Message {
		t.Errorf("waiting alert = %+v", as)
	}

	// Turned off in config.json.
	m.notifyCfg = notify.Config{On: []string{}}
	m.notified = map[string]time.Time{}
	poll(50, map[string]*status.Hook{"bg": hook(status.Running), "st": hook(status.Idle)})
	if as := poll(60, map[string]*status.Hook{"bg": hook(status.Idle), "st": hook(status.Idle)}); len(as) != 0 {
		t.Errorf("notifications off, got %+v", as)
	}
}

// Whether someone looks at the stage is checked when the alert goes out.
func TestNotifyCmdSkipsWatchedStage(t *testing.T) {
	m, _ := setup(t, "/code/api")
	var sent []string
	oldSend, oldWatched := notify.Send, watched
	t.Cleanup(func() { notify.Send, watched = oldSend, oldWatched })
	notify.Send = func(_ notify.Config, e notify.Event) error { sent = append(sent, e.Agent); return nil }
	alerts := []alert{{notify.Event{Agent: "bg"}, false}, {notify.Event{Agent: "stage"}, true}}

	watched = func() bool { return true }
	m.notifyCmd(alerts)()
	watched = func() bool { return false }
	m.notifyCmd(alerts)()
	if got := strings.Join(sent, ","); got != "bg,bg,stage" {
		t.Errorf("sent %q", got)
	}
}

func TestJumpNext(t *testing.T) {
	m, st := setup(t, "/code/a", "/code/b")
	a1, a2 := &state.Agent{ID: "a1", Kind: "claude"}, &state.Agent{ID: "a2", Kind: "codex"}
	b1 := &state.Agent{ID: "b1", Kind: "claude"}
	st.Projects[0].Agents = []*state.Agent{a1, a2}
	st.Projects[1].Agents = []*state.Agent{b1}
	st.Projects[1].Collapsed = true
	m.rebuildRows()
	set := func(id, s string, attention bool) { m.trackers[id] = &status.Tracker{Status: s, Attention: attention} }
	set("a1", status.Waiting, false)
	set("a2", status.Idle, false)
	set("b1", status.Idle, true)
	at := func() string {
		if r, ok := m.current(); ok && r.agent != nil {
			return r.agent.ID
		}
		return ""
	}

	press(m, "d") // from the first project row
	if at() != "a1" {
		t.Fatalf("first jump → %q", at())
	}
	set("a1", status.Idle, false) // answered on stage
	press(m, "d")
	if at() != "b1" || st.Projects[1].Collapsed {
		t.Fatalf("second jump → %q (collapsed=%v)", at(), st.Projects[1].Collapsed)
	}
	set("b1", status.Idle, false)
	set("a2", status.Idle, true)
	press(m, "d") // wraps around to the top
	if at() != "a2" {
		t.Fatalf("wrap → %q", at())
	}
	set("a2", status.Idle, false)
	press(m, "d")
	if at() != "a2" || !strings.Contains(m.flash, "nothing") {
		t.Errorf("nothing left: at %q, flash %q", at(), m.flash)
	}
}

func TestStalePollIsDropped(t *testing.T) {
	m, st := setup(t, "/code/a")
	o, x, y := &state.Agent{ID: "o", Kind: "claude"}, &state.Agent{ID: "x", Kind: "claude"}, &state.Agent{ID: "y", Kind: "claude"}
	st.Projects[0].Agents = []*state.Agent{o, x, y}
	m.rebuildRows()
	stageIs := func(id string, epoch int) pollMsg {
		return pollMsg{epoch: epoch, panes: []tmux.Pane{
			{ID: "%1", MadID: tmux.IDSidebar, Session: tmux.MainSession, Index: 0},
			{ID: "%2", MadID: id, Session: tmux.MainSession, Index: 1},
		}}
	}
	cursor := func() string { r, _ := m.current(); return r.agent.ID }
	m.Update(stageIs("o", m.epoch))
	m.trackers["x"] = &status.Tracker{Status: status.Waiting}
	m.trackers["y"] = &status.Tracker{Status: status.Waiting}

	// d twice, fast: o → x → y. The first swap finishes, then a poll starts
	// (it will see x on stage) while the second swap is still running.
	press(m, "d")
	m.Update(doneMsg{})
	between := m.epoch
	press(m, "d")
	if cursor() != "y" {
		t.Fatalf("cursor on %q after two d", cursor())
	}
	_, cmd := m.Update(stageIs("x", between))
	if cursor() != "y" || cmd == nil || !m.polling {
		t.Errorf("stale poll applied: cursor snapped to %q (repoll=%v)", cursor(), cmd != nil)
	}
	m.Update(doneMsg{})
	m.Update(stageIs("y", m.epoch))
	if m.stageID != "y" || cursor() != "y" {
		t.Errorf("fresh poll: stage %q cursor %q", m.stageID, cursor())
	}
}

func TestPokes(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{{ID: "x", Kind: "claude"}, {ID: "y", Kind: "claude"}}
	m.rebuildRows()
	m.trackers["y"] = &status.Tracker{Status: status.Waiting}

	// `mad jump` (Alt-n) works whatever mode the sidebar is in.
	press(m, "a")
	if _, cmd := m.Update(pokeMsg(poke.Jump)); cmd == nil {
		t.Error("jump should open y")
	}
	if r, _ := m.current(); r.agent == nil || r.agent.ID != "y" {
		t.Errorf("cursor not on y")
	}
	press(m, "esc")

	// `mad switch` swapped the stage: re-poll now, and drop the poll in flight.
	epoch := m.epoch
	if _, cmd := m.Update(pokeMsg(poke.Poll)); cmd == nil || m.epoch == epoch || !m.polling {
		t.Errorf("poll poke: cmd=%v epoch %d→%d polling=%v", cmd != nil, epoch, m.epoch, m.polling)
	}
	if _, cmd := m.Update(pokeMsg(poke.Poll)); cmd != nil {
		t.Error("a poll is already in flight; it redoes itself when it lands")
	}
}

func TestHookPokeUpdatesAtOnce(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{{ID: "c1", Kind: "claude"}}
	m.rebuildRows()
	m.panes["c1"] = tmux.Pane{ID: "%5", MadID: "c1", Session: tmux.PoolSession, Index: -1}
	report := func(s string) {
		if err := status.WriteHook("c1", &status.Hook{State: s}, time.Now()); err != nil {
			t.Fatal(err)
		}
		m.Update(pokeMsg(poke.Hook + " c1"))
	}
	report(status.Running)
	if got := m.trackers["c1"].Status; got != status.Running {
		t.Fatalf("after running report: %s", got)
	}
	report(status.Waiting)
	if got := m.trackers["c1"].Status; got != status.Waiting {
		t.Errorf("after waiting report: %s", got)
	}
	// An id the sidebar doesn't know (removed meanwhile) is ignored.
	if _, cmd := m.Update(pokeMsg(poke.Hook + " gone")); cmd != nil || m.trackers["gone"] != nil {
		t.Error("unknown agent")
	}
}

func TestTickPollsOnlyWhatNeedsIt(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{{ID: "c1", Kind: "claude"}, {ID: "s1", Kind: "shell"}, {ID: "s2", Kind: "shell"}}
	m.rebuildRows()
	m.panes["c1"] = tmux.Pane{ID: "%1", MadID: "c1"}
	m.panes["s1"] = tmux.Pane{ID: "%2", MadID: "s1"}
	m.panes["s2"] = tmux.Pane{ID: "%3", MadID: "s2", Dead: true}

	// Only live agents without hooks have their screens captured each tick.
	if got := m.screenAgents(); len(got) != 1 || got["%2"] != "s1" {
		t.Errorf("screenAgents = %v", got)
	}

	m.lastFull = time.Now()
	m.Update(tickMsg(time.Now()))
	if !m.polling || time.Since(m.lastFull) > time.Second {
		t.Errorf("tick within fullPollEvery: polling=%v", m.polling)
	}
	m.Update(screensMsg{screens: map[string]string{"s1": "$ "}})
	if m.polling || m.screens["s1"] != "$ " {
		t.Errorf("screens not taken in: polling=%v screens=%v", m.polling, m.screens)
	}

	full := time.Now().Add(-fullPollEvery)
	m.lastFull = full
	m.Update(tickMsg(time.Now()))
	if !m.lastFull.After(full) {
		t.Error("no full poll after fullPollEvery")
	}

	// A pane gone mid-capture: a full poll sorts it out.
	m.polling, m.lastFull = false, time.Now()
	m.Update(tickMsg(time.Now()))
	before := m.lastFull
	time.Sleep(time.Millisecond)
	m.Update(screensMsg{err: errors.New("can't find pane")})
	if !m.lastFull.After(before) {
		t.Error("capture error should start a full poll")
	}
}

// A switch that lands while a screen capture runs must still get its full
// poll right after, not at the next fullPollEvery.
func TestSwitchDuringScreenCapture(t *testing.T) {
	m, st := setup(t, "/code/a")
	st.Projects[0].Agents = []*state.Agent{{ID: "s1", Kind: "shell"}}
	m.rebuildRows()
	m.panes["s1"] = tmux.Pane{ID: "%2", MadID: "s1"}
	m.lastFull = time.Now()
	m.Update(tickMsg(time.Now())) // starts a capture
	if !m.polling {
		t.Fatal("no capture started")
	}
	for _, done := range []tea.Msg{doneMsg{}, pokeMsg(poke.Poll)} {
		m.polling, m.fullDue, m.lastFull = true, false, time.Now()
		before := m.lastFull
		m.Update(done)
		time.Sleep(time.Millisecond)
		m.Update(screensMsg{screens: map[string]string{"s1": "$ "}})
		if !m.lastFull.After(before) || !m.polling {
			t.Errorf("%T during a capture: no full poll after it", done)
		}
	}
}
