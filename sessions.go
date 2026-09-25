package main

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

const maxSessions = 40

var (
	titleRe      = regexp.MustCompile(`"(?:customTitle|aiTitle)":"((?:[^"\\]|\\.)*)"`)
	reminderRe   = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	sessionCache sync.Map // path+mtime → Session (or nil for skipped files)
)

// projectSessions lists resumable sessions of kind for a project, newest
// first, including sessions run in its agent worktrees.
func projectSessions(root, kind string) []Session {
	var out []Session
	switch kind {
	case "claude":
		out = claudeSessions(root)
	case "codex":
		out = codexSessions(root)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > maxSessions {
		out = out[:maxSessions]
	}
	return out
}

type fileEntry struct {
	path string
	t    time.Time
}

func claudeSessions(root string) []Session {
	base := filepath.Join(homeDir(), ".claude", "projects")
	enc := nonAlnum.ReplaceAllString(root, "-")
	dirs := []string{filepath.Join(base, enc)}
	wt, _ := filepath.Glob(filepath.Join(base, enc+"--claude-worktrees-*"))
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
		if len(out) >= maxSessions || i >= 4*maxSessions {
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
// at least one real user message.
func parseClaudeSession(path string) *Session {
	head, tail := readHeadTail(path, 256<<10, 128<<10)
	s := &Session{Kind: "claude", ID: uuidTail.FindStringSubmatch(path)[1]}
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
			firstMsg = userText(ln.Message.Content)
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

// userText extracts what the user typed, skipping injected context.
func userText(raw json.RawMessage) string {
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
	return truncate(line, 80)
}

func codexSessions(root string) []Session {
	names := codexThreadNames()
	codexDir := filepath.Join(homeDir(), ".codex", "sessions")
	var out []Session
	for day := 0; day <= historyDays && len(out) < maxSessions; day++ {
		dayDir := filepath.Join(codexDir, time.Now().AddDate(0, 0, -day).Format("2006/01/02"))
		entries, _ := os.ReadDir(dayDir)
		var files []fileEntry
		for _, e := range entries {
			if info, err := e.Info(); err == nil && strings.HasSuffix(e.Name(), ".jsonl") {
				files = append(files, fileEntry{filepath.Join(dayDir, e.Name()), info.ModTime()})
			}
		}
		for _, f := range files {
			// Cheap cwd check before a full parse.
			cwd, ok := codexCwdCache.Load(f.path)
			if !ok {
				cwd = readCwd(f.path, 16<<10)
				codexCwdCache.Store(f.path, cwd)
			}
			if c := cwd.(string); c == "" || resolveRoot(c) != root {
				continue
			}
			if s := cachedSession(f, parseCodexSession); s != nil {
				if n := names[s.ID]; n != "" {
					s.Title = n
				}
				out = append(out, *s)
			}
		}
	}
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
				Source     string          `json:"thread_source"`
				Type       string          `json:"type"`
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
			if ln.Payload.Parent != "" || (ln.Payload.Source != "" && ln.Payload.Source != "user") {
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
			s.Title = userText(ln.Payload.Content)
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

// codexThreadNames reads ~/.codex/session_index.jsonl (id → thread name,
// last entry wins).
func codexThreadNames() map[string]string {
	names := map[string]string{}
	f, err := os.Open(filepath.Join(homeDir(), ".codex", "session_index.jsonl"))
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
