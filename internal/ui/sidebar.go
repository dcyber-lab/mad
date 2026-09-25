// Package ui is the sidebar: a Bubble Tea program that runs in the left
// tmux pane, shows projects and agents with live status, and drives the
// deck (open, create, resume, kill agents).
package ui

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

const (
	pollInterval      = 500 * time.Millisecond
	externalScanEvery = 5 * time.Second
	headerLines       = 2
	footerLines       = 3
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	stHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	stProject  = lipgloss.NewStyle().Bold(true)
	stDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	stCursor   = lipgloss.NewStyle().Background(lipgloss.Color("24")).Foreground(lipgloss.Color("255")).Bold(true)
	stCursorBg = lipgloss.NewStyle().Background(lipgloss.Color("235"))
	stRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	stWaiting  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	stDone     = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	stStage    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	stFlash    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

type (
	tickMsg time.Time
	pollMsg struct {
		panes   []tmux.Pane
		screens map[string]string
		hooks   map[string]*status.Hook
		err     error
	}
	// externalsMsg carries a scan for sessions outside the deck. It runs
	// apart from pollMsg: the first scan can take seconds (lsof per
	// process) and must not hold up status updates.
	externalsMsg []discover.External
	// widthSettledMsg fires a moment after a resize; if the width is still
	// the same then, it was deliberate (drag, </>) and gets saved.
	widthSettledMsg struct{ width int }
	doneMsg         struct {
		err      error
		selectID string // agent id to put the cursor on
	}
)

type mode int

const (
	modeNormal mode = iota
	modeAddProject
	modePickKind
	modePickSession
	modeConfirm
)

// row is one sidebar line: a project, a deck agent, an agent running in
// another terminal (ext), or the "N in desktop" summary (desktop > 0).
type row struct {
	proj    *state.Project
	agent   *state.Agent
	ext     *discover.External
	desktop int
	num     int // 1-based global agent number
}

func (r row) isProject() bool { return r.agent == nil && r.ext == nil && r.desktop == 0 }

type model struct {
	st       *state.State
	stMod    time.Time
	kinds    []agent.Kind
	trackers map[string]*status.Tracker
	panes    map[string]tmux.Pane
	stageID  string
	focused  bool
	polling  bool
	scanning bool

	rows   []row
	cursor int
	offset int
	width  int
	height int
	frame  int

	mode       mode
	input      textinput.Model
	kindCursor int
	confirmMsg string
	confirmYes func() tea.Cmd

	flash      string
	flashUntil time.Time

	pk        picker
	sp        sessPicker
	externals []discover.External
	lastScan  time.Time
}

// Run is `mad sidebar`. A panic is logged to the sidebar log and exits
// non-zero; the pane's restart loop then relaunches the sidebar.
func Run() error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	m := newModel(st, agent.Load())
	defer func() {
		if r := recover(); r != nil {
			if f, ferr := os.OpenFile(paths.SidebarLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
				fmt.Fprintf(f, "%s panic: %v\n%s\n", time.Now().Format(time.RFC3339), r, debug.Stack())
				f.Close()
			}
			os.Exit(1)
		}
	}()
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithoutCatchPanics()).Run()
	return err
}

