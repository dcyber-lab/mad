package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/create"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/queue"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/team"
)

const createArch = "aaaa1111-0000-0000-0000-000000000001"

// createDeck is a deck of one agent, architect, with spawning and the
// team start faked: what they were asked to start is in specs and teams.
func createDeck(t *testing.T) (specs *[]drive.Spec, teams *[]team.Options) {
	t.Helper()
	isolate(t)
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: createArch, Kind: "claude", Name: "architect"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	specs, teams = new([]drive.Spec), new([]team.Options)
	oldSpawn, oldTeam, oldPoll, oldTimeout := spawnAgent, startTeam, createPoll, createTimeout
	spawnAgent = func(s drive.Spec, _ []agent.Kind) (*state.Agent, error) {
		*specs = append(*specs, s)
		a := &state.Agent{ID: fmt.Sprintf("cccc3333-0000-0000-0000-00000000000%d", len(*specs)), Kind: s.Kind, Name: s.Name}
		err := state.Update(func(st *state.State) error {
			pr, _ := st.AddProject("/code/a")
			pr.Agents = append(pr.Agents, a)
			return nil
		})
		return a, err
	}
	startTeam = func(o team.Options) (*team.Started, error) {
		*teams = append(*teams, o)
		rec := team.Record{Name: o.Name, Channel: o.Name}
		for i, r := range o.Roles {
			rec.Roles = append(rec.Roles, team.RoleRec{Role: r.Name, Agent: fmt.Sprintf("dddd4444-%d", i)})
		}
		return &team.Started{Record: rec}, nil
	}
	createPoll, createTimeout = 5*time.Millisecond, 10*time.Second
	t.Cleanup(func() { spawnAgent, startTeam, createPoll, createTimeout = oldSpawn, oldTeam, oldPoll, oldTimeout })
	return specs, teams
}

// asked runs the call as the architect in the background, waits for its
// request to show on the person's desk and returns it with the call's
// result, which arrives once the request is answered.
func asked(t *testing.T, tool, args string) (create.Request, <-chan string) {
	t.Helper()
	res := make(chan string, 1)
	go func() { res <- mcpCall(t, createArch, tool, args) }()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if l := create.List(); len(l) == 1 {
			return l[0], res
		}
		select {
		case got := <-res:
			t.Fatalf("%s returned before it was answered: %q", tool, got)
		default:
		}
	}
	t.Fatalf("%s: no request waits", tool)
	return create.Request{}, nil
}

func TestMCPCreateValidation(t *testing.T) {
	createDeck(t)
	for _, c := range []struct{ tool, args, want string }{
		{"agent_new", `{"kind":"gemini","message":"hi"}`, `no agent kind "gemini" (kinds: `},
		{"agent_new", `{"kind":"shell","message":"hi"}`, "no agent kind"},
		{"agent_new", `{"kind":"claude","message":"hi","project":"nope"}`, `no project "nope" in this deck (projects: a)`},
		{"agent_new", `{"kind":"claude","message":"  "}`, "message:"},
		{"agent_new", `{"kind":"claude","message":"hi","name":"Bad Name"}`, "lowercase letters, digits and dashes"},
		{"agent_new", `{"kind":"claude","message":"hi","name":"architect"}`, "an agent called architect exists"},
		{"agent_new", `{"kind":"claude","message":"hi","model":"--evil"}`, "not a model name"},
		{"agent_new", `{"kind":"claude","message":"hi","channel":"nowhere"}`, `no channel "nowhere"`},
		{"team_start", `{"goal":""}`, "goal:"},
		{"team_start", `{"goal":"g","project":"nope"}`, `no project "nope"`},
		{"team_start", `{"goal":"g","template":"nope"}`, `no team template "nope" (templates: dev`},
		{"team_start", `{"goal":"g","template":"dev","roles":[{"role":"a","kind":"claude"}]}`, "a template or roles, not both"},
		{"team_start", `{"goal":"g","roles":[{"role":"a","kind":"pi"}]}`, "a team works with claude and codex"},
		{"team_start", `{"goal":"g","roles":[{"role":"a","kind":"claude","lead":true},{"role":"b","kind":"codex","lead":true}]}`, "2 leads"},
		{"team_start", `{"goal":"g","roles":[{"role":"A b","kind":"claude"}]}`, "lowercase letters, digits and dashes"},
	} {
		if got := mcpCall(t, createArch, c.tool, c.args); !strings.HasPrefix(got, "ERROR ") || !strings.Contains(got, c.want) {
			t.Errorf("%s %s: %q, want an error with %q", c.tool, c.args, got, c.want)
		}
		if l := create.List(); len(l) != 0 {
			t.Fatalf("%s %s asked the person: %+v", c.tool, c.args, l)
		}
	}
}

