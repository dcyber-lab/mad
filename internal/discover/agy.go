package discover

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
)

// agy is Google's Antigravity CLI. Its conversations are
// ~/.gemini/antigravity-cli/brain/<conversation id>/, the transcript under
// .system_generated/logs/transcript.jsonl: one step per line (the user's
// input, the model's response and the tools it calls, their results).
//
// agy takes no settings or hooks on its command line: it reads hooks from
// ~/.gemini/config/hooks.json and MCP servers from
// ~/.gemini/config/mcp_config.json, files the user's own agy shares. mad
// adds an entry named "mad" to each and leaves the rest alone. Its hook
// does nothing outside a deck (no MAD_AGENT_ID), and its MCP server serves
// no tools there.
//
// agy picks its conversation's id itself: mad learns it from the first
// hook, and resumes it with --conversation. Its status is read from the
// screen, as codex's is; the hooks only say which conversation it is and
// when a turn ended, with what reply. Its transcripts carry no token
// counts and mad doesn't list its sessions, so it has no usage, picker or
// sync.
type agy struct{}

func (agy) Kind() string { return "agy" }

func agyHome() string      { return filepath.Join(paths.Home(), ".gemini", "antigravity-cli") }
func agyConfigDir() string { return filepath.Join(paths.Home(), ".gemini", "config") }

// agyEntry is the name of mad's entry in agy's hooks.json and
// mcp_config.json.
const agyEntry = "mad"

// agyLookPath finds agy; replaceable in tests.
var agyLookPath = exec.LookPath

// Setup writes mad's hooks and MCP server into agy's global config, when
// agy is installed. A file that isn't plain JSON (agy reads comments in
// some) is left as it is, and nothing here stops the deck from starting.
func (agy) Setup(bool) error {
	if _, err := agyLookPath("agy"); err != nil {
		return nil
	}
	_ = mergeAgyConfig(filepath.Join(agyConfigDir(), "hooks.json"), "", agyEntry, agyHooks())
	_ = mergeAgyConfig(filepath.Join(agyConfigDir(), "mcp_config.json"), "mcpServers", agyEntry, map[string]any{
		"command": paths.Self(), "args": []string{"mcp"},
	})
	return nil
}

// agyHooks is mad's entry in hooks.json: each event runs `mad hook agy
// <event>`, only in an agent the deck started. agy's payloads don't name
// their event, so the command does. Tool events take a matcher.
func agyHooks() map[string]any {
	cmd := func(event string) map[string]any {
		return map[string]any{
			"type":    "command",
			"command": `[ -z "$MAD_AGENT_ID" ] || exec ` + paths.ShellQuote(paths.Self()) + " hook agy " + event,
			"timeout": 10,
		}
	}
	return map[string]any{
		"SessionStart":  []any{cmd("session-start")},
		"PreInvocation": []any{cmd("pre-invocation")},
		"PreToolUse":    []any{map[string]any{"matcher": "*", "hooks": []any{cmd("pre-tool-use")}}},
		"Stop":          []any{cmd("stop")},
	}
}

// mergeAgyConfig sets entry name of the JSON object in file (under key
// section when one is given) to value, keeping everything else. It writes
// only when that changes the entry.
func mergeAgyConfig(file, section, name string, value any) error {
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case len(bytes.TrimSpace(data)) > 0:
		if err := json.Unmarshal(data, &top); err != nil {
			return err
		}
	}
	entries := top
	if section != "" {
		entries = map[string]json.RawMessage{}
		if raw, ok := top[section]; ok && json.Unmarshal(raw, &entries) != nil {
			return errors.New(file + ": " + section + " is not an object")
		}
	}
	want, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if old, ok := entries[name]; ok && sameJSON(old, want) {
		return nil
	}
	entries[name] = want
	if section != "" {
		raw, _ := json.Marshal(entries)
		top[section] = raw
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return paths.WriteFileAtomic(file, append(out, '\n'))
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}

