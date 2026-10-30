package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

func isolate(t *testing.T) (home, project string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	project = filepath.Join(home, "work", "demo")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, project
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSaveValidates(t *testing.T) {
	_, project := isolate(t)
	long := strings.Repeat("x", MaxDescription+1)
	for _, tc := range []struct {
		name, desc, body, scope, project string
		want                             string
	}{
		{"", "d", "b", "", project, "name"},
		{"mad-flow", "d", "b", "", project, "mad's own"},
		{"ok", "  ", "b", "", project, "description"},
		{"ok", long, "b", "", project, "400"},
		{"ok", "d", " ", "", project, "body"},
		{"ok", "d", "b", "team", project, "scope"},
		{"ok", "d", "b", "project", "", "no project"},
	} {
		if _, err := Save("a", tc.project, tc.scope, tc.name, tc.desc, tc.body, false); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Save(%q,%q,%q,%q) = %v, want %q", tc.name, tc.desc, tc.body, tc.scope, err, tc.want)
		}
	}
	p, err := Save("alice", project, "", "Run The Tests!", "Use when\nthe user says \"test\".", "# Steps\n1. go test", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "run-the-tests" || p.Scope != Project {
		t.Fatalf("%+v", p)
	}
	dir := filepath.Join(paths.StateDir(), "skills", "pending", "run-the-tests")
	if s := read(t, filepath.Join(dir, "SKILL.md")); !strings.HasPrefix(s, "---\nname: run-the-tests\ndescription: ") || !strings.Contains(s, "1. go test") {
		t.Errorf("SKILL.md:\n%s", s)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(dir, "meta.json"))), &meta); err != nil || meta["by"] != "alice" || meta["project"] != project || meta["scope"] != "project" {
		t.Errorf("meta %v %v", meta, err)
	}
	list, _ := PendingList()
	if len(list) != 1 || list[0].Description != "Use when the user says \"test\"." || list[0].Body != "# Steps\n1. go test" {
		t.Errorf("pending %+v", list)
	}
}

func TestApproveProjectAndUser(t *testing.T) {
	home, project := isolate(t)
	if _, err := Save("a", project, "project", "deploy", "When deploying", "ship it", false); err != nil {
		t.Fatal(err)
	}
	if err := Approve("deploy", nil); err != nil {
		t.Fatal(err)
	}
	if left, _ := PendingList(); len(left) != 0 {
		t.Errorf("still pending: %+v", left)
	}
	if s := read(t, filepath.Join(project, ".mad", "skills", "deploy", "SKILL.md")); !strings.Contains(s, "ship it") {
		t.Errorf("store: %s", s)
	}
	for dir, wantDesc := range map[string]string{
		filepath.Join(project, ".claude", "skills", "deploy"):   `"When deploying"`,
		filepath.Join(home, ".codex", "skills", "deploy"):       `"(for project demo) When deploying"`,
		filepath.Join(home, ".pi", "agent", "skills", "deploy"): `"(for project demo) When deploying"`,
	} {
		if s := read(t, filepath.Join(dir, "SKILL.md")); !strings.Contains(s, "description: "+wantDesc) {
			t.Errorf("%s:\n%s", dir, s)
		}
		var side map[string]string
		if err := json.Unmarshal([]byte(read(t, filepath.Join(dir, Sidecar))), &side); err != nil || side["slug"] != "deploy" || side["scope"] != "project" || side["project"] != project || side["version"] == "" {
			t.Errorf("sidecar %s: %v %v", dir, side, err)
		}
	}

	if _, err := Save("a", project, "user", "tidy", "When tidying", "sweep", false); err != nil {
		t.Fatal(err)
	}
	edited := "---\nname: tidy\ndescription: When tidying up, edited\n---\n\nsweep twice\n"
	if err := Approve("tidy", &edited); err != nil {
		t.Fatal(err)
	}
	if s := read(t, filepath.Join(home, "config", "mad", "skills", "tidy", "SKILL.md")); !strings.Contains(s, "sweep twice") {
		t.Errorf("user store: %s", s)
	}
	for _, dir := range []string{
		filepath.Join(home, ".claude", "skills", "tidy"),
		filepath.Join(home, ".codex", "skills", "tidy"),
		filepath.Join(home, ".pi", "agent", "skills", "tidy"),
	} {
		s := read(t, filepath.Join(dir, "SKILL.md"))
		if !strings.Contains(s, "When tidying up, edited") || strings.Contains(s, "for project") {
			t.Errorf("%s:\n%s", dir, s)
		}
		if !exists(filepath.Join(dir, Sidecar)) {
			t.Errorf("no sidecar in %s", dir)
		}
	}

	got := List(project)
	if len(got) != 2 || got[0].Name != "deploy" || got[0].Scope != "project" || got[1].Name != "tidy" || got[1].Scope != "user" {
		t.Errorf("List = %+v", got)
	}
	if got := List("/elsewhere"); len(got) != 1 || got[0].Name != "tidy" {
		t.Errorf("List elsewhere = %+v", got)
	}
}

