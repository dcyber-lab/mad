package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// The bus block tops the list: the board, and the channels with something
// new for you. The bus is files, so a look is cheap when nothing changed:
// the files' sizes and times are compared first, and the messages are read
// only when one moved (or now and then, for what depends on the clock).

const (
	// busEvery: how often the bus files are looked at.
	busEvery = 2 * time.Second
	// busFullEvery: how often the board is derived again whatever the
	// files say (a task stalls with time, an agent's status changes).
	busFullEvery = 15 * time.Second
	// busRowsMax: channels listed in the block; the rest are counted.
	busRowsMax = 6
)

// busRow is a row of the bus block: the board, a channel, or the count of
// channels left out.
type busRow struct {
	board  bool
	ch     string // channel id
	title  string // #name, @agent
	unread int
	paused bool
	// For the board: cards waiting for you, and open ones in all.
	waiting, open int
	more          int // the "+N more" row
}

// busSnap is what the bus block shows: every channel with news or paused,
// in the order they are listed, and the board's counts.
type busSnap struct {
	waiting, open int
	chans         []busRow
}

// busMsg is one look at the bus. A look that found the files as they were
// has same set.
type busMsg struct {
	same bool
	sig  string
	snap busSnap
	full bool
}

// busSignature is the sizes and times of the bus's files, and of your
// read cursor: what any change to the channels, tasks or handoffs moves.
func busSignature() string {
	dir := filepath.Join(paths.StateDir(), "bus")
	var b strings.Builder
	add := func(path string) {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			fmt.Fprintf(&b, "%s:%d:%d;", filepath.Base(path), fi.Size(), fi.ModTime().UnixNano())
		}
	}
	if es, err := os.ReadDir(dir); err == nil {
		for _, e := range es {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}
	add(filepath.Join(dir, "cursors", bus.You+".json"))
	return b.String()
}

// loadBus reads the bus into what the block shows. statuses are the
// agents' (nil before the first poll: the board is left out then).
func loadBus(st *state.State, statuses map[string]string, now time.Time, open string) busSnap {
	var s busSnap
	if statuses != nil {
		if cards, err := bus.Board(st, statuses, now); err == nil {
			for _, c := range cards {
				if c.Column == bus.ColDone {
					continue
				}
				s.open++
				if c.Column == bus.ColWaitingYou {
					s.waiting++
				}
			}
		}
	}
	chans, err := bus.Channels()
	if err != nil {
		return s
	}
	yours, _ := bus.YourChannels()
	for _, c := range chans {
		isDM := strings.HasPrefix(c.ID, "dm.")
		y := yours[c.ID]
		// The channel whose chat is open stays: its row closes it.
		if isDM && !yourDM(c.ID) || y.Unread == 0 && !c.Paused && c.ID != open {
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
	for _, id := range c.Members {
		if id == bus.You {
			continue
		}
		if _, a := st.FindAgent(id); a != nil {
			names = append(names, drive.Label(a))
		}
	}
	if len(names) == 0 {
		return strings.TrimPrefix(c.ID, "dm.")
	}
	return strings.Join(names, ", ")
}

// busRows are the rows of the block: the board, then the channels up to
// busRowsMax, then how many were left out.
func (s busSnap) busRows() []busRow {
	var out []busRow
	if s.open > 0 || s.waiting > 0 {
		out = append(out, busRow{board: true, title: "Board", waiting: s.waiting, open: s.open})
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
	full := now.Sub(m.busFull) >= busFullEvery
	sig := m.busSig
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
	return func() tea.Msg {
		now := time.Now()
		cur := busSignature() + "|" + open
		if cur == sig && !full {
			return busMsg{same: true}
		}
		return busMsg{sig: cur, snap: loadBus(st, statuses, now, open), full: full}
	}
}

// applyBus takes a look at the bus in; the rows change only when it did.
func (m *model) applyBus(msg busMsg) {
	m.busBusy = false
	if msg.same {
		return
	}
	m.busSig = msg.sig
	if msg.full {
		m.busFull = time.Now()
	}
	m.setBus(msg.snap)
}

func (m *model) setBus(s busSnap) {
	if reflect.DeepEqual(s, m.bus) {
		return
	}
	m.bus = s
	m.rebuildRows()
}

// busRowSegs lays out a row of the block.
func (m *model) busRowSegs(b *busRow) (left, right []seg) {
	switch {
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

// openBus opens a row of the block on stage: a channel's chat, or the
// board; again, it closes.
func (m *model) openBus(b *busRow) tea.Cmd {
	switch {
	case b.board:
		return m.toggleBusView("board", deck.SelfCommand("board -w"))
	case b.more > 0:
		m.setFlash("mad bus ls lists them all")
		return nil
	}
	return m.toggleBusView("chat:"+b.ch, deck.SelfCommand("bus chat "+paths.ShellQuote(b.ch)))
}

// toggleBusView shows cmd in the task pane, as the diff view is, or takes
// it down when it is up already.
func (m *model) toggleBusView(tag, cmd string) tea.Cmd {
	if d, ok := m.panes[tmux.IDTask]; ok && !d.Dead && m.shown[tmux.IDTask] && m.taskTag == tag {
		return m.closeTask(false)
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
