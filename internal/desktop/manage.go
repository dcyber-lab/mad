package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/host"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// The window manages the deck the way the sidebar does, so the terminal
// is not needed: it adds and removes projects, starts, renames, stops and
// removes agents, continues past sessions, and shows what an agent changed.

// Replaceable in tests.
var (
	scanHistory     = discover.ScanHistory
	projectSessions = discover.ProjectSessions
)

const (
	maxSuggest = 20
	maxHistory = 40
	maxPatch   = 2 << 20
	gitTimeout = 30 * time.Second
	// historyFresh: scanning every session file is slow, so it is done
	// again only this long after the last time.
	historyFresh = 30 * time.Second
)

// historyCache holds the projects found in session history.
type historyCache struct {
	mu   sync.Mutex
	list []discover.Candidate
	at   time.Time
}

// get returns the projects of session history, scanned unless that was
// done lately.
func (c *historyCache) get() []discover.Candidate {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || time.Since(c.at) > historyFresh {
		c.list, c.at = scanHistory(), time.Now()
	}
	return c.list
}

// reply writes v as the JSON answer.
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// fail answers with err and code.
func fail(w http.ResponseWriter, code int, err error) {
	http.Error(w, err.Error(), code)
}

// decode reads the JSON body into v, answering 400 when it is not.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMessage)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

// changed tells the sidebar that the deck's state changed.
func changed() { _ = poke.Send(poke.Poll) }

type suggestion struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Short string `json:"short"`
	Used  string `json:"used"`
	Added bool   `json:"added"`
}

// isPathQuery: q is a place to look in, not a name to look for.
func isPathQuery(q string) bool {
	return strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, ".")
}

// suggest lists directories to add as a project: those that complete the
// path in q, or the projects of session history that q matches, most
// recent first.
func (s *Server) suggest(w http.ResponseWriter, r *http.Request) {
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var out []suggestion
	add := func(path string, used time.Time) {
		sg := suggestion{Path: path, Name: filepath.Base(path), Short: paths.Short(path), Added: st.FindProject(path) != nil}
		if !used.IsZero() {
			sg.Used = used.Format(time.RFC3339)
		}
		out = append(out, sg)
	}
	if isPathQuery(q) {
		for _, dir := range discover.CompleteDir(q) {
			add(dir, time.Time{})
		}
	} else {
		type hit struct {
			c     discover.Candidate
			score int
		}
		var hits []hit
		for _, c := range s.hist.get() {
			if ok, score := discover.FuzzyMatch(q, filepath.Base(c.Path)); ok {
				hits = append(hits, hit{c, score})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score < hits[j].score
			}
			return hits[i].c.LastUsed.After(hits[j].c.LastUsed)
		})
		for _, h := range hits {
			add(h.c.Path, h.c.LastUsed)
		}
	}
	if len(out) > maxSuggest {
		out = out[:maxSuggest]
	}
	if out == nil {
		out = []suggestion{}
	}
	reply(w, out)
}

// projectAdd adds the project the directory in the body belongs to.
func (s *Server) projectAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	if !decode(w, r, &req) {
		return
	}
	dir := paths.Expand(req.Path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		fail(w, http.StatusBadRequest, errors.New("not a directory: "+paths.Short(dir)))
		return
	}
	root, _ := paths.ProjectRoot(dir)
	var p *state.Project
	var added bool
	err := state.Update(func(st *state.State) error {
		p, added = st.AddProject(root)
		return nil
	})
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	changed()
	reply(w, map[string]any{"name": p.Name, "path": p.Path, "added": added})
}

// projectRemove removes the project at path, unless agents are left in it.
func (s *Server) projectRemove(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	err := state.Update(func(st *state.State) error {
		p := st.FindProject(path)
		switch {
		case p == nil:
			return errNotFound
		case len(p.Agents) > 0:
			return errHasAgents
		}
		st.RemoveProject(p)
		return nil
	})
	finish(w, err)
}

var (
	errNotFound  = errors.New("not found")
	errHasAgents = errors.New("remove its agents first")
)

// finish answers a change of state: 204, or why not.
func finish(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		fail(w, http.StatusNotFound, err)
	case errors.Is(err, errHasAgents):
		fail(w, http.StatusConflict, err)
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
	default:
		changed()
		w.WriteHeader(http.StatusNoContent)
	}
}

