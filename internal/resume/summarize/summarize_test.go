package summarize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	return dir
}

func TestCommandPluginAndChecks(t *testing.T) {
	dir := setup(t)
	count := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "sum.sh")
	// A plugin that answers with one good and one made-up citation, a
	// decision quoting the user and one that doesn't.
	os.WriteFile(script, []byte(`#!/bin/sh
cat > /dev/null
echo x >> `+count+`
cat <<'EOF'
{"summary":"retry path fixed, per the agent","model":"fake-1","cost_usd":0.001,
 "open_questions":[{"text":"is the context cancelled early?","source":"T1"},{"text":"ghost","source":"T9"}],
 "decision_candidates":[
  {"choice":"send data when images fail","source":"T1","quote":"images fail, still send the data"},
  {"choice":"cancel everything","source":"T1","quote":"cancel all of it"},
  {"choice":"ok","source":"T1","quote":"ok?"}],
 "suggested_bookmark":{"text":"check the context lifetime","source":"T1"}}
EOF
`), 0o755)
	c := Config{Summarizer: "command", Command: script, Projects: []string{"/p"}}
	if !c.Allowed("/p") || c.Allowed("/q") || (Config{Summarizer: "command"}).Allowed("/p") {
		t.Fatal("Allowed: projects must be listed")
	}
	p, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Goal: "g", Turns: []Turn{{Ref: "T1", You: "when images fail, still send the data. ok?"}}}
	res, err := Run(p, c, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Open) != 1 || res.Dropped != 2 {
		t.Errorf("open %+v dropped %d: an unknown turn ref must be dropped", res.Open, res.Dropped)
	}
	if len(res.Decisions) != 2 || !res.Decisions[0].Quoted || res.Decisions[1].Quoted {
		t.Errorf("quotes not checked: %+v", res.Decisions)
	}
	if res.Model != "fake-1" || res.Bookmark == nil {
		t.Errorf("result %+v", res)
	}
	// Same request: from the cache, the plugin doesn't run again.
	again, err := Run(p, c, req)
	if err != nil || !again.Cached {
		t.Fatalf("second run: cached=%v err=%v", again.Cached, err)
	}
	if data, _ := os.ReadFile(count); strings.Count(string(data), "x") != 1 {
		t.Errorf("plugin ran %d times, want 1", strings.Count(string(data), "x"))
	}
}

func TestUnknownPlugin(t *testing.T) {
	if _, err := New(Config{Summarizer: "nope"}); err == nil || !strings.Contains(err.Error(), "claude") {
		t.Errorf("err %v should list the plugins there are", err)
	}
}
