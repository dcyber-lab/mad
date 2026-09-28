package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/workspace"
)

// The inbox takes over the sidebar: what still needs you, most blocking
// first. Opening an item is not handling it; it closes when the agent
// moves on (answered, a new turn), or when you say so.

const (
	inboxHeader = 3 // title, sources, rule
	snoozeFor   = 30 * time.Minute
)

type inboxView struct {
	order  []string // item ids as first shown: new items go to the end, nothing reshuffles under you
	cursor int
	offset int
	detail bool
}

// attend turns what observe saw of a into inbox items. It runs whatever
// the notification settings are: the inbox is not the notifications.
func (m *model) attend(p *state.Project, a *state.Agent, prev, cur string, hook *status.Hook, onStage bool, now time.Time) bool {
	b := m.inbox
	hooked := agent.ByName(m.kinds, a.Kind).Hooks && hook != nil
	src, ref := attention.FromScreen, ""
	if hooked {
		src, ref = attention.FromHook, hook.At.UTC().Format(time.RFC3339Nano)
	}
	base := attention.Item{AgentID: a.ID, Project: p.Path, Source: src, SourceRef: ref}
	changed := false
	raise := func(kind attention.Kind, msg string) {
		n := base
		n.Kind, n.Message = kind, msg
		it, ch := b.Raise(n, now)
		if onStage && ch {
			it.SeenAt = now // in front of you as it happened
		}
		changed = changed || ch
	}
	switch {
	case cur == status.Waiting:
		// A hook report is an event: raise it unless it was already (the
		// sidebar restarted). A screen is a guess: only a change seen here.
		if hooked && !b.Known(a.ID, attention.NeedsInput, ref) {
			raise(attention.NeedsInput, hook.Message)
		} else if !hooked && prev != "" && prev != status.Waiting {
			raise(attention.NeedsInput, "")
		}
	case cur == status.Running && prev != "" && prev != status.Running:
		changed = b.Settle(a.ID, []attention.Kind{attention.Exited}, attention.Resolved, "restarted", now) || changed
		if hooked { // the agent itself says it went on: the question was answered
			changed = b.Settle(a.ID, []attention.Kind{attention.NeedsInput}, attention.Resolved, "answered", now) || changed
			changed = b.Settle(a.ID, []attention.Kind{attention.TurnEnded}, attention.Superseded, "continued", now) || changed
		}
	case prev == status.Running && cur == status.Idle:
		if !hooked { // a whole run since the prompt, not one redraw
			changed = b.Settle(a.ID, []attention.Kind{attention.NeedsInput}, attention.Resolved, "answered (screen)", now) || changed
		}
		if hooked || now.Sub(m.runSince[a.ID]) >= notifyMinRun {
			raise(attention.TurnEnded, "")
		}
	case cur == status.Exited && prev != "" && prev != status.Exited && prev != status.Stopped:
		raise(attention.Exited, "the process ended")
	}
	return changed
}

func (m *model) saveInbox() {
	if err := m.inbox.Save(); err != nil {
		m.setFlash(err.Error())
	}
}

func (m *model) openInbox() tea.Cmd {
	m.mode = modeInbox
	m.ib = inboxView{}
	for _, it := range m.inbox.Pending(time.Now()) {
		m.ib.order = append(m.ib.order, it.ID)
	}
	return m.widen()
}

// inboxItems is the list as shown: the order it opened with, minus what
// was handled, plus what came in since at the end.
func (m *model) inboxItems(now time.Time) []*attention.Item {
	pending := m.inbox.Pending(now)
	open := map[string]*attention.Item{}
	for _, it := range pending {
		open[it.ID] = it
	}
	var out []*attention.Item
	shown := map[string]bool{}
	for _, id := range m.ib.order {
		if it := open[id]; it != nil {
			out, shown[id] = append(out, it), true
		}
	}
	for _, it := range pending {
		if !shown[it.ID] {
			out = append(out, it)
		}
	}
	return out
}

func (m *model) inboxListHeight() int {
	h := m.height - inboxHeader - 2
	if m.ib.detail {
		h -= 7 // rule and six lines
	}
	if h < 2 {
		h = 2
	}
	return h
}

func (m *model) moveInbox(d, n int) {
	m.ib.cursor += d
	if m.ib.cursor >= n {
		m.ib.cursor = n - 1
	}
	if m.ib.cursor < 0 {
		m.ib.cursor = 0
	}
	per := m.inboxListHeight() / 2
	if m.ib.cursor < m.ib.offset {
		m.ib.offset = m.ib.cursor
	}
	if m.ib.cursor >= m.ib.offset+per {
		m.ib.offset = m.ib.cursor - per + 1
	}
}

