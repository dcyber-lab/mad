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

// render lays the panel out in w columns and about h rows, one block
// after another: the run, what it is about, its flow, its roles, the
// steps taken, how it ended, and its log in the room left. On a short
// pane the steps keep their latest few and the log what is left; the
// blocks that matter least go first.
func (p *panel) render(w, h int) []string {
	w-- // a margin of one on the left
	blocks := []block{
		{text: p.title(w), keep: keepAlways},
		{text: p.about(w), keep: 2, joined: true},
		{text: p.flowView(w), keep: 4},
		{text: p.rolesView(w), keep: 3},
		{steps: true, keep: keepAlways},
		p.endingView(),
		{log: true, keep: keepAlways},
	}
	const fewestSteps = 3
	stepRows := min(len(p.pr.Entries), fewestSteps)
	if stepRows > 0 {
		stepRows++ // the head
	}
	for height(blocks)+stepRows > h {
		drop := -1
		for i, b := range blocks {
			if b.text != "" && b.keep < keepAlways && (drop < 0 || b.keep < blocks[drop].keep) {
				drop = i
			}
		}
		if drop < 0 {
			break
		}
		blocks[drop].text = ""
	}
	var out []string
	for i, b := range blocks {
		text := b.text
		switch {
		case b.steps:
			text = p.stepsView(w, h-height(blocks[i+1:])-len(out)-1)
		case b.log:
			if room := h - len(out) - 1; room >= 1 {
				text = p.logView(room)
			}
		}
		if text == "" {
			continue
		}
		if len(out) > 0 && !b.joined {
			out = append(out, "")
		}
		for _, l := range strings.Split(text, "\n") {
			out = append(out, " "+l)
		}
	}
	return out
}

// block is a part of the panel: what it says, and how long it stays when
// the pane is short, the higher the longer. The steps and the log are
// drawn last, in the room the others leave.
type block struct {
	text       string
	keep       int
	joined     bool // right under the block before, without a blank line
	steps, log bool
}

const keepAlways = 9

// height is the lines blocks take, a blank one between each two (but for
// one joined to the block before); the steps and the log count as
// nothing.
func height(blocks []block) int {
	n := 0
	for _, b := range blocks {
		if b.text != "" {
			n += lipgloss.Height(b.text) + 1
			if b.joined {
				n--
			}
		}
	}
	return n
}

// title is the run's name and how it stands, over a rule.
func (p *panel) title(w int) string {
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
	name := pBold.Render("run " + p.name)
	line := name + "\n" + st // on a narrow pane, one under the other
	if gap := w - lipgloss.Width(name) - lipgloss.Width(st); gap >= 2 {
		line = name + strings.Repeat(" ", gap) + st
	}
	return line + "\n" + pRule.Render(strings.Repeat("─", w))
}

// about is what the run is about: its task, branch, flow and budget.
func (p *panel) about(w int) string {
	pr := p.pr
	budget := pDim.Render("none")
	if p.spec.Budget > 0 {
		frac := pr.Cost / p.spec.Budget
		style := pRun
		switch {
		case frac >= 0.9:
			style = pBad
		case frac >= 0.7:
			style = pWarn
		}
		budget = gauge(frac, share(w, 0.15, 8, 20), style) + " " + textutil.USD(pr.Cost) + pDim.Render(" of "+textutil.USD(p.spec.Budget))
	}
	if pr.Tokens > 0 {
		budget += pDim.Render(" · and " + kTokens(pr.Tokens) + " tokens of models without a price")
	}
	return columns(w, [][]string{
		{pDim.Render("task"), firstLine(p.spec.Task)},
		{pDim.Render("branch"), p.branch + pDim.Render("  "+paths.Short(p.dir))},
		{pDim.Render("flow"), p.flow.Name + pDim.Render("  "+p.flow.Description)},
		{pDim.Render("budget"), budget},
	}, 1)
}

