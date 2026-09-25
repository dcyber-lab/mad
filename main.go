// mad (multi-agent deck) runs coding-agent TUIs — Claude Code, Codex, pi,
// or a plain shell — side by side in one terminal, organized by project.
//
// A private tmux server hosts the agents: the "main" session shows a
// persistent sidebar next to a stage, and every agent lives in its own
// window of the hidden "_pool" session. Showing an agent swaps its pane
// into the stage, so the sidebar never goes away.
package main

import (
	"os"

	"github.com/dcyber-lab/mad/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}))
}
