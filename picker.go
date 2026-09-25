package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The add-project picker takes over the sidebar: a search box over
// projects from Claude/Codex history (running ones first), or directory
// completion when the query looks like a path.

const pickerHeader = 4 // title, input, rule, blank-free list starts here

type pickItem struct {
	path    string
	running bool
	meta    string // age, right-aligned
	sources []string
	isDir   bool // from path completion
}

type historyMsg []Candidate

type picker struct {
	history []Candidate
	loading bool
	items   []pickItem
	cursor  int
	offset  int
}

func (m *model) openPicker() tea.Cmd {
	m.mode = modeAddProject
	m.input.Prompt = "› "
	m.input.Placeholder = "search, or type a path"
	m.input.SetValue("")
	m.pk.cursor, m.pk.offset = 0, 0
	m.pk.loading = m.pk.history == nil
	m.refreshPicker()
	return tea.Batch(m.input.Focus(), func() tea.Msg { return historyMsg(scanHistory()) })
}

func isPathQuery(q string) bool {
	return strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, ".")
}

func (m *model) refreshPicker() {
	q := strings.TrimSpace(m.input.Value())
	var items []pickItem

	if isPathQuery(q) {
		for _, d := range completeDir(q) {
			items = append(items, pickItem{path: d, isDir: true})
			if len(items) >= 300 {
				break
			}
		}
	} else {
		running := map[string]int{}
		for _, e := range m.externals {
			running[e.Root]++
		}
		type scored struct {
			pickItem
			score int
			last  time.Time
		}
		var list []scored
		seen := map[string]bool{}
		consider := func(path string, last time.Time, sources []string) {
			if seen[path] || m.st.findProject(path) != nil {
				return
			}
			seen[path] = true
			// Fuzzy on the name; the full path only as a plain substring
			// (subsequences of long paths match nearly anything).
			ok, score := fuzzyMatch(q, filepath.Base(path))
			if !ok {
				i := strings.Index(strings.ToLower(shortPath(path)), strings.ToLower(q))
				if i < 0 {
					return
				}
				score = 1000 + i // name matches rank above path matches
			}
			meta := ""
			if !last.IsZero() {
				meta = age(last)
			}
			list = append(list, scored{pickItem{path: path, running: running[path] > 0, meta: meta, sources: sources}, score, last})
		}
		for _, e := range m.externals {
			src := e.Kind
			if e.Desktop {
				src += " desktop"
			}
			consider(e.Root, time.Now(), []string{src + " (open)"})
		}
		for _, c := range m.pk.history {
			consider(c.Path, c.LastUsed, c.Sources)
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.running != b.running {
				return a.running
			}
			if q != "" && a.score != b.score {
				return a.score < b.score
			}
			return a.last.After(b.last)
		})
		for _, s := range list {
			items = append(items, s.pickItem)
		}
	}
	m.pk.items = items
	if m.pk.cursor >= len(items) {
		m.pk.cursor = len(items) - 1
	}
	if m.pk.cursor < 0 {
		m.pk.cursor = 0
	}
	m.clampPicker()
}

func (m *model) pickerListHeight() int {
	h := m.height - pickerHeader - 2
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) clampPicker() {
	h := m.pickerListHeight()
	if m.pk.cursor < m.pk.offset {
		m.pk.offset = m.pk.cursor
	}
	if m.pk.cursor >= m.pk.offset+h {
		m.pk.offset = m.pk.cursor - h + 1
	}
	if m.pk.offset < 0 {
		m.pk.offset = 0
	}
}

func (m *model) movePicker(d int) {
	m.pk.cursor += d
	if m.pk.cursor >= len(m.pk.items) {
		m.pk.cursor = len(m.pk.items) - 1
	}
	if m.pk.cursor < 0 {
		m.pk.cursor = 0
	}
	m.clampPicker()
}

func (m *model) closePicker() {
	m.mode = modeNormal
	m.input.Blur()
}

