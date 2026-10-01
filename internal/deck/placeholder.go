package deck

import (
	"io"
	"os/signal"
	"syscall"
	"time"
)

// PlaceholderText fills the stage when no agent is shown.
const PlaceholderText = `

   mad — multi-agent deck

   sidebar keys
     ↑/↓ j/k   move            enter   open agent
     n         new agent       w       new agent in a new worktree
     v         diff view       f       finish: push / PR / rebase / merge
     a         add project     t       name the agent
     r         restart/resume  x       kill agent / remove project
     s         show the agent beside the others (up to 4), or close
               its view; on a run, its panel with its agents
     o         new run         c       continue a waiting run
     1-9       open agent N of the project the cursor is in
     q         detach (agents keep running)
     < / >     narrower / wider sidebar (or drag the border)

   anywhere  (Option must act as Alt in Ghostty)
     Alt-s     sidebar ⇄ agent
     Alt-j/k   next / prev agent
     Alt-v     diff view of the agent on stage, and back
     Alt-1..9  open agent N of the project on stage
     Ctrl-] then s / n / p / 1-9 / d   same, without Alt
`

// RunPlaceholder draws the placeholder and blocks forever, ignoring the
// signals a stray Ctrl-C or Ctrl-Z would send.
func RunPlaceholder(w io.Writer) {
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)
	io.WriteString(w, "\x1b[2J\x1b[H\x1b[?25l\x1b[38;5;245m"+PlaceholderText+"\x1b[0m")
	for {
		time.Sleep(time.Hour)
	}
}
