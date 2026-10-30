package discover

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMerge(t *testing.T) {
	known := []Command{
		{Name: "model", Description: "pick one", Hint: "<model>", TUI: true},
		{Name: "help", Description: "show help", TUI: true},
	}
	found := []Command{
		{Name: "review", Description: "Review\n  a PR"},
		{Name: "model", Description: "Set the model"},
		{Name: "model", Description: "again"},
		{Name: "__internal"},
		{Name: "agents", Description: "(removed) gone"},
	}
	got := merge(known, found)
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if strings.Join(names, " ") != "help model review" {
		t.Fatalf("names = %v", names)
	}
	if m := got[1]; m.Description != "Set the model" || m.Hint != "<model>" || !m.TUI {
		t.Errorf("model = %+v, want found's description with known's hint and TUI", m)
	}
	if got[2].Description != "Review a PR" {
		t.Errorf("description = %q, want one line", got[2].Description)
	}
}

// fakeAgent puts an executable name on PATH running script, a shell
// script, which has the request on stdin; what the script was sent is
// left in the returned file, and the pid of what it started in the other.
func fakeAgent(t *testing.T, name, script string) (sent, child string) {
	t.Helper()
	dir := t.TempDir()
	sent, child = filepath.Join(dir, "sent"), filepath.Join(dir, "child")
	body := "#!/bin/sh\nread line\nprintf '%s' \"$line\" > '" + sent + "'\nsleep 30 &\necho $! > '" + child + "'\n" + script
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return sent, child
}

func TestAskClaude(t *testing.T) {
	sent, child := fakeAgent(t, "claude", `
echo '{"type":"system","subtype":"hook_started"}'
echo '{"type":"control_response","response":{"subtype":"success","request_id":"other"}}'
echo '{"type":"control_response","response":{"subtype":"success","request_id":"mad","response":{"commands":[{"name":"clear","description":"Start over","argumentHint":"[name]","aliases":["reset"]},{"name":"update-demo","description":"Re-record the demo (project)","argumentHint":""}]}}}'
wait
`)
	start := time.Now()
	got, err := Commands(context.Background(), "claude", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v: claude was not ended once it answered", d)
	}
	b, _ := os.ReadFile(sent)
	if !strings.Contains(string(b), `"subtype":"initialize"`) {
		t.Errorf("sent %q, want an initialize request", b)
	}
	byName := map[string]Command{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if c := byName["clear"]; c.Description != "Start over" || c.Hint != "[name]" || len(c.Aliases) != 1 {
		t.Errorf("clear = %+v", c)
	}
	if _, ok := byName["update-demo"]; !ok {
		t.Error("a command claude found is missing")
	}
	if c := byName["resume"]; !c.TUI {
		t.Errorf("resume = %+v, want the TUI's own, marked TUI", c)
	}
	pid, _ := os.ReadFile(child)
	n, _ := strconv.Atoi(strings.TrimSpace(string(pid)))
	if n == 0 {
		t.Fatal("no child pid")
	}
	waitGone(t, n)
}

// waitGone fails unless process pid ends soon.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("process %d the agent started outlived it", pid)
}

func TestAskPi(t *testing.T) {
	sent, _ := fakeAgent(t, "pi", `
echo '{"type":"response","id":"mad","command":"get_commands","success":true,"data":{"commands":[{"name":"skill:brave-search","description":"Web search","source":"skill"}]}}'
wait
`)
	got, err := Commands(context.Background(), "pi", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(sent); !strings.Contains(string(b), `"get_commands"`) {
		t.Errorf("sent %q", b)
	}
	var skill, settings bool
	for _, c := range got {
		skill = skill || c.Name == "skill:brave-search"
		settings = settings || c.Name == "settings" && c.TUI
	}
	if !skill || !settings {
		t.Errorf("got %+v, want pi's skill and its own settings", got)
	}
}

func TestAskFails(t *testing.T) {
	// Silent: asking gives up when ctx ends; the TUI's own are given.
	fakeAgent(t, "claude", "wait\n")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	got, err := Commands(ctx, "claude", t.TempDir())
	if err == nil || len(got) != len(claudeCommands) {
		t.Errorf("silent claude: %d commands, err %v; want its own and an error", len(got), err)
	}
	// Gone without a word.
	fakeAgent(t, "pi", "exit 1\n")
	if _, err := Commands(context.Background(), "pi", t.TempDir()); err == nil || !strings.Contains(err.Error(), "without answering") {
		t.Errorf("pi that quit: err %v", err)
	}
	// Not installed.
	t.Setenv("PATH", t.TempDir())
	if got, err := Commands(context.Background(), "claude", t.TempDir()); err == nil || len(got) == 0 {
		t.Errorf("no claude: %d commands, err %v", len(got), err)
	}
}

func TestCommandsOfOtherKinds(t *testing.T) {
	got, err := Commands(context.Background(), "codex", t.TempDir())
	if err != nil || len(got) != len(codexCommands) {
		t.Fatalf("codex: %d commands, err %v", len(got), err)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Name >= got[i].Name {
			t.Fatalf("codex commands not sorted, or doubled: %s, %s", got[i-1].Name, got[i].Name)
		}
	}
	if got, _ := Commands(context.Background(), "shell", t.TempDir()); len(got) != 0 {
		t.Errorf("shell: %v", got)
	}
}
