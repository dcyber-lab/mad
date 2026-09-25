package discover

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/status"
)

// codex is OpenAI's Codex CLI, or its desktop app. Each thread is a rollout
// file, $CODEX_HOME/sessions/YYYY/MM/DD/rollout-<time>-<thread id>.jsonl;
// the names threads were given are in session_index.jsonl next to it.
// mad learns of finished turns through codex's notify command.
type codex struct{}

func (codex) Kind() string { return "codex" }

// codexHome is codex's own directory: $CODEX_HOME, defaulting to ~/.codex.
func codexHome() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(paths.Home(), ".codex")
}

func codexSessionsDir() string { return filepath.Join(codexHome(), "sessions") }

// Setup: codex takes its wiring on the command line.
func (codex) Setup(bool) error { return nil }

// codexNotify marks mad's notify command on a codex command line.
const codexNotify = `"hook","codex"]`

// Placeholders: {codex_notify} points codex's notify at `mad hook codex`,
// unless the user set a notify command of their own.
func (codex) Placeholders() map[string]string {
	notify := ""
	if !codexHasOwnNotify() {
		notify = `-c ` + paths.ShellQuote(`notify=["`+paths.Self()+`",`+codexNotify)
	}
	return map[string]string{"{codex_notify}": notify}
}

// notifyKey matches a `notify = ...` line in codex's config.toml, at the top
// level or in a profile.
var notifyKey = regexp.MustCompile(`^\s*notify\s*=`)

