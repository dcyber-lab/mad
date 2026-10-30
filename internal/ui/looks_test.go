package ui

import (
	"testing"

	"github.com/dcyber-lab/mad/internal/status"
)

// Every shown state reads in the deck as the table says, in its tone.
func TestStatusGlyphFollowsTheTable(t *testing.T) {
	m, _ := setup(t, "/code/a")
	for _, l := range status.Looks {
		raw, attention := l.State, false
		if l.State == status.Finished {
			raw, attention = status.Idle, true
		}
		icon, label := m.statusGlyph(raw, attention)
		if label.s != l.Word {
			t.Errorf("%s: word %q, want %q", l.State, label.s, l.Word)
		}
		want := l.Glyph
		if l.State == status.Running {
			want = m.spin()
		}
		if icon.s != want {
			t.Errorf("%s: glyph %q, want %q", l.State, icon.s, want)
		}
		if l.Tone != status.ToneQuiet && label.st.GetForeground() != toneStyle(l.Tone).GetForeground() {
			t.Errorf("%s: wrong tone color", l.State)
		}
	}
	if toneStyle(status.ToneWorking).GetForeground() == toneStyle(status.ToneNeeds).GetForeground() {
		t.Error("working and needs share a color")
	}
	// Exited folds into Stopped, with no row of its own.
	if _, label := m.statusGlyph(status.Exited, false); label.s != "Stopped" {
		t.Errorf("exited reads %q", label.s)
	}
}
