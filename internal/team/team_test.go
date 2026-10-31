package team

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/state"
)

// project makes an isolated state dir and a git repository to start
// teams in, and swaps the seams for ones that only touch the state.
func project(t *testing.T) (root string, spawned *[]drive.Spec, killed *[]string) {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("HOME", dir)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	root = filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	var specs []drive.Spec
	var kills []string
	oldSpawn, oldKill, oldSleep := spawn, kill, sleep
	spawn = func(s drive.Spec, _ []agent.Kind) (*state.Agent, error) {
		a := &state.Agent{ID: state.NewUUID(), Kind: s.Kind, Name: s.Name, Args: s.Args, Role: s.Role, Team: s.Team}
		err := state.Update(func(st *state.State) error {
			p, _ := st.AddProject(root)
			for _, b := range p.Agents {
				if b.Name == s.Name {
					return os.ErrExist
				}
			}
			p.Agents = append(p.Agents, a)
			return nil
		})
		if err != nil {
			return nil, err
		}
		specs = append(specs, s)
		return a, nil
	}
	kill = func(id string) error { kills = append(kills, id); return nil }
	sleep = func(string, bool) (bool, error) { return true, nil }
	t.Cleanup(func() { spawn, kill, sleep = oldSpawn, oldKill, oldSleep })
	return root, &specs, &kills
}

