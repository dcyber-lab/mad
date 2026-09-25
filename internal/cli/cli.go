// Package cli implements the mad command line.
package cli

import (
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
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
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
                      show agent N (1-based, sidebar order) in the stage
  mad jump            show the next agent that is waiting or done
  mad scan [path]     show what sync sees: history, open sessions, and
                      the sessions of one project
  mad kill-server     stop the deck and every agent in it
  mad version         print the version

internal:
  mad sidebar         the sidebar TUI (runs inside tmux)
  mad placeholder     empty stage filler
  mad fit             restore the saved sidebar width
  mad hook claude|codex
                      status hooks called by the agents
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
		err = open()
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
	case "poke":
		err = poke.Send(strings.Join(args, " "))
	case "scan":
		scan(args, stdio.Out)
	case "fit":
		err = deck.FitSidebar()
	case "kill-server":
		err = tmux.Run("kill-server")
	case "sidebar":
		err = ui.Run()
	case "placeholder":
		deck.RunPlaceholder(stdio.Out)
	case "hook":
		hook(args, stdio.In, time.Now())
	case "-h", "--help", "help":
		fmt.Fprint(stdio.Out, Usage)
	case "-v", "--version", "version":
		fmt.Fprintln(stdio.Out, "mad", version())
	default:
		fmt.Fprintf(stdio.Err, "mad: unknown command %q\n\n%s", cmd, Usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stdio.Err, "mad:", err)
		return 1
	}
	return 0
}

// open builds the deck if needed and attaches this terminal to it.
func open() error {
	if tmux.InDeck() {
		return tmux.Run("select-pane", "-t", tmux.SidebarPane)
	}
	if err := deck.WriteConfigs(); err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	if root, ok := paths.ProjectRoot(cwd); ok {
		st, err := state.Load()
		if err != nil {
			return err
		}
		if _, added := st.AddProject(root); added {
			if err := st.Save(); err != nil {
				return err
			}
		}
	}

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 40 || h < 10 {
		w, h = 200, 50
	}
	if tmux.Run("list-sessions") == nil {
		_ = tmux.Run("source-file", paths.TmuxConf())
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
	st, err := state.Load()
	if err != nil {
		return err
	}
	if _, added := st.AddProject(root); !added {
		fmt.Fprintln(out, "already added:", paths.Short(root))
		return nil
	}
	fmt.Fprintln(out, "added:", paths.Short(root))
	return st.Save()
}

// hook records a status report from an agent. It never fails: a broken
// hook must not disturb the agent that called it.
func hook(args []string, in io.Reader, now time.Time) {
	id := os.Getenv("MAD_AGENT_ID")
	if id == "" || len(args) == 0 {
		return
	}
	var h *status.Hook
	switch args[0] {
	case "claude":
		h = status.ParseClaude(in)
	case "codex":
		if len(args) > 1 {
			h = status.ParseCodex(args[len(args)-1])
		}
	}
	if h != nil && status.WriteHook(id, h, now) == nil {
		_ = poke.Send(poke.Hook + " " + id) // the sidebar shows it now
	}
}
