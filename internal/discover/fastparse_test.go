package discover

import (
	"testing"
	"time"
)

// The lines claude.Parse reads without parsing them read as parsing
// would: a prompt typed as a string keeps its text, a tool result carries
// only its time, and a tool result with text of the user's is parsed.
func TestClaudeParseSkips(t *testing.T) {
	at := "2026-11-01T13:25:40.295Z"
	want, _ := time.Parse(time.RFC3339Nano, at)
	for _, c := range []struct {
		line, prompt string
	}{
		{`{"parentUuid":"p","type":"user","message":{"role":"user","content":"fix the bug"},"timestamp":"` + at + `"}`, "fix the bug"},
		{`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"ok \"timestamp\":\"x\""}]},"timestamp":"` + at + `"}`, ""},
		{`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"done"},{"type":"text","text":"and now this"}]},"timestamp":"` + at + `"}`, "and now this"},
	} {
		e, ok := Lookup("claude").Parse([]byte(c.line + "\n"))
		if !ok || e.Prompt != c.prompt || !e.At.Equal(want) {
			t.Errorf("%s: ok %v, prompt %q, at %v; want prompt %q at %v", c.line, ok, e.Prompt, e.At, c.prompt, want)
		}
	}
	if _, ok := Lookup("claude").Parse([]byte(`{"type":"user","isMeta":true,"message":{"content":[{"type":"tool_result","content":"x"}]},"timestamp":"` + at + `"}`)); ok {
		t.Error("a meta line was read")
	}
}