func (m *model) keyPicker(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.closePicker()
		return nil
	case "up", "ctrl+p", "ctrl+k":
		m.movePicker(-1)
		return nil
	case "down", "ctrl+n", "ctrl+j":
		m.movePicker(1)
		return nil
	case "pgup":
		m.movePicker(-m.pickerListHeight())
		return nil
	case "pgdown":
		m.movePicker(m.pickerListHeight())
		return nil
	case "tab":
		// Descend into the selected directory (or start browsing from it).
		if m.pk.cursor < len(m.pk.items) {
			m.input.SetValue(shortPath(m.pk.items[m.pk.cursor].path) + "/")
			m.input.CursorEnd()
			m.pk.cursor = 0
			m.refreshPicker()
		}
		return nil
	case "enter":
		if m.pk.cursor < len(m.pk.items) {
			return m.addPicked(m.pk.items[m.pk.cursor].path)
		}
		if q := strings.TrimSpace(m.input.Value()); isPathQuery(q) {
			return m.addPicked(expandPath(q))
		}
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.pk.cursor = 0
	m.refreshPicker()
	return cmd
}

func (m *model) addPicked(dir string) tea.Cmd {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		m.setFlash("not a directory: " + shortPath(dir))
		return nil
	}
	m.closePicker()
	root, _ := projectRoot(dir)
	p, added := m.st.addProject(root)
	if !added {
		m.setFlash("already added: " + p.Name)
	}
	m.save()
	m.rebuildRows()
	for i, r := range m.rows {
		if r.agent == nil && r.ext == nil && r.proj == p {
			m.cursor = i
		}
	}
	m.clampScroll()
	return nil
}

func (m *model) mousePicker(ev tea.MouseMsg) tea.Cmd {
	switch {
	case ev.Button == tea.MouseButtonWheelUp:
		m.movePicker(-1)
	case ev.Button == tea.MouseButtonWheelDown:
		m.movePicker(1)
	case ev.Button == tea.MouseButtonLeft && ev.Action == tea.MouseActionPress:
		i := ev.Y - pickerHeader + m.pk.offset
		if ev.Y >= pickerHeader && i < len(m.pk.items) {
			m.pk.cursor = i
			return m.addPicked(m.pk.items[i].path)
		}
	}
	return nil
}

func (m *model) pickerView() string {
	var b strings.Builder
	b.WriteString(stHeader.Render(" add project") + stDim.Render("  esc cancel") + "\n")
	b.WriteString(" " + m.input.View() + "\n")
	b.WriteString(stDim.Render(strings.Repeat("─", m.width)) + "\n")

	h := m.pickerListHeight()
	lines := 0
	switch {
	case len(m.pk.items) == 0 && m.pk.loading:
		b.WriteString(stDim.Render(" scanning claude/codex history…") + "\n")
		lines++
	case len(m.pk.items) == 0:
		b.WriteString(stDim.Render(" no match — type a path: / or ~") + "\n")
		lines++
	}
	for i := m.pk.offset; i < len(m.pk.items) && lines < h; i++ {
		it := m.pk.items[i]
		name := filepath.Base(it.path)
		if it.isDir {
			name += "/"
		}
		mark := " "
		if it.running {
			mark = stDone.Render("●")
		}
		meta := it.meta
		nameW := m.width - 5 - len(meta)
		left := fmt.Sprintf(" %s %s", mark, truncate(name, nameW))
		line := padRight(left, m.width-len(meta)-1) + stDim.Render(meta)
		if i == m.pk.cursor {
			line = stCursor.Render(padRight(ansi.Strip(line), m.width))
		}
		b.WriteString(line + "\n")
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	// Full path of the selection, since names alone can be ambiguous.
	sel := ""
	if m.pk.cursor < len(m.pk.items) {
		it := m.pk.items[m.pk.cursor]
		sel = shortPath(it.path)
		if len(it.sources) > 0 {
			sel += " · " + strings.Join(it.sources, ", ")
		}
	}
	b.WriteString(stDim.Render(" "+truncate(sel, m.width-2)) + "\n")
	b.WriteString(stDim.Render(" ⏎ add  tab browse  ● running"))
	return b.String()
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
