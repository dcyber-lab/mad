package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

// Teams in the window: the templates, a team's channel in the snapshot
// with its roles, and what /api/team refuses.
func TestTeamAPI(t *testing.T) {
	isolate(t)
	const a1, a2 = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	proj := t.TempDir()
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: proj, Agents: []*state.Agent{
		{ID: a1, Kind: "claude", Name: "architect", Role: "architect", Team: "alpha"},
		{ID: a2, Kind: "claude", Name: "builder", Role: "builder", Team: "alpha"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Create("alpha", "the goal", []string{a1, a2}, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		t.Fatal(err)
	}
	rec := `{"teams":[{"name":"alpha","template":"dev","channel":"alpha","project":"` + proj + `","permission":"auto","created_at":"2026-10-30T00:00:00Z","roles":[
	  {"role":"architect","agent":"` + a1 + `","spec":"claude:opus","lead":true},
	  {"role":"builder","agent":"` + a2 + `","spec":"claude:sonnet"}]}]}`
	if err := os.MkdirAll(filepath.Join(paths.StateDir(), "bus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.StateDir(), "bus", "teams.json"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(manageKinds)

	s.poll.Poll(time.Now())
	var snap Snapshot
	if err := json.Unmarshal(call(t, s, "GET", "/api/deck", "").Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Bus) != 1 || snap.Bus[0].Team == nil {
		t.Fatalf("bus = %+v", snap.Bus)
	}
	bt := snap.Bus[0].Team
	if bt.Name != "alpha" || bt.Template != "dev" || len(bt.Roles) != 2 || bt.Roles[0] != (BusTeamRole{Role: "architect", Agent: a1, Lead: true}) {
		t.Errorf("team = %+v", bt)
	}
	if a := snap.Projects[0].Agents[1]; a.Team != "alpha" || a.Role != "builder" {
		t.Errorf("agent = %+v", a)
	}

	var tpl struct {
		Templates []teamTemplate
		Choices   []teamChoice
		MaxTeams  int `json:"max_teams"`
	}
	if err := json.Unmarshal(call(t, s, "GET", "/api/team/templates?project="+proj, "").Body.Bytes(), &tpl); err != nil {
		t.Fatal(err)
	}
	if len(tpl.Templates) == 0 || tpl.Templates[0].Name != "dev" || len(tpl.Templates[0].Roles) != 3 || !tpl.Templates[0].Roles[0].Lead || tpl.MaxTeams != 3 {
		t.Errorf("templates = %+v", tpl)
	}
	var opus bool
	for _, c := range tpl.Choices {
		opus = opus || c.Agent == "claude:opus" && len(c.Efforts) > 0
	}
	if !opus {
		t.Errorf("choices = %+v", tpl.Choices)
	}

	for _, tt := range []struct{ body, want string }{
		{`{"op":"dance"}`, "op is"},
		{`{"op":"start","project":"/nowhere","goal":"g"}`, "no such project"},
		{`{"op":"start","project":"` + proj + `","goal":" "}`, "needs a goal"},
		{`{"op":"rm","team":"ghost"}`, "no team"},
		{`{"op":"sleep","team":"ghost"}`, "no team"},
	} {
		w := call(t, s, "POST", "/api/team", tt.body)
		if w.Code != 400 && w.Code != 409 || !strings.Contains(w.Body.String(), tt.want) {
			t.Errorf("%s: %d %s", tt.body, w.Code, w.Body)
		}
	}
}
