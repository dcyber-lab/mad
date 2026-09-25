package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

const (
	defaultSidebarWidth = 30
	minSidebarWidth     = 16
	maxSidebarWidth     = 100
)

func sidebarWidthPath() string { return filepath.Join(stateDir(), "sidebar_width") }

// sidebarWidth is the user's last sidebar width (dragged or set with </>).
func sidebarWidth() int {
	data, err := os.ReadFile(sidebarWidthPath())
	if err != nil {
		return defaultSidebarWidth
	}
	w, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return defaultSidebarWidth
	}
	return clampWidth(w)
}

func saveSidebarWidth(w int) error {
	return writeFileAtomic(sidebarWidthPath(), []byte(strconv.Itoa(clampWidth(w))))
}

func clampWidth(w int) int {
	if w < minSidebarWidth {
		return minSidebarWidth
	}
	if w > maxSidebarWidth {
		return maxSidebarWidth
	}
	return w
}

// fitSidebar restores the saved width, e.g. after the terminal resized and
// tmux spread the change over both panes.
func fitSidebar() error {
	return tmuxRun("resize-pane", "-t", sidebarPane, "-x", strconv.Itoa(sidebarWidth()))
}

func sidebarLogPath() string { return filepath.Join(stateDir(), "sidebar.log") }

// sidebarCommand keeps the sidebar alive: a crash is logged and the sidebar
// restarts instead of leaving a dead pane.
func sidebarCommand() string {
	return fmt.Sprintf("while :; do %s sidebar 2>>%s; sleep 1; done",
		shellQuote(selfPath()), shellQuote(sidebarLogPath()))
}

// cmdOpen builds the deck if needed and attaches the terminal to it.
func cmdOpen() error {
	if inDeck() {
		return tmuxRun("select-pane", "-t", sidebarPane)
	}
	if err := writeConfigs(); err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	if root, ok := projectRoot(cwd); ok {
		st, err := loadState()
		if err != nil {
			return err
		}
		if _, added := st.addProject(root); added {
			if err := st.save(); err != nil {
				return err
			}
		}
	}

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 40 || h < 10 {
		w, h = 200, 50
	}
	if tmuxRun("list-sessions") == nil {
		_ = tmuxRun("source-file", tmuxConfPath())
	}
	if err := ensureLayout(cwd, w, h); err != nil {
		return err
	}

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	// Allow opening the deck from inside someone else's tmux.
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TMUX=") {
			env = append(env, e)
		}
	}
	argv := append([]string{"tmux"}, tmuxArgs("attach-session", "-t", mainSession)...)
	return syscall.Exec(tmuxBin, argv, env)
}

func inDeck() bool {
	return strings.Contains(os.Getenv("TMUX"), "/"+tmuxSocket+",")
}

// ensureLayout creates the main (sidebar | stage) window and the hidden
// pool session. On an existing deck it restarts the sidebar so a rebuilt
// binary takes effect.
func ensureLayout(cwd string, w, h int) error {
	self := shellQuote(selfPath())
	ws, hs := strconv.Itoa(w), strconv.Itoa(h)

	if !hasSession(poolSession) {
		id, err := tmuxOut("new-session", "-d", "-s", poolSession, "-n", "keep", "-x", ws, "-y", hs,
			"-P", "-F", "#{pane_id}", self+" placeholder")
		if err != nil {
			return err
		}
		if err := tagPane(id, idKeepalive); err != nil {
			return err
		}
	}

	if !hasSession(mainSession) {
		// Stage first, then the sidebar split off to its left, so both panes
		// exist before the sidebar starts polling.
		ph, err := tmuxOut("new-session", "-d", "-s", mainSession, "-n", "deck", "-x", ws, "-y", hs, "-c", cwd,
			"-P", "-F", "#{pane_id}", self+" placeholder")
		if err != nil {
			return err
		}
		if err := tagPane(ph, idPlaceholder); err != nil {
			return err
		}
		sb, err := tmuxOut("split-window", "-h", "-b", "-l", strconv.Itoa(sidebarWidth()), "-t", ph, "-c", cwd,
			"-P", "-F", "#{pane_id}", sidebarCommand())
		if err != nil {
			return err
		}
		if err := tagPane(sb, idSidebar); err != nil {
			return err
		}
		return tmuxRun("select-pane", "-t", sb)
	}

	panes, err := listPanes()
	if err != nil {
		return err
	}
	if err := ensureStage(panes); err != nil {
		return err
	}
	if err := tmuxRun("respawn-pane", "-k", "-t", sidebarPane, "-c", cwd, sidebarCommand()); err != nil {
		return err
	}
	return fitSidebar()
}

