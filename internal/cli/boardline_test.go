package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
)

// A card that waits on tasks names them.
func TestBoardLineNamesWhatItWaitsOn(t *testing.T) {
	c := bus.Card{Kind: bus.CardTask, Task: &bus.Task{ID: "t-2"}, Title: "handlers", OwnerLabel: "kim",
		Column: bus.ColBlocked, Why: bus.WhyWaits, Waits: []string{"t-1", "t-9"}, Age: time.Hour}
	if got := boardLine(c); !strings.HasSuffix(got, "waits t-1, t-9") {
		t.Errorf("boardLine = %q", got)
	}
	c.Waits = nil
	if got := boardLine(c); !strings.HasSuffix(got, "waits") {
		t.Errorf("boardLine = %q", got)
	}
}
