package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// SessionPrefix names the tmux sessions the window attaches through. Each
// terminal in the window gets one, grouped with the pool so it shares the
// pool's windows but has a current window of its own: the agent's.
const SessionPrefix = tmux.DeskPrefix

// terms keeps track of the open terminals: which agents a window is
// looking at, and what to end when the server stops.
type terms struct {
	mu    sync.Mutex
	focus map[string]int // agent id → focused terminals
	stops map[*func()]bool
}

func newTerms() *terms {
	return &terms{focus: map[string]int{}, stops: map[*func()]bool{}}
}

func (t *terms) Focused(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.focus[id] > 0
}

func (t *terms) setFocus(id string, delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.focus[id] += delta; t.focus[id] <= 0 {
		delete(t.focus, id)
	}
}

func (t *terms) add(stop *func()) {
	t.mu.Lock()
	t.stops[stop] = true
	t.mu.Unlock()
}

func (t *terms) remove(stop *func()) {
	t.mu.Lock()
	delete(t.stops, stop)
	t.mu.Unlock()
}

func (t *terms) CloseAll() {
	t.mu.Lock()
	stops := make([]*func(), 0, len(t.stops))
	for s := range t.stops {
		stops = append(stops, s)
	}
	t.mu.Unlock()
	for _, s := range stops {
		(*s)()
	}
}

// control is a text message from the page; keystrokes come as binary ones.
type control struct {
	Type string `json:"type"` // resize, focus, blur
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// term attaches a websocket to agent id's pane: the page's keystrokes go
// to a tmux client in a pty, the client's output back to the page.
func (s *Server) term(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("id")
	cols, rows := atoiOr(q.Get("cols"), 120), atoiOr(q.Get("rows"), 40)
	// The default accept refuses other origins than the page's own.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sess, err := Attach(id, s.kinds)
	if err != nil {
		msg, _ := json.Marshal(map[string]string{"type": "error", "message": err.Error()})
		_ = c.Write(ctx, websocket.MessageText, msg)
		c.Close(websocket.StatusNormalClosure, "")
		return
	}
	defer func() { _ = tmux.Run("kill-session", "-t", "="+sess) }()

	// The session goes with its last client, so a closed tab or a crashed
	// window leaves nothing behind. Set once attached: tmux destroys an
	// unattached session as soon as the option is on.
	cmd := exec.Command("tmux", tmux.Args("attach-session", "-t", "="+sess, ";",
		"set-option", "-t", sess, "destroy-unattached", "on")...)
	cmd.Env = clientEnv()
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		c.Close(websocket.StatusInternalError, "pty")
		return
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			_ = f.Close()
			_ = cmd.Process.Kill()
		})
	}
	s.terms.add(&stop)
	defer s.terms.remove(&stop)
	defer stop()
	defer func() { go func() { _ = cmd.Wait() }() }()

	focused := false
	defer func() {
		if focused {
			s.terms.setFocus(id, -1)
		}
	}()

	go func() {
		defer stop()
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
				werr := c.Write(wctx, websocket.MessageBinary, buf[:n])
				wcancel()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			if _, err := f.Write(data); err != nil {
				return
			}
			continue
		}
		var m control
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "resize":
			if m.Cols > 0 && m.Rows > 0 {
				_ = pty.Setsize(f, &pty.Winsize{Cols: uint16(m.Cols), Rows: uint16(m.Rows)})
			}
		case "focus":
			if !focused {
				focused = true
				s.terms.setFocus(id, 1)
			}
		case "blur":
			if focused {
				focused = false
				s.terms.setFocus(id, -1)
			}
		}
	}
}

// Attach makes agent id's pane ready to be looked at from the window: the
// deck runs, the agent's process runs (resumed if it was gone), its pane
// is off the terminal deck's stage, and a session of its own shows it.
// It returns that session's name; the caller kills it when done.
func Attach(id string, kinds []agent.Kind) (string, error) {
	st, err := state.Load()
	if err != nil {
		return "", err
	}
	p, a := st.FindAgent(id)
	if a == nil {
		return "", fmt.Errorf("no agent %q", id)
	}
	if err := drive.EnsureDeck(p.Path); err != nil {
		return "", err
	}
	if err := deck.EnsureAgent(st, id, kinds); err != nil {
		return "", err
	}
	pane, err := findPane(id)
	if err != nil {
		return "", err
	}
	if tmux.OnStage(pane) {
		// A pane is in one window at a time: the terminal deck gives it up
		// (its stage shows something else) so the window can have it.
		if err := deck.CloseView(id); err != nil {
			return "", err
		}
		if pane, err = findPane(id); err != nil {
			return "", err
		}
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	sess := SessionPrefix + hex.EncodeToString(b)
	if err := tmux.Run("new-session", "-d", "-s", sess, "-t", "="+pane.Session); err != nil {
		return "", err
	}
	// Option and window targets take the name as is (no "="): it is
	// random, so it matches no other session.
	steps := [][]string{
		{"select-window", "-t", sess + ":" + pane.WindowID},
		// The terminal deck sizes pool windows by hand before it shows
		// them; here the window follows the terminal it is shown in.
		{"set-window-option", "-u", "-t", pane.WindowID, "window-size"},
	}
	for _, args := range steps {
		if err := tmux.Run(args...); err != nil {
			_ = tmux.Run("kill-session", "-t", "="+sess)
			return "", err
		}
	}
	return sess, nil
}

func findPane(id string) (tmux.Pane, error) {
	panes, err := tmux.ListPanes()
	if err != nil {
		return tmux.Pane{}, err
	}
	p, ok := tmux.FindPane(panes, id)
	if !ok {
		return tmux.Pane{}, errors.New("the agent's pane is gone")
	}
	return p, nil
}

// clientEnv is the environment of the tmux client the window drives: the
// server's own, as a terminal emulator that xterm.js is.
func clientEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k == "TMUX" || k == "TMUX_PANE" || k == "TERM" || k == "COLORTERM" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n < 1000 {
		return n
	}
	return def
}
