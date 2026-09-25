// Package deck manages the tmux layout and the lifecycle of agents: it
// builds the sidebar | stage window, starts agents in the hidden pool,
// swaps them into the stage, and writes the generated tmux and claude
// configs.
package deck

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// SelfCommand is the shell command that runs `mad <sub>`. Tests replace it
// so the deck's own panes don't run the test binary.
var SelfCommand = func(sub string) string {
	return paths.ShellQuote(paths.Self()) + " " + sub
}

const (
	DefaultSidebarWidth = 30
	MinSidebarWidth     = 16
	MaxSidebarWidth     = 100
)

// SidebarWidth is the user's last sidebar width (dragged or set with </>).
func SidebarWidth() int {
	data, err := os.ReadFile(paths.SidebarWidthFile())
	if err != nil {
		return DefaultSidebarWidth
	}
	w, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return DefaultSidebarWidth
	}
	return clampWidth(w)
}

func SaveSidebarWidth(w int) error {
	return paths.WriteFileAtomic(paths.SidebarWidthFile(), []byte(strconv.Itoa(clampWidth(w))))
}

func clampWidth(w int) int {
	if w < MinSidebarWidth {
		return MinSidebarWidth
	}
	if w > MaxSidebarWidth {
		return MaxSidebarWidth
	}
	return w
}

// FitSidebar restores the saved width, e.g. after the terminal resized and
// tmux spread the change over both panes.
func FitSidebar() error {
	return tmux.Run("resize-pane", "-t", tmux.SidebarPane, "-x", strconv.Itoa(SidebarWidth()))
}

// sidebarCommand keeps the sidebar alive: a crash is logged and the sidebar
// restarts instead of leaving a dead pane.
func sidebarCommand() string {
	return fmt.Sprintf("while :; do %s 2>>%s; sleep 1; done",
		SelfCommand("sidebar"), paths.ShellQuote(paths.SidebarLog()))
}

// EnsureLayout creates the main (sidebar | stage) window and the hidden
// pool session. On an existing deck it restarts the sidebar so a rebuilt
// binary takes effect.
func EnsureLayout(cwd string, w, h int) error {
	ws, hs := strconv.Itoa(w), strconv.Itoa(h)
	if err := os.MkdirAll(paths.StateDir(), 0o755); err != nil {
		return err
	}

	if !tmux.HasSession(tmux.PoolSession) {
		id, err := tmux.Out("new-session", "-d", "-s", tmux.PoolSession, "-n", "keep", "-x", ws, "-y", hs,
			"-P", "-F", "#{pane_id}", SelfCommand("placeholder"))
		if err != nil {
			return err
		}
		if err := tmux.Tag(id, tmux.IDKeepalive); err != nil {
			return err
		}
	}

	if !tmux.HasSession(tmux.MainSession) {
		// Stage first, then the sidebar split off to its left, so both panes
		// exist before the sidebar starts polling.
		ph, err := tmux.Out("new-session", "-d", "-s", tmux.MainSession, "-n", "deck", "-x", ws, "-y", hs, "-c", cwd,
			"-P", "-F", "#{pane_id}", SelfCommand("placeholder"))
		if err != nil {
			return err
		}
		if err := tmux.Tag(ph, tmux.IDPlaceholder); err != nil {
			return err
		}
		sb, err := tmux.Out("split-window", "-h", "-b", "-l", strconv.Itoa(SidebarWidth()), "-t", ph, "-c", cwd,
			"-P", "-F", "#{pane_id}", sidebarCommand())
		if err != nil {
			return err
		}
		if err := tmux.Tag(sb, tmux.IDSidebar); err != nil {
			return err
		}
		return tmux.Run("select-pane", "-t", sb)
	}

	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	if err := EnsureStage(panes); err != nil {
		return err
	}
	if err := tmux.Run("respawn-pane", "-k", "-t", tmux.SidebarPane, "-c", cwd, sidebarCommand()); err != nil {
		return err
	}
	return FitSidebar()
}

// EnsureStage puts a pane back to the right of the sidebar if the stage
// went missing (e.g. its pane was killed from inside).
func EnsureStage(panes []tmux.Pane) error {
	n := 0
	for _, p := range panes {
		if p.Session == tmux.MainSession {
			n++
		}
	}
	if n >= 2 {
		return nil
	}
	if ph, ok := tmux.FindPane(panes, tmux.IDPlaceholder); ok {
		if err := tmux.Run("join-pane", "-d", "-h", "-s", ph.ID, "-t", tmux.SidebarPane); err != nil {
			return err
		}
	} else {
		id, err := tmux.Out("split-window", "-d", "-h", "-t", tmux.SidebarPane, "-P", "-F", "#{pane_id}",
			SelfCommand("placeholder"))
		if err != nil {
			return err
		}
		if err := tmux.Tag(id, tmux.IDPlaceholder); err != nil {
			return err
		}
	}
	return FitSidebar()
}

