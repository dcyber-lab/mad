// Package discover finds the projects and sessions the user already has
// outside the deck: Claude/Codex session history (CLI and desktop apps) and
// agents running in other terminals or in the Claude desktop app.
package discover

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

	"github.com/dcyber-lab/mad/internal/paths"
)

// Candidate is a project suggested by the add-project picker.
type Candidate struct {
	Path     string
	LastUsed time.Time
	Sources  []string // "claude", "claude desktop", "codex", "codex desktop"
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

// Where is a short "where it runs" label.
func (e External) Where() string {
	if e.Desktop {
		return "desktop"
	}
	return e.TTY
}

// HistoryDays bounds how far back history is scanned.
const HistoryDays = 60

var (
	cwdRe     = regexp.MustCompile(`"cwd":"((?:[^"\\]|\\.)*)"`)
	uuidTail  = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)
	nonAlnum  = regexp.MustCompile(`[^A-Za-z0-9]`)
	tempRoots = []string{"/private/tmp", "/tmp", "/private/var", "/var/folders"}
	psLineRe  = regexp.MustCompile(`^\s*(\d+)\s+(\S+)\s+(.*)$`)
)

// Process inspection, replaceable in tests.
var (
	listProcesses = func() ([]byte, error) { return exec.Command("ps", "-axo", "pid=,tty=,args=").Output() }
	processCwd    = lsofCwd
	openFiles     = lsofFiles
)

func claudeProjectsDir() string { return filepath.Join(paths.Home(), ".claude", "projects") }
func codexSessionsDir() string  { return filepath.Join(paths.CodexHome(), "sessions") }

// claudeProjectDir is where claude keeps transcripts for a cwd.
func claudeProjectDir(cwd string) string {
	return filepath.Join(claudeProjectsDir(), nonAlnum.ReplaceAllString(cwd, "-"))
}

// rootCache memoizes dir → project root (git calls are the slow part).
var rootCache sync.Map

// ResolveRoot maps a working directory to the project it belongs to.
// Agent-managed worktrees (.claude/worktrees/x) count as their main repo.
func ResolveRoot(dir string) string {
	if v, ok := rootCache.Load(dir); ok {
		return v.(string)
	}
	root := dir
	if i := strings.Index(dir, "/.claude/worktrees/"); i >= 0 {
		root = dir[:i]
	}
	root, _ = paths.ProjectRoot(root)
	rootCache.Store(dir, root)
	return root
}

// UsableDir filters out directories that aren't projects: home, temp dirs,
// hidden tool state, and paths that no longer exist.
func UsableDir(dir string) bool {
	if dir == "" || dir == "/" || dir == paths.Home() {
		return false
	}
	for _, t := range tempRoots {
		if dir == t || strings.HasPrefix(dir, t+"/") {
			return false
		}
	}
	if strings.Contains(dir, "/.") {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// readCwd pulls the first "cwd" field out of the head of a session file,
// and whether a desktop app wrote it.
func readCwd(path string, limit int64) (cwd string, desktop bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, limit))
	desktop = strings.Contains(string(data), `"entrypoint":"claude-desktop"`) ||
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

// codexHeads caches rollout path → head; rollout files never change cwd.
var codexHeads sync.Map

func codexHeadOf(path string) codexHead {
	if v, ok := codexHeads.Load(path); ok {
		return v.(codexHead)
	}
	cwd, desktop := readCwd(path, 16<<10)
	h := codexHead{cwd, desktop}
	codexHeads.Store(path, h)
	return h
}

// codexFiles lists rollout files of the last HistoryDays days, newest day
// first.
func codexFiles(visit func(path string, info os.FileInfo) bool) {
	for day := 0; day <= HistoryDays; day++ {
		dayDir := filepath.Join(codexSessionsDir(), time.Now().AddDate(0, 0, -day).Format("2006/01/02"))
		entries, _ := os.ReadDir(dayDir)
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if !visit(filepath.Join(dayDir, e.Name()), info) {
				return
			}
		}
	}
}

