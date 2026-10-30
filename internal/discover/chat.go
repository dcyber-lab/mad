package discover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/dcyber-lab/mad/internal/textutil"
)

// Entry is one thing a conversation shows: what mad desktop draws of an
// agent's transcript in place of its terminal.
type Entry struct {
	Kind string `json:"kind"` // EntryUser, EntryText, ...
	// Text is markdown for EntryUser and EntryText, plain output for
	// EntryResult.
	Text string `json:"text,omitempty"`
	// Tool and Label name a call (EntryTool): the tool, and what it was
	// pointed at ("go test ./..."). Label is what an EntryCommand ran.
	Tool  string `json:"tool,omitempty"`
	Label string `json:"label,omitempty"`
	// Input is a call's input made readable: a command, a diff.
	Input string `json:"input,omitempty"`
	// ID pairs a call with its result.
	ID    string `json:"id,omitempty"`
	Error bool   `json:"error,omitempty"`
	// Msg and Part line an entry up with what was streamed of it (package
	// live). Msg is the id of the agent's message it is a part of, where
	// each part has an entry, in order, one not shown too (EntryOther):
	// claude. Part is the part's own id, where it has one: codex.
	Msg  string    `json:"msg,omitempty"`
	Part string    `json:"part,omitempty"`
	At   time.Time `json:"at"`
}

// Entry kinds.
const (
	EntryUser    = "user"    // the user said it
	EntryText    = "text"    // the agent said it
	EntryTool    = "tool"    // the agent called a tool
	EntryResult  = "result"  // what a call, or a command, gave back
	EntryCommand = "command" // the user ran a command: /model, ! ls
	EntryOther   = "other"   // a part of the agent's message not shown: thinking
)

// Chatter is a provider whose transcripts read as a conversation.
type Chatter interface {
	// Chat reads one transcript line; nil when it shows nothing of it.
	Chat(line []byte) []Entry
}

// maxShown bounds a call's input or output as shown: its head.
const maxShown = 6000

func shown(s string) string {
	if len(s) <= maxShown {
		return s
	}
	cut := maxShown
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n… %d more bytes", len(s)-cut)
}

var (
	messageKey = []byte(`"message"`)
	tagRe      = regexp.MustCompile(`(?s)<([a-z][a-z-]*)>(.*?)</[a-z][a-z-]*>`)
)

