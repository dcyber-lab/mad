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
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/status"
)

// claude is Claude Code, in a terminal or run by the Claude desktop app.
// Its sessions are ~/.claude/projects/<cwd with non-alphanumerics as
// dashes>/<session id>.jsonl, subagents under <session id>/subagents/.
type claude struct{}

func (claude) Kind() string { return "claude" }

var claudeTitleRe = regexp.MustCompile(`"(?:customTitle|aiTitle)":"((?:[^"\\]|\\.)*)"`)

func claudeProjectsDir() string { return filepath.Join(paths.Home(), ".claude", "projects") }

// claudeProjectDir is where claude keeps transcripts for a cwd.
func claudeProjectDir(cwd string) string {
	return filepath.Join(claudeProjectsDir(), nonAlnum.ReplaceAllString(cwd, "-"))
}

func (claude) History(cutoff time.Time, add func(cwd string, t time.Time, desktop bool)) {
	dirs, _ := os.ReadDir(claudeProjectsDir())
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		newest, t := newestJSONL(filepath.Join(claudeProjectsDir(), d.Name()))
		if newest == "" || t.Before(cutoff) {
			continue
		}
		cwd, desktop := readCwd(newest, 64<<10, `"entrypoint":"claude-desktop"`)
		add(cwd, t, desktop)
	}
}

func (claude) Sessions(root string) []Session {
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
	if m := claudeTitleRe.FindAllSubmatch(append(head, tail...), -1); len(m) > 0 {
		s.Title = unescapeJSON(m[len(m)-1][1])
	}
	return s
}

// Transcripts finds the session under dir's project, else under any
// project (an adopted session that ran elsewhere).
func (claude) Transcripts(dir, sid string) []string {
	base := claudeProjectDir(dir)
	main := filepath.Join(base, sid+".jsonl")
	if _, err := os.Stat(main); err != nil {
		main = ""
		entries, _ := os.ReadDir(claudeProjectsDir())
		for _, e := range entries {
			p := filepath.Join(claudeProjectsDir(), e.Name(), sid+".jsonl")
			if _, err := os.Stat(p); err == nil {
				main, base = p, filepath.Dir(p)
				break
			}
		}
		if main == "" {
			return nil
		}
	}
	out := []string{main}
	subs, _ := filepath.Glob(filepath.Join(base, sid, "subagents", "*.jsonl"))
	return append(out, subs...)
}

var usageKey = []byte(`"usage"`)

// Parse reads titles, prompts, tool calls and usage. A response is logged
// once per content block, each line repeating the usage of the whole.
func (claude) Parse(line []byte) (Event, bool) {
	var ln struct {
		Type        string `json:"type"`
		IsMeta      bool   `json:"isMeta"`
		AITitle     string `json:"aiTitle"`
		CustomTitle string `json:"customTitle"`
		Message     struct {
			ID      string          `json:"id"`
			Content json.RawMessage `json:"content"`
			Usage   *struct {
				Input      int64 `json:"input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				Output     int64 `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ln) != nil {
		return Event{}, false
	}
	var e Event
	switch ln.Type {
	case "ai-title":
		e.Title, e.TitleSource = ln.AITitle, TitleAI
	case "custom-title":
		e.Title, e.TitleSource = ln.CustomTitle, TitleCustom
	case "user":
		if ln.IsMeta {
			return e, false
		}
		e.Prompt = UserText(ln.Message.Content)
	case "assistant":
		var parts []struct {
			Type  string                     `json:"type"`
			Name  string                     `json:"name"`
			Input map[string]json.RawMessage `json:"input"`
		}
		if json.Unmarshal(ln.Message.Content, &parts) == nil {
			for _, p := range parts {
				if p.Type == "tool_use" && p.Name != "" {
					e.Tool = toolLabel(p.Name, p.Input)
				}
			}
		}
		if u := ln.Message.Usage; u != nil && bytes.Contains(line, usageKey) {
			e.Usage = &Tokens{Input: u.Input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output}
			e.Message = ln.Message.ID
		}
	default:
		return e, false
	}
	return e, true
}

// Titles: claude keeps its titles in the transcript.
func (claude) Titles() map[string]string { return nil }

// Desktop matches the Claude app's per-conversation claude process; its
// launcher wrapper and non-persisting helper runs don't count.
func (claude) Desktop(cmdline string) ([]string, bool) {
	const exe = "MacOS/claude "
	if !strings.Contains(cmdline, "/claude-code/") || !strings.Contains(cmdline, exe) ||
		strings.Contains(cmdline, "--no-session-persistence") || strings.Contains(cmdline, "Helpers/disclaimer") {
		return nil, false
	}
	return strings.Fields(cmdline[strings.Index(cmdline, exe)+len(exe):]), true
}

// ProcessSession reads --resume or --session-id; a bare `claude` or
// --continue picks its session itself and is guessed instead.
func (claude) ProcessSession(_ int, args []string) string {
	for i, a := range args {
		for _, flag := range []string{"--resume=", "--session-id="} {
			if strings.HasPrefix(a, flag) {
				return a[len(flag):]
			}
		}
		if (a == "--resume" || a == "-r" || a == "--session-id") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			return args[i+1]
		}
	}
	return ""
}

func (claude) RecentSessions(cwd string) []string {
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

func (claude) HumanSession(id string) bool {
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

// Hook takes the hook event claude writes to stdin (see deck.ClaudeSettings).
func (claude) Hook(_ []string, stdin io.Reader) *status.Hook { return status.ParseClaude(stdin) }
