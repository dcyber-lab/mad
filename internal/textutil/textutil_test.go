package textutil

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"批量解决仓库", 7, "批量解…"}, // CJK is two columns wide
		{"批量", 4, "批量"},
		{"abc", 0, ""},
		{"abc", -1, ""},
	}
	for _, c := range cases {
		got := Truncate(c.in, c.n)
		if got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
		if c.n > 0 && lipgloss.Width(got) > c.n {
			t.Errorf("Truncate(%q, %d) is %d columns wide", c.in, c.n, lipgloss.Width(got))
		}
	}
}

func TestPadRight(t *testing.T) {
	if got := PadRight("ab", 4); got != "ab  " {
		t.Errorf("got %q", got)
	}
	if got := PadRight("abcdef", 4); got != "abcdef" {
		t.Errorf("longer strings are left alone, got %q", got)
	}
	if got := PadRight("中", 4); lipgloss.Width(got) != 4 {
		t.Errorf("CJK padding width = %d", lipgloss.Width(got))
	}
	styled := lipgloss.NewStyle().Bold(true).Render("ab")
	if got := PadRight(styled, 4); lipgloss.Width(got) != 4 {
		t.Errorf("styled padding width = %d", lipgloss.Width(got))
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		0:                   "0m",
		59 * time.Second:    "0m",
		5 * time.Minute:     "5m",
		3 * time.Hour:       "3h",
		23 * time.Hour:      "23h",
		49 * time.Hour:      "2d",
		-5 * time.Minute:    "0m", // clock skew
		30 * 24 * time.Hour: "30d",
	}
	for d, want := range cases {
		if got := ageAt(now.Add(-d), now); got != want {
			t.Errorf("age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	cases := map[int64]string{
		0: "0", 980: "980", 1200: "1.2k", 9960: "10.0k", 12345: "12k", 340_000: "340k",
		1_234_567: "1.2M", 12_345_678: "12M", 999_999_999: "1000M", 1_100_000_000: "1.1B",
	}
	for n, want := range cases {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}