func TestMCPAgentNewAllowed(t *testing.T) {
	specs, _ := createDeck(t)
	if _, err := bus.Create("api", "v2", []string{createArch}, bus.From{ID: createArch, Label: "architect"}); err != nil {
		t.Fatal(err)
	}
	r, res := asked(t, "agent_new", `{"kind":"claude","model":"sonnet","name":"scout","message":"find the flaky test","channel":"#api"}`)
	s := r.Spec
	if r.By != "architect" || r.ByID != createArch || s.Type != create.TypeAgent || s.Name != "scout" || s.Kind != "claude" || s.Model != "sonnet" ||
		s.Project != "/code/a" || s.ProjectName != "a" || s.Message != "find the flaky test" || s.Channel != "api" {
		t.Fatalf("request: %+v", r)
	}
	if len(*specs) != 0 {
		t.Fatal("started before the person answered")
	}
	if err := create.Answer(r.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Agent   struct{ ID, Label string }
		Channel string
	}
	got := <-res
	if err := json.Unmarshal([]byte(got), &out); err != nil || out.Agent.Label != "scout" || out.Channel != "api" {
		t.Fatalf("result %q: %v", got, err)
	}
	if len(*specs) != 1 || (*specs)[0].Kind != "claude" || (*specs)[0].Model != "sonnet" || (*specs)[0].Name != "scout" || (*specs)[0].Dir != "/code/a" {
		t.Errorf("spawned %+v", *specs)
	}
	if c, ok, _ := bus.Find("api"); !ok || !containsID(c.Members, out.Agent.ID) {
		t.Errorf("api: %+v", c)
	}
	if msgs, _, _ := bus.Read("api", 0, 10); len(msgs) == 0 || !strings.Contains(msgs[len(msgs)-1].Text, "@scout find the flaky test") {
		t.Errorf("api messages: %+v", msgs)
	}
	if l := create.List(); len(l) != 0 {
		t.Errorf("request left: %+v", l)
	}
}

func containsID(list []string, id string) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

func TestMCPAgentNewDM(t *testing.T) {
	createDeck(t)
	r, res := asked(t, "agent_new", `{"kind":"codex:gpt-5","message":"read handler.go"}`)
	if r.Spec.Kind != "codex" || r.Spec.Model != "gpt-5" || !strings.HasPrefix(r.Spec.Name, "codex-") {
		t.Fatalf("spec %+v", r.Spec)
	}
	_ = create.Answer(r.ID, true, "")
	got := <-res
	if !strings.Contains(got, `"channel": "dm.`) {
		t.Fatalf("result %q", got)
	}
	if q, _ := queue.Load("cccc3333-0000-0000-0000-000000000001"); len(q.Items) != 1 || !q.Items[0].Bus {
		t.Errorf("its queue: %+v", q)
	}
	var out struct{ Channel string }
	_ = json.Unmarshal([]byte(got), &out)
	if msgs, _, _ := bus.Read(out.Channel, 0, 10); len(msgs) != 1 || msgs[0].Text != "read handler.go" {
		t.Errorf("its DM: %+v", msgs)
	}
}

func TestMCPCreateDeclined(t *testing.T) {
	specs, teams := createDeck(t)
	r, res := asked(t, "agent_new", `{"kind":"claude","message":"hi"}`)
	_ = create.Answer(r.ID, false, "")
	if got := <-res; got != "the person declined" {
		t.Errorf("agent_new: %q", got)
	}
	r, res = asked(t, "team_start", `{"goal":"ship it"}`)
	_ = create.Answer(r.ID, false, "")
	if got := <-res; got != "the person declined" {
		t.Errorf("team_start: %q", got)
	}
	if len(*specs)+len(*teams) != 0 || len(create.List()) != 0 {
		t.Errorf("started %v %v, left %v", *specs, *teams, create.List())
	}
}

func TestMCPCreateTimesOut(t *testing.T) {
	specs, _ := createDeck(t)
	createTimeout = 50 * time.Millisecond
	if got := mcpCall(t, createArch, "agent_new", `{"kind":"claude","message":"hi"}`); got != "the person hasn't answered yet" {
		t.Errorf("got %q", got)
	}
	if len(*specs) != 0 || len(create.List()) != 0 {
		t.Errorf("started %v, left %v", *specs, create.List())
	}
}

