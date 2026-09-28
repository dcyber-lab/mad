package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/workspace"
)

// The palette takes over the sidebar: type to find a session or project,
// or start with ">" for a command. Commands act on the target captured
// when the palette opened, by id, and are checked again before they run:
// the list moving underneath never redirects them.

const (
	palHeader = 4 // title, input, target, rule
	wideWidth = 56
)

// target is what the palette was opened on.
type target struct {
	agentID string
	proj    string // project path
	dir     string // the agent's working directory then
}

// need is what a command must have as its target.
type need int

const (
	needNothing need = iota
	needProject
	needAgent
)

// command is one entry of the registry the palette, and keys, run.
type command struct {
	id, title string
	aliases   string
	need      need
	// avail says why the command can't run on this target, "" when it can.
	avail func(p *state.Project, a *state.Agent) string
	run   func(p *state.Project, a *state.Agent) tea.Cmd
}

type palItem struct {
	label, right, detail string
	agentID, proj        string
	cmd                  *command
	why                  string // unavailable: why
	score                int
}

type palette struct {
	tgt    target
	items  []palItem
	cursor int
	offset int
}

func (m *model) commands() []command {
	row := func(p *state.Project, a *state.Agent) row { return row{proj: p, agent: a} }
	return []command{
		{id: "inbox", title: "open inbox", aliases: "attention todo pending waiting",
			run: func(*state.Project, *state.Agent) tea.Cmd { return m.openInbox() }},
		{id: "next", title: "jump to the next agent that needs you", aliases: "next jump waiting",
			run: func(*state.Project, *state.Agent) tea.Cmd { return m.jumpNext() }},
		{id: "diff", title: "show changes of the target", aliases: "diff changes view",
			need: needProject, run: func(p *state.Project, a *state.Agent) tea.Cmd { return m.diffRow(row(p, a)) }},
		{id: "new-agent", title: "start a new agent…", aliases: "new launch",
			need: needProject, run: func(p *state.Project, _ *state.Agent) tea.Cmd {
				m.selectProject(p)
				m.mode, m.kindCursor, m.wtBranch = modePickKind, 0, ""
				return nil
			}},
		{id: "workspace", title: "new workspace from template…", aliases: "new template worktree setup",
			need: needProject,
			avail: func(p *state.Project, _ *state.Agent) string {
				if ts, err := workspace.Load(p.Path); err != nil {
					return err.Error()
				} else if len(ts) == 0 {
					return "no templates: " + paths.Short(workspace.ConfigFile(p.Path)) + " is missing"
				}
				return ""
			},
			run: func(p *state.Project, _ *state.Agent) tea.Cmd { return m.openTemplateForm(p) }},
		{id: "worktree", title: "new worktree…", aliases: "new branch",
			need: needProject, run: func(p *state.Project, _ *state.Agent) tea.Cmd { return m.openWorktreeInput(p) }},
		{id: "finish", title: "finish the branch… (push, PR, merge)", aliases: "push pr merge rebase",
			need: needProject, run: func(p *state.Project, a *state.Agent) tea.Cmd { m.openFinish(row(p, a)); return nil }},
		{id: "brief", title: "resume brief: bookmark, decisions, what changed", aliases: "resume context summary catch up bookmark decision",
			need: needAgent, run: func(p *state.Project, a *state.Agent) tea.Cmd { return m.openBrief(p, a) }},
		{id: "rename", title: "name the session…", aliases: "rename title",
			need: needAgent, run: func(_ *state.Project, a *state.Agent) tea.Cmd { return m.openRename(a) }},
		{id: "restart", title: "restart the agent", aliases: "reload resume",
			need: needAgent, run: func(p *state.Project, a *state.Agent) tea.Cmd {
				m.confirm(fmt.Sprintf("restart %s in %s? (y/n)", m.agentWhere(p, a), p.Name), func() tea.Cmd { return m.restart(p, a) })
				return nil
			}},
		{id: "kill", title: "kill the agent", aliases: "remove stop close delete",
			need: needAgent, run: func(p *state.Project, a *state.Agent) tea.Cmd { m.confirmKill(p, a); return nil }},
		{id: "remove-project", title: "remove the project", aliases: "delete",
			need: needProject, run: func(p *state.Project, _ *state.Agent) tea.Cmd { m.confirmRemoveProject(p); return nil }},
		{id: "add-project", title: "add a project…", aliases: "open folder",
			run: func(*state.Project, *state.Agent) tea.Cmd { return m.openPicker() }},
		{id: "compact", title: "toggle the line under each agent", aliases: "compact dense",
			run: func(*state.Project, *state.Agent) tea.Cmd { m.toggleCompact(); return nil }},
	}
}

