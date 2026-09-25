package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
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
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
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
	// a, claude (2 lines), (gap), b, codex (2 lines)
	if got := m.lineOf; len(got) != 4 || got[2] != 4 || got[3] != 5 {
		t.Fatalf("lineOf = %v", got)
	}
	lines := strings.Split(m.View(), "\n")
	if strings.TrimSpace(lines[headerLines+3]) != "" || !strings.Contains(lines[headerLines+4], "b") {
		t.Errorf("no gap before the second project:\n%s", strings.Join(lines[:8], "\n"))
	}

	click := func(y int) {
		m.Update(tea.MouseMsg{X: 3, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	}
	click(headerLines + 3) // the gap
	if m.cursor != 0 || st.Projects[1].Collapsed {
		t.Errorf("gap click: cursor=%d collapsed=%v", m.cursor, st.Projects[1].Collapsed)
	}
	click(headerLines + 2) // the line under claude belongs to it
	if m.cursor != 1 {
		t.Errorf("detail click: cursor=%d", m.cursor)
	}
	click(headerLines + 4) // project b
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

func TestWorktreeFlow(t *testing.T) {
	m, st := setup(t, "/p/one")
	var added []string
	oldAdd, oldValid := addWorktree, validBranch
	addWorktree = func(repo, branch, dir string) error { added = append(added, repo+" "+branch+" "+dir); return nil }
	validBranch = func(name string) error {
		if strings.ContainsAny(name, " ") || name == "" {
			return errors.New("invalid branch name")
		}
		return nil
	}
	t.Cleanup(func() { addWorktree, validBranch = oldAdd, oldValid })

	press(m, "w")
	if m.mode != modeWorktree || m.wt != st.Projects[0] {
		t.Fatalf("mode=%v wt=%v", m.mode, m.wt)
	}
	if v := m.View(); !strings.Contains(v, "new worktree · one") || !strings.Contains(v, "branch") {
		t.Errorf("worktree footer missing:\n%s", v)
	}
	// A bad name is refused and shown; the prompt stays.
	press(m, "b", "a", "d", " ", "x", "enter")
	if m.mode != modeWorktree || !strings.Contains(m.View(), "invalid branch") {
		t.Errorf("mode=%v view:\n%s", m.mode, m.View())
	}
	press(m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc should leave the prompt")
	}

	press(m, "w")
	m.input.SetValue("feat/x")
	press(m, "enter")
	if m.mode != modePickKind || m.wtBranch != "feat/x" {
		t.Fatalf("mode=%v branch=%q", m.mode, m.wtBranch)
	}
	if v := m.View(); !strings.Contains(v, "one @ feat/x") {
		t.Errorf("kind menu should name the branch:\n%s", v)
	}
	press(m, "1") // claude: no session picker for a fresh worktree
	if m.mode != modeNormal || m.wtBranch != "" {
		t.Errorf("mode=%v branch=%q", m.mode, m.wtBranch)
	}
	agents := st.Projects[0].Agents
	if len(agents) != 1 || agents[0].Kind != "claude" || agents[0].Dir != "/p/one/.claude/worktrees/feat-x" {
		t.Fatalf("agents = %+v", agents)
	}
	// Before the first git scan the row already says which worktree.
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	if v := m.View(); !strings.Contains(v, "claude  feat-x") {
		t.Errorf("worktree name missing from row:\n%s", v)
	}
	// The worktree is made when the command runs, not when it is built.
	if len(added) != 0 {
		t.Errorf("worktree added early: %v", added)
	}

	// Setup that fails (e.g. git refused the branch) drops the agent again.
	m.Update(doneMsg{err: errors.New("git worktree: boom"), failedID: agents[0].ID})
	if len(st.Projects[0].Agents) != 0 {
		t.Error("failed agent kept")
	}
	if saved, _ := state.Load(); len(saved.Projects[0].Agents) != 0 {
		t.Error("failed agent still saved")
	}
	if !strings.Contains(m.View(), "boom") {
		t.Error("error not shown")
	}

	// n after w must not reuse the branch.
	press(m, "w")
	m.input.SetValue("feat/y")
	press(m, "enter", "esc", "n")
	if m.wtBranch != "" || m.mode != modePickKind {
		t.Errorf("after esc + n: branch=%q mode=%v", m.wtBranch, m.mode)
	}
	press(m, "4") // shell: straight to launch, in the project itself
	if a := st.Projects[0].Agents; len(a) != 1 || a[0].Dir != "" {
		t.Errorf("agents = %+v", a)
	}
}

func TestKillWorktreeAgentOffersRemoval(t *testing.T) {
	m, st := setup(t, "/p/one")
	dir := "/p/one/.claude/worktrees/feat"
	st.Projects[0].Agents = []*state.Agent{
		{ID: "a1", Kind: "claude", Dir: dir},
		{ID: "a2", Kind: "codex", Dir: dir},
		{ID: "a3", Kind: "claude", Dir: "/elsewhere"},
	}
	m.rebuildRows()

	press(m, "j", "x", "y") // a1: a2 still uses the worktree
	if m.mode != modeNormal {
		t.Errorf("removal offered while another agent uses the worktree: %q", m.confirmMsg)
	}
	press(m, "j", "j", "x", "y") // a3: not a mad worktree
	if m.mode != modeNormal {
		t.Errorf("removal offered for a foreign dir: %q", m.confirmMsg)
	}
	press(m, "j", "x", "y") // a2: last one in the worktree
	if m.mode != modeConfirm || !strings.Contains(m.confirmMsg, "remove worktree feat") {
		t.Fatalf("mode=%v msg=%q", m.mode, m.confirmMsg)
	}
	press(m, "n")
	if m.mode != modeNormal || len(st.Projects[0].Agents) != 0 {
		t.Errorf("mode=%v agents=%d", m.mode, len(st.Projects[0].Agents))
	}
}

func TestGitInfoInRows(t *testing.T) {
	m, st := setup(t, "/p/one")
	st.Projects[0].Agents = []*state.Agent{
		{ID: "a1", Kind: "claude"},
		{ID: "a2", Kind: "codex", Dir: "/p/one/.claude/worktrees/feat-x"},
	}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 48, Height: 30})

	// The scan asks about every project and agent directory, once each.
	old := gitStatus
	t.Cleanup(func() { gitStatus = old })
	var asked []string
	gitStatus = func(dir string) (gitInfo, bool) {
		asked = append(asked, dir)
		return gitInfo{}, false
	}
	m.gitCmd()()
	sort.Strings(asked)
	if strings.Join(asked, " ") != "/p/one /p/one/.claude/worktrees/feat-x" {
		t.Errorf("scanned %v", asked)
	}

	m.Update(gitMsg{
		"/p/one":                          {Branch: "main", Dirty: 2, Ahead: 1},
		"/p/one/.claude/worktrees/feat-x": {Branch: "feat/x"},
	})
	v := m.View()
	for _, want := range []string{"one  main ±2 ↑1", "codex  feat/x"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "claude  main") {
		t.Errorf("an agent in the project dir should carry no checkout of its own:\n%s", v)
	}

	// A turn ending asks for a fresh scan at the next tick.
	m.gitDue, m.gitScanning = false, false
	m.trackers["a1"] = &status.Tracker{Status: status.Running}
	m.applyPoll(pollMsg{panes: []tmux.Pane{{ID: "%1", MadID: "a1", Session: tmux.PoolSession, Index: -1, Dead: true}}}, time.Now())
	if !m.gitDue {
		t.Error("run ended but no scan requested")
	}
}

