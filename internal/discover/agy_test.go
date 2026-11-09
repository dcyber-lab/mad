package discover

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// withAgy fakes a home, with agy installed or not.
func withAgy(t *testing.T, installed bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := agyLookPath
	agyLookPath = func(string) (string, error) {
		if installed {
			return "/usr/local/bin/agy", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { agyLookPath = old })
	return home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

// Setup adds mad's entry to agy's hooks and MCP servers, keeping the
// user's own, and writes nothing when there is nothing to change.
func TestAgySetup(t *testing.T) {
	home := withAgy(t, true)
	cfg := filepath.Join(home, ".gemini", "config")
	os.MkdirAll(cfg, 0o755)
	os.WriteFile(filepath.Join(cfg, "hooks.json"), []byte(`{"mine": {"Stop": [{"type": "command", "command": "say done"}]}}`), 0o644)
	os.WriteFile(filepath.Join(cfg, "mcp_config.json"), []byte(`{"mcpServers": {"github": {"command": "gh-mcp"}}, "other": 1}`), 0o644)
	if err := (agy{}).Setup(false); err != nil {
		t.Fatal(err)
	}
	hooks := readJSON(t, filepath.Join(cfg, "hooks.json"))
	if hooks["mine"] == nil {
		t.Error("the user's hook was dropped")
	}
	mad, _ := hooks["mad"].(map[string]any)
	for _, ev := range []string{"SessionStart", "PreInvocation", "PreToolUse", "Stop"} {
		if mad[ev] == nil {
			t.Errorf("no %s hook: %v", ev, mad)
		}
	}
	stop := mad["Stop"].([]any)[0].(map[string]any)
	if cmd := stop["command"].(string); !strings.HasPrefix(cmd, `[ -z "$MAD_AGENT_ID" ] || exec `) || !strings.HasSuffix(cmd, " hook agy stop") {
		t.Errorf("stop command = %q", cmd)
	}
	tool := mad["PreToolUse"].([]any)[0].(map[string]any)
	if tool["matcher"] != "*" || tool["hooks"] == nil {
		t.Errorf("PreToolUse = %v", tool)
	}
	mcp := readJSON(t, filepath.Join(cfg, "mcp_config.json"))
	servers := mcp["mcpServers"].(map[string]any)
	if servers["github"] == nil || mcp["other"] == nil {
		t.Errorf("the user's servers were dropped: %v", mcp)
	}
	if m, _ := servers["mad"].(map[string]any); m == nil || len(m["args"].([]any)) != 1 || m["args"].([]any)[0] != "mcp" {
		t.Errorf("mad server = %v", servers["mad"])
	}

	// Again: nothing to change, nothing written.
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(cfg, "hooks.json"), past, past)
	(agy{}).Setup(false)
	if info, _ := os.Stat(filepath.Join(cfg, "hooks.json")); !info.ModTime().Equal(past) {
		t.Error("hooks.json rewritten with nothing changed")
	}
}

// A file agy reads but that isn't plain JSON is left alone, and without
// agy nothing is written at all.
func TestAgySetupLeavesAlone(t *testing.T) {
	home := withAgy(t, true)
	cfg := filepath.Join(home, ".gemini", "config")
	os.MkdirAll(cfg, 0o755)
	commented := "{\n  // mine\n  \"mcpServers\": {},\n}\n"
	os.WriteFile(filepath.Join(cfg, "mcp_config.json"), []byte(commented), 0o644)
	if err := (agy{}).Setup(false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(cfg, "mcp_config.json")); string(data) != commented {
		t.Errorf("commented config rewritten:\n%s", data)
	}

	home = withAgy(t, false)
	if err := (agy{}).Setup(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini")); err == nil {
		t.Error("config written without agy installed")
	}
}

func agyHook(event, payload string) Report {
	return (agy{}).Hook([]string{event}, strings.NewReader(payload), nil, time.Now())
}

// Payloads as agy sends them (captured shapes).
func TestAgyHook(t *testing.T) {
	const common = `"conversationId":"ec33ebf9","workspacePaths":["/w"],"transcriptPath":"/t/transcript.jsonl"`
	r := agyHook("session-start", `{`+common+`,"modelName":"gemini-3.1-pro-low"}`)
	if r.Hook == nil || r.Hook.State != status.Idle || r.Hook.SessionID != "ec33ebf9" {
		t.Errorf("session-start: %+v", r.Hook)
	}
	r = agyHook("pre-invocation", `{"invocationNum":0,"initialNumSteps":1,`+common+`,"lastUserInput":"fix the test"}`)
	if r.Hook == nil || r.Hook.State != status.Running || len(r.Prompts) != 1 || r.Prompts[0] != "fix the test" {
		t.Errorf("first pre-invocation: %+v %q", r.Hook, r.Prompts)
	}
	if r = agyHook("pre-invocation", `{"invocationNum":3,`+common+`,"lastUserInput":"fix the test"}`); len(r.Prompts) != 0 {
		t.Errorf("later invocation gave the prompt again: %q", r.Prompts)
	}
	r = agyHook("pre-tool-use", `{"toolCall":{"name":"run_command","args":{"CommandLine":"npm test"}},"stepIdx":19,`+common+`}`)
	if r.Hook == nil || r.Hook.State != status.Running || r.Hook.SessionID != "ec33ebf9" {
		t.Errorf("pre-tool-use: %+v", r.Hook)
	}
	if r = agyHook("pre-tool-use", `not json`); r.Hook != nil {
		t.Errorf("bad payload: %+v", r.Hook)
	}

	// Stop reports nothing itself: it starts the finish, only when agy is
	// fully idle.
	var started []string
	old := agyStartFinish
	agyStartFinish = func(sid, transcript string, _ time.Time) { started = append(started, sid+" "+transcript) }
	t.Cleanup(func() { agyStartFinish = old })
	if r = agyHook("stop", `{"executionNum":1,"terminationReason":"model_stop","fullyIdle":false,`+common+`}`); r.Hook != nil || r.Turn != nil || len(started) != 0 {
		t.Errorf("stop, not idle: %+v %v", r, started)
	}
	if r = agyHook("stop", `{"executionNum":1,"terminationReason":"model_stop","fullyIdle":true,`+common+`}`); r.Hook != nil || r.Turn != nil {
		t.Errorf("stop reported before the transcript: %+v", r)
	}
	if len(started) != 1 || started[0] != "ec33ebf9 /t/transcript.jsonl" {
		t.Errorf("finish started = %q", started)
	}
}

const agyTranscriptSample = `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-05-20T23:46:29Z","content":"<USER_REQUEST>\nread a.txt\nand say what it says\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: 2026-05-20T19:46:29-04:00.\n</ADDITIONAL_METADATA>"}
{"step_index":1,"source":"SYSTEM","type":"CONVERSATION_HISTORY","status":"DONE","created_at":"2026-05-20T23:46:29Z"}
{"step_index":2,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-05-20T23:46:29Z","tool_calls":[{"name":"run_command","args":{"CommandLine":"\"cat a.txt\"","Cwd":"\"/w\"","toolAction":"\"Reading a.txt\""}}]}
{"step_index":3,"source":"MODEL","type":"RUN_COMMAND","status":"DONE","created_at":"2026-05-20T23:46:31Z","content":"hello"}
{"step_index":4,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-05-20T23:46:33Z","content":"It says hello."}
`

// The reply is the model's last words after the last thing asked, read
// once agy has written the transcript.
func TestAgyFinish(t *testing.T) {
	withAgy(t, true)
	dir := filepath.Dir(agyTranscript("ec33ebf9"))
	os.MkdirAll(dir, 0o755)
	path := agyTranscript("ec33ebf9")
	os.WriteFile(path, []byte(agyTranscriptSample), 0o644)

	r := agyFinish("ec33ebf9", "", time.Now().Add(-time.Second), time.Second)
	if r.Hook == nil || r.Hook.Event != "Stop" || r.Hook.State != status.Idle || r.Hook.SessionID != "ec33ebf9" {
		t.Errorf("hook = %+v", r.Hook)
	}
	if r.Turn == nil || r.Turn.Reply != "It says hello." || r.Turn.SessionID != "ec33ebf9" {
		t.Errorf("turn = %+v", r.Turn)
	}

	// agy may write the reply before the Stop hook runs: a transcript that
	// ends in it is read at once, however old.
	start := time.Now()
	r = agyFinish("ec33ebf9", path, time.Now().Add(time.Hour), 5*time.Second)
	if time.Since(start) > time.Second || r.Turn == nil || r.Turn.Reply != "It says hello." {
		t.Errorf("finish waited on a written reply: %v %+v", time.Since(start), r.Turn)
	}

	// A later question with no answer yet: no reply, not the old one.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"step_index":5,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"<USER_REQUEST>\nmore\n</USER_REQUEST>"}` + "\n")
	f.Close()
	if got := agyReply(path); got != "" {
		t.Errorf("reply after a new question = %q", got)
	}

	// A transcript not written since the stop is waited for, then read as
	// it is.
	start = time.Now()
	r = agyFinish("ec33ebf9", path, time.Now().Add(time.Hour), 300*time.Millisecond)
	if time.Since(start) < 300*time.Millisecond || r.Turn == nil {
		t.Errorf("finish did not wait: %v %+v", time.Since(start), r.Turn)
	}

	// Through Hook, as `mad hook agy finish` runs it.
	r = (agy{}).Hook([]string{"finish", "ec33ebf9", path, "0"}, nil, nil, time.Now())
	if r.Turn == nil || r.Hook == nil {
		t.Errorf("finish via Hook: %+v", r)
	}
}

func TestAgyTranscript(t *testing.T) {
	withAgy(t, true)
	p := agy{}
	if got := p.Transcripts("/w", "ec33ebf9"); got != nil {
		t.Errorf("transcripts before any = %q", got)
	}
	path := agyTranscript("ec33ebf9")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(agyTranscriptSample), 0o644)
	if got := p.Transcripts("/w", "ec33ebf9"); len(got) != 1 || got[0] != path {
		t.Errorf("transcripts = %q", got)
	}
	if got := p.Transcripts("/w", ""); got != nil {
		t.Errorf("transcripts of no session = %q", got)
	}

	var prompts, tools []string
	for _, line := range strings.Split(strings.TrimSpace(agyTranscriptSample), "\n") {
		e, ok := p.Parse([]byte(line))
		if !ok {
			continue
		}
		if e.At.IsZero() {
			t.Errorf("no time: %s", line)
		}
		if e.Prompt != "" {
			prompts = append(prompts, e.Prompt)
		}
		if e.Tool != "" {
			tools = append(tools, e.Tool)
		}
	}
	if len(prompts) != 1 || prompts[0] != "read a.txt" {
		t.Errorf("prompts = %q", prompts)
	}
	if len(tools) != 1 || tools[0] != "run_command · cat a.txt" {
		t.Errorf("tools = %q", tools)
	}
	if _, ok := p.Parse([]byte(`garbage`)); ok {
		t.Error("garbage parsed")
	}
}

