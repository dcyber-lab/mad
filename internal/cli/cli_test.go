package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func run(t *testing.T, in string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, IO{In: strings.NewReader(in), Out: &out, Err: &errb})
	return code, out.String(), errb.String()
}

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("MAD_AGENT_ID", "")
	return dir
}

func TestHelpAndUnknown(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		code, out, _ := run(t, "", arg)
		if code != 0 || !strings.Contains(out, "mad add [path]") {
			t.Errorf("%s: code=%d out=%q", arg, code, out)
		}
	}
	code, _, errOut := run(t, "", "frobnicate")
	if code != 2 || !strings.Contains(errOut, `unknown command "frobnicate"`) {
		t.Errorf("unknown: code=%d err=%q", code, errOut)
	}
}

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--version"} {
		code, out, _ := run(t, "", arg)
		if code != 0 || !strings.HasPrefix(out, "mad ") || strings.TrimSpace(out) == "mad" {
			t.Errorf("%s: code=%d out=%q", arg, code, out)
		}
	}
	Version = "v9.9.9"
	defer func() { Version = "" }()
	if _, out, _ := run(t, "", "version"); out != "mad v9.9.9\n" {
		t.Errorf("ldflags version: out=%q", out)
	}
}

func TestVersionNote(t *testing.T) {
	for _, c := range [][2]string{{"3.7c", "3.7c"}, {"", "3.7c"}, {"3.4", ""}} {
		if got := versionNote(c[0], c[1]); got != "" {
			t.Errorf("versionNote(%q, %q) = %q, want nothing", c[0], c[1], got)
		}
	}
	got := versionNote("3.4", "3.7c")
	for _, want := range []string{"runs on tmux 3.4", "installed tmux is 3.7c", "mad kill-server"} {
		if !strings.Contains(got, want) {
			t.Errorf("versionNote = %q, missing %q", got, want)
		}
	}
}

// A tmux too old for mad: the deck isn't started, and nothing is written.
func TestOpenWithOldTmux(t *testing.T) {
	home := isolate(t)
	t.Setenv("TMUX", "") // the tests may run in a deck
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\necho 'tmux 2.9a'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, args := range [][]string{nil, {"open"}} {
		code, _, errOut := run(t, "", args...)
		if code != 1 || errOut != "mad: tmux 2.9a is too old: mad needs tmux 3.0 or newer\n" {
			t.Errorf("%v: code=%d err=%q", args, code, errOut)
		}
	}
	if left, _ := os.ReadDir(home); len(left) != 0 {
		t.Errorf("written before the check: %v", left)
	}
}

