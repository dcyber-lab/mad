package deck

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// MaxViews is how many panes the stage shows at once.
const MaxViews = 4

// ErrFullStage: the stage shows MaxViews panes already.
var ErrFullStage = fmt.Errorf("%d views max: s closes one", MaxViews)

// AddView shows the pane tagged madID beside those on stage, rather than
// in place of the one in use, and with focus moves the focus to it. On a
// stage with nothing on it, it takes the stage.
func AddView(madID string, focus bool) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	target, ok := tmux.FindPane(panes, madID)
	if !ok {
		return fmt.Errorf("pane for %s not found", madID)
	}
	views := tmux.Views(panes)
	switch {
	case tmux.OnStage(target):
		if focus {
			return tmux.Run("select-pane", "-t", target.ID)
		}
		return nil
	case len(views) == 0 || len(views) == 1 && views[0].MadID == tmux.IDPlaceholder:
		return ShowPane(madID, focus)
	case len(views) >= MaxViews:
		return ErrFullStage
	}
	// After the last view: Arrange keeps the panes in their order.
	args := []string{"join-pane", "-h", "-s", target.ID, "-t", views[len(views)-1].ID}
	if !focus {
		args = append(args, "-d")
	}
	if err := tmux.Run(args...); err != nil {
		return err
	}
	return Arrange()
}

// CloseView takes the pane tagged madID off the stage, back to the pool
// (a task pane, a one-off, ends). The last view gives its place to the
// placeholder.
func CloseView(madID string) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	pane, ok := tmux.FindPane(panes, madID)
	if madID == "" || !ok || !tmux.OnStage(pane) {
		return nil
	}
	if len(tmux.Views(panes)) == 1 {
		return ShowPane(tmux.IDPlaceholder, false)
	}
	if madID == tmux.IDTask {
		err = tmux.Run("kill-pane", "-t", pane.ID)
	} else {
		err = tmux.Run("break-pane", "-d", "-s", pane.ID, "-t", tmux.PoolSession+":", "-n", "view")
	}
	if err != nil {
		return err
	}
	return Arrange()
}

// OpenAgentView is OpenAgent with the agent shown beside what is on
// stage (AddView).
func OpenAgentView(st *state.State, id string, kinds []agent.Kind, focus bool) error {
	// No agent started for a view there is no room for.
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	if p, ok := tmux.FindPane(panes, id); (!ok || !tmux.OnStage(p)) && len(tmux.Views(panes)) >= MaxViews {
		return ErrFullStage
	}
	if err := ensureAgent(st, id, kinds); err != nil {
		return err
	}
	return AddView(id, focus)
}

// Arrange lays the deck's window out: the sidebar at its width on the
// left, and the views in a grid right of it (stageLayout).
func Arrange() error {
	out, err := tmux.Out("display-message", "-p", "-t", tmux.MainSession+":0", "#{window_width} #{window_height}")
	if err != nil {
		return err
	}
	var w, h int
	if _, err := fmt.Sscanf(out, "%d %d", &w, &h); err != nil {
		return fmt.Errorf("window size %q: %v", out, err)
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	ids := []int{-1}
	for _, p := range panes {
		if p.Session == tmux.MainSession && p.Index == 0 {
			ids[0] = paneNum(p.ID)
		}
	}
	for _, p := range tmux.Views(panes) {
		ids = append(ids, paneNum(p.ID))
	}
	if ids[0] < 0 || len(ids) < 2 {
		return nil
	}
	return tmux.Run("select-layout", "-t", tmux.MainSession+":0", stageLayout(w, h, SidebarWidth(), ids))
}

func paneNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "%"))
	return n
}

// minViewWidth is the narrowest a view may be made to share a row.
const minViewWidth = 70

