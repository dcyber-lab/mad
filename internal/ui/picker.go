package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// The add-project picker takes over the sidebar: a search box over
// projects from Claude/Codex history (running ones first), or directory
// completion when the query looks like a path.

const pickerHeader = 3 // title, input, rule

type pickItem struct {
	path    string
	running bool
	meta    string // age, right-aligned
	sources []string
	isDir   bool // from path completion
}

type historyMsg []discover.Candidate

type picker struct {
	history []discover.Candidate
	loading bool
	items   []pickItem
	cursor  int
	offset  int
}

// scanHistory is replaceable in tests.
var scanHistory = discover.ScanHistory

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
		for _, d := range discover.CompleteDir(q) {
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
			if seen[path] || m.st.FindProject(path) != nil {
				return
			}
			seen[path] = true
			// Fuzzy on the name; the full path only as a plain substring
			// (subsequences of long paths match nearly anything).
			ok, score := discover.FuzzyMatch(q, filepath.Base(path))
			if !ok {
				i := strings.Index(strings.ToLower(paths.Short(path)), strings.ToLower(q))
				if i < 0 {
					return
				}
				score = 1000 + i // name matches rank above path matches
			}
			meta := ""
			if !last.IsZero() {
				meta = textutil.Age(last)
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
			m.input.SetValue(paths.Short(m.pk.items[m.pk.cursor].path) + "/")
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
			return m.addPicked(paths.Expand(q))
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
		m.setFlash("not a directory: " + paths.Short(dir))
		return nil
	}
	m.closePicker()
	root, _ := paths.ProjectRoot(dir)
	p, added := m.st.AddProject(root)
	if !added {
		m.setFlash("already added: " + p.Name)
	}
	m.save()
	m.rebuildRows()
	for i, r := range m.rows {
		if r.isProject() && r.proj == p {
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

// historyKinds names the agents whose history the picker scans ("claude/codex").
func historyKinds() string {
	var kinds []string
	for _, p := range discover.Providers() {
		kinds = append(kinds, p.Kind())
	}
	return strings.Join(kinds, "/")
}

func (m *model) pickerView() string {
	var b strings.Builder
	b.WriteString(layout(m.width, nil, []seg{{stHeader, " add project"}}, []seg{{stFaint, "esc "}}) + "\n")
	b.WriteString(" " + m.input.View() + "\n")
	b.WriteString(rule(m.width) + "\n")

	h := m.pickerListHeight()
	lines := 0
	switch {
	case len(m.pk.items) == 0 && m.pk.loading:
		b.WriteString(stDim.Render(" scanning "+historyKinds()+" history…") + "\n")
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
		mark := seg{stPlain, " "}
		if it.running {
			mark = seg{stDone, "●"}
		}
		var bg lipgloss.TerminalColor
		if i == m.pk.cursor {
			bg = cSelOn
		}
		left := []seg{{stPlain, " "}, mark, {stPlain, " "}, {stName, name}}
		var right []seg
		if it.meta != "" {
			right = []seg{{stFaint, it.meta + " "}}
		}
		b.WriteString(layout(m.width, bg, left, right) + "\n")
		lines++
	}
	for ; lines < h; lines++ {
		b.WriteString("\n")
	}
	// Full path of the selection, since names alone can be ambiguous.
	sel := ""
	if m.pk.cursor < len(m.pk.items) {
		it := m.pk.items[m.pk.cursor]
		sel = paths.Short(it.path)
		if len(it.sources) > 0 {
			sel += " · " + strings.Join(it.sources, ", ")
		}
	}
	b.WriteString(stDim.Render(" "+textutil.Truncate(sel, m.width-2)) + "\n")
	b.WriteString(hints("⏎", "add", "tab", "browse") + "  " + stDone.Render("●") + stDim.Render(" running"))
	return b.String()
}