func TestAdd(t *testing.T) {
	dir := isolate(t)
	proj := filepath.Join(dir, "code", "app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, _ := run(t, "", "add", proj)
	if code != 0 || !strings.Contains(out, "added: ~/code/app") {
		t.Errorf("add: code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "", "add", "~/code/app")
	if code != 0 || !strings.Contains(out, "already added") {
		t.Errorf("re-add: code=%d out=%q", code, out)
	}
	st, _ := state.Load()
	if len(st.Projects) != 1 || st.Projects[0].Path != proj {
		t.Errorf("state = %+v", st.Projects)
	}

	code, _, errOut := run(t, "", "add", filepath.Join(dir, "missing"))
	if code != 1 || !strings.Contains(errOut, "not a directory") {
		t.Errorf("missing dir: code=%d err=%q", code, errOut)
	}
}

func TestSwitchUsage(t *testing.T) {
	isolate(t)
	code, _, errOut := run(t, "", "switch")
	if code != 1 || !strings.Contains(errOut, "usage: mad switch") {
		t.Errorf("code=%d err=%q", code, errOut)
	}
}

func TestJump(t *testing.T) {
	isolate(t)
	code, _, errOut := run(t, "", "jump")
	if code != 1 || !strings.Contains(errOut, "sidebar is not running") {
		t.Errorf("no deck: code=%d err=%q", code, errOut)
	}

	short, err := os.MkdirTemp("", "mad") // socket paths are length-limited
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(short)
	t.Setenv("XDG_STATE_HOME", short)
	got := make(chan string, 1)
	l, err := poke.Listen(func(cmd string) { got <- cmd })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if code, _, errOut := run(t, "", "jump"); code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	select {
	case cmd := <-got:
		if cmd != poke.Jump {
			t.Errorf("sidebar got %q", cmd)
		}
	case <-time.After(2 * time.Second):
		t.Error("sidebar got nothing")
	}
}

func TestHookPokesSidebar(t *testing.T) {
	isolate(t)
	short, err := os.MkdirTemp("", "mad")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(short)
	t.Setenv("XDG_STATE_HOME", short)
	got := make(chan string, 1)
	l, err := poke.Listen(func(cmd string) { got <- cmd })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	t.Setenv("MAD_AGENT_ID", "a1")
	if code, _, _ := run(t, `{"hook_event_name":"UserPromptSubmit"}`, "hook", "claude"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	select {
	case cmd := <-got:
		if cmd != "hook a1" {
			t.Errorf("sidebar got %q", cmd)
		}
	case <-time.After(2 * time.Second):
		t.Error("sidebar got nothing")
	}
	if h := status.ReadHook("a1"); h == nil || h.State != status.Running {
		t.Errorf("hook file = %+v", h)
	}
}

func TestHook(t *testing.T) {
	isolate(t)

	// Without MAD_AGENT_ID (not started by mad) the hook is a no-op.
	if code, _, _ := run(t, `{"hook_event_name":"Stop"}`, "hook", "claude"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if entries, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_STATE_HOME"), "mad", "status")); len(entries) != 0 {
		t.Fatalf("wrote status without an agent id: %v", entries)
	}

	t.Setenv("MAD_AGENT_ID", "agent-1")
	run(t, `{"hook_event_name":"SessionStart","session_id":"s-1"}`, "hook", "claude")
	run(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, "hook", "claude")
	h := status.ReadHook("agent-1")
	if h == nil || h.State != status.Running || h.SessionID != "s-1" || time.Since(h.At) > time.Minute {
		t.Errorf("claude hook = %+v", h)
	}

	// Garbage input must not fail the agent's hook call.
	if code, _, errOut := run(t, "not json", "hook", "claude"); code != 0 || errOut != "" {
		t.Errorf("bad input: code=%d err=%q", code, errOut)
	}

	// Stop ends a turn, with what claude said last.
	run(t, `{"hook_event_name":"Stop","session_id":"s-1","last_assistant_message":"all done"}`, "hook", "claude")
	if tr := status.ReadTurn("agent-1"); tr == nil || tr.Reply != "all done" || tr.SessionID != "s-1" {
		t.Errorf("claude turn = %+v", tr)
	}
	if h := status.ReadHook("agent-1"); h == nil || h.State != status.Idle || !h.At.Equal(status.ReadTurn("agent-1").At) {
		t.Errorf("claude hook after Stop = %+v", h)
	}

	t.Setenv("MAD_AGENT_ID", "agent-2")
	day := filepath.Join(os.Getenv("HOME"), ".codex", "sessions", "2026", "01", "01")
	os.MkdirAll(day, 0o755)
	os.WriteFile(filepath.Join(day, "rollout-2026-01-01T00-00-00-t-9.jsonl"),
		[]byte(`{"type":"session_meta","payload":{"id":"t-9","cwd":"/p","originator":"codex-tui"}}`+"\n"), 0o644)
	run(t, "", "hook", "codex", `{"type":"agent-turn-complete","thread-id":"t-9","last-assistant-message":"ok"}`)
	if h := status.ReadHook("agent-2"); h == nil || h.State != status.Idle || h.SessionID != "t-9" {
		t.Errorf("codex hook = %+v", h)
	}
	if tr := status.ReadTurn("agent-2"); tr == nil || tr.Reply != "ok" {
		t.Errorf("codex turn = %+v", tr)
	}
	// The thread codex names the conversation in keeps no rollout: not
	// the agent's turn.
	run(t, "", "hook", "codex", `{"type":"agent-turn-complete","thread-id":"t-title","last-assistant-message":"{}"}`)
	if h := status.ReadHook("agent-2"); h.SessionID != "t-9" || status.ReadTurn("agent-2").Reply != "ok" {
		t.Errorf("a side thread was taken for the agent's: %+v", h)
	}
	if code, _, _ := run(t, "", "hook"); code != 0 {
		t.Error("bare hook should be a no-op")
	}
}

func TestDriveUsage(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{{"send"}, {"wait"}, {"wait", "a", "b"}, {"spawn", "-x"}, {"send", "-t", "soon", "a"}} {
		if code, _, errOut := run(t, "", args...); code != 2 || !strings.Contains(errOut, "usage: mad "+args[0]) {
			t.Errorf("%q: code=%d err=%q", args, code, errOut)
		}
	}
	if code, _, errOut := run(t, "", "wait", "nobody"); code != 1 || !strings.Contains(errOut, `no agent "nobody"`) {
		t.Errorf("unknown agent: code=%d err=%q", code, errOut)
	}
}

// With nothing pending, wait prints the last reply without asking tmux.
func TestWaitPrintsLastReply(t *testing.T) {
	isolate(t)
	defer func(s string) { tmux.Socket = s }(tmux.Socket)
	tmux.Socket = fmt.Sprintf("mad-cli-test-%d", time.Now().UnixNano()) // no server: not the deck you run
	st := &state.State{}
	p, _ := st.AddProject("/p")
	p.Agents = []*state.Agent{{ID: "0123456789ab", Kind: "claude", Name: "impl"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "", "wait", "impl"); code != 0 || out != "" {
		t.Errorf("before any turn: code=%d out=%q", code, out)
	}
	status.WriteTurn("0123456789ab", &status.Turn{Reply: "line 1\nline 2\n"}, time.Now())
	if code, out, _ := run(t, "", "wait", "0123"); code != 0 || out != "line 1\nline 2\n" {
		t.Errorf("code=%d out=%q", code, out)
	}
	code, out, _ := run(t, "", "ls")
	if code != 0 || !strings.Contains(out, "01234567  impl  claude  stopped  /p") {
		t.Errorf("ls: code=%d out=%q", code, out)
	}
}

func TestScanOnEmptyHome(t *testing.T) {
	dir := isolate(t)
	code, out, _ := run(t, "", "scan", dir)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"history: 0 projects", "open outside the deck:", "claude sessions of ~: 0", "codex sessions of ~: 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("scan output lacks %q:\n%s", want, out)
		}
	}
}

func TestStatusLineHook(t *testing.T) {
	dir := isolate(t)
	t.Chdir(dir)
	in := `{"model":{"display_name":"Opus"},"context_window":{"used_percentage":34},"rate_limits":{"five_hour":{"used_percentage":62,"resets_at":` + fmt.Sprint(time.Now().Add(2*time.Hour+10*time.Minute).Unix()) + `},"seven_day":{"used_percentage":31}}}`

	// Without a status line of the user's own, mad prints a summary and
	// records the limits.
	code, out, _ := run(t, in, "hook", "statusline")
	if code != 0 || !strings.HasPrefix(out, "Opus · ctx 34% · 5h 62% (2h") || !strings.HasSuffix(strings.TrimSpace(out), "wk 31%") {
		t.Errorf("code=%d out=%q", code, out)
	}
	q, ok := status.ReadQuota("claude")
	if !ok || q.FiveHour.Used != 62 || q.SevenDay.Used != 31 {
		t.Errorf("quota = %+v %v", q, ok)
	}

	// Settings mad writes now call it as claude's; the old form above
	// is what running sessions still hold.
	os.Remove(status.QuotaPath("claude"))
	if code, out, _ := run(t, in, "hook", "claude", "statusline"); code != 0 || !strings.HasPrefix(out, "Opus · ctx 34%") {
		t.Errorf("hook claude statusline: code=%d out=%q", code, out)
	}
	if _, ok := status.ReadQuota("claude"); !ok {
		t.Error("hook claude statusline recorded no quota")
	}

	// With one, the same JSON is handed to it and its output shown.
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(`{"statusLine":{"type":"command","command":"jq -r .model.display_name | tr a-z A-Z"}}`), 0o644)
	if _, err := exec.LookPath("jq"); err == nil {
		if _, out, _ := run(t, in, "hook", "statusline"); strings.TrimSpace(out) != "OPUS" {
			t.Errorf("user status line out=%q", out)
		}
	}
	// No rate limits (an API key): nothing recorded, still a line.
	os.Remove(filepath.Join(dir, ".claude", "settings.json"))
	os.Remove(status.QuotaPath("claude"))
	if _, out, _ := run(t, `{"model":{"display_name":"Sonnet"}}`, "hook", "statusline"); strings.TrimSpace(out) != "Sonnet" {
		t.Errorf("api key out=%q", out)
	}
	if _, ok := status.ReadQuota("claude"); ok {
		t.Error("quota written without rate limits")
	}
}

// What you type into an agent of a run is a note to the run; what its
// runner sends is not.
func TestHookNotesTyped(t *testing.T) {
	home := isolate(t)
	st := &state.State{}
	p, _ := st.AddProject(home)
	p.Runs = []*state.Run{{ID: "r1", Name: "audit", Dir: home}}
	p.Agents = []*state.Agent{{ID: "a1", Kind: "claude", Run: "r1", Role: "judge"}, {ID: "a2", Kind: "claude"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, ".mad", "runs", "audit"), 0o755)
	t.Setenv("MAD_AGENT_ID", "a1")
	run(t, `{"hook_event_name":"UserPromptSubmit","prompt":"[mad run audit · triage] You are the judge"}`, "hook", "claude")
	run(t, `{"hook_event_name":"UserPromptSubmit","prompt":"F2 is not a real issue"}`, "hook", "claude")
	t.Setenv("MAD_AGENT_ID", "a2") // in no run
	run(t, `{"hook_event_name":"UserPromptSubmit","prompt":"unrelated"}`, "hook", "claude")
	data, _ := os.ReadFile(filepath.Join(home, ".mad", "runs", "audit", "inbox.jsonl"))
	if got := string(data); strings.Count(got, "\n") != 1 || !strings.Contains(got, `"role":"judge"`) || !strings.Contains(got, "F2 is not a real issue") {
		t.Errorf("inbox = %q", got)
	}
	if code, _, _ := run(t, "", "run", "note", "audit", "keep", "it", "small"); code != 0 {
		t.Fatal("run note failed")
	}
	if data, _ := os.ReadFile(filepath.Join(home, ".mad", "runs", "audit", "inbox.jsonl")); !strings.Contains(string(data), `"text":"keep it small"`) {
		t.Errorf("inbox after mad run note = %q", data)
	}
}

func TestHookKeepsRunsInWorktrees(t *testing.T) {
	home := isolate(t)
	wt := filepath.Join(home, ".claude", "worktrees", "run-audit")
	os.MkdirAll(wt, 0o755)
	st := &state.State{}
	p, _ := st.AddProject(home)
	p.Runs = []*state.Run{{ID: "r1", Name: "audit", Dir: wt}}
	p.Agents = []*state.Agent{{ID: "a1", Kind: "claude", Dir: wt, Run: "r1", Role: "builder"}, {ID: "a2", Kind: "claude", Dir: home}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		_, out, _ := run(t, `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"`+path+`"}}`, "hook", "claude")
		return out
	}
	t.Setenv("MAD_AGENT_ID", "a1")
	if out := write(filepath.Join(home, "main.go")); !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, wt) {
		t.Errorf("write to the main checkout: %q", out)
	}
	if out := write(filepath.Join(wt, "main.go")); out != "" {
		t.Errorf("write in the worktree: %q", out)
	}
	if _, out, _ := run(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"cd `+home+` && git commit -am x"}}`, "hook", "claude"); !strings.Contains(out, "deny") {
		t.Errorf("command in the main checkout: %q", out)
	}
	t.Setenv("MAD_AGENT_ID", "a2") // in no run
	if out := write(filepath.Join(home, "main.go")); out != "" {
		t.Errorf("an agent in no run was refused: %q", out)
	}
}

func TestFlowAndSkillCommands(t *testing.T) {
	home := isolate(t)
	if code, out, _ := run(t, "", "run", "flow", "show", "design-impl-review"); code != 0 || !strings.Contains(out, `"name": "design-impl-review"`) {
		t.Errorf("show: code=%d out=%q", code, out)
	}
	good := filepath.Join(home, "good.json")
	os.WriteFile(good, []byte(`{"name": "solo", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "do", "role": "a", "prompt": "{{task}}"}]}`), 0o644)
	bad := filepath.Join(home, "bad.json")
	os.WriteFile(bad, []byte(`{"name": "x", "roles": [{"name": "a", "agent": "claude"}], "steps": [{"name": "do", "role": "b", "prompt": "p"}]}`), 0o644)
	if code, out, _ := run(t, "", "run", "flow", "check", good); code != 0 || !strings.Contains(out, "flow solo is good") {
		t.Errorf("check good: code=%d out=%q", code, out)
	}
	if code, _, errOut := run(t, "", "run", "flow", "check", good, bad); code != 1 || !strings.Contains(errOut, `no role "b"`) {
		t.Errorf("check bad: code=%d err=%q", code, errOut)
	}
	if code, out, _ := run(t, "", "skill", "install"); code != 0 || !strings.Contains(out, "installed mad-flow") {
		t.Fatalf("install: code=%d out=%q", code, out)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "mad-flow", "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "name: mad-flow") {
		t.Errorf("installed skill: %v", err)
	}
	if code, _, _ := run(t, "", "skill", "install", "nope"); code != 1 {
		t.Error("installed a skill that isn't")
	}
}
