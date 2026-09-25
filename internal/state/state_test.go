package state

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func useTempState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func TestLoadMissingIsEmpty(t *testing.T) {
	useTempState(t)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Projects) != 0 || !ModTime().IsZero() {
		t.Errorf("want empty state, got %+v", s)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	useTempState(t)
	s := &State{}
	p, added := s.AddProject("/src/app")
	if !added || p.Name != "app" {
		t.Fatalf("AddProject = %+v, %v", p, added)
	}
	p.Agents = append(p.Agents, &Agent{ID: "a1", Kind: "claude", SessionID: "s1", Dir: "/src/app/wt", Fork: true})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if ModTime().IsZero() {
		t.Error("ModTime after save is zero")
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	gp, ga := got.FindAgent("a1")
	if gp == nil || gp.Path != "/src/app" || ga.SessionID != "s1" || ga.Dir != "/src/app/wt" || !ga.Fork {
		t.Errorf("round trip lost data: %+v %+v", gp, ga)
	}
}

func TestLoadCorrupt(t *testing.T) {
	useTempState(t)
	s := &State{}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("XDG_STATE_HOME") + "/mad/state.json"
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), "state.json:1:") {
		t.Errorf("want a parse error naming the line, got %v", err)
	}
}

func TestProjectsAndIgnore(t *testing.T) {
	s := &State{}
	a, _ := s.AddProject("/a")
	s.AddProject("/b")
	if _, added := s.AddProject("/a"); added {
		t.Error("duplicate add reported as new")
	}

	s.RemoveProject(a)
	if s.FindProject("/a") != nil || !s.IsIgnored("/a") {
		t.Errorf("removed project should be gone and ignored: %+v", s)
	}
	s.RemoveProject(&Project{Path: "/a"}) // removing twice doesn't duplicate
	if len(s.Ignored) != 1 {
		t.Errorf("Ignored = %v", s.Ignored)
	}

	// Adding by hand lifts the ignore.
	if _, added := s.AddProject("/a"); !added || s.IsIgnored("/a") {
		t.Errorf("re-add: added=%v ignored=%v", added, s.IsIgnored("/a"))
	}
}

func TestAgentsOrderAndRemoval(t *testing.T) {
	s := &State{}
	p1, _ := s.AddProject("/p1")
	p2, _ := s.AddProject("/p2")
	p1.Agents = []*Agent{{ID: "1"}, {ID: "2"}}
	p2.Agents = []*Agent{{ID: "3"}}

	var ids []string
	for _, a := range s.OrderedAgents() {
		ids = append(ids, a.ID)
	}
	if got := ids; len(got) != 3 || got[0] != "1" || got[2] != "3" {
		t.Errorf("OrderedAgents = %v", got)
	}

	s.RemoveAgent("2")
	if _, a := s.FindAgent("2"); a != nil {
		t.Error("agent 2 still present")
	}
	if p, a := s.FindAgent("3"); p != p2 || a == nil {
		t.Error("FindAgent(3) wrong project")
	}
	s.RemoveAgent("missing") // no-op
}

func TestDisplayName(t *testing.T) {
	p := &Project{}
	c1 := &Agent{Kind: "claude"}
	x := &Agent{Kind: "codex"}
	c2 := &Agent{Kind: "claude"}
	p.Agents = []*Agent{c1, x, c2}
	for a, want := range map[*Agent]string{c1: "claude", x: "codex", c2: "claude#2"} {
		if got := p.DisplayName(a); got != want {
			t.Errorf("DisplayName = %q, want %q", got, want)
		}
	}
}

func TestDir(t *testing.T) {
	p := &Project{Path: "/repo"}
	if got := p.Dir(&Agent{}); got != "/repo" {
		t.Errorf("default dir = %q", got)
	}
	if got := p.Dir(&Agent{Dir: "/repo/.claude/worktrees/x"}); got != "/repo/.claude/worktrees/x" {
		t.Errorf("override dir = %q", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	s := &State{Ignored: []string{"/x"}}
	p, _ := s.AddProject("/p")
	p.Agents = append(p.Agents, &Agent{ID: "a", Kind: "claude"})

	c := s.Clone()
	c.Projects[0].Name = "changed"
	c.Projects[0].Agents[0].Kind = "codex"
	c.Ignored[0] = "/y"
	if p.Name != "p" || p.Agents[0].Kind != "claude" || s.Ignored[0] != "/x" {
		t.Error("Clone shares memory with the original")
	}
}

func TestNewUUID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewUUID()
		if !re.MatchString(id) {
			t.Fatalf("not a v4 UUID: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate UUID %s", id)
		}
		seen[id] = true
	}
}
