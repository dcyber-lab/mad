package host

import (
	"bytes"
	"testing"

	"github.com/dcyber-lab/mad/internal/state"
)

// A server that takes an answer and ends the turn at once: the end is read
// before answer has come back from writing it. The agent is idle then, not
// running a turn that already ended.

// echoWriter hands each line written to it to react, before Write returns:
// the server answering as fast as it can.
type echoWriter struct {
	react func(line []byte)
}

func (w *echoWriter) Write(p []byte) (int, error) {
	w.react(bytes.TrimSpace(p))
	return len(p), nil
}

func TestCodexTurnEndsAtOnceOnAnswer(t *testing.T) {
	record(t)
	h := newHost("cx-fast")
	c := newCodex(h, &state.Agent{ID: "cx-fast", Kind: "codex"}, t.TempDir())
	h.d = c
	c.in = &echoWriter{react: func([]byte) {
		c.line([]byte(`{"method":"turn/completed","params":{"turn":{"id":"u1","status":"completed"}}}`))
	}}
	close(c.started)
	c.line([]byte(`{"method":"turn/started","params":{"turn":{"id":"u1"}}}`))
	c.line([]byte(`{"id":7,"method":"item/commandExecution/requestApproval","params":{"itemId":"e1","command":"ls"}}`))
	s := h.State()
	if s.Status != Waiting || len(s.Requests) != 1 {
		t.Fatalf("before the answer: %+v", s)
	}
	if err := c.answer(Answer{ID: s.Requests[0].ID, Allow: true}); err != nil {
		t.Fatal(err)
	}
	if s := h.State(); s.Status != Idle || len(s.Requests) != 0 {
		t.Errorf("after the turn ended: status %s, requests %+v", s.Status, s.Requests)
	}
}

func TestClaudeTurnEndsAtOnceOnAnswer(t *testing.T) {
	record(t)
	h := newHost("cl-fast")
	c := newClaude(h, "true", t.TempDir())
	h.d = c
	c.in = &echoWriter{react: func([]byte) {
		c.line([]byte(`{"type":"result","subtype":"success","is_error":false}`))
	}}
	close(c.started)
	c.line([]byte(`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"/w/a"}}}`))
	s := h.State()
	if s.Status != Waiting || len(s.Requests) != 1 {
		t.Fatalf("before the answer: %+v", s)
	}
	if err := c.answer(Answer{ID: "r1", Allow: true}); err != nil {
		t.Fatal(err)
	}
	if s := h.State(); s.Status != Idle || len(s.Requests) != 0 {
		t.Errorf("after the turn ended: status %s, requests %+v", s.Status, s.Requests)
	}
}
