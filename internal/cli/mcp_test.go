package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
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

// bus_handoff and bus_done: to an agent that exists, and to a kind, which
// starts one (drive.Spawn faked) until the chain has started its share.
func TestMCPHandoff(t *testing.T) {
	isolate(t)
	const arch, build = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: arch, Kind: "claude", Name: "architect", Dir: "/code/a/sub"}, {ID: build, Kind: "codex", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	var specs []drive.Spec
	old := spawnAgent
	spawnAgent = func(s drive.Spec, _ []agent.Kind) (*state.Agent, error) {
		specs = append(specs, s)
		a := &state.Agent{ID: fmt.Sprintf("cccc3333-0000-0000-0000-00000000000%d", len(specs)), Kind: s.Kind, Name: s.Name}
		err := state.Update(func(st *state.State) error {
			pr, _ := st.AddProject("/code/a")
			pr.Agents = append(pr.Agents, a)
			return nil
		})
		return a, err
	}
	t.Cleanup(func() { spawnAgent = old })

	got := mcpCall(t, arch, "bus_handoff", `{"to":"builder","task":"add the 404 case","context":"see design.md","files":["a.go"]}`)
	var res struct {
		Handoff string
		To      struct {
			ID, Label string
			Spawned   bool
		}
		Status string
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil || !strings.HasPrefix(res.Handoff, "h-") || res.To.Label != "builder" || res.To.Spawned || res.Status != "open" || len(specs) != 0 {
		t.Fatalf("bus_handoff = %q %v", got, err)
	}
	if q, _ := queue.Load(build); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("builder's queue: %+v", q)
	}
	if got := mcpCall(t, arch, "bus_handoff", `{"to":"architect","task":"to myself"}`); !strings.HasPrefix(got, "ERROR ") {
		t.Errorf("handoff to self: %q", got)
	}
	if got := mcpCall(t, arch, "bus_handoff", `{"to":"nobody-here","task":"to no one"}`); !strings.HasPrefix(got, "ERROR ") {
		t.Errorf("handoff to unknown: %q", got)
	}

	// Only the receiver finishes it.
	if got := mcpCall(t, arch, "bus_done", fmt.Sprintf(`{"handoff":%q,"result":"mine"}`, res.Handoff)); !strings.HasPrefix(got, "ERROR ") {
		t.Errorf("sender finished it: %q", got)
	}
	got = mcpCall(t, build, "bus_done", fmt.Sprintf(`{"handoff":%q,"result":"404 returned; tests pass"}`, res.Handoff))
	if !strings.Contains(got, `"status": "done"`) || !strings.Contains(got, `"architect"`) {
		t.Errorf("bus_done = %q", got)
	}
	if q, _ := queue.Load(arch); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("architect's queue: %+v", q)
	}

	// A kind starts an agent in the sender's directory; two per chain,
	// then no more.
	got = mcpCall(t, arch, "bus_handoff", `{"to":"codex:gpt-5","task":"write the changelog"}`)
	if err := json.Unmarshal([]byte(got), &res); err != nil || !res.To.Spawned || !strings.HasPrefix(res.To.Label, "codex-") || len(specs) != 1 {
		t.Fatalf("spawning handoff = %q %v specs %+v", got, err, specs)
	}
	if s := specs[0]; s.Kind != "codex" || s.Model != "gpt-5" || s.Dir != "/code/a/sub" || s.Name != res.To.Label {
		t.Errorf("spec = %+v", s)
	}
	if hs, _ := bus.Handoffs(); !hs[len(hs)-1].Spawned || hs[len(hs)-1].To != res.To.ID {
		t.Errorf("handoffs = %+v", hs)
	}
	// The spawned agent hands on to another kind: the chain's second.
	if got := mcpCall(t, res.To.ID, "bus_handoff", `{"to":"claude","task":"review the changelog"}`); !strings.Contains(got, `"spawned": true`) || len(specs) != 2 {
		t.Fatalf("second spawn = %q", got)
	}
	third := "cccc3333-0000-0000-0000-000000000002"
	if got := mcpCall(t, third, "bus_handoff", `{"to":"claude","task":"one agent too many"}`); !strings.HasPrefix(got, "ERROR ") || !strings.Contains(got, "started 2 agents") || len(specs) != 2 {
		t.Errorf("third spawn = %q (specs %d)", got, len(specs))
	}
	if got := mcpCall(t, arch, "bus_handoff", `{"to":"not-a-kind","task":"nothing to start"}`); !strings.HasPrefix(got, "ERROR ") || len(specs) != 2 {
		t.Errorf("unknown kind = %q", got)
	}
}
