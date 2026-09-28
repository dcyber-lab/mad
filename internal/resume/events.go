package resume

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
)

// Event kinds read from a transcript. Roles come from the transcript's
// own structure, never from text that looks like a role ("user: …").
const (
	You   = "you"   // a message you sent
	Agent = "agent" // text the agent wrote: a claim, not a fact
	Cmd   = "cmd"   // a shell command the agent ran, with its result if known
)

type Event struct {
	Kind string
	At   time.Time
	Text string // message text, or the command line
	// For commands: whether a result was read, and its exit code.
	Done bool
	Exit int
	// ExitKnown: the transcript states the outcome (Claude's is_error,
	// a codex exit code). Without it Done only means output came back.
	ExitKnown bool
	End       time.Time
	id        string
}

// IsTest reports whether a command runs tests, by its name.
func (e Event) IsTest() bool { return e.TestPart() != "" }

// TestPart is the part of the command that runs tests ("go test ./..."),
// or "" when none does.
func (e Event) TestPart() string {
	if e.Kind != Cmd {
		return ""
	}
	// Only where a command starts, on the first line: a script or heredoc
	// that mentions "go test" doesn't run it.
	line := quotedRe.ReplaceAllString(strings.SplitN(e.Text, "\n", 2)[0], "\"\"")
	for _, part := range cmdSplit.Split(line, -1) {
		part = strings.TrimSpace(part)
		for envRe.MatchString(part) {
			part = envRe.ReplaceAllString(part, "")
		}
		if testRe.MatchString(part) {
			return part
		}
	}
	return ""
}

var (
	cmdSplit = regexp.MustCompile(`&&|\|\||;|\|`)
	quotedRe = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	envRe    = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=\S*\s+`)
)

var testRe = regexp.MustCompile(`^(timeout \d+ )?(go test|pytest|cargo test|npm (run )?test|pnpm (run )?test|yarn test|jest|vitest|make test|mvn test|gradle test|bats)\b`)

// Read parses the transcript of kind at path into events, oldest first.
// Kinds without a reader give nil, ok false.
func Read(kind, path string) ([]Event, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var parse func(line []byte, add func(Event), result func(id string, exit int, known bool, at time.Time))
	switch kind {
	case "claude":
		parse = parseClaude
	case "codex":
		parse = parseCodex
	default:
		return nil, false
	}
	var evs []Event
	byID := map[string]int{}
	add := func(e Event) {
		if e.id != "" {
			byID[e.id] = len(evs)
		}
		evs = append(evs, e)
	}
	result := func(id string, exit int, known bool, at time.Time) {
		if i, ok := byID[id]; ok && !evs[i].Done {
			evs[i].Done, evs[i].Exit, evs[i].ExitKnown, evs[i].End = true, exit, known, at
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		parse(sc.Bytes(), add, result)
	}
	return evs, true
}

// fullText is a message's text, whole, or "" for what isn't a person's
// message (tool results, reminders, commands).
func fullText(raw json.RawMessage) string {
	if discover.UserText(raw) == "" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var parts []struct{ Type, Text string }
		_ = json.Unmarshal(raw, &parts)
		for _, p := range parts {
			if p.Type == "text" || p.Type == "input_text" {
				text += p.Text + "\n"
			}
		}
	}
	return strings.TrimSpace(text)
}

var exitRe = regexp.MustCompile(`(?i)(exit(ed)? (with )?code:? ?|"exit_code":)(-?\d+)`)

func parseClaude(line []byte, add func(Event), result func(string, int, bool, time.Time)) {
	var ln struct {
		Type      string    `json:"type"`
		IsMeta    bool      `json:"isMeta"`
		Sidechain bool      `json:"isSidechain"`
		At        time.Time `json:"timestamp"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ln) != nil || ln.IsMeta || ln.Sidechain {
		return
	}
	var parts []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		IsError   bool            `json:"is_error"`
		Content   json.RawMessage `json:"content"`
	}
	switch ln.Type {
	case "user":
		if json.Unmarshal(ln.Message.Content, &parts) == nil {
			tool := false
			for _, p := range parts {
				if p.Type == "tool_result" {
					tool = true
					exit := 0
					if p.IsError {
						exit = 1
						if m := exitRe.FindStringSubmatch(string(p.Content)); m != nil {
							exit, _ = strconv.Atoi(m[4])
						}
					}
					result(p.ToolUseID, exit, true, ln.At)
				}
			}
			if tool {
				return
			}
		}
		if t := fullText(ln.Message.Content); t != "" {
			add(Event{Kind: You, At: ln.At, Text: t})
		}
	case "assistant":
		if json.Unmarshal(ln.Message.Content, &parts) != nil {
			return
		}
		for _, p := range parts {
			switch {
			case p.Type == "text" && strings.TrimSpace(p.Text) != "":
				add(Event{Kind: Agent, At: ln.At, Text: strings.TrimSpace(p.Text)})
			case p.Type == "tool_use" && p.Name == "Bash":
				var in struct{ Command string }
				_ = json.Unmarshal(p.Input, &in)
				add(Event{Kind: Cmd, At: ln.At, Text: in.Command, id: p.ID})
			}
		}
	}
}

// parseCodex reads what codex's rollout files say. Their shape changes
// between versions; a command whose result isn't recognized stays
// without one rather than being guessed.
func parseCodex(line []byte, add func(Event), result func(string, int, bool, time.Time)) {
	var ln struct {
		At      time.Time `json:"timestamp"`
		Type    string    `json:"type"`
		Payload struct {
			Type      string          `json:"type"`
			Message   string          `json:"message"`
			Name      string          `json:"name"`
			Arguments string          `json:"arguments"`
			CallID    string          `json:"call_id"`
			Output    json.RawMessage `json:"output"`
			ExitCode  *int            `json:"exit_code"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &ln) != nil {
		return
	}
	p := ln.Payload
	switch p.Type {
	case "user_message":
		if t := strings.TrimSpace(p.Message); t != "" && !strings.HasPrefix(t, "<") {
			add(Event{Kind: You, At: ln.At, Text: t})
		}
	case "agent_message":
		if t := strings.TrimSpace(p.Message); t != "" {
			add(Event{Kind: Agent, At: ln.At, Text: t})
		}
	case "function_call":
		var args struct {
			Command json.RawMessage `json:"command"`
			Cmd     string          `json:"cmd"`
		}
		_ = json.Unmarshal([]byte(p.Arguments), &args)
		cmd := args.Cmd
		if cmd == "" {
			var s string
			var list []string
			if json.Unmarshal(args.Command, &s) == nil {
				cmd = s
			} else if json.Unmarshal(args.Command, &list) == nil {
				if len(list) == 3 && (list[0] == "bash" || list[0] == "sh" || list[0] == "zsh") && list[1] == "-lc" {
					cmd = list[2]
				} else {
					cmd = strings.Join(list, " ")
				}
			}
		}
		if cmd != "" {
			add(Event{Kind: Cmd, At: ln.At, Text: cmd, id: p.CallID})
		}
	case "exec_command_end":
		if p.ExitCode != nil {
			result(p.CallID, *p.ExitCode, true, ln.At)
		}
	case "function_call_output":
		if m := exitRe.FindStringSubmatch(string(p.Output)); m != nil {
			n, _ := strconv.Atoi(m[4])
			result(p.CallID, n, true, ln.At)
		} else {
			result(p.CallID, 0, false, ln.At)
		}
	}
}