// ShowPane swaps the pane tagged madID into the stage, optionally focusing it.
func ShowPane(madID string, focus bool) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	stage, ok := tmux.Stage(panes)
	if !ok {
		if err := EnsureStage(panes); err != nil {
			return err
		}
		if panes, err = tmux.ListPanes(); err != nil {
			return err
		}
		if stage, ok = tmux.Stage(panes); !ok {
			return fmt.Errorf("no stage pane")
		}
	}
	target, ok := tmux.FindPane(panes, madID)
	if !ok {
		return fmt.Errorf("pane for %s not found", madID)
	}
	if target.ID != stage.ID {
		// Pre-size the agent's window so the swap doesn't trigger a resize.
		_ = tmux.Run("resize-window", "-t", target.WindowID,
			"-x", strconv.Itoa(stage.Width), "-y", strconv.Itoa(stage.Height))
		if err := tmux.Run("swap-pane", "-d", "-s", target.ID, "-t", stage.ID); err != nil {
			return err
		}
		if stage.MadID == tmux.IDTask {
			// A task pane is a one-off: it goes when something else takes
			// the stage.
			_ = tmux.Run("kill-pane", "-t", stage.ID)
		}
	}
	if focus {
		return tmux.Run("select-pane", "-t", tmux.StagePane)
	}
	return nil
}

// OpenTask runs command in dir in a new pane on stage: the diff viewer,
// a finish command. There is one such pane at a time; an earlier one is
// replaced.
func OpenTask(dir, command string) error {
	if err := CloseTask(""); err != nil {
		return err
	}
	id, err := tmux.Out("new-window", "-d", "-t", tmux.PoolSession+":", "-n", "diff", "-c", dir,
		"-P", "-F", "#{pane_id}", command)
	if err != nil {
		return err
	}
	if err := tmux.Tag(id, tmux.IDTask); err != nil {
		return err
	}
	return ShowPane(tmux.IDTask, true)
}

// CloseTask removes the task pane. When it is on stage, backID (an agent,
// or the placeholder when empty) takes its place; focus is left where it
// is.
func CloseTask(backID string) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	pane, ok := tmux.FindPane(panes, tmux.IDTask)
	if !ok {
		return nil
	}
	if stage, ok := tmux.Stage(panes); ok && stage.ID == pane.ID {
		if backID == "" {
			backID = tmux.IDPlaceholder
		}
		if _, ok := tmux.FindPane(panes, backID); !ok {
			backID = tmux.IDPlaceholder
		}
		return ShowPane(backID, false) // kills the outgoing diff pane
	}
	return tmux.Run("kill-pane", "-t", pane.ID)
}

// DiffConfig is the "diff" object of ~/.config/mad/config.json:
//
//	{"diff": {"command": "lazygit -p {dir}"}}
//
// The command runs through sh in the agent's directory with {dir}
// replaced by it. Without one, lazygit is used when installed, else git's
// own diff in a pager.
type DiffConfig struct {
	Command string `json:"command,omitempty"`
}

// LoadDiffConfig reads the diff section of config.json.
func LoadDiffConfig() DiffConfig {
	var file struct {
		Diff *DiffConfig `json:"diff"`
	}
	data, err := os.ReadFile(paths.ConfigFile())
	if err != nil || json.Unmarshal(data, &file) != nil || file.Diff == nil {
		return DiffConfig{}
	}
	return *file.Diff
}

// lookPath is replaceable in tests.
var lookPath = exec.LookPath

// FinishAction is one entry of the finish menu (f): a command run on stage
// to wrap up a branch. In config.json:
//
//	{"finish": [{"name": "open PR", "command": "gh pr create --web"}]}
//
// replaces the built-in entries. Commands run through sh in the checkout's
// directory; {dir}, {repo}, {branch} and {base} are replaced, shell-quoted.
type FinishAction struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// LoadFinishConfig reads the finish section of config.json.
func LoadFinishConfig() []FinishAction {
	var file struct {
		Finish []FinishAction `json:"finish"`
	}
	data, err := os.ReadFile(paths.ConfigFile())
	if err != nil || json.Unmarshal(data, &file) != nil {
		return nil
	}
	return file.Finish
}

// Checkout is what a finish action works on.
type Checkout struct {
	Dir    string // the checkout: a worktree, or the project itself
	Repo   string // the project's main checkout
	Branch string // checked out in Dir
	Base   string // the branch work is merged into (git.DefaultBranch)
	// RepoBranch is what Repo has checked out: merging into Base there
	// only makes sense when it is Base.
	RepoBranch string
}

