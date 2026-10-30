package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/state"
)

// skill_save through the MCP server, then the CLI commands that review it.
func TestSkillSaveAndCommands(t *testing.T) {
	home := isolate(t)
	project := filepath.Join(home, "code", "a")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	const arch = "aaaa1111-0000-0000-0000-000000000001"
	st := &state.State{}
	p, _ := st.AddProject(project)
	p.Agents = []*state.Agent{{ID: arch, Kind: "claude", Name: "architect"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ args, want string }{
		{`{"name":"mad-flow","description":"d","body":"b"}`, "ERROR "},
		{`{"name":"x","description":"","body":"b"}`, "ERROR "},
		{`{"name":"Release Notes","description":"When asked for release notes","body":"1. git log"}`, `"name": "release-notes"`},
	} {
		if got := mcpCall(t, arch, "skill_save", c.args); !strings.Contains(got, c.want) {
			t.Errorf("skill_save %s: %q, want %q", c.args, got, c.want)
		}
	}
	if code, out, _ := run(t, "", "skill", "pending"); code != 0 || !strings.Contains(out, "release-notes") || !strings.Contains(out, "architect") {
		t.Errorf("pending: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "skill", "approve", "release-notes"); code != 0 || !strings.Contains(out, "approved") {
		t.Fatalf("approve: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "release-notes", "SKILL.md")); err != nil {
		t.Error(err)
	}
	if got := mcpCall(t, arch, "bus_who", `{}`); !strings.Contains(got, `"release-notes"`) {
		t.Errorf("bus_who lacks skills: %s", got)
	}
	if code, out, _ := run(t, "", "skill", "ls", project); code != 0 || !strings.Contains(out, "release-notes  (project)") {
		t.Errorf("ls: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "skill", "ls", "a"); code != 0 || !strings.Contains(out, "release-notes") {
		t.Errorf("ls by name: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "skill", "pending"); code != 0 || !strings.Contains(out, "no skills") {
		t.Errorf("pending after approve: %q", out)
	}
	mcpCall(t, arch, "skill_save", `{"name":"junk","description":"d","body":"b","scope":"user"}`)
	if code, _, _ := run(t, "", "skill", "discard", "junk"); code != 0 {
		t.Error("discard failed")
	}
	if code, _, _ := run(t, "", "skill", "discard", "junk"); code != 1 {
		t.Error("discarded twice")
	}
	if err := os.RemoveAll(filepath.Join(project, ".claude")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "", "skill", "sync"); code != 0 {
		t.Error("sync failed")
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "release-notes", "SKILL.md")); err != nil {
		t.Errorf("sync did not restore: %v", err)
	}
	if code, _, _ := run(t, "", "skill", "approve"); code != 2 {
		t.Errorf("approve without a name: %d", code)
	}
}
