package ui

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// The bus block tops the list: what needs you (the inbox, as the window
// has it), what finished unseen, the board, and the channels with
// something new for you. The bus is files, so a look is cheap when
// nothing changed: bus.Source reads the messages again only when a file
// moved (or now and then, for what depends on the clock).

const (
	// busEvery: how often the bus files are looked at.
	busEvery = 2 * time.Second
	// busRowsMax: channels listed in the block; the rest are counted.
	busRowsMax = 6
	// inboxRowsMax: items listed under "Needs you"; the rest are counted.
	inboxRowsMax = 6
)

// busRow is a row of the bus block: the inbox's head, one of its items,
// the finished count, the board, a channel, or a count of rows left out.
type busRow struct {
	needs int             // the "Needs you" head, with the count
	item  *attention.Item // an item of the inbox
	// moreItems is the "+N more" row of the inbox; finished the count of
	// agents finished unseen (item is the newest of them).
	moreItems int
	finished  int

	board  bool
	ch     string // channel id
	title  string // #name, @agent
	unread int
	paused bool
	// For the board: what waits for you (the inbox), and open cards.
	waiting, open int
	more          int // the channels' "+N more" row
}

// same tells whether a and b are the same row, for the cursor to stay on.
func (b *busRow) same(o *busRow) bool {
	switch {
	case b.item != nil || o.item != nil:
		return b.item != nil && o.item != nil && b.item.Key == o.item.Key && (b.finished > 0) == (o.finished > 0)
	case b.needs > 0 || o.needs > 0:
		return b.needs > 0 && o.needs > 0
	case b.moreItems > 0 || o.moreItems > 0:
		return b.moreItems > 0 && o.moreItems > 0
	}
	return b.board == o.board && b.ch == o.ch && b.more == o.more
}

// busSnap is what the bus block shows of the bus: every listed channel,
// in the order they are listed, and the board's open cards.
type busSnap struct {
	open  int
	chans []busRow
}

// busMsg is one look at the bus: what the inbox takes from it, and the
// block's channels.
type busMsg struct {
	cards  []bus.Card
	skills []skill.Pending
	snap   busSnap
}

// busSnapOf is what the block shows of look; open is the channel whose
// chat is on stage, which stays listed: its row closes it.
func busSnapOf(st *state.State, look bus.Look, open string) busSnap {
	var s busSnap
	for _, c := range look.Cards {
		if c.Column != bus.ColDone {
			s.open++
		}
	}
	for _, c := range look.Channels {
		if c.Archived {
			continue
		}
		isDM := strings.HasPrefix(c.ID, "dm.")
		y := look.Yours[c.ID]
		if isDM && !yourDM(c.ID) || !y.Listed && c.ID != open {
			continue
		}
		r := busRow{ch: c.ID, title: "#" + c.ID, unread: y.Unread, paused: c.Paused}
		if isDM {
			r.title = "@" + dmName(st, c)
		}
		s.chans = append(s.chans, r)
	}
	// Channels before DMs, as the window lists them; unread before paused.
	sort.SliceStable(s.chans, func(i, j int) bool {
		a, b := s.chans[i], s.chans[j]
		if (a.unread > 0) != (b.unread > 0) {
			return a.unread > 0
		}
		return !strings.HasPrefix(a.ch, "dm.") && strings.HasPrefix(b.ch, "dm.")
	})
	return s
}

// loadBus reads the bus into what the block shows, afresh. statuses are
// the agents' (nil before the first poll: the board is left out then).
func loadBus(st *state.State, statuses map[string]string, now time.Time, open string) busSnap {
	var src bus.Source
	look, _ := src.Look(st, statuses, now)
	return busSnapOf(st, look, open)
}

func yourDM(ch string) bool {
	for _, p := range strings.Split(strings.TrimPrefix(ch, "dm."), ".") {
		if p == bus.You {
			return true
		}
	}
	return false
}

