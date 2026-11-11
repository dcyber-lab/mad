package host

import (
	"encoding/json"
	"testing"

	"github.com/dcyber-lab/mad/internal/state"
)

// codex asks before an MCP tool call as an elicitation: a card, and the
// answer goes back as accept (for the session when allowed always) or decline.
func TestCodexMCPToolApproval(t *testing.T) {
	record(t)
	h := newHost("cx-mcp")
	c := newCodex(h, &state.Agent{ID: "cx-mcp", Kind: "codex"}, t.TempDir())
	h.d = c
	var wrote [][]byte
	c.in = &echoWriter{react: func(l []byte) { wrote = append(wrote, append([]byte(nil), l...)) }}
	close(c.started)
	c.line([]byte(`{"method":"turn/started","params":{"turn":{"id":"u1"}}}`))
	ask := `{"id":9,"method":"mcpServer/elicitation/request","params":{"serverName":"seatalk","threadId":"t","mode":"form",` +
		`"message":"Allow the seatalk MCP server to run tool \"send\"?","requestedSchema":{"type":"object","properties":{}},` +
		`"_meta":{"codex_approval_kind":"mcp_tool_call","persist":["session","always"],"tool_title":"send","tool_params":{"to":"x"}}}}`
	c.line([]byte(ask))
	s := h.State()
	if s.Status != Waiting || len(s.Requests) != 1 {
		t.Fatalf("no card: %+v", s)
	}
	r := s.Requests[0]
	if r.Tool != "seatalk · send" || !r.Always || r.Input == "" {
		t.Errorf("card: %+v", r)
	}
	if err := c.answer(Answer{ID: r.ID, Allow: true, Always: true}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		ID     int `json:"id"`
		Result struct {
			Action string         `json:"action"`
			Meta   map[string]any `json:"_meta"`
		} `json:"result"`
	}
	if len(wrote) == 0 || json.Unmarshal(wrote[len(wrote)-1], &got) != nil {
		t.Fatalf("wrote %q", wrote)
	}
	if got.ID != 9 || got.Result.Action != "accept" || got.Result.Meta["persist"] != "session" {
		t.Errorf("answer %s", wrote[len(wrote)-1])
	}

	// a form that asks for fields is declined, not left hanging
	c.line([]byte(`{"id":10,"method":"mcpServer/elicitation/request","params":{"serverName":"s","threadId":"t","mode":"form","message":"name?","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}}}}}`))
	if err := json.Unmarshal(wrote[len(wrote)-1], &got); err != nil || got.ID != 10 || got.Result.Action != "decline" {
		t.Errorf("form with fields: %s", wrote[len(wrote)-1])
	}
}
