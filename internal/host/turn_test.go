package host

import (
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/status"
)

// A hosted agent's turn ends with its reply written as a turn, as a hook
// would, so the deck's finished push can say what was said.
func TestHostedTurnWritesTheReply(t *testing.T) {
	h, _ := fakeHost(t)
	waitState(t, h, "ready", func(s State) bool { return s.Status == Idle && len(s.Models) == 2 })
	if err := h.d.send("hi", nil); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, h, "a permission request", func(s State) bool { return s.Status == Waiting && len(s.Requests) == 1 })
	if err := h.d.answer(Answer{ID: s.Requests[0].ID, Allow: true}); err != nil {
		t.Fatal(err)
	}
	s = waitState(t, h, "the turn's end", func(s State) bool { return s.Status == Idle && len(s.Requests) == 0 })
	var turn *status.Turn
	for i := 0; i < 100 && turn == nil; i++ { // written just after the state shows idle
		if turn = status.ReadTurn("agent-1"); turn == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if turn == nil || turn.Reply == "" || !strings.Contains(turn.Reply, s.Message.Blocks[0].Text) {
		t.Errorf("turn = %+v, want the reply %q", turn, s.Message.Blocks[0].Text)
	}
}