// dmName names the agent in a DM with you.
func dmName(st *state.State, c bus.Channel) string {
	var names []string
	for _, id := range bus.DMSides(c.ID) { // the id keeps both sides when the members don't
		if id == bus.You {
			continue
		}
		if _, a := st.FindAgent(id); a != nil {
			names = append(names, drive.Label(a))
		} else {
			names = append(names, drive.ShortID(id))
		}
	}
	if len(names) == 0 {
		return strings.TrimPrefix(c.ID, "dm.")
	}
	return strings.Join(names, ", ")
}

// busRows are the rows of the block: what needs you and its items (up to
// inboxRowsMax), what finished, the board, then the channels up to
// busRowsMax and how many were left out.
func (m *model) busRows() []busRow {
	var out []busRow
	in := m.view.Inbox
	if needs := in.NeedsYou(); len(needs) > 0 {
		out = append(out, busRow{needs: len(needs)})
		for i := range needs[:min(len(needs), inboxRowsMax)] {
			out = append(out, busRow{item: &needs[i]})
		}
		if n := len(needs) - inboxRowsMax; n > 0 {
			out = append(out, busRow{moreItems: n})
		}
	}
	if fin := in.Finished(); len(fin) > 0 {
		out = append(out, busRow{finished: len(fin), item: &fin[0]})
	}
	s := m.bus
	if s.open > 0 || in.Needs > 0 {
		out = append(out, busRow{board: true, title: "Board", waiting: in.Needs, open: s.open})
	}
	out = append(out, s.chans[:min(len(s.chans), busRowsMax)]...)
	if n := len(s.chans) - busRowsMax; n > 0 {
		out = append(out, busRow{more: n})
	}
	return out
}

// busCmd looks at the bus off the UI goroutine.
func (m *model) busCmd(now time.Time) tea.Cmd {
	m.busBusy, m.lastBus = true, now
	open := "" // the channel whose chat is on stage
	if d, ok := m.panes[tmux.IDTask]; ok && !d.Dead && m.shown[tmux.IDTask] && strings.HasPrefix(m.taskTag, "chat:") {
		open = strings.TrimPrefix(m.taskTag, "chat:")
	}
	st := m.st.Clone()
	var statuses map[string]string
	if len(m.trackers) > 0 || len(st.OrderedAgents()) == 0 {
		statuses = map[string]string{}
		for id, tr := range m.trackers {
			if tr != nil && tr.Status != "" {
				statuses[id] = tr.Status
			}
		}
	}
	src := m.busSrc
	return func() tea.Msg {
		look, _ := src.Look(st, statuses, time.Now())
		skills, _ := skill.PendingList()
		return busMsg{cards: look.Cards, skills: skills, snap: busSnapOf(st, look, open)}
	}
}

// applyBus takes a look at the bus in; the rows change only when it did.
func (m *model) applyBus(msg busMsg) {
	m.busBusy = false
	m.cards, m.skills = msg.cards, msg.skills
	m.setBus(msg.snap)
	m.look(time.Now())
}

func (m *model) setBus(s busSnap) {
	if reflect.DeepEqual(s, m.bus) {
		return
	}
	m.bus = s
	m.rebuildRows()
}

// narrowRow is the width under which an item's text comes before its
// context in the block: what is cut is the context, not the command.
const narrowRow = 44

