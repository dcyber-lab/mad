package ui

import (
	"math"
	"strconv"
	"testing"

	"github.com/charmbracelet/lipgloss"

	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// xterm256 is the sRGB of xterm-256 code n (16-255).
func xterm256(n int) [3]float64 {
	if n >= 232 {
		v := float64(8 + 10*(n-232))
		return [3]float64{v, v, v}
	}
	lv := []float64{0, 95, 135, 175, 215, 255}
	n -= 16
	return [3]float64{lv[n/36], lv[n/6%6], lv[n%6]}
}

func luminance(c [3]float64) float64 {
	f := func(v float64) float64 {
		s := v / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

func contrastOn(c [3]float64, bg float64) float64 {
	l1, l2 := luminance(c), luminance([3]float64{bg, bg, bg})
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// Every tone reads at 3:1 or better on a black and on a white terminal.
func TestToneContrast(t *testing.T) {
	for _, tone := range []status.Tone{status.ToneWorking, status.ToneNeeds, status.ToneFinished, status.ToneQuiet, status.ToneError} {
		col, ok := toneStyle(tone).GetForeground().(lipgloss.Color)
		if !ok {
			t.Fatalf("%s: foreground is not an xterm-256 color", tone)
		}
		code, err := strconv.Atoi(string(col))
		if err != nil {
			t.Fatal(err)
		}
		for name, bg := range map[string]float64{"black": 0, "white": 255} {
			if r := contrastOn(xterm256(code), bg); r < 3 {
				t.Errorf("%s (code %d) on %s: %.2f:1, want 3:1", tone, code, name, r)
			}
		}
	}
}

// A run waiting on you is told in the Needs-you tone, not the error red.
func TestRunWaitingLineTone(t *testing.T) {
	m, _ := setup(t, "/code/a")
	run := &state.Run{ID: "r1", Name: "feat"}
	m.runs[run.ID] = &madrun.File{Progress: madrun.Progress{Status: madrun.Waiting, Waiting: "approve the plan"}}
	segs := m.runDetailSegs(run)
	if got := segs[len(segs)-1].st.GetForeground(); got != toneStyle(status.ToneNeeds).GetForeground() {
		t.Errorf("waiting line drawn in %v, want the Needs-you %v", got, toneStyle(status.ToneNeeds).GetForeground())
	}
}

// Only a failure flashes in the error red.
func TestFlashTone(t *testing.T) {
	m, _ := setup(t, "/code/a")
	red := toneStyle(status.ToneError).GetForeground()
	m.setFlash("synced: a, b")
	if m.flashStyle().GetForeground() == red {
		t.Errorf("an info flash is drawn in the error red")
	}
	m.setFlashErr("boom")
	if m.flashStyle().GetForeground() != red {
		t.Errorf("an error flash is not drawn in the error red")
	}
	m.setFlash("noted for run X")
	if m.flashStyle().GetForeground() == red {
		t.Errorf("a flash after an error kept the error red")
	}
}
