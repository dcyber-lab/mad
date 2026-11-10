package ui

import (
	"os"
	"time"
)

// A sidebar left running over a rebuilt mad keeps the old code: it
// dispatches the queues and rewrites state.json as that version did,
// dropping the fields it does not know. So it leaves when its binary
// changes, and the pane's loop (deck.sidebarCommand) starts the new one.

// binCheckEvery is how often the sidebar looks at its binary.
const binCheckEvery = 5 * time.Second

// binSettle is how long a changed binary must stay unchanged before the
// sidebar leaves for it: an install may still be writing it.
const binSettle = 2 * time.Second

// binStamp is what tells one build of a file from another.
type binStamp struct {
	path string
	mod  time.Time
	size int64
}

func stampOf(path string) (binStamp, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return binStamp{}, false
	}
	return binStamp{path: path, mod: fi.ModTime(), size: fi.Size()}, true
}

// rebuilt reports whether the binary at b.path is another build than b,
// written binSettle ago or more. A missing file (being replaced) is not
// rebuilt yet; nor is anything when b is unknown.
func (b binStamp) rebuilt(now time.Time) bool {
	if b.path == "" {
		return false
	}
	cur, ok := stampOf(b.path)
	if !ok || cur.mod.Equal(b.mod) && cur.size == b.size {
		return false
	}
	return now.Sub(cur.mod) >= binSettle
}