// busRowSegs lays out a row of the block.
func (m *model) busRowSegs(b *busRow) (left, right []seg) {
	switch {
	case b.needs > 0:
		l := status.LookOf(status.Waiting)
		left = []seg{{stPlain, " "}, {stNeeds, m.glyph(l) + " "}, {stNeeds, l.Word}}
		right = []seg{{stNeeds, fmt.Sprint(b.needs)}, {stPlain, " "}}
	case b.finished > 0:
		l := status.LookOf(status.Finished)
		left = []seg{{stPlain, " "}, m.shownSeg(status.Finished), {stPlain, " "}, {toneStyle(l.Tone), l.Word}}
		right = []seg{{toneStyle(l.Tone), fmt.Sprint(b.finished)}, {stPlain, " "}}
	case b.item != nil:
		it := b.item
		left = []seg{{stPlain, "   "}, {stName, it.Title}}
		if it.Context != "" && m.width >= narrowRow {
			left = append(left, seg{stPlain, "  "}, seg{stDim, it.Context})
		}
		if it.Text != "" {
			left = append(left, seg{stFaint, " · " + it.Text})
		}
		if it.Context != "" && m.width < narrowRow {
			// narrow: the end is cut, and the command says more than its tool
			left = append(left, seg{stPlain, "  "}, seg{stDim, it.Context})
		}
		if !it.Since.IsZero() {
			right = []seg{{stFaint, textutil.AgeAt(it.Since, time.Now())}, {stPlain, " "}}
		}
	case b.moreItems > 0:
		left = []seg{{stPlain, "   "}, {stDim, fmt.Sprintf("+%d more", b.moreItems)}}
	case b.board:
		left = []seg{{stPlain, " "}, {stDim, "▤ "}, {stProject, "Board"}}
		switch {
		case b.waiting > 0:
			right = []seg{{stNeeds, fmt.Sprintf("%d for you", b.waiting)}, {stPlain, " "}}
		case b.open > 0:
			right = []seg{{stDim, fmt.Sprintf("%d open", b.open)}, {stPlain, " "}}
		}
	case b.more > 0:
		left = []seg{{stPlain, " "}, {stDim, fmt.Sprintf("+%d more", b.more)}}
	default:
		left = []seg{{stPlain, " "}, {stName, b.title}}
		if b.paused {
			right = append(right, seg{stNeeds, "paused"}, seg{stPlain, " "})
		}
		if b.unread > 0 {
			right = append(right, seg{stKey, fmt.Sprint(b.unread)}, seg{stPlain, " "})
		}
	}
	return left, right
}

// openBus opens a row of the block on stage: an item where it is settled,
// a channel's chat, or the board; again, a chat or the board closes.
func (m *model) openBus(b *busRow) tea.Cmd {
	switch {
	case b.item != nil:
		m.nextKey = b.item.Key
		return m.openItem(*b.item)
	case b.needs > 0:
		return m.jumpNext()
	case b.moreItems > 0:
		return m.showBusView("board", deck.SelfCommand("board -w"))
	case b.board:
		return m.toggleBusView("board", deck.SelfCommand("board -w"))
	case b.more > 0:
		m.setFlash("mad bus ls lists them all")
		return nil
	}
	return m.toggleBusView("chat:"+b.ch, chatCommand(b.ch))
}

func chatCommand(ch string) string { return deck.SelfCommand("bus chat " + paths.ShellQuote(ch)) }

// toggleBusView shows cmd in the task pane, as the diff view is, or takes
// it down when it is up already.
func (m *model) toggleBusView(tag, cmd string) tea.Cmd {
	if d, ok := m.panes[tmux.IDTask]; ok && !d.Dead && m.shown[tmux.IDTask] && m.taskTag == tag {
		return m.closeTask(false)
	}
	return m.showBusView(tag, cmd)
}

// showBusView shows cmd in the task pane, unless it shows it already.
func (m *model) showBusView(tag, cmd string) tea.Cmd {
	if d, ok := m.panes[tmux.IDTask]; ok && !d.Dead && m.shown[tmux.IDTask] && m.taskTag == tag {
		return nil
	}
	m.taskFor, m.taskTag = "", tag
	dir, _ := os.UserHomeDir()
	if len(m.st.Projects) > 0 {
		dir = m.st.Projects[0].Path
	}
	return m.action("", func() error { return deck.OpenTask(dir, cmd) })
}

// busKeys are the keys that mean something on a row of the block.
var busKeys = map[string]bool{
	"up": true, "k": true, "down": true, "j": true, "g": true, "home": true, "G": true, "end": true,
	"enter": true, "l": true, "right": true, " ": true, "<": true, "-": true, ">": true, "=": true, "+": true,
	"tab": true, "a": true, "d": true, "i": true, "q": true, "ctrl+c": true, "Q": true,
}
