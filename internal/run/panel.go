package run

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/textutil"
)

var (
	pBold  = lipgloss.NewStyle().Bold(true)
	pDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	pRun   = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	pGood  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	pWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	pBad   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	pRule  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	pLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(9)
)

// paint draws the run's panel over the runner's pane: what the run is,
// each step taken and to come, what it waits for, and its log.
func (x *runner) paint() {
	if x.readUsage(false) {
		x.save() // what it cost so far, for the sidebar too
	}
	x.mu.Lock()
	spec, pr := x.f.Spec, x.f.Progress
	pr.Entries = append([]Entry(nil), pr.Entries...)
	ctx := map[string]int64{}
	for k, v := range pr.Context {
		ctx[k] = v
	}
	tools := map[string]string{}
	for _, a := range x.roleIDs() {
		tools[a.role] = x.usage[a.id].Tool
	}
	diff := x.diff
	x.mu.Unlock()

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 20 {
		w, h = 100, 40
	}
	// The last row stays free: when the runner is done tmux says so
	// there, and a full screen would scroll the head away.
	h--
	var lines []string
	add := func(s string) { lines = append(lines, s) }
	rule := pRule.Render(" " + strings.Repeat("─", max(w-2, 1)))

	// Head: the run, then how it stands.
	end := pr.Ended
	if end.IsZero() {
		end = now()
	}
	took := ""
	if !pr.Started.IsZero() {
		took = dur(end.Sub(pr.Started))
	}
	spent := textutil.USD(pr.Cost)
	if pr.Tokens > 0 {
		spent += " + " + kTokens(pr.Tokens) + " tok"
	}
	var st string
	switch pr.Status {
	case Starting:
		st = pDim.Render("starting")
	case Running:
		cur := ""
		if pr.Step >= 0 && pr.Step < len(x.flow.Steps) {
			s := x.flow.Steps[pr.Step]
			role, _ := x.flow.Role(s.Role)
			cur = s.Label + " (" + role.Label + ")"
		}
		st = pRun.Render(fmt.Sprintf("● %s · round %d · %s · %s", cur, pr.Round, took, spent))
	case Waiting:
		st = pWarn.Bold(true).Render("◆ waiting for you") + pDim.Render(fmt.Sprintf(" · %s · %s", took, spent))
	case Done:
		st = pGood.Render(fmt.Sprintf("✓ approved%s · %s · %s", RoundsOfChanges(pr.Round-1), took, spent))
	case Cancelled:
		st = pBad.Render("✗ cancelled") + pDim.Render(" · "+spent)
	case Failed:
		st = pBad.Render("✗ failed") + pDim.Render(" · "+spent)
	}
	head := pBold.Render(" run " + x.run.Name)
	add(head + strings.Repeat(" ", max(w-lipgloss.Width(head)-lipgloss.Width(st)-1, 1)) + st)
	add(rule)
	add(" " + pLabel.Render("task") + firstLine(spec.Task))
	add(" " + pLabel.Render("branch") + x.run.Branch + pDim.Render("  "+paths.Short(x.run.Dir)))
	add(" " + pLabel.Render("flow") + x.flow.Name + pDim.Render(fmt.Sprintf("  %s · budget %s", x.flow.Description, textutil.USD(spec.Budget))))
	add("")

	// Steps: each one taken, then those to come.
	cols := func(glyph, step, who, result, t, cost string) string {
		return " " + glyph + " " + textutil.PadRight(step, 16) + textutil.PadRight(who, 12) +
			textutil.PadRight(ansi.Truncate(result, max(w-50, 10), "…"), max(w-50, 10)) + " " + textutil.PadRight(t, 7) + cost
	}
	add(pDim.Render(cols(" ", "step", "role", "result", "time", "cost")))
	for _, e := range pr.Entries {
		s := x.flow.Steps[max(x.flow.StepIndex(e.Step), 0)]
		role, _ := x.flow.Role(e.Role)
		glyph, result := pGood.Render("✓"), e.Result
		switch e.Status {
		case "running":
			glyph, result = pRun.Render("●"), pDim.Render(tools[e.Role])
		case "changes":
			glyph = pWarn.Render("↺")
		}
		stop := e.End
		if stop.IsZero() {
			stop = now()
		}
		cost := ""
		switch {
		case e.Cost > 0:
			cost = textutil.USD(e.Cost)
		case e.Tokens > 0:
			cost = kTokens(e.Tokens) + " tok"
		}
		add(cols(glyph, s.Label, roleAgent(x.kinds, role), result, dur(stop.Sub(e.Start)), cost))
	}
	if Active(pr.Status) {
		for i := pr.Step; i >= 0 && i < len(x.flow.Steps); i++ {
			s := x.flow.Steps[i]
			if n := len(pr.Entries); i == pr.Step && n > 0 && pr.Entries[n-1].Step == s.Name && pr.Entries[n-1].Status == "running" {
				continue
			}
			role, _ := x.flow.Role(s.Role)
			add(pDim.Render(cols("○", s.Label, roleAgent(x.kinds, role), "", "", "")))
		}
	}

	// What it waits for, or how it ended.
	switch pr.Status {
	case Waiting:
		add("")
		add(" " + pWarn.Bold(true).Render("◆ waiting for you: ") + pWarn.Render(pr.Waiting))
		add(pDim.Render("   on this run in the sidebar: c continue · x cancel · enter on a role to step in"))
	case Failed:
		add("")
		add(" " + pBad.Render("✗ "+pr.Waiting))
	case Done:
		if len(diff) > 0 {
			add("")
			add(" " + pLabel.Render("changes"))
			for i, l := range diff {
				if i >= 10 {
					add(pDim.Render(fmt.Sprintf("   … %d more", len(diff)-i)))
					break
				}
				add("   " + l)
			}
		}
		add("")
		add(pDim.Render(" on this run in the sidebar: f pull request or merge · v diff · x remove"))
	}
	if n := len(pr.Notes); n > 0 {
		add("")
		add(" " + pLabel.Render("from you") + pDim.Render(fmt.Sprintf("%d · %s/notes.md", n, RelDir(x.run))))
		for _, note := range pr.Notes[max(n-3, 0):] {
			who := "every role"
			if note.Role != "" {
				who = "the " + x.flow.RoleLabel(note.Role)
			}
			add("   " + pDim.Render(who+": ") + firstLine(note.Text))
		}
	}
	if len(ctx) > 0 {
		var parts []string
		for _, r := range x.flow.Roles {
			if c, ok := ctx[r.Name]; ok {
				parts = append(parts, r.Label+" "+kTokens(c))
			}
		}
		add("")
		add(" " + pLabel.Render("context") + strings.Join(parts, pDim.Render(" · ")) +
			pDim.Render(fmt.Sprintf("   (a claude role past %s is compacted)", kTokens(spec.Compact))))
	}

	// The log takes what is left of the pane.
	add("")
	add(pDim.Render(" log  " + paths.Short(x.dir)))
	room := h - len(lines) - 1
	if room < 3 {
		room = 3
	}
	for _, l := range logTail(x.dir, room) {
		add(pDim.Render(" " + l))
	}

	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[H")
	for i, l := range lines {
		if i >= h {
			break
		}
		b.WriteString(ansi.Truncate(l, w, "…") + "\x1b[K")
		if i < len(lines)-1 && i < h-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[J")
	_, _ = x.out.Write([]byte(b.String()))
}

type roleID struct{ role, id string }

// roleIDs are the agents of the run's roles as last read; x.mu is held.
func (x *runner) roleIDs() []roleID {
	var out []roleID
	for id := range x.usage {
		out = append(out, roleID{x.roleOf[id], id})
	}
	return out
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func dur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// RoundsOfChanges says how often a review sent the work back, if at all.
func RoundsOfChanges(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return " after 1 round of changes"
	}
	return fmt.Sprintf(" after %d rounds of changes", n)
}
