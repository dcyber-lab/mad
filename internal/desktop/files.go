package desktop

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/state"
)

// Files offered as @ is typed in a message: the agent's directory's, as
// git lists them (ignored ones left out), else as found in it.

const (
	maxFiles   = 50000            // listed of a directory
	filesFound = 50               // offered for what is typed
	filesFor   = 15 * time.Second // a listing is kept
)

type fileEntry struct {
	Path string `json:"path"` // from the directory, "/"-separated
	Dir  bool   `json:"dir,omitempty"`
}

// fileCache keeps each directory's listing a while: one is asked for at
// each key typed.
type fileCache struct {
	mu   sync.Mutex
	dirs map[string]fileListing
}

type fileListing struct {
	at   time.Time
	list []fileEntry
}

func (c *fileCache) get(ctx context.Context, dir string) []fileEntry {
	c.mu.Lock()
	l, ok := c.dirs[dir]
	c.mu.Unlock()
	if ok && time.Since(l.at) < filesFor {
		return l.list
	}
	list := listFiles(ctx, dir)
	c.mu.Lock()
	if c.dirs == nil {
		c.dirs = map[string]fileListing{}
	}
	c.dirs[dir] = fileListing{at: time.Now(), list: list}
	c.mu.Unlock()
	return list
}

// files answers GET /api/files?id=AGENT (or project=PATH, for a new
// chat) &q=TYPED: the files and directories there that match, best
// first.
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	q := r.URL.Query()
	var dir string
	if id := q.Get("id"); id != "" {
		p, a := st.FindAgent(id)
		if a == nil {
			fail(w, http.StatusNotFound, errors.New("no such agent"))
			return
		}
		dir = p.Dir(a)
	} else if p := st.FindProject(q.Get("project")); p != nil {
		dir = p.Path
	} else {
		fail(w, http.StatusNotFound, errors.New("no such project"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gitTimeout)
	defer cancel()
	reply(w, matchFiles(s.fileList.get(ctx, dir), q.Get("q"), filesFound))
}

// listFiles lists dir's files, and the directories they are in: git's
// tracked and untracked ones, not ignored; else what a walk finds,
// leaving out hidden directories and node_modules.
func listFiles(ctx context.Context, dir string) []fileEntry {
	var files []string
	if out, err := gitOut(ctx, dir, "ls-files", "--cached", "--others", "--exclude-standard", "-z"); err == nil {
		for _, f := range bytes.Split(out, []byte{0}) {
			if len(f) > 0 && len(files) < maxFiles {
				files = append(files, string(f))
			}
		}
	} else {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if len(files) >= maxFiles || ctx.Err() != nil {
				return filepath.SkipAll
			}
			if err != nil {
				return nil // unreadable: left out
			}
			if p == dir {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if rel, err := filepath.Rel(dir, p); err == nil {
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	seen := map[string]bool{}
	var out []fileEntry
	for _, f := range files {
		for d := path.Dir(f); d != "." && !seen[d]; d = path.Dir(d) {
			seen[d] = true
			out = append(out, fileEntry{Path: d + "/", Dir: true})
		}
		out = append(out, fileEntry{Path: f})
	}
	return out
}

// matchFiles picks up to n of list for what was typed, q: the name being
// q first, then starting so, having it in it, the path having it, the
// path having its letters in order; shallower and shorter first within
// each. With nothing typed, the top of the tree: directories, then files,
// by name.
func matchFiles(list []fileEntry, q string, n int) []fileEntry {
	q = strings.ToLower(strings.TrimSpace(q))
	type hit struct {
		e     fileEntry
		rank  int
		depth int
	}
	var hits []hit
	for _, e := range list {
		p := strings.ToLower(strings.TrimSuffix(e.Path, "/"))
		name := p[strings.LastIndex(p, "/")+1:]
		depth := strings.Count(p, "/")
		r := -1
		switch {
		case q == "":
			if depth == 0 {
				r = 0
			}
		case name == q:
			r = 0
		case strings.HasPrefix(name, q):
			r = 1
		case strings.Contains(name, q):
			r = 2
		case strings.Contains(p, q):
			r = 3
		case inOrder(p, q):
			r = 4
		}
		if r >= 0 {
			hits = append(hits, hit{e, r, depth})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if q == "" {
			if a.e.Dir != b.e.Dir {
				return a.e.Dir
			}
			return strings.ToLower(a.e.Path) < strings.ToLower(b.e.Path)
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.depth != b.depth {
			return a.depth < b.depth
		}
		if len(a.e.Path) != len(b.e.Path) {
			return len(a.e.Path) < len(b.e.Path)
		}
		return a.e.Path < b.e.Path
	})
	out := []fileEntry{}
	for i := 0; i < len(hits) && i < n; i++ {
		out = append(out, hits[i].e)
	}
	return out
}

// inOrder reports whether s has q's letters, in order.
func inOrder(s, q string) bool {
	for _, c := range q {
		i := strings.IndexRune(s, c)
		if i < 0 {
			return false
		}
		s = s[i+len(string(c)):]
	}
	return true
}
