package cli

import (
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
