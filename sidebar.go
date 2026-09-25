package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	pollInterval   = 500 * time.Millisecond
	activeWindow   = 2 * time.Second  // screen changed this recently → running
	staleRunning   = 10 * time.Second // hook says running but screen frozen → interrupted
	headerLines    = 2
	footerLines    = 3
	waitingTailLen = 15 // screen lines scanned for waiting patterns
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

type agentView struct {
	hash       uint64
	seen       bool
	lastChange time.Time
	status     string
	attention  bool // finished or blocked while not on stage
}

type (
	tickMsg time.Time
	pollMsg struct {
		panes   []Pane
		screens map[string]string
		hooks   map[string]*HookStatus
		err     error
		// externals is set when this poll also scanned for outside sessions.
		externals []External
		scanned   bool
	}
	// widthSettledMsg fires a moment after a resize; if the width is still
	// the same then, it was deliberate (drag, </>) and gets saved.
	widthSettledMsg struct{ width int }
	doneMsg         struct {
		err     error
		select_ string // agent id to put the cursor on
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

const externalScanEvery = 5 * time.Second

// row is one sidebar line: a project, a deck agent, an agent running in
// another terminal (ext), or the "N in desktop" summary (desktop > 0).
type row struct {
	proj    *Project
	agent   *Agent
	ext     *External
	desktop int
	num     int // 1-based global agent number
}

func (r row) isProject() bool { return r.agent == nil && r.ext == nil && r.desktop == 0 }

type model struct {
	st      *State
	stMod   time.Time
	kinds   []AgentKind
	views   map[string]*agentView
	panes   map[string]Pane
	stageID string
	focused bool
	polling bool

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
	externals []External
	lastScan  time.Time
}

func runSidebar() error {
	st, err := loadState()
	if err != nil {
		return err
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 512
	m := &model{
		st:    st,
		stMod: stateModTime(),
		kinds: loadKinds(),
		views: map[string]*agentView{},
		panes: map[string]Pane{},
		input: ti,
	}
	m.rebuildRows()
	defer func() {
		if r := recover(); r != nil {
			// Logged, then the restart loop in sidebarCommand relaunches us.
			if f, ferr := os.OpenFile(sidebarLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
				fmt.Fprintf(f, "%s panic: %v\n%s\n", time.Now().Format(time.RFC3339), r, debug.Stack())
				f.Close()
			}
			os.Exit(1)
		}
	}()
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithoutCatchPanics()).Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.pollCmd(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) pollCmd() tea.Cmd {
	m.polling = true
	var ids []string
	for _, a := range m.st.orderedAgents() {
		ids = append(ids, a.ID)
	}
	scan := time.Since(m.lastScan) > externalScanEvery
	if scan {
		m.lastScan = time.Now()
	}
	return func() tea.Msg {
		panes, err := listPanes()
		if err != nil {
			return pollMsg{err: err}
		}
		_ = ensureStage(panes) // no-op unless the stage pane went away
		msg := pollMsg{panes: panes, screens: map[string]string{}, hooks: map[string]*HookStatus{}}
		if scan {
			deckTTYs := map[string]bool{}
			for _, p := range panes {
				deckTTYs[p.TTY] = true
			}
			msg.externals, msg.scanned = scanExternal([]string{"claude", "codex"}, deckTTYs), true
		}
		for _, p := range panes {
			if isAgentID(p.MadID) && !p.Dead {
				msg.screens[p.MadID] = capturePane(p.ID)
			}
		}
		for _, id := range ids {
			msg.hooks[id] = readHookStatus(id)
		}
		return msg
	}
}

func isAgentID(id string) bool { return id != "" && !strings.HasPrefix(id, "_") }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - len(m.input.Prompt) - 2
		m.clampScroll()
		w := msg.Width
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return widthSettledMsg{w} })
	case widthSettledMsg:
		if msg.width == m.width && msg.width != sidebarWidth() {
			if err := saveSidebarWidth(msg.width); err != nil {
				m.setFlash(err.Error())
			}
		}
	case tickMsg:
		m.frame++
		if m.polling {
			return m, tick()
		}
		return m, tea.Batch(m.pollCmd(), tick())
	case pollMsg:
		m.polling = false
		m.applyPoll(msg)
	case doneMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error())
		}
		if msg.select_ != "" {
			m.selectAgent(msg.select_)
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

func (m *model) applyPoll(msg pollMsg) {
	if msg.err != nil {
		m.setFlash(msg.err.Error())
		return
	}
	// Pick up `mad add` and other writers.
	if mod := stateModTime(); mod.After(m.stMod) {
		if st, err := loadState(); err == nil {
			m.st, m.stMod = st, mod
			m.rebuildRows()
		}
	}

	m.panes = map[string]Pane{}
	prevStage := m.stageID
	m.stageID = ""
	for _, p := range msg.panes {
		if p.MadID != "" {
			m.panes[p.MadID] = p
		}
		if p.Session == mainSession && p.Index == 1 {
			m.stageID = p.MadID
		}
		if p.Session == mainSession && p.Index == 0 {
			m.focused = p.Active
		}
	}
	if m.stageID != prevStage && isAgentID(m.stageID) {
		m.selectAgent(m.stageID)
	}
	if msg.scanned {
		m.applyExternals(msg.externals)
	}

	now := time.Now()
	dirty := false
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			v := m.views[a.ID]
			if v == nil {
				v = &agentView{}
				m.views[a.ID] = v
			}
			hs := msg.hooks[a.ID]
			pane, ok := m.panes[a.ID]
			s := computeStatus(kindByName(m.kinds, a.Kind), pane, ok, v, hs, msg.screens[a.ID], now)
			onStage := a.ID == m.stageID
			if onStage {
				v.attention = false
			} else if v.status == statusRunning && (s == statusIdle || s == statusWaiting) {
				v.attention = true
			}
			v.status = s
			if hs != nil && hs.SessionID != "" && hs.SessionID != a.SessionID {
				// A forked resume reports its new id here; later resumes use it.
				a.SessionID, a.Fork = hs.SessionID, false
				dirty = true
			}
		}
	}
	if dirty {
		m.save()
	}
}