func newModel(st *state.State, kinds []agent.Kind) *model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 512
	m := &model{
		st:       st,
		stMod:    state.ModTime(),
		kinds:    kinds,
		trackers: map[string]*status.Tracker{},
		panes:    map[string]tmux.Pane{},
		input:    ti,
	}
	m.rebuildRows()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.pollCmd(), m.scanCmd(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// scanCmd looks for claude/codex sessions outside the deck.
func (m *model) scanCmd() tea.Cmd {
	m.scanning, m.lastScan = true, time.Now()
	return func() tea.Msg {
		deckTTYs := map[string]bool{}
		if panes, err := tmux.ListPanes(); err == nil {
			for _, p := range panes {
				deckTTYs[p.TTY] = true
			}
		}
		return externalsMsg(discover.ScanExternal([]string{"claude", "codex"}, deckTTYs))
	}
}

// pollCmd gathers tmux panes, screens and hook reports off the UI
// goroutine.
func (m *model) pollCmd() tea.Cmd {
	m.polling = true
	var ids []string
	for _, a := range m.st.OrderedAgents() {
		ids = append(ids, a.ID)
	}
	return func() tea.Msg {
		panes, err := tmux.ListPanes()
		if err != nil {
			return pollMsg{err: err}
		}
		_ = deck.EnsureStage(panes) // no-op unless the stage pane went away
		msg := pollMsg{panes: panes, screens: map[string]string{}, hooks: map[string]*status.Hook{}}
		for _, p := range panes {
			if tmux.IsAgentID(p.MadID) && !p.Dead {
				msg.screens[p.MadID] = tmux.Capture(p.ID)
			}
		}
		for _, id := range ids {
			msg.hooks[id] = status.ReadHook(id)
		}
		return msg
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - len(m.input.Prompt) - 2
		m.clampScroll()
		w := msg.Width
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return widthSettledMsg{w} })
	case widthSettledMsg:
		if msg.width == m.width && msg.width != deck.SidebarWidth() {
			if err := deck.SaveSidebarWidth(msg.width); err != nil {
				m.setFlash(err.Error())
			}
		}
	case tickMsg:
		m.frame++
		cmds := []tea.Cmd{tick()}
		if !m.polling {
			cmds = append(cmds, m.pollCmd())
		}
		if !m.scanning && time.Since(m.lastScan) > externalScanEvery {
			cmds = append(cmds, m.scanCmd())
		}
		return m, tea.Batch(cmds...)
	case pollMsg:
		m.polling = false
		m.applyPoll(msg, time.Now())
	case externalsMsg:
		m.scanning = false
		m.applyExternals(msg)
	case doneMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error())
		}
		if msg.selectID != "" {
			m.selectAgent(msg.selectID)
		}
		if m.polling {
			return m, nil
		}
		return m, m.pollCmd()
	case historyMsg:
		m.pk.history, m.pk.loading = msg, false
		if m.mode == modeAddProject {
			m.refreshPicker()
		}
	case sessionsMsg:
		if m.mode == modePickSession && msg.root == m.sp.proj.Path && msg.kind == m.sp.kind {
			m.sp.items, m.sp.loading = msg.list, false
		}
	case tea.MouseMsg:
		switch m.mode {
		case modeAddProject:
			return m, m.mousePicker(msg)
		case modePickSession:
			return m, m.mouseSessions(msg)
		}
		return m, m.handleMouse(msg)
	case tea.KeyMsg:
		switch m.mode {
		case modeAddProject:
			return m, m.keyPicker(msg)
		case modePickSession:
			return m, m.keySessions(msg)
		case modePickKind:
			return m, m.keyPickKind(msg)
		case modeConfirm:
			return m, m.keyConfirm(msg)
		default:
			// Fast typing arrives as one multi-rune key; handle each rune.
			if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
				var cmds []tea.Cmd
				for _, r := range msg.Runes {
					cmds = append(cmds, m.keyNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
				}
				return m, tea.Batch(cmds...)
			}
			return m, m.keyNormal(msg)
		}
	}
	if m.mode == modeAddProject {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) applyPoll(msg pollMsg, now time.Time) {
	if msg.err != nil {
		m.setFlash(msg.err.Error())
		return
	}
	// Pick up `mad add` and other writers.
	if mod := state.ModTime(); mod.After(m.stMod) {
		if st, err := state.Load(); err == nil {
			m.st, m.stMod = st, mod
			m.rebuildRows()
		}
	}

	m.panes = map[string]tmux.Pane{}
	prevStage := m.stageID
	m.stageID = ""
	for _, p := range msg.panes {
		if p.MadID != "" {
			m.panes[p.MadID] = p
		}
		if p.Session == tmux.MainSession && p.Index == 1 {
			m.stageID = p.MadID
		}
		if p.Session == tmux.MainSession && p.Index == 0 {
			m.focused = p.Active
		}
	}
	if m.stageID != prevStage && tmux.IsAgentID(m.stageID) {
		m.selectAgent(m.stageID)
	}

	dirty := false
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			tr := m.trackers[a.ID]
			if tr == nil {
				tr = &status.Tracker{}
				m.trackers[a.ID] = tr
			}
			hook := msg.hooks[a.ID]
			pane, ok := m.panes[a.ID]
			tr.Observe(agent.ByName(m.kinds, a.Kind), pane, ok, hook, msg.screens[a.ID], a.ID == m.stageID, now)
			if hook != nil && hook.SessionID != "" && hook.SessionID != a.SessionID {
				// A forked resume reports its new id here; later resumes use it.
				a.SessionID, a.Fork = hook.SessionID, false
				dirty = true
			}
		}
	}
	if dirty {
		m.save()
	}
}

