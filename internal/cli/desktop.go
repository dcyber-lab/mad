package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
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
	id := windowID()
	// A window opens on the phone's server when it is up and answers to
	// the phone's token, rather than on a server of its own with a second
	// poller doing the same tmux work. The server stays when the window
	// closes: only the window's browser ends.
	if !*serve && !*noWindow && !flagSet(fs, "addr") {
		if t, ok := phone.ExistingToken(); ok {
			base := fmt.Sprintf("http://127.0.0.1:%d", phone.Port)
			client := &http.Client{Timeout: 3 * time.Second}
			if desktop.Reusable(ctx, client, base, t) {
				fmt.Fprintf(stdio.Err, "mad: window on the phone's server (%s); it keeps running when the window closes\n", base)
				openWindow(ctx, stdio, base+"/#token="+t+"&win="+id, func() bool {
					return desktop.OpenOnServer(ctx, client, base, t, id)
				}, stop)
				<-ctx.Done()
				return nil
			}
		}
	}
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
		openWindow(ctx, stdio, url+"&win="+id, func() bool { return srv.WindowOpen(id) }, stop)
	})
}

// flagSet says whether flag name was given.
func flagSet(fs *flag.FlagSet, name string) bool {
	var set bool
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func windowID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// openWindow opens url in mad's app window and, in the background, ends
// mad (stop) when the window is closed: closing it does not quit the
// browser on a Mac, so the browser on mad's profile is closed too, never
// the person's own. open says whether the page is still there.
func openWindow(ctx context.Context, stdio IO, url string, open func() bool, stop func()) {
	go func() {
		profile := desktop.ProfileDir(paths.StateDir())
		_ = os.MkdirAll(profile, 0o700)
		cmd := desktop.WindowCommand(url, profile)
		own := slices.Contains(cmd.Args, "--user-data-dir="+profile)
		started := time.Now()
		if own {
			go func() {
				desktop.WatchWindow(ctx, open)
				if ctx.Err() == nil {
					desktop.CloseBrowser(profile)
					stop()
				}
			}()
		}
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
		if own {
			fmt.Fprintln(stdio.Err, "mad: serving until the window closes")
		} else {
			fmt.Fprintln(stdio.Err, "mad: serving until Ctrl-C")
		}
	}()
}
