package deck

import (
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/tmux"
)

func TestWorkUnder(t *testing.T) {
	ps := strings.Join([]string{
		"    1     0     1 /sbin/launchd",
		"  100     1   100 tmux",
		"  200   100   200 claude", // an idle agent: only its MCP server
		"  201   200   200 node",
		"  300   100   300 claude", // running a command
		"  301   300   300 node",
		"  302   300   302 /bin/zsh",
		"  303   302   302 go",
		"  400   100   400 node", // npm's codex, the real one under it
		"  401   400   400 codex",
		"  402   401   402 -bash",
		"  500   100   500 /bin/sh", // the pane's own shell doesn't count
		"  501   500   500 sleep",
		"  600     1   600 /Applications/Some App.app/Contents/MacOS/Some App",
		"garbage",
	}, "\n")
	for pid, want := range map[int]string{200: "", 300: "go", 400: "bash", 500: "", 600: "", 999: ""} {
		if got := workUnder(procTable(ps), pid); got != want {
			t.Errorf("workUnder(%d) = %q, want %q", pid, got, want)
		}
	}
	if got := procTable(ps)[600].name(); got != "Some App" {
		t.Errorf("name with a space = %q", got)
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
		return workUnder(procTable(string(out)), pb.PID) != ""
	}, "busy agent's command starts")

	var be *BusyError
	if ok, err := SleepAgent(busy.ID, true); ok || !errors.As(err, &be) || be.Command != "sleep" {
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

func TestStop(t *testing.T) {
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl not installed")
	}
	useDeck(t)
	old := stopGrace
	stopGrace = 500 * time.Millisecond
	t.Cleanup(func() { stopGrace = old })
	// tmux's hangup reaches the pane's process group: 619 and the agent.
	// 617 runs in a group of its own and 618 ignores the hangup.
	kind := []agent.Kind{{Name: "fake", Start: `sh -c 'perl -e "setpgrp(0,0); exec q(sleep), 617" & nohup sleep 618 >/dev/null 2>&1 & sleep 619'`}}
	st := &state.State{}
	p, _ := st.AddProject(os.TempDir())
	a := &state.Agent{ID: "a", Kind: "fake"}
	p.Agents = []*state.Agent{a}
	if err := StartAgent(p, a, false, kind); err != nil {
		t.Fatal(err)
	}
	sleeps := func() []string {
		out, _ := processes()
		var found []string
		for _, pr := range procTable(string(out)) {
			if pr.name() != "sleep" {
				continue
			}
			args, _ := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pr.pid)).Output()
			for _, n := range []string{"617", "618", "619"} {
				if strings.TrimSpace(string(args)) == "sleep "+n {
					found = append(found, n)
				}
			}
		}
		sort.Strings(found)
		return found
	}
	waitFor(t, func() bool { return len(sleeps()) == 3 }, "the agent's sleeps start")

	names, err := Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || !strings.HasPrefix(names[0], "sleep ") || !strings.HasPrefix(names[1], "sleep ") {
		t.Errorf("Stop named %q, want the two sleeps that outlived the deck", names)
	}
	if left := sleeps(); len(left) > 0 {
		t.Errorf("still running after Stop: sleep %v", left)
	}
	if tmux.Run("list-sessions") == nil {
		t.Error("the deck still runs")
	}
}
