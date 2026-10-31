package status

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The fixtures in testdata are frames of a real claude 2.1.312 pane
// (haiku), captured every 10ms while it called AskUserQuestion:
//
//	37.549  spinning   its spinner
//	37.605  hook       its spinner "(running PreToolUse hook · 1s)", to 37.640
//	37.614             the PreToolUse hook: waiting
//	37.640  drawn      the question, "Enter to select"
//
// A look (every 500ms in the deck, 1s in the window) that comes in
// 37.614..37.640 sees the hook's frame: the screen moved since it asked,
// and shows no prompt yet.
func frame(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "claude-ask-"+name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// One frame without the prompt is not an answer: the question drawn after
// it waits, however long.
func TestWaitBeforeItsPromptIsDrawn(t *testing.T) {
	spinning, hook, drawn := frame(t, "spinning"), frame(t, "hook"), frame(t, "drawn")
	base := time.Unix(1793407037, 0)
	at := func(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }
	running := &Hook{State: Running, At: at(-1000)}
	asked := &Hook{State: Waiting, At: at(614)}
	var tr Tracker
	tr.Observe(claude, alive, true, running, spinning, false, at(120))
	for _, c := range []struct {
		screen string
		ms     int
	}{{hook, 630}, {drawn, 1130}, {drawn, 5630}, {drawn, 15630}, {drawn, 60630}} {
		if s := tr.Observe(claude, alive, true, asked, c.screen, false, at(c.ms)); s != Waiting {
			t.Errorf("37.%03d+: %s, want waiting", c.ms, s)
		}
	}
}

// Answered: its prompt off a moving screen for answerSettle, never from
// one look; a prompt on it again is waiting again, nothing kept.
func TestWaitAnsweredOnlyOnceSettled(t *testing.T) {
	hook, drawn := frame(t, "hook"), frame(t, "drawn")
	asked := &Hook{State: Waiting, At: t0}
	var tr Tracker
	tr.Observe(claude, alive, true, asked, drawn, false, t0.Add(time.Second))
	moving := func(n int) string { return hook + string(rune('a'+n)) }
	look := func(screen string, d time.Duration) string {
		return tr.Observe(claude, alive, true, asked, screen, false, t0.Add(d))
	}
	if s := look(moving(0), 2*time.Second); s != Waiting || !tr.Clearing() {
		t.Errorf("one look off it: %s clearing %v", s, tr.Clearing())
	}
	if s := look(moving(1), 3*time.Second); s != Waiting || !tr.Clearing() {
		t.Errorf("off it 1s: %s clearing %v", s, tr.Clearing())
	}
	if s := look(moving(2), 2*time.Second+answerSettle); s != Running || tr.Clearing() {
		t.Errorf("off it %v: %s clearing %v", answerSettle, s, tr.Clearing())
	}
	// Drawn again (the same wait, no new hook): waiting again, and the
	// next frame without it is one look again, not an answer.
	if s := look(drawn, 5*time.Second); s != Waiting || tr.Clearing() {
		t.Errorf("drawn again: %s clearing %v", s, tr.Clearing())
	}
	if s := look(moving(3), 6*time.Second); s != Waiting {
		t.Errorf("off it again, one look: %s", s)
	}
}
