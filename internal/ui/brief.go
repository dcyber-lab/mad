package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/resume"
	"github.com/dcyber-lab/mad/internal/resume/summarize"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// The resume brief takes over the sidebar next to the agent on stage:
// your bookmark, your decisions, and what changed since you last caught
// up, each saying what it rests on. Opening an agent never counts as
// catching up; only c on a brief does, and only up to that brief's cutoff.

const snapEvery = 10 * time.Second

// Editing steps in the brief.
const (
	editNone = iota
	editAnchor
	editChoice
	editWhy
	editRejected
	editScope
)

type briefView struct {
	agentID  string
	brief    resume.Brief
	loading  bool
	fresh    int // came in after the cutoff
	lastYou  string
	offset   int
	edit     int
	draft    resume.Decision
	checking bool
	lastRef  time.Time

	ai        *summarize.Result // the summarizer plugin's candidates
	aiErr     string
	aiBusy    bool
	aiOff     string       // why no summary runs, when it doesn't
	adopted   map[int]bool // decision candidates you took
	bmAdopted bool
}

// aiMsg is a summarizer run, for the brief with that cutoff.
type aiMsg struct {
	agentID string
	cutoff  time.Time
	res     summarize.Result
	err     error
}

// briefMsg carries what a brief is built from, collected off the UI.
type briefMsg struct {
	agentID string
	snap    resume.Snap
	snapErr error
	in      resume.Input
	auto    bool // opened by going to the agent, not asked for
	warm    bool // a background summary: nothing is shown
	refresh bool // only counting what came in since the cutoff
}

type snapsMsg map[string]resume.Snap

// leaveMsg is the workspace as you left an agent.
type leaveMsg struct {
	agentID, session string
	snap             resume.Snap
	err              error
}

func sessionOf(a *state.Agent) string {
	if a.SessionID != "" {
		return a.SessionID
	}
	return a.ID
}

// collect reads the workspace and transcript of a for a brief.
func (m *model) collect(p *state.Project, a *state.Agent, auto, refresh bool) tea.Cmd {
	dir, kind, sid := p.Dir(a), a.Kind, sessionOf(a)
	in := resume.Input{Dir: dir, Session: sid, Title: m.agentTitle(p, a)}
	for _, o := range p.Agents {
		if o.ID != a.ID && p.Dir(o) == dir {
			in.Shared++
		}
	}
	for _, it := range m.inbox.Pending(time.Now()) {
		if it.AgentID == a.ID && it.Kind == attention.NeedsInput {
			in.Question = it.Message
			if in.Question == "" {
				in.Question = "(a prompt on screen; its text wasn't reported)"
			}
		}
	}
	id := a.ID
	return func() tea.Msg {
		msg := briefMsg{agentID: id, in: in, auto: auto, refresh: refresh}
		msg.snap, msg.snapErr = resume.Take(dir, time.Now())
		if pr := discover.Lookup(kind); pr != nil {
			if files := pr.Transcripts(dir, sid); len(files) > 0 {
				msg.in.Events, msg.in.Followed = resume.Read(kind, files[0])
			}
		}
		msg.in.Now = time.Now()
		return msg
	}
}

// ctxFor is a's context, made on first use.
func (m *model) ctxFor(id string) *resume.Context {
	c := m.ctxs[id]
	if c == nil {
		c = resume.New(id)
		m.ctxs[id] = c
	}
	return c
}

func (m *model) saveCtx(c *resume.Context) {
	if err := c.Save(); err != nil {
		m.setFlash(err.Error())
	}
}

// openBrief shows the brief for a, collecting first.
func (m *model) openBrief(p *state.Project, a *state.Agent) tea.Cmd {
	m.mode = modeBrief
	m.br = briefView{agentID: a.ID, loading: true}
	return tea.Batch(m.collect(p, a, false, false), m.widen())
}

// openAgentResuming opens a and, when it has a resume point, decides from
// the brief how much to show: all of it when something needs you, a line
// otherwise.
func (m *model) openAgentResuming(p *state.Project, a *state.Agent) tea.Cmd {
	cmd := m.openCmd(a)
	if c := m.ctxs[a.ID]; c != nil && (c.Anchor != nil || len(c.Decs) > 0 || c.CaughtUp != nil || c.Left != nil) {
		return tea.Batch(cmd, m.collect(p, a, true, false))
	}
	return cmd
}