// FinishActions is the finish menu for c: the configured commands, else
// the built-in ones that apply. Placeholders are filled in.
func FinishActions(cfg []FinishAction, c Checkout) []FinishAction {
	var acts []FinishAction
	if len(cfg) > 0 {
		acts = append(acts, cfg...)
	} else {
		if _, err := lookPath("gh"); err == nil {
			acts = append(acts, FinishAction{"push & open PR", "git push -u origin HEAD && gh pr create"})
		}
		if c.Base != "" && c.Branch != c.Base {
			acts = append(acts, FinishAction{"rebase onto " + c.Base, "git rebase {base}"})
			if c.RepoBranch == c.Base && c.Dir != c.Repo {
				acts = append(acts, FinishAction{"merge into " + c.Base, "git -C {repo} merge --no-edit {branch}"})
			}
		}
		acts = append(acts, FinishAction{"push", "git push -u origin HEAD"})
	}
	r := strings.NewReplacer(
		"{dir}", paths.ShellQuote(c.Dir), "{repo}", paths.ShellQuote(c.Repo),
		"{branch}", paths.ShellQuote(c.Branch), "{base}", paths.ShellQuote(c.Base))
	for i := range acts {
		acts[i].Command = r.Replace(acts[i].Command)
	}
	return acts
}

// HoldCommand wraps command so its pane shows how it ended and stays until
// enter is pressed, rather than vanishing with its output.
func HoldCommand(command string) string {
	return "sh -c " + paths.ShellQuote(command+`; s=$?; printf '
[mad] exit %s · press enter
' "$s"; read _`)
}

// DiffCommand is the shell command that shows the changes in dir.
func DiffCommand(cfg DiffConfig, dir string) string {
	q := paths.ShellQuote(dir)
	if cfg.Command != "" {
		return strings.ReplaceAll(cfg.Command, "{dir}", q)
	}
	if _, err := lookPath("lazygit"); err == nil {
		return "lazygit -p " + q
	}
	// Status first (it lists untracked files, which diff skips), then the
	// diff against HEAD: everything the agent did since the last commit.
	return fmt.Sprintf("cd %s && { git -c color.status=always status --short --branch; echo; git -c color.diff=always diff HEAD; } | less -R", q)
}

// command is a's shell command; the kind's provider, if any, says which
// sessions exist to resume.
func command(p *state.Project, a *state.Agent, resume bool, kinds []agent.Kind) string {
	var exists func(string) bool
	if pv := discover.Lookup(a.Kind); pv != nil {
		dir := p.Dir(a)
		exists = func(sid string) bool { return len(pv.Transcripts(dir, sid)) > 0 }
	}
	return agent.ByName(kinds, a.Kind).Command(a, resume, exists)
}

// StartAgent launches a in a new pool window, with MAD_AGENT_ID set for
// its status hooks.
func StartAgent(p *state.Project, a *state.Agent, resume bool, kinds []agent.Kind) error {
	cmd := command(p, a, resume, kinds)
	id, err := tmux.Out("new-window", "-d", "-t", tmux.PoolSession+":", "-n", a.Kind, "-c", p.Dir(a),
		"-e", "MAD_AGENT_ID="+a.ID, "-P", "-F", "#{pane_id}", cmd)
	if err != nil {
		return err
	}
	return tmux.Tag(id, a.ID)
}

// RestartAgent reruns a in its existing pane, resuming its session.
func RestartAgent(p *state.Project, a *state.Agent, paneID string, kinds []agent.Kind) error {
	cmd := command(p, a, true, kinds)
	return tmux.Run("respawn-pane", "-k", "-t", paneID, "-c", p.Dir(a), "-e", "MAD_AGENT_ID="+a.ID, cmd)
}

// OpenAgent shows agent id in the stage, (re)starting it with its previous
// session if its process is gone.
func OpenAgent(st *state.State, id string, kinds []agent.Kind) error {
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
		err = StartAgent(p, a, true, kinds)
	case pane.Dead:
		err = RestartAgent(p, a, pane.ID, kinds)
	}
	if err != nil {
		return err
	}
	return ShowPane(id, true)
}

// KillAgent stops an agent's process and closes its pane, putting the
// placeholder back first if the agent was on stage.
func KillAgent(id string) error {
	panes, err := tmux.ListPanes()
	if err != nil {
		return err
	}
	pane, ok := tmux.FindPane(panes, id)
	if !ok {
		return nil
	}
	if stage, ok := tmux.Stage(panes); ok && stage.ID == pane.ID {
		if err := ShowPane(tmux.IDPlaceholder, false); err != nil {
			return err
		}
	}
	return tmux.Run("kill-pane", "-t", pane.ID)
}

