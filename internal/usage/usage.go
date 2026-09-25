// Package usage counts the tokens an agent's session has consumed, read
// from the transcript the agent writes as it goes. The agent itself is
// never asked: the files are followed from where the last read stopped,
// so a poll costs a stat per file and a parse of what was appended.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/dcyber-lab/mad/internal/discover"
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
	// locate is replaceable in tests.
	locate func(a Agent) []string
}

func NewReader() *Reader {
	r := &Reader{files: map[string]*file{}, codex: map[string]string{}}
	r.locate = r.transcripts
	return r
}

// transcripts finds an agent's files. A claude session is looked up each
// time: its subagents come and go. A codex rollout never moves, so the
// search through the dated directories is done once.
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

// Read brings every agent's transcript up to date and returns the totals
// of those that have one. Files no agent uses any more are forgotten.
func (r *Reader) Read(agents []Agent) map[string]Totals {
	out := map[string]Totals{}
	live := map[string]bool{}
	for _, a := range agents {
		paths := r.locate(a)
		if len(paths) == 0 {
			continue
		}
		var sum Totals
		for _, p := range paths {
			f := r.files[p]
			if f == nil {
				f = &file{kind: a.Kind}
				r.files[p] = f
			}
			live[p] = true
			f.update(p)
			sum = sum.add(f.totals)
		}
		out[a.ID] = sum
	}
	for p := range r.files {
		if !live[p] {
			delete(r.files, p)
		}
	}
	return out
}

// file is how far one transcript has been read and what it added up to.
type file struct {
	kind   string
	offset int64 // end of the last complete line read
	totals Totals
	// byID is claude's usage per message id: a message is logged once
	// per content block, each line repeating the usage of the whole.
	byID map[string]Totals
}

// update reads whatever was appended since the last call. A file that
// shrank was rewritten and is read again from the start.
func (f *file) update(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	if fi.Size() < f.offset {
		f.offset, f.totals, f.byID = 0, Totals{}, nil
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

var (
	usageKey      = []byte(`"usage"`)
	tokenCountKey = []byte(`"token_count"`)
)

func (f *file) claudeLine(line []byte) {
	if !bytes.Contains(line, usageKey) {
		return
	}
	var ln struct {
		Type    string `json:"type"`
		Message struct {
			ID    string `json:"id"`
			Usage *struct {
				Input      int64 `json:"input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				Output     int64 `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ln) != nil || ln.Type != "assistant" || ln.Message.Usage == nil {
		return
	}
	u := ln.Message.Usage
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

// codexLine takes the running total codex logs after every response.
func (f *file) codexLine(line []byte) {
	if !bytes.Contains(line, tokenCountKey) {
		return
	}
	var ln struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
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
	if json.Unmarshal(line, &ln) != nil || ln.Type != "event_msg" || ln.Payload.Type != "token_count" || ln.Payload.Info == nil {
		return
	}
	// codex counts cached tokens inside input_tokens.
	u := ln.Payload.Info.Total
	input := u.Input - u.CacheRead - u.CacheWrite
	if input < 0 {
		input = 0
	}
	f.totals = Totals{Input: input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Output: u.Output}
}
