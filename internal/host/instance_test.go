package host

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// A host that died hard (SIGKILL, OOM) leaves <id>.asks.json. The next
// host for the agent starts with nothing open and must not leave the dead
// host's requests standing.
func TestNewHostClearsStaleAsks(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stale := []status.Ask{{Host: status.NewHostInstance(), ID: "toolu_dead", Kind: status.AskPermission, Tool: "Bash", Command: "rm -rf build", At: time.Now().Add(-time.Hour)}}
	if err := status.WriteAsks("agent-1", stale); err != nil {
		t.Fatal(err)
	}
	h := newHost("agent-1")
	h.update(func(s *State) { s.Status = Idle })
	if a := status.ReadAsks("agent-1"); a != nil {
		t.Errorf("new host, nothing open, yet the asks file says %+v", a)
	}
}

// An answer names the host its request was read from: a request id of
// another host (codex restarts them at 0) is not this host's.
func TestAnswerOfAnotherHostIsRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h := newHost("agent-1")
	err := h.do(Command{Type: "answer", Answer: &Answer{ID: "0", Allow: true, Instance: "1.old"}})
	if err == nil || !strings.Contains(err.Error(), "answered already") {
		t.Errorf("answer for another host: %v", err)
	}
	// The asks it writes name it.
	h.update(func(s *State) { s.Requests = []Request{{ID: "0", Kind: "tool", Tool: "Bash"}} })
	if a := status.ReadAsks("agent-1"); len(a) != 1 || a[0].Host != h.instance {
		t.Errorf("asks %+v, host %q", a, h.instance)
	}
}
