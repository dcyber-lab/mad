package main

import (
	"fmt"
	"time"
)

// cmdScan prints what project sync sees; handy when a project or session
// doesn't show up.
func cmdScan(args []string) {
	t := time.Now()
	hist := scanHistory()
	fmt.Printf("history: %d projects (%s)\n", len(hist), time.Since(t).Round(time.Millisecond))
	for i, c := range hist {
		if i == 15 {
			fmt.Printf("  … %d more\n", len(hist)-i)
			break
		}
		fmt.Printf("  %-4s %-50s %v\n", age(c.LastUsed), shortPath(c.Path), c.Sources)
	}

	t = time.Now()
	ext := scanExternal([]string{"claude", "codex"}, nil)
	fmt.Printf("\nopen outside the deck: %d (%s)\n", len(ext), time.Since(t).Round(time.Millisecond))
	for _, e := range ext {
		fmt.Printf("  %-7d %-8s %-6s desktop=%-5v %-40s %s\n", e.PID, e.TTY, e.Kind, e.Desktop, shortPath(e.Root), e.SessionID)
	}

	if len(args) > 0 {
		root := resolveRoot(expandPath(args[0]))
		for _, kind := range []string{"claude", "codex"} {
			t = time.Now()
			ss := projectSessions(root, kind)
			fmt.Printf("\n%s sessions of %s: %d (%s)\n", kind, shortPath(root), len(ss), time.Since(t).Round(time.Millisecond))
			for i, s := range ss {
				if i == 10 {
					break
				}
				fmt.Printf("  %-4s %-7s %s  %s\n", age(s.Updated), s.Origin, s.ID[:8], truncate(s.Title, 60))
			}
		}
	}
}
