package discover

import (
	"strings"
	"testing"

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
		h := parseClaudeHook(strings.NewReader(c.in))
		switch {
		case c.state == "" && h != nil:
			t.Errorf("%s: want ignored, got %+v", c.in, h)
		case c.state != "" && (h == nil || h.State != c.state):
			t.Errorf("%s: got %+v, want state %s", c.in, h, c.state)
		}
	}
	if h := parseClaudeHook(strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"s1"}`)); h.SessionID != "s1" {
		t.Errorf("session id not kept: %+v", h)
	}
}

func TestCodexHook(t *testing.T) {
	h := parseCodexHook(`{"type":"agent-turn-complete","thread-id":"t-1","last-assistant-message":"done"}`)
	if h == nil || h.State != status.Idle || h.SessionID != "t-1" {
		t.Errorf("got %+v", h)
	}
	for _, in := range []string{`{"type":"other"}`, `nope`} {
		if h := parseCodexHook(in); h != nil {
			t.Errorf("%s: want nil, got %+v", in, h)
		}
	}
}