// SwitchIndex resolves `mad switch` arguments (N, next, prev) against n
// agents, with cur the index of the agent on stage (-1 if none).
func SwitchIndex(arg string, cur, n int) (int, bool) {
	if n == 0 {
		return 0, false
	}
	switch arg {
	case "next":
		return (cur + 1) % n, true
	case "prev":
		if cur < 0 {
			return n - 1, true
		}
		return (cur - 1 + n) % n, true
	}
	k, err := strconv.Atoi(arg)
	if err != nil || k < 1 || k > n {
		return 0, false
	}
	return k - 1, true
}

// Switch shows the agent chosen by arg (see SwitchIndex).
func Switch(arg string) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	agents := st.OrderedAgents()
	cur := -1
	if panes, err := tmux.ListPanes(); err == nil {
		if stage, ok := tmux.Stage(panes); ok {
			for i, a := range agents {
				if a.ID == stage.MadID {
					cur = i
				}
			}
		}
	}
	i, ok := SwitchIndex(arg, cur, len(agents))
	if !ok {
		return nil
	}
	if err := OpenAgent(st, agents[i].ID, agent.Load()); err != nil {
		return err
	}
	_ = poke.Send(poke.Poll) // so the sidebar shows it now, not a poll later
	return nil
}

// WriteConfigs writes the tmux config and the claude hook settings.
func WriteConfigs() error {
	if err := paths.WriteFileAtomic(paths.TmuxConf(), []byte(TmuxConfig())); err != nil {
		return err
	}
	return paths.WriteFileAtomic(paths.ClaudeSettings(), ClaudeSettings(QuotaEnabled()))
}

// TmuxConfig is the config of mad's tmux server.
func TmuxConfig() string {
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
	fmt.Fprintf(&b, "set-hook -g client-resized %s\n", tmuxQuote("run-shell -b "+tmuxQuote(SelfCommand("fit"))))
	// An agent's process ended: tell the sidebar now rather than at its
	// next full poll.
	fmt.Fprintf(&b, "set-hook -g pane-died %s\n", tmuxQuote("run-shell -b "+tmuxQuote(SelfCommand("poke poll"))))
	// Sidebar width: drag the border, or prefix + < / >.
	fmt.Fprintf(&b, "bind -r < resize-pane -t %s -L 2\nbind -r > resize-pane -t %s -R 2\n", tmux.SidebarPane, tmux.SidebarPane)
	toggle := fmt.Sprintf(`if -F "#{==:#{pane_index},0}" "select-pane -t %s" "select-pane -t %s"`, tmux.StagePane, tmux.SidebarPane)
	fmt.Fprintf(&b, "bind -n M-s %s\nbind s %s\n", toggle, toggle)
	// The sidebar knows which agents need you; let it pick the next one.
	fmt.Fprintf(&b, "bind -n M-n run-shell -b %s\n", tmuxQuote(SelfCommand("jump")))
	// The diff view of the agent on stage, and back.
	fmt.Fprintf(&b, "bind -n M-v run-shell -b %s\nbind v run-shell -b %s\n", tmuxQuote(SelfCommand("diff")), tmuxQuote(SelfCommand("diff")))
	run := func(key, arg string) {
		fmt.Fprintf(&b, "bind %s run-shell -b %s\n", key, tmuxQuote(SelfCommand("switch "+arg)))
	}
	run("-n M-j", "next")
	run("-n M-k", "prev")
	run("n", "next")
	run("p", "prev")
	for i := 1; i <= 9; i++ {
		run(fmt.Sprintf("-n M-%d", i), strconv.Itoa(i))
		run(strconv.Itoa(i), strconv.Itoa(i))
	}
	return b.String()
}

// tmuxQuote double-quotes s for a tmux config line.
func tmuxQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(s) + `"`
}

// ClaudeSettings is passed to claude with --settings: hooks that report
// status to `mad hook claude`, and with quota set a status line command
// that records the plan's usage limits (it runs the user's own status
// line afterwards). The user's own settings stay untouched.
func ClaudeSettings(quota bool) []byte {
	hook := []map[string]any{{"hooks": []map[string]any{{
		"type": "command", "command": SelfCommand("hook claude"), "timeout": 5,
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
	if quota {
		settings["statusLine"] = map[string]any{"type": "command", "command": SelfCommand("hook statusline")}
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	return data
}

// QuotaEnabled is the "quota" flag of config.json: whether mad follows
// the plans' usage limits (claude through its status line, codex through
// its rollout) and shows them under the sidebar's header. On by default;
// {"quota": false} turns it off.
func QuotaEnabled() bool {
	var file struct {
		Quota *bool `json:"quota"`
	}
	data, err := os.ReadFile(paths.ConfigFile())
	if err != nil || json.Unmarshal(data, &file) != nil || file.Quota == nil {
		return true
	}
	return *file.Quota
}
