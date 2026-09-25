package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestLayout(t *testing.T) {
	agentRow := []seg{{stPlain, " ▌ 1 ⠋ "}, {stName, "claude#2"}} // 15 wide
	cases := []struct {
		name        string
		width       int
		left, right []seg
		bg          lipgloss.TerminalColor
		want        string
	}{
		{"fits", 20, []seg{{stPlain, " ab"}}, []seg{{stDim, "x "}}, nil,
			" ab" + strings.Repeat(" ", 15) + "x "},
		{"left cut, right kept", 20, []seg{{stPlain, " "}, {stName, "a-very-long-project-name"}}, []seg{{stDim, "3 "}}, nil,
			" a-very-long-pro… 3 "},
		{"right dropped so the name fits", 20, agentRow, []seg{{stDim, "running "}}, nil,
			" ▌ 1 ⠋ claude#2     "},
		{"right kept when there is room", 32, agentRow, []seg{{stDim, "running "}}, nil,
			" ▌ 1 ⠋ claude#2         running "},
		{"selected row pads to the edge", 12, []seg{{stPlain, " x"}}, nil, cSelOn,
			" x          "},
		{"wide runes count double", 12, []seg{{stPlain, " "}, {stName, "中文项目名称"}}, []seg{{stDim, "1 "}}, nil,
			" 中文项目名…"},
	}
	for _, c := range cases {
		got := layout(c.width, c.bg, c.left, c.right)
		if w := lipgloss.Width(got); w != c.width {
			t.Errorf("%s: width %d, want %d: %q", c.name, w, c.width, got)
		}
		if s := ansi.Strip(got); s != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, s, c.want)
		}
	}
}

func TestHints(t *testing.T) {
	if got := ansi.Strip(hints("⏎", "open", "n", "new")); got != " ⏎ open  n new" {
		t.Errorf("hints = %q", got)
	}
}
