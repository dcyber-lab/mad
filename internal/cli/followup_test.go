package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBusLeadAndReport(t *testing.T) {
	arch, build := boardDeck(t)
	// The agent that makes a channel leads it; bus_who says so.
	mcpCall(t, arch, "bus_channel", `{"action":"create","channel":"dev","members":["builder"]}`)
	lead := func(as string) string {
		var who struct{ Channels []struct{ ID, Lead string } }
		if err := json.Unmarshal([]byte(mcpCall(t, as, "bus_who", `{}`)), &who); err != nil || len(who.Channels) != 1 {
			t.Fatalf("bus_who: %+v %v", who, err)
		}
		return who.Channels[0].Lead
	}
	if got := lead(build); got != "architect" {
		t.Errorf("lead = %q, want the creator", got)
	}
	// bus_channel lead: another agent, or me.
	if got := mcpCall(t, arch, "bus_channel", `{"action":"lead","channel":"dev","members":["builder"]}`); !strings.Contains(got, `"lead": "`+build+`"`) {
		t.Errorf("lead builder = %q", got)
	}
	if got := lead(arch); got != "builder" {
		t.Errorf("lead = %q", got)
	}
	mcpCall(t, arch, "bus_channel", `{"action":"lead","channel":"dev","members":["me"]}`)
	if got := lead(build); got != "architect" {
		t.Errorf("lead me = %q", got)
	}
	// mad bus lead CHANNEL AGENT.
	if code, _, errs := run(t, "", "bus", "lead", "dev", "builder"); code != 0 {
		t.Fatalf("bus lead = %d %q", code, errs)
	}
	if got := lead(arch); got != "builder" {
		t.Errorf("lead after mad bus lead = %q", got)
	}
	if code, _, _ := run(t, "", "bus", "lead", "dev", "nobody"); code == 0 {
		t.Error("lead of an agent that does not exist")
	}
	if code, _, _ := run(t, "", "bus", "lead", "dev"); code == 0 {
		t.Error("bus lead without an agent")
	}

	// mad bus report: nothing open, then a line from mad in the channel.
	if code, out, _ := run(t, "", "bus", "report", "dev"); code != 0 || !strings.Contains(out, "Nothing open in #dev") {
		t.Fatalf("empty report = %d %q", code, out)
	}
	mcpCall(t, arch, "task_set", `{"title":"review the api","owner":"builder","channel":"dev","status":"review"}`)
	code, out, errs := run(t, "", "bus", "report", "dev")
	if code != 0 || strings.TrimSpace(out) != "dev: 1 review" {
		t.Fatalf("report = %d %q %q", code, out, errs)
	}
	code, out, _ = run(t, "", "bus", "read", "dev")
	if code != 0 || !strings.Contains(out, "mad: dev: 1 review") {
		t.Errorf("channel = %q", out)
	}
	// With no channel: the whole report, each channel's part in its own.
	code, out, _ = run(t, "", "bus", "report")
	if code != 0 || !strings.HasPrefix(out, "1 open · 1 review\ndev: 1 review") {
		t.Errorf("full report = %q", out)
	}
	if _, out, _ = run(t, "", "bus", "read", "dev"); strings.Count(out, "mad: dev: 1 review") != 2 {
		t.Errorf("channel after both = %q", out)
	}
}
