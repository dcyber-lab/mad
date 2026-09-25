package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/paths"
)

func writeConfig(t *testing.T, body string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if body == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	cases := []struct {
		name, file string
		want       Config
	}{
		{"missing", "", Default()},
		{"invalid", "{", Default()},
		{"no notify key", `{"other": 1}`, Default()},
		{"no on list", `{"notify": {"command": "say hi"}}`, Config{On: []string{Done, Waiting}, Command: "say hi"}},
		{"off", `{"notify": {"on": []}}`, Config{On: []string{}}},
		{"done only", `{"notify": {"on": ["done"]}}`, Config{On: []string{Done}}},
	}
	for _, c := range cases {
		writeConfig(t, c.file)
		if got := Load(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Load() = %+v, want %+v", c.name, got, c.want)
		}
	}
	if (Config{On: []string{}}).Wants(Done) || !Default().Wants(Waiting) {
		t.Error("Wants")
	}
}

func TestBody(t *testing.T) {
	cases := map[string]Event{
		"claude finished":           {Kind: Done, Agent: "claude"},
		"codex needs you":           {Kind: Waiting, Agent: "codex"},
		"claude#2: needs Bash okay": {Kind: Waiting, Agent: "claude#2", Message: "needs Bash okay"},
	}
	for want, e := range cases {
		if got := e.Body(); got != want {
			t.Errorf("Body() = %q, want %q", got, want)
		}
	}
	if got := (Event{Project: "cdn-api"}).Title(); got != "mad · cdn-api" {
		t.Errorf("Title() = %q", got)
	}
}

func TestCommand(t *testing.T) {
	ctx := context.Background()
	e := Event{Kind: Done, Project: `it's "p"`, Agent: "claude"}
	found := func(string) (string, error) { return "/usr/bin/notify-send", nil }
	missing := func(string) (string, error) { return "", errors.New("no") }

	mac := command(ctx, Default(), e, "darwin", missing)
	if mac == nil || filepath.Base(mac.Path) != "osascript" || mac.Args[len(mac.Args)-2] != e.Title() {
		t.Errorf("darwin: %v", mac)
	}
	linux := command(ctx, Default(), e, "linux", found)
	if linux == nil || linux.Args[len(linux.Args)-1] != "claude finished" {
		t.Errorf("linux: %v", linux)
	}
	if cmd := command(ctx, Default(), e, "linux", missing); cmd != nil {
		t.Errorf("linux without notify-send: %v", cmd)
	}
	if cmd := command(ctx, Default(), e, "windows", found); cmd != nil {
		t.Errorf("windows: %v", cmd)
	}

	custom := command(ctx, Config{Command: "echo hi"}, e, "darwin", missing)
	if custom == nil || custom.Args[len(custom.Args)-1] != "echo hi" {
		t.Fatalf("custom: %v", custom)
	}
	env := strings.Join(custom.Env, "\n")
	for _, kv := range []string{"MAD_EVENT=done", `MAD_PROJECT=it's "p"`, "MAD_AGENT=claude", "MAD_MESSAGE=claude finished"} {
		if !strings.Contains(env, kv) {
			t.Errorf("custom env lacks %q", kv)
		}
	}
}

func TestSendRunsCustomCommand(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	c := Config{On: Default().On, Command: `printf '%s|%s' "$MAD_TITLE" "$MAD_MESSAGE" > "$OUT"`}
	t.Setenv("OUT", out)
	if err := Send(c, Event{Kind: Waiting, Project: "api", Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "mad · api|codex needs you" {
		t.Errorf("command wrote %q", data)
	}
}

func TestModTime(t *testing.T) {
	writeConfig(t, "")
	if !ModTime().IsZero() {
		t.Error("missing file should have a zero ModTime")
	}
	writeConfig(t, `{"notify": {"on": []}}`)
	if ModTime().IsZero() {
		t.Error("existing file has a zero ModTime")
	}
}
