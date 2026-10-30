package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/state"
)

func TestCommandsAPI(t *testing.T) {
	dir := isolate(t)
	st := &state.State{Projects: []*state.Project{{Name: "p", Path: dir, Agents: []*state.Agent{
		{ID: "a1", Kind: "claude"}, {ID: "a2", Kind: "claude"},
	}}}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	asked := 0
	release := make(chan struct{})
	findCommands = func(_ context.Context, kind, d string) ([]discover.Command, error) {
		mu.Lock()
		asked++
		n := asked
		mu.Unlock()
		<-release
		if n == 2 {
			return nil, errors.New("claude broke")
		}
		return []discover.Command{{Name: "model", Description: kind + " in " + d}}, nil
	}
	t.Cleanup(func() { findCommands = discover.Commands })

	s := New(agent.Builtin())
	h := s.Handler()
	look := func(id string) []discover.Command {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/commands?id="+id+"&token="+s.Token, nil))
		var got []discover.Command
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatalf("GET /api/commands: %d %s", w.Code, w.Body)
		}
		return got
	}

	// Two agents of a kind in a directory, looking at once: asked once.
	var wg sync.WaitGroup
	res := make([][]discover.Command, 2)
	for i, id := range []string{"a1", "a2"} {
		wg.Add(1)
		go func() { defer wg.Done(); res[i] = look(id) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for _, got := range res {
		if len(got) != 1 || got[0].Description != "claude in "+dir {
			t.Fatalf("got %+v", got)
		}
	}
	if asked != 1 {
		t.Fatalf("asked %d times, want once", asked)
	}

	// Old: given while it is asked again. Asking failed: the answer before
	// stays, and it is asked again sooner.
	key := "claude\x00" + dir
	s.cmds.lists[key].at = time.Now().Add(-commandsFresh - time.Second)
	if got := look("a1"); len(got) != 1 {
		t.Fatalf("old answer: %+v", got)
	}
	waitFor(t, "asked again", func() bool {
		s.cmds.mu.Lock()
		defer s.cmds.mu.Unlock()
		return s.cmds.lists[key].done() && s.cmds.lists[key].err != nil
	})
	if got := look("a1"); len(got) != 1 {
		t.Errorf("after a failure: %+v, want the answer before", got)
	}
	if !s.cmds.lists[key].stale(time.Now().Add(commandsRetry + time.Second)) {
		t.Error("a failure is not asked again")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/commands?id=nope&token="+s.Token, nil))
	if w.Code != 404 {
		t.Errorf("unknown agent: %d", w.Code)
	}
}