func (m *model) applyBrief(msg briefMsg) tea.Cmd {
	if _, a := m.st.FindAgent(msg.agentID); a == nil {
		return nil
	}
	c := m.ctxFor(msg.agentID)
	if msg.snapErr == nil && c.Observe(msg.snap) {
		m.saveCtx(c)
	}
	if msg.warm {
		return m.warmSummarize(msg.agentID, resume.Build(c, msg.in))
	}
	if msg.refresh {
		if m.mode == modeBrief && m.br.agentID == msg.agentID {
			m.br.checking = false
			m.br.fresh = resume.Fresh(m.br.brief, c, msg.in.Events)
		}
		return nil
	}
	b := resume.Build(c, msg.in)
	if msg.snapErr != nil {
		b.Gaps = append(b.Gaps, "workspace not read: "+msg.snapErr.Error())
	}
	lastYou := ""
	for _, e := range msg.in.Events {
		if e.Kind == resume.You && !e.At.After(msg.in.Now) {
			lastYou = e.Text
		}
	}
	if msg.auto {
		if m.mode != modeNormal {
			return nil
		}
		switch b.Level {
		case resume.Attention:
			m.mode = modeBrief
		case resume.Progress:
			m.setFlash(fmt.Sprintf("↺ %s · %d changes · b for the brief", resumeLine(b), len(b.Items)))
			return nil
		default:
			m.setFlash("↺ " + resumeLine(b))
			return nil
		}
	} else if m.mode != modeBrief || m.br.agentID != msg.agentID {
		return nil
	}
	m.br = briefView{agentID: msg.agentID, brief: b, lastYou: lastYou, lastRef: time.Now(), adopted: map[int]bool{}}
	return tea.Batch(m.widen(), m.summarizeCmd())
}

func resumeLine(b resume.Brief) string {
	switch {
	case b.Anchor != nil:
		return "bookmark: " + b.Anchor.Text
	case b.LastYou != "":
		return "your last instruction: " + b.LastYou
	}
	return "no bookmark"
}

// snapCmd keeps the workspace history of agents with a context, so a
// test run can be tied to the code it saw.
func (m *model) snapCmd() tea.Cmd {
	m.snapping, m.lastSnap = true, time.Now()
	dirs := map[string]string{}
	for id := range m.ctxs {
		if p, a := m.st.FindAgent(id); a != nil {
			dirs[id] = p.Dir(a)
		}
	}
	return func() tea.Msg {
		out := snapsMsg{}
		for id, dir := range dirs {
			if s, err := resume.Take(dir, time.Now()); err == nil {
				out[id] = s
			}
		}
		return out
	}
}

func (m *model) applySnaps(msg snapsMsg) {
	m.snapping = false
	for id, s := range msg {
		if c := m.ctxs[id]; c != nil && c.Observe(s) {
			m.saveCtx(c)
		}
	}
}

// leave records the workspace as you moved away from agent id.
func (m *model) leave(id string) tea.Cmd {
	p, a := m.st.FindAgent(id)
	if a == nil {
		return nil
	}
	dir, sid := p.Dir(a), sessionOf(a)
	return func() tea.Msg {
		s, err := resume.Take(dir, time.Now())
		return leaveMsg{agentID: id, session: sid, snap: s, err: err}
	}
}

func (m *model) applyLeave(msg leaveMsg) {
	c := m.ctxFor(msg.agentID)
	cp := resume.Checkpoint{At: time.Now(), Session: msg.session}
	if msg.err == nil {
		c.Observe(msg.snap)
		cur, _ := c.Current()
		cp.Snap = cur.N
	}
	c.Left = &cp
	m.saveCtx(c)
}

// ---- keys ----