func computeStatus(k AgentKind, pane Pane, ok bool, v *agentView, hs *HookStatus, screen string, now time.Time) string {
	if !ok {
		return statusStopped
	}
	if pane.Dead {
		return statusExited
	}
	h := fnv.New64a()
	h.Write([]byte(screen))
	sum := h.Sum64()
	if !v.seen {
		v.seen, v.hash = true, sum
	} else if sum != v.hash {
		v.hash, v.lastChange = sum, now
	}

	if k.Hooks && hs != nil {
		switch hs.State {
		case statusWaiting:
			return statusWaiting
		case statusRunning:
			last := hs.At
			if v.lastChange.After(last) {
				last = v.lastChange
			}
			if now.Sub(last) > staleRunning {
				return statusIdle // interrupted: no Stop hook fires on Esc
			}
			return statusRunning
		default:
			return statusIdle
		}
	}
	if now.Sub(v.lastChange) < activeWindow {
		return statusRunning
	}
	if k.screenWaiting(tailLines(screen, waitingTailLen)) {
		return statusWaiting
	}
	return statusIdle
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n "), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
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
	p, _ := m.st.findAgent(id)
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
	if err := m.st.save(); err != nil {
		m.setFlash(err.Error())
	}
	m.stMod = stateModTime()
}

func (m *model) setFlash(s string) {
	m.flash, m.flashUntil = s, time.Now().Add(5*time.Second)
}

// ---- actions (tmux work runs off the UI goroutine) ----

func action(selectID string, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{err: f(), select_: selectID} }
}