// Chat: the user's messages and commands, the agent's text and tool
// calls with what they returned. Thinking, subagents' lines (they have
// files of their own) and what claude adds for itself are left out.
func (claude) Chat(line []byte) []Entry {
	if !bytes.Contains(line, messageKey) {
		return nil // most of what is not a message is big: attachments
	}
	var ln struct {
		Type             string    `json:"type"`
		Timestamp        time.Time `json:"timestamp"`
		IsMeta           bool      `json:"isMeta"`
		IsSidechain      bool      `json:"isSidechain"`
		IsCompactSummary bool      `json:"isCompactSummary"`
		Message          struct {
			ID      string          `json:"id"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ln) != nil || ln.IsMeta || ln.IsSidechain || ln.IsCompactSummary {
		return nil
	}
	if ln.Type != "user" && ln.Type != "assistant" {
		return nil
	}
	at := ln.Timestamp
	var text string
	if json.Unmarshal(ln.Message.Content, &text) == nil {
		if ln.Type == "user" {
			return claudeUserChat(text, at)
		}
		return nil
	}
	var blocks []struct {
		Type      string                     `json:"type"`
		Text      string                     `json:"text"`
		ID        string                     `json:"id"`
		Name      string                     `json:"name"`
		Input     map[string]json.RawMessage `json:"input"`
		ToolUseID string                     `json:"tool_use_id"`
		Content   json.RawMessage            `json:"content"`
		IsError   bool                       `json:"is_error"`
	}
	if json.Unmarshal(ln.Message.Content, &blocks) != nil {
		return nil
	}
	var out []Entry
	for _, b := range blocks {
		if ln.Type == "user" {
			switch b.Type {
			case "text":
				out = append(out, claudeUserChat(b.Text, at)...)
			case "tool_result":
				out = append(out, Entry{Kind: EntryResult, ID: b.ToolUseID, Text: shown(resultText(b.Content)), Error: b.IsError, At: at})
			}
			continue
		}
		e := Entry{Kind: EntryOther, Msg: ln.Message.ID, At: at}
		switch b.Type {
		case "text":
			e.Kind, e.Text = EntryText, b.Text
		case "tool_use":
			label := strings.TrimPrefix(strings.TrimPrefix(toolLabel(b.Name, b.Input), b.Name), " · ")
			e.Kind, e.Tool, e.Label, e.Input, e.ID = EntryTool, b.Name, label, shown(toolInput(b.Input)), b.ID
		}
		out = append(out, e)
	}
	return out
}

// claudeUserChat reads a user line's text. Claude logs the commands the
// user ran, and their output, as tagged text; other tagged text (a
// notification, a hook's output) is not the user's.
func claudeUserChat(text string, at time.Time) []Entry {
	text = strings.TrimSpace(reminderRe.ReplaceAllString(text, ""))
	if text == "" || strings.HasPrefix(text, "Caveat:") {
		return nil
	}
	if !strings.HasPrefix(text, "<") {
		return []Entry{{Kind: EntryUser, Text: text, At: at}}
	}
	tags := map[string]string{}
	for _, m := range tagRe.FindAllStringSubmatch(text, -1) {
		tags[m[1]] = strings.TrimSpace(m[2])
	}
	switch {
	case tags["command-name"] != "":
		return []Entry{{Kind: EntryCommand, Label: strings.TrimSpace(tags["command-name"] + " " + tags["command-args"]), At: at}}
	case tags["bash-input"] != "":
		return []Entry{{Kind: EntryCommand, Label: "! " + tags["bash-input"], At: at}}
	}
	var out []string
	for _, k := range []string{"local-command-stdout", "bash-stdout", "bash-stderr"} {
		if tags[k] != "" {
			out = append(out, ansi.Strip(tags[k]))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return []Entry{{Kind: EntryResult, Text: shown(strings.Join(out, "\n")), At: at}}
}

// resultText is what a call returned: its text, or the text of its parts.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text", "input_text", "output_text":
			out = append(out, p.Text)
		case "image":
			out = append(out, "(image)")
		}
	}
	return strings.Join(out, "\n")
}

// toolInput shows a call's input: the command it runs, the change it
// makes, else its arguments.
func toolInput(in map[string]json.RawMessage) string {
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(in[k], &s)
		return s
	}
	file := str("file_path") + str("path") // claude's, pi's
	switch {
	case str("command") != "":
		return "$ " + str("command")
	case in["old_string"] != nil:
		return file + "\n" + marked(str("old_string"), "- ") + "\n" + marked(str("new_string"), "+ ")
	case in["oldText"] != nil:
		return file + "\n" + marked(str("oldText"), "- ") + "\n" + marked(str("newText"), "+ ")
	case in["content"] != nil && file != "":
		return file + "\n" + str("content")
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(in)
	return strings.TrimSpace(b.String())
}

// marked starts each of s's lines with mark.
func marked(s, mark string) string {
	return mark + strings.ReplaceAll(s, "\n", "\n"+mark)
}

// Chat: what the user typed, the agent's messages (commentary and answer)
// and its tool calls with their output. Reasoning is not shown but has an
// entry. Each of the agent's parts carries its item id, as streamed.
func (codex) Chat(line []byte) []Entry {
	if !bytes.Contains(line, responseItemKey) {
		return nil
	}
	var ln struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		Payload   struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			Name      string          `json:"name"`
			Arguments string          `json:"arguments"`
			Input     string          `json:"input"`
			CallID    string          `json:"call_id"`
			Output    json.RawMessage `json:"output"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &ln) != nil || ln.Type != "response_item" {
		return nil
	}
	p, at := ln.Payload, ln.Timestamp
	switch {
	case p.Type == "message" && p.Role == "user":
		// codex hands the model its context as user messages too.
		text := strings.TrimSpace(resultText(p.Content))
		if text == "" || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "# AGENTS.md") {
			return nil
		}
		return []Entry{{Kind: EntryUser, Text: text, At: at}}
	case p.Type == "message" && p.Role == "assistant":
		return []Entry{{Kind: EntryText, Text: resultText(p.Content), Part: p.ID, At: at}}
	case p.Type == "reasoning":
		return []Entry{{Kind: EntryOther, Part: p.ID, At: at}}
	case p.Type == "function_call":
		var args map[string]json.RawMessage
		_ = json.Unmarshal([]byte(p.Arguments), &args)
		label := strings.TrimPrefix(strings.TrimPrefix(toolLabel(p.Name, args), p.Name), " · ")
		return []Entry{{Kind: EntryTool, Tool: p.Name, Label: label, Input: shown(toolInput(args)), ID: p.CallID, Part: p.ID, At: at}}
	case p.Type == "custom_tool_call":
		return []Entry{{Kind: EntryTool, Tool: p.Name, Label: codexLabel(p.Input), Input: shown(p.Input), ID: p.CallID, Part: p.ID, At: at}}
	case strings.HasSuffix(p.Type, "_call_output"):
		return []Entry{{Kind: EntryResult, ID: p.CallID, Text: shown(resultText(p.Output)), At: at}}
	case strings.HasSuffix(p.Type, "_call"):
		return []Entry{{Kind: EntryTool, Tool: p.Type, ID: p.CallID, Part: p.ID, At: at}}
	}
	return nil
}

var (
	responseItemKey = []byte(`"response_item"`)
	codexCmdRe      = regexp.MustCompile(`"(?:cmd|command)"\s*:\s*("(?:[^"\\]|\\.)*")`)
	patchFileRe     = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)
)

// codexLabel says what a custom tool call (a script for exec, a patch) is
// pointed at: the command it runs, the file it changes, else its first
// line.
func codexLabel(input string) string {
	if m := codexCmdRe.FindStringSubmatch(input); m != nil {
		var cmd string
		if json.Unmarshal([]byte(m[1]), &cmd) == nil {
			return textutil.Truncate(strings.SplitN(cmd, "\n", 2)[0], 80)
		}
	}
	if m := patchFileRe.FindStringSubmatch(input); m != nil {
		return textutil.Truncate(m[1], 80)
	}
	return textutil.Truncate(strings.TrimSpace(strings.SplitN(input, "\n", 2)[0]), 80)
}