func (m *model) keyInbox(k tea.KeyMsg) tea.Cmd {
	now := time.Now()
	items := m.inboxItems(now)
	var it *attention.Item
	if m.ib.cursor < len(items) {
		it = items[m.ib.cursor]
	}
	switch k.String() {
	case "esc", "q", "i", "ctrl+c":
		m.mode = modeNormal
	case "up", "k":
		m.moveInbox(-1, len(items))
	case "down", "j":
		m.moveInbox(1, len(items))
	case "d":
		m.ib.detail = !m.ib.detail
	case "r":
		if it != nil {
			m.inbox.Resolve(it.ID, "marked by you", now)
			m.saveInbox()
			m.moveInbox(0, len(items)-1)
		}
	case "s":
		if it != nil {
			m.inbox.Snooze(it.ID, now.Add(snoozeFor))
			m.saveInbox()
			m.setFlash("snoozed until " + now.Add(snoozeFor).Format("15:04"))
			m.moveInbox(0, len(items)-1)
		}
	case "R":
		if it != nil && it.Kind == attention.SetupFailed {
			m.mode = modeNormal
			return m.retryRun(it)
		}
	case "enter", "l":
		if it != nil {
			return m.openItem(it, now)
		}
	}
	return nil
}

// openItem goes to what the item is about. It marks it seen, not handled.
func (m *model) openItem(it *attention.Item, now time.Time) tea.Cmd {
	it.SeenAt = now
	m.saveInbox()
	if it.Kind == attention.SetupFailed {
		r, err := workspace.LoadRun(it.RunID)
		if err != nil {
			m.setFlash(err.Error())
			return nil
		}
		log := ""
		if s, ok := r.FailedStep(); ok {
			log = s.Log
		}
		if log == "" {
			m.setFlash(r.Summary())
			return nil
		}
		m.mode, m.taskFor = modeNormal, ""
		dir := r.Plan.Dir
		if !r.Worktree {
			dir = r.Plan.Repo
		}
		cmd := "less -R +G " + paths.ShellQuote(log)
		return m.action("", func() error { return deck.OpenTask(dir, cmd) })
	}
	p, a := m.st.FindAgent(it.AgentID)
	if a == nil {
		m.setFlash("that agent is gone; r to clear the item")
		return nil
	}
	m.mode = modeNormal
	m.selectAgent(a.ID)
	return m.openAgentResuming(p, a)
}

// itemLabel is "project / branch-or-title" of an item.
func (m *model) itemLabel(it *attention.Item) string {
	proj := filepath.Base(it.Project)
	if p := m.st.FindProject(it.Project); p != nil {
		proj = p.Name
	}
	if it.AgentID == "" {
		return proj + " / " + it.Where
	}
	p, a := m.st.FindAgent(it.AgentID)
	if a == nil {
		return proj + " / (removed)"
	}
	return proj + " / " + m.agentWhere(p, a)
}

// agentWhere names an agent by its branch when it has its own checkout,
// else by its title.
func (m *model) agentWhere(p *state.Project, a *state.Agent) string {
	if a.Dir != "" && a.Dir != p.Path {
		if b := m.gitInfo[a.Dir].Branch; b != "" {
			return b
		}
		return filepath.Base(a.Dir)
	}
	return m.agentTitle(p, a)
}

func itemGlyph(k attention.Kind) seg {
	switch k {
	case attention.NeedsInput:
		return seg{stWaiting, "?"}
	case attention.TurnEnded:
		return seg{stDone, "●"}
	case attention.SetupFailed:
		return seg{stRunning, "!"}
	}
	return seg{stDim, "✗"}
}

// itemWhy is the item's second line: why, how long, and on what basis.
func itemWhy(it *attention.Item) (string, lipgloss.Style) {
	switch it.Kind {
	case attention.NeedsInput:
		if it.Message != "" {
			return "needs input · “" + it.Message + "”", stWaiting
		}
		if it.Source == attention.FromScreen {
			return "needs input · prompt on screen", stWaiting
		}
		return "needs input", stWaiting
	case attention.TurnEnded:
		if it.Source == attention.FromScreen {
			return "maybe idle · screen went quiet, check it", stDim
		}
		return "turn ended · result not looked at", stDone
	case attention.SetupFailed:
		return "setup failed · " + it.Message, stRunning
	}
	return "exited · " + it.Message, stDim
}

