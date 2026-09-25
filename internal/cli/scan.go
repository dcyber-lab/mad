package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// scan prints what project sync sees; handy when a project or session
// doesn't show up.
func scan(args []string, out io.Writer) {
	t := time.Now()
	hist := discover.ScanHistory()
	fmt.Fprintf(out, "history: %d projects (%s)\n", len(hist), time.Since(t).Round(time.Millisecond))
	for i, c := range hist {
		if i == 15 {
			fmt.Fprintf(out, "  … %d more\n", len(hist)-i)
			break
		}
		fmt.Fprintf(out, "  %-4s %-50s %v\n", textutil.Age(c.LastUsed), paths.Short(c.Path), c.Sources)
	}

	t = time.Now()
	ext := discover.ScanExternal(nil)
	fmt.Fprintf(out, "\nopen outside the deck: %d (%s)\n", len(ext), time.Since(t).Round(time.Millisecond))
	for _, e := range ext {
		fmt.Fprintf(out, "  %-7d %-8s %-6s desktop=%-5v %-40s %s\n", e.PID, e.TTY, e.Kind, e.Desktop, paths.Short(e.Root), e.SessionID)
	}

	if len(args) == 0 {
		return
	}
	root := discover.ResolveRoot(paths.Expand(args[0]))
	for _, p := range discover.Providers() {
		kind := p.Kind()
		t = time.Now()
		ss := discover.ProjectSessions(root, kind)
		fmt.Fprintf(out, "\n%s sessions of %s: %d (%s)\n", kind, paths.Short(root), len(ss), time.Since(t).Round(time.Millisecond))
		for i, s := range ss {
			if i == 10 {
				break
			}
			id := s.ID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Fprintf(out, "  %-4s %-7s %-8s  %s\n", textutil.Age(s.Updated), s.Origin, id, textutil.Truncate(s.Title, 60))
		}
	}
}
