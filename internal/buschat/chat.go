// Package buschat is `mad bus chat`: one channel (or DM) of the bus read
// and written in a terminal, as the window does it. The deck opens it on
// stage from the sidebar's bus block.
package buschat

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
)

// pollEvery is how often the channel's file is looked at.
const pollEvery = time.Second

var (
	cAccent = lipgloss.Color("75")
	cText   = lipgloss.Color("252")
	cDim    = lipgloss.Color("243")
	cWarn   = lipgloss.Color("203")
	cDone   = lipgloss.Color("78")
	cBusy   = lipgloss.Color("214")

	stHead  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stYou   = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stName  = lipgloss.NewStyle().Bold(true).Foreground(cText)
	stDim   = lipgloss.NewStyle().Foreground(cDim)
	stWarn  = lipgloss.NewStyle().Foreground(cWarn)
	stOK    = lipgloss.NewStyle().Foreground(cDone)
	stBusy  = lipgloss.NewStyle().Foreground(cBusy)
	stPlain = lipgloss.NewStyle()
)

var mention = regexp.MustCompile(`@[\p{L}\p{N}_.\-]+`)

type tickMsg time.Time

// Model is the chat view of one channel.
type Model struct {
	channel  string
	title    string
	topic    string
	paused   bool
	readOnly bool // a DM between two agents: yours to read, not to write in
	st       *state.State
	members  []string // what @ completes

	msgs   []bus.Message
	last   int
	scroll int // lines above the newest
	width  int
	height int

	input  textinput.Model
	status string
	isErr  bool

	tabbing bool   // an @word is being completed
	tabWord string // what it started with
	tabBase string // the input before it
	tabIdx  int
}

// New opens channel: its last messages are read, and marked read.
func New(channel string) (*Model, error) {
	c, ok, err := bus.Find(channel)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no channel #%s", channel)
	}
	st, _ := state.Load()
	if st == nil {
		st = &state.State{}
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 4000
	ti.Focus()
	m := &Model{channel: channel, st: st, input: ti, width: 80, height: 24}
	m.describe(c)
	m.readOnly = strings.HasPrefix(channel, "dm.") && !yoursDM(channel)
	if err := m.poll(); err != nil {
		return nil, err
	}
	return m, nil
}

// yoursDM tells whether ch is a DM the person is in.
func yoursDM(ch string) bool {
	for _, p := range strings.Split(strings.TrimPrefix(ch, "dm."), ".") {
		if p == bus.You {
			return true
		}
	}
	return false
}

// describe takes the title, topic, pause and member names from c.
func (m *Model) describe(c bus.Channel) {
	m.topic, m.paused = c.Topic, c.Paused
	var names []string
	m.members = nil
	for _, id := range c.Members {
		if _, a := m.st.FindAgent(id); a != nil {
			names = append(names, drive.Label(a))
			m.members = append(m.members, drive.Label(a))
		}
	}
	m.members = append(m.members, "all")
	switch {
	case !strings.HasPrefix(c.ID, "dm."):
		m.title = "#" + c.ID
	case yoursDM(c.ID):
		m.title = "@" + strings.Join(names, ", ")
	default:
		m.title = strings.Join(names, " ↔ ")
	}
}

