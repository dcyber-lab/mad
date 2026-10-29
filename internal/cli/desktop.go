package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/desktop"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// desktopCmd serves the deck to a window of its own and opens it. The
// server ends with the window, or with Ctrl-C when the window is a tab
// of the user's browser (mad can't tell when that one closes).
func desktopCmd(args []string, stdio IO) error {
	fs := flags("desktop", stdio.Err)
	addr := fs.String("addr", "127.0.0.1:0", "`address` to listen on")
	noWindow := fs.Bool("no-window", false, "only serve; print the URL to open")
	fs.Usage = func() {
		fmt.Fprintln(stdio.Err, "usage: mad desktop [-addr host:port] [-no-window]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return errUsage
	}
	if tmux.InDeck() {
		return fmt.Errorf("run mad desktop outside the deck: its agents would be attached from inside one of them")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := desktop.New(kinds())
	return srv.Serve(ctx, *addr, func(url string) {
		fmt.Fprintln(stdio.Out, url)
		if *noWindow {
			return
		}
		go func() {
			profile := desktop.ProfileDir(paths.StateDir())
			_ = os.MkdirAll(profile, 0o700)
			cmd := desktop.WindowCommand(url, profile)
			started := time.Now()
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(stdio.Err, "mad: no window (%v); open the URL above\n", err)
				return
			}
			// A browser that hands the page to one already running, or
			// xdg-open, returns at once; the window outlives it.
			if time.Since(started) > 3*time.Second {
				stop()
				return
			}
			fmt.Fprintln(stdio.Err, "mad: serving until Ctrl-C")
		}()
	})
}
