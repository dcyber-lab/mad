package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

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
	b.WriteString(m.renderHeader() + "\n")
	for _, l := range m.quotaLines(time.Now()) {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")

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
		l, rh := m.lineOf[i], m.rowHeight(m.rows[i])
		if l+rh <= m.offset {
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
		if l >= m.offset { // else only its second line is in view
			left, right := m.rowSegs(m.rows[i])
			b.WriteString(layout(m.width, bg, left, right) + "\n")
			next++
			lines++
		}
		if rh > 1 && lines < h {
			b.WriteString(layout(m.width, bg, m.detailSegs(m.rows[i].agent), nil) + "\n")
			next++
			lines++
		}
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

// headerH is how many lines the list starts after: the brand line, a
// line per kind with known usage limits, and a blank one.
func (m *model) headerH() int {
	return headerLines + len(m.quotaKinds(time.Now()))
}

// quotaKinds lists the kinds with usage limits to show, in kinds order,
// with each report's spent windows already dropped.
func (m *model) quotaKinds(now time.Time) []string {
	if !m.cfg.Quota {
		return nil
	}
	var out []string
	for _, k := range m.kinds {
		q, ok := m.quota[k.Name]
		if !ok {
			continue
		}
		if q = q.Expire(now); q.FiveHour.Known() || q.SevenDay.Known() || !q.At.IsZero() {
			out = append(out, k.Name)
		}
	}
	return out
}

const quotaBar = 8 // cells in the five-hour bar

// quotaLines draws the usage limits under the header, one kind a line:
// its icon, the five-hour window as a bar with the percentage and the
// time to its reset, and the weekly window on the right.
func (m *model) quotaLines(now time.Time) []string {
	var out []string
	for _, name := range m.quotaKinds(now) {
		k := agent.ByName(m.kinds, name)
		q := m.quota[name].Expire(now)
		icon := seg{stName.Bold(true), textutil.PadRight(k.Glyph(), m.iconWidth())}
		if k.Color != "" {
			icon.st = icon.st.Foreground(lipgloss.Color(k.Color))
		}
		left := []seg{{stPlain, " "}, icon, {stPlain, " "}, {stFaint, "5h "}}
		w := q.FiveHour
		if m.width >= 36 {
			filled := int(w.Used/100*quotaBar + 0.5)
			if filled > quotaBar {
				filled = quotaBar
			}
			left = append(left, seg{quotaStyle(w.Used), strings.Repeat("▓", filled)}, seg{stFaint, strings.Repeat("░", quotaBar-filled)}, seg{stPlain, " "})
		}
		left = append(left, seg{quotaStyle(w.Used), fmt.Sprintf("%.0f%%", w.Used)})
		if !w.ResetAt.IsZero() {
			left = append(left, seg{stFaint, " " + textutil.Until(w.ResetAt, now)})
		}
		var right []seg
		if wk := q.SevenDay; wk.Known() {
			right = []seg{{stFaint, "wk "}, {quotaStyle(wk.Used), fmt.Sprintf("%.0f%%", wk.Used)}}
			if !wk.ResetAt.IsZero() {
				right = append(right, seg{stFaint, " " + textutil.Until(wk.ResetAt, now)})
			}
			right = append(right, seg{stPlain, " "})
		}
		out = append(out, layout(m.width, nil, left, right))
	}
	return out
}

// quotaStyle colors a used percentage: plain, then orange from 80%, red
// from 95%.
func quotaStyle(used float64) lipgloss.Style {
	switch {
	case used >= 95:
		return stWaiting
	case used >= 80:
		return stRunning
	}
	return stName
}

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
		left = []seg{{stPlain, " "}, {stDim, arrow}, {stProject, r.proj.Name}}
		left = append(left, m.gitSegs(r.proj.Path, "")...)
		return left, m.withTokens(left, right, m.projectTokens(r.proj))
	}
	a := r.agent
	st, attention := status.Stopped, false
	if tr := m.trackers[a.ID]; tr != nil && tr.Status != "" {
		st, attention = tr.Status, tr.Attention
	}
	bar, name := seg{stPlain, " "}, seg{stName, m.agentTitle(r.proj, a)}
	if a.ID == m.stageID || m.stageID == tmux.IDTask && a.ID == m.taskFor {
		bar, name = seg{stStage, "▌"}, seg{stStage, name.s}
	}
	num := " "
	if r.num <= 9 {
		num = fmt.Sprint(r.num)
	}
	icon, label := m.statusGlyph(st, attention)
	k := agent.ByName(m.kinds, a.Kind)
	kind := seg{stName.Bold(true), textutil.PadRight(k.Glyph(), m.iconWidth())}
	if k.Color != "" {
		kind.st = kind.st.Foreground(lipgloss.Color(k.Color))
	}
	left = []seg{{stPlain, " "}, bar, {stPlain, " "}, {stFaint, num}, {stPlain, " "}, icon, {stPlain, " "}, kind, {stPlain, " "}, name}
	if a.Dir != "" && a.Dir != r.proj.Path {
		// Its own checkout: name it, even before the first git scan.
		left = append(left, m.gitSegs(a.Dir, filepath.Base(a.Dir))...)
	}
	return left, m.withTokens(left, []seg{label, {stPlain, " "}}, m.transcripts[a.ID].Tokens.Total())
}