// openPalette captures the target: the agent on stage when asked from
// there (tmux prefix + space), else the row under the cursor.
func (m *model) openPalette(fromStage bool) tea.Cmd {
	var t target
	if p, a := m.st.FindAgent(m.stageID); fromStage && a != nil {
		t = target{agentID: a.ID, proj: p.Path, dir: p.Dir(a)}
	} else if r, ok := m.current(); ok {
		t.proj = r.proj.Path
		if r.agent != nil {
			t.agentID, t.dir = r.agent.ID, r.proj.Dir(r.agent)
		}
	}
	m.mode = modePalette
	m.pal = palette{tgt: t}
	m.input.Prompt = "› "
	m.input.Placeholder = "session or project; > for commands"
	m.input.SetValue("")
	m.refreshPalette()
	return tea.Batch(m.input.Focus(), m.widen())
}

// resolve finds the target again, or says why it is gone: a command never
// falls through to whatever sits in its place now.
func (m *model) resolve(t target) (*state.Project, *state.Agent, error) {
	if t.agentID != "" {
		p, a := m.st.FindAgent(t.agentID)
		if a == nil {
			return nil, nil, fmt.Errorf("the target agent is gone; nothing done")
		}
		if p.Dir(a) != t.dir {
			return nil, nil, fmt.Errorf("the target moved to %s; nothing done", paths.Short(p.Dir(a)))
		}
		return p, a, nil
	}
	if t.proj == "" {
		return nil, nil, nil
	}
	p := m.st.FindProject(t.proj)
	if p == nil {
		return nil, nil, fmt.Errorf("the target project is gone; nothing done")
	}
	return p, nil, nil
}

func (m *model) targetLabel() string {
	p, a, err := m.resolve(m.pal.tgt)
	switch {
	case err != nil:
		return "gone"
	case a != nil:
		return p.Name + " / " + m.agentWhere(p, a) + " · " + a.Kind
	case p != nil:
		return p.Name
	}
	return "none"
}

func (m *model) refreshPalette() {
	q := strings.TrimSpace(m.input.Value())
	var items []palItem
	if rest, ok := strings.CutPrefix(q, ">"); ok {
		items = m.commandItems(strings.TrimSpace(rest))
	} else {
		items = m.objectItems(q)
	}
	m.pal.items = items
	if m.pal.cursor >= len(items) {
		m.pal.cursor = len(items) - 1
	}
	if m.pal.cursor < 0 {
		m.pal.cursor = 0
	}
	m.clampPalette()
}

// bestMatch is the best fuzzy score of q over fields.
func bestMatch(q string, fields ...string) (bool, int) {
	best, found := 0, false
	for _, f := range fields {
		if ok, s := discover.FuzzyMatch(q, f); ok && (!found || s < best) {
			best, found = s, true
		}
	}
	return found, best
}

func (m *model) objectItems(q string) []palItem {
	var items []palItem
	for _, p := range m.st.Projects {
		for _, a := range p.Agents {
			where := m.agentWhere(p, a)
			ok, score := bestMatch(q, where, m.agentTitle(p, a), a.Name, p.Name+" "+where, a.Kind)
			if !ok {
				continue
			}
			items = append(items, palItem{label: p.Name + " / " + where, right: a.Kind, agentID: a.ID, proj: p.Path,
				detail: "session · " + m.agentTitle(p, a) + " · " + paths.Short(p.Dir(a)), score: score})
		}
	}
	for _, p := range m.st.Projects {
		ok, score := bestMatch(q, p.Name)
		if !ok {
			continue
		}
		items = append(items, palItem{label: p.Name, right: "project", proj: p.Path, detail: "project · " + paths.Short(p.Path), score: score + 1})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].score < items[j].score })
	return items
}

func (m *model) commandItems(q string) []palItem {
	p, a, terr := m.resolve(m.pal.tgt)
	var items []palItem
	for _, c := range m.commands() {
		ok, score := bestMatch(q, c.title, c.aliases, c.id)
		if !ok {
			continue
		}
		c := c
		it := palItem{label: c.title, cmd: &c, score: score}
		switch {
		case c.need != needNothing && terr != nil:
			it.why = terr.Error()
		case c.need == needAgent && a == nil:
			it.why = "needs a session as its target"
		case c.need == needProject && p == nil:
			it.why = "needs a project as its target"
		case c.avail != nil:
			it.why = c.avail(p, a)
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if (items[i].why == "") != (items[j].why == "") {
			return items[i].why == ""
		}
		return items[i].score < items[j].score
	})
	return items
}

func (m *model) palListHeight() int {
	h := m.height - palHeader - 3
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) clampPalette() {
	h := m.palListHeight()
	if m.pal.cursor < m.pal.offset {
		m.pal.offset = m.pal.cursor
	}
	if m.pal.cursor >= m.pal.offset+h {
		m.pal.offset = m.pal.cursor - h + 1
	}
	if m.pal.offset < 0 {
		m.pal.offset = 0
	}
}

