package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The session picker runs after choosing claude/codex for a new agent:
// start fresh, or continue one of the project's past sessions from the CLI
// or a desktop app.

type sessPicker struct {
	proj    *Project
	kind    string
	items   []Session // row 0 of the list is "new session"
	loading bool
	cursor  int
	offset  int
}

type sessionsMsg struct {
	root, kind string
	list       []Session
}

func (m *model) openSessionPicker(p *Project, kind string) tea.Cmd {
	m.mode = modePickSession
	m.sp = sessPicker{proj: p, kind: kind, loading: true}
	root := p.Path
	return func() tea.Msg { return sessionsMsg{root, kind, projectSessions(root, kind)} }
}

// liveSessions are session ids currently open outside the deck.
func (m *model) liveSessions() map[string]External {
	live := map[string]External{}
	for _, e := range m.externals {
		if e.SessionID != "" {
			live[e.SessionID] = e
		}
	}
	return live
}

func (m *model) sessListHeight() int {
	h := m.height - 3 - 2
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) moveSessions(d int) {
	n := len(m.sp.items) + 1
	m.sp.cursor += d
	if m.sp.cursor >= n {
		m.sp.cursor = n - 1
	}
	if m.sp.cursor < 0 {
		m.sp.cursor = 0
	}
	h := m.sessListHeight()
	if m.sp.cursor < m.sp.offset {
		m.sp.offset = m.sp.cursor
	}
	if m.sp.cursor >= m.sp.offset+h {
		m.sp.offset = m.sp.cursor - h + 1
	}
}

func (m *model) keySessions(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q", "ctrl+c":
		m.mode = modeNormal
	case "up", "k", "ctrl+p":
		m.moveSessions(-1)
	case "down", "j", "ctrl+n":
		m.moveSessions(1)
	case "pgup":
		m.moveSessions(-m.sessListHeight())
	case "pgdown":
		m.moveSessions(m.sessListHeight())
	case "enter", "l":
		return m.pickSession(m.sp.cursor)
	}
	return nil
}

func (m *model) mouseSessions(ev tea.MouseMsg) tea.Cmd {
	switch {
	case ev.Button == tea.MouseButtonWheelUp:
		m.moveSessions(-1)
	case ev.Button == tea.MouseButtonWheelDown:
		m.moveSessions(1)
	case ev.Button == tea.MouseButtonLeft && ev.Action == tea.MouseActionPress:
		if i := ev.Y - 3 + m.sp.offset; ev.Y >= 3 && i <= len(m.sp.items) {
			return m.pickSession(i)
		}
	}
	return nil
}

func (m *model) pickSession(i int) tea.Cmd {
	m.mode = modeNormal
	p := m.sp.proj
	if i == 0 {
		return m.newAgent(p, m.sp.kind)
	}
	s := m.sp.items[i-1]
	a := &Agent{ID: newUUID(), Kind: s.Kind, SessionID: s.ID, CreatedAt: time.Now()}
	if s.Cwd != "" && s.Cwd != p.Path {
		if fi, err := os.Stat(s.Cwd); err == nil && fi.IsDir() {
			a.Dir = s.Cwd // e.g. the worktree the session ran in
		}
	}
	if e, live := m.liveSessions()[s.ID]; live {
		if s.Kind != "claude" {
			m.setFlash(fmt.Sprintf("%s is still open (%s); close it there first", s.Kind, where(e)))
			return nil
		}
		a.Fork = true // don't fight the running copy; continue in a fork
	}
	return m.launch(p, a, true, nil)
}

func where(e External) string {
	if e.Desktop {
		return "desktop"
	}
	return e.TTY
}

func (m *model) sessionsView() string {
	var b strings.Builder
	b.WriteString(stHeader.Render(" "+m.sp.kind) + stDim.Render(" · "+m.sp.proj.Name+"  esc") + "\n")
	b.WriteString(stDim.Render(" new, or continue a session") + "\n")
	b.WriteString(stDim.Render(strings.Repeat("─", m.width)) + "\n")

	live := m.liveSessions()
	h := m.sessListHeight()
	lines := 0
	for i := m.sp.offset; i <= len(m.sp.items) && lines < h; i++ {
		var line string
		if i == 0 {
			line = " ＋ new session"
		} else {
			s := m.sp.items[i-1]
			mark := " "
			switch {
			case live[s.ID].PID != 0:
				mark = stDone.Render("●")
			case s.Origin == "desktop":
				mark = stDim.Render("◇")
			}
			meta := age(s.Updated)
			left := fmt.Sprintf(" %s %s", mark, truncate(s.Title, m.width-5-len(meta)))
			line = padRight(left, m.width-len(meta)-1) + stDim.Render(meta)
		}
		if i == m.sp.cursor {
			line = stCursor.Render(padRight(ansi.Strip(line), m.width))
		}
		b.WriteString(line + "\n")
		lines++
	}
	if m.sp.loading && lines < h {
		b.WriteString(stDim.Render(" loading sessions…") + "\n")
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	info := ""
	if c := m.sp.cursor; c > 0 && c <= len(m.sp.items) {
		s := m.sp.items[c-1]
		info = s.Origin
		if e, ok := live[s.ID]; ok {
			info = "open in " + where(e)
			if s.Kind == "claude" {
				info += " → fork"
			}
		}
		info += " · " + shortPath(s.Cwd)
	}
	b.WriteString(stDim.Render(" "+truncate(info, m.width-2)) + "\n")
	b.WriteString(stDim.Render(" ● open now  ◇ desktop"))
	return b.String()
}