// ensureStage puts a pane back to the right of the sidebar if the stage
// went missing (e.g. its pane was killed from inside).
func ensureStage(panes []Pane) error {
	n := 0
	for _, p := range panes {
		if p.Session == mainSession {
			n++
		}
	}
	if n >= 2 {
		return nil
	}
	if ph, ok := findPane(panes, idPlaceholder); ok {
		if err := tmuxRun("join-pane", "-d", "-h", "-s", ph.ID, "-t", sidebarPane); err != nil {
			return err
		}
	} else {
		id, err := tmuxOut("split-window", "-d", "-h", "-t", sidebarPane, "-P", "-F", "#{pane_id}",
			shellQuote(selfPath())+" placeholder")
		if err != nil {
			return err
		}
		if err := tagPane(id, idPlaceholder); err != nil {
			return err
		}
	}
	return fitSidebar()
}

// showPane swaps the pane tagged madID into the stage and focuses it.
func showPane(madID string, focus bool) error {
	panes, err := listPanes()
	if err != nil {
		return err
	}
	stage, ok := stageOf(panes)
	if !ok {
		if err := ensureStage(panes); err != nil {
			return err
		}
		if panes, err = listPanes(); err != nil {
			return err
		}
		if stage, ok = stageOf(panes); !ok {
			return fmt.Errorf("no stage pane")
		}
	}
	target, ok := findPane(panes, madID)
	if !ok {
		return fmt.Errorf("pane for %s not found", madID)
	}
	if target.ID != stage.ID {
		// Pre-size the agent's window so the swap doesn't trigger a resize.
		_ = tmuxRun("resize-window", "-t", target.WindowID,
			"-x", strconv.Itoa(stage.Width), "-y", strconv.Itoa(stage.Height))
		if err := tmuxRun("swap-pane", "-d", "-s", target.ID, "-t", stage.ID); err != nil {
			return err
		}
	}
	if focus {
		return tmuxRun("select-pane", "-t", stagePane)
	}
	return nil
}

func startAgent(p *Project, a *Agent, resume bool, kinds []AgentKind) error {
	cmd := kindByName(kinds, a.Kind).command(a, resume)
	id, err := tmuxOut("new-window", "-d", "-t", poolSession+":", "-n", a.Kind, "-c", p.dirFor(a),
		"-e", "MAD_AGENT_ID="+a.ID, "-P", "-F", "#{pane_id}", cmd)
	if err != nil {
		return err
	}
	return tagPane(id, a.ID)
}

func restartAgent(p *Project, a *Agent, paneID string, kinds []AgentKind) error {
	cmd := kindByName(kinds, a.Kind).command(a, true)
	return tmuxRun("respawn-pane", "-k", "-t", paneID, "-c", p.dirFor(a), "-e", "MAD_AGENT_ID="+a.ID, cmd)
}

// openAgent shows agent id in the stage, (re)starting it with its previous
// session if its process is gone.
func openAgent(st *State, id string, kinds []AgentKind) error {
	p, a := st.findAgent(id)
	if a == nil {
		return fmt.Errorf("unknown agent %s", id)
	}
	panes, err := listPanes()
	if err != nil {
		return err
	}
	pane, ok := findPane(panes, id)
	switch {
	case !ok:
		err = startAgent(p, a, true, kinds)
	case pane.Dead:
		err = restartAgent(p, a, pane.ID, kinds)
	}
	if err != nil {
		return err
	}
	return showPane(id, true)
}