// codexHasOwnNotify reports whether the user set codex's notify command
// themselves. codex takes a single notify command, so mad's -c notify=...
// would replace theirs; mad leaves it alone then, and codex resumes fall
// back to the most recent session because mad never learns the thread id.
func codexHasOwnNotify() bool {
	data, err := os.ReadFile(filepath.Join(codexHome(), "config.toml"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if notifyKey.MatchString(line) {
			return true
		}
	}
	return false
}

func (codex) Launched(cmdline string) bool { return strings.Contains(cmdline, codexNotify) }

// Hook takes the notify payload, codex's last argument.
func (codex) Hook(args []string, _ io.Reader, _ io.Writer, _ time.Time) Report {
	if len(args) == 0 {
		return Report{}
	}
	return Report{Hook: parseCodexHook(args[len(args)-1])}
}

// parseCodexHook maps a notify payload to a status.
func parseCodexHook(payload string) *status.Hook {
	var ev struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread-id"`
	}
	if json.Unmarshal([]byte(payload), &ev) != nil || ev.Type != "agent-turn-complete" {
		return nil
	}
	return &status.Hook{State: status.Idle, Event: ev.Type, SessionID: ev.ThreadID}
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
	cwd, desktop := readCwd(path, 16<<10, `"originator":"Codex Desktop"`)
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

// History goes by day directories, which bound it by themselves.
func (codex) History(_ time.Time, add func(cwd string, t time.Time, desktop bool)) {
	codexFiles(func(path string, info os.FileInfo) bool {
		h := codexHeadOf(path)
		add(h.cwd, info.ModTime(), h.desktop)
		return true
	})
}

func (codex) Sessions(root string) []Session {
	names := codexThreadNames()
	var out []Session
	codexFiles(func(path string, info os.FileInfo) bool {
		// Cheap cwd check before a full parse.
		if c := codexHeadOf(path).cwd; c == "" || ResolveRoot(c) != root {
			return true
		}
		if s := cachedSession(fileEntry{path, info.ModTime()}, parseCodexSession); s != nil {
			ss := *s // the cached one stays as parsed
			if n := names[ss.ID]; n != "" {
				ss.Title = n
			}
			out = append(out, ss)
		}
		return len(out) < MaxSessions
	})
	return out
}

func parseCodexSession(path string) *Session {
	head, _ := readHeadTail(path, 512<<10, 0)
	s := &Session{Kind: "codex"}
	sc := bufio.NewScanner(bytes.NewReader(head))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var ln struct {
			Type    string `json:"type"`
			Payload struct {
				ID         string          `json:"id"`
				Cwd        string          `json:"cwd"`
				Originator string          `json:"originator"`
				Parent     string          `json:"parent_thread_id"`
				Source     json.RawMessage `json:"thread_source"`
				Role       string          `json:"role"`
				Content    json.RawMessage `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &ln) != nil {
			continue
		}
		switch {
		case ln.Type == "session_meta":
			s.ID, s.Cwd = ln.Payload.ID, ln.Payload.Cwd
			var source string
			_ = json.Unmarshal(ln.Payload.Source, &source)
			if ln.Payload.Parent != "" || (source != "" && source != "user") {
				return nil // subagent / guardian review threads
			}
			switch {
			case strings.Contains(ln.Payload.Originator, "Desktop"):
				s.Origin = "desktop"
			case ln.Payload.Originator == "codex_exec":
				return nil // non-interactive
			default:
				s.Origin = "cli"
			}
		case ln.Type == "response_item" && ln.Payload.Role == "user" && s.Title == "":
			s.Title = UserText(ln.Payload.Content)
		}
		if s.ID != "" && s.Title != "" {
			break
		}
	}
	if s.ID == "" {
		return nil
	}
	if s.Title == "" {
		s.Title = "(no messages)"
	}
	return s
}

// codexRollouts caches thread id → rollout file once found: finding one
// means globbing the whole sessions tree.
var codexRollouts sync.Map

// Transcripts is the thread's rollout; codex logs its subagents as threads
// of their own.
func (codex) Transcripts(_, sid string) []string {
	if sid == "" {
		return nil
	}
	if v, ok := codexRollouts.Load(sid); ok {
		return []string{v.(string)}
	}
	matches, _ := filepath.Glob(filepath.Join(codexSessionsDir(), "*", "*", "*", "rollout-*-"+sid+".jsonl"))
	for _, m := range matches {
		if strings.HasSuffix(m, "-"+sid+".jsonl") {
			codexRollouts.Store(sid, m)
			return []string{m}
		}
	}
	return nil
}

var tokenCountKey = []byte(`"token_count"`)

// Parse reads the running token total and rate limits codex logs after
// every response, what the user typed, and the function calls it makes.
func (codex) Parse(line []byte) (Event, bool) {
	var ln struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Payload   struct {
			Type       string          `json:"type"`
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			Name       string          `json:"name"`
			Arguments  string          `json:"arguments"`
			RateLimits *struct {
				Primary   *codexWindow `json:"primary"`
				Secondary *codexWindow `json:"secondary"`
			} `json:"rate_limits"`
			Info *struct {
				Total struct {
					Input      int64 `json:"input_tokens"`
					CacheRead  int64 `json:"cached_input_tokens"`
					CacheWrite int64 `json:"cache_write_input_tokens"`
					Output     int64 `json:"output_tokens"`
				} `json:"total_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &ln) != nil {
		return Event{}, false
	}
	var e Event
	p := ln.Payload
	switch {
	case ln.Type == "event_msg" && p.Type == "token_count":
		if p.RateLimits != nil {
			at, _ := time.Parse(time.RFC3339Nano, ln.Timestamp)
			e.Quota = &status.Quota{At: at}
			if w := p.RateLimits.Primary; w != nil {
				e.Quota.FiveHour = w.window(at)
			}
			if w := p.RateLimits.Secondary; w != nil {
				e.Quota.SevenDay = w.window(at)
			}
		}
		if p.Info != nil && bytes.Contains(line, tokenCountKey) {
			// codex counts cached tokens inside input_tokens.
			u := p.Info.Total
			input := u.Input - u.CacheRead - u.CacheWrite
			if input < 0 {
				input = 0
			}
			e.Total = &Tokens{Input: input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output}
		}
	case ln.Type == "response_item" && p.Type == "message" && p.Role == "user":
		e.Prompt = UserText(p.Content)
	case ln.Type == "response_item" && (p.Type == "function_call" || p.Type == "custom_tool_call") && p.Name != "":
		var args map[string]json.RawMessage
		json.Unmarshal([]byte(p.Arguments), &args)
		e.Tool = toolLabel(p.Name, args)
	default:
		return e, false
	}
	return e, true
}

// codexWindow is one of codex's rate limit windows: the primary one is
// the five-hour window, the secondary the weekly one. Newer codex gives
// the reset as a time, older as seconds from the event.
type codexWindow struct {
	Used     float64 `json:"used_percent"`
	ResetsAt int64   `json:"resets_at"`
	ResetsIn int64   `json:"resets_in_seconds"`
}

func (w *codexWindow) window(at time.Time) status.Window {
	out := status.Window{Used: w.Used}
	switch {
	case w.ResetsAt > 0:
		out.ResetAt = time.Unix(w.ResetsAt, 0)
	case w.ResetsIn > 0 && !at.IsZero():
		out.ResetAt = at.Add(time.Duration(w.ResetsIn) * time.Second)
	}
	return out
}

func (codex) Titles() map[string]string { return codexThreadNames() }

// codexNames is session_index.jsonl as last read. The file lists every
// thread ever named, and the sidebar asks for it every few seconds, so it
// is read again only when it changes.
var codexNames struct {
	sync.Mutex
	path  string
	mod   time.Time
	size  int64
	names map[string]string
}

// codexThreadNames reads session_index.jsonl (id → thread name, last
// entry wins). The map is shared: callers only read it.
func codexThreadNames() map[string]string {
	path := filepath.Join(filepath.Dir(codexSessionsDir()), "session_index.jsonl")
	fi, err := os.Stat(path)
	if err != nil {
		return map[string]string{}
	}
	c := &codexNames
	c.Lock()
	defer c.Unlock()
	if c.names == nil || c.path != path || !c.mod.Equal(fi.ModTime()) || c.size != fi.Size() {
		c.path, c.mod, c.size, c.names = path, fi.ModTime(), fi.Size(), readCodexNames(path)
	}
	return c.names
}

func readCodexNames(path string) map[string]string {
	names := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var ln struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(sc.Bytes(), &ln) == nil && ln.Name != "" {
			names[ln.ID] = ln.Name
		}
	}
	return names
}

// Desktop: the Codex app runs its threads in-process.
func (codex) Desktop(string) ([]string, bool) { return nil, false }

// ProcessSession reads `codex resume <id>`, else the rollout file codex
// keeps open (the newest one when there are several).
func (codex) ProcessSession(pid int, args []string) string {
	for i, a := range args {
		if a == "resume" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			return args[i+1]
		}
	}
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

// RecentSessions: a codex process always has its rollout open.
func (codex) RecentSessions(string) []string { return nil }

func (codex) HumanSession(string) bool { return true }