// agentAdd creates an agent in a project: new, or continuing a past
// session. One that runs headless starts when it has its first message,
// a shell at once.
func (s *Server) agentAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Project, Kind, Text, Model, Session, Cwd string }
	if !decode(w, r, &req) {
		return
	}
	if !s.knows(req.Kind) {
		fail(w, http.StatusBadRequest, errors.New("unknown kind "+req.Kind))
		return
	}
	a := &state.Agent{ID: state.NewUUID(), Kind: req.Kind, SessionID: req.Session, CreatedAt: time.Now()}
	if req.Model != "" {
		a.Args = []string{"--model", req.Model}
	}
	if deck.Headless[a.Kind] {
		a.Mode = state.ModeWindow
	}
	var p *state.Project
	err := state.Update(func(st *state.State) error {
		if p = st.FindProject(req.Project); p == nil {
			return errNotFound
		}
		if req.Cwd != "" && req.Cwd != p.Path {
			if fi, err := os.Stat(req.Cwd); err == nil && fi.IsDir() {
				a.Dir = req.Cwd // e.g. the worktree the session ran in
			}
		}
		p.Agents = append(p.Agents, a)
		return nil
	})
	if errors.Is(err, errNotFound) {
		fail(w, http.StatusBadRequest, errors.New("no such project"))
		return
	} else if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	changed()
	out := map[string]string{"id": a.ID}
	if a.Mode == "" {
		err = drive.EnsureDeck(p.Path)
		if err == nil {
			err = deck.StartAgent(p, a, a.SessionID != "", s.kinds)
		}
	} else if req.Text != "" {
		err = s.ensureHost(r.Context(), a.ID)
		if err == nil {
			err = host.Do(a.ID, host.Command{Type: "send", Text: req.Text})
		}
	}
	if err != nil {
		out["error"] = err.Error()
	}
	reply(w, out)
}

// knows reports whether kind is one of the kinds an agent can be.
func (s *Server) knows(kind string) bool {
	for _, k := range s.kinds {
		if k.Name == kind {
			return true
		}
	}
	return false
}

// agentRename gives the agent the name in the body; none clears it.
func (s *Server) agentRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !decode(w, r, &req) {
		return
	}
	id := r.URL.Query().Get("id")
	finish(w, state.Update(func(st *state.State) error {
		_, a := st.FindAgent(id)
		if a == nil {
			return errNotFound
		}
		a.Name = strings.TrimSpace(req.Name)
		return nil
	}))
}

// agentRemove removes the agent and ends its process.
func (s *Server) agentRemove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	err := state.Update(func(st *state.State) error {
		if _, a := st.FindAgent(id); a == nil {
			return errNotFound
		}
		st.RemoveAgent(id)
		return nil
	})
	if err == nil {
		status.RemoveHook(id)
		queue.Forget(id)
		err = deck.KillAgent(id)
	}
	finish(w, err)
}

// agentStop ends the agent's process; it resumes when it is sent a
// message. One with a command running under it is left as it is.
func (s *Server) agentStop(w http.ResponseWriter, r *http.Request) {
	var busy *deck.BusyError
	_, err := deck.SleepAgent(r.URL.Query().Get("id"), false)
	if errors.As(err, &busy) {
		fail(w, http.StatusConflict, err)
		return
	}
	finish(w, err)
}

// openTerminal opens the user's terminal app running command. It is
// replaced in tests.
var openTerminal = func(command string) error {
	switch runtime.GOOS {
	case "darwin":
		return openMacTerminal(command)
	default:
		return openLinuxTerminal(command)
	}
}

// exists reports whether path does.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func openMacTerminal(command string) error {
	switch {
	case exists("/Applications/Ghostty.app"):
		return exec.Command("open", "-na", "Ghostty.app", "--args", "-e", "/bin/sh", "-lc", command).Run()
	case exists("/Applications/iTerm.app"):
		return exec.Command("osascript", "-e",
			`tell application "iTerm" to create window with default profile command `+appleString(command)).Run()
	}
	return exec.Command("osascript",
		"-e", `tell application "Terminal" to do script `+appleString(command),
		"-e", `tell application "Terminal" to activate`).Run()
}

func openLinuxTerminal(command string) error {
	if p, err := lookPath("x-terminal-emulator"); err == nil {
		return exec.Command(p, "-e", "/bin/sh", "-lc", command).Start()
	}
	if p, err := lookPath("gnome-terminal"); err == nil {
		return exec.Command(p, "--", "/bin/sh", "-lc", command).Start()
	}
	return errors.New("no terminal app found")
}

// deckEnv is what a shell of the terminal app needs to find this deck,
// not the usual one: a terminal app starts its shells afresh.
func deckEnv() string {
	var b strings.Builder
	for _, k := range []string{"MAD_SOCKET", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		if v, ok := os.LookupEnv(k); ok {
			b.WriteString(k + "=" + paths.ShellQuote(v) + " ")
		}
	}
	return b.String()
}

