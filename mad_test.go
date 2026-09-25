package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandResumeFork(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no claude transcripts → fresh start
	claude := kindByName(builtinKinds, "claude")
	a := &Agent{ID: "id-1", Kind: "claude", SessionID: "sid-1", Fork: true}

	if got := claude.command(a, true); !strings.Contains(got, "--session-id id-1") {
		t.Errorf("unknown session should start fresh, got %q", got)
	}

	codex := kindByName(builtinKinds, "codex")
	a = &Agent{ID: "id-2", Kind: "codex", SessionID: "thread-9"}
	if got := codex.command(a, true); !strings.Contains(got, "resume") || !strings.HasSuffix(got, " thread-9") {
		t.Errorf("codex resume by id, got %q", got)
	}
	a.SessionID = ""
	if got := codex.command(a, true); !strings.HasSuffix(got, "--last") {
		t.Errorf("codex resume without id should use --last, got %q", got)
	}
}

func TestCommandForkFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sid-1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claude := kindByName(builtinKinds, "claude")
	a := &Agent{ID: "id-1", Kind: "claude", SessionID: "sid-1", Fork: true}
	if got := claude.command(a, true); !strings.Contains(got, "--resume sid-1 --fork-session") {
		t.Errorf("resume of a session open elsewhere should fork, got %q", got)
	}
	if got := claude.command(a, false); strings.Contains(got, "--fork-session") {
		t.Errorf("fork must only apply to resume, got %q", got)
	}
}

func TestSessionFromArgs(t *testing.T) {
	cases := []struct {
		kind string
		args []string
		want string
	}{
		{"claude", []string{"--resume=abc", "--model", "x"}, "abc"},
		{"claude", []string{"-r", "abc"}, "abc"},
		{"claude", []string{"--session-id", "abc"}, "abc"},
		{"claude", []string{"--continue"}, ""},
		{"codex", []string{"resume", "t-1"}, "t-1"},
		{"codex", []string{"resume", "--last"}, ""},
	}
	for _, c := range cases {
		if got := sessionFromArgs(c.kind, c.args); got != c.want {
			t.Errorf("%s %v: got %q want %q", c.kind, c.args, got, c.want)
		}
	}
}

func TestUserText(t *testing.T) {
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	cases := []struct {
		in   json.RawMessage
		want string
	}{
		{raw("<system-reminder>\nwt\n</system-reminder>\n\nfix the bug\nmore"), "fix the bug"},
		{raw([]map[string]string{{"type": "input_text", "text": "# AGENTS.md instructions"}}), ""},
		{raw([]map[string]string{{"type": "text", "text": "hello"}}), "hello"},
		{raw("<command-name>/model</command-name>"), ""},
	}
	for _, c := range cases {
		if got := userText(c.in); got != c.want {
			t.Errorf("userText(%s) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestFuzzyAndTruncate(t *testing.T) {
	if ok, _ := fuzzyMatch("cdn", "cdn-api"); !ok {
		t.Error("substring should match")
	}
	if ok, _ := fuzzyMatch("xyz", "cdn-api"); ok {
		t.Error("unrelated should not match")
	}
	if got := truncate("批量解决仓库", 7); got != "批量解…" {
		t.Errorf("truncate CJK by columns, got %q", got)
	}
}
