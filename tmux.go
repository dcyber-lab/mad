package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// tmuxSocket is mad's private tmux server (MAD_SOCKET overrides, for tests).
var tmuxSocket = envOr("MAD_SOCKET", "mad")

const (
	mainSession = "main"
	poolSession = "_pool"
	sidebarPane = "main:0.0"
	stagePane   = "main:0.1"

	// @mad_id values for mad's own panes; agent panes carry their UUID.
	idSidebar     = "_sidebar"
	idPlaceholder = "_placeholder"
	idKeepalive   = "_keep"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func tmuxArgs(args ...string) []string {
	return append([]string{"-L", tmuxSocket, "-f", tmuxConfPath()}, args...)
}

func tmuxOut(args ...string) (string, error) {
	cmd := exec.Command("tmux", tmuxArgs(args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func tmuxRun(args ...string) error {
	_, err := tmuxOut(args...)
	return err
}

func hasSession(name string) bool {
	return tmuxRun("has-session", "-t", "="+name) == nil
}

type Pane struct {
	ID       string // %12
	MadID    string // @mad_id pane option
	Session  string
	WindowID string
	Index    int
	Dead     bool
	Active   bool
	TTY      string
	Width    int
	Height   int
}

func listPanes() ([]Pane, error) {
	out, err := tmuxOut("list-panes", "-a", "-F",
		"#{pane_id}\t#{@mad_id}\t#{session_name}\t#{window_id}\t#{window_index}.#{pane_index}\t#{pane_dead}\t#{pane_width}\t#{pane_height}\t#{pane_active}\t#{pane_tty}")
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 10 {
			continue
		}
		p := Pane{ID: f[0], MadID: f[1], Session: f[2], WindowID: f[3], Dead: f[5] == "1", Active: f[8] == "1", TTY: f[9]}
		p.Width, _ = strconv.Atoi(f[6])
		p.Height, _ = strconv.Atoi(f[7])
		// Only the main window's layout matters: 0.0 sidebar, 0.1 stage.
		switch {
		case p.Session != mainSession:
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
	return panes, nil
}

func findPane(panes []Pane, madID string) (Pane, bool) {
	for _, p := range panes {
		if p.MadID == madID {
			return p, true
		}
	}
	return Pane{}, false
}

func stageOf(panes []Pane) (Pane, bool) {
	for _, p := range panes {
		if p.Session == mainSession && p.Index == 1 {
			return p, true
		}
	}
	return Pane{}, false
}

func tagPane(paneID, madID string) error {
	return tmuxRun("set-option", "-p", "-t", paneID, "@mad_id", madID)
}

func capturePane(paneID string) string {
	out, _ := tmuxOut("capture-pane", "-p", "-t", paneID)
	return out
}
