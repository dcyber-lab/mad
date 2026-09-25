package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Discovery of projects the user already works on outside the deck:
// Claude/Codex session history (for the add-project picker) and agent
// processes running in other terminals (auto-synced into the sidebar).

// Candidate is a project suggested by the picker.
type Candidate struct {
	Path     string
	LastUsed time.Time
	Sources  []string // "claude", "codex"
}

// External is a claude/codex session running outside the deck: a TUI in
// another terminal, or a Claude desktop app conversation.
type External struct {
	PID       int
	TTY       string
	Desktop   bool
	Kind      string
	Cwd       string
	Root      string
	SessionID string
}

const historyDays = 60

var (
	cwdRe     = regexp.MustCompile(`"cwd":"((?:[^"\\]|\\.)*)"`)
	uuidTail  = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)
	nonAlnum  = regexp.MustCompile(`[^A-Za-z0-9]`)
	tempRoots = []string{"/private/tmp", "/tmp", "/private/var", "/var/folders"}
	psLineRe  = regexp.MustCompile(`^\s*(\d+)\s+(\S+)\s+(.*)$`)
)

// rootCache memoizes dir → project root (git calls are the slow part).
var rootCache sync.Map

// resolveRoot maps a working directory to the project it belongs to.
// Agent-managed worktrees (.claude/worktrees/x) count as their main repo.
func resolveRoot(dir string) string {
	if v, ok := rootCache.Load(dir); ok {
		return v.(string)
	}
	root := dir
	if i := strings.Index(dir, "/.claude/worktrees/"); i >= 0 {
		root = dir[:i]
	}
	root, _ = projectRoot(root)
	rootCache.Store(dir, root)
	return root
}

func usableDir(dir string) bool {
	if dir == "" || dir == "/" || dir == homeDir() {
		return false
	}
	for _, t := range tempRoots {
		if dir == t || strings.HasPrefix(dir, t+"/") {
			return false
		}
	}
	// Hidden dirs are tool state (~/.claude-mem/..., caches), not projects.
	if strings.Contains(dir, "/.") {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// readCwd pulls the first "cwd" field out of the head of a session file.
func readCwd(path string, limit int64) string {
	cwd, _ := readCwdOrigin(path, limit)
	return cwd
}

// readCwdOrigin also tells whether a desktop app wrote the session.
func readCwdOrigin(path string, limit int64) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, limit))
	desktop := strings.Contains(string(data), `"entrypoint":"claude-desktop"`) ||
		strings.Contains(string(data), `"originator":"Codex Desktop"`)
	m := cwdRe.FindSubmatch(data)
	if m == nil {
		return "", desktop
	}
	return strings.ReplaceAll(string(m[1]), `\/`, `/`), desktop
}

type codexHead struct {
	cwd     string
	desktop bool
}

// codexCwdCache / codexHeadCache: rollout files never change their cwd.
var codexCwdCache, codexHeadCache sync.Map

// scanHistory lists projects from Claude and Codex session history, most
// recently used first.
func scanHistory() []Candidate {
	byRoot := map[string]*Candidate{}
	add := func(dir string, t time.Time, src string) {
		if !usableDir(dir) {
			return
		}
		root := resolveRoot(dir)
		if !usableDir(root) {
			return
		}
		c := byRoot[root]
		if c == nil {
			c = &Candidate{Path: root}
			byRoot[root] = c
		}
		if t.After(c.LastUsed) {
			c.LastUsed = t
		}
		for _, s := range c.Sources {
			if s == src {
				return
			}
		}
		c.Sources = append(c.Sources, src)
	}

	cutoff := time.Now().AddDate(0, 0, -historyDays)

	// Claude: ~/.claude/projects/<encoded cwd>/<session>.jsonl
	claudeDir := filepath.Join(homeDir(), ".claude", "projects")
	dirs, _ := os.ReadDir(claudeDir)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		newest, t := newestJSONL(filepath.Join(claudeDir, d.Name()))
		if newest == "" || t.Before(cutoff) {
			continue
		}
		cwd, desktop := readCwdOrigin(newest, 64<<10)
		src := "claude"
		if desktop {
			src = "claude desktop"
		}
		add(cwd, t, src)
	}

	// Codex: ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl
	codexDir := filepath.Join(homeDir(), ".codex", "sessions")
	for day := 0; day <= historyDays; day++ {
		dayDir := filepath.Join(codexDir, time.Now().AddDate(0, 0, -day).Format("2006/01/02"))
		files, _ := os.ReadDir(dayDir)
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dayDir, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}
			v, ok := codexHeadCache.Load(path)
			if !ok {
				cwd, desktop := readCwdOrigin(path, 16<<10)
				v = codexHead{cwd, desktop}
				codexHeadCache.Store(path, v)
			}
			h := v.(codexHead)
			src := "codex"
			if h.desktop {
				src = "codex desktop"
			}
			add(h.cwd, info.ModTime(), src)
		}
	}

	out := make([]Candidate, 0, len(byRoot))
	for _, c := range byRoot {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}