func (m *model) openCmd(a *Agent) tea.Cmd {
	st := m.snapshot()
	kinds := m.kinds
	return action("", func() error { return openAgent(st, a.ID, kinds) })
}

// snapshot copies the state so actions never race with Update.
func (m *model) snapshot() *State {
	cp := &State{}
	for _, p := range m.st.Projects {
		pc := *p
		pc.Agents = nil
		for _, a := range p.Agents {
			ac := *a
			pc.Agents = append(pc.Agents, &ac)
		}
		cp.Projects = append(cp.Projects, &pc)
	}
	return cp
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

// applyExternals records outside sessions and auto-adds their projects.
func (m *model) applyExternals(ext []External) {
	m.externals = ext
	var added []string
	for _, e := range ext {
		if m.st.findProject(e.Root) != nil || m.st.isIgnored(e.Root) || !usableDir(e.Root) {
			continue
		}
		p, _ := m.st.addProject(e.Root)
		added = append(added, p.Name)
	}
	if len(added) > 0 {
		m.save()
		m.setFlash("synced: " + strings.Join(added, ", "))
	}
	m.rebuildRows()
}

// adopt moves a terminal session into the deck: the original process is
// asked to exit, then the same session resumes here.
func (m *model) adopt(p *Project, e External) tea.Cmd {
	a := &Agent{ID: newUUID(), Kind: e.Kind, SessionID: e.SessionID, CreatedAt: time.Now()}
	if e.Cwd != p.Path {
		a.Dir = e.Cwd
	}
	for i := range m.externals {
		if m.externals[i].PID == e.PID {
			m.externals = append(m.externals[:i], m.externals[i+1:]...)
			break
		}
	}
	return m.launch(p, a, true, func() error { return terminateExternal(e.PID) })
}

func (m *model) newAgent(p *Project, kind string) tea.Cmd {
	return m.launch(p, &Agent{ID: newUUID(), Kind: kind, CreatedAt: time.Now()}, false, nil)
}

// launch adds a to p and starts it in the stage; before runs first (off
// the UI goroutine) and can abort the start.
func (m *model) launch(p *Project, a *Agent, resume bool, before func() error) tea.Cmd {
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
		if err := startAgent(&pc, &ac, resume, kinds); err != nil {
			return err
		}
		return showPane(ac.ID, true)
	})
}

func (m *model) restart(p *Project, a *Agent) tea.Cmd {
	pane, ok := m.panes[a.ID]
	pc, ac, kinds := *p, *a, m.kinds
	return action(a.ID, func() error {
		var err error
		if ok {
			err = restartAgent(&pc, &ac, pane.ID, kinds)
		} else {
			err = startAgent(&pc, &ac, true, kinds)
		}
		if err != nil {
			return err
		}
		return showPane(ac.ID, true)
	})
}