type gitInfo = git.Info

func TestDiffView(t *testing.T) {
	m, st := setup(t, "/p/one")
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude"}, {ID: "a2", Kind: "claude", Dir: "/p/one/.claude/worktrees/w"}}
	m.rebuildRows()
	now := time.Now()
	sidebar := tmux.Pane{ID: "%1", MadID: tmux.IDSidebar, Session: tmux.MainSession, Index: 0, Active: true}
	m.applyPoll(pollMsg{panes: []tmux.Pane{sidebar,
		{ID: "%2", MadID: "a1", Session: tmux.MainSession, Index: 1},
	}}, now)

	// v on a row opens its directory; the stage bar stays on the agent
	// while its diff is up.
	press(m, "j", "j")
	if cmd := m.keyNormal(key("v")); cmd == nil || m.taskFor != "a2" {
		t.Fatalf("cmd=%v taskFor=%q", cmd, m.taskFor)
	}
	diff := tmux.Pane{ID: "%9", MadID: tmux.IDTask, Session: tmux.MainSession, Index: 1}
	m.applyPoll(pollMsg{panes: []tmux.Pane{sidebar, diff, {ID: "%2", MadID: "a1", Session: tmux.PoolSession, Index: -1}}}, now)
	if m.stageID != tmux.IDTask {
		t.Fatalf("stage = %q", m.stageID)
	}
	if r, _ := m.current(); r.agent.ID != "a2" {
		t.Errorf("cursor moved to %v", r.agent)
	}
	if left, _ := m.rowSegs(m.rows[2]); left[1].s != "▌" {
		t.Error("the agent whose diff is shown should carry the stage bar")
	}
	if m.taskCleanup([]tmux.Pane{sidebar, diff}) != nil {
		t.Error("a live diff view on stage must be left alone")
	}
	// v again on the same row takes it down; on another row it switches.
	if cmd := m.keyNormal(key("v")); cmd == nil {
		t.Error("toggle off returned nothing")
	}
	press(m, "k")
	if cmd := m.keyNormal(key("v")); cmd == nil || m.taskFor != "a1" {
		t.Errorf("switch: cmd=%v taskFor=%q", cmd, m.taskFor)
	}
	// Quitting the viewer leaves a dead pane on stage: cleaned up. Parked
	// in the pool: cleaned up too.
	dead := diff
	dead.Dead = true
	if m.taskCleanup([]tmux.Pane{sidebar, dead}) == nil {
		t.Error("dead diff view not closed")
	}
	parked := diff
	parked.Session, parked.Index = tmux.PoolSession, -1
	if m.taskCleanup([]tmux.Pane{sidebar, parked}) == nil {
		t.Error("parked diff view not closed")
	}
	// Alt-v from the stage: for the agent there, or back from the diff.
	m.applyPoll(pollMsg{panes: []tmux.Pane{sidebar, {ID: "%2", MadID: "a1", Session: tmux.MainSession, Index: 1}}}, now)
	if _, cmd := m.Update(pokeMsg(poke.Diff)); cmd == nil || m.taskFor != "a1" {
		t.Errorf("poke diff: cmd=%v taskFor=%q", cmd, m.taskFor)
	}
	m.applyPoll(pollMsg{panes: []tmux.Pane{sidebar, diff}}, now)
	if _, cmd := m.Update(pokeMsg(poke.Diff)); cmd == nil {
		t.Error("poke diff on the diff view should close it")
	}
}

