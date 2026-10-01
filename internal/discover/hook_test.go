package discover

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

func TestClaudeHook(t *testing.T) {
	cases := []struct {
		in    string
		state string // "" = ignored
	}{
		{`{"hook_event_name":"SessionStart","session_id":"s1"}`, status.Idle},
		{`{"hook_event_name":"UserPromptSubmit"}`, status.Running},
		{`{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, status.Running},
		{`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion"}`, status.Waiting},
		{`{"hook_event_name":"PreToolUse","tool_name":"ExitPlanMode"}`, status.Waiting},
		{`{"hook_event_name":"PostToolUse"}`, status.Running},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, status.Waiting},
		{`{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}`, status.Waiting},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, status.Idle},
		{`{"hook_event_name":"Notification","message":"Claude is waiting for your input"}`, status.Idle},
		{`{"hook_event_name":"Notification","message":"something else"}`, ""},
		{`{"hook_event_name":"Stop"}`, status.Idle},
		{`{"hook_event_name":"SubagentStop"}`, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		h := parseClaudeHook(strings.NewReader(c.in)).Hook
		switch {
		case c.state == "" && h != nil:
			t.Errorf("%s: want ignored, got %+v", c.in, h)
		case c.state != "" && (h == nil || h.State != c.state):
			t.Errorf("%s: got %+v, want state %s", c.in, h, c.state)
		}
	}
	if h := parseClaudeHook(strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"s1"}`)).Hook; h.SessionID != "s1" {
		t.Errorf("session id not kept: %+v", h)
	}
	// Only Stop ends a turn, with what claude said last.
	if turn := parseClaudeHook(strings.NewReader(`{"hook_event_name":"PostToolUse"}`)).Turn; turn != nil {
		t.Errorf("PostToolUse ended a turn: %+v", turn)
	}
	turn := parseClaudeHook(strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1","last_assistant_message":"pong"}`)).Turn
	if turn == nil || turn.Reply != "pong" || turn.SessionID != "s1" {
		t.Errorf("Stop: turn = %+v", turn)
	}
	if p := parseClaudeHook(strings.NewReader(`{"hook_event_name":"UserPromptSubmit","prompt":"F2 is fine"}`)).Prompts; len(p) != 1 || p[0] != "F2 is fine" {
		t.Errorf("prompt = %q", p)
	}
	// What a tool is about to write or run, and how claude is told no.
	tool := parseClaudeHook(strings.NewReader(`{"hook_event_name":"PreToolUse","cwd":"/w","tool_name":"Write","tool_input":{"file_path":"a/b.go"}}`)).Tool
	if tool == nil || tool.Name != "Write" || tool.Path != "/w/a/b.go" {
		t.Errorf("Write: %+v", tool)
	}
	tool = parseClaudeHook(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`)).Tool
	if tool == nil || tool.Command != "go test ./..." || tool.Path != "" {
		t.Errorf("Bash: %+v", tool)
	}
	var out strings.Builder
	r := claude{}.Hook(nil, strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"/x"}}`), &out, time.Now())
	r.Tool.Deny("not here")
	if want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"not here"}}`; strings.TrimSpace(out.String()) != want {
		t.Errorf("deny = %s", out.String())
	}
}

func TestCodexHook(t *testing.T) {
	h, turn, inputs := parseCodexHook(`{"type":"agent-turn-complete","thread-id":"t-1","last-assistant-message":"done","input-messages":["[mad run x · review] …","also check y"]}`)
	if len(inputs) != 2 || inputs[1] != "also check y" {
		t.Errorf("inputs = %q", inputs)
	}
	if h == nil || h.State != status.Idle || h.SessionID != "t-1" {
		t.Errorf("got %+v", h)
	}
	if turn == nil || turn.Reply != "done" || turn.SessionID != "t-1" {
		t.Errorf("turn = %+v", turn)
	}
	for _, in := range []string{`{"type":"other"}`, `nope`} {
		if h, turn, _ := parseCodexHook(in); h != nil || turn != nil {
			t.Errorf("%s: want nil, got %+v %+v", in, h, turn)
		}
	}
}
