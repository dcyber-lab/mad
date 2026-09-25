package ui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

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
			m.mode, m.kindCursor, m.wtBranch = modePickKind, 0, ""
		} else {
			m.setFlash("add a project first (a)")
		}
	case "w":
		if ok {
			return m.openWorktreeInput(r.proj)
		}
		m.setFlash("add a project first (a)")
	case "v":
		if ok {
			return m.diffRow(r)
		}
	case "t":
		if ok && r.agent != nil {
			return m.openRename(r.agent)
		}
		m.setFlash("select an agent to name")
	case "f":
		if ok {
			m.openFinish(r)
		}
	case "i":
		m.st.Compact = !m.st.Compact
		m.save()
		m.rebuildRows()
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
			p, a := r.proj, r.agent
			m.confirm(fmt.Sprintf("kill %s? (y/n)", p.DisplayName(a)), func() tea.Cmd {
				cmd := m.removeAgents(a.ID)
				// Its worktree was made for it: offer to clean up too. git
				// refuses when there are uncommitted changes.
				if dir := a.Dir; dir != "" && git.IsWorktree(p.Path, dir) && !dirInUse(p, dir) {
					repo := p.Path
					m.confirm(fmt.Sprintf("remove worktree %s? (y/n)", filepath.Base(dir)), func() tea.Cmd {
						return m.action("", func() error { return git.RemoveWorktree(repo, dir) })
					})
				}
				return cmd
			})
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
		m.mode, m.wtBranch = modeNormal, ""
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
	if branch := m.wtBranch; branch != "" {
		// A fresh worktree has no sessions to continue: start right away.
		m.wtBranch = ""
		return m.newWorktreeAgent(m.wt, m.kinds[i].Name, branch)
	}
	r, ok := m.current()
	if !ok {
		return nil
	}
	if k := m.kinds[i].Name; discover.Lookup(k) != nil {
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
		if i, ok := m.rowAt(ev.Y - m.headerH()); ok && ev.Y >= m.headerH() {
			m.cursor = i
			return m.activate(m.rows[i])
		}
	}
	return nil
}
