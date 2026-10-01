// Package tmux wraps mad's private tmux server.
//
// Layout: session "main" has one window with the sidebar (pane 0) and the
// stage (pane 1). Every agent runs in its own window of the hidden "_pool"
// session and is swapped into the stage when shown. Panes are identified by
// the @mad_id pane option: the agent's UUID, or one of the Id* values.
package tmux

import (
	"bytes"
	"errors"
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
	// Asleep: mad ended the agent's process to free what it held (the
	// @mad_asleep pane option); the dead pane waits to be resumed.
	Asleep bool
	Active bool
	TTY    string
	PID    int // the pane's process
	Width  int
	Height int
}

const paneFormat = "#{pane_id}\t#{@mad_id}\t#{session_name}\t#{window_id}\t#{window_index}.#{pane_index}\t" +
	"#{pane_dead}\t#{pane_width}\t#{pane_height}\t#{pane_active}\t#{pane_tty}\t#{pane_pid}\t#{@mad_asleep}"

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
		if len(f) != 12 {
			continue
		}
		p := Pane{ID: f[0], MadID: f[1], Session: f[2], WindowID: f[3], Dead: f[5] == "1", Active: f[8] == "1", TTY: f[9],
			Asleep: f[11] == "1"}
		p.Width, _ = strconv.Atoi(f[6])
		p.Height, _ = strconv.Atoi(f[7])
		p.PID, _ = strconv.Atoi(f[10])
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

// CaptureStyled returns the visible text of a pane with its colors and
// attributes as escape sequences.
func CaptureStyled(paneID string) string {
	out, _ := Out("capture-pane", "-p", "-e", "-t", paneID)
	return out
}

// PasteSettle is how long Paste waits between the text and enter: an agent
// that gets enter in the same read as a paste can take it for part of it.
var PasteSettle = 300 * time.Millisecond

// Paste types text into a pane as one message: a bracketed paste (agents
// that ask for one take newlines in it as text, not as enter), then enter.
func Paste(paneID, text string) error {
	buf := "mad-paste-" + strings.TrimPrefix(paneID, "%")
	cmd := exec.Command("tmux", Args("load-buffer", "-b", buf, "-")...)
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer: %v: %s", err, strings.TrimSpace(string(out)))
	}
	// -r keeps newlines as they are; tmux would turn them into returns.
	if err := Run("paste-buffer", "-p", "-r", "-d", "-b", buf, "-t", paneID); err != nil {
		return err
	}
	time.Sleep(PasteSettle)
	return Run("send-keys", "-t", paneID, "Enter")
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

// parseVersion reads the numbers of "tmux 3.3a" or "tmux next-3.5"; ok is
// false for a version without them ("tmux master").
func parseVersion(v string) (major, minor int, ok bool) {
	i := strings.IndexAny(v, "0123456789")
	if i < 0 {
		return 0, 0, false
	}
	if n, _ := fmt.Sscanf(v[i:], "%d.%d", &major, &minor); n < 2 {
		return 0, 0, false
	}
	return major, minor, true
}

// versionAtLeast: anything without a version number counts as too old.
func versionAtLeast(v string, major, minor int) bool {
	ma, mi, ok := parseVersion(v)
	return ok && (ma > major || ma == major && mi >= minor)
}

// The oldest tmux mad runs on. 3.0 brought pane options, which tell mad's
// panes apart (@mad_id), and -e, which hands an agent its id.
const minMajor, minMinor = 3, 0

// CheckVersion says what is wrong when tmux is missing or too old for mad,
// rather than leave it to the first tmux command that fails.
func CheckVersion() error {
	out, err := exec.Command("tmux", "-V").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("tmux is not installed (or not on your PATH); mad needs tmux %d.%d or newer", minMajor, minMinor)
	}
	if err != nil {
		return nil // tmux is there; what it does will show
	}
	return checkVersion(string(out))
}

// checkVersion lets a version it can't read pass: a build from tmux's
// master says "tmux master".
func checkVersion(v string) error {
	if _, _, ok := parseVersion(v); !ok || versionAtLeast(v, minMajor, minMinor) {
		return nil
	}
	return fmt.Errorf("%s is too old: mad needs tmux %d.%d or newer", strings.TrimSpace(v), minMajor, minMinor)
}

// Versions returns the version of the running server and of the installed
// tmux, as in "3.4" and "3.7c"; "" for one that cannot be told. They differ
// once tmux is upgraded under a running deck.
func Versions() (server, client string) {
	server, _ = Out("display-message", "-p", "#{version}")
	out, _ := exec.Command("tmux", "-V").Output()
	return server, clientVersion(string(out))
}

// clientVersion takes the version out of what "tmux -V" prints.
func clientVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "tmux ")
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
