package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, lines string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(lines)
}

func TestClaude(t *testing.T) {
	main := filepath.Join(t.TempDir(), "s1.jsonl")
	sub := filepath.Join(filepath.Dir(main), "s1", "subagents", "agent-1.jsonl")
	r := NewReader()
	r.locate = func(a Agent) []string { // like the real one: only files that exist
		var out []string
		for _, p := range []string{main, sub} {
			if _, err := os.Stat(p); err == nil && a.Kind == "claude" {
				out = append(out, p)
			}
		}
		return out
	}
	agents := []Agent{{ID: "a", Kind: "claude", Session: "s1"}, {ID: "sh", Kind: "shell"}}

	if got := r.Read(agents); len(got) != 0 {
		t.Errorf("no files yet: %v", got)
	}

	write(t, main, `{"type":"user","message":{"role":"user","content":"<system-reminder>x</system-reminder>\nfix the flaky test\nplease"}}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"On it."}],"usage":{"input_tokens":10,"cache_read_input_tokens":100,"output_tokens":5}}}
{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...\n","description":"Run the tests"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":100,"output_tokens":20}}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
`)
	write(t, sub, `{"type":"assistant","message":{"id":"x1","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a/b.go"}}],"usage":{"input_tokens":1,"output_tokens":1}}}
`)
	got := r.Read(agents)["a"]
	want := Info{Tokens: Totals{Input: 11, CacheRead: 100, Output: 21}, Title: "fix the flaky test", Prompt: "fix the flaky test", Tool: "Bash · Run the tests"}
	if got != want {
		t.Errorf("after first turn:\n got %+v\nwant %+v", got, want)
	}

	// The generated title replaces the first prompt; a partial line waits.
	write(t, main, `{"type":"ai-title","aiTitle":"Flaky test fix"}
{"type":"user","message":{"role":"user","content":"now the docs"}}
{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/x/README.md"}}],"usage":{"input_tokens":1}`)
	got = r.Read(agents)["a"]
	want = Info{Tokens: Totals{Input: 11, CacheRead: 100, Output: 21}, Title: "Flaky test fix", Prompt: "now the docs"}
	if got != want {
		t.Errorf("new prompt:\n got %+v\nwant %+v", got, want)
	}
	write(t, main, `}}
{"type":"custom-title","customTitle":"my name"}
`)
	got = r.Read(agents)["a"]
	want = Info{Tokens: Totals{Input: 12, CacheRead: 100, Output: 21}, Title: "my name", Prompt: "now the docs", Tool: "Edit · README.md"}
	if got != want {
		t.Errorf("custom title:\n got %+v\nwant %+v", got, want)
	}

	// A rewritten (shorter) file is read again from the start.
	os.WriteFile(main, []byte(`{"type":"user","message":{"role":"user","content":"fresh"}}
`), 0o644)
	got = r.Read(agents)["a"]
	want = Info{Tokens: Totals{Input: 1, Output: 1}, Title: "fresh", Prompt: "fresh"}
	if got != want {
		t.Errorf("rewritten:\n got %+v\nwant %+v", got, want)
	}

	// Files of agents that are gone are forgotten.
	r.Read(nil)
	if len(r.files) != 0 {
		t.Errorf("files kept: %d", len(r.files))
	}
}

func TestCodex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	r := NewReader()
	r.locate = func(a Agent) []string { return []string{path} }
	names := map[string]string{}
	r.threadNames = func() map[string]string { return names }
	agents := []Agent{{ID: "c", Kind: "codex", Session: "t1"}}

	write(t, path, `{"type":"session_meta","payload":{"id":"t1"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\nstuff"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>x</environment_context>"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"why is the build slow"}]}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"go build ./...\"]}"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":800,"output_tokens":50}}}}
`)
	got := r.Read(agents)["c"]
	want := Info{Tokens: Totals{Input: 200, CacheRead: 800, Output: 50}, Title: "why is the build slow", Prompt: "why is the build slow", Tool: "shell · bash -lc go build ./..."}
	if got != want {
		t.Errorf("\n got %+v\nwant %+v", got, want)
	}
	names["t1"] = "Build speed"
	if got := r.Read(agents)["c"].Title; got != "Build speed" {
		t.Errorf("thread name not used: %q", got)
	}
}
