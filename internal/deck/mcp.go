package deck

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/dcyber-lab/mad/internal/paths"
)

// An agent talks on the bus (package bus) through mad's MCP server,
// `mad mcp --agent <id>`, which its kind starts with it. It is told who it
// is in its arguments, and which deck in an environment of its own, as
// codex passes few of its own on.

func mcpArgs(id string) []string { return []string{"mcp", "--agent", id} }

// mcpEnv is what says which deck this is (its socket, its state and
// config), as set here; none for the user's own.
func mcpEnv() map[string]string {
	env := map[string]string{}
	for _, k := range []string{"MAD_SOCKET", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "HOME"} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}
	return env
}

// agentIDRe is what an agent id looks like, to be put on a command line.
var agentIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// MCPFlags put mad's MCP server on agent id's command line: claude's
// --mcp-config (beside the user's own servers), codex's -c
// mcp_servers.mad; "" for other kinds.
func MCPFlags(kind, id string) string {
	if !agentIDRe.MatchString(id) {
		return ""
	}
	switch kind {
	case "claude":
		cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{
			"mad": map[string]any{"command": paths.Self(), "args": mcpArgs(id), "env": mcpEnv()},
		}})
		return "--mcp-config " + paths.ShellQuote(string(cfg))
	case "codex":
		args := CodexMCPArgs(id)
		var parts []string
		for i := 0; i+1 < len(args); i += 2 {
			parts = append(parts, args[i], paths.ShellQuote(args[i+1]))
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// CodexMCPArgs are codex's -c pairs that start mad's MCP server for
// agent id, as arguments (codex app-server takes them too).
func CodexMCPArgs(id string) []string {
	if !agentIDRe.MatchString(id) {
		return nil
	}
	cmd, _ := json.Marshal(paths.Self())
	args, _ := json.Marshal(mcpArgs(id))
	out := []string{"-c", "mcp_servers.mad.command=" + string(cmd), "-c", "mcp_servers.mad.args=" + string(args)}
	// A TOML inline table: {KEY="value", ...}, in a steady order.
	env := mcpEnv()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		v, _ := json.Marshal(env[k])
		pairs = append(pairs, k+"="+string(v))
	}
	if len(pairs) > 0 {
		out = append(out, "-c", "mcp_servers.mad.env={"+strings.Join(pairs, ",")+"}")
	}
	return out
}
