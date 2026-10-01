package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// leaving is an agent as the note on leaving the deck tells of it.
type leaving struct {
	name   string // project/title
	status string
	sleeps bool   // its kind can be put to sleep
	busy   string // the command that keeps it awake while idle
}

// detach leaves the deck (q, and the prefix's d through poke). The
// terminal shows in the client's place what keeps running in it, so a
// deck left behind is not forgotten.
func (m *model) detach() tea.Cmd {
	var agents []leaving
	pids := map[int]int{} // pane pid → index in agents
	for _, a := range m.st.OrderedAgents() {
		p, _ := m.st.FindAgent(a.ID)
		l := leaving{name: p.Name + "/" + textutil.Truncate(m.agentTitle(p, a), 30), sleeps: m.canSleep(a)}
		if tr := m.trackers[a.ID]; tr != nil {
			l.status = tr.Status
		}
		if pane, ok := m.panes[a.ID]; ok && l.status == status.Idle && l.sleeps {
			pids[pane.PID] = len(agents)
		}
		agents = append(agents, l)
	}
	after := m.cfg.SleepAfter
	return m.action("", func() error {
		var list []int
		for pid := range pids {
			list = append(list, pid)
		}
		busy, _ := deck.Busy(list...)
		for pid, c := range busy {
			agents[pids[pid]].busy = c
		}
		note := paths.LeaveNote()
		if paths.WriteFileAtomic(note, []byte(leaveNote(agents, after))) != nil {
			return tmux.Run("detach-client")
		}
		return tmux.Run("detach-client", "-E", "cat "+paths.ShellQuote(note))
	})
}

// leaveNote tells what keeps running in the deck once you leave it, and
// how to stop it.
func leaveNote(agents []leaving, sleepAfter time.Duration) string {
	var working, waiting, busy, awake []string
	idle, asleep := 0, 0
	for _, a := range agents {
		switch {
		case a.status == status.Running:
			working = append(working, a.name)
		case a.status == status.Waiting:
			waiting = append(waiting, a.name)
		case a.status == status.Asleep:
			asleep++
		case a.status != status.Idle: // exited, stopped: nothing runs
		case !a.sleeps:
			awake = append(awake, a.name)
		case a.busy != "":
			busy = append(busy, a.name+" ("+a.busy+")")
		default:
			idle++
		}
	}
	var b strings.Builder
	b.WriteString("mad: the deck keeps running in the background\n")
	line := func(label, what string) {
		fmt.Fprintf(&b, "  %-16s %s\n", label, what)
	}
	names := func(label string, ns []string) {
		if len(ns) == 0 {
			return
		}
		if len(ns) > 4 {
			ns = append(ns[:4:4], fmt.Sprintf("+%d", len(ns)-4))
		}
		line(label, strings.Join(ns, ", "))
	}
	names("working", working)
	names("waiting for you", waiting)
	names("kept awake by", busy)
	if idle > 0 {
		if sleepAfter > 0 {
			line("idle", fmt.Sprintf("%d, asleep after %s idle", idle, shortDuration(sleepAfter)))
		} else {
			line("idle", fmt.Sprintf("%d, never asleep (sleep.after is 0)", idle))
		}
	}
	names("can't sleep", awake)
	if asleep > 0 {
		line("asleep", fmt.Sprint(asleep))
	}
	b.WriteString(`"mad" takes you back, "mad kill-server" stops it all` + "\n")
	return b.String()
}

// shortDuration is what time.Duration prints without the zero units at
// its end: 1h, 1h30m, 45m.
func shortDuration(d time.Duration) string {
	s := d.String() // 1h0m0s
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}