// poll reads what is new and marks it read.
func (m *Model) poll() error {
	before := len(m.render())
	msgs, last, err := bus.Read(m.channel, m.last, 200)
	if err != nil {
		return err
	}
	if c, ok, _ := bus.Find(m.channel); ok {
		m.describe(c)
	}
	if len(msgs) > 0 {
		m.msgs = append(m.msgs, msgs...)
		m.last = msgs[len(msgs)-1].Seq
		if m.scroll > 0 { // keep what is being read where it is
			m.scroll += len(m.render()) - before
		}
	}
	if last > 0 {
		_ = bus.MarkRead(bus.You, m.channel, last)
	}
	return nil
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(pollEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(msg.Width-4, 1)
	case tickMsg:
		if err := m.poll(); err != nil {
			m.fail(err)
		}
		return m, m.Init()
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) fail(err error)  { m.status, m.isErr = err.Error(), true }
func (m *Model) say(s string)    { m.status, m.isErr = s, false }
func (m *Model) viewHeight() int { return max(m.height-4, 1) }

func (m *Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.input.Value() == "" {
			return m, tea.Quit
		}
		m.input.SetValue("")
		m.tabbing = false
		return m, nil
	case "q":
		if m.readOnly { // no input to type it in
			return m, tea.Quit
		}
	case "pgup":
		m.scrollBy(max(m.viewHeight()/2, 1))
		return m, nil
	case "pgdown":
		m.scrollBy(-max(m.viewHeight()/2, 1))
		return m, nil
	case "end":
		if m.scroll > 0 { // else it is the input's
			m.scroll = 0
			return m, nil
		}
	case "tab":
		if !m.readOnly {
			m.complete()
		}
		return m, nil
	case "enter":
		if !m.readOnly {
			m.send()
		}
		return m, nil
	}
	if m.readOnly {
		switch k.String() {
		case "up", "k":
			m.scrollBy(1)
		case "down", "j":
			m.scrollBy(-1)
		}
		return m, nil
	}
	m.tabbing = false
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m *Model) scrollBy(n int) {
	maxUp := max(len(m.render())-m.viewHeight(), 0)
	m.scroll = min(max(m.scroll+n, 0), maxUp)
}

// send posts the input as the person.
func (m *Model) send() {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return
	}
	me := bus.From{ID: bus.You, Label: bus.You}
	var res bus.PostResult
	var err error
	if strings.HasPrefix(m.channel, "dm.") {
		ids := strings.Split(strings.TrimPrefix(m.channel, "dm."), ".")
		to := ids[0]
		if to == bus.You && len(ids) > 1 {
			to = ids[1]
		}
		_, res, err = bus.DM(me, to, text)
	} else {
		res, err = bus.Post(m.channel, me, text, 0)
	}
	if err != nil {
		m.fail(err)
		return
	}
	m.input.SetValue("")
	m.tabbing = false
	m.scroll = 0
	s := fmt.Sprintf("sent #%d", res.Seq)
	if len(res.Woke) > 0 {
		s += "; woke " + strings.Join(res.Woke, ", ")
	}
	if res.Warning != "" {
		s += " (" + res.Warning + ")"
	}
	m.say(s)
	if err := m.poll(); err != nil {
		m.fail(err)
	}
}

// complete finishes the @word at the end of the input with a member's
// label; pressed again, it goes on to the next one.
func (m *Model) complete() {
	v := m.input.Value()
	if !m.tabbing {
		i := strings.LastIndexAny(v, " \n") + 1
		w := v[i:]
		if !strings.HasPrefix(w, "@") {
			return
		}
		m.tabbing, m.tabWord, m.tabBase, m.tabIdx = true, strings.ToLower(w[1:]), v[:i], -1
	}
	var cands []string
	for _, l := range m.members {
		if strings.HasPrefix(strings.ToLower(l), m.tabWord) {
			cands = append(cands, l)
		}
	}
	if len(cands) == 0 {
		return
	}
	m.tabIdx = (m.tabIdx + 1) % len(cands)
	m.input.SetValue(m.tabBase + "@" + cands[m.tabIdx] + " ")
	m.input.CursorEnd()
}

// label is who a message is from, as the deck names them now.
func (m *Model) label(id, fallback string) string {
	if id == bus.You {
		return bus.You
	}
	if _, a := m.st.FindAgent(id); a != nil {
		return drive.Label(a)
	}
	if fallback == "" {
		return drive.ShortID(id)
	}
	return fallback
}