func TestAgyToolLabel(t *testing.T) {
	raw := func(m map[string]string) map[string]json.RawMessage {
		out := map[string]json.RawMessage{}
		for k, v := range m {
			out[k], _ = json.Marshal(v)
		}
		return out
	}
	cases := []struct {
		name string
		args map[string]json.RawMessage
		want string
	}{
		{"run_command", raw(map[string]string{"CommandLine": "go test ./...\nmore"}), "run_command · go test ./..."},
		{"write_to_file", raw(map[string]string{"TargetFile": `"/w/main.go"`}), "write_to_file · /w/main.go"},
		{"list_permissions", raw(map[string]string{"toolAction": `"Listing current permissions"`}), "list_permissions · Listing current permissions"},
		{"browser", nil, "browser"},
		{"", nil, ""},
	}
	for _, c := range cases {
		if got := agyToolLabel(c.name, c.args); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// agy agents are offered no sessions to continue, and are never taken
// for ones running outside the deck.
func TestAgyProvider(t *testing.T) {
	if Lookup("agy") == nil {
		t.Fatal("no agy provider")
	}
	if ListsSessions("agy") || !ListsSessions("claude") || !ListsSessions("codex") || ListsSessions("pi") {
		t.Error("ListsSessions wrong")
	}
	if !(agy{}).Launched("agy --conversation x") {
		t.Error("agy taken for one outside the deck")
	}
}
