package creator

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/create"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/state"
)

const asker = "aaaa1111-0000-0000-0000-000000000001"

func setup(t *testing.T) (spawned *int, mu *sync.Mutex) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: asker, Kind: "claude", Name: "architect"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	spawned, mu = new(int), new(sync.Mutex)
	old := Spawn
	Spawn = func(s drive.Spec, _ []agent.Kind) (*state.Agent, error) {
		mu.Lock()
		defer mu.Unlock()
		*spawned++
		return &state.Agent{ID: fmt.Sprintf("cccc3333-0000-0000-0000-00000000000%d", *spawned), Kind: s.Kind, Name: s.Name}, nil
	}
	t.Cleanup(func() { Spawn = old })
	return spawned, mu
}

// The race that was: the request is due when a sweep lists it, and the
// person's Allow lands before the sweep acts. The sweep must leave it, and
// the start must create the agent and end the request as created.
func TestSweepDoesNotExpireWhatWasAllowed(t *testing.T) {
	spawned, _ := setup(t)
	r, _ := create.Add("architect", asker, "", create.Spec{Type: create.TypeAgent, Name: "scout", Kind: "claude", Project: "/code/a", Message: "hi"})
	r.At = time.Now().Add(-create.Expiry - time.Minute)
	_ = create.Put(r)
	listed := create.All()[0]
	if err := create.Answer(r.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := state.Load()
	sweepOne(listed, st) // acts on what it listed: waiting and due
	if got, _ := create.Get(r.ID); got.Status != create.Allowed {
		t.Fatalf("the sweep changed it: %+v", got)
	}
	Settle(r.ID)
	got, _ := create.Get(r.ID)
	if got.Status != create.Created || got.Result.Agent == "" || *spawned != 1 {
		t.Fatalf("after the start: %+v, spawned %d", got, *spawned)
	}
	// And the sweep, still holding a stale listing, cannot undo it.
	sweepOne(listed, st)
	if got, _ := create.Get(r.ID); got.Status != create.Created {
		t.Errorf("the sweep undid it: %+v", got)
	}
}

// Two processes starting one allowed request start it once.
func TestRunStartsOnce(t *testing.T) {
	spawned, mu := setup(t)
	r, _ := create.Add("architect", asker, "", create.Spec{Type: create.TypeAgent, Name: "scout", Kind: "claude", Project: "/code/a", Message: "hi"})
	if err := create.Answer(r.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); Settle(r.ID) }()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if got, _ := create.Get(r.ID); *spawned != 1 || got.Status != create.Created {
		t.Errorf("spawned %d, %+v", *spawned, got)
	}
}

// How a request ended reaches the asking agent: it is woken and its digest carries mad's word, for a request
// made, and for one declined.
func TestTheAskerIsToldHowItEnded(t *testing.T) {
	setup(t)
	digest := func() string {
		t.Helper()
		text, _, ok, err := bus.Compose(asker, time.Now())
		if err != nil || !ok {
			t.Fatalf("the asker's digest is empty: %q %v %v", text, ok, err)
		}
		return text
	}
	r, _ := create.Add("architect", asker, "", create.Spec{Type: create.TypeAgent, Name: "scout", Kind: "claude", Project: "/code/a", Message: "hi"})
	if err := create.Answer(r.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	Settle(r.ID)
	text := digest()
	if !strings.Contains(text, "your request "+r.ID+" to create agent scout") || !strings.Contains(text, "was created") {
		t.Errorf("made: %q", text)
	}
	if _, upto, _, _ := bus.Compose(asker, time.Now()); upto != nil {
		_ = bus.Delivered(asker, upto)
	}
	r2, _ := create.Add("architect", asker, "", create.Spec{Type: create.TypeAgent, Name: "scout2", Kind: "claude", Project: "/code/a", Message: "hi"})
	if err := create.Answer(r2.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	Settle(r2.ID)
	text = digest()
	if !strings.Contains(text, "your request "+r2.ID+" to create agent scout2") || !strings.Contains(text, "declined") {
		t.Errorf("declined: %q", text)
	}
}