// flowView draws the flow: a box for each step, saying how it stands and
// who takes it, arrows between them, and under them each review's way
// back. A flow too wide for the pane is one line.
func (p *panel) flowView(w int) string {
	loops := p.loops()
	arrowStyle := func(i int) lipgloss.Style {
		if p.stepView(i).runs > 0 {
			return pGood
		}
		return pRule
	}
	var parts []string
	var centers []int
	x := 0
	for i, s := range p.flow.Steps {
		v := p.stepView(i)
		if i > 0 {
			// On the line of the steps' names, under the boxes' tops.
			arrow := "\n" + arrowStyle(i).Render("──▶")
			parts = append(parts, arrow)
			x += lipgloss.Width(arrow)
		}
		head := v.glyph + " " + s.Label
		if v.runs > 1 {
			head += fmt.Sprintf(" ×%d", v.runs)
		}
		text := v.style
		if v.glyph == "●" {
			text = text.Bold(true)
		}
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(v.style.GetForeground()).Padding(0, 1).
			Render(text.Render(head) + "\n" + pDim.Render(p.flow.RoleLabel(s.Role)))
		parts = append(parts, box)
		centers = append(centers, x+lipgloss.Width(box)/2)
		x += lipgloss.Width(box)
	}
	if x > w {
		return p.chain(loops)
	}
	boxes := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	if len(loops) == 0 {
		return boxes
	}
	// Under the boxes: an arrow up into each step a review sends work
	// back to, a line down from the review, and the way between them
	// below, the shortest nearest.
	c := newCanvas(x, len(loops)+1)
	for k, l := range loops {
		a, b, st := centers[l.to], centers[l.from], l.style()
		c.set(0, a, '▲', st)
		for r := 0; r <= k; r++ {
			c.set(r, a, '│', st)
			c.set(r, b, '│', st)
		}
		c.set(k+1, a, '╰', st)
		c.set(k+1, b, '╯', st)
		c.line(k+1, a+1, b-1, st)
		c.label(k+1, a+1, b-1, l.label(), st)
	}
	return boxes + "\n" + c.String()
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
	line := strings.Join(parts, pRule.Render(" → "))
	for _, l := range loops {
		line += "  " + l.style().Render(fmt.Sprintf("↺ %s → %s%s", p.flow.Steps[l.from].Label, p.flow.Steps[l.to].Label,
			strings.TrimSuffix(strings.TrimPrefix(l.label(), " CHANGES"), " ")))
	}
	return line
}