func TestTokensInRows(t *testing.T) {
	m, st := setup(t, "/p/one", "/p/two")
	st.Projects[0].Agents = []*state.Agent{
		{ID: "a1", Kind: "claude", SessionID: "s1"},
		{ID: "a2", Kind: "codex", Dir: "/p/one/.claude/worktrees/w"},
		{ID: "a3", Kind: "shell"},
	}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 48, Height: 30})

	// The read covers every agent, with its session id and directory.
	old := m.reader
	t.Cleanup(func() { m.reader = old })
	m.readCmd() // just the snapshot; the read itself needs no transcripts
	if !m.reading || m.readDue {
		t.Error("scan not marked in flight")
	}
	// Before any transcript, rows carry no count.
	if v := m.View(); strings.Contains(v, "0  ") {
		t.Errorf("empty counts shown:\n%s", v)
	}

	m.Update(transcriptMsg{
		"a1": {Tokens: transcript.Totals{Input: 10, CacheRead: 1_200_000, Output: 500}},
		"a2": {Tokens: transcript.Totals{Input: 33_000, Output: 1_000}},
	})
	if m.reading {
		t.Error("scan still marked in flight")
	}
	v := m.View()
	for _, want := range []string{"1.2M  stopped", "34k  stopped", "1.2M  3 "} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	lines := strings.Split(v, "\n")
	for _, l := range lines {
		if strings.Contains(l, "shell") && strings.Contains(l, "k  ") {
			t.Errorf("shell has no transcript but shows a count: %q", l)
		}
		if strings.Contains(l, "two") && strings.Contains(l, "  0 ") {
			t.Errorf("an empty project shows a count: %q", l)
		}
	}

	// Too narrow for both: the count goes, the status stays.
	m.Update(tea.WindowSizeMsg{Width: 25, Height: 30})
	v = m.View()
	if strings.Contains(v, "1.2M  stopped") || !strings.Contains(v, "claude") || strings.Count(v, "stopped") != 3 {
		t.Errorf("at width 25:\n%s", v)
	}
	m.Update(tea.WindowSizeMsg{Width: 48, Height: 30})

	// A turn ending asks for a fresh read at the next tick.
	m.readDue, m.reading = false, false
	m.trackers["a1"] = &status.Tracker{Status: status.Running}
	m.applyPoll(pollMsg{panes: []tmux.Pane{{ID: "%1", MadID: "a1", Session: tmux.PoolSession, Index: -1, Dead: true}}}, time.Now())
	if !m.readDue {
		t.Error("run ended but no read requested")
	}
}

