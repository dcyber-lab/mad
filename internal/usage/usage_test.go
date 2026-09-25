package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, path string, lines ...any) {
	t.Helper()
	appendLines(t, path, os.O_TRUNC, lines...)
}

func appendLines(t *testing.T, path string, flag int, lines ...any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|flag, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if s, ok := l.(string); ok {
			f.WriteString(s) // raw, possibly without newline
			continue
		}
		data, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(append(data, '\n'))
	}
}

// claudeMsg is one transcript line of an assistant message.
func claudeMsg(id string, in, cw, cr, out int64) map[string]any {
	return map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id": id, "role": "assistant",
			"usage": map[string]any{
				"input_tokens": in, "cache_creation_input_tokens": cw,
				"cache_read_input_tokens": cr, "output_tokens": out,
			},
		},
	}
}

func codexCount(in, cached, out int64) map[string]any {
	return map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"total_token_usage": map[string]any{
					"input_tokens": in, "cached_input_tokens": cached, "output_tokens": out,
					"total_tokens": in + out,
				},
			},
		},
	}
}

func reader(t *testing.T, files map[string][]string) *Reader {
	r := NewReader()
	r.locate = func(a Agent) []string { return files[a.ID] }
	return r
}

func TestClaudeTotals(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "s.jsonl")
	writeLines(t, main,
		map[string]any{"type": "user", "message": map[string]any{"content": "hi"}},
		claudeMsg("m1", 2, 100, 1000, 30),
		claudeMsg("m1", 2, 100, 1000, 30),                                                  // the same message, second content block
		map[string]any{"type": "assistant", "message": map[string]any{"content": []any{}}}, // no usage
		claudeMsg("m2", 1, 0, 1100, 10),
	)
	r := reader(t, map[string][]string{"a": {main}})
	got := r.Read([]Agent{{ID: "a", Kind: "claude"}})
	want := Totals{Input: 3, CacheWrite: 100, CacheRead: 2100, Output: 40}
	if got["a"] != want {
		t.Fatalf("got %+v want %+v", got["a"], want)
	}
	if got["a"].Total() != 2243 {
		t.Errorf("total %d", got["a"].Total())
	}
}

func TestIncrementalRead(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "s.jsonl")
	writeLines(t, main, claudeMsg("m1", 0, 0, 100, 10))
	r := reader(t, map[string][]string{"a": {main}})
	agents := []Agent{{ID: "a", Kind: "claude"}}
	if got := r.Read(agents)["a"].Total(); got != 110 {
		t.Fatalf("first read %d", got)
	}
	// A line still being written doesn't count and isn't skipped.
	partial, _ := json.Marshal(claudeMsg("m2", 0, 0, 200, 20))
	half := string(partial[:len(partial)/2])
	appendLines(t, main, 0, half)
	if got := r.Read(agents)["a"].Total(); got != 110 {
		t.Fatalf("partial line counted: %d", got)
	}
	appendLines(t, main, 0, string(partial[len(partial)/2:])+"\n")
	if got := r.Read(agents)["a"].Total(); got != 330 {
		t.Fatalf("after completion %d", got)
	}
	// The same message logged again later, with final numbers, replaces
	// the earlier ones.
	appendLines(t, main, 0, claudeMsg("m2", 0, 0, 200, 25))
	if got := r.Read(agents)["a"].Total(); got != 335 {
		t.Fatalf("after update %d", got)
	}
	// A rewritten (shorter) file is read from the start again.
	writeLines(t, main, claudeMsg("m9", 0, 0, 5, 5))
	if got := r.Read(agents)["a"].Total(); got != 10 {
		t.Fatalf("after rewrite %d", got)
	}
}

func TestSubagentsAndForgetting(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "s.jsonl")
	sub := filepath.Join(dir, "s", "subagents", "agent-1.jsonl")
	writeLines(t, main, claudeMsg("m1", 0, 0, 100, 0))
	writeLines(t, sub, claudeMsg("m2", 0, 0, 50, 0))
	files := map[string][]string{"a": {main, sub}, "b": nil}
	r := reader(t, files)
	got := r.Read([]Agent{{ID: "a", Kind: "claude"}, {ID: "b", Kind: "shell"}})
	if got["a"].Total() != 150 {
		t.Errorf("with subagent: %d", got["a"].Total())
	}
	if _, ok := got["b"]; ok {
		t.Error("an agent without a transcript got totals")
	}
	if len(r.files) != 2 {
		t.Errorf("tracking %d files", len(r.files))
	}
	files["a"] = []string{main} // e.g. after /clear moved to another session
	r.Read([]Agent{{ID: "a", Kind: "claude"}})
	if len(r.files) != 1 {
		t.Errorf("stale files kept: %d", len(r.files))
	}
}

func TestCodexTotals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	writeLines(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "x"}},
		codexCount(1000, 200, 50),
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": nil}},
		codexCount(3000, 2500, 120), // cumulative: the last one is the answer
	)
	r := reader(t, map[string][]string{"c": {path}})
	got := r.Read([]Agent{{ID: "c", Kind: "codex"}})["c"]
	want := Totals{Input: 500, CacheRead: 2500, Output: 120}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
	if got.Total() != 3120 {
		t.Errorf("total %d", got.Total())
	}
}

func TestMissingFile(t *testing.T) {
	r := reader(t, map[string][]string{"a": {filepath.Join(t.TempDir(), "nope.jsonl")}})
	got := r.Read([]Agent{{ID: "a", Kind: "claude"}})
	if got["a"] != (Totals{}) {
		t.Errorf("got %+v", got["a"])
	}
}

func TestLocateByKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	r := NewReader()
	for _, k := range []string{"pi", "shell", "claude", "codex"} {
		if got := r.transcripts(Agent{Kind: k, Dir: "/p", Session: "s"}); len(got) != 0 {
			t.Errorf("%s: %v", k, got)
		}
	}
	main := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-p", "s.jsonl")
	writeLines(t, main, claudeMsg("m", 0, 0, 1, 1))
	got := r.transcripts(Agent{Kind: "claude", Dir: "/p", Session: "s"})
	if len(got) != 1 || !strings.HasSuffix(got[0], "/-p/s.jsonl") {
		t.Errorf("claude: %v", got)
	}
}
