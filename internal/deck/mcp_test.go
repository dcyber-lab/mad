package deck

import (
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// An agent starts with mad's MCP server, told who it is.
func TestMCPFlags(t *testing.T) {
	self := paths.Self()
	// Which deck this is goes with it: codex passes little of its own
	// environment on to MCP servers.
	t.Setenv("MAD_SOCKET", "mw")
	t.Setenv("XDG_STATE_HOME", "/tmp/s")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if got, want := MCPFlags("claude", "a1b2"), `--mcp-config '{"mcpServers":{"mad":{"args":["mcp","--agent","a1b2"],"command":"`+self+`","env":{"HOME":"/h","MAD_SOCKET":"mw","XDG_STATE_HOME":"/tmp/s"}}}}'`; got != want {
		t.Errorf("claude:\n%s\nwant\n%s", got, want)
	}
	if got, want := MCPFlags("codex", "a1b2"), `-c 'mcp_servers.mad.command="`+self+`"' -c 'mcp_servers.mad.args=["mcp","--agent","a1b2"]' -c 'mcp_servers.mad.env={HOME="/h",MAD_SOCKET="mw",XDG_STATE_HOME="/tmp/s"}'`; got != want {
		t.Errorf("codex:\n%s\nwant\n%s", got, want)
	}
	for _, c := range [][2]string{{"shell", "a1b2"}, {"pi", "a1b2"}, {"claude", "x; rm -rf /"}} {
		if got := MCPFlags(c[0], c[1]); got != "" {
			t.Errorf("%v: %q", c, got)
		}
	}
	// In claude's command, before --session-id: --mcp-config takes several.
	a := &state.Agent{ID: "a1b2", Kind: "claude"}
	cmd := agent.ByName(agent.Builtin(), "claude").Command(a, false, agent.Launch{Vars: map[string]string{"{mad_mcp}": MCPFlags("claude", a.ID)}})
	if !strings.Contains(cmd, "--mcp-config '") || strings.Index(cmd, "--mcp-config") > strings.Index(cmd, "--session-id") {
		t.Errorf("claude's command: %s", cmd)
	}
	if cmd := agent.ByName(agent.Builtin(), "claude").Command(a, false, agent.Launch{}); strings.Contains(cmd, "mad_mcp") || strings.Contains(cmd, "  ") {
		t.Errorf("without it: %q", cmd)
	}
}
