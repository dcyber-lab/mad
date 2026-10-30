package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dcyber-lab/mad/internal/status"
)

// harness runs app.js's own look, shownStatus and windowed on cases read
// from the file in argv[3] ({snap, hs}), against the table in argv[2], and
// prints the shown state of each.
const shownHarness = `
const fs = require("node:fs"), vm = require("node:vm");
const src = fs.readFileSync(process.argv[1], "utf8");
function fn(name) {
  const i = src.indexOf("function " + name + "(");
  let depth = 0;
  for (let k = src.indexOf("{", i); k < src.length; k++) {
    if (src[k] === "{") depth++;
    if (src[k] === "}" && --depth === 0) return src.slice(i, k + 1);
  }
}
const lookDecl = src.slice(src.indexOf("let looks = new Map();"), src.indexOf("function look("));
const refresh = src.split("\n").find((l) => l.includes("looks = new Map((deck.states"));
const deck = { states: JSON.parse(fs.readFileSync(process.argv[2], "utf8")) };
const ctx = { deck, opened: new Map() };
vm.createContext(ctx);
vm.runInContext(lookDecl + fn("look") + fn("shownStatus") + fn("windowed") + refresh +
  "\nglobalThis.shownStatus = shownStatus;", ctx);
const out = [];
for (const c of JSON.parse(fs.readFileSync(process.argv[3], "utf8"))) {
  ctx.opened.clear();
  if (c.hs) ctx.opened.set(c.snap.id, { hs: c.hs });
  out.push(ctx.shownStatus(c.snap));
}
console.log(JSON.stringify(out));
`

// The page draws the snapshot's shown state for what the snapshot knows
// (Finished, Stopped), and the host's only for what only it knows live.
func TestShownStatusMatchesSnapshot(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	write := func(name string, v any) string {
		b, _ := json.Marshal(v)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	type hs struct {
		Status string `json:"status"`
	}
	type snap struct {
		ID     string `json:"id"`
		Mode   string `json:"mode"`
		Status string `json:"status"`
		Shown  string `json:"shown"`
	}
	type tc struct {
		Snap snap `json:"snap"`
		HS   *hs  `json:"hs,omitempty"`
		Want string
	}
	win := func(raw, shown string) snap { return snap{"a1", "window", raw, shown} }
	cases := []tc{
		{win("stopped", "stopped"), &hs{"off"}, "stopped"},
		{win("asleep", "finished"), &hs{"off"}, "finished"},
		{win("asleep", "asleep"), &hs{"off"}, "asleep"},
		{win("idle", "finished"), &hs{"idle"}, "finished"},
		{win("idle", "idle"), &hs{"idle"}, "idle"},
		{win("idle", "idle"), &hs{"running"}, "running"},
		{win("idle", "idle"), &hs{"waiting"}, "waiting"},
		{win("asleep", "asleep"), &hs{"starting"}, "starting"},
		{win("running", "running"), nil, "running"},
		{snap{"a1", "tui", "idle", "finished"}, &hs{"off"}, "finished"},
	}
	cmd := exec.Command(node, "-e", shownHarness, "--",
		"web/app.js", write("looks.json", status.Looks), write("cases.json", cases))
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, b)
	}
	var got []string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	for i, c := range cases {
		if got[i] != c.Want {
			t.Errorf("snapshot shown=%q host=%v: page draws %q, want %q", c.Snap.Shown, c.HS, got[i], c.Want)
		}
	}
}
