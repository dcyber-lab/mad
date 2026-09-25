package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/transcript"
)

func (m *model) rebuildRows() {
	prev, hadPrev := m.current()
	m.rows = m.rows[:0]
	num := 0
	for _, p := range m.st.Projects {
		m.rows = append(m.rows, row{proj: p})
		for _, a := range p.Agents {
			num++
			if !p.Collapsed {
				m.rows = append(m.rows, row{proj: p, agent: a, num: num})
			}
		}
		if p.Collapsed {
			continue
		}
		desk := row{proj: p}
		for i := range m.externals {
			e := &m.externals[i]
			if e.Root != p.Path {
				continue
			}
			if e.Desktop {
				desk.desktop++
				if desk.deskKind == "" {
					desk.deskKind = e.Kind
				}
			} else {
				m.rows = append(m.rows, row{proj: p, ext: e})
			}
		}
		if desk.desktop > 0 {
			m.rows = append(m.rows, desk)
		}
	}
	// Keep the cursor on the same item when possible.
	if hadPrev {
		for i, r := range m.rows {
			same := r.proj.Path == prev.proj.Path
			switch {
			case prev.agent != nil:
				same = r.agent != nil && r.agent.ID == prev.agent.ID
			case prev.ext != nil:
				same = same && r.ext != nil && r.ext.PID == prev.ext.PID
			case prev.desktop > 0:
				same = same && r.desktop > 0
			default:
				same = same && r.isProject()
			}
			if same {
				m.cursor = i
				break
			}
		}
	}
	m.lineOf = m.lineOf[:0]
	line := 0
	for i, r := range m.rows {
		if i > 0 && r.isProject() {
			line++
		}
		m.lineOf = append(m.lineOf, line)
		line += m.rowHeight(r)
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

// rowHeight is how many screen lines a row takes: an agent whose
// transcript is followed gets a second line for what it is on.
func (m *model) rowHeight(r row) int {
	if r.agent != nil && !m.st.Compact && transcript.Followed(r.agent.Kind) {
		return 2
	}
	return 1
}

// rowAt is the row drawn on list line y (0 = first list line), if any.
func (m *model) rowAt(y int) (int, bool) {
	line := y + m.offset
	for i, l := range m.lineOf {
		if line >= l && line < l+m.rowHeight(m.rows[i]) {
			return i, true
		}
	}
	return 0, false
}

func (m *model) selectAgent(id string) {
	p, _ := m.st.FindAgent(id)
	if p != nil && p.Collapsed {
		p.Collapsed = false
		m.save()
		m.rebuildRows()
	}
	for i, r := range m.rows {
		if r.agent != nil && r.agent.ID == id {
			m.cursor = i
		}
	}
	m.clampScroll()
}

func (m *model) listHeight() int {
	footer := footerLines
	switch m.mode {
	case modePickKind:
		footer = len(m.kinds) + 2 // rule, title, one line per kind
	case modeWorktree, modeRename:
		footer = 3 // rule, title, input
	case modePickFinish:
		footer = len(m.fin) + 2
	}
	h := m.height - m.headerH() - footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) clampScroll() {
	if m.cursor >= len(m.lineOf) {
		m.offset = 0
		return
	}
	h, line := m.listHeight(), m.lineOf[m.cursor]
	if last := line + m.rowHeight(m.rows[m.cursor]) - 1; last >= m.offset+h {
		m.offset = last - h + 1
	}
	if line < m.offset {
		m.offset = line
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) move(d int) {
	m.cursor += d
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

func (m *model) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m *model) save() {
	if m.stErr != nil {
		m.configError(fmt.Errorf("%w; changes aren't saved until it's fixed", m.stErr))
		return
	}
	if err := m.st.Save(); err != nil {
		m.setFlash(err.Error())
	}
	m.stMod = state.ModTime()
}

func (m *model) setFlash(s string) {
	m.flash, m.flashUntil = s, time.Now().Add(5*time.Second)
}

// configError shows a broken config file longer than other flashes: the
// file and line lead, so they survive truncation to the sidebar's width.
func (m *model) configError(err error) {
	msg := strings.ReplaceAll(err.Error(), "\n", "; ") // errors.Join puts one per line
	m.flash, m.flashUntil = msg, time.Now().Add(20*time.Second)
}
