package deck

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestShellUnder(t *testing.T) {
	ps := strings.Join([]string{
		"    1     0 /sbin/launchd",
		"  100     1 tmux",
		"  200   100 claude", // an idle agent: only its MCP server
		"  201   200 node",
		"  300   100 claude", // running a command
		"  301   300 node",
		"  302   300 /bin/zsh",
		"  303   302 go",
		"  400   100 node", // npm's codex, the real one under it
		"  401   400 codex",
		"  402   401 -bash",
		"  500   100 /bin/sh", // the pane's own shell doesn't count
		"  501   500 sleep",
		"  600     1 /Applications/Some App.app/Contents/MacOS/Some App",
		"garbage",
	}, "\n")
	for pid, want := range map[int]string{200: "", 300: "zsh", 400: "bash", 500: "", 600: "", 999: ""} {
		if got := shellUnder(ps, pid); got != want {
			t.Errorf("shellUnder(%d) = %q, want %q", pid, got, want)
		}
	}
}

func TestSleepAgent(t *testing.T) {
	useDeck(t)
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	idle := &state.Agent{ID: "idle", Kind: "fake"}
	busy := &state.Agent{ID: "busy", Kind: "fake"}
	p.Agents = []*state.Agent{idle, busy}
	for _, a := range p.Agents {
		if err := StartAgent(p, a, false, fakeKind); err != nil {
			t.Fatal(err)
		}
	}
	// A shell under the agent: a command it runs.
	busyKind := []agent.Kind{{Name: "fake", Start: `sh -c 'sh -c "sleep 600; true"; true'`}}
	if pb, _ := tmux.FindPane(panes(t), busy.ID); tmux.Run("respawn-pane", "-k", "-t", pb.ID, busyKind[0].Start) != nil {
		t.Fatal("respawn busy agent")
	}
	waitFor(t, func() bool {
		out, _ := processes()
		pb, _ := tmux.FindPane(panes(t), busy.ID)
		return shellUnder(string(out), pb.PID) != ""
	}, "busy agent's command starts")

	var be *BusyError
	if ok, err := SleepAgent(busy.ID, true); ok || !errors.As(err, &be) || be.Shell != "sh" {
		t.Errorf("busy agent: %v %v", ok, err)
	}

	// Opened since it was picked: left alone by the idle check, not by z.
	if err := OpenAgent(st, idle.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	if ok, err := SleepAgent(idle.ID, true); ok || err != nil {
		t.Errorf("idle check on stage: %v %v", ok, err)
	}
	if ok, err := SleepAgent(idle.ID, false); !ok || err != nil {
		t.Fatalf("z on stage: %v %v", ok, err)
	}
	if got := stageID(t); got != tmux.IDPlaceholder {
		t.Errorf("stage after z = %q", got)
	}
	waitFor(t, func() bool {
		pa, _ := tmux.FindPane(panes(t), idle.ID)
		return pa.Dead && pa.Asleep
	}, "agent asleep")
	if ok, err := SleepAgent(idle.ID, true); ok || err != nil {
		t.Errorf("already asleep: %v %v", ok, err)
	}
	if ok, err := SleepAgent("never-started", true); ok || err != nil {
		t.Errorf("unknown agent: %v %v", ok, err)
	}

	// Opening it wakes it up, and the mark goes.
	if err := OpenAgent(st, idle.ID, fakeKind); err != nil {
		t.Fatal(err)
	}
	if pa, _ := tmux.FindPane(panes(t), idle.ID); pa.Dead || pa.Asleep || stageID(t) != idle.ID {
		t.Errorf("woken agent = %+v", pa)
	}
	if pb, _ := tmux.FindPane(panes(t), busy.ID); pb.Dead || pb.Asleep {
		t.Errorf("busy agent = %+v", pb)
	}
}
