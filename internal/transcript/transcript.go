// Package transcript follows what an agent writes about its own session:
// the tokens it consumed, the title it gave the conversation, the last
// thing the user asked and the tool it is running. The agent itself is
// never asked: the files are followed from where the last read stopped,
// so a poll costs a stat per file and a parse of what was appended.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// Totals is what a session has consumed so far, in tokens.
type Totals struct {
	Input      int64 // uncached prompt tokens
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

// Total is every token that went through the model.
func (t Totals) Total() int64 { return t.Input + t.CacheRead + t.CacheWrite + t.Output }

func (t Totals) add(o Totals) Totals {
	return Totals{t.Input + o.Input, t.CacheRead + o.CacheRead, t.CacheWrite + o.CacheWrite, t.Output + o.Output}
}

func (t Totals) sub(o Totals) Totals {
	return Totals{t.Input - o.Input, t.CacheRead - o.CacheRead, t.CacheWrite - o.CacheWrite, t.Output - o.Output}
}

// Info is what a session's transcript says about it.
type Info struct {
	Tokens Totals
	// Title is the conversation's name: one the user gave, else the one
	// the agent generated, else the first thing the user asked.
	Title string
	// Prompt is the last thing the user asked, first line only.
	Prompt string
	// Tool is the tool call the agent made last, with a hint of its input
	// ("Bash · go test ./..."). Cleared when the user asks something new.
	Tool string
	// Quota is the account's usage limits as last reported in the
	// transcript (codex writes them after every response).
	Quota status.Quota
}

// Agent names a session whose transcript to follow.
type Agent struct {
	ID      string
	Kind    string // claude | codex; other kinds have no transcript
	Dir     string // where it runs
	Session string // its current session id
}

// Reader follows transcripts across polls. It is not safe for concurrent
// use: one poll at a time.
type Reader struct {
	files map[string]*file  // transcript path → progress
	codex map[string]string // codex session id → rollout file, once found
	// locate and threadNames are replaceable in tests.
	locate      func(a Agent) []string
	threadNames func() map[string]string
}

func NewReader() *Reader {
	r := &Reader{files: map[string]*file{}, codex: map[string]string{}, threadNames: discover.CodexThreadNames}
	r.locate = r.transcripts
	return r
}

// Followed reports whether agents of kind write a transcript mad can read.
func Followed(kind string) bool { return kind == "claude" || kind == "codex" }

// transcripts finds an agent's files. A codex rollout is looked up once:
// finding it means globbing the sessions tree.
func (r *Reader) transcripts(a Agent) []string {
	switch a.Kind {
	case "claude":
		return discover.ClaudeTranscripts(a.Dir, a.Session)
	case "codex":
		p, ok := r.codex[a.Session]
		if !ok && a.Session != "" {
			p = discover.CodexTranscript(a.Session)
			if p != "" {
				r.codex[a.Session] = p
			}
		}
		if p != "" {
			return []string{p}
		}
	}
	return nil
}

// Read brings every agent's transcript up to date and returns what is
// known of those that have one. Tokens are summed over the session and
// its subagents; the rest comes from the session's own file. Files no
// agent uses any more are forgotten.
func (r *Reader) Read(agents []Agent) map[string]Info {
	out := map[string]Info{}
	live := map[string]bool{}
	var names map[string]string // codex thread names, read once per poll
	for _, a := range agents {
		paths := r.locate(a)
		if len(paths) == 0 {
			continue
		}
		var info Info
		for i, p := range paths {
			f := r.files[p]
			if f == nil {
				f = &file{kind: a.Kind}
				r.files[p] = f
			}
			live[p] = true
			f.update(p)
			info.Tokens = info.Tokens.add(f.totals)
			if i == 0 {
				info.Title, info.Prompt, info.Tool, info.Quota = f.title(), f.prompt, f.tool, f.quota
			}
		}
		if a.Kind == "codex" {
			if names == nil {
				names = r.threadNames()
			}
			if n := names[a.Session]; n != "" {
				info.Title = n
			}
		}
		out[a.ID] = info
	}
	for p := range r.files {
		if !live[p] {
			delete(r.files, p)
		}
	}
	return out
}

// file is how far one transcript has been read and what it said.
type file struct {
	kind   string
	offset int64 // end of the last complete line read
	totals Totals
	// byID is claude's usage per message id: a message is logged once
	// per content block, each line repeating the usage of the whole.
	byID map[string]Totals

	custom, ai, first string // titles by source; first is the first prompt
	prompt, tool      string
	quota             status.Quota
}

func (f *file) title() string {
	for _, t := range []string{f.custom, f.ai, f.first} {
		if t != "" {
			return t
		}
	}
	return ""
}

func (f *file) reset() {
	*f = file{kind: f.kind}
}

// update reads whatever was appended since the last call. A file that
// shrank was rewritten and is read again from the start.
func (f *file) update(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	if fi.Size() < f.offset {
		f.reset()
	}
	if fi.Size() == f.offset {
		return
	}
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer fh.Close()
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReaderSize(fh, 256<<10)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return // a partial last line waits for its newline
		}
		f.offset += int64(len(line))
		switch f.kind {
		case "claude":
			f.claudeLine(line)
		case "codex":
			f.codexLine(line)
		}
	}
}