// appleString quotes s as an AppleScript string.
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// terminal shows the agent in the deck and opens the deck in the user's
// terminal app. An agent in window mode goes back to its TUI.
func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	p, a := st.FindAgent(id)
	if a == nil {
		fail(w, http.StatusNotFound, errors.New("no such agent"))
		return
	}
	if err = drive.EnsureDeck(p.Path); err == nil {
		err = deck.OpenAgent(st, id, s.kinds)
	}
	if err == nil {
		err = openTerminal(deckEnv() + strings.TrimSpace(deck.SelfCommand("")))
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	changed()
	w.WriteHeader(http.StatusNoContent)
}

// history lists the past sessions of a project not open as one of its
// agents, newest first.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("project")
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	p := st.FindProject(root)
	if p == nil {
		fail(w, http.StatusNotFound, errors.New("no such project"))
		return
	}
	open := map[string]bool{}
	for _, a := range p.Agents {
		open[a.ID], open[a.SessionID] = true, true
	}
	var list []discover.Session
	for _, pr := range discover.Providers() {
		for _, se := range projectSessions(root, pr.Kind()) {
			if !open[se.ID] {
				list = append(list, se)
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Updated.After(list[j].Updated) })
	if len(list) > maxHistory {
		list = list[:maxHistory]
	}
	out := []map[string]any{}
	for _, se := range list {
		out = append(out, map[string]any{"kind": se.Kind, "id": se.ID, "title": se.Title, "cwd": se.Cwd, "origin": se.Origin, "updated": se.Updated})
	}
	reply(w, out)
}

type changedFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// gitOut runs git in dir and returns what it printed. Exit status 1 with
// output is no failure: `diff --no-index` says so for files that differ.
func gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 && out.Len() > 0 {
		err = nil
	}
	return out.Bytes(), err
}

// diff answers what agent id changed in its directory since the last
// commit: the files, and their changes as one unified diff.
func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	p, a := st.FindAgent(r.URL.Query().Get("id"))
	if a == nil {
		fail(w, http.StatusNotFound, errors.New("no such agent"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gitTimeout)
	defer cancel()
	dir := p.Dir(a)
	res, err := agentDiff(ctx, dir)
	if err != nil {
		reply(w, map[string]any{"dir": dir, "error": err.Error()})
		return
	}
	reply(w, res)
}

type diffResult struct {
	Dir    string        `json:"dir"`
	Branch string        `json:"branch"`
	Files  []changedFile `json:"files"`
	Patch  string        `json:"patch"`
	Cut    bool          `json:"cut,omitempty"`
}

// emptyTree is git's tree of nothing: what a repository with no commit
// yet is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// agentDiff collects the changes in git directory dir against HEAD,
// untracked files included.
func agentDiff(ctx context.Context, dir string) (*diffResult, error) {
	if _, err := gitOut(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return nil, errors.New("not a git repository: " + paths.Short(dir))
	}
	base := "HEAD"
	if _, err := gitOut(ctx, dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		base = emptyTree
	}
	res := &diffResult{Dir: dir, Files: []changedFile{}}
	branch, _ := gitOut(ctx, dir, "branch", "--show-current")
	res.Branch = strings.TrimSpace(string(branch))

	stat, err := gitOut(ctx, dir, "-c", "status.renames=false", "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil, err
	}
	counts, err := gitOut(ctx, dir, "diff", base, "--no-renames", "--numstat", "-z")
	if err != nil {
		return nil, err
	}
	n := map[string][2]int{}
	for _, f := range strings.Split(string(counts), "\x00") {
		if t := strings.SplitN(f, "\t", 3); len(t) == 3 { // "-" for a binary file
			add, _ := strconv.Atoi(t[0])
			del, _ := strconv.Atoi(t[1])
			n[t[2]] = [2]int{add, del}
		}
	}
	var untracked []string
	for _, e := range strings.Split(string(stat), "\x00") {
		if len(e) < 4 {
			continue
		}
		xy, path := e[:2], e[3:]
		f := changedFile{Path: path, Status: strings.TrimSpace(xy[1:])}
		switch {
		case xy == "??":
			f.Status = "??"
			untracked = append(untracked, path)
		case f.Status == "":
			f.Status = strings.TrimSpace(xy[:1])
		}
		f.Added, f.Deleted = n[path][0], n[path][1]
		res.Files = append(res.Files, f)
	}

	patch, err := gitOut(ctx, dir, "diff", base, "--no-renames")
	if err != nil {
		return nil, err
	}
	var all bytes.Buffer
	all.Write(patch)
	for _, path := range untracked {
		out, _ := gitOut(ctx, dir, "diff", "--no-index", "--", "/dev/null", path)
		all.Write(out)
		for i := range res.Files {
			if res.Files[i].Path == path {
				res.Files[i].Added = max(bytes.Count(out, []byte("\n+"))-1, 0) // not the "+++" line
			}
		}
	}
	if all.Len() > maxPatch {
		all.Truncate(maxPatch)
		res.Cut = true
	}
	res.Patch = all.String()
	return res, nil
}
