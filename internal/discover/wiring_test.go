package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	}{
		{"", true},
		{"model = \"o3\"\n# notify = [\"old\"]\n", true},
		{"notify = [\"terminal-notifier\", \"-title\", \"codex\"]\n", false},
		{"model = \"o3\"\n\n[profiles.work]\n  notify = [\"say\"]\n", false},
	}
	for _, c := range cases {
		write(filepath.Join(home, ".codex"), c.config)
		if got := notify() != ""; got != c.wired {
			t.Errorf("config %q: mad's notify wired = %v, want %v", c.config, got, c.wired)
		}
	}

	// CODEX_HOME moves codex's config.
	other := t.TempDir()
	t.Setenv("CODEX_HOME", other)
	if notify() == "" {
		t.Error("~/.codex should not count once CODEX_HOME is set")
	}
	write(other, "notify = [\"x\"]\n")
	if notify() != "" {
		t.Error("notify in $CODEX_HOME/config.toml was overridden")
	}
}