// stageLayout is a tmux layout of a w×h window: the sidebar (ids[0]) sw
// wide on the left, and the views (ids[1:]) in a grid on the rest, as
// many to a row as fit minViewWidth, the rows as even as they come.
func stageLayout(w, h, sw int, ids []int) string {
	sw = min(max(sw, 1), w-2)
	rest, views := w-sw-1, ids[1:]
	cols := min(len(views), max((rest+1)/(minViewWidth+1), 1))
	var rows []node
	for i := 0; i < len(views); i += cols {
		var row []node
		for _, id := range views[i:min(i+cols, len(views))] {
			row = append(row, leafNode(id))
		}
		rows = append(rows, splitNode(true, row))
	}
	stage := splitNode(false, rows)(rest, h, sw+1, 0)
	body := fmt.Sprintf("%dx%d,0,0{%s,%s}", w, h, leafNode(ids[0])(sw, h, 0, 0), stage)
	return fmt.Sprintf("%04x,%s", layoutChecksum(body), body)
}

// node is a cell of a tmux layout, drawn once its size and place are
// known.
type node func(w, h, x, y int) string

func leafNode(id int) node {
	return func(w, h, x, y int) string { return fmt.Sprintf("%dx%d,%d,%d,%d", w, h, x, y, id) }
}

// splitNode shares a cell among children evenly, side by side when across,
// else one over the other; the borders between them take a line each.
func splitNode(across bool, children []node) node {
	if len(children) == 1 {
		return children[0]
	}
	return func(w, h, x, y int) string {
		length := h
		if across {
			length = w
		}
		n := len(children)
		each := (length - (n - 1)) / n
		var cells []string
		at := 0
		for i, c := range children {
			size := each
			if i == n-1 {
				size = length - at
			}
			if across {
				cells = append(cells, c(size, h, x+at, y))
			} else {
				cells = append(cells, c(w, size, x, y+at))
			}
			at += size + 1
		}
		open, end := "[", "]"
		if across {
			open, end = "{", "}"
		}
		return fmt.Sprintf("%dx%d,%d,%d%s%s%s", w, h, x, y, open, strings.Join(cells, ","), end)
	}
}

// layoutChecksum is the checksum tmux expects at the head of a layout.
func layoutChecksum(s string) uint16 {
	var c uint16
	for i := 0; i < len(s); i++ {
		c = (c >> 1) + ((c & 1) << 15)
		c += uint16(s[i])
	}
	return c
}

// ensureAgent starts agent id, or resumes its session when its process
// is gone.
func ensureAgent(st *state.State, id string, kinds []agent.Kind) error {
	p, a := st.FindAgent(id)
	if a == nil {
		return fmt.Errorf("unknown agent %s", id)
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	pane, ok := tmux.FindPane(panes, id)
	switch {
	case !ok:
		return StartAgent(p, a, true, kinds)
	case pane.Dead:
		return RestartAgent(p, a, pane.ID, kinds)
	}
	return nil
}

// FocusStage moves the focus to the view of madID, else to the view in
// use.
func FocusStage(madID string) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	if p, ok := tmux.FindPane(panes, madID); ok && tmux.OnStage(p) {
		return tmux.Run("select-pane", "-t", p.ID)
	}
	if p, ok := tmux.Stage(panes); ok {
		return tmux.Run("select-pane", "-t", p.ID)
	}
	return nil
}

// ShowViews makes the stage show the panes tagged ids, at most MaxViews,
// and nothing else: the first in the view in use, the others beside it.
// Agents among them are started or resumed as needed. With focus the
// first takes the focus; else it stays where it is.
func ShowViews(st *state.State, ids []string, kinds []agent.Kind, focus bool) error {
	ids = ids[:min(len(ids), MaxViews)]
	for _, id := range ids {
		if tmux.IsAgentID(id) {
			if err := ensureAgent(st, id, kinds); err != nil {
				return err
			}
		}
	}
	if err := ShowPane(ids[0], false); err != nil {
		return err
	}
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for _, v := range tmux.Views(panes) {
		if v.MadID != "" && !keep[v.MadID] {
			if err := CloseView(v.MadID); err != nil {
				return err
			}
		}
	}
	for _, id := range ids[1:] {
		if err := AddView(id, false); err != nil {
			return err
		}
	}
	if focus {
		return FocusStage(ids[0])
	}
	return nil
}