func sourceName(s string) string {
	switch s {
	case attention.FromHook:
		return "agent lifecycle event"
	case attention.FromScreen:
		return "screen guess"
	}
	return "mad (exit code, log)"
}

func (m *model) inboxView() string {
	now := time.Now()
	items := m.inboxItems(now)
	if m.ib.cursor >= len(items) {
		m.ib.cursor = max(len(items)-1, 0)
	}
	var b strings.Builder
	title := fmt.Sprintf(" ⚑ inbox · %d open", len(items))
	b.WriteString(layout(m.width, nil, []seg{{stHeader, title}}, []seg{{stFaint, "esc "}}) + "\n")
	b.WriteString(stFaint.Render(" blocked first · then longest waiting") + "\n")
	b.WriteString(rule(m.width) + "\n")

	h := m.inboxListHeight()
	lines := 0
	if len(items) == 0 {
		b.WriteString(stDim.Render(" nothing needs you") + "\n")
		lines++
	}
	for i := m.ib.offset; i < len(items) && lines+1 < h; i++ {
		it := items[i]
		var bg lipgloss.TerminalColor
		if i == m.ib.cursor {
			bg = cSelOn
		}
		name := stName
		if !it.Seen() {
			name = stProject // unseen: bold
		}
		left := []seg{{stPlain, " "}, itemGlyph(it.Kind), {stPlain, " "}, {name, m.itemLabel(it)}}
		right := []seg{{stFaint, textutil.Age(it.CreatedAt) + " "}}
		if it.AgentID != "" {
			if _, a := m.st.FindAgent(it.AgentID); a != nil {
				right = append([]seg{{stFaint, a.Kind}, {stPlain, "  "}}, right...)
			}
		}
		b.WriteString(layout(m.width, bg, left, right) + "\n")
		why, st := itemWhy(it)
		b.WriteString(layout(m.width, bg, []seg{{stPlain, "   "}, {st, why}}, nil) + "\n")
		lines += 2
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	if m.ib.detail {
		b.WriteString(m.inboxDetail(items, now))
	}
	b.WriteString(rule(m.width) + "\n")
	l1, l2 := hints("⏎", "open", "r", "resolved", "s", "snooze"), hints("d", "details", "esc", "back")
	if m.ib.cursor < len(items) && items[m.ib.cursor].Kind == attention.SetupFailed {
		l1 = hints("⏎", "log", "R", "retry", "r", "resolved", "s", "snooze")
	}
	b.WriteString(l1 + "\n" + l2)
	return b.String()
}

// inboxDetail is six lines on the selected item: what it rests on.
func (m *model) inboxDetail(items []*attention.Item, now time.Time) string {
	lines := make([]string, 6)
	if m.ib.cursor < len(items) {
		it := items[m.ib.cursor]
		seen := "not yet"
		if !it.SeenAt.IsZero() {
			seen = textutil.Age(it.SeenAt) + " ago"
		}
		lines[0] = " " + stDim.Render("source  ") + stName.Render(sourceName(it.Source))
		lines[1] = " " + stDim.Render("since   ") + stName.Render(it.CreatedAt.Format("15:04:05")) + stFaint.Render(" · updated "+it.UpdatedAt.Format("15:04:05"))
		lines[2] = " " + stDim.Render("seen    ") + stName.Render(seen)
		switch {
		case it.Kind == attention.SetupFailed:
			if r, err := workspace.LoadRun(it.RunID); err == nil {
				lines[3] = " " + stDim.Render("run     ") + stName.Render(r.Summary())
				if s, ok := r.FailedStep(); ok {
					lines[4] = " " + stDim.Render("log     ") + stFaint.Render(paths.Short(s.Log))
				}
				lines[5] = " " + stDim.Render("dir     ") + stFaint.Render(paths.Short(r.Plan.Dir))
			}
		case it.AgentID != "":
			if p, a := m.st.FindAgent(it.AgentID); a != nil {
				who := p.DisplayName(a)
				if t := m.agentTitle(p, a); t != who {
					who += " · " + t
				}
				lines[3] = " " + stDim.Render("agent   ") + stName.Render(who)
				lines[4] = " " + stDim.Render("dir     ") + stFaint.Render(paths.Short(p.Dir(a)))
			}
			if it.Message != "" {
				lines[5] = " " + stDim.Render("says    ") + stName.Render(it.Message)
			}
		}
	}
	return rule(m.width) + "\n" + strings.Join(lines, "\n") + "\n"
}
