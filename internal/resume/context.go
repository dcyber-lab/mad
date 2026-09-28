// Package resume keeps what you need to pick a piece of work back up: your
// bookmark (what you meant to look at next), the decisions you pinned, and
// the facts collected meanwhile (workspace snapshots, commands and their
// results from the transcript). A brief is built from those on demand; it
// is a view, never a source, and rebuilding it rewrites nothing you wrote.
//
// What was said and what happened are kept apart: an agent saying tests
// pass is a claim, a command exiting 0 is a record, and a record on an
// older snapshot of the workspace says nothing about the current one.
package resume

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Anchor is your bookmark: the thinking not finished yet.
type Anchor struct {
	Text   string    `json:"text"`
	Source string    `json:"source"` // "typed" or "your message"
	At     time.Time `json:"at"`
}

// Decision states.
const (
	Candidate  = "candidate"
	Confirmed  = "confirmed"
	Superseded = "superseded"
	Revoked    = "revoked"
)

// Decision is something you settled, with why and what it covers, so it
// is neither reopened nor stretched past its scope.
type Decision struct {
	ID         string    `json:"id"` // D1, D2… within the context
	Choice     string    `json:"choice"`
	Why        string    `json:"why,omitempty"`
	Rejected   string    `json:"rejected,omitempty"`
	Scope      string    `json:"scope,omitempty"`
	State      string    `json:"state"`
	Source     string    `json:"source"` // where it came from, quoted
	At         time.Time `json:"at"`
	Supersedes string    `json:"supersedes,omitempty"`
}

// Checkpoint is a point you said you were caught up to: the brief you
// read, as of its cutoff. Opening the agent never moves it.
type Checkpoint struct {
	At      time.Time `json:"at"`   // the brief's cutoff
	Snap    int       `json:"snap"` // workspace snapshot then, 0 unknown
	Session string    `json:"session,omitempty"`
}

// Snap is the workspace as observed at one time: HEAD plus a fingerprint
// of each file that differs from it. Fingerprints tell that a file
// changed, not what it was.
type Snap struct {
	N     int               `json:"n"`
	FP    string            `json:"fp"`
	Head  string            `json:"head"`
	Files map[string]string `json:"files"` // path → content hash, "-" deleted
	At    time.Time         `json:"at"`
	// Moving: the workspace changed while it was read; a guess, not a
	// consistent snapshot.
	Moving bool `json:"moving,omitempty"`
}

// Context is one agent's resume point (one session a context, for now).
type Context struct {
	ID       string      `json:"id"`
	AgentID  string      `json:"agent_id"`
	Anchor   *Anchor     `json:"anchor,omitempty"`
	Older    []Anchor    `json:"older_anchors,omitempty"` // replaced bookmarks, kept
	Decs     []Decision  `json:"decisions,omitempty"`
	CaughtUp *Checkpoint `json:"caught_up,omitempty"`
	// Left is when you last moved away from the agent, with the workspace
	// then: the fallback baseline until you catch up explicitly.
	Left     *Checkpoint `json:"left,omitempty"`
	Visited  time.Time   `json:"last_visited,omitzero"`
	Snaps    []Snap      `json:"snaps,omitempty"`
	NextSnap int         `json:"next_snap"`
}

const keepSnaps = 60

func dir() string { return filepath.Join(paths.StateDir(), "contexts") }

func file(agentID string) string { return filepath.Join(dir(), agentID+".json") }

// Load returns agent's context, or nil when it has none yet.
func Load(agentID string) *Context {
	var c Context
	if err := paths.ReadJSON(file(agentID), &c); err != nil || c.AgentID == "" {
		return nil
	}
	return &c
}

// LoadAll reads every context, by agent id.
func LoadAll() map[string]*Context {
	out := map[string]*Context{}
	ents, _ := os.ReadDir(dir())
	for _, e := range ents {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			if c := Load(id); c != nil {
				out[id] = c
			}
		}
	}
	return out
}

func New(agentID string) *Context {
	return &Context{ID: "ctx-" + agentID[:min(8, len(agentID))], AgentID: agentID}
}

func (c *Context) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(file(c.AgentID), data)
}

func Remove(agentID string) { os.Remove(file(agentID)) }