func (agy) Placeholders() map[string]string { return nil }

// agyPayload is what agy passes every hook on stdin, with the fields of
// the events mad listens to.
type agyPayload struct {
	ConversationID string `json:"conversationId"`
	TranscriptPath string `json:"transcriptPath"`
	LastUserInput  string `json:"lastUserInput"`
	InvocationNum  int    `json:"invocationNum"`
	FullyIdle      bool   `json:"fullyIdle"`
}

// Hook takes `mad hook agy <event>` with agy's payload on stdin, or
// `finish <conversation> <transcript> <unix nanos>`, which Stop starts on
// the side (see agyFinish). It prints nothing: agy reads no answer from
// these events.
func (agy) Hook(args []string, stdin io.Reader, _ io.Writer, now time.Time) Report {
	if len(args) == 0 {
		return Report{}
	}
	if args[0] == "finish" && len(args) == 4 {
		ns, _ := strconv.ParseInt(args[3], 10, 64)
		return agyFinish(args[1], args[2], time.Unix(0, ns), agyFinishFor)
	}
	var ev agyPayload
	data, _ := io.ReadAll(stdin)
	if json.Unmarshal(data, &ev) != nil {
		return Report{}
	}
	h := &status.Hook{Event: args[0], SessionID: ev.ConversationID}
	switch args[0] {
	case "session-start":
		h.State = status.Idle
	case "pre-invocation":
		// Before every call to the model: the first of a turn follows the
		// message it answers.
		h.State = status.Running
		r := Report{Hook: h}
		if ev.InvocationNum == 0 && strings.TrimSpace(ev.LastUserInput) != "" {
			r.Prompts = []string{ev.LastUserInput}
		}
		return r
	case "pre-tool-use":
		h.State = status.Running
	case "stop":
		// Not fully idle: background work goes on, and the turn with it.
		// The transcript, which holds the reply, is written only after
		// this hook returns: the turn is reported from the side, once
		// it is.
		if ev.FullyIdle && ev.ConversationID != "" {
			agyStartFinish(ev.ConversationID, ev.TranscriptPath, now)
		}
		return Report{}
	default:
		return Report{}
	}
	return Report{Hook: h}
}

// agyFinishFor bounds how long a turn's reply is waited for.
var agyFinishFor = 20 * time.Second