func (m *model) removeAgents(ids ...string) tea.Cmd {
	for _, id := range ids {
		m.st.removeAgent(id)
		delete(m.views, id)
		os.Remove(hookStatusPath(id))
	}
	m.save()
	m.rebuildRows()
	return action("", func() error {
		for _, id := range ids {
			if err := killAgent(id); err != nil {
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
		return action("", func() error { return tmuxRun("resize-pane", "-t", sidebarPane, "-L", "2") })
	case ">", "=", "+":
		return action("", func() error { return tmuxRun("resize-pane", "-t", sidebarPane, "-R", "2") })
	case "tab":
		return action("", func() error { return tmuxRun("select-pane", "-t", stagePane) })
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		n := int(k.String()[0] - '0')
		if agents := m.st.orderedAgents(); n <= len(agents) {
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
			m.confirm(fmt.Sprintf("kill %s? (y/n)", r.proj.displayName(r.agent)), func() tea.Cmd { return m.removeAgents(id) })
		} else {
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
				m.st.removeProject(p)
				m.save()
				m.rebuildRows()
				return cmd
			})
		}
	case "q", "ctrl+c":
		return action("", func() error { return tmuxRun("detach-client") })
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

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.mode {
	case modeAddProject:
		return m.pickerView()
	case modePickSession:
		return m.sessionsView()
	}
	var b strings.Builder
	b.WriteString(stHeader.Render(" ⧉ mad") + stDim.Render(fmt.Sprintf("  %d agents", len(m.st.orderedAgents()))) + "\n\n")

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
			line = padRight(ansi.Strip(line), m.width)
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
		return fmt.Sprintf("    %s %s %s", stDim.Render("↗"), padRight(r.ext.Kind, 11), stDim.Render(r.ext.TTY))
	case r.desktop > 0:
		return stDim.Render(fmt.Sprintf("    ◇ %d in desktop", r.desktop))
	}
	if r.agent == nil {
		arrow := "▾"
		extra := ""
		if r.proj.Collapsed {
			arrow = "▸"
			extra = m.projectSummary(r.proj)
		}
		name := truncate(r.proj.Name, m.width-6)
		return " " + arrow + " " + stProject.Render(name) + extra
	}
	a := r.agent
	v := m.views[a.ID]
	status := statusStopped
	attention := false
	if v != nil && v.status != "" {
		status, attention = v.status, v.attention
	}

	mark := " "
	if a.ID == m.stageID {
		mark = stStage.Render("▶")
	}
	num := " "
	if r.num <= 9 {
		num = stDim.Render(fmt.Sprint(r.num))
	}
	icon, label := m.statusGlyph(status, attention)
	name := padRight(truncate(r.proj.displayName(a), 11), 11)
	return fmt.Sprintf("  %s%s %s %s %s", mark, num, icon, name, label)
}

func (m *model) statusGlyph(status string, attention bool) (string, string) {
	switch status {
	case statusRunning:
		return stRunning.Render(spinner[m.frame%len(spinner)]), stRunning.Render("running")
	case statusWaiting:
		return stWaiting.Render("?"), stWaiting.Render("waiting")
	case statusIdle:
		if attention {
			return stDone.Render("●"), stDone.Render("done")
		}
		return stDim.Render("○"), stDim.Render("idle")
	case statusExited:
		return stDim.Render("✗"), stDim.Render("exited")
	default:
		return stDim.Render("·"), stDim.Render("stopped")
	}
}

// projectSummary is shown next to a collapsed project: agent count plus
// the most urgent status inside it.
func (m *model) projectSummary(p *Project) string {
	if len(p.Agents) == 0 {
		return ""
	}
	waiting, running, done := 0, 0, 0
	for _, a := range p.Agents {
		if v := m.views[a.ID]; v != nil {
			switch {
			case v.status == statusWaiting:
				waiting++
			case v.status == statusRunning:
				running++
			case v.attention:
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
	case modeAddProject:
		l1 = " add project (enter/esc)"
		l2 = " " + m.input.View()
	case modeConfirm:
		l1 = " " + stWaiting.Render(m.confirmMsg)
	case modePickKind:
		// The kind menu replaces the footer; it is short enough.
		var b strings.Builder
		b.WriteString(stDim.Render(" new agent (enter/esc)") + "\n")
		for i, k := range m.kinds {
			line := fmt.Sprintf("  %d %s", i+1, k.Name)
			if i == m.kindCursor {
				line = stCursor.Render(padRight(line, m.width))
			}
			b.WriteString(line + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	default:
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			l1 = " " + stFlash.Render(truncate(m.flash, m.width-2))
		} else {
			l1 = stDim.Render(" ⏎ open  n new  a project")
		}
		l2 = stDim.Render(" r resume  x kill  q detach")
	}
	return stDim.Render(strings.Repeat("─", m.width)) + "\n" + l1 + "\n" + l2
}

// truncate cuts s to n display columns (CJK counts double).
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
