// Package tmux wraps mad's private tmux server.
//
// Layout: session "main" has one window with the sidebar (pane 0) and the
// stage (pane 1). Every agent runs in its own window of the hidden "_pool"
// session and is swapped into the stage when shown. Panes are identified by
// the @mad_id pane option: the agent's UUID, or one of the Id* values.
package tmux

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Socket is the tmux server name (-L); MAD_SOCKET overrides it, which is
// handy for development and tests.
var Socket = envOr("MAD_SOCKET", "mad")

const (
	MainSession = "main"
	PoolSession = "_pool"
	SidebarPane = "main:0.0"
	StagePane   = "main:0.1"

	IDSidebar     = "_sidebar"
	IDPlaceholder = "_placeholder"
	IDKeepalive   = "_keep"
	IDTask        = "_task" // a one-off pane on stage: the diff viewer (v), a finish command (f)
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Args prefixes a tmux command line with mad's socket and config. -u: without
// a UTF-8 locale tmux turns the tabs in -F output into "_" (breaking
// ListPanes) and draws the sidebar's glyphs as "_" too.
func Args(args ...string) []string {
	return append([]string{"-u", "-L", Socket, "-f", paths.TmuxConf()}, args...)
}

// Out runs a tmux command and returns its trimmed stdout.
func Out(args ...string) (string, error) {
	cmd := exec.Command("tmux", Args(args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func Run(args ...string) error {
	_, err := Out(args...)
	return err
}

func HasSession(name string) bool {
	return Run("has-session", "-t", "="+name) == nil
}

// InDeck reports whether this process runs inside mad's tmux server.
func InDeck() bool {
	return strings.Contains(os.Getenv("TMUX"), "/"+Socket+",")
}

// IsAgentID tells agent panes from mad's own (sidebar, placeholder, ...).
func IsAgentID(id string) bool { return id != "" && !strings.HasPrefix(id, "_") }

type Pane struct {
	ID       string // %12
	MadID    string // @mad_id pane option
	Session  string
	WindowID string
	Index    int // 0 sidebar, 1 stage, -1 anywhere else
	Dead     bool
	Active   bool
	TTY      string
	Width    int
	Height   int
}

const paneFormat = "#{pane_id}\t#{@mad_id}\t#{session_name}\t#{window_id}\t#{window_index}.#{pane_index}\t" +
	"#{pane_dead}\t#{pane_width}\t#{pane_height}\t#{pane_active}\t#{pane_tty}"

func ListPanes() ([]Pane, error) {
	out, err := Out("list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return nil, err
	}
	return parsePanes(out), nil
}

func parsePanes(out string) []Pane {
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 10 {
			continue
		}
		p := Pane{ID: f[0], MadID: f[1], Session: f[2], WindowID: f[3], Dead: f[5] == "1", Active: f[8] == "1", TTY: f[9]}
		p.Width, _ = strconv.Atoi(f[6])
		p.Height, _ = strconv.Atoi(f[7])
		switch {
		case p.Session != MainSession:
			p.Index = -1
		case f[4] == "0.0":
			p.Index = 0
		case f[4] == "0.1":
			p.Index = 1
		default:
			p.Index = -1
		}
		panes = append(panes, p)
	}
	return panes
}

func FindPane(panes []Pane, madID string) (Pane, bool) {
	for _, p := range panes {
		if p.MadID == madID {
			return p, true
		}
	}
	return Pane{}, false
}

// Stage is the pane currently shown right of the sidebar.
func Stage(panes []Pane) (Pane, bool) {
	for _, p := range panes {
		if p.Session == MainSession && p.Index == 1 {
			return p, true
		}
	}
	return Pane{}, false
}

func Tag(paneID, madID string) error {
	return Run("set-option", "-p", "-t", paneID, "@mad_id", madID)
}

// Capture returns the visible text of a pane.
func Capture(paneID string) string {
	out, _ := Out("capture-pane", "-p", "-t", paneID)
	return out
}

// Watched reports whether someone is looking at the deck: a client is
// attached to the main session and its terminal has focus. tmux tracks focus
// from 3.3 on; with an older tmux any attached client counts as looking.
func Watched() bool {
	out, err := Out("list-clients", "-t", MainSession, "-F", "x#{client_flags}")
	if err != nil {
		return false
	}
	return watched(out, tracksFocus())
}

func watched(clients string, focusKnown bool) bool {
	for _, line := range strings.Split(clients, "\n") {
		flags, ok := strings.CutPrefix(line, "x") // one line per client
		if !ok {
			continue
		}
		if !focusKnown {
			return true
		}
		for _, f := range strings.Split(flags, ",") {
			if f == "focused" {
				return true
			}
		}
	}
	return false
}

var focusVersion struct {
	sync.Once
	ok bool
}

func tracksFocus() bool {
	focusVersion.Do(func() {
		out, err := exec.Command("tmux", "-V").Output()
		focusVersion.ok = err == nil && versionAtLeast(string(out), 3, 3)
	})
	return focusVersion.ok
}

// versionAtLeast parses "tmux 3.3a" or "tmux next-3.5"; anything without a
// version number counts as too old.
func versionAtLeast(v string, major, minor int) bool {
	i := strings.IndexAny(v, "0123456789")
	if i < 0 {
		return false
	}
	var ma, mi int
	if n, _ := fmt.Sscanf(v[i:], "%d.%d", &ma, &mi); n < 2 {
		return false
	}
	return ma > major || ma == major && mi >= minor
}

// CaptureAll returns the visible text of several panes, keyed by pane id,
// with one tmux call instead of one per pane: a marker line printed before
// each capture splits the output. If any pane is gone tmux stops at it, so
// callers fall back to Capture on error.
func CaptureAll(paneIDs []string) (map[string]string, error) {
	screens := map[string]string{}
	if len(paneIDs) == 0 {
		return screens, nil
	}
	marker := fmt.Sprintf("mad-capture-%d-%d ", os.Getpid(), time.Now().UnixNano())
	var args []string
	for _, id := range paneIDs {
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args, "display-message", "-p", "-t", id, marker+"#{pane_id}", ";", "capture-pane", "-p", "-t", id)
	}
	out, err := Out(args...)
	if err != nil {
		return nil, err
	}
	var cur string
	var b strings.Builder
	flush := func() {
		if cur != "" {
			screens[cur] = strings.TrimRight(b.String(), "\n")
		}
		b.Reset()
	}
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(line, marker); ok {
			flush()
			cur = id
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	flush()
	if len(screens) != len(paneIDs) {
		return nil, fmt.Errorf("tmux: captured %d of %d panes", len(screens), len(paneIDs))
	}
	return screens, nil
}
