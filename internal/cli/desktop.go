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
	"github.com/dcyber-lab/mad/internal/phone"
	"github.com/dcyber-lab/mad/internal/tmux"
)

// desktopCmd serves the deck to a window of its own and opens it. The
// server ends with the window, or with Ctrl-C when the window is a tab
// of the user's browser (mad can't tell when that one closes).
func desktopCmd(args []string, stdio IO) error {
	fs := flags("desktop", stdio.Err)
	addr := fs.String("addr", "127.0.0.1:0", "`address` to listen on")
	noWindow := fs.Bool("no-window", false, "only serve; print the URL to open")
	serve := fs.Bool("serve", false, "serve the phone (mad phone): its token, no window")
	origin := fs.String("origin", "", "`host:port` the page is also opened from (mad phone)")
	fs.Usage = func() {
		fmt.Fprintln(stdio.Err, "usage: mad desktop [-addr host:port] [-no-window]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return errUsage
	}
	if tmux.InDeck() && !*serve {
		return fmt.Errorf("run mad desktop outside the deck: the window is not one of its panes")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	srv := desktop.New(kinds())
	if *origin != "" {
		srv.Origins = []string{*origin}
	}
	if *serve {
		// The phone's: its token kept, as the phone keeps it.
		t, err := phone.Token()
		if err != nil {
			return err
		}
		srv.Token = t
		*noWindow = true
	}
	return srv.Serve(ctx, *addr, func(url string) {
		if *serve {
			fmt.Fprintln(stdio.Err, "mad: serving the phone on", *addr)
			return
		}
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
