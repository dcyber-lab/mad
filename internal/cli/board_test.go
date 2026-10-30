package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/state"
)

// boardDeck is a deck of two agents.
func boardDeck(t *testing.T) (arch, build string) {
	t.Helper()
	isolate(t)
	arch, build = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: arch, Kind: "claude", Name: "architect"}, {ID: build, Kind: "codex", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	return arch, build
}

type taskRow struct {
	ID, Title, Owner, Status, Derived, Channel, Handoff, Age, Note string
}

func listTasks(t *testing.T, as, args string) []taskRow {
	t.Helper()
	var rows []taskRow
	got := mcpCall(t, as, "task_list", args)
	if err := json.Unmarshal([]byte(got), &rows); err != nil {
		t.Fatalf("task_list %s = %q: %v", args, got, err)
	}
	return rows
}

func TestMCPTasks(t *testing.T) {
	arch, build := boardDeck(t)

	// Make, change, list.
	got := mcpCall(t, arch, "task_set", `{"title":"review the api","owner":"builder","channel":"#dev","links":[{"kind":"pr","ref":"#12"}]}`)
	var task struct{ ID, Title, Owner, Status, Channel, Project string }
	if err := json.Unmarshal([]byte(got), &task); err != nil || !strings.HasPrefix(task.ID, "t-") || task.Owner != build ||
		task.Status != "todo" || task.Channel != "dev" || task.Project != "/code/a" {
		t.Fatalf("task_set = %q %v", got, err)
	}
	rows := listTasks(t, arch, `{}`)
	if len(rows) != 0 {
		t.Errorf("architect owns %+v", rows)
	}
	rows = listTasks(t, build, `{}`)
	if len(rows) != 1 || rows[0].ID != task.ID || rows[0].Owner != "builder" || rows[0].Derived != "todo" || rows[0].Channel != "dev" {
		t.Fatalf("builder's tasks = %+v", rows)
	}
	if got := mcpCall(t, build, "task_set", fmt.Sprintf(`{"id":%q,"status":"doing","note":"started"}`, task.ID)); !strings.Contains(got, `"status": "doing"`) {
		t.Errorf("task_set doing = %q", got)
	}
	if rows = listTasks(t, build, `{}`); rows[0].Derived != "doing" || rows[0].Note != "started" {
		t.Errorf("after doing = %+v", rows)
	}
	if rows = listTasks(t, arch, `{"scope":"all"}`); len(rows) != 1 {
		t.Errorf("all = %+v", rows)
	}
	if rows = listTasks(t, arch, `{"scope":"channel","channel":"dev"}`); len(rows) != 1 {
		t.Errorf("channel dev = %+v", rows)
	}
	if rows = listTasks(t, arch, `{"scope":"channel","channel":"other"}`); len(rows) != 0 {
		t.Errorf("channel other = %+v", rows)
	}
	for _, args := range []string{`{"scope":"channel"}`, `{"scope":"some"}`} {
		if got := mcpCall(t, arch, "task_list", args); !strings.HasPrefix(got, "ERROR ") {
			t.Errorf("task_list %s = %q", args, got)
		}
	}
	for _, args := range []string{`{}`, `{"title":"x","owner":"nobody"}`, `{"title":"x","status":"nope"}`, `{"id":"t-none","note":"x"}`} {
		if got := mcpCall(t, arch, "task_set", args); !strings.HasPrefix(got, "ERROR ") {
			t.Errorf("task_set %s = %q", args, got)
		}
	}

	// Handing the task over by bus_handoff attaches to it; then only
	// bus_done closes it.
	got = mcpCall(t, arch, "bus_handoff", fmt.Sprintf(`{"to":"builder","task":"do the review","task_id":%q}`, task.ID))
	var res struct{ Handoff, Task string }
	if err := json.Unmarshal([]byte(got), &res); err != nil || res.Task != task.ID {
		t.Fatalf("bus_handoff = %q %v", got, err)
	}
	if got := mcpCall(t, build, "task_set", fmt.Sprintf(`{"id":%q,"status":"done"}`, task.ID)); !strings.HasPrefix(got, "ERROR ") || !strings.Contains(got, "bus_done") {
		t.Errorf("closed relayed work: %q", got)
	}
	mcpCall(t, build, "bus_done", fmt.Sprintf(`{"handoff":%q,"result":"looks fine\nsee notes"}`, res.Handoff))
	if rows = listTasks(t, build, `{}`); len(rows) != 1 || rows[0].Status != "done" || rows[0].Note != "looks fine" {
		t.Errorf("after bus_done = %+v", rows)
	}

	// Owner you; bus_who counts open tasks per agent.
	mcpCall(t, arch, "task_set", `{"title":"pick a name","owner":"you"}`)
	mcpCall(t, arch, "task_set", `{"title":"another","owner":"builder"}`)
	if rows = listTasks(t, arch, `{"scope":"all"}`); len(rows) != 3 {
		t.Errorf("all = %+v", rows)
	}
	var who struct {
		Agents []struct {
			Label string
			Tasks int
		}
	}
	if err := json.Unmarshal([]byte(mcpCall(t, arch, "bus_who", `{}`)), &who); err != nil || len(who.Agents) != 1 || who.Agents[0].Tasks != 1 {
		t.Errorf("bus_who = %+v %v", who, err)
	}
}

func TestBoardCommand(t *testing.T) {
	arch, _ := boardDeck(t)
	code, out, _ := run(t, "", "board")
	if code != 0 || !strings.Contains(out, "Nothing on the board") {
		t.Fatalf("empty board = %d %q", code, out)
	}
	mcpCall(t, arch, "task_set", `{"title":"review the api","owner":"builder","channel":"dev","status":"review"}`)
	mcpCall(t, arch, "task_set", `{"title":"pick a name","owner":"you"}`)
	mcpCall(t, arch, "task_set", `{"title":"later","owner":"builder"}`)
	mcpCall(t, arch, "bus_dm", `{"to":"you","text":"is the schema ok?"}`)
	code, out, errs := run(t, "", "board")
	if code != 0 {
		t.Fatalf("board = %d %q", code, errs)
	}
	// The report heads it, a blank line ends it.
	header, rest, _ := strings.Cut(out, "\n\n")
	if !strings.HasPrefix(header, "4 open · 2 waiting for you · 1 review · 1 todo\ndev: 1 review\n") {
		t.Errorf("header = %q", header)
	}
	var heads []string
	for _, l := range strings.Split(strings.TrimSpace(rest), "\n") {
		if !strings.HasPrefix(l, " ") {
			heads = append(heads, l)
		}
	}
	if strings.Join(heads, ",") != "Waiting for you,Review,Todo" {
		t.Errorf("columns = %v:\n%s", heads, out)
	}
	// Waiting for you is the inbox, as the window and the deck list it.
	for _, want := range []string{"pick a name · yours · now · t-", "architect · in your DM · is the schema ok? · now", "review the api · builder · #dev · now", "later · builder"} {
		if !strings.Contains(out, want) {
			t.Errorf("board lacks %q:\n%s", want, out)
		}
	}
}
