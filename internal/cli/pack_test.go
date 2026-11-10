package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/pack"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// The packs mad comes with are good as `mad pack check` has it: every
// flow and template reads, every skill a role names is in its pack, and
// no skill points at a file that is not there.
func TestBuiltinPacksAreGood(t *testing.T) {
	isolate(t)
	for _, p := range pack.Builtin() {
		flows, teams, err := checkPack(p)
		if err != nil {
			t.Errorf("pack %s:\n%v", p.Name, err)
		}
		if flows+teams == 0 {
			t.Errorf("pack %s has no flows and no templates", p.Name)
		}
	}
	code, out, errOut := run(t, "", "pack", "check")
	if code != 0 || !strings.Contains(out, "pack mad 1 is good: 2 flows, 1 templates, 0 skills") || !strings.Contains(out, "pack pstack 0.1.0 is good: 5 flows, 3 templates,") {
		t.Errorf("pack check = %d %q %q", code, out, errOut)
	}
}

func TestPackCmd(t *testing.T) {
	home := isolate(t)
	if code, out, _ := run(t, "", "pack", "ls"); code != 0 || !strings.Contains(out, "pstack") || !strings.Contains(out, "built in") {
		t.Errorf("pack ls = %d %q", code, out)
	}
	code, out, _ := run(t, "", "pack", "show", "pstack")
	for _, want := range []string{"pstack 0.1.0:", "from built in", "$strong = claude:opus@high", "pstack-bugfix", "reproduce → diagnose → challenge → fix → check",
		"architect (lead), builder, interrogator", "a role names one as pstack:NAME", "blast-radius"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("pack show lacks %q: %d\n%s", want, code, out)
		}
	}
	if code, _, errOut := run(t, "", "pack", "show", "nothing"); code != 1 || !strings.Contains(errOut, `no pack "nothing"`) {
		t.Errorf("show of no pack = %d %q", code, errOut)
	}

	// Ejected, the copy takes the built-in one's place, and is changed
	// without mad.
	code, out, errOut := run(t, "", "pack", "eject", "pstack")
	dst := filepath.Join(home, "config", "mad", "packs", "pstack")
	if code != 0 || !strings.Contains(out, "pack pstack 0.1.0 copied to "+paths.Short(dst)) {
		t.Fatalf("eject = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, "", "pack", "eject", "pstack"); code != 1 || !strings.Contains(errOut, "is there already") {
		t.Errorf("eject twice = %d %q", code, errOut)
	}
	file := filepath.Join(dst, "pack.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(strings.Replace(string(data), `"0.1.0"`, `"0.1.0-mine"`, 1), `"claude:sonnet@high"`, `"codex"`, 1)
	if err := os.WriteFile(file, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "", "pack", "ls"); code != 0 || !strings.Contains(out, "0.1.0-mine") || !strings.Contains(out, "(in place of built in 0.1.0)") {
		t.Errorf("pack ls with the copy = %d %q", code, out)
	}
	if code, out, _ := run(t, "", "run", "flow", "ls"); code != 0 || !strings.Contains(out, "builder=codex") || !strings.Contains(out, paths.Short(filepath.Join(dst, "flows", "pstack-feature.json"))) {
		t.Errorf("flow ls with the copy = %d %q", code, out)
	}

	// What a change can break is found by the check.
	if err := os.RemoveAll(filepath.Join(dst, "skills", "blast-radius")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dst, "skills", "principles", "fix-root-causes.md")); err != nil {
		t.Fatal(err)
	}
	changed = strings.Replace(changed, `"other": "codex"`, `"other": "gemini@loud"`, 1)
	if err := os.WriteFile(file, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = run(t, "", "pack", "check", dst)
	for _, want := range []string{
		"flows/pstack-refactor.json, role checker: the pack has no skill blast-radius",
		"skills/bugfix/SKILL.md points at ../principles/fix-root-causes.md, which is not there",
		`agents: $other is "gemini@loud": a role is played by claude or codex`,
		`flows/pstack-docs.json: role checker: agent "gemini@loud": a run works with claude and codex`,
	} {
		if code != 1 || !strings.Contains(errOut, want) {
			t.Errorf("check of the broken copy lacks %q: %d\n%s", want, code, errOut)
		}
	}
	if code, _, errOut := run(t, "", "pack", "frob"); code != 2 || !strings.Contains(errOut, "mad pack ls") {
		t.Errorf("unknown = %d %q", code, errOut)
	}
}

// The hook refuses the edits of a team's role that edits nothing.
func TestTeamHookRefuses(t *testing.T) {
	isolate(t)
	const breaker, builder = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{
		{ID: breaker, Kind: "claude", Name: "interrogator", Role: "interrogator", Team: "alpha"},
		{ID: builder, Kind: "claude", Name: "builder", Role: "builder", Team: "alpha"},
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	rec := `{"teams":[{"name":"alpha","template":"pstack","channel":"alpha","project":"/code/a","permission":"auto","created_at":"2026-10-30T00:00:00Z","roles":[
	  {"role":"builder","agent":"` + builder + `","spec":"claude:sonnet"},
	  {"role":"interrogator","agent":"` + breaker + `","spec":"claude:opus","no_edits":true}]}]}`
	if err := paths.WriteFileAtomic(filepath.Join(paths.StateDir(), "bus", "teams.json"), []byte(rec)); err != nil {
		t.Fatal(err)
	}
	edit := &discover.Tool{Name: "Edit", Path: "/code/a/main.go"}
	if why := outsideRun(breaker, edit); !strings.Contains(why, "the interrogator edits no file in the team's directory") {
		t.Errorf("the interrogator's edit: %q", why)
	}
	if why := outsideRun(builder, edit); why != "" {
		t.Errorf("the builder's edit: %q", why)
	}
	if why := outsideRun(breaker, &discover.Tool{Name: "Bash", Command: "git push"}); why != "" {
		t.Errorf("a team's push is its lead's to decide: %q", why)
	}
}
