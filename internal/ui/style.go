package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/textutil"
)

// Palette (xterm-256 so it looks the same under tmux-256color everywhere).
var (
	cAccent  = lipgloss.Color("75")  // brand, stage, keys
	cText    = lipgloss.Color("252") // names
	cDim     = lipgloss.Color("243") // secondary text
	cFaint   = lipgloss.Color("239") // rules, counts
	cRunning = lipgloss.Color("214")
	cWaiting = lipgloss.Color("203")
	cDone    = lipgloss.Color("78")
	cSelOn   = lipgloss.Color("24")  // cursor bar, sidebar focused
	cSelOff  = lipgloss.Color("236") // cursor bar, sidebar not focused
)

var (
	stPlain   = lipgloss.NewStyle()
	stHeader  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stProject = lipgloss.NewStyle().Bold(true).Foreground(cText)
	stName    = lipgloss.NewStyle().Foreground(cText)
	stDim     = lipgloss.NewStyle().Foreground(cDim)
	stFaint   = lipgloss.NewStyle().Foreground(cFaint)
	stKey     = lipgloss.NewStyle().Foreground(cAccent)
	stRunning = lipgloss.NewStyle().Foreground(cRunning)
	stWaiting = lipgloss.NewStyle().Foreground(cWaiting).Bold(true)
	stDone    = lipgloss.NewStyle().Foreground(cDone)
	stStage   = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	stFlash   = lipgloss.NewStyle().Foreground(cWaiting)
)

// seg is one styled run of text within a line.
type seg struct {
	st lipgloss.Style
	s  string
}

func segWidth(segs []seg) int {
	w := 0
	for _, s := range segs {
		w += lipgloss.Width(s.s)
	}
	return w
}

// minLeft is how much of the left side must stay visible before the right
// side is dropped to make room: at narrow widths an agent's name matters
// more than its status word (the icon still shows the status).
const minLeft = 14

// layout renders left and right-aligned segments into exactly width
// columns. With bg set, every segment and the padding get that background,
// so a selected row keeps its colors on a solid bar. The left side is cut
// with "…" when it doesn't fit; the right side is dropped first when even
// minLeft columns wouldn't be left for it.
func layout(width int, bg lipgloss.TerminalColor, left, right []seg) string {
	paint := func(st lipgloss.Style, s string) string {
		if bg != nil {
			st = st.Background(bg)
			if st.GetForeground() == cFaint {
				st = st.Foreground(cDim) // faint would vanish on the bar
			}
		}
		return st.Render(s)
	}
	rw := segWidth(right)
	if rw > 0 && width-rw-1 < minLeft && width-rw-1 < segWidth(left) {
		right, rw = nil, 0
	}
	room := width
	if rw > 0 {
		room = width - rw - 1
	}
	var b strings.Builder
	used := 0
	for _, s := range left {
		w := lipgloss.Width(s.s)
		if used+w > room {
			t := textutil.Truncate(s.s, room-used)
			b.WriteString(paint(s.st, t))
			used += lipgloss.Width(t)
			break
		}
		b.WriteString(paint(s.st, s.s))
		used += w
	}
	if pad := width - used - rw; pad > 0 {
		b.WriteString(paint(stPlain, strings.Repeat(" ", pad)))
	}
	for _, s := range right {
		b.WriteString(paint(s.st, s.s))
	}
	return b.String()
}

// hints renders "key label" pairs with the keys highlighted.
func hints(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(stKey.Render(pairs[i]) + " " + stDim.Render(pairs[i+1]))
	}
	return " " + b.String()
}

func rule(width int) string {
	return stFaint.Render(strings.Repeat("─", width))
}
