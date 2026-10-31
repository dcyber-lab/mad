package attention

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// Every prompt the fixtures hold looks like one, read whole or not; no
// screen at work or at rest does.
func TestShowsPrompt(t *testing.T) {
	for file, asks := range map[string]bool{
		"claude-bash.txt": true, "claude-bash-long.txt": true, "claude-bash-parallel.txt": true, "claude-write.txt": true,
		"claude-ask.txt": true, "claude-ask-two.txt": true, "claude-ask-multi.txt": true,
		"codex-approval.txt": true, "codex-trust.txt": true, "codex-update.txt": true,
		"claude-working.txt": false, "claude-idle.txt": false, "codex-working.txt": false,
	} {
		if got := status.ShowsPrompt(fixture(t, file)); got != asks {
			t.Errorf("%s: %v", file, got)
		}
	}
}

// claude's PreToolUse hook comes a moment before its question is drawn: a
// look between them sees the screen move with no prompt on it. Its
// tracker waits on through that, so the question drawn after it waits,
// and Interrupt sends nothing onto it.
func TestInterruptPaneHookBeforePrompt(t *testing.T) {
	claude := agent.ByName(agent.Builtin(), "claude")
	alive := tmux.Pane{ID: "%7"}
	t0 := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	hook := &status.Hook{State: status.Waiting, At: t0} // PreToolUse AskUserQuestion
	spin1 := fixture(t, "claude-working.txt")
	spin2 := strings.Replace(spin1, "(5s ·", "(6s ·", 1)
	form := fixture(t, "claude-ask-two.txt")
	steps := []struct {
		screen string
		at     time.Duration
	}{{spin1, -time.Second}, {spin2, 300 * time.Millisecond}, {form, 1300 * time.Millisecond}, {form, 3 * time.Second}, {form, 20 * time.Second}}
	var tr status.Tracker
	var s string
	for _, st := range steps {
		s = tr.Observe(claude, alive, true, hook, st.screen, false, t0.Add(st.at))
		if st.at > 0 && s != status.Waiting {
			t.Errorf("t+%v: %s, want waiting", st.at, s)
		}
	}
	k := newKeyed(t, form, transcript.Info{})
	// Whatever its tracker says: the screen decides.
	for _, working := range []bool{s == status.Running, true} {
		k.sent = nil
		if err := InterruptPane("%7", working); !errors.Is(err, ErrAsks) || len(k.sent) > 0 {
			t.Errorf("working %v: %v, sent %q onto a prompt", working, err, k.sent)
		}
	}
}

// claude 2.1.312: its PreToolUse hook (running) at 352.727, the prompt
// for a command of 35 lines on its screen by 353.388, its permission
// Notification (waiting) only at 358.998. In between its tracker says it
// runs; the prompt runs off the top of the screen, so it is not read.
// Interrupt sends nothing onto it all the same.
func TestInterruptPaneUnreadPrompt(t *testing.T) {
	claude := agent.ByName(agent.Builtin(), "claude")
	alive := tmux.Pane{ID: "%7"}
	p := time.Unix(1793407352, 726000000)
	pre := &status.Hook{State: status.Running, At: p}
	long := fixture(t, "claude-bash-long.txt")
	var tr status.Tracker
	tr.Observe(claude, alive, true, pre, fixture(t, "claude-working.txt"), false, p.Add(-300*time.Millisecond))
	tr.Observe(claude, alive, true, pre, long, false, p.Add(700*time.Millisecond))
	s := tr.Observe(claude, alive, true, pre, long, false, p.Add(5*time.Second))
	if _, ok := parsePrompt(long); ok {
		t.Fatal("read whole: not the case this tests")
	}
	k := newKeyed(t, long, transcript.Info{})
	k.sent = nil
	if err := InterruptPane("%7", s == status.Running); !errors.Is(err, ErrAsks) || len(k.sent) > 0 {
		t.Errorf("tracker %s: %v, sent %q onto a permission prompt", s, err, k.sent)
	}
}
