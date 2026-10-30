package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPCheckin(t *testing.T) {
	arch, build := boardDeck(t)
	mcpCall(t, arch, "bus_channel", `{"action":"create","channel":"dev","members":["builder"]}`)

	// The schema, as the client sees it.
	var tool struct {
		InputSchema struct {
			Required   []string
			Properties map[string]json.RawMessage
		}
	}
	found := false
	for _, tl := range mcpTools(arch) {
		if tl.Name == "bus_checkin" {
			found = true
			if err := json.Unmarshal([]byte(`{"inputSchema":`+string(tl.InputSchema)+`}`), &tool); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found || len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "action" {
		t.Fatalf("bus_checkin schema = %+v", tool)
	}
	for _, p := range []string{"action", "in", "at", "note", "channel", "id"} {
		if _, ok := tool.InputSchema.Properties[p]; !ok {
			t.Errorf("no property %q", p)
		}
	}

	// Book, list, cancel.
	got := mcpCall(t, arch, "bus_checkin", `{"action":"book","in":"20m","note":"look at CI on PR 42"}`)
	var booked struct{ ID, At, Channel string }
	if err := json.Unmarshal([]byte(got), &booked); err != nil || !strings.HasPrefix(booked.ID, "r-") || booked.Channel != "dm."+arch+".you" {
		t.Fatalf("book = %q %v", got, err)
	}
	got = mcpCall(t, arch, "bus_checkin", `{"action":"book","in":"6h","note":"verify the deploy","channel":"#dev"}`)
	if !strings.Contains(got, `"channel": "dev"`) {
		t.Errorf("book in a channel = %q", got)
	}
	if got := mcpCall(t, arch, "bus_checkin", `{"action":"list"}`); !strings.Contains(got, booked.ID) || !strings.Contains(got, "look at CI on PR 42") {
		t.Errorf("list = %q", got)
	}
	if got := mcpCall(t, build, "bus_checkin", `{"action":"list"}`); strings.Contains(got, booked.ID) {
		t.Errorf("another agent sees it: %q", got)
	}
	if got := mcpCall(t, arch, "bus_who", `{}`); !strings.Contains(got, `"checkins"`) || !strings.Contains(got, booked.ID) {
		t.Errorf("bus_who lacks the check-in: %q", got)
	}
	for _, bad := range []string{
		`{"action":"book","note":"when?"}`,
		`{"action":"book","in":"20m","at":"15:00","note":"both"}`,
		`{"action":"book","in":"1m","note":"too soon"}`,
		`{"action":"book","in":"20m"}`,
		`{"action":"book","in":"20m","note":"x","channel":"nowhere"}`,
		`{"action":"frobnicate"}`,
	} {
		if got := mcpCall(t, arch, "bus_checkin", bad); !strings.HasPrefix(got, "ERROR ") {
			t.Errorf("%s = %q", bad, got)
		}
	}
	mcpCall(t, arch, "bus_checkin", `{"action":"book","in":"30m","note":"third"}`)
	if got := mcpCall(t, arch, "bus_checkin", `{"action":"book","in":"40m","note":"fourth"}`); !strings.HasPrefix(got, "ERROR ") || !strings.Contains(got, "3 open") {
		t.Errorf("4th = %q", got)
	}
	if got := mcpCall(t, build, "bus_checkin", `{"action":"cancel","id":"`+booked.ID+`"}`); !strings.HasPrefix(got, "ERROR ") {
		t.Errorf("cancelled another's: %q", got)
	}
	if got := mcpCall(t, arch, "bus_checkin", `{"action":"cancel","id":"`+booked.ID+`"}`); strings.HasPrefix(got, "ERROR ") || strings.Contains(got, "look at CI on PR 42") {
		t.Errorf("cancel = %q", got)
	}
}

func TestRoutineCommands(t *testing.T) {
	arch, _ := boardDeck(t)
	mcpCall(t, arch, "bus_channel", `{"action":"create","channel":"dev","members":["builder"]}`)

	code, out, errs := run(t, "", "routine", "add", "weekdays 09:00", "dev", "@builder", "status", "please")
	if code != 0 || !strings.Contains(out, "weekdays 09:00") {
		t.Fatalf("add = %d %q %q", code, out, errs)
	}
	id := strings.Fields(out)[0]
	if code, out, _ := run(t, "", "routine", "add", "every 30m", "dev", "--report"); code != 0 {
		t.Fatalf("add --report = %d %q", code, out)
	}
	for _, bad := range [][]string{
		{"add"}, {"add", "weekdays 09:00"}, {"add", "every 5m", "dev", "hi"}, {"add", "weekdays 09:00", "nowhere", "hi"},
		{"add", "sometime", "dev", "hi"}, {"add", "daily 09:00", "dev", "hi", "--report"}, {"frob"}, {"rm"}, {"fire", "r-nothing"},
	} {
		if code, _, _ := run(t, "", append([]string{"routine"}, bad...)...); code == 0 {
			t.Errorf("routine %v succeeded", bad)
		}
	}
	code, out, _ = run(t, "", "routine", "ls")
	if code != 0 || !strings.Contains(out, id) || !strings.Contains(out, "@builder status please") || !strings.Contains(out, "(progress report)") || !strings.Contains(out, "#dev") {
		t.Fatalf("ls = %d %q", code, out)
	}
	if code, _, _ := run(t, "", "routine", "pause", id); code != 0 {
		t.Fatal("pause")
	}
	if _, out, _ := run(t, "", "routine", "ls"); !strings.Contains(out, "paused") {
		t.Errorf("ls after pause: %q", out)
	}
	if code, _, _ := run(t, "", "routine", "resume", id); code != 0 {
		t.Fatal("resume")
	}
	// Fire now: the post is in the channel.
	if code, out, errs := run(t, "", "routine", "fire", id); code != 0 || !strings.Contains(out, "fired") {
		t.Fatalf("fire = %d %q %q", code, out, errs)
	}
	if _, out, _ := run(t, "", "bus", "read", "dev"); !strings.Contains(out, "@builder status please") {
		t.Errorf("channel after fire: %q", out)
	}

	// Check-ins: the agents' own.
	mcpCall(t, arch, "bus_checkin", `{"action":"book","in":"20m","note":"look at CI"}`)
	if _, out, _ := run(t, "", "routine", "checkins"); !strings.Contains(out, "architect") || !strings.Contains(out, "look at CI") {
		t.Errorf("checkins = %q", out)
	}

	// Inside an agent a routine is not its to set.
	t.Setenv("MAD_AGENT_ID", arch)
	for _, args := range [][]string{{"add", "daily 09:00", "dev", "hi"}, {"pause", id}, {"fire", id}} {
		if code, _, errs := run(t, "", append([]string{"routine"}, args...)...); code == 0 || !strings.Contains(errs, "person's") {
			t.Errorf("agent ran routine %v: %d %q", args, code, errs)
		}
	}
	if code, _, _ := run(t, "", "routine", "rm", id); code == 0 {
		t.Error("an agent removed the person's routine")
	}
	t.Setenv("MAD_AGENT_ID", "")
	if code, _, _ := run(t, "", "routine", "rm", id); code != 0 {
		t.Error("rm")
	}
	if _, out, _ := run(t, "", "routine", "ls"); strings.Contains(out, id) {
		t.Errorf("ls after rm: %q", out)
	}
}