// withTokens puts a token count before the right side of a row, unless
// that would cut into the title: the count goes before the title does,
// and before the status.
func (m *model) withTokens(left, right []seg, tokens int64) []seg {
	if tokens <= 0 {
		return right
	}
	with := append([]seg{{stFaint, textutil.Count(tokens)}, {stPlain, "  "}}, right...)
	if m.width-segWidth(with)-1 < segWidth(left) {
		return right
	}
	return with
}

// iconWidth is the column the kind icons share: as wide as the widest.
func (m *model) iconWidth() int {
	w := 1
	for _, k := range m.kinds {
		if kw := lipgloss.Width(k.Glyph()); kw > w {
			w = kw
		}
	}
	return w
}

// agentTitle is what an agent's row is called: the name the user gave it,
// else its conversation's title, else claude / claude#2.
func (m *model) agentTitle(p *state.Project, a *state.Agent) string {
	if a.Name != "" {
		return a.Name
	}
	if t := m.transcripts[a.ID].Title; t != "" {
		return t
	}
	return p.DisplayName(a)
}

// detailSegs is the line under an agent: the tool it is calling while it
// runs or waits, else the prompt it is on or was last given.
func (m *model) detailSegs(a *state.Agent) []seg {
	info := m.transcripts[a.ID]
	indent := seg{stPlain, "         "}
	if tr := m.trackers[a.ID]; tr != nil && (tr.Status == status.Running || tr.Status == status.Waiting) && info.Tool != "" {
		return []seg{indent, {stDim, info.Tool}}
	}
	return []seg{indent, {stFaint, info.Prompt}}
}

// projectTokens is what all of p's agents consumed together.
func (m *model) projectTokens(p *state.Project) int64 {
	var n int64
	for _, a := range p.Agents {
		n += m.transcripts[a.ID].Tokens.Total()
	}
	return n
}

// gitSegs is the checkout of dir as shown after a name: the branch, then
// how many files changed and how many commits are unpushed, when any.
func (m *model) gitSegs(dir, fallback string) []seg {
	info, ok := m.gitInfo[dir]
	name := info.Branch
	if name == "" && ok {
		name = "detached"
	}
	if name == "" {
		name = fallback
	}
	if name == "" {
		return nil
	}
	segs := []seg{{stPlain, "  "}, {stFaint, name}}
	if info.Dirty > 0 {
		segs = append(segs, seg{stPlain, " "}, seg{stDim, fmt.Sprintf("±%d", info.Dirty)})
	}
	if info.Ahead > 0 {
		segs = append(segs, seg{stPlain, " "}, seg{stFaint, fmt.Sprintf("↑%d", info.Ahead)})
	}
	return segs
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
	var l1, l2, l3 string
	switch m.mode {
	case modeConfirm:
		l1 = " " + stWaiting.Render(m.confirmMsg)
	case modeWorktree, modeRename:
		title := " name"
		if m.mode == modeWorktree {
			title = " new worktree · " + m.wt.Name
		} else if p, a := m.st.FindAgent(m.renameID); a != nil {
			title += " · " + p.DisplayName(a)
		}
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			title = " " + stFlash.Render(textutil.Truncate(m.flash, m.width-2))
		}
		return rule(m.width) + "\n" +
			layout(m.width, nil, []seg{{stHeader, title}}, []seg{{stFaint, "esc "}}) + "\n" +
			" " + m.input.View()
	case modePickKind:
		title := "new agent"
		if m.wtBranch != "" {
			title += " · " + m.wt.Name + " @ " + m.wtBranch
		} else if r, ok := m.current(); ok {
			title += " · " + r.proj.Name
		}
		var names []string
		for _, k := range m.kinds {
			names = append(names, k.Name)
		}
		return m.menu(title, names, m.kindCursor)
	case modePickFinish:
		var names []string
		for _, a := range m.fin {
			names = append(names, a.Name)
		}
		return m.menu(m.finTitle, names, m.finCursor)
	default:
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			l1 = " " + stFlash.Render(textutil.Truncate(m.flash, m.width-2))
		} else {
			l1 = hints("⏎", "open", "n", "new", "a", "add", "d", "next")
		}
		l2 = hints("w", "worktree", "v", "diff", "f", "finish")
		l3 = hints("t", "name", "x", "kill", "q", "detach")
	}
	return rule(m.width) + "\n" + l1 + "\n" + l2 + "\n" + l3
}

// menu replaces the footer with a titled, numbered list; it is short
// enough for that.
func (m *model) menu(title string, items []string, cursor int) string {
	var b strings.Builder
	b.WriteString(rule(m.width) + "\n")
	b.WriteString(layout(m.width, nil, []seg{{stHeader, " " + title}}, []seg{{stFaint, "esc "}}) + "\n")
	for i, it := range items {
		var bg lipgloss.TerminalColor
		if i == cursor {
			bg = cSelOn
		}
		b.WriteString(layout(m.width, bg, []seg{{stPlain, "  "}, {stKey, fmt.Sprint(i + 1)}, {stPlain, " "}, {stName, it}}, nil) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
