package discover

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// claude is Claude Code, in a terminal or run by the Claude desktop app.
// Its sessions are ~/.claude/projects/<cwd with non-alphanumerics as
// dashes>/<session id>.jsonl, subagents under <session id>/subagents/.
// mad passes it a settings file of its own with --settings: hooks that
// report status, and a status line that reports the usage limits.
type claude struct{}

func (claude) Kind() string { return "claude" }

const claudeSettingsName = "claude-settings.json"

// claudeSettingsPath is mad's settings file for claude, next to mad's
// config.
func claudeSettingsPath() string { return filepath.Join(paths.ConfigDir(), claudeSettingsName) }

// claudeSettings holds hooks that report status to `mad hook claude`, and
// with quota set a status line command that records the plan's usage
// limits (it runs the user's own status line afterwards). The user's own
// settings stay untouched.
func claudeSettings(quota bool) []byte {
	self := paths.ShellQuote(paths.Self())
	hook := []map[string]any{{"hooks": []map[string]any{{
		"type": "command", "command": self + " hook claude", "timeout": 5,
	}}}}
	toolHook := []map[string]any{{"matcher": "*", "hooks": hook[0]["hooks"]}}
	settings := map[string]any{"hooks": map[string]any{
		"SessionStart":     hook,
		"UserPromptSubmit": hook,
		"PreToolUse":       toolHook,
		"PostToolUse":      toolHook,
		"Notification":     hook,
		"Stop":             hook,
	}}
	if quota {
		settings["statusLine"] = map[string]any{"type": "command", "command": self + " hook claude statusline"}
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	return data
}

func (claude) Setup(quota bool) error {
	return paths.WriteFileAtomic(claudeSettingsPath(), claudeSettings(quota))
}

func (claude) Placeholders() map[string]string {
	return map[string]string{"{claude_settings}": paths.ShellQuote(claudeSettingsPath())}
}

// Launched: mad's settings file is on the command line (whichever config
// dir the deck that started it uses).
func (claude) Launched(cmdline string) bool {
	return strings.Contains(cmdline, "/mad/"+claudeSettingsName)
}

// Hook takes a hook event on stdin, or with "statusline" the JSON claude
// gives its status line, whose output is the status line shown.
func (claude) Hook(args []string, stdin io.Reader, stdout io.Writer, now time.Time) Report {
	if len(args) > 0 && args[0] == "statusline" {
		return Report{Quota: claudeStatusLine(stdin, stdout, now)}
	}
	return Report{Hook: parseClaudeHook(stdin)}
}

// parseClaudeHook maps a hook event to a status; nil means the event
// doesn't change it.
func parseClaudeHook(r io.Reader) *status.Hook {
	var ev struct {
		Event            string `json:"hook_event_name"`
		SessionID        string `json:"session_id"`
		ToolName         string `json:"tool_name"`
		Message          string `json:"message"`
		NotificationType string `json:"notification_type"`
	}
	data, _ := io.ReadAll(r)
	if json.Unmarshal(data, &ev) != nil {
		return nil
	}
	h := &status.Hook{Event: ev.Event, SessionID: ev.SessionID}
	switch ev.Event {
	case "SessionStart", "Stop":
		h.State = status.Idle
	case "UserPromptSubmit", "PostToolUse":
		h.State = status.Running
	case "PreToolUse":
		h.State = status.Running
		if ev.ToolName == "AskUserQuestion" || ev.ToolName == "ExitPlanMode" {
			h.State = status.Waiting
		}
	case "Notification":
		msg := strings.ToLower(ev.Message)
		switch {
		case ev.NotificationType == "permission_prompt", strings.Contains(msg, "permission"):
			h.State, h.Message = status.Waiting, ev.Message
		case ev.NotificationType == "idle_prompt", strings.Contains(msg, "waiting for your input"):
			h.State = status.Idle
		default:
			return nil
		}
	default:
		return nil
	}
	return h
}

// claudeStatusLine reads the usage limits off the status line JSON, then
// hands the same JSON to the status line the user configured, if any, so
// theirs still shows. Without one it prints a short line of its own.
func claudeStatusLine(in io.Reader, out io.Writer, now time.Time) *status.Quota {
	data, _ := io.ReadAll(in)
	q, known := status.ParseStatusLine(bytes.NewReader(data))
	if cmd := claudeUserStatusLine(); cmd != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "sh", "-c", cmd)
		c.Stdin, c.Stdout, c.Stderr = bytes.NewReader(data), out, io.Discard
		_ = c.Run()
	} else {
		var ev struct {
			Model struct {
				Name string `json:"display_name"`
			} `json:"model"`
			Context struct {
				Used *float64 `json:"used_percentage"`
			} `json:"context_window"`
		}
		_ = json.Unmarshal(data, &ev)
		var parts []string
		if ev.Model.Name != "" {
			parts = append(parts, ev.Model.Name)
		}
		if ev.Context.Used != nil {
			parts = append(parts, fmt.Sprintf("ctx %.0f%%", *ev.Context.Used))
		}
		if known {
			if w := q.FiveHour; w.Known() {
				s := fmt.Sprintf("5h %.0f%%", w.Used)
				if !w.ResetAt.IsZero() {
					s += " (" + textutil.Until(w.ResetAt, now) + ")"
				}
				parts = append(parts, s)
			}
			if w := q.SevenDay; w.Known() {
				parts = append(parts, fmt.Sprintf("wk %.0f%%", w.Used))
			}
		}
		fmt.Fprintln(out, strings.Join(parts, " · "))
	}
	if !known {
		return nil
	}
	return &q
}

// claudeUserStatusLine is the status line command from the user's own
// claude settings, the most specific first: the project's local and
// shared settings, then ~/.claude/settings.json.
func claudeUserStatusLine() string {
	cwd, _ := os.Getwd()
	files := []string{
		filepath.Join(cwd, ".claude", "settings.local.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(paths.Home(), ".claude", "settings.json"),
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var st struct {
			StatusLine *struct {
				Command string `json:"command"`
			} `json:"statusLine"`
		}
		if json.Unmarshal(data, &st) == nil && st.StatusLine != nil && st.StatusLine.Command != "" {
			return st.StatusLine.Command
		}
	}
	return ""
}

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

// claudeHumans remembers sessions found to be a person's: what makes one
// so is at its head and doesn't change as the conversation goes on, while
// its mtime does at every message.
var claudeHumans sync.Map // session id → true

func (claude) HumanSession(id string) bool {
	if id == "" {
		return false
	}
	if _, ok := claudeHumans.Load(id); ok {
		return true
	}
	matches, _ := filepath.Glob(filepath.Join(claudeProjectsDir(), "*", id+".jsonl"))
	if len(matches) == 0 {
		return false
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		return false
	}
	if cachedSession(fileEntry{matches[0], info.ModTime()}, parseClaudeSession) == nil {
		return false // maybe not yet: a new conversation has no message
	}
	claudeHumans.Store(id, true)
	return true
}