// ScanHistory lists projects from Claude and Codex session history, most
// recently used first.
func ScanHistory() []Candidate {
	byRoot := map[string]*Candidate{}
	add := func(dir string, t time.Time, src string) {
		if dir == "" {
			return
		}
		// Resolve first: worktree paths (…/.claude/worktrees/x) are hidden
		// dirs themselves but belong to a usable project.
		root := ResolveRoot(dir)
		if !UsableDir(root) {
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

	cutoff := time.Now().AddDate(0, 0, -HistoryDays)
	dirs, _ := os.ReadDir(claudeProjectsDir())
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		newest, t := newestJSONL(filepath.Join(claudeProjectsDir(), d.Name()))
		if newest == "" || t.Before(cutoff) {
			continue
		}
		cwd, desktop := readCwd(newest, 64<<10)
		src := "claude"
		if desktop {
			src = "claude desktop"
		}
		add(cwd, t, src)
	}

	codexFiles(func(path string, info os.FileInfo) bool {
		h := codexHeadOf(path)
		src := "codex"
		if h.desktop {
			src = "codex desktop"
		}
		add(h.cwd, info.ModTime(), src)
		return true
	})

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
	entries, _ := os.ReadDir(claudeProjectDir(cwd))
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

// proc is an agent process picked out of ps output.
type proc struct {
	pid     int
	tty     string
	kind    string
	args    []string
	desktop bool
}

// parsePS picks claude/codex processes (kinds in want) out of
// `ps -axo pid=,tty=,args=` output: TUIs on a terminal that isn't one of
// the deck's (deckTTYs), and Claude desktop conversations.
func parsePS(out string, want, deckTTYs map[string]bool) []proc {
	var procs []proc
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		m := psLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		tty, cmdline := m[2], m[3]
		p := proc{tty: tty}
		p.pid, _ = strconv.Atoi(m[1])
		switch {
		case isTerminal(tty) && !deckTTYs["/dev/"+tty]:
			f := strings.Fields(cmdline)
			p.kind, p.args = filepath.Base(f[0]), f[1:]
		case (tty == "??" || tty == "?") && isClaudeDesktop(cmdline):
			p.kind, p.desktop = "claude", true
			i := strings.Index(cmdline, "MacOS/claude ") + len("MacOS/claude ")
			p.args = strings.Fields(cmdline[i:])
		default:
			continue
		}
		if !want[p.kind] {
			continue
		}
		// Started by a mad deck (maybe one on another socket).
		if strings.Contains(cmdline, "/mad/claude-settings.json") {
			continue
		}
		procs = append(procs, p)
	}
	return procs
}

func isTerminal(tty string) bool {
	return strings.HasPrefix(tty, "ttys") || strings.HasPrefix(tty, "pts/") || strings.HasPrefix(tty, "tty")
}

// isClaudeDesktop matches the Claude app's per-conversation claude process;
// its launcher wrapper and non-persisting helper runs don't count.
func isClaudeDesktop(cmdline string) bool {
	return strings.Contains(cmdline, "/claude-code/") &&
		strings.Contains(cmdline, "MacOS/claude ") &&
		!strings.Contains(cmdline, "--no-session-persistence") &&
		!strings.Contains(cmdline, "Helpers/disclaimer")
}

// externalCache: a pid's cwd and session don't change while it lives.
var (
	externalMu    sync.Mutex
	externalCache = map[int]External{}
)

// ScanExternal finds claude/codex sessions (kinds) running outside the
// deck, whose own terminals are deckTTYs (e.g. /dev/ttys004).
func ScanExternal(kinds []string, deckTTYs map[string]bool) []External {
	out, err := listProcesses()
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
	var guess []int // claude entries without a known session id
	for _, p := range parsePS(string(out), want, deckTTYs) {
		alive[p.pid] = true
		e, ok := externalCache[p.pid]
		if !ok {
			e = External{PID: p.pid, TTY: p.tty, Desktop: p.desktop, Kind: p.kind, Cwd: processCwd(p.pid)}
			if e.Cwd == "" {
				continue
			}
			e.Root = ResolveRoot(e.Cwd)
			e.SessionID = SessionFromArgs(p.kind, p.args)
			if p.kind == "codex" && e.SessionID == "" {
				e.SessionID = codexOpenRollout(p.pid)
			}
			externalCache[p.pid] = e
		}
		res = append(res, e)
		if e.Kind == "claude" && e.SessionID == "" {
			guess = append(guess, len(res)-1)
		}
	}
	for pid := range externalCache {
		if !alive[pid] {
			delete(externalCache, pid)
		}
	}

	// Claude without an explicit id: hand out that cwd's newest sessions,
	// newest process first. A guess, but right for the one-per-dir case.
	sort.Slice(guess, func(i, j int) bool { return res[guess[i]].PID > res[guess[j]].PID })
	used := map[string]bool{}
	for _, e := range res {
		used[e.SessionID] = true
	}
	for _, i := range guess {
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
	matches, _ := filepath.Glob(filepath.Join(claudeProjectsDir(), "*", id+".jsonl"))
	if len(matches) == 0 {
		return false
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		return false
	}
	return cachedSession(fileEntry{matches[0], info.ModTime()}, parseClaudeSession) != nil
}

func lsofCwd(pid int) string {
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

func lsofFiles(pid int) []string {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n") {
			files = append(files, l[1:])
		}
	}
	return files
}

// SessionFromArgs reads an explicit session id off an agent's arguments.
func SessionFromArgs(kind string, args []string) string {
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
	best := ""
	for _, f := range openFiles(pid) {
		if strings.Contains(f, "/rollout-") && filepath.Base(f) > filepath.Base(best) {
			best = f
		}
	}
	if m := uuidTail.FindStringSubmatch(best); m != nil {
		return m[1]
	}
	return ""
}

// TerminateExternal asks an external agent to exit (SIGTERM) and waits up
// to five seconds, so its session can be resumed inside the deck.
func TerminateExternal(pid int) error {
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
	return &ExitTimeoutError{pid}
}

type ExitTimeoutError struct{ PID int }

func (e *ExitTimeoutError) Error() string {
	return "pid " + strconv.Itoa(e.PID) + " did not exit; close it in its terminal and retry"
}

// FuzzyMatch reports whether all runes of q appear in s in order
// (case-insensitive), with a score where lower is better; substrings beat
// scattered matches.
func FuzzyMatch(q, s string) (bool, int) {
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

// CompleteDir lists subdirectories matching a path being typed
// ("~/go/sr"); hidden ones only when the typed name starts with a dot.
func CompleteDir(input string) []string {
	if input == "~" {
		input = "~/"
	}
	// Split the raw input: cleaning it first would eat a trailing ".".
	dirPart, prefix := ".", input
	if i := strings.LastIndex(input, "/"); i >= 0 {
		dirPart, prefix = input[:i+1], input[i+1:]
	}
	dir := paths.Expand(dirPart)
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