// agyStartFinish starts `mad hook agy finish` in a session of its own, so
// it outlives the hook agy waits for. It inherits MAD_AGENT_ID.
var agyStartFinish = func(sid, transcript string, stopped time.Time) {
	cmd := exec.Command(paths.Self(), "hook", "agy", "finish", sid, transcript, strconv.FormatInt(stopped.UnixNano(), 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// agyFinish reports the turn that ended at stopped: idle, with the reply
// read from the transcript once agy has written it there (or as it stands
// after wait). agy writes it around the Stop hook, before or after: the
// turn is over once the transcript ends in the model's reply, or was
// written since the stop.
func agyFinish(sid, transcript string, stopped time.Time, wait time.Duration) Report {
	if transcript == "" {
		transcript = agyTranscript(sid)
	}
	for end := time.Now().Add(wait); ; time.Sleep(200 * time.Millisecond) {
		if _, done := agyLastReply(transcript); done {
			break
		}
		info, err := os.Stat(transcript)
		if err == nil && !info.ModTime().Before(stopped) || time.Now().After(end) {
			break
		}
	}
	return Report{
		Hook: &status.Hook{Event: "Stop", State: status.Idle, SessionID: sid},
		Turn: &status.Turn{Reply: agyReply(transcript), SessionID: sid},
	}
}

// agyStep is one line of agy's transcript.
type agyStep struct {
	Source    string `json:"source"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
	ToolCalls []struct {
		Name string                     `json:"name"`
		Args map[string]json.RawMessage `json:"args"`
	} `json:"tool_calls"`
}

// agyReply is what the model said last in the transcript's last turn.
func agyReply(transcript string) string {
	reply, _ := agyLastReply(transcript)
	return reply
}

// agyLastReply is agyReply, and whether the transcript ends in it: the
// model's last step said something and called no tool.
func agyLastReply(transcript string) (reply string, final bool) {
	f, err := os.Open(transcript)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var s agyStep
		if json.Unmarshal(sc.Bytes(), &s) != nil {
			continue
		}
		final = false
		switch {
		case s.Type == "USER_INPUT":
			reply = ""
		case s.Type == "PLANNER_RESPONSE" && strings.TrimSpace(s.Content) != "":
			reply = strings.TrimSpace(s.Content)
			final = len(s.ToolCalls) == 0
		}
	}
	return reply, final
}

func agyTranscript(sid string) string {
	return filepath.Join(agyHome(), "brain", sid, ".system_generated", "logs", "transcript.jsonl")
}

// agyArg reads one of a tool call's arguments as text. agy logs most of
// them encoded twice ("\"npm test\"").
func agyArg(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	var inner string
	if strings.HasPrefix(s, `"`) && json.Unmarshal([]byte(s), &inner) == nil {
		return inner
	}
	return s
}

// agyToolLabel names a tool call by what it is pointed at ("run_command ·
// go test ./..."), or the summary agy gives it.
func agyToolLabel(name string, args map[string]json.RawMessage) string {
	if name == "" {
		return ""
	}
	for _, k := range []string{"CommandLine", "TargetFile", "AbsolutePath", "DirectoryPath", "SearchPath", "Query", "Url", "toolAction"} {
		if s := strings.TrimSpace(agyArg(args[k])); s != "" {
			return name + " · " + textutil.Truncate(firstLine(s), 80)
		}
	}
	return name
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// agyRequest is the request in a user step, without what agy adds around
// it (the time, settings changes).
func agyRequest(content string) string {
	const open, closing = "<USER_REQUEST>", "</USER_REQUEST>"
	if i := strings.Index(content, open); i >= 0 {
		content = content[i+len(open):]
		if j := strings.Index(content, closing); j >= 0 {
			content = content[:j]
		}
	}
	return strings.TrimSpace(content)
}

func (agy) History(time.Time, func(string, time.Time, bool)) {}
func (agy) Sessions(string) []Session                        { return nil }

// ListsSessions: mad can't tell where an agy conversation ran, so it has
// none to offer when an agent is started.
func (agy) ListsSessions() bool { return false }

func (agy) Transcripts(_, sid string) []string {
	if sid == "" {
		return nil
	}
	path := agyTranscript(sid)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return []string{path}
}

func (agy) Written(time.Time) []string { return nil }

// Parse reads a transcript step: what the user asked, and the tool the
// model called.
func (agy) Parse(line []byte) (Event, bool) {
	var s agyStep
	if json.Unmarshal(line, &s) != nil {
		return Event{}, false
	}
	var e Event
	e.At, _ = time.Parse(time.RFC3339, s.CreatedAt)
	switch s.Type {
	case "USER_INPUT":
		if s.Source != "USER_EXPLICIT" {
			return Event{}, false
		}
		e.Prompt = firstLine(agyRequest(s.Content))
		return e, e.Prompt != ""
	case "PLANNER_RESPONSE":
		if len(s.ToolCalls) == 0 {
			return Event{}, false
		}
		e.Tool = agyToolLabel(s.ToolCalls[0].Name, s.ToolCalls[0].Args)
		return e, e.Tool != ""
	}
	return Event{}, false
}

func (agy) Titles() map[string]string { return nil }

// Launched: agy's command line carries nothing of mad's. Every agy is
// taken as the deck's, so none is offered as running elsewhere.
func (agy) Launched(string) bool { return true }

func (agy) Desktop(string) ([]string, bool)     { return nil, false }
func (agy) ProcessSession(int, []string) string { return "" }
func (agy) RecentSessions(string) []string      { return nil }
func (agy) HumanSession(string) bool            { return true }
