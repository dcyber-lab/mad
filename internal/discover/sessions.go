package discover

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/textutil"
)

// Session is a past Claude/Codex conversation, from the CLI or a desktop
// app, that can be resumed as a deck agent.
type Session struct {
	Kind    string // claude | codex
	ID      string
	Title   string
	Cwd     string
	Origin  string // cli | desktop
	Updated time.Time
}

// MaxSessions caps how many sessions a project lists.
const MaxSessions = 40

var (
	titleRe      = regexp.MustCompile(`"(?:customTitle|aiTitle)":"((?:[^"\\]|\\.)*)"`)
	reminderRe   = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	sessionCache sync.Map // path@mtime → *Session (nil for skipped files)
)

// ProjectSessions lists resumable sessions of kind ("claude" or "codex")
// for a project root, newest first, including sessions that ran in its
// agent worktrees. Sessions started by programs rather than a person
// (desktop workflows, -p/SDK runs, codex subagents) are left out.
func ProjectSessions(root, kind string) []Session {
	var out []Session
	switch kind {
	case "claude":
		out = claudeSessions(root)
	case "codex":
		out = codexSessions(root)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > MaxSessions {
		out = out[:MaxSessions]
	}
	return out
}

type fileEntry struct {
	path string
	t    time.Time
}

func claudeSessions(root string) []Session {
	base := claudeProjectDir(root)
	dirs := []string{base}
	wt, _ := filepath.Glob(base + "--claude-worktrees-*")
	dirs = append(dirs, wt...)

	var files []fileEntry
	for _, d := range dirs {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if e.IsDir() || !uuidTail.MatchString(e.Name()) {
				continue
			}
			if info, err := e.Info(); err == nil {
				files = append(files, fileEntry{filepath.Join(d, e.Name()), info.ModTime()})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].t.After(files[j].t) })

	var out []Session
	for i, f := range files {
		// Automated sessions can vastly outnumber real ones; bound the work.
		if len(out) >= MaxSessions || i >= 4*MaxSessions {
			break
		}
		if s := cachedSession(f, parseClaudeSession); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func cachedSession(f fileEntry, parse func(string) *Session) *Session {
	key := f.path + "@" + f.t.String()
	if v, ok := sessionCache.Load(key); ok {
		return v.(*Session)
	}
	s := parse(f.path)
	if s != nil {
		s.Updated = f.t
	}
	sessionCache.Store(key, s)
	return s
}

// parseClaudeSession keeps interactive sessions (CLI or desktop) that have
// at least one message a person typed.
func parseClaudeSession(path string) *Session {
	head, tail := readHeadTail(path, 256<<10, 128<<10)
	m := uuidTail.FindStringSubmatch(path)
	if m == nil {
		return nil
	}
	s := &Session{Kind: "claude", ID: m[1]}
	var firstMsg string
	sc := bufio.NewScanner(bytes.NewReader(head))
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	for sc.Scan() {
		var ln struct {
			Type       string `json:"type"`
			Entrypoint string `json:"entrypoint"`
			Cwd        string `json:"cwd"`
			IsMeta     bool   `json:"isMeta"`
			TurnOrigin string `json:"turnOrigin"`
			Message    struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &ln) != nil {
			continue
		}
		if ln.Entrypoint != "" && s.Origin == "" {
			switch ln.Entrypoint {
			case "cli":
				s.Origin = "cli"
			case "claude-desktop":
				s.Origin = "desktop"
			default:
				return nil // sdk / -p runs
			}
		}
		if ln.Cwd != "" && s.Cwd == "" {
			s.Cwd = ln.Cwd
		}
		if ln.Type == "user" && !ln.IsMeta && firstMsg == "" {
			if ln.TurnOrigin == "sdk" {
				return nil // started by a program (desktop workflows), not a person
			}
			firstMsg = UserText(ln.Message.Content)
		}
		if s.Origin != "" && s.Cwd != "" && firstMsg != "" {
			break
		}
	}
	if s.Origin == "" || firstMsg == "" {
		return nil
	}
	s.Title = firstMsg
	if m := titleRe.FindAllSubmatch(append(head, tail...), -1); len(m) > 0 {
		s.Title = unescapeJSON(m[len(m)-1][1])
	}
	return s
}

// UserText extracts what the user typed, skipping injected context
// (system reminders, AGENTS.md, slash-command wrappers).
func UserText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if p.Type == "text" || p.Type == "input_text" {
				text += p.Text + "\n"
			}
		}
	}
	text = strings.TrimSpace(reminderRe.ReplaceAllString(text, ""))
	if text == "" || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "# AGENTS.md") ||
		strings.HasPrefix(text, "Caveat:") {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	return textutil.Truncate(line, 80)
}

func codexSessions(root string) []Session {
	names := CodexThreadNames()
	var out []Session
	codexFiles(func(path string, info os.FileInfo) bool {
		// Cheap cwd check before a full parse.
		if c := codexHeadOf(path).cwd; c == "" || ResolveRoot(c) != root {
			return true
		}
		if s := cachedSession(fileEntry{path, info.ModTime()}, parseCodexSession); s != nil {
			if n := names[s.ID]; n != "" {
				s.Title = n
			}
			out = append(out, *s)
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

// CodexThreadNames reads ~/.codex/session_index.jsonl (id → thread name,
// last entry wins).
func CodexThreadNames() map[string]string {
	names := map[string]string{}
	f, err := os.Open(filepath.Join(filepath.Dir(codexSessionsDir()), "session_index.jsonl"))
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

// readHeadTail returns up to head bytes from the start of a file and up to
// tail bytes from its end (not overlapping the head).
func readHeadTail(path string, head, tail int64) ([]byte, []byte) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	h, _ := io.ReadAll(io.LimitReader(f, head))
	var t []byte
	if tail > 0 {
		if fi, err := f.Stat(); err == nil && fi.Size() > head {
			off := fi.Size() - tail
			if off < head {
				off = head
			}
			if _, err := f.Seek(off, io.SeekStart); err == nil {
				t, _ = io.ReadAll(f)
			}
		}
	}
	return h, t
}

func unescapeJSON(b []byte) string {
	var s string
	if json.Unmarshal(append(append([]byte{'"'}, b...), '"'), &s) != nil {
		return string(b)
	}
	return s
}
