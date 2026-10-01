// Package cli implements the mad command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	madrun "github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/ui"
)

const Usage = `mad - multi-agent deck

usage:
  mad                 open the deck (adds the current git project)
  mad add [path]      add a project (default: current directory)
  mad switch N|next|prev
                      show agent N of the project on stage, or the
                      next / previous agent
  mad jump            show the next agent that is waiting or done
  mad diff            toggle the diff view for the agent on stage
  mad scan [path]     show what sync sees: history, open sessions, and
                      the sessions of one project
  mad ls              list the agents: id, name, kind, status, directory
  mad spawn [-k kind] [-n name] [-m model] [-C dir] [-w branch] [-- flags]
                      start an agent off stage and print its id
  mad send [-f file] [-w] [-t duration] AGENT [TEXT...|-]
                      type a message into an agent (by id or name);
                      -w waits for its reply and prints it
  mad wait [-t duration] AGENT
                      wait until the agent's turn ends; print its reply
  mad run new|start|ls|continue|cancel
                      runs: a task handed through agents in roles
                      (design, implement, review); "mad run" for more
  mad skill install   put the skill that designs flows with you
                      (mad-flow) where claude finds it
  mad kill-server     stop the deck and every agent in it
  mad version         print the version

internal:
  mad sidebar         the sidebar TUI (runs inside tmux)
  mad placeholder     empty stage filler
  mad fit             restore the saved sidebar width
  mad hook KIND ...    status hooks called by the agents (claude, codex);
                      claude's status line is "hook claude statusline"
  mad poke CMD        pass an event to the sidebar (tmux hooks use it)
`

// Version is set by the release build (-ldflags "-X .../cli.Version=v1.2.3").
// Binaries built with `go install module@version` report the module version
// instead; anything else reports "devel".
var Version string

// version returns the version to print.
func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}

// IO is where a command reads and writes.
type IO struct {
	In       io.Reader
	Out, Err io.Writer
}

// Run executes a mad command line (without the program name) and returns
// the process exit code.
func Run(args []string, stdio IO) int {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "", "open":
		err = open(stdio.Err)
	case "add":
		err = add(args, stdio.Out)
	case "switch":
		if len(args) != 1 {
			err = fmt.Errorf("usage: mad switch N|next|prev")
		} else {
			err = deck.Switch(args[0])
		}
	case "jump":
		err = poke.Send(poke.Jump)
	case "diff":
		err = poke.Send(poke.Diff)
	case "poke":
		err = poke.Send(strings.Join(args, " "))
	case "scan":
		scan(args, stdio.Out)
	case "ls":
		err = ls(stdio.Out)
	case "spawn":
		err = spawn(args, stdio)
	case "send":
		err = send(args, stdio)
	case "wait":
		err = wait(args, stdio)
	case "run":
		err = runCmd(args, stdio)
	case "skill":
		err = skillCmd(args, stdio)
	case "fit":
		err = deck.FitSidebar()
	case "kill-server":
		err = tmux.Run("kill-server")
	case "sidebar":
		err = ui.Run()
	case "placeholder":
		deck.RunPlaceholder(stdio.Out)
	case "hook":
		hook(args, stdio.In, stdio.Out, time.Now())
	case "-h", "--help", "help":
		fmt.Fprint(stdio.Out, Usage)
	case "-v", "--version", "version":
		fmt.Fprintln(stdio.Out, "mad", version())
	default:
		fmt.Fprintf(stdio.Err, "mad: unknown command %q\n\n%s", cmd, Usage)
		return 2
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		return 2
	}
	fmt.Fprintln(stdio.Err, "mad:", err)
	if errors.Is(err, drive.ErrTimeout) {
		return 124 // as timeout(1)
	}
	return 1
}

// versionNote is what to say before attaching when tmux was upgraded under
// a running deck. Commands still reach a server of another version, but
// attaching may not: a 3.7c client gets "open terminal failed: not a
// terminal" from a 3.4 server.
func versionNote(server, client string) string {
	if server == "" || client == "" || server == client {
		return ""
	}
	return fmt.Sprintf(`mad: the deck runs on tmux %s, the installed tmux is %s.
     If the deck does not open, run "mad kill-server" and then "mad".
     That stops every agent; press enter on each one to resume it.
`, server, client)
}

