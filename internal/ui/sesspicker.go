package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// The session picker runs after choosing a kind with a provider (see
// discover.Provider) for a new agent: start fresh, or continue one of the
// project's past sessions from the CLI or a desktop app.

const sessHeader = 3 // title, hint, rule

type sessPicker struct {
	proj    *state.Project
	kind    string
	items   []discover.Session // list row 0 is "new session", then items
	loading bool
	cursor  int
	offset  int
}

type sessionsMsg struct {
	root, kind string
	list       []discover.Session
}

// projectSessions is replaceable in tests.
var projectSessions = discover.ProjectSessions

func (m *model) openSessionPicker(p *state.Project, kind string) tea.Cmd {
	m.mode = modePickSession
	m.sp = sessPicker{proj: p, kind: kind, loading: true}
	root := p.Path
	return func() tea.Msg { return sessionsMsg{root, kind, projectSessions(root, kind)} }
}

// liveSessions are session ids currently open outside the deck.
func (m *model) liveSessions() map[string]discover.External {
	live := map[string]discover.External{}
	for _, e := range m.externals {
		if e.SessionID != "" {
			live[e.SessionID] = e
		}
	}
	return live
}

func (m *model) sessListHeight() int {
	h := m.height - sessHeader - 2
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
		if i := ev.Y - sessHeader + m.sp.offset; ev.Y >= sessHeader && i <= len(m.sp.items) {
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
	a := &state.Agent{ID: state.NewUUID(), Kind: s.Kind, SessionID: s.ID, CreatedAt: time.Now()}
	if s.Cwd != "" && s.Cwd != p.Path {
		if fi, err := os.Stat(s.Cwd); err == nil && fi.IsDir() {
			a.Dir = s.Cwd // e.g. the worktree the session ran in
		}
	}
	if e, live := m.liveSessions()[s.ID]; live {
		if agent.ByName(m.kinds, s.Kind).Fork == "" {
			m.setFlash(fmt.Sprintf("%s is still open (%s); close it there first", s.Kind, e.Where()))
			return nil
		}
		a.Fork = true // don't fight the running copy; continue in a fork
	}
	return m.launch(p, a, true, nil)
}

func (m *model) sessionsView() string {
	var b strings.Builder
	b.WriteString(layout(m.width, nil, []seg{{stHeader, " " + m.sp.kind}, {stDim, " · " + m.sp.proj.Name}}, []seg{{stFaint, "esc "}}) + "\n")
	b.WriteString(stDim.Render(" new, or continue a session") + "\n")
	b.WriteString(rule(m.width) + "\n")

	live := m.liveSessions()
	h := m.sessListHeight()
	lines := 0
	for i := m.sp.offset; i <= len(m.sp.items) && lines < h; i++ {
		var left, right []seg
		if i == 0 {
			left = []seg{{stPlain, " "}, {stKey, "＋"}, {stPlain, " "}, {stName, "new session"}}
		} else {
			s := m.sp.items[i-1]
			mark := seg{stPlain, " "}
			if _, ok := live[s.ID]; ok {
				mark = seg{stDone, "●"}
			} else if s.Origin == "desktop" {
				mark = seg{stDim, "◇"}
			}
			left = []seg{{stPlain, " "}, mark, {stPlain, " "}, {stName, s.Title}}
			right = []seg{{stFaint, textutil.Age(s.Updated) + " "}}
		}
		var bg lipgloss.TerminalColor
		if i == m.sp.cursor {
			bg = cSelOn
		}
		b.WriteString(layout(m.width, bg, left, right) + "\n")
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
			info = "open in " + e.Where()
			if agent.ByName(m.kinds, s.Kind).Fork != "" {
				info += " → fork"
			}
		}
		info += " · " + paths.Short(s.Cwd)
	}
	b.WriteString(stDim.Render(" "+textutil.Truncate(info, m.width-2)) + "\n")
	b.WriteString(" " + stDone.Render("●") + stDim.Render(" open now  ") + stDim.Render("◇ desktop"))
	return b.String()
}
