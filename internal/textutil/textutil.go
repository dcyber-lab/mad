// Package textutil has width-aware string helpers for terminal display.
package textutil

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Truncate cuts s to n display columns (wide CJK runes count as two),
// ending with "…" when shortened.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

// PadRight pads s with spaces to n display columns; ANSI styling is not
// counted.
func PadRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// Age is a compact "how long ago": 5m, 3h, 12d.
func Age(t time.Time) string {
	return ageAt(t, time.Now())
}

func ageAt(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Count is a compact token count: 980, 4.2k, 340k, 1.2M, 12M, 1.1B.
func Count(n int64) string {
	f := float64(n)
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", f/1e3)
	case n < 1_000_000:
		return fmt.Sprintf("%.0fk", f/1e3)
	case n < 10_000_000:
		return fmt.Sprintf("%.1fM", f/1e6)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.0fM", f/1e6)
	default:
		return fmt.Sprintf("%.1fB", f/1e9)
	}
}
