package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

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
		return m.openSessionPicker(r.proj, r.deskKind)
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
	m.epoch++
	return func() tea.Msg {
		if before != nil {
			if err := before(); err != nil {
				return doneMsg{err: err, failedID: ac.ID}
			}
		}
		if err := deck.StartAgent(&pc, &ac, resume, kinds); err != nil {
			return doneMsg{err: err, selectID: ac.ID}
		}
		return doneMsg{err: deck.ShowPane(ac.ID, true), selectID: ac.ID}
	}
}

// newWorktreeAgent starts kind in its own worktree of p on branch, which
// is created from HEAD when new. Claude Code's worktree directory is used,
// so sessions in it are grouped under p everywhere in mad.
func (m *model) newWorktreeAgent(p *state.Project, kind, branch string) tea.Cmd {
	dir := git.WorktreeDir(p.Path, branch)
	a := &state.Agent{ID: state.NewUUID(), Kind: kind, Dir: dir, CreatedAt: time.Now()}
	repo := p.Path
	return m.launch(p, a, false, func() error { return addWorktree(repo, branch, dir) })
}

// dirInUse reports whether an agent of p runs in dir.
func dirInUse(p *state.Project, dir string) bool {
	for _, a := range p.Agents {
		if p.Dir(a) == dir {
			return true
		}
	}
	return false
}

// ---- diff view ----

// toggleDiff shows the changes in dir (of agent id, or a project when id
// is empty) on stage, or takes them down again when they are up already.
func (m *model) toggleDiff(id, dir string, focusStage bool) tea.Cmd {
	if d, ok := m.panes[tmux.IDTask]; ok && !d.Dead && m.stageID == tmux.IDTask && m.taskFor == id {
		return m.closeTask(focusStage)
	}
	m.taskFor = id
	cmd := deck.DiffCommand(m.cfg.Diff, dir)
	return m.action("", func() error { return deck.OpenTask(dir, cmd) })
}

// closeTask puts the agent the diff was opened for back on stage.
func (m *model) closeTask(focusStage bool) tea.Cmd {
	back := m.taskFor
	return m.action("", func() error {
		if err := deck.CloseTask(back); err != nil {
			return err
		}
		target := tmux.SidebarPane
		if focusStage {
			target = tmux.StagePane
		}
		return tmux.Run("select-pane", "-t", target)
	})
}

// diffFromStage is `mad diff` (Alt-v): the diff of the agent on stage, or
// back to the agent from its diff; with no agent on stage, the cursor row.
func (m *model) diffFromStage() tea.Cmd {
	if m.stageID == tmux.IDTask {
		return m.closeTask(true)
	}
	if p, a := m.st.FindAgent(m.stageID); a != nil {
		return m.toggleDiff(a.ID, p.Dir(a), true)
	}
	if r, ok := m.current(); ok {
		return m.diffRow(r)
	}
	return nil
}

func (m *model) diffRow(r row) tea.Cmd {
	if r.agent != nil {
		return m.toggleDiff(r.agent.ID, r.proj.Dir(r.agent), false)
	}
	return m.toggleDiff("", r.proj.Path, false)
}

// taskCleanup takes the task pane (diff view, finish command) down once
// its command has exited (the pane stays dead on stage, remain-on-exit),
// or if it got parked in the pool: it is a one-off, not an agent.
func (m *model) taskCleanup(panes []tmux.Pane) tea.Cmd {
	d, ok := tmux.FindPane(panes, tmux.IDTask)
	if !ok {
		return nil
	}
	onStage := d.Session == tmux.MainSession && d.Index == 1
	if onStage && !d.Dead {
		return nil
	}
	if onStage {
		return m.closeTask(false) // you quit the viewer: back to the sidebar
	}
	return m.action("", func() error { return deck.CloseTask("") })
}

// ---- finishing a branch ----

// openFinish shows the finish menu for the checkout of r: push, open a
// pull request, rebase onto or merge into the default branch.
func (m *model) openFinish(r row) {
	c := deck.Checkout{Dir: r.proj.Path, Repo: r.proj.Path}
	m.finID = ""
	if r.agent != nil {
		c.Dir, m.finID = r.proj.Dir(r.agent), r.agent.ID
	}
	info, ok := m.gitInfo[c.Dir]
	if !ok || info.Branch == "" {
		m.setFlash("not on a branch")
		return
	}
	c.Branch, c.RepoBranch = info.Branch, m.gitInfo[c.Repo].Branch
	base, known := m.baseOf[c.Repo]
	if !known {
		base = defaultBranch(c.Repo)
		m.baseOf[c.Repo] = base
	}
	c.Base = base
	m.fin = deck.FinishActions(m.cfg.Finish, c)
	if len(m.fin) == 0 {
		m.setFlash("nothing to do for " + c.Branch)
		return
	}
	m.finTitle = "finish · " + c.Branch
	if c.Base != "" && c.Base != c.Branch {
		m.finTitle += " → " + c.Base
	}
	m.finCo, m.finCursor, m.mode = c, 0, modePickFinish
}

func (m *model) keyPickFinish(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q", "ctrl+c":
		m.mode = modeNormal
	case "up", "k":
		if m.finCursor > 0 {
			m.finCursor--
		}
	case "down", "j":
		if m.finCursor < len(m.fin)-1 {
			m.finCursor++
		}
	case "enter", "l":
		return m.runFinish(m.finCursor)
	default:
		if s := k.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if i := int(s[0] - '1'); i < len(m.fin) {
				return m.runFinish(i)
			}
		}
	}
	return nil
}

// runFinish runs the chosen command on stage, in the checkout; the pane
// stays until enter so the outcome (a PR link, a conflict) can be read.
func (m *model) runFinish(i int) tea.Cmd {
	m.mode = modeNormal
	dir, cmd := m.finCo.Dir, deck.HoldCommand(m.fin[i].Command)
	m.taskFor = m.finID
	return m.action("", func() error { return deck.OpenTask(dir, cmd) })
}

// ---- worktrees ----

func (m *model) openWorktreeInput(p *state.Project) tea.Cmd {
	m.mode, m.wt, m.wtBranch = modeWorktree, p, ""
	m.input.Prompt = "branch › "
	m.input.Placeholder = "new or existing"
	m.input.SetValue("")
	return m.input.Focus()
}

func (m *model) keyWorktree(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.mode = modeNormal
		m.input.Blur()
		return nil
	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if err := validBranch(name); err != nil {
			m.setFlash(err.Error())
			return nil
		}
		m.wtBranch = name
		m.mode, m.kindCursor = modePickKind, 0
		m.input.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}

// ---- naming ----

func (m *model) openRename(a *state.Agent) tea.Cmd {
	m.mode, m.renameID, m.flash = modeRename, a.ID, ""
	m.input.Prompt = "name › "
	m.input.Placeholder = "empty: the conversation's title"
	m.input.SetValue(a.Name)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *model) keyRename(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.mode = modeNormal
		m.input.Blur()
		return nil
	case "enter":
		if _, a := m.st.FindAgent(m.renameID); a != nil {
			a.Name = strings.TrimSpace(m.input.Value())
			m.save()
		}
		m.mode = modeNormal
		m.input.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
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