// rolesView is the run's roles: who plays each, how it stands, how full
// its context is against where a claude role gets compacted, what it
// cost, and what it is doing.
func (p *panel) rolesView(w int) string {
	gw := share(w, 0.1, 6, 16)
	rows := [][]string{{pDim.Render("role"), pDim.Render("agent"), pDim.Render("state"), pDim.Render("context"), pDim.Render("cost"),
		pDim.Render(fmt.Sprintf("(context compacts past %s)", kTokens(p.spec.Compact)))}}
	for _, r := range p.flow.Roles {
		who := agent.ByName(p.kinds, r.Kind).Glyph() + " " + r.Label
		v, started := p.roles[r.Name]
		if !started {
			rows = append(rows, []string{who, pDim.Render(roleModel(r)), pRule.Render("not started")})
			continue
		}
		state := pDim
		switch v.state {
		case "working":
			state = pRun
		case "waiting":
			state = pWarn
		case "exited", "stopped":
			if Active(p.pr.Status) {
				state = pBad // a role the run still needs
			}
		}
		ctx := ""
		if c := p.pr.Context[r.Name]; c > 0 {
			ctx = kTokens(c)
			if gw > 0 && p.spec.Compact > 0 {
				frac := float64(c) / float64(p.spec.Compact)
				style := pRun
				if frac >= 0.8 {
					style = pWarn
				}
				ctx = gauge(frac, gw, style) + " " + ctx
			}
		}
		cost := textutil.USD(v.cost)
		if v.cost == 0 && v.tokens > 0 {
			cost = kTokens(v.tokens) + " tok"
		}
		now := ""
		if v.state == "working" {
			now = pDim.Render(v.tool)
		}
		rows = append(rows, []string{who, pDim.Render(roleModel(r)), state.Render(v.state), ctx, cost, now})
	}
	return columns(w, rows, 5)
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

// stepsView is the steps taken, at most room lines of them, the latest:
// for each, who took it, when and for how long against the whole run,
// what it cost and what it came to.
func (p *panel) stepsView(w, room int) string {
	pr := p.pr
	if len(pr.Entries) == 0 {
		return ""
	}
	start := pr.Started
	if start.IsZero() || pr.Entries[0].Start.Before(start) {
		start = pr.Entries[0].Start
	}
	end := pr.Ended
	if end.IsZero() {
		end = p.now
	}
	total := float64(max(end.Sub(start), time.Second))
	sw := share(w, 0.2, 8, 32)

	when := ""
	if sw > 0 {
		from, to := start.Local().Format("15:04"), end.Local().Format("15:04")
		when = from + strings.Repeat(" ", max(sw-len(from)-len(to), 1)) + to
	}
	rows := [][]string{{"", pDim.Render("step"), pDim.Render("role"), pDim.Render(when), pDim.Render("time"), pDim.Render("cost"), ""}}
	// The latest that fit under the head, one line saying how many
	// earlier ones did not.
	entries := pr.Entries
	if fit := max(room-1, 1); len(entries) > fit {
		keep := max(fit-1, 1)
		rows = append(rows, []string{"", pDim.Render(fmt.Sprintf("… %d before", len(entries)-keep))})
		entries = entries[len(entries)-keep:]
	}
	for _, e := range entries {
		glyph, style := entryGlyph(e, pr.Status)
		stop := e.End
		if stop.IsZero() {
			stop = p.now
		}
		label := e.Step
		if i := p.flow.StepIndex(e.Step); i >= 0 {
			label = p.flow.Steps[i].Label
		}
		if e.Round > 1 {
			label += fmt.Sprintf(" #%d", e.Round)
		}
		place := ""
		if sw > 0 {
			place = span(float64(e.Start.Sub(start))/total, float64(stop.Sub(start))/total, sw, style)
		}
		cost := ""
		switch {
		case e.Cost > 0:
			cost = textutil.USD(e.Cost)
		case e.Tokens > 0:
			cost = kTokens(e.Tokens)
		}
		came := result(e.Result)
		if e.Status == "running" {
			came = pDim.Render(p.roles[e.Role].tool)
		}
		rows = append(rows, []string{style.Render(glyph), label, pDim.Render(p.flow.RoleLabel(e.Role)), place, dur(stop.Sub(e.Start)), cost, came})
	}
	return columns(w, rows, 6)
}

// endingView is what the run waits for, or how it ended, and what you
// told it: what it waits for stays longest on a short pane.
func (p *panel) endingView() block {
	var blocks []string
	pr := p.pr
	switch pr.Status {
	case Waiting:
		blocks = append(blocks, pWarn.Bold(true).Render("◆ waiting for you: ")+pWarn.Render(pr.Waiting)+"\n"+
			pDim.Render("  on this run in the sidebar: c continue · x cancel · enter on a role to step in"))
	case Failed:
		blocks = append(blocks, pBad.Render("✗ "+pr.Waiting))
	case Done:
		if len(p.diff) > 0 {
			lines := []string{pDim.Render("changes")}
			for i, l := range p.diff {
				if i >= 6 {
					lines = append(lines, pDim.Render(fmt.Sprintf("  … %d more", len(p.diff)-i)))
					break
				}
				lines = append(lines, " "+l)
			}
			blocks = append(blocks, strings.Join(lines, "\n"))
		}
		blocks = append(blocks, pDim.Render("on this run in the sidebar: f pull request or merge · v diff · x remove"))
	}
	if n := len(pr.Notes); n > 0 {
		rows := [][]string{{pDim.Render("from you"), pDim.Render(fmt.Sprintf("%d · %s/notes.md", n, strings.TrimPrefix(p.logDir, p.dir+"/")))}}
		for _, note := range pr.Notes[max(n-3, 0):] {
			who := "every role"
			if note.Role != "" {
				who = "the " + p.flow.RoleLabel(note.Role)
			}
			rows = append(rows, []string{"", pDim.Render(who+": ") + firstLine(note.Text)})
		}
		blocks = append(blocks, columns(1<<16, rows, -1))
	}
	keep := 1
	if pr.Status == Waiting || pr.Status == Failed {
		keep = keepAlways
	}
	return block{text: strings.Join(blocks, "\n\n"), keep: keep}
}

// logView is the last n lines of the log that the steps do not say.
func (p *panel) logView(n int) string {
	lines := []string{pDim.Render("log  " + paths.Short(p.logDir))}
	for _, l := range events(p.logDir, n-1) {
		lines = append(lines, pDim.Render(l))
	}
	return strings.Join(lines, "\n")
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
