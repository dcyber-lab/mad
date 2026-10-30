package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
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
// owner, channel, age in its state and why it waits.
func board(out io.Writer) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	statuses, at := drive.Statuses(st, kinds()), time.Now()
	cards, err := bus.Board(st, statuses, at)
	if err != nil {
		return err
	}
	if len(cards) == 0 {
		fmt.Fprintln(out, "Nothing on the board.")
		return nil
	}
	if r := bus.Report(st, statuses, at); r != "" {
		fmt.Fprintln(out, r)
		fmt.Fprintln(out)
	}
	for _, col := range bus.Columns {
		first := true
		for _, c := range cards {
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
		parts = append(parts, c.Why)
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