func TestDetailLines(t *testing.T) {
	m, st := setup(t, "/p/one")
	st.Projects[0].Agents = []*state.Agent{
		{ID: "a1", Kind: "claude"},
		{ID: "a2", Kind: "codex"},
		{ID: "a3", Kind: "shell"},
	}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	// claude and codex take two lines, shell one.
	if got := m.lineOf; len(got) != 4 || got[1] != 1 || got[2] != 3 || got[3] != 5 {
		t.Fatalf("lineOf = %v", got)
	}
	line := func(n int) string { return strings.Split(m.View(), "\n")[headerLines+n] }

	// Before any transcript: the kind's icon and claude / claude#2.
	if l := line(1); !strings.Contains(l, "✻  claude ") {
		t.Errorf("no title yet: %q", l)
	}
	if l := line(3); !strings.Contains(l, ">_ codex ") {
		t.Errorf("codex icon: %q", l)
	}
	if l := line(5); !strings.Contains(l, "$  shell ") {
		t.Errorf("shell icon: %q", l)
	}
	m.Update(transcriptMsg{
		"a1": {Title: "Flaky test fix", Prompt: "now the docs", Tool: "Edit · README.md"},
		"a2": {Title: "Build speed"},
	})
	// The title takes the name's place; the last prompt goes under it.
	if l := line(1); !strings.Contains(l, "✻  Flaky test fix") || strings.Contains(l, "claude") {
		t.Errorf("title should replace the name: %q", l)
	}
	if l := line(2); !strings.Contains(l, "now the docs") || strings.Contains(l, "Edit") {
		t.Errorf("idle claude should show its last prompt: %q", l)
	}
	if l := line(3); !strings.Contains(l, ">_ Build speed") {
		t.Errorf("codex title: %q", l)
	}
	// Running: the tool, or the prompt before any tool call.
	m.trackers["a1"] = &status.Tracker{Status: status.Running}
	if l := line(2); !strings.Contains(l, "Edit · README.md") {
		t.Errorf("running claude should show its tool: %q", l)
	}
	m.transcripts["a1"] = transcript.Info{Title: "Flaky test fix", Prompt: "now the docs"}
	if l := line(2); !strings.Contains(l, "now the docs") {
		t.Errorf("running claude without a tool should show the prompt: %q", l)
	}
	m.trackers["a1"] = &status.Tracker{Status: status.Waiting}
	m.transcripts["a1"] = transcript.Info{Title: "Flaky test fix", Tool: "Bash · rm -rf build"}
	if l := line(2); !strings.Contains(l, "rm -rf build") {
		t.Errorf("waiting claude should show the tool it asks about: %q", l)
	}

	// A long line is cut to the width, never wrapped.
	m.transcripts["a1"] = transcript.Info{Tool: "Bash · " + strings.Repeat("x", 100)}
	if l := line(2); lipgloss.Width(l) > 40 || !strings.Contains(l, "…") {
		t.Errorf("detail not truncated (%d wide): %q", lipgloss.Width(l), l)
	}

	// The cursor moves by row, not by line.
	press(m, "j", "j")
	if r, _ := m.current(); r.agent == nil || r.agent.ID != "a2" {
		t.Errorf("j moved to %+v, want a2", r)
	}

	// i hides the lines and remembers that.
	press(m, "i")
	if got := m.lineOf; got[3] != 3 || !st.Compact {
		t.Errorf("compact: lineOf=%v compact=%v", got, st.Compact)
	}
	if v := m.View(); !strings.Contains(v, "Build speed") || strings.Contains(v, "now the docs") {
		t.Errorf("compact should keep titles and drop the line under:\n%s", v)
	}
	press(m, "i")
	if st.Compact || m.lineOf[3] != 5 {
		t.Errorf("not back: compact=%v lineOf=%v", st.Compact, m.lineOf)
	}
}

