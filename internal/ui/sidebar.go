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
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
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

type (
	tickMsg time.Time
	pollMsg struct {
		panes   []tmux.Pane
		screens map[string]string
		hooks   map[string]*status.Hook
		watched bool // someone is looking at the deck (tmux.Watched)
		epoch   int  // model.epoch when the poll started
		err     error
	}
	// externalsMsg carries a scan for sessions outside the deck. It runs
	// apart from pollMsg: the first scan can take seconds (lsof per
	// process) and must not hold up status updates.
	externalsMsg []discover.External
	// widthSettledMsg fires a moment after a resize; if the width is still
	// the same then, it was deliberate (drag, </>) and gets saved.
	widthSettledMsg struct{ width int }
	// pokeMsg is a command from another mad process (poke.Poll/Jump).
	pokeMsg string
	doneMsg struct {
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
	// epoch counts actions started and finished. A poll that began in
	// another epoch may predate a swap: it is dropped and redone, or the
	// cursor would jump back to the old stage for a moment.
	epoch    int
	scanning bool

	rows   []row
	lineOf []int // screen line of each row: projects after the first get a blank line above
	cursor int
	offset int // first visible screen line
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

	notifyCfg notify.Config
	notifyMod time.Time            // config.json mtime notifyCfg was read at
	runSince  map[string]time.Time // agent id → when its current run started
	notified  map[string]time.Time // agent id + event kind → last notification
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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithoutCatchPanics())
	// Without the socket everything still works, just a poll behind.
	if l, err := poke.Listen(func(cmd string) { p.Send(pokeMsg(cmd)) }); err == nil {
		defer l.Close()
	}
	_, err = p.Run()
	return err
}