func (m *model) keyBrief(k tea.KeyMsg) tea.Cmd {
	if m.br.edit != editNone {
		return m.keyBriefEdit(k)
	}
	p, a := m.st.FindAgent(m.br.agentID)
	if a == nil {
		m.mode = modeNormal
		return nil
	}
	c := m.ctxFor(a.ID)
	switch k.String() {
	case "esc", "q", "b", "ctrl+c":
		m.mode = modeNormal
	case "up", "k":
		m.br.offset = max(m.br.offset-1, 0)
	case "down", "j":
		m.br.offset++
	case "enter":
		m.mode = modeNormal
		m.selectAgent(a.ID)
		return m.openCmd(a)
	case "c":
		if m.br.loading {
			return nil
		}
		cp := m.br.brief.Cutoff
		c.CaughtUp = &cp
		m.saveCtx(c)
		msg := "caught up to " + cp.At.Format("15:04:05") + " (this brief's cutoff)"
		if m.br.fresh > 0 {
			msg += fmt.Sprintf("; %d newer not included", m.br.fresh)
		}
		m.setFlash(msg)
		m.br.loading = true
		return m.collect(p, a, false, false)
	case "e":
		m.br.edit = editAnchor
		m.input.Prompt, m.input.Placeholder = "next › ", "what to look at when you're back"
		m.input.SetValue("")
		if c.Anchor != nil {
			m.input.SetValue(c.Anchor.Text)
		}
		m.input.CursorEnd()
		return m.input.Focus()
	case "p":
		if m.br.lastYou == "" {
			m.setFlash("no message of yours to pin")
			return nil
		}
		c.SetAnchor(firstLineOf(m.br.lastYou), "your message", time.Now())
		m.saveCtx(c)
		m.br.brief.Anchor = c.Anchor
	case "D":
		m.br.edit, m.br.draft = editChoice, resume.Decision{}
		return m.promptDraft()
	case "v":
		m.mode = modeNormal
		return m.toggleDiff(a.ID, p.Dir(a), false)
	case "r":
		m.br.loading = true
		return m.collect(p, a, false, false)
	case "B":
		if ai := m.br.ai; ai != nil && ai.Bookmark != nil && !m.br.bmAdopted {
			c.SetAnchor(ai.Bookmark.Text, "suggested by "+ai.Plugin+", taken by you", time.Now())
			m.saveCtx(c)
			m.br.brief.Anchor, m.br.bmAdopted = c.Anchor, true
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := int(k.String()[0] - '1')
		if ai := m.br.ai; ai != nil && i < len(ai.Decisions) && !m.br.adopted[i] {
			dc := ai.Decisions[i]
			src := fmt.Sprintf("candidate from %s (%s), taken by you", ai.Plugin, dc.Source)
			if dc.Quoted {
				src += " · your words: “" + dc.Quote + "”"
			}
			d := c.AddDecision(resume.Decision{Choice: dc.Choice, Why: dc.Why, Rejected: dc.Rejected, Scope: dc.Scope, Source: src}, time.Now())
			m.saveCtx(c)
			m.br.brief.Decisions, m.br.adopted[i] = c.Active(), true
			m.setFlash(d.ID + " saved from candidate " + k.String())
		}
	}
	return nil
}

var draftPrompts = map[int][2]string{
	editChoice:   {"decided › ", "what you chose"},
	editWhy:      {"why › ", "optional"},
	editRejected: {"not › ", "what you ruled out, optional"},
	editScope:    {"scope › ", "where it applies, optional"},
}

func (m *model) promptDraft() tea.Cmd {
	pr := draftPrompts[m.br.edit]
	m.input.Prompt, m.input.Placeholder = pr[0], pr[1]
	m.input.SetValue("")
	return m.input.Focus()
}

func (m *model) keyBriefEdit(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.br.edit = editNone
		m.input.Blur()
		return nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		c := m.ctxFor(m.br.agentID)
		now := time.Now()
		switch m.br.edit {
		case editAnchor:
			c.SetAnchor(v, "typed", now)
			m.saveCtx(c)
			m.br.brief.Anchor = c.Anchor
			m.br.edit = editNone
			m.input.Blur()
			return nil
		case editChoice:
			if v == "" {
				m.br.edit = editNone
				m.input.Blur()
				return nil
			}
			m.br.draft.Choice = v
		case editWhy:
			m.br.draft.Why = v
		case editRejected:
			m.br.draft.Rejected = v
		case editScope:
			m.br.draft.Scope = v
			d := m.br.draft
			d.Source = "typed by you " + now.Format("15:04")
			if m.br.lastYou != "" {
				d.Source += " · your last message then: “" + textutil.Truncate(firstLineOf(m.br.lastYou), 80) + "”"
			}
			d = c.AddDecision(d, now)
			m.saveCtx(c)
			m.br.brief.Decisions = c.Active()
			m.br.edit = editNone
			m.input.Blur()
			m.setFlash(d.ID + " saved")
			return nil
		}
		m.br.edit++
		return m.promptDraft()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}

func firstLineOf(s string) string {
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
}

// ---- view ----

func itemMark(style string) seg {
	switch style {
	case resume.Block:
		return seg{stWaiting, "?"}
	case resume.Fail:
		return seg{stWaiting, "✗"}
	case resume.Warn:
		return seg{stRunning, "!"}
	case resume.OK:
		return seg{stDone, "✓"}
	case resume.Claim:
		return seg{stDim, "“"}
	}
	return seg{stDim, "±"}
}

func (m *model) briefBody() []string {
	b := m.br.brief
	var out []string
	add := func(segs ...seg) { out = append(out, layout(m.width, nil, segs, nil)) }
	blank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}
	// para wraps text after first (the line's lead), later lines indented to
	// match, at most n lines.
	para := func(first []seg, st lipgloss.Style, text string, n int) {
		lead := segWidth(first)
		w := max(m.width-lead-3, 8) // room for hanging punctuation
		lines := wrapText(text, w)
		if len(lines) > n {
			lines = lines[:n]
			lines[n-1] = runewidth.Truncate(lines[n-1], w-2, "") + " …"
		}
		for i, l := range lines {
			if i == 0 {
				add(append(append([]seg{}, first...), seg{st, l})...)
			} else {
				add(seg{stPlain, strings.Repeat(" ", lead)}, seg{st, l})
			}
		}
	}
	head := func(st lipgloss.Style, s, right string) {
		out = append(out, layout(m.width, nil, []seg{{stPlain, " "}, {st, s}}, []seg{{stFaint, right + " "}}))
	}

	// 1. What needs you, first.
	var needs []string
	for _, n := range b.Needs {
		if !strings.HasPrefix(n, "your bookmark:") {
			needs = append(needs, n)
		}
	}
	if len(needs) > 0 {
		head(stWaiting, "▶ needs you", "")
		for _, n := range needs {
			para([]seg{{stPlain, "   "}}, stName, n, 3)
		}
		blank()
	}

	// 2. The summary, when a plugin made one.
	m.aiSection(para, add, blank, func(s string) { out = append(out, s) })

	// 3. What you set down yourself.
	if b.Anchor != nil {
		para([]seg{{stPlain, " "}, {stKey, "✎ next  "}}, stName, b.Anchor.Text, 3)
	}
	for _, d := range b.Decisions {
		text := d.Choice
		if d.Scope != "" {
			text += "  · " + d.Scope
		}
		para([]seg{{stPlain, " "}, {stDone, "✓ " + d.ID + "    "}}, stName, text, 2)
	}
	if b.Anchor != nil || len(b.Decisions) > 0 {
		blank()
	}

	// 4. The conversation: the last turn in full, earlier ones a line each.
	if n := len(b.Turns); n > 0 {
		last := b.Turns[n-1]
		label := "last turn"
		if b.Before {
			label = "where it was left"
		}
		head(stKey, label, last.At.Local().Format("15:04"))
		para([]seg{{stPlain, "   "}, {stDim, "you    "}}, stName, flatten(last.You), 2)
		if last.Reply != "" {
			para([]seg{{stPlain, "   "}, {stDim, "agent  "}}, stName, flatten(last.Reply), 3)
		} else {
			add(seg{stPlain, "   "}, seg{stDim, "agent  "}, seg{stFaint, "no reply yet"})
		}
		if b.ClaimLast {
			add(seg{stPlain, "          "}, seg{stFaint, "its words, not a check"})
		}
		if n > 1 || b.Earlier > 0 {
			blank()
			head(stDim, "before that", "")
			for i := n - 2; i >= 0; i-- {
				t := b.Turns[i]
				add(seg{stFaint, "   " + t.At.Local().Format("15:04") + "  "}, seg{stDim, firstLineOf(flatten(t.You))})
			}
			if b.Earlier > 0 {
				add(seg{stFaint, fmt.Sprintf("   +%d earlier", b.Earlier)})
			}
		}
		blank()
	} else if b.LastYou != "" {
		para([]seg{{stPlain, " "}, {stDim, "you last said  "}}, stName, b.LastYou, 2)
		blank()
	}

	// 5. Facts, a line each.
	var facts []resume.Item
	for _, it := range b.Items {
		if it.Style != resume.Block { // already under "needs you"
			facts = append(facts, it)
		}
	}
	if len(facts) > 0 {
		head(stDim, "facts", "")
		for _, it := range facts {
			para([]seg{{stPlain, "   "}, itemMark(it.Style), {stPlain, " "}}, stName, it.Text, 2)
		}
		blank()
	}
	if len(b.Turns) == 0 && len(facts) == 0 && len(needs) == 0 {
		add(seg{stFaint, "   nothing collected yet"})
	}
	for _, g := range b.Gaps {
		para([]seg{{stFaint, " · "}}, stFaint, g, 2)
	}
	if off := m.br.aiOff; off != "" && m.br.ai == nil {
		para([]seg{{stFaint, " · "}}, stFaint, off, 2)
	}
	return out
}