func writeTemplate(t *testing.T, root, name, body string) {
	t.Helper()
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinDev(t *testing.T) {
	dev := Builtin()[0]
	if dev.Name != "dev" || len(dev.Roles) != 3 || dev.Source != "built in" {
		t.Fatalf("dev = %+v", dev)
	}
	lead, ok := dev.LeadRole()
	if !ok || lead.Name != "architect" || lead.Model != "opus" || lead.Kind != "claude" {
		t.Errorf("lead = %+v", lead)
	}
	if r := dev.Roles[2]; r.Kind != "codex" || !r.ReadOnly || !strings.Contains(string(r.Brief), "APPROVE") {
		t.Errorf("reviewer = %+v", r)
	}
}

func TestTemplatesLoadedLikeFlows(t *testing.T) {
	root, _, _ := project(t)
	// The project's template of a built in name takes its place; another is added.
	writeTemplate(t, root, "dev", `{"name":"dev","roles":[{"name":"solo","agent":"claude:haiku","lead":true}]}`)
	writeTemplate(t, root, "pair", `{"name":"pair","roles":[{"name":"a","agent":"claude","lead":true},{"name":"b","agent":"codex@high"}]}`)
	writeTemplate(t, root, "broken", `{"name":"broken","roles":[]}`)
	list, err := Templates(root)
	if err == nil || !strings.Contains(err.Error(), "broken.json") {
		t.Errorf("err = %v, want the broken file named", err)
	}
	var names []string
	for _, tp := range list {
		names = append(names, tp.Name)
	}
	if strings.Join(names, " ") != "dev pair" {
		t.Errorf("templates = %v", names)
	}
	if len(list[0].Roles) != 1 || !strings.HasSuffix(list[0].Source, "dev.json") {
		t.Errorf("dev = %+v", list[0])
	}
	if tp, ok := ByName(root, "pair"); !ok || tp.Roles[1].Effort != "high" {
		t.Errorf("pair = %+v %v", tp, ok)
	}
}

func TestCheck(t *testing.T) {
	role := func(name, agent string, lead bool) string {
		l := ""
		if lead {
			l = `,"lead":true`
		}
		return `{"name":"` + name + `","agent":"` + agent + `"` + l + `}`
	}
	team := func(roles ...string) string {
		return `{"name":"t","roles":[` + strings.Join(roles, ",") + `]}`
	}
	var seven []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		seven = append(seven, role(n, "claude", n == "a"))
	}
	tests := []struct{ name, in, want string }{
		{"ok", team(role("a", "claude:opus", true), role("b", "codex", false)), ""},
		{"no lead", team(role("a", "claude", false)), "no lead"},
		{"two leads", team(role("a", "claude", true), role("b", "claude", true)), "2 leads"},
		{"twice", team(role("a", "claude", true), role("a", "codex", false)), "role a: twice"},
		{"too many", team(seven...), "7 roles"},
		{"no roles", team(), "no roles"},
		{"unknown kind", team(role("a", "gemini", true)), `agent "gemini"`},
		{"no kind", team(role("a", "", true)), "no agent"},
		{"bad effort", team(role("a", "claude@loud", true)), "effort"},
		{"bad name", `{"name":"T","roles":[` + role("a", "claude", true) + `]}`, "name"},
		{"placeholder", `{"name":"t","kickoff":"{{task}}","roles":[` + role("a", "claude", true) + `]}`, "{{task}}"},
		{"unknown field", `{"name":"t","step":1,"roles":[]}`, "not a team"},
	}
	for _, tt := range tests {
		_, err := Parse([]byte(tt.in))
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%s: %v", tt.name, err)
		case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestStart(t *testing.T) {
	root, specs, _ := project(t)
	s, err := Start(Options{Project: root, Goal: "add a --version flag to tool.py", Permission: run.PermAllowlist,
		Agents: map[string]string{"builder": "claude:haiku@low"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "add-a-version-flag-to-tool-py" && !strings.HasPrefix(s.Name, "add-a-version") {
		t.Errorf("name = %q", s.Name)
	}
	if len(*specs) != 3 {
		t.Fatalf("spawned %d", len(*specs))
	}
	for i, want := range []string{"architect", "builder", "reviewer"} {
		sp := (*specs)[i]
		if sp.Name != want || sp.Role != want || sp.Team != s.Name || sp.Dir != root || sp.Branch != "" {
			t.Errorf("spec %d = %+v", i, sp)
		}
	}
	// Models, efforts and permissions as a run's roles get them.
	b := (*specs)[1]
	if b.Model != "haiku" || !strings.Contains(strings.Join(b.Args, " "), "--effort low") || !strings.Contains(strings.Join(b.Args, " "), "acceptEdits") {
		t.Errorf("builder = %+v", b)
	}
	if r := (*specs)[2]; r.Kind != "codex" || !strings.Contains(strings.Join(r.Args, " "), "read-only") {
		t.Errorf("reviewer = %+v", r)
	}

	// The channel: topic, members, lead.
	st, _ := state.Load()
	c, ok, _ := bus.Find(s.Channel)
	if !ok || len(c.Members) != 3 || c.Topic != "add a --version flag to tool.py" {
		t.Fatalf("channel = %+v", c)
	}
	_, lead := st.FindAgent(c.Lead)
	if lead == nil || lead.Role != "architect" || lead.Team != s.Name {
		t.Errorf("lead = %+v", lead)
	}
	// One kickoff, by you, mentioning only the lead.
	msgs, _, _ := bus.Read(s.Channel, 0, 50)
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v", msgs)
	}
	m := msgs[0]
	if m.From.ID != bus.You || len(m.Mentions) != 1 || m.Mentions[0] != c.Lead || !strings.HasPrefix(m.Text, "@architect Goal: add a --version flag") {
		t.Errorf("kickoff = %+v", m)
	}
	// The record.
	list, _ := Running(nil)
	if len(list) != 1 || list[0].Template != "dev" || list[0].Permission != "allowlist" || list[0].Roles[1].Spec != "claude:haiku@low" || list[0].Roles[2].Agent == "" {
		t.Errorf("records = %+v", list)
	}
}

func TestStartNamesAreUnique(t *testing.T) {
	root, specs, _ := project(t)
	// A name in the project is taken: the role name gets the team's.
	if err := state.Update(func(st *state.State) error {
		p, _ := st.AddProject(root)
		p.Agents = append(p.Agents, &state.Agent{ID: state.NewUUID(), Kind: "claude", Name: "builder"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err := Start(Options{Project: root, Name: "one", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*specs)[1].Name; got != "builder-one" {
		t.Errorf("builder named %q", got)
	}
	if (*specs)[0].Name != "architect" {
		t.Errorf("architect named %q", (*specs)[0].Name)
	}
	// Brief names the agent as the bus does.
	st, _ := state.Load()
	if b := Brief(st, s.Roles[0].Agent); !strings.Contains(b, "@builder-one") {
		t.Errorf("brief = %q", b)
	}
	// The team's name is taken, by a team, or by a channel.
	if _, err := Start(Options{Project: root, Name: "one", Goal: "g"}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("same name: %v", err)
	}
	if _, err := bus.Create("two", "", nil, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(Options{Project: root, Name: "two", Goal: "g"}); err == nil {
		t.Error("a channel's name was taken for a team")
	}
	// Named by the goal, the same goal twice makes two teams.
	a, err := Start(Options{Project: root, Goal: "fix the parser"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Start(Options{Project: root, Goal: "fix the parser"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "fix-the-parser" || b.Name != "fix-the-parser-2" {
		t.Errorf("names = %q, %q", a.Name, b.Name)
	}
	if _, err := Start(Options{Project: root, Goal: "one more"}); err == nil || !strings.Contains(err.Error(), "3 teams") {
		t.Errorf("a fourth team: %v", err)
	}
}

func TestStartLimitsAndErrors(t *testing.T) {
	root, specs, _ := project(t)
	for _, tt := range []struct {
		o    Options
		want string
	}{
		{Options{Project: root}, "needs a goal"},
		{Options{Project: root, Goal: "g", Template: "nope"}, "no team template"},
		{Options{Project: root, Goal: "g", Permission: "yolo"}, "permission"},
		{Options{Project: root, Goal: "g", Agents: map[string]string{"cook": "claude"}}, "no role"},
		{Options{Project: root, Goal: "g", Agents: map[string]string{"builder": "pi"}}, "unknown agent"},
		{Options{Project: filepath.Join(root, "missing"), Goal: "g"}, "not a directory"},
	} {
		if _, err := Start(tt.o); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: err = %v, want %q", tt.o, err, tt.want)
		}
	}
	if len(*specs) != 0 {
		t.Errorf("spawned %d", len(*specs))
	}
}

func TestStartFailureCleansUp(t *testing.T) {
	root, specs, killed := project(t)
	inner := spawn
	spawn = func(s drive.Spec, k []agent.Kind) (*state.Agent, error) {
		if s.Role == "reviewer" {
			return nil, os.ErrPermission
		}
		return inner(s, k)
	}
	if _, err := Start(Options{Project: root, Name: "doomed", Goal: "g"}); err == nil || !strings.Contains(err.Error(), "starting the reviewer") {
		t.Fatalf("err = %v", err)
	}
	if len(*specs) != 2 || len(*killed) != 2 {
		t.Errorf("spawned %d, killed %d", len(*specs), len(*killed))
	}
	st, _ := state.Load()
	if n := len(st.FindProject(root).Agents); n != 0 {
		t.Errorf("%d agents left", n)
	}
	if list, _ := Running(nil); len(list) != 0 {
		t.Errorf("records = %+v", list)
	}
	if chans, _ := bus.Channels(); len(chans) != 0 {
		t.Errorf("channels = %+v", chans)
	}
}

func TestStartInAWorktree(t *testing.T) {
	root, specs, _ := project(t)
	s, err := Start(Options{Project: root, Goal: "g", Branch: "team/g"})
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(root, ".claude", "worktrees", "team-g")
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Fatalf("no worktree: %v", err)
	}
	for _, sp := range *specs {
		if sp.Dir != wt || sp.Branch != "" {
			t.Errorf("%s runs in %s, branch %q", sp.Name, sp.Dir, sp.Branch)
		}
	}
	if s.Branch != "team/g" {
		t.Errorf("branch = %q", s.Branch)
	}
}

func TestBrief(t *testing.T) {
	root, _, _ := project(t)
	s, err := Start(Options{Project: root, Name: "alpha", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := state.Load()
	lead, build, review := s.Roles[0].Agent, s.Roles[1].Agent, s.Roles[2].Agent

	got := Brief(st, build)
	for _, want := range []string{
		"You are the builder of team alpha, working in #alpha",
		"The lead is architect (@architect)",
		"reviewer (@reviewer, codex, read only): You review diffs you are asked to",
		"Your part: You implement what @architect hands you",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("builder's brief lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "builder (@") {
		t.Errorf("a teammate list names the agent itself:\n%s", got)
	}
	if b := Brief(st, lead); !strings.Contains(b, "You are the lead") || !strings.Contains(b, "builder (@builder, claude sonnet): You implement") {
		t.Errorf("lead's brief:\n%s", b)
	}
	if b := Brief(st, review); !strings.Contains(b, "APPROVE or CHANGES") {
		t.Errorf("reviewer's brief:\n%s", b)
	}
	// An agent in no team has none; one of a gone team neither.
	if Brief(st, "nobody") != "" {
		t.Error("a brief for nobody")
	}
	if err := os.Remove(recordsFile()); err != nil {
		t.Fatal(err)
	}
	if Brief(st, lead) != "" {
		t.Error("a brief without a record")
	}
}

func TestSleepAndRemove(t *testing.T) {
	root, _, killed := project(t)
	s, err := Start(Options{Project: root, Name: "alpha", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	var slept []string
	sleep = func(id string, away bool) (bool, error) {
		if away {
			t.Error("away")
		}
		slept = append(slept, id)
		return true, nil
	}
	n, err := Sleep("alpha")
	if err != nil || n != 3 || len(slept) != 3 {
		t.Errorf("slept %d %v: %v", n, slept, err)
	}
	if _, err := Sleep("nope"); err == nil {
		t.Error("slept a team that is not")
	}
	// Still there: agents, channel, record.
	if st, _ := state.Load(); len(st.FindProject(root).Agents) != 3 {
		t.Error("sleep removed agents")
	}

	if err := Remove("alpha"); err != nil {
		t.Fatal(err)
	}
	if len(*killed) != 3 {
		t.Errorf("killed %v", *killed)
	}
	if st, _ := state.Load(); len(st.FindProject(root).Agents) != 0 {
		t.Error("agents are left")
	}
	if chans, _ := bus.Channels(); len(chans) != 0 {
		t.Errorf("channel is listed: %+v", chans)
	}
	if c, ok, _ := bus.Find("alpha"); !ok || !c.Archived || len(c.Members) != 0 {
		t.Errorf("channel = %+v", c)
	}
	if list, _ := Running(nil); len(list) != 0 {
		t.Errorf("records = %+v", list)
	}
	if err := Remove("alpha"); err == nil {
		t.Error("removed twice")
	}
	_ = s
}

func TestRemovingAnAgentUpdatesTheRecord(t *testing.T) {
	root, _, _ := project(t)
	s, err := Start(Options{Project: root, Name: "alpha", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	gone := s.Roles[1].Agent
	if err := state.Update(func(st *state.State) error { st.RemoveAgent(gone); return nil }); err != nil {
		t.Fatal(err)
	}
	list, _ := Running(nil)
	if len(list) != 1 || len(list[0].Roles) != 2 {
		t.Fatalf("records = %+v", list)
	}
	for _, r := range list[0].Roles {
		if r.Agent == gone {
			t.Error("the role stays")
		}
	}
	if c, _, _ := bus.Find("alpha"); len(c.Members) != 2 {
		t.Errorf("members = %v", c.Members)
	}
	// All gone: the team and its channel with them.
	if err := state.Update(func(st *state.State) error {
		for _, r := range list[0].Roles {
			st.RemoveAgent(r.Agent)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if list, _ := Running(nil); len(list) != 0 {
		t.Errorf("records = %+v", list)
	}
	if chans, _ := bus.Channels(); len(chans) != 0 {
		t.Errorf("channels = %+v", chans)
	}
	if _, err := os.Stat(filepath.Join(paths.StateDir(), "bus", "teams.json")); err != nil {
		t.Error(err)
	}
}

// Messages name agents by name across the whole deck: a team started in
// one project must not reuse a role's name an agent of another project
// has, or the lead's kickoff "@architect" names two agents.
func TestStartNamesAreUniqueAcrossProjects(t *testing.T) {
	root, _, _ := project(t)
	if _, err := Start(Options{Project: root, Goal: "first goal"}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(root), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", other}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	// The seam adds agents to root's project; add the other project's own.
	prev := spawn
	spawn = func(s drive.Spec, k []agent.Kind) (*state.Agent, error) {
		a := &state.Agent{ID: state.NewUUID(), Kind: s.Kind, Name: s.Name, Role: s.Role, Team: s.Team}
		err := state.Update(func(st *state.State) error {
			p, _ := st.AddProject(other)
			p.Agents = append(p.Agents, a)
			return nil
		})
		return a, err
	}
	defer func() { spawn = prev }()
	got, err := Start(Options{Project: other, Goal: "second goal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 0 {
		t.Errorf("notes = %v", got.Notes)
	}
	st, _ := state.Load()
	seen := map[string]bool{}
	for _, p := range st.Projects {
		for _, a := range p.Agents {
			if seen[a.Name] {
				t.Errorf("two agents called %q", a.Name)
			}
			seen[a.Name] = true
		}
	}
	msgs, _, _ := bus.Read(got.Channel, 0, 50)
	if len(msgs) != 1 {
		t.Errorf("kickoff not posted: %+v", msgs)
	}
}

func TestTeamNameTypedOnAPhone(t *testing.T) {
	project(t)
	for in, want := range map[string]string{"Ios": "ios", " My Team ": "my-team", "api_v2": "api-v2", "开发ios": "ios", "开发 iOS 版": "ios", "--a--b--": "a-b"} {
		got, err := teamName(in, "goal", nil)
		if err != nil || got != want {
			t.Errorf("teamName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got, err := teamName("开发", "make the api", nil); err != nil || got == "" || !nameRe.MatchString(got) {
		t.Errorf("an all-Chinese name = %q, %v; want one from the goal", got, err)
	}
}