func newestJSONL(dir string) (string, time.Time) {
	entries, _ := os.ReadDir(dir)
	var best string
	var bestT time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestT) {
			best, bestT = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	return best, bestT
}

// claudeSessionsByMtime lists a cwd's claude session ids, newest first.
func claudeSessionsByMtime(cwd string) []string {
	dir := filepath.Join(homeDir(), ".claude", "projects", nonAlnum.ReplaceAllString(cwd, "-"))
	entries, _ := os.ReadDir(dir)
	type f struct {
		id string
		t  time.Time
	}
	var fs []f
	for _, e := range entries {
		if m := uuidTail.FindStringSubmatch(e.Name()); m != nil {
			if info, err := e.Info(); err == nil {
				fs = append(fs, f{m[1], info.ModTime()})
			}
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].t.After(fs[j].t) })
	ids := make([]string, len(fs))
	for i, x := range fs {
		ids[i] = x.id
	}
	return ids
}

// externalCache: a pid's cwd/session doesn't change while it lives.
var (
	externalMu    sync.Mutex
	externalCache = map[int]External{}
)

// scanExternal finds claude/codex TUIs running in terminals other than the
// deck's own panes (deckTTYs, e.g. /dev/ttys004).
func scanExternal(kinds []string, deckTTYs map[string]bool) []External {
	out, err := exec.Command("ps", "-axo", "pid=,tty=,args=").Output()
	if err != nil {
		return nil
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}

	externalMu.Lock()
	defer externalMu.Unlock()
	alive := map[int]bool{}
	var res []External
	var claudes []int // indexes into res
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		m := psLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		tty, cmdline := m[2], m[3]
		var kind string
		var args []string
		desktop := false
		switch {
		case strings.HasPrefix(tty, "ttys") && !deckTTYs["/dev/"+tty]:
			f := strings.Fields(cmdline)
			kind, args = filepath.Base(f[0]), f[1:]
		case tty == "??" && want["claude"] && strings.Contains(cmdline, "/claude-code/") &&
			strings.Contains(cmdline, "MacOS/claude ") && !strings.Contains(cmdline, "--no-session-persistence") &&
			!strings.Contains(cmdline, "Helpers/disclaimer"): // the wrapper of the same process
			// A Claude desktop conversation (helper runs don't persist).
			kind, desktop = "claude", true
			args = strings.Fields(cmdline[strings.Index(cmdline, "MacOS/claude ")+len("MacOS/claude "):])
		default:
			continue
		}
		if !want[kind] {
			continue
		}
		if strings.Contains(cmdline, claudeSettingsPath()) {
			continue // started by a mad deck (maybe another socket)
		}
		pid, _ := strconv.Atoi(m[1])
		alive[pid] = true
		e, ok := externalCache[pid]
		if !ok {
			e = External{PID: pid, TTY: tty, Desktop: desktop, Kind: kind, Cwd: processCwd(pid)}
			if e.Cwd == "" {
				continue
			}
			e.Root = resolveRoot(e.Cwd)
			e.SessionID = sessionFromArgs(kind, args)
			if kind == "codex" && e.SessionID == "" {
				e.SessionID = codexOpenRollout(pid)
			}
			externalCache[pid] = e
		}
		res = append(res, e)
		if e.Kind == "claude" && e.SessionID == "" {
			claudes = append(claudes, len(res)-1)
		}
	}
	for pid := range externalCache {
		if !alive[pid] {
			delete(externalCache, pid)
		}
	}
	// Claude without an explicit id: hand out that cwd's newest sessions,
	// newest process first. A guess, but right for the one-per-dir case.
	sort.Slice(claudes, func(i, j int) bool { return res[claudes[i]].PID > res[claudes[j]].PID })
	used := map[string]bool{}
	for _, e := range res {
		used[e.SessionID] = true
	}
	for _, i := range claudes {
		e := &res[i]
		for _, id := range claudeSessionsByMtime(e.Cwd) {
			if !used[id] {
				e.SessionID, used[id] = id, true
				break
			}
		}
	}
	// Desktop processes also run programmatic sessions (workflows); keep
	// only conversations a person is having.
	kept := res[:0]
	for _, e := range res {
		if !e.Desktop || humanClaudeSession(e.SessionID) {
			kept = append(kept, e)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].PID < kept[j].PID })
	return kept
}

