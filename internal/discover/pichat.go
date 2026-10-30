package discover

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Conversations reads a kind's sessions as conversations (Chat): where
// they are, and what a line says.
type Conversations interface {
	Transcripts(dir, sid string) []string
	Chatter
}

// Conversation returns how to read kind's sessions as conversations: its
// provider's, or, for pi, which has none (no history, sync or usage), a
// reader of its sessions for that alone.
func Conversation(kind string) (Conversations, bool) {
	if kind == "pi" {
		return piChat{}, true
	}
	c, ok := Lookup(kind).(Conversations)
	return c, ok
}

// piChat reads pi's sessions: <agent dir>/sessions/--<cwd>--/<time>_<id>.jsonl,
// one entry a line, a message in each "message" entry.
type piChat struct{}

func piAgentDir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	return filepath.Join(paths.Home(), ".pi", "agent")
}

// Transcripts finds session sid in any directory's folder: mad starts pi
// with --session-id, so the id is the agent's own.
func (piChat) Transcripts(_, sid string) []string {
	if sid == "" {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(piAgentDir(), "sessions", "*", "*_"+sid+".jsonl"))
	if len(matches) == 0 {
		return nil
	}
	return matches[:1]
}

var piMessageKey = []byte(`"message"`)

// Chat: what the user typed or ran (! commands), and the agent's messages,
// a part an entry, in order, under the message's timestamp, which is what
// streams of it (package live) are known by.
func (piChat) Chat(line []byte) []Entry {
	if !bytes.Contains(line, piMessageKey) {
		return nil
	}
	var ln struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		Message   struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			Timestamp  int64           `json:"timestamp"`
			ToolCallID string          `json:"toolCallId"`
			IsError    bool            `json:"isError"`
			Command    string          `json:"command"`
			Output     string          `json:"output"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ln) != nil || ln.Type != "message" {
		return nil
	}
	m, at := ln.Message, ln.Timestamp
	switch m.Role {
	case "user":
		if text := resultText(m.Content); text != "" {
			return []Entry{{Kind: EntryUser, Text: text, At: at}}
		}
	case "bashExecution":
		return []Entry{
			{Kind: EntryCommand, Label: "! " + m.Command, At: at},
			{Kind: EntryResult, Text: shown(m.Output), At: at},
		}
	case "toolResult":
		return []Entry{{Kind: EntryResult, ID: m.ToolCallID, Text: shown(resultText(m.Content)), Error: m.IsError, At: at}}
	case "assistant":
		var blocks []struct {
			Type      string                     `json:"type"`
			Text      string                     `json:"text"`
			ID        string                     `json:"id"`
			Name      string                     `json:"name"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(m.Content, &blocks)
		msg := strconv.FormatInt(m.Timestamp, 10)
		var out []Entry
		for _, b := range blocks {
			e := Entry{Kind: EntryOther, Msg: msg, At: at}
			switch b.Type {
			case "text":
				e.Kind, e.Text = EntryText, b.Text
			case "toolCall":
				label := strings.TrimPrefix(strings.TrimPrefix(toolLabel(b.Name, b.Arguments), b.Name), " · ")
				e.Kind, e.Tool, e.Label, e.Input, e.ID = EntryTool, b.Name, label, shown(toolInput(b.Arguments)), b.ID
			}
			out = append(out, e)
		}
		return out
	}
	return nil
}