func TestRename(t *testing.T) {
	m, st := setup(t, "/p/one")
	a := &state.Agent{ID: "a1", Kind: "claude"}
	st.Projects[0].Agents = []*state.Agent{a}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	m.Update(transcriptMsg{"a1": {Title: "Flaky test fix", Prompt: "fix it", Tool: "Bash · go test"}})

	press(m, "t") // on the project row: nothing to name
	if m.mode != modeNormal || !strings.Contains(m.View(), "select an agent") {
		t.Errorf("mode=%v view:\n%s", m.mode, m.View())
	}
	press(m, "j", "t")
	if m.mode != modeRename || !strings.Contains(m.View(), "name · claude") {
		t.Errorf("mode=%v view:\n%s", m.mode, m.View())
	}
	press(m, "esc")
	if m.mode != modeNormal || a.Name != "" {
		t.Errorf("esc: mode=%v name=%q", m.mode, a.Name)
	}
	press(m, "t", "d", "o", "c", "s", "enter")
	if m.mode != modeNormal || a.Name != "docs" {
		t.Errorf("enter: mode=%v name=%q", m.mode, a.Name)
	}
	if v := m.View(); !strings.Contains(v, "✻  docs") || strings.Contains(v, "Flaky") {
		t.Errorf("name should replace the title:\n%s", v)
	}
	// The name is saved; the line under still says what it does.
	if saved, _ := state.Load(); saved.Projects[0].Agents[0].Name != "docs" {
		t.Error("name not saved")
	}
	m.trackers["a1"] = &status.Tracker{Status: status.Running}
	if v := m.View(); !strings.Contains(v, "go test") || !strings.Contains(v, "✻  docs") {
		t.Errorf("running agent should show its tool under the name:\n%s", v)
	}
	// Clearing the name goes back to the transcript's title; the old name
	// is offered for editing first.
	m.trackers["a1"] = nil
	press(m, "t")
	if m.input.Value() != "docs" {
		t.Errorf("input = %q, want the current name", m.input.Value())
	}
	press(m, "backspace", "backspace", "backspace", "backspace", "enter")
	if a.Name != "" || !strings.Contains(m.View(), "Flaky test fix") {
		t.Errorf("name=%q view:\n%s", a.Name, m.View())
	}
}

func TestFinishMenu(t *testing.T) {
	m, st := setup(t, "/p/one")
	wt := "/p/one/.claude/worktrees/feat-x"
	st.Projects[0].Agents = []*state.Agent{{ID: "a1", Kind: "claude", Dir: wt}, {ID: "a2", Kind: "shell"}}
	m.rebuildRows()
	old := defaultBranch
	t.Cleanup(func() { defaultBranch = old })
	asked := 0
	defaultBranch = func(repo string) string { asked++; return "main" }

	// Before the git scan nothing is known about the checkout.
	press(m, "j", "f")
	if m.mode != modeNormal || !strings.Contains(m.View(), "not on a branch") {
		t.Fatalf("mode=%v view:\n%s", m.mode, m.View())
	}
	m.Update(gitMsg{"/p/one": {Branch: "main"}, wt: {Branch: "feat/x", Dirty: 1}})

	press(m, "f")
	if m.mode != modePickFinish || m.finID != "a1" || m.finCo.Branch != "feat/x" || m.finCo.Base != "main" {
		t.Fatalf("mode=%v id=%q co=%+v", m.mode, m.finID, m.finCo)
	}
	v := m.View()
	for _, want := range []string{"finish · feat/x → main", "rebase onto main", "merge into main", "push"} {
		if !strings.Contains(v, want) {
			t.Errorf("menu lacks %q:\n%s", want, v)
		}
	}
	press(m, "j", "j") // cursor on the third entry, whatever gh made of it
	if m.finCursor != 2 {
		t.Errorf("cursor = %d", m.finCursor)
	}
	if cmd := m.keyPickFinish(key("enter")); cmd == nil || m.mode != modeNormal || m.taskFor != "a1" {
		t.Errorf("cmd=%v mode=%v taskFor=%q", cmd, m.mode, m.taskFor)
	}

	// The default branch is looked up once per repo; esc leaves the menu.
	press(m, "f", "esc")
	if m.mode != modeNormal || asked != 1 {
		t.Errorf("mode=%v lookups=%d", m.mode, asked)
	}
	// On the project row (main itself) there is nothing to rebase or merge.
	press(m, "g", "f")
	if m.mode != modePickFinish || m.finID != "" {
		t.Fatalf("mode=%v id=%q", m.mode, m.finID)
	}
	if v := m.View(); strings.Contains(v, "rebase") || strings.Contains(v, "merge") || strings.Contains(v, "→") {
		t.Errorf("project on its base branch:\n%s", v)
	}
	press(m, "esc")
	// Configured actions replace the menu.
	m.finCfg = []deck.FinishAction{{Name: "ship it", Command: "ship {branch}"}}
	press(m, "j", "f")
	if len(m.fin) != 1 || m.fin[0].Command != "ship 'feat/x'" || !strings.Contains(m.View(), "ship it") {
		t.Errorf("configured menu: %+v", m.fin)
	}
	press(m, "esc")
}
