package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/host"
)

// hostCmd runs an agent headless in its pane, for the desktop window to
// draw (window mode, package host). The deck starts it.
func hostCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: mad host AGENT_ID")
	}
	kinds, _ := agent.Load()
	if kinds == nil {
		kinds = agent.Builtin()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return host.Run(ctx, args[0], kinds)
}
