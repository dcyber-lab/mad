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
	// Input is a call's input made readable: a command, a diff. Args is
	// the input itself, a JSON object, its long strings cut: what the
	// window draws each tool from, Input where it does not know the tool.
	Input string          `json:"input,omitempty"`
	Args  json.RawMessage `json:"args,omitempty"`
	// Images are what the user sent (EntryUser) or a call gave back
	// (EntryResult): a screenshot, a file read.
	Images []Image `json:"images,omitempty"`
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

// Image is one an entry shows. Its data stays here: who serves the
// entries gives each a URL to fetch it by.
type Image struct {
	Mime string `json:"mime"`
	URL  string `json:"url,omitempty"`
	Data string `json:"-"` // base64
}

// contentImages are the images among a message's parts, in order:
// claude's {type: image, source: {data, media_type}}, pi's {type: image,
// data, mimeType}, codex's {type: input_image, image_url: a data URL}.
func contentImages(raw json.RawMessage) []Image {
	var parts []struct {
		Type   string `json:"type"`
		Source struct {
			Data      string `json:"data"`
			MediaType string `json:"media_type"`
		} `json:"source"`
		Data     string          `json:"data"`
		MimeType string          `json:"mimeType"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	var out []Image
	for _, p := range parts {
		var img Image
		switch p.Type {
		case "image":
			img = Image{Mime: p.Source.MediaType, Data: p.Source.Data}
			if img.Data == "" {
				img = Image{Mime: p.MimeType, Data: p.Data}
			}
		case "input_image":
			var url string
			if json.Unmarshal(p.ImageURL, &url) != nil {
				var o struct{ URL string }
				_ = json.Unmarshal(p.ImageURL, &o)
				url = o.URL
			}
			if m := dataURLRe.FindStringSubmatch(url); m != nil {
				img = Image{Mime: m[1], Data: url[len(m[0]):]}
			}
		}
		if img.Data == "" {
			continue
		}
		if img.Mime == "" {
			img.Mime = sniffImage(img.Data)
		}
		out = append(out, img)
	}
	return out
}

// partsShown is what a message's parts show: their text, and their
// images, which then stand for themselves in it.
func partsShown(raw json.RawMessage) (string, []Image) {
	text, imgs := resultText(raw), contentImages(raw)
	if len(imgs) > 0 {
		text = strings.TrimSpace(strings.ReplaceAll(text, "(image)", ""))
	}
	return text, imgs
}

var dataURLRe = regexp.MustCompile(`^data:([\w/+.-]+);base64,`)

// sniffImage tells an image's type by how its base64 starts.
func sniffImage(data string) string {
	for prefix, mime := range map[string]string{"iVBOR": "image/png", "/9j/": "image/jpeg", "R0lGOD": "image/gif", "UklGR": "image/webp"} {
		if strings.HasPrefix(data, prefix) {
			return mime
		}
	}
	return ""
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
	return shownN(s, maxShown)
}

// shownN is s cut to its first n bytes, saying how much more there was.
func shownN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
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
				text, imgs := partsShown(b.Content)
				out = append(out, Entry{Kind: EntryResult, ID: b.ToolUseID, Text: shown(text), Error: b.IsError, Images: imgs, At: at})
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
			e.Args = toolArgs(b.Input)
		}
		out = append(out, e)
	}
	if ln.Type == "user" {
		out = withSent(out, contentImages(ln.Message.Content), at)
	}
	return out
}

// withSent puts the images the user sent with a message (imgs) with its
// words, the last of out's; alone if it had none.
func withSent(out []Entry, imgs []Image, at time.Time) []Entry {
	if len(imgs) == 0 {
		return out
	}
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Kind == EntryUser {
			out[i].Images = append(out[i].Images, imgs...)
			return out
		}
	}
	return append(out, Entry{Kind: EntryUser, Images: imgs, At: at})
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
// ToolText is how a call to tool name with input (JSON) shows: what it is
// pointed at, and its input, as the conversation shows calls.
func ToolText(name string, input json.RawMessage) (label, text string) {
	var in map[string]json.RawMessage
	_ = json.Unmarshal(input, &in)
	label = strings.TrimPrefix(strings.TrimPrefix(toolLabel(name, in), name), " · ")
	return label, shown(toolInput(in))
}

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

// ToolArgs is how a call's input (JSON) is an entry's Args.
func ToolArgs(input json.RawMessage) json.RawMessage {
	var in map[string]json.RawMessage
	_ = json.Unmarshal(input, &in)
	return toolArgs(in)
}

// maxArg bounds each string in a call's Args: a file written whole, say.
const maxArg = 20000

// toolArgs is a call's input as Args: each string longer than maxArg cut
// to its head, nil when there is none.
func toolArgs(in map[string]json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	var cut func(v any) any
	cut = func(v any) any {
		switch v := v.(type) {
		case string:
			return shownN(v, maxArg)
		case []any:
			for i := range v {
				v[i] = cut(v[i])
			}
		case map[string]any:
			for k := range v {
				v[k] = cut(v[k])
			}
		}
		return v
	}
	out := make(map[string]any, len(in))
	for k, raw := range in {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			out[k] = cut(v)
		}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(out) != nil {
		return nil
	}
	return bytes.TrimSpace(b.Bytes())
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
		// codex hands the model its context as user messages too. An image
		// sent has a tag in the text naming it.
		text, imgs := partsShown(p.Content)
		text = strings.TrimSpace(codexImageTagRe.ReplaceAllString(text, ""))
		if text == "" && len(imgs) == 0 || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "# AGENTS.md") {
			return nil
		}
		return []Entry{{Kind: EntryUser, Text: text, Images: imgs, At: at}}
	case p.Type == "message" && p.Role == "assistant":
		return []Entry{{Kind: EntryText, Text: resultText(p.Content), Part: p.ID, At: at}}
	case p.Type == "reasoning":
		return []Entry{{Kind: EntryOther, Part: p.ID, At: at}}
	case p.Type == "function_call":
		var args map[string]json.RawMessage
		_ = json.Unmarshal([]byte(p.Arguments), &args)
		label := strings.TrimPrefix(strings.TrimPrefix(toolLabel(p.Name, args), p.Name), " · ")
		return []Entry{{Kind: EntryTool, Tool: p.Name, Label: label, Input: shown(toolInput(args)), Args: toolArgs(args), ID: p.CallID, Part: p.ID, At: at}}
	case p.Type == "custom_tool_call":
		// A script for exec, a patch: its input is that text.
		input, _ := json.Marshal(p.Input)
		args := toolArgs(map[string]json.RawMessage{"input": input})
		return []Entry{{Kind: EntryTool, Tool: p.Name, Label: codexLabel(p.Input), Input: shown(p.Input), Args: args, ID: p.CallID, Part: p.ID, At: at}}
	case strings.HasSuffix(p.Type, "_call_output"):
		text, imgs := partsShown(p.Output)
		return []Entry{{Kind: EntryResult, ID: p.CallID, Text: shown(text), Images: imgs, At: at}}
	case strings.HasSuffix(p.Type, "_call"):
		return []Entry{{Kind: EntryTool, Tool: p.Type, ID: p.CallID, Part: p.ID, At: at}}
	}
	return nil
}

var (
	responseItemKey = []byte(`"response_item"`)
	codexCmdRe      = regexp.MustCompile(`"(?:cmd|command)"\s*:\s*("(?:[^"\\]|\\.)*")`)
	codexImageTagRe = regexp.MustCompile(`</?image\b[^>]*>`)
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