func TestMCPTeamStart(t *testing.T) {
	_, teams := createDeck(t)
	r, res := asked(t, "team_start", `{"name":"Fix Flaky","goal":"stop the flake","roles":[{"role":"fixer","kind":"claude","model":"opus"},{"role":"checker","kind":"codex"}]}`)
	s := r.Spec
	if s.Type != create.TypeTeam || s.Name != "fix-flaky" || s.Goal != "stop the flake" || len(s.Roles) != 2 ||
		s.Roles[0] != (create.Role{Role: "fixer", Kind: "claude", Model: "opus", Lead: true}) || s.Roles[1].Lead {
		t.Fatalf("spec %+v", s)
	}
	_ = create.Answer(r.ID, true, "")
	var out struct {
		Team, Channel string
		Agents        map[string]string
	}
	got := <-res
	if err := json.Unmarshal([]byte(got), &out); err != nil || out.Team != "fix-flaky" || out.Channel != "fix-flaky" || len(out.Agents) != 2 {
		t.Fatalf("result %q: %v", got, err)
	}
	if len(*teams) != 1 {
		t.Fatalf("teams %+v", *teams)
	}
	o := (*teams)[0]
	if o.Project != "/code/a" || o.Name != "fix-flaky" || o.Goal != "stop the flake" || len(o.Roles) != 2 || o.Roles[0].Agent != "claude:opus" || !o.Roles[0].Lead || o.Roles[1].Agent != "codex" {
		t.Errorf("options %+v", o)
	}
	// By template: the request names it.
	r, res = asked(t, "team_start", `{"goal":"ship it","template":"dev"}`)
	if r.Spec.Template != "dev" || len(r.Spec.Roles) != 0 || r.Spec.Name == "" {
		t.Errorf("spec %+v", r.Spec)
	}
	_ = create.Answer(r.ID, true, "")
	<-res
	if o := (*teams)[1]; o.Template != "dev" || len(o.Roles) != 0 {
		t.Errorf("options %+v", o)
	}
}