// usableDir is replaceable in tests (temp dirs don't count as projects).
var usableDir = discover.UsableDir

// applyExternals records outside sessions and auto-adds their projects.
func (m *model) applyExternals(ext []discover.External) {
	m.externals = ext
	var added []string
	for _, e := range ext {
		if m.st.FindProject(e.Root) != nil || m.st.IsIgnored(e.Root) || !usableDir(e.Root) {
			continue
		}
		p, _ := m.st.AddProject(e.Root)
		added = append(added, p.Name)
	}
	if len(added) > 0 {
		m.save()
		m.setFlash("synced: " + strings.Join(added, ", "))
	}
	m.rebuildRows()
}

// ---- rows & selection ----

func (m *model) rebuildRows() {
	prev, hadPrev := m.current()
	m.rows = m.rows[:0]
	num := 0
	for _, p := range m.st.Projects {
		m.rows = append(m.rows, row{proj: p})
		for _, a := range p.Agents {
			num++
			if !p.Collapsed {
				m.rows = append(m.rows, row{proj: p, agent: a, num: num})
			}
		}
		if p.Collapsed {
			continue
		}
		desktop := 0
		for i := range m.externals {
			e := &m.externals[i]
			if e.Root != p.Path {
				continue
			}
			if e.Desktop {
				desktop++
			} else {
				m.rows = append(m.rows, row{proj: p, ext: e})
			}
		}
		if desktop > 0 {
			m.rows = append(m.rows, row{proj: p, desktop: desktop})
		}
	}
	// Keep the cursor on the same item when possible.
	if hadPrev {
		for i, r := range m.rows {
			same := r.proj.Path == prev.proj.Path
			switch {
			case prev.agent != nil:
				same = r.agent != nil && r.agent.ID == prev.agent.ID
			case prev.ext != nil:
				same = same && r.ext != nil && r.ext.PID == prev.ext.PID
			case prev.desktop > 0:
				same = same && r.desktop > 0
			default:
				same = same && r.isProject()
			}
			if same {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

func (m *model) selectAgent(id string) {
	p, _ := m.st.FindAgent(id)
	if p != nil && p.Collapsed {
		p.Collapsed = false
		m.save()
		m.rebuildRows()
	}
	for i, r := range m.rows {
		if r.agent != nil && r.agent.ID == id {
			m.cursor = i
		}
	}
	m.clampScroll()
}

func (m *model) listHeight() int {
	footer := footerLines
	if m.mode == modePickKind {
		footer = len(m.kinds) + 1
	}
	h := m.height - headerLines - footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) clampScroll() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) move(d int) {
	m.cursor += d
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

func (m *model) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m *model) save() {
	if err := m.st.Save(); err != nil {
		m.setFlash(err.Error())
	}
	m.stMod = state.ModTime()
}

func (m *model) setFlash(s string) {
	m.flash, m.flashUntil = s, time.Now().Add(5*time.Second)
}

// ---- actions (tmux work runs off the UI goroutine) ----

func action(selectID string, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{err: f(), selectID: selectID} }
}

func (m *model) openCmd(a *state.Agent) tea.Cmd {
	st, kinds, id := m.st.Clone(), m.kinds, a.ID
	return action("", func() error { return deck.OpenAgent(st, id, kinds) })
}

func (m *model) activate(r row) tea.Cmd {
	switch {
	case r.agent != nil:
		return m.openCmd(r.agent)
	case r.ext != nil:
		e, p := *r.ext, r.proj
		m.confirm(fmt.Sprintf("take over %s from %s? it exits there (y/n)", e.Kind, e.TTY), func() tea.Cmd {
			return m.adopt(p, e)
		})
		return nil
	case r.desktop > 0:
		return m.openSessionPicker(r.proj, "claude")
	}
	r.proj.Collapsed = !r.proj.Collapsed
	m.save()
	m.rebuildRows()
	return nil
}

// adopt moves a terminal session into the deck: the original process is
// asked to exit, then the same session resumes here.
func (m *model) adopt(p *state.Project, e discover.External) tea.Cmd {
	a := &state.Agent{ID: state.NewUUID(), Kind: e.Kind, SessionID: e.SessionID, CreatedAt: time.Now()}
	if e.Cwd != p.Path {
		a.Dir = e.Cwd
	}
	for i := range m.externals {
		if m.externals[i].PID == e.PID {
			m.externals = append(m.externals[:i], m.externals[i+1:]...)
			break
		}
	}
	return m.launch(p, a, true, func() error { return discover.TerminateExternal(e.PID) })
}

func (m *model) newAgent(p *state.Project, kind string) tea.Cmd {
	return m.launch(p, &state.Agent{ID: state.NewUUID(), Kind: kind, CreatedAt: time.Now()}, false, nil)
}

// launch adds a to p and starts it in the stage; before runs first (off
// the UI goroutine) and can abort the start.
func (m *model) launch(p *state.Project, a *state.Agent, resume bool, before func() error) tea.Cmd {
	p.Agents = append(p.Agents, a)
	p.Collapsed = false
	m.save()
	m.rebuildRows()
	m.selectAgent(a.ID)
	pc, ac, kinds := *p, *a, m.kinds
	return action(a.ID, func() error {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		if err := deck.StartAgent(&pc, &ac, resume, kinds); err != nil {
			return err
		}
		return deck.ShowPane(ac.ID, true)
	})
}

func (m *model) restart(p *state.Project, a *state.Agent) tea.Cmd {
	pane, ok := m.panes[a.ID]
	pc, ac, kinds := *p, *a, m.kinds
	return action(a.ID, func() error {
		var err error
		if ok {
			err = deck.RestartAgent(&pc, &ac, pane.ID, kinds)
		} else {
			err = deck.StartAgent(&pc, &ac, true, kinds)
		}
		if err != nil {
			return err
		}
		return deck.ShowPane(ac.ID, true)
	})
}

func (m *model) removeAgents(ids ...string) tea.Cmd {
	for _, id := range ids {
		m.st.RemoveAgent(id)
		delete(m.trackers, id)
		status.RemoveHook(id)
	}
	m.save()
	m.rebuildRows()
	return action("", func() error {
		for _, id := range ids {
			if err := deck.KillAgent(id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- keys ----

func (m *model) keyNormal(k tea.KeyMsg) tea.Cmd {
	r, ok := m.current()
	switch k.String() {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "g", "home":
		m.move(-len(m.rows))
	case "G", "end":
		m.move(len(m.rows))
	case "enter", "l", "right", " ":
		if ok {
			return m.activate(r)
		}
	case "<", "-":
		return action("", func() error { return tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-L", "2") })
	case ">", "=", "+":
		return action("", func() error { return tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-R", "2") })
	case "tab":
		return action("", func() error { return tmux.Run("select-pane", "-t", tmux.StagePane) })
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		n := int(k.String()[0] - '0')
		if agents := m.st.OrderedAgents(); n <= len(agents) {
			return m.openCmd(agents[n-1])
		}
	case "n":
		if ok {
			m.mode, m.kindCursor = modePickKind, 0
		} else {
			m.setFlash("add a project first (a)")
		}
	case "a":
		return m.openPicker()
	case "r":
		if ok && r.agent != nil {
			return m.restart(r.proj, r.agent)
		}
	case "x":
		if !ok || r.ext != nil || r.desktop > 0 {
			return nil
		}
		if r.agent != nil {
			id := r.agent.ID
			m.confirm(fmt.Sprintf("kill %s? (y/n)", r.proj.DisplayName(r.agent)), func() tea.Cmd { return m.removeAgents(id) })
			return nil
		}
		p := r.proj
		var ids []string
		for _, a := range p.Agents {
			ids = append(ids, a.ID)
		}
		msg := fmt.Sprintf("remove %s? (y/n)", p.Name)
		if len(ids) > 0 {
			msg = fmt.Sprintf("remove %s + kill %d? (y/n)", p.Name, len(ids))
		}
		m.confirm(msg, func() tea.Cmd {
			cmd := m.removeAgents(ids...)
			m.st.RemoveProject(p)
			m.save()
			m.rebuildRows()
			return cmd
		})
	case "q", "ctrl+c":
		return action("", func() error { return tmux.Run("detach-client") })
	}
	return nil
}

func (m *model) confirm(msg string, yes func() tea.Cmd) {
	m.mode, m.confirmMsg, m.confirmYes = modeConfirm, msg, yes
}

func (m *model) keyConfirm(k tea.KeyMsg) tea.Cmd {
	m.mode = modeNormal
	if k.String() == "y" || k.String() == "Y" {
		return m.confirmYes()
	}
	return nil
}

func (m *model) keyPickKind(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q", "ctrl+c":
		m.mode = modeNormal
	case "up", "k":
		if m.kindCursor > 0 {
			m.kindCursor--
		}
	case "down", "j":
		if m.kindCursor < len(m.kinds)-1 {
			m.kindCursor++
		}
	case "enter", "l":
		return m.pickKind(m.kindCursor)
	default:
		if s := k.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if i := int(s[0] - '1'); i < len(m.kinds) {
				return m.pickKind(i)
			}
		}
	}
	return nil
}

func (m *model) pickKind(i int) tea.Cmd {
	m.mode = modeNormal
	r, ok := m.current()
	if !ok {
		return nil
	}
	if k := m.kinds[i].Name; k == "claude" || k == "codex" {
		return m.openSessionPicker(r.proj, k)
	}
	return m.newAgent(r.proj, m.kinds[i].Name)
}

func (m *model) handleMouse(ev tea.MouseMsg) tea.Cmd {
	if m.mode != modeNormal {
		return nil
	}
	switch {
	case ev.Button == tea.MouseButtonWheelUp:
		m.move(-1)
	case ev.Button == tea.MouseButtonWheelDown:
		m.move(1)
	case ev.Button == tea.MouseButtonLeft && ev.Action == tea.MouseActionPress:
		i := ev.Y - headerLines + m.offset
		if ev.Y >= headerLines && i < len(m.rows) {
			m.cursor = i
			return m.activate(m.rows[i])
		}
	}
	return nil
}

// ---- view ----

// View never draws past the pane width: tmux would wrap the line and push
// the whole layout down.
func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	var v string
	switch m.mode {
	case modeAddProject:
		v = m.pickerView()
	case modePickSession:
		v = m.sessionsView()
	default:
		v = m.sidebarView()
	}
	lines := strings.Split(v, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "")
	}
	return strings.Join(lines, "\n")
}

func (m *model) sidebarView() string {
	var b strings.Builder
	b.WriteString(stHeader.Render(" ⧉ mad") + stDim.Render(fmt.Sprintf("  %d agents", len(m.st.OrderedAgents()))) + "\n\n")

	h := m.listHeight()
	lines := 0
	if len(m.rows) == 0 {
		b.WriteString(stDim.Render(" no projects — press a") + "\n")
		lines++
	}
	for i := m.offset; i < len(m.rows) && lines < h; i++ {
		line := m.renderRow(m.rows[i])
		if i == m.cursor {
			// Inner styles reset the background, so the cursor row is
			// rendered plain on a solid bar.
			line = textutil.PadRight(ansi.Strip(line), m.width)
			if m.focused {
				line = stCursor.Render(line)
			} else {
				line = stCursorBg.Render(line)
			}
		}
		b.WriteString(line + "\n")
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m *model) renderRow(r row) string {
	switch {
	case r.ext != nil:
		return fmt.Sprintf("    %s %s %s", stDim.Render("↗"), textutil.PadRight(r.ext.Kind, 11), stDim.Render(r.ext.TTY))
	case r.desktop > 0:
		return stDim.Render(fmt.Sprintf("    ◇ %d in desktop", r.desktop))
	case r.agent == nil:
		arrow, extra := "▾", ""
		if r.proj.Collapsed {
			arrow, extra = "▸", m.projectSummary(r.proj)
		}
		return " " + arrow + " " + stProject.Render(textutil.Truncate(r.proj.Name, m.width-6)) + extra
	}
	a := r.agent
	st, attention := status.Stopped, false
	if tr := m.trackers[a.ID]; tr != nil && tr.Status != "" {
		st, attention = tr.Status, tr.Attention
	}
	mark := " "
	if a.ID == m.stageID {
		mark = stStage.Render("▶")
	}
	num := " "
	if r.num <= 9 {
		num = stDim.Render(fmt.Sprint(r.num))
	}
	icon, label := m.statusGlyph(st, attention)
	name := textutil.PadRight(textutil.Truncate(r.proj.DisplayName(a), 11), 11)
	return fmt.Sprintf("  %s%s %s %s %s", mark, num, icon, name, label)
}

func (m *model) statusGlyph(s string, attention bool) (string, string) {
	switch s {
	case status.Running:
		return stRunning.Render(spinner[m.frame%len(spinner)]), stRunning.Render("running")
	case status.Waiting:
		return stWaiting.Render("?"), stWaiting.Render("waiting")
	case status.Idle:
		if attention {
			return stDone.Render("●"), stDone.Render("done")
		}
		return stDim.Render("○"), stDim.Render("idle")
	case status.Exited:
		return stDim.Render("✗"), stDim.Render("exited")
	default:
		return stDim.Render("·"), stDim.Render("stopped")
	}
}

// projectSummary is shown next to a collapsed project: agent count plus
// the most urgent status inside it.
func (m *model) projectSummary(p *state.Project) string {
	if len(p.Agents) == 0 {
		return ""
	}
	waiting, running, done := 0, 0, 0
	for _, a := range p.Agents {
		if tr := m.trackers[a.ID]; tr != nil {
			switch {
			case tr.Status == status.Waiting:
				waiting++
			case tr.Status == status.Running:
				running++
			case tr.Attention:
				done++
			}
		}
	}
	s := stDim.Render(fmt.Sprintf(" (%d)", len(p.Agents)))
	switch {
	case waiting > 0:
		s += " " + stWaiting.Render("?")
	case running > 0:
		s += " " + stRunning.Render(spinner[m.frame%len(spinner)])
	case done > 0:
		s += " " + stDone.Render("●")
	}
	return s
}

func (m *model) renderFooter() string {
	var l1, l2 string
	switch m.mode {
	case modeConfirm:
		l1 = " " + stWaiting.Render(m.confirmMsg)
	case modePickKind:
		// The kind menu replaces the footer; it is short enough.
		var b strings.Builder
		b.WriteString(stDim.Render(" new agent (enter/esc)") + "\n")
		for i, k := range m.kinds {
			line := fmt.Sprintf("  %d %s", i+1, k.Name)
			if i == m.kindCursor {
				line = stCursor.Render(textutil.PadRight(line, m.width))
			}
			b.WriteString(line + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	default:
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			l1 = " " + stFlash.Render(textutil.Truncate(m.flash, m.width-2))
		} else {
			l1 = stDim.Render(" ⏎ open  n new  a project")
		}
		l2 = stDim.Render(" r resume  x kill  q detach")
	}
	return stDim.Render(strings.Repeat("─", m.width)) + "\n" + l1 + "\n" + l2
}
