package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/agent"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/transcript"
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
	if len(m.st.Projects) == 0 {
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
		r := m.rows[i]
		if r.teamHead != "" {
			if l >= m.offset && lines+1 >= h {
				break // a head with no room for anyone under it is not drawn
			}
			if l >= m.offset { // else only the lines under it are in view
				left, right := m.teamHeadSegs(i)
				b.WriteString(layout(m.width, nil, left, right) + "\n")
				next++
				lines++
			}
			l++
			rh--
		}
		if l >= m.offset && lines < h { // else only its second line is in view
			left, right := m.rowSegs(r)
			b.WriteString(layout(m.width, bg, left, right) + "\n")
			next++
			lines++
		}
		if rh > 1 && lines < h {
			detail := m.detailSegs(r)
			b.WriteString(layout(m.width, bg, detail, nil) + "\n")
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

// renderHeader is the brand plus what needs you: the inbox's count, the
// agents at work and those finished while away, as the window counts
// them; or the agent count when all is quiet.
func (m *model) renderHeader() string {
	agents := m.st.OrderedAgents()
	waiting, running, done := m.view.Inbox.Needs, len(m.view.Working), len(m.view.Inbox.Finished())
	var right []seg
	add := func(n int, state string) {
		if n > 0 {
			ic := m.shownSeg(state)
			right = append(right, seg{ic.st, fmt.Sprintf("%s%d", ic.s, n)}, seg{stPlain, "  "})
		}
	}
	for _, f := range m.runs {
		if f.Progress.Status == madrun.Waiting {
			waiting++
		}
	}
	add(waiting, status.Waiting)
	add(running, status.Running)
	add(done, status.Finished)
	if len(right) > 0 {
		right[len(right)-1].s = " "
	} else {
		n := fmt.Sprintf("%d agents ", len(agents))
		if len(agents) == 1 {
			n = "1 agent "
		}
		right = []seg{{stFaint, n}}
	}
	left := []seg{{stHeader, " ⧉ mad"}}
	// What today cost so far, where it fits whole: a cut amount misleads.
	if m.dayCost > 0 {
		with := append(left, seg{stPlain, "  "}, seg{stFaint, "today " + textutil.USD(m.dayCost)})
		if segWidth(with)+1+segWidth(right) <= m.width {
			left = with
		}
	}
	return layout(m.width, nil, left, right)
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

// quotaStyle colors a used percentage: plain, then amber from 80%, red
// from 95%.
func quotaStyle(used float64) lipgloss.Style {
	switch {
	case used >= 95:
		return stError
	case used >= 80:
		return stNeeds
	}
	return stName
}

// rowSegs lays out one row: the left part is cut to fit, the right part
// (status, count, tty) is right-aligned and dropped when too narrow.
func (m *model) rowSegs(r row) (left, right []seg) {
	switch {
	case r.bus != nil:
		return m.busRowSegs(r.bus)
	case r.ext != nil:
		return []seg{{stPlain, "     "}, {stDim, "↗ "}, {stDim, r.ext.Kind}},
			[]seg{{stFaint, r.ext.TTY + " "}}
	case r.desktop > 0:
		return []seg{{stPlain, "     "}, {stDim, "◇ "}, {stDim, fmt.Sprintf("%d in desktop", r.desktop)}}, nil
	case r.run != nil:
		return m.runRowSegs(r)
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
		return left, m.withUsage(left, right, m.projectUsage(r.proj))
	}
	a := r.agent
	st, attention := status.Stopped, false
	if tr := m.trackers[a.ID]; tr != nil && tr.Status != "" {
		st, attention = tr.Status, tr.Attention
	}
	bar, name := seg{stPlain, " "}, seg{stName, m.agentTitle(r.proj, a)}
	if m.onStage(a.ID) {
		bar, name = seg{stStage, "▌"}, seg{stStage, name.s}
	}
	num := " "
	if r.num <= 9 {
		num = fmt.Sprint(r.num)
	}
	if inRun(r.proj, a) {
		attention = false // its run says when it is done
	}
	icon, label := m.statusGlyph(st, attention)
	k := agent.ByName(m.kinds, a.Kind)
	kind := seg{stName.Bold(true), textutil.PadRight(k.Glyph(), m.iconWidth())}
	if k.Color != "" {
		kind.st = kind.st.Foreground(lipgloss.Color(k.Color))
	}
	left = []seg{{stPlain, " "}, bar, {stPlain, " "}, {stFaint, num}, {stPlain, " "}, icon, {stPlain, " "}, kind, {stPlain, " "}, name}
	if n := m.queued[a.ID]; n > 0 {
		// Messages waiting for its turn to end (mad queue).
		left = append(left, seg{stFaint, fmt.Sprintf(" ⇢%d", n)})
	}
	if inRun(r.proj, a) || r.inTeam {
		// A role: indented under its run or team, a run's with the model it was given.
		left = append(left[:2], append([]seg{{stPlain, m.indent()}}, left[2:]...)...)
		if model := modelOf(a); model != "" && !r.inTeam {
			left = append(left, seg{stFaint, " " + model})
		}
	}
	if !inRun(r.proj, a) && a.Dir != "" && a.Dir != r.proj.Path && !m.teamShares(r) {
		// Its own checkout: name it, even before the first git scan.
		left = append(left, m.gitSegs(a.Dir, filepath.Base(a.Dir))...)
	}
	right = []seg{label, {stPlain, " "}}
	// The × goes before the title would be cut (the usage before it); a
	// role goes with its run.
	with := []seg{label, {stPlain, " "}, {stFaint, closeGlyph}, {stPlain, " "}}
	if !inRun(r.proj, a) && m.width-segWidth(with)-1 >= segWidth(left) {
		right = with
	}
	return left, m.withUsage(left, right, usageText(m.transcripts[a.ID].Tokens))
}

// closeGlyph ends an agent's row: clicking it kills the agent.
const closeGlyph = "×"

// onClose is whether a click at column x of screen line line (counted
// from the top of the list, scrolled lines included) is on row i's ×,
// give or take a column.
func (m *model) onClose(i, x, line int) bool {
	main := m.lineOf[i]
	if m.rows[i].teamHead != "" {
		main++
	}
	if line != main || x < m.width-3 {
		return false
	}
	left, right := m.rowSegs(m.rows[i])
	n := len(right)
	return n >= 2 && right[n-2].s == closeGlyph && fitsRight(m.width, left, right)
}

// withUsage puts what was consumed before the right side of a row, unless
// that would cut into the title: it goes before the title does, and
// before the status.
func (m *model) withUsage(left, right []seg, usage string) []seg {
	if usage == "" {
		return right
	}
	with := append([]seg{{stFaint, usage}, {stPlain, "  "}}, right...)
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
	if inRun(p, a) {
		if f := m.runs[a.Run]; f != nil {
			return f.Flow().RoleLabel(a.Role)
		}
		for _, f := range madrun.Builtin() { // until its run is read
			if r, ok := f.Role(a.Role); ok {
				return r.Label
			}
		}
		return a.Role
	}
	if a.Name != "" {
		return a.Name
	}
	if t := m.transcripts[a.ID].Title; t != "" {
		return t
	}
	return p.DisplayName(a)
}

// detailSegs is the line under an agent: the tool it is calling while it
// runs or waits, else the prompt it is on or was last given, or for a
// role of a run its last step. Under a run, where the run stands.
func (m *model) detailSegs(r row) []seg {
	if r.run != nil {
		return m.runDetailSegs(r.run)
	}
	a := r.agent
	info := m.transcripts[a.ID]
	indent := seg{stPlain, "         "}
	if inRun(r.proj, a) || r.inTeam {
		indent.s += m.indent()
	}
	if tr := m.trackers[a.ID]; tr != nil && (tr.Status == status.Running || tr.Status == status.Waiting) && info.Tool != "" {
		return []seg{indent, {stDim, info.Tool}}
	}
	// A role's prompts are mad's: what it last came to says more.
	if f := m.runs[a.Run]; f != nil && inRun(r.proj, a) {
		if line := madrun.RoleLine(f, a.Role); line != "" {
			return []seg{indent, {stFaint, line}}
		}
	}
	return []seg{indent, {stFaint, info.Prompt}}
}

// usageText is what a session consumed: what it cost, else, for a model
// without a known price, its tokens.
func usageText(t transcript.Totals) string {
	switch {
	case t.Cost > 0:
		return textutil.USD(t.Cost)
	case t.Total() > 0:
		return textutil.Count(t.Total())
	}
	return ""
}

// projectUsage is what all of p's agents consumed together: what they
// cost, plus the tokens of those without a price.
func (m *model) projectUsage(p *state.Project) string {
	var usd float64
	var unpriced int64
	for _, a := range p.Agents {
		if t := m.transcripts[a.ID].Tokens; t.Cost > 0 {
			usd += t.Cost
		} else {
			unpriced += t.Total()
		}
	}
	switch {
	case usd == 0 && unpriced == 0:
		return ""
	case usd == 0:
		return textutil.Count(unpriced)
	case unpriced == 0:
		return textutil.USD(usd)
	}
	return textutil.USD(usd) + " + " + textutil.Count(unpriced)
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

// glyph is the icon of a shown state: its table glyph, a spinner frame
// while it works.
func (m *model) glyph(l status.Look) string {
	if l.State == status.Running {
		return m.spin()
	}
	return l.Glyph
}

// statusGlyph is the icon and the word of an agent's tracked status s,
// as status.Shown reads it with attention.
func (m *model) statusGlyph(s string, attention bool) (icon, label seg) {
	l := status.LookOf(status.Shown(s, attention))
	st, ist := toneStyle(l.Tone), toneStyle(l.Tone)
	if l.Tone == status.ToneQuiet {
		ist = stDim
	}
	return seg{ist, m.glyph(l)}, seg{st, l.Word}
}

// projectSummary is the most urgent state inside a collapsed project,
// when it is more than quiet.
func (m *model) projectSummary(p *state.Project) seg {
	var shown []string
	for _, a := range p.Agents {
		if tr := m.trackers[a.ID]; tr != nil && tr.Status != "" {
			shown = append(shown, status.Shown(tr.Status, tr.Attention && !inRun(p, a)))
		}
	}
	top := status.Roll(shown).Top()
	if top == "" || status.LookOf(top).Tone == status.ToneQuiet {
		return seg{}
	}
	return m.shownSeg(top)
}

// indent is how far a role sits under its run or team: less on a narrow
// sidebar, where a name cut short is worse than a flush edge.
func (m *model) indent() string {
	switch {
	case m.width < 28:
		return "" // the head says it; the name is worth the columns
	case m.width < 36:
		return " "
	}
	return "  "
}

// teamMembers are the rows of the team whose head row i is: the agents
// that follow it, together.
func (m *model) teamMembers(i int) []row {
	var out []row
	for j := i; j < len(m.rows); j++ {
		r := m.rows[j]
		if r.agent == nil || !r.inTeam || r.proj != m.rows[i].proj || r.agent.Team != m.rows[i].teamHead || j > i && r.teamHead != "" {
			break
		}
		out = append(out, r)
	}
	return out
}

// teamShares is whether r is a member of a team whose agents all work in one
// checkout other than the project's: the team's head names it, not each row.
func (m *model) teamShares(r row) bool {
	if !r.inTeam {
		return false
	}
	for i, x := range m.rows {
		if x.agent == r.agent {
			for k := i; k >= 0; k-- {
				if m.rows[k].teamHead != "" {
					return m.teamDir(k) != ""
				}
			}
		}
	}
	return false
}

// teamDir is the checkout the team under head row i shares, or "" when its
// agents are not all in one checkout of their own.
func (m *model) teamDir(i int) string {
	if len(m.teamMembers(i)) < 2 {
		return "" // a team of one: its row says it
	}
	dir := ""
	for _, r := range m.teamMembers(i) {
		d := r.agent.Dir
		if d == "" || d == r.proj.Path || dir != "" && d != dir {
			return ""
		}
		dir = d
	}
	return dir
}

// teamHeadSegs is the line over a team's agents: its name, the checkout they
// share if they do, and on the right how they stand, as a project's folded
// head says it (the most urgent state that is not quiet) with the number of
// agents.
func (m *model) teamHeadSegs(i int) (left, right []seg) {
	r := m.rows[i]
	var shown []string
	members := m.teamMembers(i)
	for _, x := range members {
		if tr := m.trackers[x.agent.ID]; tr != nil && tr.Status != "" {
			shown = append(shown, status.Shown(tr.Status, tr.Attention))
		}
	}
	left = []seg{{stPlain, " "}, {stPlain, m.indent()}, {stKey, "team "}, {stProject, r.teamHead}}
	if dir := m.teamDir(i); dir != "" {
		left = append(left, m.gitSegs(dir, filepath.Base(dir))...)
	}
	if top := status.Roll(shown).Top(); top != "" && status.LookOf(top).Tone != status.ToneQuiet {
		right = append(right, m.shownSeg(top), seg{stPlain, " "})
	}
	right = append(right, seg{stFaint, fmt.Sprintf("%d ", len(members))})
	return left, right
}

// shownSeg is the colored icon of a shown state.
func (m *model) shownSeg(state string) seg {
	l := status.LookOf(state)
	return seg{toneStyle(l.Tone), m.glyph(l)}
}

func (m *model) renderFooter() string {
	var l1, l2, l3 string
	switch m.mode {
	case modeConfirm:
		l1 = " " + stNeeds.Render(m.confirmMsg)
	case modeWorktree, modeRename, modeNote:
		title := " name"
		switch {
		case m.mode == modeWorktree:
			title = " new worktree · " + m.wt.Name
		case m.mode == modeNote:
			title = " note to every role · run " + m.noteRun.Name
		default:
			if p, a := m.st.FindAgent(m.renameID); a != nil {
				title += " · " + p.DisplayName(a)
			}
		}
		if m.flash != "" && time.Now().Before(m.flashUntil) {
			title = " " + m.flashStyle().Render(textutil.Truncate(m.flash, m.width-2))
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
			l1 = " " + m.flashStyle().Render(textutil.Truncate(m.flash, m.width-2))
		} else if m.spend.Closed {
			l1 = " " + stNeeds.Render(spendFooter(m.spend))
		} else {
			l1 = hints("⏎", "open", "n", "new", "a", "add", "d", "next")
		}
		l2 = hints("w", "worktree", "s", "split", "v", "diff", "f", "finish")
		l3 = hints("o", "run", "t", "name", "x", "kill", "q", "detach")
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

// modelOf is the model an agent was started with (--model) and its
// effort (claude's --effort, codex's model_reasoning_effort), if given.
func modelOf(a *state.Agent) string {
	var model, effort string
	for i, arg := range a.Args {
		next := ""
		if i+1 < len(a.Args) {
			next = a.Args[i+1]
		}
		switch {
		case arg == "--model":
			model = next
		case arg == "--effort":
			effort = next
		case arg == "-c" && strings.HasPrefix(next, "model_reasoning_effort="):
			effort = strings.TrimPrefix(next, "model_reasoning_effort=")
		}
	}
	return strings.TrimSpace(model + " " + effort)
}
