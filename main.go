// mad (multi-agent deck): a project-first manager for coding-agent TUIs
// (claude, codex, pi, ...) backed by a private tmux server.
//
// Layout: tmux session "main" holds one window with a persistent sidebar
// pane (left) and a stage pane (right). Every agent runs in its own window
// inside the hidden "_pool" session; switching agents swaps the agent's pane
// into the stage, so the sidebar never goes away.
package main

import (
	"fmt"
	"os"
)

const usageText = `mad - multi-agent deck

usage:
  mad                 open the deck (adds the current git project)
  mad add [path]      add a project (default: current directory)
  mad switch N|next|prev
                      show agent N (1-based, sidebar order) in the stage
  mad kill-server     stop the deck and every agent in it
  mad scan [path]     show what sync sees: history, open sessions, and
                      the sessions of one project

internal:
  mad sidebar         the sidebar TUI (runs inside tmux)
  mad placeholder     empty stage filler
  mad hook claude|codex
                      status hooks called by the agents
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	var err error
	switch cmd {
	case "", "open":
		err = cmdOpen()
	case "add":
		err = cmdAdd(args)
	case "switch":
		err = cmdSwitch(args)
	case "scan":
		cmdScan(args)
	case "fit":
		err = fitSidebar()
	case "kill-server":
		err = tmuxRun("kill-server")
	case "sidebar":
		err = runSidebar()
	case "placeholder":
		runPlaceholder()
	case "hook":
		cmdHook(args)
	case "-h", "--help", "help":
		fmt.Print(usageText)
	default:
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mad:", err)
		os.Exit(1)
	}
}