// briefStatus is the brief's second line, in plain words: where you left
// off and whether anything happened since.
func briefStatus(b resume.Brief) string {
	var s string
	switch b.Base {
	case "caught up":
		s = "caught up at " + b.BaseAt.Format("15:04")
	case "since you left":
		s = "left at " + b.BaseAt.Format("15:04")
	default:
		s = "first look"
	}
	switch {
	case b.Before || b.Base != "" && len(b.Window) == 0:
		s += " · nothing new since"
	case b.Base != "":
		s += " · " + countOf(len(b.Window), "new turn") + " since"
	}
	return s
}

// wrapText breaks s into lines of at most w columns: between words, and
// between any two CJK characters, which need no space to break.
func wrapText(s string, w int) []string {
	var lines []string
	var cur strings.Builder
	cw := 0
	flush := func() {
		lines = append(lines, strings.TrimRight(cur.String(), " "))
		cur.Reset()
		cw = 0
	}
	for _, tok := range tokens(s) {
		tw := runewidth.StringWidth(tok)
		if tok == " " && cw == 0 {
			continue
		}
		// Closing punctuation hangs at the end of the line rather than
		// starting the next one; callers leave a column for it.
		if cw+tw > w && cw > 0 && !(hangs(tok) && cw+tw <= w+2) {
			flush()
			if tok == " " {
				continue
			}
		}
		for tw > w { // a word longer than a line: cut it
			cut := runewidth.Truncate(tok, w, "")
			cur.WriteString(cut)
			flush()
			tok = strings.TrimPrefix(tok, cut)
			tw = runewidth.StringWidth(tok)
		}
		cur.WriteString(tok)
		cw += tw
	}
	if cw > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// hangs: CJK punctuation that must not start a line.
func hangs(tok string) bool {
	return strings.Contains("，。、；：？！）」』”》", tok) && tok != ""
}

// tokens splits s into words, single spaces, and single wide characters.
func tokens(s string) []string {
	var out []string
	var word strings.Builder
	end := func() {
		if word.Len() > 0 {
			out = append(out, word.String())
			word.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n':
			end()
			out = append(out, " ")
		case runewidth.RuneWidth(r) == 2:
			end()
			out = append(out, string(r))
		default:
			word.WriteRune(r)
		}
	}
	end()
	return out
}

func (m *model) briefView() string {
	var b strings.Builder
	name := m.br.agentID
	if p, a := m.st.FindAgent(m.br.agentID); a != nil {
		name = p.Name + " / " + m.agentWhere(p, a)
	}
	br := m.br.brief
	title := br.Goal
	if title == "" {
		title = name
	}
	b.WriteString(layout(m.width, nil, []seg{{stHeader, " ↺ " + title}}, []seg{{stFaint, "esc "}}) + "\n")
	proj := name
	if p, _ := m.st.FindAgent(m.br.agentID); p != nil {
		proj = p.Name
	}
	sub := proj + " · " + briefStatus(br)
	if m.br.loading {
		sub = "collecting…"
	}
	b.WriteString(stFaint.Render(" "+sub) + "\n")
	b.WriteString(rule(m.width) + "\n")

	footer := 3
	if m.br.edit != editNone {
		footer = 4
	}
	h := max(m.height-3-footer, 1)
	body := []string{}
	if !m.br.loading {
		body = m.briefBody()
	}
	if m.br.offset > max(len(body)-h, 0) {
		m.br.offset = max(len(body)-h, 0)
	}
	lines := 0
	for i := m.br.offset; i < len(body) && lines < h; i++ {
		b.WriteString(body[i] + "\n")
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	b.WriteString(rule(m.width) + "\n")
	if m.br.edit != editNone {
		label := "bookmark: what's next when you come back"
		if m.br.edit >= editChoice {
			label = fmt.Sprintf("decision %d/4 · enter to go on, esc to drop", m.br.edit-editChoice+1)
		}
		b.WriteString(stDim.Render(" "+label) + "\n")
		b.WriteString(" " + m.input.View() + "\n")
		b.WriteString(hints("⏎", "save", "esc", "cancel"))
		return b.String()
	}
	if m.br.fresh > 0 {
		b.WriteString(stRunning.Render(fmt.Sprintf(" %d new since this brief · r to rebuild", m.br.fresh)) + "\n")
	} else {
		b.WriteString(hints("⏎", "continue", "c", "caught up here", "v", "diff") + "\n")
	}
	l3 := hints("e", "bookmark", "p", "pin msg", "D", "decision", "r", "rebuild")
	if ai := m.br.ai; ai != nil && (len(ai.Decisions) > 0 || ai.Bookmark != nil) {
		l3 = hints("1-9", "take", "B", "take bookmark", "e", "edit", "D", "add")
	}
	b.WriteString(l3)
	return b.String()
}

// flatten joins a reply's lines, markdown marks dropped: it is shown
// wrapped under the turn.
func flatten(s string) string {
	var parts []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>-* "))
		if l != "" && !strings.HasPrefix(l, "|--") && !strings.HasPrefix(l, "```") {
			parts = append(parts, strings.ReplaceAll(l, "**", ""))
		}
	}
	return strings.Join(parts, " ")
}

func countOf(n int, s string) string {
	if n == 1 {
		return "1 " + s
	}
	return fmt.Sprintf("%d %ss", n, s)
}

// ---- the summarizer plugin ----

// summarizeCmd runs the configured summarizer on the brief just built,
// when the project allows it. Its answer only ever adds candidates.
func (m *model) summarizeCmd() tea.Cmd {
	p, a := m.st.FindAgent(m.br.agentID)
	if a == nil {
		return nil
	}
	cfg := m.cfg.Brief
	if !cfg.Allowed(p.Path) {
		m.br.aiOff = "summaries off for this project · brief.summarizer in config.json"
		if cfg.Summarizer != "" {
			m.br.aiOff = "summaries not allowed for " + p.Name + " · add it to brief.projects"
		}
		return nil
	}
	plug, err := summarize.New(cfg)
	if err != nil {
		m.br.aiErr = err.Error()
		return nil
	}
	req := resume.Request(m.br.brief, cfg.MaxInput())
	if len(req.Turns) == 0 {
		m.br.aiOff = "nothing to summarize since the baseline"
		return nil
	}
	m.br.aiBusy = true
	id, cutoff := a.ID, m.br.brief.Cutoff.At
	return func() tea.Msg {
		res, err := summarize.Run(plug, cfg, req)
		return aiMsg{agentID: id, cutoff: cutoff, res: res, err: err}
	}
}

func (m *model) applyAI(msg aiMsg) {
	if m.br.agentID != msg.agentID || !m.br.brief.Cutoff.At.Equal(msg.cutoff) {
		return // a later brief replaced the one it was for
	}
	m.br.aiBusy = false
	if msg.err != nil {
		m.br.aiErr = msg.err.Error()
		return
	}
	res := msg.res
	m.br.ai = &res
}

func (m *model) aiSection(para func([]seg, lipgloss.Style, string, int), add func(...seg), blank func(), raw func(string)) {
	br := m.br
	switch {
	case br.aiBusy:
		add(seg{stKey, " ✦ "}, seg{stDim, "summarizing…"})
		blank()
		return
	case br.aiErr != "":
		para([]seg{{stKey, " ✦ "}}, stWaiting, "summarizer failed: "+br.aiErr, 2)
		blank()
		return
	case br.ai == nil:
		return // why not is a line at the bottom
	}
	ai := br.ai
	who := ai.Plugin
	if ai.Model != "" {
		who = ai.Model
	}
	who = strings.TrimPrefix(who, "claude-")
	right := who + fmt.Sprintf(" · $%.3f", ai.CostUSD)
	if ai.Cached {
		right = who + " · ready " + ai.At.Local().Format("15:04")
	}
	raw(layout(m.width, nil, []seg{{stPlain, " "}, {stKey, "✦ summary"}}, []seg{{stFaint, right + " "}}))
	para([]seg{{stPlain, "   "}}, stName, ai.Summary, 4)
	for _, q := range ai.Open[:min(len(ai.Open), 2)] {
		para([]seg{{stPlain, "   "}, {stWaiting, "? "}}, stName, q.Text, 2)
	}
	for i, d := range ai.Decisions {
		if i == 3 {
			break
		}
		mark := seg{stKey, fmt.Sprintf("[%d] ", i+1)}
		if br.adopted[i] {
			mark = seg{stDone, " ✓  "}
		}
		para([]seg{{stPlain, "   "}, mark}, stName, "decided? "+d.Choice, 2)
		basis := "your words not found in " + d.Source
		if d.Quoted {
			basis = "“" + d.Quote + "” " + d.Source
		}
		para([]seg{{stPlain, "       "}}, stFaint, basis, 1)
	}
	if bm := ai.Bookmark; bm != nil {
		mark := seg{stKey, "[B] "}
		if br.bmAdopted {
			mark = seg{stDone, " ✓  "}
		}
		para([]seg{{stPlain, "   "}, mark}, stName, "next? "+bm.Text, 2)
	}
	if ai.Dropped > 0 {
		add(seg{stFaint, fmt.Sprintf("   %d items dropped: they cited turns that weren't sent", ai.Dropped)})
	}
	blank()
}

// ---- summaries ahead of time ----

// warmAfter lets a turn that ended settle: agents often go on at once.
const warmAfter = 20 * time.Second

type warmMsg struct {
	agentID string
	res     summarize.Result
	err     error
}

// scheduleWarm plans a background summary when a turn of a ends while you
// are elsewhere, and calls it off when the agent goes on.
func (m *model) scheduleWarm(p *state.Project, a *state.Agent, prev, cur string, now time.Time) {
	switch {
	case cur == status.Running:
		delete(m.warmDue, a.ID)
	case prev == status.Running && (cur == status.Idle || cur == status.Waiting) && a.ID != m.stageID:
		if cfg := m.cfg.Brief; cfg.Automatic() && cfg.Allowed(p.Path) {
			m.warmDue[a.ID] = now.Add(warmAfter)
		}
	}
}

// warmNext starts the next due background summary; one at a time.
func (m *model) warmNext(now time.Time) tea.Cmd {
	if m.warming {
		return nil
	}
	for id, due := range m.warmDue {
		if now.Before(due) {
			continue
		}
		delete(m.warmDue, id)
		p, a := m.st.FindAgent(id)
		if a == nil || a.ID == m.stageID {
			continue
		}
		m.warming = true
		collect := m.collect(p, a, false, false)
		return func() tea.Msg {
			msg := collect().(briefMsg)
			msg.warm = true
			return msg
		}
	}
	return nil
}

// warmSummarize runs the summarizer on a brief built only for that: the
// result lands in the cache the real brief reads.
func (m *model) warmSummarize(id string, b resume.Brief) tea.Cmd {
	cfg := m.cfg.Brief
	req := resume.Request(b, cfg.MaxInput())
	plug, err := summarize.New(cfg)
	if err != nil || len(req.Turns) == 0 {
		m.warming = false
		return nil
	}
	return func() tea.Msg {
		res, err := summarize.Run(plug, cfg, req)
		return warmMsg{agentID: id, res: res, err: err}
	}
}

func (m *model) applyWarm(msg warmMsg) {
	m.warming = false
	if msg.err != nil {
		logSidebar("background summary of %s: %v", msg.agentID, msg.err)
	}
}

// logSidebar notes something that happened in the background, where no
// flash would be seen, in the sidebar's log.
func logSidebar(format string, args ...any) {
	if f, err := os.OpenFile(paths.SidebarLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
		f.Close()
	}
}
