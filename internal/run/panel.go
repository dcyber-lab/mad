package run

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

var (
	pBold  = lipgloss.NewStyle().Bold(true)
	pDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	pRun   = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	pGood  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	pWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	pBad   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	pRule  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	pLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(9)
)

// listPanes is replaceable in tests, which must not look at a real deck.
var listPanes = tmux.ListPanes

// paint draws the run's panel over the runner's pane: what the run is,
// its flow, its roles, the steps taken, and what happened besides.
func (x *runner) paint() {
	if x.readUsage(false) {
		x.save() // what it cost so far, for the sidebar too
	}
	panes, _ := listPanes()
	x.mu.Lock()
	p := x.panel(panes)
	x.mu.Unlock()

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 20 {
		w, h = 100, 40
	}
	// The last row stays free: when the runner is done tmux says so
	// there, and a full screen would scroll the head away.
	h--
	lines := p.render(w, h)
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[H")
	for i, l := range lines {
		if i >= h {
			break
		}
		b.WriteString(ansi.Truncate(l, w, "…") + "\x1b[K")
		if i < len(lines)-1 && i < h-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[J")
	_, _ = x.out.Write([]byte(b.String()))
}

// panel is what the run's panel shows, taken under x.mu so that drawing
// it needs no lock.
type panel struct {
	name, branch, dir, logDir string
	flow                      Flow
	kinds                     []agent.Kind
	spec                      Spec
	pr                        Progress
	roles                     map[string]roleView
	diff                      []string
	now                       time.Time
}

// roleView is how the agent of a role stands.
type roleView struct {
	state  string // working, waiting, idle, asleep, exited, stopped
	tool   string // what it is doing right now
	cost   float64
	tokens int64 // for a model without a price
}

// panel takes what the panel shows; x.mu is held. panes are the deck's,
// nil when they could not be listed.
func (x *runner) panel(panes []tmux.Pane) *panel {
	pr := x.f.Progress
	pr.Entries = append([]Entry(nil), pr.Entries...)
	pr.Notes = append([]Note(nil), pr.Notes...)
	ctx := map[string]int64{}
	for k, v := range pr.Context {
		ctx[k] = v
	}
	pr.Context = ctx
	p := &panel{name: x.run.Name, branch: x.run.Branch, dir: x.run.Dir, logDir: x.dir, flow: x.flow, kinds: x.kinds,
		spec: x.f.Spec, pr: pr, diff: x.diff, now: now(), roles: map[string]roleView{}}
	cur := ""
	if Active(pr.Status) && pr.Step >= 0 && pr.Step < len(x.flow.Steps) {
		cur = x.flow.Steps[pr.Step].Role
	}
	for id, info := range x.usage {
		role := x.roleOf[id]
		v := roleView{tool: info.Tool, cost: info.Tokens.Cost}
		if v.cost == 0 {
			v.tokens = info.Tokens.Total()
		}
		pane, ok := tmux.FindPane(panes, id)
		switch {
		case ok && pane.Dead && pane.Asleep:
			v.state = "asleep"
		case ok && pane.Dead:
			v.state = "exited"
		case panes != nil && !ok:
			v.state = "stopped"
		case role == cur && pr.Status == Waiting:
			v.state = "waiting"
		case role == cur && pr.Status == Running:
			v.state = "working"
		default:
			v.state = "idle"
		}
		p.roles[role] = v
	}
	return p
}

// render lays the panel out in w columns and about h rows: what does not
// fit is the oldest steps and log lines.
func (p *panel) render(w, h int) []string {
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	add(p.head(w)...)
	add("")
	add(p.flowLines(w)...)
	add("")
	add(p.roleLines(w)...)
	tail := p.ending()

	// The steps get the room left, keeping a few lines for the log.
	head, rows := p.timeline(w)
	room := h - len(lines) - len(tail) - 1 - len(head) - 4
	if len(rows) > 0 {
		add("")
		add(head...)
		if room < len(rows) {
			k := max(room-1, 1)
			add(pDim.Render(fmt.Sprintf("   … %d steps before", len(rows)-k)))
			rows = rows[len(rows)-k:]
		}
		add(rows...)
	}
	add(tail...)

	room = h - len(lines) - 2
	if room >= 1 {
		add("")
		add(pDim.Render(" log  " + paths.Short(p.logDir)))
		for _, l := range events(p.logDir, room) {
			add(pDim.Render(" " + l))
		}
	}
	return lines
}

func (p *panel) head(w int) []string {
	pr := p.pr
	end := pr.Ended
	if end.IsZero() {
		end = p.now
	}
	took := ""
	if !pr.Started.IsZero() {
		took = dur(end.Sub(pr.Started))
	}
	spent := textutil.USD(pr.Cost)
	if pr.Tokens > 0 {
		spent += " + " + kTokens(pr.Tokens) + " tok"
	}
	var st string
	switch pr.Status {
	case Starting:
		st = pDim.Render("starting")
	case Running:
		cur := ""
		if pr.Step >= 0 && pr.Step < len(p.flow.Steps) {
			s := p.flow.Steps[pr.Step]
			cur = s.Label + " (" + p.flow.RoleLabel(s.Role) + ")"
		}
		st = pRun.Render(fmt.Sprintf("● %s · round %d · %s · %s", cur, pr.Round, took, spent))
	case Waiting:
		st = pWarn.Bold(true).Render("◆ waiting for you") + pDim.Render(fmt.Sprintf(" · %s · %s", took, spent))
	case Done:
		st = pGood.Render(fmt.Sprintf("✓ approved%s · %s · %s", RoundsOfChanges(pr.Round-1), took, spent))
	case Cancelled:
		st = pBad.Render("✗ cancelled") + pDim.Render(" · "+spent)
	case Failed:
		st = pBad.Render("✗ failed") + pDim.Render(" · "+spent)
	}
	title := pBold.Render(" run " + p.name)
	budget := pDim.Render("no budget")
	if p.spec.Budget > 0 {
		frac := pr.Cost / p.spec.Budget
		style := pRun
		switch {
		case frac >= 0.9:
			style = pBad
		case frac >= 0.7:
			style = pWarn
		}
		budget = bar(frac, 20, style) + " " + textutil.USD(pr.Cost) + pDim.Render(" of "+textutil.USD(p.spec.Budget))
	}
	if pr.Tokens > 0 {
		budget += pDim.Render(" · and " + kTokens(pr.Tokens) + " tokens of models without a price")
	}
	return []string{
		title + strings.Repeat(" ", max(w-lipgloss.Width(title)-lipgloss.Width(st)-1, 1)) + st,
		pRule.Render(" " + strings.Repeat("─", max(w-2, 1))),
		" " + pLabel.Render("task") + firstLine(p.spec.Task),
		" " + pLabel.Render("branch") + p.branch + pDim.Render("  "+paths.Short(p.dir)),
		" " + pLabel.Render("flow") + p.flow.Name + pDim.Render("  "+p.flow.Description),
		" " + pLabel.Render("budget") + budget,
	}
}

// bar is a gauge width cells long, frac of it filled.
func bar(frac float64, width int, style lipgloss.Style) string {
	n := min(max(int(frac*float64(width)+0.5), 0), width)
	if frac > 0 && n == 0 {
		n = 1
	}
	return style.Render(strings.Repeat("█", n)) + pRule.Render(strings.Repeat("░", width-n))
}

// stepView is how a step of the flow stands.
type stepView struct {
	glyph string
	style lipgloss.Style
	runs  int // how often it was taken
}

func (p *panel) stepView(i int) stepView {
	v := stepView{glyph: "○", style: pRule}
	var last *Entry
	for j := range p.pr.Entries {
		if e := &p.pr.Entries[j]; e.Step == p.flow.Steps[i].Name {
			v.runs++
			last = e
		}
	}
	if last != nil {
		v.glyph, v.style = entryGlyph(*last, p.pr.Status)
	}
	return v
}

// entryGlyph marks a step taken: under way, waiting, sent back, done.
func entryGlyph(e Entry, run string) (string, lipgloss.Style) {
	switch e.Status {
	case "running":
		switch run {
		case Waiting:
			return "◆", pWarn
		case Running, Starting:
			return "●", pRun
		}
		return "✗", pBad
	case "changes":
		return "↺", pWarn
	}
	return "✓", pGood
}

// loop is a review's way back to the step it sends changes to.
type loop struct {
	from, to int // step indexes: the review, the step it goes back to
	n        int // how often it went back
}

func (p *panel) loops() []loop {
	var out []loop
	for i, s := range p.flow.Steps {
		t := p.flow.StepIndex(s.Back)
		if !s.Review || t < 0 || t >= i {
			continue
		}
		l := loop{from: i, to: t}
		for _, e := range p.pr.Entries {
			if e.Step == s.Name && e.Status == "changes" {
				l.n++
			}
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].from-out[a].to < out[b].from-out[b].to })
	return out
}

func (l loop) style() lipgloss.Style {
	if l.n > 0 {
		return pWarn
	}
	return pRule
}

func (l loop) label() string {
	if l.n > 0 {
		return fmt.Sprintf(" CHANGES ×%d ", l.n)
	}
	return " CHANGES "
}

// flowLines draws the flow: a box for each step, how it stands and who
// takes it, arrows between them, and under them each review's way back.
// A flow too wide for the pane is one line.
func (p *panel) flowLines(w int) []string {
	type box struct {
		top, mid, low, bottom string
		center                int
	}
	loops := p.loops()
	anchors := map[int]loop{} // box → the loop that ends or starts under it
	for _, l := range loops {
		anchors[l.to], anchors[l.from] = l, l
	}
	var boxes []box
	x := 1
	for i, s := range p.flow.Steps {
		v := p.stepView(i)
		l1 := v.glyph + " " + s.Label
		if v.runs > 1 {
			l1 += fmt.Sprintf(" ×%d", v.runs)
		}
		l2 := p.flow.RoleLabel(s.Role)
		inner := max(lipgloss.Width(l1), lipgloss.Width(l2))
		width := inner + 4
		b := box{center: x + width/2}
		edge := v.style
		label := v.style
		if v.glyph == "●" {
			label = label.Bold(true)
		}
		b.top = edge.Render("╭" + strings.Repeat("─", width-2) + "╮")
		b.mid = edge.Render("│ ") + label.Render(textutil.PadRight(l1, inner)) + edge.Render(" │")
		b.low = edge.Render("│ ") + pDim.Render(textutil.PadRight(l2, inner)) + edge.Render(" │")
		bottom := edge.Render("╰" + strings.Repeat("─", width-2) + "╯")
		if l, ok := anchors[i]; ok {
			mark := "┬"
			if l.to == i {
				mark = "▲"
			}
			left := width/2 - 1
			bottom = edge.Render("╰"+strings.Repeat("─", left)) + l.style().Render(mark) +
				edge.Render(strings.Repeat("─", width-3-left)+"╯")
		}
		b.bottom = bottom
		boxes = append(boxes, b)
		x += width + 3
	}
	if x-3 > w {
		return []string{p.chain(loops)}
	}

	var top, mid, low, bottom strings.Builder
	for _, s := range []*strings.Builder{&top, &mid, &low, &bottom} {
		s.WriteString(" ")
	}
	for i, b := range boxes {
		if i > 0 {
			arrow := pRule
			if p.stepView(i).runs > 0 {
				arrow = pGood
			}
			top.WriteString("   ")
			mid.WriteString(arrow.Render("──▶"))
			low.WriteString("   ")
			bottom.WriteString("   ")
		}
		top.WriteString(b.top)
		mid.WriteString(b.mid)
		low.WriteString(b.low)
		bottom.WriteString(b.bottom)
	}
	lines := []string{top.String(), mid.String(), low.String(), bottom.String()}

	// The ways back, the shortest nearest: one row each, the lines of
	// those below it passing through.
	c := newCanvas(x, len(loops))
	for k, l := range loops {
		a, b := boxes[l.to].center, boxes[l.from].center
		st := l.style()
		for r := 0; r < k; r++ {
			c.set(r, a, '│', st)
			c.set(r, b, '│', st)
		}
		c.set(k, a, '╰', st)
		c.set(k, b, '╯', st)
		for i := a + 1; i < b; i++ {
			c.set(k, i, '─', st)
		}
		if lbl := []rune(l.label()); b-a-1 >= len(lbl)+2 {
			at := a + 1 + (b-a-1-len(lbl))/2
			for i, r := range lbl {
				c.put(k, at+i, r, st)
			}
		}
	}
	return append(lines, c.lines()...)
}

// chain is the flow in one line, for a pane too narrow for its boxes.
func (p *panel) chain(loops []loop) string {
	var parts []string
	for i, s := range p.flow.Steps {
		v := p.stepView(i)
		t := s.Label
		if v.runs > 1 {
			t += fmt.Sprintf(" ×%d", v.runs)
		}
		parts = append(parts, v.style.Render(v.glyph)+" "+t)
	}
	line := " " + strings.Join(parts, pRule.Render(" → "))
	for _, l := range loops {
		line += "  " + l.style().Render(fmt.Sprintf("↺ %s → %s%s", p.flow.Steps[l.from].Label, p.flow.Steps[l.to].Label,
			strings.TrimSuffix(strings.TrimPrefix(l.label(), " CHANGES"), " ")))
	}
	return line
}

// canvas is a grid of one-column runes, each with its style.
type canvas struct {
	cells  [][]rune
	styles [][]lipgloss.Style
}

func newCanvas(w, h int) *canvas {
	c := &canvas{}
	for range h {
		row := make([]rune, w)
		for i := range row {
			row[i] = ' '
		}
		c.cells = append(c.cells, row)
		c.styles = append(c.styles, make([]lipgloss.Style, w))
	}
	return c
}

// set draws a line's rune, joining it with one already there.
func (c *canvas) set(r, col int, ch rune, st lipgloss.Style) {
	if col < 0 || col >= len(c.cells[r]) {
		return
	}
	switch old := c.cells[r][col]; {
	case old == ' ':
	case ch == '│' && old == '─', ch == '─' && old == '│':
		ch = '┼'
	case ch == '│' && old == '╰':
		ch = '├'
	case ch == '│' && old == '╯':
		ch = '┤'
	default:
		return
	}
	c.cells[r][col], c.styles[r][col] = ch, st
}

// put writes text over a line, but not over another line crossing it.
func (c *canvas) put(r, col int, ch rune, st lipgloss.Style) {
	if col >= 0 && col < len(c.cells[r]) && c.cells[r][col] == '─' {
		c.cells[r][col], c.styles[r][col] = ch, st
	}
}

func (c *canvas) lines() []string {
	var out []string
	for r, row := range c.cells {
		var b strings.Builder
		for i := 0; i < len(row); {
			j := i
			for j < len(row) && c.styles[r][j].GetForeground() == c.styles[r][i].GetForeground() {
				j++
			}
			b.WriteString(c.styles[r][i].Render(string(row[i:j])))
			i = j
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// roleLines are the run's roles: who plays each, how it stands, how full
// its context is (against where a claude role is compacted), and what it
// cost.
func (p *panel) roleLines(w int) []string {
	nameW, agentW := 4, 5
	for _, r := range p.flow.Roles {
		nameW = max(nameW, lipgloss.Width(r.Label))
		agentW = max(agentW, lipgloss.Width(roleModel(r)))
	}
	barW := 16
	if w < 90 {
		barW = 0
	}
	lines := []string{pDim.Render(" " + textutil.PadRight("roles", 5+nameW+agentW+2+9) + textutil.PadRight("context", barW+6) +
		textutil.PadRight("  cost", 11) + fmt.Sprintf("a claude role is compacted past %s", kTokens(p.spec.Compact)))}
	for _, r := range p.flow.Roles {
		v, started := p.roles[r.Name]
		glyph := textutil.PadRight(agent.ByName(p.kinds, r.Kind).Glyph(), 2)
		line := "  " + glyph + " " + textutil.PadRight(r.Label, nameW) + "  " + pDim.Render(textutil.PadRight(roleModel(r), agentW)) + "  "
		if !started {
			lines = append(lines, line+pRule.Render("not started"))
			continue
		}
		state := pDim
		switch v.state {
		case "working":
			state = pRun
		case "waiting":
			state = pWarn
		case "exited", "stopped":
			state = pBad
		}
		line += state.Render(textutil.PadRight(v.state, 9))
		c := p.pr.Context[r.Name]
		if barW > 0 {
			gauge := pRun
			if p.spec.Compact > 0 && float64(c) >= 0.8*float64(p.spec.Compact) {
				gauge = pWarn
			}
			frac := 0.0
			if p.spec.Compact > 0 {
				frac = float64(c) / float64(p.spec.Compact)
			}
			line += bar(frac, barW, gauge) + " "
		}
		ctx := ""
		if c > 0 {
			ctx = kTokens(c)
		}
		line += fmt.Sprintf("%5s  ", ctx)
		cost := textutil.USD(v.cost)
		if v.cost == 0 && v.tokens > 0 {
			cost = kTokens(v.tokens) + " tok"
		}
		line += textutil.PadRight(cost, 9)
		if v.state == "working" && v.tool != "" {
			line += pDim.Render(v.tool)
		}
		lines = append(lines, line)
	}
	return lines
}

// roleModel is what plays a role: its model or kind, and its effort.
func roleModel(r Role) string {
	s := r.Kind
	if r.Model != "" {
		s = r.Model
	}
	if r.Effort != "" {
		s += "@" + r.Effort
	}
	return s
}

// timeline is the head and rows of the steps taken: for each, who took
// it, when and how long against the whole run, what it cost and what it
// came to.
func (p *panel) timeline(w int) (head, rows []string) {
	pr := p.pr
	if len(pr.Entries) == 0 {
		return nil, nil
	}
	start := pr.Started
	if start.IsZero() || pr.Entries[0].Start.Before(start) {
		start = pr.Entries[0].Start
	}
	end := pr.Ended
	if end.IsZero() {
		end = p.now
	}
	total := max(end.Sub(start), time.Second)

	stepW, roleW := 4, 4
	label := func(e Entry) string {
		l := e.Step
		if i := p.flow.StepIndex(e.Step); i >= 0 {
			l = p.flow.Steps[i].Label
		}
		if e.Round > 1 {
			l += fmt.Sprintf(" #%d", e.Round)
		}
		return l
	}
	for _, e := range pr.Entries {
		stepW = max(stepW, lipgloss.Width(label(e)))
		roleW = max(roleW, lipgloss.Width(p.flow.RoleLabel(e.Role)))
	}
	barW := 0
	switch {
	case w >= 120:
		barW = 32
	case w >= 95:
		barW = 20
	}
	lead := 4 + stepW + 2 + roleW + 2
	h := " " + textutil.PadRight("steps", lead-1)
	if barW > 0 {
		from, to := start.Local().Format("15:04"), end.Local().Format("15:04")
		h += from + strings.Repeat(" ", max(barW-len(from)-len(to), 1)) + to + " "
	}
	h += textutil.PadRight(" time", 6) + "  cost"
	head = []string{pDim.Render(h)}

	for _, e := range pr.Entries {
		glyph, style := entryGlyph(e, pr.Status)
		stop := e.End
		if stop.IsZero() {
			stop = p.now
		}
		line := "  " + style.Render(glyph) + " " + textutil.PadRight(label(e), stepW) + "  " +
			pDim.Render(textutil.PadRight(p.flow.RoleLabel(e.Role), roleW)) + "  "
		if barW > 0 {
			off := int(float64(e.Start.Sub(start)) / float64(total) * float64(barW))
			n := int(float64(stop.Sub(e.Start))/float64(total)*float64(barW) + 0.5)
			off = min(max(off, 0), barW-1)
			n = min(n, barW-off)
			fill := strings.Repeat("█", n)
			if n == 0 {
				fill, n = "▏", 1
			}
			line += strings.Repeat(" ", off) + style.Render(fill) + strings.Repeat(" ", barW-off-n) + " "
		}
		cost := ""
		switch {
		case e.Cost > 0:
			cost = textutil.USD(e.Cost)
		case e.Tokens > 0:
			cost = kTokens(e.Tokens)
		}
		line += fmt.Sprintf("%5s  %-6s  ", dur(stop.Sub(e.Start)), cost)
		if e.Status == "running" {
			if v := p.roles[e.Role]; v.tool != "" {
				line += pDim.Render(v.tool)
			}
		} else {
			line += result(e.Result)
		}
		rows = append(rows, line)
	}
	return head, rows
}

var resultVerdict = regexp.MustCompile(`^(APPROVE|CHANGES):\s*`)

// result is what a step came to, its verdict in colour and the markdown
// and the repeated verdict taken out.
func result(s string) string {
	v := ""
	if m := resultVerdict.FindStringSubmatch(s); m != nil {
		v, s = m[1], s[len(m[0]):]
	}
	s = strings.TrimLeft(plain(verdictRe.ReplaceAllString(plain(s), "")), " 。.:：,，-—")
	switch {
	case v == Approve:
		v = pGood.Render(v)
	case v == Changes:
		v = pWarn.Render(v)
	default:
		return s
	}
	if s == "" {
		return v
	}
	return v + ": " + s
}

// plain is a line of markdown as text: no emphasis marks or heading.
func plain(s string) string {
	s = strings.NewReplacer("**", "", "__", "").Replace(s)
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "#"))
}

// ending is what the run waits for, or how it ended, and what you told it.
func (p *panel) ending() []string {
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	pr := p.pr
	switch pr.Status {
	case Waiting:
		add("")
		add(" " + pWarn.Bold(true).Render("◆ waiting for you: ") + pWarn.Render(pr.Waiting))
		add(pDim.Render("   on this run in the sidebar: c continue · x cancel · enter on a role to step in"))
	case Failed:
		add("")
		add(" " + pBad.Render("✗ "+pr.Waiting))
	case Done:
		if len(p.diff) > 0 {
			add("")
			add(" " + pLabel.Render("changes"))
			for i, l := range p.diff {
				if i >= 6 {
					add(pDim.Render(fmt.Sprintf("   … %d more", len(p.diff)-i)))
					break
				}
				add("   " + l)
			}
		}
		add("")
		add(pDim.Render(" on this run in the sidebar: f pull request or merge · v diff · x remove"))
	}
	if n := len(pr.Notes); n > 0 {
		add("")
		add(" " + pLabel.Render("from you") + pDim.Render(fmt.Sprintf("%d · %s/notes.md", n, strings.TrimPrefix(p.logDir, p.dir+"/"))))
		for _, note := range pr.Notes[max(n-3, 0):] {
			who := "every role"
			if note.Role != "" {
				who = "the " + p.flow.RoleLabel(note.Role)
			}
			add("   " + pDim.Render(who+": ") + firstLine(note.Text))
		}
	}
	return lines
}

// events are the last n lines of the run's log that the steps above do
// not already say: questions, waits, notes, compactions, failures.
func events(dir string, n int) []string {
	var out []string
	for _, l := range logTail(dir, 500) {
		if strings.Contains(l, " starts the ") || strings.Contains(l, " finished the ") || strings.Contains(l, " starting the ") {
			continue
		}
		out = append(out, l)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func dur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// RoundsOfChanges says how often a review sent the work back, if at all.
func RoundsOfChanges(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return " after 1 round of changes"
	}
	return fmt.Sprintf(" after %d rounds of changes", n)
}

// RoleLine is how a role of the run stands, for the line under its agent
// in the sidebar: the step it is on, or the last it took and what that
// came to; "" before its first.
func RoleLine(f *File, role string) string {
	fl := f.Flow()
	for i := len(f.Progress.Entries) - 1; i >= 0; i-- {
		e := f.Progress.Entries[i]
		if e.Role != role {
			continue
		}
		l := e.Step
		if j := fl.StepIndex(e.Step); j >= 0 {
			l = fl.Steps[j].Label
		}
		if e.Round > 1 {
			l += fmt.Sprintf(" #%d", e.Round)
		}
		if e.Status == "running" {
			return l + " · under way"
		}
		return l + " · " + ansi.Strip(result(e.Result))
	}
	return ""
}