func killAgent(id string) error {
	panes, err := listPanes()
	if err != nil {
		return err
	}
	pane, ok := findPane(panes, id)
	if !ok {
		return nil
	}
	if stage, ok := stageOf(panes); ok && stage.ID == pane.ID {
		if err := showPane(idPlaceholder, false); err != nil {
			return err
		}
	}
	return tmuxRun("kill-pane", "-t", pane.ID)
}

func cmdAdd(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	root, _ := projectRoot(expandPath(dir))
	st, err := loadState()
	if err != nil {
		return err
	}
	if _, added := st.addProject(root); !added {
		fmt.Println("already added:", shortPath(root))
		return nil
	}
	fmt.Println("added:", shortPath(root))
	return st.save()
}

func cmdSwitch(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: mad switch N|next|prev")
	}
	st, err := loadState()
	if err != nil {
		return err
	}
	agents := st.orderedAgents()
	n := len(agents)
	if n == 0 {
		return nil
	}
	cur := -1
	if panes, err := listPanes(); err == nil {
		if stage, ok := stageOf(panes); ok {
			for i, a := range agents {
				if a.ID == stage.MadID {
					cur = i
				}
			}
		}
	}
	var i int
	switch args[0] {
	case "next":
		i = (cur + 1) % n
	case "prev":
		i = (cur - 1 + n) % n
		if cur < 0 {
			i = n - 1
		}
	default:
		k, err := strconv.Atoi(args[0])
		if err != nil || k < 1 || k > n {
			return nil
		}
		i = k - 1
	}
	return openAgent(st, agents[i].ID, loadKinds())
}

func writeConfigs() error {
	self := shellQuote(selfPath())
	var b strings.Builder
	b.WriteString(`# generated by mad; rewritten on every "mad" start
set -g prefix C-]
unbind C-b
bind C-] send-prefix
set -g status off
set -g mouse on
set -sg escape-time 10
set -g focus-events on
set -g history-limit 50000
set -g remain-on-exit on
set -g set-clipboard on
set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",xterm-256color:RGB,xterm-ghostty:RGB"
set -g pane-border-style "fg=colour238"
set -g pane-active-border-style "fg=colour75"
`)
	fmt.Fprintf(&b, "set-hook -g client-resized \"run-shell -b '%s fit'\"\n", selfPath())
	// Sidebar width: drag the border, or prefix + < / >.
	fmt.Fprintf(&b, "bind -r < resize-pane -t %s -L 2\nbind -r > resize-pane -t %s -R 2\n", sidebarPane, sidebarPane)
	toggle := fmt.Sprintf(`if -F "#{==:#{pane_index},0}" "select-pane -t %s" "select-pane -t %s"`, stagePane, sidebarPane)
	fmt.Fprintf(&b, "bind -n M-s %s\nbind s %s\n", toggle, toggle)
	run := func(key, arg string) {
		fmt.Fprintf(&b, "bind %s run-shell -b \"%s switch %s\"\n", key, self, arg)
	}
	run("-n M-j", "next")
	run("-n M-k", "prev")
	run("n", "next")
	run("p", "prev")
	for i := 1; i <= 9; i++ {
		run(fmt.Sprintf("-n M-%d", i), strconv.Itoa(i))
		run(strconv.Itoa(i), strconv.Itoa(i))
	}
	if err := writeFileAtomic(tmuxConfPath(), []byte(b.String())); err != nil {
		return err
	}

	hook := []map[string]any{{"hooks": []map[string]any{{
		"type": "command", "command": self + " hook claude", "timeout": 5,
	}}}}
	toolHook := []map[string]any{{"matcher": "*", "hooks": hook[0]["hooks"]}}
	settings := map[string]any{"hooks": map[string]any{
		"SessionStart":     hook,
		"UserPromptSubmit": hook,
		"PreToolUse":       toolHook,
		"PostToolUse":      toolHook,
		"Notification":     hook,
		"Stop":             hook,
	}}
	data, _ := json.MarshalIndent(settings, "", "  ")
	return writeFileAtomic(claudeSettingsPath(), data)
}