func newModel(st *state.State, kinds []agent.Kind) *model {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 512
	m := &model{
		st:        st,
		stMod:     state.ModTime(),
		kinds:     kinds,
		trackers:  map[string]*status.Tracker{},
		panes:     map[string]tmux.Pane{},
		input:     ti,
		notifyCfg: notify.Load(),
		notifyMod: notify.ModTime(),
		runSince:  map[string]time.Time{},
		notified:  map[string]time.Time{},
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
	epoch := m.epoch
	return func() tea.Msg {
		panes, err := tmux.ListPanes()
		if err != nil {
			return pollMsg{err: err, epoch: epoch}
		}
		_ = deck.EnsureStage(panes) // no-op unless the stage pane went away
		msg := pollMsg{panes: panes, screens: map[string]string{}, hooks: map[string]*status.Hook{},
			watched: tmux.Watched(), epoch: epoch}
		// One tmux call for every screen: a call per agent costs a few ms
		// each and adds up to the whole poll interval with many agents.
		byPane := map[string]string{}
		var paneIDs []string
		for _, p := range panes {
			if tmux.IsAgentID(p.MadID) && !p.Dead {
				byPane[p.ID] = p.MadID
				paneIDs = append(paneIDs, p.ID)
			}
		}
		screens, err := tmux.CaptureAll(paneIDs)
		for _, pid := range paneIDs {
			if err != nil { // a pane went away mid-poll
				msg.screens[byPane[pid]] = tmux.Capture(pid)
			} else {
				msg.screens[byPane[pid]] = screens[pid]
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
		if msg.epoch != m.epoch {
			return m, m.pollCmd() // began before an action finished: stale
		}
		return m, m.notifyCmd(m.applyPoll(msg, time.Now()))
	case externalsMsg:
		m.scanning = false
		m.applyExternals(msg)
	case pokeMsg:
		switch string(msg) {
		case poke.Jump:
			return m, m.jumpNext()
		case poke.Poll: // the stage was swapped from outside (mad switch)
			m.epoch++
			if !m.polling {
				return m, m.pollCmd()
			}
		}
		return m, nil
	case doneMsg:
		m.epoch++
		if msg.err != nil {
			m.setFlash(msg.err.Error())
		}
		if msg.selectID != "" {
			m.selectAgent(msg.selectID)
		}
		if m.polling {
			return m, nil // the poll in flight is stale now and redoes itself
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

// applyPoll updates panes and statuses, and returns the notifications the
// status changes call for.
func (m *model) applyPoll(msg pollMsg, now time.Time) []notify.Event {
	if msg.err != nil {
		m.setFlash(msg.err.Error())
		return nil
	}
	if mod := notify.ModTime(); !mod.Equal(m.notifyMod) {
		m.notifyCfg, m.notifyMod = notify.Load(), mod
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
	var events []notify.Event
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			tr := m.trackers[a.ID]
			if tr == nil {
				tr = &status.Tracker{}
				m.trackers[a.ID] = tr
			}
			hook := msg.hooks[a.ID]
			pane, ok := m.panes[a.ID]
			prev := tr.Status
			tr.Observe(agent.ByName(m.kinds, a.Kind), pane, ok, hook, msg.screens[a.ID], a.ID == m.stageID, now)
			if e, ok := m.event(p, a, prev, tr.Status, hook, a.ID == m.stageID && msg.watched, now); ok {
				events = append(events, e)
			}
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
	return events
}

const (
	// notifyMinRun: a shorter run (a redraw, a quick reply) isn't worth a
	// notification.
	notifyMinRun = 5 * time.Second
	// notifyCooldown keeps a flapping status from repeating itself.
	notifyCooldown = 15 * time.Second
)

// event turns a status change into a notification: a run that ended, or
// an agent that started waiting for you. seen means the agent is on stage
// and someone is looking, so there is nothing to tell.
func (m *model) event(p *state.Project, a *state.Agent, prev, cur string, hook *status.Hook, seen bool, now time.Time) (notify.Event, bool) {
	if cur == status.Running && prev != status.Running {
		m.runSince[a.ID] = now
	}
	var kind string
	switch {
	case prev == status.Running && cur == status.Idle && now.Sub(m.runSince[a.ID]) >= notifyMinRun:
		kind = notify.Done
	case prev != "" && prev != status.Waiting && cur == status.Waiting:
		kind = notify.Waiting // prev "": already waiting when the sidebar started
	default:
		return notify.Event{}, false
	}
	key := a.ID + "/" + kind
	if seen || !m.notifyCfg.Wants(kind) || now.Sub(m.notified[key]) < notifyCooldown {
		return notify.Event{}, false
	}
	m.notified[key] = now
	e := notify.Event{Kind: kind, Project: p.Name, Agent: p.DisplayName(a)}
	if kind == notify.Waiting && hook != nil && hook.State == status.Waiting {
		e.Message = hook.Message
	}
	return e, true
}

func (m *model) notifyCmd(events []notify.Event) tea.Cmd {
	if len(events) == 0 {
		return nil
	}
	cfg := m.notifyCfg
	return func() tea.Msg {
		for _, e := range events {
			_ = notify.Send(cfg, e) // best effort: no notifier is not an error
		}
		return nil
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
	m.lineOf = m.lineOf[:0]
	line := 0
	for i, r := range m.rows {
		if i > 0 && r.isProject() {
			line++
		}
		m.lineOf = append(m.lineOf, line)
		line++
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

// rowAt is the row drawn on list line y (0 = first list line), if any.
func (m *model) rowAt(y int) (int, bool) {
	line := y + m.offset
	for i, l := range m.lineOf {
		if l == line {
			return i, true
		}
	}
	return 0, false
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
		footer = len(m.kinds) + 2 // rule, title, one line per kind
	}
	h := m.height - headerLines - footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) clampScroll() {
	if m.cursor >= len(m.lineOf) {
		m.offset = 0
		return
	}
	h, line := m.listHeight(), m.lineOf[m.cursor]
	if line < m.offset {
		m.offset = line
	}
	if line >= m.offset+h {
		m.offset = line - h + 1
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

// action runs tmux work off the UI goroutine; polls started before it
// finishes are discarded (see epoch).
func (m *model) action(selectID string, f func() error) tea.Cmd {
	m.epoch++
	return func() tea.Msg { return doneMsg{err: f(), selectID: selectID} }
}

func (m *model) openCmd(a *state.Agent) tea.Cmd {
	st, kinds, id := m.st.Clone(), m.kinds, a.ID
	return m.action("", func() error { return deck.OpenAgent(st, id, kinds) })
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
	return m.action(a.ID, func() error {
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
	return m.action(a.ID, func() error {
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
	return m.action("", func() error {
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
		return m.action("", func() error { return tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-L", "2") })
	case ">", "=", "+":
		return m.action("", func() error { return tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-R", "2") })
	case "tab":
		return m.action("", func() error { return tmux.Run("select-pane", "-t", tmux.StagePane) })
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
	case "d":
		return m.jumpNext()
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
		return m.action("", func() error { return tmux.Run("detach-client") })
	}
	return nil
}

// needsYou: waiting for input, or finished while you were elsewhere.
func (m *model) needsYou(a *state.Agent) bool {
	tr := m.trackers[a.ID]
	return tr != nil && (tr.Status == status.Waiting || tr.Status == status.Idle && tr.Attention)
}

// jumpNext opens the next agent after the cursor that needs you, wrapping
// around; opening it clears its mark, so pressing again moves on.
func (m *model) jumpNext() tea.Cmd {
	agents := m.st.OrderedAgents()
	start := 0 // index into agents to search from
	if r, ok := m.current(); ok {
		switch {
		case r.agent != nil:
			start = r.num // r.num is 1-based: the agent after it
		default:
			for _, p := range m.st.Projects {
				if p == r.proj {
					break
				}
				start += len(p.Agents)
			}
		}
	}
	for k := range agents {
		if a := agents[(start+k)%len(agents)]; m.needsYou(a) {
			m.selectAgent(a.ID)
			return m.openCmd(a)
		}
	}
	m.setFlash("nothing waiting or done")
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
		if i, ok := m.rowAt(ev.Y - headerLines); ok && ev.Y >= headerLines {
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
	b.WriteString(m.renderHeader() + "\n\n")

	h := m.listHeight()
	lines := 0
	if len(m.rows) == 0 {
		b.WriteString(stDim.Render(" no projects yet") + "\n")
		lines++
		if lines < h {
			b.WriteString(hints("a", "add one") + "\n")
			lines++
		}
	}
	next := m.offset // next screen line to draw
	for i := 0; i < len(m.rows) && lines < h; i++ {
		l := m.lineOf[i]
		if l < m.offset {
			continue
		}
		for ; next < l && lines < h; next++ {
			b.WriteString("\n") // gap between projects
			lines++
		}
		if lines >= h {
			break
		}
		var bg lipgloss.TerminalColor
		if i == m.cursor {
			bg = cSelOff
			if m.focused {
				bg = cSelOn
			}
		}
		left, right := m.rowSegs(m.rows[i])
		b.WriteString(layout(m.width, bg, left, right) + "\n")
		next++
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	b.WriteString(m.renderFooter())
	return b.String()
}

// renderHeader is the brand plus what needs you: waiting, running and
// finished-while-away counts, or the agent count when all is quiet.
func (m *model) renderHeader() string {
	var waiting, running, done int
	agents := m.st.OrderedAgents()
	for _, a := range agents {
		if tr := m.trackers[a.ID]; tr != nil {
			switch {
			case tr.Status == status.Waiting:
				waiting++
			case tr.Status == status.Running:
				running++
			case tr.Status == status.Idle && tr.Attention:
				done++
			}
		}
	}
	var right []seg
	add := func(n int, st lipgloss.Style, icon string) {
		if n > 0 {
			right = append(right, seg{st, fmt.Sprintf("%s%d", icon, n)}, seg{stPlain, "  "})
		}
	}
	add(waiting, stWaiting, "?")
	add(running, stRunning, m.spin())
	add(done, stDone, "●")
	if len(right) > 0 {
		right[len(right)-1].s = " "
	} else {
		n := fmt.Sprintf("%d agents ", len(agents))
		if len(agents) == 1 {
			n = "1 agent "
		}
		right = []seg{{stFaint, n}}
	}
	return layout(m.width, nil, []seg{{stHeader, " ⧉ mad"}}, right)
}

func (m *model) spin() string { return spinner[m.frame%len(spinner)] }

// rowSegs lays out one row: the left part is cut to fit, the right part
// (status, count, tty) is right-aligned and dropped when too narrow.
func (m *model) rowSegs(r row) (left, right []seg) {
	switch {
	case r.ext != nil:
		return []seg{{stPlain, "     "}, {stDim, "↗ "}, {stDim, r.ext.Kind}},
			[]seg{{stFaint, r.ext.TTY + " "}}
	case r.desktop > 0:
		return []seg{{stPlain, "     "}, {stDim, "◇ "}, {stDim, fmt.Sprintf("%d in desktop", r.desktop)}}, nil
	case r.agent == nil:
		arrow := "▾ "
		right = []seg{{stFaint, fmt.Sprintf("%d ", len(r.proj.Agents))}}
		if r.proj.Collapsed {
			arrow = "▸ "
			if icon := m.projectSummary(r.proj); icon.s != "" {
				right = append([]seg{icon, {stPlain, " "}}, right...)
			}
		}
		if len(r.proj.Agents) == 0 {
			right = nil
		}
		return []seg{{stPlain, " "}, {stDim, arrow}, {stProject, r.proj.Name}}, right
	}
	a := r.agent
	st, attention := status.Stopped, false
	if tr := m.trackers[a.ID]; tr != nil && tr.Status != "" {
		st, attention = tr.Status, tr.Attention
	}
	bar, name := seg{stPlain, " "}, seg{stName, r.proj.DisplayName(a)}
	if a.ID == m.stageID {
		bar, name = seg{stStage, "▌"}, seg{stStage, name.s}
	}
	num := " "
	if r.num <= 9 {
		num = fmt.Sprint(r.num)
	}
	icon, label := m.statusGlyph(st, attention)
	return []seg{{stPlain, " "}, bar, {stPlain, " "}, {stFaint, num}, {stPlain, " "}, icon, {stPlain, " "}, name},
		[]seg{label, {stPlain, " "}}
}

func (m *model) statusGlyph(s string, attention bool) (icon, label seg) {
	switch s {
	case status.Running:
		return seg{stRunning, m.spin()}, seg{stRunning, "running"}
	case status.Waiting:
		return seg{stWaiting, "?"}, seg{stWaiting, "waiting"}
	case status.Idle:
		if attention {
			return seg{stDone, "●"}, seg{stDone, "done"}
		}
		return seg{stDim, "○"}, seg{stFaint, "idle"}
	case status.Exited:
		return seg{stDim, "✗"}, seg{stFaint, "exited"}
	default:
		return seg{stFaint, "·"}, seg{stFaint, "stopped"}
	}
}

// projectSummary is the most urgent status inside a collapsed project.
func (m *model) projectSummary(p *state.Project) seg {
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
	switch {
	case waiting > 0:
		return seg{stWaiting, "?"}
	case running > 0:
		return seg{stRunning, m.spin()}
	case done > 0:
		return seg{stDone, "●"}
	}
	return seg{}
}

func (m *model) renderFooter() string {
	var l1, l2 string
	switch m.mode {
	case modeConfirm:
		l1 = " " + stWaiting.Render(m.confirmMsg)
	case modePickKind:
		// The kind menu replaces the footer; it is short enough.
		var b strings.Builder
		title := "new agent"
		if r, ok := m.current(); ok {
			title += " · " + r.proj.Name
		}
		b.WriteString(rule(m.width) + "\n")
		b.WriteString(layout(m.width, nil, []seg{{stHeader, " " + title}}, []seg{{stFaint, "esc "}}) + "\n")
		for i, k := range m.kinds {
			var bg lipgloss.TerminalColor
			if i == m.kindCursor {
				bg = cSelOn
			}
			b.WriteString(layout(m.width, bg, []seg{{stPlain, "  "}, {stKey, fmt.Sprint(i + 1)}, {stPlain, " "}, {stName, k.Name}}, nil) + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	default:
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			l1 = " " + stFlash.Render(textutil.Truncate(m.flash, m.width-2))
		} else {
			l1 = hints("⏎", "open", "n", "new", "a", "add", "d", "next")
		}
		l2 = hints("r", "resume", "x", "kill", "q", "detach")
	}
	return rule(m.width) + "\n" + l1 + "\n" + l2
}
