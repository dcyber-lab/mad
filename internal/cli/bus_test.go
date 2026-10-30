package cli

import (
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/state"
)

func TestBusHandoffs(t *testing.T) {
	isolate(t)
	const a, b, c = "aaaa1111-0000-0000-0000-000000000001", "bbbb2222-0000-0000-0000-000000000002", "cccc3333-0000-0000-0000-000000000003"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: a, Kind: "claude", Name: "architect"}, {ID: b, Kind: "claude", Name: "builder"}, {ID: c, Kind: "codex", Name: "tester"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "", "bus", "handoffs"); code != 0 || out != "" {
		t.Fatalf("no handoffs = %d %q", code, out)
	}
	h1, _, err := bus.HandOff(bus.From{ID: a, Label: "architect"}, b, "design the api\nin detail", "", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	h2, _, err := bus.HandOff(bus.From{ID: b, Label: "builder"}, c, "test the api", "", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bus.Done(bus.From{ID: c, Label: "tester"}, h2.ID, "all green", "done"); err != nil {
		t.Fatal(err)
	}
	want := h1.ID + " architect → builder [open] design the api\n  " + h2.ID + " builder → tester [done] test the api\n"
	if code, out, errs := run(t, "", "bus", "handoffs"); code != 0 || out != want {
		t.Errorf("handoffs = %d %q %q, want %q", code, out, errs, want)
	}
}

func TestBusCommands(t *testing.T) {
	isolate(t)
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{
		{ID: "aaaa1111-0000-0000-0000-000000000001", Kind: "claude", Name: "architect"},
		{ID: "bbbb2222-0000-0000-0000-000000000002", Kind: "claude", Name: "builder"},
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	if code, out, _ := run(t, "", "bus", "create", "API", "-t", "the api", "architect"); code != 0 || out != "api\n" {
		t.Fatalf("create = %d %q", code, out)
	}
	code, out, errs := run(t, "", "bus", "post", "#api", "@builder", "look", "at", "this")
	if code != 0 || out != "#1 in api; woke builder\n" {
		t.Fatalf("post = %d %q %q", code, out, errs)
	}
	if code, out, _ = run(t, "ok it is then, a longer line\n", "bus", "post", "api"); code != 0 || !strings.HasPrefix(out, "#2 in api") {
		t.Fatalf("post from stdin = %d %q", code, out)
	}
	code, out, _ = run(t, "", "bus", "read", "api", "-n", "1")
	if code != 0 || !strings.Contains(out, "#2 ") || strings.Contains(out, "#1 ") {
		t.Errorf("read = %d %q", code, out)
	}
	code, out, _ = run(t, "", "bus", "log", "api")
	if code != 0 || !strings.Contains(out, "you: @builder look at this") {
		t.Errorf("log = %d %q", code, out)
	}
	if code, out, _ = run(t, "", "bus", "dm", "builder", "a private word for you"); code != 0 || !strings.Contains(out, "woke builder") {
		t.Errorf("dm = %d %q", code, out)
	}
	code, out, _ = run(t, "", "bus", "ls")
	if code != 0 || !strings.Contains(out, "api") || !strings.Contains(out, "architect, builder") || !strings.Contains(out, "#2") {
		t.Errorf("ls = %d %q", code, out)
	}
	if code, _, _ = run(t, "", "bus", "resume", "api"); code != 0 {
		t.Errorf("resume = %d", code)
	}

	// Inside an agent's shell it speaks as that agent.
	t.Setenv("MAD_AGENT_ID", "bbbb2222-0000-0000-0000-000000000002")
	if code, _, _ = run(t, "", "bus", "post", "api", "@architect done with the handler, see the diff"); code != 0 {
		t.Fatal("post as agent")
	}
	if _, out, _ = run(t, "", "bus", "read", "api"); !strings.Contains(out, "builder: @architect done") {
		t.Errorf("read = %q", out)
	}

	if code, _, _ = run(t, "", "bus"); code != 2 {
		t.Errorf("no args: code %d", code)
	}
	if code, _, _ = run(t, "", "bus", "post", "nope", "hello there everyone"); code == 0 {
		t.Error("posted to no channel")
	}
}
