package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/dcyber-lab/mad/internal/live"
)

// streamCmd runs the deck's hub of what agents write as they write it
// (package live). The deck keeps it running in a pane of its own.
func streamCmd() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return live.Serve(ctx)
}