func (m *model) closePalette() {
	m.mode = modeNormal
	m.input.Blur()
}

func (m *model) keyPalette(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.closePalette()
		return nil
	case "up", "ctrl+p", "ctrl+k":
		m.pal.cursor = max(m.pal.cursor-1, 0)
		m.clampPalette()
		return nil
	case "down", "ctrl+n", "ctrl+j":
		m.pal.cursor = min(m.pal.cursor+1, max(len(m.pal.items)-1, 0))
		m.clampPalette()
		return nil
	case "enter":
		if m.pal.cursor < len(m.pal.items) {
			return m.runPalItem(m.pal.items[m.pal.cursor])
		}
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.pal.cursor = 0
	m.refreshPalette()
	return cmd
}

func (m *model) runPalItem(it palItem) tea.Cmd {
	if it.cmd == nil { // a session or project: go there
		m.closePalette()
		if it.agentID != "" {
			if p, a := m.st.FindAgent(it.agentID); a != nil {
				m.selectAgent(a.ID)
				return m.openAgentResuming(p, a)
			}
			m.setFlash("that session is gone")
			return nil
		}
		if p := m.st.FindProject(it.proj); p != nil {
			m.selectProject(p)
		}
		return nil
	}
	if it.why != "" {
		m.setFlash(it.cmd.title + ": " + it.why)
		return nil
	}
	p, a, err := m.resolve(m.pal.tgt) // again: time passed since the list was drawn
	if err != nil && it.cmd.need != needNothing {
		m.closePalette()
		m.setFlash(err.Error())
		return nil
	}
	m.closePalette()
	return it.cmd.run(p, a)
}

func (m *model) selectProject(p *state.Project) {
	for i, r := range m.rows {
		if r.isProject() && r.proj == p {
			m.cursor = i
		}
	}
	m.clampScroll()
}

func (m *model) paletteView() string {
	var b strings.Builder
	b.WriteString(layout(m.width, nil, []seg{{stHeader, " palette"}}, []seg{{stFaint, "esc "}}) + "\n")
	b.WriteString(" " + m.input.View() + "\n")
	b.WriteString(layout(m.width, nil, []seg{{stFaint, " target "}, {stName, m.targetLabel()}}, nil) + "\n")
	b.WriteString(rule(m.width) + "\n")

	h := m.palListHeight()
	lines := 0
	if len(m.pal.items) == 0 {
		b.WriteString(stDim.Render(" no match") + "\n")
		lines++
	}
	for i := m.pal.offset; i < len(m.pal.items) && lines < h; i++ {
		it := m.pal.items[i]
		var bg lipgloss.TerminalColor
		if i == m.pal.cursor {
			bg = cSelOn
		}
		var left, right []seg
		if it.cmd != nil {
			name := stName
			if it.why != "" {
				name = stFaint
			}
			left = []seg{{stPlain, " "}, {stKey, "›"}, {stPlain, " "}, {name, it.label}}
		} else {
			mark := seg{stDim, "▸"}
			if it.agentID == "" {
				mark = seg{stDim, "▾"}
			}
			left = []seg{{stPlain, " "}, mark, {stPlain, " "}, {stName, it.label}}
			right = []seg{{stFaint, it.right + " "}}
		}
		b.WriteString(layout(m.width, bg, left, right) + "\n")
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	b.WriteString(rule(m.width) + "\n")
	info := ""
	if m.pal.cursor < len(m.pal.items) {
		it := m.pal.items[m.pal.cursor]
		switch {
		case it.why != "":
			info = stWaiting.Render(" unavailable: " + textutil.Truncate(it.why, m.width-16))
		case it.cmd != nil:
			info = stDim.Render(" on " + textutil.Truncate(m.targetLabel(), m.width-5))
		default:
			info = stDim.Render(" " + textutil.Truncate(it.detail, m.width-2))
		}
	}
	b.WriteString(info + "\n")
	b.WriteString(hints("⏎", "go", "↑↓", "select", ">", "commands"))
	return b.String()
}

// widen makes the sidebar wide enough for a takeover view; it goes back
// when the sidebar returns to its list (see Update).
func (m *model) widen() tea.Cmd {
	if m.wideFrom != 0 || m.width == 0 || m.width >= wideWidth {
		return nil
	}
	m.wideFrom = m.width
	return func() tea.Msg {
		_ = tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-x", fmt.Sprint(wideWidth))
		return nil
	}
}

func (m *model) unwiden() tea.Cmd {
	w := m.wideFrom
	m.wideFrom = 0
	return func() tea.Msg {
		_ = tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-x", fmt.Sprint(w))
		return nil
	}
}

func wideMode(md mode) bool {
	return md == modePalette || md == modeInbox || md == modeTemplate || md == modeBrief
}