func humanClaudeSession(id string) bool {
	if id == "" {
		return false
	}
	matches, _ := filepath.Glob(filepath.Join(homeDir(), ".claude", "projects", "*", id+".jsonl"))
	if len(matches) == 0 {
		return false
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		return false
	}
	return cachedSession(fileEntry{matches[0], info.ModTime()}, parseClaudeSession) != nil
}

func processCwd(pid int) string {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n") {
			return l[1:]
		}
	}
	return ""
}

func sessionFromArgs(kind string, args []string) string {
	for i, a := range args {
		switch kind {
		case "claude":
			for _, flag := range []string{"--resume=", "--session-id="} {
				if strings.HasPrefix(a, flag) {
					return a[len(flag):]
				}
			}
			if (a == "--resume" || a == "-r" || a == "--session-id") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1]
			}
		case "codex":
			if a == "resume" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1]
			}
		}
	}
	return ""
}

// codexOpenRollout reads the session id off the rollout file codex keeps
// open (the newest one when there are several).
func codexOpenRollout(pid int) string {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return ""
	}
	best := ""
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n") && strings.Contains(l, "/rollout-") && filepath.Base(l) > filepath.Base(best) {
			best = l[1:]
		}
	}
	if m := uuidTail.FindStringSubmatch(best); m != nil {
		return m[1]
	}
	return ""
}

// terminateExternal asks an external agent to exit (SIGTERM) and waits for
// it, so its session can be resumed inside the deck.
func terminateExternal(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return nil
		}
	}
	return &exitTimeoutError{pid}
}

type exitTimeoutError struct{ pid int }

func (e *exitTimeoutError) Error() string {
	return "pid " + strconv.Itoa(e.pid) + " did not exit; close it in its terminal and retry"
}

// fuzzyMatch reports whether all runes of q appear in s in order
// (case-insensitive), and a score: lower is better.
func fuzzyMatch(q, s string) (bool, int) {
	if q == "" {
		return true, 0
	}
	q, s = strings.ToLower(q), strings.ToLower(s)
	if i := strings.Index(s, q); i >= 0 {
		return true, i
	}
	score, pos := 100, 0
	for _, r := range q {
		i := strings.IndexRune(s[pos:], r)
		if i < 0 {
			return false, 0
		}
		score += i
		pos += i + len(string(r))
	}
	return true, score
}

// completeDir lists subdirectories for a path being typed ("~/go/sr").
func completeDir(input string) []string {
	p := expandPath(input)
	dir, prefix := p, ""
	if !strings.HasSuffix(input, "/") {
		dir, prefix = filepath.Dir(p), filepath.Base(p)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}