// With agents_create_without_asking on, no one is asked.
func TestMCPCreateWithoutAsking(t *testing.T) {
	specs, _ := createDeck(t)
	if err := os.MkdirAll(filepath.Dir(paths.ConfigFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile(), []byte(`{"agents_create_without_asking": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := mcpCall(t, createArch, "agent_new", `{"kind":"claude","name":"scout","message":"hi"}`)
	if !strings.Contains(got, `"label": "scout"`) || len(*specs) != 1 || len(create.List()) != 0 {
		t.Errorf("got %q, spawned %v", got, *specs)
	}
	// Off by default.
	if err := os.WriteFile(paths.ConfigFile(), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	createTimeout = 30 * time.Millisecond
	if got := mcpCall(t, createArch, "agent_new", `{"kind":"claude","name":"scout2","message":"hi"}`); got != "the person hasn't answered yet" || len(*specs) != 1 {
		t.Errorf("got %q, spawned %v", got, *specs)
	}
}

// A spent budget refuses at once, asked or not.
func TestMCPCreateBudgetSpent(t *testing.T) {
	specs, _ := createDeck(t)
	ledger, _ := json.Marshal(bus.Spend{Day: time.Now().Format("2006-01-02"), Turns: 100000})
	if err := os.MkdirAll(filepath.Join(paths.StateDir(), "bus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StateDir(), "bus", "spend.json"), ledger, 0o644); err != nil {
		t.Fatal(err)
	}
	if bus.SpendOK(time.Now()) {
		t.Skip("the ledger did not close the budget")
	}
	for _, c := range []struct{ tool, args string }{
		{"agent_new", `{"kind":"claude","message":"hi"}`},
		{"team_start", `{"goal":"g"}`},
	} {
		if got := mcpCall(t, createArch, c.tool, c.args); !strings.HasPrefix(got, "ERROR ") || !strings.Contains(got, "budget") {
			t.Errorf("%s: %q", c.tool, got)
		}
	}
	if len(*specs) != 0 || len(create.List()) != 0 {
		t.Errorf("started %v, asked %v", *specs, create.List())
	}
}

// eventsOf are the event lines of channel, as "event ref".
func eventsOf(t *testing.T, channel string) []string {
	t.Helper()
	msgs, _, err := bus.Read(channel, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range msgs {
		if strings.HasPrefix(m.Event, "create") {
			if m.From.Kind != "system" {
				t.Errorf("event line from %+v", m.From)
			}
			out = append(out, m.Event+" "+m.Ref)
		}
	}
	return out
}

// The person's words go back as the result, the outcome stays on the
// request and, from a channel, the channel gets an event line (not the
// bot's text) for each way it ends.
func TestMCPCreateOutcomesAndEvents(t *testing.T) {
	_, teams := createDeck(t)
	if _, err := bus.Create("api", "v2", []string{createArch}, bus.From{ID: createArch, Label: "architect"}); err != nil {
		t.Fatal(err)
	}
	// from_channel must be a channel the agent is in.
	if got := mcpCall(t, createArch, "agent_new", `{"kind":"claude","message":"hi","from_channel":"nowhere"}`); !strings.Contains(got, "ERROR ") || !strings.Contains(got, "from_channel") {
		t.Errorf("unknown from_channel: %q", got)
	}

	r, res := asked(t, "agent_new", `{"kind":"claude","name":"scout","message":"hi","from_channel":"#api"}`)
	if r.From != "api" {
		t.Fatalf("from %q", r.From)
	}
	_ = create.Answer(r.ID, false, "use sonnet, not opus")
	if got := <-res; got != "the person wants changes: use sonnet, not opus" {
		t.Errorf("changes: %q", got)
	}
	if got, _ := create.Get(r.ID); got.Status != create.Changes || got.Reply != "use sonnet, not opus" {
		t.Errorf("request %+v", got)
	}

	r, res = asked(t, "agent_new", `{"kind":"claude","name":"scout","message":"hi","from_channel":"api"}`)
	_ = create.Answer(r.ID, false, "")
	if got := <-res; got != "the person declined" {
		t.Errorf("declined: %q", got)
	}

	r, res = asked(t, "agent_new", `{"kind":"claude","name":"scout","message":"hi","from_channel":"api"}`)
	_ = create.Answer(r.ID, true, "")
	var out struct{ Agent struct{ ID string } }
	if err := json.Unmarshal([]byte(<-res), &out); err != nil {
		t.Fatal(err)
	}
	if got, _ := create.Get(r.ID); got.Status != create.Created || got.Result.Agent != out.Agent.ID || got.Result.Channel == "" || got.DoneAt.IsZero() {
		t.Errorf("created: %+v", got)
	}

	r, res = asked(t, "team_start", `{"goal":"g","name":"tm","from_channel":"api"}`)
	_ = create.Answer(r.ID, true, "")
	<-res
	if got, _ := create.Get(r.ID); got.Status != create.Created || got.Result.Team != "tm" || got.Result.Channel != "tm" {
		t.Errorf("team created: %+v", got)
	}

	// A start that fails ends as failed, with its reason; the agent gets the error.
	startTeam = func(team.Options) (*team.Started, error) { return nil, fmt.Errorf("starting the fixer: boom") }
	r, res = asked(t, "team_start", `{"goal":"g","name":"tm2","from_channel":"api"}`)
	_ = create.Answer(r.ID, true, "")
	if got := <-res; !strings.HasPrefix(got, "ERROR ") || !strings.Contains(got, "boom") {
		t.Errorf("failed: %q", got)
	}
	if got, _ := create.Get(r.ID); got.Status != create.Failed || !strings.Contains(got.Result.Reason, "boom") {
		t.Errorf("failed request: %+v", got)
	}

	createTimeout = 30 * time.Millisecond
	if got := mcpCall(t, createArch, "agent_new", `{"kind":"claude","name":"late","message":"hi","from_channel":"api"}`); got != "the person hasn't answered yet" {
		t.Errorf("expired: %q", got)
	}
	var expired create.Request
	for _, x := range create.All() {
		if x.Status == create.Expired {
			expired = x
		}
	}
	if expired.ID == "" || expired.DoneAt.IsZero() {
		t.Errorf("no expired request in %+v", create.All())
	}

	want := []string{"create_changes", "create_declined", "created", "created", "create_failed", "create_expired"}
	var got []string
	for _, e := range eventsOf(t, "api") {
		ev, ref, _ := strings.Cut(e, " ")
		if !strings.HasPrefix(ref, "create:c-") {
			t.Errorf("event %q has ref %q", ev, ref)
		}
		got = append(got, ev)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("events %v, want %v", got, want)
	}
	if len(*teams) != 1 {
		t.Errorf("teams started: %d", len(*teams))
	}
	// No from_channel, no event line anywhere else.
	r, res = asked(t, "agent_new", `{"kind":"claude","name":"quiet","message":"hi"}`)
	_ = create.Answer(r.ID, true, "")
	<-res
	if n := len(eventsOf(t, "api")); n != len(want) {
		t.Errorf("api has %d event lines", n)
	}
}
