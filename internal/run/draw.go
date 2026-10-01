package run

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// What the panel is drawn with: blocks of lines that size themselves to
// what they hold, and leave the room left to one column.

// columns lays rows out in columns as wide as their widest cell, two
// spaces apart, in w cells: column flex gets what the others leave, its
// cells cut to fit (-1: none does). A column with nothing in it takes no
// room.
func columns(w int, rows [][]string, flex int) string {
	const gap = 2
	var widths []int
	for _, r := range rows {
		for c, cell := range r {
			if c >= len(widths) {
				widths = append(widths, 0)
			}
			widths[c] = max(widths[c], lipgloss.Width(cell))
		}
	}
	if flex >= 0 && flex < len(widths) {
		used := 0
		for c, cw := range widths {
			if c != flex && cw > 0 {
				used += cw + gap
			}
		}
		widths[flex] = min(widths[flex], max(w-used, 0))
	}
	last := len(widths) - 1
	for last > 0 && widths[last] == 0 {
		last--
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		var b strings.Builder
		for c := 0; c <= last; c++ {
			if widths[c] == 0 {
				continue
			}
			cell := ""
			if c < len(r) {
				cell = ansi.Truncate(r[c], widths[c], "…")
			}
			b.WriteString(cell)
			if c < last {
				b.WriteString(strings.Repeat(" ", widths[c]-lipgloss.Width(cell)+gap))
			}
		}
		lines[i] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// gauge is a bar width cells long, frac of it filled.
func gauge(frac float64, width int, style lipgloss.Style) string {
	n := min(max(int(frac*float64(width)+0.5), 0), width)
	if frac > 0 && n == 0 {
		n = 1
	}
	return style.Render(strings.Repeat("█", n)) + pRule.Render(strings.Repeat("░", width-n))
}

// span is a bar width cells long, filled from frac from to frac to: a
// step's place in the whole run.
func span(from, to float64, width int, style lipgloss.Style) string {
	a := min(max(int(from*float64(width)), 0), width-1)
	n := min(int((to-from)*float64(width)+0.5), width-a)
	fill := strings.Repeat("█", n)
	if n == 0 {
		fill, n = "▏", 1
	}
	return strings.Repeat(" ", a) + style.Render(fill) + strings.Repeat(" ", width-a-n)
}

// share is frac of w, at most most cells, or 0 when that is under least:
// the room a picture gets in a pane w wide.
func share(w int, frac float64, least, most int) int {
	n := min(int(float64(w)*frac), most)
	if n < least {
		return 0
	}
	return n
}

// canvas is a grid of one-cell runes, each with its style, for drawing
// lines that join.
type canvas struct {
	cells  [][]rune
	styles [][]lipgloss.Style
}

func newCanvas(w, h int) *canvas {
	c := &canvas{}
	for range h {
		row := make([]rune, w)
		for i := range row {
			row[i] = ' '
		}
		c.cells = append(c.cells, row)
		c.styles = append(c.styles, make([]lipgloss.Style, w))
	}
	return c
}

// set draws a line's rune, joining it with one already there.
func (c *canvas) set(r, col int, ch rune, st lipgloss.Style) {
	if r < 0 || r >= len(c.cells) || col < 0 || col >= len(c.cells[r]) {
		return
	}
	switch old := c.cells[r][col]; {
	case old == ' ':
	case ch == '│' && old == '─', ch == '─' && old == '│':
		ch = '┼'
	case ch == '│' && old == '╰':
		ch = '├'
	case ch == '│' && old == '╯':
		ch = '┤'
	default:
		return
	}
	c.cells[r][col], c.styles[r][col] = ch, st
}

// line draws a line from a to b in row r.
func (c *canvas) line(r, a, b int, st lipgloss.Style) {
	for i := a; i <= b; i++ {
		c.set(r, i, '─', st)
	}
}

// label writes text centred between a and b in row r, over a line but
// not over another crossing it; when it doesn't fit, it is left out.
func (c *canvas) label(r, a, b int, text string, st lipgloss.Style) {
	t := []rune(text)
	if b-a+1 < len(t)+2 {
		return
	}
	at := a + (b-a+1-len(t))/2
	for i, ch := range t {
		if col := at + i; c.cells[r][col] == '─' {
			c.cells[r][col], c.styles[r][col] = ch, st
		}
	}
}

func (c *canvas) String() string {
	lines := make([]string, len(c.cells))
	for r, row := range c.cells {
		var b strings.Builder
		for i := 0; i < len(row); {
			j := i
			for j < len(row) && c.styles[r][j].GetForeground() == c.styles[r][i].GetForeground() {
				j++
			}
			b.WriteString(c.styles[r][i].Render(string(row[i:j])))
			i = j
		}
		lines[r] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}