func TestHistoryOnSecondApprove(t *testing.T) {
	_, project := isolate(t)
	for _, body := range []string{"v1", "v2", "v3"} {
		if _, err := Save("a", project, "project", "deploy", "d", body, false); err != nil {
			t.Fatal(err)
		}
		if err := Approve("deploy", nil); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(project, ".mad", "skills", "deploy")
	hist, _ := filepath.Glob(filepath.Join(dir, "history", "*.md"))
	if len(hist) != 2 {
		t.Fatalf("history: %v", hist)
	}
	all := read(t, hist[0]) + read(t, hist[1])
	if !strings.Contains(all, "v1") || !strings.Contains(all, "v2") || strings.Contains(all, "v3") {
		t.Errorf("history holds %s", all)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "SKILL.md")), "v3") {
		t.Error("store not at v3")
	}
}

func TestRefusesForeignDir(t *testing.T) {
	home, project := isolate(t)
	foreign := filepath.Join(home, ".codex", "skills", "deploy")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save("a", project, "project", "deploy", "d", "body", false); err != nil {
		t.Fatal(err)
	}
	err := Approve("deploy", nil)
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("Approve = %v", err)
	}
	if read(t, filepath.Join(foreign, "SKILL.md")) != "mine" {
		t.Error("foreign dir was touched")
	}
	if exists(filepath.Join(project, ".mad", "skills", "deploy")) {
		t.Error("store written despite the refusal")
	}
	if _, err := Save("a", project, "project", "deploy", "d", "body", true); err != nil {
		t.Fatal(err)
	}
	if err := Approve("deploy", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(foreign, "SKILL.md")), "body") || !exists(filepath.Join(foreign, Sidecar)) {
		t.Error("not replaced")
	}
}

func TestDiscard(t *testing.T) {
	_, project := isolate(t)
	if _, err := Save("a", project, "", "gone", "d", "b", false); err != nil {
		t.Fatal(err)
	}
	if err := Discard("gone"); err != nil {
		t.Fatal(err)
	}
	if left, _ := PendingList(); len(left) != 0 {
		t.Errorf("%+v", left)
	}
	for _, bad := range []string{"gone", "../x", ""} {
		if Discard(bad) == nil || Approve(bad, nil) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSync(t *testing.T) {
	home, project := isolate(t)
	st := &state.State{Projects: []*state.Project{{Name: "demo", Path: project}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	for _, sc := range []string{"project", "user"} {
		if _, err := Save("a", project, sc, "s-"+sc, "d", "b", false); err != nil {
			t.Fatal(err)
		}
		if err := Approve("s-"+sc, nil); err != nil {
			t.Fatal(err)
		}
	}
	claudeU := filepath.Join(home, ".claude", "skills", "s-user")
	codexP := filepath.Join(home, ".codex", "skills", "s-project")
	if err := os.RemoveAll(claudeU); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexP, "SKILL.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not mad's: left alone.
	pi := filepath.Join(home, ".pi", "agent", "skills", "s-user")
	if err := os.Remove(filepath.Join(pi, Sidecar)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pi, "SKILL.md"), []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Sync(); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(claudeU, "SKILL.md")) || !exists(filepath.Join(claudeU, Sidecar)) {
		t.Error("user skill not re-copied")
	}
	if s := read(t, filepath.Join(codexP, "SKILL.md")); !strings.Contains(s, "(for project demo) d") {
		t.Errorf("stale copy kept: %s", s)
	}
	if read(t, filepath.Join(pi, "SKILL.md")) != "theirs" {
		t.Error("a dir without the sidecar was overwritten")
	}
}
