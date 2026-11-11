package rounds

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// No test asks the real tmux server whether someone is at the deck.
func TestMain(m *testing.M) {
	Watched = func() bool { return false }
	os.Exit(m.Run())
}

// newDeck is a deck of one project, /code/a, in a state directory of the
// test's own, its rounds not yet made. set says how an agent stands, and
// facts is what a keeper's poll would have found.
func newDeck(t *testing.T) (r *Rounds, st *state.State, set func(id, status string), facts func() Facts) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("CODEX_HOME", "")
	st = &state.State{}
	st.AddProject("/code/a")
	agents := map[string]Agent{}
	set = func(id, s string) { agents[id] = Agent{Status: s} }
	facts = func() Facts { return Facts{State: st, Kinds: agent.Builtin(), Agents: agents} }
	return New(), st, set, facts
}

// The first tick makes every round; the ones after make each when its time
// has come round again, and not while its job is still out. The deck is
// asked for once at most, and not at all when no round needs it.
func TestDue(t *testing.T) {
	r, st, set, facts := newDeck(t)
	st.Projects[0].Agents = []*state.Agent{{ID: "one", Kind: "claude"}}
	set("one", status.Running)
	asked := 0
	counted := func() Facts { asked++; return facts() }
	cfg := Config{SleepAfter: time.Hour}

	t0 := time.Now()
	jobs := r.Due(t0, cfg, counted)
	// Nobody to put to sleep and nothing queued: the lapse check and the
	// follow-ups are what there is to do.
	if len(jobs) != 2 || !r.lapsing || !r.following || r.sleeping || r.dispatching {
		t.Fatalf("first tick: %d jobs, lapsing=%v following=%v sleeping=%v dispatching=%v", len(jobs), r.lapsing, r.following, r.sleeping, r.dispatching)
	}
	if asked != 1 {
		t.Errorf("the deck was asked for %d times in one tick", asked)
	}
	for _, j := range jobs {
		if j.Stirs {
			t.Error("a lapse check or a follow-up says it stirs the panes")
		}
		if out := r.Done(j.Run(), t0); out.Poll || out.Err != nil || out.Spent {
			t.Errorf("outcome %+v", out)
		}
	}
	if r.lapsing || r.following {
		t.Fatal("still out after Done")
	}

	// Half a second on, nothing is due, and the deck is not asked for.
	asked = 0
	if jobs := r.Due(t0.Add(500*time.Millisecond), cfg, counted); len(jobs) != 0 || asked != 0 {
		t.Errorf("half a second later: %d jobs, the deck asked for %d times", len(jobs), asked)
	}
	// A second on, the queues are looked at (none: no job, no deck).
	if jobs := r.Due(t0.Add(dispatchEvery), cfg, counted); len(jobs) != 0 || asked != 0 || !r.lastDispatch.Equal(t0.Add(dispatchEvery)) {
		t.Errorf("a second later: %d jobs, asked %d, queues last looked at %v", len(jobs), asked, r.lastDispatch)
	}
	// Ten seconds on, the lapses again; while that job is out, not once more.
	if jobs := r.Due(t0.Add(lapseEvery), cfg, counted); len(jobs) != 1 || !r.lapsing {
		t.Fatalf("ten seconds later: %d jobs, lapsing=%v", len(jobs), r.lapsing)
	}
	if jobs := r.Due(t0.Add(2*lapseEvery), cfg, counted); len(jobs) != 0 {
		t.Errorf("a second lapse check started while the first was out: %d jobs", len(jobs))
	}
}