// SetAnchor replaces the bookmark, keeping the old one.
func (c *Context) SetAnchor(text, source string, now time.Time) {
	if c.Anchor != nil {
		c.Older = append(c.Older, *c.Anchor)
	}
	c.Anchor = &Anchor{Text: text, Source: source, At: now}
	if text == "" {
		c.Anchor = nil
	}
}

// AddDecision records a decision you made, confirmed: you typed it.
func (c *Context) AddDecision(d Decision, now time.Time) Decision {
	d.ID = "D" + itoa(len(c.Decs)+1)
	d.State, d.At = Confirmed, now
	c.Decs = append(c.Decs, d)
	return d
}

// Active are the decisions still in force.
func (c *Context) Active() []Decision {
	var out []Decision
	for _, d := range c.Decs {
		if d.State == Confirmed {
			out = append(out, d)
		}
	}
	return out
}

// Observe adds s unless the workspace looks the same as last time; it
// reports whether s is new.
func (c *Context) Observe(s Snap) bool {
	if n := len(c.Snaps); n > 0 && c.Snaps[n-1].FP == s.FP {
		return false
	}
	c.NextSnap++
	s.N = c.NextSnap
	c.Snaps = append(c.Snaps, s)
	if len(c.Snaps) > keepSnaps {
		c.Snaps = c.Snaps[len(c.Snaps)-keepSnaps:]
	}
	return true
}

// Current is the latest snapshot, if any.
func (c *Context) Current() (Snap, bool) {
	if len(c.Snaps) == 0 {
		return Snap{}, false
	}
	return c.Snaps[len(c.Snaps)-1], true
}

// SnapAt is the snapshot in force at t: the last one taken by then.
func (c *Context) SnapAt(t time.Time) (Snap, bool) {
	var out Snap
	ok := false
	for _, s := range c.Snaps {
		if s.At.After(t) {
			break
		}
		out, ok = s, true
	}
	return out, ok
}

func (c *Context) snap(n int) (Snap, bool) {
	for _, s := range c.Snaps {
		if s.N == n {
			return s, true
		}
	}
	return Snap{}, false
}

// ---- workspace ----

const maxHashed = 8 << 20

// Take reads dir's workspace without touching it: no stash, no index
// writes. It is read twice around the hashing; a difference marks the
// snapshot as taken while the workspace moved.
func Take(dir string, now time.Time) (Snap, error) {
	before, err := statusZ(dir)
	if err != nil {
		return Snap{}, err
	}
	head, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	s := Snap{Head: strings.TrimSpace(string(head)), Files: map[string]string{}, At: now}
	for _, p := range before {
		s.Files[p] = hashFile(filepath.Join(dir, p))
	}
	after, _ := statusZ(dir)
	s.Moving = strings.Join(before, "\x00") != strings.Join(after, "\x00")
	keys := make([]string, 0, len(s.Files))
	for k := range s.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	io.WriteString(h, s.Head)
	for _, k := range keys {
		io.WriteString(h, "\x00"+k+"\x00"+s.Files[k])
	}
	s.FP = hex.EncodeToString(h.Sum(nil))[:12]
	return s, nil
}

// statusZ lists the paths that differ from HEAD, untracked ones included.
func statusZ(dir string) ([]string, error) {
	out, err := exec.Command("git", "--no-optional-locks", "-C", dir, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		if e[0] == 'R' || e[0] == 'C' {
			i++ // the rename's source follows
		}
		if p := e[3:]; !strings.HasPrefix(p, ".claude/worktrees/") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func hashFile(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return "-"
	}
	if fi.IsDir() || fi.Size() > maxHashed {
		return "size:" + itoa(int(fi.Size())) + "@" + fi.ModTime().UTC().Format(time.RFC3339Nano)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "?"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// Changed lists the files that differ between two snapshots of dir: files
// whose fingerprints differ, plus what commits between the two HEADs
// touched.
func Changed(dir string, a, b Snap) []string {
	set := map[string]bool{}
	for p, h := range a.Files {
		if b.Files[p] != h {
			set[p] = true
		}
	}
	for p, h := range b.Files {
		if a.Files[p] != h {
			set[p] = true
		}
	}
	if a.Head != b.Head && a.Head != "" && b.Head != "" {
		out, _ := exec.Command("git", "-C", dir, "diff", "--name-only", a.Head, b.Head).Output()
		for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if p != "" {
				// A file committed unchanged since a is in both Files maps
				// as clean; git's list is the committed part.
				set[p] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