// render lays out all the messages for the width, a line each.
func (m *Model) render() []string {
	w := max(m.width, 20)
	var out []string
	for _, msg := range m.msgs {
		out = append(out, m.renderMsg(msg, w)...)
	}
	return out
}

func (m *Model) renderMsg(msg bus.Message, w int) []string {
	if msg.Kind == "system" {
		var out []string
		for _, l := range wrap("· "+msg.Text, w-1) {
			out = append(out, stDim.Render(" "+l))
		}
		return out
	}
	who := m.label(msg.From.ID, msg.From.Label)
	whoSt := stName
	if msg.From.ID == bus.You {
		whoSt = stYou
	}
	head := " " + whoSt.Render(who) + stDim.Render(fmt.Sprintf("  #%d  %s", msg.Seq, msg.At.Local().Format("15:04")))
	if msg.ReplyTo > 0 {
		head += stDim.Render(fmt.Sprintf("  ↳ #%d", msg.ReplyTo))
	}
	body := stPlain
	switch msg.Kind {
	case "handoff":
		to := ""
		if msg.Handoff != nil {
			to = " → " + m.label(msg.Handoff.To, "")
		}
		head += stBusy.Render("  handoff" + to)
		body = stBusy
	case "done":
		head += stOK.Render("  done")
		body = stOK
	}
	out := []string{ansi.Truncate(head, w, "")}
	for _, l := range wrap(msg.Text, w-3) {
		out = append(out, "   "+styleLine(l, body))
	}
	return out
}

// styleLine makes @mentions bold in a line drawn in st.
func styleLine(l string, st lipgloss.Style) string {
	idx := mention.FindAllStringIndex(l, -1)
	if len(idx) == 0 {
		return st.Render(l)
	}
	var b strings.Builder
	at := 0
	for _, p := range idx {
		b.WriteString(st.Render(l[at:p[0]]))
		b.WriteString(st.Bold(true).Render(l[p[0]:p[1]]))
		at = p[1]
	}
	b.WriteString(st.Render(l[at:]))
	return b.String()
}

// wrap breaks text into lines of at most w cells, keeping its own breaks.
func wrap(text string, w int) []string {
	w = max(w, 8)
	var out []string
	for _, para := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		para = strings.TrimRight(para, " \r\t")
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(para, w, ""), "\n")...)
	}
	return out
}

// View is the title, the messages, a status line and the input.
func (m *Model) View() string {
	w := max(m.width, 20)
	head := " " + stHead.Render(m.title)
	if m.topic != "" {
		head += stDim.Render(" · " + m.topic)
	}
	if m.paused {
		head += " " + stWarn.Render("paused · mad bus resume "+m.channel)
	}
	lines := m.render()
	h := m.viewHeight()
	end := len(lines) - min(m.scroll, max(len(lines)-h, 0))
	start := max(end-h, 0)
	view := append([]string(nil), lines[start:end]...)
	for len(view) < h { // the newest sits at the bottom
		view = append([]string{""}, view...)
	}
	status := ""
	switch {
	case m.status != "" && m.isErr:
		status = " " + stWarn.Render(m.status)
	case m.status != "":
		status = " " + stDim.Render(m.status)
	case m.scroll > 0:
		status = " " + stDim.Render("scrolled up · end to follow")
	case m.readOnly:
		status = " " + stDim.Render("a DM between agents: read only · q closes")
	default:
		status = " " + stDim.Render("enter sends · tab completes @ · pgup/pgdn scroll · esc closes")
	}
	in := stDim.Render(" (read only)")
	if !m.readOnly {
		in = " " + m.input.View()
	}
	all := append([]string{ansi.Truncate(head, w, ""), stDim.Render(strings.Repeat("─", w))}, view...)
	all = append(all, ansi.Truncate(status, w, ""), ansi.Truncate(in, w, ""))
	return strings.Join(all, "\n")
}

// Run is `mad bus chat CHANNEL`.
func Run(channel string) error {
	m, err := New(channel)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
