package deck

import (
	"os"
	"path/filepath"

	"github.com/dcyber-lab/mad/internal/paths"
)

// pi has no MCP client, but takes extensions. A pi agent is started with
// this one, which runs mad's MCP server for it (the spec in --mad-bus, as
// MCPFlags makes it) and offers its tools to the model as pi tools.

// piBusExtension is the extension: pi loads TypeScript, and this is plain
// JavaScript too. The tools come from the server's tools/list, and again
// when it says they changed; pi cannot drop a tool, so one that is gone
// just fails when called. The server's instructions go on the system
// prompt. A server that is not there leaves the agent without the tools.
const piBusExtension = `// mad's pi extension (written by mad): mad's bus tools for this agent, from
// the MCP server "mad mcp --agent <id>" it runs.
import { spawn } from "node:child_process";

export default function (pi) {
  pi.registerFlag("mad-bus", { description: "mad's MCP server to run: JSON {command, args, env}", type: "string" });
  let spec = null, child = null, buf = "", nextId = 1, instructions = "", stopped = false, delay = 500;
  const pending = new Map();
  const known = new Set();

  const send = (msg) => child && child.stdin.writable && child.stdin.write(JSON.stringify(msg) + "\n");
  const request = (method, params) => new Promise((resolve, reject) => {
    if (!child) return reject(new Error("mad's bus is not running"));
    const id = nextId++;
    pending.set(id, { resolve, reject });
    send({ jsonrpc: "2.0", id, method, params });
  });
  const onLine = (line) => {
    let msg;
    try { msg = JSON.parse(line); } catch { return; }
    if (msg.id !== undefined && pending.has(msg.id)) {
      const p = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error.message || "mad: error")); else p.resolve(msg.result);
    } else if (msg.method === "notifications/tools/list_changed") {
      list().catch(() => {});
    }
  };

  const register = (t) => {
    if (known.has(t.name)) return;
    known.add(t.name);
    const first = String(t.description || "").split("\n")[0];
    pi.registerTool({
      name: t.name,
      label: t.name,
      description: t.description || t.name,
      promptSnippet: first,
      parameters: t.inputSchema || { type: "object", properties: {} },
      async execute(_id, params, signal) {
        const call = request("tools/call", { name: t.name, arguments: params || {} });
        const res = await (signal ? Promise.race([call, new Promise((_, no) => signal.addEventListener("abort", () => no(new Error("cancelled")), { once: true }))]) : call);
        const text = (res.content || []).filter((c) => c.type === "text").map((c) => c.text).join("\n");
        if (res.isError) throw new Error(text || "failed");
        return { content: [{ type: "text", text }], details: {} };
      },
    });
  };
  const list = async () => {
    const res = await request("tools/list", {});
    for (const t of res.tools || []) register(t);
  };

  const start = async () => {
    if (stopped || !spec) return;
    let c;
    try {
      c = spawn(spec.command, spec.args || [], { env: { ...process.env, ...(spec.env || {}) }, stdio: ["pipe", "pipe", "ignore"] });
    } catch { return; }
    child = c;
    buf = "";
    c.stdout.setEncoding("utf8");
    c.stdout.on("data", (d) => {
      buf += d;
      let i;
      while ((i = buf.indexOf("\n")) >= 0) { const line = buf.slice(0, i); buf = buf.slice(i + 1); if (line.trim()) onLine(line); }
    });
    c.stdin.on("error", () => {});
    c.on("error", () => {});
    c.on("exit", () => {
      if (child !== c) return;
      child = null;
      for (const p of pending.values()) p.reject(new Error("mad's bus stopped"));
      pending.clear();
      if (!stopped) setTimeout(start, delay = Math.min(delay * 2, 10000));
    });
    try {
      const init = await request("initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "pi", version: "1" } });
      instructions = init.instructions || instructions;
      send({ jsonrpc: "2.0", method: "notifications/initialized" });
      await list();
      delay = 500;
    } catch {}
  };

  pi.on("session_start", async () => {
    try { spec = JSON.parse(pi.getFlag("mad-bus") || ""); } catch { return; }
    if (spec && spec.command) await start();
  });
  pi.on("session_shutdown", () => { stopped = true; if (child) child.kill(); });
  pi.on("before_agent_start", (event) => {
    if (!instructions) return;
    return { systemPrompt: event.systemPrompt + "\n\n" + instructions };
  });
}
`

func piBusFile() string { return filepath.Join(paths.StateDir(), "pi-bus.ts") }

// writePiBus puts the extension where pi agents are pointed at, unless it
// is there as it should be; a mad rebuilt brings its new one.
func writePiBus() error {
	f := piBusFile()
	if old, err := os.ReadFile(f); err == nil && string(old) == piBusExtension {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, []byte(piBusExtension), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}
