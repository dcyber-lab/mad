package rounds

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// An agent sleeps only if waking it reopens its own session: claude's is
// its id, agy's and codex's must have been learnt.
func TestCanSleep(t *testing.T) {
	cases := []struct {
		a    state.Agent
		want bool
	}{
		{state.Agent{ID: "a", Kind: "claude"}, true},
		{state.Agent{ID: "b", Kind: "agy"}, false},
		{state.Agent{ID: "c", Kind: "agy", SessionID: "ec33ebf9"}, true},
		{state.Agent{ID: "d", Kind: "codex"}, false},
		{state.Agent{ID: "e", Kind: "codex", SessionID: "019a"}, true},
		{state.Agent{ID: "f", Kind: "pi"}, false},
		{state.Agent{ID: "g", Kind: "shell"}, false},
	}
	for _, c := range cases {
		if got := CanSleep(agent.Builtin(), &c.a); got != c.want {
			t.Errorf("%s %+v: CanSleep = %v", c.a.Kind, c.a, got)
		}
	}
}

func TestSleepIdleAgents(t *testing.T) {
	r, st, _, _ := newDeck(t)
	for _, id := range []string{"old", "stage", "run", "wait", "sh", "new", "busy", "role"} {
		a := &state.Agent{ID: id, Kind: "claude"}
		switch id {
		case "sh":
			a.Kind = "shell"
		case "role":
			a.Run = "r1"
		}
		st.Projects[0].Agents = append(st.Projects[0].Agents, a)
	}
	var calls []string
	old := SleepAgent
	t.Cleanup(func() { SleepAgent = old })
	var fail error
	SleepAgent = func(id string, away bool) (bool, error) {
		calls = append(calls, fmt.Sprintf("%s away=%v", id, away))
		if id == "busy" {
			return false, &deck.BusyError{Command: "node"}
		}
		return true, fail
	}

	// The deck an hour and a minute after its keeper first saw it.
	t0 := time.Now()
	later := t0.Add(time.Hour + time.Minute)
	agents := map[string]Agent{
		"old":   {Status: status.Idle, QuietSince: t0},
		"stage": {Status: status.Idle, QuietSince: t0, Seen: true},
		"run":   {Status: status.Running, QuietSince: later},
		"wait":  {Status: status.Waiting, QuietSince: t0},
		"sh":    {Status: status.Idle, QuietSince: t0}, // a shell has no session to reopen
		"new":   {Status: status.Idle, QuietSince: t0.Add(31 * time.Minute)},
		"busy":  {Status: status.Idle, QuietSince: t0},
		"role":  {Status: status.Idle, QuietSince: t0},
	}
	working := true
	facts := func() Facts {
		return Facts{State: st, Kinds: agent.Builtin(), Agents: agents, RunActive: func(run string) bool { return working && run == "r1" }}
	}
	cfg := Config{SleepAfter: time.Hour}

	if got := strings.Join(Sleepers(later, cfg.SleepAfter, facts()), ","); got != "old,busy" {
		t.Errorf("sleepers = %q", got)
	}
	// One in front of someone stays awake.
	seen := agents["old"]
	seen.Seen = true
	agents["old"] = seen
	if got := strings.Join(Sleepers(later, cfg.SleepAfter, facts()), ","); got != "busy" {
		t.Errorf("sleepers with old in front of someone = %q", got)
	}
	seen.Seen = false
	agents["old"] = seen
	// A run that is over no longer keeps its agents awake.
	working = false
	if got := strings.Join(Sleepers(later, cfg.SleepAfter, facts()), ","); got != "old,busy,role" {
		t.Errorf("sleepers once the run ended = %q", got)
	}
	working = true

	job := r.sleep(later, cfg, facts)
	if job == nil || !r.sleeping || !job.Stirs {
		t.Fatal("no sleep round started, or one that says it leaves the panes alone")
	}
	res := job.Run()
	if got := strings.Join(calls, ","); got != "old away=true,busy away=true" {
		t.Errorf("slept %q", got)
	}
	// One kept awake by what runs under it is no failure: it is left for
	// the next check.
	if out := r.Done(res, later); r.sleeping || out.Err != nil || !out.Poll {
		t.Errorf("after the round: sleeping=%v outcome=%+v", r.sleeping, out)
	}
	if r.sleep(t0.Add(time.Minute), cfg, facts) != nil || r.sleeping {
		t.Error("a round started with nobody idle long enough")
	}
	// A failure ends the round and is the keeper's to tell.
	fail = errors.New("no such pane")
	if out := r.Done(r.sleep(later, cfg, facts).Run(), later); out.Err == nil || !out.Poll {
		t.Errorf("a failed round: %+v", out)
	}

	// A tick looks only when sleep.after is set, every sleepCheckEvery.
	fail, r.lastSleepCheck = nil, time.Time{}
	r.Due(later, Config{}, facts)
	if !r.lastSleepCheck.IsZero() {
		t.Error("checked with sleep.after unset")
	}
	r.Due(later, cfg, facts)
	if !r.lastSleepCheck.Equal(later) || !r.sleeping {
		t.Error("no check with sleep.after set")
	}
	if n := len(calls); n != 3 {
		t.Fatalf("calls = %q", calls)
	}
	r.Due(later.Add(sleepCheckEvery), cfg, facts)
	if !r.lastSleepCheck.Equal(later) {
		t.Error("a second check while the first round was out")
	}
}
