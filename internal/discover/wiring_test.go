package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClaudeSettings(t *testing.T) {
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(claudeSettings(true), &s); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop"} {
		entries := s.Hooks[ev]
		if len(entries) != 1 || len(entries[0].Hooks) != 1 {
			t.Fatalf("%s: %+v", ev, entries)
		}
		h := entries[0].Hooks[0]
		if h.Type != "command" || !strings.HasSuffix(h.Command, " hook claude") {
			t.Errorf("%s hook = %+v", ev, h)
		}
	}
	if s.Hooks["PreToolUse"][0].Matcher != "*" {
		t.Error("tool hooks must match every tool")
	}
}

func TestClaudeSettingsStatusLine(t *testing.T) {
	var with, without map[string]any
	json.Unmarshal(claudeSettings(true), &with)
	json.Unmarshal(claudeSettings(false), &without)
	sl, _ := with["statusLine"].(map[string]any)
	if cmd, _ := sl["command"].(string); !strings.Contains(cmd, "hook claude statusline") {
		t.Errorf("statusLine = %v", with["statusLine"])
	}
	if _, ok := without["statusLine"]; ok {
		t.Error("statusLine injected with quota off")
	}
}

func TestCodexNotify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	notify := func() string { return (codex{}).Placeholders()["{codex_notify}"] }
	write := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := notify(); !strings.HasPrefix(n, "-c 'notify=[") || !strings.HasSuffix(n, `"hook","codex"]'`) {
		t.Errorf("notify = %q", n)
	}
	cases := []struct {
		config string
		wired  bool
		chain  []string // the user's notify, run by mad's
	}{
		{"", true, nil},
		{"model = \"o3\"\n# notify = [\"old\"]\n", true, nil},
		{"notify = [\"terminal-notifier\", \"-title\", \"codex\"]\n", true, []string{"terminal-notifier", "-title", "codex"}},
		{"notify = [\n  'say', # literal\n  \"a \\\"b\\\"\",\n]\n[tui]\nx = 1\n", true, []string{"say", `a "b"`}},
		// Which profile applies is codex's business.
		{"model = \"o3\"\n\n[profiles.work]\n  notify = [\"say\"]\n", false, nil},
		// What mad can't read, or mad itself, is left alone.
		{"notify = \"\"\"x\"\"\"\n", false, nil},
		{"notify = [\"/u/bin/mad\", \"hook\", \"codex\"]\n", false, nil},
	}
	for _, c := range cases {
		write(filepath.Join(home, ".codex"), c.config)
		if got := notify() != ""; got != c.wired {
			t.Errorf("config %q: mad's notify wired = %v, want %v", c.config, got, c.wired)
		}
		if argv, _ := codexUserNotify(); !reflect.DeepEqual(argv, c.chain) {
			t.Errorf("config %q: user's notify = %q, want %q", c.config, argv, c.chain)
		}
	}

	// CODEX_HOME moves codex's config.
	other := t.TempDir()
	t.Setenv("CODEX_HOME", other)
	if notify() == "" {
		t.Error("~/.codex should not count once CODEX_HOME is set")
	}
	write(other, "notify = [\"x\"]\n")
	if argv, _ := codexUserNotify(); len(argv) != 1 || argv[0] != "x" {
		t.Errorf("notify in $CODEX_HOME/config.toml = %q", argv)
	}
}

// mad's notify hands the payload on to the user's.
func TestCodexHookRunsUserNotify(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	var ran []string
	defer func(f func([]string, string)) { runNotify = f }(runNotify)
	runNotify = func(argv []string, payload string) { ran = append(argv, payload) }
	defer func(f func(string) bool) { codexThread = f }(codexThread)
	codexThread = func(id string) bool { return id == "t-1" }

	payload := `{"type":"agent-turn-complete","thread-id":"t-1"}`
	(codex{}).Hook([]string{payload}, nil, nil, time.Time{})
	if ran != nil {
		t.Errorf("ran %q without a notify of the user's", ran)
	}
	os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"), []byte(`notify = ["say", "hi"]`), 0o644)
	r := (codex{}).Hook([]string{payload}, nil, nil, time.Time{})
	if want := []string{"say", "hi", payload}; !reflect.DeepEqual(ran, want) {
		t.Errorf("ran %q, want %q", ran, want)
	}
	if r.Turn == nil || r.Turn.SessionID != "t-1" {
		t.Errorf("report = %+v", r)
	}
	// A thread on the side (the one naming the conversation) is handed
	// on, but is not the agent's turn.
	ran = nil
	side := `{"type":"agent-turn-complete","thread-id":"t-2","last-assistant-message":"{\"title\":\"x\"}"}`
	if r := (codex{}).Hook([]string{side}, nil, nil, time.Time{}); r.Hook != nil || r.Turn != nil {
		t.Errorf("side thread reported %+v", r)
	}
	if len(ran) == 0 {
		t.Error("side thread not handed on to the user's notify")
	}
}

func TestCodexThread(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	day := filepath.Join(home, "sessions", "2026", "10", "01")
	os.MkdirAll(day, 0o755)
	rollout := func(id, meta string) {
		os.WriteFile(filepath.Join(day, "rollout-2026-10-01T10-00-00-"+id+".jsonl"),
			[]byte(`{"type":"session_meta","payload":{"id":"`+id+`","cwd":"/p","originator":"codex-tui"`+meta+`}}`+"\n"), 0o644)
	}
	const main, sub, gone = "01a0f575-0000-7000-8000-000000000001", "01a0f575-0000-7000-8000-000000000002", "01a0f575-0000-7000-8000-000000000003"
	rollout(main, `,"thread_source":"user"`)
	rollout(sub, `,"parent_thread_id":"`+main+`"`)
	for id, want := range map[string]bool{main: true, sub: false, gone: false} {
		if got := codexThread(id); got != want {
			t.Errorf("codexThread(%s) = %v, want %v", id, got, want)
		}
	}
}
