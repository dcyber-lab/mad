package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// mad mcp tells a teammate its part in the server's instructions, and
// one that is in no team nothing more than its name.
func TestMCPIdentityBrief(t *testing.T) {
	isolate(t)
	const arch, build, other = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002", "cccc3333-0000-0000-0000-000000000003"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{
		{ID: arch, Kind: "claude", Name: "architect", Role: "architect", Team: "alpha"},
		{ID: build, Kind: "claude", Name: "builder-alpha", Role: "builder", Team: "alpha"},
		{ID: other, Kind: "claude", Name: "solo"},
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	rec := `{"teams":[{"name":"alpha","template":"dev","channel":"alpha","project":"/code/a","permission":"auto","created_at":"2026-10-30T00:00:00Z","roles":[
	  {"role":"architect","agent":"` + arch + `","spec":"claude:opus","lead":true,"brief":"You lead.\nSecond line."},
	  {"role":"builder","agent":"` + build + `","spec":"claude:sonnet","brief":"You build."},
	  {"role":"reviewer","agent":"","spec":"codex","read_only":true,"brief":"You review."}]}]}`
	if err := os.MkdirAll(filepath.Join(paths.StateDir(), "bus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StateDir(), "bus", "teams.json"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}

	got := mcpIdentity(build)
	for _, want := range []string{
		"calls this session builder-alpha",
		"You are the builder of team alpha, working in #alpha",
		"The lead is architect (@architect)",
		"reviewer (@reviewer, codex, read only): You review.",
		"Your part: You build.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("builder's identity lacks %q:\n%s", want, got)
		}
	}
	lead := mcpIdentity(arch)
	if !strings.Contains(lead, "You are the lead") || !strings.Contains(lead, "builder (@builder-alpha, claude sonnet): You build.") || strings.Contains(lead, "Second line") == false {
		t.Errorf("lead's identity:\n%s", lead)
	}
	if solo := mcpIdentity(other); strings.Contains(solo, "team") {
		t.Errorf("solo's identity:\n%s", solo)
	}
	// It reaches the client in the server's instructions.
	if code, out, _ := run(t, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n", "mcp", "--agent", build); code != 0 || !strings.Contains(out, "of team alpha") {
		t.Errorf("mcp = %d %q", code, out)
	}
}

func TestTeamCmd(t *testing.T) {
	dir := isolate(t)
	if code, out, _ := run(t, "", "team", "ls"); code != 0 || !strings.Contains(out, "dev") || !strings.Contains(out, "architect=claude:opus (lead)") || !strings.Contains(out, "built in") {
		t.Errorf("team ls = %d %q", code, out)
	}
	good := filepath.Join(dir, "good.json")
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(good, []byte(`{"name":"pair","roles":[{"name":"a","agent":"claude","lead":true},{"name":"b","agent":"codex"}]}`), 0o644)
	_ = os.WriteFile(bad, []byte(`{"name":"bad","roles":[{"name":"a","agent":"claude"},{"name":"a","agent":"gemini"}]}`), 0o644)
	if code, out, _ := run(t, "", "team", "check", good); code != 0 || !strings.Contains(out, "team pair is good: 2 roles") {
		t.Errorf("check good = %d %q", code, out)
	}
	code, _, errs := run(t, "", "team", "check", good, bad)
	if code != 1 || !strings.Contains(errs, "no lead") || !strings.Contains(errs, "role a: twice") || !strings.Contains(errs, `agent "gemini"`) {
		t.Errorf("check bad = %d %q", code, errs)
	}
	for _, args := range [][]string{{"team"}, {"team", "nope"}, {"team", "rm"}, {"team", "start", "-bogus"}} {
		if code, _, _ := run(t, "", args...); code != 2 {
			t.Errorf("%v = %d, want usage", args, code)
		}
	}
	// A goal is needed; a team that isn't has no sleep or show.
	if code, _, errs := run(t, "", "team", "start", "-C", dir); code != 1 || !strings.Contains(errs, "needs a goal") {
		t.Errorf("start without goal = %d %q", code, errs)
	}
	for _, sub := range []string{"show", "sleep", "rm"} {
		if code, _, errs := run(t, "", "team", sub, "ghost"); code != 1 || !strings.Contains(errs, `no team "ghost"`) {
			t.Errorf("%s ghost = %d %q", sub, code, errs)
		}
	}
	if code, out, _ := run(t, "", "help"); code != 0 || !strings.Contains(out, "mad team ls|start|show|sleep|rm|check") {
		t.Errorf("help lacks team: %q", out)
	}
}
