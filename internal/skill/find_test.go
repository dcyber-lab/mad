package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFind(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	write := func(path string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	proj, other := filepath.Join(home, "proj"), filepath.Join(home, "other")
	pstack := filepath.Join(home, ".claude/plugins/cache/m/pstack/1.0")
	scoped := filepath.Join(home, ".claude/plugins/cache/m/team/1.0")
	fromPlugin := write(pstack + "/skills/poteto-mode/SKILL.md")
	review := write(scoped + "/skills/review/SKILL.md")
	write(home + "/.claude/plugins/installed_plugins.json")
	if err := os.WriteFile(home+"/.claude/plugins/installed_plugins.json", fmt.Appendf(nil,
		`{"version":2,"plugins":{"pstack@m":[{"scope":"user","installPath":%q}],
		"team@m":[{"scope":"project","installPath":%q,"projectPath":%q}]}}`, pstack, scoped, other), 0o644); err != nil {
		t.Fatal(err)
	}
	user := write(home + "/.claude/skills/poteto-mode/SKILL.md")
	inProject := write(proj + "/.claude/skills/mine/SKILL.md")
	codex := write(home + "/.codex/skills/poteto-mode/SKILL.md")

	for _, c := range []struct{ kind, dir, ref, want string }{
		{"claude", proj, "pstack:poteto-mode", fromPlugin}, // the plugin's, not the user's of that name
		{"claude", proj, "poteto-mode", user},
		{"claude", proj, "mine", inProject},
		{"claude", other, "mine", ""},
		{"claude", proj, "other:poteto-mode", ""},
		{"claude", proj, "review", ""}, // installed for another project
		{"claude", filepath.Join(other, ".claude/worktrees/x"), "team:review", review},
		{"codex", proj, "pstack:poteto-mode", codex},
		{"codex", proj, "mine", ""},
		{"pi", proj, "poteto-mode", ""},
	} {
		if got := Find(c.kind, c.dir, c.ref); got != c.want {
			t.Errorf("Find(%s, %s, %s) = %q, want %q", c.kind, c.dir, c.ref, got, c.want)
		}
	}

	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	if got := Find("codex", proj, "poteto-mode"); got != "" {
		t.Errorf("codex looked past CODEX_HOME: %s", got)
	}
	if got := Find("claude", proj, "poteto-mode"); got != "" {
		t.Errorf("claude looked past CLAUDE_CONFIG_DIR: %s", got)
	}
	moved := write(home + "/claude/skills/poteto-mode/SKILL.md")
	if got := Find("claude", proj, "poteto-mode"); got != moved {
		t.Errorf("claude in CLAUDE_CONFIG_DIR: %q", got)
	}
}

func TestCheckRef(t *testing.T) {
	for ref, ok := range map[string]bool{
		"pstack:poteto-mode": true, "mad-flow": true, "a1": true,
		"": false, "Pstack": false, "a/b": false, "a:b:c": false, ":x": false, "x:": false, "poteto mode": false,
	} {
		if err := CheckRef(ref); (err == nil) != ok {
			t.Errorf("CheckRef(%q) = %v", ref, err)
		}
	}
}
