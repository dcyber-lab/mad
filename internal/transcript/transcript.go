// Package transcript follows what an agent writes about its own session:
// the tokens it consumed, the title it gave the conversation, the last
// thing the user asked and the tool it is running. The agent itself is
// never asked: the files are followed from where the last read stopped,
// so a poll costs a stat per file and a parse of what was appended. Where
// the files are and what a line means is up to the kind's provider (see
// discover.Provider); this package keeps the running sums.
package transcript

import (
	"bufio"
	"io"
	"os"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/status"
)

// Totals is what a session has consumed so far, in tokens. It has the
// fields of discover.Tokens, so either converts to the other.
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
	Kind    string // kinds without a provider have no transcript
	Dir     string // where it runs
	Session string // its current session id
}

// Reader follows transcripts across polls. It is not safe for concurrent
// use: one poll at a time.
type Reader struct {
	files map[string]*file // transcript path → progress
	// locate and titles are replaceable in tests.
	locate func(a Agent) []string
	titles func(p discover.Provider) map[string]string
}

func NewReader() *Reader {
	return &Reader{files: map[string]*file{}, locate: transcripts, titles: discover.Provider.Titles}
}

// transcripts finds an agent's files through its kind's provider.
func transcripts(a Agent) []string {
	if p := discover.Lookup(a.Kind); p != nil {
		return p.Transcripts(a.Dir, a.Session)
	}
	return nil
}

// Followed reports whether agents of kind write a transcript mad can read.
func Followed(kind string) bool { return discover.Lookup(kind) != nil }

// Read brings every agent's transcript up to date and returns what is
// known of those that have one. Tokens are summed over the session and
// its subagents; the rest comes from the session's own file. Files no
// agent uses any more are forgotten.
func (r *Reader) Read(agents []Agent) map[string]Info {
	out := map[string]Info{}
	live := map[string]bool{}
	titles := map[string]map[string]string{} // by kind, read once per poll
	for _, a := range agents {
		p := discover.Lookup(a.Kind)
		if p == nil {
			continue
		}
		paths := r.locate(a)
		if len(paths) == 0 {
			continue
		}
		var info Info
		for i, path := range paths {
			f := r.files[path]
			if f == nil {
				f = &file{}
				r.files[path] = f
			}
			live[path] = true
			f.update(path, p)
			info.Tokens = info.Tokens.add(f.totals)
			if i == 0 {
				info.Title, info.Prompt, info.Tool, info.Quota = f.title(), f.prompt, f.tool, f.quota
			}
		}
		names, ok := titles[a.Kind]
		if !ok {
			names = r.titles(p)
			titles[a.Kind] = names
		}
		if n := names[a.Session]; n != "" {
			info.Title = n
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
	offset int64 // end of the last complete line read
	totals Totals
	// byMessage is the usage per response, for agents that log one more
	// than once (the last line for it wins).
	byMessage map[string]Totals

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

// update reads whatever was appended since the last call. A file that
// shrank was rewritten and is read again from the start.
func (f *file) update(path string, p discover.Provider) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	if fi.Size() < f.offset {
		*f = file{}
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
		if e, ok := p.Parse(line); ok {
			f.apply(e)
		}
	}
}

func (f *file) apply(e discover.Event) {
	if e.Prompt != "" {
		if f.first == "" {
			f.first = e.Prompt
		}
		f.prompt, f.tool = e.Prompt, ""
	}
	if e.Tool != "" {
		f.tool = e.Tool
	}
	switch e.TitleSource {
	case discover.TitleCustom:
		f.custom = e.Title
	case discover.TitleAI:
		f.ai = e.Title
	}
	if e.Total != nil {
		f.totals = Totals(*e.Total)
	}
	if e.Usage != nil {
		t := Totals(*e.Usage)
		if id := e.Message; id != "" {
			if f.byMessage == nil {
				f.byMessage = map[string]Totals{}
			}
			f.totals = f.totals.sub(f.byMessage[id])
			f.byMessage[id] = t
		}
		f.totals = f.totals.add(t)
	}
	if e.Quota != nil {
		f.quota = *e.Quota
	}
}
