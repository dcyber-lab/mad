package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
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

	t.Setenv("MAD_AGENT_ID", "agent-2")
	run(t, "", "hook", "codex", `{"type":"agent-turn-complete","thread-id":"t-9"}`)
	if h := status.ReadHook("agent-2"); h == nil || h.State != status.Idle || h.SessionID != "t-9" {
		t.Errorf("codex hook = %+v", h)
	}
	if code, _, _ := run(t, "", "hook"); code != 0 {
		t.Error("bare hook should be a no-op")
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