func (f *file) asked(text string) {
	if text == "" {
		return
	}
	if f.first == "" {
		f.first = text
	}
	f.prompt, f.tool = text, ""
}

// toolLabel names a call: the tool, then what it was pointed at.
func toolLabel(name string, input map[string]json.RawMessage) string {
	for _, k := range []string{"description", "file_path", "command", "pattern", "url", "query"} {
		raw, ok := input[k]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			var parts []string // codex logs commands as argv
			if json.Unmarshal(raw, &parts) != nil {
				continue
			}
			s = strings.Join(parts, " ")
		}
		if k == "file_path" {
			s = filepath.Base(s)
		}
		s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
		if s != "" {
			return name + " · " + textutil.Truncate(s, 80)
		}
	}
	return name
}

var (
	usageKey      = []byte(`"usage"`)
	tokenCountKey = []byte(`"token_count"`)
)

func (f *file) claudeLine(line []byte) {
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
		return
	}
	switch ln.Type {
	case "ai-title":
		f.ai = ln.AITitle
	case "custom-title":
		f.custom = ln.CustomTitle
	case "user":
		if !ln.IsMeta {
			f.asked(discover.UserText(ln.Message.Content))
		}
	case "assistant":
		var parts []struct {
			Type  string                     `json:"type"`
			Name  string                     `json:"name"`
			Input map[string]json.RawMessage `json:"input"`
		}
		if json.Unmarshal(ln.Message.Content, &parts) == nil {
			for _, p := range parts {
				if p.Type == "tool_use" && p.Name != "" {
					f.tool = toolLabel(p.Name, p.Input)
				}
			}
		}
		if u := ln.Message.Usage; u != nil && bytes.Contains(line, usageKey) {
			t := Totals{Input: u.Input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output}
			if id := ln.Message.ID; id != "" {
				if f.byID == nil {
					f.byID = map[string]Totals{}
				}
				f.totals = f.totals.sub(f.byID[id]) // the last line for a message wins
				f.byID[id] = t
			}
			f.totals = f.totals.add(t)
		}
	}
}

// codexLine reads codex's rollout: the running token total it logs after
// every response, what the user typed, and the function calls it makes.
func (f *file) codexLine(line []byte) {
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
		return
	}
	p := ln.Payload
	if ln.Type == "event_msg" && p.Type == "token_count" && p.RateLimits != nil {
		at, _ := time.Parse(time.RFC3339Nano, ln.Timestamp)
		f.quota = status.Quota{At: at}
		if w := p.RateLimits.Primary; w != nil {
			f.quota.FiveHour = w.window(at)
		}
		if w := p.RateLimits.Secondary; w != nil {
			f.quota.SevenDay = w.window(at)
		}
	}
	switch {
	case ln.Type == "event_msg" && p.Type == "token_count" && p.Info != nil && bytes.Contains(line, tokenCountKey):
		// codex counts cached tokens inside input_tokens.
		u := p.Info.Total
		input := u.Input - u.CacheRead - u.CacheWrite
		if input < 0 {
			input = 0
		}
		f.totals = Totals{Input: input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output}
	case ln.Type == "response_item" && p.Type == "message" && p.Role == "user":
		f.asked(discover.UserText(p.Content))
	case ln.Type == "response_item" && (p.Type == "function_call" || p.Type == "custom_tool_call") && p.Name != "":
		var args map[string]json.RawMessage
		json.Unmarshal([]byte(p.Arguments), &args)
		f.tool = toolLabel(p.Name, args)
	}
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
