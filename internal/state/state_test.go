package state

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
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

// A run's agents are numbered and ordered among themselves, ahead of the
// project's own.
func TestRunGroups(t *testing.T) {
	st := &State{}
	p, _ := st.AddProject("/p")
	a, b, c, d := &Agent{ID: "a"}, &Agent{ID: "b", Run: "r1"}, &Agent{ID: "c"}, &Agent{ID: "d", Run: "r1"}
	gone := &Agent{ID: "e", Run: "removed"}
	p.Agents = []*Agent{a, b, c, d, gone}
	p.Runs = []*Run{{ID: "r1", Name: "x"}}
	ids := func(as []*Agent) string {
		var s []string
		for _, x := range as {
			s = append(s, x.ID)
		}
		return strings.Join(s, " ")
	}
	for got, want := range map[string]string{
		ids(p.RunAgents("r1")):  "b d",
		ids(p.TopAgents()):      "a c e", // e's run is gone: it is the project's again
		ids(p.Group(d)):         "b d",
		ids(p.Group(c)):         "a c e",
		ids(st.OrderedAgents()): "b d a c e",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if p.InRun(gone) || !p.InRun(b) {
		t.Error("InRun")
	}
}

func TestUpdateKeepsConcurrentWrites(t *testing.T) {
	useTempState(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := Update(func(s *State) error {
				p, _ := s.AddProject(fmt.Sprintf("/p/%d", i))
				p.Agents = append(p.Agents, &Agent{ID: NewUUID(), Kind: "claude"})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	s, _ := Load()
	if len(s.Projects) != 20 {
		t.Errorf("%d projects after 20 updates", len(s.Projects))
	}
}

func TestSaveMergedKeepsOthersAdditions(t *testing.T) {
	useTempState(t)
	s := &State{}
	p, _ := s.AddProject("/p")
	_ = s.Save()
	base, since := s.Clone(), ModTime()
	time.Sleep(10 * time.Millisecond)
	_ = Update(func(o *State) error {
		o.FindProject("/p").Agents = append(o.FindProject("/p").Agents, &Agent{ID: "theirs", Kind: "claude"})
		return nil
	})
	p.Collapsed = true
	if err := s.SaveMerged(base, since); err != nil {
		t.Fatal(err)
	}
	got, _ := Load()
	if _, a := got.FindAgent("theirs"); a == nil || !got.FindProject("/p").Collapsed {
		t.Errorf("merged state lost an agent or my change: %+v", got.FindProject("/p"))
	}
}

func TestSaveMergedKeepsProjectAddedBack(t *testing.T) {
	useTempState(t)
	s := &State{}
	p, _ := s.AddProject("/p")
	s.RemoveProject(p)
	_ = s.Save()
	base, since := s.Clone(), ModTime()
	time.Sleep(10 * time.Millisecond)
	_ = Update(func(o *State) error {
		q, _ := o.AddProject("/p")
		q.Runs = append(q.Runs, &Run{ID: "r1"})
		return nil
	})
	if err := s.SaveMerged(base, since); err != nil {
		t.Fatal(err)
	}
	got, _ := Load()
	if _, r := got.FindRun("r1"); r == nil || got.IsIgnored("/p") {
		t.Errorf("project added back was lost: projects=%d ignored=%v", len(got.Projects), got.Ignored)
	}
}

// TestMerge runs merge on a base, ours (a change made here) and theirs (a
// change another writer made to the file).
func TestMerge(t *testing.T) {
	start := func() *State {
		return &State{Projects: []*Project{
			{Name: "p", Path: "/p", Agents: []*Agent{{ID: "a", Kind: "claude", Name: "old"}}, Runs: []*Run{{ID: "r", Name: "run"}}},
			{Name: "q", Path: "/q", Agents: []*Agent{}},
		}}
	}
	agent := func(s *State) *Agent { _, a := s.FindAgent("a"); return a }
	for _, tc := range []struct {
		name   string
		ours   func(*State)
		theirs func(*State)
		check  func(*testing.T, *State)
	}{{
		name:   "their field edit to an agent ours left alone",
		ours:   func(s *State) { s.FindProject("/q").Collapsed = true },
		theirs: func(s *State) { agent(s).Mode, agent(s).SessionID = ModeWindow, "s2" },
		check: func(t *testing.T, s *State) {
			if a := agent(s); a.Mode != ModeWindow || a.SessionID != "s2" || !s.FindProject("/q").Collapsed {
				t.Errorf("agent %+v, q collapsed %v", a, s.FindProject("/q").Collapsed)
			}
		},
	}, {
		name:   "both changed different fields of an agent",
		ours:   func(s *State) { agent(s).Name = "mine" },
		theirs: func(s *State) { agent(s).Mode = ModeWindow },
		check: func(t *testing.T, s *State) {
			if a := agent(s); a.Name != "mine" || a.Mode != ModeWindow {
				t.Errorf("agent %+v", a)
			}
		},
	}, {
		name:   "both changed the same field",
		ours:   func(s *State) { agent(s).Name = "mine" },
		theirs: func(s *State) { agent(s).Name = "theirs" },
		check: func(t *testing.T, s *State) {
			if a := agent(s); a.Name != "mine" {
				t.Errorf("name %q, want ours", a.Name)
			}
		},
	}, {
		name: "their run and project field edits",
		ours: func(s *State) { agent(s).Name = "mine" },
		theirs: func(s *State) {
			_, r := s.FindRun("r")
			r.Collapsed = true
			s.FindProject("/p").Name = "renamed"
			s.Compact = true
		},
		check: func(t *testing.T, s *State) {
			if _, r := s.FindRun("r"); !r.Collapsed || s.FindProject("/p").Name != "renamed" || !s.Compact {
				t.Errorf("run %+v, project %q, compact %v", r, s.FindProject("/p").Name, s.Compact)
			}
		},
	}, {
		name:   "project removed by another, unchanged here",
		ours:   func(s *State) { agent(s).Name = "mine" },
		theirs: func(s *State) { s.RemoveProject(s.FindProject("/q")) },
		check: func(t *testing.T, s *State) {
			if s.FindProject("/q") != nil || !s.IsIgnored("/q") {
				t.Errorf("q resurrected: ignored=%v", s.Ignored)
			}
		},
	}, {
		name:   "project removed by another, changed here",
		ours:   func(s *State) { s.FindProject("/q").Agents = append(s.FindProject("/q").Agents, &Agent{ID: "new"}) },
		theirs: func(s *State) { s.RemoveProject(s.FindProject("/q")) },
		check: func(t *testing.T, s *State) {
			if s.FindProject("/q") == nil || s.IsIgnored("/q") {
				t.Errorf("q dropped with an agent added here: ignored=%v", s.Ignored)
			}
		},
	}, {
		name:   "ignored list: their removal and their lift both taken",
		ours:   func(s *State) { s.Ignored = append(s.Ignored, "/mine") },
		theirs: func(s *State) { s.Ignored = []string{"/theirs"} },
		check: func(t *testing.T, s *State) {
			if strings.Join(s.Ignored, " ") != "/mine /theirs" {
				t.Errorf("ignored %v", s.Ignored)
			}
		},
	}, {
		name: "agents and runs added and removed on both sides, in order",
		ours: func(s *State) {
			p := s.FindProject("/p")
			p.Agents = append(p.Agents, &Agent{ID: "o1"}, &Agent{ID: "o2"})
		},
		theirs: func(s *State) {
			p := s.FindProject("/p")
			p.Agents = append(p.Agents[:0], &Agent{ID: "t1"})
			p.Runs = nil
		},
		check: func(t *testing.T, s *State) {
			var ids []string
			for _, a := range s.FindProject("/p").Agents {
				ids = append(ids, a.ID)
			}
			if strings.Join(ids, " ") != "o1 o2 t1" || len(s.FindProject("/p").Runs) != 0 {
				t.Errorf("agents %v, runs %d", ids, len(s.FindProject("/p").Runs))
			}
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			base := start()
			base.Ignored = []string{"/lifted"}
			ours, theirs := base.Clone(), base.Clone()
			tc.ours(ours)
			tc.theirs(theirs)
			kept := agent(ours)
			ours.merge(base, theirs)
			tc.check(t, ours)
			if a := agent(ours); a != nil && a != kept {
				t.Error("agent replaced, not updated in place")
			}
		})
	}
}

func TestSaveMergedKeepsOthersFieldEdit(t *testing.T) {
	useTempState(t)
	s := &State{}
	p, _ := s.AddProject("/p")
	p.Agents = append(p.Agents, &Agent{ID: "a", Kind: "claude"})
	_ = s.Save()
	base, since := s.Clone(), ModTime()
	time.Sleep(10 * time.Millisecond)
	_ = Update(func(o *State) error {
		_, a := o.FindAgent("a")
		a.Mode, a.Name = ModeWindow, "theirs"
		return nil
	})
	p.Collapsed = true
	if err := s.SaveMerged(base, since); err != nil {
		t.Fatal(err)
	}
	got, _ := Load()
	if _, a := got.FindAgent("a"); a.Mode != ModeWindow || a.Name != "theirs" || !got.FindProject("/p").Collapsed {
		t.Errorf("merged state lost their edit or my change: %+v", a)
	}
}

func TestSaveMergedDropsProjectRemovedByOther(t *testing.T) {
	useTempState(t)
	s := &State{}
	s.AddProject("/p")
	q, _ := s.AddProject("/q")
	_ = s.Save()
	base, since := s.Clone(), ModTime()
	time.Sleep(10 * time.Millisecond)
	_ = Update(func(o *State) error {
		o.RemoveProject(o.FindProject("/p"))
		return nil
	})
	q.Collapsed = true
	if err := s.SaveMerged(base, since); err != nil {
		t.Fatal(err)
	}
	got, _ := Load()
	if got.FindProject("/p") != nil || !got.IsIgnored("/p") || !got.FindProject("/q").Collapsed {
		t.Errorf("project removed by another came back: %d projects, ignored %v", len(got.Projects), got.Ignored)
	}
}
