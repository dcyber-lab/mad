package discover

import (
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

// Session is a past conversation, from the CLI or a desktop app, that can
// be resumed as a deck agent.
type Session struct {
	Kind    string // its provider's
	ID      string
	Title   string
	Cwd     string
	Origin  string // cli | desktop
	Updated time.Time
}

// MaxSessions caps how many sessions a project lists.
const MaxSessions = 40

var (
	reminderRe   = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	sessionCache sync.Map // path → cachedFile
)

// ProjectSessions lists resumable sessions of kind for a project root,
// newest first, including sessions that ran in its agent worktrees.
// Sessions started by programs rather than a person (desktop workflows,
// -p/SDK runs, codex subagents) are left out. Kinds without a provider
// have none.
func ProjectSessions(root, kind string) []Session {
	p := Lookup(kind)
	if p == nil {
		return nil
	}
	out := p.Sessions(root)
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

// cachedFile is a session file as parsed at one mtime; s is nil for files
// that aren't a person's session.
type cachedFile struct {
	t time.Time
	s *Session
}

// cachedSession parses f unless it was parsed at the same mtime. One entry
// per file: a file being written replaces its own entry. The Session is
// shared; callers copy it before changing it.
func cachedSession(f fileEntry, parse func(string) *Session) *Session {
	if v, ok := sessionCache.Load(f.path); ok && v.(cachedFile).t.Equal(f.t) {
		return v.(cachedFile).s
	}
	s := parse(f.path)
	if s != nil {
		s.Updated = f.t
	}
	sessionCache.Store(f.path, cachedFile{f.t, s})
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