// open builds the deck if needed and attaches this terminal to it.
func open(errOut io.Writer) error {
	if tmux.InDeck() {
		return tmux.Run("select-pane", "-t", tmux.SidebarPane)
	}
	if err := tmux.CheckVersion(); err != nil {
		return err
	}
	if err := deck.WriteConfigs(); err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	if root, ok := paths.ProjectRoot(cwd); ok {
		if err := state.Update(func(st *state.State) error {
			st.AddProject(root)
			return nil
		}); err != nil {
			return err
		}
	}

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 40 || h < 10 {
		w, h = 200, 50
	}
	if tmux.Run("list-sessions") == nil {
		_ = tmux.Run("source-file", paths.TmuxConf())
		fmt.Fprint(errOut, versionNote(tmux.Versions()))
	}
	if err := deck.EnsureLayout(cwd, w, h); err != nil {
		return err
	}

	bin, err := exec.LookPath("tmux")
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
	argv := append([]string{"tmux"}, tmux.Args("attach-session", "-t", tmux.MainSession)...)
	return syscall.Exec(bin, argv, env)
}

func add(args []string, out io.Writer) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs := paths.Expand(dir)
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	root, _ := paths.ProjectRoot(abs)
	added := false
	if err := state.Update(func(st *state.State) error {
		_, added = st.AddProject(root)
		return nil
	}); err != nil {
		return err
	}
	if !added {
		fmt.Fprintln(out, "already added:", paths.Short(root))
		return nil
	}
	fmt.Fprintln(out, "added:", paths.Short(root))
	return nil
}

// noteTyped passes on what you typed into an agent of a run, rather than
// its runner sent, as a note the run's other roles hear of.
func noteTyped(id string, prompts []string) {
	var typed []string
	for _, p := range prompts {
		if !madrun.FromRunner(p) && strings.TrimSpace(p) != "" {
			typed = append(typed, p)
		}
	}
	if len(typed) == 0 {
		return
	}
	st, err := state.Load()
	if err != nil {
		return
	}
	p, a := st.FindAgent(id)
	if a == nil || !p.InRun(a) {
		return
	}
	_, r := st.FindRun(a.Run)
	for _, text := range typed {
		_ = madrun.AddNote(r, a.Role, text)
	}
}

// outsideRun says why agent id may not make tool call t: the agent plays
// a role in a run, and the call reaches out of the run's worktree.
func outsideRun(id string, t *discover.Tool) string {
	st, err := state.Load()
	if err != nil {
		return ""
	}
	p, a := st.FindAgent(id)
	if a == nil || !p.InRun(a) {
		return ""
	}
	_, r := st.FindRun(a.Run)
	return madrun.Outside(r.Dir, p.Path, t)
}

// hook takes a report from an agent to its kind's provider, and records
// the status and usage limits it carries. It never fails: a broken hook
// must not disturb the agent that called it.
func hook(args []string, in io.Reader, out io.Writer, now time.Time) {
	if len(args) == 0 {
		return
	}
	if args[0] == "statusline" {
		// claude settings from before `mad hook claude statusline`, still
		// held by a running claude.
		args = append([]string{"claude"}, args...)
	}
	p := discover.Lookup(args[0])
	if p == nil {
		return
	}
	r := p.Hook(args[1:], in, out, now)
	// Only an agent mad started has a status to keep.
	id := os.Getenv("MAD_AGENT_ID")
	if id != "" && r.Turn != nil {
		// Before the status: `mad wait` takes the idle report as the
		// turn's end and reads its reply then.
		_ = status.WriteTurn(id, r.Turn, now)
	}
	if id != "" && r.Hook != nil && status.WriteHook(id, r.Hook, now) == nil {
		_ = poke.Send(poke.Hook + " " + id) // the sidebar shows it now
	}
	if id != "" && len(r.Prompts) > 0 {
		noteTyped(id, r.Prompts)
	}
	if id != "" && r.Tool != nil && r.Tool.Deny != nil && (r.Tool.Path != "" || r.Tool.Command != "") {
		if why := outsideRun(id, r.Tool); why != "" {
			r.Tool.Deny(why)
		}
	}
	if r.Quota != nil && status.WriteQuota(p.Kind(), *r.Quota, now) == nil {
		_ = poke.Send(poke.Poll)
	}
}
