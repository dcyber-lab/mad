package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
)

// mcpCall is one session of mad mcp for agent id, as an agent's client
// has it: the handshake, then a call; its reply's text ("ERROR …" when
// the call failed), or the server's name for the handshake alone.
func mcpCall(t *testing.T, id, tool, args string) string {
	t.Helper()
	in := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"
	if tool != "" {
		in += fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args) + "\n"
	}
	code, out, errs := run(t, in, "mcp", "--agent", id)
	if code != 0 {
		t.Fatalf("mad mcp = %d %q", code, errs)
	}
	text := ""
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var r struct {
			ID     int
			Result struct {
				Content    []struct{ Text string }
				IsError    bool
				ServerInfo struct{ Name string }
			}
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("reply %q: %v", line, err)
		}
		switch {
		case tool == "" && r.ID == 0:
			text = r.Result.ServerInfo.Name
		case r.ID == 1 && len(r.Result.Content) > 0:
			text = r.Result.Content[0].Text
			if r.Result.IsError {
				text = "ERROR " + text
			}
		}
	}
	return text
}

// mad mcp's bus tools, as the agent the server was started for.
func TestMCP(t *testing.T) {
	isolate(t)
	const arch, build = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: arch, Kind: "claude", Name: "architect"}, {ID: build, Kind: "codex", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ tool, args, want string }{
		{"", "", "mad"},
		{"bus_who", `{}`, `"label": "builder"`},
		{"bus_channel", `{"action":"create","channel":"API redesign","topic":"v2","members":["builder"]}`, `"channel": "api-redesign"`},
		{"bus_post", `{"channel":"api-redesign","text":"@builder return 404 for an unknown id, see design.md"}`, `"builder"`},
		{"bus_post", `{"channel":"api-redesign","text":"ok"}`, "ERROR "},
		{"bus_dm", `{"to":"builder","text":"the tests live in handler_test.go"}`, `"channel": "dm.`},
		{"bus_dm", `{"to":"architect","text":"talking to myself here"}`, "ERROR that is you"},
		{"bus_dm", `{"to":"you","text":"the handler is done, please look"}`, `.you"`},
		{"bus_read", `{"channel":"builder"}`, "handler_test.go"},
		{"bus_read", `{"channel":"nowhere"}`, "ERROR no channel"},
	} {
		if got := mcpCall(t, arch, c.tool, c.args); !strings.Contains(got, c.want) {
			t.Errorf("%s %s: %q, want it to have %q", c.tool, c.args, got, c.want)
		}
	}
	// The builder has one bus item waiting: the post and the DM in one turn.
	q, err := queue.Load(build)
	if err != nil || len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("builder's queue: %+v %v", q, err)
	}
}
