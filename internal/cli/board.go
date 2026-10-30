package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/attention"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// columnNames are how the board's columns are headed.
var columnNames = map[string]string{
	bus.ColWaitingYou: "Waiting for you",
	bus.ColDoing:      "Doing",
	bus.ColBlocked:    "Blocked",
	bus.ColReview:     "Review",
	bus.ColTodo:       "Todo",
	bus.ColDone:       "Done today",
}

// board prints the task board grouped by column, a line a card: title,
// owner, channel, age in its state and why it waits. "Waiting for you"
// is the inbox, as the window and the deck have it: what waits on the
// person, whether or not a task carries it.
func board(out io.Writer) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	trackers, at := drive.Trackers(st, kinds()), time.Now()
	facts := attention.Facts{Spend: bus.SpendNow(at), Now: at}
	for _, p := range st.Projects {
		for _, a := range p.Agents {
			facts.Agents = append(facts.Agents, attention.AgentOf(p, a, trackers[a.ID], "", status.ReadReport(a.ID), transcript.Info{}))
		}
	}
	statuses := facts.Statuses()
	cards, err := bus.Board(st, statuses, at)
	if err != nil {
		return err
	}
	facts.Cards = cards
	facts.Skills, _ = skill.PendingList()
	v := attention.NewDesk().Look(facts)
	if len(v.Board) == 0 && v.Inbox.Needs == 0 {
		fmt.Fprintln(out, "Nothing on the board.")
		return nil
	}
	// The report's channel lines, under totals that count what waits for
	// you as the column below lists it.
	_, lines, _ := strings.Cut(bus.Report(st, statuses, at), "\n")
	fmt.Fprintln(out, boardTotals(v))
	if lines != "" {
		fmt.Fprintln(out, lines)
	}
	fmt.Fprintln(out)
	if needs := v.Inbox.NeedsYou(); len(needs) > 0 {
		fmt.Fprintln(out, columnNames[bus.ColWaitingYou])
		for _, it := range needs {
			fmt.Fprintln(out, "  "+itemLine(it, at))
		}
	}
	for _, col := range bus.Columns[1:] {
		first := true
		for _, c := range v.Board {
			if c.Column != col {
				continue
			}
			if first {
				fmt.Fprintf(out, "%s\n", columnNames[col])
				first = false
			}
			fmt.Fprintln(out, "  "+boardLine(c))
		}
	}
	return nil
}

// boardTotals is the board's first line: "4 open · 2 waiting for you ·
// 1 review", what waits for you being the inbox.
func boardTotals(v attention.View) string {
	n := map[string]int{bus.ColWaitingYou: v.Inbox.Needs}
	for _, c := range v.Board {
		n[c.Column]++
	}
	open := 0
	var parts []string
	for _, col := range bus.Columns[:len(bus.Columns)-1] { // done is not open
		if n[col] > 0 {
			open += n[col]
			parts = append(parts, fmt.Sprintf("%d %s", n[col], strings.ToLower(columnNames[col])))
		}
	}
	return strings.Join(append([]string{fmt.Sprintf("%d open", open)}, parts...), " · ")
}

// itemLine is one thing waiting for the person: who or what, its
// context and text, its age, and the tasks it holds up.
func itemLine(it attention.Item, at time.Time) string {
	var parts []string
	for _, s := range []string{it.Title, it.Context, it.Text} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	parts = append(parts, mcpAge(at.Sub(it.Since)))
	for _, t := range it.Tasks {
		parts = append(parts, t.ID)
	}
	return strings.Join(parts, " · ")
}

// boardLine is one card: title, owner, #channel, age, why.
func boardLine(c bus.Card) string {
	parts := []string{c.Title}
	owner := c.OwnerLabel
	if owner == "" {
		owner = "unowned"
	}
	parts = append(parts, owner)
	if c.Channel != "" {
		if strings.HasPrefix(c.Channel, "dm.") {
			parts = append(parts, "DM")
		} else {
			parts = append(parts, "#"+c.Channel)
		}
	}
	parts = append(parts, mcpAge(c.Age))
	if c.Why != "" {
		why := c.Why
		if len(c.Waits) > 0 {
			why += " " + strings.Join(c.Waits, ", ")
		}
		parts = append(parts, why)
	}
	if c.Task != nil {
		parts = append([]string{c.Task.ID}, parts...)
	}
	return strings.Join(parts, " · ")
}

// boardCmd is `mad board [-w]`.
func boardCmd(args []string, stdio IO) error {
	switch {
	case len(args) == 0:
		return board(stdio.Out)
	case len(args) == 1 && args[0] == "-w":
		_, err := tea.NewProgram(boardWatch{}, tea.WithAltScreen()).Run()
		return err
	}
	fmt.Fprintln(stdio.Err, "usage: mad board [-w]")
	return errUsage
}

// boardEvery is how often `mad board -w` draws the board again.
const boardEvery = 3 * time.Second

type (
	boardTick time.Time
	boardText string
)

// boardWatch is the board kept on screen; q or esc closes it.
type boardWatch struct {
	text          string
	width, height int
	top           int
}

func (b boardWatch) Init() tea.Cmd { return tea.Batch(boardRead, boardWait()) }

func boardWait() tea.Cmd {
	return tea.Tick(boardEvery, func(t time.Time) tea.Msg { return boardTick(t) })
}

func boardRead() tea.Msg {
	var buf strings.Builder
	if err := board(&buf); err != nil {
		return boardText(err.Error())
	}
	return boardText(buf.String())
}

func (b boardWatch) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
	case boardTick:
		return b, tea.Batch(boardRead, boardWait())
	case boardText:
		b.text = string(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return b, tea.Quit
		case "j", "down":
			b.top = min(b.top+1, strings.Count(strings.TrimRight(b.text, "\n"), "\n"))
		case "k", "up":
			b.top = max(b.top-1, 0)
		}
	}
	return b, nil
}

func (b boardWatch) View() string {
	lines := strings.Split(strings.TrimRight(b.text, "\n"), "\n")
	b.top = min(b.top, max(len(lines)-1, 0))
	lines = lines[b.top:]
	if b.height > 1 && len(lines) > b.height-1 {
		lines = lines[:b.height-1]
	}
	for i, l := range lines {
		if b.width > 0 {
			lines[i] = ansi.Truncate(l, b.width, "…")
		}
	}
	return strings.Join(lines, "\n") + "\n" + "q closes · updates every 3s"
}
